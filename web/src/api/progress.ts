/**
 * 请求进度回调的类型与归一化(§2.3 #9,M172)
 *
 * ⚠️ **为什么不让调用方直接用 axios 的 AxiosProgressEvent**:
 *   axios 给的 `total` 在**拿不到 Content-Length 时是 undefined**
 *   (chunked 传输 / 一些代理 / 分片上传的第二块起都是),`progress` 也随之是
 *   undefined —— 调用方每个都要自己写 `total ? loaded / total : ?`。
 *   这里一次归一化:`total: null` / `percent: null` 表示"未知总量",
 *   调用方要么显示不确定性进度(转圈),要么按字节展示。
 *
 * percent 口径:0-100 整数、向上不破百(浮点 99.9999 显示成 100,
 * 但 loaded 还没到 total 时 percent 不会提前 100 —— Math.round(99.4)=99)。
 */
export interface HttpProgress {
  /** 已传输字节数 */
  loaded: number
  /** 总字节数;拿不到(无 Content-Length)时 null */
  total: number | null
  /** 0-100 整数;total 未知时 null */
  percent: number | null
}

/** 纯函数:axios 的进度事件 → 归一化的 HttpProgress */
export function normalizeProgress(e: { loaded?: number; total?: number | null }): HttpProgress {
  const loaded = typeof e.loaded === 'number' && e.loaded >= 0 ? e.loaded : 0
  const total = typeof e.total === 'number' && e.total > 0 ? e.total : null
  return {
    loaded,
    total,
    percent: total === null ? null : Math.min(100, Math.round((loaded / total) * 100)),
  }
}
