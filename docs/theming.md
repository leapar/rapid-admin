# UI 皮肤定制规划(theme skins)

> 目标:让接框架的人**换肤只写一个文件** —— 文博棕金、科技蓝、政务红,
> 各是一份声明式的皮肤定义;再配合后端 `branding:` 改 yaml 就能切,
> 延续本仓库"平台名是配置项不是常量"的既定哲学。

状态:**P0–P2 已落地**(2026-11-05,五套皮肤 + 沉浸式 + 全站去外来色);
P3 皮肤编辑器(§7)未做。本文档 = 设计定稿(§3–§4 即实现形态)+ 上手指南(§5)。

> **⚠️ 2026-11-05 扁平化更新(以本横幅为准,正文未及之处按此理解)**
> P0/P1/P2 落地当天,架构按用户拍板从"双轴"改为**扁平单轴**:
>
> 1. **皮肤 = 整体外观**:白色/黑色/科技蓝/政务红/文博棕金五套同级,
>    每套**一套色板**(`palette` / `comp` / `cssVars` 不再分 light/dark),
>    新增 `appearance: 'light' | 'dark'` 字段决定 antd 算法与 `<html data-theme>`。
> 2. **明暗切换器退役**:`ThemeSwitcher` 删除,`useUIStore.theme` 轴移除
>    (想暗色就选黑色皮肤;旧 localStorage 的 theme 值被忽略,
>    index.html 防闪脚本兼容读取一次)。
> 3. **`orange` 退役**:原默认皮肤更名拆分为 `white`(浅色面)/ `black`(深色面),
>    值零变化;旧 id `orange` 在 registry 里静默别名到 `white`。
> 4. **字段全集守卫**:`skins.complete.test.ts` 守住"所有皮肤与 white/black
>    基准键集完全一致",漏任何色调键即红。
> 5. **取值重调 + 显式全量定义**:五套皮肤色值按 antd "seed → map token"
>    算法理念整体重调(中性面同一色温、品牌浅底同系衍生,白色中性面为
>    纯灰、文博暖纸底);皮肤文件 = **逐参数显式色值**,没有推导运行时层
>    —— 拷 `custom.example.ts` 逐项改即可。
> 6. §7.3 皮肤编辑器相应简化:编辑的就是**单套色板**,不再有"明暗联动生成"。

---

## 1. 现状盘点

当前的主题体系是**双轴**:明暗(theme)已做好,品牌色相(skin)不存在。

| 层 | 位置 | 现状 |
|---|---|---|
| Antd 组件 token | `web/src/components/common/LangRoot.tsx` | `palette`(深浅两套语义色板)+ `comp`(深浅两套组件级:header/sider/menu/table…)+ `token.colorPrimary` 三段,**品牌橙 #f60 系列 27 处硬编码** |
| CSS 变量 | `web/src/styles/variables.css` | `--app-*`(5 档阴影/画布/选中/语义浅底),`data-theme='dark'` 一段覆盖;**5 处橙系值** |
| 明暗切换 | `ThemeSwitcher` + `useUIStore.theme` | 已完成:`algorithm` + `data-theme` 联动,localStorage 持久化 |
| 品牌名 | 后端 `branding.app_name/app_title` → `publicConfig` store | 已完成,改 yaml 换名 |
| **品牌色** | —— | **写死在前端两处,换肤必须改框架代码** ❌ |

痛点:接框架的人想要"政务红",得同时在 LangRoot.tsx 的三个对象和 variables.css 里
找齐 30+ 处橙系衍生值(含一堆 `rgba(255,102,0,x)` 派生),漏一处就出现"橙蓝混色"。

## 2. 目标形态

1. **一个皮肤 = 一个文件**:`src/theme/skins/<id>.ts`,声明式对象,不懂 antd token 体系也能照着模板改。
2. **换肤不改框架代码**:内置皮肤(白/黑/文博/科技/政务)+ 后端 `branding.skin: gov` 一行切换;
   自定义皮肤拷 `src/theme/skins/custom.example.ts` 模板,升级框架不冲突。
3. **单源,无双份维护**:CSS 变量从皮肤对象**运行时注入**,不搞"TS 一份、CSS 一份"漂移。
4. **明暗轴保持独立**:每套皮肤自带 light/dark 两套色板,`ThemeSwitcher` 行为不变。
   **〔已废弃 —— 2026-11-05 扁平化:皮肤即整体外观,见顶部横幅〕**

