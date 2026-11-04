// FileService 真实实现
// 设计见 docs/文件系统模块设计方案V1.0.md §8.1 / §4.5
//
// ⚠️ **三个入口(UploadToken / Receive / Register)共用同一个 key 构造路径**
// (`buildFileKey`)。它们只是"字节从哪来"不同 —— 服务端写、客户端直传、
// 分片合并。key 的规则一旦三处各写一份,改前缀时必漏一处,漏掉的那处
// 会把文件写到别人目录里或写到不该写的段去,而且没有任何报错。
package service

import (
	"context"
	"errors"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/oss"
	"rapid-admin/internal/pkg/sonyflake"
)

// fileService 真实实现
type fileService struct {
	db   *gorm.DB
	prov oss.Provider
	opt  FileOptions
	// folders 共用同一个实例,保证 ensureFolder 的懒创建只有一份实现
	folders FolderService
	// transcodeSlots 转码并发闸(M145)
	//
	// ⚠️ **每个转码是一个 ffmpeg 进程,吃满一个核** —— 没有闸的话,
	// 20 个用户同时传 wmv 就是 20 个 ffmpeg 一起跑,那台机器就废了。
	// 容量 = TranscodeWorkers。
	transcodeSlots chan struct{}
}

// NewFileService 构造
func NewFileService(db *gorm.DB, prov oss.Provider, opt FileOptions) FileService {
	o := opt.normalize()
	return &fileService{
		db:      db,
		prov:    prov,
		opt:     o,
		folders: NewFolderService(db, prov, o),
		// ⚠️ **容量为 0 = 转码整个关着** —— 但那样 enqueueTranscode 根本不会派活,
		// 所以这里给个至少 1 的容量,免得"忘了初始化"变成"永远阻塞"。
		// 真正判断开关的是 enqueueTranscode 里那一句 TranscodeWorkers <= 0。
		transcodeSlots: make(chan struct{}, max(o.TranscodeWorkers, 1)),
	}
}

func (s *fileService) UploadToken(ctx context.Context, caller *Caller, req FileUploadTokenReq) (*oss.UploadToken, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	if s.prov == nil {
		return nil, errcode.New(errcode.Internal, "存储未初始化")
	}
	if err := s.checkDeclaredSize(req.FileSize); err != nil {
		return nil, err
	}
	folder, err := s.folders.ensureFolder(ctx, caller, req.FolderID, req.Biz, "")
	if err != nil {
		return nil, err
	}
	key, err := s.buildFileKey(uid, folder.Biz, folder.ID, req.FileName)
	if err != nil {
		return nil, err
	}
	// ⚠️ **这里不预扣配额**。直传是客户端拿着 token 直接怼到 OSS,服务端在这
	// 一刻根本不知道文件最终会有多大、多久传完 —— 预扣等于拿一个假的承诺
	// 当真。真要防超额,该在 Register 时按存储里的实际大小算,那时候拿得到真值。
	// 代价是:恶意客户端可以只签 token 不 Register,拿到一堆 OSS 垃圾对象。
	// 那个洞由"生命周期规则"兜(见 §13 风险),不靠在这里假装拦住。
	return s.prov.SignUploadToken(ctx, key, s.opt.UploadTokenTTL, s.opt.MaxFileSize)
}

