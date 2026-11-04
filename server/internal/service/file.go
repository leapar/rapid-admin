// FileService —— 文件元信息的 CRUD + 直传凭证
// 设计见 docs/文件系统模块设计方案V1.0.md §8
//
// ⚠️ **这一层不碰字节**。所有内容读写都走 `oss.Provider`,本文件只负责:
// 归属校验(§8.4) / 配额记账(§4.5) / 元信息登记 / info.json 生成(§2.3)。
// 职责不拆开就会出现"service 直接写磁盘"和"provider 直接写 DB"两头对账。
package service

import (
	"context"
	"io"
	"time"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/oss"
)

// FileOptions 文件模块的可调参数。
//
// ⚠️ **M81 之前这些是默认值,不是配置**。留着这个结构体而不是把数字散在各方法里,
// 是为了让 M81 接 yaml 时只做"config → FileOptions"的映射,不用回头翻业务代码。
type FileOptions struct {
	// Root 路径根词,空 → oss.DefaultRoot
	Root string
	// UploadTokenTTL 直传凭证有效期
	UploadTokenTTL time.Duration
	// MaxFileSize 单文件字节上限。0 = 不限(不推荐,失去 BodyLimit 之外的第二道闸)
	MaxFileSize int64
	// DownloadURLTTL 下载临时 URL 默认有效期
	DownloadURLTTL time.Duration
	// MaxDownloadTTL 下载 URL 有效期上限(防前端传 ttl=999999h)
	MaxDownloadTTL time.Duration
	// SessionTTL 上传会话有效期,创建时算成绝对时刻写进 sys_upload_session.expires_at
	SessionTTL time.Duration
	// MultipartThreshold 超过这个大小走分片,以下走单次
	MultipartThreshold int64
	// MultipartPartSize 分片大小
	MultipartPartSize int64
	// AllowedBiz 业务段白名单(`file.allowed_biz`)。**空 = 不限**,只做语法校验。
	//
	// ⚠️ 框架不硬编码业务枚举(与字典单一来源一致),哪个 biz 合法由业务侧自己
	// 决定。留空时的行为和 M80 落地时完全一致 —— 这是一道**新增的闸**,
	// 默认关闭,不是行为变更。
	AllowedBiz []string

	// FFmpegPath ffmpeg 可执行文件路径(M141)。空 = 不做媒体探测。
	//
	// ⚠️ **空 = 关掉整个媒体探测**,不是"用 PATH 找" —— 因为"用 PATH 找"
	//   是配置层的默认(见 config.ResolveMediaTools),到 service 层时
	//   空值只可能意味着"没配"。
	FFmpegPath string
	// FFprobePath ffprobe 可执行文件路径(M141)。空 = 不做媒体探测。
	FFprobePath string
	// MediaMaxSize 媒体探测的文件大小上限(字节)。0 = 不限。
	//
	// ⚠️ **探测要把文件完整落到本地磁盘** —— 这个闸就是"临时盘要留多少空间"。
	MediaMaxSize int64
	// MediaTmpDir 媒体临时副本的根目录。空 = os.TempDir()。
	MediaTmpDir string

	// TranscodeWorkers 转码并发数(M145)。**0 = 整个转码功能关着**。
	//
	// ⚠️ **每个转码是一个 ffmpeg 进程,吃满一个核** —— 这个数就是
	//   "最多同时烧几个核"。默认 2 是刻意的:够一个上传高峰用,
	//   又不会把一台 2 核的小机器打满。
	//
	// ⚠️ **关掉它就完全不做转码** —— 浏览器还是播不了那些文件,
	//   但列表里会显示"浏览器不支持" + 原因(见 MediaReason),
	//   用户至少知道为什么。
	TranscodeWorkers int
}

// DefaultFileOptions 默认参数
func DefaultFileOptions() FileOptions {
	return FileOptions{
		Root:               oss.DefaultRoot,
		UploadTokenTTL:     30 * time.Minute,
		MaxFileSize:        100 << 20, // 100 MB
		DownloadURLTTL:     15 * time.Minute,
		MaxDownloadTTL:     24 * time.Hour,
		SessionTTL:         24 * time.Hour,
		MultipartThreshold: 100 << 20, // 100 MB,对齐既有 MultipartMinSize
		MultipartPartSize:  8 << 20,   // 8 MB
		// ⚠️ **默认开着** —— 但它还额外要求 FFmpegPath 配了,
		//   没装 ffmpeg 的机器上它自然不跑。
		TranscodeWorkers: 2,
	}
}