## 3. 架构设计

### 3.1 皮肤定义类型(核心)

```ts
// web/src/theme/types.ts —— **扁平单轴**(2026-11-05 落地形态)
export type SkinAppearance = 'light' | 'dark'

export interface SkinDefinition {
  id: string                       // 'white' | 'black' | 'museum' | 'tech' | 'gov'
  name: string                     // 展示名,例:"政务红"
  appearance: SkinAppearance       // 决定 antd 算法与 <html data-theme>
  /** 品牌主色三态:hover 亮一档 / active 暗一档(L 轴) */
  brand: { primary: string; hover: string; active: string }
  /** 语义色板 25 键(antd 全局 token 的品牌相关子集) */
  palette: PaletteTokens
  /** 组件级 token 33 键(header/sider/menu/table/breadcrumb…,含沉浸式 header 文字 4 键) */
  comp: CompTokens
  /** --app-* CSS 变量(品牌系 7 键必给,applySkinCssVars 运行时注入) */
  cssVars: Record<string, string>
}
```

键集唯一定义处是 `types.ts`;`skins.complete.test.ts` 守"所有皮肤与 white/black
基准键集完全一致"(palette 25 / comp 33 / brand 3,漏一个色调即红)。

### 3.2 目录与数据流

```
web/src/theme/
├── types.ts            # SkinDefinition / PaletteTokens / CompTokens 类型
├── skins/
│   ├── white.ts        # 白色(默认,纯灰中性面 + 橙强调)
│   ├── black.ts        # 黑色(深色面)
│   ├── museum.ts       # 文博棕金
│   ├── tech.ts         # 科技蓝
│   ├── gov.ts          # 政务红
│   └── custom.example.ts  # 自定义模板(拷走改名即用)
├── registry.ts         # SKINS 表 + getSkin()/registerSkin();useSkin(id) 取用
└── apply.ts            # applySkinCssVars(skin):把品牌系 + 中性面/文字梯度
                        # 映射成 --app-* 注入 <html style>(见下"数据流")
```

数据流(LangRoot 消费):

```
useUIStore.skin ──→ LangRoot
                    ├─ applySkinCssVars(skin) → <html style="--app-*">
                    │    (品牌 7 键 + bodyBg/siderBg/中性面/文字梯度映射,单源)
                    └─ ConfigProvider token/components ← skin.palette + skin.comp
                         (appearance 决定 algorithm 与 data-theme)
```

> **`variables.css` 的定位**(2026-11-05 起):静态明暗两套 `--app-*` 只是
> **首帧(注入前)兜底**;皮肤一旦应用,`--app-bg-layout` / `--app-surface-*` /
> `--app-text*` / `--app-primary*` 全部由 `applySkinCssVars` 按皮肤注入覆盖,
> variables.css 不再承载第二份真值。

### 3.3 关键取舍

| 决策 | 理由 |
|---|---|
| 运行时注入 CSS 变量,而非 `:root[data-skin=x]` 静态 CSS | 双份(TS+CSS)必漂移,违反反漂移原则;注入发生在 LangRoot 挂载,无闪烁(默认皮肤值仍留在 variables.css 作 fallback,注入值覆盖之) |
| 皮肤存 ui store + localStorage,同 theme | 同一套持久化机制,零新增基建 |
| 后端 `branding.skin` 只能选**内置 id**,不传颜色 | 配 yaml 传 30 个 token 不可维护;要深度定制就走 custom.ts 重新构建 —— 声明边界,别两头都做半吊子 |
| 不做运行时上传皮肤文件 | XSS 面 + 复杂度;真有需求走 P3 再议 |
| `comp` 不抽公共默认 + per-skin 覆盖 | 先全量声明(照模板改),落地稳定后再考虑抽 `baseSkin` 合并;过早抽象会把"改一处懂全局"变成"改一处查三层" |

### 4. 内置皮肤规格(2026-11-05 落地值)

五套皮肤的取值策略 = **沉浸式**:顶栏用品牌色实底(gov/museum/tech)配浅色
文字(`comp.headerTextColor` 四键,Header.tsx 内联 remap 到 `--app-text*`),
侧栏/画布/表头/菜单选中带同色相的品牌晕;white/black 保持毛玻璃顶栏。
文字梯度:gov 保持纯灰(掺红相会发棕,实测踩过),museum 用暖棕。
**三段深度**:顶栏(最深)→ 侧栏(中,比画布深约 2 档)→ 画布/卡片(亮,
承载内容)。

