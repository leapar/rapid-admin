/**
 * 皮肤管理 —— 编辑器 MVP(P3 第一步,docs/theming.md §7.2/§7.3)
 *
 * # 定位与边界(2026-11-05)
 *
 * 面向"想可视化调完色再落文件"的人:编辑 → 实时预览 → 整页试穿 →
 * **导出 TS/JSON**。导出产物 = 标准 SkinDefinition,粘进
 * `web/src/theme/skins/` 并在 registry 挂一行即成为构建内皮肤
 * (§5 上手指南;皮肤守卫测试对它同样生效)。
 *
 * **本期不做**(后续轮次):保存草稿到系统配置 `skin.custom` /
 * `skin.active`(需要 seed 增 key + migrate)、publicConfig 下发自定义
 * 皮肤、按租户多皮肤 —— 与 theming.md §7.3 的"明确不做"边界一致。
 *
 * # 架构对齐
 *
 * - **运行时没有推导层**:suggestSkinParts(derive.ts)只产出"建议值",
 *   生成结果直接落到具体色值,高级模式可逐键覆盖;产物与内置皮肤同构。
 * - **registerSkin 运行时注册**:试穿走 registry 的对象表(后注册者赢),
 *   草稿 id 固定 `custom-draft`,永不与内置 id 冲突;退出/卸载恢复原皮肤。
 * - **注入安全**:整条链路只走 antd token 与 setProperty,不碰 innerHTML。
 */
import React, { useEffect, useMemo, useState } from 'react'
import {
  App,
  Button,
  Card,
  ColorPicker,
  Collapse,
  Divider,
  Input,
  Popconfirm,
  Select,
  Space,
  Tag,
  Tooltip,
  Typography,
  theme as antdTheme,
  ConfigProvider,
} from 'antd'
import { BgColorsOutlined, CopyOutlined, DownloadOutlined, EyeOutlined } from '@ant-design/icons'
import { PageHeader } from '@/components/common/PageHeader'
import { useI18n } from '@/locales'
import { useUIStore } from '@/stores/ui'
import { getSkin, listSkins, registerSkin } from '@/theme/registry'
import { suggestSkin, suggestSkinParts } from '@/theme/derive'
import type { SkinDefinition } from '@/theme/types'

/** 草稿固定 id —— 永不与内置皮肤冲突(registerSkin 后注册者赢) */
const DRAFT_ID = 'custom-draft'

/** 深拷贝(皮肤对象是纯 JSON 树,structuredClone 足够) */
const cloneSkin = (s: SkinDefinition): SkinDefinition =>
  typeof structuredClone === 'function'
    ? structuredClone(s)
    : (JSON.parse(JSON.stringify(s)) as SkinDefinition)

/** 高级模式的键分组(Collapse 面板)—— 键名抄 types.ts 的分组注释;label 是 i18n 键 */
const PALETTE_GROUPS: { key: string; label: string; keys: (keyof SkinDefinition['palette'])[] }[] = [
  {
    key: 'canvas',
    label: 'page.skin.groupCanvas',
    keys: ['colorBgBase', 'colorBgLayout', 'colorBgContainer', 'colorBgElevated', 'colorBgSpotlight', 'colorBgMask'],
  },
  {
    key: 'text',
    label: 'page.skin.groupText',
    keys: ['colorText', 'colorTextSecondary', 'colorTextTertiary', 'colorTextQuaternary', 'colorTextDescription', 'colorTextPlaceholder'],
  },
  { key: 'border', label: 'page.skin.groupBorder', keys: ['colorBorder', 'colorBorderSecondary', 'colorSplit'] },
  {
    key: 'brand',
    label: 'page.skin.groupBrand',
    keys: ['primaryText', 'primaryTextHover', 'primaryTextActive', 'colorPrimaryBg', 'colorPrimaryBgHover', 'colorPrimaryBorder', 'colorPrimaryBorderHover', 'colorLink', 'colorLinkHover', 'colorLinkActive'],
  },
]