// normalize 补齐零值。
//
// ⚠️ **只补"没配"和"配错成负数",不补配错成 0 的**。`MaxFileSize: 0` 是"不限额"
// 的意思还是"忘了配"的意思,本函数选了前者 —— 想放开就把配置显式写成 0,
// 而不是把默认值悄悄兜回去导致测试环境和生产环境行为不同。
func (o FileOptions) normalize() FileOptions {
	d := DefaultFileOptions()
	if o.Root == "" {
		o.Root = d.Root
	}
	if o.UploadTokenTTL <= 0 {
		o.UploadTokenTTL = d.UploadTokenTTL
	}
	if o.DownloadURLTTL <= 0 {
		o.DownloadURLTTL = d.DownloadURLTTL
	}
	// 上限不能小于默认值,否则 ttl=1 反而比 ttl=15m 更容易过期,越传越容易 403
	if o.MaxDownloadTTL < o.DownloadURLTTL {
		o.MaxDownloadTTL = o.DownloadURLTTL
	}
	if o.SessionTTL <= 0 {
		o.SessionTTL = d.SessionTTL
	}
	if o.MultipartThreshold <= 0 {
		o.MultipartThreshold = d.MultipartThreshold
	}
	if o.MultipartPartSize <= 0 {
		o.MultipartPartSize = d.MultipartPartSize
	}
	return o
}

// FileUploadTokenReq 申请直传凭证
type FileUploadTokenReq struct {
	// FolderID 目标文件夹。0 = 用 (biz) 下的默认文件夹(不存在则懒创建)
	FolderID uint64 `json:"folder_id,string"`
	// Biz 业务英文名(路径第二段)。**决定归属校验的维度**,不能由前端随意填
	// 别人业务段的文件夹。
	Biz string `json:"biz"`
	// FileName 原始文件名
	FileName string `json:"file_name"`
	// ContentType MIME。⚠️ 客户端说了算,只作为提示,落库时以服务端探测为准
	ContentType string `json:"content_type"`
	// FileSize 申报字节数,用于配额预检
	FileSize int64 `json:"file_size"`
}

// FileRegisterReq 直传完成后登记元数据。
//
// ⚠️ **`POST /files` 一条路由同时承担两件事**:服务端中转上传(`multipart/form-data`,
// 走 `Receive`)和直传后登记(`application/json`,走这里)。拆成两个端点的话,
// 前端要为"OSS 直传"和"本地中转"各写一套登记逻辑,而两边的登记字段一模一样。
type FileRegisterReq struct {
	// Key 直传时客户端拿到的 key
	Key         string `json:"key"`
	FolderID    uint64 `json:"folder_id,string"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
}

// FileReceiveReq 服务端中转上传。
//
// ⚠️ **`Size` 是申报值,`prov.Put` 返回的才是实际值**,两者必须分开记账
// (预扣用申报,结算用实际 —— §4.5)。直接拿 Size 记账的话,客户端少报就能
// 少占配额,而存储里写的是完整的内容。
type FileReceiveReq struct {
	FolderID    uint64
	Biz         string
	FileName    string
	ContentType string
	// Size 申报字节数,0 = 未知(不预扣,改为传完后按实际结算)
	Size int64
	Body io.Reader
}

// FileListQuery 文件列表查询
type FileListQuery struct {
	FolderID uint64
	Biz      string
	Status   int
	Keyword  string
	Page     int
	Size     int
}

// FileView 文件视图(列表 / 详情共用)
type FileView struct {
	ID          uint64    `json:"id,string"`
	FolderID    uint64    `json:"folder_id,string"`
	UserID      uint64    `json:"user_id,string"`
	Biz         string    `json:"biz"`
	FileKey     string    `json:"file_key"`
	FileName    string    `json:"file_name"`
	FileSize    int64     `json:"file_size"`
	ContentType string    `json:"content_type"`
	ETag        string    `json:"etag"`
	Storage     int       `json:"storage"`
	IsCover     bool      `json:"is_cover"`
	Status      int       `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	// URL 访问地址(**稳定直链**,能直接塞进 `<img src>`)。
	//
	// ⚠️ **M134:列表也填了** —— 缩略图视图要把它塞进 `<img src>`,
	//   逐条调 `/files/:id/url` 的话一页就是 N 个往返。
	//
	//   以前这里"列表不填",理由写的是"要签 N 个 URL,拖慢查询" ——
	//   **那个前提在 M80 单桶单树模型之后已经不成立**:直链是**拼出来的**
	//   (不签名、不回存储、不调 OSS),一次填 N 条的代价只是 N 次字符串拼接
	//   (local provider 多一次 stat)。文件夹列表的 `cover_url` 早就是
	//   这么干的,文件列表跟它对齐。
	//
	// ⚠️ 拿不到就留空(前端据此显示占位图标),不报错 —— 一个文件的直链
	//   签不出来,不该让整页列表 500。
	URL string `json:"url,omitempty"`

	// DurationMs 媒体时长(毫秒)。0 = 不是媒体 / 没探测出来。
	//
	// ⚠️ **单位是毫秒** —— 秒单位下几百毫秒的短视频会被截成 0,
	//   看起来跟"没有时长"一模一样。
	DurationMs int64 `json:"duration_ms"`

	// CoverURL 封面帧的直链(视频抽出来的第一帧)。
	//
	// ⚠️ **空串 = 没有封面** —— 音频没有封面(前端摆音频图标)、
	//   没装 ffmpeg、探测失败,都是空串。前端据此回落到格式图标。
	//
	// ⚠️ **它是"稳定直链"不是签名 URL** —— 和 URL 同一个道理,
	//   签名过期就裂图。
	CoverURL string `json:"cover_url,omitempty"`

	// MediaContainer 原始容器名(mp4 / webm / mkv / avi / asf / flv …)
	MediaContainer string `json:"media_container,omitempty"`
	// MediaVideoCodec 原始视频编码(h264 / hevc / vp9 / mpeg4 …)
	MediaVideoCodec string `json:"media_video_codec,omitempty"`
	// MediaAudioCodec 原始音频编码(aac / mp3 / opus / wmav2 …)
	MediaAudioCodec string `json:"media_audio_codec,omitempty"`
	// MediaReason 为什么浏览器播不了(一句人话)。**空 = 浏览器能直接播**。
	//
	// ⚠️ **这是给人看的** —— 前端直接显示,不用自己拿编码去拼这句话
	//   (前端多一份判据表 = 后端改一次要同步改两处)。
	MediaReason string `json:"media_reason,omitempty"`

	// PlaybackURL 浏览器可播副本的直链(M145)。
	//
	// ⚠️ **空串 = 没有可播副本** —— 三种情况:浏览器本来就能播(不需要转)、
	//   还在转、或者转失败了。前端据此决定"用原文件还是用副本"。
	PlaybackURL string `json:"playback_url,omitempty"`

	// TranscodeStatus 转码状态(见 model.TranscodeStatus)
	TranscodeStatus int `json:"transcode_status"`
}

