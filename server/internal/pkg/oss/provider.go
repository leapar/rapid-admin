// OSS 网关 —— 抽象 Provider
// 文档 §14.1
package oss

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Provider 实现名 —— `Provider.Name()` 的**唯一取值集合**。
//
// ⚠️ **为什么必须是常量而不是各 provider 自己写字符串**:
// service 层要按 provider 名判断"这个文件实际躺在哪"
// (model.FileStorage:local / oss),而 provider 分散在三个包里
// (local / aliyun / mock)。早先 service 层比的是一个手写字符串 "aliyun",
// 而 aliyun provider 的 Name() 返回的是 "oss" —— **永远不相等**。
// 于是无论真配了 OSS 还是本地磁盘,每一行文件都被记成"本地磁盘",
// 逐行记 storage 的意义(迁移期两种 provider 共存)整个废掉。
//
// 反过来也提醒:**别再往 service 层写 provider 名字面量**,一律用这里的常量。
const (
	// ProviderNameLocal 本地磁盘
	ProviderNameLocal = "local"
	// ProviderNameOSS 阿里云 OSS
	ProviderNameOSS = "oss"
	// ProviderNameMock 测试替身
	ProviderNameMock = "mock"
)

// Provider 对象存储统一抽象。
//
// ⚠️ **调 service 层永远按这个接口调,不要 type-assert 到具体实现**。
// 分片上传不在这里 —— 它留在 `MultipartProvider` 可选接口(§5.3),不支持时
// 降级到单次上传。把分片塞进主接口会让 local / oss 之外再没有实现余地。
//
// 为什么 `List` 只收一个 `ListOption` 而不是 `(prefix, opt)`:prefix 是列举的
// 第一个维度,不是可选项 —— OSS 的 ListObjects、local 的目录 walk 都是"从某
// 个前缀开始"。拆成两个参数只会让每个调用点都要在传 opt 之前先决定 prefix,
// 而 prefix 永远是必填的。
type Provider interface {
	// Name 实现名(local / oss / mock)。排障和日志用,也是选型断言的抓手。
	Name() string

	// ---- 上传 ----

	// SignUploadToken 签发上传凭证
	//   - key: **服务端决定好的完整对象 key**,不是目录前缀
	//   - maxSize: 单文件字节上限,0 = 用 provider 默认
	//
	// ⚠️ **参数是 key 不是 dir,而且是有意的**。旧的 PostObject 形态只签
	// `starts-with $key =<dir>`,于是客户端能在那个前缀下**任意**写 key ——
	// 拿到凭证就能把文件塞进同 bucket 的任意目录。改成 `eq $key =<key>` 之后,
	// 凭证的作用域就是那一个对象,而 `content-length-range` 这个大小闸门还能
	// 留着(预签名 PUT URL 反而做不到,见设计文档 §5.1 的更正)。
	SignUploadToken(ctx context.Context, key string, ttl time.Duration, maxSize int64) (*UploadToken, error)

	// Put 服务端直写。local 的上传路径,OSS 模式下仅小文件中转用。
	//
	// ⚠️ 实现必须**原子上传**(临时文件 + rename / OSS 自身语义):写到一半
	// 失败的 key 不能留下一个"看起来存在、内容却是半截"的对象。
	Put(ctx context.Context, key string, r io.Reader, contentType string) (*ObjectMeta, error)

	// ---- 访问 ----

	// SignDownloadURL 签发私有 bucket 下载 URL。
	SignDownloadURL(ctx context.Context, key string, ttl time.Duration) (string, error)

	// ---- 信息 ----

	// Stat 取单个对象元信息。对象不存在必须返回 `IsNotFound(err) == true`,
	// service 层靠它把 404 和 500 分开 —— 上游用 `errors.Is(err, os.ErrNotExist)`
	// 判是判不出来的。
	Stat(ctx context.Context, key string) (*ObjectMeta, error)

	// ---- 删除 ----

	// Delete 删除对象。key 不存在**不算错**(幂等),否则重试会永远失败。
	Delete(ctx context.Context, keys ...string) error

	// DeletePrefix 删掉某个前缀下的全部对象,返回删掉的**字节数**。
	//
	// ⚠️ 返回字节数不是顺手加的:用户存储配额(`sys_user.storage_used_bytes`)
	// 靠它回退,没有返回值就只能整表重算。
	DeletePrefix(ctx context.Context, prefix string) (int64, error)

	// ---- 列举 ----

	// List 列举前缀下的对象 + 虚拟目录。分页靠 `ListOption.Marker` /
	// `ListResult.NextMarker`。
	List(ctx context.Context, opt ListOption) (*ListResult, error)
}

// UploadToken 上传凭证。
//
// ⚠️ **字段是按模式分组的**:proxy 模式只填 Mode/Key/MaxSize/ExpireAt,
// direct 模式才填 Host/Policy/Signature 那半。全部 `omitempty` 是为了让
// proxy 模式的响应里不出现一堆空字段 —— 那会让前端分不清"没下发"和"下发成空"。
type UploadToken struct {
	// Mode 上传通道,前端按它决定是 PUT 到 host 还是 POST 给后端
	Mode UploadMode `json:"mode"`
	// Key 服务端决定好的完整对象 key。
	//
	// 两种模式都返回,原因一样:客户端**永远不需要自己拼 key**(拼了就能越权),
	// 而且上传完之后它需要知道文件落在哪,才能显示"上传成功"。
	Key string `json:"key"`
	// MaxSize 单文件字节上限(proxy 模式由服务端 body limit 执行)
	MaxSize int64 `json:"max_size"`
	// ExpireAt 过期时间(unix 秒)
	ExpireAt int64 `json:"expire_at"`
	// Provider 哪个实现签的,排障时用来区分 local / oss 行为差异
	Provider string `json:"provider"`

	// ---- 以下仅 direct(OSS)模式 ----

	// Host 上传 endpoint,形如 https://bucket.oss-cn-hangzhou.aliyuncs.com
	Host string `json:"host,omitempty"`
	// Bucket 目标 bucket
	Bucket string `json:"bucket,omitempty"`
	// AccessKeyID 阿里云 AccessKey ID
	//
	// ⚠️ **只有 ID,没有 Secret** —— 这是整套直传机制的安全支点。Secret 留在
	// 服务端签名,前端永远拿不到。
	AccessKeyID string `json:"access_key_id,omitempty"`
	// Policy base64 编码的 policy
	Policy string `json:"policy,omitempty"`
	// Signature policy 的 HMAC-SHA1 签名
	Signature string `json:"signature,omitempty"`
	// SecurityToken STS 临时凭证(用 STS 时才有)
	SecurityToken string `json:"security_token,omitempty"`
}

// ---- 错误 ----

// ErrNotImplemented 留作后续接阿里云 SDK
var ErrNotImplemented = fmt.Errorf("oss: not implemented")

// ⚠️ 这里**没有** "签不出直链" 的哨兵 —— 以前有一个
// `ErrDirectURLUnsupported`,配套的是"只有公开子树挂静态、只有公开对象能签
// 直链"的老模型。现在存储侧只有一个树 / 一个桶,**每个文件都能签出直链**,
// 于是"签不出来"这个状态**根本不存在**,留着一个没人返回的哨兵只会让人
// 以为还有第二条路径要处理。
