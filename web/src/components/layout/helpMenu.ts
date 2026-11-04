/**
 * 帮助下拉 —— 纯逻辑(2026-11-03 用户直报)
 *
 * 「右上角帮助按钮现在是死的,要变成下拉:API 文档 + 使用教程(GitBook)」。
 *
 * ## 为什么这个函数必须独立成 .ts
 *
 * ① react-refresh 豁免:本文件只导出纯函数,Header 改动不会触发 HMR 全页重载;
 * ② 可测:GitBook 的挂载位置有两个真相(见下),拼 URL 的规则值得单测钉死。
 *
 * ## 为什么不是简单拼 `{apiBase}/docs/`
 *
 * GitBook 在后端是挂在 **server 根** 的(`/docs/*`,见 router.go 的
 * `docs.Mount(h, "/docs", ...)`)—— 它**不在** `/rapid-admin/v1` 前缀下面。
 * 而 `getBaseURL()` 返回的是 API 前缀:
 *
 *   · 生产:`https://api.example.com/rapid-admin/v1` → /docs 的 origin 是
 *     `https://api.example.com`(API 网关同一域名)→ 取 origin 拼 `/docs/`。
 *   · 本地 dev:`VITE_API_BASE_*` 没配 → base 是相对路径 `/rapid-admin/v1`
 *     (vite proxy 转发)→ 此时根本拿不到后端 origin,只能用**当前页面**
 *     的 origin(vite dev server)。✅ **2026-11-03 起 dev 也可用**:vite
 *     配了 `/docs` 代理(见 vite.config.ts,docs-proxy.test.ts 盯着),
 *     当前页 origin 打到的 `/docs` 会被转发到后端。
 *
 * 规则:**绝对 URL 取 origin,相对路径落当前 origin**,结尾恒带 `/`
 * (GitBook 的 index 是 302 → `index.html`,裸 `/docs` 也能到,
 * 但带上斜杠少一跳)。
 */
export function gitbookDocsUrl(apiBase: string, currentOrigin: string): string {
  const base = (apiBase || '').trim()
  if (/^https?:\/\//i.test(base)) {
    try {
      return new URL(base).origin + '/docs/'
    } catch {
      // new URL 理论上不会失败(前面已过正则),但吞掉比抛出去稳 ——
      // 这只是个"打开教程"的菜单项,不值得让它炸掉整次点击
    }
  }
  return `${currentOrigin.replace(/\/+$/, '')}/docs/`
}
