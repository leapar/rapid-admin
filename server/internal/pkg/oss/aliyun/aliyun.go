// Package aliyun 阿里云 OSS provider — §1.10 #3 P0
//
// 实现 oss.Provider 接口:
//   - SignUploadToken:生成 PostObject 直传凭证(后端签 policy+sig,前端不接触 secret)
//   - SignDownloadURL:私有 bucket 临时下载 URL(GET presigned)
//   - Delete:删除对象
//
// PostObject 直传流程:
//
// 1. 后端用 AccessKeySecret 签一个 base64 编码的 policy,policy 里有两条条件:
//
//	content-length-range  →  单文件字节上限(防单请求 OOM / 防超配额)
//	eq $key               →  **精确**匹配服务端决定的那一个 key
//
// 2. 前端拿到 {mode, key, host, policy, signature, expire_at} 后,用 form-data
// POST 到 host —— 不需要 SDK,任何 form POST 都能上传。
//
// 安全要点:
//
//   - 永远不要把 AccessSecret 返回给前端(只在后端签名用)
//   - policy 设 expiration ≤ 15min(标准实践,防泄漏窗口)
//   - eq $key 是 M78 起的写法。⚠️ 早先用 `starts-with $key`,那挡不住往目标 key
//     后面接后缀(`a.pdf` → `a.pdf.evil.exe`),客户端能往同一个文件夹里塞任意
//     文件。见 SignUploadToken 的注释。
//
// 依赖: github.com/aliyun/aliyun-oss-go-sdk/oss v3.x
package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	alioss "github.com/aliyun/aliyun-oss-go-sdk/oss"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/oss"
)

// Provider 阿里云 OSS provider
type Provider struct {
	cfg alioss.Config
	// 单 bucket —— **所有文件都在这一个桶里**。
	//
	// ⚠️ 以前是双桶(admin 私有 + public 公共读),按 key 前缀分流。那套设计的
	// 代价是:每个 provider 操作都要先判"这个 key 属于哪个桶",于是 SignDownloadURL
	// / Delete / List / DeletePrefix 各写一遍分流,前缀一改就分叉。
	// 而"公开/私有"在本平台**不是访问控制** —— 私密只意味着外部用户猜不到 key。
	// 所以现在只有一个桶,直链靠 bucket 自己的公共读 ACL。
	bucket     *alioss.Bucket
	bucketName string
	accessKey  string
	// publicHost 拼直链用的**外网** host(不含 scheme)。
	//
	// ⚠️ **它跟 SDK 客户端用的 endpoint 不是同一个** —— 内网 endpoint
	// (`oss-cn-xxx-internal`)给本机调 OSS 用,直链是给**浏览器**看的,
	// 浏览器在公网,给它内网地址它访问不到。
	publicHost string
	// cdnPrefix 拼直链用的 CDN 前缀(带 scheme,不带尾斜杠)。空 = 不用 CDN。
	cdnPrefix string
}

// Config provider 配置
type Config struct {
	// Endpoint **外网** endpoint,如 `oss-cn-shenzhen.aliyuncs.com`。
	//
	// ⚠️ **带不带 scheme 都认**(`https://` 前缀会被剥掉)—— 配置文件里两种
	// 写法都常见,而直链是**拼**出来的,不先规整就会拼出
	// `https://bucket.https://oss-cn-...` 这种废 URL(每个链接都坏,
	// 而且报的是"域名解析失败",指向不了真因)。
	//
	// ⚠️ **它只用于拼直链**(给浏览器看)。SDK 客户端用哪个 endpoint 由
	// `EndpointPrivate` 决定,见下面。
	Endpoint string
	// EndpointPrivate **内网** endpoint,如 `oss-cn-shenzhen-internal.aliyuncs.com`。
	//
	// 同一地域的阿里云 ECS 上用它更快(流量不走公网)。**只给 SDK 客户端用**,
	// 不参与拼直链 —— 直链是给公网上的浏览器看的。
	//
	// ⚠️ **填了不代表一定用得上**:开发机 / 非阿里云的机器根本解析不了
	// `-internal` 域名(硬用就连不上)。所以 New 会先探一次 DNS,解析不了
	// 就自动回落到外网 endpoint,并把选了哪个打进日志。
	EndpointPrivate string
	AccessKey       string
	AccessSecret    string
	// Bucket 存文件的桶 —— **只有一个**。
	//
	// ⚠️ **这个桶必须是公共读(public-read)**。直链是拼出来的、不带签名的
	// 稳定 URL(见 SignDownloadURL),私有桶上它会 403。
	// 直链稳定是刻意的:前端把它塞进 <img src>,签名 URL 一过期页面上的图就裂。
	Bucket string
	// CDNDomain 可选的 CDN 域名,如 `https://f.3dman.cn`。
	//
	// ⚠️ **带不带 scheme 都认**(不带就补 https)。它替换的是"直链的域名部分",
	// 路径仍然是 key —— 所以 CDN 必须真的回源到这个桶,否则直链全是 404。
	CDNDomain string
	UseHTTPS  bool // 默认 true
}

