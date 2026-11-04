// 头像上传 —— 走 M80 文件系统,落进"用户 id 相关文件夹"
//
// 设计见 docs/文件系统模块设计方案V1.0.md §17(头像业务段)
//
// # 为什么头像要走文件系统,而不是直接往 sys_user 上存字节
//
// sys_user.avatar 是 **size:255 的字符串列**,存的是**直链**。
// 早期设计里前端可以直接往 `PATCH /auth/profile` 传一个 avatar 字段
// (任意 URL),那条路是 `UpdateProfileDTO.Avatar` —— **死代码**:
// 前端从来没发过它,但它**能被发**,而且能被发成**任意外部 URL**。
//
// 而头像会渲染在**别人的浏览器里**(管理端用户列表 / 顶栏 / 身份切换器)
// ⇒ 那条路等于让任意用户往管理员的页面上塞一个**外部图片**,
// 那就是**跟踪像素**(能读 IP、Referer、还能给管理员浏览器发请求)。
//
// ⇒ 现在的规矩:
//   1. **头像只能上传**,不能贴外链(`UpdateProfileDTO.Avatar` 已删);
//   2. 落盘走文件系统,key 落在**自己 id 底下**,别人拿不到;
//   3. 存进 sys_user.avatar 的是**稳定直链**,不是签名 URL ——
//      签名 URL 15 分钟就过期,而头像要一直显示着。
//
// # 落点
//
//	biz = "avatar",folder_id = 0
//	⇒ ensureFolder 懒建 (user_id, biz, name) 的文件夹
//	⇒ key = "{root}/{user_id}/avatar/{folder_id}/{name}"
//
// ⚠️ **依赖 `file.allowed_biz` 留空**(不限业务段)。
// 运营把它收窄成比如 ["default"] 的话,头像上传会直接被
// "biz 不在允许范围内"拒掉 —— 症状是"头像传不上去",但看不出是配置问题。
// 收窄白名单时**必须把 avatar 加进去**(同"框架不硬编码业务枚举"的取舍)。
package service

import (
	"bufio"
	"context"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// avatarBiz 头像在文件系统里的业务段
const avatarBiz = "avatar"

// avatarFolderName 头像文件夹的名字
//
// ⚠️ **它必须是专属名字,不能复用"默认文件夹"(M140)** —— 头像段是
// "一个用户一个文件夹"的专属空间,叫"默认文件夹"会让人在文件夹管理页
// 分不清哪个是头像、哪个是普通素材。用户 2026-10-31 报的就是这个。
//
// ⚠️ **名字进了唯一索引** `uk_folder_owner_biz_parent_name`
// (user_id, biz, parent_id, name) ⇒ 改它等于换一套"什么算同一个文件夹"
// 的语义。历史行的改名由 NormalizeAvatarFolders 统一做
// (见 folder_maintain.go),**不能只改常量** —— 只改常量的话老用户会同时
// 存在"默认文件夹"和"头像"两个头像文件夹,头像文件散落在两处。
const avatarFolderName = "头像"

// maxAvatarBytes 头像字节上限(5 MB)
//
// ⚠️ **比文件系统自己的上限(100MB)小得多** —— 文件系统的上限是给
// "工程图纸 / 视频"那类东西用的,头像没有理由那么大。
// 而这里就是**唯一**能给用户省配额的地方:头像换了十次,
// 旧文件会被回收(见 applyAvatar),但如果不限大小,一次传 100MB
// 就能把配额吃光。
const maxAvatarBytes = 5 << 20

// allowedAvatarTypes 允许的头像 MIME(服务端探测出来的,不是浏览器上报的)
var allowedAvatarTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/jpg":  true,
	"image/gif":  true,
	"image/webp": true,
	"image/bmp":  true,
}

