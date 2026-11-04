/**
 * 资源编辑弹窗的纯规则 —— 单测
 *
 * 2026-10-30 用户报的三件事里,有两件的"判定"都在这个文件里:
 *   · HTTP 方法的「全部」怎么翻译成提交载荷
 *   · 父资源能不能跨类型选
 *
 * ⚠️ **为什么规则要抽成纯函数** —— 这些判定散在 JSX 里的时候,
 *   只能靠"渲染出来点一点"来验,一条规则改动要跑十几秒的 DOM 用例。
 *   抽出来之后,规则本身是毫秒级可测的。
 *
 * ⚠️ **这些规则在后端有一份镜像**(server/internal/service/role_impl.go 的
 *   `allowedParentTypes`)—— 改一边必须改另一边。
 */
import { describe, it, expect } from 'vitest'
import {
  allowedParentTypes,
  buildParentCandidates,
  descendantIds,
  normalizeIconPayload,
  normalizeMethodPayload,
  METHOD_ALL,
  ICON_NONE,
} from '../resourceRules'
import type { Resource } from '@/types/permission'

/** 造一条资源 */
function res(
  id: string,
  type: Resource['type'],
  parent_id = '0',
  name = id,
  code = id,
): Resource {
  return { id, code, name, type, parent_id, sort: 1 }
}

describe('父资源类型规则', () => {
  it('菜单只能挂在菜单下', () => {
    expect(allowedParentTypes('menu')).toEqual(['menu'])
  })

  // ⚠️ 2026-11-03 用户拍板:parent_id 只有菜单树一个读者,
  //    按钮/接口挂父级是死数据 ⇒ 白名单清空,编辑弹窗也不再渲染父级字段
  it('按钮不允许有父级', () => {
    expect(allowedParentTypes('button')).toEqual([])
  })

  it('接口不允许有父级', () => {
    expect(allowedParentTypes('api')).toEqual([])
  })
})

describe('buildParentCandidates', () => {
  /** 一棵混合树:菜单 → 按钮 → 接口 */
  const TREE: Resource[] = [
    res('m1', 'menu', '0', '系统管理'),
    res('m2', 'menu', 'm1', '用户管理'),
    res('m3', 'menu', 'm2', '角色管理'),
    res('b1', 'button', 'm2', '新建用户'),
    res('a1', 'api', 'b1', '用户列表接口'),
  ]

  it('菜单的候选项里只有菜单', () => {
    const out = buildParentCandidates(TREE, { id: 'm2', type: 'menu' })
    expect(out.map((o) => o.value)).toEqual(['m1'])
  })

  it('按钮的候选项为空(不允许挂父级)', () => {
    expect(buildParentCandidates(TREE, { id: 'b1', type: 'button' })).toEqual([])
  })

  it('接口的候选项为空(不允许挂父级)', () => {
    expect(buildParentCandidates(TREE, { id: 'a1', type: 'api' })).toEqual([])
  })

  it('排除自己和自己的后代(避免父子成环)', () => {
    const out = buildParentCandidates(TREE, { id: 'm1', type: 'menu' })
    const values = out.map((o) => o.value)
    expect(values).not.toContain('m1') // 自己
    expect(values).not.toContain('m2') // 儿子
    expect(values).not.toContain('b1') // 孙子
    expect(values).not.toContain('a1') // 曾孙
  })

  it('层级深度用「— 」前缀表示(顶级没有前缀)', () => {
    // 编辑三级菜单 m3 ⇒ 候选是 m1(顶级,无前缀)和 m2(深度 1)
    const out = buildParentCandidates(TREE, { id: 'm3', type: 'menu' })
    expect(out.find((o) => o.value === 'm1')?.label).toBe('系统管理 (m1)')
    expect(out.find((o) => o.value === 'm2')?.label).toBe('— 用户管理 (m2)')
  })

  it('祖先链上有环时不死循环', () => {
    // a→b→a(历史脏数据)
    const cyclic: Resource[] = [res('x', 'menu', 'y'), res('y', 'menu', 'x')]
    expect(() => buildParentCandidates(cyclic, { id: 'z', type: 'menu' })).not.toThrow()
  })
})

describe('descendantIds', () => {
  it('找出全部后代', () => {
    const tree = [
      res('a', 'menu', '0'),
      res('b', 'menu', 'a'),
      res('c', 'menu', 'b'),
      res('d', 'button', '0'),
    ]
    expect(Array.from(descendantIds(tree, 'a')).sort()).toEqual(['a', 'b', 'c'])
  })
})

describe('提交载荷归一化', () => {
  it('「全部」→ 空串(后端按"显式清空"处理)', () => {
    expect(normalizeMethodPayload(METHOD_ALL)).toBe('')
  })

  it('没填 → 空串', () => {
    expect(normalizeMethodPayload(undefined)).toBe('')
    expect(normalizeMethodPayload(null)).toBe('')
    expect(normalizeMethodPayload('')).toBe('')
  })

  it('小写方法名统一大写', () => {
    expect(normalizeMethodPayload('get')).toBe('GET')
  })

  it('「无图标」→ 空串', () => {
    expect(normalizeIconPayload(ICON_NONE)).toBe('')
    expect(normalizeIconPayload(undefined)).toBe('')
  })

  it('正常图标 key 原样保留', () => {
    expect(normalizeIconPayload('system')).toBe('system')
  })
})
