// M80 文件系统 —— service 层特征测试
// 设计见 docs/文件系统模块设计方案V1.0.md §8
//
// 覆盖的是**记账和归属**,不是"能不能写文件"(那是 M77 provider 的测试)。
// 这两件事错起来都不报错:
//   - 记账漂移 → 用户看到"配额莫名其妙少了一截",而文件一个不少
//   - 归属漏判 → A 能下载 B 的文件,而 HTTP 状态是 200
//
// 用真实的 local provider + 真实的内存 sqlite,不用 mock:这一层的 bug 大多
// 藏在"provider 的真实返回值"和"GORM 的真实行为"(RowsAffected 是不是 0)
// 之间,两个都 mock 掉就等于什么都没测。
package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/oss"
	"rapid-admin/internal/pkg/oss/local"
	"rapid-admin/internal/pkg/sonyflake"
)

// ====================== 测试脚手架 ======================

func newFileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysUser{},
		&model.SysFolder{},
		&model.SysFile{},
		&model.SysUploadSession{},
	))
	return db
}

// seedFileUser 建一个用户。quotaBytes=0 = 不限额
func seedFileUser(t *testing.T, db *gorm.DB, quotaBytes int64) uint64 {
	t.Helper()
	id := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel:         model.BaseModel{ID: id},
		Username:          "u" + itoa(id),
		IsActive:          true,
		StorageQuotaBytes: quotaBytes,
	}).Error)
	return id
}

func mkFileCaller(userID uint64) *Caller {
	return &Caller{UserID: userID, IdentityID: userID, RoleCode: "role_admin", Audience: "admin", Username: "t"}
}

// newFileTestProv 真实 local provider,落在临时目录
func newFileTestProv(t *testing.T) (*local.Provider, FileOptions) {
	t.Helper()
	root := t.TempDir()
	opt := DefaultFileOptions()
	opt.Root = oss.DefaultRoot
	p, err := local.New(local.Config{
		Root: root,
		// ⚠️ **URLBase 必须等于 "/" + opt.Root** —— 生产里 factory 就是这么
		// 装的(static 挂载点是 key 的第一段)。两个值对不上时:
		//   key  = rapid-admin/{uid}/...
		//   URL  = /res/{uid}/...        ← 挂载点是 /rapid-admin,永远 404
		// 而直链是**不过后端、不查库**的 ⇒ 线上表现是"图裂了,日志里一次
		// 请求都没有"。这里写死 "/res" 就是复刻那个 bug。
		URLBase: "/" + opt.Root,
	})
	require.NoError(t, err)
	return p, opt
}

func newFileSvc(t *testing.T, db *gorm.DB) (FileService, *local.Provider) {
	t.Helper()
	p, opt := newFileTestProv(t)
	return NewFileService(db, p, opt), p
}

func newUploadSvc(t *testing.T, db *gorm.DB) (UploadService, *local.Provider) {
	t.Helper()
	p, opt := newFileTestProv(t)
	return NewUploadService(db, p, opt), p
}

func usedBytes(t *testing.T, db *gorm.DB, userID uint64) int64 {
	t.Helper()
	var u model.SysUser
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Select("storage_used_bytes").First(&u).Error)
	return u.StorageUsedBytes
}

func errCodeIs(err error, code int) bool {
	var e *errcode.Error
	return errors.As(err, &e) && e.Code == code
}

func put(t *testing.T, svc FileService, caller *Caller, name string, content []byte, size int64) *FileView {
	t.Helper()
	v, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: name, ContentType: "application/octet-stream",
		Size: size, Body: bytes.NewReader(content),
	})
	require.NoError(t, err)
	return v
}

// ===== 分片尺寸约定 =====
//
// ⚠️ **分片测试必须用 ≥ oss.MinPartSize 的尺寸,不能用 256 字节这种"看起来
// 更清楚"的小分片**。planParts 会把任何小于 MinPartSize 的请求抬到下限,而
// local 的分片偏移是按 **session 里存的那个 partSize** 算的
// (`offset = (partNumber-1) * sc.PartSize`)。于是客户端要是按自己请求的
// 256 去切,第 2 片就会落在 offset=102400 —— 一个 1KB 的文件当场报
// "写到 offset 102400 会超出总大小 1024",报错信息完全指不到真正的原因。
//
// 换句话说:**resp.PartSize 是权威值,客户端必须按它切**(见 planParts 的注释)。
// TestMultipart_PartSizeIsAuthoritative 把这条契约本身锁住。
const (
	mpPartSize = int64(128 << 10) // 131072,≥ MinPartSize(100KB),不会被抬
	mpFileSize = int64(2) * mpPartSize
	mpPartNum  = 2
)

