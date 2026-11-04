/**
 * useOnline — 监听 navigator.onLine 状态
 * 2026-10-19 M64:断网/弱网检测
 * 2026-10-25 M7:修「回调身份导致监听反复重绑」
 *
 * 设计:
 *  - 浏览器 navigator.onLine + online/offline 事件
 *  - SSR 兼容(无 navigator 时默认 true)
 *  - 初始值用 navigator.onLine 当前真实值(避免 hydrate 闪烁)
 *  - online 恢复时触发可选 onReconnect 回调
 *
 * 用法:
 *   const online = useOnline({ onReconnect: () => message.success('网络已恢复') })
 *   if (!online) return <NoNetworkBanner />
 */
import { useEffect, useRef, useState } from 'react'

export interface UseOnlineOptions {
  /** 从 offline 切回 online 时触发(去重 toast / 重新拉数据) */
  onReconnect?: () => void
  /** 从 online 切到 offline 时触发(打点 / 暂停轮询) */
  onDisconnect?: () => void
}

export function useOnline(options: UseOnlineOptions = {}): boolean {
  const { onReconnect, onDisconnect } = options
  // SSR / Node 端:无 navigator,默认 online (true)
  const [online, setOnline] = useState<boolean>(() => {
    if (typeof navigator === 'undefined') return true
    return navigator.onLine
  })

  /**
   * ⚠️ **回调必须用 ref 固定**(2026-10-25 M7)
   *
   * 以前 effect 的依赖是 `[onReconnect, onDisconnect]`。调用方几乎都写
   * `useOnline({ onReconnect: () => reload() })` —— **每次 render 都是新函数**
   * → 依赖每渲染都变 → 监听器被 remove/add 反复重绑。
   *
   * 看着只是白做功,实际有两个真问题:
   *   1. 换绑的瞬间存在窗口期,'online' 事件正好落在这个缝里就**漏掉了**
   *      —— 而"网络恢复后自动重载"恰恰依赖这个事件(M7 的核心)
   *   2. 事件处理闭包拿到的是**旧回调**,回调里读到的 state 是旧的
   *
   * 改成:回调存 ref,sync effect 只做赋值;监听 effect 依赖恒为 `[]`,
   * 全生命周期只订阅一次。
   */
  const cbRef = useRef<UseOnlineOptions>({ onReconnect, onDisconnect })
  useEffect(() => {
    cbRef.current.onReconnect = onReconnect
    cbRef.current.onDisconnect = onDisconnect
  }, [onReconnect, onDisconnect])

  useEffect(() => {
    if (typeof window === 'undefined') return
    const handleOnline = () => {
      setOnline(true)
      cbRef.current.onReconnect?.()
    }
    const handleOffline = () => {
      setOnline(false)
      cbRef.current.onDisconnect?.()
    }
    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)
    return () => {
      window.removeEventListener('online', handleOnline)
      window.removeEventListener('offline', handleOffline)
    }
  }, [])

  return online
}
