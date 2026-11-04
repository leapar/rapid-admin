// 文件模块的共用小工具 —— 归属校验 / 配额记账 / view 转换
// 设计见 docs/文件系统模块设计方案V1.0.md §4.5 / §8.4 / §10.2
//
// ⚠️ **这些函数单独成文件不是为了整洁,是因为它们跨 FileService /
// FolderService / UploadService 三个实现复用**。各写一份的话,三处的
// "0 配额 = 不限" 迟早会有一处写成 `used+size > quota` 忘了 `quota > 0`,
// 于是不限额的账号一上传就被拒,而且报错是"超出配额",极难定位。
package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/oss"
)

// nowFn 时间源,可注入(测试里控制过期判定,不用真 sleep)
var nowFn = func() time.Time { return time.Now() }

// itoa uint64 → string
func itoa(n uint64) string { return strconv.FormatUint(n, 10) }

// normPage 归一分页参数
func normPage(page, size, maxSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > maxSize {
		size = 20
	}
	return page, size
}

// requireUser 取当前登录人 id
func requireUser(caller *Caller) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	return caller.UserID, nil
}

// callerCanCrossUserRead super_admin 可跨用户**读**(设计文档 §10.2)。
//
// ⚠️ **只读,不是可写**。想让管理员也能改别人的文件,是一个独立的产品决策,
// 不能顺带从"能看"滑到"能删"。
func callerCanCrossUserRead(caller *Caller) bool {
	return caller != nil && caller.RoleCode == model.RoleSuperAdmin
}

// callerOwns 校验归属:不是自己的就返 404 而不是 403。
//
// ⚠️ **返 404 而不是 403 是安全要求**(§10.3):403 会告诉攻击者"这个 id
// 确实存在,只是你没权限",403 和 404 的差异足以把全站 id 枚举出来。
func callerOwns(caller *Caller, ownerID uint64) error {
	uid, err := requireUser(caller)
	if err != nil {
		return err
	}
	if ownerID != uid {
		return errcode.New(errcode.NotFound, "资源不存在")
	}
	return nil
}

// validateBiz 业务段语法校验。
//
// 规则本身在 oss 包(和路径构造共用一份),这里只做转发 + 把 oss 的错误类型
// 映射成 errcode —— 让前端看到的是"biz 不合法"而不是 `oss: invalid key`。
func validateBiz(biz string) error {
	if biz == "" {
		return errcode.New(errcode.ParamMissing, "biz 必填")
	}
	// 用一个占位 pathSpec 跑一遍完整构造:根词 / user / folder / 对象名都合法,
	// 所以构造失败只可能是 biz 这一段。⚠️ **不要为了"只校验一段"另写一个
	// 校验函数** —— 那份规则迟早跟 oss 包里的分叉,而分叉出来的版本没人记得测。
	if _, err := oss.BuildKey(oss.PathSpec{
		Root: oss.DefaultRoot, UserID: 1, Biz: biz, FolderID: 1, ObjectName: "x",
	}); err != nil {
		return errcode.New(errcode.InvalidParam, "biz 不合法").WithDetail(err.Error())
	}
	return nil
}

// checkBiz 语法校验 + 白名单(`file.allowed_biz`)。
//
// ⚠️ **白名单由业务侧配,框架不硬编码**(与字典单一来源的思路一致):
// 留空 = 不限,只做语法校验;填了就当白名单用。
//
// ⚠️ 为什么**必须真的拦住**,而不能只当"文档里的一句话":biz 是 key 的第二段,
// 而 key 是静态挂载和生命周期规则的输入。不限的话,任何登录用户都能 POST
// `{biz:"<任意合法小写串>"}` 建出一整个新的目录树 —— 别人想用"删除生命周期
// 规则清掉过期文件"时,清不干净,因为目录根本不在规则覆盖范围内。
func (s *folderService) checkBiz(biz string) error {
	if err := validateBiz(biz); err != nil {
		return err
	}
	allowed := s.opt.AllowedBiz
	if len(allowed) == 0 {
		return nil
	}
	for _, a := range allowed {
		if a == biz {
			return nil
		}
	}
	return errcode.New(errcode.InvalidParam, "biz 不在允许范围内")
}

