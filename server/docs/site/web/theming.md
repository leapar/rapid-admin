# theming —— 前端 UI 皮肤(换肤只写一个文件)

> **皮肤 = 整体外观**(2026-11-05 扁平化):黑色皮肤就是黑、白色皮肤就是白,
> 皮肤内部不分 dark/light 两套,顶栏也没有明暗切换器。
> 换肤 = 写一个声明式 ts 文件,或在部署配置里配一行 `branding.skin`。
> 完整设计文档(架构取舍)见仓库根 `docs/theming.md`,本章只讲怎么用。

## 功能

- **五个同级内置皮肤**:白色(默认)/ 黑色 / 科技蓝 / 政务红 / 文博棕金
  —— 每套皮肤一套色板 + 一个 `appearance`(light/dark,决定 antd 算法与
  `<html data-theme>`),点谁切谁
- **皮肤编辑器(MVP)**:管理端 `系统管理 → 皮肤管理`(`/system/skin`,
  super_admin 可见)可视化编辑 —— 克隆内置皮肤起步,改完可整页试穿、
  导出 TS/JSON,导出产物与代码皮肤**同一格式**(见下文「皮肤编辑器」)
- **字段全集基准**:任何皮肤都必须定义与 `white.ts`/`black.ts` 完全相同的
  token 键集(`skins.complete.test.ts` 在守,漏一个色调即红)
- **部署级下发**:后端 `branding.skin` 配置 → `GET /public/config` 下发 → 部署级默认皮肤,
  改 yaml 即全生效,不用改前端
- **自定义皮肤**:拷模板改色值,升级框架不冲突

## 配置

`server/configs/config.example.yaml`:

```yaml
branding:
  app_name: ""
  app_title: ""
  skin: ""   # white / black / tech / gov / museum(旧 orange 兼容映射到 white)
             # 留空 = 用户自己在顶栏选;配了 = 部署级默认(用户仍可在顶栏切换)
```

- 配了**未知 id 不报错**:前端 `getSkin` 显式降级默认白并打 console.warn(容错降级)。
- `skin` 走 `GET /rapid-admin/v1/public/config` 的白名单字段(匿名可读 ——
  皮肤和品牌名一样印在登录页上,不是秘密)。

## 使用方法

### 顶栏切换

登录后顶栏:调色板图标 = 皮肤菜单(五个同级项,带色板圆点,当前项打勾)。
选择持久化在 localStorage,跨会话保留;旧版本存的 `theme`(明暗轴)不再生效。

### 皮肤编辑器(可视化,不碰代码)

`系统管理 → 皮肤管理` 三栏:左栏内置皮肤列表(**副本**起步)、中栏编辑表单、
右栏预览 + 试穿 + 导出。

- **两个独立输入,别混**:**界面色调**决定画布/侧栏/顶栏/文字/边框等中性面的
  色相与浓度(选**纯灰**就是白色/黑色皮肤那种无彩底);**主色**只决定
  按钮/链接/选中态等强调色。两者互不影响 —— 内置 white/black 的界面颜色
  与品牌橙 `#f60` 无关,就是这个模型。
- **按主色重算全部色值**:以上两个输入重算全部 58 键建议值(卡片/弹窗/
  表单背景和文字都包含在内),明度对比阶梯照搬参考皮肤(浅色=文博棕、
  深色=黑),不会调出对比度不成立的组合;⚠️ 高级模式里的手动调整会被覆盖。
- **整页试穿**:把草稿注册成当前皮肤穿到整站(编辑即时生效,离开页面自动
  恢复原皮肤)。
- **导出**:TS 片段或 JSON —— 产物是标准 `SkinDefinition`。**本期编辑器
  不做后端保存**,落地方式:TS 片段存为 `web/src/theme/skins/my-skin.ts`
  并在 registry 挂一行(即下面的三步流程),JSON 留作备份/交接。
- **高级模式**:palette 25 键 + comp 33 键逐键覆盖(按画布/文字/边框/
  品牌强调/布局/菜单/表格分组)。

### 定义自定义皮肤(三步,约 15 分钟)

> 有皮肤编辑器时,下面这三步通常是**导出 TS 片段后落文件 + 挂 registry** 的
> 手工版;从零手写也一样从拷模板开始。

```bash
cd web/src/theme/skins
cp custom.example.ts my-brand.ts
```

打开 `my-brand.ts` —— 皮肤是**显式全量定义**,palette / comp / cssVars 每个
颜色一个参数,逐个可改。**必改头部几项**:

```ts
id: 'my-brand',
name: '我的品牌',
appearance: 'light', // 深色外观写 'dark' 并整表换暗色值(参考 black.ts)
brand: { primary: '#7C3AED', hover: '#9864f1', active: '#6115e4' },
```

其余色值按需微调:模板按 antd "seed → map token" 算法理念整体调过
(中性面同一色温、品牌浅底同系衍生),只想换个主色的话**对着内置皮肤同键位
抄档位**即可 —— 例如把白色皮肤的画布/文字/边框档位抄过来,只换品牌系。

然后在 `web/src/theme/registry.ts` 的 `SKINS` 表挂一行:

```ts
import myBrand from './skins/my-brand'
const SKINS = { white, black, tech, gov, museum, 'my-brand': myBrand }
```

选色心法(theming.md §5):hover 亮 8–12% / active 暗 8–12%;浅底(Bg 系)用主色
≥10% 透明度或 95% 明度的衍生色;深色皮肤(如 black)的品牌文字比主色提亮一档;
阴影只换色相不动结构。改完跑 `pnpm vitest run src/theme` ——
`skins.complete.test.ts` 会核对字段全集与同调性。

### 沉浸式(皮肤怎么"铺满"界面)

内置品牌皮肤(gov/museum/tech)是**沉浸式**的:顶栏用品牌色实底
(`comp.layoutHeaderBg`)配浅色文字(`headerTextColor` 四键,Header.tsx
内联 remap 到 `--app-text*` 变量),侧栏/画布/表头/菜单选中带同色相的品牌晕;
white/black 保持毛玻璃顶栏。三段深度:顶栏(最深)→ 侧栏(中)→ 画布/卡片(亮)。
自定义皮肤想要实底顶栏,把 `layoutHeaderBg` 换实色 + `headerTextColor`
四键换浅色文字即可(参考 `gov.ts`);侧栏背景由 LangRoot 的 `Layout.siderBg`
token 驱动,**不要在布局组件里写死内联 background**(内联样式会压过 token)。

### 语义色边界(哪些颜色不随皮肤)

success 绿 / warning 金 / error 红 / diff 增删底是**状态语义**,全站统一,
不随皮肤走。antd 预设蓝的"信息高亮"(`Tag color="blue"` 与 `type="info"`
的 Alert)已由 `styles/global.css` 的 CSS 覆盖统一重定义为品牌浅底
(走 `--app-primary*` 变量)—— ⚠️ 这类覆盖**必须加 `!important`**:
antd cssinjs 是运行时注入 `<head>`,同优先级平局时后注入者赢。

### 硬约束

**新增品牌色值必须进 `web/src/theme/skins/`,不得直接写进 `LangRoot` 的 token** ——
色值的单一来源是皮肤对象;`variables.css` 里的 `--app-*` 静态值只是首帧 fallback,
运行时由 `applySkinCssVars` 注入覆盖(品牌系 7 键之外,还映射 `--app-bg-layout` /
`--app-surface-*` / `--app-text*` 等中性面与文字梯度 —— 皮肤切换后 CSS 变量
消费方也跟着换色温)。这条在根 `AGENTS.md` 硬约束 #6。
