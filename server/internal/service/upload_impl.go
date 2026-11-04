// UploadService 真实实现 —— 分片上传会话(设计文档 §8.2 / §9)
// 替换 M76 之前那个"只返占位 OSS 凭证"的 upload_impl.go。
//
// ⚠️ **预扣配额在这一层做,而不是在 FileService.Receive 里做**。直传那一刻拿不到
// 真实大小所以那边不预扣(见 file_impl.go UploadToken 的注释);分片不一样 ——
// 客户端在 init 时就申报了总大小,而且**必须申报**(合并阶段要拿它当完整性闸)。
// 既然拿得到真值就在这里扣,免得用户同时开 10 个会话各传一个头就把配额占满。
//
// ⚠️ **为什么持有的是 FileService / FolderService 的具体类型而不是接口**:
// Complete 要复用 fileService.commitFile(那里面是"结算配额 + 建行 + 刷冗余
// 计数在同一个事务里"这一整套账务,复写成第二份必然和 Receive 那份分叉),
// 而这些方法都不是对外接口的一部分。让接口保持最小,是靠**同包的类型**拿到
// 内部方法,而不是把它们加到公开接口上去。
package service

import (
	"context"
	"errors"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/oss"
	"rapid-admin/internal/pkg/sonyflake"
)

// uploadService 真实实现
type uploadService struct {
	db   *gorm.DB
	prov oss.Provider
	opt  FileOptions
	// mp 分片能力。
	//
	// ⚠️ **在构造时断言一次存成字段,不在每个方法里 type-assert**。每请求断言
	// 一次不贵,但"Init 时判定支持、Complete 时判定不支持"这种事一旦发生,
	// 写出来的就是一个永远完成不掉、还占着配额的僵尸会话。
	mp      oss.MultipartProvider
	folders *folderService
	files   *fileService
}

// NewUploadService 构造
func NewUploadService(db *gorm.DB, prov oss.Provider, opt FileOptions) UploadService {
	o := opt.normalize()
	mp, _ := prov.(oss.MultipartProvider)
	return &uploadService{
		db:      db,
		prov:    prov,
		opt:     o,
		mp:      mp,
		folders: NewFolderService(db, prov, o).(*folderService),
		files:   NewFileService(db, prov, o).(*fileService),
	}
}

func (s *uploadService) SupportsMultipart() bool { return s.prov != nil && s.mp != nil }

// ===== Init =====