// resolveBiz 定业务段:没给就用默认 `default`。
//
// ⚠️ **默认值写死成 `default`,不是从用户资料推**。用户资料里的 biz 是
// 身份概念(业主/物业/供应商),而这里是存储目录概念,两者混在一起会出现
// "同一用户在不同身份下文件目录不同",而文件夹列表是按 (user_id, biz) 查的,
// 于是他每次切身份都觉得自己文件没了。
func (s *folderService) resolveBiz(biz string) (string, error) {
	if biz == "" {
		return defaultBiz, nil
	}
	if err := s.checkBiz(biz); err != nil {
		return "", err
	}
	return biz, nil
}

// defaultBiz 未指定业务段时的默认值
const defaultBiz = "default"

// maxFileNameBytes 文件名字节上限(和 sys_file.file_name /
// sys_upload_session.file_name 的 size:255 对齐)
//
// ⚠️ **按字节不按字符**:255 字节的中文名只有 85 个字,而 iOS 允许 255 字符。
//
// ⚠️ **不能靠 oss.SanitizeObjectName 兜**:它限的是**清洗后**的对象名
// (≤240 字节),而 sys_file.file_name 存的是**原名**。清洗只会让名字变短
// (剥路径 / NFC 归一化),所以 `dir/dir/.../很长的名字.pdf` 能过清洗却塞不进
// varchar(255) —— MySQL 严格模式下 INSERT 直接失败(表现为"传完了但登记失败",
// 对象刚写进去又被删掉),非严格模式下静默截断(文件名被砍掉一截,用户不知道)。
const maxFileNameBytes = 255

// checkFileName 文件名能不能进 sys_file.file_name 列。
func checkFileName(name string) error {
	if len(name) > maxFileNameBytes {
		return errcode.New(errcode.InvalidParam, "文件名过长").
			WithDetail("原始名 " + itoa(uint64(len(name))) + " 字节,上限 " + itoa(maxFileNameBytes) + " 字节")
	}
	return nil
}

// isDuplicateErr 这个错是不是"撞唯一索引了"。
//
// ⚠️ **不能只靠 errors.Is(err, gorm.ErrDuplicatedKey)**:那是各 dialect 自己注册
// 的翻译器给出的 sentinel,而本仓两种驱动(MySQL / glebarez sqlite,单测用)
// 都翻译了、postgres 驱动没进依赖 —— 一旦有人加一种驱动,这个判断会静默变成
// "永远 false",而它守的是"把重复键和真故障分开"这条线。所以补一层消息匹配。
func isDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || // MySQL 1062
		strings.Contains(msg, "UNIQUE constraint failed") || // SQLite
		strings.Contains(msg, "duplicate key value") // PostgreSQL 23505
}

// ============== 配额记账 ==============
//
// ⚠️ **全部用单条 SQL 的原子自增,不用"读出来 +n 再写回去"**。后者在并发
// 预扣下必然丢更新:两个请求各读到 used=100,各写 100+50=150,实际应该是 200,
// 而账上少了 50 —— 而且永远对不上,没有任何报错。

// quotaWhere 配额闸门的 SQL 条件。
//
// ⚠️ **这段 SQL 和 `model.SysUser.QuotaExceeded` 必须同进同退** —— 两处都把
// 0 当"不限"。这里之所以要再写一遍 SQL 而不是调 Go 方法,是因为**只有一条
// SQL 才能把"检查"和"自增"合成原子操作**;Go 方法只能用来提前失败、给个人话
// 错误信息,它**永远不是**判定依据(那是 TOCTOU)。
//
// ⚠️⚠️ **这里自带一个 `?`** —— 它绑的就是 size。调用方**只能再传一个 size**
// (见 holdQuota)。多传一个不会报编译错,也不会报运行错在"SQL 语法"上,而是
// 变成 "sql: expected 4 arguments, got 5"(详见 holdQuota 的注释)。
const quotaWhere = "(storage_quota_bytes <= 0 OR storage_used_bytes + ? <= storage_quota_bytes)"

