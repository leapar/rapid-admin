/**
 * 字典 `is_active` 的**边界转换** —— 表单层 ↔ wire 层(2026-10-28 M107)
 *
 * ## 为什么需要转换
 *
 * 这一层有两个互不兼容的表示:
 *
 * | 层 | 表示 | 谁定的 |
 * |---|---|---|
 * | **表单层** | `1` / `0` | antd `Select` 的 option value **只收 `string \| number`**,塞 boolean 进不去 |
 * | **wire 层** | `true` / `false` | 后端 `model.SysDict.IsActive` 是 Go `bool`,`json:"is_active"` 序列化出来就是布尔 |
 *
 * (SQLite 里存成 1 / 0 那是**存储层**表示,不是 wire —— 别拿它当契约。)
 *
 * ## 为什么 wire 层必须是 boolean,不能退化成 1 / 0
 *
 * 后端 `UpdateDict` handler 走 `h.BindJSON(c, &body)` 绑到 `model.SysDict`,
 * Go 的 `encoding/json` 把 `1` 往 bool 字段里塞会**直接报错**
 * ("cannot unmarshal number into Go struct field ... of type bool"),
 * 整个 PATCH 失败 —— 不是"当成启用",是**整条请求挂掉**。
 *
 * ## 为什么单独一个文件
 *
 * ① **能测**:字典页整页以前**一条测试都没有**,转换逻辑藏在 `onSubmit` 里,改坏了没人拦。
 *    抽成纯函数不是为了好看,是为了**能测**(见 `__tests__/isActive.test.ts`)。
 * ② **不污染页面模块**:这两个函数是页面组件文件里的**非组件导出**,
 *    放在一起会触发 `react-refresh/only-export-components` 警告
 *    (而本仓 `npm run lint` 是 `--max-warnings 0`,警告即红)。
 *
 * ⚠️ 以前 `DictItem.is_active` 在 `api/config.ts` 里声明成 `number`,
 *   页面只能写 `v === 1 || v === true` 同时兼容两种 —— 兜底**掩盖**了类型撒谎,
 *   一旦后端改 schema 就静默走错分支(2026-10-18 踩过:全表 113 行显示成"禁用")。
 *   M107 把 `api/config.ts` 的声明改成 `boolean`,兜底随之删掉。
 */

/**
 * 表单层(1 / 0) → wire 层(boolean)
 *
 * ⚠️ 返回值必须是**真的 boolean**,不能是 `1` / `0` —— 见文件头。
 */
export function toWireIsActive(formValue: number): boolean {
  return formValue === 1
}

/**
 * wire 层(boolean) → 表单层(1 / 0)
 *
 * 反向:打开编辑弹窗时把后端返回的 boolean 填进 antd Select(只收 number)。
 */
export function toFormIsActive(wireValue: boolean): number {
  return wireValue ? 1 : 0
}
