// info.json 生成物(设计文档 §2.3 D3 / §3.4)
//
// ⚠️ **info.json 是生成物,DB 才是唯一真相源**。任何"先写 DB 再写文件"的顺序
// 都会在崩溃 / 磁盘满时留下半截状态;选"随时可以从 DB 重建"之后,info.json
// 丢了只是少了一份可以给人看的快照,不影响任何业务判断。
//
// ⚠️ **它不计入 file_count / total_size**,因为它自己不在 sys_file 里。
// 要是把它算进去,列表上的文件数就会比逐个点进去数出来的多一个,而且每传一个
// 文件数字就涨一次 —— 删了 info.json 又降回去,没法解释。
package service

import (
	"context"
	"encoding/json"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/oss"
)

// infoFileName info.json 的对象名
const infoFileName = "info.json"

// folderInfo info.json 的结构
type folderInfo struct {
	FolderID uint64 `json:"folder_id,string"`
	UserID   uint64 `json:"user_id,string"`
	Biz      string `json:"biz"`
	Name     string `json:"name"`
	// CoverKey 封面 key(没有封面时空串,不是 null)
	CoverKey  string     `json:"cover_key"`
	FileCount int        `json:"file_count"`
	TotalSize int64      `json:"total_size"`
	Generated string     `json:"generated_at"`
	Files     []infoFile `json:"files"`
}

type infoFile struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	ETag        string `json:"etag"`
	// UploadedAt 上传时间(需求原文点名要记的两个字段:时间 + 大小)
	UploadedAt string `json:"uploaded_at"`
}

// infoKey 构造 info.json 的 key
func infoKey(root string, userID uint64, biz string, folderID uint64) (string, error) {
	prefix, err := oss.FolderPrefix(root, userID, biz, folderID)
	if err != nil {
		return "", err
	}
	return prefix + infoFileName, nil
}