// mpBody 生成内容可辨识的测试文件(每片首字节不同,片序错乱立刻能看出来)
func mpBody() []byte {
	b := make([]byte, mpFileSize)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// ====================== 单次上传 ======================

// 特征:落库的是**实际**大小,不是申报大小;配额按实际收;文件夹冗余计数跟着涨。
func TestReceive_HappyPath(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	body := []byte("hello rapid-admin")
	v := put(t, svc, caller, "plan.txt", body, int64(len(body)))

	require.Equal(t, int64(len(body)), v.FileSize, "库里记的必须是实际字节数")
	require.Equal(t, body, mustRead(t, prov, v.FileKey), "存储里的内容必须一模一样")
	require.Equal(t, int64(len(body)), usedBytes(t, db, uid), "配额按实际大小扣")

	var folder model.SysFolder
	require.NoError(t, db.First(&folder, v.FolderID).Error)
	require.Equal(t, 1, folder.FileCount, "文件夹冗余计数要跟着涨")
	require.Equal(t, int64(len(body)), folder.TotalSize)
}

// 特征:**上传出口就带直链** —— 前端拿 FileView 就能直接渲染,
// 不用再打一次 /files/:id/url。
//
// ⚠️ 存储侧只有一棵树,没有"公开/私有"之分(见 oss/path.go 的文件头),
// 所以这条对**每一个**文件都成立 —— 不是"公开文件才有的特权"。
func TestReceive_GetsDirectURL(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	body := []byte("publicbytes")
	v, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: "cover.jpg", ContentType: "image/jpeg",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.NoError(t, err)
	require.NotEmpty(t, v.URL, "上传出口必须带直链")
	require.True(t, strings.HasPrefix(v.URL, "/"+oss.DefaultRoot+"/"),
		"直链要指向静态挂载点(/"+oss.DefaultRoot+"/), got %q", v.URL)
	require.NotContains(t, v.URL, "/public/",
		"没有公开段这回事了 —— key 恒为 root/uid/biz/folder/name")

	// 再传一个:直链也必须给得出来 —— 不能"第一次有、第二次没有"。
	second := put(t, svc, caller, "cover2.jpg", body, int64(len(body)))
	require.NotEmpty(t, second.URL, "每个文件都要有直链")
}

// ====================== M134 列表带直链 ======================

// 特征:**列表里的每一行都带稳定直链** —— 缩略图视图要把它塞进 `<img src>`。
//
// ⚠️ 以前列表**不填** url,理由写的是"一次签 N 个 URL 会把列表拖慢"。
// **那个前提在 M80 单桶单树模型之后已经不成立**:直链是**拼出来的**
// (不签名、不回存储、不调 OSS),代价只是 N 次字符串拼接 —— 文件夹列表的
// `cover_url` 早就是这么干的,两边对齐。
//
// 不填的后果:缩略图视图只能逐条调 `/files/:id/url`,一页 20 个文件
// 就是 20 个往返(而且慢的那个决定整页的显示)。
//
// ⚠️ **直链必须稳定**(不带签名、不带过期参数)—— 签名 URL 过期就裂图,
// 而 `<img src>` 是**页面打开那一刻**就取一次,之后再也不会重签。
func TestList_FillsDirectURL(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	put(t, svc, caller, "a.txt", []byte("hello"), 5)

	res, err := svc.List(context.Background(), caller, FileListQuery{})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Total)

	files := pageFiles(t, res)
	require.Len(t, files, 1)
	require.NotEmpty(t, files[0].URL, "列表必须带直链 —— 缩略图视图靠它渲染 <img src>")
	require.True(t, strings.HasPrefix(files[0].URL, "/"+oss.DefaultRoot+"/"),
		"直链要指向静态挂载点(/"+oss.DefaultRoot+"/), got %q", files[0].URL)
	require.NotContains(t, files[0].URL, "?",
		"直链必须是稳定的(带签名参数 = 过期就裂图)")
}

// 特征:**对象不在了也不能让整页列表失败** —— 拿不到直链就留空。
//
// ⚠️ 直链是纯展示用的。一个文件的字节没了(存储被清过 / 手工删过),
// 整页列表 500 是**把一个展示问题升级成整页不可用** —— 前端只能显示
// "加载失败",用户以为文件都丢了。
func TestList_URLFailureDoesNotBreakThePage(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	v := put(t, svc, caller, "b.txt", []byte("hello"), 5)

	// 把对象从存储里直接抽掉(模拟"库里还有、存储里没了")
	require.NoError(t, prov.Delete(context.Background(), v.FileKey))

	res, err := svc.List(context.Background(), caller, FileListQuery{})
	require.NoError(t, err, "一个文件签不出直链,列表不该报错")
	require.Equal(t, int64(1), res.Total)
	files := pageFiles(t, res)
	require.Len(t, files, 1)
	require.Empty(t, files[0].URL, "签不出来就留空,前端据此显示占位图标")
}