// holdQuota 预扣配额。超了返 false(**不改任何东西**)
//
// ⚠️⚠️ **实参个数必须刚好等于占位符个数** —— 这里曾经多传了一个 size,
// 线上表现是"上传文件报 sql: expected 4 arguments, got 5"。
//
// **为什么多传一个不会立刻被发现**:GORM 的 `clause.Expr.Build` 在
// "占位符比参数少" 时,把多出来的参数当成 `sql.NamedArg` 塞进 `stmt.Vars`,
// **却不给它写对应的 `?`**。于是最终 SQL 有 4 个 ?、5 个参数 ——
// 而 `database/sql` 只在驱动报告了 NumInput() >= 0 时才查个数:
//
//   - 单测用的 glebarez/sqlite(底层 modernc.org/sqlite)报 **-1**(=不查)
//     ⇒ 单测**恒绿**,这个 bug 能在仓库里活很久;
//   - MySQL 驱动报真实个数 ⇒ 直接炸 "expected N arguments, got M"。
//
// **所以"单测跑过了"在这里什么都不能证明** —— 必须显式断言占位符个数,
// 见 file_quota_test.go 的 TestHoldQuota_ArgCountMatchesPlaceholders。
func holdQuota(tx *gorm.DB, userID, size int64) (bool, error) {
	if size <= 0 {
		return true, nil
	}
	res := tx.Model(&model.SysUser{}).
		// ⚠️ **两个实参,不是三个**:`id = ?` 绑 userID,`quotaWhere` 里那个
		// `?` 绑 size。**不要再补第三个 size**。
		Where("id = ? AND "+quotaWhere, userID, size).
		Update("storage_used_bytes", gorm.Expr("storage_used_bytes + ?", size))
	if res.Error != nil {
		return false, errcode.New(errcode.Internal, "预扣配额失败").WithDetail(res.Error.Error())
	}
	return res.RowsAffected > 0, nil
}

// releaseQuota 退回配额
//
// ⚠️ **不做下限截断**。`storage_used_bytes` 会因为"文件落盘成功、DB 记账失败"
// 这类崩溃而漂移(§4.5),截断成 0 看着好看,但会把"漂移"变成"账被清零" ——
// 后者更难查:用户会发现配额莫名其妙涨回很大。
func releaseQuota(tx *gorm.DB, userID, size int64) error {
	if size <= 0 {
		return nil
	}
	res := tx.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("storage_used_bytes", gorm.Expr("storage_used_bytes - ?", size))
	if res.Error != nil {
		return errcode.New(errcode.Internal, "退回配额失败").WithDetail(res.Error.Error())
	}
	return nil
}

// settleQuota 按实际大小结算预扣(§4.5 的 ③ 找零)。
//
// held 是当初预扣的,actual 是真正落盘的。差值要么退(实际更小),
// 要么**再扣**(实际更大 —— 客户端申报的大小可以撒谎)。
func settleQuota(tx *gorm.DB, userID, held, actual int64) error {
	delta := held - actual
	if delta > 0 {
		return releaseQuota(tx, userID, delta)
	}
	if delta < 0 {
		ok, err := holdQuota(tx, userID, -delta)
		if err != nil {
			return err
		}
		if !ok {
			return errcode.New(errcode.InvalidParam, "实际大小超出配额")
		}
	}
	return nil
}

// quotaInfo 读用户配额(给 /files/quota 之类的展示用)
func quotaInfo(ctx context.Context, db *gorm.DB, caller *Caller) (*QuotaView, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	var u model.SysUser
	if err := db.WithContext(ctx).Select("storage_used_bytes", "storage_quota_bytes").
		Where("id = ?", uid).First(&u).Error; err != nil {
		return nil, errcode.New(errcode.NotFound, "用户不存在")
	}
	return &QuotaView{
		UsedBytes:   u.StorageUsedBytes,
		QuotaBytes:  u.StorageQuotaBytes,
		Unlimited:   u.StorageQuotaBytes <= 0,
		RemainBytes: u.QuotaRemaining(u.StorageUsedBytes),
	}, nil
}

// QuotaView 配额视图
type QuotaView struct {
	UsedBytes int64 `json:"used_bytes"`
	// QuotaBytes 0 = 不限
	QuotaBytes int64 `json:"quota_bytes"`
	// Unlimited 显式告诉前端"不限额",别让它靠 quota_bytes==0 去猜
	Unlimited   bool  `json:"unlimited"`
	RemainBytes int64 `json:"remain_bytes"`
}

// ============== key 构造 ==============

