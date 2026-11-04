/**
 * LanguageSwitcher —— 顶栏语言切换 (§2.6 #8)
 *
 * 切换走 useUIStore.setLang:
 *  - 自动持久化(localStorage,跟 theme 同一份)
 *  - main.tsx::LangRoot 监听 lang 变化,ConfigProvider locale + dayjs locale 跟着切
 *  - 业务文案 t() 由 useI18n 即时响应
 */
import React from 'react'
import { Button, Dropdown, Tooltip } from 'antd'
import { GlobalOutlined } from '@ant-design/icons'
import { useUIStore, type Lang } from '@/stores/ui'
import { useI18n } from '@/locales'

export const LanguageSwitcher: React.FC = () => {
  const { t } = useI18n()
  const lang = useUIStore((s) => s.lang)
  const setLang = useUIStore((s) => s.setLang)

  const items = [
    { key: 'zh-CN', label: t('lang.zh-CN') },
    { key: 'en-US', label: t('lang.en-US') },
  ]
  // 嵌套顺序有讲究,别改回 <Tooltip><Dropdown>:
  // Tooltip 内部要测触发元素的宽度(rc-resize-observer),它靠子组件把 ref
  // 透传到真实 DOM 节点上。Dropdown 的 ref 停在 rc-trigger 实例(带
  // nativeElement),不是 HTMLElement,于是 rc-resize-observer 退回
  // ReactDOM.findDOMNode → React 18 StrictMode 下报
  // "findDOMNode is deprecated"。
  // 换成 Tooltip 直接包 Button 后,ref 落在 <button> 上,警告消失。
  // 点击仍可用:@rc-component/trigger 显式支持嵌套 Trigger(源码
  // // Pass props into cloneProps for nest usage),外层事件会与内层组合后
  // 一路透传到 Button。
  return (
    <Dropdown
      menu={{
        items,
        selectable: true,
        selectedKeys: [lang],
        onClick: ({ key }) => setLang(key as Lang),
      }}
      placement="bottomRight"
      trigger={['click']}
    >
      <Tooltip title={t('lang.switch')}>
        <Button
          type="text"
          icon={<GlobalOutlined />}
          aria-label={t('lang.switch')}
        />
      </Tooltip>
    </Dropdown>
  )
}

export default LanguageSwitcher