// 特征:**中转读取的字节必须逐字节一致**,响应头要拿得到原名和类型。
//
// ⚠️ 直链是主路径,中转是兜底 —— 而兜底坏掉的症状更隐蔽(前端只在直链
// 拿不到时才走它),可它坏掉的场合恰恰就是"直链拿不到"的场合。
func TestContent_StreamsBytes(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	body := []byte("payload 42")
	v := put(t, svc, caller, "note.txt", body, int64(len(body)))

	rc, f, err := svc.Content(context.Background(), caller, v.ID)
	require.NoError(t, err)
	defer rc.Close()

	require.Equal(t, body, mustReadSeek(t, rc), "中转读出来的字节必须逐字节一致")
	require.Equal(t, "note.txt", f.FileName, "下游要拿原名填 Content-Disposition")
	require.NotEmpty(t, f.ContentType, "下游要拿服务端探测的类型填响应头")
	require.Equal(t, int64(len(body)), f.FileSize, "下游要拿它算 Content-Length")
}

// 特征:**中转读的归属校验和 URL 那条路完全一样** —— 跨用户一律 404。
//
// ⚠️ 403 是错的:它等于告诉 A"这个 id 确实存在,只是不是你的",扫一遍 id
// 就能把别人的文件清单摸出来。
func TestContent_CrossUserIsNotFound(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)

	v := put(t, svc, mkFileCaller(uidA), "secret.txt", []byte("s3cret"), 6)

	_, _, err := svc.Content(context.Background(), mkFileCaller(uidB), v.ID)
	require.True(t, errCodeIs(err, errcode.NotFound), "want NotFound, got %v", err)
}

// 特征:provider 不实现 oss.ContentProvider 时返 **501 而不是 500**。
// "OSS 部署下不支持中转"是一个可预期的形态,不是一个故障 —— 用 500 报出来
// 会让监控把它算进故障率,而实际上它只是说明"这个部署走直链"。
func TestContent_UnsupportedProviderIsNotImplemented(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	_, prov := newFileSvc(t, db)
	// 套一个只实现 Provider、不实现 ContentProvider 的壳
	svc := NewFileService(db, noContentProvider{Provider: prov}, DefaultFileOptions())

	v := put(t, svc, mkFileCaller(uid), "a.txt", []byte("abc"), 3)

	_, _, err := svc.Content(context.Background(), mkFileCaller(uid), v.ID)
	require.True(t, errCodeIs(err, errcode.NotImplemented), "want NotImplemented, got %v", err)
}

// noContentProvider 刻意不实现 oss.ContentProvider 的 provider 包装,
// 用来验证"可选接口缺失"这条分支。嵌入 Provider 而不是显式列方法,
// 是为了让编译期就保证它满足 oss.Provider。
type noContentProvider struct{ oss.Provider }

// 特征:**不采信申报大小**。客户端报 1000 实际传 10,配额只收 10 ——
// 反过来(多报)也不会被反扣成负数,settleQuota 会再扣一次。
func TestReceive_SettlesByActualNotDeclared(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	v := put(t, svc, caller, "small.txt", []byte("0123456789"), 1000)

	require.Equal(t, int64(10), v.FileSize)
	require.Equal(t, int64(10), usedBytes(t, db, uid),
		"申报 1000 实际 10 → 找零 990,最终只占 10")
}

// 特征:**写失败必须把预扣退回去**。漏这一步的话每传一次坏文件就白吃一份配额,
// 用户什么文件都没传上,看到的却是"空间越来越不够"。
func TestReceive_PutFailureRollsBackQuota(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	_, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: "boom.txt", Size: 500, Body: failingReader{},
	})
	require.Error(t, err)
	require.Equal(t, int64(0), usedBytes(t, db, uid), "写失败 → 预扣要退回去")

	var n int64
	require.NoError(t, db.Model(&model.SysFile{}).Count(&n).Error)
	require.Equal(t, int64(0), n, "写失败不该留下 sys_file 行")
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

// 特征:超额直接拒,且**什么都没写**。
func TestReceive_QuotaExceededRejected(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 100)
	svc, prov := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	_, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: "big.bin", Size: 500, Body: bytes.NewReader(make([]byte, 500)),
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)
	require.Equal(t, int64(0), usedBytes(t, db, uid))

	// 存储里不该有任何对象 —— 这一步要靠 List 才能看出来,光看 DB 不够。
	// ⚠️ **必须递归列**:oss.Provider.List 只列一层(local 把子目录塞进
	// CommonPrefixes),而文件在 res/{uid}/{biz}/{fid}/ 下有好几层 ——
	// 只列一层的话这条断言恒为真,连"根本没被拦住"都能过。
	require.Empty(t, listAllKeys(t, prov, "res/"), "超额被拒时不该有半成品落在存储里")
}

// ====================== 归属 ======================

