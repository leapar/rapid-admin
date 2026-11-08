# Rapid Admin · 后台快速开发平台

> 开箱即用的 Go + React 通用后台骨架:RBAC 权限、数据范围、字段级脱敏、双级缓存、审计、手册与 Swagger 全内置。
> A production-ready admin boilerplate in Go (Hertz + GORM + Casbin) with a React 18 + Ant Design 5 frontend.

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=black)
![Ant Design](https://img.shields.io/badge/Ant%20Design-5-1677FF?logo=antdesign&logoColor=white)

## 功能特性

- **鉴权与账号**:JWT(access/refresh 双令牌 + 吊销)、多身份多部门切换、短信/验证码登录、登录态管理、异常登录检测
- **权限模型**:接口级 RBAC(Casbin `rbac_with_domains`)+ 行级数据范围(仅本人/本部门/本项目可扩展)+ 字段级可见性(隐藏/打码,前端自动消费)
- **动态表单**:PocketBase 风格的动态集合(设计器 + 运行时 SDK),字段类型/校验/关联表开箱即用
- **通用审批**:可配置审批流引擎,提交人≠审批人强制分人
- **文件系统**:单桶单树模型,分片上传、稳定直链、媒体探测(ffmpeg)
- **消息与通知**:站内信、SSE 事件流、短信(多 provider + 限流 + 发送审计)、邮件
- **框架原语 53 个**:两级缓存(LRU + Redis)、幂等键、分布式锁、重试、限流、熔断、灰度、KMS、数据变更审计、全文搜索……每个都有手册章节 + 可运行示例
- **工程化守卫**:框架原语可达性守卫、文档断链扫描、手册产物漂移检测、计数一致性校验 —— 文档说谎会让 CI 变红
- **内置手册与 API 文档**:70 页 GitBook 手册(`go:embed` 嵌进二进制)+ OpenAPI 3.0(Swagger UI 挂在 `/swagger`)

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go 1.22+ · [CloudWeGo Hertz](https://github.com/cloudwego/hertz) · [GORM](https://gorm.io) · [Casbin](https://github.com/casbin/casbin) · [go-redis v9](https://github.com/redis/go-redis) · [Viper](https://github.com/spf13/viper) · [Zap](https://github.com/uber-go/zap) · [golang-jwt v5](https://github.com/golang-jwt/jwt) · [Sonyflake](https://github.com/sony/sonyflake) |
| 前端 | React 18 · Ant Design 5 · TypeScript 5 · Vite 5 · Zustand · Axios · ECharts · Vitest |
| SDK | [@rapid-admin/dyn-sdk](js-sdk/)(零运行时依赖,动态表单数据面) |
| 文档 | HonKit(GitBook)70 页 · Swagger UI · 58 个 `go run` 即跑的示例 |
| 数据库 | MySQL 8(生产) / SQLite(开发,免装依赖) · Redis(缓存/限流/幂等,可选) |

## 快速启动

```bash
# 1. 后端(SQLite 开发模式,免装 MySQL)
cd server
make migrate-sqlite        # 建表(AutoMigrate)
make seed-sqlite           # 灌种子:角色/资源/字典/超管账号
make dev-sqlite            # 启动 → http://127.0.0.1:8888
# 默认超管:admin / Admin@2026(生产环境务必改掉)

# 2. 前端
cd web
pnpm install
pnpm dev                   # → http://localhost:5173(代理到 8888)

# 3. 文档
cd server/docs && npm install && npm run serve   # 手册 → http://localhost:4000
# 或服务起来后直接看 http://127.0.0.1:8888/docs/index.html
# API 文档: http://127.0.0.1:8888/swagger/index.html
```

> MySQL 模式:`make migrate-mysql && make seed-mysql && make dev-mysql`(配置见 `server/configs/config.example.yaml`)。
> 测试:`cd server && make test` · `cd web && pnpm test`。

## 目录结构

```
rapid-admin/
├── server/          # Go 后端(Hertz + GORM + Casbin,53 个框架原语 + 70 页手册)
├── web/             # React 18 + Ant Design 5 管理端
├── js-sdk/          # @rapid-admin/dyn-sdk —— 动态表单数据面 SDK(零运行时依赖)
├── docs/            # 设计文档(权限模型 / 文件系统 / 通用审批 / 完成清单 ROADMAP)
├── scripts/         # 工程守卫脚本(手册链接检查 / 清单一致性校验)
└── server/deploy/   # systemd unit / 备份恢复脚本 / Prometheus 告警规则
```

## Keywords / Topics

`admin-dashboard` `admin-template` `boilerplate` `rbac` `casbin` `permission-management`
`go` `golang` `hertz` `gorm` `react` `antd` `ant-design` `typescript` `vite`
`zustand` `jwt` `dynamic-form` `audit-log` `swagger` `openapi` `admin-panel`
`scaffold` `rapid-admin` `后台管理系统` `权限管理` `快速开发平台`


## License

[Apache-2.0](LICENSE)
