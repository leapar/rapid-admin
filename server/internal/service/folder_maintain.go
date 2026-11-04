// 文件夹自愈 —— 头像文件夹改名 + 缺失的 info.json 补齐(M140)
//
// # 这两个问题的共同点:它们都不会报错
//
// 用户 2026-10-31 报了两件事:
//
//  1. 用户的头像文件夹名字也叫"默认文件夹"(该叫"头像");
//  2. 用户头像文件夹里没有 info.json("按道理每个文件夹都应该有这个文件")。
//
// 查下来第 2 条有两个来源:
//
//   - **机制上的**:info.json 是**生成物**(设计文档 §2.3 D3),只在
//     **文件事件**(上传 / 删除 / 改封面)之后才重建 ⇒ **一个刚建的空文件夹
//     里永远没有 info.json**。这是"每个文件夹都有 info.json"这条约定
//     (§3.4 的物理布局)从来没被真正兑现的地方。
//   - **历史遗留的**:M133 之前 Receive 这条路不重建 info.json,
//     所以 M133 之前传过文件的文件夹也可能缺。
//
// 而第 1 条是个**语义错位**:头像段(biz="avatar")是"一个用户一个文件夹"的
// 专属空间,叫"默认文件夹"会让人在文件夹管理页分不清哪个是头像、哪个是素材。
//
// ⚠️ **两件事都不会报错** —— 文件夹照样建得出来、文件照样传得进去、
// 列表照样有数据。区别只在**存储里的物理布局**和**界面上的名字**。
// 所以"接口返 200"不能证明它们是对的,只能去看存储里那一层。
//
// ⚠️ **本文件的注释一律不缩进续行** —— Go 1.19 的 gofmt 会把缩进的续行
// 当成"代码块"重排(多一行空行 + 一个 tab),段落被拦腰截断。
// 新增文件要过 gofmt。
package service

import (
	"context"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/oss"
)

// defaultInfoBackfillLimit 一次启动最多补多少个文件夹的 info.json
//
// ⚠️ **必须有上界** —— 补 info.json 要逐个问存储"在不在"(每个文件夹一次
// HEAD),文件夹多了就是启动期的网络风暴。上界之外的部分留给下一次启动
// (补齐是幂等的,分几次补完没差别)。
const defaultInfoBackfillLimit = 500

// NormalizeAvatarFolders 把头像段的文件夹统一成"专属名字 + 非默认"。
//
// ⚠️ **改名不碰存储** —— 文件夹名是**展示用**的,不进 key
// (key 用的是 folder_id,见 §3.1)⇒ 改名字不需要搬对象,零迁移。
//
// ⚠️ **返回"改了几行",不改名的那行也要把 is_default 归零** —— 头像文件夹
// 标成"默认"会让前端把它当成上传目标(前端按 is_default 定位默认文件夹),
// 于是普通文件会传进头像文件夹。
//
// ⚠️ **目标名被占就不改名** —— 唯一索引
// (user_id, biz, parent_id, name) 撞了会直接失败,而失败会连累整个启动流程。
// 这种情况下只归零 is_default,并记一条 Warn(说明有历史脏数据要人工看)。
func NormalizeAvatarFolders(ctx context.Context, db *gorm.DB) (int, error) {
	var rows []model.SysFolder
	if err := db.WithContext(ctx).
		Where("biz = ? AND status = ?", avatarBiz, model.FileStatusNormal).
		Order("id ASC").Find(&rows).Error; err != nil {
		return 0, err
	}
	changed := 0
	for i := range rows {
		ok, err := renameAvatarFolderRow(ctx, db, &rows[i])
		if err != nil {
			return changed, err
		}
		if ok {
			changed++
		}
	}
	return changed, nil
}

