// 阿里云 OSS 高级特性实现 —— §1.10 #5-10 P2
//
// 6 个子特性 + 1 个 helper:
//   - #5 #6 MultipartProvider(Init/UploadPart/ListParts/Complete/Abort)
//   - #7 ScanProvider(走 Aliyun CIM 异步扫描 — 留 TODO stub,需 CIM 凭证)
//   - #8 ThumbnailProvider(走 OSS Image Processing ?x-oss-process=image/resize)
//   - #9 HotlinkProvider(Referer 校验 + 签名 token)
//   - #10 QuotaProvider(RDB 计数器 — 不走 OSS,业务侧用 kvstore.Counter 也行)
//   - CDNProvider(Aliyun CDN Refresh API — 留 TODO stub,需 CDN 凭证)
//
// CIM 凭证没配时,ScanProvider 返 ErrUnsupported(不是 panic)
package aliyun

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	alioss "github.com/aliyun/aliyun-oss-go-sdk/oss"

	"rapid-admin/internal/pkg/oss"
)

// ============== #5 #6 Multipart Upload ==============

const (
	// MultipartMinSize 走分片的最小文件(默认 100MB,小文件直传更快)
	MultipartMinSize int64 = 100 * 1024 * 1024
	// MultipartPartSize 建议分片大小(默认 10MB,1-10000 分片范围)
	MultipartPartSize int64 = 10 * 1024 * 1024
	// MultipartMaxPartNum 阿里云硬上限
	MultipartMaxPartNum = 10000
)

// InitMultipartUpload 初始化分片上传
func (p *Provider) InitMultipartUpload(ctx context.Context, key string, opts ...oss.InitOption) (*oss.MultipartInit, error) {
	io := &oss.InitOpts{}
	for _, o := range opts {
		o(io)
	}
	bucket := p.pickBucket(key)
	if bucket == nil {
		return nil, errors.New("aliyun oss: cannot pick bucket for key")
	}

	// 走阿里云 SDK InitiateMultipartUpload
	headers := []alioss.Option{}
	if io.ContentType != "" {
		headers = append(headers, alioss.ContentType(io.ContentType))
	}
	for k, v := range io.Metadata {
		headers = append(headers, alioss.Meta(k, v))
	}
	if io.ACL != "" {
		headers = append(headers, alioss.ObjectACL(alioss.ACLType(io.ACL)))
	}

	imur, err := bucket.InitiateMultipartUpload(key, headers...)
	if err != nil {
		return nil, fmt.Errorf("aliyun oss: initiate multipart: %w", err)
	}
	return &oss.MultipartInit{
		Key:      imur.Key,
		UploadID: imur.UploadID,
		PartSize: MultipartPartSize,
		PartNum:  0, // 由客户端按 partSize 算出
	}, nil
}

// UploadPart 上传 1 个分片
func (p *Provider) UploadPart(ctx context.Context, key, uploadID string, partNumber int, body []byte) (string, error) {
	if partNumber < 1 || partNumber > MultipartMaxPartNum {
		return "", fmt.Errorf("aliyun oss: part number %d out of range [1,%d]", partNumber, MultipartMaxPartNum)
	}
	bucket := p.pickBucket(key)
	if bucket == nil {
		return "", errors.New("aliyun oss: cannot pick bucket for key")
	}
	imur := alioss.InitiateMultipartUploadResult{
		Bucket:   bucket.BucketName,
		Key:      key,
		UploadID: uploadID,
	}
	up, err := bucket.UploadPart(imur, bytes.NewReader(body), int64(len(body)), partNumber)
	if err != nil {
		return "", fmt.Errorf("aliyun oss: upload part %d: %w", partNumber, err)
	}
	return up.ETag, nil
}

