// M80 收尾 —— 复审发现的缺陷的回归测试
//
// 这一组守的五件事,共同点是**错了都不报错**:
//   - 配额泄漏:用户配额越来越少,文件一个没多
//   - 对象残留:文件夹删了,存储用量一点没降
//   - status 过滤自相矛盾:查历史恒返回空,而调用方以为自己写错了参数
//   - 超长文件名:INSERT 失败 → 对象写进去又被删 → 用户永远传不上去
//   - 错误映射过宽:DB 故障报成"同名文件夹已存在",把人引到错误方向
//
// 脚手架沿用 file_test.go(真实 local provider + 真实内存 sqlite),不 mock:
// 这些 bug 大多藏在"GORM 的真实错误"和"provider 的真实返回值"之间。
package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/oss"
)

// pageFiles 把 PageResponse 的 interface{} 列表拉成 []FileView
func pageFiles(t *testing.T, res *model.PageResponse) []FileView {
	t.Helper()
	raw, ok := res.List.([]FileView)
	require.True(t, ok, "列表元素类型变了: %T", res.List)
	return raw
}

// listAllKeys 递归列出存储里某前缀下的**所有** key。
//
// ⚠️ **oss.Provider.List 本身不递归**(local 只列一层,子目录进 CommonPrefixes;
// OSS 则是靠 delimiter 参数切页的同一语义),所以只调一次 List 断言"存储是空的"
// 是**假绿**:文件都在两层以下的目录里,一次 List 一个都看不到。
func listAllKeys(t *testing.T, p oss.Provider, prefix string) []string {
	t.Helper()
	res, err := p.List(context.Background(), oss.ListOption{Prefix: prefix})
	require.NoError(t, err)
	out := make([]string, 0, len(res.Objects))
	for _, o := range res.Objects {
		out = append(out, o.Key)
	}
	for _, cp := range res.CommonPrefixes {
		out = append(out, listAllKeys(t, p, prefix+cp)...)
	}
	return out
}

func countStoredObjects(t *testing.T, p oss.Provider) int {
	t.Helper()
	// ⚠️ 前缀必须跟着 oss.DefaultRoot 走 —— key 的第一段就是它
	// (M80 单桶单树)。写死 "res/" 的话,默认值一改这里就数到 0 个对象,
	// 而"删完是 0"那条断言会**假绿**(0 == 0)。
	return len(listAllKeys(t, p, oss.DefaultRoot+"/"))
}

// countStoredFileObjects 数**用户文件**对象(不含 info.json)。
//
// ⚠️ **为什么要把 info.json 排除掉** —— info.json 是**每个文件夹的清单**
// (设计文档 §2.3 D3 的生成物),M140 起**建文件夹时就写**。于是"这次上传被拒了"
// 之后,存储里留下的那个对象是**文件夹的清单**,不是"被拒的那个文件"。
// 把它算进去的话,"被拒的上传不该留下对象"那条断言就永远不成立 ——
// 而它真正要守的是"**用户文件**没被留下"(配额 / 计费 / 孤儿对象)。
func countStoredFileObjects(t *testing.T, p oss.Provider) int {
	t.Helper()
	keys := listAllKeys(t, p, oss.DefaultRoot+"/")
	n := 0
	for _, k := range keys {
		if strings.HasSuffix(k, "/"+infoFileName) {
			continue
		}
		n++
	}
	return n
}

// ====================== A1 配额预扣泄漏 ======================