// SetAvatar 上传并设置当前用户的头像。
func (s *userService) SetAvatar(ctx context.Context, caller *Caller, req AvatarUploadReq) (*AvatarView, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	if s.files == nil {
		return nil, errcode.New(errcode.Internal, "文件系统未初始化")
	}
	if req.Body == nil || req.Size <= 0 {
		return nil, errcode.New(errcode.ParamMissing, "文件内容为空")
	}
	if req.Size > maxAvatarBytes {
		return nil, errcode.New(errcode.InvalidParam, "头像不能超过 5 MB")
	}

	// ⚠️ **类型判定要两关,缺一不可**:
	//
	// ① **服务端探测出来的 MIME**(`probeContentType`:扩展名优先,其次上报值)。
	//    这一关管的是"**存下来之后用什么 Content-Type 发出去**"。
	//    扩展名优先 ⇒ 一个叫 evil.html 的文件会被判成 text/html → 直接拒。
	//    (浏览器上报的 type 是客户端说了算的,不能作准。)
	//
	// ② **文件头魔数**。这一关管的是"**这真的是一张图吗**"。
	//    只做 ① 的话,一个把 HTML 改名成 a.png 的文件会被放行,
	//    存下来以 image/png 发出去 —— 浏览器不会当脚本跑(✅),
	//    但它也不是图片(❌ 头像位会显示裂图)。
	//
	// 两关都过才放行。魔数用 bufio.Peek 读,**不消费** Body。
	br := bufio.NewReader(req.Body)
	ct := probeContentType(req.FileName, req.ContentType)
	if !allowedAvatarTypes[ct] {
		return nil, errcode.New(errcode.InvalidParam, "头像只支持 PNG / JPEG / GIF / WEBP / BMP 图片")
	}
	if !isImageBytes(br) {
		return nil, errcode.New(errcode.InvalidParam, "文件内容不是图片")
	}

	// 落盘。⚠️ **folder_id = 0** ⇒ 文件系统懒建 (uid, "avatar") 的文件夹,
	// key 落在自己 id 底下(见文件头)。
	fv, err := s.files.Receive(ctx, caller, FileReceiveReq{
		FolderID:    0,
		Biz:         avatarBiz,
		FileName:    req.FileName,
		ContentType: ct,
		Size:        req.Size,
		Body:        br,
	})
	if err != nil {
		return nil, err
	}

	// 写 sys_user.avatar + 回收旧头像
	if err := s.applyAvatar(ctx, caller, fv); err != nil {
		// ⚠️ **上传成功了但写库失败 ⇒ 把刚传的那个删掉**。
		// 不删的话,存储里躺着一个**谁也不引用**的文件 —— 它占着配额,
		// 而且在文件管理页里看得到(用户会问"这什么文件")。
		if delErr := s.files.Delete(ctx, caller, fv.ID); delErr != nil {
			zap.L().Warn("头像写库失败后回收新文件也失败",
				zap.Uint64("user_id", uid),
				zap.Uint64("file_id", fv.ID),
				zap.Error(delErr))
		}
		return nil, err
	}

	return &AvatarView{
		URL:      fv.URL,
		FileID:   itoa(fv.ID),
		FileName: fv.FileName,
		FileSize: fv.FileSize,
	}, nil
}