func (s *uploadService) Init(ctx context.Context, caller *Caller, req MultipartInitReq) (*MultipartInitResp, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	if s.mp == nil {
		return nil, errcode.New(errcode.NotImplemented, "当前存储不支持分片上传")
	}
	// ⚠️ **file_size 在这里是必填的,不是可选的**。它同时是三件事的输入:
	// 预扣多少配额、切几片、以及 Complete 阶段的完整性校验(本地磁盘唯一的闸)。
	// 缺了它,合并出来的文件多大都收,而配额只能事后按 actual 补扣 —— 那时
	// 内容已经落盘了,拦不住。
	if req.FileSize <= 0 {
		return nil, errcode.New(errcode.ParamMissing, "file_size 必填")
	}
	if err := s.files.checkDeclaredSize(req.FileSize); err != nil {
		return nil, err
	}

	partSize, partNum, err := s.planParts(req.FileSize, req.PartSize)
	if err != nil {
		return nil, err
	}

	folder, err := s.folders.ensureFolder(ctx, caller, req.FolderID, req.Biz, "")
	if err != nil {
		return nil, err
	}
	key, err := buildFileKey(s.opt, uid, folder.Biz, folder.ID, req.FileName)
	if err != nil {
		return nil, err
	}

	// 预扣 —— **必须在 InitMultipartUpload 之前**。反过来的话,配额已经超了的
	// 用户仍然能在存储上留下一堆没人认领的半成品,清理任务还得逐个去 Abort。
	ok, err := holdQuota(s.db.WithContext(ctx), int64(uid), req.FileSize)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errcode.New(errcode.InvalidParam, "存储空间不足")
	}

	ct := probeContentType(req.FileName, req.ContentType)

	// ⚠️ **媒体探测要留一份本地副本**(M141)—— ffmpeg 只能读本地文件。
	//
	//   这里**只建一个空文件 + 记下路径**,真正的字节在 UploadPart 里按偏移
	//   写进去(偏移和存储侧合并分片的偏移一致,所以副本和最终对象同源)。
	//
	//   句柄现在就关掉 —— 跨请求不能指望进程内的句柄还活着,每个请求
	//   自己按偏移打开写。
	mediaPath := ""
	if s.files.mediaProbeEligible(req.FileName, ct, req.FileSize) {
		tmp, tErr := newMediaTemp(s.opt.MediaTmpDir, req.FileName, s.opt.MediaMaxSize)
		if tErr != nil {
			// 建副本失败 → 不探测。**不影响上传**(理由见 media_probe.go 文件头)。
			logger.L().Warn("分片上传: 建媒体临时副本失败(不影响上传)",
				zap.String("file_name", req.FileName), zap.Error(tErr))
		} else {
			mediaPath = tmp.path
			tmp.Close()
		}
	}

	// ⚠️ TotalSize 一定要传:它是 local provider 在合并后校验"总尺寸对不对"的
	// 唯一依据(见 oss.InitOpts.TotalSize 注释)。不传的话客户端少传一片也能
	// 合并成功,得到一个静默截断的文件。
	mi, err := s.mp.InitMultipartUpload(ctx, key,
		oss.WithPartSize(partSize), oss.WithTotalSize(req.FileSize), oss.WithContentType(ct))
	if err != nil {
		if rErr := releaseQuota(s.db.WithContext(ctx), int64(uid), req.FileSize); rErr != nil {
			return nil, rErr
		}
		// ⚠️ 副本也要清 —— 会话没建成,没人会再来收尾它(临时盘会攒垃圾)。
		cleanupMediaTemp(mediaPath)
		return nil, errcode.New(errcode.Internal, "初始化分片上传失败").WithDetail(err.Error())
	}

	row := model.SysUploadSession{
		// ⚠️ **必须显式发号**。model.BaseModel.ID 是 `autoIncrement:false`
		// (全仓用 sonyflake 在 Go 侧发号),不写就是插 id=0。而后面的
		// loadSessionForWrite 对 id==0 直接返 NotFound —— 分片上传会在
		// "Init 成功、下一片立刻 404"的地方断掉。
		BaseModel:   model.BaseModel{ID: sonyflake.Next()},
		UploadID:    mi.UploadID,
		FileKey:     key,
		UserID:      uid,
		FolderID:    folder.ID,
		FileName:    req.FileName,
		FileSize:    req.FileSize,
		ContentType: ct,
		PartSize:    partSize,
		Status:      model.UploadSessionActive,
		QuotaHeld:   req.FileSize,
		ExpiresAt:   nowFn().Add(s.opt.SessionTTL),
		// ⚠️ **媒体探测用的本地副本路径**(M141)—— 分片是分批传到存储的,
		//   字节**没有**经过服务端的一次性流,所以没有 tee 的机会。
		//   不留一份本地副本,就探不出时长、抽不出封面。
		MediaTmpPath: mediaPath,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 建会话失败 → 存储侧那个 upload 已经没人认领了。必须 Abort,否则它会
		// 一直挂在账单上,等存储自己清理(有的配置下是永不过期)。
		//
		// ⚠️ **Abort 成功才退配额**。Abort 失败意味着半成品还在,而我们正在
		// 把它"忘了" —— 那就两头不靠:用户丢了配额,存储留下了垃圾。此时退
		// 配额等于白送容量,不退至少账是对的。
		if aErr := s.mp.AbortMultipartUpload(ctx, key, mi.UploadID); aErr != nil {
			logger.L().Error("分片上传已创建但建会话失败且 abort 失败,需人工清理",
				zap.String("key", key), zap.String("upload_id", mi.UploadID), zap.Error(aErr))
			return nil, errcode.New(errcode.Internal, "创建上传会话失败,且已上传分片需人工清理").
				WithDetail(aErr.Error())
		}
		if rErr := releaseQuota(s.db.WithContext(ctx), int64(uid), req.FileSize); rErr != nil {
			return nil, rErr
		}
		// ⚠️ 副本也要清 —— 会话没建成,没人会再来收尾它。
		cleanupMediaTemp(mediaPath)
		return nil, errcode.New(errcode.Internal, "创建上传会话失败").WithDetail(err.Error())
	}

	return &MultipartInitResp{
		SessionID: row.ID,
		Key:       key,
		PartSize:  partSize,
		PartNum:   partNum,
		ExpiresAt: row.ExpiresAt.Unix(),
	}, nil
}

