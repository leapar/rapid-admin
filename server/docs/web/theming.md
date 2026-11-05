# theming —— 前端 UI 皮肤(换肤只写一个文件)

> **皮肤 = 整体外观**(2026-11-05 扁平化):黑色皮肤就是黑、白色皮肤就是白,
> 皮肤内部不分 dark/light 两套,顶栏也没有明暗切换器。
> 换肤 = 写一个声明式 ts 文件,或在部署配置里配一行 `branding.skin`。
> 完整设计文档(架构取舍 / 皮肤编辑器规划)见仓库根 `docs/theming.md`,本章只讲怎么用。

## 功能

- **五个同级内置皮肤**:白色(默认)/ 黑色 / 科技蓝 / 政务红 / 文博棕金
  —— 每套皮肤一套色板 + 一个 `appearance`(light/dark,决定 antd 算法与
  `<html data-theme>`),点谁切谁
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

### 定义自定义皮肤(三步,约 15 分钟)

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

### 硬约束

**新增品牌色值必须进 `web/src/theme/skins/`,不得直接写进 `LangRoot` 的 token** ——
色值的单一来源是皮肤对象;`variables.css` 里的 `--app-primary-*` 只是首帧 fallback,
运行时由 `applySkinCssVars` 注入覆盖。这条在根 `AGENTS.md` 硬约束 #6。
