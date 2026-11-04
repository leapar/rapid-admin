// FolderService 真实实现
// 设计见 docs/文件系统模块设计方案V1.0.md §4.1 / §8.3
package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/oss"
	"rapid-admin/internal/pkg/sonyflake"
)

// defaultFolderName 懒创建的默认文件夹名
//
// ⚠️ **名字是索引的一部分**(unique(user_id, biz, name)),所以改这三个字等于
// 换一套"什么算同一个文件夹"的语义。老用户的默认文件夹不会跟着改名,
// 于是改完之后同一个用户会同时存在"默认文件夹"和"默认目录"两个默认文件夹。
const defaultFolderName = "默认文件夹"

// maxFolderNameBytes 文件夹名字节上限(和 sys_folder.name 的 size:128 对齐)
const maxFolderNameBytes = 128

// folderService 真实实现
type folderService struct {
	db   *gorm.DB
	prov oss.Provider
	opt  FileOptions
}

// NewFolderService 构造
func NewFolderService(db *gorm.DB, prov oss.Provider, opt FileOptions) FolderService {
	return &folderService{db: db, prov: prov, opt: opt.normalize()}
}

func (s *folderService) List(ctx context.Context, caller *Caller, q FolderListQuery) (*FolderPageResult, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	// ⚠️ **归属人 = 查询里指定的 user_id,没指定就是自己** ——
	//   超管可以看别人的空间(设计文档 §10.2:可跨用户**读**),普通用户不行。
	//
	//   以前这里写死 `Where("user_id = ?", uid)`,于是**超管在文件夹管理页
	//   根本列不出别人的文件夹** —— 而"用户层"正是为了让这件事做得到
	//   (见 folder_users.go 的文件头)。
	owner := uid
	if q.UserID != nil && *q.UserID != uid {
		if !callerCanCrossUserRead(caller) {
			// ⚠️ **返 403,不静默改回自己** —— 静默的话前端以为筛选生效了,
			//   实际返回的是自己的数据(数字和用户预期对不上,而且没有任何报错)。
			return nil, errcode.New(errcode.Forbidden, "无权查看其他用户的文件夹")
		}
		owner = *q.UserID
	}
	page, size := normPage(q.Page, q.Size, 200)

	tx := s.db.WithContext(ctx).Model(&model.SysFolder{}).
		Where("user_id = ?", owner).
		Where("status = ?", model.FileStatusNormal)
	biz := strings.TrimSpace(q.Biz)
	if biz != "" {
		tx = tx.Where("biz = ?", biz)
	}
	// ⚠️ **LIKE 的 % 不能拼进参数值** —— 这里拼的是**值**("%"+kw+"%"),
	//   不是拼 SQL 片段,所以 kw 里带 % 或 _ 只会被当成通配符,不会造成注入。
	//   (拼 SQL 片段那种写法才会被注入。)
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		tx = tx.Where("name LIKE ?", "%"+kw+"%")
	}
	// ⚠️ **层级过滤是"只列这一级",不是"列这棵树"** ——
	//   前端进入一个文件夹时只想看它的直接子级(而不是把整棵树拍平)。
	//   nil = 不限层级(老调用方 / 下拉框);0 = 只列根级;>0 = 只列它的直接子级。
	if q.ParentID != nil {
		tx = tx.Where("parent_id = ?", *q.ParentID)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询文件夹数失败").WithDetail(err.Error())
	}
	var rows []model.SysFolder
	if err := tx.Order("is_default DESC, id ASC").
		Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询文件夹失败").WithDetail(err.Error())
	}
	// ⚠️ **骨架按"同一个 biz"读** —— 口径必须和上面的查询一致,
	//   否则算出来的路径里会混进别的业务段的祖先。
	//
	// ⚠️ **骨架按 owner 读,不是按调用方读** —— 跨用户看别人的空间时,
	//   祖先链和路径必须是**那个用户的**,否则算出来是空路径。
	tree, err := loadFolderTree(ctx, s.db, owner, biz)
	if err != nil {
		return nil, err
	}
	out := make([]FolderView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.enrichFolderView(ctx, tree, toFolderView(r)))
	}
	return &FolderPageResult{
		List:  out,
		Total: total,
		Page:  page,
		Size:  size,
		// ⚠️ **归属人的展示名** —— 跨用户看别人的空间时,界面上要能显示
		//   "这是谁的空间",不然只有一串 id,谁也分不清。
		OwnerName: s.userDisplayName(ctx, owner),
	}, nil
}

