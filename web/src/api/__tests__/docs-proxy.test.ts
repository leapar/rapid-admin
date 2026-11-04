/**
 * GitBook 手册挂载点 · vite 代理契约(2026-11-03 用户直报)
 *
 * 「使用教程」点开是 `{origin}/docs/`(见 components/layout/helpMenu.ts 的
 * gitbookDocsUrl)。后端把 GitBook 挂在 **server 根** 的 `/docs` 下
 * (router.go:`docs.Mount(h, "/docs", ...)`,go:embed server/docs/site)。
 *
 * 开发期页面在 vite(5173)、后端在 8888 —— 不加代理,`/docs` 直连 vite:
 * 它不是前端路由 ⇒ **直接 404**(比 /swagger 的 SPA-fallback 假登录页诚实,
 * 但一样是坏的)。修法同 /swagger:vite 代理转发到后端。
 *
 * ## 这张网怎么防回归(探针方向,反向也有效)
 *
 *   · 从 vite.config.ts 的 proxy 里删掉 '/docs' → 「挂载点被代理」红
 *   · 把 vite 里的前缀改成 '/docs2' → 「前缀跟后端一致」红
 *   · 把后端 router.go 的 docs.Mount 路径改成别的 → 同上红
 *   · 在 '/docs' 代理里加 rewrite → 「代理不能重写路径」红
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/** 仓库根 = 本文件在 `web/src/api/__tests__/`,往上四级 */
const REPO_ROOT = join(__dirname, '..', '..', '..', '..')

const VITE_CONFIG = join(REPO_ROOT, 'web', 'vite.config.ts')

/** 后端路由装配(GitBook 的挂载点在这里) */
const SERVER_ROUTER = join(REPO_ROOT, 'server', 'internal', 'api', 'router', 'router.go')

/**
 * 后端 `docs.Mount(h, "<挂载点>", ...)` 的挂载点
 *
 * ⚠️ 解析失败要**当场报错**,不能返回空串继续跑 ——
 *   解析器失效 = 这张网成了空网(网空了就等于没有网)。
 */
function backendMountPath(): string {
  const src = readFileSync(SERVER_ROUTER, 'utf-8')
  const m = src.match(/docs\.Mount\(\s*\w+\s*,\s*"([^"]+)"/)
  expect(
    m,
    'server/internal/api/router/router.go 里找不到 docs.Mount 的挂载点 —— 解析器失效,这张网成了空网',
  ).not.toBeNull()
  return m![1]!
}

/** vite proxy 里配的路径前缀清单(与 swagger-proxy.test.ts 同一解析口径) */
function viteProxyPrefixes(): string[] {
  const src = readFileSync(VITE_CONFIG, 'utf-8')
  const out: string[] = []
  const re = /^\s*'(\/[^']*)'\s*:\s*\{/gm
  let m: RegExpExecArray | null
  while ((m = re.exec(src)) !== null) {
    out.push(m[1]!)
  }
  return out
}

describe('GitBook 手册挂载点 · vite 代理契约', () => {
  it('挂载点必须被 vite 代理(否则开发期点「使用教程」看到的是 404)', () => {
    const mount = backendMountPath()
    // router.go 里写的就是带斜杠的 "/docs";保险起见归一化,不假设它总带
    const full = mount.startsWith('/') ? mount : `/${mount}`
    const prefixes = viteProxyPrefixes()
    expect(
      prefixes,
      `vite.config.ts 的 proxy 里没有 GitBook 的挂载点 "${full}" —— 开发期点「使用教程」直连 vite 自己,拿到 404。\n已配的代理前缀: ${prefixes.join(', ') || '(空)'}`,
    ).toContain(full)
  })

  it('代理不能重写路径(后端认的就是 /docs/... 这个完整路径)', () => {
    const src = readFileSync(VITE_CONFIG, 'utf-8')
    const block = src.slice(src.indexOf("'/docs'"))
    expect(
      block.slice(0, block.indexOf('}') + 1),
      "vite 的 '/docs' 代理里出现了 rewrite —— 重写路径会把路径改掉,后端按原路径找资源就找不到",
    ).not.toMatch(/rewrite/)
  })
})