// 特征:**跨用户返 404 而不是 403**。返 403 等于告诉 A "这个 id 确实存在,
// 只是不是你的",扫一遍 id 就能把别人的文件清单摸出来。
func TestDetail_CrossUserIsNotFound(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)

	v := put(t, svc, mkFileCaller(uidA), "secret.txt", []byte("s3cret"), 6)

	_, err := svc.Detail(context.Background(), mkFileCaller(uidB), v.ID)
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.NotFound), "want NotFound, got %v", err)

	err = svc.Delete(context.Background(), mkFileCaller(uidB), v.ID)
	require.True(t, errCodeIs(err, errcode.NotFound), "删别人的文件也要是 NotFound, got %v", err)

	// 确认 A 的文件还好好地在
	require.Equal(t, int64(6), usedBytes(t, db, uidA))
}

// ====================== 删除 ======================

// 特征:删文件是**四件事**:物理对象 / sys_file 行 / 配额 / 文件夹冗余计数。
// 漏任何一件都不会报错,只会让数据慢慢漂。
func TestDelete_ReleasesEverything(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	v := put(t, svc, caller, "a.txt", []byte("12345"), 5)
	fid := v.FolderID

	require.NoError(t, svc.Delete(context.Background(), caller, v.ID))

	require.Equal(t, int64(0), usedBytes(t, db, uid), "配额要退回去")
	_, err := prov.Stat(context.Background(), v.FileKey)
	require.True(t, oss.IsNotFound(err), "物理对象要真删掉, err=%v", err)

	var row model.SysFile
	// ⚠️ **必须 Unscoped**。Delete 走软删,普通 First 会自动带上
	// `deleted_at IS NULL` 而查不到 —— 表现是"record not found",而这恰恰
	// 是一条测试想验证"行还在、只是被标记了"的用例。
	require.NoError(t, db.Unscoped().First(&row, v.ID).Error)
	require.Equal(t, model.FileStatusDeleted, row.Status, "行是软删不是硬删")
	require.NotNil(t, row.DeletedAt)

	var folder model.SysFolder
	require.NoError(t, db.First(&folder, fid).Error)
	require.Equal(t, 0, folder.FileCount, "文件夹冗余计数要跟着退")
	require.Equal(t, int64(0), folder.TotalSize)
}

// 删掉封面文件时,文件夹上的封面引用必须被摘掉 —— 否则文件夹一直指着一个
// 已经没了的文件,封面位永远空白,而且没有任何报错。
func TestDelete_ClearsFolderCover(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	v := put(t, svc, caller, "cover.jpg", []byte("jpgbytes"), 8)
	fd, err := folders.Detail(context.Background(), caller, v.FolderID)
	require.NoError(t, err)
	_, err = folders.SetCover(context.Background(), caller, fd.ID, v.ID)
	require.NoError(t, err)

	require.NoError(t, svc.Delete(context.Background(), caller, v.ID))

	var folder model.SysFolder
	require.NoError(t, db.First(&folder, v.FolderID).Error)
	require.Equal(t, uint64(0), folder.CoverFileID, "封面文件被删后引用必须清掉")
	require.Empty(t, folder.CoverKey)
}

// ====================== 分片上传 ======================

// 特征:分片合并出来的内容必须**逐字节**等于原始内容,而且配额按合并后的真实
// 大小结算。local 的 Complete 会校验总尺寸,对不上直接丢弃,所以这条同时也在
// 守 provider 的那道闸有没有被绕过去。
func TestMultipart_HappyPath(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newUploadSvc(t, db)
	caller := mkFileCaller(uid)

	full := mpBody()

	init, err := svc.Init(context.Background(), caller, MultipartInitReq{
		Biz: "asset", FileName: "big.bin", FileSize: mpFileSize,
		ContentType: "application/octet-stream", PartSize: mpPartSize,
	})
	require.NoError(t, err)
	require.Equal(t, mpPartSize, init.PartSize, "resp.part_size 是权威值,前端按它切")
	require.Equal(t, mpPartNum, init.PartNum)
	require.Equal(t, mpFileSize, usedBytes(t, db, uid), "Init 就要预扣")

	for i := 1; i <= init.PartNum; i++ {
		_, err := svc.UploadPart(context.Background(), caller, UploadPartReq{
			SessionID: init.SessionID, PartNumber: i,
			Body: full[int64(i-1)*init.PartSize : int64(i)*init.PartSize],
		})
		require.NoError(t, err, "part %d", i)
	}

	v, err := svc.Complete(context.Background(), caller, CompleteReq{SessionID: init.SessionID})
	require.NoError(t, err)
	require.Equal(t, mpFileSize, v.FileSize)
	require.Equal(t, full, mustRead(t, prov, v.FileKey), "合并结果必须逐字节一致")
	require.Equal(t, mpFileSize, usedBytes(t, db, uid))
	require.NotNil(t, v.ID, "Complete 要直接返回 FileView,前端不用再查一次")

	var sess model.SysUploadSession
	require.NoError(t, db.First(&sess, init.SessionID).Error)
	require.Equal(t, model.UploadSessionDone, sess.Status)
	require.Equal(t, int64(0), sess.QuotaHeld, "完成后配额已结清,不能留着再退一次")
}

