// MockProvider —— 内存桩实现
//
// 用途有两个,都不是"真跑起来":
//  1. 测试里注入,验证 service 层不依赖任何真实存储
//  2. 本地开发时 `file.provider: mock` 顶一顶,让不配存储也能起服务
//
// ⚠️ **它不是磁盘桩,是内存桩**:进程一重启数据全没。这跟"本地开发用 local
// provider"的定位不冲突 —— 两者用途不同,不要互相替代。
package oss

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MockProvider 桩实现
type MockProvider struct {
	mu   sync.RWMutex
	data map[string][]byte // key → 内容
	// now 可注入,让测试能控制 LastModified / 过期判定,不用真 sleep
	now func() time.Time
}

func NewMockProvider() *MockProvider {
	return &MockProvider{
		data: make(map[string][]byte),
		now:  time.Now,
	}
}

func (p *MockProvider) Name() string { return ProviderNameMock }

// SignUploadToken 返回一个 direct 模式的假凭证。
//
// ⚠️ **它会过 `UploadToken.Check()`**:桩实现如果只填一半字段,测试里报错会指向
// 凭证而不是桩,白查一轮。桩和真实现共用同一条自检,才说明真实现也自检。
func (p *MockProvider) SignUploadToken(ctx context.Context, key string, ttl time.Duration, maxSize int64) (*UploadToken, error) {
	if key == "" {
		return nil, &ErrInvalidKey{Reason: "key 为空"}
	}
	if !SafeKey(key) {
		return nil, &ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	if maxSize <= 0 {
		maxSize = 50 << 20
	}
	tok := &UploadToken{
		Mode:        UploadModeDirect,
		Key:         key,
		MaxSize:     maxSize,
		Host:        "https://mock.oss.local",
		Bucket:      "mock-bucket",
		AccessKeyID: "mock-key",
		Policy:      "mock-policy",
		Signature:   "mock-sig",
		ExpireAt:    p.now().Add(ttl).Unix(),
		Provider:    p.Name(),
	}
	if err := tok.Check(); err != nil {
		return nil, err
	}
	return tok, nil
}

// SignDownloadURL 返回一个看起来像样的假 URL。
//
// ⚠️ 真 provider 对私有 key 会签一个真能下载的临时 URL;这个 URL 访问不到任何
// 东西。**测试里断言"拿到 URL 就当能下载"会假绿** —— 要测下载就得改用 local
// provider 或真 OSS。
func (p *MockProvider) SignDownloadURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", &ErrInvalidKey{Reason: "key 为空"}
	}
	return "https://mock.oss.local/" + key + "?Expires=" +
		p.now().Add(ttl).Format("20060102"), nil
}

// Put 写入内存。
func (p *MockProvider) Put(ctx context.Context, key string, r io.Reader, contentType string) (*ObjectMeta, error) {
	if key == "" {
		return nil, &ErrInvalidKey{Reason: "key 为空"}
	}
	if !SafeKey(key) {
		return nil, &ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("oss mock: read %s: %w", key, err)
	}
	p.mu.Lock()
	p.data[key] = b
	p.mu.Unlock()
	_ = contentType // 桩不真的判类型,Stat 时按 key 后缀现算
	return p.Stat(ctx, key)
}

// Stat 取内存里的元信息。
func (p *MockProvider) Stat(ctx context.Context, key string) (*ObjectMeta, error) {
	if key == "" {
		return nil, &ErrInvalidKey{Reason: "key 为空"}
	}
	if !SafeKey(key) {
		return nil, &ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	p.mu.RLock()
	b, ok := p.data[key]
	p.mu.RUnlock()
	if !ok {
		return nil, &ErrNotFound{Key: key}
	}
	sum := md5.Sum(b)
	ct := mime.TypeByExtension(filepath.Ext(key))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return &ObjectMeta{
		Key:          key,
		Size:         int64(len(b)),
		MimeType:     ct,
		ETag:         hex.EncodeToString(sum[:]),
		LastModified: p.now(),
	}, nil
}

// Delete 删除对象。key 不存在不算错(幂等)。
func (p *MockProvider) Delete(ctx context.Context, keys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range keys {
		delete(p.data, k)
	}
	return nil
}

// DeletePrefix 删前缀下的全部对象,返回删掉的字节数。
func (p *MockProvider) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	if prefix == "" {
		return 0, &ErrInvalidKey{Reason: "prefix 为空"}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var total int64
	for k, v := range p.data {
		if strings.HasPrefix(k, prefix) {
			total += int64(len(v))
			delete(p.data, k)
		}
	}
	return total, nil
}

// List 列举前缀,按 key 排序 + 支持 marker 分页。
func (p *MockProvider) List(ctx context.Context, opt ListOption) (*ListResult, error) {
	if opt.Prefix != "" && !SafeKey(strings.TrimSuffix(opt.Prefix, "/")) {
		return nil, &ErrInvalidKey{Key: opt.Prefix, Reason: "prefix 未通过 SafeKey"}
	}
	p.mu.RLock()
	var keys []string
	for k := range p.data {
		if strings.HasPrefix(k, opt.Prefix) {
			keys = append(keys, k)
		}
	}
	p.mu.RUnlock()
	sort.Strings(keys)

	res := &ListResult{}
	for _, k := range keys {
		if opt.Marker != "" && k <= opt.Marker {
			continue
		}
		if opt.MaxKeys > 0 && len(res.Objects) >= opt.MaxKeys {
			res.IsTruncated = true
			break
		}
		m, err := p.Stat(ctx, k)
		if err != nil {
			return nil, err
		}
		res.Objects = append(res.Objects, *m)
	}
	if res.IsTruncated && len(res.Objects) > 0 {
		res.NextMarker = res.Objects[len(res.Objects)-1].Key
	}
	return res, nil
}

// Get 读回内容(见 oss.MetaReader)。
//
// ⚠️ **不在 Provider 主接口里** —— 它是可选能力(oss.MetaReader),
// 主接口只有 8 个方法,读字节不在其中(见 advanced.go 的 MetaReader 注释)。
//
// ⚠️ **签名跟 M133 之前不一样**:之前是 `Get(key) ([]byte, bool)`(桩特有),
// 现在统一成 `Get(ctx, key) ([]byte, error)` —— 因为 service 层要读回
// info.json,而它的 type-assert 认的是这个形状(见 service/folder_info.go)。
// 旧签名全仓零调用方(只有本文件里这一处定义),改成什么都不会影响别人。
func (p *MockProvider) Get(ctx context.Context, key string) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	b, ok := p.data[key]
	if !ok {
		return nil, &ErrNotFound{Key: key}
	}
	return bytes.Clone(b), nil
}

// SetClock 注入时间源(测试用)。
func (p *MockProvider) SetClock(f func() time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.now = f
}

// 编译期保证桩不会跟接口漂移。
//
// ⚠️ 没有这行的话,给 Provider 加方法时 mock 不会跟着补,而且**不会有人报错**
// —— 直到某个测试因为"mock 少一个方法"而 panic 在类型断言里,报错信息还指向
// 调用点而不是缺口。
var _ Provider = (*MockProvider)(nil)

// ⚠️ **MetaReader 的实现检查**(M133):以前没有这一行,而 M133 之前 mock 的
// Get 签名是 `Get(key) ([]byte, bool)`(桩特有),跟 service 层 type-assert
// 的形状完全对不上 —— 于是桩**看着有 Get**,实际断言必然失败。
// 签名统一之后,这一行才有意义:它把"桩的 Get 跟真 provider 的 Get 同形"
// 变成编译期事实,而不是约定。
var _ MetaReader = (*MockProvider)(nil)