// renameAvatarFolderRow 把一行头像文件夹纠正成"专属名字 + 非默认"。
//
// ⚠️ **这是唯一的改名实现** —— 启动期的批量改和懒路径上的顺手改共用它。
// 两份实现必然会分叉(比如一份改了 is_default、另一份没改),
// 而分叉出来的行为是"有的用户的头像是'头像'、有的是'默认文件夹'"。
//
// ⚠️ **返回"真的改了没有"** —— 调用点用它来计数/打日志。
func renameAvatarFolderRow(ctx context.Context, db *gorm.DB, f *model.SysFolder) (bool, error) {
	if f == nil || f.Biz != avatarBiz {
		return false, nil
	}
	if f.Name == avatarFolderName && !f.IsDefault {
		return false, nil
	}
	// ⚠️ **目标名被占就不改名** —— 撞唯一索引会失败,而且失败发生在
	//   "写库"这一步 ⇒ 整个改名动作回滚,连 is_default 也归不了零。
	var dup int64
	if err := db.WithContext(ctx).Model(&model.SysFolder{}).
		Where("user_id = ? AND biz = ? AND parent_id = ? AND name = ? AND id <> ? AND status = ?",
			f.UserID, f.Biz, f.ParentID, avatarFolderName, f.ID, model.FileStatusNormal).
		Count(&dup).Error; err != nil {
		return false, err
	}
	updates := map[string]any{}
	if dup > 0 {
		logger.L().Warn("file: 头像文件夹改名跳过 —— 目标名已被占用(历史脏数据,请人工确认)",
			zap.Uint64("folder_id", f.ID),
			zap.Uint64("user_id", f.UserID),
			zap.String("current_name", f.Name),
			zap.String("target_name", avatarFolderName))
	} else if f.Name != avatarFolderName {
		updates["name"] = avatarFolderName
	}
	if f.IsDefault {
		updates["is_default"] = false
	}
	if len(updates) == 0 {
		return false, nil
	}
	if err := db.WithContext(ctx).Model(&model.SysFolder{}).
		Where("id = ?", f.ID).Updates(updates).Error; err != nil {
		return false, err
	}
	if _, ok := updates["name"]; ok {
		f.Name = avatarFolderName
	}
	f.IsDefault = false
	return true, nil
}

// normalizeAvatarFolderName 懒路径上顺手把头像文件夹的名字纠正过来。
//
// ⚠️ **失败只记 Warn,不返错** —— 头像文件夹叫什么不影响头像能不能传
// (落点用的是 folder_id,不是名字)。为一个展示用的名字把整次上传搞失败
// 是本末倒置。
func (s *folderService) normalizeAvatarFolderName(ctx context.Context, f *model.SysFolder) {
	if _, err := renameAvatarFolderRow(ctx, s.db, f); err != nil {
		logger.L().Warn("file: 头像文件夹改名失败(不影响业务,下次启动会重试)",
			zap.Uint64("folder_id", f.ID), zap.Error(err))
	}
}

// BackfillFolderInfo 给**缺 info.json** 的文件夹补一份。
//
// ⚠️ **先问存储"在不在",缺了才写** —— 每次启动都无脑重写一遍的话,
// 存储侧凭空多 N 次 PUT,而 info.json 是生成物、内容一样,写了也是白写。
//
// ⚠️ **单个文件夹失败不中断整批** —— 一个文件夹写不进去(存储抖了、
// key 非法)不该让后面的文件夹都补不上。失败记 Warn,继续下一个。
//
// ⚠️ **只补"正常"状态的文件夹** —— 软删的文件夹它的 info.json 早就跟着
// 前缀一起删掉了,补回去是给一堆死数据续命。
//
// ⚠️ **幂等** —— 重复调用不会产生副作用:在的跳过,缺的补上。
func BackfillFolderInfo(ctx context.Context, db *gorm.DB, prov oss.Provider, opt FileOptions, limit int) (int, error) {
	if prov == nil {
		// ⚠️ 没装存储 ⇒ 没有 info.json 这回事(不是错误,是没这个能力)
		return 0, nil
	}
	if limit <= 0 {
		limit = defaultInfoBackfillLimit
	}
	var rows []model.SysFolder
	if err := db.WithContext(ctx).
		Where("status = ?", model.FileStatusNormal).
		Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	written := 0
	for _, f := range rows {
		key, err := infoKey(opt.Root, f.UserID, f.Biz, f.ID)
		if err != nil {
			logger.L().Warn("file: 构造 info.json key 失败,跳过该文件夹",
				zap.Uint64("folder_id", f.ID), zap.Error(err))
			continue
		}
		_, statErr := prov.Stat(ctx, key)
		if statErr == nil {
			continue // 已经有了
		}
		if !oss.IsNotFound(statErr) {
			// ⚠️ **不是"不存在"就别写** —— 存储读失败(网络 / 权限 / 凭证过期)
			//   时写下去也是失败,而且会把真正的故障原因淹掉。
			logger.L().Warn("file: 读 info.json 失败,跳过该文件夹",
				zap.Uint64("folder_id", f.ID), zap.String("key", key), zap.Error(statErr))
			continue
		}
		if err := writeFolderInfo(ctx, db, prov, opt, f.ID); err != nil {
			logger.L().Warn("file: 补写 info.json 失败",
				zap.Uint64("folder_id", f.ID), zap.String("key", key), zap.Error(err))
			continue
		}
		written++
	}
	return written, nil
}