// 特征:**记账失败必须把预扣退回去**。
//
// ⚠️ 这里的失败点是"对象已经写进存储、sys_file 行写不进去"。早先的写法是
// 只删对象不还配额,而 holdQuota 是在事务**外**单独提交的 —— 事务回滚退不掉它。
// 表现是:每次 DB 抖动都凭空吃掉用户一份配额,而用户什么文件都没传上去。
//
// 失败怎么造:把 sys_file 表删掉。Put 走的是真 local provider(成功),
// commitFile 里的 Create 立刻报 "no such table" —— 和真实故障同一条分支。
func TestReceive_CommitFailureRollsBackHeldQuota(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	body := []byte("payload that will never be registered")
	require.NoError(t, db.Migrator().DropTable(&model.SysFile{}))

	_, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: "orphan.txt", ContentType: "text/plain",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.Internal), "want Internal, got %v", err)

	require.Equal(t, int64(0), usedBytes(t, db, uid),
		"预扣在事务外,记账失败后没人再动它 —— 必须显式还回去")
	require.Equal(t, 0, countStoredObjects(t, prov),
		"对象已成孤儿,必须删掉,否则在存储里堆到账单爆炸")
}

// 对照组:同一条路径,**成功**时预扣要留得住。
//
// ⚠️ 没有这条的话,上面那条最简单的"过度修复"就是把所有 held 一律退回去 ——
// 那会让每个文件传完都变成"配额占用 0",配额形同虚设。
func TestReceive_CommitSuccessKeepsHeldQuota(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	body := []byte("registered fine")
	v, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: "ok.txt", ContentType: "text/plain",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.NoError(t, err)
	require.Equal(t, int64(len(body)), usedBytes(t, db, uid),
		"成功路径不能退配额,否则配额闸形同虚设")
	require.Equal(t, int64(len(body)), v.FileSize)
}

// ====================== A2 删文件夹漏删存储对象 ======================

// 特征:**删文件夹要删掉它在存储侧占用的全部前缀**。
//
// ⚠️ 早先的 key 有"私有/公开"两种形态,只删了私有的那一条,于是另一半对象
// 永远留在存储里,而它的行已经被软删 —— 清理任务按行驱动,行没了它就再也
// 看不见,OSS 却一直按量收这个对象的钱。
//
// 现在存储侧只有一棵树、前缀只有一条,所以"删全"= "删那一条"。这条用例守的
// 是"别再漏",而漏的形态已经变了 —— 不再是"漏了另一条前缀",而是"前缀算错
// 导致一个都没删"。
func TestFolderDelete_RemovesStoredObjects(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newFileSvc(t, db)
	folders := NewFolderService(db, prov, optOf(t))
	caller := mkFileCaller(uid)

	// ⚠️ 必须建一个**非默认**文件夹:懒创建出来的"默认文件夹"按设计不可删
	// (前端菜单和上传按钮都按它定位),拿它来测会一路撞在 Forbidden 上,
	// 根本走不到 DeletePrefix。
	fd, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "房源图", Biz: "asset"})
	require.NoError(t, err)
	require.False(t, fd.IsDefault)

	// 同一个文件夹里的两个文件
	body := []byte("bytes!")
	first, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		FolderID: fd.ID, Biz: "asset", FileName: "p.txt", Size: int64(len(body)),
		Body: bytes.NewReader(body),
	})
	require.NoError(t, err)
	second, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		FolderID: fd.ID, Biz: "asset", FileName: "q.txt", Size: int64(len(body)),
		Body: bytes.NewReader(body),
	})
	require.NoError(t, err)
	require.Equal(t, first.FolderID, second.FolderID, "两个文件要落在同一个文件夹")
	// ⚠️ **M133:对象数从 2 变成 3** —— 上传会重建 info.json(设计文档
	//   §2.3 D3 的"生成时机"),所以这个文件夹在存储侧占**三个**对象:
	//   两个文件 + 一份清单。
	//
	//   这条用例守的是"删文件夹 = 删掉它在存储侧占用的**整段前缀**",
	//   而 info.json 就在那段前缀里 —— 少删它,清单就成了指向空气的孤儿
	//   (行已经软删,OSS 还在按量计费,而且没人看得见)。
	//
	//   前置写 3(而不是"至少 2")是故意的:它顺带证明**清单真的生成了**,
	//   否则后面那句"删完是 0"就可能只是因为清单压根没写过。
	require.Equal(t, 3, countStoredObjects(t, prov),
		"前置:两个文件 + 一份 info.json 清单都在存储里")

	require.NoError(t, folders.Delete(context.Background(), caller, fd.ID))

	require.Equal(t, 0, countStoredObjects(t, prov),
		"对象必须被删 —— 否则它是存储里删不掉的孤儿(info.json 也在内)")
	require.Equal(t, int64(0), usedBytes(t, db, uid), "配额要全退")
}