// Receive 服务端中转上传。
func (s *fileService) Receive(ctx context.Context, caller *Caller, req FileReceiveReq) (*FileView, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	if s.prov == nil {
		return nil, errcode.New(errcode.Internal, "存储未初始化")
	}
	if req.Body == nil {
		return nil, errcode.New(errcode.ParamMissing, "文件内容为空")
	}
	if err := s.checkDeclaredSize(req.Size); err != nil {
		return nil, err
	}
	folder, err := s.folders.ensureFolder(ctx, caller, req.FolderID, req.Biz, "")
	if err != nil {
		return nil, err
	}
	key, err := s.buildFileKey(uid, folder.Biz, folder.ID, req.FileName)
	if err != nil {
		return nil, err
	}

	// 预扣(§4.5 ①)。申报 0 时不预扣 —— 传完按实际收,照样拦得住超配额,
	// 只是拒得晚一点(内容已经落到磁盘上了)。
	held := req.Size
	if held > 0 {
		ok, err := holdQuota(s.db.WithContext(ctx), int64(uid), held)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errcode.New(errcode.InvalidParam, "存储空间不足")
		}
	}

	ct := probeContentType(req.FileName, req.ContentType)

	// ⚠️ **媒体探测要留一份本地副本**(M141)—— ffmpeg 只能读本地文件,
	//   而字节此刻正从客户端流过来。所以 tee 一份到临时文件,传完再探。
	//
	//   这不是"顺手做的":没有副本就没有时长和封面,而这两个正是这个功能的
	//   全部产出。代价是"临时盘要留一份文件大小" —— 由 media_max_size_mb 兜
	//   (mediaTemp.Write 超限就停写,副本不会无限长)。
	//
	//   ⚠️ **建副本失败只记日志** —— 探测是锦上添花,不能让它把上传搞失败。
	var tmp *mediaTemp
	// ⚠️ **tmpOwned = 副本的清理权已经交给转码 worker 了**(M145)。
	//   两边都删的话,worker 转码时拿到的是"文件不存在",
	//   ffmpeg 报 exit status 1,而真因是"文件被人删了"。
	tmpOwned := false
	defer func() {
		if tmp != nil && !tmpOwned {
			tmp.Cleanup()
		}
	}()
	body := req.Body
	if s.mediaProbeEligible(req.FileName, req.ContentType, req.Size) {
		t, tErr := newMediaTemp(s.opt.MediaTmpDir, req.FileName, s.opt.MediaMaxSize)
		if tErr != nil {
			logger.L().Warn("file: 建媒体临时副本失败(不影响上传)",
				zap.String("file_name", req.FileName), zap.Error(tErr))
		} else {
			tmp = t
			// ⚠️ **Cleanup 挂在 defer 上** —— 探测在这之后、函数返回之前,
			//   defer 保证副本在探测用完之后被删掉(哪怕探测 panic 也不会留垃圾)。
			body = io.TeeReader(req.Body, tmp)
		}
	}

	meta, putErr := s.prov.Put(ctx, key, body, ct)
	if putErr != nil {
		// ⚠️ **写失败必须把预扣退回去**。漏这一步的话,每次磁盘满都会凭空
		// 吃掉用户一份配额,而用户什么文件都没传上去 —— 表现是"配额越来越少"。
		if held > 0 {
			if rErr := releaseQuota(s.db.WithContext(ctx), int64(uid), held); rErr != nil {
				return nil, rErr
			}
		}
		return nil, errcode.New(errcode.Internal, "写入存储失败").WithDetail(putErr.Error())
	}

	// ⚠️ **字节落完就关掉副本的句柄**(M145)——
	//
	//   副本是给 ffprobe/ffmpeg 按**路径**读的,它们自己开自己的句柄,
	//   我们这个句柄留着没用(而且 tee 写也已经结束了)。
	//
	//   而"句柄开着"有个具体的坏后果:转码 worker 干完活要
	//   os.RemoveAll 这个目录,Windows 上文件还开着句柄 ⇒ RemoveAll 直接失败
	//   ("The process cannot access the file because it is being used by
	//   another process")⇒ 临时目录永远删不掉;
	//   Linux 上虽然能删,句柄却泄漏了(每次上传泄一个 fd)。
	//
	//   ⚠️ **必须在这里关,不能推到 defer 里** —— defer 在函数返回时才跑,
	//   而 worker 可能已经转完并开始删目录了(假 ffmpeg 是毫秒级的),
	//   那个窗口里 RemoveAll 会撞上开着的句柄。
	tmp.Close()

	// ⚠️ **以存储返回的为准,不采信申报值**。这是客户端唯一没法撒谎的地方。
	fileID, err := s.commitFile(ctx, uid, folder, &model.SysFile{
		FileKey:     key,
		FileName:    req.FileName,
		FileSize:    meta.Size,
		ContentType: ct,
		ETag:        meta.ETag,
	}, held)
	if err != nil {
		// ⚠️ **预扣在事务外,事务回滚退不掉它 —— 必须在这里显式还回去**。
		//
		// holdQuota 是本函数里一条独立的已提交 SQL(见上面那行),而
		// commitFile 把 settleQuota 关在它自己的事务里。事务一失败,settleQuota
		// 一起回滚,但**预扣的那笔已经落库了**,没人再动它。
		// 少这一句的表现:每次记账失败(DB 抖动、唯一索引冲突)都凭空吃掉用户一份
		// 配额 —— 和上面 Put 失败那条路径对比一眼就更刺眼:那条会退,这条不会。
		if held > 0 {
			if rErr := releaseQuota(s.db.WithContext(ctx), int64(uid), held); rErr != nil {
				return nil, rErr
			}
		}
		// 记账失败 → 对象已成孤儿。删掉它,免得孤儿对象在存储里堆到账单爆炸。
		// ⚠️ 删失败只记日志不返错:这时候返回错误也改变不了"对象已经存在"
		// 这个事实,反而把"用户看到失败但文件其实传上来了"的歧义留给前端。
		if delErr := s.prov.Delete(ctx, key); delErr != nil {
			return nil, errcode.New(errcode.Internal, "写入失败且回滚失败,需人工清理").WithDetail(delErr.Error())
		}
		return nil, err
	}
	// ⚠️ **M133:这里原来不重建 info.json** —— 设计文档 §2.3 D3 明写
	//   "生成时机:文件注册、删除、封面变更之后各调一次",而服务端中转这条路
	//   (Receive)是**最常见的注册路径**(直传 OSS 那条走 Register)。
	//   漏了它的后果:用户传完文件,文件夹里的 info.json 还是空的或者停留在
	//   上一次的状态 —— `GET /folders/:id` 返的 info_json 内容永远对不上
	//   实际文件列表。
	//
	//   放在 commitFile 之后:info.json 是按 DB 现状重建的快照,文件行还没
	//   落库就重建,写出来的是"上一个版本"。syncFolderInfo 自己吞错误记
	//   Warn(见 folder_info.go),不会让这次上传失败。
	s.syncFolderInfo(folder.ID)

	// ⚠️ **媒体探测放在最后** —— 文件已经落库、字节已经在存储里了,
	//   这时候探时长/抽封面,失败也只是"列表里少一个时长"。
	//
	//   期望大小用**存储返回的实际大小**,不是申报值 —— 副本是 tee 出来的,
	//   大小对不上(超上限被截断)就说明副本不完整,不能拿去探测。
	if tmp != nil {
		tmpOwned = s.probeAndApply(ctx, &model.SysFile{
			BaseModel: model.BaseModel{ID: fileID},
			UserID:    uid,
			Biz:       folder.Biz,
			FolderID:  folder.ID,
			FileKey:   key,
		}, tmp.path, meta.Size)
	}
	return s.viewByKeyWithURL(ctx, key)
}

