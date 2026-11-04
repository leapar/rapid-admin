/**
 * 卡片右上角的「⋯」操作菜单 —— **鼠标移入展开**,操作从上到下排开
 *
 * # 为什么把卡片上的操作收进菜单
 *
 * 缩略图视图里一排五个文字按钮(详情 / 下载 / 设为封面 / 复制直链 / 删除)
 * 会把卡片撑得很宽,窄屏下还换行 ⇒ 卡片高度参差(用户 2026-11-01 提的)。
 * 收进右上角一个「⋯」之后,卡片只占一个角,操作**从上到下**排开,
 * 鼠标移上去就展开。
 *
 * # ⚠️ **动作来源与权限口径必须与表格视图完全一致**
 *
 * 这里吃的是**同一份** `actionsFor(row)`,可见性判定走 `visibleRowActions`
 * (与 `<RowActions>` 是同一个函数)——
 * 表格里能删、卡片里删不了 = 用户看着就是 bug。
 *
 * # ⚠️ **必须用 `usePermission()`(订阅 store)**
 *
 * 不能用 `usePermissionStore.getState()` —— 那是**一次性快照**,不建立订阅 ⇒
 * 切身份 / 收到 perm_changed 之后菜单还是按旧权限渲染
 * (同 `<RowActions>` 的教训,M102)。
 */
import { Button, Dropdown, Tooltip, theme } from 'antd'
import type { MenuProps } from 'antd'
import { EllipsisOutlined } from '@ant-design/icons'
import { usePermission } from '@/hooks/usePermission'
import { useI18n } from '@/locales'
import { visibleRowActions, type RowAction } from './rowAction'

export interface CardActionsMenuProps<T = unknown> {
  /** 操作列表 —— 与表格视图共用同一份 `actionsFor(row)` */
  actions: RowAction<T>[]
  /** 这一行的数据(点击时回传给 onClick) */
  row: T
}

export function CardActionsMenu<T = unknown>({ actions, row }: CardActionsMenuProps<T>) {
  const { t } = useI18n()
  const { token } = theme.useToken()
  const { has, hasRole } = usePermission()

  const visible = visibleRowActions(actions, { has, hasRole })

  // 一个操作都看不见 ⇒ 不摆那个「⋯」(摆一个点开是空的菜单更让人困惑)
  if (visible.length === 0) return null

  const items: MenuProps['items'] = visible.map((a) => ({
    key: a.key,
    /**
     * ⚠️ 禁用项的"为什么不能点"用 Tooltip 说明。
     *
     *   antd 的 disabled 菜单项只给 `<a>` 加 `pointer-events: none`,
     *   菜单项本身仍可悬浮 ⇒ Tooltip 照样能弹(见 antd menu/style 的
     *   `item-disabled` 规则)。
     */
    label: a.disabled && a.disabledTip ? <Tooltip title={a.disabledTip}>{a.label}</Tooltip> : a.label,
    danger: a.danger,
    disabled: a.disabled,
  }))

  return (
    <Dropdown
      /**
       * ⚠️ 悬浮展开是用户提的要求;**click 也留着** ——
       *   键盘用户 Tab 到这个按钮按 Enter 就是一次 click,
       *   没有它这些操作对键盘用户等于不存在。
       */
      trigger={['hover', 'click']}
      placement="bottomRight"
      menu={{
        items,
        onClick: ({ key }) => {
          const hit = visible.find((a) => a.key === key)
          hit?.onClick(row)
        },
      }}
    >
      <Button
        type="text"
        size="small"
        /**
         * ⚠️ **`EllipsisOutlined`(竖着的三个点 ⋮),不是 `MoreOutlined`**
         *   (横着的三个点 ⋯)—— 用户 2026-11-01 明确要的:
         *   「card-actions-trigger 是从上到下的三个点,不是从左到右的三个点」。
         *   竖着的点是"这个菜单往下展开"的通用暗示(纵向 kebab 菜单)。
         */
        icon={<EllipsisOutlined />}
        aria-label={t('common.action')}
        data-testid="card-actions-trigger"
        style={{
          // ⚠️ 覆盖在缩略图上 —— 没有底色的话,浅色图标压在浅色图片上等于看不见
          color: token.colorTextLightSolid,
          background: token.colorBgMask,
          // ⚠️ **压成 28×20 的长方形**(2026-11-01 用户提的:
          //   「现在是 28*28,应该改矮一点」「改矮一点,改宽一点,变成长方形」)
          //   —— 本仓 `Button.controlHeightSM` 被调成了 28,于是 small 按钮是
          //   28×28;贴在卡片角落里太高,压扁成 28×20 才像个"角标"。
          //   `minWidth` 也要一起写 —— antd 给按钮兜了 `minWidth: controlHeight`。
          height: 20,
          minWidth: 28,
          width: 28,
          padding: 0,
        }}
      />
    </Dropdown>
  )
}

export default CardActionsMenu
