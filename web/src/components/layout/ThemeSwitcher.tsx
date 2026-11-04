/**
 * ThemeSwitcher —— 顶栏主题切换
 *
 * §2.8 #19 P1 —— 提供 light / dark 两种主题
 *  - 'realDark' 暂不实现 UI(留为未来"紧凑暗色"扩展)
 *  - 切换走 useUIStore.setTheme,自动持久化(localStorage)
 *  - main.tsx::LangRoot 监听 theme 变化,ConfigProvider algorithm + data-theme 跟着切
 */
import React from 'react'
import { Button, Dropdown, Tooltip } from 'antd'
import { SunOutlined, MoonOutlined, DesktopOutlined } from '@ant-design/icons'
import { useUIStore } from '@/stores/ui'
import { useI18n } from '@/locales'

export const ThemeSwitcher: React.FC = () => {
  const { t } = useI18n()
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)

  const items = [
    { key: 'light', icon: <SunOutlined />, label: t('theme.light') },
    { key: 'dark', icon: <MoonOutlined />, label: t('theme.dark') },
  ]
  // 嵌套顺序有讲究,别改回 <Tooltip><Dropdown> —— 原因见 LanguageSwitcher 的同款注释:
  // Tooltip 的子组件必须把 ref 透传到真实 DOM,否则 rc-resize-observer 会退回
  // ReactDOM.findDOMNode,在 StrictMode 下报 deprecated 警告。
  return (
    <Dropdown
      menu={{
        items,
        selectable: true,
        selectedKeys: [theme === 'realDark' ? 'dark' : theme],
        onClick: ({ key }) => setTheme(key as 'light' | 'dark'),
      }}
      placement="bottomRight"
      trigger={['click']}
    >
      <Tooltip title={t('theme.switch')}>
        <Button
          type="text"
          icon={theme === 'dark' || theme === 'realDark' ? <MoonOutlined /> : <SunOutlined />}
          aria-label={t('theme.switch')}
        />
      </Tooltip>
    </Dropdown>
  )
}

// 占位:realDark 未来可能走 DesktopOutlined / 紧凑布局
export const _ThemeIconPlaceholder = DesktopOutlined

export default ThemeSwitcher