// Register OSS 直传完成后登记。
func (s *fileService) Register(ctx context.Context, caller *Caller, req FileRegisterReq) (*FileView, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	if s.prov == nil {
		return nil, errcode.New(errcode.Internal, "存储未初始化")
	}
	if strings.TrimSpace(req.Key) == "" {
		return nil, errcode.New(errcode.ParamMissing, "key 必填")
	}
	if !oss.SafeKey(req.Key) {
		return nil, errcode.New(errcode.InvalidParam, "key 不合法")
	}
	// ⚠️ **归属从 key 自己解析,再跟库里比对** —— 不能只靠"key 前缀里那个
	// user id 等于当前用户"就放行:key 是客户端上次拿到的,它可能指向
	// 任何人目录下、任何业务段下的对象。
	ownerID, biz, folderID, ok := parseKeyOwner(req.Key, s.opt.Root)
	if !ok {
		return nil, errcode.New(errcode.InvalidParam, "key 不合法")
	}
	if ownerID != uid {
		return nil, errcode.New(errcode.NotFound, "文件不存在")
	}
	folder, err := s.folders.ensureFolder(ctx, caller, folderID, biz, "")
	if err != nil {
		return nil, err
	}
	if folder.Biz != biz {
		return nil, errcode.New(errcode.NotFound, "文件不存在")
	}

	// ⚠️ **大小从存储取,不从请求取**。客户端报的 file_size 是可以随便填的,
	// 采信它等于配额形同虚设:报 1 字节,传 10GB。
	meta, err := s.prov.Stat(ctx, req.Key)
	if err != nil {
		if oss.IsNotFound(err) {
			return nil, errcode.New(errcode.NotFound, "对象不存在,可能已过期或被清理")
		}
		return nil, errcode.New(errcode.Internal, "读取对象信息失败").WithDetail(err.Error())
	}
	if err := s.checkDeclaredSize(meta.Size); err != nil {
		return nil, err
	}

	name := req.FileName
	if strings.TrimSpace(name) == "" {
		name = keyBaseName(req.Key)
	}
	// ⚠️ 这条路**不走 buildFileKey**(key 是客户端上次拿到的,不是现造的),
	// 所以长度闸要在这里单独调一次 —— 少了它就是一个"文件名超长 → INSERT 失败
	// → 对象刚写进去又被删掉"的死循环。理由见 checkFileName 的注释。
	if err := checkFileName(name); err != nil {
		return nil, err
	}
	ct := probeContentType(name, req.ContentType)
	// 已有行 = 客户端重试(直传接口超时重试很常见),直接返回,**不要重复记账**。
	// 少了这一判,重试一次就多扣一份配额。
	if v, err := s.viewByKeyWithURL(ctx, req.Key); err == nil {
		return v, nil
	}
	// ⚠️ **`held` 传 0,不是 meta.Size** —— 这是本函数最容易写错的一行。
	//
	// `commitFile` 的 held 语义是"**之前已经预扣了多少**",`settleQuota` 算的是
	// `delta = held - actual` 再加减回去。直传这条路**从来没有预扣过**
	// (`UploadToken` 那一刻拿不到真实大小,见那里的注释),所以传 meta.Size 等于
	// 告诉它"你已经预扣了 18 字节",净变化 0 —— **每个直传文件都完全免费**,
	// 配额形同虚设,用户可以绕开上限无限传,而且列表页看不出任何异常。
	//
	// 传 0 走 commitFile 的 else 分支(`holdQuotaOrFail`),那才是
	// "没有预扣 → 整份扣下来,扣不动就拒"。
	fileID, err := s.commitFile(ctx, uid, folder, &model.SysFile{
		FileKey:     req.Key,
		FileName:    name,
		FileSize:    meta.Size,
		ContentType: ct,
		ETag:        meta.ETag,
		// ⚠️ **held 必须是 0,不是 meta.Size**(理由见下面的注释)。
	}, 0)
	if err != nil {
		// 扣不动配额时对象已经在存储里了(客户端自己直传上去的,拦不住)。
		// 删掉它:留着就是一份没人认领、也没人计费的孤儿对象,按量计费会一直
		// 烧钱。⚠️ 删失败只记日志不覆盖原错误 —— 用户看到的仍然是"空间不足"
		// 这个真正的失败原因,不该被"回滚也失败了"盖掉。
		if delErr := s.prov.Delete(ctx, req.Key); delErr != nil {
			logger.L().Warn("直传登记超额,对象回滚失败,需人工清理",
				zap.String("key", req.Key), zap.Error(delErr))
		}
		return nil, err
	}
	// ⚠️ **M133:这里原来不重建 info.json**(理由同 Receive 那处)。
	//   直传这条路尤其需要 —— 客户端自己怼到 OSS 之后才来登记,这是
	//   "文件真的落地了"的那一刻,清单不在这儿刷新就永远差一个文件。
	s.syncFolderInfo(folder.ID)

	// ⚠️ **媒体探测走的是"从存储里拉一份到本地"** —— 直传这条路字节**没有**
	//   经过服务端,没有 tee 的机会。要探测只能自己拉回来(见 probeStoredMedia)。
	s.probeStoredMedia(ctx, &model.SysFile{
		BaseModel:   model.BaseModel{ID: fileID},
		UserID:      uid,
		Biz:         folder.Biz,
		FolderID:    folder.ID,
		FileKey:     req.Key,
		FileName:    name,
		FileSize:    meta.Size,
		ContentType: ct,
	}, req.Key)
	return s.viewByKey(ctx, req.Key)
}

