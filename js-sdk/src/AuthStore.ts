/**
 * token 存放处
 *
 * # 为什么不用 localStorage
 *
 * 集成方所在的环境五花八门(浏览器 / 微信小程序 / Node 脚本 / 桌面壳),
 * "帮你塞 localStorage"在第二种环境里就是报错。
 * ⇒ 默认**只在内存里存**,浏览器端要跨刷新保持的集成方自己 save 一次
 * (README 有示例);这与 PocketBase 的 LocalAuthStore 可选持久化同思路,但更瘦。
 */
export class AuthStore {
  private tokenValue = ''
  private listeners: Array<(token: string) => void> = []

  /** 当前 token;未登录 = '' */
  get token(): string {
    return this.tokenValue
  }

  /** 是否已带 token */
  get isValid(): boolean {
    return this.tokenValue !== ''
  }

  /** 保存 token(传空串 = 清除;与当前值相同则什么都不发生) */
  save(token: string): void {
    const next = token || ''
    if (next === this.tokenValue) {
      return
    }
    this.tokenValue = next
    this.emit()
  }

  /** 清除 token */
  clear(): void {
    this.tokenValue = ''
    this.emit()
  }

  /** 订阅变化;返回取消订阅函数。**save 成新值才发通知,save 同值不发**(同 PB 语义) */
  onChange(listener: (token: string) => void): () => void {
    this.listeners.push(listener)
    return () => {
      this.listeners = this.listeners.filter((l) => l !== listener)
    }
  }

  private emit(): void {
    // ⚠️ **先拷贝再遍历** —— 监听器里再 save/clear 会改数组,倒着遍历也一样
    for (const l of [...this.listeners]) {
      l(this.tokenValue)
    }
  }
}
