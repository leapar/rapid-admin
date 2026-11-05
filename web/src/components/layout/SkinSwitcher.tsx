/**
 * SkinSwitcher —— 顶栏皮肤切换(品牌色相轴,docs/theming.md P1)
 *
 * 与 ThemeSwitcher(明暗轴)并列:theme 管 light/dark,skin 管品牌色相,
 * 两轴正交 —— 任何皮肤 × 任何明暗都合法,组合由 LangRoot 统一渲染。
 * 皮肤清单来自 registry(内置四套 + 自定义注册),不走 i18n ——
 * 皮肤名是皮肤自己的属性(定义在 skins/<id>.ts 的 name)。
 */
import React from 'react'
import { Button, Dropdown, Tooltip } from 'antd'
import { BgColorsOutlined, CheckOutlined } from '@ant-design/icons'
import { useUIStore } from '@/stores/ui'
import { getSkin, listSkins } from '@/theme/registry'
import { useI18n } from '@/locales'

export const SkinSwitcher: React.FC = () => {
  const { t } = useI18n()
  const skin = useUIStore((s) => s.skin)
  const setSkin = useUIStore((s) => s.setSkin)
  const active = getSkin(skin)

  const items = listSkins().map((s) => ({
    key: s.id,
    icon: s.id === active.id ? <CheckOutlined /> : <span style={{ width: 14 }} />,
    // 皮肤名不进语言包:它是皮肤定义的一部分,不是界面文案
    label: s.name,
  }))

  // 嵌套顺序讲究同 ThemeSwitcher(Tooltip 子组件必须透传 ref,见那边注释)
  return (
    <Dropdown
      menu={{
        items,
        selectable: true,
        selectedKeys: [active.id],
        onClick: ({ key }) => setSkin(key),
      }}
      placement="bottomRight"
      trigger={['click']}
    >
      <Tooltip title={t('theme.skin')}>
        <Button type="text" icon={<BgColorsOutlined />} aria-label={t('theme.skin')} />
      </Tooltip>
    </Dropdown>
  )
}

export default SkinSwitcher