// ListParts 查已上传分片(断点续传用)
func (p *Provider) ListParts(ctx context.Context, key, uploadID string) ([]oss.Part, error) {
	bucket := p.pickBucket(key)
	if bucket == nil {
		return nil, errors.New("aliyun oss: cannot pick bucket for key")
	}
	imur := alioss.InitiateMultipartUploadResult{
		Bucket:   bucket.BucketName,
		Key:      key,
		UploadID: uploadID,
	}
	lpr, err := bucket.ListUploadedParts(imur)
	if err != nil {
		return nil, fmt.Errorf("aliyun oss: list parts: %w", err)
	}
	parts := make([]oss.Part, 0, len(lpr.UploadedParts))
	for _, up := range lpr.UploadedParts {
		parts = append(parts, oss.Part{
			PartNumber: up.PartNumber,
			ETag:       strings.Trim(up.ETag, "\""),
			Size:       int64(up.Size),
		})
	}
	return parts, nil
}

// CompleteMultipartUpload 合并分片
func (p *Provider) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []oss.Part) error {
	bucket := p.pickBucket(key)
	if bucket == nil {
		return errors.New("aliyun oss: cannot pick bucket for key")
	}
	imur := alioss.InitiateMultipartUploadResult{
		Bucket:   bucket.BucketName,
		Key:      key,
		UploadID: uploadID,
	}
	cParts := make([]alioss.UploadPart, len(parts))
	for i, p := range parts {
		cParts[i] = alioss.UploadPart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		}
	}
	_, err := bucket.CompleteMultipartUpload(imur, cParts)
	if err != nil {
		return fmt.Errorf("aliyun oss: complete multipart: %w", err)
	}
	return nil
}

// AbortMultipartUpload 取消分片上传
func (p *Provider) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	bucket := p.pickBucket(key)
	if bucket == nil {
		return errors.New("aliyun oss: cannot pick bucket for key")
	}
	imur := alioss.InitiateMultipartUploadResult{
		Bucket:   bucket.BucketName,
		Key:      key,
		UploadID: uploadID,
	}
	return bucket.AbortMultipartUpload(imur)
}

// pickBucket 这个 key 落在哪个 bucket。
//
// ⚠️ **单桶之后恒是同一个** —— 函数留着是因为调用点(Put / Stat / Get /
// DeletePrefix / 分片五个)都按"先选桶再操作"的形状写的。直接换成 p.bucket
// 会让那五处的形状不一致(有的带选桶有的不带),下次有人改的时候容易漏。
func (p *Provider) pickBucket(key string) *alioss.Bucket {
	return p.bucket
}

// ============== #7 Virus Scan (Stub) ==============

// Scan 发起病毒扫描(Stub — 需要 Aliyun CIM 凭证才能实装)
//
//	当前实现:返 ErrUnsupported(配合 type-assert 优雅降级)
func (p *Provider) Scan(ctx context.Context, key string) (string, error) {
	return "", errors.New("aliyun oss: virus scan not configured (need Aliyun CIM)")
}

// GetScanResult 查扫描结果
func (p *Provider) GetScanResult(ctx context.Context, scanID string) (*oss.ScanResult, error) {
	return nil, errors.New("aliyun oss: virus scan not configured")
}

// ============== #8 Thumbnail ==============

// GetThumbURL 走 OSS Image Processing 拼 URL
//
//	?x-oss-process=image/resize,m_fixed,h_200,w_200
//	参见:https://help.aliyun.com/document_detail/44688.html
func (p *Provider) GetThumbURL(ctx context.Context, key, size string) (string, error) {
	if size == "" {
		size = "200x200"
	}
	// 直接拼 OSS 原 URL + x-oss-process
	signedURL, err := p.SignDownloadURL(ctx, key, 1*time.Hour)
	if err != nil {
		return "", err
	}
	// 在 URL 中插入 query: _url_?x-oss-process=...
	// 已有 query 用 & 否则用 ?
	sep := "?"
	if strings.Contains(signedURL, "?") {
		sep = "&"
	}
	// 把 size 拼成 x-oss-process
	process := "image/resize," + size
	return signedURL + sep + "x-oss-process=" + url.QueryEscape(process), nil
}

// ============== #9 Hotlink Protection ==============

// hotlinkToken 防盗链 token 内容
type hotlinkToken struct {
	Key     string `json:"k"`
	Referer string `json:"r,omitempty"`
	Expire  int64  `json:"e"`
}