// New 构造阿里云 provider(endpoint 必填,AccessKey/Secret 必填)
func New(cfg Config) (*Provider, error) {
	if cfg.Endpoint == "" || cfg.AccessKey == "" || cfg.AccessSecret == "" {
		return nil, fmt.Errorf("aliyun oss: endpoint/access_key/access_secret required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("aliyun oss: bucket required")
	}

	// ⚠️ **SDK 客户端用内网 endpoint(能连上的话),直链用外网** —— 两个用途
	//   的"endpoint"不是同一个东西。内网连不上(开发机解析不了 -internal
	//   域名)就回落外网,别让"配了内网"变成"启动即 Fatal"。
	clientEndpoint, usedPrivate := pickClientEndpoint(cfg.Endpoint, cfg.EndpointPrivate, probeEndpointReachable)
	if usedPrivate {
		logger.L().Info("aliyun oss: SDK 客户端走内网 endpoint(直链仍用外网)",
			zap.String("endpoint_private", clientEndpoint))
	} else if strings.TrimSpace(cfg.EndpointPrivate) != "" {
		logger.L().Warn("aliyun oss: 内网 endpoint 连不上(不在同地域 VPC 内?),已回落外网",
			zap.String("endpoint_private", cfg.EndpointPrivate),
			zap.String("fallback", cfg.Endpoint))
	}

	cli, err := alioss.New(clientEndpoint, cfg.AccessKey, cfg.AccessSecret,
		alioss.SecurityToken(""), // 不使用 STS,长 Key 签名
		alioss.Timeout(10, 60),   // conn 10s / read 60s
	)
	if err != nil {
		return nil, fmt.Errorf("aliyun oss: new client: %w", err)
	}
	bucket, err := cli.Bucket(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("aliyun oss: bucket %s: %w", cfg.Bucket, err)
	}

	return &Provider{
		cfg:        alioss.Config{Endpoint: clientEndpoint, AccessKeyID: cfg.AccessKey, AccessKeySecret: cfg.AccessSecret},
		bucket:     bucket,
		bucketName: cfg.Bucket,
		accessKey:  cfg.AccessKey,
		publicHost: normalizeEndpoint(cfg.Endpoint),
		cdnPrefix:  normalizeCDN(cfg.CDNDomain),
	}, nil
}

// pickClientEndpoint 选 SDK 客户端该连哪个 endpoint。
//
// ⚠️ **内网优先生效的前提是"这台机器真的连得上它"** —— `-internal` 域名
// 指向 VPC 内部的 100.64.0.0/10 网段,**在公网上也能解析**(实测:开发机上
// nslookup 得到 100.118.78.1~4),但**路由不通**,连过去直接超时。
// 硬用(不探测)的后果是**每一次 OSS 操作都等满超时**。
//
// ⚠️ **返回值一定带 scheme**。SDK 的 urlMaker 在 endpoint 没写 scheme 时
// **默认按 http 处理**(conn.go:InitExt),于是"配置里没写 https"就等于
// "悄悄降级成明文传输" —— 而且没有任何日志。所以这里统一补齐。
//
// probe 可注入,便于测试。
func pickClientEndpoint(pub, priv string, probe func(string) bool) (string, bool) {
	priv = strings.TrimSpace(priv)
	if priv == "" {
		return ensureScheme(pub), false
	}
	if normalizeEndpoint(priv) == "" {
		return ensureScheme(pub), false
	}
	if !probe(priv) {
		return ensureScheme(pub), false
	}
	return ensureScheme(priv), true
}

// endpointProbeTimeout 连通性探测的**总预算**(DNS + TCP 握手共用)。
//
// ⚠️ **别调大**:它是**启动路径上的固定延迟** —— 内网 endpoint 连不上时
// (公网机器的常态)每一台都要等满,而它只是"决定用哪个 endpoint"。
//
// ⚠️ **为什么从 1500ms 砍到 300ms(2026-10-31)**:判据是"连得上没有",
// 而**误判的代价是不对称的** ——
// · 误判"连不上"(假阴性)⇒ 改用外网 endpoint ⇒ **只是慢**(流量走公网),
// 不出错、不丢文件、随时能改回来;
// · 误判"连得上"(假阳性)⇒ 每次 OSS 操作都等满 SDK 超时 ⇒ **文件功能整体卡死**。
// ⇒ 宁可假阴性。而**真在 VPC 里时**,内网 TCP 握手是 ~1ms,300ms 有几十倍余量。
//
// ⚠️ 真机实测:开发机(不在 VPC)上探 `-internal` 域名,DNS 解析**是成功的**
// (它指向 100.64.0.0/10 的 VPC 内部地址),但**路由不通** ⇒ TCP 握手一直
// 等到超时。1500ms 就是这么来的 —— **每次启动白等一秒半**。
const endpointProbeTimeout = 300 * time.Millisecond

// probeEndpointReachable 探 endpoint 是不是**真的连得上**。
//
// ⚠️ **只探 DNS 是不够的 —— 这是真机跑出来才发现的**。
// 阿里云的 `-internal` 域名在**公网上也能解析**(它指向 VPC 内部的
// 100.64.0.0/10 网段,那批地址公网可达性为零)。实测开发机:
//
//	nslookup oss-cn-shenzhen-internal.aliyuncs.com
//	  Address: 100.118.78.2   ← 解析成功
//	dial tcp 100.118.78.2:443 → i/o timeout   ← 连不上
//
// 所以"DNS 解析成功"完全不代表"能用"。只探 DNS 的后果是:启动日志说
// "走内网",然后**每一次 OSS 操作都等满 60 秒超时**,而日志里看不出原因。
// 必须探到 TCP 层。
//
// ⚠️ 探的是 **TCP 连通性**,不是"OSS 服务是否正常" —— 只要握手成功就算通,
// 免得启动时被 OSS 的一次抖动卡住。
func probeEndpointReachable(ep string) bool {
	host := normalizeEndpoint(ep)
	if host == "" {
		return false
	}
	h, port := host, "443"
	if hh, pp, err := net.SplitHostPort(host); err == nil {
		h, port = hh, pp
	} else if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ep)), "http://") {
		port = "80"
	}
	// ⚠️ **DNS 和 TCP 握手共用同一个预算** —— 分开各给一份的话,
	//   "DNS 慢 + 握手慢"会叠成两倍启动延迟,而这里只是"选个 endpoint"。
	deadline := time.Now().Add(endpointProbeTimeout)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	// ⚠️ **必须用带 context 的 LookupHost** —— 默认的 net.LookupHost
	//   **不受超时约束**(它走解析器自己的超时,慢机器上能卡好几秒)。
	if _, err := net.DefaultResolver.LookupHost(ctx, h); err != nil {
		return false
	}
	// ⚠️ DNS 已经把预算吃掉的话就别再拨了 —— 拨了也是等满超时。
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(h, port), remaining)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ensureScheme 给 endpoint 补上 scheme —— **已有 scheme 不动**。
//
// ⚠️ 只补 `https://`,但**不覆盖已有的 `http://`** ——
//   · 没写 scheme = 想要加密(默认值给最安全的那个)
//   · 写了 http = 用户明确要明文(比如内网自建网关只支持 http)
// 覆盖掉就是在偷偷改用户配的行为。
func ensureScheme(ep string) string {
	ep = strings.TrimSpace(ep)
	if ep == "" {
		return ep
	}
	if strings.Contains(ep, "://") {
		return strings.TrimSuffix(ep, "/")
	}
	return "https://" + strings.TrimSuffix(ep, "/")
}