// optOf 造一份测试用的 FileOptions(和 newFileTestProv 保持一致)
func optOf(t *testing.T) FileOptions {
	t.Helper()
	opt := DefaultFileOptions()
	opt.Root = oss.DefaultRoot
	return opt
}

// ====================== A3 status 过滤 ======================

// 特征:**默认只列正常态,但显式传 status 要能真的筛出历史**。
//
// ⚠️ 早先的写法是无条件 `status = 正常` 之后再来一个 `if q.Status > 0` 追加
// `status = q.Status`,两个条件 AND 在一起恒空 —— 注释写着"要看历史要显式传
// status",而传了必然返回空列表,且 HTTP 200。
func TestList_StatusFilterReplacesDefault(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	keep := put(t, svc, caller, "keep.txt", []byte("keep"), 4)
	gone := put(t, svc, caller, "gone.txt", []byte("gone"), 4)
	require.NoError(t, svc.Delete(context.Background(), caller, gone.ID))

	// 默认:只有活着的
	res, err := svc.List(context.Background(), caller, FileListQuery{})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Total)
	require.Equal(t, keep.ID, pageFiles(t, res)[0].ID)

	// 显式要历史:必须拿得到那条软删的
	res, err = svc.List(context.Background(), caller, FileListQuery{Status: int(model.FileStatusDeleted)})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Total,
		"传 status=已删 恒返回空 —— 查询被写成了 status=正常 AND status=已删")
	require.Equal(t, gone.ID, pageFiles(t, res)[0].ID)

	// 显式传 0(正常态的值)仍然等价于默认,不能变成"不过滤"
	res, err = svc.List(context.Background(), caller, FileListQuery{Status: 0})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Total)
	require.Equal(t, keep.ID, pageFiles(t, res)[0].ID)
}

// ====================== A5 文件名长度 ======================

// 特征:**原名超长要在落库前就被拒**,而不是等 MySQL 报"Data too long"。
//
// ⚠️ 关键在"带路径前缀的长名":oss.SanitizeObjectName 只限**清洗后**的对象名,
// 而 `dir/…/name.pdf` 清洗后只剩 `name.pdf` 能过闸,而 sys_file.file_name 存的
// 是**原名**。于是这种名字能一路走到 INSERT,在严格模式下失败 → 刚写进存储的
// 对象又被回滚删掉 → 用户永远传不上去,且每次报的都是"登记文件失败"。
func TestReceive_RejectsOverlongFileName(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	// 清洗后只剩 5 字节,原名 300 字节 —— 正好卡在 sanitizer 管不到的那一段
	long := strings.Repeat("a", 295) + "/b.txt"
	body := []byte("x")
	_, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: long, Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)
	require.Contains(t, err.Error(), "文件名过长")
	require.Equal(t, 0, countStoredFileObjects(t, prov), "被拒的上传不该留下对象")
	require.Equal(t, int64(0), usedBytes(t, db, uid), "被拒的上传不该扣配额")

	// 边界:原名正好 255 字节要能过(列是 varchar(255),不是 254)。
	// ⚠️ **必须带路径前缀**,否则过不了的是 oss 那道 240 字节的对象名闸,
	// 根本轮不到 checkFileName —— 那条闸更严,普通名字的实际上限是 240。
	okName := strings.Repeat("n", maxFileNameBytes-6) + "/b.txt"
	require.Len(t, okName, maxFileNameBytes)
	_, err = svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: okName, Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.NoError(t, err, "255 字节是列的上限,不能误杀")

	// 256 字节要拒
	_, err = svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: okName + "x", Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)
	require.Contains(t, err.Error(), "文件名过长")
}