func (s *fileService) List(ctx context.Context, caller *Caller, q FileListQuery) (*model.PageResponse, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	page, size := normPage(q.Page, q.Size, 200)

	// ⚠️ **默认只列正常态,但显式传 status 时要"替换"而不是"叠加"**。
	// 叠加的话查询会变成 `status = 0 AND status = 1`,恒空 —— 而调用方看到的是
	// 200 + 空列表,不会知道自己传错了什么(早先的写法正是这样:注释说"要看
	// 历史要显式传 status",而传了必然返回空)。
	// 列出已删文件会让用户以为"删不掉",所以默认必须是正常态。
	status := model.FileStatusNormal
	if q.Status > 0 {
		status = model.FileStatus(q.Status)
	}
	tx := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Where("user_id = ?", uid).
		Where("status = ?", status)
	// ⚠️ **查非正常态必须 Unscoped**。sys_file 走的是 BaseModel.DeletedAt
	// (GORM 软删作用域),所有查询自动加 `deleted_at IS NULL` —— 而 Delete()
	// 恰好**同时**写了 status 和 deleted_at。于是即使把 status 条件写对,
	// "已删"那些行照样被作用域挡在查询外面,结果是恒空。
	// 两套删除标记并存这件事见 model/file.go 的类型注释;这里是它唯一的
	// 需要显式绕过的地方,而且必须是调用方**明确要求**才绕。
	if status != model.FileStatusNormal {
		tx = tx.Unscoped()
	}
	if q.FolderID > 0 {
		tx = tx.Where("folder_id = ?", q.FolderID)
	}
	if biz := strings.TrimSpace(q.Biz); biz != "" {
		tx = tx.Where("biz = ?", biz)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		tx = tx.Where("file_name LIKE ?", "%"+kw+"%")
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询文件数失败").WithDetail(err.Error())
	}
	var rows []model.SysFile
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询文件失败").WithDetail(err.Error())
	}
	list := make([]FileView, 0, len(rows))
	for _, r := range rows {
		v := toFileView(r)
		// ⚠️ **M134:列表也带直链** —— 缩略图视图要把它塞进 `<img src>`。
		//
		//   以前这里不填,理由是"一次签 N 个 URL 会把列表拖慢" —— **那个前提
		//   在 M80 单桶单树模型之后已经不成立**:直链是**拼出来的**(不签名、
		//   不回存储、不调 OSS),代价只是 N 次字符串拼接(local provider 多
		//   一次 stat)。文件夹列表的 `cover_url` 早就是这么干的,两边对齐。
		//
		//   拿不到就留空 —— 前端据此显示占位图标,不让一个文件的直链问题
		//   把整页列表带崩。
		v.URL = s.fileURL(ctx, r.FileKey)
		// ⚠️ **M141:视频封面直链** —— 和 URL 同一个道理(拼出来的稳定直链),
		//   空串 = 没有封面(音频 / 没装 ffmpeg / 探测失败),前端据此回落到格式图标。
		v.CoverURL = s.fileURL(ctx, r.CoverKey)
		// ⚠️ **M145:可播副本直链** —— 空串 = 没有副本(不需要转 / 还在转 / 转失败)。
		v.PlaybackURL = s.fileURL(ctx, r.PlaybackKey)
		list = append(list, v)
	}
	return &model.PageResponse{List: list, Total: total, Page: page, Size: size}, nil
}