// userDisplayName 用户的展示名(real_name → nickname → username)。
//
// ⚠️ **查不到就返回空串,不报错** —— 用户被删了、或三个名字字段都是空的,
//   界面上退回显示 id 就行,不该让整个文件夹列表 500。
func (s *folderService) userDisplayName(ctx context.Context, userID uint64) string {
	if userID == 0 {
		return ""
	}
	var u model.SysUser
	if err := s.db.WithContext(ctx).Select("username", "nickname", "real_name").
		Where("id = ?", userID).First(&u).Error; err != nil {
		return ""
	}
	return folderUserDisplayName(u)
}

// enrichFolderView 补上路径 / 子级数 / 封面直链。
//
// ⚠️ **这三样都必须从骨架 + provider 现算,不能存冗余列** ——
// 冗余列(如 parent_path)一旦有行没同步,界面上就会有一条路径是错的,
// 而且没有任何地方会告诉你它错了。
func (s *folderService) enrichFolderView(ctx context.Context, t *folderTree, v FolderView) FolderView {
	v.Path = t.path(v.ID)
	v.ChildCount = t.childCount(v.ID)
	v.CoverURL = s.coverURL(ctx, v.CoverKey)
	return v
}

// coverURL 封面直链(能直接塞进 `<img src>`)。
//
// ⚠️ **拿不到就返空串,不返错** —— 封面是锦上添花,不能因为封面文件已被删
// 就让整个文件夹列表 500。空串在前端 = 不显示封面,而不是报错。
//
// ⚠️ **这是稳定直链,不是签名 URL** —— 单桶单树模型下每个文件都有稳定直链
// (local 是 `/rapid-admin/...`,OSS 是 `https://{bucket}.{endpoint}/{key}`),
// 签名 URL 过期封面就裂图。
func (s *folderService) coverURL(ctx context.Context, key string) string {
	if key == "" || s.prov == nil {
		return ""
	}
	u, err := s.prov.SignDownloadURL(ctx, key, s.opt.DownloadURLTTL)
	if err != nil {
		return ""
	}
	return u
}