// normalizeEndpoint 把 endpoint 规整成**裸 host**(不含 scheme、不含尾斜杠)。
//
// ⚠️ 为什么要规整而不是"要求配置里必须写对":配置是人手写的,两种写法
// (`oss-cn-x.aliyuncs.com` / `https://oss-cn-x.aliyuncs.com`)都很常见,
// 而**直链是拼出来的** —— 不规整就会拼出
// `https://bucket.https://oss-cn-x.aliyuncs.com`,每个链接都坏,
// 而且报的是"域名解析失败",指向不了真因。
func normalizeEndpoint(ep string) string {
	ep = strings.TrimSpace(ep)
	ep = strings.TrimSuffix(ep, "/")
	if i := strings.Index(ep, "://"); i >= 0 {
		ep = ep[i+3:]
	}
	return strings.Trim(ep, "/")
}

// normalizeCDN 把 CDN 域名规整成"能直接拼 key 的前缀"(带 scheme、不带尾斜杠)。
//
// ⚠️ **不能像 normalizeEndpoint 那样把 scheme 剥掉** —— CDN 前缀是要
// 原样出现在 URL 里的:`https://f.3dman.cn/...`。剥掉再补 https 的话,
// 用户配了 `http://` 就会被改成 https(行为被偷偷改掉)。
// 没配 scheme 就补 https(与 endpoint 的默认一致)。
func normalizeCDN(cdn string) string {
	cdn = strings.TrimSpace(cdn)
	if cdn == "" {
		return ""
	}
	cdn = strings.TrimSuffix(cdn, "/")
	if !strings.Contains(cdn, "://") {
		cdn = "https://" + cdn
	}
	return cdn
}

