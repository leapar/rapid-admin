/**
 * 菜单图标选择器 —— RESOURCE_ICON_MAP 词表的图形化出口
 *
 * # 为什么抽成公共组件
 *
 * 图标下拉有两个消费方:
 *
 *   - 资源管理(system/permission)—— 菜单/按钮的图标
 *   - 业务表单设计器(system/dyn)—— 动态集合的图标
 *
 * 之前资源管理自己拼 options + optionRender,设计器只有一份**纯文字**下拉
 * (2026-11-02 用户报:"现在都是文字,应该显示真实图标出来")——
 * 两份实现迟早对不上:加一组图标,一边画得出一边画不出。
 *
 * ⇒ 一个组件,两个地方 import;词表仍然是唯一的真源。
 *
 * # 选项画的是**真实图标**,不是 key
 *
 * 词表 key('system' / 'job')是**给后端存的**,不是给人看的 ——
 * 用户要看到"选完长什么样",不用去记 key。
 *
 * ⚠️ **label 保持纯字符串** ——
 * ① showSearch 靠 optionFilterProp="label" 过滤,label 是 JSX 就没法搜;
 * ② 折叠态的原生 tooltip(title 属性)取的是选项的 title 字段。
 *   图标由 optionRender / labelRender 另外画,不污染这两处。
 *
 * # 下拉是**多列网格**(2026-11-02 用户:"IconSelect 改成多列显示,
 * 否则太多上下滚动麻烦")
 *
 * 词表扩到 ~90 个图标之后,单列下拉要滚 90 行 ⇒ 挑一个图标变成体力活。
 * ⇒ 下拉改成网格:一格一个磁贴(图标在上、文字在下),横向 6~7 列。
 *
 * ⚠️ **网格是 CSS 实现的,规则在 `styles/global.css` 的 `.icon-select-dropdown` 段**
 *   —— 下拉面板是 antd 用 portal 挂到 body 上的,组件里的局部样式够不着它
 *   (本仓只有两份全局样式表,没有 CSS Modules)。
 *   组件这边负责三件事:
 *     ① `classNames.popup.root` 挂上 `icon-select-dropdown` 类名(CSS 的钩子)
 *     ② `virtual={false}` —— 网格要求所有选项都在 DOM 里
 *        (rc-select 的虚拟列表按"固定行高"绝对定位,模型是"一行一个选项",
 *          与"一格一个选项"的网格互斥)
 *     ③ optionRender / labelRender 把选项、折叠态画成磁贴
 *
 * ⚠️ **labelRender 只改"显示",不改 title** ——
 *   rc-select 折叠态的 title 取自**选项的 title 字段**(不是 label),
 *   所以每个选项都显式带 `title: label`,原生 tooltip 才不会丢。
 */
import { useMemo, type ReactNode } from 'react'
import { Select } from 'antd'
import { BorderOutlined } from '@ant-design/icons'
import { RESOURCE_ICON_KEYS, RESOURCE_ICON_MAP, resolveResourceIcon } from '@/constants/resourceIcons'
import { useI18n } from '@/locales'

/**
 * 下拉面板的可视高度(px)
 *
 * 磁贴一格约 56px 高 ⇒ 320px 看得见 5 行多,剩下的在面板里滚。
 */
const LIST_HEIGHT = 320

export interface IconSelectProps {
  value?: string | null
  onChange?: (value: string) => void
  /**
   * 「无图标」选项 —— 资源管理允许菜单不带图标(提交时归一成空串,
   * 见 resourceRules.normalizeIconPayload);动态集合**必须**有图标
   * (侧边栏菜单项靠它显示)⇒ 不传这个 prop 就不提供该选项。
   */
  noneOption?: { value: string; label: string }
  placeholder?: string
}

/**
 * 磁贴上的图标
 *
 * 词表里没有的 key(比如「无图标」那个哨兵)⇒ 画一个虚线方框占位,
 * 让这一格跟别的格子一样是"图标在上、文字在下",不至于高低不齐。
 */
function tileIcon(value: unknown): ReactNode {
  const key = String(value)
  if (!key || !RESOURCE_ICON_MAP[key]) return <BorderOutlined />
  return resolveResourceIcon(key)
}

export default function IconSelect({ value, onChange, noneOption, placeholder }: IconSelectProps) {
  const { t } = useI18n()
  const noneValue = noneOption?.value
  const noneLabel = noneOption?.label
  const options = useMemo(() => {
    const base = RESOURCE_ICON_KEYS.map((key) => {
      const label = t(`resourceIcon.${key}`)
      // ⚠️ title 必须带上 —— 折叠态的原生 tooltip 取的是选项的 title 字段
      // ⚠️ className 是给 CSS 的钩子 —— antd 那条 `.ant-select-item-option`
      //   是 (0,2,0),我们这条要多一层类名才压得过它(见 global.css 那节)
      return { value: key, label, title: label, className: 'icon-select-option' }
    })
    return noneValue
      ? [
          {
            value: noneValue,
            label: noneLabel ?? '',
            title: noneLabel ?? '',
            className: 'icon-select-option',
          },
          ...base,
        ]
      : base
    // ⚠️ 依赖是拆开的 noneValue/noneLabel 而不是 noneOption ——
    // 调用方每次渲染都新建对象字面量,按对象判等会每次都不一样。
  }, [t, noneValue, noneLabel])
  return (
    <Select
      style={{ width: '100%' }}
      value={value ?? noneValue}
      onChange={(v) => onChange?.(v ?? noneValue ?? '')}
      showSearch
      optionFilterProp="label"
      placeholder={placeholder}
      options={options}
      // ⚠️ **必须关掉虚拟滚动** —— 网格要求所有选项同时在 DOM 里
      virtual={false}
      listHeight={LIST_HEIGHT}
      // ↓ CSS 网格的钩子 + 面板宽度(磁贴一行 64px 起,太窄就没几列)
      classNames={{ popup: { root: 'icon-select-dropdown' } }}
      styles={{ popup: { root: { minWidth: 420, maxWidth: 'calc(100vw - 32px)' } } }}
      optionRender={(opt) => (
        <span className="icon-select-tile">
          <span className="icon-select-tile-icon">{tileIcon(opt.value)}</span>
          <span className="icon-select-tile-label">{opt.label}</span>
        </span>
      )}
      labelRender={(item) => (
        <span className="icon-select-value">
          <span className="icon-select-value-icon">{tileIcon(item.value)}</span>
          <span className="icon-select-value-label">{item.label}</span>
        </span>
      )}
    />
  )
}