func (s *folderService) Create(ctx context.Context, caller *Caller, req FolderCreateReq) (*FolderView, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errcode.New(errcode.ParamMissing, "文件夹名必填")
	}
	if len(name) > maxFolderNameBytes {
		return nil, errcode.New(errcode.InvalidParam, "文件夹名过长")
	}
	// ⚠️ **名字里不能有 "/"** —— 路径("素材/图纸")是用 "/" 拼的,
	//   名字里带 "/" 会让路径有歧义("a/b" 到底是两级还是一个名字),
	//   而前端要靠路径拆出"所在目录"来显示同名文件夹的区别。
	//   这条不做的话,界面上会出现两条一模一样的路径。
	if strings.Contains(name, "/") {
		return nil, errcode.New(errcode.InvalidParam, "文件夹名不能包含 /")
	}

	// ⚠️ **有上级时,业务段继承上级的** —— 前端建子文件夹时通常不传 biz,
	//   而"默认业务段(default)"和上级所在的段多半不一样。不继承的话子文件夹
	//   会落到另一个目录下,用户在上级里**看不到它**,而且不报错。
	//
	//   显式传了**不一致**的 biz → 报错,不静默改写 —— 调用方明确说了要放哪,
	//   偷偷改成另一个地方比报错更难查。
	biz := strings.TrimSpace(req.Biz)
	var parent *model.SysFolder
	if req.ParentID > 0 {
		p, err := s.loadFolderForWrite(ctx, caller, req.ParentID)
		if err != nil {
			return nil, err
		}
		parent = p
		if biz == "" {
			biz = parent.Biz
		} else if biz != parent.Biz {
			return nil, errcode.New(errcode.InvalidParam, "子文件夹必须与上级文件夹属于同一业务段")
		}
	}
	biz, err = s.resolveBiz(biz)
	if err != nil {
		return nil, err
	}

	// ⚠️ **层级上限在这里判,不在 DB 里判** —— parent_id 是应用层写的,
	//   DB 唯一索引管不了深度。而"建了 30 层"的用户会让路径字符串长到
	//   面包屑一行放不下。
	if parent != nil {
		depth, err := s.folderDepth(ctx, uid, parent.ID)
		if err != nil {
			return nil, err
		}
		if depth+1 > maxFolderDepth {
			return nil, errcode.New(errcode.InvalidParam,
				"文件夹层级不能超过 "+itoa(uint64(maxFolderDepth))+" 层")
		}
	}

	row := model.SysFolder{
		// ⚠️ **ID 必须在这里生成**。model.BaseModel.ID 是
		// `autoIncrement:false`(全仓用 sonyflake 在 Go 侧发号),所以
		// 不给 id 就是给数据库插一个 0 —— 而 0 不是"待分配",它是一个
		// 真实的主键值。后果:第二次建同名文件夹撞主键、loadFolder(0) 永远
		// 查不到、buildFileKey 因为 folder_id=0 直接拒收(整个上传失败),
		// 而且**全程没有任何报错**。全仓建行都要写这一行(见 activity_impl /
		// asset_impl 的同款)。
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    uid,
		Biz:       biz,
		Name:      name,
		ParentID:  req.ParentID,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 撞唯一索引 = 同用户同业务**同父级**下已有同名文件夹。
		// ⚠️ **不要把底层 err 直接吐出去**(带表名和索引名),这里换成人话 ——
		// 但仍然返回 Conflict 而不是 400,因为它确实是状态冲突。
		//
		// ⚠️ **只对"确实是重复键"这么判**。早先这里是**所有** Create 错误一律
		// 返"同名文件夹已存在",于是 DB 抖动 / 连接断 / 磁盘满统统报成这句话 ——
		// 排障的人会被引到完全错误的方向上,而且原错误被整个吞掉,日志里也没有。
		if isDuplicateErr(err) {
			return nil, errcode.New(errcode.Conflict, "同级下已有同名文件夹")
		}
		return nil, errcode.New(errcode.Internal, "创建文件夹失败").WithDetail(err.Error())
	}
	// ⚠️ **新建文件夹立刻写一份 info.json(M140)** —— 设计文档 §3.4 的物理布局
	//   是"每个文件夹里都有一份 info.json",而它以前只在**文件事件**
	//   (上传 / 删除 / 改封面)之后才生成 ⇒ 刚建的空文件夹里没有 info.json,
	//   用户对着存储一看就是"少了一个文件"。
	//   写失败只记警告(syncFolderInfo 自己吞错),不影响这次建文件夹。
	s.syncFolderInfo(row.ID)
	return ptrFolderView(toFolderView(row)), nil
}

// folderDepth 这个文件夹在树里的层数(根级 = 1)。
//
// ⚠️ **按需往上走,不全量读树** —— 建文件夹是个写操作,不该为了判个层级
// 把用户的所有文件夹都拉进内存。层数最多 maxFolderDepth,所以最多查这么多次。
//
// ⚠️ **不按 status 过滤** —— 软删掉的文件夹也占一层。保守方向是对的:
//   宁可少建一层,也不要造出一条超长的路径。
func (s *folderService) folderDepth(ctx context.Context, userID, id uint64) (int, error) {
	depth := 1
	seen := map[uint64]bool{}
	cur := id
	for {
		var p model.SysFolder
		if err := s.db.WithContext(ctx).Select("id", "parent_id").
			Where("id = ? AND user_id = ?", cur, userID).First(&p).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				break
			}
			return 0, errcode.New(errcode.Internal, "查询上级文件夹失败").WithDetail(err.Error())
		}
		if seen[p.ID] {
			break // 环(脏数据),停
		}
		seen[p.ID] = true
		if p.ParentID == 0 {
			break
		}
		depth++
		if depth > maxFolderDepth {
			// 已经超了,不用再往上数了 —— 反正要拒
			break
		}
		cur = p.ParentID
	}
	return depth, nil
}