// FileService 文件元信息服务
type FileService interface {
	// UploadToken 签直传凭证(OSS 直传用)。proxy 模式下前端应改走 Receive 中转。
	UploadToken(ctx context.Context, caller *Caller, req FileUploadTokenReq) (*oss.UploadToken, error)
	// Receive 服务端中转:写字节 + 记元数据 + 结算配额
	Receive(ctx context.Context, caller *Caller, req FileReceiveReq) (*FileView, error)
	// Register OSS 直传完成后登记元数据(按 key 去存储里取真实大小,不采信客户端申报)
	Register(ctx context.Context, caller *Caller, req FileRegisterReq) (*FileView, error)
	// List 文件列表
	List(ctx context.Context, caller *Caller, q FileListQuery) (*model.PageResponse, error)
	// Detail 文件详情(含访问 URL)
	Detail(ctx context.Context, caller *Caller, id uint64) (*FileView, error)
	// URL 取访问 URL。ttl<=0 用默认值;超过上限会被夹到上限(不报错)
	URL(ctx context.Context, caller *Caller, id uint64, ttl time.Duration) (string, error)
	// Content 打开字节流,由业务接口回源读(不经直链)。
	//
	// ⚠️ **这是直链签不出来时的兜底**,不是主路径。返回的流必须在本次请求内
	// 读完并 Close —— provider 持有文件句柄,不能交给异步逻辑。
	//
	// 同时返回 FileView 是因为下游要两样它独有的东西:**原始文件名**(给
	// Content-Disposition)和**服务端探测出来的 ContentType**(给响应头)。
	// 两者都不能从浏览器上报的值取 —— 上报 text/html 就能在本站下执行脚本。
	//
	// provider 不实现 oss.ContentProvider 时返 NotImplemented(501)而不是 500:
	// "OSS 模式不支持中转"是一个可预期的部署形态,不是一个故障。
	Content(ctx context.Context, caller *Caller, id uint64) (io.ReadSeekCloser, *FileView, error)
	// Delete 删文件(物理对象 + sys_file 行 + 配额 + 冗余计数 + info.json)
	Delete(ctx context.Context, caller *Caller, id uint64) error
	// Quota 当前用户的存储配额
	Quota(ctx context.Context, caller *Caller) (*QuotaView, error)
	// ResolveKeys 批量把文件 key 解析成「原文件名 + 直链」
	//
	// ⚠️ **不校验归属** —— 这是给"动态集合的附件字段"用的:
	// 一条记录里的附件 key 可能是别人上传的(管理员替人录数据),
	// 按"只有上传者能读"的规则去解析,附件就永远显示不出来。
	//
	// ⚠️ **查不到就不在返回的 map 里,不报错** ——
	// 文件被删了 / key 写错了 / 存储还没同步,都是"这一项没有解析出来",
	// 不是"整个请求失败"。
	ResolveKeys(ctx context.Context, keys []string) (map[string]FileRef, error)
}

// FileRef 文件的展示信息(原文件名 + 直链)
type FileRef struct {
	// Name 原始文件名(给用户看的那一份,不是存储里的对象名)
	Name string `json:"name"`
	// URL 直链。**可能为空** —— 签不出来时是空串(纯展示用,拿不到就空着)
	URL string `json:"url"`
}
