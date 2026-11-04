/**
 * Sidebar —— 侧边栏菜单
 *
 * 数据源:usePermissionStore.menus(后端按当前身份返回)
 * 展示名:走 i18n —— `t('resource.' + r.code.replace(/[:.\-]/g, '_'), r.code)`
 *   后端 sys_resource.name 与 code 同值(零 i18n 责任),前端用 code 派生翻译 key
 *   缺翻译时回落到 code,确保新资源不会显示空白
 *
 * ⚠️ **待办角标挂在「我的审批」(menu.approval)上**(M88)——
 *   角标数是 `box=todo` 的条数,即"有 N 张单子在等我审"。
 *   **不是"我发起的"** —— 发起数不是等待数,挂上去会变成"我催别人"的提示。
 */
import React, { useMemo } from 'react'
import { Badge, Menu, MenuProps } from 'antd'
import { useLocation, useNavigate } from 'react-router-dom'
import { usePermissionStore } from '@/stores/permission'
import { useUIStore } from '@/stores/ui'
import { useI18n } from '@/locales'
import { useApprovalTodoCount } from '@/hooks/useEventStream'
import { Resource } from '@/types/permission'
// ⚠️ **图标词表是共享的**(2026-10-30 抽到 constants/resourceIcons.ts)——
//   资源编辑弹窗的「图标」下拉框也要用同一张表,抄一份迟早对不上。
import { resolveResourceIcon } from '@/constants/resourceIcons'
// ⚠️ **菜单显示名走 `./pageName.ts` 的 `resolveMenuLabel`**(2026-10-24 抽出)——
//   面包屑与 tab 标题用的是**同一个函数**,三处显示的名字必然一致。
//   之前这里自己写了一份 `resolveLabel`,面包屑又写了另一份(走后端 name),
//   结果英文环境下侧边栏是英文、面包屑是中文。
import { resolveMenuLabel } from './pageName'

type MenuItem = Required<MenuProps>['items'][number]

function resolveIcon(code: string): React.ReactNode {
  const prefix = code.split(':')[0] || ''
  // ⚠️ `resolveResourceIcon` 找不到时返回 null(不是 undefined)——
  //   所以这里用 `??` 接着试完整 code,不能写成 `||`(虽然 null 也能过,
  //   但语义上"没找到"和"有值但为空"要分清)。
  return resolveResourceIcon(prefix) ?? resolveResourceIcon(code)
}

