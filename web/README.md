# Rapid Admin Web

> 通用后台管理前端骨架(React 18 + Ant Design 5 + Vite 5),配合 [rapid-admin](../server) 后端使用。
> 项目代号:`rapid-admin-web`

## 一、技术栈

| 类别 | 选型 | 版本 |
|------|------|------|
| 构建 | Vite | 5.4 |
| 框架 | React | 18.3 |
| 语言 | TypeScript | 5.5 |
| UI | Ant Design | 5.21 |
| 路由 | React Router | 6.26 |
| 状态 | Zustand | 4.5 |
| HTTP | Axios | 1.7 |
| 工具 | dayjs | 1.11 |
| 图表 | ECharts | 5.5 |
| 测试 | Vitest + Testing Library | 2.1 |
| 代码风格 | ESLint + Prettier | - |

## 二、目录结构

```
web/
├── public/                  # 静态资源(不参与构建)
├── src/
│   ├── api/                 # API 客户端 + 各模块 API
│   │   ├── client.ts        # Axios 实例(拦截器 / 错误处理)
│   │   ├── baseURL.ts       # 后端地址解析(多后端支持)
│   │   ├── auth.ts          # 鉴权(登录/切换身份/密码)
│   │   ├── user.ts          # 用户管理
│   │   ├── role.ts          # 角色 / 资源 / 部门
│   │   ├── config.ts        # 字典 / 平台配置
│   │   ├── file.ts          # 文件系统(单桶单树)
│   │   ├── upload.ts        # 上传
│   │   ├── job.ts           # 定时任务
│   │   ├── smsTemplate.ts   # 短信模板
│   │   ├── inbox.ts         # 站内信
│   │   ├── approval.ts      # 通用审批
│   │   ├── flow.ts          # 审批流程
│   │   ├── dyn.ts           # 动态业务集合(业务表单设计器)
│   │   ├── audit.ts         # 审计
│   │   ├── dashboard.ts     # 工作台
│   │   └── index.ts         # 统一导出
│   │
│   ├── components/          # 公共组件
│   │   ├── common/          # 通用(PageHeader / DataTable / SearchForm / SensitiveField / ImageUploader / LoginHeatmap / ...)
│   │   ├── auth/            # 权限(PermissionGuard / RoleGuard)
│   │   ├── layout/          # 布局(BasicLayout / Header / Sidebar / Breadcrumb / IdentitySwitcher)
│   │   └── index.ts
│   │
│   ├── constants/           # 常量(角色/权限码/字典/应用)
│   │   ├── roles.ts         # 角色枚举(超管 / 平台运营 / 只读)
│   │   ├── permissions.ts   # 全部权限码
│   │   ├── dict.ts          # 字典值
│   │   ├── resourceIcons.ts # 菜单图标词表(单一真源)
│   │   ├── app.ts           # 应用级(STORAGE_KEYS / ROUTES / ...)
│   │   └── index.ts
│   │
│   ├── hooks/               # 自定义 hooks
│   │   ├── usePermission.ts # 权限判断
│   │   ├── useAuth.ts       # 用户 / 身份
│   │   ├── useRequest.ts    # 通用异步
│   │   ├── useTable.ts      # 分页列表
│   │   ├── useDict.ts       # 字典
│   │   ├── useConfig.ts     # 平台配置
│   │   └── index.ts
│   │
│   ├── locales/             # 国际化(中/英,键对称由类型 + 测试双守)
│   │   ├── zh-CN.ts
│   │   ├── en-US.ts
│   │   ├── useI18n.ts
│   │   └── index.ts
│   │
│   ├── mock/                # Mock 数据(开发期)
│   │   └── index.ts
│   │
│   ├── pages/               # 页面
│   │   ├── _template.tsx    # 新页面模板
│   │   ├── login/           # 登录
│   │   ├── forgot-password/ # 忘记密码
│   │   ├── dashboard/       # 工作台(文件统计 / 在线人数 / 登录热力图 / 服务器资源)
│   │   ├── system/          # 系统管理(user / role / permission / department / dict / config / job / sms-template / dyn)
│   │   ├── file/            # 文件系统(list / folder)
│   │   ├── approval/        # 通用审批
│   │   ├── flow/            # 审批流程
│   │   ├── dyn/             # 动态业务集合(设计器驱动的通用列表页)
│   │   ├── audit/           # 审计(operation / data-change / login / system)
│   │   ├── api-docs/        # API 文档
│   │   ├── profile/         # 个人中心
│   │   └── error/           # 错误页 403/404/500
│   │
│   ├── router/              # 路由
│   │   ├── routes.tsx       # 路由表
│   │   ├── guards.tsx       # 鉴权/权限守卫 + 权限加载器
│   │   └── index.tsx        # RouterProvider 出口
│   │
│   ├── stores/              # 全局状态(Zustand)
│   │   ├── user.ts          # 用户 / 身份
│   │   ├── permission.ts    # 权限码 / 菜单
│   │   ├── ui.ts            # 侧边栏 / 主题 / 语言 / loading
│   │   ├── config.ts        # 平台配置
│   │   ├── dict.ts          # 字典
│   │   └── index.ts
│   │
│   ├── styles/              # 全局样式(明暗双主题,CSS 变量驱动)
│   │   ├── global.css
│   │   └── variables.css
│   │
│   ├── test/                # 测试设施(sourceScan / apiRouteScan / setup)
│   │
│   ├── types/               # TS 类型定义
│   │   ├── api.ts           # ApiResponse / PageRequest / BizCode / ...
│   │   ├── user.ts          # UserInfo / Identity / ...
│   │   ├── permission.ts    # Role / Resource / Department / SysUser
│   │   ├── config.ts        # ConfigGroup / ConfigItem / ...
│   │   ├── audit.ts         # OperationLog / LoginLog
│   │   ├── global.d.ts      # Window 扩展
│   │   └── index.ts
│   │
│   ├── utils/               # 工具
│   │   ├── storage.ts       # localStorage 封装(rapid-admin: 前缀)
│   │   ├── format.ts        # 时间/数字/脱敏格式化
│   │   ├── request.ts       # 请求 ID / Idempotency-Key
│   │   ├── crypto.ts        # 密码预处理(SHA-256)
│   │   ├── validators.ts    # 表单校验
│   │   ├── eventBus.ts      # 事件总线
│   │   └── index.ts
│   │
│   ├── App.tsx              # 应用壳
│   ├── main.tsx             # 入口
│   └── vite-env.d.ts        # Vite 类型
│
├── .env.development         # 开发环境变量
├── .env.production          # 生产环境变量
├── .eslintrc.cjs            # ESLint
├── .prettierrc              # Prettier
├── .gitignore
├── index.html               # HTML 入口
├── package.json
├── tsconfig.json            # TS 配置(主)
├── tsconfig.node.json       # TS 配置(node)
├── vite.config.ts           # Vite 配置
└── README.md
```

