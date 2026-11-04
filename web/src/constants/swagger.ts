/**
 * Swagger UI 的前端侧路径(2026-11-01 · 开源通用化 §8 G5)
 *
 * ## 这两个路径从哪来
 *
 * Swagger UI **不是前端的一部分** —— 它由后端挂在 `/swagger` 下:
 *
 *   server/internal/api/router/router.go → swaggerx.Mount(h, "/swagger", ...)
 *   server/swagger/ui/                  → //go:embed 打进二进制的静态资源
 *
 * 所以前端这边只有一个**入口页**(/api-docs),页里给一个按钮在新标签页
 * 打开下面这个地址。
 *
 * ## ⚠️ 改挂载点要三处一起改
 *
 *   1. 后端 `swaggerx.Mount(h, "<挂载点>", ...)`
 *   2. 本文件的两个常量
 *   3. web/vite.config.ts 的 proxy 前缀(开发期)
 *
 * 少改任何一处,症状都是**静默**的:开发期点按钮看到的是 vite 的 SPA
 * fallback(200 + 后台登录页),生产期点按钮是 404。
 * `web/src/api/__tests__/swagger-proxy.test.ts` 会把三处对着验。
 */

/** Swagger UI 页面地址(挂载点 + 入口文件) */
export const SWAGGER_UI_PATH = '/swagger/index.html'

/** OpenAPI 描述文件地址(前端下载 / 工具链读的是它) */
export const SWAGGER_SPEC_PATH = '/swagger/doc.json'
