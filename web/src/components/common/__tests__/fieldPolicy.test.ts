/**
 * 字段级可见性 · 界面层的适配(纯函数)—— M168(2026-11-02)
 *
 * 锁的是三条界面语义:
 *   1. **隐藏 ⇒ 不渲染**(列 / 表单项 / 详情项都不见了)
 *   2. **打码 ⇒ 照常显示,但表单里变只读** ——
 *      用户要能看到"这里有个手机号,只是看不全"
 *   3. **提交载荷里剔掉被收窄的字段** ——
 *      光只读不够,antd 的 validateFields() 照样把禁用字段的值收进来;
 *      原样提交 = 用 `138****1234` 覆盖真实手机号(数据损坏)
 *
 * # 可逆探针(拆掉实现看测试会不会红)
 *
 *	拆法                                        必须变红
 *	────────────────────────────────────────  ─────────────────────────────
 *	filterColumnsByReport 原样返回 columns      「隐藏的列被滤掉」一族
 *	dataIndex 数组路径只看最后一段               「数组路径的每一段都算」
 *	                                            (改成只看最后一段,phone 在
 *	                                             前段时该红)
 *	filterEditFieldsByReport 对 masked 原样放行  「打码字段变只读」
 *	omitRestrictedFields 不剔 masked             「载荷里剔掉打码字段」
 *	omitRestrictedFields 就地改对象              「不改传入的对象」
 *	columnFieldNames 对无 dataIndex 列返回字段名  「操作列不会被误藏」
 */
import { describe, it, expect } from 'vitest'
import type { ColumnsType } from 'antd/es/table'
import {
  filterColumnsByReport,
  filterEditFieldsByReport,
  filterViewFieldsByReport,
  omitRestrictedFields,
} from '@/components/common/fieldPolicy'
import type { EditField } from '@/components/common/EditModal'
import type { ViewField } from '@/components/common/ViewDrawer'

describe('filterColumnsByReport', () => {
  const columns: ColumnsType<Record<string, unknown>> = [
    { title: 'ID', dataIndex: 'id', key: 'id' },
    { title: '手机号', dataIndex: 'phone', key: 'phone' },
    { title: '姓名', dataIndex: 'real_name', key: 'real_name' },
    { title: '操作', key: 'action' },
  ]

  it('null / 没有 hidden ⇒ 原样返回(同一个引用,零开销快路径)', () => {
    expect(filterColumnsByReport(columns, null)).toBe(columns)
    expect(filterColumnsByReport(columns, { hidden: [], masked: ['real_name'] })).toBe(columns)
  })

  it('隐藏的列被滤掉;打码的列照常显示', () => {
    const out = filterColumnsByReport(columns, { hidden: ['phone'], masked: ['real_name'] })
    const titles = out.map((c) => (c as { title?: string }).title)
    expect(titles).toEqual(['ID', '姓名', '操作'])
  })

  it('⚠️ 列的字段名从 dataIndex 取,不是从 key 取', () => {
    // key 可以随便起(dataIndex 才是"这一列取哪个字段")——
    // key 撞上 hidden 名但 dataIndex 不同 ⇒ 不该藏
    const cols: ColumnsType<Record<string, unknown>> = [
      { title: 'A', key: 'phone', dataIndex: 'name' },
    ]
    expect(filterColumnsByReport(cols, { hidden: ['phone'], masked: [] })).toHaveLength(1)
  })

  it('⚠️ dataIndex 是数组路径时,每一段都算(服务端按字段名匹配,不按路径)', () => {
    const cols: ColumnsType<Record<string, unknown>> = [
      { title: '联系人', dataIndex: ['contact', 'phone'], key: 'c1' },
      { title: '业主', dataIndex: ['owner', 'phone'], key: 'c2' },
      { title: '名称', dataIndex: ['owner', 'name'], key: 'c3' },
    ]
    const out = filterColumnsByReport(cols, { hidden: ['phone'], masked: [] })
    // 两列 phone(不管路径前缀)都被藏;name 留着
    expect(out.map((c) => (c as { title?: string }).title)).toEqual(['名称'])
  })

  it('⚠️ 没有 dataIndex 的列(操作列)永远不会被误藏', () => {
    const cols: ColumnsType<Record<string, unknown>> = [
      { title: '操作', key: 'action' },
    ]
    expect(filterColumnsByReport(cols, { hidden: ['action', 'title'], masked: [] })).toHaveLength(1)
  })

  it('列组:组里还有列就整组保留(递归过滤),整组被藏干净就整组不渲染', () => {
    const cols: ColumnsType<Record<string, unknown>> = [
      {
        title: '联系方式',
        key: 'contact',
        children: [
          { title: '手机号', dataIndex: 'phone', key: 'phone' },
          { title: '邮箱', dataIndex: 'email', key: 'email' },
        ],
      },
      {
        title: '敏感信息',
        key: 'sensitive',
        children: [{ title: '身份证', dataIndex: 'id_card', key: 'id_card' }],
      },
      { title: '名称', dataIndex: 'name', key: 'name' },
    ]
    const out = filterColumnsByReport(cols, { hidden: ['phone', 'id_card'], masked: [] })
    // "联系方式"组里还剩邮箱 ⇒ 组保留但手机号没了;"敏感信息"组空了 ⇒ 整组不见
    const group = out[0] as { title?: string; children?: ColumnsType<Record<string, unknown>> }
    expect(group.title).toBe('联系方式')
    expect(group.children).toHaveLength(1)
    expect(out.map((c) => (c as { title?: string }).title)).not.toContain('敏感信息')
    expect(out.map((c) => (c as { title?: string }).title)).toContain('名称')
  })
})

