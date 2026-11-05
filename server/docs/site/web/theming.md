# theming —— 前端 UI 皮肤(换肤只写一个文件)

> 管理端的品牌色相是**皮肤**(skin),与明暗主题(theme)正交:任何皮肤 × 浅色/深色都合法。
> 换肤 = 写一个声明式 ts 文件,或在部署配置里配一行 `branding.skin`。
> 完整设计文档(架构取舍 / 皮肤编辑器规划)见仓库根 `docs/theming.md`,本章只讲怎么用。

## 功能

- **四套内置皮肤**:`orange`(品牌橙,默认)/ `museum`(文博棕金)/ `tech`(科技蓝)/ `gov`(政务红)
- **两轴正交**:皮肤(品牌色相)× 明暗主题各自独立切换、各自持久化(localStorage)
- **部署级下发**:后端 `branding.skin` 配置 → `GET /public/config` 下发 → 部署级默认皮肤,
  改 yaml 即全生效,不用改前端
- **自定义皮肤**:拷模板改色值,升级框架不冲突

## 配置

`server/configs/config.example.yaml`:

```yaml
branding:
  app_name: ""
  app_title: ""
  skin: ""   # orange / museum / tech / gov;留空 = 用户自己在顶栏选
             # 配了 = 部署级默认(用户仍可在顶栏切换)
```

- 配了**未知 id 不报错**:前端 `getSkin` 显式降级默认橙并打 console.warn(容错降级)。
- `skin` 走 `GET /rapid-admin/v1/public/config` 的白名单字段(匿名可读 ——
  皮肤和品牌名一样印在登录页上,不是秘密)。

## 使用方法

### 顶栏切换

登录后顶栏:调色板图标 = 皮肤切换(品牌色相),太阳/月亮 = 明暗切换。
选择持久化在 localStorage,跨会话保留。

### 定义自定义皮肤(三步,约 20 分钟)

```bash
cd web/src/theme/skins
cp custom.example.ts my-brand.ts
```

打开 `my-brand.ts`,**必改 6 个键**(其余模板已派生好):

```ts
id: 'my-brand',
name: '我的品牌',
brand: { primary: '#7C3AED', hover: '#9B5CF6', active: '#5B21B6' },
palette: { light: { colorBgLayout: '#F6F4FA', colorPrimaryBg: '#F1EAFD' }, dark: { /* 同梯度暗色 */ } },
```

然后在 `web/src/theme/registry.ts` 的 `SKINS` 表挂一行:

```ts
import myBrand from './skins/my-brand'
const SKINS = { orange, museum, tech, gov, 'my-brand': myBrand }
```

选色心法(theming.md §5):hover 亮 8–12% / active 暗 8–12%;浅底(Bg 系)用主色
≥10% 透明度或 95% 明度的衍生色;深色模式品牌文字比主色提亮一档;阴影只换色相不动结构。

### 硬约束

**新增品牌色值必须进 `web/src/theme/skins/`,不得直接写进 `LangRoot` 的 token** ——
色值的单一来源是皮肤对象;`variables.css` 里的 `--app-primary-*` 只是首帧 fallback,
运行时由 `applySkinCssVars` 注入覆盖。这条在根 `AGENTS.md` 硬约束 #6。