// ===== UploadPart =====

func (s *uploadService) UploadPart(ctx context.Context, caller *Caller, req UploadPartReq) (*PartInfo, error) {
	if s.mp == nil {
		return nil, errcode.New(errcode.NotImplemented, "当前存储不支持分片上传")
	}
	sess, err := s.loadSessionForWrite(ctx, caller, req.SessionID)
	if err != nil {
		return nil, err
	}
	if err := oss.CheckPartNumber(req.PartNumber); err != nil {
		return nil, errcode.New(errcode.InvalidParam, err.Error())
	}
	// ⚠️ **长度必须限死**。partSize 是"一个分片应该多大"的约定,客户端可以声明
	// 8MB 却 POST 500MB 上来 —— 不拦的话这就是一个无鉴权的放大器,本地 provider
	// 合并时也会被撑爆。hertz 已经把 body 读进内存了,所以这道闸只能加在
	// service 层(见设计文档 §9.3)。
	if len(req.Body) == 0 {
		return nil, errcode.New(errcode.ParamMissing, "分片内容为空")
	}
	if sess.PartSize > 0 && int64(len(req.Body)) > sess.PartSize {
		return nil, errcode.New(errcode.PayloadTooLarge, "分片超过约定大小")
	}

	etag, err := s.mp.UploadPart(ctx, sess.FileKey, sess.UploadID, req.PartNumber, req.Body)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "上传分片失败").WithDetail(err.Error())
	}

	// ⚠️ **顺带写一份本地副本**(M141,媒体探测用)—— 偏移是
	//   (partNumber-1) * partSize,和存储侧合并分片的偏移一致,
	//   所以副本和最终对象是同一份字节。
	//
	//   写失败只记日志:探测是锦上添花,不能因为它把上传搞失败。
	//   副本最终的大小会和申报的 file_size 比对(见 mediaTempUsable),
	//   对不上就整个跳过探测 —— 宁可没有时长,也不要一个编出来的时长。
	if sess.MediaTmpPath != "" {
		offset := int64(req.PartNumber-1) * sess.PartSize
		if err := writeMediaPart(sess.MediaTmpPath, req.Body, offset); err != nil {
			logger.L().Warn("分片上传: 写媒体临时副本失败(不影响上传)",
				zap.Uint64("session_id", sess.ID),
				zap.Int("part_number", req.PartNumber),
				zap.Error(err))
		}
	}
	return &PartInfo{PartNumber: req.PartNumber, ETag: etag, Size: int64(len(req.Body))}, nil
}

// ===== Parts(断点续传)=====

func (s *uploadService) Parts(ctx context.Context, caller *Caller, sessionID uint64) ([]PartInfo, error) {
	if s.mp == nil {
		return nil, errcode.New(errcode.NotImplemented, "当前存储不支持分片上传")
	}
	sess, err := s.loadSessionForWrite(ctx, caller, sessionID)
	if err != nil {
		return nil, err
	}
	parts, err := s.mp.ListParts(ctx, sess.FileKey, sess.UploadID)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询已传分片失败").WithDetail(err.Error())
	}
	out := make([]PartInfo, 0, len(parts))
	for _, p := range parts {
		out = append(out, PartInfo{PartNumber: p.PartNumber, ETag: p.ETag, Size: p.Size})
	}
	return out, nil
}

// ===== Complete =====