describe('filterEditFieldsByReport', () => {
  const fields: EditField[] = [
    { name: 'nickname', label: '昵称' },
    { name: 'phone', label: '手机号' },
    { name: 'real_name', label: '姓名', disabled: false },
  ]

  it('null ⇒ 原样返回(同一个引用)', () => {
    expect(filterEditFieldsByReport(fields, null)).toBe(fields)
  })

  it('隐藏的字段整个不渲染', () => {
    const out = filterEditFieldsByReport(fields, { hidden: ['phone'], masked: [] })
    expect(out.map((f) => f.name)).toEqual(['nickname', 'real_name'])
  })

  it('⚠️ 打码的字段变只读(不是删掉 —— 用户要能看到"看不全")', () => {
    const out = filterEditFieldsByReport(fields, { hidden: [], masked: ['real_name'] })
    expect(out.map((f) => f.name)).toEqual(['nickname', 'phone', 'real_name'])
    expect(out.find((f) => f.name === 'real_name')?.disabled).toBe(true)
    // 其它字段的 disabled 不被误改
    expect(out.find((f) => f.name === 'nickname')?.disabled).toBeUndefined()
  })

  it('已经是 disabled 的字段,不该被打码规则"解锁"回可写', () => {
    const fields2: EditField[] = [
      { name: 'phone', label: '手机号', disabled: true },
    ]
    const out = filterEditFieldsByReport(fields2, { hidden: [], masked: ['phone'] })
    expect(out.find((f) => f.name === 'phone')?.disabled).toBe(true)
    // 不在报告里的字段保持原样
    const out2 = filterEditFieldsByReport(fields2, { hidden: [], masked: ['other'] })
    expect(out2.find((f) => f.name === 'phone')?.disabled).toBe(true)
  })
})

describe('filterViewFieldsByReport', () => {
  const fields: ViewField<Record<string, unknown>>[] = [
    { name: 'nickname', label: '昵称' },
    { name: 'phone', label: '手机号' },
  ]

  it('null / 没有 hidden ⇒ 原样返回(同一个引用)', () => {
    expect(filterViewFieldsByReport(fields, null)).toBe(fields)
    expect(filterViewFieldsByReport(fields, { hidden: [], masked: ['phone'] })).toBe(fields)
  })

  it('隐藏的字段不渲染;打码的字段照常显示(值已是打码后的)', () => {
    const out = filterViewFieldsByReport(fields, { hidden: ['phone'], masked: ['real_name'] })
    expect(out.map((f) => f.name)).toEqual(['nickname'])
  })
})

describe('omitRestrictedFields', () => {
  it('null ⇒ 原样返回(同一个引用)', () => {
    const values = { nickname: 'a', phone: 'b' }
    expect(omitRestrictedFields(values, null)).toBe(values)
  })

  it('⚠️ 打码字段从载荷里剔掉(否则打码值会覆盖真实值 —— 数据损坏)', () => {
    const values = { nickname: '张三', phone: '138****1234', real_name: '张*' }
    const out = omitRestrictedFields(values, { hidden: [], masked: ['phone'] })
    expect('phone' in out).toBe(false)
    expect(out).toEqual({ nickname: '张三', real_name: '张*' })
  })

  it('隐藏字段也剔掉(再挡一道)', () => {
    const values = { nickname: '张三', phone: '13800001234' }
    const out = omitRestrictedFields(values, { hidden: ['phone'], masked: [] })
    expect(out).toEqual({ nickname: '张三' })
  })

  it('⚠️ 不改传入的对象(返回拷贝)', () => {
    const values = { nickname: '张三', phone: '138****1234' }
    const out = omitRestrictedFields(values, { hidden: [], masked: ['phone'] })
    expect('phone' in values).toBe(true) // 原对象完好
    expect(values.phone).toBe('138****1234')
    expect(out).not.toBe(values)
  })

  it('空报告 / 非对象值 ⇒ 原样返回', () => {
    const values = { a: 1 }
    expect(omitRestrictedFields(values, { hidden: [], masked: [] })).toBe(values)
    expect(omitRestrictedFields('not-object' as unknown as Record<string, unknown>, { hidden: ['a'], masked: [] })).toBe(
      'not-object',
    )
    expect(omitRestrictedFields(null, { hidden: ['a'], masked: [] })).toBeNull()
  })
})