func (s *folderService) Detail(ctx context.Context, caller *Caller, id uint64) (*FolderDetail, error) {
	if _, err := requireUser(caller); err != nil {
		return nil, err
	}
	f, err := s.loadFolder(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	var files []model.SysFile
	if err := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Where("folder_id = ? AND status = ?", f.ID, model.FileStatusNormal).
		Order("id ASC").Find(&files).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询文件失败").WithDetail(err.Error())
	}
	views := make([]FileView, 0, len(files))
	for _, x := range files {
		views = append(views, toFileView(x))
	}
	// ⚠️ **骨架按"文件夹归属人"读,不是按调用方** —— super_admin 可以跨用户
	//   读别人的文件夹(loadFolder 放行的),这时候祖先链必须按**文件的主人**
	//   找,否则算出来的路径是空的。
	tree, err := loadFolderTree(ctx, s.db, f.UserID, "")
	if err != nil {
		return nil, err
	}
	d := &FolderDetail{
		FolderView: s.enrichFolderView(ctx, tree, toFolderView(*f)),
		Files:      views,
		// ⚠️ **空切片不是 nil** —— nil 序列化成 null,前端 `map(...).length` 会炸。
		Ancestors: make([]FolderView, 0, 4),
		Children:  make([]FolderView, 0, 4),
	}
	// 祖先链(根 → 直接上级):面包屑靠它。
	for _, a := range tree.ancestors(f.ID) {
		d.Ancestors = append(d.Ancestors, s.enrichFolderView(ctx, tree, toFolderView(a)))
	}
	// 直接子级:进入文件夹时"我下面有什么"。
	for _, cid := range tree.childIDs(f.ID) {
		c, ok := tree.nodes[cid]
		if !ok {
			continue
		}
		d.Children = append(d.Children, s.enrichFolderView(ctx, tree, toFolderView(c)))
	}
	// info.json 是生成物,取不到返回空串而不是报错(§2.3 说过它随时能重建)
	d.InfoJSON = readFolderInfo(s.prov, s.opt, f)
	return d, nil
}

// Delete 删文件夹。
//
// ⚠️ **三件事必须做,少一件都会留下漂移**:物理对象、sys_file 行、用户配额。
// 少删物理对象 → 云存储按量计费,钱一直掉;少删配额 → 用户配额永远退不回来,
// 表现是"删了文件还是传不进去"。
//
// ⚠️ **多级文件夹(M125)之后,删的是"整棵子树"** —— 只删自己那一层的话,
//   子文件夹就成了**孤儿**:父没了、子还在,而且再也点不进去(前端按 parent_id
//   列目录),用户会以为文件凭空消失了,而且没有任何地方会告诉他"你删的只是
//   上一层"。所以这里把子孙一起删 —— 物理对象、行、配额一个都不少。
func (s *folderService) Delete(ctx context.Context, caller *Caller, id uint64) error {
	if _, err := requireUser(caller); err != nil {
		return err
	}
	f, err := s.loadFolderForWrite(ctx, caller, id)
	if err != nil {
		return err
	}
	if f.IsDefault {
		return errcode.New(errcode.Forbidden, "默认文件夹不可删除")
	}
	// ⚠️ **头像段是系统托管空间,不可删(M140)** —— 头像文件夹以前删不掉,
	//   是因为它被**误标成了 is_default**;M140 把 is_default 归零了
	//   (is_default 的语义是"上传落点",不是"受保护",见 ensureFolder),
	//   所以这里必须**显式**挡住,不能靠那个误标顺带挡。
	//
	//   不挡的后果:用户删掉"头像"文件夹 ⇒ 头像文件被连带删掉,而
	//   sys_user.avatar 还指着那个已经死掉的 URL ⇒ 全站头像裂图
	//   (前端会回落到首字母头像,但那是"降级",不是"删除"该有的样子);
	//   而且这个文件夹本来就不是用户该管的东西(设计文档 §17:头像只有
	//   一条写通道 = POST /auth/avatar)。
	if f.Biz == avatarBiz {
		return errcode.New(errcode.Forbidden, "头像文件夹不可删除")
	}

	// ⚠️ **骨架不限 biz** —— 子孙可能跨段(历史数据),限了 biz 就会漏删。
	tree, err := loadFolderTree(ctx, s.db, f.UserID, "")
	if err != nil {
		return err
	}
	targets := s.deleteTargets(tree, f)

	// 先查要退多少配额 —— **必须在软删之前查**,软删后 rows 就不再匹配条件了
	var freed int64
	if err := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Where("folder_id IN ? AND status = ?", targets.ids, model.FileStatusNormal).
		Select("COALESCE(SUM(file_size), 0)").Scan(&freed).Error; err != nil {
		return errcode.New(errcode.Internal, "统计待释放配额失败").WithDetail(err.Error())
	}

	// 每个文件夹在存储侧占一段前缀 —— 子孙各有各的,少删一段就是"文件还在
	// 云存储里,但库里已经没了"(账单照收,而且没人会知道)。
	prefixes, err := s.folderPrefixesOf(targets.rows)
	if err != nil {
		return errcode.New(errcode.Internal, "构造文件夹前缀失败").WithDetail(err.Error())
	}

	// 物理删除放在事务外:OSS 的一次网络往返不该把 DB 事务拖长,更不该在
	// 事务里等网络。⚠️ 顺序是"先删物理、后删 DB" —— 反过来的话 DB 提交后
	// 物理删失败,就留下一条指向空气的记录,而且没人会再去清理它。
	if s.prov != nil {
		for _, p := range prefixes {
			if _, err := s.prov.DeletePrefix(ctx, p); err != nil {
				return errcode.New(errcode.Internal, "删除物理文件失败").WithDetail(err.Error())
			}
		}
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := nowFn()
		if err := tx.Model(&model.SysFile{}).
			Where("folder_id IN ? AND status = ?", targets.ids, model.FileStatusNormal).
			Updates(map[string]any{
				"status":     model.FileStatusDeleted,
				"is_cover":   false,
				"deleted_at": now,
			}).Error; err != nil {
			return errcode.New(errcode.Internal, "删除文件记录失败").WithDetail(err.Error())
		}
		// ⚠️ **软删必须改名**,否则唯一索引 (user_id, biz, parent_id, name)
		// 会让这个名字一直被占着,用户"删掉再传"就传不进去了。原因见
		// 设计文档 §4.1 的更正块。
		for _, row := range targets.rows {
			if err := tx.Model(&model.SysFolder{}).Where("id = ?", row.ID).
				Updates(map[string]any{
					"name":       deletedFolderName(row.Name, row.ID),
					"status":     model.FileStatusDeleted,
					"deleted_at": now,
				}).Error; err != nil {
				return errcode.New(errcode.Internal, "删除文件夹记录失败").WithDetail(err.Error())
			}
		}
		if freed > 0 {
			if err := releaseQuota(tx, int64(f.UserID), freed); err != nil {
				return err
			}
		}
		return nil
	})
	// ⚠️ **这里刻意不调 syncFolderInfo**,而且调了也没用:文件夹已经软删,
	// 而 gorm 的软删会给查询自动加 `deleted_at IS NULL`,writeFolderInfo
	// 那一行 First() 取不到它。前面 DeletePrefix 也已经把整个前缀连同
	// info.json 一起删掉了 —— 没有需要更新的东西。
}