func (s *uploadService) Complete(ctx context.Context, caller *Caller, req CompleteReq) (*FileView, error) {
	if s.mp == nil {
		return nil, errcode.New(errcode.NotImplemented, "当前存储不支持分片上传")
	}
	sess, err := s.loadSessionForWrite(ctx, caller, req.SessionID)
	if err != nil {
		return nil, err
	}

	// ⚠️ **分片清单一律从存储取,不采信客户端上报的**(设计文档 §8.2 更正块)。
	// 客户端漏报一片 → OSS 报一个含混的 InvalidPart,local 直接合并出一个少
	// 一片的文件;客户端多报一片 → 合并结果不可预期。用存储自己的账本,这一
	// 整类问题就不存在了。
	parts, err := s.mp.ListParts(ctx, sess.FileKey, sess.UploadID)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询已传分片失败").WithDetail(err.Error())
	}
	if err := checkPartsCover(parts, sess.FileSize); err != nil {
		// ⚠️ **这里把会话终结掉,而不是留着等它过期**。分片不齐说明这次上传已经
		// 废了,留着等于把预扣的配额白占一个 SessionTTL(默认 24h)。
		s.abandonSession(ctx, sess)
		return nil, err
	}

	if err := s.mp.CompleteMultipartUpload(ctx, sess.FileKey, sess.UploadID, parts); err != nil {
		return nil, errcode.New(errcode.Internal, "合并分片失败").WithDetail(err.Error())
	}

	meta, err := s.prov.Stat(ctx, sess.FileKey)
	if err != nil {
		// ⚠️ 合并已经成功了但拿不到元信息。此时**不能退配额**(用户确实占了这些
		// 字节,对象就在存储里),**也不能把会话标成失败**(会让人来重传一份,
		// 而旧的那份还在)。返回错误让人重试 Stat / Complete —— Complete 再调
		// 一次会报 upload 不存在,那时靠清理任务收尾。
		return nil, errcode.New(errcode.Internal, "读取合并结果失败,请重试").WithDetail(err.Error())
	}
	if sess.FileSize > 0 && meta.Size != sess.FileSize {
		// 对不上 = 内容坏了,而用户并不知道。删掉它比留着强:留着会让他在几周后
		// 打开一个截断的文件,那时已经查不出是谁的问题了。
		if dErr := s.prov.Delete(ctx, sess.FileKey); dErr == nil {
			s.abandonSession(ctx, sess)
			return nil, errcode.New(errcode.Internal, "合并结果大小不符,已丢弃").
				WithDetail(sizeDetail(meta.Size, sess.FileSize))
		}
		return nil, errcode.New(errcode.Internal, "合并结果大小不符").WithDetail(sizeDetail(meta.Size, sess.FileSize))
	}

	_, biz, _, ok := parseKeyOwner(sess.FileKey, s.opt.Root)
	if !ok {
		// key 是本进程 buildFileKey 造出来的,解析不了说明规则被改过或数据被篡改
		return nil, errcode.New(errcode.Internal, "上传会话的 key 不合法")
	}
	folder, err := s.folders.ensureFolder(ctx, caller, sess.FolderID, biz, sess.FileName)
	if err != nil {
		return nil, err
	}

	fileID, err := s.files.commitFile(ctx, sess.UserID, folder, &model.SysFile{
		FileKey:     sess.FileKey,
		FileName:    sess.FileName,
		FileSize:    meta.Size,
		ContentType: sess.ContentType,
		ETag:        meta.ETag,
	}, sess.QuotaHeld)
	if err != nil {
		// ⚠️ **合并已经成功,字节就在存储里了,而账没记上** —— 和 Receive 里
		// commitFile 失败是同一种形状(对象已成孤儿),那边删对象 + 退预扣,
		// 这边不能只返错。
		//
		// 不删的话:sys_file 里没有这一行,清理任务按行驱动看不见它;local 会让它
		// 一直躺在磁盘上,OSS 会一直按量收这个对象的账单。而用户看到的是
		// "上传失败了",重传会拿到一个**新的 key**(对象名带秒级时间戳 + 随机段),
		// 于是每一次失败重试都在存储里多留一份。
		//
		// 退配额走 abandonSession → closeSession(它有 status=active 的守卫,
		// 不会退两次)。⚠️ 注意**顺序**:先删对象再退配额。反过来的话中途挂掉就变成
		// "配额退完了、字节还在" —— 那比现在更糟,因为它看起来是正常的。
		//
		// ⚠️ **和下面那段"状态更新失败不回滚"是两回事**:那里的账已经记上了,
		// 回滚只会让文件凭空消失;这里的账压根没记上,删掉对象才是把状态还原回去。
		if dErr := s.prov.Delete(ctx, sess.FileKey); dErr != nil {
			// 删不掉不能静默:用户重传会在存储里多留一份,而这一份没人认领。
			// 仍然退配额(占用它的是一份没人能访问的垃圾),但留下日志线索。
			logger.L().Error("分片合并成功但记账失败,对象回滚失败,需人工清理",
				zap.String("key", sess.FileKey), zap.String("upload_id", sess.UploadID), zap.Error(dErr))
		}
		s.abandonSession(ctx, sess)
		return nil, err
	}

	// ⚠️ **M141:媒体探测**(时长 + 封面)—— 用分片期间攒出来的那份本地副本。
	//
	//   放在 commitFile 之后:文件已经落库、字节已经在存储里了,探测失败只是
	//   "列表里少一个时长",绝不能反过来把这次上传搞失败。
	//
	//   期望大小用 sess.FileSize —— 副本和它对不上(某片没写进去)就跳过探测,
	//   见 mediaTempUsable。
	//
	//   ⚠️ **返回值 = 副本的清理权归谁了**(M145)—— true 表示已经派了转码任务,
	//   worker 会负责删这份副本,这里不能再删(否则 worker 转码时文件已经没了)。
	transcodeOwnsTmp := false
	if sess.MediaTmpPath != "" {
		transcodeOwnsTmp = s.files.probeAndApply(ctx, &model.SysFile{
			BaseModel: model.BaseModel{ID: fileID},
			UserID:    sess.UserID,
			Biz:       folder.Biz,
			FolderID:  folder.ID,
			FileKey:   sess.FileKey,
		}, sess.MediaTmpPath, sess.FileSize)
	}

	// 结算完成:预扣已经由 commitFile 里的 settleQuota 结清,这里只改状态。
	//
	// ⚠️ **这一步失败不回滚上面的记账**。文件已经在 sys_file 里了、字节已经
	// 在存储里了,把行删掉只会让用户看到"文件凭空消失"。会话留在 active 是
	// 更可接受的残留 —— 清理任务看到它会发现 complete 已经过了,不会去 Abort
	// 一个已经合并好的对象。
	if err := s.db.WithContext(ctx).Model(&model.SysUploadSession{}).
		Where("id = ?", sess.ID).
		Updates(map[string]any{
			"status":     model.UploadSessionDone,
			"quota_held": 0, // 已结清。再退一次就是双重退款
			"upload_id":  "",
			// ⚠️ 副本路径一并清掉 —— 会话已经终结,不该再有人去读它。
			"media_tmp_path": "",
		}).Error; err != nil {
		logger.L().Warn("分片已完成但更新会话状态失败(不影响文件本身)",
			zap.Uint64("session_id", sess.ID), zap.Error(err))
	}
	// ⚠️ **副本在这里删** —— 探测已经做完(上面),留着就是纯占磁盘。
	//   删失败只记日志:会话已经 done,没人会再来收尾它(下次清理任务扫到
	//   它时 path 已经清空,这个副本就留在临时盘上了 —— 临时盘本来就是
	//   系统会清的地方,不值得为它把上传流程搞复杂)。
	//
	//   ⚠️ **但转码 worker 接手了的副本不能删**(M145)—— worker 还要读它。
	//   删了就只剩一句 ffmpeg 的 "exit status 1",看不出真因。
	if !transcodeOwnsTmp {
		cleanupMediaTemp(sess.MediaTmpPath)
	}
	s.syncFolderInfo(folder.ID)
	// 顺带签 URL:调用方拿到 FileView 就能直接渲染,不用再打一次 /files/:id/url
	return s.files.viewByKeyWithURL(ctx, sess.FileKey)
}

