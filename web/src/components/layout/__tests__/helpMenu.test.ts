/**
 * 帮助下拉 · GitBook URL 拼装(2026-11-03 用户直报)
 *
 * ## 这条网守的是什么
 *
 * 「使用教程」要开 `{后端}/docs/` 的新标签页。后端 origin **只有一个来源:
 * API base**(getBaseURL()),但 API base 有两种形态,拼法不同(详见
 * helpMenu.ts 的头注释):
 *
 *   · 绝对 URL(生产,API 与 /docs 同域网关)→ 取 **origin**,丢弃 /rapid-admin/v1 路径
 *   · 相对路径(dev / vite proxy,拿不到后端 origin)→ 落**当前页面 origin**
 *
 * 拼错了的后果:教程 404 或者打到前端路由(白屏),用户以为"没教程"。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `new URL(base).origin` 换成 `base` → 「绝对 URL」红(拼出 /rapid-admin/v1/docs/)
 *   · 把 `currentOrigin` 分支删掉 → 「相对路径」红(拼出 undefined/docs/)
 */
import { describe, it, expect } from 'vitest'
import { gitbookDocsUrl } from '../helpMenu'

describe('gitbookDocsUrl —— /docs 挂在 server 根,不在 API 前缀下', () => {
  it('绝对 URL:取 origin,丢掉 /rapid-admin/v1 路径段', () => {
    expect(gitbookDocsUrl('https://api.example.com/rapid-admin/v1', 'https://admin.example.com')).toBe(
      'https://api.example.com/docs/',
    )
  })

  it('相对路径(dev,vite proxy):落当前页面 origin', () => {
    expect(gitbookDocsUrl('/rapid-admin/v1', 'http://localhost:5173')).toBe('http://localhost:5173/docs/')
  })

  it('base 为空(env 没配到最裸的程度):落当前 origin,不产 "undefined/docs/"', () => {
    expect(gitbookDocsUrl('', 'http://localhost:5173')).toBe('http://localhost:5173/docs/')
  })

  it('base 结尾多斜杠:不影响结果(恒 origin + /docs/)', () => {
    expect(gitbookDocsUrl('https://api.example.com/rapid-admin/v1/', 'https://admin.example.com')).toBe(
      'https://api.example.com/docs/',
    )
  })

  it('currentOrigin 结尾多斜杠:归一,不产双斜杠', () => {
    expect(gitbookDocsUrl('/rapid-admin/v1', 'http://localhost:5173/')).toBe('http://localhost:5173/docs/')
  })
})