// bucketHost 计算直链用的 bucket 主机名(https://{bucket}.{host})
func (p *Provider) bucketHost(bucket string) string {
	// OSS endpoint 形如 oss-cn-hangzhou.aliyuncs.com
	// bucket 形如 my-bucket
	// 标准 URL: https://my-bucket.oss-cn-hangzhou.aliyuncs.com
	return fmt.Sprintf("https://%s.%s", bucket, p.publicHost)
}

// encodeKeyPath 把 key 按段 percent-encode,拼进 URL 路径用。
//
// ⚠️ **必须逐段编码,不能整条编码一次** —— 整条编码会把 `/` 也变成 `%2F`,
// 那条路径在 OSS 上根本不存在。
//
// ⚠️ **不能不编码** —— key 里可能有中文和空格(oss.SafeKey 允许 UTF-8)。
//   不编码的话中文直链在"复制到聊天工具/邮件"这类场景里会被截断,
//   空格会让 URL 解析提前结束。local provider 一直是逐段编码的
//   (见 local.SignDownloadURL),两个 provider 的直链形状必须一致。
func encodeKeyPath(key string) string {
	parts := strings.Split(key, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

/* ====================== SignUploadToken ====================== */

// Name provider 名
//
// ⚠️ 用 oss.ProviderNameOSS 常量,不要写字面量 —— service 层要按这个名字
// 判断"文件实际在哪"(model.FileStorage),两边必须对得上。
func (p *Provider) Name() string { return oss.ProviderNameOSS }

// SignUploadToken 生成直传 token(PostObject 表单)
//   - key: 服务端决定的**完整对象 key**(M77 起不再是目录前缀)
//   - ttl: token 有效期(建议 5-15min)
//   - maxSize: 单文件最大字节(content-length-range 上限)
//
// ⚠️ **key 条件用 `eq` 不是 `starts-with`,这是 M78 的重点。**
// `starts-with $key = rapid-admin/1/asset/2/a.pdf` 挡不住
// `rapid-admin/1/asset/2/a.pdf.evil.exe`
// —— 客户端可以往目标 key 后面**任意接后缀**,往同一个目录里塞任意文件。
//
// 🔍 **曾经误判过**:M77 的注释写的是"PostObject 表单天然按前缀约束,要换预签名
// PUT 才能精确到 key"。**这是错的,已核实更正** —— PostObject policy 的
// conditions 支持 `eq`/`starts-with`/`in`/`not-in` 四种匹配方式,`eq` 是官方
// 标准写法。而且换成预签名 PUT 是**净损失**:预签名 URL 没有
// `content-length-range`,等于把单文件大小上限这个闸门扔掉。留在 PostObject
// 把 `starts-with` 换成 `eq`,key 精度和大小限制两个闸门同时保住。
// 来源:help.aliyun.com/document_detail/116277.htm(PostObject API)
func (p *Provider) SignUploadToken(ctx context.Context, key string, ttl time.Duration, maxSize int64) (*oss.UploadToken, error) {
	if key == "" {
		return nil, fmt.Errorf("aliyun oss: key required")
	}
	if !oss.SafeKey(key) {
		return nil, &oss.ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	if maxSize <= 0 {
		maxSize = 50 << 20 // 默认 50MB
	}
	expire := time.Now().Add(ttl)

	// 1. 构造 policy
	policy := map[string]any{
		"expiration": expire.UTC().Format("2006-01-02T15:04:05.000Z"),
		"conditions": []any{
			// 内容长度范围(防单请求 OOM / 防超配额)
			[]string{"content-length-range", "0", fmt.Sprintf("%d", maxSize)},
			// key **精确**匹配 —— 凭证只对这一个对象生效,接后缀也不行
			[]string{"eq", "$key", key},
		},
	}
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("aliyun oss: marshal policy: %w", err)
	}
	policyB64 := base64.StdEncoding.EncodeToString(policyJSON)

	// 2. HMAC-SHA1 签名
	h := hmac.New(sha1.New, []byte(p.cfg.AccessKeySecret))
	h.Write([]byte(policyB64))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	tok := &oss.UploadToken{
		Mode:        oss.UploadModeDirect,
		Key:         key,
		MaxSize:     maxSize,
		Host:        p.bucketHost(p.bucketName),
		Bucket:      p.bucketName,
		AccessKeyID: p.accessKey,
		Policy:      policyB64,
		Signature:   signature,
		ExpireAt:    expire.Unix(),
		Provider:    p.Name(),
	}
	if err := tok.Check(); err != nil {
		return nil, err
	}
	return tok, nil
}

/* ====================== SignDownloadURL ====================== */

// SignDownloadURL 返**稳定的直链**(不签名、不带过期时间)。
//
// ⚠️ **为什么不签临时 URL**:前端把这个 URL 塞进 <img src>,签名 URL 一过期
// 页面上的图就裂 —— 而文件列表页可能开一整天。直链稳定是刻意的。
//
// ⚠️ **代价是这个 bucket 必须是公共读**:私有桶上这个 URL 会 403。
// 本平台"私密"只意味着**外部用户猜不到 key**(uid 是雪花 id、对象名带随机
// 后缀),不是靠 bucket ACL 挡 —— 所以单桶 + 公共读是自洽的。
//
// ⚠️ ttl 参数在这里**不用**:没有过期时间的东西不需要 ttl。签名它是为了
// 满足 Provider 接口的统一形状,不是要生效。
func (p *Provider) SignDownloadURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", fmt.Errorf("aliyun oss: key required")
	}
	if !oss.SafeKey(key) {
		return "", &oss.ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	// ⚠️ **CDN 前缀已经是"带 scheme 的完整前缀"**(normalizeCDN 处理过)。
	//   以前这里写死 `fmt.Sprintf("https://%s/%s", p.cdnDomain, key)`,
	//   而配置里常见的写法是 `https://f.3dman.cn`(带 scheme)⇒ 拼出
	//   `https://https://f.3dman.cn/...` ⇒ **每一条 OSS 直链都是废的**,
	//   而且报的是"域名解析失败",看不出真因是拼串拼重了。
	prefix := p.cdnPrefix
	if prefix == "" {
		prefix = p.bucketHost(p.bucketName)
	}
	return prefix + "/" + encodeKeyPath(key), nil
}

/* ====================== Delete ====================== */

// Delete 删除对象(批量)
func (p *Provider) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	// ⚠️⚠️ **key 必须原样传给 OSS,不能 percent-encode**。
	//
	// DeleteObjects 的 key 放在 **XML 请求体**里(不是 URL 路径),OSS 按
	// 解码后的原文匹配对象。而 `url.PathEscape` 会把 `/` 也编码成 `%2F`
	// (`rapid-admin/1/asset/2/a.pdf` → `rapid-admin%2F1%2Fasset%2F2%2Fa.pdf`)
	// ⇒ OSS 收到的是一个**不存在的 key**,而"删不存在的 key"在 OSS 看来是
	// **成功**(幂等)⇒ **返回 nil,但对象根本没被删**。
	//
	// 症状:DB 那一行软删了、列表里看不见了、DB 记的配额也退了,
	// 但**对象还在桶里继续占存储费**,而且桶里多出来的文件没人知道是谁的。
	// ⚠️ 同仓的 `DeletePrefix` 用的是原始 key,所以它是对的 —— 两个删除
	// 路径行为不一致,这就是"删单个删不掉、删整棵能删干净"的原因。
	if _, err := p.bucket.DeleteObjects(keys); err != nil {
		logger.L().Error("aliyun oss: delete keys failed",
			zap.Strings("keys", keys), zap.Error(err))
		return err
	}
	return nil
}