// ===== Cancel =====

func (s *uploadService) Cancel(ctx context.Context, caller *Caller, sessionID uint64) error {
	if s.mp == nil {
		return errcode.New(errcode.NotImplemented, "当前存储不支持分片上传")
	}
	sess, err := s.loadSessionForWrite(ctx, caller, sessionID)
	if err != nil {
		return err
	}
	// ⚠️ **已完成的不给取消**。OSS 上那个 upload 已经不存在了(Abort 报
	// NoSuchUpload),而 local provider 会把合并后的成品文件当成临时文件删掉 ——
	// 用户文件凭空消失,而且没有任何报错。
	if sess.Status == model.UploadSessionDone {
		return errcode.New(errcode.Conflict, "上传已完成,不能取消")
	}
	if sess.UploadID != "" {
		if err := s.mp.AbortMultipartUpload(ctx, sess.FileKey, sess.UploadID); err != nil && !oss.IsNotFound(err) {
			return errcode.New(errcode.Internal, "取消分片上传失败").WithDetail(err.Error())
		}
	}
	s.closeSession(ctx, sess, model.UploadSessionCanceled)
	return nil
}

// ===== 内部 =====

// loadSessionForWrite 取会话并校验归属 / 状态 / 有效期。
//
// ⚠️ 上传会话**没有跨用户读的口子**。就算 super_admin 也不该替别人续传一个
// 会话 —— 那等于替别人写文件,而 §10.2 只给了读权限。
func (s *uploadService) loadSessionForWrite(ctx context.Context, caller *Caller, id uint64) (*model.SysUploadSession, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errcode.New(errcode.NotFound, "上传会话不存在")
	}
	var sess model.SysUploadSession
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&sess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "上传会话不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询上传会话失败").WithDetail(err.Error())
	}
	if sess.UserID != uid {
		return nil, errcode.New(errcode.NotFound, "上传会话不存在")
	}
	if sess.Status == model.UploadSessionDone {
		return nil, errcode.New(errcode.Conflict, "上传已完成")
	}
	if sess.Status != model.UploadSessionActive {
		// 已取消 / 已过期
		return nil, errcode.New(errcode.Conflict, sessionClosedMsg(sess.Status))
	}
	if sess.IsExpired(nowFn()) {
		// ⚠️ **过期就地收尾,不只返个错等清理任务慢慢扫**。过期会话的配额是
		// 实打实占着用户空间的,而清理任务定时跑(间隔几十分钟起)。用户在这
		// 期间看到的是"我明明没存东西却提示空间不足",而且查不出为什么。
		s.expireSession(ctx, &sess)
		return nil, errcode.New(errcode.Conflict, "上传会话已过期,请重新发起")
	}
	return &sess, nil
}

