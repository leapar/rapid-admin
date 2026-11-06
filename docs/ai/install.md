# install.md —— AI 可执行的起步剧本

> 目标:AI agent 按本文从零把 rapid-admin 跑起来,并打第一个真实接口。
> 每步都写了**预期输出** —— 用于判断成功/失败;失败先按"排查"小节处理,
> 不要跳步。人读的同款内容在 [server/docs/getting-started/README.md](../../server/docs/getting-started/README.md)。

## 0. 前置依赖

| 依赖 | 版本 | 检查命令 | 预期 |
|---|---|---|---|
| Go | 1.22+ | `go version` | `go1.22` 及以上 |
| Node + pnpm | Node 18+,pnpm 8+ | `pnpm -v` | 有版本号即装好 |
| MySQL 8 | 可选¹ | —— | 没有也能跑(见下) |
| Redis 7+ | 可选¹ | —— | 没有自动降级到进程内实现 |

¹ 示例与单测用 SQLite / 内存实现,起步阶段**不需要**真装 MySQL / Redis。
ffmpeg / ffprobe 只有 `media` 模块需要,起步不用。

## 1. 后端五步

```bash
cd server
cp configs/config.example.yaml configs/config.yaml   # ① 配置(每个键有注释,默认值可跑)
go run ./cmd/migrate -config configs/config.yaml     # ② 建表 + seed
go run ./cmd/server -c configs/config.yaml           # ③ 启动(注意是 -c,不是 -config)
```

- ② 预期:输出迁移与 seed 日志,最后提示完成。改初始密码:加
  `--password '你的新密码'`(migrate 认 `-config`,server 认 `-c`,别混)。
- ③ 预期:监听 `:8888`,启动日志打印模块装配清单;Ctrl+C 退出。

**验证**(另开一个终端):

```bash
curl -s http://127.0.0.1:8888/healthz       # 预期:{"code":0,...} 存活
curl -s http://127.0.0.1:8888/readyz        # 预期:code 0,依赖探活通过
```

| 端点 | 说明 |
|---|---|
| `http://127.0.0.1:8888/swagger/index.html` | API 文档(OpenAPI 3.0 全量接口 + RBAC 权限码) |
| `http://127.0.0.1:8888/docs/index.html` | 内嵌手册(57 页,不需要 node) |

## 2. 打第一个真实接口

```bash
# 登录(public 组,免 token)—— 预期:code 0,data 里有 accessToken / refreshToken
curl -s -X POST http://127.0.0.1:8888/rapid-admin/v1/public/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin@2026"}'

# 用返回的 accessToken 调一个鉴权接口(路径与字段以 /swagger 的 doc.json 为准)
curl -s http://127.0.0.1:8888/rapid-admin/v1/system/profile \
  -H "Authorization: Bearer <accessToken>"
```

排查:登录 401 → 密码改过(重跑 migrate `--password 'Admin@2026'` 不行就查库);
`readyz` 非 0 → 看 `/readyz` 返回体里哪个依赖红(默认配置不探 MySQL/Redis,一般不会红)。

## 3. 前端三步

```bash
cd web
pnpm install          # 预期:装完无 err(pnpm 会按 workspace 提示,直接回车)
pnpm dev              # 预期:Vite 启动,地址 http://localhost:5173
```

浏览器打开 `http://localhost:5173` → 登录页 → `admin / Admin@2026`。
预期:进入后左侧菜单按数据库下发的权限码渲染。

- dev 代理已在 `vite.config.ts` 配好(指向 `127.0.0.1:8888`),后端没起时登录会 5xx。
- 类型检查 / 测试:`pnpm typecheck`、`pnpm test`(Vitest)、`pnpm lint`(零 warning 容忍)。

## 4. 跑一个框架模块示例

```bash
cd server
go run ./examples/cachex        # 例:两级缓存。预期:打印命中/未命中演示
go run ./examples/idempotentx   # 例:幂等键。预期:重复请求第二次被拦
```

52 个模块清单与每模块一句话:[docs/ai/module-list.md](module-list.md);
手册对应章节:`server/docs/reference/packages.md`(包速查表)。

## 5. 完成定义

- [ ] `curl /healthz` 返回 code 0
- [ ] 登录接口返回 accessToken
- [ ] 前端登录成功,能看到菜单
- [ ] `go run ./examples/<某模块>` 有输出
- [ ] `cd server && go test ./...` 全绿(改动代码后必跑)
