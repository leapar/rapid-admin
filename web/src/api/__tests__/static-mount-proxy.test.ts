/**
 * 文件静态直链的**路由契约** —— vite 开发服务器必须代理后端的静态挂载点
 * (2026-10-30)
 *
 * ## 这条网守的是什么
 *
 *   用户报的是:**「复制直链的路径 http://localhost:5173/<挂载点>/... 还是 404」**。
 *
 *   查下来是**开发期的路由缺口**,不是后端的问题:
 *
 *   · local provider 给的直链是**同源相对路径**
 *     `/rapid-admin/...`(后端把整棵树
 *     挂在 `/rapid-admin` 上)—— 这在后端自己域名下是对的;
 *   · 开发期页面在 vite(5173)、后端在 8888,**是两个 origin**;
 *   · vite 的 proxy 只配了 `/rapid-admin/v1` / `/api/v1` / `/oss`,**没有挂载点**;
 *   · 于是 `/rapid-admin/...` 打到 vite 自己,而 vite 的 **SPA fallback 对未知路径
 *     返回 index.html + 200**。
 *
 *   ⚠️ **200 + HTML 在 `<img src>` 里不报错** —— 浏览器当"解码失败",
 *     表现是图片裂开,后端日志里**一次请求都没有**(根本没到后端)。
 *     粘贴到地址栏更坑:看到的是登录页(SPA 认路由),很容易被当成"404"。
 *     所以这个洞是**完全静默**的,只有真跑起来看图才发现。
 *
 *   生产环境靠 Caddy 把整个域名 reverse_proxy 给后端(技术方案 §15.7),
 *   挂载点自然跟着走;**开发期这条代理就是等价的那个东西**。
 *
 * ## 为什么前缀要从**后端源码**里读,而不是在前端抄一份
 *
 *   后端的挂载点是 `file.path_root`(默认 `rapid-admin`)。抄一份 = 两份迟早漂移,
 *   而漂移的方向永远是"前端那份没跟着改" ⇒ 又变成静默裂图。
 *   直接读 `config.go`,后端改了默认值 → 本条测试当场变红。
 *   (跟 config-i18n.test.ts / biz-code-i18n.test.ts 一个套路:
 *   后端是唯一真源,前端只管接上。)
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 从 vite.config.ts 的 proxy 里删掉挂载点 → 「挂载点被代理」红
 *   · 把 vite 里的挂载点改成 '/files' → 「前缀跟后端一致」红
 *   · 把后端 config.go 里的 path_root 默认值改成别的 → 同上红
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/** 仓库根 = 本文件在 `web/src/api/__tests__/`,往上四级 */
const REPO_ROOT = join(__dirname, '..', '..', '..', '..')

/** 前端开发服务器配置 */
const VITE_CONFIG = join(REPO_ROOT, 'web', 'vite.config.ts')

/** 后端配置默认值(path_root 的默认就在这个文件里) */
const SERVER_CONFIG = join(REPO_ROOT, 'server', 'internal', 'config', 'config.go')

/** 后端 file.path_root 的默认值 —— vite 的代理前缀必须跟它一致 */
function serverPathRootDefault(): string {
  const src = readFileSync(SERVER_CONFIG, 'utf-8')
  const m = src.match(/SetDefault\(\s*"file\.path_root"\s*,\s*"([^"]+)"\s*\)/)
  expect(m, 'server/internal/config/config.go 里找不到 file.path_root 的 SetDefault').not.toBeNull()
  return m![1]!
}

/** vite proxy 里配的路径前缀清单 */
function viteProxyPrefixes(): string[] {
  const src = readFileSync(VITE_CONFIG, 'utf-8')
  // 只认 proxy 块里那种"裸字符串键":'/xxx': {
  const out: string[] = []
  const re = /^\s*'(\/[^']*)'\s*:\s*\{/gm
  let m: RegExpExecArray | null
  while ((m = re.exec(src)) !== null) {
    out.push(m[1]!)
  }
  return out
}

describe('文件静态挂载点 · vite 代理契约', () => {
  it('挂载点必须被 vite 代理(否则开发期全是裂图,而且零报错)', () => {
    const prefixes = viteProxyPrefixes()
    expect(
      prefixes,
      `vite.config.ts 的 proxy 里没有文件静态挂载点 —— 开发期 <img src="/rapid-admin/..."> 会打到 vite 的 SPA fallback,拿到 200 + index.html,图片裂掉而后端日志里一次请求都没有。\n已配的代理前缀: ${prefixes.join(', ') || '(空)'}`,
    ).toContain('/rapid-admin')
  })

  it('代理前缀必须跟后端 file.path_root 的默认值一致(两份不许漂移)', () => {
    const root = serverPathRootDefault()
    const prefixes = viteProxyPrefixes()
    expect(
      prefixes,
      `后端 file.path_root 默认是 "${root}",vite 却没代理 /${root} —— 直链会打到 vite 自己。\n已配的代理前缀: ${prefixes.join(', ') || '(空)'}`,
    ).toContain(`/${root}`)
  })

  it('代理不能重写路径(后端认的就是 /rapid-admin/... 这个完整 key)', () => {
    const src = readFileSync(VITE_CONFIG, 'utf-8')
    const block = src.slice(src.indexOf("'/rapid-admin'"))
    expect(
      block.slice(0, block.indexOf('}') + 1),
      "vite 的 '/rapid-admin' 代理里出现了 rewrite —— 重写路径会把 key 改掉,后端按完整 key 找文件就找不到",
    ).not.toMatch(/rewrite/)
  })
})
