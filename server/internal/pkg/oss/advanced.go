// OSS 高级特性 —— §1.10 #5-10 P2
//
// 与基础 provider.go(直传凭证 / 临时下载 / 删除)平行,提供:
//
//	#5 分片上传(Multipart Upload): 大文件分片 Init / UploadPart / Complete / Abort
//	#6 断点续传(Resume Upload): 记录已上传分片,中断后只需补传
//	#7 病毒扫描(Virus Scan): 异步扫描,Aliyun CIM / ClamAV 自建
//	#8 缩略图(Thumbnail): 走 OSS 触发器 / ImageMagick
//	#9 防盗链(Hotlink Protection): Referer 白名单 + 签名 URL 增强
//	#10 配额 / CDN 刷新(Quota / CDN): 单用户/单目录流量统计 + CDN URL 主动刷新
//
// 设计:
//   - 与基础 Provider 平行的子接口 AdvancedProvider(可选实现)
//   - 业务方按需调 type-assert 拿;没有就降级
//   - 每个子特性独立接口,业务用多少引多少
package oss

import (
	"context"
	"errors"
	"io"
	"time"
)

// ============== #5 #6 分片上传 + 断点续传 ==============

// MultipartProvider 分片上传 + 断点续传
//
//	InitMultipart: 大文件(默认 > 100MB)走分片,提升成功率 + 并发
//	ListParts: 中断后查已上传分片,客户端只需补传缺失
type MultipartProvider interface {
	// InitMultipartUpload 初始化分片上传,返 uploadId + 建议分片大小
	InitMultipartUpload(ctx context.Context, key string, opts ...InitOption) (*MultipartInit, error)
	// UploadPart 上传 1 个分片(partNumber 1..10000,etag 客户端必须记录)
	UploadPart(ctx context.Context, key, uploadID string, partNumber int, body []byte) (etag string, err error)
	// ListParts 列出已上传分片(用于断点续传)
	ListParts(ctx context.Context, key, uploadID string) ([]Part, error)
	// CompleteMultipartUpload 完成分片上传(parts 按 partNumber 升序)
	CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []Part) error
	// AbortMultipartUpload 取消分片上传
	AbortMultipartUpload(ctx context.Context, key, uploadID string) error
}

// InitOption Init 选项
type InitOption func(*InitOpts)

// InitOpts Init 选项结构
type InitOpts struct {
	ContentType string
	Metadata    map[string]string
	// ACL: default / private / public-read / public-read-write
	ACL string

	// PartSize 分片大小,0 = 用 DefaultPartSize。
	//
	// ⚠️ **service 层应该显式给**,不要依赖默认:默认 10MB 对 100GB 文件会
	// 正好卡满 10000 片上限,而 1GB 文件用 100MB 分片只要 10 片。
	PartSize int64
	// TotalSize 文件总字节数,0 = 未知。
	//
	// 给了的话 CompleteMultipartUpload 会校验"合并后尺寸 == TotalSize",
	// 尺寸对不上直接丢弃临时文件。**这是本地磁盘唯一的完整性闸** —— OSS
	// 侧由 OSS 自己保证 part 列表和尺寸一致,本地全靠这一条。
	TotalSize int64
}

func WithContentType(ct string) InitOption {
	return func(o *InitOpts) { o.ContentType = ct }
}
func WithMetadata(m map[string]string) InitOption {
	return func(o *InitOpts) { o.Metadata = m }
}
func WithACL(acl string) InitOption {
	return func(o *InitOpts) { o.ACL = acl }
}

// WithPartSize 指定分片大小。非法值不在这里拦(那会要求每个调用点都记得判错),
// 由 provider 在 InitMultipartUpload 里统一 CheckPartSize。
func WithPartSize(n int64) InitOption {
	return func(o *InitOpts) { o.PartSize = n }
}