// expireSession 过期收尾:Abort 掉存储侧的上传 + 退预扣
func (s *uploadService) expireSession(ctx context.Context, sess *model.SysUploadSession) {
	if s.mp != nil && sess.UploadID != "" {
		if err := s.mp.AbortMultipartUpload(ctx, sess.FileKey, sess.UploadID); err != nil && !oss.IsNotFound(err) {
			// Abort 失败不影响退配额:预扣是 DB 上的账,和存储侧那个半成品是两回事
			logger.L().Warn("过期会话 abort 失败", zap.Uint64("session_id", sess.ID), zap.Error(err))
		}
	}
	s.closeSession(ctx, sess, model.UploadSessionExpired)
}

// abandonSession 上传作废(分片不齐 / 合并结果损坏):清掉存储侧半成品 + 退预扣
func (s *uploadService) abandonSession(ctx context.Context, sess *model.SysUploadSession) {
	if s.mp != nil && sess.UploadID != "" {
		if err := s.mp.AbortMultipartUpload(ctx, sess.FileKey, sess.UploadID); err != nil && !oss.IsNotFound(err) {
			logger.L().Warn("作废会话 abort 失败", zap.Uint64("session_id", sess.ID), zap.Error(err))
		}
	}
	s.closeSession(ctx, sess, model.UploadSessionExpired)
}