export const Sidebar: React.FC = () => {
  const { t } = useI18n()
  const menus = usePermissionStore((s) => s.menus)
  const collapsed = useUIStore((s) => s.sidebarCollapsed)
  // ⚠️ **只读,不开连接** —— 见 useEventStream 里 useApprovalTodoCount 的注释:
  //   连接的持有者是常驻的 BasicLayout,侧边栏只负责显示一个数。
  const approvalTodoCount = useApprovalTodoCount()
  const location = useLocation()
  const navigate = useNavigate()

  const items = useMemo<MenuItem[]>(() => {
    const toItems = (resources: Resource[]): MenuItem[] =>
      resources
        // 防御性过滤:只接受 code 以 menu. 开头的资源
        //   后端 accessible('menu') 应已按 type=menu 过滤, 但若 DB 缺 seed 数据
        //   (api 类型资源混进), 此处兜底防止叶子按钮级条目污染 Sidebar
        .filter((r) => r.code?.startsWith('menu.'))
        .filter((r) => r.visible !== false)
        .sort((a, b) => (a.sort || 0) - (b.sort || 0))
        .map((r) => {
          const label = resolveMenuLabel(t, r)
          const item: MenuItem = {
            key: r.path || r.code,
            icon: r.icon
              ? <span>{typeof r.icon === 'string' ? resolveIcon(r.icon) : r.icon}</span>
              : resolveIcon(r.code),
            // 菜单优先 resourceMenu.menu_*, 按钮级 resource.<code>, 都没有回落 code 自身
            label:
              r.code === 'menu.approval' ? (
                // ⚠️ **showZero={false}** —— 没有待办时不显示"0"那个小红点。
                //   count 给 0 而 showZero 为 true 的话,菜单右边永远挂一个
                //   "0",看久了像坏了。
                <Badge
                  count={approvalTodoCount ?? 0}
                  size="small"
                  showZero={false}
                  offset={[10, -2]}
                  title={t('approval.pendingBadge')}
                >
                  {label}
                </Badge>
              ) : (
                label
              ),
          }
          if (r.children?.length) {
            ;(item as { children?: MenuItem[] }).children = toItems(r.children)
          }
          return item
        })
    return toItems(menus)
  }, [menus, t, approvalTodoCount])

  // 根据当前路径找匹配 key —— 递归遍历, 命中项 + 其全部祖先都进 selectedKeys
  // 同时收集所有命中项的祖先 key 进 ancestorKeys, 让父菜单(如「系统管理」)自动展开
  //
  // 需求: /system/role 页面下, 侧边栏
  //   - '系统管理' + '角色管理' 都要高亮 → selectedKeys
  //   - '系统管理' 子菜单要展开    → ancestorKeys (仅祖先, 不含自身)
  //   - menu.system (key /system)         pathname.startsWith('/system/') ✓ 命中
  //   - menu.system.role (key /system/role)  pathname === '/system/role'  ✓ 命中
  //   - 两者都进 selectedKeys → antd Menu 同时给父子打 selected 样式(橙色底)
  //   - '/system' 进 ancestorKeys → '系统管理' 子菜单展开
  //
  // 实现要点: 用闭包 + 数组传参避免 children 共享同一祖先链
  const { selectedKeys, ancestorKeys } = useMemo(() => {
    const selected: string[] = []
    const opened: string[] = []

    const walk = (item: MenuItem, ancestors: string[]): void => {
      const key = String(item?.key || '')
      const matches =
        !!key && (location.pathname === key || location.pathname.startsWith(`${key}/`))

      if (matches) {
        selected.push(...ancestors, key)
        // 子菜单展开: 命中项的所有祖先. 命中项自身作为 selected 即可, 不必进 opened
        for (const a of ancestors) {
          if (!opened.includes(a)) opened.push(a)
        }
      }

      const children = (item as { children?: MenuItem[] }).children
      if (children?.length) {
        for (const child of children) {
          walk(child, [...ancestors, key])
        }
      }
    }

    for (const item of items) {
      walk(item, [])
    }
    return { selectedKeys: selected, ancestorKeys: opened }
  }, [items, location.pathname])

  /**
   * ⚠️ **M103:展开状态必须受控(controlled),不能只给 `defaultOpenKeys`。**
   *
   *   antd 的 `defaultOpenKeys` 是**挂载时一次性**的值,之后不跟随:
   *     antd Menu → rc-menu `useMergedState(defaultOpenKeys, { value: openKeys })`
   *     → rc-util 的 `useControlledState` 只在 `value === undefined` 时
   *       **在挂载那一刻**用 defaultValue 初始化内部 state,之后 defaultValue
   *       再变也不会同步。
   *
   *   后果(实测复现):从 `/system/user` 切到 `/owner/list` 之后,
   *   「业主管理」这一组**不展开**,当前页在侧边栏里根本看不见 ——
   *   用户看到的是"菜单丢了",而且不报错。
   *
   *   修法:自己持有 `openKeys` state,路由变化时把当前页的祖先 key **并进去**,
   *   并把 `onOpenChange` 接上(受控模式下不接的话用户点不动菜单)。
   *
   *   为什么是「并进去」而不是「替换成祖先集合」:
   *   替换会把用户**手动展开**的其它分组也一起收掉(点一下别的页面,
   *   刚展开的「业主管理」就合上了),观感更差。合并只保证
   *   "当前页所在的那一组永远展开",其余交给用户自己控制。
   */
  const [openKeys, setOpenKeys] = React.useState<string[]>(() => ancestorKeys)

  React.useEffect(() => {
    setOpenKeys((prev) => {
      const missing = ancestorKeys.filter((k) => !prev.includes(k))
      // 没有新增 → 返回原引用,React 直接跳过重渲染(避免每次 render 都 setState)
      return missing.length === 0 ? prev : [...prev, ...missing]
    })
  }, [ancestorKeys])

  return (
    <div
      style={{
        // 菜单区独立滚动: flex: 1 占满 Sider 子容器剩余高度, min-height: 0 允许 overflow
        flex: 1,
        minHeight: 0,
        overflowY: 'auto',
        overflowX: 'hidden',
        padding: '4px 8px 16px',
      }}
    >
      <Menu
        mode="inline"
        theme="light"
        items={items}
        selectedKeys={selectedKeys}
        openKeys={openKeys}
        onOpenChange={(keys) => setOpenKeys(keys)}
        inlineCollapsed={collapsed}
        onClick={({ key }) => {
          if (key.startsWith('/')) navigate(key)
        }}
        style={{ borderRight: 0, background: 'transparent' }}
      />
    </div>
  )
}

export default Sidebar