// 特征:**resp.PartSize 可能被抬高,客户端必须按它切**。
//
// ⚠️ 这条是 M80 里最容易踩的坑,值得单独立一条用例:planParts 会把任何小于
// oss.MinPartSize(100KB)的请求抬到下限,而 local 的分片偏移是按 session 里
// 那个 partSize 算的。客户端要还按自己请求的 256 切,第 2 片就落到
// offset=102400 —— 对一个 1KB 的文件来说这直接越界,报错是
// "写到 offset 102400 会超出总大小 1024",完全指不到真正的原因。
func TestMultipart_PartSizeIsAuthoritative(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newUploadSvc(t, db)

	// 小文件 + 客户端点名要 1KB 分片(低于硬下限)
	const want = int64(1 << 10)
	const fileSize = int64(4 << 10)

	init, err := svc.Init(context.Background(), mkFileCaller(uid), MultipartInitReq{
		Biz: "asset", FileName: "small.bin", FileSize: fileSize, PartSize: want,
	})
	require.NoError(t, err)
	require.Equal(t, oss.MinPartSize, init.PartSize,
		"低于 MinPartSize 的请求要被抬到下限")
	require.Equal(t, 1, init.PartNum, "抬到下限后,这个文件只有一片")

	// 按 resp.PartSize 切 → 恰好一片,能传上去
	_, err = svc.UploadPart(context.Background(), mkFileCaller(uid), UploadPartReq{
		SessionID: init.SessionID, PartNumber: 1, Body: make([]byte, fileSize),
	})
	require.NoError(t, err, "按 resp.part_size 切就能过")
}

// 特征:**过期就地收尾**,不是返个错等清理任务慢慢扫。过期会话的配额是实打实
// 占着用户空间的,而清理任务几十分钟才跑一轮。
func TestMultipart_ExpiredSessionIsClosedImmediately(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newUploadSvc(t, db)
	caller := mkFileCaller(uid)

	init, err := svc.Init(context.Background(), caller, MultipartInitReq{
		Biz: "asset", FileName: "big.bin", FileSize: mpFileSize, PartSize: mpPartSize,
	})
	require.NoError(t, err)
	require.Equal(t, mpFileSize, usedBytes(t, db, uid))

	// 把时间源拨到未来 25h(> session_ttl 默认 24h)
	orig := nowFn
	nowFn = func() time.Time { return time.Now().Add(25 * time.Hour) }
	defer func() { nowFn = orig }()

	_, err = svc.UploadPart(context.Background(), caller, UploadPartReq{
		SessionID: init.SessionID, PartNumber: 1, Body: make([]byte, mpPartSize),
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.Conflict), "want Conflict, got %v", err)

	var sess model.SysUploadSession
	require.NoError(t, db.First(&sess, init.SessionID).Error)
	require.NotEqual(t, model.UploadSessionActive, sess.Status, "过期会话要当场终结")
	require.Equal(t, int64(0), usedBytes(t, db, uid), "过期的预扣要退回去")
}

// 特征:分片不齐就 complete → 会话作废 + 预扣退回 + 半成品清掉。
// 少传一片却合并成功,是最难查的一类上传 bug:用户拿到一个缺口的文件,
// 而且直到某天打开它才发现。
func TestMultipart_CompleteWithMissingPartAbandons(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newUploadSvc(t, db)
	caller := mkFileCaller(uid)

	full := mpBody()
	init, err := svc.Init(context.Background(), caller, MultipartInitReq{
		Biz: "asset", FileName: "big.bin", FileSize: mpFileSize, PartSize: mpPartSize,
	})
	require.NoError(t, err)
	// 只传第 1 片,第 2 片"网络断了"
	_, err = svc.UploadPart(context.Background(), caller, UploadPartReq{
		SessionID: init.SessionID, PartNumber: 1, Body: full[:mpPartSize],
	})
	require.NoError(t, err)

	_, err = svc.Complete(context.Background(), caller, CompleteReq{SessionID: init.SessionID})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.Conflict), "want Conflict, got %v", err)

	require.Equal(t, int64(0), usedBytes(t, db, uid), "作废的会话不能白占配额")
	_, serr := prov.Stat(context.Background(), init.Key)
	require.True(t, oss.IsNotFound(serr), "不该留下半成品文件, err=%v", serr)

	var sess model.SysUploadSession
	require.NoError(t, db.First(&sess, init.SessionID).Error)
	require.NotEqual(t, model.UploadSessionActive, sess.Status)
}