// closeSession 终结会话 + 退回预扣。
//
// ⚠️ **退配额和改状态必须在同一个事务里**。反过来(先退再改状态)的话,进程在
// 两步之间挂掉,会话还是 active 而配额已经退了 —— 用户可以反复
// init → 崩溃 → init 把配额刷成负数,等于空间无限。
func (s *uploadService) closeSession(ctx context.Context, sess *model.SysUploadSession, status model.UploadSessionStatus) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.SysUploadSession{}).
			// ⚠️ 带 status = active 的条件:并发下别人已经终结过它的话
			// RowsAffected = 0,这时**不能再退配额**,否则退两次。
			Where("id = ? AND status = ?", sess.ID, model.UploadSessionActive).
			Updates(map[string]any{
				"status":     status,
				"quota_held": 0,
				"upload_id":  "",
				// ⚠️ 副本路径一并清掉 —— 会话已经终结,不该再有人去读它。
				"media_tmp_path": "",
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		return releaseQuota(tx, int64(sess.UserID), sess.QuotaHeld)
	})
	if err != nil {
		// 退配额失败意味着账漂了,必须留下痕迹:已被 close 的会话不会被清理
		// 任务再碰(Done() 为真),这条记录就成了唯一的线索。
		logger.L().Error("终结上传会话失败,配额可能未退回",
			zap.Uint64("session_id", sess.ID), zap.Error(err))
	}
	// ⚠️ **副本跟着会话一起清**(取消 / 过期 / 作废三条路都走这里)。
	//   漏了的话:用户传了一半就取消,临时盘上那份副本一直留着 ——
	//   而**没有任何东西会再碰它**(会话已经终结,清理任务只认 active 的)。
	cleanupMediaTemp(sess.MediaTmpPath)
}

// planParts 决定分片大小和片数。
//
// ⚠️ **策略在 service 层,不在 provider 里** —— oss.CheckPartSize 的注释写明
// 了这一点("100GB 文件用 10MB 分片刚好卡满 10000 片,该调大 partSize,但那是
// service 层按文件大小做的策略决策")。这里做两件事:
//
//  1. 尊重客户端的选择,但不许小于硬下限(否则 local 和 OSS 行为不一致)。
//  2. **片数不得超过 MaxPartNum**。超大文件按默认片长会算出超过 10000 片,
//     OSS 直接拒绝,而 local 会开出上万个临时文件 —— 两边表现完全不同,统一
//     在这里兜住,并把抬大后的片长**回传给客户端**(resp.PartSize),否则客户端
//     还在按它自己以为的片长切,合并出来的顺序全乱。
func (s *uploadService) planParts(fileSize, want int64) (partSize int64, partNum int, err error) {
	partSize = want
	if partSize <= 0 {
		partSize = s.opt.MultipartPartSize
	}
	if partSize < oss.MinPartSize {
		partSize = oss.MinPartSize
	}
	if min := ceilDiv(fileSize, int64(oss.MaxPartNum)); partSize < min {
		partSize = min
	}
	partNum = int(ceilDiv(fileSize, partSize))
	if partNum > oss.MaxPartNum {
		// 到这一步只可能是 fileSize 本身大到连"抬到刚够"都装不下,而那条在
		// Init 开头已经按 MaxFileSize 拦过了。留着是兜底,不是预期路径。
		return 0, 0, errcode.New(errcode.PayloadTooLarge, "文件过大,无法分片")
	}
	return partSize, partNum, nil
}

// checkPartsCover 分片是否凑齐(按总字节,不按片数)
func checkPartsCover(parts []oss.Part, fileSize int64) error {
	if len(parts) == 0 {
		return errcode.New(errcode.Conflict, "还没有上传任何分片")
	}
	var total int64
	seen := make(map[int]bool, len(parts))
	for _, p := range parts {
		if seen[p.PartNumber] {
			return errcode.New(errcode.InvalidParam, "分片清单里有重复片号")
		}
		seen[p.PartNumber] = true
		total += p.Size
	}
	// ⚠️ **按总字节数对,不只是数片数**。片数对了但有一片是 0 字节(客户端把空
	// 分片也报了)的情况,只数片数发现不了,而合并出来就是个缺口的文件。
	if fileSize > 0 && total != fileSize {
		return errcode.New(errcode.Conflict, "分片未传完").WithDetail(sizeDetail(total, fileSize))
	}
	return nil
}

func sizeDetail(actual, expect int64) string {
	return "实际 " + itoa(uint64(actual)) + " 字节,预期 " + itoa(uint64(expect)) + " 字节"
}

func ceilDiv(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

func sessionClosedMsg(st model.UploadSessionStatus) string {
	switch st {
	case model.UploadSessionCanceled:
		return "上传已取消"
	case model.UploadSessionExpired:
		return "上传会话已过期"
	default:
		return "上传会话已结束"
	}
}