/* ====================== Put / Stat / List / DeletePrefix ====================== */

// Put 服务端直写。
//
// ⚠️ OSS 的 PutObject 本身就是原子的(要么对象存在且完整,要么不存在),
// 所以这里**不需要**像 local 那样"临时文件 + rename" —— 那套逻辑是本地磁盘
// 特有的,因为 POSIX 写文件不是原子的。两条路径的目标一样:不让调用方看到一个
// "存在但只有一半"的对象。
func (p *Provider) Put(ctx context.Context, key string, r io.Reader, contentType string) (*oss.ObjectMeta, error) {
	if key == "" {
		return nil, &oss.ErrInvalidKey{Key: key, Reason: "key 为空"}
	}
	if !oss.SafeKey(key) {
		return nil, &oss.ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(key))
	}
	opts := []alioss.Option{alioss.ContentType(contentType)}
	// ⚠️ 不设 ACL:对象的读写权限由 bucket ACL 决定(单桶 + 公共读)。
	bucket := p.pickBucket(key)
	if err := bucket.PutObject(key, r, opts...); err != nil {
		return nil, fmt.Errorf("aliyun oss: put %s: %w", key, err)
	}
	// 写完再 Stat 一次拿元信息 —— PutObject 不返回 meta,而 service 层
	// 落库需要 size / etag。与其自己猜,不如多一次往返问权威来源。
	return p.Stat(ctx, key)
}

