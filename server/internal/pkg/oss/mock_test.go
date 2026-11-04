// M133 桩 provider 的 MetaReader(Get)—— 形状必须和真 provider 一致
//
// ⚠️ **桩的 Get 以前是 `Get(key) ([]byte, bool)`**(桩特有签名),而 service
// 层 type-assert 的是 `Get(ctx, key) ([]byte, error)` —— 形状对不上,
// 断言必然失败。
//
// 这类"看着有、其实对不上"的错最难查:桩能编译(它满足 Provider),
// 但**用它的那个断言永远失败** ⇒ 上层拿到的是"静默降级成空串"。
// M133 把签名统一了,这里把"统一"钉成事实。
package oss

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// 特征:桩的 Get 必须和真 provider 一样是 `Get(ctx, key) ([]byte, error)`,
// 且读回来的字节与写进去的**逐字节一致**。
func TestMockProvider_GetRoundTrip(t *testing.T) {
	p := NewMockProvider()
	ctx := context.Background()

	body := []byte(`{"file_count":1,"files":[{"name":"户型图 v2.pdf"}]}`)
	key := "res/94883951113467590/default/96695920002907268/info.json"
	if _, err := p.Put(ctx, key, bytes.NewReader(body), "application/json"); err != nil {
		t.Fatalf("Put 失败: %v", err)
	}

	got, err := p.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if !bytes.Equal(body, got) {
		t.Fatalf("读回来的字节不一致\n写: %s\n读: %s", body, got)
	}
}

// 特征:不存在时必须报 oss.ErrNotFound —— 和真 provider 同形。
//
// ⚠️ 桩如果返回一个自定义错误,上层分不清"还没生成"和"读失败",
// 而真 provider 报的是 ErrNotFound ⇒ **换存储后端行为就变了**。
func TestMockProvider_GetNotFound(t *testing.T) {
	p := NewMockProvider()

	_, err := p.Get(context.Background(), "res/1/asset/2/info.json")
	if err == nil {
		t.Fatal("读不存在的对象必须报错")
	}
	if !errors.Is(err, &ErrNotFound{}) {
		t.Fatalf("必须报 ErrNotFound,got %v", err)
	}
}

// 特征:桩的 Get 必须是 **MetaReader** —— 这正是 service 层断言的那个形状。
//
// ⚠️ 这条是本文件的核心:签名对不上时,桩**照样编译通过**
// (它满足 Provider),但 service 层的 type-assert 会静默失败。
// 写成断言,漂移就在编译期暴露。
func TestMockProvider_ImplementsMetaReader(t *testing.T) {
	var p Provider = NewMockProvider()
	g, ok := p.(MetaReader)
	if !ok {
		t.Fatal("MockProvider 必须实现 MetaReader —— service 层要靠它读回 info.json")
	}
	if _, err := g.Get(context.Background(), "res/1/asset/2/info.json"); !IsNotFound(err) {
		t.Fatalf("经 MetaReader 读也要报 NotFound,got %v", err)
	}
}
