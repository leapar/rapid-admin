/**
 * pageName 规则单测
 *
 * ⚠️ **这个模块为什么值得有测试**:它是「面包屑」和「tab 标题」共用的
 *   **唯一一份**路径 → 页面名规则。规则一改,两个地方同时变 ——
 *   改错了不会报错,只会两个地方同时显示错的名字。
 *   而且它承载了一条硬约束:**永不掉回整条 path**(tab 显示 "/system/user"
 *   那个 bug 就是从这儿来的)。
 *
 * 本文件同时是 **web/ 测试设施的第一个真实用例** —— 它验证了:
 *   `@/` 别名能解析、TS 能编译、vitest 能跑。
 */
import { describe, it, expect } from 'vitest'
import {
  codeToI18nKey,
  findLongestPrefix,
  lastSegment,
  resolveMenuLabel,
  resolvePageTitle,
  type Translate,
} from '@/components/layout/pageName'
import type { Resource } from '@/types/permission'

/** 造一个菜单资源 */
function menu(code: string, name: string, path?: string, children?: Resource[]): Resource {
  return { id: code, code, name, type: 'menu', sort: 0, path, children }
}

/** 假翻译表 */
const DICT: Record<string, string> = {
  'resource.user_list': '用户管理',
  'resourceMenu.menu_system': '系统管理',
  'tab.page.configAudit': '审计日志',
  'tab.page.profile': '个人中心',
  'resourceMenu.menu_approval': '我的审批',
}

const t: Translate = (key, fallback) => DICT[key] ?? fallback ?? ''

describe('codeToI18nKey', () => {
  it('menu. 前缀走 resourceMenu 命名空间', () => {
    expect(codeToI18nKey('menu.system')).toEqual(['resourceMenu.menu_system'])
  })

  it('非 menu. 前缀走 resource 命名空间,分隔符统一换成下划线', () => {
    expect(codeToI18nKey('user:list')).toEqual(['resource.user_list'])
    expect(codeToI18nKey('a.b-c')).toEqual(['resource.a_b_c'])
  })
})

describe('resolveMenuLabel', () => {
  it('有 i18n 翻译时用翻译', () => {
    expect(resolveMenuLabel(t, menu('user:list', '用户', '/system/user'))).toBe('用户管理')
  })

  it('缺翻译时回落后端的中文 name', () => {
    expect(resolveMenuLabel(t, menu('nope:x', '兜底名'))).toBe('兜底名')
  })

  it('缺翻译且 name 为空时回落到 code —— 不能返回空串', () => {
    // 空标题会让侧边栏出现一个"点了没反应"的空菜单项
    expect(resolveMenuLabel(t, menu('nope:x', ''))).toBe('nope:x')
  })
})

describe('lastSegment', () => {
  it('取路径末段', () => {
    expect(lastSegment('/system/user')).toBe('user')
    expect(lastSegment('/system/config/audit')).toBe('audit')
  })

  it('空路径返回空串', () => {
    expect(lastSegment('')).toBe('')
    expect(lastSegment('/')).toBe('')
  })
})

describe('findLongestPrefix', () => {
  const menus: Resource[] = [
    menu('menu.system', '系统管理', '/system', [
      menu('user:list', '用户管理', '/system/user'),
      menu('config:list', '系统配置', '/system/config'),
    ]),
  ]

  it('命中嵌套的最深一层,而不是父级', () => {
    const hit = findLongestPrefix(menus, '/system/user')
    expect(hit).not.toBeNull()
    expect(hit?.matchedPath).toBe('/system/user')
    expect(hit?.chain.map((r) => r.code)).toEqual(['menu.system', 'user:list'])
  })

  it('没命中时返回 null', () => {
    expect(findLongestPrefix(menus, '/other/x')).toBeNull()
  })

  it('子节点比父节点深时选子节点', () => {
    const hit = findLongestPrefix(menus, '/system/config')
    expect(hit?.matchedPath).toBe('/system/config')
  })
})

describe('resolvePageTitle', () => {
  const menus: Resource[] = [
    menu('menu.system', '系统管理', '/system', [
      menu('user:list', '用户管理', '/system/user'),
      menu('config:list', '系统配置', '/system/config'),
    ]),
    menu('menu.approval', '我的审批', '/approval'),
  ]

  it('路径正好等于菜单 path → 菜单名', () => {
    expect(resolvePageTitle('/system/user', menus, t)).toBe('用户管理')
  })

  it('子页且末段在登记表里 → 登记名', () => {
    expect(resolvePageTitle('/system/config/audit', menus, t)).toBe('审计日志')
  })

  it('详情页(动态段)→ 菜单名 + 末段,避免同名 tab 分不清', () => {
    expect(resolvePageTitle('/approval/123', menus, t)).toBe('我的审批 #123')
  })

  it('没命中菜单但末段在登记表里 → 登记名', () => {
    expect(resolvePageTitle('/profile', [], t)).toBe('个人中心')
  })

  it('没命中菜单也没登记 → 末段原文(而不是整条 path)', () => {
    expect(resolvePageTitle('/unknown/x', [], t)).toBe('x')
  })

  it('空路径不炸,返回空串', () => {
    expect(resolvePageTitle('', [], t)).toBe('')
  })

  it('永远不返回整条 path —— 这条是这个模块存在的全部意义', () => {
    // tab 标题曾经直接把 path 当名字,于是 tab 上写着 "/system/user"
    for (const p of ['/system/user', '/system/config/audit', '/approval/9', '/profile']) {
      expect(resolvePageTitle(p, menus, t)).not.toBe(p)
    }
  })
})
