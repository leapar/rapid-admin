// Package docsbuild 算"手册源 ↔ 手册产物"的同步账本。
//
// # 它解决什么问题
//
// 手册是**生成物**:源在 server/docs/*.md,产物在 server/docs/site/。
// 二进制里嵌的是产物(server/docs/docs.go 的 //go:embed all:site)
// ⇒ **源改了没重建 = 线上手册是旧的**。
//
// 这个坑的特点是**没有任何报错**:编译照过、测试照过、页面照开,
// 只是内容停在上一版。所以这里把"产物够不够新"变成一个可判定的量:
// 源文件指纹(source hash)。
//
// # 指纹怎么算
//
// 参与计算:server/docs/ 下**除构建产物与 node_modules 外**的所有文件。
// .md / book.json / styles/ / favicon.ico / package.json / package-lock.json
// 全都算 —— 插件版本变了产物也会变,少算一个就有"改了插件没重建"的漏网。
//
// 不参与计算:**.go 文件**。手册源目录同时是一个 Go 包(docs.go / docs_test.go),
// 那些文件的改动不影响手册产物;算进去会让指纹每次提交都变 ⇒ 每次都判定
// "过期" ⇒ 永远在重建 —— 那等于没有这个检查。
//
// # 为什么这个包不放在 server/docs/ 里
//
// ⚠️ **因为那会造成启动死锁**:server/docs 那个包带 //go:embed all:site,
// 而 site/ 是构建产物。构建工具(cmd/docs)如果 import 它,就会出现
// "要构建 site/ 得先能编译 cmd/docs,要编译 cmd/docs 得先有 site/"。
// 所以指纹逻辑必须住在**不依赖 embed** 的地方 —— 就是本包。
package docsbuild

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildInfoFile 构建信息文件名(放在 site/ 根下)。
const BuildInfoFile = "buildinfo.json"

// BuildInfo 一次手册构建的元信息 —— 写在 site/buildinfo.json。
type BuildInfo struct {
	// SourceHash 源文件指纹(见 SourceHash)
	SourceHash string `json:"source_hash"`
	// BuiltAt 构建时间(RFC3339)
	BuiltAt string `json:"built_at"`
	// Pages 产出的 HTML 页数
	Pages int `json:"pages"`
	// Files 产出的文件总数
	Files int `json:"files"`
	// Tool 构建工具与版本(例:honkit 6.2.5)
	Tool string `json:"tool"`
}

// SourceHash 计算手册源文件的指纹。
//
// root 是 server/docs/ 这一层(手册源目录)。返回 hex(sha256)。
//
// ⚠️ **遍历顺序必须确定** —— filepath.WalkDir 本身是按字典序走的,
// 但这里仍然显式排一遍:万一将来改成并发收集,指纹就会变成"随机",
// 于是每次都判定过期。
func SourceHash(root string) (string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// 构建产物与依赖目录不参与指纹
			if p != root {
				switch d.Name() {
				case "site", "node_modules", ".git":
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("遍历手册源目录失败: %w", err)
	}
	sort.Strings(files)

	h := sha256.New()
	for _, p := range files {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		// 用 0 字节分隔 —— 免得"a.md"+"bc"和"ab.md"+"c"撞出同一个指纹
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		h.Write(normalizeNewlines(data))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// normalizeNewlines 把 CRLF 归一成 LF。
//
// ⚠️ **为什么必须归一** —— 本仓的 git 配了 core.autocrlf=true(Windows 默认),
// 于是同一个源文件在三个地方是三种字节:
//
//	Windows 工作区 :CRLF(git 检出时转换过)
//	仓库里(git 里):LF
//	Linux 工作区   :LF
//
// 不归一的话,同一份源在每个人的机器上算出不同的指纹 ⇒ 提交在仓库里的产物指纹
// 谁都匹配不上 ⇒ **漂移网在每台机器上都是红的**,而实际上什么都没变。
//
// ⚠️ 只换 CRLF,不碰裸 CR —— 裸 CR 在 Markdown 里是有意义的字符(反引号里的
// 转义说明),把它当换行会改变内容。
func normalizeNewlines(b []byte) []byte {
	if !bytes.Contains(b, []byte("\r\n")) {
		return b
	}
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

// ReadBuildInfo 读 site/buildinfo.json。
//
// 读不到(没构建过 / 文件坏了)时返回零值 + 错误。
func ReadBuildInfo(root string) (BuildInfo, error) {
	var bi BuildInfo
	data, err := os.ReadFile(filepath.Join(root, "site", BuildInfoFile))
	if err != nil {
		return bi, err
	}
	if err := json.Unmarshal(data, &bi); err != nil {
		return bi, fmt.Errorf("site/%s 解析失败: %w", BuildInfoFile, err)
	}
	return bi, nil
}

// WriteBuildInfo 写 site/buildinfo.json。
func WriteBuildInfo(root string, bi BuildInfo) error {
	data, err := json.MarshalIndent(bi, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(root, "site", BuildInfoFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return nil
}

// IsStale 判断手册产物是否落后于源文件。
//
// 返回 (是否过期, 当前产物信息, 错误)。
//
// ⚠️ **产物压根没构建过**也算过期 —— 而且不能报错:
// 第一次 clone 之后还没构建,这时候"过期"是正常状态,不是故障。
func IsStale(root string) (bool, BuildInfo, error) {
	bi, err := ReadBuildInfo(root)
	if err != nil {
		if os.IsNotExist(err) {
			return true, bi, nil
		}
		return true, bi, err
	}
	cur, err := SourceHash(root)
	if err != nil {
		return true, bi, err
	}
	return cur != bi.SourceHash, bi, nil
}

// CountSite 统计产物的页数与文件数。
func CountSite(root string) (pages, files int, err error) {
	site := filepath.Join(root, "site")
	err = filepath.WalkDir(site, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files++
		if strings.EqualFold(filepath.Ext(p), ".html") {
			pages++
		}
		return nil
	})
	return pages, files, err
}