// fileURL 拼一个文件的稳定直链;拿不到就返空串(不报错)。
//
// ⚠️ **"签不出来"不是列表的错** —— 对象可能刚被删、provider 可能没配
// 静态挂载点。这些都不该让整页列表 500,所以这里吞掉错误只留空串。
func (s *fileService) fileURL(ctx context.Context, key string) string {
	if key == "" || s.prov == nil {
		return ""
	}
	u, err := s.prov.SignDownloadURL(ctx, key, s.opt.DownloadURLTTL)
	if err != nil {
		return ""
	}
	return u
}

func (s *fileService) Detail(ctx context.Context, caller *Caller, id uint64) (*FileView, error) {
	f, err := s.loadFile(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	v := toFileView(*f)
	// 详情顺带给 URL:这个页面本来就要显示一个可点的链接,让前端再发一次请求
	// 纯属多一个往返。列表也带(M134,理由见 List 里那段注释)。
	v.URL = s.fileURL(ctx, f.FileKey)
	// ⚠️ **M141:详情也带封面直链** —— 详情页要显示大图,不能只列表有。
	v.CoverURL = s.fileURL(ctx, f.CoverKey)
	// ⚠️ **M145:详情也带可播副本直链** —— 详情页要显示"用哪个文件播"。
	v.PlaybackURL = s.fileURL(ctx, f.PlaybackKey)
	return &v, nil
}

func (s *fileService) URL(ctx context.Context, caller *Caller, id uint64, ttl time.Duration) (string, error) {
	if s.prov == nil {
		return "", errcode.New(errcode.Internal, "存储未初始化")
	}
	f, err := s.loadFile(ctx, caller, id)
	if err != nil {
		return "", err
	}
	if ttl <= 0 || ttl > s.opt.MaxDownloadTTL {
		ttl = s.opt.DownloadURLTTL
	}
	u, err := s.prov.SignDownloadURL(ctx, f.FileKey, ttl)
	if err != nil {
		// ⚠️ **"签不出来"是真故障** —— 存储侧只有一棵树,每个文件都有直链,
		// 签不出来说明对象不存在 / 磁盘读失败 / provider 没配静态挂载点。
		// 这些都要报出来,不能返空串糊过去。
		//
		//   以前这里对"私有文件"返空串,前端见到空 url 会改走
		//   `GET /files/:id/content` 中转;现在那条分支没了 —— 直链是唯一主路径,
		//   中转只是兜底。
		return "", errcode.New(errcode.Internal, "生成访问地址失败").WithDetail(err.Error())
	}
	return u, nil
}

func (s *fileService) Quota(ctx context.Context, caller *Caller) (*QuotaView, error) {
	return quotaInfo(ctx, s.db, caller)
}

// Content 打开字节流(直链兜底路径)。
//
// ⚠️ **归属校验和 URL 那条路完全一样** —— 跨用户一律 404,不是 403。理由见
// loadFile。
func (s *fileService) Content(ctx context.Context, caller *Caller, id uint64) (io.ReadSeekCloser, *FileView, error) {
	f, err := s.loadFile(ctx, caller, id)
	if err != nil {
		return nil, nil, err
	}
	cp, ok := s.prov.(oss.ContentProvider)
	if !ok {
		// ⚠️ 501 而不是 500:"这个部署形态不做中转"是可预期的配置,
		// 报 500 会让监控把它当成故障告警。
		return nil, nil, errcode.New(errcode.NotImplemented,
			"当前存储不支持服务端中转读取,私有文件请用直链访问")
	}
	rc, meta, err := cp.Open(ctx, f.FileKey)
	if err != nil {
		if oss.IsNotFound(err) {
			return nil, nil, errcode.New(errcode.NotFound, "文件已不存在")
		}
		return nil, nil, errcode.New(errcode.Internal, "读取文件失败").WithDetail(err.Error())
	}
	v := toFileView(*f)
	if v.ContentType == "" && meta != nil {
		v.ContentType = meta.MimeType
	}
	return rc, &v, nil
}

// Delete 删文件。
//
// ⚠️ **四件事**:物理对象 / sys_file 行 / 用户配额 / 文件夹冗余计数。
// 顺序是"先查配额 → 再删物理 → 最后事务里改 DB"。
func (s *fileService) Delete(ctx context.Context, caller *Caller, id uint64) error {
	f, err := s.loadFileForWrite(ctx, caller, id)
	if err != nil {
		return err
	}

	if s.prov != nil {
		if err := s.prov.Delete(ctx, f.FileKey); err != nil && !oss.IsNotFound(err) {
			return errcode.New(errcode.Internal, "删除物理文件失败").WithDetail(err.Error())
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.SysFile{}).
			Where("id = ? AND status = ?", f.ID, model.FileStatusNormal).
			Updates(map[string]any{"status": model.FileStatusDeleted, "deleted_at": nowFn()})
		if res.Error != nil {
			return errcode.New(errcode.Internal, "删除文件记录失败").WithDetail(res.Error.Error())
		}
		if res.RowsAffected == 0 {
			// 并发下别人先删了。**不是错误** —— 幂等,返回成功。
			return nil
		}
		// 文件是文件夹封面时,顺手把文件夹上的封面引用摘掉。
		// ⚠️ 漏这一步的话,文件夹会一直指着一个已经被删掉的文件,
		// 页面上封面位置显示空白,而且没有任何报错。
		if err := tx.Model(&model.SysFolder{}).
			Where("cover_file_id = ?", f.ID).
			Updates(map[string]any{"cover_file_id": 0, "cover_key": ""}).Error; err != nil {
			return errcode.New(errcode.Internal, "清除文件夹封面引用失败").WithDetail(err.Error())
		}
		if err := syncFolderStats(tx, f.FolderID); err != nil {
			return err
		}
		return releaseQuota(tx, int64(f.UserID), f.FileSize)
	})
	if err != nil {
		return err
	}
	// ⚠️ 快照重建失败**不返错**:删已经成功了,返错会让用户看到"删除失败"
	// 而文件确实没了,重试也只会更让人困惑。理由见 folder_info.go 的 syncFolderInfo。
	s.syncFolderInfo(f.FolderID)

	// ⚠️ **M141:删掉视频封面对象** —— 封面**不是**用户文件(它不在 sys_file 里,
	//   不计文件数、不占配额),所以删主文件不会级联到它。漏了这一步的话,
	//   封面会变成孤儿对象,永远占着存储空间,而且没有任何东西会告诉你。
	//
	//   放在事务之后:事务失败时文件还在,封面也还在,两边状态一致。
	s.deleteCoverObject(ctx, f.CoverKey)
	// ⚠️ **M145:同样要删掉播放副本** —— 它和封面一样不是 sys_file 里的行,
	//   删主文件不会级联到它。漏了这一步 = 存储里留一份没人认领的转码产物,
	//   而且**它可能比原文件还大**(低码率的 DivX 转完 H.264 反而变大)。
	s.deletePlaybackObject(ctx, f.PlaybackKey)
	return nil
}