// 特征:分片大小**不能超约定**。partSize 是"一个分片应该多大"的约定,
// 客户端可以声明 128KB 却 POST 1MB 上来 —— 不拦就是无鉴权的放大器。
func TestMultipart_OversizePartRejected(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newUploadSvc(t, db)
	caller := mkFileCaller(uid)

	init, err := svc.Init(context.Background(), caller, MultipartInitReq{
		Biz: "asset", FileName: "big.bin", FileSize: mpFileSize, PartSize: mpPartSize,
	})
	require.NoError(t, err)

	_, err = svc.UploadPart(context.Background(), caller, UploadPartReq{
		SessionID: init.SessionID, PartNumber: 1, Body: make([]byte, mpPartSize+1),
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.PayloadTooLarge), "want PayloadTooLarge, got %v", err)
}

// 特征:空分片要被拒。0 字节的分片会让"分片数对得上、总字节对不上"变成
// 另一种形态的缺口文件,而它的排查难度比"少传一片"高一个量级。
func TestMultipart_EmptyPartRejected(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newUploadSvc(t, db)
	caller := mkFileCaller(uid)

	init, err := svc.Init(context.Background(), caller, MultipartInitReq{
		Biz: "asset", FileName: "big.bin", FileSize: mpFileSize, PartSize: mpPartSize,
	})
	require.NoError(t, err)

	_, err = svc.UploadPart(context.Background(), caller, UploadPartReq{
		SessionID: init.SessionID, PartNumber: 1, Body: []byte{},
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.ParamMissing), "want ParamMissing, got %v", err)
}

// 特征:跨用户不能操作别人的上传会话 —— 续传、查片、合并、取消都不行。
func TestMultipart_CrossUserSessionIsNotFound(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	svc, _ := newUploadSvc(t, db)

	init, err := svc.Init(context.Background(), mkFileCaller(uidA), MultipartInitReq{
		Biz: "asset", FileName: "big.bin", FileSize: mpFileSize, PartSize: mpPartSize,
	})
	require.NoError(t, err)

	b := mkFileCaller(uidB)
	_, err = svc.UploadPart(context.Background(), b, UploadPartReq{
		SessionID: init.SessionID, PartNumber: 1, Body: make([]byte, mpPartSize),
	})
	require.True(t, errCodeIs(err, errcode.NotFound), "UploadPart want NotFound, got %v", err)

	_, err = svc.Parts(context.Background(), b, init.SessionID)
	require.True(t, errCodeIs(err, errcode.NotFound), "Parts want NotFound, got %v", err)

	_, err = svc.Complete(context.Background(), b, CompleteReq{SessionID: init.SessionID})
	require.True(t, errCodeIs(err, errcode.NotFound), "Complete want NotFound, got %v", err)

	err = svc.Cancel(context.Background(), b, init.SessionID)
	require.True(t, errCodeIs(err, errcode.NotFound), "Cancel want NotFound, got %v", err)

	// A 的会话还活着,配额还占着 —— B 的越权尝试不该有任何副作用
	require.Equal(t, mpFileSize, usedBytes(t, db, uidA))
	require.Equal(t, int64(0), usedBytes(t, db, uidB))
}

// ====================== 直传凭证 ======================

// 特征:key **完全由服务端生成**。这是直传唯一的越权防线 —— 前端自己拼 key
// 就等于允许写别人目录。
//
// ⚠️ 用例里的文件名是**真的恶心**:`..\..\..\etc\passwd` + 控制字符。
// 早先这里断言的是"空格被换成安全字符",那是断言错了 —— SanitizeObjectName
// **刻意保留空格和中文**(只有 < > : " | ? * 和控制字符会被换),因为
// "户型图 v2.pdf" 是绝大多数真实文件名,把它变成 `__.pdf` 是纯粹的伤害。
// 尖括号和路径分隔符才是真正要拦的东西。
func TestUploadToken_KeyIsServerOwned(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	tok, err := svc.UploadToken(context.Background(), caller, FileUploadTokenReq{
		Biz: "asset", FileName: "..\\..\\..\\etc\\passwd\x00\x1b[31m", FileSize: 1024,
	})
	require.NoError(t, err)
	require.Equal(t, oss.UploadModeProxy, tok.Mode, "local provider 必须是 proxy 模式")
	require.Contains(t, tok.Key, oss.DefaultRoot+"/")
	require.Contains(t, tok.Key, "asset", "key 里要有业务段")
	require.Contains(t, tok.Key, itoa(uid), "key 里要有用户 id")
	require.True(t, strings.HasSuffix(tok.Key, "passwd__[31m"),
		"路径分隔符要被剥掉、控制字符换成 _,只剩末段文件名, got %q", tok.Key)
	require.NotContains(t, tok.Key, "..", "不能留下任何 ..,否则就是一次目录穿越")
	require.True(t, oss.SafeKey(tok.Key), "生成的 key 必须过 SafeKey")
}