// deleteTargets 待删的文件夹集合(自己 + 全部子孙)
type deleteTargets struct {
	// ids 待删的文件夹 id
	ids []uint64
	// rows 对应的行(要拿 name 改名、拿 biz 拼存储前缀)
	rows []model.SysFolder
}

// deleteTargets 收集"自己 + 整棵子树"。
//
// ⚠️ **自己一定在第一行** —— 即使它在骨架里找不着(理论上不可能:
//   loadFolderForWrite 刚查出来过),也要保证 ids 非空,否则
//   `WHERE folder_id IN ()` 会匹配不到任何行,配额和文件一个都删不掉,
//   而接口还返回 200("删除成功")。
func (s *folderService) deleteTargets(t *folderTree, f *model.SysFolder) deleteTargets {
	out := deleteTargets{ids: []uint64{f.ID}, rows: []model.SysFolder{*f}}
	for _, id := range t.subtree(f.ID) {
		if id == f.ID {
			continue
		}
		n, ok := t.nodes[id]
		if !ok {
			continue
		}
		out.ids = append(out.ids, n.ID)
		out.rows = append(out.rows, n)
	}
	return out
}

// folderPrefixesOf 这些文件夹在存储侧占用的**全部**前缀。
//
// ⚠️ **每个文件夹一段前缀** —— 存储侧只有一棵树(见 oss/path.go 的文件头),
// 没有"公开/私有"两套布局,所以一个文件夹在存储里只占一段前缀。
//
// 以前这里返回两条(私有 + 公开),因为 key 有两种形态;模型简化后第二条
// 永远是空的。留着它不会报错,但会让"文件夹删了但存储用量没降"这种问题
// 的排查多一个假设。
//
// ⚠️ **每个文件夹的前缀用它自己的 biz 拼** —— key 的第三段是 biz,
//   子文件夹继承父的 biz(建的时候校验过),但历史数据不一定,
//   所以这里按行取,不按父取。
func (s *folderService) folderPrefixesOf(rows []model.SysFolder) ([]string, error) {
	out := make([]string, 0, len(rows))
	for _, f := range rows {
		p, err := oss.FolderPrefix(s.opt.Root, f.UserID, f.Biz, f.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// deletedFolderName 软删时的新名字。
//
// ⚠️ **带 id 后缀不是为了好看,是为了让原名立刻可用**。原名被占着的话,
// 唯一索引会让用户再也建不出同名文件夹,而用户完全看不出为什么。
func deletedFolderName(name string, id uint64) string {
	suffix := "#del:" + itoa(id)
	// 截断原名保证加上后缀不超 size:128 —— 超了 MySQL 严格模式下直接报错,
	// 而这种错误只在"文件夹名刚好接近上限 + 删它"时才出现,极难复现。
	room := maxFolderNameBytes - len(suffix)
	if len(name) > room {
		// ⚠️ 按字节截会切碎 UTF-8 多字节字符。这里宁可留空也不切 ——
		// 一个叫 "#del:123" 的行不影响任何逻辑(它已经查不到了)。
		name = ""
	}
	return name + suffix
}

// SetCover 关联封面:把 folder 里的某个文件设为封面。
//
// ⚠️ **只有图片能当封面**(M125)。以前这里什么都不查,于是把一个 PDF /
//   一个 txt 设成封面也能成功 —— 而封面是**当图片渲染**的(列表页的缩略图、
//   详情页的大图),非图片文件的表现是"封面位置裂图",而且用户完全不知道
//   是自己选错了文件。
func (s *folderService) SetCover(ctx context.Context, caller *Caller, id, fileID uint64) (*FolderView, error) {
	if _, err := requireUser(caller); err != nil {
		return nil, err
	}
	f, err := s.loadFolderForWrite(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	var file model.SysFile
	if err := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Where("id = ? AND folder_id = ? AND status = ?", fileID, f.ID, model.FileStatusNormal).
		First(&file).Error; err != nil {
		// ⚠️ 文件不在这个文件夹里同样返 404 —— 泄露"该 file_id 存在但不在此文件夹"
		// 和"文件不存在"之间的差异,攻击者能靠它摸清别人的文件布局。
		return nil, errcode.New(errcode.NotFound, "文件不存在")
	}
	// ⚠️ **判据是库里存的 content_type,不是文件扩展名** ——
	//   content_type 是**服务端按扩展名探测**后落库的值(见 probeContentType),
	//   不是浏览器上报的,所以它至少是"服务端认定的类型"。
	if !isCoverImage(file.ContentType) {
		return nil, errcode.New(errcode.InvalidParam, "只有图片才能设为封面")
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先把旧的 is_cover 清掉再设新的。⚠️ 少了这步,换封面之后旧文件
		// 仍然带 is_cover=1,而"删封面文件"是按这个标记决定要不要顺带清文件夹封面的。
		if err := tx.Model(&model.SysFile{}).
			Where("folder_id = ? AND is_cover = ?", f.ID, true).
			Update("is_cover", false).Error; err != nil {
			return errcode.New(errcode.Internal, "清除旧封面标记失败").WithDetail(err.Error())
		}
		if err := tx.Model(&model.SysFile{}).Where("id = ?", file.ID).
			Update("is_cover", true).Error; err != nil {
			return errcode.New(errcode.Internal, "设置封面标记失败").WithDetail(err.Error())
		}
		if err := tx.Model(&model.SysFolder{}).Where("id = ?", f.ID).
			Updates(map[string]any{"cover_key": file.FileKey, "cover_file_id": file.ID}).Error; err != nil {
			return errcode.New(errcode.Internal, "设置文件夹封面失败").WithDetail(err.Error())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// ⚠️ **info.json 写失败不影响这次操作的成败,但封面本身必须照常返回**。
	// syncFolderInfo 自己吞掉错误并记 Warn(见 folder_info.go);早先这里写的
	// 是 `return nil, nil`,那会让 handler 以 HTTP 200 + 空 body 应答,前端拿到
	// undefined,表现是"设置成功了但界面没变"—— 比报错更难查。
	s.syncFolderInfo(f.ID)
	f.CoverKey, f.CoverFileID = file.FileKey, file.ID
	v := toFolderView(*f)
	// ⚠️ **返回体里带上封面直链** —— 前端拿到 cover_key 自己拼不出地址
	// (key 的形态在 local 和 OSS 下不一样),拼错就是一张裂图。
	v.CoverURL = s.coverURL(ctx, file.FileKey)
	return ptrFolderView(v), nil
}

// isCoverImage 这个 MIME 能不能当封面。
//
// ⚠️ **SVG 被排除** —— 理由和 handler 的 isInlineType 一样:SVG 里能塞
//   <script>,而封面会被"点开看大图"(新标签页打开直链)—— 那时它是**同源
//   文档**,脚本会跑在本站的 origin 下。这是存储变成站内 XSS 投放位。
//
// ⚠️ **只看 MIME,不看文件头** —— 设封面时文件已经在存储里了,这里要再判
//   一次魔数就得把字节读一遍(一次 IO,而且 OSS provider 刻意不实现
//   Open,读不了)⇒ 两套部署行为会不一致。魔数那一关在**上传**时已经做过了
//   (见 user_avatar.go 的 isImageBytes),这里是第二道、便宜的闸。
func isCoverImage(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if !strings.HasPrefix(ct, "image/") {
		return false
	}
	if strings.Contains(ct, "svg") {
		return false
	}
	return true
}

// ensureFolder 取或建目标文件夹。
//
// ⚠️ **懒创建默认文件夹** 是并发热点:两个请求同时发现"没有默认文件夹"、
// 同时去建,后一个撞唯一索引。处理方式是**把唯一索引的冲突当成"别人建好了"**,
// 重查一次返回 —— 而不是报错让用户重试。
//
// ⚠️ **头像段(biz="avatar")是另一套规则(M140)** —— 那里是"一个用户一个
// 专属文件夹",查找**不按名字**,按 (user_id, biz, parent_id)。
// 理由:老用户的头像文件夹叫"默认文件夹"(M140 之前懒创建时复用了默认名),
// 按名字找会找不到它 ⇒ 会**再建一个"头像"**,于是同一个用户有两个头像文件夹,
// 头像文件散落在两处,而用户完全看不出(两个都叫得出名字,页面也不报错)。
func (s *folderService) ensureFolder(ctx context.Context, caller *Caller, folderID uint64, biz, name string) (*FolderView, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	if folderID > 0 {
		f, err := s.loadFolder(ctx, caller, folderID)
		if err != nil {
			return nil, err
		}
		return ptrFolderView(toFolderView(*f)), nil
	}

	if biz == "" {
		biz, err = s.resolveBiz("")
		if err != nil {
			return nil, err
		}
	} else if err := s.checkBiz(biz); err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name = defaultFolderName
	}
	// ⚠️ **头像段用专属名字** —— 它不是"用户的默认文件夹",是"用户的头像文件夹"。
	//   名字进了唯一索引,所以历史行的改名由 NormalizeAvatarFolders 统一做
	//   (见 folder_maintain.go),这里只管新行的命名。
	avatarSeg := biz == avatarBiz
	if avatarSeg {
		name = avatarFolderName
	}

	f, err := s.findEnsureFolder(ctx, uid, biz, name, avatarSeg)
	if err == nil {
		// ⚠️ **顺手把头像文件夹的名字纠正过来** —— 启动期的 NormalizeAvatarFolders
		//   是"尽力而为"(可能没跑过、可能跑失败),这里是最后一道收敛:
		//   只要有人动了头像段,名字就一定是对的。
		if avatarSeg {
			s.normalizeAvatarFolderName(ctx, &f)
		}
		return ptrFolderView(toFolderView(f)), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errcode.New(errcode.Internal, "查询文件夹失败").WithDetail(err.Error())
	}

	f = model.SysFolder{
		// ⚠️ 必须显式发号,理由见上面 Create() 的注释(BaseModel 是
		// autoIncrement:false,不写就是插 id=0)
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    uid,
		Biz:       biz,
		Name:      name,
		ParentID:  0,
		// ⚠️ **头像文件夹不是"默认文件夹"** —— is_default 的语义是"用户上传时
		//   folder_id=0 落到的那个"(前端按它定位上传目标)。头像段是专属空间,
		//   标成默认会让前端把它当上传目标 ⇒ 普通文件会传进头像文件夹。
		IsDefault: !avatarSeg && name == defaultFolderName,
	}
	if err := s.db.WithContext(ctx).Create(&f).Error; err != nil {
		// 撞唯一索引 = 并发下别人先建好了,重查一次。
		// ⚠️ **只在真的是重复键时才重查**。别的情况(DB 挂了、字段太长、连接断)
		// 去重查必然查不到,查不到就落到下面那句"创建文件夹失败"—— 但原错误
		// 已经被这次重查吞掉了,而此时它才是唯一有诊断价值的信息。
		if isDuplicateErr(err) {
			if existing, reErr := s.findEnsureFolder(ctx, uid, biz, name, avatarSeg); reErr == nil {
				return ptrFolderView(toFolderView(existing)), nil
			}
		}
		return nil, errcode.New(errcode.Internal, "创建文件夹失败").WithDetail(err.Error())
	}
	// ⚠️ **新建文件夹立刻写一份 info.json(M140)** —— 设计文档 §3.4 的物理布局
	//   是"每个文件夹里都有一份 info.json",而它以前只在**文件事件**
	//   (上传 / 删除 / 改封面)之后才生成 ⇒ 刚建的空文件夹里没有 info.json,
	//   用户对着存储一看就是"少了一个文件"。
	//   写失败只记警告(syncFolderInfo 自己吞错),不影响这次建文件夹。
	s.syncFolderInfo(f.ID)
	return ptrFolderView(toFolderView(f)), nil
}

// findEnsureFolder 懒创建时找"已经存在的那个文件夹"。
//
// ⚠️ **parent_id = 0 也要进查询条件** —— 唯一索引是
// (user_id, biz, parent_id, name),默认文件夹建在**根级**,
// 而"同名但挂在别人文件夹下"的那一行**不该**被当成它。
//
// ⚠️ **头像段不按名字找**(avatarSeg=true)—— 那一段是"一个用户一个文件夹",
// 按名字找会漏掉历史上叫"默认文件夹"的那一个(见 ensureFolder 的注释)。
func (s *folderService) findEnsureFolder(ctx context.Context, uid uint64, biz, name string, avatarSeg bool) (model.SysFolder, error) {
	var f model.SysFolder
	tx := s.db.WithContext(ctx).
		Where("user_id = ? AND biz = ? AND parent_id = ? AND status = ?",
			uid, biz, uint64(0), model.FileStatusNormal)
	if avatarSeg {
		// ⚠️ **取最早建的那个** —— 万一历史上真的建出过多个(老版本按名字
		//   找、现在按段找的边界),要有一个稳定的"主"文件夹,
		//   否则每次调用可能命中不同的那个,头像文件就会在两个文件夹之间跳。
		tx = tx.Order("id ASC")
	} else {
		tx = tx.Where("name = ?", name)
	}
	err := tx.First(&f).Error
	return f, err
}

// loadFolder 取文件夹并校验归属。
//
// ⚠️ **"不存在"和"不是你的"返同一个 404**(设计文档 §10.3)。分开返的话,
// 攻击者能靠状态码差异枚举出哪些 id 真的存在。
func (s *folderService) loadFolder(ctx context.Context, caller *Caller, id uint64) (*model.SysFolder, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errcode.New(errcode.NotFound, "文件夹不存在")
	}
	var f model.SysFolder
	if err := s.db.WithContext(ctx).Where("id = ? AND status = ?", id, model.FileStatusNormal).
		First(&f).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "文件夹不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询文件夹失败").WithDetail(err.Error())
	}
	// super_admin 可以跨用户**读**,但不能改 —— 所以只有只读路径才放行,
	// 且这里返回的行会被写路径拒掉(见 loadFolderForWrite)。
	if f.UserID != uid && !callerCanCrossUserRead(caller) {
		return nil, errcode.New(errcode.NotFound, "文件夹不存在")
	}
	return &f, nil
}

// loadFolderForWrite 同上,但**不允许跨用户**。
//
// ⚠️ 写路径必须和读路径分开(设计文档 §10.2):读允许 super_admin 跨用户,
// 写不允许。合成一个函数靠调用方传 flag 的话,总有一天有人传错 —— 而且
// 传错的后果是 super_admin 能删掉别人的文件,而这种洞在审计日志里看不出来
// (他本来就有权限登录)。
func (s *folderService) loadFolderForWrite(ctx context.Context, caller *Caller, id uint64) (*model.SysFolder, error) {
	f, err := s.loadFolder(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	if err := callerOwns(caller, f.UserID); err != nil {
		return nil, err
	}
	return f, nil
}