## 三、快速开始

### 3.1 环境要求

- Node.js 18+
- pnpm 8+(推荐)或 npm 9+

### 3.2 安装依赖

```bash
cd web
pnpm install
```

### 3.3 启动开发

```bash
pnpm dev
# 默认监听 http://localhost:5173
```

`.env.development` 关键变量:

```bash
VITE_API_BASE=/rapid-admin/v1
VITE_USE_MOCK=true     # 启用 mock
VITE_API_TIMEOUT=30000
```

### 3.4 构建生产

```bash
pnpm build
# 产物在 dist/
```

### 3.5 代码质量

```bash
pnpm lint         # ESLint 检查(--max-warnings 0,警告也算失败)
pnpm lint:fix     # 自动修复
pnpm format       # Prettier 格式化
pnpm typecheck    # TypeScript 类型检查
pnpm test         # vitest 全量单测
```

## 四、环境变量

| 变量名 | 必填 | 默认值 | 说明 |
|--------|------|--------|------|
| `VITE_API_BASE` | ✓ | `/rapid-admin/v1` | 后端 API 前缀 |
| `VITE_API_TIMEOUT` | - | `30000` | 请求超时(ms) |
| `VITE_USE_MOCK` | - | `false` | 是否启用 mock |
| `VITE_APP_NAME` | - | `rapid-admin` | 应用名 |
| `VITE_APP_VERSION` | - | `0.1.0` | 客户端版本,提交到 X-Client-Version |
| `VITE_UPLOAD_BIZ` | - | `image` | 默认上传业务类型 |

> 所有 `VITE_*` 变量必须在 `vite.config.ts` 的 `define` 中显式声明,否则类型推导会失败。

## 五、与后端约定

### 5.1 API 响应格式

```typescript
interface ApiResponse<T> {
  code: number          // 0 成功
  message: string
  data: T
  request_id: string    // 串联日志用
  timestamp: number     // ms
}
```

