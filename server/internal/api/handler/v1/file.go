// 文件 / 文件夹 / 分片上传 handler
// 设计见 docs/文件系统模块设计方案V1.0.md §8
//
// ⚠️ **本文件顶掉的是 M76 之前那个 upload.go**(只发占位 OSS 凭证的那个)。
// 两条老路由 `POST /biz/uploads/token`、`GET /biz/uploads/url` 已经退役,
// 对应能力搬到了 `/files/upload-token` 和 `/files/:id/url`。
package v1

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/service"
)

// FileHandler 文件 + 文件夹
type FileHandler struct{ *handler.BaseHandler }

func NewFileHandler(r *service.Registry) *FileHandler {
	return &FileHandler{BaseHandler: handler.New(r)}
}

func (h *FileHandler) rc(c *app.RequestContext) *service.Caller {
	return h.BaseHandler.RequireCaller(c)
}

// ===== 直传凭证 =====

// UploadToken 取直传凭证
func (h *FileHandler) UploadToken(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	var body service.FileUploadTokenReq
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	tok, err := h.Services.File.UploadToken(ctx, caller, body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, tok)
}

// ===== 中转上传 / 直传登记 =====

// Upload `POST /files` —— **按 Content-Type 分流**,不是按路径。
//
// multipart/form-data → 服务端中转(Receive)
// 其它(含 application/json)→ 直传后登记(Register)
//
// ⚠️ 一条路由两种语义是有意的(设计文档 §8.1):两边的登记字段一模一样,
// 拆成两个端点的话前端要为"OSS 直传"和"本地中转"各写一套登记逻辑。
func (h *FileHandler) Upload(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	if isMultipart(c) {
		h.receive(ctx, c, caller)
		return
	}
	var body service.FileRegisterReq
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.File.Register(ctx, caller, body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

func (h *FileHandler) receive(ctx context.Context, c *app.RequestContext, caller *service.Caller) {
	fh, err := c.FormFile("file")
	if err != nil {
		h.FailCode(c, errcode.ParamMissing, "缺少文件字段 file")
		return
	}
	f, err := fh.Open()
	if err != nil {
		h.FailCode(c, errcode.BadRequest, "读取上传内容失败")
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		h.FailCode(c, errcode.BadRequest, "读取上传内容失败")
		return
	}
	v, err := h.Services.File.Receive(ctx, caller, service.FileReceiveReq{
		FolderID:    h.formUint(c, "folder_id"),
		Biz:         c.PostForm("biz"),
		FileName:    firstNonEmpty(c.PostForm("file_name"), fh.Filename),
		ContentType: c.PostForm("content_type"),
		// ⚠️ 申报大小传 `fh.Size`(multipart 解析出的这一段字节数)而不是
		// `len(body)`。两者正常相等,但 fh.Size 是"解析器认为这一段有多大",
		// 拿它去预扣、拿 provider 返回的 meta.Size 去结算,口径才一致。
		Size: fh.Size,
		Body: bytes.NewReader(body),
	})
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// ===== 列表 / 详情 / URL / 删除 / 配额 =====

func (h *FileHandler) List(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	page, size := h.GetPage(c)
	list, err := h.Services.File.List(ctx, caller, service.FileListQuery{
		FolderID: h.MustUint(c, "folder_id", 0),
		Biz:      c.Query("biz"),
		Status:   int(h.MustUint(c, "status", 0)),
		Keyword:  c.Query("keyword"),
		Page:     page,
		Size:     size,
	})
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, list)
}

func (h *FileHandler) Detail(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.File.Detail(ctx, caller, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// URL 取访问地址。ttl 单位分钟,0/缺省 → service 层用配置默认
func (h *FileHandler) URL(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	ttl := time.Duration(h.MustUint(c, "ttl", 0)) * time.Minute
	u, err := h.Services.File.URL(ctx, caller, id, ttl)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, map[string]any{"url": u})
}

func (h *FileHandler) Delete(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.File.Delete(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Content `GET /files/:id/content` —— 服务端中转读字节。
//
// ⚠️ **它是兜底,不是主路径** —— 直链(静态挂载 / OSS 公共读直链)是主路径。
// 留着它是因为 provider 不实现 oss.ContentProvider 时(OSS 侧刻意不实现,
// 中转会把大文件带宽拉回应用服务器),前端还有一条能拿到字节的路。
//
// ⚠️ 它必须**进鉴权链**(挂在 biz 组里),和静态挂载是两条完全不同的路:
// 静态挂载没有中间件,靠 key 的不可猜性保护;这里是逐请求查归属。
func (h *FileHandler) Content(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	rc, f, err := h.Services.File.Content(ctx, caller, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	// ⚠️ **这里没有 defer rc.Close()**,而且是有意的:SetBodyStream 在写完
	// 响应后会自己关掉实现了 io.Closer 的流,handler 再关一次就是对同一个
	// 句柄关两遍。local 返的是 *os.File,第二次 Close 只丢一个 error;换
	// provider 就未必无害。
	//
	// 配套约束:**SetBodyStream 之后不再有任何 return 分支** —— 流的所有权
	// 在那一刻就交出去了。所以下面所有判断都放在它前面做完;真要提前退出
	// (416、Seek 失败),必须自己 rc.Close(),别指望 defer。

	// Content-Type 取**库里存的那个**(服务端按扩展名探测的值),不是浏览器
	// 上报的 —— 上报 text/html 就能让本站替它执行脚本。探测不出就按
	// octet-stream 走,让浏览器闭嘴。
	ct := f.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	disposition := "inline"
	if !isInlineType(ct) {
		disposition = "attachment"
	}

	c.Response.Header.SetContentType(ct)
	setContentDisposition(c, f.FileName, disposition)
	// ⚠️ `private` 不是锦上添花:这个响应的准入是**逐请求查库**算出来的,
	// 而任何中间缓存只看 Cache-Control。缺了它,一个人打开过之后别人不登录
	// 也能从同一个出口拿到同样的字节。
	c.Response.Header.Set("Cache-Control", "private, max-age=300")
	// ⚠️ nosniff:不写它浏览器会绕过 Content-Type 拿内容去猜类型,上面
	// "探测不出就 octet-stream" 的兜底等于白设。
	c.Response.Header.Set("X-Content-Type-Options", "nosniff")
	// 同origin 而非 DENY:PDF 预览常用 iframe/embed 内嵌,DENY 会把它挡掉,
	// 而跨源内嵌本来就该挡。
	c.Response.Header.Set("X-Frame-Options", "SAMEORIGIN")

	// ⚠️ 长度未知必须传 -1(读到 EOF),不能传 0 —— 传 0 等于宣告"响应体是
	// 空的",hertz 会直接把 body 丢掉,表现是 200 + 0 字节。
	size := int(f.FileSize)
	if size < 0 {
		size = -1
	}

	// ⚠️ **Range 支持不是锦上添花**:没有它,`<video>` 拖不动进度条,而且
	// **iOS Safari 直接拒绝播放**(它要求服务端给 206)。
	// 签名直链那条路(OSS / 静态挂载)自带 Range,只有这条中转兜底没有 ——
	// 于是同一份文件,"公开的能拖、私有的不能",症状指向权限,
	// 真因却在响应头。
	switch st, start, length := parseRange(string(c.GetHeader("Range")), int64(size)); st {
	case rangePartial:
		if _, err := rc.Seek(start, io.SeekStart); err != nil {
			rc.Close()
			h.FailCode(c, errcode.Internal, "读取文件失败")
			return
		}
		c.Response.Header.Set("Content-Range", contentRangeHeader(start, length, int64(size)))
		c.SetStatusCode(consts.StatusPartialContent)
		c.Response.SetBodyStream(rc, int(length))
	case rangeUnsatisfiable:
		// ⚠️ **这里流的所有权还在我们手上**(还没交给 SetBodyStream),
		// 上面那段注释的"不 Close"约定不适用 —— 忘了关就是文件句柄泄漏,
		// 而这个 handler 每个视频请求走一次。
		rc.Close()
		c.Response.Header.Set("Content-Range", contentRangeUnsatHeader(int64(size)))
		c.SetStatusCode(consts.StatusRequestedRangeNotSatisfiable)
	default:
		c.SetStatusCode(consts.StatusOK)
		c.Response.SetBodyStream(rc, size)
	}
}

// ===== Range =====

// contentRangeHeader 拼 206 的 Content-Range。
//
// ⚠️ **单独成函数而不是内联拼**:这个字符串格式错了编译期发现不了,运行期也不
// 报错(只是个普通响应头),只有播放器会安静地少播一段 —— 测试也才有一个能直接
// 调的入口,而不是在测试里复制一遍拼法假装验过了。
func contentRangeHeader(start, length, size int64) string {
	return "bytes " + strconv.FormatInt(start, 10) + "-" +
		strconv.FormatInt(start+length-1, 10) + "/" + strconv.FormatInt(size, 10)
}

// contentRangeUnsatHeader 拼 416 的 Content-Range(只有总长,`bytes * /size`)
func contentRangeUnsatHeader(size int64) string {
	return "bytes */" + strconv.FormatInt(size, 10)
}

// rangeResult Range 解析的三种结果
type rangeResult int

const (
	// rangeFull:不按 Range 处理,整份返回(200)
	rangeFull rangeResult = iota
	// rangePartial:返回 [start, start+length) 这一段(206)
	rangePartial
	// rangeUnsatisfiable:范围非法且越界(416)
	rangeUnsatisfiable
)

// parseRange 解析单段 Range 头(RFC 7233 §2.1)。
//
// ⚠️ **只认单段**。多段(`bytes=0-1,5-6`)按规范允许服务端**忽略整个 Range**
// 返整份 200 —— 客户端把多段响应当成一个拼接体来解,���实期望也是"我给一串
// 范围你给我一串响应",而多数播放器根本不发多段。为它写 multipart/byteranges
// 只会引入一个只有测试见过的分支。
//
// ⚠️ **越界 clamp、非法 ignore、两者分清**:
//   - `bytes=0-99999` 而文件只有 100 字节 → 夹到 `0-99`,不是 416
//   - `bytes=500-600` 而文件只有 100 字节 → 416(起点就已在文件外)
//   - `bytes=abc` / `items=0-1` / `bytes=9-3` → 当作没传,整份 200
//
// 第三条最容易写错:`9-3` 是**非法**而不是越界,按 416 报的话,一个客户端的
// 边界 bug 会变成用户眼里的"视频播不了",而且日志里只有一个没头没尾的 416。
//
// ⚠️ size<=0 一律整份返回:FileSize 是库里的记账值,理论上和实际一致,
// 但真不一致时**宁可多发字节也不能发错范围** —— 206 的 Content-Range 错了,
// 客户端会把两段内容拼起来,得到一个坏掉的文件。
func parseRange(hdr string, size int64) (rangeResult, int64, int64) {
	if size <= 0 {
		return rangeFull, 0, 0
	}
	const prefix = "bytes="
	if len(hdr) <= len(prefix) || !strings.EqualFold(hdr[:len(prefix)], prefix) {
		return rangeFull, 0, 0
	}
	spec := hdr[len(prefix):]
	if strings.Contains(spec, ",") {
		return rangeFull, 0, 0
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return rangeFull, 0, 0
	}
	rawStart, rawEnd := spec[:dash], spec[dash+1:]

	// 后缀形态 `bytes=-N`:最后 N 字节
	if rawStart == "" {
		if rawEnd == "" {
			return rangeFull, 0, 0
		}
		n, err := strconv.ParseInt(rawEnd, 10, 64)
		if err != nil || n <= 0 {
			return rangeUnsatisfiable, 0, 0
		}
		if n > size {
			n = size
		}
		return rangePartial, size - n, n
	}

	start, err := strconv.ParseInt(rawStart, 10, 64)
	if err != nil || start < 0 {
		return rangeFull, 0, 0
	}
	if start >= size {
		return rangeUnsatisfiable, 0, 0
	}
	end := size - 1
	if rawEnd != "" {
		e, err := strconv.ParseInt(rawEnd, 10, 64)
		// `bytes=9-3`:数字都合法但区间倒过来了 —— 非法,不是越界
		if err != nil || e < start {
			return rangeFull, 0, 0
		}
		if e < end {
			end = e
		}
	}
	return rangePartial, start, end - start + 1
}

// setContentDisposition 写 Content-Disposition。
//
// ⚠️ **文件名是原样入库的用户输入**,里面可能有引号、CRLF、中文,而
// Content-Disposition 必须原样带出它(不然用户下载到的全是 "download")。
// 所以两条参数各自解决一半的问题:
//
//   - filename= 走 ASCII 兜底,引号和反斜杠在 quoted-string 内转义、
//     控制字符换 _、非 ASCII 换 _ —— 老客户端只认这一条。
//   - filename* 走 RFC 5987 的百分号编码,保留真名 —— 新客户端优先这一条。
//
// 少任何一条都不致命,但两条都缺的表现是"下载得到的文件名偶尔对不上",
// 而且只在部分客户端上出现,极难复现。
func setContentDisposition(c *app.RequestContext, name, disposition string) {
	if name == "" {
		name = "download"
	}
	ascii := asciiFallback(name)
	// ⚠️ url.PathEscape 覆盖了 RFC 5987 要求的全部非 attr-char,而且额外
	// 转义了 `/ ; , ?`,比 attr-char 严格 —— 比标准严不会出错,比它松会。
	c.Response.Header.Set("Content-Disposition", disposition+
		"; filename=\""+ascii+"\"; filename*=UTF-8''"+url.PathEscape(name))
}

// asciiFallback 生成 quoted-string 里能安全放的 ASCII 文件名。
//
// ⚠️ 非 ASCII 一律换 '_' 而不是原样保留或丢弃 —— 保留会被代理按 latin-1
// 存成乱码,丢弃则"名字都是一半"的下载比 "_" 更让人困惑。
func asciiFallback(name string) string {
	b := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		ch := name[i]
		switch {
		case ch == '"' || ch == '\\':
			b = append(b, '\\', ch) // quoted-string 内转义,否则能把文件名提前闭合
		case ch < 0x20 || ch == 0x7f: // 控制字符:CR/LF 在这里被吃掉,防响应头拆分
			b = append(b, '_')
		case ch >= 0x80: // 非 ASCII
			b = append(b, '_')
		default:
			b = append(b, ch)
		}
	}
	// 空串会让 filename="" 这种畸形值出去(部分客户端当无名文件,部分报错)
	if len(b) == 0 {
		return "download"
	}
	return string(b)
}

// isInlineType 浏览器能不能直接渲染(而不是必须下载)
//
// ⚠️ **image/svg+xml 被故意排除**。SVG 里能塞 <script>,而这里是**同源**
// 响应:inline 一个用户上传的 SVG 等于把存储变成站内的 XSS 投放位。
// 上传的图片格式里 SVG 是唯一的例外,其余(image/png|jpeg|gif|webp|avif…)
// 都是被动解码的,inline 没有额外风险。
func isInlineType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		return false
	}
	if strings.Contains(ct, "svg") {
		return false
	}
	switch {
	case strings.HasPrefix(ct, "image/"),
		strings.HasPrefix(ct, "video/"),
		strings.HasPrefix(ct, "audio/"),
		ct == "application/pdf",
		ct == "text/plain":
		return true
	}
	return false
}

func (h *FileHandler) Quota(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	q, err := h.Services.File.Quota(ctx, caller)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, q)
}

// ===== 文件夹 =====

func (h *FileHandler) FolderList(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	page, size := h.GetPage(c)
	// ⚠️ **parent_id 三态,别用 MustUint** ——
	//   不传 = 不限层级(平铺列全部);传 0 = 只列根级;传 id = 只列它的直接子级。
	//   而 MustUint 拿不到 0 和"没传"的区别(它的默认就是 0),
	//   于是"进入文件夹"和"看全部"会变成同一个请求。
	parentID, err := optionalUintQuery(c, "parent_id")
	if err != nil {
		h.FailCode(c, errcode.InvalidParam, "parent_id 不合法")
		return
	}
	// ⚠️ **user_id 也是三态** —— 不传 = 列调用方自己的;传了 = 列那个用户的
	//   (只有 super_admin 能传别人的,service 层会返 403)。
	userID, err := optionalUintQuery(c, "user_id")
	if err != nil {
		h.FailCode(c, errcode.InvalidParam, "user_id 不合法")
		return
	}
	res, err := h.Services.Folder.List(ctx, caller, service.FolderListQuery{
		Biz:  c.Query("biz"),
		Page: page,
		Size: size,
		// ⚠️ 搜索框(前端 ManagementTable 的 onSearch)打过来的关键字。
		//   少这一行的话前端搜索框**看着能打字,回车没反应** ——
		//   参数没被后端接住,列表纹丝不动。
		Keyword: c.Query("keyword"),
		// ⚠️ **指针,不是值** —— nil 和 0 是两个意思,见上面。
		ParentID: parentID,
		// ⚠️ **指针** —— nil = "没指定 = 列我的",和"指定了某个 id"是两回事。
		UserID: userID,
	})
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// FolderUsers 用户层:列出"有文件夹的用户"。
//
// ⚠️ **普通用户只会拿到自己那一行** —— 判定在后端
//   (callerCanCrossUserRead,见 service/folder_users.go),
//   前端不自己猜角色。
func (h *FileHandler) FolderUsers(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	page, size := h.GetPage(c)
	res, err := h.Services.Folder.ListUsers(ctx, caller, service.FolderUserListQuery{
		Page:    page,
		Size:    size,
		Keyword: c.Query("keyword"),
	})
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// optionalUintQuery 取一个"可以不传"的 uint64 查询参数。
//
// ⚠️ **返回 nil 表示"没传"**,和"传了 0"是两回事 —— 层级查询靠这个区分
//   "不限层级"和"只列根级"。
//
// ⚠️ **传了但不是合法数字 → 报错**,不静默当成没传 —— 静默的话前端拼错参数
//   (比如把 id 拼成了 "12abc")会得到"全部文件夹"而不是一个明确的错误,
//   界面上表现为"筛选没生效"。
func optionalUintQuery(c *app.RequestContext, key string) (*uint64, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (h *FileHandler) FolderCreate(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	var body service.FolderCreateReq
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.Folder.Create(ctx, caller, body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, v.ID)
}

func (h *FileHandler) FolderDetail(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	d, err := h.Services.Folder.Detail(ctx, caller, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, d)
}

func (h *FileHandler) FolderDelete(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Folder.Delete(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

func (h *FileHandler) FolderSetCover(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body struct {
		FileID uint64 `json:"file_id,string"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.Folder.SetCover(ctx, caller, id, body.FileID)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// ===== 工具 =====

// isMultipart 判断是不是 multipart/form-data
func isMultipart(c *app.RequestContext) bool {
	ct := c.GetHeader("Content-Type")
	return len(ct) >= 19 && string(ct[:19]) == "multipart/form-data"
}

// formUint 取表单里的 uint64 字段
func (h *FileHandler) formUint(c *app.RequestContext, key string) uint64 {
	v := c.PostForm(key)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
