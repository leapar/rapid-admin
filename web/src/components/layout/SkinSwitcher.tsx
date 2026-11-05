/**
 * SkinSwitcher —— 顶栏皮肤切换(docs/theming.md P1)
 *
 * 2026-11-05 皮肤扁平化:皮肤 = **整体外观**(含明暗),五个同级选项
 * 白色(默认)/ 黑色 / 科技蓝 / 政务红 / 文博棕金 —— 点谁切谁,一步到位;
 * 没有"明暗切换器"这一层(旧 ThemeSwitcher 已退役,想暗色就选黑色皮肤)。
 *
 * 清单来自 registry(listSkins,声明顺序即展示顺序),不走 i18n ——
 * 皮肤名是皮肤自己的属性(定义在 skins/<id>.ts 的 name)。
 */
import type React from 'react'
import { Button, Dropdown, Tooltip } from 'antd'
import { BgColorsOutlined, CheckOutlined } from '@ant-design/icons'
import { useUIStore } from '@/stores/ui'
import { getSkin, listSkins } from '@/theme/registry'
import { useI18n } from '@/locales'

/** 黑白皮肤的圆点特例色:brand.primary 是品牌橙,但圆点要表达的是外观明暗 */
const APPEARANCE_DOT: Record<string, string> = {
  white: '#ffffff',
  black: '#1f1f1f',
}

const SwatchDot: React.FC<{ color: string }> = ({ color }) => (
  <span
    style={{
      display: 'inline-block',
      width: 14,
      height: 14,
      marginRight: 8, // 色板圆点与文字的间隔(antd 对自绘 icon 不加 margin)
      borderRadius: '50%',
      background: color,
      border: '1px solid rgba(0,0,0,0.15)',
      verticalAlign: 'middle',
    }}
  />
)

export const SkinSwitcher: React.FC = () => {
  const { t } = useI18n()
  const skin = useUIStore((s) => s.skin)
  const setSkin = useUIStore((s) => s.setSkin)
  const active = getSkin(skin) // 未知 id(含旧 orange)已由 registry 降级/别名

  const items = listSkins().map((s) => ({
    key: s.id,
    icon:
      s.id === active.id ? (
        <CheckOutlined />
      ) : (
        // 圆点:黑白皮肤表达外观明暗(特例),品牌皮肤取自身主色
        // (动态 —— 自定义皮肤注册后自动正确)
        <SwatchDot
          color={APPEARANCE_DOT[s.id] ?? getSkin(s.id).brand.primary}
        />
      ),
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
