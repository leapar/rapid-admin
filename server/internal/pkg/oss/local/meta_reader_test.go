// M133 小元数据回读(oss.MetaReader)—— local provider
//
// ⚠️ **这个文件是为 M133 单独开的**,不是"顺手补一个 Get 的用例"。
// Get 之前**根本不存在** —— M78 定 8 个方法时没有调用方要读字节,于是
// 主接口里没有它,M133 service 层要读回 info.json 时才发现三家 provider
// 一个都没实现(见 service/folder_info.go 的 readFolderInfo 注释)。
//
// 覆盖三件事,少一件就有一类故障漏过去:
//
//	① 往返:写进去什么,读回来必须**逐字节一致**(info.json 是 JSON,差一个字节就是解析失败)
//	② 不存在:必须报 oss.ErrNotFound —— 调用方靠它区分"还没生成"和"读失败"
//	③ 上限:超过 8MB 必须拒 —— Get 是给"小元数据"用的,拿它读大文件会 OOM
package local

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"rapid-admin/internal/pkg/oss"
)

// infoKey 是 info.json 的标准 key 形态(与 service 层 infoKey 一致)
const infoKey = "res/94883951113467590/default/96695920002907268/info.json"

// 特征:往返必须逐字节一致 —— 且**中文不许被转义/替换**。
//
// ⚠️ 内容用真 JSON 而不是随便一个字符串:info.json 里存的是文件名,
// 而文件名在中文环境下就是中文。这条用例锁住的是"Get 原样返回",
// 一旦实现里加了任何"清洗"步骤,这里立刻红。
func TestGet_RoundTrip(t *testing.T) {
	p := newTestProvider(t)
	ctx := context.Background()

	body := []byte(`{
  "folder_id": "96695920002907268",
  "biz": "default",
  "name": "户型图",
  "file_count": 1,
  "files": [{"name": "户型图 v2.pdf", "size": 12, "content_type": "application/pdf"}]
}`)
	putFile(t, p, infoKey, body)

	got, err := p.Get(ctx, infoKey)
	if err != nil {
		t.Fatalf("Get(%q) 失败: %v", infoKey, err)
	}
	if !bytes.Equal(body, got) {
		t.Fatalf("读回来的字节和写进去的不一致\n写: %s\n读: %s", body, got)
	}
}

// 特征:**不存在时必须报 oss.ErrNotFound**。
//
// ⚠️ 这条不是"顺手断言一下" —— service 层的 readFolderInfo 靠
// `errors.Is(err, &oss.ErrNotFound{})` 区分"这个文件夹还没生成 info.json"
// (正常,比如刚建还没传文件)和"读失败"(异常,要去翻存储的日志)。
// 报别的错 ⇒ 正常的空文件夹被当成异常,排障的人被引到完全错误的方向上。
func TestGet_NotFound(t *testing.T) {
	p := newTestProvider(t)

	_, err := p.Get(context.Background(), infoKey)
	if err == nil {
		t.Fatal("读一个不存在的对象必须报错")
	}
	if !errors.Is(err, &oss.ErrNotFound{}) {
		t.Fatalf("必须报 oss.ErrNotFound,got %v", err)
	}
	if !oss.IsNotFound(err) {
		t.Fatalf("oss.IsNotFound 也必须认,got %v", err)
	}
}

// 特征:**超过 8MB 必须拒**,而且错要说清"要读大文件请用 Open"。
//
// ⚠️ 上限的意义是防手滑:有人拿 Get 去读 500MB 的视频,一次性 OOM。
// 所以这条不是"性能保护",是**正确性保护**,必须真的拒,不能截断 ——
// 截断的话调用方拿到半截 JSON,解析失败的原因会指向错误的方向。
func TestGet_RejectsOversize(t *testing.T) {
	p := newTestProvider(t)
	ctx := context.Background()

	big := mkContent(maxGetBytes+1, 'x')
	putFile(t, p, infoKey, big)

	got, err := p.Get(ctx, infoKey)
	if err == nil {
		t.Fatalf("超过上限必须报错,却读出了 %d 字节", len(got))
	}
	// ⚠️ 不能是 ErrNotFound —— 对象明明在,报"不存在"会把人引到错误方向
	if errors.Is(err, &oss.ErrNotFound{}) {
		t.Fatalf("超限不能报 NotFound(对象是在的),got %v", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("Open")) {
		t.Fatalf("报错要说清「要读大文件请用 Open」,got %v", err)
	}
}

// 特征:**恰好等于上限要能读出来** —— 闸门是"超过",不是"达到"。
//
// ⚠️ 用 `>` 判还是 `>=` 判,差一个字节。这条把它钉死:8MB 的 info.json
// 是合法的(虽然现实中不会到),而 8MB+1 必须拒。
func TestGet_AtLimitIsAllowed(t *testing.T) {
	p := newTestProvider(t)
	ctx := context.Background()

	body := mkContent(maxGetBytes, 'y')
	putFile(t, p, infoKey, body)

	got, err := p.Get(ctx, infoKey)
	if err != nil {
		t.Fatalf("恰好等于上限必须能读,got %v", err)
	}
	if len(got) != maxGetBytes {
		t.Fatalf("读出 %d 字节,期望 %d", len(got), maxGetBytes)
	}
}

// 特征:**非法 key 必须在碰磁盘之前就拒** —— 和 Put / Stat 同一套 SafeKey。
//
// ⚠️ Get 是"读"接口,但它一样能把路径写出存储根(local 的 ../ 在磁盘上
// 是真的会走目录的)。少这道闸,一个 `../../etc/passwd` 就能读出根外的文件。
func TestGet_RejectsUnsafeKey(t *testing.T) {
	p := newTestProvider(t)

	for _, key := range []string{
		"../etc/passwd",
		"res/../../etc/passwd",
		"",
	} {
		_, err := p.Get(context.Background(), key)
		if err == nil {
			t.Fatalf("非法 key %q 必须被拒", key)
		}
	}
}
