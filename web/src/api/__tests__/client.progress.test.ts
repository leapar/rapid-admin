/**
 * 请求进度回调(§2.3 #9,M172)
 *
 * 两层:normalizeProgress 纯函数(总量未知 / 百分比口径)+
 * client.ts 出口(一等选项 → 包归一化 → axios 事件)。
 * 可逆探针:拆掉 client 里的归一化包装 ⇒ 端到端那条红。
 */
import { describe, it, expect, afterEach, vi } from 'vitest'

import { normalizeProgress } from '@/api/progress'
import { http, instance } from '@/api/client'

describe('normalizeProgress', () => {
  it('有总量:percent = round(loaded/total*100),不破百', () => {
    expect(normalizeProgress({ loaded: 30, total: 100 })).toEqual({
      loaded: 30,
      total: 100,
      percent: 30,
    })
    expect(normalizeProgress({ loaded: 99, total: 100 }).percent).toBe(99)
    expect(normalizeProgress({ loaded: 100, total: 100 }).percent).toBe(100)
  })

  it('总量未知(chunked / 无 Content-Length):total 与 percent 都是 null,loaded 照报', () => {
    expect(normalizeProgress({ loaded: 4096, total: undefined })).toEqual({
      loaded: 4096,
      total: null,
      percent: null,
    })
    expect(normalizeProgress({ loaded: 1, total: 0 })).toEqual({
      loaded: 1,
      total: null,
      percent: null,
    })
  })

  it('异常输入兜底:负 loaded 归 0', () => {
    expect(normalizeProgress({ loaded: -5, total: 100 }).loaded).toBe(0)
    expect(normalizeProgress({}).loaded).toBe(0)
  })
})

describe('http 的一等进度选项(端到端)', () => {
  const originalAdapter = instance.defaults.adapter
  afterEach(() => {
    instance.defaults.adapter = originalAdapter
  })

  it('onUploadProgress 收到归一化后的 HttpProgress', async () => {
    instance.defaults.adapter = async (config) => {
      // 模拟 xhr adapter:发进度事件再返回响应
      ;(config as unknown as { onUploadProgress?: (e: unknown) => void }).onUploadProgress?.({
        loaded: 30,
        total: 100,
      })
      ;(config as unknown as { onUploadProgress?: (e: unknown) => void }).onUploadProgress?.({
        loaded: 100,
        total: 100,
      })
      return {
        data: { code: 0, data: null, message: 'ok' },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    const onUploadProgress = vi.fn()
    await http.post('/files', new FormData(), { onUploadProgress })
    expect(onUploadProgress).toHaveBeenCalledTimes(2)
    expect(onUploadProgress.mock.calls[0]![0]).toEqual({ loaded: 30, total: 100, percent: 30 })
    expect(onUploadProgress.mock.calls[1]![0]).toEqual({ loaded: 100, total: 100, percent: 100 })
  })

  it('onDownloadProgress 同样生效', async () => {
    instance.defaults.adapter = async (config) => {
      ;(config as unknown as { onDownloadProgress?: (e: unknown) => void }).onDownloadProgress?.({
        loaded: 1,
        total: undefined, // 总量未知
      })
      return {
        data: { code: 0, data: null, message: 'ok' },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    const onDownloadProgress = vi.fn()
    await http.get('/files/1/content', undefined, { onDownloadProgress })
    expect(onDownloadProgress).toHaveBeenCalledWith({ loaded: 1, total: null, percent: null })
  })

  it('不传进度回调时,config 里不该有 onUploadProgress 键(快路径零污染)', async () => {
    let captured: Record<string, unknown> | undefined
    instance.defaults.adapter = async (config) => {
      captured = config as unknown as Record<string, unknown>
      return {
        data: { code: 0, data: null, message: 'ok' },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    await http.post('/files', { a: 1 })
    // toAxiosConfig 的快路径必须原样返回 —— 若实现给 config 塞了 undefined 处理器,
    // 说明快路径被污染(axios 对 undefined 处理器是宽容的,但这层纪律防的是
    // 将来有人在 undefined 分支上误加默认行为)
    expect('onUploadProgress' in (captured ?? {})).toBe(false)
  })
})