### 4.1 `museum` 文博棕金(博物馆/文博领域)

| Token | 值 | 说明 |
|---|---|---|
| brand | `#8C5A2B` / `#af7136` / `#694320` | 赭石棕三态 |
| comp.layoutHeaderBg | `#5d4128` | 深棕实底顶栏 + 米色文字(#f4eae1) |
| palette.colorBgLayout | `#ecdfd2` | 暖纸画布 |
| palette.colorBgContainer | `#fdf7f1` | 微暖白卡片 |
| comp.siderBg | `#e9dcc9` | 比画布深一档的暖棕侧栏 |
| comp.menuSelectedBg / menuHoverBg | `#e9d6c3` / `#f0e5d8` | 棕金浅底收口 |
| palette.colorPrimaryBg | `#f7efe4` | 品牌浅底(29° 同色相) |

### 4.2 `tech` 科技蓝(数据/科技产品)

| Token | 值 | 说明 |
|---|---|---|
| brand | `#2F6BFF` / `#5d8cff` / `#014aff` | 电光蓝(避开 antd 默认 #1677ff) |
| comp.layoutHeaderBg | `#192f76` | 深蓝实底顶栏 + 白字 |
| palette.colorBgLayout | `#e6e9f5` | 冷蓝画布 |
| palette.colorBgContainer | `#fafbfe` | 卡片 |
| comp.siderBg | `#dfe5f4` | 冷蓝侧栏 |
| comp.menuSelectedBg / menuHoverBg | `#d0deff` / `#e4ebff` | |
| palette.colorPrimaryBg | `#e5edff` | |

### 4.3 `gov` 政务红(政务/国企)

| Token | 值 | 说明 |
|---|---|---|
| brand | `#C7000B` / `#f5000e` / `#990008` | 国旗红三态 |
| comp.layoutHeaderBg | `#C7000B` | 国旗红实底顶栏 + 白字 |
| palette.colorBgLayout | `#f3e2e2` | 红晕画布 |
| palette.colorBgContainer | `#fef7f7` | 近白卡片 |
| comp.siderBg | `#efdbdb` | 红晕侧栏 |
| comp.menuSelectedBg / menuHoverBg | `#f8caca` / `#f7dedf` | |
| palette.colorPrimaryBg | `#ffe5e7` | |
| palette.colorText | `#292929`(纯灰) | ⚠️ 掺红相的文字会发棕,2026-11-05 实测踩过 |

语义色(success 绿 / warning 金 / error 红 / diff 增删底)全站统一,**不随
皮肤走** —— 红 skin 与 error 语义的分工靠语义色本身是 antd 默认 `#ff4d4f` 系。
品牌浅底收口(信息 Tag blue/processing/geekblue 与 info Alert)走
`styles/global.css` 的 CSS 覆盖通道(`--app-primary*` 变量 + `!important`,
antd cssinjs 运行时注入 <head>,同优先级平局时后注入者赢,不加会被压住)。

## 5. 上手指南:定义你自己的皮肤(最终形态)

> 本节即落地后对外发布的"怎么换肤"文档,三步,约 20 分钟。

### 第 1 步:拷模板

```bash
cd web/src/theme/skins
cp custom.example.ts my-brand.ts
```

### 第 2 步:填色板

打开 `my-brand.ts`,按注释逐键替换。**必改头部几项**(其余保持模板默认就已经协调):

```ts
id: 'my-brand',
name: '我的品牌',
appearance: 'light',   // 深色外观写 'dark' 并整表换暗色值(参考 black.ts)
brand: { primary: '#7C3AED', hover: '#9864f1', active: '#6115e4' },  // ← 主色三态
```

皮肤是**显式全量定义**(palette 25 键 / comp 33 键 / cssVars 品牌系 7 键,
逐参数一个色值)—— 想要沉浸实底顶栏就把 `comp.layoutHeaderBg` 换实色、
`headerTextColor` 四键换浅色文字(参考 gov.ts);其余色值**对着内置皮肤同
键位抄档位**最稳,不要手调新色相。

选色心法(照做就不会丑):

1. 主色只定一个 `brand.primary`,hover/active 分别亮/暗 8–12%;
2. 一切"浅底"(Bg 系)用主色 **≥10% 透明度或 95% 明度**的衍生色,不要手调新色相;
3. 深色皮肤(appearance='dark')的"品牌文字/链接"比主色提亮一档(参考 black.ts);
4. 阴影别动结构,只换色相成主色淡晕;改完跑 `pnpm vitest run src/theme`,
   `skins.complete.test.ts` 会核对字段全集与同调性。

### 第 3 步:注册 + 启用

```ts
// src/theme/registry.ts —— SKINS 表挂一行
import myBrand from './skins/my-brand'
const SKINS = Object.fromEntries([white, black, museum, tech, gov, myBrand].map((s) => [s.id, s]))
```

启用二选一:

- **开发/自托管前端**:顶栏皮肤切换器选它,或 localStorage 首选;随构建生效。
- **后端统一下发**(推荐,SaaS/多部署):`server/configs/*.yaml`:

```yaml
branding:
  app_name: "文博数字资产管理平台"
  skin: museum        # ← 只认内置 id;不在清单里 = 落回默认 white 并打 warn 日志
```

验收:`pnpm dev` 打开登录页与首页,**登录页背景/按钮、侧栏选中态、表格 hover、
暗色模式**五处过一遍,无残留旧色即完成。

## 6. 风险与守卫

| 风险 | 对策 |
|---|---|
| 搬家时漏改导致行为回归 | 首个 PR 只做 `orange.ts` 原样搬家,验收 = 视觉零变化;新皮肤后续 PR 各自独立 |
| 红 skin 与 error 语义色混淆 | gov 皮肤把 `--app-error-*` 明确为更橙的 `#FF4D4F` 系并写进注释;§4.3 已标 |
| 派生梯度(antd 自动生成的 colorPrimary 系)与新主色不协调 | 只显式覆盖 §4 列出的键,其余信任 darkAlgorithm/defaultAlgorithm 推导 —— 与现实现同一策略 |
| branding.skin 配了不存在的 id | publicConfig 校验 + console.warn + 落回 white;不算错误(容错降级,符合"显式降级"前提) |
| 未来 login 页 logo/插画也算皮肤的一部分 | 列入 P2:`branding.logo_url` 走 publicConfig,与本规划同机制,不另起炉灶 |

## 7. 皮肤管理界面(P3 规划)

前面 §2 的形态是"写一个 ts 文件换肤" —— 面向开发者。但还有一个真实受众:
**不想碰代码的运营/实施人员**,以及"想先可视化调完色再落文件"的开发者。
给这两类人做一个**皮肤管理页面**(管理端内,非公开页),能力 = 列表 / 新建 / 编辑 / 预览 / 启用。

### 7.1 为什么架构天然支持

P0 的两个设计决策在这里兑现:

- **运行时注入**(§3.3):`applySkin()` 吃任意 `SkinDefinition` 对象 —— 编辑器在浏览器里
  生成的对象和 `skins/gov.ts` 编译进去的对象**走同一条渲染路径**,改色即时生效,零构建。
- **registry 是对象表而非静态枚举**:`registerSkin(def)` 运行时合入,内置皮肤与编辑器皮肤同表竞争。

### 7.2 功能设计

**两级编辑模式**(核心取舍:全量 token 40+ 个键,一把梭会让运营直接劝退):

| 模式 | 暴露什么 | 受众 |
|---|---|---|
| **基础模式**(默认) | 主色三态(picker)+ 画布底色 + 圆角;**其余全部键由前端从主色按 §5 选色心法自动派生** | 运营/实施 |
| **高级模式** | 折叠面板按组展开全部 `palette` / `comp` / `cssVars` 键 + 原始 JSON 导入导出 | 开发者 |

**其余能力**:

- **实时预览**:右侧内嵌迷你预览区(登录卡片 + 表格 + 按钮组 + 菜单片段),
  编辑即调 `applySkin()` 热更新,不落库不持久;另配"整页试穿"按钮临时切全局。
- **明暗联动**:每改一个 light 值,自动按"暗底提亮一档"规则生成 dark 建议值(可覆盖)。
- **导出**:一键导出 `SkinDefinition` TS 片段(粘进 `skins/` 即成为构建内皮肤)
  或 JSON(粘进高级模式导入)—— **编辑器产物与代码皮肤格式完全同构,没有第二套格式**。
- **保存/启用**:存后端系统配置(`skin.custom` / `skin.active` 两个配置项,见 §7.3),
  启用即全员下次加载生效。

**明确不做**(边界):图片/纹理上传(走 logo_url 通道)、布局密度/结构编辑(那是另一轴)、
在线字体、按租户多皮肤并存(单部署单皮肤,与 branding 单值语义一致)。

### 7.3 界面、保存流程与权限(落地细节)

**在哪个界面**:管理端新页 `系统管理 → 皮肤管理`(`/system/skin`),
权限与 `/system/config` 同款(super_admin 独占,`RoutePermissionGuard`)。

三栏布局:

```
┌─────────────┬──────────────────────────┬──────────────┐
│ 皮肤列表      │  编辑表单                  │  实时预览      │
│ · 内置(只读)  │  基础模式:主色三态 picker    │  登录卡片      │
│   orange/    │   + 画布底色 + 圆角          │  表格+按钮组   │
│   museum/    │  高级模式:全量 token 折叠面板  │  菜单片段      │
│   tech/gov   │   + JSON 导入/导出          │  [整页试穿]   │
│ · 自定义(可编辑)│  [保存草稿] [启用] [停用]     │              │
└─────────────┴──────────────────────────┴──────────────┘
```

**怎么保存**(复用既有系统配置机制,**不新建表、不新建存储模块**):

| 动作 | 落点 | 说明 |
|---|---|---|
| 编辑中 | 仅前端内存(zustand) | 即改即预览;刷新/切走丢弃,有未保存提示 |
| 保存草稿 | 系统配置项 `skin.custom`(JSON 值) | 走既有 config update 接口 —— 版本号、`config/audit` 审计页**自动获得**,零新代码 |
| 启用 | 系统配置项 `skin.active`(内置 id 或 `custom`) | 单独按钮,改 yaml `branding.skin` 的运行时等价物 |
| 停用/回滚 | 把 `skin.active` 改回内置 id / 回退配置版本 | 审计页可见历史 |

**生效链路**:`skin.active` 有值 → `public/config` 在下发 branding 时附带该皮肤
(自定义则内联 SkinDefinition JSON,内置则只下发 id)→ 前端 `main.tsx` 启动时
`registerSkin(...)` + `useUIStore.setSkin(...)` → LangRoot 走既有 `applySkin` 路径。
**所有用户下次进入页面即生效**,无需重新构建或重启。

- 安全:值全部走 `setProperty` / antd token,**只接受颜色/字符串 token 值,永不进 innerHTML**;
  保存与下发两端都做 schema 校验(键白名单 + 颜色格式 + 长度上限),服务端为准。
- 权限:沿用 `/system/config` 的 super_admin 独占模式起步;将来要下放再拆
  `sys:skin:manage` 独立权限码(菜单挂系统管理下)。

### 7.4 依赖与顺序

皮肤编辑器是 **P0 运行时注入架构 + P2 publicConfig 下发通道的直接消费者**,
自身不新增基建,因此排在 P3;若先做编辑器、后做 P0 搬家,会出现"编辑器对接硬编码"的返工。

## 8. 实施顺序

| 阶段 | 内容 | 工作量 |
|---|---|---|
| **P0** ✅ | `types.ts` + `apply.ts` + `registry.ts`;LangRoot palette/comp 与 variables.css 的皮肤相关值搬进 `skins/`(后按用户拍板改为显式全量定义);ui store 加 `skin` 字段 | 已落地 |
| **P1** ✅ | 内置 `white` / `black` / `museum` / `tech` / `gov` 五皮肤 + `custom.example.ts` 模板 + 顶栏皮肤切换器 | 已落地 |
| **P2** ✅ | 后端 `branding.skin` 下发(publicConfig 扩展 + 校验降级);本章手册版挂 `server/docs/web/theming.md` 与 llms.txt。`branding.logo_url` 未做 | 已落地(logo_url 除外) |
| **P3** | 皮肤管理界面(§7):基础/高级两级编辑 + 实时预览 + 后端持久化与 RBAC 权限码 | 2–3 天(未做) |

守卫:P0 验收后给 LangRoot 加一条注释锚点("新增品牌色值必须进 skins/,不得直接写 token"),
并让 `check_docs_links` 收录本文档;待 module-list 生成脚本上线(见 docs/ai-coding.md §5.3),
把三套内置皮肤列为 registry 的机器可读清单。