// ===== 内部 =====

// buildFileKey 构造文件 key —— 转发到全仓唯一的那份实现(file_common.go),
// UploadService 的分片 Init 也走它。
func (s *fileService) buildFileKey(userID uint64, biz string, folderID uint64, fileName string) (string, error) {
	return buildFileKey(s.opt, userID, biz, folderID, fileName)
}

// commitFile 落库 + 结算配额 + 刷冗余计数。
//
// ⚠️ **配额结算在同一个事务里**,这样"文件行了但配额没扣"不会发生。
// 反过来(先扣配额再插行)则会在插入失败时留下凭空多扣的配额。
//
// ⚠️ **返回新建行的 id** —— M141 起媒体探测要拿它写回 duration_ms / cover_key。
// 事务回滚时返回 0,调用方看到 0 就知道"这行没建成",不去写回。
func (s *fileService) commitFile(ctx context.Context, uid uint64, folder *FolderView, f *model.SysFile, held int64) (uint64, error) {
	var fileID uint64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if held > 0 {
			if err := settleQuota(tx, int64(uid), held, f.FileSize); err != nil {
				return err
			}
		} else if err := holdQuotaOrFail(tx, int64(uid), f.FileSize); err != nil {
			return err
		}
		row := *f
		// ⚠️ **必须显式发号**。model.BaseModel.ID 是 `autoIncrement:false`
		// (全仓用 sonyflake 在 Go 侧发号),不写就是往数据库插 id=0。而
		// sys_file 的唯一索引建在 file_key 上、**不在 id 上**,所以多行 id=0
		// 照样能插进去 —— 直到 loadFile(0) 随便捞出别人的那一条,
		// 用户删 A 文件结果删掉了 B 的对象,而唯一索引一个错都不报。
		row.ID = sonyflake.Next()
		row.UserID = uid
		row.Biz = folder.Biz
		row.FolderID = folder.ID
		row.Storage = s.storageOf()
		row.Status = model.FileStatusNormal
		if err := tx.Create(&row).Error; err != nil {
			return errcode.New(errcode.Internal, "登记文件失败").WithDetail(err.Error())
		}
		fileID = row.ID
		return syncFolderStats(tx, folder.ID)
	})
	if err != nil {
		return 0, err
	}
	return fileID, nil
}

