/**
 * LangRoot —— Antd ConfigProvider 包裹层(主题 / 语言 / 浏览器标题)
 *
 * 2026-10-29 M109 从 `main.tsx` 搬到这里(内容**逐行原样搬运**,未做任何改动)。
 *
 * ⚠️ **为什么搬**(不是"整理代码",是修 bug 的前置条件):
 *   错误边界的**装配顺序**决定"出错了显示什么"。原先 LangRoot 和边界装配
 *   都写在 main.tsx 里 ⇒ 想测"fallback 有没有继承主题"就必须 import main.tsx,
 *   而 main.tsx 在模块顶层就 `createRoot().render(...)` —— 一 import 就真渲染,
 *   测不了。
 *   把 LangRoot 抽成独立组件之后,边界装配(`AppErrorBoundary.tsx`)才能被单测覆盖。
 *
 * 它管三件事:
 *   1. Antd locale(响应 useUIStore.lang)
 *   2. Antd theme algorithm + 品牌 token(响应 useUIStore.theme)
 *   3. 浏览器标题 + <html lang> + <html data-theme>
 *
 * 2026-11-05 皮肤抽取(docs/theming.md P0):palette / comp / 品牌三态与
 * --app-* 品牌变量全部搬进 src/theme/skins/,本组件改为**消费**皮肤对象 ——
 * 这里不再出现任何具体色值。
 *
 * ⚠️ **新增品牌色值必须进 web/src/theme/skins/,不得直接写进本文件的 token**
 *   (AGENTS.md 硬约束 6;skin 相关键见 theme/types.ts 的 PaletteTokens / CompTokens)。
 */