// 特征:**中文和空格保留**。SanitizeObjectName 只动控制字符和 < > : " | ? *,
// 名字长什么样就存什么样 —— 把 "户型图 v2.pdf" 存成 "____v2.pdf" 对用户
// 毫无帮助,而直链读取侧本来就按段转义,空格不构成风险。
func TestUploadToken_KeepsChineseAndSpaces(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)

	tok, err := svc.UploadToken(context.Background(), mkFileCaller(uid), FileUploadTokenReq{
		Biz: "asset", FileName: "户型图 v2.pdf", FileSize: 1024,
	})
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(tok.Key, "户型图 v2.pdf"),
		"中文和空格要原样保留, got %q", tok.Key)
	require.True(t, oss.SafeKey(tok.Key))
}

// 特征:key **恒为 5 段**(root/uid/biz/folder/name),没有"公开段"这回事。
//
// ⚠️ 以前有 `res/public/{uid}/...` 这一形态,于是"文件公开还是私有"是靠
// **key 的形状**表达的 —— 一个配置疏漏就把文件放错段,而且没有任何报错。
// 现在只有一种形状,这类错误从根上不存在了。
func TestUploadToken_KeyHasFiveSegments(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)

	tok, err := svc.UploadToken(context.Background(), mkFileCaller(uid), FileUploadTokenReq{
		Biz: "asset", FileName: "a.jpg", FileSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, strings.Split(tok.Key, "/"), 5,
		"key 必须恒为 5 段(root/uid/biz/folder/name), got %q", tok.Key)
	require.NotContains(t, tok.Key, "/public/",
		"没有公开段 —— 存储侧只有一棵树")
}

// 特征:**直传登记不能把别人的 key 登记成自己的文件**。
//
// ⚠️ 这是整个隔离边界里最容易漏的一条(设计文档 §3.1.1 第 3 道)。key 是
// 客户端上次 `UploadToken` 拿到的,它完全可能指向别人目录下 —— 只做
// `SafeKey` 语法校验(它只管路径穿越,不管归属)是不够的。必须从 key 里把
// userId 解析出来跟当前登录人比。
//
// 少了这道闸,拿到别人 key 的人虽然写不进去(对象是 A 直传的),却能把它
// 登记到自己名下:占自己的配额、进自己的列表,表现是一份凭空出现的文件。
func TestRegister_CrossUserKeyIsNotFound(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	svc, prov := newFileSvc(t, db)
	opt := DefaultFileOptions()

	// 造一个**真的躺在 B 名下**的对象(key 由服务端规则拼,模拟 B 已经直传完)
	fB, err := NewFolderService(db, prov, opt).Create(context.Background(), mkFileCaller(uidB),
		FolderCreateReq{Biz: "asset", Name: "B的文件夹"})
	require.NoError(t, err)
	prefix, err := oss.FolderPrefix(opt.Root, uidB, "asset", fB.ID)
	require.NoError(t, err)
	stealKey := prefix + "1-aaaaaaaa-b.jpg"
	body := []byte("b's private object")
	_, err = prov.Put(context.Background(), stealKey, bytes.NewReader(body), "image/jpeg")
	require.NoError(t, err)

	// A 拿着这个 key 来登记
	_, err = svc.Register(context.Background(), mkFileCaller(uidA), FileRegisterReq{
		Key: stealKey, FileName: "b.jpg", ContentType: "image/jpeg",
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.NotFound), "want NotFound, got %v", err)

	// A 一行都没建,B 的配额也不能被动过
	var n int64
	require.NoError(t, db.Model(&model.SysFile{}).Count(&n).Error)
	require.Equal(t, int64(0), n)
	require.Equal(t, int64(0), usedBytes(t, db, uidA))
	require.Equal(t, int64(0), usedBytes(t, db, uidB))

	// 同一个 key,B 自己登记得进去 —— 证明上面拒的不是"key 非法"
	got, err := svc.Register(context.Background(), mkFileCaller(uidB), FileRegisterReq{
		Key: stealKey, FileName: "b.jpg", ContentType: "image/jpeg",
	})
	require.NoError(t, err, "本人登记同一个 key 必须成功,否则说明拒的其实是 key 本身")
	require.Equal(t, int64(len(body)), got.FileSize, "大小从存储取,不是从请求取")
	require.Equal(t, int64(len(body)), usedBytes(t, db, uidB))
}