// holdQuotaOrFail 预扣或返回"空间不足"
func holdQuotaOrFail(tx *gorm.DB, userID, size int64) error {
	ok, err := holdQuota(tx, userID, size)
	if err != nil {
		return err
	}
	if !ok {
		return errcode.New(errcode.InvalidParam, "存储空间不足")
	}
	return nil
}

// storageOf 当前 provider 对应哪个枚举值
//
// ⚠️ **比的是 oss.ProviderNameOSS,不是手写的 "aliyun"** ——
// aliyun provider 的 Name() 返回的是 "oss"(见 pkg/oss/aliyun),拿 "aliyun"
// 去比**永远不相等** ⇒ 这条分支恒不成立 ⇒ 配了 OSS 的部署里,
// 每一行文件都被记成"本地磁盘"。
// 逐行记 storage 的意义正是迁移期两种 provider 共存(先复制再切),
// 记错了就等于这个字段白留,还会在页面上把本地文件显示成 OSS。
func (s *fileService) storageOf() model.FileStorage {
	if s.prov != nil && s.prov.Name() == oss.ProviderNameOSS {
		return model.FileStorageOSS
	}
	return model.FileStorageLocal
}

// checkDeclaredSize 单文件上限
func (s *fileService) checkDeclaredSize(size int64) error {
	if size <= 0 {
		return nil
	}
	if s.opt.MaxFileSize > 0 && size > s.opt.MaxFileSize {
		return errcode.New(errcode.PayloadTooLarge, "文件超过单文件上限")
	}
	return nil
}

// loadFile 取文件并校验归属(读;super_admin 可跨用户)
func (s *fileService) loadFile(ctx context.Context, caller *Caller, id uint64) (*model.SysFile, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	var f model.SysFile
	if id == 0 {
		return nil, errcode.New(errcode.NotFound, "文件不存在")
	}
	if err := s.db.WithContext(ctx).Where("id = ? AND status = ?", id, model.FileStatusNormal).
		First(&f).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "文件不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询文件失败").WithDetail(err.Error())
	}
	if f.UserID != uid && !callerCanCrossUserRead(caller) {
		return nil, errcode.New(errcode.NotFound, "文件不存在")
	}
	return &f, nil
}

