/**
 * 行操作的**形状**与**可见性判定**
 *
 * ⚠️ **为什么从 RowActions.tsx 里拆出来**
 *
 *   判定逻辑要被**两处**用:
 *   · `<RowActions>`    —— 表格里的行内按钮;
 *   · `<CardActionsMenu>` —— 卡片右上角的「⋯」悬浮菜单。
 *
 *   两处各抄一份 ⇒ 抄的那份迟早跟真的那份对不上 ⇒ 卡片里的操作跟表格不一致
 *   (用户看着就是 bug:表格里能删、卡片里删不了)。
 *
 * ⚠️ **为什么单独一个文件**
 *
 *   组件文件里导出**非组件**的东西会踩 `react-refresh/only-export-components`
 *   (热更新时整页失效),而本仓 lint 是 `--max-warnings 0` ⇒ 纯函数必须单独
 *   文件。同 MediaStatusTag.tsx 把常量挪到 mediaStatus.ts 是一个道理。
 */

/** 一个行操作(表格里的按钮 / 卡片菜单里的一项,共用这个形状) */
export interface RowAction<T = unknown> {
  /** 唯一 key */
  key: string
  /** 按钮文案 */
  label: string
  /** 权限码(可选):不传=总可见;传了但 caller 没权限则不渲染 */
  perm?: string
  /** 任意角色 code,符合其一就显示 */
  roleAnyOf?: string[]
  /** 危险操作(红色) */
  danger?: boolean
  /** 禁用 */
  disabled?: boolean
  /** 禁用时 tooltip */
  disabledTip?: string
  /** 点击回调 */
  onClick: (row: T) => void
}

/**
 * 判定"这个操作该不该露出来"要用的两个函数。
 *
 * ⚠️ 与 `usePermission()` 返的 `has` / `hasRole` **同形** —— 传进来就行,
 *   本模块不碰 store(免得又出现"组件订阅了、纯函数没订阅"的两套口径)。
 */
export interface RowActionJudge {
  /** 有没有这个权限码 */
  has: (perm: string) => boolean
  /** 有没有这个角色 code */
  hasRole: (roleCode: string) => boolean
}

/**
 * 按权限 / 角色过滤出**该露出来**的操作。
 *
 * ⚠️ **判据只有这一处** —— 行内按钮与卡片菜单必须同口径,否则同一个账号在
 *   表格里能删、在卡片里删不了。
 */
export function visibleRowActions<T>(
  actions: RowAction<T>[],
  judge: RowActionJudge,
): RowAction<T>[] {
  return actions.filter((a) => {
    if (a.perm && !judge.has(a.perm)) return false
    if (a.roleAnyOf && !a.roleAnyOf.some((r) => judge.hasRole(r))) return false
    return true
  })
}
