/**
 * RowActions —— 行操作列(查看/编辑/删除/... )
 *
 * 替代业务页里散落的 <Space><Button>...</Button></Space> 写法
 *
 * 用法:
 *   <RowActions
 *     actions={[
 *       { key: 'view', label: t('common.detail'), onClick: () => view(row) },
 *       { key: 'edit', label: t('common.edit'), perm: 'user:update', onClick: () => edit(row), disabled: row.isBuiltin },
 *       { key: 'delete', label: t('common.delete'), perm: 'user:delete', danger: true, onClick: () => remove(row), disabled: row.isBuiltin },
 *     ]}
 *   />
 */
import { Button, Space, Tooltip } from 'antd'
import { usePermission } from '@/hooks/usePermission'
import { visibleRowActions, type RowAction } from './rowAction'

/**
 * ⚠️ **类型的家搬到了 rowAction.ts**(2026-11-01)——
 *   卡片的「⋯」菜单(`<CardActionsMenu>`)也要用同一个形状 + 同一套可见性
 *   判定,两处各抄一份迟早对不上 ⇒ 表格里能删、卡片里删不了。
 *   这里原样转出去,已有调用方(`import { RowActions, type RowAction } from
 *   '@/components/common/RowActions'`)不用改。
 */
export type { RowAction }

export interface RowActionsProps<T = unknown> {
  actions: RowAction<T>[]
  row: T
}

export function RowActions<T = unknown>({ actions, row }: RowActionsProps<T>) {
  /**
   * ⚠️ M102:这里**必须用 usePermission()**(订阅 store),不能用
   * `usePermissionStore.getState()`。
   *
   * `getState()` 是一次性的快照读取,**不建立订阅** —— 组件不会因为权限变化
   * 而重渲染。以前这里就是 `getState()`,于是:
   *   切换身份 / SSE 收到 perm_changed → store 里的权限码已经变了,
   *   但**行内按钮还是按旧权限渲染的**:刚被撤掉的"删除"按钮还在,
   *   刚授予的按钮不出现。点下去只会换来后端一个 403。
   *
   * `usePermission()` 订阅 `permissionCodes` / `roleCodes`,
   * 权限一变就重渲染 —— 和 `PermissionGuard` / `RoleGuard` 同一套机制。
   */
  const { has, hasRole } = usePermission()

  // ⚠️ **可见性判定走 `visibleRowActions`** —— 与卡片菜单同一个函数,
  //   两处各判一次迟早对不上(表格里能删、卡片里删不了 = 用户看着就是 bug)。
  const visible = visibleRowActions(actions, { has, hasRole })

  return (
    <Space size={4}>
      {visible.map((a) => {
        /**
         * ⚠️ **这里曾经有一个 `compact` 分支**(2026-10-31 删掉)。
         *
         *   它给按钮加了 `style={{ padding: 0 }}` ⇒ 五个操作变成**一排裸文字**,
         *   而同一个页面的表格视图是带内边距的按钮 ⇒ 两种观感。
         *   更糟的是它的注释写的是「紧凑模式(只显示图标 + Tooltip label)」——
         *   **实现里根本没有图标**,只有文字。
         *
         *   ⇒ 唯一的调用方(文件列表的缩略图视图)改回跟表格一致后,
         *   这个分支**零调用方**,留着就是个陷阱(谁再加一次 compact
         *   就会重现那个观感问题)。所以整条删掉,而不是留着。
         */
        const btn = (
          <Button
            key={a.key}
            type="link"
            size="small"
            danger={a.danger}
            disabled={a.disabled}
            onClick={() => a.onClick(row)}
          >
            {a.label}
          </Button>
        )

        return a.disabled && a.disabledTip ? (
          <Tooltip title={a.disabledTip} key={a.key}>
            {btn}
          </Tooltip>
        ) : (
          <span key={a.key}>{btn}</span>
        )
      })}
    </Space>
  )
}

export default RowActions
