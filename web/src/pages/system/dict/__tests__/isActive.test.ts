/**
 * 字典页 · `is_active` 的表单层 ↔ wire 层转换(2026-10-28 M107)
 *
 * ## 这条网守的是什么
 *
 *   字典页的编辑弹窗里,「启用 / 禁用」是一个 antd `Select`:
 *     · 表单层的 option value 是 **1 / 0** —— `EditFieldOption.value` 只收
 *       `string | number`,塞 boolean 进不去;
 *     · wire 上是 **boolean** —— 后端 `model.SysDict.IsActive` 是 Go `bool`。
 *
 *   ⚠️ **这条边界必须是 boolean,不能是 1 / 0**:
 *     后端 `UpdateDict` handler 走 `h.BindJSON(c, &body)` 绑到 `model.SysDict`,
 *     Go 的 `encoding/json` 把 `1` 往 bool 字段里塞会**直接报错**
 *     ("cannot unmarshal number into Go struct field ... of type bool"),
 *     整个 PATCH 失败 —— 不是"当成启用",是**整条请求挂掉**。
 *
 *   ⚠️ 字典页以前**一条测试都没有**,转换逻辑藏在 `onSubmit` 里,改坏了没人拦。
 *     这两个纯函数是从 onSubmit 里抽出来的 —— 抽出来不是为了好看,是为了**能测**。
 *     (放在 `../isActive` 而不是页面组件文件 `../index`:页面文件里放非组件导出会触发
 *      `react-refresh/only-export-components` 警告,而本仓 lint 是 `--max-warnings 0`。)
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `toWireIsActive` 改成 `formValue === 1 ? 1 : 0`(退回发 1/0)→ 立刻变红。
 *   · 把 `toFormIsActive` 改成 `wireValue ? 1 : 0` 之外的任何写法 → 立刻变红。
 */
import { describe, it, expect } from 'vitest'
import { toWireIsActive, toFormIsActive } from '../isActive'

describe('字典页 · is_active 的边界转换(M107)', () => {
  it('表单层 1 → wire 层 true', () => {
    expect(toWireIsActive(1)).toBe(true)
  })

  it('表单层 0 → wire 层 false', () => {
    expect(toWireIsActive(0)).toBe(false)
  })

  it('wire 层 true → 表单层 1', () => {
    expect(toFormIsActive(true)).toBe(1)
  })

  it('wire 层 false → 表单层 0', () => {
    expect(toFormIsActive(false)).toBe(0)
  })

  it('往返一致(1 → true → 1)', () => {
    expect(toFormIsActive(toWireIsActive(1))).toBe(1)
    expect(toFormIsActive(toWireIsActive(0))).toBe(0)
  })

  it('wire 层必须是 **boolean**,不是 1 / 0(Go 绑 bool 字段收到数字会整条请求报错)', () => {
    const wire = toWireIsActive(1)
    expect(typeof wire).toBe('boolean')
    expect(wire).not.toBe(1)
  })
})
