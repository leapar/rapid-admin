// 文件系统模块 M77 —— 直传模式 / 上传凭证的共用词汇
//
// 设计见 `docs/文件系统模块设计方案V1.0.md` §2(双通道上传)与 §5。
//
// 这一组类型**不是 local 专属的**:`Provider` 抽象的是"存对象",而 OSS 和本地磁盘
// 是两种完全不同的存法(D2)。把差异收敛成 `UploadMode` 之后,service 层不需要
// `if provider == "local" {...}` 这种分支,只判 mode。
package oss

import (
	"errors"
	"fmt"
	"time"
)

// ============== 上传模式 ==============

// UploadMode 上传通道。
//
// 为什么不用"provider 名字"来分支:local 和 oss 是**部署选择**,会随环境变
// (本地开发用 local,生产用 oss)。而"这次上传走直传还是服务端中转"是**业务事实**,
// 不因为换环境就变。判 mode 而不是判 provider,换环境时前端一行都不用改。
type UploadMode string

const (
	// UploadModeProxy 服务端中转 —— 客户端把文件 POST 给后端,后端写存储。
	//
	// local 磁盘**只能**走这条:浏览器没法直接写服务器的磁盘。
	UploadModeProxy UploadMode = "proxy"

	// UploadModeDirect 客户端直传 —— 客户端拿着凭证 PUT 到对象存储。
	//
	// OSS 走这条,理由不是"省服务器带宽"这么表面:大文件经后端中转要占住一个
	// 连接和一个请求体,而直传是客户端 ↔ OSS 直连。
	UploadModeDirect UploadMode = "direct"
)

// Valid 合法模式
func (m UploadMode) Valid() bool {
	return m == UploadModeProxy || m == UploadModeDirect
}

// ============== 错误 ==============

// ErrNotFound 对象不存在。
//
// ⚠️ **做成类型而不是 sentinel var**:定位信息(key)是排查刚需,而
// `errors.Is` 只认 sentinel。两条路都要,所以实现 `Is` 方法 —— 调用方既能
// `errors.Is` 判类型,又能 `errors.As` 拿 key。
type ErrNotFound struct {
	Key string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("oss: object not found: %s", e.Key)
}

// Is 让 `errors.Is(err, &oss.ErrNotFound{})` 成立(忽略 Key 字段差异)
func (e *ErrNotFound) Is(target error) bool {
	_, ok := target.(*ErrNotFound)
	return ok
}

// IsNotFound 判断错误是不是"对象不存在"。
//
// 为什么给一行 helper:`errors.As(err, &target)` 需要一个可寻址的变量,写不成
// 内联形式;而每个调用点重复 `var e *ErrNotFound; errors.As(err, &e)` 三行很吵。
func IsNotFound(err error) bool {
	var e *ErrNotFound
	return errors.As(err, &e)
}

// ============== 分片常量 ==============

const (
	// DefaultPartSize 默认分片大小 10MB。
	//
	// 取值理由:太小则请求数暴涨(OSS 硬上限 10000 片),太大则单片失败要重传
	// 的比例高。10MB 是 OSS 官方推荐值。
	DefaultPartSize int64 = 10 << 20

	// MinPartSize 分片大小下限,对齐 OSS 的 100KB 硬限制。
	// 本地磁盘没有这个限制,但**统一**上限可以让两边的行为一致 ——
	// 开发时 local 通过、生产 OSS 报错的 bug 是最恶心的那类。
	MinPartSize int64 = 100 << 10

	// MaxPartNum 分片数上限,对齐 OSS 的 10000 片硬限制。
	MaxPartNum = 10000
)

// CheckPartSize 校验分片大小是否可用。
//
// ⚠️ **这里只管硬限制,不管"合不合理"**:100GB 文件用 10MB 分片刚好卡满 10000 片,
// 该调大 partSize,但那是 service 层按文件大小做的策略决策,不在 provider 里定。
func CheckPartSize(partSize int64) error {
	if partSize < MinPartSize {
		return fmt.Errorf("oss: 分片大小 %d 小于下限 %d", partSize, MinPartSize)
	}
	return nil
}

// CheckPartNumber 校验分片序号。
func CheckPartNumber(n int) error {
	if n < 1 || n > MaxPartNum {
		return fmt.Errorf("oss: 分片号 %d 越界(1..%d)", n, MaxPartNum)
	}
	return nil
}

// ============== 凭证 ==============

// Expired 凭证是否已过期。
//
// ⚠️ 用 `>=` 不是 `>`:ExpireAt 是秒级时间戳,`now == ExpireAt` 时有效期恰好归零,
// 按"还有效"处理会让客户端多试一次注定 403 的请求。
func (t *UploadToken) Expired() bool {
	return time.Now().Unix() >= t.ExpireAt
}

// Check 发凭证之前做一次自检。
//
// 字段漏填的凭证一旦发出去,前端会拿着空 host 去请求 OSS,报出来的错指向完全
// 错误的方向(看起来像 OSS 挂了)。在这里拦住,报的是本地能定位的错误。
func (t *UploadToken) Check() error {
	if !t.Mode.Valid() {
		return fmt.Errorf("oss: 上传模式 %q 非法", t.Mode)
	}
	switch t.Mode {
	case UploadModeDirect:
		if t.Key == "" {
			return errors.New("oss: direct 凭证缺 key")
		}
		if t.Host == "" || t.Policy == "" || t.Signature == "" || t.AccessKeyID == "" {
			return errors.New("oss: direct 凭证缺 host/policy/signature/access_key_id")
		}
	case UploadModeProxy:
		if t.Key == "" {
			return errors.New("oss: proxy 凭证缺 key")
		}
		if t.MaxSize <= 0 {
			return errors.New("oss: proxy 凭证缺 max_size(服务端靠它限流,漏了就是无限制上传)")
		}
	}
	return nil
}
