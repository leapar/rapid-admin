// oss —— 对象存储统一抽象
//
// ## 这个包解决什么问题
//
// 本仓的文件存储可以是本地磁盘、阿里云 OSS、或内存 mock。
// 业务代码不该关心是哪种 ⇒ 统一成 `Provider` 接口。
//
// ## 模型:一个桶 / 一棵树 / key 恒 5 段
//
//		<root>/<userID>/<biz>/<folderID>/<objectName>
//
//	  - **文件没有 public 状态**:私密靠"key 不可猜"实现,不是靠 ACL
//	  - 每个文件都有一个**不带签名的稳定直链**(前端直接塞 <img src>),
//	    签名 URL 过期就裂图
//
// ## 关键点
//
//   - **key 由服务端决定**,不是客户端传什么就存什么
//   - **ParseKey 是 key 的唯一反解入口**。全仓只能问这一份实现 ——
//     不许在 service / handler / 前端各写一遍 strings.Split 再按下标取:
//     那样在"biz 段数变了"时会错位,而错位之后取到的 uid 是别人的,
//     **归属校验会静默失效**
//   - SignUploadToken 的参数是**完整 key 不是目录前缀**(见 Provider 注释)
//   - Stat 找不到对象时必须让 IsNotFound(err) == true,
//     service 层靠它把 404 和 500 分开
//   - DeletePrefix 返回**字节数**:用户存储配额靠它回退
//
// ## 跑法
//
//	go run ./examples/oss
package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/oss"
)

func main() {
	ctx := context.Background()

	// ── 一、对象名清洗 ──────────────────────────────────────────
	fmt.Println("=== 对象名 ===")
	name, err := oss.NewObjectName("合同 扫描件.pdf")
	if err != nil {
		fmt.Println("生成对象名失败:", err)
		return
	}
	fmt.Printf("NewObjectName(\"合同 扫描件.pdf\") = %s\n", name)
	fmt.Printf("扩展名 = %q\n", oss.ObjectExt(name))

	// ── 二、构造 key ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== key ===")
	spec := oss.PathSpec{
		Root:       oss.DefaultRoot,
		UserID:     1001,
		Biz:        "house",
		FolderID:   8801,
		ObjectName: name,
	}
	key, err := oss.BuildKey(spec)
	if err != nil {
		fmt.Println("构造 key 失败:", err)
		return
	}
	fmt.Printf("key = %s\n", key)
	fmt.Printf("段数 = %d\n", len(splitSlash(key)))

	// ── 三、反解 key ────────────────────────────────────────────
	//
	// ⚠️ 这是唯一的反解入口,别在别处再写一遍 Split。
	fmt.Println()
	fmt.Println("=== 反解 key ===")
	parts, err := oss.ParseKey(oss.DefaultRoot, key)
	if err != nil {
		fmt.Println("解析失败:", err)
		return
	}
	fmt.Printf("root=%s userID=%d biz=%s folderID=%d object=%s\n",
		parts.Root, parts.UserID, parts.Biz, parts.FolderID, parts.ObjectName)

	// ── 四、MockProvider:不依赖外部存储 ────────────────────────
	fmt.Println()
	fmt.Println("=== MockProvider ===")
	p := oss.NewMockProvider()
	fmt.Printf("provider = %s\n", p.Name())

	meta, err := p.Put(ctx, key, bytes.NewReader([]byte("hello")), "application/pdf")
	if err != nil {
		fmt.Println("上传失败:", err)
		return
	}
	fmt.Printf("上传后: key=%s size=%d mime=%s\n", meta.Key, meta.Size, meta.MimeType)

	got, err := p.Get(ctx, key)
	fmt.Printf("读回来: %q (err=%v)\n", string(got), err)

	_, err = p.Stat(ctx, "不存在的key")
	fmt.Printf("读不存在的对象 → IsNotFound = %v\n", oss.IsNotFound(err))

	// ── 五、列举 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 列举 ===")
	res, err := p.List(ctx, oss.ListOption{Prefix: oss.DefaultRoot + "/1001/"})
	if err != nil {
		fmt.Println("列举失败:", err)
		return
	}
	fmt.Printf("前缀下有 %d 个对象\n", len(res.Objects))
	for _, o := range res.Objects {
		fmt.Printf("  %s (%d 字节)\n", o.Key, o.Size)
	}

	// ── 六、删除前缀(返回字节数,给配额回退用)─────────────────
	fmt.Println()
	fmt.Println("=== 删前缀 ===")
	n, err := p.DeletePrefix(ctx, oss.DefaultRoot+"/1001/")
	fmt.Printf("删掉 %d 字节 (err=%v)⇒ 用户存储配额靠这个值回退\n", n, err)

	// ── 七、下载直链 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 下载直链 ===")
	url, err := p.SignDownloadURL(ctx, key, time.Hour)
	if err != nil {
		fmt.Println("签名失败:", err)
	} else {
		fmt.Printf("签名 URL = %s\n", url)
	}
}

// splitSlash 按 / 切分
func splitSlash(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == '/' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}