// WithTotalSize 声明文件总大小,启用 Complete 阶段的尺寸校验。
func WithTotalSize(n int64) InitOption {
	return func(o *InitOpts) { o.TotalSize = n }
}

// MultipartInit Init 返回
type MultipartInit struct {
	Key      string `json:"key"`
	UploadID string `json:"upload_id"`
	// PartSize 建议分片大小(字节,client 切片用)
	PartSize int64 `json:"part_size"`
	// PartNum 总分片数(由 partSize 算出,client 进度展示用)
	PartNum int `json:"part_num"`
}

// Part 已上传分片
type Part struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

// ============== #7 病毒扫描 ==============

// ScanProvider 病毒扫描
//
//	Scan: 异步,返 scanID;后续轮询 GetResult
//	Result: clean / infected / error / timeout
type ScanProvider interface {
	// Scan 发起扫描(异步,框架立即返)
	Scan(ctx context.Context, key string) (scanID string, err error)
	// GetScanResult 查扫描结果
	GetScanResult(ctx context.Context, scanID string) (*ScanResult, error)
}

// ScanStatus 扫描状态
type ScanStatus string

const (
	ScanStatusPending  ScanStatus = "pending"  // 排队
	ScanStatusScanning ScanStatus = "scanning" // 扫描中
	ScanStatusClean    ScanStatus = "clean"    // 干净
	ScanStatusInfected ScanStatus = "infected" // 检出
	ScanStatusError    ScanStatus = "error"    // 错误
	ScanStatusTimeout  ScanStatus = "timeout"  // 超时
)

// ScanResult 扫描结果
type ScanResult struct {
	ScanID    string     `json:"scan_id"`
	Key       string     `json:"key"`
	Status    ScanStatus `json:"status"`
	Threats   []string   `json:"threats,omitempty"` // 病毒名(infected 时填)
	CheckedAt time.Time  `json:"checked_at"`
}

// ErrInfected 检出病毒(框架层返这个 error,业务可判断)
var ErrInfected = errors.New("oss: file infected")

// ============== #8 缩略图 ==============

// ThumbnailProvider 缩略图
//
//	策略 A: 走 OSS 触发器(上传时自动生成,业务用 GetThumbURL)
//	策略 B: 现算现传(框架生成图片二进制,再传到 OSS)
type ThumbnailProvider interface {
	// GetThumbURL 拿缩略图访问 URL(策略 A 走 OSS Image Processing)
	//   - size: "200x200" / "100x100>" 等
	GetThumbURL(ctx context.Context, key, size string) (url string, err error)
}

// ============== #9 防盗链 ==============

// HotlinkProvider 防盗链
//
//	Verify: 校验 Referer / 签名 token,返回 nil = 放行
type HotlinkProvider interface {
	// Verify 校验 referer / signed token
	//   - referer: HTTP Referer header(可能空,带就直接 OK)
	//   - token: URL 签名后的 sig 参数
	//   - bucket: 请求的 bucket(admin/public)
	Verify(ctx context.Context, referer, key, token string) error
	// SignToken 签防盗链 token(绑 referer + 过期)
	SignToken(ctx context.Context, key string, ttl time.Duration) (token string, err error)
}

// ============== #10 配额 / CDN 刷新 ==============

// QuotaProvider 配额管理
type QuotaProvider interface {
	// GetUsage 查某路径/某用户已用字节
	GetUsage(ctx context.Context, scope QuotaScope) (used int64, err error)
	// SetLimit 设置上限(超限时后续上传返 ErrQuotaExceeded)
	SetLimit(ctx context.Context, scope QuotaScope, limitBytes int64) error
}

// QuotaScope 配额作用域
type QuotaScope struct {
	// UserID 用户级(可选)
	UserID uint64
	// Prefix 路径级(可选,如 "user-avatar/")
	Prefix string
	// Bucket 哪个 bucket
	Bucket string
}

// ErrQuotaExceeded 配额超限
var ErrQuotaExceeded = errors.New("oss: quota exceeded")