// buildFileKey 构造文件 key —— **全仓唯一的一份**。
//
// ⚠️ 三个入口(UploadToken / Receive / 分片 Init)必须走同一个函数。它们只是
// "字节从哪来"不同 —— 服务端写、客户端直传、分片合并 —— 而 key 的规则
// (根词 / user / biz / folder / 对象名)一旦分叉,漏改的那处会把
// 文件写到别人目录里或者不该写的段去,**而且没有任何报错**。
//
// ⚠️ **没有"公开"参数** —— 存储侧只有一棵树,文件放哪儿跟公开性无关。
func buildFileKey(o FileOptions, userID uint64, biz string, folderID uint64, fileName string) (string, error) {
	// ⚠️ **原名也要过长度闸**,不能只靠 NewObjectName 限清洗后的对象名 ——
	// 理由见 checkFileName 的注释(sys_file.file_name 存的是原名)。
	if err := checkFileName(fileName); err != nil {
		return "", err
	}
	objName, err := oss.NewObjectName(fileName)
	if err != nil {
		return "", errcode.New(errcode.InvalidParam, "文件名不合法").WithDetail(err.Error())
	}
	key, err := oss.BuildKey(oss.PathSpec{
		Root:       o.Root,
		UserID:     userID,
		Biz:        biz,
		FolderID:   folderID,
		ObjectName: objName,
	})
	if err != nil {
		return "", errcode.New(errcode.InvalidParam, "文件路径不合法").WithDetail(err.Error())
	}
	return key, nil
}

// ============== view 转换 ==============

func toFolderView(r model.SysFolder) FolderView {
	return FolderView{
		ID:          r.ID,
		UserID:      r.UserID,
		Biz:         r.Biz,
		Name:        r.Name,
		ParentID:    r.ParentID,
		CoverKey:    r.CoverKey,
		CoverFileID: r.CoverFileID,
		FileCount:   r.FileCount,
		TotalSize:   r.TotalSize,
		IsDefault:   r.IsDefault,
		Status:      int(r.Status),
		CreatedAt:   r.CreatedAt,
	}
}

func ptrFolderView(v FolderView) *FolderView { return &v }

// toFileView 把行转成视图。
func toFileView(r model.SysFile) FileView {
	return FileView{
		ID:          r.ID,
		FolderID:    r.FolderID,
		UserID:      r.UserID,
		Biz:         r.Biz,
		FileKey:     r.FileKey,
		FileName:    r.FileName,
		FileSize:    r.FileSize,
		ContentType: r.ContentType,
		ETag:        r.ETag,
		Storage:     int(r.Storage),
		IsCover:     r.IsCover,
		Status:      int(r.Status),
		CreatedAt:   r.CreatedAt,
		DurationMs:  r.DurationMs,
		// ⚠️ **M145:原始编码 + 为什么播不了** —— 原样透传,前端直接显示。
		//   判据在 pkg/media(BrowserPlayable),这里只搬运。
		MediaContainer:  r.MediaContainer,
		MediaVideoCodec: r.MediaVideoCodec,
		MediaAudioCodec: r.MediaAudioCodec,
		MediaReason:     r.MediaReason,
		TranscodeStatus: int(r.TranscodeStatus),
	}
}

// folderFileStats 重算文件夹的 file_count / total_size。
//
// ⚠️ **每次都从 sys_file 重算,不做增量**。增量更新(UPDATE ... SET
// file_count = file_count + 1)在"软删 + 回滚 + 并发"组合下会算错,而算错了
// 列表页上完全看不出来(只是数字不对,没有报错)。重算的成本是一次
// COUNT + SUM,有 (folder_id) 索引兜着。
func folderFileStats(tx *gorm.DB, folderID uint64) (count int, total int64, err error) {
	type agg struct {
		C int64
		S int64
	}
	var a agg
	if err := tx.Model(&model.SysFile{}).
		Select("COUNT(*) AS c, COALESCE(SUM(file_size), 0) AS s").
		Where("folder_id = ? AND status = ?", folderID, model.FileStatusNormal).
		Scan(&a).Error; err != nil {
		return 0, 0, errcode.New(errcode.Internal, "统计文件夹失败").WithDetail(err.Error())
	}
	return int(a.C), a.S, nil
}

// syncFolderStats 重算并回写冗余计数
func syncFolderStats(tx *gorm.DB, folderID uint64) error {
	c, s, err := folderFileStats(tx, folderID)
	if err != nil {
		return err
	}
	if err := tx.Model(&model.SysFolder{}).Where("id = ?", folderID).
		Updates(map[string]any{"file_count": c, "total_size": s}).Error; err != nil {
		return errcode.New(errcode.Internal, "更新文件夹统计失败").WithDetail(err.Error())
	}
	return nil
}