// 直传登记这条路**不走 buildFileKey**(key 是客户端上次拿到的),所以长度闸要
// 单独在 Register 里调一次 —— 少了它就是一个"超长名 → 登记失败 → 反复重试"的
// 死循环,而对象一直躺在存储里。
func TestRegister_RejectsOverlongFileName(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, prov := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	fv := put(t, svc, caller, "seed.txt", []byte("seed"), 4)
	folderID := fv.FolderID
	require.NoError(t, svc.Delete(context.Background(), caller, fv.ID))

	_, err := svc.Register(context.Background(), caller, FileRegisterReq{
		Key:      folderObjKey(t, prov, caller, folderID),
		FileName: strings.Repeat("x", 300) + ".txt",
	})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)
	require.Contains(t, err.Error(), "文件名过长")
}

// folderObjKey 造一个"确实存在于存储里"的 key,用来测 Register 的前置校验
func folderObjKey(t *testing.T, p oss.Provider, caller *Caller, folderID uint64) string {
	t.Helper()
	k, err := buildFileKey(optOf(t), caller.UserID, "asset", folderID, "direct.bin")
	require.NoError(t, err)
	_, putErr := p.Put(context.Background(), k, strings.NewReader("direct bytes"), "application/octet-stream")
	require.NoError(t, putErr)
	return k
}

// ====================== A6 创建文件夹的错误映射 ======================

// 特征:**只有"撞唯一索引"才叫同名文件夹已存在**。
//
// ⚠️ 早先这里是所有 Create 错误一律返 Conflict,而且把原错误整个吞掉。DB 抖动 /
// 连接断 / 表不存在统统报成"同名文件夹已存在",排障的人会被引到完全错误的方向,
// 日志里也没有任何原始信息。
func TestFolderCreate_DuplicateIsConflict(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	_, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "户型图"})
	require.NoError(t, err)

	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "户型图"})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.Conflict), "want Conflict, got %v", err)
}

func TestFolderCreate_NonDuplicateErrorIsNotConflict(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())

	require.NoError(t, db.Migrator().DropTable(&model.SysFolder{}))

	_, err := folders.Create(context.Background(), mkFileCaller(uid), FolderCreateReq{Name: "随便"})
	require.Error(t, err)
	require.False(t, errCodeIs(err, errcode.Conflict),
		"建表失败不是「同名文件夹已存在」—— 这会把排障引到完全错误的方向")
	require.True(t, errCodeIs(err, errcode.Internal), "want Internal, got %v", err)
}

// ====================== B2 allowed_biz 白名单 ======================

// 特征:**file.allowed_biz 配了就要真的生效**。
//
// ⚠️ 这个配置项的注释写着"填了就当白名单用",而映射那一步之前是漏的 ——
// 配了不生效,启动日志一个字都不提。biz 是 key 的第二段,不限的话任何登录用户
// 都能建出一整棵新的目录树来,而删除生命周期规则按前缀覆盖,根本盖不到它。
func TestAllowedBiz_WhitelistIsEnforced(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)

	opt := optOf(t)
	opt.AllowedBiz = []string{"house", "material"}
	folders := NewFolderService(db, mustProv(t), opt).(*folderService)
	caller := mkFileCaller(uid)

	// 白名单里的:正常建
	for _, biz := range []string{"house", "material"} {
		_, err := folders.resolveBiz(biz)
		require.NoErrorf(t, err, "biz=%q 应在白名单里", biz)
	}
	// 白名单外的:拒
	_, err := folders.resolveBiz("warehouse")
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)

	// 建文件夹这条路也必须守(它不走 resolveBiz,自己调 checkBiz)
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "x", Biz: "warehouse"})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "Create 也必须守白名单, got %v", err)
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "x", Biz: "house"})
	require.NoError(t, err)
}