// writeFolderInfo 按 DB 现状重建 info.json。
//
// ⚠️ **失败只记日志,不返错**。调用点都是"文件已经成功写进 DB"之后,
// 这时候因为一个锦上添花的快照失败而回滚整个上传,是本末倒置。
// 下一次任何文件变动会顺手把它重建好。
func writeFolderInfo(ctx context.Context, db *gorm.DB, prov oss.Provider, opt FileOptions, folderID uint64) error {
	if prov == nil || folderID == 0 {
		return nil
	}
	var f model.SysFolder
	if err := db.WithContext(ctx).Where("id = ?", folderID).First(&f).Error; err != nil {
		return err
	}
	var files []model.SysFile
	if err := db.WithContext(ctx).
		Where("folder_id = ? AND status = ?", folderID, model.FileStatusNormal).
		Order("id ASC").Find(&files).Error; err != nil {
		return err
	}
	info := folderInfo{
		FolderID:  f.ID,
		UserID:    f.UserID,
		Biz:       f.Biz,
		Name:      f.Name,
		CoverKey:  f.CoverKey,
		FileCount: len(files),
		Generated: nowFn().UTC().Format("2006-01-02T15:04:05Z"),
		Files:     make([]infoFile, 0, len(files)),
	}
	var total int64
	for _, x := range files {
		info.Files = append(info.Files, infoFile{
			Name:        x.FileName,
			Size:        x.FileSize,
			ContentType: x.ContentType,
			ETag:        x.ETag,
			UploadedAt:  x.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
		total += x.FileSize
	}
	info.TotalSize = total

	body, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	key, err := infoKey(opt.Root, f.UserID, f.Biz, f.ID)
	if err != nil {
		return err
	}
	_, err = prov.Put(ctx, key, strings.NewReader(string(body)), "application/json")
	return err
}

// readFolderInfo 读回 info.json 原文。
//
// ⚠️ **M133:这个函数以前永远返回空串** —— 它 type-assert 的是
// `Get(ctx, key) ([]byte, error)`,而**三家 provider 当时一个都没实现**
// (local 只有 Open/Stat,aliyun 连读的方法都没有,mock 的 Get 签名还不一样)。
// 于是 `GET /folders/:id` 的 `info_json` 字段恒为空 —— 而设计文档
// §1001 明写这个端点要返"详情 + info.json 内容"。
// ⇒ 现在三家都实现了 oss.MetaReader,这里才真的读得到。
//
// ⚠️ **读不到就返回空串,不报错** —— info.json 是**生成物**(§2.3 D3),
// DB 才是唯一真相源。读失败(文件还没生成 / 存储抖了)不该让整个文件夹详情
// 查不出来;排障的人看一眼 info_json 字段是空的,再去翻存储那一层的日志。
//
// ⚠️ **provider 没实现 MetaReader 时也返回空串**(type-assert 失败)——
// 新接一个 provider 忘了实现,顶多是这个字段空着,不该整个详情接口 500。
func readFolderInfo(prov oss.Provider, opt FileOptions, f *model.SysFolder) string {
	if prov == nil {
		return ""
	}
	g, ok := prov.(oss.MetaReader)
	if !ok {
		return ""
	}
	key, err := infoKey(opt.Root, f.UserID, f.Biz, f.ID)
	if err != nil {
		return ""
	}
	b, err := g.Get(context.Background(), key)
	if err != nil {
		return ""
	}
	return string(b)
}

// syncFolderInfo 重建 info.json,**失败只记警告,永不返错**。
//
// ⚠️ **两处刻意的选择**:
//  1. **不返错** —— 调用点都在"文件已经成功落库 / 封面已经改成功"之后,
//     为一个可随时重建的快照让整个操作报失败是本末倒置。更糟的是那种
//     "报失败但其实成功":用户看到红色 toast,文件却已经删掉了。
//     真要观察它有没有坏,靠这条 Warn 日志。
//  2. **不用请求的 ctx** —— 请求 ctx 在 handler 返回后就被取消了,而这一步
//     发生在 handler 之外,拿已取消的 ctx 写存储必然失败。用 Background 是
//     有意的,代价是它不受客户端断连影响(这正是我们要的)。
//
// ⚠️ **文件夹被删时不调它**,而且是**调了也没用**:writeFolderInfo 按 id 取
// 文件夹行,而 gorm 的软删会给查询自动加 `deleted_at IS NULL`,软删之后
// 那行已经取不到了。文件夹没了,它的 info.json 也就没有存在意义。
func (s *folderService) syncFolderInfo(folderID uint64) {
	if err := writeFolderInfo(context.Background(), s.db, s.prov, s.opt, folderID); err != nil {
		logger.L().Warn("重建 info.json 失败(可由下次文件变动修复)",
			zap.Uint64("folder_id", folderID), zap.Error(err))
	}
}

func (s *fileService) syncFolderInfo(folderID uint64) {
	if err := writeFolderInfo(context.Background(), s.db, s.prov, s.opt, folderID); err != nil {
		logger.L().Warn("重建 info.json 失败(可由下次文件变动修复)",
			zap.Uint64("folder_id", folderID), zap.Error(err))
	}
}

// uploadService 分片合并成功后同样要刷快照 —— 走的是同一条路径,不是第三份实现。
func (s *uploadService) syncFolderInfo(folderID uint64) {
	if err := writeFolderInfo(context.Background(), s.db, s.prov, s.opt, folderID); err != nil {
		logger.L().Warn("重建 info.json 失败(可由下次文件变动修复)",
			zap.Uint64("folder_id", folderID), zap.Error(err))
	}
}

// parseKeyOwner 从 key 反解 (user_id, biz, folder_id)。
//
// 形态:`{root}/{uid}/{biz}/{fid}/{object}` —— 固定 5 段。
//
// ⚠️ **这里不做自己的解析,直接问 oss.ParseKey** —— oss.KeyParts 是全仓
// key 反解的唯一入口(见 oss/path.go 的注释)。这里再写一份 `strings.Split`
// 就等于多一个"按下标取 uid"的地方,那段数一变就错位,而错位取到的 uid
// 是别人的 —— 归属校验会静默失效。
func parseKeyOwner(key, root string) (userID uint64, biz string, folderID uint64, ok bool) {
	parts, err := oss.ParseKey(root, key)
	if err != nil {
		return 0, "", 0, false
	}
	return parts.UserID, parts.Biz, parts.FolderID, true
}