// CDNProvider CDN 刷新
type CDNProvider interface {
	// Refresh 主动刷新 URL(让 CDN 立即回源)
	Refresh(ctx context.Context, urls ...string) error
	// Purge 主动失效 URL(从 CDN 删除)
	Purge(ctx context.Context, urls ...string) error
	// Prefetch 主动预热 URL(让 CDN 提前拉源)
	Prefetch(ctx context.Context, urls ...string) error
}

// ============== 服务端中转读 ==============

// ContentProvider 由业务接口回源读字节(不经直链)。
//
// ⚠️ **为什么它是可选接口而不是 Provider 的方法**:直链是主路径(OSS 上是桶的
// 公网地址、local 上是静态挂载),中转是**兜底** —— 中转要按原始文件名填
// Content-Disposition、要按访问者鉴权的时候才有意义。
//
// ⚠️ 早期它兜的是另一个场景:"local 私有文件签不出直链" —— 那套模型已经没了
// (存储侧只有一棵树,每个文件都有直链,见 oss/path.go 的文件头)。现在"签不出
// 直链" = 对象真的不在 = 真故障,必须报错,没有"兜一下"的余地。
//
// 现状:**只有 local 实现**。OSS 侧不实现是刻意的 —— 中转会把大文件的带宽
// 拉回应用服务器,与"直传"的存在意义相反。type-assert 失败返 nil,调用方返
// 501 而不是 500(见 service.FileService.Content)。
type ContentProvider interface {
	// Open 打开对象读流。返回的 ReadSeekCloser 必须由调用方 Close。
	//
	// ⚠️ 调用方**必须在这一轮请求内读完**,不能把流交给异步逻辑 ——
	// provider 可能持有文件句柄,请求结束就失效。
	Open(ctx context.Context, key string) (io.ReadSeekCloser, *ObjectMeta, error)
}

// ============== 小元数据回读 ==============

// MetaReader 读回**小元数据对象**的字节(当前唯一用途:info.json)。
//
// ⚠️ **为什么是可选接口而不是 Provider 的方法**:M78 定 8 个方法时逐条问过
// "有没有调用方需要它",答案是没有,所以没进主接口。后来 service 层要
// 把 info.json 读回来给运维排障(见 service/folder_info.go 的
// readFolderInfo),就有了调用方 —— 但**仍然不该进主接口**:
//
// 进主接口等于向全仓宣告"读任意对象的字节"是 provider 的标准能力,
// 而这个方法的语义是"只读小元数据"(各有 8MB 上限,见各实现)。
// 以后有人拿它读 500MB 视频,就是在应用服务器上 OOM。
// 真要读大文件走 ContentProvider(Open)或直链。
//
// ⚠️ **上限 8MB**:info.json 只有几 KB,这个闸门是防手滑的。
type MetaReader interface {
	// Get 读回对象的字节。
	//
	// ⚠️ **不存在时必须返回 `oss.ErrNotFound`** —— 调用方靠它区分
	// "这个文件夹还没有 info.json"(正常,比如刚建还没传文件)和
	// "读失败"(异常)。返回别的错会被当成异常上报。
	Get(ctx context.Context, key string) ([]byte, error)
}

// ============== 工具 ==============

// Advanced 高级特性门面(framework 层选型)
//
//	业务通过 type-assert oss.Provider.(oss.MultipartProvider) 拿
//	失败降级:返 nil / false 表示 provider 不支持
func Advanced(p Provider) (MultipartProvider, ScanProvider, ThumbnailProvider, HotlinkProvider, QuotaProvider, CDNProvider) {
	mp, _ := p.(MultipartProvider)
	sp, _ := p.(ScanProvider)
	tp, _ := p.(ThumbnailProvider)
	hp, _ := p.(HotlinkProvider)
	qp, _ := p.(QuotaProvider)
	cp, _ := p.(CDNProvider)
	return mp, sp, tp, hp, qp, cp
}