// 对照组:白名单留空 = 不限,只做语法校验。这保证这道闸默认关闭,不是行为变更。
func TestAllowedBiz_EmptyMeansUnrestricted(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), optOf(t)).(*folderService)

	_, err := folders.resolveBiz("anything_goes")
	require.NoError(t, err, "留空 = 不限,只做语法校验")

	// 但语法仍然要拦
	_, err = folders.resolveBiz("Bad Biz")
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "语法错仍要拒, got %v", err)
	require.NotZero(t, uid)
}

// ====================== isDuplicateErr ======================

func TestIsDuplicateErr(t *testing.T) {
	require.False(t, isDuplicateErr(nil))
	require.False(t, isDuplicateErr(errcode.New(errcode.Internal, "普通故障")))
	require.True(t, isDuplicateErr(errors.New("Error 1062: Duplicate entry 'a-b' for key 'uk'")))
	require.True(t, isDuplicateErr(errors.New("UNIQUE constraint failed: sys_folder.user_id, sys_folder.biz")))
	require.True(t, isDuplicateErr(errors.New(`ERROR: duplicate key value violates unique constraint "uk_x"`)))
}

// ====================== 分片 Complete 的孤儿对象 ======================

// 特征:**合并成功但记账失败时,对象必须被删掉**。
//
// ⚠️ 这一条和 Receive 那条(A1)是同一种形状,只是隔了一次合并:
// Complete 里 `s.mp.CompleteMultipartUpload` 成功之后字节就在存储里了,
// 而 `commitFile` 可能失败(DB 抖动、唯一索引冲突)。早先这里只 `return nil, err`,
// 于是留下一个**没人认领也没人计费**的对象:sys_file 里没有这一行,清理任务按行
// 驱动看不见它;用户看到"上传失败",重传拿到的是**另一个 key**(对象名带秒级
// 时间戳 + 随机段),于是每失败一次就在存储里多留一份。
//
// 失败怎么造:传完所有分片之后把 sys_file 表删掉。合并走的是真实 local
// provider(成功),`commitFile` 里的 Create 立刻报 "no such table"。
func TestMultipart_CompleteCommitFailureRemovesObject(t *testing.T) {
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
	for i := 1; i <= init.PartNum; i++ {
		_, err := svc.UploadPart(context.Background(), caller, UploadPartReq{
			SessionID: init.SessionID, PartNumber: i,
			Body: full[int64(i-1)*init.PartSize : int64(i)*init.PartSize],
		})
		require.NoError(t, err, "part %d", i)
	}
	require.Equal(t, mpFileSize, usedBytes(t, db, uid), "前置:Init 已预扣")

	require.NoError(t, db.Migrator().DropTable(&model.SysFile{}))

	_, err = svc.Complete(context.Background(), caller, CompleteReq{SessionID: init.SessionID})
	require.Error(t, err)
	require.Equal(t, 0, countStoredFileObjects(t, prov),
		"合并成功但记账失败 —— 对象已成孤儿,必须删掉")
	require.Equal(t, int64(0), usedBytes(t, db, uid),
		"预扣必须退回去(走 abandonSession → closeSession)")

	// 会话也不能留在 active:留着的话清理任务会一直等一个"还可能完成"的会话
	var sess model.SysUploadSession
	require.NoError(t, db.First(&sess, init.SessionID).Error)
	require.NotEqual(t, model.UploadSessionActive, sess.Status,
		"这次上传已经废了,会话必须终结,否则预扣要占满一个 SessionTTL")
	require.Equal(t, int64(0), sess.QuotaHeld, "退完必须归零,否则清理任务会再退一次")
}