import React from 'react'
import { ConfigProvider, App as AntdApp, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import enUS from 'antd/locale/en_US'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'
import 'dayjs/locale/en'

import { useUIStore } from '@/stores/ui'
import { usePublicConfigStore } from '@/stores/publicConfig'
import { useI18n, ensureLocale } from '@/locales'
import { getSkin } from '@/theme/registry'
import { applySkinCssVars } from '@/theme/apply'

/** LangRoot 包裹层 —— 响应 useUIStore.lang 切换 Antd locale + dayjs locale,响应 theme 切换 algorithm + data-theme */
export const LangRoot: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const lang = useUIStore((s) => s.lang)
  const curTheme = useUIStore((s) => s.theme)
  // 平台名(后端 branding 配置下发,开源通用化)。空串 = 没配 → 落回语言包。
  const appTitle = usePublicConfigStore((s) => s.appTitle)
  const { t } = useI18n()
  React.useEffect(() => {
    dayjs.locale(lang === 'en-US' ? 'en' : 'zh-cn')
  }, [lang])
  // 语言包懒加载(§2.6 #7,M173):非默认语言在这里触发异步加载,
  // 包到达后 registry 版本信号让 useI18n 组件自动重渲染成英文。
  //   ⚠️ 挂在 LangRoot 不挂 setLang:持久化恢复的初始 lang(zustand rehydrate)
  //     不经过 setLang,但一定经过这里挂载后的 effect —— 单一触发点。
  //   ⚠️ 失败静默:网络抖动拉不到语言包时界面回落中文(可用),不打断使用;
  //     registry 侧已清 pending,下次切换语言会重试。
  React.useEffect(() => {
    void ensureLocale(lang).catch(() => undefined)
  }, [lang])
  // 浏览器标签页标题 + <html lang> 跟着界面语言走
  //   ⚠️ index.html 里那个 <title> 是**写死的中文**,只对"水合前那一瞬"成立;
  //     不在运行时改的话,英文用户的标签页标题永远是中文。
  //   ⚠️ <html lang> 是给屏幕阅读器选语音用的 —— 界面是英文、lang 却是 zh-CN,
  //     中文语音会念错。
  //   ⚠️ 依赖里必须有 t:t 的引用随语言切换而变(useMemo([dict])),
  //     漏掉它切语言后标题会停在旧语言上。
  //   ⚠️ appTitle 在依赖里:后端 branding 是**异步**到货的(main.tsx 启动时拉,
  //     不 await),不在依赖里的话配置到了也不会改标题 —— 标题会停在语言包的值上。
  React.useEffect(() => {
    document.title = appTitle || t('common.appTitle')
    document.documentElement.lang = lang
  }, [lang, t, appTitle])
  React.useEffect(() => {
    // 把 theme 同步到 <html data-theme="...">,CSS variables 跟着切
    document.documentElement.dataset.theme = curTheme
  }, [curTheme])
  // 主题 → Antd algorithm(§2.8 #19)
  const isDark = curTheme === 'dark' || curTheme === 'realDark'
  const algorithm = isDark ? theme.darkAlgorithm : theme.defaultAlgorithm

  // 皮肤(品牌色相轴,docs/theming.md):registry 取用,未知 id 降级默认橙。
  // --app-* 品牌变量随皮肤+明暗注入 <html>(CSS 变量单源,见 theme/apply.ts)
  const skin = getSkin(useUIStore((s) => s.skin))
  React.useEffect(() => {
    applySkinCssVars(skin, isDark)
  }, [skin, isDark])

  // 深浅两套语义色板 / 组件级差异 —— 值在皮肤对象里(types.ts 键名一一对应)
  const palette = isDark ? skin.palette.dark : skin.palette.light
  const comp = isDark ? skin.comp.dark : skin.comp.light

  return (
    <ConfigProvider
      locale={lang === 'en-US' ? enUS : zhCN}
      theme={{
        algorithm,
        token: {
          // === 品牌色三态(来自皮肤对象,docs/theming.md)===
          colorPrimary: skin.brand.primary,
          colorPrimaryHover: skin.brand.hover,
          colorPrimaryActive: skin.brand.active,
          colorPrimaryBg: palette.colorPrimaryBg,
          colorPrimaryBgHover: palette.colorPrimaryBgHover,
          colorPrimaryBorder: palette.colorPrimaryBorder,
          colorPrimaryBorderHover: palette.colorPrimaryBorderHover,
          colorPrimaryText: palette.primaryText,
          colorPrimaryTextHover: palette.primaryTextHover,
          colorPrimaryTextActive: palette.primaryTextActive,

          // === 链接色(2026-10-31 补)===
          // ⚠️ **antd 的 `colorLink` 不设 = 蓝色**,不是"跟随主色":
          //   antd/lib/theme/themes/shared/genColorMapToken.js:28
          //   `const colorLink = seed.colorLink || seed.colorInfo`
          //   而 `seed.colorInfo` 的默认值是 **#1677ff 蓝**。
          // ⇒ 本仓设计系统里**一个蓝都没有**(variables.css 只有橙/灰/红/绿),
          //   于是"表格操作列 / 空态重试 / 搜索折叠 / 批量条清除"这些
          //   `type="link"` 按钮会在整站冒出一片**外来的蓝** —— 用户 2026-10-31
          //   报的「文字都是蓝色的」就是它。
          // ⚠️ **这是主题少了一个 token,不是某个页面的问题** —— 所以改在这里:
          //   一次改完全站(20 个业务页的 RowActions 操作列 + 6 处独立 link 按钮)。
          //   改页面只能治标(而且下次谁再写一个 link 按钮又蓝回去了)。
          colorLink: palette.colorLink,
          colorLinkHover: palette.colorLinkHover,
          colorLinkActive: palette.colorLinkActive,

          // === 画布与表面(深浅两套,见上方 palette)===
          colorBgBase: palette.colorBgBase,
          colorBgLayout: palette.colorBgLayout,
          colorBgContainer: palette.colorBgContainer,
          colorBgElevated: palette.colorBgElevated,
          colorBgSpotlight: palette.colorBgSpotlight,
          colorBgMask: palette.colorBgMask,

          // === 文字 (slate 调, 不纯黑/不死白) ===
          colorText: palette.colorText,
          colorTextSecondary: palette.colorTextSecondary,
          colorTextTertiary: palette.colorTextTertiary,
          colorTextQuaternary: palette.colorTextQuaternary,
          colorTextDescription: palette.colorTextDescription,
          colorTextPlaceholder: palette.colorTextPlaceholder,

          // === 边框 ===
          colorBorder: palette.colorBorder,
          colorBorderSecondary: palette.colorBorderSecondary,
          colorSplit: palette.colorSplit,

          // === 圆角 (3 档) ===
          // 参考原型: --r-xl 22 / --r-lg 16 / --r-md 12,按钮 10、小控件 8
          borderRadius: 10,
          borderRadiusLG: 16,
          borderRadiusSM: 8,
          borderRadiusXS: 4,
          lineWidth: 0.5,

          // === 字体 (参考原型的 --sans / .mono) ===
          // 在 antd 默认栈上补 Noto Sans CJK SC / Source Han Sans SC(Linux 服务器端
          // 常见中文字体,Windows 上没有);保留 PingFang SC / Hiragino(mac 优先)。
          // 同一套栈也写在 styles/variables.css 的 --app-font-sans,两边要一起改。
          fontFamily:
            "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', " +
            "'Noto Sans CJK SC', 'Source Han Sans SC', 'Microsoft YaHei', 'Helvetica Neue', Helvetica, Arial, sans-serif",
          fontFamilyCode: "'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace",

          // === 字号与控件 (全站下调一档) ===
          // ⚠️ fontSizeSM / fontSizeLG / lineHeight 在 antd 里是**派生**的
          //   (genFontMapToken: fontSizeSM = base*e^-0.2 取偶整数):
          //   base=12 会派生出 fontSizeSM=10 / fontSizeLG=14 —— 小字直接塌一档。
          //   这三项必须显式写死(显式值走 theme.override 通道,优先级高于派生值)。
          fontSize: 12,
          fontSizeSM: 12,
          fontSizeLG: 16,
          lineHeight: 1.6,
          controlHeight: 36,
          controlHeightLG: 40,
          controlHeightSM: 28,

          // === 动效 (与参考项目一致: 220ms 软出) ===
          motionDurationFast: '0.12s',
          motionDurationMid: '0.22s',
          motionDurationSlow: '0.36s',
          motionEaseOut: 'cubic-bezier(0.16, 1, 0.3, 1)',
          motionEaseInOut: 'cubic-bezier(0.4, 0, 0.2, 1)',

          // === 阴影 (引用 variables.css 5 档) ===
          boxShadow: 'var(--app-shadow-l4)',
          boxShadowSecondary: 'var(--app-shadow-l3)',
          boxShadowTertiary: 'var(--app-shadow-l1)',

          wireframe: false,
        },
        components: {
          // === Layout: 表面随主题, Header 毛玻璃 ===
          Layout: {
            headerBg: comp.layoutHeaderBg,
            headerHeight: 56,
            headerPadding: '0 20px',
            siderBg: comp.siderBg,
            bodyBg: comp.bodyBg,
            footerBg: 'transparent',
            footerPadding: '16px 24px',
            triggerBg: 'transparent',
            triggerColor: comp.triggerColor,
          },
          // === Menu: 表面随主题 + 浅橙/半透明橙选中 ===
          Menu: {
            itemBg: comp.menuItemBg,
            itemSelectedBg: comp.menuSelectedBg,
            itemSelectedColor: comp.menuSelectedColor,
            itemHoverBg: comp.menuHoverBg,
            itemHoverColor: comp.menuHoverColor,
            itemColor: comp.menuColor,
            itemMarginInline: 6,
            itemBorderRadius: 10,
            itemHeight: 40,
            iconSize: 16,
            // 参考原型的导航项 13px(antd 的 Menu.fontSize 是**组件级 AliasToken**,
            // components.Menu.fontSize 真的生效 —— 跟 Table.fontSize 不一样)
            fontSize: 13,
            subMenuItemBorderRadius: 8,
            darkItemBg: 'transparent',
            darkItemColor: 'rgba(255, 255, 255, 0.7)',
            darkItemSelectedBg: comp.menuDarkItemSelectedBg,
            darkItemSelectedColor: comp.menuDarkItemSelectedColor,
            darkItemHoverBg: 'rgba(255, 255, 255, 0.04)',
            darkSubMenuItemBg: '#0a0a0d',
            darkPopupBg: '#1c1c22',
          },
          // === Table: 浅灰表头 + 无竖线 + 12px 正文 ===
          Table: {
            headerBg: comp.tableHeaderBg,
            // ⚠️ 表头的**竖**分隔线(antd 用 th::before 画):参考原型的表头只有
            //    一条下边框, 没有竖线 → 置 transparent
            headerSplitColor: 'transparent',
            headerColor: comp.tableHeaderColor,
            borderColor: comp.tableBorderColor,
            headerBorderRadius: 12,
            // ⚠️ cellPaddingBlock/Inline **只在 size="large" 生效**;
            //    ManagementTable 用的是 size="middle" → 真正生效的是 MD 变体。
            //    之前只写非 MD 的那两个 = 横向 14px 的内边距从来没生效过(实际 16px)。
            //
            // 横向内边距取 12 而不是参考原型的 14:参考原型的表格列宽是**内容自适应**
            // (没有固定 width),我们这边每列都写死了 width,横向留白直接吃掉列宽。
            // 按"每列能放下的字符数不变"校准:旧值 8px 边距 + 13px 字号 = 104px 可用宽,
            // 字号降到 12px 后要保持同样容量,边距 = (120 - 104×12/13) / 2 = 12px。
            // (14px 会让 120px 的"账号"列放不下 "e2e_house_admin" —— 实测 95px > 92px)
            cellPaddingBlock: 12,
            cellPaddingInline: 12,
            cellPaddingBlockMD: 12,
            cellPaddingInlineMD: 12,
            cellPaddingBlockSM: 8,
            cellPaddingInlineSM: 10,
            // ⚠️ 表格正文字号读的是 cellFontSize*。
            //    antd 的 prepareComponentToken 会先把「全局 token + components.Table
            //    的自定义」合并,再用合并后的 fontSize 去填 cellFontSize / cellFontSizeMD /
            //    cellFontSizeSM —— 所以老配置里的 fontSize:13 是**碰巧**生效的(它先喂给了
            //    prepareComponentToken),而不是 antd 文档里的 Table token。
            //    这里直接写死三个 cellFontSize*,不再依赖那条隐式链路。
            cellFontSize: 12,
            cellFontSizeMD: 12,
            cellFontSizeSM: 11,
            rowHoverBg: comp.rowHoverBg,
            rowSelectedBg: comp.rowSelectedBg,
            rowSelectedHoverBg: comp.rowSelectedHoverBg,
          },
          // === Button: 圆角 10, 主按钮 L1 阴影 + edge 光 ===
          Button: {
            borderRadius: 10,
            borderRadiusLG: 12,
            borderRadiusSM: 8,
            controlHeight: 36,
            controlHeightLG: 40,
            controlHeightSM: 28,
            // ⚠️ antd 的 small 按钮字号回落到全局 fontSize(=13),比它所在的表格正文(12px)
            //    还大一号 —— 显式压到 SM 档(表格里的"详情/编辑/删除"就是 small 按钮)。
            contentFontSizeSM: 12,
            fontWeight: 500,
            primaryShadow: comp.primaryShadow,
            defaultShadow: comp.defaultShadow,
          },
          // === Input: 圆角 10, focus 橙色光晕 ===
          Input: {
            borderRadius: 10,
            controlHeight: 36,
            paddingBlock: 8,
            paddingInline: 12,
            activeShadow: comp.inputActiveShadow,
          },
          // === Card: 表面随主题, 圆角 16, L2 阴影 ===
          Card: {
            borderRadiusLG: 16,
            paddingLG: 18,
            headerBg: 'transparent',
            headerHeight: 52,
            headerHeightSM: 44,
            colorBgContainer: palette.colorBgContainer,
          },
          // === Modal: L4 覆盖, 圆角 16 ===
          Modal: {
            borderRadiusLG: 16,
            paddingContentHorizontal: 24,
            contentBg: palette.colorBgContainer,
            headerBg: palette.colorBgContainer,
          },
          // === Drawer: L4 覆盖, padding 20 ===
          Drawer: {
            paddingLG: 20,
          },
          // === Tag: 圆角 8, 字号 10(参考原型的徽标) ===
          Tag: {
            borderRadiusSM: 8,
            // Tag 的字号读的是 tagFontSize = fontSizeSM(组件作用域内) → 10px
            fontSizeSM: 10,
          },
          // === Tooltip: 深浅互换的"反色"底, L3 浮 ===
          Tooltip: {
            borderRadius: 8,
            colorBgSpotlight: palette.colorBgSpotlight,
            colorTextLightSolid: '#ffffff',
          },
          // === Dropdown: L3 浮, 圆角 12 ===
          Dropdown: {
            borderRadiusLG: 12,
            colorBgElevated: palette.colorBgElevated,
          },
          // === Popover: L3 浮, 圆角 12 ===
          Popover: {
            borderRadiusLG: 12,
          },
          // === Form: label 字号 12, item 间距 18 ===
          Form: {
            itemMarginBottom: 18,
            labelFontSize: 12,
          },
          // === Pagination: 浅橙激活, 字号 11(参考原型的分页) ===
          Pagination: {
            itemActiveBg: palette.colorPrimaryBg,
            itemBg: comp.paginationItemBg,
            // 分页数字的字号来自组件作用域的 fontSize(resetComponent),
            // 翻页箭头/省略号读 fontSizeSM —— 两个都要压到 11px
            fontSize: 11,
            fontSizeSM: 11,
            // 参考原型的 .pager button 圆角是 7px(小控件档), 不是主控件的 10px
            borderRadius: 7,
          },
          // === Notification: L3, 圆角 12 ===
          Notification: {
            borderRadiusLG: 12,
          },
          // === Message: L3, 圆角 10, 表面随主题 ===
          Message: {
            borderRadiusLG: 10,
            contentBg: palette.colorBgElevated,
          },
          // === Tabs: 透明 nav, 激活浅橙 ===
          Tabs: {
            cardBg: palette.colorBgContainer,
            cardHeight: 36,
            horizontalMargin: '0 0 12px 0',
            itemActiveColor: comp.tabsActiveColor,
            itemSelectedColor: comp.tabsActiveColor,
            itemHoverColor: comp.tabsActiveColor,
            inkBarColor: comp.tabsActiveColor,
            titleFontSize: 12,
          },
          // === Select: 同 Input ===
          Select: {
            borderRadius: 10,
            controlHeight: 36,
            optionSelectedBg: palette.colorPrimaryBg,
            optionSelectedColor: comp.selectOptionSelectedColor,
          },
          // === DatePicker: 同 Input ===
          DatePicker: {
            activeShadow: comp.inputActiveShadow,
            cellHoverBg: comp.dateCellHoverBg,
            cellActiveWithRangeBg: comp.dateCellRangeBg,
          },
          // === Breadcrumb: 文字次级色, 字号 12(参考原型的面包屑) ===
          Breadcrumb: {
            itemColor: comp.breadcrumbItemColor,
            lastItemColor: comp.breadcrumbLastColor,
            separatorColor: comp.breadcrumbSeparatorColor,
            fontSize: 14,
          },
        },
      }}
    >
      <AntdApp>{children}</AntdApp>
    </ConfigProvider>
  )
}