// 特征:同一个 key 重复登记**不重复记账**。直传接口超时重试很常见,少了这一判
// 就会"重试一次多扣一份配额",而用户文件一个都没多。
func TestRegister_IdempotentNoDoubleCharge(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newFileSvc(t, db)
	opt := DefaultFileOptions()

	f, err := NewFolderService(db, prov, opt).Create(
		context.Background(), mkFileCaller(uid), FolderCreateReq{Biz: "asset", Name: "f"})
	require.NoError(t, err)
	prefix, err := oss.FolderPrefix(opt.Root, uid, "asset", f.ID)
	require.NoError(t, err)
	key := prefix + "1-bbbbbbbb-c.png"
	prov2 := prov
	_, err = prov2.Put(context.Background(), key, bytes.NewReader([]byte("png-bytes")), "image/png")
	require.NoError(t, err)

	v, err := svc.Register(context.Background(), mkFileCaller(uid), FileRegisterReq{
		Key: key, FileName: "c.png", ContentType: "image/png",
	})
	require.NoError(t, err)
	first := usedBytes(t, db, uid)
	require.Equal(t, int64(len("png-bytes")), first, "直传登记必须真的扣配额")

	_, err = svc.Register(context.Background(), mkFileCaller(uid), FileRegisterReq{
		Key: key, FileName: "c.png", ContentType: "image/png",
	})
	require.NoError(t, err, "重复登记要成功,只是不重复记账")
	require.Equal(t, first, usedBytes(t, db, uid), "重试不能多扣配额")
	require.NotZero(t, v.ID)
}

// 特征:**直传登记必须扣配额,而且扣不动就拒**。
//
// ⚠️ 这条是 M80 实施时踩到的真 P0。`commitFile` 的 held 语义是"之前已经预扣了
// 多少",`settleQuota` 算 `delta = held - actual`。而直传这条路**从来没预扣过**
// (UploadToken 拿不到真实大小),早先这里传的是 `meta.Size` —— 等于告诉
// settleQuota"你已经预扣了 18 字节",净变化 0。
//
// 症状是**每一个直传文件都完全免费**:文件在、列表里有、字节在存储里,
// 只有配额纹丝不动。用户可以绕开上限无限传,而且没有任何报错。
// 这正是 §4.5 要防的那件事。
func TestRegister_ChargesQuotaAndRejectsOverQuota(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 50) // 只给 50 字节
	svc, prov := newFileSvc(t, db)
	opt := DefaultFileOptions()

	f, err := NewFolderService(db, prov, opt).Create(
		context.Background(), mkFileCaller(uid), FolderCreateReq{Biz: "asset", Name: "f"})
	require.NoError(t, err)
	prefix, err := oss.FolderPrefix(opt.Root, uid, "asset", f.ID)
	require.NoError(t, err)

	// 放一个 200 字节的对象进去(客户端直传完成的形态)
	bigKey := prefix + "1-cccccccc-big.bin"
	big := bytes.Repeat([]byte("x"), 200)
	_, err = prov.Put(context.Background(), bigKey, bytes.NewReader(big), "application/octet-stream")
	require.NoError(t, err)

	_, err = svc.Register(context.Background(), mkFileCaller(uid), FileRegisterReq{
		Key: bigKey, FileName: "big.bin", ContentType: "application/octet-stream",
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam(空间不足), got %v", err)
	require.Equal(t, int64(0), usedBytes(t, db, uid), "被拒的不占配额")

	var n int64
	require.NoError(t, db.Model(&model.SysFile{}).Count(&n).Error)
	require.Equal(t, int64(0), n, "被拒的不建行")

	// ⚠️ 对象已经直传上去了,登记被拒就必须在**存储侧**删掉 —— 留着就是一份
	// 没人认领、也没人计费的孤儿对象,按量计费会一直烧钱。
	_, serr := prov.Stat(context.Background(), bigKey)
	require.True(t, oss.IsNotFound(serr), "被拒的直传对象必须被回滚, err=%v", serr)

	// 再放一个刚好塞得下的 → 能登记,配额扣满
	okKey := prefix + "1-dddddddd-ok.bin"
	small := bytes.Repeat([]byte("y"), 40)
	_, err = prov.Put(context.Background(), okKey, bytes.NewReader(small), "application/octet-stream")
	require.NoError(t, err)
	got, err := svc.Register(context.Background(), mkFileCaller(uid), FileRegisterReq{
		Key: okKey, FileName: "ok.bin", ContentType: "application/octet-stream",
	})
	require.NoError(t, err)
	require.Equal(t, int64(40), got.FileSize)
	require.Equal(t, int64(40), usedBytes(t, db, uid), "配额按存储返回的实际大小扣")
}

// ====================== 工具 ======================

func mustProv(t *testing.T) *local.Provider {
	t.Helper()
	p, err := local.New(local.Config{Root: t.TempDir(), URLBase: "/res"})
	require.NoError(t, err)
	return p
}

func mustRead(t *testing.T, p *local.Provider, key string) []byte {
	t.Helper()
	f, _, err := p.Open(context.Background(), key)
	require.NoError(t, err)
	defer f.Close()
	b, err := io.ReadAll(f)
	require.NoError(t, err)
	return b
}

func mustReadSeek(t *testing.T, rc io.ReadSeekCloser) []byte {
	t.Helper()
	b, err := io.ReadAll(rc)
	require.NoError(t, err)
	return b
}