// loadFileForWrite 同上,但**不允许跨用户**。
//
// ⚠️ 写路径必须和读路径分开:读允许 super_admin 跨用户(§10.2),写不允许。
// 合成一个函数靠调用方传 flag 的话,总有一天有人传错。
func (s *fileService) loadFileForWrite(ctx context.Context, caller *Caller, id uint64) (*model.SysFile, error) {
	f, err := s.loadFile(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	if err := callerOwns(caller, f.UserID); err != nil {
		return nil, err
	}
	return f, nil
}

// viewByKey 按 key 找刚登记的文件
//
// ⚠️ **不再校验归属** —— 调用方(Receive / Register)都已经校验过了,这里
// 再查一遍不会更安全,只会多一次查询。而它返回的文件一定刚由本进程写入。
func (s *fileService) viewByKey(ctx context.Context, key string) (*FileView, error) {
	var f model.SysFile
	if err := s.db.Where("file_key = ?", key).First(&f).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.Internal, "文件登记后读不回来")
		}
		return nil, errcode.New(errcode.Internal, "查询文件失败").WithDetail(err.Error())
	}
	v := toFileView(f)
	// ⚠️ **顺手填封面直链**(M141)—— 封面 key 就在同一行里,不用多查一次库。
	v.CoverURL = s.fileURL(ctx, f.CoverKey)
	// ⚠️ **M145:可播副本直链** —— 同样在那一行里,顺手填。
	v.PlaybackURL = s.fileURL(ctx, f.PlaybackKey)
	return &v, nil
}

// viewByKeyWithURL = viewByKey + 顺带签一个下载 URL。
//
// ⚠️ **只有"刚上传完"的三个出口走它**(Receive / Register / 分片 Complete)。
// 上传完的调用方下一个动作就是把图显示出来,让它再发一次 `/files/:id/url`
// 纯属白等一个往返(上传本来就是最慢的一步,后面再慢一拍体验更差)。
//
// ⚠️ 列表也带直链(M134),但走的是另一条路(List 里直接拼)—— 这里的
// "签不出来不返错"口径与列表一致:直链是纯展示用的,拿不到就留空。
func (s *fileService) viewByKeyWithURL(ctx context.Context, key string) (*FileView, error) {
	v, err := s.viewByKey(ctx, key)
	if err != nil || s.prov == nil {
		return v, err
	}
	// ⚠️ **签不出来不返错**。文件这会儿已经在库里、字节已经在存储里了,
	// 为一个纯展示用的 URL 让整个上传失败是本末倒置 —— 前端拿着 FileView
	// 里的 id 随时能再取一次,那时大概率已经好了(比如 OSS 临时抖动)。
	v.URL = s.fileURL(ctx, key)
	return v, nil
}

// ResolveKeys 批量把文件 key 解析成「原文件名 + 直链」
//
// ⚠️ **一次 IN 查询,不按 key 逐个查** ——
// 一页记录里可能有几十个附件,逐个查就是 N+1。
//
// ⚠️ **查不到就不在 map 里,不报错** ——
// 文件被删了 / key 写错了,都是"这一项没有解析出来",
// 不是"整个请求失败"(旁路纪律:展示用的东西取不到就降级)。
func (s *fileService) ResolveKeys(ctx context.Context, keys []string) (map[string]FileRef, error) {
	out := make(map[string]FileRef, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	// 去重(同一页里可能有多个字段指向同一个文件)
	uniq := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		uniq = append(uniq, k)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	var rows []model.SysFile
	if err := s.db.WithContext(ctx).Where("file_key IN ?", uniq).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询文件失败").WithDetail(err.Error())
	}
	for _, f := range rows {
		// ⚠️ **直链签不出来就留空,不报错** —— 与 fileURL 同一口径:
		// 附件是展示用的,拿不到直链不该让整页记录失败。
		out[f.FileKey] = FileRef{Name: f.FileName, URL: s.fileURL(ctx, f.FileKey)}
	}
	return out, nil
}

// probeContentType 按扩展名探测 MIME。
//
// ⚠️ **不采信客户端上报的 ContentType**:它是客户端说了算的,而服务端会把
// 它写进 Content-Type 响应头发给别人 —— 客户端报 `text/html` 就能让你的
// 域下存一个可执行 HTML,别人访问时浏览器直接当页面跑(XSS)。
func probeContentType(fileName, reported string) string {
	if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName))); ct != "" {
		return ct
	}
	if reported != "" && !strings.ContainsAny(reported, "<>") && len(reported) <= 128 {
		return reported
	}
	return "application/octet-stream"
}

// keyBaseName 取 key 的末段(客户端没给 file_name 时的兜底)
func keyBaseName(key string) string {
	if i := strings.LastIndexByte(key, '/'); i >= 0 && i+1 < len(key) {
		return key[i+1:]
	}
	return "file"
}
