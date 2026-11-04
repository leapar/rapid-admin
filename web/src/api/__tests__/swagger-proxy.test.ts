/**
 * Swagger UI 的**路由契约** —— vite 开发服务器必须代理后端的挂载点
 * (2026-11-01 · 开源通用化 §8 G5)
 *
 * ## 这条网守的是什么
 *
 *   Swagger UI 由**后端**挂在 `/swagger` 下(不是前端的一部分),而开发期
 *   页面在 vite(5173)、后端在 8888 —— **两个 origin**。
 *
 *   不加这条代理的后果**不是报错**,而是更迷惑的东西:
 *   `/swagger/index.html` 打到 vite 自己 ⇒ vite 的 **SPA fallback** 对未知
 *   路径返回 **index.html + 200** ⇒ 浏览器把**后台的登录页**当成 Swagger UI
 *   显示出来。用户看到的是一个登录框,完全不知道「文档」其实在另一个进程上。
 *
 *   (跟 `/rapid-admin` 那条代理是同一个坑的同一个形状 ——
 *    见 static-mount-proxy.test.ts。)
 *
 * ## 为什么前缀要从**后端源码**里读,而不是在前端抄一份
 *
 *   后端的挂载点是 `swaggerx.Mount(h, "/swagger", ...)`。抄一份 = 两份迟早
 *   漂移,而漂移的方向永远是「前端那份没跟着改」⇒ 又变成静默的 404。
 *   直接读 router.go,后端改了挂载点 → 本条测试当场变红。
 *
 *   本条同时盯着**第三份**:前端页面里的地址常量
 *   (`src/constants/swagger.ts`)。三处(后端挂载点 / vite 代理 / 前端常量)
 *   必须是同一个前缀。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 从 vite.config.ts 的 proxy 里删掉 '/swagger' → 「挂载点被代理」红
 *   · 把 vite 里的前缀改成 '/apidoc' → 「前缀跟后端一致」红
 *   · 把后端 router.go 的 Mount 路径改成别的 → 同上红 + 「前端常量对得上」红
 *   · 把前端常量改成 '/swagger2/index.html' → 「前端常量对得上」红
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { SWAGGER_SPEC_PATH, SWAGGER_UI_PATH } from '@/constants/swagger'

/** 仓库根 = 本文件在 `web/src/api/__tests__/`,往上四级 */
const REPO_ROOT = join(__dirname, '..', '..', '..', '..')

/** 前端开发服务器配置 */
const VITE_CONFIG = join(REPO_ROOT, 'web', 'vite.config.ts')

/** 后端路由装配(Swagger UI 的挂载点在这里) */
const SERVER_ROUTER = join(REPO_ROOT, 'server', 'internal', 'api', 'router', 'router.go')

/**
 * 后端 `swaggerx.Mount(h, "<挂载点>", ...)` 的挂载点
 *
 * ⚠️ 解析失败要**当场报错**,不能返回一个空串继续跑 ——
 *   解析器失效 = 这张网成了空网(网空了就等于没有网)。
 */
function backendMountPath(): string {
  const src = readFileSync(SERVER_ROUTER, 'utf-8')
  const m = src.match(/swaggerx\.Mount\(\s*\w+\s*,\s*"([^"]+)"/)
  expect(
    m,
    'server/internal/api/router/router.go 里找不到 swaggerx.Mount 的挂载点 —— 解析器失效,这张网成了空网',
  ).not.toBeNull()
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

describe('Swagger UI 挂载点 · vite 代理契约', () => {
  it('挂载点必须被 vite 代理(否则开发期点按钮看到的是后台登录页)', () => {
    const mount = backendMountPath()
    const prefixes = viteProxyPrefixes()
    expect(
      prefixes,
      `vite.config.ts 的 proxy 里没有 Swagger UI 的挂载点 "${mount}" —— 开发期点「API 文档」会打到 vite 的 SPA fallback,拿到 200 + index.html(后台登录页),看起来像"文档坏了"。\n已配的代理前缀: ${prefixes.join(', ') || '(空)'}`,
    ).toContain(mount)
  })

  it('前端页面里的地址必须落在后端挂载点下', () => {
    const mount = backendMountPath()
    expect(
      SWAGGER_UI_PATH.startsWith(`${mount}/`),
      `前端常量 SWAGGER_UI_PATH="${SWAGGER_UI_PATH}" 不在后端挂载点 "/${mount}" 下 —— 改后端挂载点要同步改 src/constants/swagger.ts`,
    ).toBe(true)
    expect(
      SWAGGER_SPEC_PATH.startsWith(`${mount}/`),
      `前端常量 SWAGGER_SPEC_PATH="${SWAGGER_SPEC_PATH}" 不在后端挂载点 "/${mount}" 下 —— 改后端挂载点要同步改 src/constants/swagger.ts`,
    ).toBe(true)
  })

  it('代理不能重写路径(后端认的就是 /swagger/... 这个完整路径)', () => {
    const src = readFileSync(VITE_CONFIG, 'utf-8')
    const block = src.slice(src.indexOf("'/swagger'"))
    expect(
      block.slice(0, block.indexOf('}') + 1),
      "vite 的 '/swagger' 代理里出现了 rewrite —— 重写路径会把路径改掉,后端按原路径找资源就找不到",
    ).not.toMatch(/rewrite/)
  })
})