// Stat 取对象元信息。
//
// ⚠️ **不存在必须返回 `IsNotFound(err) == true`**:service 层靠它把 404 和 500
// 分开。alioss 没有可 `errors.As` 的哨兵,只能在这里翻译成 `oss.ErrNotFound`,
// 不能让每个调用点各写一遍字符串匹配。
func (p *Provider) Stat(ctx context.Context, key string) (*oss.ObjectMeta, error) {
	if key == "" {
		return nil, &oss.ErrInvalidKey{Key: key, Reason: "key 为空"}
	}
	if !oss.SafeKey(key) {
		return nil, &oss.ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	// ⚠️ SDK 的 GetObjectDetailedMeta 返回的是 **http.Header**,不是结构体 ——
	// 所以 size / etag / mtime 全要从 header 里手抠,下面几行是必须的,不是偷懒。
	h, err := p.pickBucket(key).GetObjectDetailedMeta(key)
	if err != nil {
		if isOSSNotFound(err) {
			return nil, &oss.ErrNotFound{Key: key}
		}
		return nil, fmt.Errorf("aliyun oss: stat %s: %w", key, err)
	}
	meta := &oss.ObjectMeta{
		Key:          key,
		Size:         parseContentLength(h.Get("Content-Length")),
		MimeType:     h.Get("Content-Type"),
		ETag:         trimETagQuotes(h.Get("ETag")),
		LastModified: parseHTTPTime(h.Get("Last-Modified")),
	}
	if meta.MimeType == "" {
		meta.MimeType = mime.TypeByExtension(filepath.Ext(key))
	}
	return meta, nil
}

// List 列举前缀下的对象 + 虚拟目录。
//
// ⚠️ 单桶之后不存在"漏掉另一半"的问题 —— 但**合并/重排那套逻辑留着**:
// 删掉它会让 diff 变大,而且它是"前缀列举"正确性的一部分(翻页游标)。
func (p *Provider) List(ctx context.Context, opt oss.ListOption) (*oss.ListResult, error) {
	prefix := opt.Prefix
	if prefix != "" && !oss.SafeKey(strings.TrimSuffix(prefix, "/")) {
		return nil, &oss.ErrInvalidKey{Key: prefix, Reason: "prefix 未通过 SafeKey"}
	}
	opts := make([]alioss.Option, 0, 4)
	if prefix != "" {
		opts = append(opts, alioss.Prefix(prefix))
	}
	if opt.IncludeSelf {
		// Delimiter="/" 让 SDK 把"目录"折叠成 CommonPrefixes 而不是列进 Objects
		opts = append(opts, alioss.Delimiter("/"))
	}
	if opt.Marker != "" {
		opts = append(opts, alioss.Marker(opt.Marker))
	}
	// ⚠️ MaxKeys<=0 时**不能**传 MaxKeys(0):SDK 会把它当成"要 0 条"发给 OSS,
	// 于是永远返回空列表。交给 OSS 用它的默认 1000。
	if opt.MaxKeys > 0 {
		opts = append(opts, alioss.MaxKeys(opt.MaxKeys))
	}

	out := &oss.ListResult{}
	var all []oss.ObjectMeta
	for _, b := range p.bucketsForPrefix(prefix) {
		res, err := b.ListObjects(opts...)
		if err != nil {
			return nil, fmt.Errorf("aliyun oss: list %s: %w", prefix, err)
		}
		for _, o := range res.Objects {
			ct := mime.TypeByExtension(filepath.Ext(o.Key))
			if ct == "" {
				ct = "application/octet-stream"
			}
			all = append(all, oss.ObjectMeta{
				Key:          o.Key,
				Size:         o.Size,
				MimeType:     ct,
				ETag:         trimETagQuotes(o.ETag),
				LastModified: o.LastModified,
			})
		}
		out.CommonPrefixes = append(out.CommonPrefixes, res.CommonPrefixes...)
		if res.IsTruncated {
			out.IsTruncated = true
			// OSS 自己给的 NextMarker 比"最后一个 key"准 —— 开了 Delimiter 时
			// 最后一个 key 之前可能还有没列出来的 CommonPrefixes
			out.NextMarker = res.NextMarker
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Key < all[j].Key })
	out.Objects = all
	return out, nil
}

// DeletePrefix 删掉前缀下的全部对象,返回删掉的字节数。
//
// ⚠️ **必须循环翻页**:ListObjects 单页上限 1000,超了 OSS 自己截断。只删
// 第一页 = "删了 1000 个然后假装删干净了",剩下的静静躺在存储里继续占配额。
// DeleteObjects 单批也是 1000 上限,所以按页删正好对齐。
//
// ⚠️ **游标必须严格前进**,否则 OSS 返回同一页时死循环。下面 `next <= marker`
// 那道判断是兜底:翻页条件全由对端控制,不能假设它会前进。
func (p *Provider) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	if prefix == "" {
		return 0, &oss.ErrInvalidKey{Key: prefix, Reason: "prefix 为空"}
	}
	var total int64
	for _, b := range p.bucketsForPrefix(prefix) {
		marker := ""
		for {
			opts := make([]alioss.Option, 0, 3)
			opts = append(opts, alioss.Prefix(prefix), alioss.MaxKeys(1000))
			if marker != "" {
				opts = append(opts, alioss.Marker(marker))
			}
			res, err := b.ListObjects(opts...)
			if err != nil {
				return total, fmt.Errorf("aliyun oss: list prefix %s: %w", prefix, err)
			}
			if len(res.Objects) == 0 {
				break
			}
			keys := make([]string, 0, len(res.Objects))
			for _, o := range res.Objects {
				total += o.Size
				keys = append(keys, o.Key)
			}
			if _, err := b.DeleteObjects(keys); err != nil {
				return total, fmt.Errorf("aliyun oss: delete prefix %s: %w", prefix, err)
			}
			if !res.IsTruncated {
				break
			}
			// 优先用 OSS 的 NextMarker;它为空时退回最后一个 key
			next := res.NextMarker
			if next == "" {
				next = res.Objects[len(res.Objects)-1].Key
			}
			if next <= marker {
				logger.L().Warn("aliyun oss: DeletePrefix 游标未前进,提前中止",
					zap.String("prefix", prefix), zap.String("marker", marker), zap.String("next", next))
				break
			}
			marker = next
		}
	}
	return total, nil
}

