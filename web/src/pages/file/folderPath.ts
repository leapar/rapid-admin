/**
 * 文件夹路径的小工具 —— **表格视图和卡片视图共用**(M144)
 *
 * ⚠️ **为什么单独一个文件** —— 两个视图都要"从 path 反推父级路径",
 *   写在组件文件里就得导出一个普通函数,而 `react-refresh/only-export-components`
 *   会因为"组件文件里导出了非组件"报 warning —— 本仓 lint 是
 *   `--max-warnings 0`,warning 直接变错误。
 *
 * ⚠️ **不要自己拼 path** —— path 是后端按祖先链算好的,前端只做"截父级"这一件事。
 */

/**
 * 父级路径 —— "A/图纸" → "A";根级("图纸")→ ""
 *
 * ⚠️ **按最后一个 "/" 切是安全的** —— 后端禁止文件夹名里带 "/"
 *   (service/folder_impl.go 的 Create),所以路径里只有分隔符,
 *   没有名字自带的斜杠。
 */
export function parentPathOf(path: string): string {
  const i = (path || '').lastIndexOf('/')
  return i < 0 ? '' : path.slice(0, i)
}