const COMP_GROUPS: { key: string; label: string; keys: (keyof SkinDefinition['comp'])[] }[] = [
  { key: 'layout', label: 'page.skin.groupLayout', keys: ['layoutHeaderBg', 'siderBg', 'bodyBg', 'triggerColor', 'headerTextColor', 'headerTextSecondaryColor', 'headerTextTertiaryColor', 'headerBorderColor'] },
  { key: 'menu', label: 'page.skin.groupMenu', keys: ['menuItemBg', 'menuSelectedBg', 'menuSelectedColor', 'menuHoverBg', 'menuHoverColor', 'menuColor', 'menuDarkItemSelectedBg', 'menuDarkItemSelectedColor'] },
  { key: 'table', label: 'page.skin.groupTable', keys: ['tableHeaderBg', 'tableHeaderColor', 'tableBorderColor', 'rowHoverBg', 'rowSelectedBg', 'rowSelectedHoverBg'] },
  { key: 'misc', label: 'page.skin.groupMisc', keys: ['primaryShadow', 'defaultShadow', 'inputActiveShadow', 'paginationItemBg', 'dateCellHoverBg', 'dateCellRangeBg', 'breadcrumbItemColor', 'breadcrumbLastColor', 'breadcrumbSeparatorColor', 'tabsActiveColor', 'selectOptionSelectedColor'] },
]

/** 导出 TS 片段(粘进 skins/<id>.ts 即用) */
function toTsSnippet(skin: SkinDefinition): string {
  const body = JSON.stringify(skin, null, 2)
  return `import type { SkinDefinition } from '../types'\n\nconst skin: SkinDefinition = ${body}\n\nexport default skin\n`
}