/* ====================== 小元数据回读 ====================== */

// maxGetBytes Get 的字节上限(8MB),与 local 一致。
//
// ⚠️ Get 是给"小元数据"用的(当前唯一用途:info.json,几 KB)。
// 这个上限是防手滑的闸门 —— 拿它读大文件会把带宽拉回应用服务器,
// 那正是"直传"要避免的事(见上面 ContentProvider 的注释)。
const maxGetBytes = 8 << 20

// Get 读回对象的字节(小元数据专用,见 oss.MetaReader)。
func (p *Provider) Get(ctx context.Context, key string) ([]byte, error) {
	if key == "" {
		return nil, &oss.ErrInvalidKey{Key: key, Reason: "key 为空"}
	}
	if !oss.SafeKey(key) {
		return nil, &oss.ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	rc, err := p.pickBucket(key).GetObject(key)
	if err != nil {
		if isOSSNotFound(err) {
			return nil, &oss.ErrNotFound{Key: key}
		}
		return nil, fmt.Errorf("aliyun oss: get %s: %w", key, err)
	}
	// ⚠️ Close 的错误故意丢掉:读已经成功了,关连接的失败改不了结果,
	//   而把它返出去会让"读成功"变成"读失败"(用户看到红色 toast,
	//   文件却确实读出来了)。errcheck 也要过 —— 所以显式 `_ =`。
	defer func() { _ = rc.Close() }()
	// ⚠️ **限长读**:多读 1 字节用来判"超限",而不是先把整个对象拉进内存
	//   再判大小 —— 那样上限就只是"事后报个错",拦不住那次拉取。
	b, err := io.ReadAll(io.LimitReader(rc, maxGetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("aliyun oss: 读取 %s 失败: %w", key, err)
	}
	if len(b) > maxGetBytes {
		return nil, fmt.Errorf("aliyun oss: 对象 %q 超过 Get 上限(%d 字节)", key, int64(maxGetBytes))
	}
	return b, nil
}

/* ====================== 辅助 ====================== */

// bucketsForPrefix 列举 / 前缀删除时该扫哪些 bucket。
//
// ⚠️ **单桶之后恒为一个** —— 留着这个函数是因为调用点(List / DeletePrefix)
// 按"扫哪几个桶再合并"的结构写的,直接塞一个 bucket 进去会让那两处的
// 合并/重排逻辑变成死代码(而重排逻辑删错了会让翻页游标跳)。
func (p *Provider) bucketsForPrefix(prefix string) []*alioss.Bucket {
	if p.bucket == nil {
		return nil
	}
	return []*alioss.Bucket{p.bucket}
}

// isOSSNotFound 判断 alioss 的 404。
//
// ⚠️ alioss 用字符串型错误(`alioss.ServiceError`),没有可 `errors.As` 的哨兵,
// 只能翻译。**匹配错误码而不是文案**:文案随 SDK 版本变,`Code` 不会。
// StatusCode 也看一眼,因为某些边缘情况(如 bucket 被删)SDK 只填状态码。
func isOSSNotFound(err error) bool {
	var se alioss.ServiceError
	if errors.As(err, &se) {
		switch se.Code {
		case "NoSuchKey", "NoSuchBucket", "NotFound", "NoSuchWebsiteConfiguration":
			return true
		}
		return se.StatusCode == 404
	}
	return false
}

// parseContentLength 解析 Content-Length。非法/缺失一律当 0。
//
// ⚠️ 解析失败**不能报错**:Stat 要么完整成功要么完整失败,一个空对象的
// Content-Length 缺失不该让整个 Stat 挂掉 —— 那个 0 会被上层当成"空文件"
// 记进库,比报一个无从查证的错更容易定位。
func parseContentLength(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseHTTPTime 解析 Last-Modified。RFC1123 格式,失败返回零值。
func parseHTTPTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := http.ParseTime(s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// trimETag 去掉 OSS 返回的 ETag 外层引号。
//
// ⚠️ 不去的话,库里存的是 `"abc123"`,客户端从 Stat 拿到的也是 `"abc123"`,
// 两边对得上 —— 但拿去跟任何以裸 hex 为准的地方比(本地 provider 的 md5、
// 日志、跨 provider 对账)就会莫名其妙地不等。统一裸值。
func trimETagQuotes(etag string) string {
	return strings.Trim(etag, `"`)
}

// ⚠️ `SafeKey` 已移到 `oss` 包顶层(`path.go`),语义同时放宽以支持中文文件名。
// 详见 oss.SafeKey 的注释。留在本包会和新版本行为不一致,不如删干净。

// EncodeKey URL 编码 key(用于 path 里)
func EncodeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// 实现 interface 检查
var _ oss.Provider = (*Provider)(nil)

// ⚠️ **MetaReader 的实现检查也放在这里**(M133)—— 以前没有这一行,
// 于是"三家 provider 一个都没实现 Get"这件事没有任何编译期信号,
// 而 service 层的 readFolderInfo 靠 type-assert 静默降级成"返空串"
// (见 service/folder_info.go)。加了这一行,以后谁把 Get 改名或删掉,
// 编译期就会红。
var _ oss.MetaReader = (*Provider)(nil)

// 防止 hmac/sha1 在某些 build tag 下未用警告
var _ = sort.Strings