### 5.2 业务码(BizCode)

错误码 ↔ i18n 的映射由契约测试守着(`src/locales/__tests__/biz-code-i18n.test.ts`,
直接解析后端 `errcode.go`):前端映射的每个码都必须在后端真实存在,漏映射会回落
后端原文,静默失败。

### 5.3 请求头

| 头 | 说明 |
|----|------|
| `Authorization: Bearer <token>` | 鉴权 |
| `X-Request-Id` | UUID,串联日志(前端自动注入) |
| `Idempotency-Key` | 写操作幂等键(前端自动注入) |
| `X-Client` | 固定 `admin-web` |
| `X-Client-Version` | 客户端版本 |

### 5.4 契约测试(前端 ↔ 后端 源码级对齐)

web 侧有几条直接**解析后端 Go 源码**的守门网,后端漂移会当场红:

| 测试 | 守的约定 | 对齐的后端出处 |
|------|----------|----------------|
| `api/__tests__/api-route-parity.test.ts` | 每处前端调用都打得到后端路由 | `router.go` |
| `router/__tests__/menu-route-parity.test.ts` | 后端菜单 path 都有前端路由 | `seed.go` menuList |
| `constants/__tests__/resourceIcons.test.ts` | 菜单图标 key 都在前端词表登记 | `seed.go` menuList Icon |
| `constants/__tests__/roles.test.ts` | 角色 code 前端 ⊆ 后端落库 | `enums.go` + `seed.go` roleList |
| `pages/dashboard/__tests__/domain-parity.test.ts` | 看板指标前后端同源 | `dashboard.go` Overview |

### 5.5 路由 path

集中维护在 `src/constants/app.ts` 的 `ROUTES` 常量,业务代码**禁止写死字符串**。

## 六、安全要点

1. **token 单独存**:`storage.set(STORAGE_KEYS.TOKEN, ...)`,不放入 store body
2. **敏感字段脱敏**:用 `<SensitiveField />` 或 `mask.phone()` 工具,**前端绝不展示明文原始数据**
3. **密码传输前预处理**:`preHashPassword()` 做 `sha256("rapid-admin::" + 明文)`,后端再 bcrypt(与后端 seed 的自愈迁移同一契约,**改 salt 必须前后端同 commit**)
4. **写操作幂等**:每个 POST/PUT/PATCH/DELETE 自动注入 `Idempotency-Key`
5. **XSS 防御**:严禁使用 `dangerouslySetInnerHTML`,富文本须用 `DOMPurify`
6. **权限实时刷新**:权限加载统一走 `store.fetchAccessible`,显示层不自己算权限(读执行层权威字段)
7. **错误日志**:后端出错会带 `request_id`,前端报错时展示给用户方便提单

## 七、功能模块清单

- **系统管理**:用户 / 角色 / 资源(权限矩阵)/ 部门 / 数据字典 / 平台配置(含变更审计)
- **文件系统**:单桶单树,文件列表 + 文件夹,每个文件有不带签名的稳定直链
- **审计**:操作日志 / 数据变更 / 登录日志 / 系统审计
- **通用审批**:审批管理(模板 / 流程)/ 我的审批 / 流程详情
- **动态业务集合**(M151):业务表单设计器 + 通用列表页(`dyn/:code`),零代码建表单
- **定时任务 / 短信模板 / 站内信**
- **工作台**:文件统计 / 当前在线 / 每日登录热力图 / 服务器资源快照(按 `dashboard:server` 权限显隐)
- **国际化**:中英双语,键对称由类型(`typeof zhCN`)+ locale-parity 测试双守

## 八、扩展新页面的纪律

1. 路由进 `router/routes.tsx`,path 常量进 `constants/app.ts` 的 `ROUTES`,**禁止写死字符串**
2. 权限码进 `constants/permissions.ts`,路由守卫用 `anyOf` 兜底超管
3. 新页面的菜单 / 图标要在**后端 seed** 登记资源行,图标 key 必须在 `constants/resourceIcons.ts` 词表里
4. 新调用的 API 路径必须与后端 group 前缀一致(契约测试会红)
5. 中英文 locale **同步加键**(两份文件键对称,漏一边编译期/测试都会红)
6. 页头用 `<PageHeader description={t('page.xxx.description')} />`,不传 title(标题归面包屑管),不写死中文