/** 触发浏览器下载 */
function download(filename: string, content: string): void {
  const blob = new Blob([content], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

/**
 * cssVars 7 键按当前草稿重算 —— cssVars 不进编辑表单(它只是
 * brand 三态 + palette 品牌浅底系列的镜像),但试穿/导出必须以
 * **改过的**值为准,不能吃克隆来源的旧值。
 */
function withDerivedCssVars(draft: SkinDefinition): SkinDefinition {
  return {
    ...draft,
    cssVars: {
      '--app-primary': draft.brand.primary,
      '--app-primary-hover': draft.brand.hover,
      '--app-primary-active': draft.brand.active,
      '--app-primary-bg': draft.palette.colorPrimaryBg,
      '--app-primary-bg-hover': draft.palette.colorPrimaryBgHover,
      '--app-primary-border': draft.palette.colorPrimaryBorder,
      '--app-primary-border-hover': draft.palette.colorPrimaryBorderHover,
    },
  }
}

/* ====================== 迷你预览(嵌套 ConfigProvider,吃草稿 token) ====================== */

const SkinPreview: React.FC<{ skin: SkinDefinition }> = ({ skin }) => {
  const { t } = useI18n()
  const { palette, brand, appearance } = skin
  const previews = useMemo(
    () => ({
      algorithm: appearance === 'dark' ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
      token: {
        colorPrimary: brand.primary,
        colorBgBase: palette.colorBgBase,
        colorBgLayout: palette.colorBgLayout,
        colorBgContainer: palette.colorBgContainer,
        colorText: palette.colorText,
        colorTextSecondary: palette.colorTextSecondary,
        colorBorder: palette.colorBorder,
        colorLink: palette.colorLink,
      },
    }),
    [palette, brand, appearance]
  )

  return (
    <ConfigProvider theme={previews}>
      {/* 预览区自带画布底色,不透出编辑页自己的背景 */}
      <div style={{ background: palette.colorBgLayout, padding: 12, borderRadius: 8 }}>
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Space wrap>
            <Button type="primary">{t('page.skin.tagPrimary')}</Button>
            <Button>{t('page.skin.tagDefault')}</Button>
            <Button type="dashed">{t('page.skin.tagDashed')}</Button>
            <Button type="text" danger>{t('page.skin.tagDanger')}</Button>
          </Space>
          <Input placeholder={t('page.skin.previewInputPlaceholder')} />
          <Space wrap size={4}>
            {/* 语义 Tag 用内联色:global.css 的 .ant-tag-blue 品牌覆盖会劫持
                color="blue" 预设,预览区要展示语义色本样 */}
            <Tag style={{ color: palette.colorLink, background: 'transparent', borderColor: palette.colorBorder }}>{t('page.skin.tagInfo')}</Tag>
            <Tag color="success">{t('page.skin.tagSuccess')}</Tag>
            <Tag color="warning">{t('page.skin.tagWarning')}</Tag>
            <Tag color="error">{t('page.skin.tagError')}</Tag>
          </Space>
          <div style={{ display: 'flex', gap: 6 }}>
            {[brand.primary, brand.hover, brand.active, palette.colorPrimaryBg, palette.colorPrimaryBorder].map(
              (c) => (
                <div key={c} style={{ flex: 1, height: 22, borderRadius: 4, background: c, border: '0.5px solid rgba(0,0,0,0.08)' }} />
              )
            )}
          </div>
        </Space>
      </div>
    </ConfigProvider>
  )
}

/* ====================== 页面 ====================== */

export const SkinEditorPage: React.FC = () => {
  const { t } = useI18n()
  const { message, modal } = App.useApp()
  const setSkin = useUIStore((s) => s.setSkin)

  const [draft, setDraft] = useState<SkinDefinition | null>(null)
  // 界面基色(中性面的色相/浓度来源)—— 与品牌主色互相独立(derive.ts 双输入模型);
  // 不进 SkinDefinition(它是编辑器的推导输入,不是皮肤数据)
  const [surface, setSurface] = useState<string>('#888888')
  const [advanced, setAdvanced] = useState(false)
  // 试穿状态:进入页面记下原皮肤,退出/卸载恢复 —— 草稿 id 是注册进全局表的,
  // 不恢复的话切走之后"当前皮肤"还指着草稿对象
  const [tryingOn, setTryingOn] = useState(false)
  const prevSkinRef = React.useRef<string | null>(null)

  // 卸载时若还在试穿,恢复原皮肤
  useEffect(
    () => () => {
      if (tryingOn && prevSkinRef.current) setSkin(prevSkinRef.current)
    },
    [tryingOn, setSkin]
  )

  // 试穿中草稿每次变化都热更新(编辑即生效)
  useEffect(() => {
    if (tryingOn && draft) registerSkin({ ...withDerivedCssVars(draft), id: DRAFT_ID })
  }, [tryingOn, draft])

  /* ---------- 动作 ---------- */

  const newFromBrand = (): void => {
    setDraft(
      suggestSkin(
        { id: DRAFT_ID, name: t('page.skin.defaultName') },
        { appearance: 'light', primary: '#7C3AED', surface: '#888888' }
      )
    )
    setSurface('#888888')
    setTryingOn(false)
  }

  const cloneFrom = (id: string): void => {
    const src = getSkin(id)
    const copy = cloneSkin(src)
    copy.id = DRAFT_ID
    copy.name = `${copy.name} · ${t('page.skin.cloneSuffix')}`
    setDraft(copy)
    // 界面基色从被克隆皮肤的画布底色取(是 hex 才能进 picker,否则退中性灰)
    const bg = src.palette.colorBgLayout.trim()
    setSurface(/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(bg) ? bg : '#888888')
    setTryingOn(false)
  }

  const rederive = (): void => {
    if (!draft) return
    const parts = suggestSkinParts({
      appearance: draft.appearance,
      primary: draft.brand.primary,
      surface,
    })
    setDraft((d) => d && { ...d, ...parts })
    message.info(t('page.skin.rederived'))
  }

  const changeAppearance = (appearance: SkinDefinition['appearance']): void => {
    if (!draft) return
    // 明暗一换,全部色值的参考系就变了(浅色参考 museum/深色 black)—— 重算
    const parts = suggestSkinParts({ appearance, primary: draft.brand.primary, surface })
    setDraft((d) => d && { ...d, appearance, ...parts })
  }

  const tryOn = (): void => {
    if (!draft) return
    if (!tryingOn) {
      prevSkinRef.current = useUIStore.getState().skin
      registerSkin({ ...withDerivedCssVars(draft), id: DRAFT_ID })
      setSkin(DRAFT_ID)
      setTryingOn(true)
    } else {
      if (prevSkinRef.current) setSkin(prevSkinRef.current)
      setTryingOn(false)
    }
  }

  const exportTs = async (): Promise<void> => {
    if (!draft) return
    const code = toTsSnippet(withDerivedCssVars({ ...draft, id: draft.id === DRAFT_ID ? 'my-skin' : draft.id }))
    modal.info({
      title: t('page.skin.exportTsTitle'),
      width: 720,
      content: (
        <>
          <Typography.Paragraph type="secondary">{t('page.skin.exportTsHint')}</Typography.Paragraph>
          <Input.TextArea rows={14} value={code} readOnly style={{ fontFamily: 'monospace', fontSize: 12 }} />
        </>
      ),
      okText: t('page.skin.copy'),
      onOk: async () => {
        await navigator.clipboard.writeText(code)
        message.success(t('page.skin.copied'))
      },
    })
  }

  const exportJson = (): void => {
    if (!draft) return
    const out = withDerivedCssVars({ ...draft, id: draft.id === DRAFT_ID ? 'my-skin' : draft.id })
    download(`${out.id}.skin.json`, JSON.stringify(out, null, 2))
  }

  /* ---------- 渲染 ---------- */

  const colorField = (
    label: string,
    value: string,
    onChange: (v: string) => void,
    labelTip?: string
  ): React.ReactNode => (
    <div key={label} style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
      <span style={{ flex: '0 0 190px', fontSize: 12, opacity: 0.75 }}>
        {labelTip ? <Tooltip title={labelTip}>{label}</Tooltip> : label}
      </span>
      {/* 非色值(rgba 串/阴影)也能编辑:ColorPicker 只在能解析成色值时参与 */}
      <Input
        size="small"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        style={{ fontFamily: 'monospace', fontSize: 12 }}
      />
      <ColorPicker
        size="small"
        value={value}
        onChange={(c) => onChange(c.toHexString())}
        disabled={!/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(value.trim())}
      />
    </div>
  )

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      <PageHeader description={t('page.skin.description')} />

      <div style={{ flex: 1, minHeight: 0, display: 'flex', gap: 12, alignItems: 'stretch' }}>
        {/* 左栏:皮肤列表(内置只读) */}
        <Card
          size="small"
          title={t('page.skin.listTitle')}
          style={{ width: 230, flexShrink: 0, overflowY: 'auto', margin: 0, height: 'calc(100% - 8px)', padding: '14px 16px' }}
          styles={{
             header: { padding: '0' },
            body: { padding: '14px 16px', height: 'calc(100% - 44px)', overflow: 'auto' },
          }}
        >
          <Space direction="vertical" size={6} style={{ width: '100%' }}>
            {listSkins().map((s) => (
              <div key={s.id} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                {/* 双色芯片:左半 = 界面底色(皮肤的身份),右半 = 品牌强调色 ——
                    white/black 的品牌色同为橙,单色芯片会让人以为显示错了 */}
                <span
                  style={{
                    display: 'inline-flex',
                    width: 14,
                    height: 14,
                    borderRadius: 4,
                    overflow: 'hidden',
                    flexShrink: 0,
                    border: '0.5px solid rgba(0,0,0,0.15)',
                  }}
                >
                  <span style={{ flex: 1, background: getSkin(s.id).palette.colorBgLayout }} />
                  <span style={{ flex: 1, background: getSkin(s.id).brand.primary }} />
                </span>
                <span style={{ flex: 1 }}>{s.name}</span>
                <Button size="small" type="text" onClick={() => cloneFrom(s.id)}>
                  {t('page.skin.clone')}
                </Button>
              </div>
            ))}
            <Divider style={{ margin: '6px 0' }} />
            <Button icon={<BgColorsOutlined />} onClick={newFromBrand} block>
              {t('page.skin.newSkin')}
            </Button>
          </Space>
        </Card>

        {/* 中栏:编辑表单 */}
        <Card
          size="small"
          title={
            <Space>
              {t('page.skin.editTitle')}
              {draft && <Tag>{draft.appearance === 'dark' ? t('page.skin.appearanceDark') : t('page.skin.appearanceLight')}</Tag>}
            </Space>
          }
          style={{ flex: 1, minWidth: 380, overflowY: 'auto', margin: 0, height: 'calc(100% - 8px)', padding: '14px 16px' }}
          styles={{
             header: { padding: '0' },
            body: { padding: '14px 16px', height: 'calc(100% - 44px)', overflow: 'auto' },
          }}
        >
          {!draft ? (
            <Typography.Text type="secondary">{t('page.skin.pickFirst')}</Typography.Text>
          ) : (
            <>
              <Space style={{ marginBottom: 12 }} wrap>
                <Select
                  value={draft.appearance}
                  style={{ width: 120 }}
                  onChange={changeAppearance}
                  options={[
                    { value: 'light', label: t('page.skin.appearanceLight') },
                    { value: 'dark', label: t('page.skin.appearanceDark') },
                  ]}
                />
                <Input
                  style={{ width: 150 }}
                  value={draft.name}
                  onChange={(e) => setDraft((d) => (d ? { ...d, name: e.target.value } : d))}
                  placeholder={t('page.skin.skinName')}
                />
                <Popconfirm title={t('page.skin.rederiveConfirm')} onConfirm={rederive}>
                  <Tooltip title={t('page.skin.rederiveTip')}>
                    <Button>{t('page.skin.rederive')}</Button>
                  </Tooltip>
                </Popconfirm>
              </Space>

              {/* 基础模式:界面色调 + 主色三态(两个独立输入,见各 tip) */}
              <Card
                type="inner"
                size="small"
                title={<Tooltip title={t('page.skin.baseModeTip')}>{t('page.skin.baseMode')}</Tooltip>}
                style={{ marginBottom: 12 }}
              >
                {colorField(t('page.skin.surface'), surface, setSurface, t('page.skin.surfaceTip'))}
                {(['primary', 'hover', 'active'] as const).map((k) =>
                  colorField(t(`page.skin.${k}`), draft.brand[k], (v) =>
                    setDraft((d) => d && { ...d, brand: { ...d.brand, [k]: v } })
                  )
                )}
              </Card>

              {/* 高级模式:全量 token 逐键覆盖 */}
              <Collapse
                size="small"
                activeKey={advanced ? ['palette', 'comp'] : []}
                onChange={(keys) => setAdvanced(keys.includes('palette') || keys.includes('comp'))}
                items={[
                  {
                    key: 'palette',
                    label: t('page.skin.advancedPalette'),
                    children: PALETTE_GROUPS.map((g) => (
                      <Card key={g.key} type="inner" size="small" title={t(g.label)} style={{ marginBottom: 8 }}>
                        {g.keys.map((k) =>
                          colorField(k, draft.palette[k], (v) =>
                            setDraft((d) => d && { ...d, palette: { ...d.palette, [k]: v } })
                          )
                        )}
                      </Card>
                    )),
                  },
                  {
                    key: 'comp',
                    label: t('page.skin.advancedComp'),
                    children: COMP_GROUPS.map((g) => (
                      <Card key={g.key} type="inner" size="small" title={t(g.label)} style={{ marginBottom: 8 }}>
                        {g.keys.map((k) =>
                          colorField(k, draft.comp[k], (v) =>
                            setDraft((d) => d && { ...d, comp: { ...d.comp, [k]: v } })
                          )
                        )}
                      </Card>
                    )),
                  },
                ]}
              />
            </>
          )}
        </Card>

        {/* 右栏:实时预览 + 试穿 + 导出 */}
        <Card
          size="small"
          title={t('page.skin.previewTitle')}
          style={{ width: 320, flexShrink: 0, overflowY: 'auto', margin: 0, height: 'calc(100% - 8px)', padding: '14px 16px' }}
          styles={{
             header: { padding: '0' },
            body: { padding: '14px 16px', height: 'calc(100% - 44px)', overflow: 'auto' },
          }}
          extra={
            draft && (
              <Space size={4}>
                <Button size="small" icon={<DownloadOutlined />} onClick={exportJson}>
                  JSON
                </Button>
                <Button size="small" icon={<CopyOutlined />} onClick={exportTs}>
                  TS
                </Button>
              </Space>
            )
          }
        >
          {draft ? (
            <>
              <SkinPreview skin={draft} />
              <Divider style={{ margin: '12px 0' }} />
              <Space direction="vertical" style={{ width: '100%' }} size={6}>
                <Button
                  block
                  type={tryingOn ? 'default' : 'primary'}
                  icon={<EyeOutlined />}
                  onClick={tryOn}
                >
                  {tryingOn ? t('page.skin.exitTryOn') : t('page.skin.tryOn')}
                </Button>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {t('page.skin.tryOnHint')}
                </Typography.Text>
              </Space>
            </>
          ) : (
            <Typography.Text type="secondary">{t('page.skin.pickFirst')}</Typography.Text>
          )}
        </Card>
      </div>
    </div>
  )
}

export default SkinEditorPage
