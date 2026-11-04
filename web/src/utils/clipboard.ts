/**
 * 复制文本到剪贴板 —— 带降级(2026-10-30)
 *
 * ⚠️ `navigator.clipboard` **只在安全上下文**(https / localhost)里存在,
 *   而且只在**用户手势**的同步调用栈里可用 —— 在 `await` 之后调用会被拒。
 *   所以两条路都得留:
 *
 *   1. 有 `navigator.clipboard` → 直接用;
 *   2. 没有、或被拒(权限 / 非用户手势 / iframe 没 allow)→ 降级到
 *      `textarea + execCommand`;
 *   3. 降级也失败 → 返回 false。
 *
 * ⚠️ **返回 boolean 而不是抛异常**:复制失败不是"程序出错",是"这次没成功",
 *   调用方要区别于"接口调用失败"来提示用户(手动复制)。
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 被拒了 → 落到下面的降级
  }

  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    // ⚠️ 定位到屏幕外而不是 display:none —— 隐藏的元素 select() 选不中,
    //    execCommand 会返回 false。
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    // jsdom / 老浏览器里 execCommand 可能压根不存在
    return false
  }
}