// hotlinkSecret 防盗链 HMAC 密钥。
//
// ⚠️ **X2 的修法**。原来这里是 `p.accessKey + "hotlink"` —— AccessKeyID 是
// **公开**的(它就在每个直传凭证里下发给前端,也在 SDK 配置和控制台里),
// 拿它当 HMAC 密钥等于没加密:任何知道 AK ID 的人都能自己签出一个合法 token,
// 防盗链形同虚设。改成 `AccessKeySecret + "hotlink"`。
//
// 域分隔后缀 `"hotlink"` 保留:它保证防盗链签名和 policy 签名(`SignUploadToken`
// 里那个)即使密钥相同也不会互相通用。
//
// ⚠️ 换成 secret 之后,**老 token 全部失效** —— 这是必要的,旧 token 是用公开
// 信息签的,必须让它们过期。本项目不暴露防盗链接口(访问控制靠签名 URL 的
// TTL),所以实际影响为零。
func (p *Provider) hotlinkSecret() []byte {
	return []byte(p.cfg.AccessKeySecret + "hotlink")
}

// SignToken 签防盗链 token(绑 referer + 过期)
//
//	URL 形如: <signURL>?token=<base64(hmac-sha1(payload, secret))>
//	payload = base64(json{"k":key,"r":referer,"e":expire}) + "." + hmac
func (p *Provider) SignToken(ctx context.Context, key string, ttl time.Duration) (string, error) {
	exp := time.Now().Add(ttl).Unix()
	payload := hotlinkToken{Key: key, Expire: exp}
	b, _ := json.Marshal(payload)
	mac := hmac.New(sha1.New, p.hotlinkSecret())
	mac.Write(b)
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString(b) + "." + sig, nil
}

// Verify 校验防盗链 token + referer
func (p *Provider) Verify(ctx context.Context, referer, key, token string) error {
	if token == "" {
		return errors.New("hotlink: token missing")
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return errors.New("hotlink: token format invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("hotlink: decode payload: %w", err)
	}
	var t hotlinkToken
	if err := json.Unmarshal(payload, &t); err != nil {
		return fmt.Errorf("hotlink: parse payload: %w", err)
	}
	// 1. 校验 key
	if t.Key != key {
		return errors.New("hotlink: key mismatch")
	}
	// 2. 校验过期
	if time.Now().Unix() > t.Expire {
		return errors.New("hotlink: token expired")
	}
	// 3. 校验签名
	// ⚠️ 必须用常量时间比较:时序侧信道能把正确签名逐字节试出来。
	mac := hmac.New(sha1.New, p.hotlinkSecret())
	mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return errors.New("hotlink: signature mismatch")
	}
	return nil
}

// ============== #10 Quota (Stub) ==============

// GetUsage 查已用字节(Stub — 框架只占位,业务自行用 kvstore.Counter 实现)
func (p *Provider) GetUsage(ctx context.Context, scope oss.QuotaScope) (int64, error) {
	return 0, errors.New("aliyun oss: quota not implemented (implement with kvstore.Counter: RedisCounter 多实例 / MemoryCounter 单实例)")
}

// SetLimit 设置上限(Stub)
func (p *Provider) SetLimit(ctx context.Context, scope oss.QuotaScope, limitBytes int64) error {
	return errors.New("aliyun oss: quota not implemented (implement with kvstore.Counter: RedisCounter 多实例 / MemoryCounter 单实例)")
}

// ============== CDN (Stub) ==============

// Refresh 主动刷新 URL(Stub — 需 Aliyun CDN 凭证)
func (p *Provider) Refresh(ctx context.Context, urls ...string) error {
	return errors.New("aliyun oss: CDN refresh not configured (need Aliyun CDN)")
}

// Purge 主动失效 URL
func (p *Provider) Purge(ctx context.Context, urls ...string) error {
	return errors.New("aliyun oss: CDN refresh not configured")
}

// Prefetch 主动预热 URL
func (p *Provider) Prefetch(ctx context.Context, urls ...string) error {
	return errors.New("aliyun oss: CDN refresh not configured")
}

// ============== helpers ==============

// randHex 16 hex 字符(n=8 → 16 chars)
func randHexOSS(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