// applyAvatar 把新头像写进 sys_user,并回收旧头像文件。
//
// # 为什么要回收旧头像
//
// 不回收的话,用户每换一次头像就**永久多占一份配额**(头像最大 5MB,
// 配额几十 MB ⇒ 换十几次就传不上了)。而文件管理页里会躺着一堆
// "同一张脸的历史版本",用户自己也不知道那些是什么。
//
// # 回收哪几个
//
//	(user_id = 我) AND (biz = "avatar") AND (id != 新文件)
//	AND (created_at <= 新文件的 created_at)
//
// ⚠️ **用 created_at 而不是"当前 avatar 指向的那个"** ——
// 当前 avatar 存的是 URL,从 URL 反推文件要靠 provider 的 URL 形态,
// 而 local 和 OSS 的 URL 结构不一样(local 少了 root 段)⇒ 那是第二份真相。
// created_at 是文件自己的字段,不会随部署形态变化。
//
// # 并发:同一个人同时传两次
//
// 判据是"**谁最后写 avatar**"和"**谁删谁的文件**"不能打架。
// 做法:
//   1. 事务里**锁住 sys_user 行**(`FOR UPDATE`)⇒ 同一个人的两次上传串行化;
//   2. 锁内先确认**新文件还活着**(status = normal),
//      活着才写 avatar —— 被上一次并发清理掉的就不写;
//   3. 锁内收集要回收的 id,事务外再真删(事务里不做 IO)。
//
// ⚠️ **SQLite 没有行级锁**(`clause.Locking` 被忽略,见
// approval_engine_impl.go 的同款用法与 approval_engine_test.go 的说明),
// 所以 dev 环境这条路径没有真正的串行化 —— 最坏结果是头像指向一个
// 刚被删掉的文件(404),而前端的 UserAvatar 会自动回落到首字母头像。
// 真并发要在 MySQL 上验。
func (s *userService) applyAvatar(ctx context.Context, caller *Caller, fv *FileView) error {
	uid, err := requireUser(caller)
	if err != nil {
		return err
	}
	var staleIDs []uint64
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 锁用户行 —— 同一个人的并发上传在这里串行化
		var user model.SysUser
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", uid).First(&user).Error; err != nil {
			return errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
		}
		// 新文件可能已经被并发的另一次上传当成"旧文件"回收了
		var alive int64
		if err := tx.Model(&model.SysFile{}).
			Where("id = ? AND status = ?", fv.ID, model.FileStatusNormal).
			Count(&alive).Error; err != nil {
			return errcode.New(errcode.Internal, "校验头像文件失败").WithDetail(err.Error())
		}
		if alive == 0 {
			return errcode.New(errcode.Conflict, "头像已被另一次上传替换,请重试")
		}
		if err := tx.Model(&model.SysUser{}).Where("id = ?", uid).
			Update("avatar", fv.URL).Error; err != nil {
			return errcode.New(errcode.Internal, "保存头像失败").WithDetail(err.Error())
		}
		// 收集旧头像(比新文件旧的、且不是新文件本身)
		if err := tx.Model(&model.SysFile{}).
			Where("user_id = ? AND biz = ? AND id != ? AND status = ? AND created_at <= ?",
				uid, avatarBiz, fv.ID, model.FileStatusNormal, fv.CreatedAt).
			Pluck("id", &staleIDs).Error; err != nil {
			return errcode.New(errcode.Internal, "查询旧头像失败").WithDetail(err.Error())
		}
		return nil
	})
	if err != nil {
		return err
	}
	// ⚠️ **物理删除放在事务外** —— 事务里不做 IO(删对象可能慢,
	// 而这一行锁会把同用户的其它写全堵住)。
	// 删失败只记日志:旧头像删不掉不影响新头像生效,
	// 而在这里返错会让用户看到"头像设置失败"——实际上已经成功了。
	for _, id := range staleIDs {
		if err := s.files.Delete(ctx, caller, id); err != nil {
			zap.L().Warn("回收旧头像文件失败",
				zap.Uint64("user_id", uid),
				zap.Uint64("file_id", id),
				zap.Error(err))
		}
	}
	return nil
}

// isImageBytes 判文件头是不是已知图片格式。
//
// ⚠️ **只看文件头** —— 完整的图片解析(解码器)不现实也没必要:
// 头像只会被渲染,不会被执行,风险点是"伪装成图片的脚本"。
// 脚本文件的魔数没有一个跟图片撞上。
//
// ⚠️ **只 Peek 不消费** —— 返回的 bufio.Reader 后面还要交给
// `Receive` 写进存储,这里把流读空了,存进去的就是 0 字节。
func isImageBytes(br *bufio.Reader) bool {
	head, err := br.Peek(16)
	if err != nil && len(head) == 0 {
		return false
	}
	switch {
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return true
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return true
	case len(head) >= 6 && (string(head[:6]) == "GIF87a" || string(head[:6]) == "GIF89a"):
		return true
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return true
	case len(head) >= 2 && head[0] == 'B' && head[1] == 'M':
		return true
	}
	return false
}
