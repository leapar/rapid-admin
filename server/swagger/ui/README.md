# swagger-ui 静态资产

## 这是什么

Swagger UI 的**官方发行版**(`swagger-ui-dist`),加上本仓库自己写的两个文件:

| 文件 | 来源 | 说明 |
|---|---|---|
| `swagger-ui*.js` / `swagger-ui.css` | 上游发行版 | 未改动 |
| `index.css` / `oauth2-redirect.html` / `favicon-*.png` | 上游发行版 | 未改动 |
| `index.html` | **本仓库改写** | 标题改中文、`lang="zh-CN"`、**保证零外链**(见下) |
| `swagger-initializer.js` | **本仓库改写** | spec 地址指向 `./doc.json`(上游默认指 petstore) |

### 关于"零外链"

`index.html` 里一条外链都没有 —— 内网/离线部署时,外链请求会挂住,
页面渲染被拖到超时,症状是"UI 转圈半天不出来",而控制台不一定有明显的错。

⚠️ **别误以为上游 5.10.5 有外链** —— 它没有(旧版 swagger-ui 3.x 的
`swagger-ui.css` 里有过 Google Fonts 的 `@import`,本仓库这份 5.10.5 已经没有了)。
这条约束是**留给以后**的:谁哪天往 `index.html` 里加一行 CDN 字体,测试会兜住
(`server/swagger/embed_test.go` 的 `TestAssets_indexHTML没有外链`)。

## 版本与出处

- **swagger-ui 5.10.5**(2023-12 发行版)
- 上游:https://github.com/swagger-api/swagger-ui —— 发行包 `swagger-ui-dist`
- 许可:**Apache-2.0** —— 许可证全文**已随资产收在本目录的 `LICENSE` 文件里**
  (开源时必须带上:分发第三方二进制就得带上它的许可证)

⚠️ **升级版本时请同步替换全部 `swagger-ui*.js` / `swagger-ui.css`** ——
它们必须来自同一次发行,混版会出现"页面空白但控制台无错"这类难查的问题。

## 为什么丢了 `.map` 文件

上游发行包带 `*.js.map` / `*.css.map`(source map),合计约 4.6MB。

- 它们**只在调试压缩后的 swagger-ui 本身时有用** —— 而没人会调 swagger-ui 的源码
- 它们让仓库凭空多一倍体积,且每次升级都要重新 review 一堆不可读的 JSON

⇒ 决定:**不收录 .map**。要调 swagger-ui 的话,去上游仓库改,别在本仓库做。

## 怎么改

- 改配色 / 布局 → 改 `swagger-ui.css` 末尾追加自己的规则(**不要**改上游文件本体,
  下次升级会冲突)
- 改标题 / logo → 改 `index.html`
- 改初始行为(默认展开哪些 tag、是否深链接)→ 改 `swagger-initializer.js`

⚠️ 这三个文件在本仓库里**会被 `//go:embed` 打进二进制** —— 改完直接重启服务即可,
不需要重新编译前端。

## 鉴权

`/swagger` 整条路径(UI + `doc.json`)默认带 **BasicAuth**,
配置见 `server/internal/config/swagger.go`。
