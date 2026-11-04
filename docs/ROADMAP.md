# 框架层未完成项清单 v2.0

> **目的**:盘点后端 / 前端**框架层**(非业务)代码的完成度。**已完成**与**未完成**同等对待,逐条标证据。
>
> **范围**:**只盘点"通用框架能力"** —— 不依赖具体业务、抽出后能复用到其他项目。
> **不盘点**业务实现 —— 业务方接入自己的模块后,可以照这份清单的口径
> 给自己的业务层盘点(清单本身只对"通用框架能力"负责)。
>
> **关联文档**:
> - 设计:[docs/design/后台权限模型设计V1.0.md](design/后台权限模型设计V1.0.md)
>
> **更新日期**:2026-11-02(本次**第一轮**:§1.1 #17「一份能力清单取代三份码表」入账 + 复查时撞出 §1.1 #18(审计菜单死菜单洞)⇒ RBAC 100% → 89%,§5 60% → 55%;状态对账见下方 **M92**;原始盘点 2026-10-19,基于 git HEAD a923f9f)
> **更新日期**:2026-11-02(**第二轮**:把「一个通用后台框架还缺什么横切能力」重新过了一遍 ⇒
> **新开 §1.13 数据可见性(行级 + 字段级)/ §1.14 领域事件总线 / 事务性 outbox**;
> §1.12 补 4 项(字段类型 / 导入导出 / 行级操作 / 审批适配器)、§2.8 补 1 项(保存的列表视图)、
> §6 由 8 类扩到 10 类、§8 补 §8.2(手册缺章 + 审批适配器)
> ⇒ **后端 145/149 = 97% → 147/162 = 91%**,前端 **107/116 = 92% → 106/116 = 91%**;
> **更新日期**:2026-11-02(**第三轮**:M160 把 dyn 的数据面接上数据范围 ⇒
> 关掉 §1.12 #11 与 §1.13 #3 ⇒ 后端 **147/162 = 91% → 149/162 = 92%**,
> §1.12 **67% → 73%**、§1.13 **40% → 60%**);
> **更新日期**:2026-11-02(**第四轮**,M161):业务查询接上数据范围 ⇒ 关掉 §1.13 #2 ⇒
> 后端 **149/162 = 92% → 150/162 = 93%**,§1.13 **60% → 80%**);
> **更新日期**:2026-11-02(**第五轮**:M162-M164 字段级那一轴的**服务端一半 + 配置界面**落地 ⇒
> 关掉 §1.13 #4 ⇒ 后端 **150/162 = 93% → 151/163 = 93%**,§1.13 **80% → 83%**)
> ⚠️ **§1.13 #5 当时还开着两条尾巴** —— 两个登记表之间没有交叉守门网 / 前端消费那一半(记在 §2.7 #16)
> **更新日期**:2026-11-02(**第六轮**:M166 补上两个登记表之间的**交叉守门网** ⇒
> §1.13 #5 只剩**一条尾巴**(前端消费那一半,记在 §2.7 #16);
> ⚠️ **百分比仍是 83%**(5/6)—— 尾巴没关掉就不许涨)
> **更新日期**:2026-11-02(**第七轮**:M167 给"脱敏收口在出口"补上**机械守卫**
> (`internal/api/handler/outlet_guard_test.go` 扫源码:绕过出口的写入点必须逐条登记
> + 写明理由 + **条数精确**)⇒ §1.13 **仍是 83%**(5/6)—— #5 的尾巴没关掉就不许涨)
> **更新日期**:2026-11-02(**第八轮**,M168):前端消费那一半落地 ⇒ 关掉 §1.13 #5 与 §2.7 #16 ⇒
> 后端 **151/163 = 93% → 152/163 = 93%**,§1.13 **83% → 100%**;
> 前端 **106/116 = 91% → 107/116 = 92%**,§2.7 **88% → 94%**)
> **更新日期**:2026-11-02(**第九轮**,M169):平台运营负责人的审计死菜单洞补上 ⇒ 关掉 §1.1 #18 ⇒
> 后端 **152/163 = 93% → 153/163 = 94%**,§1.1 **89% → 94%**;
> 前端不变 **107/116 = 92%**)
> **更新日期**:2026-11-02(**第十一轮**,M171):§2.8 #16 可编辑表格落地 ⇒
> §2.8 **82% → 86%**;前端 **107/116 = 92% → 108/116 = 93%**;后端不变 **153/163 = 94%)
> **更新日期**:2026-11-02(**第十二轮**,M172):§2.3 #9 请求进度回调落地 ⇒
> §2.3 **92% → 100%**;前端 **108/116 = 93% → 109/116 = 94%**;后端不变 **153/163 = 94%)
> **更新日期**:2026-11-02(**第十三轮**,M173):§2.6 #7 懒加载语言包落地 ⇒
> §2.6 **90% → 100%**;前端 **109/116 = 94% → 110/116 = 95%**;后端不变 **153/163 = 94%**;
> en-US 语言包从主 chunk 拆成独立异步 chunk(实测 build:70.51 kB / gzip 23.70 kB)——
> 多数中文用户首屏不再白下载它)
> **更新日期**:2026-11-03(**第十四轮**,M174):§2.8 #17 拖拽排序落地(零新依赖,原生 HTML5 DnD,
> 工作副本快照状态机)⇒ §2.8 **86% → 91%**;前端 **110/116 = 95% → 111/116 = 96%**;后端不变)
> **更新日期**:2026-11-03(**第十五轮**,M175):§1.14 领域事件总线落地(`pkg/eventx` 原语 + 事务性
> outbox + 投递器退避/死信 + 消费幂等 Deduper + 两个手写 publisher 收口线字节对拍)⇒
> §1.14 **0% → 100%**;后端 **153/163 = 94% → 157/163 = 96%**;前端不变 **111/116 = 96%**;
> §6 十类全部 ≥90% 达标线达成)
> **更新日期**:2026-11-03(**第十六轮**,用户拍板两项范围外):**移动端 / 响应式**(§2.8 原 #20)与
> **RS256**(§1.2 原 #9)经用户拍板移出账本不进计数 ⇒ §2.8 **91% → 95%**、§1.2 **92% → 100%**;
> 后端 **157/163 → 157/162 = 97%**;前端 **111/116 → 111/115 = 97%**
> **更新日期**:2026-11-03(**第十七轮**,框架收尾 + 4 个并行 agent):按「通用后台框架」定位做终局裁决 ——
> **落地 4 项**:M176 `useSavedViews`(§2.8 #21)、M177 dyn 行级操作+批量导入导出(§1.12.2 #13/#14)、
> M178 通用 SchemaForm(§2.7 #15)、M179 文件夹页内嵌文件区 + 帮助下拉(API 文档/GitBook,用户直报);
> **收编 1 项**:§2.2 #10 数据级权限(框架侧已由 §1.13 全链路落地,该行过时);
> **移出/暂缓 8 项**(各有墓碑注记):§1.1 #17 能力清单本体、§1.12 #12 字段类型补齐 / #15 审批适配器(暂缓第二期)、
> §2.10 #13 业务 API 类型、§3 #10 业务 endpoint、§5 #6 异地多活 / #9 画布式设计器 / #11(并入 §1.1)
> ⇒ **后端 §1.1-§1.14 = 159/159 = 100%,前端 §2.1-§2.10 = 114/114 = 100% —— 框架层账本全部收口**
> (暂缓/范围外项以墓碑注记留档,不进计数;真要做时各节有现成施工图)
> **更新日期**:2026-11-02(**第十轮**,M170):§1.1 #17 的止血网(第 0+1 步)落地 ⇒
> 启动期 fail-fast 守门网 + 叶子菜单必调码登记表,**当天抓出两个真洞**(property_manager 缺 folder:list /> dev.db 垃圾角色 ces 五个死菜单);合计不变 —— 后端 **153/163 = 94%**,前端 **107/116 = 92%)
> ⚠️ **数字降下来不是变差,是账本补齐了** —— 后端新补进来的 **13 个**子项(§1.12 +4 / §1.13 5 / §1.14 4)此前一个都没被数过。
> 另:§1.12 的计数块 `###` → `####`,**第一次进了守门网**(此前守门网扫不到那一节))
> **v2.1 修订(2026-10-26,M92)**:全表**状态对账** —— 此前 **9 个**「❌ 未完成(N 项)」块的
> 每一行都已经打了 ✅ M-xx(早就做完了),另有一堆计数跟实际行数对不上。本次把每一节的
> 计数、百分比、两张总览表全部改成**跟表格实际对得上**,并加了机器守门网
> `scripts/check_backlog_consistency.py`(计数模型见 §0,落地记录见文末 M92)。
> **M1-M11 已落地**(2026-10-19):
> - ✅ M1 `is_primary` 业务层唯一约束(commit 32d7c4b)
> - ✅ M2 资源代码表 + 启动期一致性校验(commit 9154937)
> - ✅ M3 `/readyz` 真实探活(DB + Redis,500ms)(commit ba0aa84)
> - ✅ M4 OpenTelemetry trace 导出(OTLP gRPC/HTTP,接阿里云 ARMS APM)(commit 3b15550)
> - ✅ M5 per-route 超时熔断(Timeout + gobreaker)(commit 40070d9)
> - ✅ M6 前端错误码 i18n(tError + BIZ_CODE_KEYS,34 码)(commit 7c406c7)
> - ✅ M7 缓存三防(singleflight + 随机 TTL + null sentinel)(commit e3a1719)
> - ✅ M8 Prometheus 指标 + `/metrics` 端点(commit 5384c36)
> - ✅ M9a Gzip 压缩中间件(commit 63d98a4)
> - ✅ M9b 请求体大小限制(commit 3b9cb95)
> - ✅ M9c Idempotency-Key 框架级(commit 93dc607)
> - ✅ M10 阿里云 OSS provider(PostObject 直传)(commit 073bd61)
> - ✅ M11 前端 AbortController 请求取消(commit 9639fd5)
> **M12-M17 已落地**(2026-10-19):
> - ✅ M12 框架级 PII 字段解密访问审计 `sys_pii_access_log` + `pkg/piiaudit/` 异步批写(commit 6ed5a4e)
> - ✅ M13 业务错误码集中文档生成器 `cmd/gen-errcode-doc` + `docs/reference/error-codes.md` 44 码(commit ca546cd)
> - ✅ M14 短信发送审计 `sys_sms_send_log` + `pkg/smsaudit/` 装饰 Sender(commit 474bfd0)
> - ✅ M15 短信限流 per phone+scene `pkg/smslimit/` Redis 固定窗口(commit 42ab280;**🔴 M112 修正**:旧"固定窗口"每次续 TTL ⇒ 计数永不过期 ⇒ 超限手机号被永久拉黑)
> - ✅ M16 顶栏主题切换 light/dark(Antd 5 algorithm + CSS 变量覆盖)(commit 97e7a20)
> - ✅ M17 路由多 tab + 软 keepAlive(scrollY + formState)(commit 95f020b)
> **M18-M22 已落地**(2026-10-19):
> - ✅ M18 token 自动刷新(401→refresh 重试 + 5min 提前调度 + 单飞锁)(commit 52f048a)
> - ✅ M19 顶栏语言切换 + 数字/日期 locale 感知(commit 4d4a2cd)
> - ✅ M20 go-playground/validator 框架集成 + `BindAndValidate` 装饰器(commit 5be77af)
> - ✅ M21 密钥使用审计 `sys_crypto_audit_log` + crypto 包接入(commit 3cb5889)
> - ✅ M22 权限实时刷新 SSE(Redis pub/sub → `/auth/perm-stream`)(commit 5407557)
> **M23-M28 已落地**(2026-10-19):
> - ✅ M23 OpenAPI 3.0 + Swagger UI 集成(commit 8968a46)
>   - `pkg/swaggerx/`:从 `h.Routes()` + rbac tracking 自动生成 OpenAPI spec;`/swagger/index.html` UI + `/swagger/doc.json` JSON
>   - 覆盖 185+ 受保护路由 + x-rbac-code 扩展字段;`/swagger/*` 绕过 auth/rbac
> - ✅ M24 token 撤销跨实例广播(commit cd6f256)
>   - `pkg/auth/revocache/`:Redis pub/sub `auth:revoke:channel` + 实例本地 cache O(1) 优先;启动期 SCAN 全量加载
>   - `pkg/auth/redis_store.go`:`Revoke` 三步(Redis SET 持久 + 本地 cache + pub/sub 广播);`IsRevoked` 本地 cache 优先
> - ✅ M25 角色 is_active=false 级联停用 identity(commit 3c0c1dd)
>   - `service/role_impl.go:UpdateRole` 同事务内:`sys_role.is_active=false` + `sys_user_identity SET is_active=false WHERE role_code=?`
>   - 重启用不级联(避免自动恢复人为单独关停的);`permnotify.PublishInvalidateAll` 广播
> - ✅ M26 SSE / WebSocket 鉴权 SSEAuth(commit 1ee4b49)
>   - `api/middleware/sseauth.go`:`SSEAuth()` 优先 Authorization → `?access_token=` → cookie
>   - 复用 Auth() JWT 解析 + M24 blacklist 校验;`/auth/perm-stream` 改挂 SSEAuth
> - ✅ M27 Sentry 错误上报(commit 5dbabf7)
>   - `utils/sentry.ts`:错误捕获(ErrorBoundary + SDK 接管的全局捕获)+ opt-in DSN
>   - `stores/user.ts` / `stores/ui.ts` 变化自动 sync context;`beforeSend` 过滤 AbortError(M11 noise)
>   - ⚠️ **M109(2026-10-29)复核**:原写的"三道全局捕获"是**错的** —— SDK 的 globalHandlers
>     已经接管 `window.onerror` / `window.onunhandledrejection`,自己再装一份 = 每个错误上报两次
> - ✅ M28 §2.6 完整性核查 + 补 3 个本地化格式工具(commit a923f9f)
>   - `formatDate.{timeLocalized,weekday,month}`:Intl.DateTimeFormat locale 感知
>   - §2.6 #9 同步标 ✅(M19 已实装 ConfigProvider locale,本批补 format 工具)
> **M29-M33 已落地**(2026-10-19):
> - ✅ M29 通用重试/退避 utility `pkg/retryx/`(commit 61d2c90)
>   - `retryx.Do(ctx, Opts{MaxAttempts,InitialBackoff,MaxBackoff,Multiplier,RetryIf,OnRetry,Logger}, fn)`
>   - full jitter(`uniform(0, backoff)`,AWS 论文推荐,避雷鸣群);context.Canceled / DeadlineExceeded 不重试
>   - 6 单测:首次成功/重试至成功/耗尽/RetryIf 阻断/ctx cancel/默认 predicate
>   - §1.11 #8 标 ✅
> - ✅ M30 前端 axios 自动重试(commit 33b2422)
>   - `utils/retry.ts`:Network Error / ECONNABORTED / 5xx / 408 / 429 自动重试
>   - GET/HEAD 默认 3 次,写操作默认不重试(避免双发);全局开关 `VITE_HTTP_RETRY`
>   - 集成 client.ts 响应拦截器(toast 在重试后);信号透传(用户 abort 立即停)
>   - §2.3 #8 标 ✅
> - ✅ M31 多 baseURL 通道 admin/owner/supplier 运行时切换(commit 783184d)
>   - `api/baseURL.ts`:`Channel` 类型 + `getBaseURL(channel)` + `listChannels()` 扫描 env
>   - RequestOptions 加 `baseURL?` per-request 覆盖;`setDefaultChannel` / `withChannel` 全局切换
>   - env:`VITE_API_BASE_ADMIN/OWNER/SUPPLIER`,未配 fallback `VITE_API_BASE`
>   - §2.3 #11 标 ✅
> - ✅ M32 密钥分级 PII/Secret/Normal + 轮转(commit 8ed5d65)
>   - `pkg/crypto/envelope.go`:v2 格式 `Base64(0x02|keyVer|nonce|ct)`,keyVer 嵌密文本身(无需 DB 列)
>   - `pkg/crypto/registry.go`:Tier{PII,Secret,Normal} + KeysConfig + InitKeys;每个 tier 1 active + N 老 key 仅解
>   - legacy v1 兜底(无 version byte → 走 `KEY_LEGACY` 解)
>   - `main.go` `initCryptoKeys`:env `KEY_<TIER>_V1..V255` + `KEY_<TIER>_ACTIVE` 装载
>   - 6 单测:基本/轮转/分级/未注册/legacy 兜底/未知 version
>   - §1.5 #6 #7 标 ✅
> - ✅ M33 GORM 慢 SQL 报警(commit b948b9d)
>   - `pkg/slowquery/`:包装 `db.Logger.Trace(ctx,begin,fc,err)` 钩子,每条 SQL 必走
>   - 超过 Threshold → 触发 OnSlow + Prometheus 计数 `app_slow_sql_total{table,op,is_error}`
>   - Config:Threshold/OnSlow/LogRawSQL/SampleRate/Logger;Install() 幂等
>   - 4 单测:triggers slow / no trigger under threshold / extractTable / 幂等
>   - 设计选择:用 logger.Trace 而非 callback chain,避开 GORM Statement 克隆的坑
>   - §1.8 #14 标 ✅
> **M34-M56 已落地**(2026-10-19,见 [[session-2026-10-19-batch3..batch6]]):
> - ✅ M34-M36 站内信+通知 / LRU 二级缓存 / KMS 抽象
> - ✅ M37-M39 syncx 并发原语 / cachex Prom 监控 / cachex 启动预热 WarmAll
> - ✅ M40-M50 panic 兜底 / apiver / sign 防重放 / anomalylogin / searchidx 加密搜索 / reqgzip / secheaders / /version / randomx / grayx / ratelimitx
> - ✅ M51-M56 emailx / pushx / OSS 高级(分片+断点续传+扫描+缩略图+防盗链+CDN)/ rsajwt(JWT RS256+轮转)/ SSE UI / TablePro(虚拟滚动+列设置+树形+CSV)
> **M57-M60 已落地**(2026-10-19,本批,session 2026-10-19-batch7):**多身份 + 多部门 P0 完整落地**
> - ✅ M57 `MultiDeptTreeSelect` 组件(`web/src/components/common/MultiDeptTreeSelect.tsx`,commit 8212619)
>   - Tree 选多个部门 + ⭐ 主部门标记(自动取第一项 / 手动切换)
>   - 拉取部门树内置(GET `/system/departments/tree`)+ readOnly 模式
> - ✅ M58 `IdentityEditorDrawer` 组件(`web/src/components/common/IdentityEditorDrawer.tsx`,commit 8212619)
>   - 多身份编辑 Drawer,每行:角色+项目+部门+有效期+主身份
>   - 主身份有且只有 1 条(切换 ⭐ 时其他自动取消)
>   - 角色实时拉 `roleApi.list`,部门走树,新增/删除/复制行
> - ✅ M59 EditModal 加 `multiDept` field type(`web/src/components/common/EditModal.tsx`,commit 8212619)
>   - 业务方 0 接入成本: `{ name: 'multiDept', type: 'multiDept', required: true }` 即可
>   - 桥接 `getValueProps` / `getValueFromEvent` 透传 `{ departmentIds, primaryDepartmentId }`
> - ✅ M60 user 页接入(`web/src/pages/system/user/index.tsx`,commit fef250d)
>   - EditModal 字段 `department_id` 单选 → `multiDept` 多选
>   - ViewDrawer 增 '所属部门' + '身份/角色' 两个字段,以 Tag 列表展示(⭐ 主部门/主身份高亮)
>   - ViewDrawer 右上角 '管理身份' 按钮弹 IdentityEditorDrawer
> - ✅ 后端 P0 bug 修复(commit 8320ebf)
>   - `service/user.go` DTO 加 `IdentityItem{RoleCode, RoleName, ProjectID, DeptID, ValidFrom, ValidTo, IsPrimary}` + `DepartmentIDs []uint64` + `PrimaryDepartmentID`
>   - `service/user_impl.go` Create + Update 真正读 IdentityItem 字段
>   - **修硬编码 P0 bug**: 旧 IdentityIDs 路径硬编码 `RoleCode="role_user_admin"` / `RoleName="后台员工"`,前端传什么角色都被吞
>   - 拆出 `writeUserDepartments` / `writeUserIdentitiesFromItems` helper + `derefUint64` 安全解引用
>   - 业务层软约束 1 user 1 primary identity(沿用 `ensureSinglePrimary`)

> **M61-M63 已落地**(2026-10-19,本批,session 2026-10-19-batch8):**RBAC 框架 UI 完整闭环**
> - ✅ M61 user 页接 resetPassword 真实调用(`web/src/pages/system/user/index.tsx`,commit 9890bf5)
>   - 后端 `auth_impl.go:ResetPassword` 已实装(超管代重置,清 failed_count + locked_until,设 must_change_pwd)
>   - 前端生成 32 字节 hex 模拟 pre-hash(SHA-256),传 `resetPassword(userId, newPwd)`
>   - 真实生产应弹 SMS 二次验证弹窗(框架预留)
> - ✅ M62 audit.RecordRoleSwitch 业务专用细表双写(commit 66bf181)
>   - service.AuditService.RecordRoleSwitch 增接口 + 实现
>   - `audit_impl.go:RecordRoleSwitch` 单事务双写 `biz_role_switch_audit` + `sys_operation_log`
>   - `handler/v1/role_switch.go:writeRoleSwitchAudit` 改用新接口,失败也写 audit(to_role="FAIL:err")
>   - 加 `fmtUint64` 简单 uint64→string helper
> - ✅ M63 batch + 详情接口全套(commit 7ac5a0c + 96aa1f9 + 9890bf5)
>   - **M63a** UserService.BatchRemove(`service/user.go` + `user_impl.go`):
>     - 预校验:不能删自己/不能含 super_admin/全部 ID 必须存在/去重/上限 200
>     - 单事务原子删 user + sys_user_identity + sys_user_department
>     - 写 1 条 sys_operation_log(action=batch-delete, description 列表 ID)
>   - **M63a** UserHandler.BatchRemove 解析 `ids:[]string` → uint64,挂 `rbac(user, delete)`
>   - **M63b/c/d** 单个详情 3 个 handler + 路由(供编辑 Drawer pre-fill):
>     - GET /rapid-admin/v1/system/roles/:id        rbac(role, read)        GetRole
>     - GET /rapid-admin/v1/system/resources/:id    rbac(resource, read)    GetResource
>     - GET /rapid-admin/v1/system/departments/:id  rbac(dept, read)        GetDepartment
>   - **M63b/c/d** seed resourceList 增 3 资源码: `role:read` / `resource:read` / `dept:read`
>     - super_admin 通过 `mergeConcat(resources, menuCodes)` 自动含
>     - user_admin 通过 `filter(resources, "role:")` 自动含 role:read(无需改 bindingRules)
>   - **M63 前端** user 页:
>     - 勾选行 → 工具栏出现 "批量删除 (N)" 按钮 → ConfirmDialog → `userApi.batchRemove`
>     - rowSelection 排除自己(防误删)
>   - **M63 前端** `api/role.ts` 标 3 个 detail API 后端已实装,去掉 TODO 注释
>
> **M64-M66 本批(2026-10-19 batch9)**:前端 UX 必备 3 项(网络监听 + 错误重试 + 降级 UI)
> - ✅ M64 网络监听(`hooks/useOnline.ts` + 顶层 useEffect + 全局 Banner 提示)—— 断网/弱网必备,2 小时
> - ✅ M65 错误自动重试(`api/client.ts` interceptor + 指数退避 + 最多 3 次)—— 4xx 不重试,5xx/网络错误重试,4 小时
> - ✅ M66 降级 UI(`components/common/ErrorBoundary.tsx` + 全局 `<ErrorBoundary>` + `FallbackPlaceholder` 组件)—— 组件级兜底,4 小时

> **M71-M73 已完成(2026-10-22 设计与落地)**:事件推送框架层(SSE 通用通道)—— **M67-M70 的收尾:定时任务状态刷新此前是纯手动的,任务跑完页面不会自己变**
> - ✅ **M71** `pkg/push` 地基(Event/Topic/Redis 单路径 Transport/丢最旧背压+lag 预留位)+ 迁 permnotify 与 inbox 的 Redis 代码 — `5acbd6d`,24 测试,探针 3/3
> - ✅ **M72** `GET /events` SSE 端点(单连接多 topic,服务端按权限定订阅)+ 前端 `useEventStream` + Header 未读角标 — `6b9e216`,7 测试(E1-E7),探针 2/2
> - ✅ **M73** cronx 任务状态接入(依赖倒置:main 注入回调,cronx 不依赖 push)+ 审批/待办事件骨架 — `4189c1d`,10 测试(`TestT9_OnFinish_`×7 + `TestT5_`×3),探针 4/4
>
> 完整设计见 **`事件推送框架层设计方案V1.0.md`**。⚠️ 编号已被本文占用,后续新模块从 **M74** 起。
> **落地记录见该文档 §10** —— 规划与实测的 6 处不符、落地时才查出来的 4 个问题
> (1 个老 bug + 1 处自己写的死代码)、以及探针抓出的一条**测试自己的假绿**
> 都记在那里;**读规划不要跳过 §10**。
>
> **选型是 SSE 不是 WebSocket** —— 本仓 SSE 四层(permnotify / perm-stream / SSEAuth / usePermissionStream)
> 早已齐全,且数据流是单向的、所有上行都走普通 HTTP。详见该文 §2。
>
> ⚠️ **顺带修掉的老 bug(规划时才发现)**:全仓前后端**零 `SetCookie`**,而旧的
> `usePermissionStream` 只用 `withCredentials` 从没带过 token —— 那条 SSE 通道
> **一直在 401 → 3 秒重试 → 再 401 的死循环里**,权限"实时刷新"从来没生效过。
> 新 hook 改走 `?access_token=`(详见 §10)。Cookie 那一级兜底至今仍不可达。
>
> ✅ 站内信**列表弹层 M74 已完成**(见下)。

> **M74 已完成(2026-10-22)**:站内信前端入口 —— 把 M72 留下的哑弹接通
> - ✅ **M74-1** `inbox.List` 改 typed DTO(`MessageView`)—— `7eb7a94`
> - ✅ **M74-2** 铃铛 Popover 列表弹层 + Header 接线 + types/inbox.ts + i18n —— `098d986`
>
> **落地时挖出来的三个缺陷(M34 的接口此前一个前端调用方都没有,所以全都没暴露)**:
> `List` 返回的是 `[]map[string]any`,那是整个 `pkg/` 树里唯一一处这种写法,实测出:
> 1. **wire 上的 ID 是裸数字** —— 模型上的 `json:"id,string"` 被 map 整个绕开。
>    Notify 当时不写 id、靠 DB 自增所以看不出来(⚠️ **那个自增在 MySQL 上根本
>    不存在** —— 见下面 2026-10-24 的 id 修复),但模型声明了 `autoIncrement:false`
>    (ID 该由雪花生成),任何一次走 model 结构体的插入都会写出 19 位 ID,前端
>    JSON.parse 静默截断、回传后**标到错误的那一条**。这也解释了 `pkg/jsonid` 的
>    棘轮为什么一直没报警:它扫 struct tag,map 上没有 tag 可扫。
> 2. **同一列两种时间格式** —— map 直出 driver 原始值,GORM 写的是本地时间带纳秒,
>    别的写入路径给的是 UTC 秒级,同一响应里并存。
> 3. **NULL 与空串混用** —— "没有值"一半 null 一半 ""/0,前端每个字段都得判两遍。
>
> 顺带发现**测试夹具与生产不一致**:夹具的建表语句没有 `deleted_at` 列,而 List
> 返 map 的时候 GORM 的软删除作用域本来就不生效 —— 夹具的失真一直被这个 bug 盖着。
> 改成 typed 扫描后作用域生效,夹具补列才通过。**夹具保真度本身就是被测对象**。
>
> ⚠️ **仍然没解决的(M75+,业务侧)**:**业务模块**仍零调用方 ——
> `Registry.InboxNotifier` 注入了但没人用,service/asset.go:27 的
> `notify_inbox` 是孤字段。**哪个业务模块在什么时机投递**是业务侧的活,
> 不属框架层,所以框架这边交的是"能收能读能标已读"。
> - 同属 M75+: `Manager.Delete`(有实现未挂路由)、`sys_inbox_user_prefs` 偏好表(建了表零功能)
>
> 🔴 **2026-10-24 更正:上面"至今没有数据来源"这句是错的**(写的时候只查了
> `Registry.InboxNotifier` 一个入口,漏了另一条)。**框架自己就有一条数据来源**:
> M88 的审批超时巡检(`internal/service/approval_scan.go` →
> `RegisterApprovalScanJob(db, inbox.New(db))`,`main.go:316` 装配),
> 给「审批人全部离职」「单子停留超 7 天」两种情况发站内信。
> ⚠️ **但那条路径当时也是断的** —— 见下面 2026-10-24 的 id 修复。
>
> 🔴 **2026-10-24 修的真 bug:站内信 id 没生成,MySQL 上整条投递路径是断的**
>
> `Manager.Notify` / `NotifyBatch` 用 `map[string]any` 插入,**不写 `id`**。
> 而 `model.InboxMessage` 继承 `BaseModel`,`BaseModel.ID` 声明的是
> `primaryKey;autoIncrement:false` —— **全仓的表都是应用侧雪花 ID**。
> 用 MySQL 的 DDL 措辞实测(`gorm.io/driver/mysql` 的 `FullDataTypeOf`):
> 这一列是 `bigint unsigned`,**没有 AUTO_INCREMENT**。于是:
>
> | 环境 | 结果 |
> |---|---|
> | MySQL 严格模式(默认) | INSERT 报 **1364「Field 'id' doesn't have a default value」** —— **站内信一条都投不出去** |
> | MySQL 非严格模式 | 全部写成 **id=0** —— 而 `MarkRead(userID, 0)` 会把**这个人的所有消息**一起标成已读 |
> | SQLite | `INTEGER PRIMARY KEY` 是 rowid 别名,驱动自动填 1/2/3 —— **看不出任何异常** |
>
> ⚠️ **为什么单测一直是绿的**:夹具的建表语句写的是
> `id INTEGER PRIMARY KEY AUTOINCREMENT` —— SQLite 两处自动填值叠在一起,
> 把 bug 完美地盖住了。**夹具保真度本身就是被测对象**(上面那句 2026-10-22
> 刚踩过一次)。现在夹具改成跟生产一致(`id BIGINT NOT NULL` + `PRIMARY KEY (id)`
> —— SQLite 只在列类型**恰好是 INTEGER** 时才把 PK 当 rowid 别名),
> 于是"漏填 id"在夹具里也会炸,**与生产同一种病**。
>
> **修法**:id 改由应用侧生成(`newRow` 里 `sonyflake.Next()`),`Notify` 与
> `NotifyBatch` **共用同一个 newRow** —— 老 bug 的形状正是"两份 map 抄岔了"。
> 探针实测:把 id 改回 `0` → `UNIQUE constraint failed` 变红;整条键删掉 →
> `NOT NULL constraint failed` 变红(**就是 MySQL 1364 在夹具里的同一形状**)。
>
> ✅ **2026-10-24 M3:审批通知 → 站内信打通**(结项之后补的框架能力)
>
> 此前站内信**只有一条框架侧数据来源**(M88 的超时巡检)—— 审批的**动作本身**
> (新待办、通过、驳回、打回)**一条消息都不发**:审批人不知道自己多了一条待办,
> 发起人不知道单子批没批下来。
>
> **接法是装饰器,不是改引擎**:`NewInboxApprovalPublisher(inner, notifier, db)`
> 包住原来的 publisher —— 事件照发(Redis 通道完全不受影响),顺带投一条站内信。
> **引擎对站内信一无所知**(它只认 `ApprovalEvent`)。
>
> **收件人规则**:`assigned` → 审批人;`approved`(整单通过)/ `rejected` /
> `rolled_back` → 发起人;`cancelled` → 不投(发起人自己做的,不通知自己)。
>
> 🔴 **踩到的一个真歧义**:`approved` 有**两种语境** —— 会签里"这一票记下了"
> 与整单通过**用的是同一个 action**(§9.2 那张表)。给发起人发消息**必须**分得出
> 这两种,否则会签到一半就报"审批已通过"(**假消息**,比不报更糟)。
> **解法是引擎给事件加一个 `Result bool`**(⚠️ **不广播**,只活在引擎 → publisher
> 的进程内接口上,wire 契约一个字没变):终局/驳回/打回 = true,
> 新待办/会签中间票/作废 = false。
> **旁路读实例 `status` 猜是有竞态的** —— 会签的中间票与最后一票前后脚投递,
> 谁先被读到是随机的(要么漏发要么重发)。**这件事不能靠猜。**
>
> **顺带放开权限**:`inbox:list`/`inbox:read`/`inbox:mark_read` 此前**只有超管有**
> —— 站内信是审批通知唯一的**持久化**落地处,普通账号压根看不到自己的收件箱。
> 现在进了 `authBase`(12 个非超管角色全给);**安全**是因为 handler 把每一条
> 查询都按 `caller.UserID` 收窄("能调 `inbox:list`"只等于"能看**自己**的收件箱")。
>
> 详见 `docs/通用审批系统设计方案V1.0.md` §13 的 M3 条目(9 条可逆探针全红)。

> ✅ **2026-10-25 M4a:通知通道接线(邮件 + 推送)** —— 把 §1.9 #9 #10 的"✅ P2"变成真的
>
> **接线的动机不是"想做邮件",是那份清单自己写着**:emailx 15 个单测、pushx 12 个单测,
> **零生产调用方** —— "有 12 个测试却没接线,别当成'推送能力已通'"。
> 文档打 ✅ 的是"代码完备",不是"能力已通",这两件事此前被混成一件事。
>
> **接缝在 `inbox.Notifier` 这个接口上** —— `NewInboxApprovalPublisher(inner, notifier, db)`
> 收的是接口,所以塞一个 Fanout 进去就行,**审批引擎一行都不用改**。装配前
> `registry.InboxNotifier = inboxMgr`,装配后 `= notifyx.Fanout{inner: inboxMgr, ...}`。
> **所有既有的站内信投递自动多一条通道**,未来的也一样 —— 这是接口当接缝的好处。
>
> - ✅ `pkg/notifyx/`:`Dispatcher`(多通道分发,串行 + 每通道 recover)/
>   `Channel`(通道接口)/ `EmailChannel`/`Resolver`(`DBResolver` 查 sys_user)/
>   `Fanout`(站内信 + 额外交付)—— 22 单测 + **7 条可逆探针全红**
> - ✅ `config.EmailConfig` / `config.PushConfig` + `Server` 结构体挂载 + 两份 yaml 模板
> - ✅ `main.go` 装配:`buildEmailSender` / `buildPushSender` / `buildNotifierDispatcher`
> - ✅ `Registry.EmailSender` / `Registry.PushSender`(可为 nil,业务侧要能降级)
>
> **三条设计取舍(都写进了代码注释)**:
> 1. **未知 provider 不回落 console** —— 与 `buildSmsSender` **故意不同**。短信回落
>    console 是开发期便利;邮件回落 console 会让运维在日志里看到"邮件发出去了",
>    而它根本没发。**宁可不发,不要假装发了。**
> 2. **没有邮箱 = skipped,不是 failed** —— 后台账号大量不填邮箱,那是常态;
>    记成 failed 会让"投递成功率"这个指标永远掉。
> 3. **收件人邮箱做格式校验** —— `model.SysUser.Email` 现在是明文,但模型和
>    `user_impl.go` 的注释都写明**将来要加密**。不校验的话,等 PII 加密落地那天,
>    平台会"把 Base64 密文当收件地址发邮件" —— 邮件发到一个不存在的地址,日志里
>    看不出任何异常。**判断"没有"比判断"有"安全。**
>
> ⚠️ **推送故意停在半路** —— sender 装好了、`Registry.PushSender` 挂上了,
> **但没有注册成 `notifyx` 通道**:全仓没有设备 token 表,推送没有投递目标。
> 一条"永远跳过"的通道在监控上看着像通了,**比没接更糟**。补齐需要设备 token 表
> + 迁移 + 采集端点 + RBAC 码,属独立一轮。
>
> 🔴 **顺带修掉一个真 bug**:`pkg/pushx` 的 `ConsoleSender.Out` 是个**空的
> `bytes.Buffer`**,输出全被吞 —— 而老测试只读 `Output()`(读的是内部留档),
> 所以一直绿。**测试读的是另一个副本,不是真实输出** —— 典型的假信心。
>
> 🔴 **探针抓出一条测试自己的假绿**:`TestEmailChannel_EscapesHTML` 原本把注入载荷
> 放在**标题**里,断言 `<script>` 不出现 —— 可标题和正文是**两条独立的转义路径**,
> 把正文的转义去掉测试照样绿。已改成载荷放正文 + 标题另立一条用例,重跑探针转红。

> **M67-M70 已完成(2026-10-21 设计 → 2026-10-22 落地)**:定时任务调度框架层 —— **本清单此前完全没有 cron 条目,属文档盲区**
> - ✅ **M67** cronx 核心(robfig/cron/v3 + 两层去重 + 执行历史 + 优雅停机 + 崩溃行回收)— `160c30b`,45 测试,探针 7/7
> - ✅ **M68** 后端 API(**7 路由** + rbac + seed)— `f36eab7`,26 测试,探针 2/2
> - ✅ **M69** 后端前端(定时任务页 + 执行记录页 + 权限 + 路由 + i18n)— `2251d7f`,tsc 零新增 + vite build 通过
> - ✅ **M70** 内置框架任务 ×2(执行记录清理 + KMS 轮转检查)— `79948ab`,20 测试 + 修 M67 的 purge 软删缺陷
> - ✅ **M120** cron 表达式翻译成人话(列表列 + `CronInput` 实时提示;**认不出的形态闭嘴不猜**,不显示任何描述)— 2026-10-30,64 测试 + 探针 10/10
>   —— 补做 `定时任务调度框架层设计方案V1.0.md` §6.4 规划了但 M69 **没做**的那件事(列表里那列一直是裸表达式)。
>   实现是**真解析器**而不是规划里写的预设映射表(预设表认不出运营真会写的 `7,17,27,37,47,57 * * * *`)。
>   详见该文档 §6.4「落地情况」与 §11.6。
>
> 完整设计见 **`定时任务调度框架层设计方案V1.0.md`**。⚠️ 编号已被本文占用,后续新模块从 **M71** 起。
> **落地记录见该文档 §11** —— 规划与实测的 6 处不符、12 个被测试/探针逼出来的真缺陷、
> 以及 3 条已知盲区都记在那里;**读规划不要跳过 §11**。

---

> **M76-M82 已落地(2026-10-22)**:文件系统模块 —— **本地磁盘 + 阿里云 OSS 双后端**
>
> **实施前的现状不是「从零开始」,是「写完了但没人用过」**:`internal/pkg/oss` 的 `Provider` 抽象、分片接口
> (`MultipartProvider` 5 方法)**有真实实现不是 stub**,`aliyun.Provider` 完整 —— 但
> `grep -rn "internal/pkg/oss"`(排除包自身)**零命中**,`cfg.OSS` 零引用,`SafeKey` 零调用方,
> 且**没有 `LocalProvider`**。`upload_impl.go` 全文占位(硬编码 host + `Signature="PLACEHOLDER"`)。
>
> - ✅ **M76** 路径与校验(`SafeKey` 提到 `oss` 包顶层 + 路径构造器 + 文件名清洗 + `ObjectMeta`/`ListResult`)— `f53b59d`
> - ✅ **M77** `LocalProvider`(`Put`/`Stat`/`List`/`DeletePrefix` + **三层目录穿越防御** + 原子写 + local 分片模拟)— `31a7f18`
> - ✅ **M78** 接口扩展(`Provider` 3 → 8 方法)+ `SignUploadToken` 改 key 语义 + 修 2 个 OSS 缺陷— `43d2409`
> - ✅ **M79** 数据层(`sys_folder` / `sys_file` / `sys_upload_session` 3 表 + `sys_user` 加 2 列)— `5a74565`
> - ✅ **M80** 服务与路由(**16 条路由** + RBAC 四处同步 + `upload:*` 并入 `file:*`)+ **M81** 配置与接线(`file.*` 配置 + local/oss 选型 + 静态挂载)— `88e34b8`
> - ✅ **M82** 前端(文件管理页:文件列表 + 文件夹管理 + 上传进度)— `9c1aa7f`
>
> 完整设计见 **`文件系统模块设计方案V1.0.md`**。⚠️ 编号已被本文占用,后续新模块从 **M83** 起。
> **落地记录见该文档 §15** —— 规划与实测的 3 处不符、实施中抓到的 2 个 P0
> (直传登记**从来没扣过配额** / `Gzip` 中间件把流式响应**整份读进内存**)、以及 §14 那 3 个问题的答案
> 都记在那里;**读规划不要跳过 §15**。
> **结项复审见该文档 §16** —— 交付后又过了一遍前后端,查出 **9 个"错了也不报错"的缺陷**
> (分片合并永远失败 / 配额凭空被吃 / 删文件夹留下公开孤儿对象 / 查历史恒返回空 /
> 超长文件名永远传不上去 / 建文件夹错误映射过宽 / `allowed_biz` 是死配置 /
> 中转下载没有 `Range` 导致 iOS 不播视频 / 分片合并成功但记账失败留下孤儿对象),
> 全部已修并有回归测试;同时留档了 **3 项核实后撤回的"疑似缺陷"** 的理由,
> 免得下一个人再去"修"一遍。
> 规划期自查出的 3 个既有缺陷**全部已修**:BodyLimit 20MB 从未生效 → `dd5084f`(漏调 `c.Abort()`,
> 中间件**一次都没真正拦下过请求**)/ 防盗链 HMAC 用了 AccessKeyID 当密钥 → `43d2409` /
> `pickBucket` 认的公开前缀与新路径不匹配 → `43d2409`。
> ⚠️ **§14 的 3 个问题已按设计取舍落地**(不嵌套 / 封面是文件级 / `upload:*` 并入 `file:*`),
> 答案与依据见文档 §14 —— 其中「文件夹不嵌套」是**实施期间的默认取舍**,不是业务签字结论。
> 🔄 **2026-10-30 M125 已改**:文件夹**支持多级嵌套**了(而且**不需要全量迁移**,
> 因为路径恒 5 段、`{folderId}` 本来就是叶子文件夹的 id)。见下面 M125 落地块
> 与 `文件系统模块设计方案V1.0.md` §2.7 / §18。
>
> 需求来源(均为既有文档中已写、只是没人做):明源增量同步 / KMS 轮转扫描 / 数据保留清理 /
> **grant 到期自动转 expired**(权限委托的自认缺口)/ 内置探测任务。
>
> ⚠️ **框架已就绪但业务任务一个都没写**(约定前缀 `biz.*`)—— 这三个需求
> (明源增量同步 / grant 到期转 expired / 其它探测)仍待业务侧实现。

---

> **M83-M88 已规划(2026-10-22),尚未实施**:通用审批系统(框架层)
>
> **需求**:管理员可配置多级审批链、逐级指定审批人;员工对业务记录发起后,
> 审批人可**通过 / 拒绝 / 打回**,通过与拒绝均可带备注。要通用、不要复杂。
>
> **立项依据不是"想要一个审批系统",是三件已经存在的事**:
> 1. **每个带审核的业务表都各自写死状态整数** —— `biz_digital_material`(`asset.go:51`)/
>    `biz_issue`(`delivery.go:89`)/ `biz_inspection`(`delivery.go:70`)/ `biz_house`(`house.go:56`)/
>    `biz_user`(`owner.go:23`)/ `biz_house_binding`(`owner.go:46`),**没有一处共用抽象**;
>    连枚举风格都三套并存(`type XxxStatus int` / `string` / **裸 int + 行尾注释**)。
> 2. **已有的状态流转全是 check-then-act** —— `asset_impl.go:439` 的 `Where` **只有 id,
>    没有 `AND status = 1`**。资料审核场景是低频,**审批场景(同一关两个人一起看)是必然双写**。
> 3. **审批事件契约早被 M73 定死了却一直没有生产者** —— `push/event.go:57-59,111-112,123-133`
>    的 `TopicApproval` / `EventApprovalTodo` / `EventApprovalDone` / `ApprovalPayload`,
>    以及 `handler/v1/event.go:148` 的 `CanAccess(..., "approval", "read", ...)` 门禁,
>    `push/event.go:102-110` 明写"**目前没有任何发布端**"。加了 `approval:read` 资源码自动生效。
>
> **选型结论:零新增依赖,自研。** 2026-10-22 调研了 Java 系(Flowable / Camunda / Activiti /
> jBPM)、国内系(FlowLong / EasyFlow / **LiteFlow —— 它不是审批引擎**,官方对比里明说
> "不适合 OA 审批类场景")、Go 系(gflow-engine / jeeflow-go / weedbox / yggdrasil 等 10 个)
> 与编排系(Temporal / Cadence / Airflow)。**决定性理由三条**:
> ① **审批人解析是本仓独有价值** —— 三家 Go 引擎都留了 `IdentityService` SPI 洞
> (组织架构归宿主),而本仓**已经有**部门树 + 多身份 + 委托授权,接进来省下的是
> **一整套身份设施的重复实现**,正是 `rbac.go:164-166` 明令禁止的"两套实现分叉";
> ② 核心逻辑(状态机 + 会签或签 + 打回 + 解析)**不到 800 行**,能逐行 review 完,
> 换 2026 年新出的零验证外部引擎风险大于收益;③ 依赖与方言(`floxy` 仅 PostgreSQL,
> 本仓 MySQL + SQLite 双方言)。
>
> - ⬜ **M83** 数据层(**7 张** `sys_approval_*` 表 + 枚举)。**第一个动作是改事件推送文档:477 的边界声明**
>   (见下);既有枚举债(`MaterialStatus` 缺 5/6、`delivery.go:11` 无注释)**单独一个 `fix(model):` commit,不混进来**
> - ⬜ **M84** 引擎内核(状态机 + **6 动作** + **实例行 `SELECT ... FOR UPDATE` + 30s 锁超时**
>   + 会签或签 + **`stale` 表达轮次** + **轮次快照 + 重提必须改内容守卫(V2.1)**)。**15 条必测用例见方案 §10.1**
> - ⬜ **M85** 审批人解析(`ApproverResolver` SPI + **3 种**内置 + 自审拦截 + **error≠空集** + 环检测)
> - ⬜ **M86** API 层(**12 端点**,含 `PUT /approvals/:id/draft` + `BizHandler` SPI + 可见性 + **RBAC 六处同步**)
>   **含把 `biz_digital_material` 的审核从 `asset_impl.go` 切到新引擎**(否则那个 check-then-act 还在)
> - ⬜ **M87** 前端(流程配置**表格非拖拽** + **一个页面三个盒子**(2026-10-26 已从"三个 Tab"改成"搜索栏里一个必选下拉",见《通用审批系统设计方案V1.0.md》§13 的 M90)+ 时间线详情 + **轮次切换 + `returned` 态内联编辑** + 待办角标)
> - ⬜ **M88** 收尾(`push:approval` 生产者(**必须 `context.WithoutCancel`**)+ inbox 通知 + 超时巡检)
>
> 完整设计见 **`通用审批系统设计方案V2.1`**(文件 `通用审批系统设计方案V1.0.md`)。
> ⚠️ 编号已被本文占用,后续新模块从 **M89** 起。
>
> ⚠️ **本文推翻了 `事件推送框架层设计方案V1.0.md:477` 的"审批模块本体属业务模块,不属框架层"**
> —— 审批引擎本体是横向能力(交付 / 资料 / 业主 / 资产四个域都要用),
> 且**正因为它不认识业务表**(只存 `biz_type` + `biz_id`),才可能是框架层而不是某个业务的附属品。
> **该行的修改排在 M83 的第一个 commit,不是 M88** —— 排在最后意味着整个开发期读那份文档的人
> 都读到错的边界声明。
>
> **⚠️ 方案已定稿为 V2.1(2026-10-23),V1.0 / V1.1 全部作废。**
>
> **四个版本走过的路**:
> - **V1.0**:从通用领域知识推出来的,没参照任何实现 → 评审查出 **6 致命 + 12 严重**缺陷,
>   **其中两个是我自己犯了方案 §0.2 批评 `asset_impl.go` 的同一个并发 bug**
> - **V1.1**:扒 5 个开源引擎的源码纠错 → 调研结论有 **4 条被源码推翻**,范围越滚越大
> - **V2.0**:**砍范围**。V1.1 里有七样东西是调研时自己加的、需求从没要求过,全部砍掉
> - **V2.1**(当前):**补轮次留痕**。用户提出"打回后改内容重走 + 能翻多轮历史",加 `sys_approval_round`
>
> **V2.0 相对 V1.1 砍掉的**(每条在方案 §1 记了加回条件):模板版本机制 / 转办 / 催办 /
> 撤回 / 打回到指定关卡 / 日志轮次分界(`seq`) / 19 个动作级权限码 / `active_key` / 会签 5 种规则 /
> 统计端点 / `terminated` 状态 / 小程序端 / 意见附件 / 3 个菜单(6→2)。
> **规模:7 表 / 6 动作 / 3 种解析器 / 12 端点 / 11 权限码 / 3 页面。**
>
> **三个核心设计**:
> **① 轮次靠 `stale` 表达,不要日志分界** —— 打回时把本实例所有 task 置 `stale`,
> 分母的 `status IN (...)` 自然排除上一轮。**没有旁路写日志的可能**,分界不可能算错;
> 连带把 `log.seq` 这个曾专门核实过 MySQL 兼容性的列整个删掉。
> **② 实例行悲观锁 + 锁内重算,不维护会签计数器** —— 有开源项目(go-workflow)实证:
> 读计数 → `--` → 整行写回,漏任何一环就**静默丢票且不可自愈**。
> **③ `BizHandler` 回调同事务,失败整体回滚** —— 因为 outbox 明确不在 V1 范围内,
> "不回滚"就没有自愈路径;V1 的回调只是一次单字段 UPDATE,回滚自洽。
>
> ⚠️ **V2.1 没动上面任何一条核心设计。** `sys_approval_round` 是**纯展示表**:判定分母照旧只看
> `task.status`,`stale` 一字未改。**一个"当前轮次"的权威答案只能有一个** —— 判定也去读 `round_no`,
> 写错它就从"历史显示错"升级成"审批算错票"。
>
> **V2.1 顺带解掉的 D9 边角**:草稿阶段可以直接改 `template_code`(`PUT /approvals/:id/draft`),
> 阶段是 `submit` 时才读的、不在建草稿时快照,所以改字段即可。**"建草稿时选错模板"不再需要
> `cancel` 再重来**,最高频的那条卡点消失,而 D9 的唯一索引一个字没动。
>
> **9 个待拍板问题已全部定案**(方案 §14):Q1 两步草稿 / Q2 拒绝备注必填 ≥5 字 /
> Q3 打回回发起人 / Q9 会签意见互相可见 / **Q10 打回后可改内容 + 多轮历史** = **用户拍板**;
> Q4 `BizHandler` 失败语义 / Q5 模板软删改写 / Q6 重复发起硬拦 / Q7 `BizHandler` 谁写 /
> Q8 小程序端 / **Q11 重提强制改内容** / **Q12 轮次不改判定** / **Q13 `update` 不记 log** =
> **产品侧已定,不再挂起**。
> ⚠️ **没有遗留的结构性空洞,可以直接开工。**

> **M5b / M5c / M5d / M6 / M7 已落地(2026-10-25,本批)**:表格交互补完 —— **行选择 + 批量操作** + **分页参数契约统一** + **用户部门契约** + **请求生命周期** + **列表错误态与重试**
>
> - ✅ **M5b 行选择 + 批量操作**(§2.8 #14 关闭)
>   - `hooks/useTableSelection.ts`(226 行):行选择状态机 + **跨页选择不被 antd 吃掉**
>   - `components/common/BatchActionBar.tsx`(69 行):"已选 N 项" + 业务动作 + "取消选择"
>   - **为什么必须自己写一层**:antd 的 `useSelection.setSelectedKeys()` 会把
>     **"不在当前 dataSource 里的 key"** 过滤掉
>     (`node_modules/antd/es/table/hooks/useSelection.js:168-181`),
>     除非 `preserveSelectedRowKeys = true`。后台表格全是**后端分页**(每页 20 条)→
>     "第 1 页勾 3 条 → 翻到第 2 页 → 点表头全选"会把第 1 页那 3 条**静默丢掉**,
>     而用户以为"全选"= 全部。hook 默认开这个开关。
>   - **antd 的 `onChange` 第 2 个参数 `records` 跨页时不可信**(是 `undefined`)→
>     hook 自己维护 key→行 的缓存,`selectedRows` 跨页也拿得到行对象
>   - ⚠️ **缓存必须在渲染期填,不能在 effect 里填**:effect 里填的话首屏
>     `selectedRows` 算成空数组,而且因为 `selectedRowKeys` / `data` 都没变,
>     memo 不会重算 → **`defaultSelectedRowKeys` 选中的行一直是空的**(假绿)。
>     探针实测:去掉渲染期填缓存 → `selectAll` / `defaultSelectedRowKeys` 两条用例变红。
>   - 业务页接入:`useTableSelection` + `BatchActionBar` 两行,权限仍然归
>     `AuthButton`(组件本身不感知权限,跟 `RowActions` 一个分工)
>   - **用户管理页已迁移**(原来是自己写的 `selectedIds` + 手搓 `rowSelection`)
>   - 测试 17 条(vitest 12 文件 / 96 用例全绿),探针 3/3 变红
>
> - ✅ **M5c 分页参数契约统一**(§2.3 #12 关闭)
>   - 后端只认 `page` + `size`(`BaseHandler.GetPage`,`server/internal/api/handler/handler.go:86`),
>     `page_size` 在 `server/` 下**零命中**;而前端三方各写各的:
>     `useTable` 内部 `pageSize` / `PageRequest` 声明 `page_size` / 后端 wire 名 `size`
>   - **后果:全仓 useTable 页面的"每页条数"选择器都是失效的**(选了 50 条,后端按默认 20 返回;
>     页码是对的,因为 `page` 三方一致)
>   - 改法:在 `api/client.ts` 出口统一翻译(`normalizeQueryParams`),
>     `page_size` / `pageSize` 一律翻成 `size` 并从 query 里剔除;同时把 `useTable`
>     内部字段换成后端 wire 名 `size`(旧写法仍能跑对,直接调 API 的老代码不用改)
>   - **顺带修好 5 处"一次拉全"的调用**(之前实际只拿到 20 条)
>   - tsc 59 → 56(净减 3,零新增);探针 4/4 变红
>
> - ✅ **M5d 用户部门契约补全**(§2.10 #15 关闭)
>   - 后端 `GET /system/users/:id` 现在返 `departments[]`(全部部门 + `is_primary` 主部门标记,
>     主部门排最前);`Get` 返回类型 `*model.SysUser` → `*service.UserDetail`
>     (**嵌入** SysUser —— JSON 形状不变,只多一个字段;抄一份字段的话 model 一改就漂)
>   - ⚠️ **没有给 model.SysUser 挂 many2many**:join 表 `sys_user_department` 不是
>     GORM 自动管的(多两列 `is_primary` / `is_leader` + 手写迁移),
>     挂上去 AutoMigrate 会跟手写迁移打架 → 改成 service 层一条 JOIN 查完
>   - 前端编辑弹窗的"部门"从**只读**改回可编辑(只读的理由就是"详情不返全部部门,
>     发了会把次要部门洗掉");回填用详情里的全部部门,
>     发出去的 `department_ids` 是**改完之后完整的全集**,没改一个字段都不发
>   - **顺带修了一个同源的洞**:`UserListItem` 里 `department_id` / `department_name`
>     **从没人赋值** → 前端"部门"列恒为空(字段在 DTO 里,就是没人给值)。
>     现在按主部门填上(一条 SQL 取本页所有用户的主部门,不是 N+1)
>   - 顺带修了 `MultiDeptTreeSelect` 的一个告警:树是异步拉的,首帧 treeData 还是空的,
>     这时候把 value 传给 TreeSelect → rc-tree 打 "Tree missing follow keys";
>     改成"树没到位就不渲染选择器"
>   - 测试:后端 3 条(service 层真 sqlite)+ 前端 7 条(userForm 纯逻辑 7 / 页面级 3);
>     探针 2/2(后端)变红
>
> - ✅ **M6 前端请求生命周期**(§2.3 #13 关闭)
>   - **列表请求的竞态**:`useTable` 以前是"谁回来谁写 state"—— 两个请求同时在飞时
>     **先发的不一定先回**(慢接口 + 用户狂点筛选 = 生产必然出现),
>     慢的那个晚回来就把新数据覆盖成旧数据 → 页面显示的是**上一次筛选**的结果,
>     而筛选框写的是新的(用户会以为是后端算错了)
>   - 修法:**请求序号**(`seqRef`),每次发起请求领一个自增号,
>     只有最后一次的号才有资格写 state;过期响应直接丢掉
>   - 顺带治好了 loading 的同类问题:旧请求的 `finally` 不能再把新请求的转圈关掉
>     (否则转圈提前消失、数据还没到)
>   - **失败的请求以前是 unhandled rejection**(`useEffect` 里 `load(query)` 没有 catch)
>     → 现在进 `error` 状态交给页面;**已有数据不清空**(一次抖动把整页洗白不行)
>   - **取消的请求以前会弹红色 toast**:axios 的 `CanceledError` 没有 response,
>     一路落到错误拦截器最后的 else 分支 → `message.error("canceled")`
>     → 现在 `isCanceled()` 前置拦截、静默 reject(错误对象带 `canceled: true`)
>   - **`useRequest` 的 AbortController 曾是假的**:建了 controller 也调了 `abort()`,
>     但 **signal 从没传给 service** → abort 只让 hook 忽略结果,**网络请求照跑到底**;
>     而 `RequestOptions.signal` 的注释写着"已自动集成"—— 文档与实现不符。
>     现在 signal 作为 **service 的最后一个参数** 传出来(axios 的惯例);
>     service 转给 axios 才是**真取消**(浏览器直接断连接)
>   - 测试:前端 11 条(useTable +4 / useRequest 新文件 5 / client +2);
>     探针 10/10 变红(`git stash` 只抽实现、保留用例)
>   - 验证:web 13 文件 / 121 测试全绿(改动前 12 / 110);tsc 56 = 基线零新增;
>     eslint 3 个 `consistent-type-imports` 全是改动前就有的(都在没动过的 import 行)
>
> - ✅ **M7 列表错误态 + 重试 + 断网恢复自动重载**(§2.5 #6 / #7 / #8 关闭)
>   - **M6 把错误记下来了,但没人看等于没有**:全仓 17 个管理页写的都是
>     `<ManagementTable tableProps={tableProps} />`,错误信息若放在 `useTable` 返回值的
>     **另一个字段**上,就得改 17 个页面才能透传 → 现在错误与重试回调随 `tableProps`
>     一起交给外壳(`TableErrorChannel`),**业务页零改动**
>   - 两种形态按"表里还有没有旧数据"分:
>     **没数据**(首屏就失败)→ 表格 empty 区换成降级占位(加载失败 + 重试按钮);
>     **有旧数据**(刷新失败)→ 表格上方一条提示条,**旧数据继续留着**(洗白比不新更糟)
>   - ⚠️ `error` / `onRetry` **不是 Antd 的 prop**,外壳展开给 `<Table>` 前先摘掉
>   - ⚠️ **不把 `<Table>` 摘掉**:摘掉会让 auto-height 那段测量拿到 0,
>     `scroll.y` 变成 0 → 数据回来时表格高度是 0 → **行看不见**(比空表格还糟)
>   - **断网恢复没有自动重载**:`OfflineBanner` 只负责"提示",它自己的注释写着
>     "网络恢复时调 useTable 的 reload 是组件外的事"—— 而**没有任何组件**去接这个
>     "组件外的事"(断网时列表失败,网络恢复后页面还是空的,用户只能自己刷新浏览器)。
>     现在 `useTable` 订阅 `useOnline` 的 `onReconnect`,**只在"因为失败而空着"时**
>     自动重载一次(判据用 ref 不用 state —— `online` 事件是同步回调,
>     那时 React 还没重新渲染,闭包里读到的 state 是旧的)
>   - **`useOnline` 的回调身份导致监听反复重绑**:effect 依赖是
>     `[onReconnect, onDisconnect]`,而调用方几乎都写 `useOnline({ onReconnect: () => ... })`
>     —— 每次 render 都是新函数 → 依赖每渲染都变 → 监听被反复 remove/add。
>     看着只是白做功,实际有窗口期:`online` 事件正好落在这个缝里就**漏掉了**
>     —— 而"网络恢复后自动重载"恰恰依赖这个事件。改成回调存 ref、只订阅一次
>   - 顺带:`FallbackPlaceholder` 此前**零调用方**(死码),现在是降级占位的实现体;
>     它的文案此前写死中文,现在走 i18n(新增 `common.loadFailed` / `common.retry`)
>   - 测试:前端 9 条(useOnline 新文件 3 / useTable +2 / ManagementTable +4);
>     探针 4/4 变红(`git stash` 只抽实现、保留用例):监听重绑次数(3 ≠ 1)、
>     断网恢复不重载、空态无"加载失败"、旧数据页无提示条
>   - 验证:web 14 文件 / 130 测试全绿(改动前 13 / 121);tsc 56 → **55**(净减 1:
>     `ManagementTable` 的 `ColumnsType` 此前从 `antd` 根取 → TS2305,
>     按仓库约定改成 `antd/es/table`);eslint 改动文件 **0 error**(只剩 4 个改动前就有的
>     `no-explicit-any` warning)
>
> - ✅ **M8 跨标签页会话同步 + 刷新失败不误踢**(§1.2 缺口 E 关闭)
>   - **病根是"单次使用"的 refresh token**:后端轮换(用一次就吊销、换一张新的)是对的,
>     但浏览器里**同一个会话可以同时存在多份 refresh 副本**(每个标签页一份),
>     副本之间零同步 → A 拿 R 刷成功(服务端吊销 R、发出 R2)、B 同时拿 R 刷 → 被拒
>     → 旧行为直接 `handleUnauthorized()` → **B 被踢回登录页**。
>     用户看到的是「我开了两个标签页,有一个莫名其妙被登出了」
>   - **两种存储后端,两种疼法**(这是设计的关键):
>     · 勾了「记住我」(localStorage):两个 tab **共享**同一份 R → B 失败后
>       **重新读一次存储**就拿到 A 写进去的 R2,**不用发任何请求**
>     · 没勾(sessionStorage):sessionStorage **按标签页隔离**,重读也没用
>       → 只有**跨标签页广播**能把 R2 送到 B 手里
>   - **新增 `utils/tokenSync.ts`(纯协议,不碰 store)**:两种消息
>     (`tokens` 广播我这一组 / `need` 求助),四道采用闸门
>     —— **同一个人**(防会话串号)、**access token 还有效**、
>     **不是刚失败的那个**(已被消费)、**不是本地的那个**(防乒乓)
>   - **为什么用 BroadcastChannel 而不是 localStorage 当信使**:它**不落盘** ——
>     拿 localStorage 当信使就得把 refresh token 写进磁盘,等于**把「不记住我」
>     这个选择废掉**;而且它同源天然成立,不用自己判断 origin
>   - **`doRefresh()` 加恢复链**(顺序有讲究):
>     ① 共享存储重读(**不发请求** —— 再发一次等于把兄弟刚拿到的 refresh
>     token 又消费掉,两个 tab 从此互相抢,最后谁都续不上)
>     → ② 跨标签页求助(800ms 超时)→ ③ 都没有才登出
>   - **`api/client.ts` 的 refresh 请求 401 不再当场清会话**:refresh token 是单次使用,
>     401 **可能是竞态不是掉线** —— 登不登出这个决定归 `doRefresh` 走完恢复链再定。
>     (拦截器只负责把 401 变成 reject;这条分工不成立的话整个恢复链就是死代码)
>   - **轮换检测**:`scheduleTokenRefresh` 记下"这次是为哪个 token 排的",
>     到点发现 token 换过了 → 直接重排、**不发请求** —— 共享存储时两个 tab
>     按同一份到期时刻排刷新,触发时刻几乎一样,先发的人成功、后发的人被拒。
>     有了这个快照,竞态压根不发生
>   - **降级**:没有 `BroadcastChannel` 的老浏览器 → 通道为 null →
>     勾了「记住我」照样能自愈,没勾的退回旧行为(被踢下线)。
>     ⚠️ **Node 的 BroadcastChannel(worker_threads 那个)必须排除**:
>     它派发 Node 的 MessageEvent 跟 jsdom 的 Event 打架,还吊住事件循环 ——
>     用 `prototype.unref` 区分,命中就降级
>   - **store 补记 refresh 到期时刻**(`REFRESH_TOKEN_EXPIRES_AT`):
>     跨标签页采用来的 refresh token 要按**它自己的**剩余寿命写盘 ——
>     少了这个值就只剩 8h 兜底,「记住我」该有的 30 天寿命被砍成 8 小时
>   - 测试:前端 23 条新用例(协议 14 + 调度 7 + 拦截器 2);
>     **探针 6/6 变红**
>   - ⚠️ **探针过程中抓到一条假绿用例**:「共享存储里换成了别人的会话 → 不捡」
>     原本把存储替换写在 `doRefresh` **之前** → `doRefresh` 读到的"待用 token"
>     就是新写进去的那个 → 「刚失败的那个 token」与「存储里的那个」是同一个
>     → `adoptFromSharedStorage` 在**第一道闸**就返回 false,压根没走到防串号那一道。
>     改成"兄弟在本次请求飞行途中换人登录"才真正压住那道闸(探针复测变红)
>   - **服务端宽限窗仍未做(明确不在本轮)**:后端加 3~5s 宽限窗能顺带治好
>     「响应丢了客户端拿已消费的 token 重试」和小程序侧 —— 但那是**服务端**改动,
>     牵动吊销语义,单独一轮
>   - 验证:web **18 文件 / 157 测试**全绿(改动前 14 / 130);
>     tsc **55 = 基线 55**;eslint 改动文件 **0 error**;
>     `vite build` 通过且产物里已无 `require(`
>
> **推迟/删除清单**(2026-10-19 batch9 收尾,**未分配编号**):
> - **推迟(业务触发型,3 项)**: 树形表格 / 可编辑表格 / 拖拽排序
>   (原先还有"行选择 + 批量操作",**2026-10-25 M5b 已交付框架层** —— 见下面 M5b/M5c 那条)
> - **删除(10 项 + 原因)**:
>   1. 业务级 metrics 埋点 — 框架埋点已就位(BusinessCounter/Histogram),业务方按需埋
>   2. 告警规则 — **运维侧(Prometheus/Grafana),不在 server 范围**
>   3. 日志聚合(Loki/ELK) — **运维侧,不在 server 范围**
>   4. 缓存注解 `@Cacheable` — Go 不像 Java 有 Spring AOP,`@Cacheable` 风格需要 code-gen,违反 Go 惯例,**显式 cachex.Get/Set 更好**
>   5. 业主/家庭/资料/资产/问题等业务 API 类型 — **业务线,用户明确不写**
>   6. 数据级权限(某条记录) — ReBAC 演进方案已规划,3 周工作量,**已 backlog**
>   7. 请求进度回调(下载进度) — ImageUploader 用 XHR 已实现,**99% 业务不需要**
>   8. 懒加载语言包 — zh-CN+en-US 全量 < 80KB,**真没必要**
>   9. 字段权限联动 — 需要后端 resource 字段级 metadata,**超出框架**
>   10. 表单设计器 / schema 驱动 — **EditModal 已是 schema 驱动**,不需要再设计器
>   11. 移动端适配 — PC 后台,**移动端不是当前目标**

> **M90 已落地(2026-10-25)**:缓存层收口 —— 后端抽象 + **三防唯一实现** + principal 缓存迁入
>
> **立项依据是 §1.7.1 自己写的那条审计结论**:
> "真正的业务缓存是另起炉灶的 …… `pkg/authz/cached_loader.go` 自己实现了 Redis get/set +
> singleflight,独立于 cachex,且**三防缺两样** …… 这是架构债,将来要决定迁移还是长期双轨"。
> 本轮把它迁了 —— **双轨关闭**。
>
> - ✅ **M90-1** `pkg/cachex/backend.go`:`Backend` 接口(`Get/Set/SetNX/Del/Expire`,未命中 → `ErrMiss`)
>   + `RedisBackend` 实现 + 两个**可选能力接口** `Scanner`(SCAN)/ `Pinger`(PING)。
>   **全仓只有这一个文件真正发 Redis 命令**(另两处 `*redis.Client` 只是构造函数签名)
> - ✅ **M90-2** `pkg/cachex/core.go`:三防收敛成**一个**泛型函数 `getOrLoadJSON[T]`
>   (singleflight / null 占位 / TTL 抖动)。`Safe.GetOrLoad`(any 版)与 `GetOrLoadTypedL2`(泛型版)
>   都调它 —— "三防只有一份实现"从此是字面意义上的
> - ✅ **M90-3** `pkg/authz/cached_loader.go`:principal 缓存迁到 cachex;
>   `repository/principal_cache.go` 与 `authz.CacheBackend` 一并删除
> - ✅ **M90-4** `pkg/cachex/memory_backend.go`:进程内后端(让三防**真路径可测**)+
>   `main.go` 装配改 `cachex.New(rdb)`
>
> **三个真收益(不是"重构洁癖")**:
> 1. **权限缓存补上了缺的那两样防**:改权限那 5 分钟里所有 principal 同时到期 = 雪崩;
>    不存在的身份每次穿透打 4 条 SQL。现在有 null 占位 + TTL 抖动
> 2. **全平台唯一在跑的缓存,终于看得见了**:principal 缓存的命中率进了 `app_cache_ops_total`
> 3. **拆掉一个 uint64 精度陷阱**:老实现 `JSON → any → JSON → T` 的中转会把整数 `float64` 化,
>    超过 2^53 的 ID 反序列化直接失败(principal 里装的正是 UserID / GrantID / ResourceID)
>
> **顺带修掉的两个真缺陷**:
> 1. **`nullTTL=0` 时 `ErrNotFound` 会冒泡成 error** —— "占位开不开"变成了"函数成不成功",
>    缓存配置开始影响业务语义。已改成:`ErrNotFound` 一律翻译成"零值 + 无错",
>    `nullTTL` 只管**写不写**占位
> 2. **数据竞争(`-race` 当场抓到)**:singleflight 路径下同一个 `*Principal` 会被多个 goroutine
>    拿到,直接写它的 `CacheHit` 字段就是竞争。已改成**先值拷贝再改**
>
> **刻意不做**:
> - **不加 L1**:L1 是进程内的,grant/revoke 的 Invalidate 只能清本实例 →
>    "改完权限立刻生效"退化成"最多 10 秒后生效"。**安全属性不值那点 RTT 收益**
> - **不用全局 cachex 池**(`cachex.Default()`):同上,那个池带 L1
> - **最后一公里仍不接**:字典 / 系统配置的直查要改业务 service,越界
>
> **测试**:cachex 新增 `core_test.go` + `memory_backend_test.go`(三防逐条:命中 / 未命中 /
> null 占位 / 抖动 / singleflight / 精度 / fail-open / 脏数据回源 / 无后端透传),
> `prefix_test.go` 补上 DeletePrefix 的 SCAN 路径(此前标着"未覆盖");
> authz 迁移测试 + 新增 null 占位 / 精度 / 无缓存透传三组;
> middleware 测试里那份自制的假缓存换成 cachex 内存后端。
> **可逆探针 9/9 全部变红**(抖动 / 单飞 / null 占位 / any 往返 / fail-open / 内存过期 /
> 共享指针竞争 / 失效删错 key / SCAN 路径)。
>
> 🔴 **顺带查明一件更严重的事:cachex 的真路径此前从没被测过**。
> 老的测试只能塞一个指向 127.0.0.1:6379 的死客户端,而本机没有 Redis ——
> 于是每次读都走"连接失败 → 降级回源"那条分支。**测的是降级,不是缓存。**
> singleflight / null 占位 / TTL 抖动三样,一样都没被验过。后端抽象之后才真跑起来。
>
> **M91 已落地(2026-10-25)**:授权缓存失效链补全 —— 失效粒度改成**按用户** +
> grant 写路径接上失效 + 前缀失效上生产
>
> **立项依据是读代码读出来的**(不是清单上写着的):principal 缓存装的是
> **授权画像**,而授权挂在**用户**上;但失效的接口却是**按身份**的。
> 于是查了一遍全仓所有写路径,发现 4 个缺口 —— **其中 2 个是安全级别的**:
>
> | # | 位置 | 缺口 | 后果 |
> |---|---|---|---|
> | **G1** | `owner_impl.go` `RevokeAuthorization` | **完全不失效缓存** —— ownerService 当时连 loader 依赖都没有 | 被撤销的授权最长一个 TTL(默认 5 分钟)仍然放行,且**无任何报错** |
> | **G2** | `owner_impl.go` `CreateAuthorization` | 同上 | 新授权最长 5 分钟不生效("授权给了,对方说没反应") |
> | **G3** | `family.go` `Revoke` | 只失效**主身份**那一条 | 多身份用户的其它身份缓存原封不动 → 旧授权最长 5 分钟仍放行 |
> | **G4** | `family.go` `AcceptInvite` | 同 G3 | 新授权对非主身份不生效 |
>
> 另有两处"静默":失效的错误被 `_ =` 吞掉;"查主身份失败 → 跳过失效"。
> **失效失败是查不出来的故障** —— 接口返 200、DB 也确实改了,只有权限画像是旧的。
>
> - ✅ **M91-1** `authz.Loader` 的失效方法从 `Invalidate(userID, identityID)` 改成
>   **`InvalidateUser(userID)`** —— 粒度是**用户**。
>   理由:principal 里装的东西几乎全是用户级的(grant 记 grantee_user_id、
>   override 记 user_id、feature flag 记 user_id),授权一改就是改"这个人";
>   只清一个身份 = 这个人的其它身份继续拿着旧授权。
>   反过来多清几个身份没有代价(只是多几次回源),所以接口里**只留粗粒度一种**。
> - ✅ **M91-2** `CachedLoader.InvalidateUser` 走 **cachex 前缀失效**
>   (`principal:{userID}:*`),不是"先查有哪些身份再逐个删" ——
>   后者要多打一次 DB,而且**查的那一瞬间之后新建的身份会漏掉**。
>   ⚠️ 前缀末尾的冒号不能省:少了它 `principal:1*` 会匹配上 `principal:12:22`,
>   把不相干的人的画像一起删了(删多了和没删一样危险)。
> - ✅ **M91-3** `cachex.Safe.SupportsScan()` —— 前缀失效**依赖后端支持 SCAN**。
>   `DeletePrefix` 在不支持时是 fail-open(返回 0, nil),那个语义对"清点不用的
>   缓存"没问题,但对"撤销授权"是灾难:静默什么都没删 = 权限没撤销。
>   所以 `InvalidateUser` 先问一句能力,拿不到就**报错**,绝不假装成功。
> - ✅ **M91-4** 四个写路径接上失效:owner 的 grant 新建/撤销、family 的接受/撤销。
>   失效失败**记 error 不吞掉**,也不回滚业务动作(DB 里已经生效,报"失败"是撒谎);
>   ownerService 从此**有** loader 依赖(与 familyService 对齐)。
> - ✅ **M91-5** `prefix.go` 第一次有了**生产调用方** —— 此前整份文件只服务测试
>   (审计表里那一条"typed.go 与 prefix.go 整个文件只服务测试"到此收口)。
>
> **测试**:authz 新增 4 例(多身份全清 / 前缀不误伤 / 不支持扫描必须报错 /
> 无缓存透传)+ owner 新增 `owner_test.go` 4 例(新建失效 / 撤销失效 /
> 越权撤销不失效 / 无 loader 不炸)+ cachex 补 `SupportsScan` 1 例。
> **可逆探针 7/7 全部变红**(只清一个身份 / 前缀少冒号 / 跳过能力检查 /
> owner 撤销不失效 / owner 新建不失效 / family 撤销不失效 / family 接受不失效)。
>
> **刻意不做**:
> - **不改缓存 key 格式**(principal:{userID}:{identityID} 是与线上 Redis 存量数据
>   绑在一起的隐式契约,改了上线瞬间会有一波全量回源)
> - **不做"逐个身份删 + DB 查身份列表"**(多一次 DB 查询 + 漏掉新建身份的窗口)
> - **不给 principal 缓存加 L1**(与 M90 同一条理由:改完权限立刻生效不能退化成
>   "最多 L1 TTL 后生效")
> - **不给失效失败加事务回滚**(DB 才是真相,回滚不了已经生效的授权变更)

> ✅ **2026-10-26:「问题列表打开是 404」—— 两处独立失效叠加,都修了**
>
> 用户报 `/issue/list` 点进去是 404 页。查下来**不是一处,是两处**,
> 而且**只修一处还是 404** —— 这是本次最值得记的一点:
>
> | # | 失效 | 位置 | 静默程度 |
> |---|---|---|---|
> | **E1** | 前端路由表里**根本没有** `/issue/list` 这条路由 | `web/src/router/routes.tsx` | 落到 `path: '*'` → 404 页 |
> | **E2** | 5 个 api 模块 **77 处调用漏了 group 前缀**(`/biz`),每一个必然 404 | `web/src/api/{asset,house,issue,owner,upload}.ts` | axios 弹个 toast,页面照常渲染,**CI 全绿** |
>
> **E1 的根因是一次"半边改动"**:后端 seed 里 `menu.issue.list` 的 path 在
> 2026-10-18(commit 82fd624)从 `/issue` 改成 `/issue/list`,**前端路由表没跟着改**;
> 而 2026-10-17(commit 291819e)当初跳过一级菜单 redirect 的理由正是
> "seed 里两个菜单 path 都是 `/issue`" —— seed 一改,那个前提就没了,
> redirect 一直没补回来。侧边栏是后端 `sys_resource` 驱动的,
> 点的就是 `/issue/list`。
>
> **E2 的根因是"路径基准"没对齐**:前端 API 路径是**相对 baseURL**(`/rapid-admin/v1`)的,
> 后端把业务端点挂在 `biz := api.Group("/biz")` 组下 —— 差一个 `/biz`。
> 写对了的模块长什么样可以对照:`/system/approvals` / `/platform/configs` /
> `/platform/dashboard` / `/system/flows` 都带着组前缀,`inbox.ts` 早就写的
> `/biz/inbox/...`。**只有这 5 个模块漏了**,像是同一批写的、忘了带前缀。
>
> **修法不是"逐个改对"就完事** —— 加了两张网:
> - `web/src/router/__tests__/menu-route-parity.test.ts` —— 解析后端 `seed.go`
>   的菜单 path,逐条比对前端路由表(commit 01c0d2e)
> - `web/src/api/__tests__/api-route-parity.test.ts` + `web/src/test/apiRouteScan.ts`
>   —— 解析后端 `router.go` 的**全部路由**(group 前缀沿父链解析),
>   用 TypeScript AST 扫前端**全部** `http.<verb>()` 调用,逐条比对(6 条断言)
>
> **两张网都做过可逆探针**:删掉 `issue/list` 那条路由 → 立刻红;
> 去掉某个 `/biz` 前缀 → 立刻红,并报出"哪个文件的哪一行打到了哪个不存在的路径"。
>
> ⚠️ **两条边界纪律,顺手记在这里**:
> 1. **菜单 / 路由 / API 路径是三份契约,改一份必须同时看另外两份** ——
>    "只改后端 seed"就会造出点菜单进不去的洞。
> 2. **"缺端点"和"路径写错"是两回事**:白名单里的 23 条是"后端欠一个端点"
>    (业务线),网只管"路径写错"(框架层)。两者混在一起,就会像这次一样
>    77 个真 bug 被当成"业务还没做"放过去。

> **M92 已落地(2026-10-26)**:本清单的**状态对账** —— **这份清单此前在说谎**
>
> 起因是照着 §4 排期,发现排出来的全是早已做完的事。查下来不是一处:
> **14 个「❌ 未完成(N 项)」块里有 9 个,块里每一行都已经打了 ✅ M-xx**
> —— 早就做完了,只是当初 M-xx 落地时在那行上加了个 ✅,**没把行从 ❌ 块搬到 ✅ 块**。
> 于是标题继续写着"未完成 6 项",表里 6 行却全是绿的。**读的人按标题排期,
> 排出来的全是已经做完的事** —— 这正是本仓纪律里反复强调要防的那类偏差。
>
> | # | 病 | 改法 |
> |---|---|---|
> | 1 | ❌ 块的行早做完了,标题还说"未完成 N 项" | N 改成**实际还开着的行数**;整块都做完的,块标记 ❌ → ✅ |
> | 2 | ✅ 块标题的 `(d/t)` 里 d / t 跟表里对不上 | d = 块里的行数、t = 本节总子项数;缺行补上(§1.2 #10 一直没进表) |
> | 3 | 小节标题的百分比是拍的 | 全部按表重算(`round(已完成 / 总子项 × 100)`) |
> | 4 | §1 / §2 两张总览表跟小节标题各说各话 | 两张表的"完整度"列跟小节标题逐个对齐 |
> | 5 | §4 / §5 的 P1 / P2 清单大半已完成却没标 | 补"状态"列,标出真正还开着的项 |
> | 6 | §3 / §4 / §5 / §6 是没有块结构的散表,守门网压根管不着 | 拆成 ✅ / ❌ 块,接进同一套计数模型 |
> | 7 | §6 与两张总览表的百分比各写各的(JWT 写 90%、§1.2 写 92%) | 改成**自引用**格式 `**92%** (§1.2)`,守门网逐格核对源小节 |
>
> **同时加了一张网**:`scripts/check_backlog_consistency.py` —— 清单里每一个计数
> 都必须和它下面的表格对得上,否则非零退出。这条网守的**只有"说自己做完了的,
> 是不是真的做完了"**,不判断优先级对不对,也不管证据链接还生不生效。
> 网一共查 5 类:① ✅ 块的行数 ② ✅ 块标题里的 (d/t) ③ ❌ 块里**还开着**的行数
> ④ 小节标题的百分比 ⑤ **每个百分比格的自引用**(§1/§2 总览表 + §6)。
> §3 / §4 / §5 / §6 原来是没有块结构的散表,**压根不在网的覆盖范围里** ——
> 这一轮把它们也拆成 ✅ / ❌ 块接了进来。
>
> ⚠️ **一条被顺手澄清的边界**:`⚠️` / `🔧` / `📋` 三种块**不计入**小节完成度 ——
> 它们装的是**范围说明 / 修复记录 / 证据表**,行数跟"子项数"不是一回事。
> §1.8 的"业务指标 / 告警规则 / 日志聚合"三条因此移进 `📋` 块 ——
> **它们是业务侧 / 运维侧的事,不是框架层的欠账**,混在框架清单里只会虚报欠账。
>
> **可逆探针**:把某个 ✅ 块标题的 N 改错 → 立刻红;
> 把某个 ❌ 块里的一行的 ✅ 去掉 → 立刻红(它变成"还开着"了);
> 把小节百分比改错 → 立刻红;
> **把 §1.2 的小节百分比改错 → §1 总览表和 §6 的自引用格**同时**红**(一处错、两处报警);
> 把某个百分比格的自引用去掉(写成裸 `**92%**`)→ 立刻红(它跟源小节失去绑定)。

> **M121 已落地(2026-10-30)**:前端测试设施的两档超时 —— **全量套件在满载下假红**
>
> 整轮跑(101 文件 / 908 例)时,两条**页面级**用例会因为机器忙而超时,
> 而它们其实**自带时序假设**:
> - `useRequest` 的取消用例:service 里写死 `sleep(20ms)`,机器一忙,
>   **service 先 resolve、cancel 后到** ⇒ 守卫通过、data 被写进去 ⇒ 用例红。
>   真 bug 不在这,在于**用例把"谁先到"写死了**。
> - `file/__tests__/list.test.tsx` 的用例等的是 `expect(listMock).toHaveBeenCalled()`
>   (**请求发出去了**),不是 DOM(**行渲染出来了**)—— 这两者之间隔着一整个 React 渲染。
>
> **两个超时旋钮此前只拧了一个**:vitest 的 `testTimeout`/`hookTimeout`(外层,60s)
> 和 @testing-library/dom 的 `asyncUtilTimeout`(`waitFor` 内层,**默认 1s**)。
> 内层 1s 是真正的瓶颈 —— `waitFor` 在 1s 内等不到就抛,外层 60s 根本用不上。
> 现在内层 `10_000`、外层 `120_000`,并补 `web/src/test/timeouts.test.ts`(5 例)把
> **"内层 < 外层"这条不变式**守住(测的是**配置本身**,不是行为)。
> ⚠️ 外层从 60s 提到 120s 是有实测依据的:满载时用户页 101s、资源编辑页 126s。
>
> **M123 已落地(2026-10-30)**:cron 分钟列表这一形态的语义要说全
>
> M120 把 `7,17,27,37,47,57 * * * *` 描述成「每小时第 7、17、27、37、47、57 分」——
> **只说了一半**,没说日期范围。用户把口径说准了:`*` 依次表示
> **每小时、每日、每月、每星期**,前面的数字是**第几分钟**。
> 于是这一形态改成**把日期范围整个写进句子**:
> 「每月每日每星期每小时的第 7、17、27、37、47、57 分钟各执行一次」。
> 探针 5/5 全红(其中一条是"范围被拼两次"—— 句子读起来仍然通顺,只有逐字断言抓得住)。
> 详见 `定时任务调度框架层设计方案V1.0.md` §11.7。
>
> **M122 已落地(2026-10-30)**:用户管理列表加一列「权限」
>
> `后台权限模型设计V1.0.md` §17.22 写的用户管理页是
> "9 列(…/部门/**角色**/…)",但**「角色」这一列从来没有真的存在过** ——
> 规划里点名了,代码里没渲染。想知道"这个人到底能干什么",
> 管理员得**点进详情抽屉**才看得到(还要单独发一次请求)。
>
> `UserListItem` 加 `Roles []UserRoleBrief`,三条语义边界:
> - **只取 `is_active = true` 的身份** —— 停用的身份不授予任何权限
>   (M25 的级联停用正是这条的来源)。把停用身份的角色也列出来 =
>   让管理员以为这个人还有那个权限,**看得见的错权限比看不见的更危险**。
> - **同一个 `role_code` 只保留一条** —— 这一列回答"这个人有什么**权限**",
>   不是"有**几个身份**"(身份清单在详情抽屉里,那里才有项目/部门/有效期)。
> - **没有角色返回空数组,不是 `null`** —— `null` 是"字段缺失",
>   前端区分不了"这个人没有角色"和"接口没返这个字段"。
>
> 一条 SQL 查完本页所有用户(不是 N+1);主身份的角色排最前。
> 6 条 Go 用例 + 3 条前端用例,探针 6/6 + 3/3 全红。
> 详见 `后台权限模型设计V1.0.md` §17.37。
>
> **M124 已落地(2026-10-30)**:个人中心头像(走文件系统)
>
> 「个人中心用户头像现在不能设置」—— 查下来是**两层同时失效**:
> `PATCH /auth/profile` 一直收一个 `avatar` 字段(任意 URL),
> 但**前端从来没提供过这个输入框** ⇒ 用户点不到任何地方;
> 而那条路本身带一个安全问题 —— **外链头像会渲染在别人的浏览器里**
> (管理端用户列表 / 顶栏)⇒ 那等于一个**跟踪像素**。
>
> ⇒ `UpdateProfileDTO.Avatar` **删了**(web 的 `UpdateProfileRequest.avatar` 同 commit 删),
> 新增 `POST /rapid-admin/v1/auth/avatar`(multipart,字段名 `file`),
> 落盘走文件系统:`biz = "avatar"`,存在**自己 id 底下**
> (`res/{user_id}/avatar/{folder_id}/{name}`),返回**稳定直链**。
>
> 四条边界:
> - **服务端三道闸**:大小(5 MB)/ 扩展名(MIME 白名单)/ **文件头魔数** ——
>   只信浏览器上报的 `Content-Type` 是能被伪造的。
>   前端那两道只是"友好提示",**不是安全边界**(注释里不能写成安全边界)。
> - **权限复用 `auth:update-profile`** —— 它本来就在 `seed.go` 的 `authBaseCodes` 里
>   ⇒ 所有后台角色都已绑上 ⇒ **不用新增 seed、不用跑 `cmd/migrate`**。
>   新增一个码的话,"改个头像"要迁移数据库才能用,是荒谬的耦合。
> - **旧头像回收** —— 换头像时同 `(user_id, biz='avatar')` 下更旧的文件软删,
>   否则换 10 次头像吃 10 份配额。
> - **空头像不裂图** —— antd 的 `<Avatar src="">` 会渲染 `<img src="">` = 浏览器裂图,
>   而且空 src **不触发 onError** ⇒ icon 兜底永远等不到。
>   三处展示(个人中心 / 顶栏 / 用户管理列表)统一走 `UserAvatar` 组件回落成首字母圆牌。
>
> 交互形态按用户要求:**没有单独的「更换头像」按钮** ——
> 头像本身就是触发器(悬停浮出铅笔遮罩,点击打开文件框)。
> 后端 8 条用例 + 前端 14 条用例(9 + 3 + 2),探针 6/6(后端)+ 9/9(前端)全红。
> 详见 `文件系统模块设计方案V1.0.md` §17 与 `后台权限模型设计V1.0.md` §17.38。
>
> ⚠️ **跑 vitest 的 cwd 必须用 realpath(盘符大小写归一)** ——
> 写探针脚本时踩出来的:`vitest.config.ts` 把 root 钉成 `realpathSync.native`,
> 但**测试文件自己的 import 仍按 cwd 解析**。cwd 用小写盘符时,
> `@/api/auth` 会解析出**两个模块实例** ⇒ `vi.mock('@/api/auth')` 的工厂
> 只作用在其中一个 ⇒ 被测代码拿到**真函数** ⇒
> `TypeError: uploadAvatarMock.mockResolvedValue is not a function`。
> **同一个文件、同一条命令**:`D:\` ⇒ 9/9 绿;`d:\` ⇒ 5 条红。
> 跟 `web-vitest-dual-react.md` 是同一个坑的另一个面(那次是 React,这次是 api 模块)。
> ⇒ 任何用 `subprocess` 跑 vitest 的脚本,`cwd` 一律用 `os.path.realpath()`。
>
> **M125 已落地(2026-10-30)**:多级文件夹 + 封面必须是图片
>
> 用户诉求:「创建文件夹时候要能创建多级文件夹,文件夹点击可以去子文件夹;
> 把文件设置为封面必须图片才能设置;设置为封面以后在哪里显示这个封面?」
>
> **为什么不用全量迁移就能支持嵌套** —— 原设计 §2.7 D7 决定"文件夹是扁平的"时,
> 担心的代价是"将来要支持 A/B/C 三级,路径规范必须改,需要一次全量数据迁移"。
> **这个担心没有成立**:路径是 `res/{userId}/{biz}/{folderId}/{对象名}`,
> `{folderId}` 放的一直是"文件所在**叶子**文件夹"的 id —— 嵌套之后叶子照样有自己的雪花 id,
> key 段数恒 5 段,已落库的文件**一个字节都不用挪**。
> 当初"表结构留 `parent_id`、不改 key 生成逻辑"这个选择正好是红利。
>
> 四条边界:
> - **唯一索引必须带 `parent_id`** —— 语义从"一个用户一个业务段下名字唯一"变成
>   "**同一个父级**下名字唯一"(A 下和 B 下都可以有"图纸")。
>   ⚠️ **GORM `AutoMigrate` 只加索引不删索引** ⇒ 光改 struct tag 不够,
>   旧索引 `uk_folder_owner_biz_name` 会继续生效 ⇒ "B/图纸"建不了,
>   而且报的还是"同级下已有同名文件夹"(B 下明明没有)。**报错信息对不上真实原因**。
>   必须有一条显式的 `dropLegacyFolderIndex` 迁移步骤。
> - **业务段继承上级** —— 子文件夹跟父级必须同段,否则父级里看不到它(而且不报错)。
>   前端建子文件夹时**不显示** biz 输入框;显式传了不一致的 biz → 报错,不静默改写。
> - **删文件夹 = 删整棵子树** —— 子文件夹里的文件也要没。少删一段就是
>   "文件还在云存储里,但库里已经没了"(账单照收,而且没人会知道)。
> - **层级上限 10 层** —— 在应用层判(唯一索引管不了深度);理由是"建了 30 层"
>   会让路径长到面包屑一行放不下。
>
> **封面两个约束**:
> - **只有图片能当封面**(SVG 也排除 —— SVG 能塞 `<script>`,而封面会被
>   "点开看大图"打开成**同源文档**)。服务端 `isCoverImage` 拦一道,
>   前端**同口径**把按钮直接禁用(不是点了才报错)—— 前端禁是为了**当场给一句人话**。
> - **封面显示在三处**:文件夹列表缩略图列 / 文件夹详情抽屉 / 文件行上的金色「封面」标签。
>   ⚠️ `cover_url` 是**稳定直链**(`/res/...` 或 OSS URL),不是签名 URL ——
>   签名 URL 过期封面就裂图。拿不到封面返**空串**(前端显示"无封面"),不返错。
>
> **前端这一页从"平铺列表"变成了"目录浏览器"**:当前所在文件夹放在 URL
> (`?folder_id=`)里 —— 刷新不丢、链接能直接发给同事、深链进来首屏就列那一级。
> 面包屑的祖先链**只能从后端要**(前端手里只有当前 id,分页列表里未必有祖先)。
> ⚠️ 层级过滤是**三态**(不传=不限层级 / 0=只列根级 / id=只列直接子级),
> "不传"绝不能翻译成 "0" —— 那样"进入文件夹"和"看全部"变成同一个请求。
> ⚠️ 有关键字时**不限层级**,否则根级搜索永远搜不到子文件夹,而用户看不出为什么。
>
> 后端 12 条新用例 + 前端 13 + 13 + 8 条,探针全红。
> 详见 `文件系统模块设计方案V1.0.md` §2.7 / §18。
>
> **这次没做**(留档,免得下一个人以为漏了):文件夹**重命名 / 移动**(拖拽改 `parent_id`)——
> 后端没有这两个接口,页面上的每个按钮都必须对应一个**真实存在**的后端动作;
> 子文件夹的 `file_count`/`total_size` 也**不递归累加**。
>
> **M127 已落地(2026-10-31)**:满载假红的第二类根因 —— **等请求 ≠ 等渲染**
>
> **为什么单开一条**:全量 vitest 是 **104 文件 / 948 例 / 754 s ≈ 12.6 min**,
> 这是"一个模块要花 15~18 分钟"里最大的那 80%。而它**稳定地产生 1 条假红**
> ⇒ 每个模块要跑**两遍** ⇒ 实际 25 分钟。
> M121 修的是**超时**(两个旋钮只拧了一个),本轮修的是**等错了东西**。
>
> **根因**(与 M121 记的 `file/__tests__/list.test.tsx` 那条同一个,但更普遍):
>
> ```ts
> await waitFor(() => expect(listMock).toHaveBeenCalled())   // ← 等的是"请求发出去了"
> expect(screen.getByText('审核通过')).toBeInTheDocument()    // ← 却立刻断言"表格已经渲染出来了"
> ```
>
> `listMock` 是 `vi.fn()` —— `toHaveBeenCalled()` 在**调用发生的那一刻**就为真;
> 组件还要 `await listMock()` → setState → React 重渲染,中间隔着
> **至少一个微任务 + 一次 React 调度**(React 18 的并发渲染走 scheduler 的
> `MessageChannel` **宏任务**)。⇒ 满载时这个间隔被拉长到**超过同步断言的时机**,
> 断言就落在"数据还没画出来"的那一帧上。
> **单独跑那个文件是绿的** —— 这就是"满载假红"的判定特征。
>
> **三种长相**(修法不同,但根因是同一个):
>
> | 长相 | 现象 | 修法 |
> |---|---|---|
> | 直接查文本 | `Unable to find an element with the text: 审核通过` | `await waitFor(() => expect(screen.getByText(...)))` |
> | 查**自定义 helper**(读 DOM 的) | `expected [] to deeply equal [ 'aliyun.oss', 'aliyun.sms' ]` | helper 自己外面套 `waitFor` |
> | helper 在行还没画时**抛异常** | `表格里没有 scene=login 那一行`(⚠️ **看起来像真 bug**) | 加 `waitForRow()` 先把行等出来 |
>
> 还有第四种,更隐蔽 —— **元素在请求发出后的那个窗口里"暂时不在"**:
> `CaptchaField` 拉新图期间 `loading=true` ⇒ 渲染的是 `<Spin/>`,
> `<img alt="captcha">` **整个从 DOM 里消失**;而
> `toHaveBeenCalledTimes(2)` 恰好在**请求发出那一刻**为真 ⇒ 正好落在这个窗口里。
> ⇒ 改成等**旧图重新出现**(失败时 `image` 没被清掉,它一定会回来)。
>
> **这一轮改了 15 处 / 4 个文件**:
> `sms-template` ×8(文本 ×4 + `rowOf` ×4)、`file/__tests__/list.test.tsx` ×3、
> `config/index.tsx` ×3、`captchaField` ×1。
> ⚠️ 顺带**修掉一条假绿**:`sms-template` 的"没有按钮权限时行操作按钮不渲染" ——
> 表格还没画的时候自然一个按钮都没有,断言白绿 ⇒ 先把表格等出来再断言按钮不存在。
> **假绿比假红更坑**(它让一个真 bug 永远不会被发现)。
>
> **判据**(别乱改,免得把安全的也改了):
> - 等的是 `mock.toHaveBeenCalled()` **且**后面跟的裸断言查的是**响应派生的内容**
>   ⇒ 要改;
> - 等的是 DOM 的(`await waitFor(() => expect(screen.getByText(...)))`)⇒ **安全,别动**;
> - 断言查的是**静态 UI**(工具栏按钮 / 搜索框 / URL 参数派生的标签)⇒ **安全,别动**。
>   ⚠️ 例如 `config/__tests__/audit.test.tsx` 那条 `aliyun.sms` 来自
>   `params.get('configGroup')`(**首次渲染就在**),不是响应 ⇒ 不受影响。
>
> **全仓扫过一遍**:规则 = "等请求之后的第一条裸 DOM 断言" ⇒ 51 处候选,
> 逐条读实现后确认 **真站点只有上面那 15 处**,其余是静态 UI 或已在 `waitFor` 内。
> ⚠️ **扫描器本身也骗过人**:第一版只认 `screen.getBy*`,
> 漏掉了用自定义 helper 的 `config/__tests__/index.test.tsx`(就是本轮实测红的那条);
> 换成"等请求之后第一条**不引用 mock 本身**的断言"才把候选捞全。
>
> **顺带实测了三个提速杠杆**(改之前先知道值不值):
>
> | 做法 | 耗时 | 结论 |
> |---|---|---|
> | 默认(forks) | 754 s | 基线 |
> | `--pool=threads` | 690 s(−8.6%) | 保持隔离,**可用但收益一般** |
> | `--no-isolate` | 385 s | ❌ **不能用**:416 例红 / 67 文件红 |
>
> `--no-isolate` 快的机制是**消掉了 61% 的非断言开销**
> (vitest 自己报的 `collect + environment + setup` 从 10529 降到 4125)——
> 这部分正是"每个测试文件重新加载一遍模块图 + 重新启动一次 jsdom"。
> 但代价是**跨文件串味**:`react-dom` 自己的 `flushSync` 抛
> "Should not already be working."(143 次)、zustand 单例串味、DOM 残留。
> **要采纳它得先修 69 个文件的隔离卫生 —— 那是另一个项目,别顺手做。**
>
> ⚠️ **修完之后两个池都一次过**(2026-10-31 实测):
> 默认池 **104 文件 / 948 例全绿 / 578 s**、`--pool=threads` **597 s 同样全绿**。
> 修之前这两个池**各稳定产生 1 条假红** ⇒ 每个模块要跑两遍(25 分钟),
> 现在一趟就过 ⇒ **每个模块省 12.6 分钟**。
> ⚠️ 这也说明 `--pool=threads` 的价值**不在速度**(它甚至比默认池慢 19 s),
> 而在**它能把假红逼出来** —— 默认池下假红出现的**位置**还会漂移,
> threads 池下它**稳定落在同一处** ⇒ **它是更好的复现条件,不是更快的跑法**。
>
> ⚠️ **vitest 的时间构成**(决定了所有提速方向):
> `collect 6904 s + environment 2640 s + setup 985 s` = **77%**,
> 真正跑断言只有 `tests 2853 s` = 23%
> ⇒ **成本由环境搭建主导,不由断言数量主导**。
>
> **以后的判假红流程**(省掉"再跑一遍 12.6 分钟"):
> **全量红了 → 先单独重跑那个文件 → 单独跑是绿的 ⇒ 满载假红,不是回归**。
>
> **这次没做**(留档):① 剩下几条**假绿**(断言"某东西**不**存在"的用例 ——
> 组件还没渲染时自然不存在,断言白绿;`identitySwitcher` / `CronInput` / `user` 各一条)
> —— 修它们要"等一个**不该出现**的东西不出现",跟本轮的"等它出现"是两种时序,另开一轮;
> ② `--pool=threads` 提不提速由用户定(本轮**没有**擅自改 `vitest.config.ts`)。

---

> **M128 已落地(2026-10-31)**:异常登录检测 —— **清单上标 ✅ ≠ 那行被读过**
>
> **为什么单开一条**:M126/M127 都在修「**实现了但没装配**」。
> 这一轮是同一族的第三个形状 —— **「实现了、清单也标了 ✅、但旋钮是死的」**。
>
> §1.1 #11 那行从 M45 起就写着「✅ M45:`pkg/anomalylogin/` 纯函数 Detector
> (**MaxConcurrent=3 默认**)… 10 单测 -race 干净」。
> 逐行读下来发现三件事:
>
> | # | 清单说的 | 实际 |
> |---|---|---|
> | ① | 「MaxConcurrent=3 默认」 | `Detect` 里硬编码 `len(active) > 1` —— **`MaxConcurrent` 从来没被任何一行读过**。配 10 也**第 2 端就报 High**。清单描述的是**字段注释**,不是行为 |
> | ② | 「Notifier interface(LogNotifier 默认)」 | `LogNotifier.Notify` 函数体只有 `if n.Hook != nil { n.Hook(...) }` —— 注释写着"落 stderr",实现里**一行日志都没有**。用默认通知器 = 什么都不发生 |
> | ③ | 「用户被盗号时无法感知」(痛点) | `grep -rn 'internal/pkg/anomalylogin'` **全仓零命中** —— 这个包**从来没有被任何一行生产代码调用过**。⇒ 这条痛点**一次都没被真正解决过** |
>
> ① 和 ② 是"旋钮是死的",③ 是"没人叫它"。三条叠起来 = **清单标 ✅ 的东西可能一行都没跑过**。
>
> **M128-1**(`468d2bb`):修包本身 —— `Detect` 真的按阈值判
> (`maxConcurrent()` 把 ≤0 兜底成 **1**,不是 0:零值 Detector 若按字面当
> "不允许任何并发",那么"只有当前这一个会话"也超限 ⇒ **登录一次报一次异常**);
> `LogNotifier.Notify` 真的记一条 **WARN**(安全事件不是调试信息),
> 但**不阻断登录**。补 6 条新用例(3 条原有断言**编码了那个 bug**,已改成显式阈值)。
>
> **M128-2**(本条):**接线**。
>
> - `internal/service/auth_anomaly.go`(新):`detectLoginAnomaly` 挂在 `Login` 的
>   **第 10 步**(成功登录之后、写完登录日志之后)。**检测失败永不阻断登录** ——
>   查库失败 / 站内信投递失败都只记日志。一个"安全增强"把所有人的登录搞挂,
>   是比没有检测更贵的事故。
> - ⚠️ **两个入口都接了**:仓里有**两条**能签发 token + 建会话的路 ——
>   密码登录(`Login`)与**短信登录**(`LoginBySms`)。只在密码那条接检测的话,
>   短信那条就是一条**免检通道**:被冒用的账号改走短信登录(手机号也到手了,
>   恰恰最可能是这条路径),本人**永远收不到提醒**。
>   ⇒ 凡是"签发 token + 建会话"的入口,检测都得跟着走一遍。
>   (`SwitchIdentity` **不接**:它沿用当前会话的 sid,不是新会话 ——
>   同一个 sid 自己去查自己会被 `session_id != currentSID` 排除掉,接了是自欺欺人;
>   `RefreshToken` 也不接:它复用 sid,同理。)

> - **「同时在线」的口径**(本模块最锋利的一条):仓里有两个"会话还活着"的口径 ——
>   `sessionExpired`(`COALESCE(last_active_at, login_at) + refresh_ttl <= now`,
>   refresh_ttl = **720h / 30 天**)答的是「这条会话**还算不算数**」;
>   本模块的**活动窗口**(`active_window`,默认 **30m**)答的是
>   「这个人**现在在不在用**」。⚠️ **必须用后者** —— 用前者的话,
>   一个 30 天里在 3 台设备上登录过的人,第 4 次登录时"同时活跃会话数"就是 4 > 阈值 3
>   ⇒ **每次登录都报异常** ⇒ 阈值形同虚设,功能上线第一天就会被关掉
>   ⇒ 于是又回到"检测从未真正生效"。活动时刻取 `COALESCE(last_active_at, login_at)`
>   —— 与「活跃设备」列表同源。
> - **站内信只发 High**:Medium(只是设备不同)太常见 —— 一个"手机 + 电脑"的
>   正常人每次登录都会命中一次。发站内信 ⇒ 用户被反复骚扰 ⇒ 关掉通知
>   ⇒ 真异常也没人知道。Medium 只进日志(给运维看),High 才进站内信(给用户看)。
> - **旋钮**:`auth.anomaly.enabled` / `max_concurrent` / `active_window` /
>   `impossible_travel_window`。⚠️ `impossible_travel_window` **默认 0(关)**
>   是**刻意的**:这条规则只看 IP 不看距离,用户从家里 WiFi 切到手机流量
>   (几秒换 IP)必然误报 —— 真正的"不可能旅行"需要 **IP 地理库**,本仓没有。
>   框架把旋钮留好,要不要开由运维按误报容忍度定。
> - **`NewAuthService` 的两个新参数是必填**:漏接是**编译错误**,不是"静默不跑"。
>   理由:检测一旦接进登录链路,"忘了接"的症状是"日志里一条异常都没有",
>   而它与"跑了但没发现异常"**完全无法区分** —— 最难查的一类故障。
> - **启动自检**(`cmd/server/main.go`):检测是**静默**的(不报错、不弹窗),
>   所以把开关与阈值打成一行启动日志,并对两种危险形态 loud 告警:
>   `enabled=false`(检测整个不跑)、`max_concurrent<=1`(被兜底成"只允许单端在线")。
>
> **测试**:包层 18 条(service 侧 14 条 + config 侧 3 条 + pkg 层补 6 条)。
> service 侧那组**故意让 `newAuthTestSvc` 默认开着检测** ——
> 于是本包**每一个**登录用例都在跑检测,"检测不影响登录"这条性质由所有用例共同守着。
>
> **三组可逆探针**(拆掉实现看测试会不会红):
>
> | 探针 | 预期变红的用例 | 实测 |
> |---|---|---|
> | 注释掉 `Login` 里的 `s.detectLoginAnomaly(...)` | 6 条"应当检出"的用例 | ✅ 正好 6 条红;6 条"不该检出"的用例保持绿(**负向断言对"没接线"天然不敏感** —— 检出信号全靠正向那组) |
> | 注释掉 `LoginBySms` 里的 `s.detectLoginAnomaly(...)` | 只有 `LoginBySms` 那条 | ✅ 正好 1 条红;**密码登录那 6 条保持绿** ⇒ 两处接线**互相独立**,各自的用例钉各自的路径 |
> | 去掉 `base.Before(cutoff)` 这个活动窗口过滤 | `StaleSessions_NotCounted` + `ActiveWindowBoundary/窗口外_31分钟` | ✅ 正好这 2 条红 |
>
> ⚠️ 第二组探针是本模块的**核心风险**:它就是"口径用错"长什么样。
>
> **这次没做**(留档):① 异常登录**没有强制下线**旧会话 —— 检测是**提醒**不是处置,
> 处置(吊销 jti / sid)已有现成机制(`pkg/auth` 的吊销存储),但**由谁来触发**是业务决策;
> ② **没有 IP 地理库**,所以 `impossible_travel_window` 只能空转;
> ③ 异常记录**没有单独的表** —— 目前只有日志 + 站内信,没有"异常登录历史"可查。

---

> **M129 已落地(2026-10-31)**:零调用方清理 + **可达性守卫** ——
> 把「清单上标 ✅ ≠ 那行被读过」从**人工发现**变成**机械拦截**
>
> **为什么单开一条**:M128 修的是一个具体包(异常登录检测)。
> 这一轮问的是**下一个还会不会发生** —— 答案是「会」,因为发现手段是
> `grep` + 人工读清单,而**清单是人写的,人会忘**。
>
> 全仓实测(脚本扫 import 图,排除测试文件):
>
> | 包 | 生产调用方 | 单测 | 清单怎么标的 | 处置 |
> |---|---|---|---|---|
> | `pkg/sms/` **已删** | **0** | **0** | §1.6 #4/#5、§1.9 #4/#5/#6 **五处**都指着它 | **删**(M129-1) |
> | `pkg/ratelimitx/` | **0** | 12 条 | §1.11 #9 标 "P2 ✅" | **留 + 如实标注**(M129-2) |
> | `pkg/searchidx/` | **0** | 13 条 | §1.5 #9 标 "P2 ✅" | **留 + 如实标注**(M129-2) |
>
> ⚠️ 三条根因同源,只有一条:
> **「有单测」被当成了「在跑」**。`ratelimitx` / `searchidx` 的单测都跑得干干净净
> (`-race` 干净、3x 复测),清单也都标了 ✅ —— 但**没有一行生产代码调它们**。
> M128 挖出来的 `pkg/anomalylogin` 是同一个形状(172 行单测 + 零调用方)。
>
> **M129-1**:`pkg/sms/` **删除**(129 行:provider.go 112 + mock.go 17)。
> 删除前逐条核对**它的四块能力是否另有活着的实现** —— 结论是**四块全有**:
>
> | 已删的 `pkg/sms` 里的 | 真正在用的是 | 状态 |
> |---|---|---|
> | `RateLimiter`(Redis 固定窗口) | `pkg/smslimit/`(已装配在 `main.go` + `auth_impl.go`) | ✅ 在跑 |
> | `VerifyStore`(Redis 5 分钟 TTL) | `sys_sms_code` 表 + `service/auth_impl.go:VerifySms` | ✅ 在跑 |
> | `Provider` 抽象 | `pkg/msn.Sender` + `msn.Resolver`/`DynamicSender` | ✅ 在跑 |
> | `MockProvider` | `pkg/msn/console/`(dev 模式打印) | ✅ 在跑 |
>
> ⇒ 四块能力**没有一块**会因为删包而消失。删的是**第四份**实现。
>
> ⚠️ 删包还带出**两处口径错**(不删就一直错):
> ① `internal/model/sms.go` 的 TTL 注释写着"默认 5 分钟" —— 那是**已删的
> `pkg/sms.VerifyStore`**(Redis 版)的口径;真正在用的 `sms.rate_limit.code_ttl`
> 模板值是 **600 秒 = 10 分钟**。运维按 5 分钟去排查一个 10 分钟的行为,会被带偏。
> ② 清单 §1.6 #5 的标题「VerifyStore(5 分钟 TTL)」也是同一个错的来源。
>
> 文档同步(照 M112 删 `pkg/httputil` 的先例):清单 §1.6/§1.9/附录 A、
> `server/README.md` 目录树、后台管理平台技术方案 V1.0(未随开源库分发)
> §4.1「目录树订正表」+ §14.5.5 —— **删一个包要改三处文档**,少一处就有一处
> 会继续把人引向死代码。
>
> **M129-2**:`pkg/ratelimitx/` / `pkg/searchidx/` **不删** —— 它们是**库类原语**,
> 接线点在业务线(具体端点 / 第一个密文字段),框架层先把原语备好是合理的。
> 但清单里那两行此前只写"✅ M41/M46 原语已交付",**读的人会以为能力已经在跑了**。
> ⇒ 两行各加一段 🔴 订正:明写「**零生产调用方**」+ 接线点在哪 +
> 判「这个原语有没有用」要看的不是它的 13 条单测,是**它的调用方**。
> (顺带把 §1.11 #9 那个**从没填过的 `(commit xxx)` 占位符**填成真的 `e60912b`。)
>
> **M129-3**:**可达性守卫** —— `server/internal/guard/`(新包)+ `guard_test.go`。
> 把上面这张表**变成 CI 里的一条断言**:每个 `internal/pkg/*` 包,
> **要么**能从 `cmd/` 入口走到,**要么**在 allowlist 里写明理由。
>
> - **判据是「可达性」,不是「谁 import 了我」** —— 包 A import 包 B,
>   而没人 import 包 A 时,包 B 的 import 方计数是 1,但它**同样没在跑**。
>   ⇒ 从 `cmd/**` 出发做一次可达性传播。
>   (入口取 `cmd/**` 而不是只取 `cmd/server`:本仓还有 `cmd/seed` / `cmd/migrate` /
>   `cmd/seed_audit` / `cmd/gen-errcode-doc` 几个**一次性运维脚本**,
>   它们也是**会编译、会被执行**的真程序 —— 只被它们用到的包**不算死代码**。
>   实测今天两种取法结果一致(54 个 `internal/pkg` 包,不可达的都是账本上那两个),
>   但那是**巧合**不是保证,所以取宽的。)
> - **边只取非测试文件里的 import** —— 测试 import 一个包只证明"有人在测它",
>   不证明"它在跑"。这正是本仓最常见的假绿(`pkg/anomalylogin` 就是这么骗过所有人的)。
> - **allowlist 是账本,不是逃逸名单** —— 每条都要写清"为什么它现在可以没有调用方";
>   另有一条测试守着 allowlist **不许留已经不存在的包名**(留着 = 给守卫开后门)。
> - 守卫放在 `internal/guard/`(**不放在 `internal/pkg/` 底下** —— 它不在自己的扫描集里)。
> - 当前账本:`ratelimitx`(库类原语,接线点在具体端点)+ `searchidx`
>   (库类原语,接线点在第一个密文字段)。两条都在清单里如实标注了。
>
> **三组可逆探针**(拆掉实现看测试会不会红):
>
> | 探针 | 预期变红的用例 | 实测 |
> |---|---|---|
> | 在 `internal/pkg/` 底下新造一个没人 import 的孤儿包 | `TestEveryInternalPkgPackage_IsReachableOrAllowlisted` | ✅ 红,并点名那个孤儿包 |
> | 临时切断 `cmd/server/main.go` → `pkg/slowquery` 的 import 边 | 同上 | ✅ 红,并点名 `slowquery`(**真实包**,不是造出来的) |
> | 往 allowlist 里塞一个不存在的包名 | `TestAllowlist_EntriesActuallyExist` | ✅ 红,提示"留着它 = 守卫的后门" |
>
> ⚠️ 第二组探针才是**真探针**:第一组只证明"新造的孤儿能被抓到",
> 第二组证明**"已经在跑的包掉线了会被抓到"** —— 那才是这个守卫的日常职责。
>
> **这次没做**(留档):① 守卫只覆盖 `internal/pkg/**` —— `internal/service` /
> `internal/api` / `internal/config` 这些层**不扫**(它们是**业务面**,
> "有没有调用方"要按业务场景判,机械判会误伤);② 入口只认 `cmd/**` ——
> 本仓现有的 6 个可执行目录(`server` / `seed` / `migrate` / `seed_audit` /
> `seed_dept` / `gen-errcode-doc`)**全部**在覆盖内,但**将来**若在 `cmd/` 之外
> (例如 `tools/`、`scripts/` 下再长出常驻程序)长出新的入口,守卫**看不见**它
> —— 需要手工把那个目录加进 `entriesOf`;③ M127 记下的 **3 处假绿用例**
> (`identitySwitcher` / `CronInput` / `user`)**仍未修** —— 那是前端,跟本轮无关。

---

> **M130 已落地(2026-10-31)**:文档指路牌守卫 ——
> 把「文档里指的路还通不通」变成 `go test` 里的一条断言
>
> **为什么单开一条**:M129 删掉 `pkg/sms/`(M129 已删)之后顺手发现,清单里还留着
> 几处指向 `pkg/httputil/`(M112 已删)的路标 —— 那个包 M112 就删了。
> 根因跟 M129 挖出来的"有单测 ≠ 在跑"是**同一条**:**人删了代码,忘了改文档**。
> 而"忘了"这件事,只有变成断言才拦得住。
>
> **M130-1**:清单里 **18 处**过期路标(分布在 16 行)逐条改成真实现(不是改名了事 ——
> 每处都对着源码核过)。另加 **3 处**口径订正 —— 那不是"路径变了",
> 而是**口径本来就写错了**(§1.10 那张 OSS 表还在描述 M80 之前的双桶模型):
>
> | §1.10 那一行 | 此前写的是 | 实际是 |
> |---|---|---|
> | OSS provider | `SignDownloadURL` 私有 bucket 临时下载 URL | **稳定直链**(不签名、不带过期时间);`Delete` 批量删除**单桶**,不再按 public/ 前缀分桶 |
> | 下载 | 私有 bucket 签 GET presigned | **不签 presigned** —— 签名 URL 一过期,页面上的图就裂;私密靠"key 猜不到",不靠 bucket ACL |
> | 分片上传 | 按 public/ 前缀选桶 | `pickBucket` **单桶**;`bucketsForPrefix` 也只返那一个桶 |
>
> ⇒ 这三处**比路径过期更坏**:路径过期读者扑空,口径过期读者**照着错的做**
> (运维按"私有桶预签名"去配 CDN 域,或者以为改个 public/ 前缀就能分桶)。
> 逐行核过:`pkg/oss/aliyun/aliyun.go` 的 `SignDownloadURL` / `Delete`、
> `pkg/oss/aliyun/advanced.go` 的 `pickBucket`。
>
> **M130-2**:**指路牌守卫** —— `server/internal/guard/docrefs.go` + `docrefs_test.go`。
> 把"文档里指的路还通不通"变成 `go test` 里的一条断言。
>
> - **判定链**(五步,每一步都能单独解释):
>   ① 抽出反引号段 → ② 形状判定(`pathTokenOf`)→ ③ 仓库形状过滤
>   (`looksLikeRepoPath`)→ ④ 第三方库过滤(`isVendorPath`)→ ⑤ 拿仓库索引
>   **真的解析一次**(`resolve`),解析不到再问"这是不是历史陈述"
>   (`exemptByDeletion`)。
> - **形状判定看的是形状,不是认不认得它**:有扩展名 ⇒ 扩展名部分**原样**全小写;
>   无扩展名 ⇒ 整段全小写。⚠️ **不能用扩展名白名单** —— 白名单会把**拼错的
>   扩展名**整条漏掉(探针把 `.service` 拼成 `.servce`,那个文件不存在,而守卫
>   一声不吭)。这条规则是**探针逼出来的**,不是想出来的。
> - **"首段必须是本仓某个目录名"**是最要紧的一条取舍:文档里大量反引号内容
>   **根本不是仓库路径**(npm 包内部布局 / 标准库 / 相对 URL / 错误码区间 /
>   枚举值)。⇒ 代价:会漏掉"首段本身就不存在"的错指针;⇒ 换来:零误报。
>   **误报比漏报更致命** —— 一个天天误报的守卫,没人愿意跑第二次。
> - **"历史陈述"按"删除动词解释它旁边那一个路标"判**,中间隔了别的路标就不算
>   (隔了类型名**算** —— `authz.CacheBackend` 不是路标)。
>   ⚠️ **逐行判**:删除动词必须和那个路径**写在同一行**(跨行不豁免 ——
>   表格里"上一行一个路径、下一行一个删除动词"太常见,跨行豁免会放过不相干的路径)。
> - **距离按字符算,不按字节**:文档是中文,一个汉字 3 字节,按字节算会把
>   「本行此前写的是 … 已于 M129 删除」这种正常句子判成"离得太远"。
>   阈值 40 字符是**量出来的**:清单里"删除动词 → 它解释的那个路标"实测
>   最大 31 字符,而不相干的路标距离 ≥ 66 字符。
> - **只查一种谎**:报"路标指向不存在的路径";
>   **故意不查**"文档说它已删除,但它还在"(散文里"删除"指哪个路径不可靠,
>   实测两处就会误判)。宁可漏,不错报。
> - **报错不只说"断了",还说"你是不是想写这个"**:文件名对得上、目录对不上 ⇒
>   提示同名的文件;**词干对得上、扩展名对不上** ⇒ 直接把同名词干的真文件指出来。
>   探针把 `.service` 拼成 `.servce` 时,提示是
>   「同名词干的文件在仓里,扩展名对不上? → server/deploy/systemd/rapid-admin.service」
>   —— 照着改就行,不用自己翻仓。
>
> **三组可逆探针**(拆掉实现看测试会不会红):
>
> | 探针 | 预期变红的用例 | 实测 |
> |---|---|---|
> | 把 README 里 systemd unit 的扩展名拼成 `.servce` | `TestCheckDocRefs_扫描集里没有断链的路标` | ✅ 红,并点名那个拼错的路标 |
> | 把清单里 `pkg/msn/console/console.go` 的文件名改成 console2.go(多一个 2) | 同上 | ✅ 红,并点名那个 console2.go |
> | 临时把 `CheckDocRefs` 换成假实现(永远返空) | `TestCheckDocRefs_抓断链的路标` | ✅ 红(这条守的是守卫本身没坏) |
>
> ⚠️ **第一组探针当场把守卫的盲区逼出来了**:第一版按"扩展名白名单"判,
> 探针一打就发现守卫**看不见**拼错的扩展名 ⇒ 规则改成"看形状"。
> (第二组之前还踩过一次"用反引号段当路标"的错判据 —— 类型名隔在中间时误报,
> 是靠读清单第 637 行的真实形状才发现的。)
>
> **M130-3**:**扫描集只有两份文档** —— 清单 + `server/README.md`,
> 理由是它们**描述现状**。设计稿**故意不进**:把 4 份设计稿一起扫,
> **21 条命中里只有 4 条是真问题**(见下),其余是:
>
> | 命中长什么样 | 条数 | 为什么不查 |
> |---|---|---|
> | 还没建的计划(pkg/shorturl、cmd/cache/scanner/main.go、docs/api/config-full.md …) | 9 | 在设计稿里,"路径不存在"意味着**还没实现**,不是指路牌烂了 |
> | 第三方库内部路径(hertz 的 pkg/app/context.go、pkg/route/engine.go、pkg/app/fs.go) | 3 | 本仓的 `pkg/` 指 `server/internal/pkg/`,两者同名不同物 |
> | 订正表 / 旧写法里**故意引用**的路径(技术方案 §4.1 那张表的"树里写的"那一列) | 4 | 不写出来就没法对照;那一列写的就是"错的原样" |
> | 另一个项目的路径 | 1 | 指参考项目,不是本仓 |
>
> ⇒ 剩下 **3 条真错**已经顺手修了(`docs/文件系统模块设计方案V1.0.md`:
> `oss.NewObjectName` 的实现文件写错了、web/src/api/permissions.ts 应为
> `web/src/constants/permissions.ts`、cmd/server/router.go 应为
> `internal/api/router/router.go`)。⚠️ **设计稿没有机械守门网** —— 这三处
> 是靠人读出来的,以后还是得靠人读。
>
> ⚠️ **清单里的反引号路径现在带承诺**:加反引号 = 承诺它存在。
> 想提一个还没建的路径,**别加反引号**(本块里那些路径就是故意不加的)。
>
> **M130-4**:`server/README.md` §7 那个指向 `deploy/aliyun/README.md` 的链接是断的 —— **已删**(commit `037e2f8`,2026-10-17 改走 systemd 单机时删的),链接一直指着空气。已改指现役的 unit / 备份脚本,目录树里那行 deploy/aliyun/ 一并订正(补上一直缺的 deploy/scripts 与 deploy/prometheus)。
>
> **这次没做**(留档):① 守卫**不查代码块里的路径** —— 目录树写在 ``` 围栏里,
> 不是反引号段,守卫看不见(`server/README.md` 目录树那行 deploy/aliyun/
> 就是这么烂掉的,是人读出来的,不是守卫抓的);② **不查"文档说它已删除,
> 但它还在"**那种谎(判不准,见上);③ **设计稿不进扫描集**(M130-3),
> 它们里面的真错只能靠人读;④ M127 记下的 **3 处假绿用例**
> (`identitySwitcher` / `CronInput` / `user`)**仍未修** —— 那是前端,跟本轮无关;
> ⑤ 技术方案 §6.6.5「12 大类配置项定义」那张表里,aliyun.oss 那一段还在写
> **双桶**(`aliyun.oss.bucket_private` / `aliyun.oss.bucket_public`)——
> M80 已经改成单桶,实际 seed 里只有 `aliyun.oss.bucket` 一个桶键
> (另有 `provider` / `upload_token_ttl`)。⚠️ 那张表是**设计期目录**
> (12 大类约 60 个键,当前 seed 只有 16 个),整体重校是另一件事,M130 不碰 ——
> 但要知道它已经和 M80 的模型不一致了。

---

> **M131 已落地(2026-10-31)**:重试原语的一处**假绿用例** ——
> 断言跟 full jitter 的语义打架(约 30% 概率必红)
>
> **怎么发现的**:M130 跑全量 race 时 `internal/pkg/retryx` 偶发红(三次全量跑里红了
> 两次)。单独复跑那个用例:**10 次红 3 次** —— 不是"偶发",是一条**约 30% 概率
> 必红**的用例。
>
> **根因**:`retryx.Do` 的退避是 **full jitter**(`wait = uniform(0, backoff)`,
> 见 `pkg/retryx/retryx.go` 的 `jitter`),所以"deadline 之前能重试几次"本身是
> **随机**的 —— 抽到接近 0 就连着转,50ms 的 deadline 里转 4 次以上是常事。
> 而旧用例断言的是 `calls > 3` 就红 ⇒ **它守的不是行为,是运气**。
>
> ⇒ 一个 30% 概率红的守卫 = 没有守卫(跑的人只会学会忽略它,或者把它当"环境问题"
> 跳过 —— 这正是 M121 记的"满载假红"那类坑的后端版本)。
>
> **改成三个确定性断言**(每个都不依赖随机数):
>
> | 子用例 | 断言 | 守的是哪一处 |
> |---|---|---|
> | A 已取消的 ctx | fn **一次都不调**;错误链带 `context.Canceled` | 循环开头就查 ctx(实现的第①处检查) |
> | B fn 里取消 | 只调 1 次;错误链带 `context.Canceled` | 等待被 ctx 打断(第⑤处) |
> | C deadline 到点 | 错误链带 `context.DeadlineExceeded`;**没把 MaxAttempts 用光** | 停的原因是 ctx 到点,不是撞了次数上限 |
>
> **B / C 里"错误链带 ctx 错"才是强断言**:实现若无视 ctx,循环会一路重试到
> MaxAttempts(100 次)才返回 **fn 的原始错误** —— 那条错误链里**没有** ctx 错
> ⇒ 断言变红(而不是"跑得慢一点"这种含糊的失败)。
>
> **三组可逆探针**(拆掉实现看测试会不会红):
>
> | 探针 | 实测 |
> |---|---|
> | 把实现里的 ctx 检查**整个摘掉**(第①处不查 + 第⑤处不等 ctx) | ✅ **三个子用例全红**,且各自报出自己的那条断言 |
> | 只摘第①处(循环开头不查 ctx) | ✅ 只有 A 红(B / C 由第⑤处兜住) |
> | 只摘第⑤处(等待不再被 ctx 打断) | ⚪ **绿** —— 这是**正确**的行为:第①处仍在下一轮开头拦住循环(只多跑一次极快的空转)。⇒ 第⑤处的提前返回是**优化**,不是正确性依赖 —— 这条"探针没红"本身就是信息,记在这儿免得后来人以为漏了探针 |
>
> **验证**:新用例 **50 次连跑全绿**(`-race -count=50`);整包 50 次全绿;
> `go build` / `go vet` / `golangci-lint ./internal/pkg/retryx/...` 全绿;`gofmt` 无新增。
>
> **这次没做**(留档):① **没给 jitter 加注入口** —— 让测试能定住随机数是更彻底的
> 做法,但那要改 `Opts` 的公开 API(现在只有 `OnRetry` / `Logger` 两个注入口),
> 而本轮这条用例已经能确定性地守住行为 ⇒ 不动公开 API;② `Do` 里 `OnRetry` 在
> ctx 已经取消时**仍会被调一次**(先埋点再进 select)—— 那是一条多余的 warn 日志,
> 不是 bug(下一轮开头就停了),不动;③ 全仓可能还有别的"跟随机数打架"的用例 ——
> 本轮只修了跑出来的那一条,**没做全仓普查**(那是另一件事)。

---

> **M132 已落地(2026-10-31)**:M127 记的三处 web 假绿 —— **"等请求"当锚点** + **"不存在"查的是文案不是那东西本身**
>
> **范围**:`web/` 三个文件,各一处用例 ——
> [identitySwitcher.test.tsx:175](../../web/src/components/layout/__tests__/identitySwitcher.test.tsx#L175)、
> [CronInput.test.tsx:118](../../web/src/pages/system/job/__tests__/CronInput.test.tsx#L118)、
> [user/__tests__/index.test.tsx:246](../../web/src/pages/system/user/__tests__/index.test.tsx#L246)。
> 只动**测试**,没动实现(探针期间临时改过实现,已全部 `git checkout --` 还原)。
>
> **⚠️ 先纠正 M127 的一个判断:那三处旧锚点,在本仓环境里其实**抓得住**同类错误。**
>
> M127 的说法是"只等'接口被调了' ⇒ 组件还没渲染 ⇒ 断言'弹窗不存在'自然成立 ⇒ 白绿"。
> 我用**可逆探针**实测了这条(把实现改坏,看旧锚点的用例会不会红):
>
> | 探针 | 旧锚点(`等接口被调了`) | 新锚点(`等正向提示上屏`) |
> |---|---|---|
> | identitySwitcher:切完也弹短信窗 | ✅ **红** | ✅ 红 |
> | CronInput:描述算不出来时兜一句"大概是…" | ✅ **红** | ✅ 红 |
> | user:详情失败也照样开弹窗 | ✅ **红** | ✅ 红 |
>
> **为什么旧锚点没漏**:@testing-library/react v16 的 `asyncWrapper` 在每次
> `waitFor` / `findByText` 之后都会 `await new Promise(r => setTimeout(r, 0))`
> (见 `node_modules/.pnpm/@testing-library+react@16.3*/dist/pure.js`),
> **微任务队列被排空** ⇒ "请求发出"这个锚点其实已经等到了"响应被处理完、
> 那一轮渲染也落地了"。⇒ **M127 那条"旧锚点必然白绿"的结论,在 RTL v16 下不成立**
> (至少对这三条而言)。⇒ 这条结论得记下来,免得后来人照着它去改一堆**并不需要改**的用例。
>
> **但真洞还是挖到了两个,而且比 M127 说的更隐蔽:**
>
> **① 夹具被 `restoreMocks: true` 抹掉 ⇒ "切换成功"那条路整条没被测到**
> (`identitySwitcher.test.tsx`)
>
> `vitest.config.ts` 开了 `restoreMocks: true` ⇒ 每个用例前所有 mock 都被 reset。
> 而 `resourceApi.accessible` 的实现是写在 **`vi.mock` 工厂里**的
> (`.mockResolvedValue([])`)⇒ **每个用例开始时它就被抹成返回 `undefined`**。
> 于是 `fetchAccessible()` 里 `for (const r of apiResources)` 直接抛 ⇒ 组件走进
> catch 的 **`window.location.reload()` 降级分支** ⇒ 测试里那句
> "免得走进 catch 分支"的注释**一直没生效**,"切换成功"这条路**根本没跑到过**。
> (症状:stderr 里一条 `Not implemented: navigation to another Document`。)
> ⇒ 修法:工厂里只留空壳 `vi.fn()`,实现挪进 `beforeEach`。
>
> **② 三处"不存在"查的都是**文案/字段 id**,不是"那东西本身"** ⇒ 换个措辞就漏
>
> | 用例 | 旧断言(查什么) | 新断言(查什么) |
> |---|---|---|
> | identitySwitcher | `queryByText('切换高敏感身份')` —— 某句**标题** | `queryByRole('dialog')` —— **有没有弹窗** |
> | user | `getElementById('roles')` —— 弹窗里**某个字段** | `queryByRole('dialog')` —— **有没有弹窗** |
> | CronInput | 查两句**具体文案**在不在 | 查**描述位上有没有节点**(根节点的直接子节点里带 `ant-typography` 的) |
>
> `role="dialog"` 是 rc-dialog 的 Panel / rc-drawer 的 DrawerPanel 自己挂的
> (`node_modules/.pnpm/rc-dialog@9.6.0/.../Dialog/Content/Panel.js:135`、
> `rc-drawer@7.3.0/.../DrawerPanel.js:30`),**跟文案无关** ⇒ 换措辞、换 i18n key
> 都漏不掉。CronInput 那条是**结构性**断言:描述位是根节点下的
> `Typography.Text`(span.ant-typography);「接下来 5 次」那句也是 Typography.Text,
> 但它**套在 Spin 容器里**,不是根节点的直接子节点 ⇒ 按"根节点直接子节点"筛能把它排除。
>
> **三组可逆探针**(改实现 → 看断言会不会红):
>
> | 探针(改的是实现) | 旧断言 | 新断言 |
> |---|---|---|
> | identitySwitcher:切完后弹一个**标题不同**的确认框 | 🟢 **绿(假绿!)** | ✅ 红(`expected <div role="dialog"> to be null`) |
> | user:详情失败也开弹窗、且**不渲染角色字段**(降级只让用户改昵称) | 🟢 **绿(假绿!)** | ✅ 红(`expected <div role="dialog"> to be null`) |
> | CronInput:描述算不出来时兜一句**措辞不同**的猜测 | 🟢 **绿(假绿!)** | ✅ 红(`expected [ <span> ] to have a length of +0 but got 1`) |
>
> ⇒ **三个探针都是"旧断言绿着、新断言红着"** —— 这才是这三处真正的假绿:
> **"不存在"查的是某句文案/某个字段,而不是那件事本身。**
> (第一版探针在 user 那条上还误伤了"新增用户"用例 —— 探针条件写宽了,
> `editing.detail` 在新增态也是 undefined ⇒ 角色字段被一起藏了。
> 收紧成 `editing.detail || !isEdit` 之后只剩目标用例红。**探针自己也可能错。**)
>
> **验证**:三个文件 **25/25 全绿**(实现还原后);探针期间只有目标用例红;
> `npx eslint` 三个文件 0 warning(本仓 lint 是 `--max-warnings 0`)。
>
> **这次没做**(留档):① **没做全仓普查** —— "查文案而不是查那东西本身"这个模式
> 在别的用例里肯定还有,但本轮只修 M127 点名的这三处;② **没给"锚点"这件事立规矩**
> —— M127 推的"先等正向锚点再断言不存在"这条规矩本身是对的(明确、不依赖 RTL 的
> 排空时机),但**它不是这三条红的原因**,别把它当成本轮的修复内容讲;
> ③ 三处用例的**文件名/用例名没改**,清单里 M127 提到的位置仍对得上。

---

> **M133 已落地(2026-10-31)**:文件系统模块的 `info.json` —— **契约一直在,实现从来没兑现过**
>
> **症状**:`GET /folders/:id` 返的 `info_json` 字段**恒为空串**。
> 设计文档 §8.3 那张表里写的是「详情 + `info.json` 内容」——
> **契约在文档里躺了很久,实现一行没写**。
>
> **根因是两个洞叠在一起,症状是同一个:**
>
> | # | 洞 | 位置 | 单独修它够不够 |
> |---|---|---|---|
> | ① | **读不回来** —— `readFolderInfo` type-assert 的是 `Get(ctx, key) ([]byte, error)`,而**三家 provider 一个都没实现**(mock 的 `Get` 签名还不一样,是 `Get(key) ([]byte, bool)`)⇒ 断言必然失败 ⇒ 直接返回 `""` | [folder_info.go:133](../../server/internal/service/folder_info.go#L133) | ❌ 压根没有 info.json 可读 |
> | ② | **写不进去** —— 两条注册路径(`Receive` 服务端中转 / `Register` OSS 直传)**从来不调 `syncFolderInfo`**(只有 `SetCover` / 删除 / 分片合并调)⇒ 文件夹里压根就没有 info.json | [file_impl.go:164](../../server/internal/service/file_impl.go#L164)、[file_impl.go:261](../../server/internal/service/file_impl.go#L261) | ❌ 读回来还是空串 |
>
> ⚠️ **只修一个,现象完全不变** —— 这是本轮最容易白干的地方。
> ⇒ 所以下面每条用例都**同时**盯住「存储里真的有」和「读回来真的有」。
>
> **新增可选接口 `oss.MetaReader`**([advanced.go:240](../../server/internal/pkg/oss/advanced.go#L240)):
>
> ```go
> type MetaReader interface {
>     Get(ctx context.Context, key string) ([]byte, error)
> }
> ```
>
> **为什么不进 `Provider` 主接口**:跟 `MultipartProvider` / `ContentProvider` 同源 ——
> **能力是 provider 之间的差异,不是接口的公共义务**。`Get` 的语义是「读回**小**对象」
> (8MB 上限),进主接口等于向全仓宣告「读任意对象的字节」是标准能力,
> 以后有人拿它读 500MB 视频,就是在应用服务器上 OOM。
> 真要读大文件走 `ContentProvider.Open` 或直链。
>
> **三家都实现了**,各有各的闸门:
>
> | provider | 位置 | 关键点 |
> |---|---|---|
> | local | [local.go:396](../../server/internal/pkg/oss/local/local.go#L396) | 超限**拒**(不是截断)+ 报错说清「要读大文件请用 Open」;不存在返 `oss.ErrNotFound` |
> | aliyun | [aliyun.go:433](../../server/internal/pkg/oss/aliyun/aliyun.go#L433) | **限长读**(多读 1 字节判超限,而不是先整个拉进内存再判 —— 那样上限只是「事后报个错」);`SafeKey` 闸门在**联网之前** |
> | mock | [mock.go:207](../../server/internal/pkg/oss/mock.go#L207) | 签名从 `Get(key) ([]byte, bool)` 改成 `Get(ctx, key) ([]byte, error)`(旧签名全仓零调用方) |
>
> **三条编译期实现检查**(少一条,provider 漂移就没人知道):
> [local.go:994](../../server/internal/pkg/oss/local/local.go#L994)、
> [aliyun.go:551](../../server/internal/pkg/oss/aliyun/aliyun.go#L551)、
> [mock.go:236](../../server/internal/pkg/oss/mock.go#L236)。
>
> **⚠️ 调用方必须优雅降级**:`readFolderInfo` 的 type-assert **失败就返回空串**,
> 不是报错 —— 新接一个 provider 忘了实现 `MetaReader`,顶多是 `info_json` 字段空着,
> 不该整个 `GET /folders/:id` 500。
>
> **可逆探针**(拆掉实现看测试会不会红;探针跑完全部还原,`cmp` 逐字节校验过):
>
> | 探针(改的是实现) | 变红的用例 | 该红的/不该红的 |
> |---|---|---|
> | A:让 `readFolderInfo` 的断言落空(模拟「provider 没实现 MetaReader」) | `TestFolderInfo_ReadableAfterUpload`、`RebuiltNotAppended`、`RebuiltAfterDirectRegister`、`ShrinksAfterDelete` | `TestFolderDetail_ProviderWithoutMetaReaderDoesNotFail` **保持绿** —— 它守的正是「降级不许炸」 |
> | B:拆掉 `Receive` 里的 `syncFolderInfo` | `ReadableAfterUpload`、`RebuiltNotAppended` | `RebuiltAfterDirectRegister`(走 Register,不受影响)、`ShrinksAfterDelete`(走删除路径,本来就有) |
> | C:拆掉 `Register` 里的 `syncFolderInfo` | `RebuiltAfterDirectRegister` **只有它** | 其余三条全绿 ⇒ 两条注册路径各自被独立守住 |
> | D:拆掉 local 的 `Get` | local 包 `TestGet_RoundTrip`、`TestGet_AtLimitIsAllowed`;service 四条全红 | local 的 `NotFound` / `RejectsOversize` / `RejectsUnsafeKey` 仍绿(它们守的是另外三件事) |
> | E:拆掉 aliyun 的 `SafeKey` 闸门 | `TestGet_RejectsUnsafeKeyBeforeNetwork` | —— |
>
> ⚠️ **第一版探针 B/C 打错了地方**:`patch()` 的 `count` 参数是「替换前 N 处」而不是
> 「替换第 N 处」⇒ 探针 C 一口气拆了两条注册路径,归因失真。
> 另外第一版探针 E 的锚点命中的是 `Put` 而不是 `Get`(同一段代码在文件里出现了三次),
> 于是**测试一直绿着**——一个「拆错了地方」的探针会给出「拆了也没事」的假结论。
> 两处都改成唯一锚点后才拿到上表的归因。⇒ **探针自己也可能错,而且错了会伪装成
> 「这条实现其实不需要测试」。**
>
> **验证**:`go build ./...` 通过;`go test ./internal/pkg/oss/...` 三个包全绿;
> service 包 5 条新用例全绿;`gofmt -s -l`(先归一化到 LF 再比)对 10 个改动文件**全干净**。
>
> **这次没做**(留档):① **没在"建文件夹"时写 info.json** —— 设计文档 §2.3 D3 定的
> 生成时机是「文件注册、删除、封面变更之后各调一次」,顺手加上会让
> `TestReceive_QuotaExceededRejected` 那条(断言 `res/` 底下没有任何对象)红掉;
> ② **没给 `readFolderInfo` 的空串加"到底为什么空"的诊断字段** —— 现在排障只能
> 去看日志,接口层面看不出是「没生成」还是「provider 不支持」;
> ③ **aliyun 的 `Get` 没有真 OSS 往返测试**(要真 bucket 真 AK/SK),
> 只测了「联网之前就能判掉的那一段」。

---

> **M134 已落地(2026-10-31)**:文件列表的「表格 / 缩略图」双视图 —— **表格看不出文件到底是什么**
>
> **缺口**:文件列表里图纸、照片、PDF、压缩包混在一起,表格视图只有文件名和大小,
> 用户**看不出这个文件是什么**(文件夹页有封面缩略图,文件列表没有)。
>
> **先说清楚:后端早就具备条件,是前端没做。** 单桶单树模型(M80)之后
> **每个文件都有一条稳定直链**(不带签名、不带过期),而拼直链只是**字符串拼接**,
> 不是签名。之前"列表不填 url,免得一条列表签 N 个 URL"那个理由
> **在 M80 之后已经不成立**(那时的直链是带签名、带过期的)。
> ⇒ M134 把 `FileView.URL` 填进列表返回([file_impl.go](../../server/internal/service/file_impl.go)),
> 跟 `folderService.coverURL` 同一个做法。
>
> ⚠️ **拿不到就留空,不报错** —— 签不出来(对象没了 / provider 没配挂载点)时
> `url` 是空串,前端据此画占位图标。**一条 URL 拼不出来不许把整页列表带崩。**
>
> **前端怎么做的**:
>
> | 件 | 落点 | 说明 |
> |---|---|---|
> | 视图开关 | [list.tsx](../../web/src/pages/file/list.tsx) | `Radio.Group optionType="button"`(本仓**筛选器**统一是 Select,两态开关的现成写法是 Radio.Group —— 见 dashboard 的时间范围) |
> | 缩略图网格 | [FileGrid.tsx](../../web/src/pages/file/FileGrid.tsx) | **自写 antd 卡片**,不引 `@ant-design/x`(那个包要的是"增强",这里要的只是"一个卡片网格") |
> | 复用外壳 | [ManagementTable.tsx](../../web/src/components/common/ManagementTable.tsx) | 加了一个**通用** `body` 槽位 —— 传了就替换表格本体,页头 / 搜索区 / 工具栏不变 |
> | 视图模式记档 | [app.ts](../../web/src/constants/app.ts) | `STORAGE_KEYS.FILE_LIST_VIEW`,走 `utils/storage.ts` 的信封格式 |
>
> ⚠️ **为什么不复制一份搜索区**:两种视图共用同一套搜索区和工具栏(上传按钮、
>   配额、文件夹筛选都在里面)。复制一份 = 两份上传逻辑、两份筛选状态,迟早对不上。
>   ⇒ 给 `ManagementTable` 开一个 `body` 槽位(它被 17 个页面共用,
>   槽位里**不许写任何页面专属的东西**)。
>
> ⚠️ **`body` 传了之后分页器要自己渲染** —— 表格那份分页器长在 `<Table>` 里,
>   换掉表格它就没了(错误态同理:缩略图自己渲染降级占位)。
>
> ⚠️ **`ManagementTable` 的测高 effect 依赖里必须带 `hasBody`** ——
>   页面可能**先渲染网格、再切回表格**(视图模式是记在 localStorage 里的,
>   上次停在网格模式就是这个顺序)。依赖不带上它,表格是后挂上去的,
>   测高那段 effect 不会再跑 ⇒ `scroll.y` 永远停在初始的 400 ⇒ **表格不撑满卡片**。
>
> **两种视图的"操作"是同一份**:行操作那段以前写在列的 `render` 里,
> 缩略图拿不到 ⇒ 抽成 `actionsFor(file)` 工厂,两边共用。
> 否则会出现「表格里"设为封面"能点、缩略图里点不了」= 用户看着就是 bug。
>
> **顺带**:这一页原来有一份**本地拷贝**的 `formatSize`,现在改用仓库里已有的
> `formatNumber.fileSize` —— 两份拷贝迟早对不上(0 字节一边显示 "-" 一边显示 "0 B")。
>
> **新用例 13 条**([grid.test.tsx](../../web/src/pages/file/__tests__/grid.test.tsx)):
>
> | # | 守什么 | 反面断言 |
> |---|---|---|
> | 1 | 默认是表格视图(缩略图是"切过去"的,不是默认态) | —— |
> | 2 | 切过去后一个文件一张卡片 | 表格不能还在(两个叠在一起) |
> | 3 | 图片文件渲染缩略图,`src` 就是列表里那条直链 | —— |
> | 4 | **不许逐条调 `getFileUrl`**(一页就是 N 个往返) | —— |
> | 5 | 非图片文件不渲染 `<img>` | —— |
> | 6 | 图片但没有直链 → 不渲染 `<img>`,不画碎图 | —— |
> | 7 | 直链加载失败 → 换成占位图标 | —— |
> | 8 | 缩略图里的「删除」走**同一个** `deleteFile` | —— |
> | 9 | 视图模式记下来了(下次进来还是缩略图) | —— |
> | 10 | localStorage 里已经是缩略图模式 → 首屏就是缩略图 | —— |
> | 11 | 缩略图视图也有分页器,翻页会重新拉数据 | —— |
> | 12 | 切回表格视图,表格还在(不是切坏了) | —— |
> | 13 | **直链变了之后,加载失败的记号要清掉** | —— |
>
> **可逆探针 5 个**(跑完全部还原):
>
> | 探针(改的是实现) | 变红的用例 |
> |---|---|
> | A:删掉 `<img>` 的 `onError` | 7(直链加载失败) |
> | B:`body` 恒置空(缩略图压根不渲染) | 2(切到缩略图视图后) |
> | C:缩略图不给操作按钮 | 8(删除) |
> | D:缩略图的 `src` 不再用列表里的直链 | 3(src 就是那条直链) |
> | E:视图模式不落盘 | 9(记下来了) |
>
> ⚠️ **探针自己踩过的坑**:探针脚本第一版按 `\n` 匹配锚点,而 `list.tsx` 是
> **CRLF** 文件 ⇒ C / E 两个探针的锚点**根本没命中**(断言 `count == 0` 直接抛错,
> 没有"假装拆掉了")。改成按文件实际换行符匹配之后才拿到上表的归因。
> ⇒ **"探针没报错"不等于"探针生效了"** —— 锚点没命中时它可能什么都没拆。
>
> **这次没做**(留档):① **没做"点开大图"** —— 只做了静态网格;
> ② **没有虚拟滚动**(一页 50 张 `<img>` 一次性建,靠 `loading="lazy"` 交给浏览器);
> ③ **没给缩略图视图加排序**(表格视图也没有)。

---

> **M135 已落地(2026-10-31)**:文件列表页搜索区的「已用多少空间」—— **语义错配,不是刷新漏了**
>
> **用户报的那句**:
> > 「文件列表界面 search-section 里面的已用多少空间这个数值,
> > 不会跟随切换文件夹而变化。」
>
> **根因不是"忘了刷新"**,是**两个口径的数摆在了一起**:
>
> | # | 事实 | 位置 |
> |---|---|---|
> | ① | 那个数是 `quota.used_bytes`(`sys_user.storage_used_bytes`,**账号总用量**) —— 它**按定义**就不跟随文件夹 | [list.tsx:482](../../web/src/pages/file/list.tsx#L482) |
> | ② | 它就摆在**文件夹筛选框旁边**,而且连"已用"两个字都没有 —— 用户理所当然以为它说的是**当前文件夹** | [list.tsx:474-498](../../web/src/pages/file/list.tsx#L474-L498) |
> | ③ | 连带一条真 bug:文件夹列表(`total_size` / `file_count`)**上传/删除后不重拉** ⇒ 下拉框里的 `(N)` 和任何按它算的数都停在旧值 | [list.tsx:133](../../web/src/pages/file/list.tsx#L133)、[list.tsx:284](../../web/src/pages/file/list.tsx#L284)、[list.tsx:546](../../web/src/pages/file/list.tsx#L546) |
>
> ⚠️ **修法不是"让它刷新"** —— 账号总用量刷成文件夹的数,配额就管不住了。
> ⇒ **拆成两个数,各说各的**:
>
> | 位置 | 显示 | 口径 |
> |---|---|---|
> | 搜索区(紧挨着文件夹筛选) | **本文件夹 12.3 MB · 5 个文件** | 该文件夹的 `total_size` / `file_count` |
> | 工具栏 | **总用量 1.2 GB**(或配额环) | 账号级,永远不变 |
>
> ⚠️ **口径必须与文件列表一致**:后端 `folderFileStats` 统计的是「**直接**放在这个文件夹里
> 的文件」(不递归子文件夹),而文件列表按 `folder_id` 过滤也是同一口径 ⇒
> 标签上那个数 = 表格里那些文件的总大小,**不会一个说 12 MB 一个说 30 MB**。
>
> **顺带修的**:文件夹列表在**上传完 / 删完**之后要重拉(③)—— 不重拉的话
> 标签上的大小是操作之前的值(文件已经传上去了,数字还没变)。
>
> **新用例 7 条**([list.test.tsx](../../web/src/pages/file/__tests__/list.test.tsx)):
>
> | # | 守什么 | 反面断言 |
> |---|---|---|
> | 1 | 没选文件夹时**不显示**「本文件夹」标签 | —— |
> | 2 | 选中文件夹后显示的是**那个文件夹**的大小和个数 | 不能是账号总用量(100.0 MB) |
> | 3 | **切到另一个文件夹,数字跟着变** ← 用户报的就是这句 | 不能停在上一个文件夹的数 |
> | 4 | 工具栏那个是**账号级**,不跟随文件夹 | —— |
> | 5 | 文件夹不在下拉前 200 条里(URL 直接带 `folder_id`)也能显示 | 得单独拉一次详情 |
> | 6 | 文件夹拉不到(被删了)**不显示标签**,也不把页面搞挂 | 表格还在 |
> | 7 | 删完文件要**重拉文件夹列表** | —— |
>
> **可逆探针 3 个**(脚本 `C:\tmp\m135probe\probe.py`,跑完还原、校验字节一致):
>
> | 探针 | 拆什么 | 红的用例 |
> |---|---|---|
> | A | 作用域标签改用账号总用量(= 用户报的那个症状) | ② ③ ⑤ —— 正好是三条"文件夹级"的 |
> | B | 整个作用域标签不渲染 | ② ③ ④ ⑤ ⑦ —— 凡是依赖这个标签的 |
> | C | 删完文件不重拉文件夹列表 | ⑦ 一条 |
>
> ⚠️ **探针脚本踩的坑(值得记)**:用 **小写 `d:\`** 当 cwd 跑 vitest,
> `vi.mock('@/api/upload')` **整条失效**(全部用例报
> `listMock.mockResolvedValue is not a function`)——
> `vitest.config.ts` 的 root 用 `realpathSync.native(__dirname)`(大写 `D:`),
> 而 `vite.config.ts` 的别名是 `path.resolve(__dirname, 'src')`(跟着 `__dirname` 的大小写走)
> ⇒ 两边解析出的模块 id 不是同一个 ⇒ mock 不生效。
> 这跟 vitest.config.ts 里记的「双 React 根因」是**同一类**问题(路径大小写)。
> ⇒ **探针脚本里 cwd 必须写盘符大写的 `D:\...`**。
>
> ⚠️ **写完先跑 eslint,别等 commit 才看**:本仓 lint 是 `--max-warnings 0`,
> 而我第一版写的 `size == null`(想同时判 `null` 和 `undefined`)**直接被
> `eqeqeq` 判红**。改成「缺的那份用 `undefined` 表示」——
> `total_size` / `file_count` 在类型上本来就是 `number`(不是 `number | null`),
> 真的"没有"只可能是两个分支都没命中 ⇒ `undefined` 足够表达,不需要 `== null`。
> ⇒ **探针跑绿 ≠ 可以提交**:探针只验行为,lint 验的是另一层。
>
> **这次没做**(留档):① **没把"本文件夹"做成可点开的一级目录树** ——
> 文件夹的层级浏览在**文件夹页**(`/file/folders`)已经有一套,列表页再搞一套会两处打架;
> ② **没做"按当前表格内容算用量"** —— 表格是分页的,按"表格里那些行"算会随着翻页变,
> 那是第三个口径,更糊;
> ③ **文件夹占用没算子文件夹** —— 后端 `folderFileStats` 就是直接文件的和,
> 前端跟它对齐;要"含子文件夹"得改后端口径,那是另一个话题。

---

> **M136 已落地(2026-10-31)**:文件存储的**路径根段**从 `res` 改成 `rapid-admin`
> —— **改常量是三步迁移,不是改一行**
>
> **用户要求**:「另外把 path_root 从 res 改为 rapid-admin,历史数据也帮我调整一下」
>
> **为什么这不是改个默认值就完了** —— `file.path_root` **一处决定两件事**:
>
> | # | 事实 | 位置 |
> |---|---|---|
> | ① | 它是 **key 的第一段**(`{root}/{uid}/{biz}/{fid}/{对象名}`,固定 5 段) | [oss/path.go](../../server/internal/pkg/oss/path.go) |
> | ② | 它同时是**静态挂载点** —— 后端把 `<file.root>/rapid-admin` 整棵树挂在 `/rapid-admin` | [staticfs.go:59-85](../../server/internal/api/router/staticfs.go#L59-L85) |
>
> ⇒ 改它 = **磁盘目录名、库里的 key、URL 前缀三样一起动**。
> 只改代码不动存量 ⇒ 每个历史文件 404,而且 **直链不经后端、不查库**,
> 后端日志里**一次请求都没有**(浏览器那边表现是"图裂了")。
>
> **两个默认值不许漂移**(新增用例盯着):
>
> | 默认值 | 在哪 | 兜底的是 |
> |---|---|---|
> | `oss.DefaultRoot` | [oss/path.go](../../server/internal/pkg/oss/path.go) | oss 层(`BuildKey` / `ParseKey` 拿到空 root 时) |
> | `v.SetDefault("file.path_root", ...)` | [config.go](../../server/internal/config/config.go) | 配置层(yaml 漏写这一项时) |
>
> 两者漂移 ⇒ 「用兜底值的路径」和「用配置值的路径」**长成两棵树**,症状是全站 404。
> ⇒ 新增 `TestPathRootDefault_MatchesOssDefault`
> ([file_test.go](../../server/internal/config/file_test.go)):加载一份**不含
> `file.path_root`** 的最小 yaml,断言取到的值 == `oss.DefaultRoot`。
>
> **存量怎么迁的**:本地运行时数据(`/server/data/`)是 **gitignored** 的
> (`.gitignore:141` 的 `/server/data`)⇒ 新克隆的机器根本不会带旧的 `res` key
> ⇒ **不需要写迁移工具**,改库 + 改目录名就够了:
>
> | 步骤 | 动作 | 结果 |
> |---|---|---|
> | 1 | `data/files/res` → `data/files/rapid-admin` | 4 个文件(头像 ×1 / 户型图 ×1 / 视频 ×1 / info.json ×1) |
> | 2 | 库里的 5 行 key 改前缀 | `sys_user.avatar` ×1、`sys_file.file_key` ×3、`sys_folder.cover_key` ×1 |
> | 3 | 全库 TEXT 列重扫 `res/` 引用 | 严格匹配 **0**;宽松匹配 40 处全是无关的(`resource:list` / `reset_password` / `in_progress` …) |
>
> ⚠️ **我一开始走错了方向**:先写了一套完整迁移工具(`oss.PrefixCopier` + factory 包 +
> `cmd/filemigrate`(已删除)+ `internal/migrate` 里那个临时 DB 重写器(同批删除),3 步搬迁)。用户一句话点破:
> > 「就这里面几个文件,没有 oss 里面的文件,你迁移就没必要写这么多代码,
> > 直接数据库里面处理一下,然后把这个文件夹重命名就行了」
>
> ⇒ **工具全部删掉**(一行不留)。教训:本地运行时数据 + gitignore 的组合下,
> "写个迁移器"是**默认反应**,但要先问一句「存量到底在哪儿、有多少」。
>
> ⚠️ **`info.json` 是个坑**:它是**生成物**(DB 才是真相源),但里面存着
> `cover_key` 的**快照** ⇒ 目录改名后它里面的 `cover_key` 还指着 `res/...`,
> 而 `GET /folders/:id` 把原文**直接吐出来** ⇒ 用户看到的是一个死链接。
> 已一起改掉(下一次文件变动也会按 DB 自动重建)。
>
> **顺带修的两处「改名之后才露」的测试问题**(不是改名引起的,是改名**暴露**的):
>
> | # | 问题 | 后果 | 修法 |
> |---|---|---|---|
> | ① | `newFileTestProv` 里 `opt.Root = "rapid-admin"` 而 `URLBase = "/res"` —— 生产里 `URLBase` 恒等于 `"/" + path_root` | key 是 `rapid-admin/...` 而直链是 `/res/...`,挂载点 `/rapid-admin` **永远 404**,且直链不过后端 ⇒ 线上"图裂了、日志零请求" | 夹具改成 `URLBase: "/" + opt.Root` |
> | ② | `countStoredObjects` 前缀写死 `"res/"` | 默认值一改就数到 **0** 个对象,而"删完是 0"那条断言 **0 == 0 假绿** | 改成跟 `oss.DefaultRoot` 走 |
>
> **验证**(起真服务跑,不是"看代码觉得对"):
>
> | 检查 | 结果 |
> |---|---|
> | 静态挂载日志 | `mount=/rapid-admin  root=<data>/files/rapid-admin  strip_segments=1` |
> | `GET /rapid-admin/.../3s.mp4` | 200 · `video/mp4` · 994992 字节(与磁盘一致) |
> | `GET /rapid-admin/.../info.json` | 200 · 里面的 `cover_key` 已是 `rapid-admin/...` |
> | `GET /res/.../info.json`(老前缀) | **404** |
> | `GET /rapid-admin/v1/auth/me` | `avatar = /rapid-admin/94883951113467590/avatar/...` |
> | `GET /rapid-admin/v1/biz/folders/:id` | `cover_key = rapid-admin/...`、`cover_url = /rapid-admin/...` |
> | `GET /rapid-admin/v1/biz/files/:id/url` | `url = /rapid-admin/...` |
> | 头像 / 封面直链 | 各 200(image/jpeg,16959 / 137756 字节) |
>
> **可逆探针 1 个**:把 `oss.DefaultRoot` 改回 `"res"` ⇒
> `TestPathRootDefault_MatchesOssDefault` **红**,消息就是预期的那句
> ("两个默认值不许漂移")—— 证明这条网真的在盯,不是摆设。
>
> **前端侧**:vite 的代理前缀 `/rapid-admin` 必须跟后端 `file.path_root` 默认值一致 ——
> [static-mount-proxy.test.ts](../../web/src/api/__tests__/static-mount-proxy.test.ts)
> 直接从 `config.go` 里 grep 出 `SetDefault("file.path_root", "...")` 的字面量,
> 两边对不上当场红(前端抄一份 = 两份迟早漂移,而漂移的方向永远是"前端没跟着改"
> ⇒ 静默裂图)。
>
> ⚠️ **web 侧 5 个用例文件里的 `/res/...` 夹具也一起改了** ——
> 那些是**模拟直链的不透明串**(前端只当字符串渲染 / 判 `startsWith('/')` /
> 原样复制),改了行为上零影响;但留着不改,全仓就有**两个"路径根"**,
> 下一个人 grep `res` 会以为改漏了。
> (`headerAvatar` / `folder` / `list` / `avatarUpload` / 用户管理页,共 61 条用例全绿)
>
> **这次没做**(留档):① **没写通用迁移工具**(用户明确说不需要;
> 且 `/server/data` 是 gitignored 的,新克隆不带旧数据);
> ② **没给 OSS 存量对象做搬迁**(本地根本没有 OSS 数据);
> ③ **没做改名回滚预案** —— 备份 `data/dev.db.bak-20261031` 躺在本地运行时目录里,
> 不在版本库里 ⇒ **换台机器就没有它了**。

---

> **M137 已落地(2026-10-31)**:阿里云 OSS **真机跑通** —— 修了 4 个真 bug,
> 加了内网 endpoint,切 provider 的三件事齐了
>
> **用户要求**:「\server\configs\config.dev.yaml 里面我已经把 oss 的阿里云 参数填好了,
> 你要不要把 oss 跑通?我看你 oss 还有很多是 mock」
> +「oss 的配置里面 endpoint_private、endpoint 分两个,一个是内网调用 oss 更快,一个是外网更慢」
>
> **"很多是 mock"的真相**:`oss.provider: mock` 这个键**全仓没有任何代码读它**
> —— 真正决定"文件存哪"的是 `file.provider`(`local | aliyun`)。
> ⇒ 两个键讲同一件事、只有一个有人读 ⇒ **配置里写什么都零症状**,
> 而读配置的人会以为"OSS 还没接"。
> ⇒ **键已删**([oss.go](../../server/internal/config/oss.go)),
> 并在 [file_test.go](../../server/internal/config/file_test.go) 的 removedKeys 列表里
> 盯着它别再回来。
>
> **更要紧的是:OSS provider 从来没被真机验过** —— m78_test.go 末尾自己写着
> "Put / Stat / List / DeletePrefix 的联网行为没测"。于是拿真凭证跑了一遍,
> **挖出 4 个真 bug**:
>
> | # | 现象 | 真因 |
> |---|---|---|
> | ① | 直链 = `https://https://f.3dman.cn/...` | CDN 域名带 scheme,代码又写死拼 `https://%s/` |
> | ② | 不配 CDN 时直链 = `https://bucket.https://oss-cn-...` | endpoint 也带 scheme |
> | ③ | **删单个文件:返回成功,对象还在桶里** | `Delete` 对 key 做了 `url.PathEscape`,而 OSS 的 `DeleteObjects` 把 key 放在 **XML 请求体**里 ⇒ OSS 收到一个**不存在的 key** ⇒ "删不存在的 key"是幂等成功 |
> | ④ | 直链里中文/空格没编码 | `SignDownloadURL` 直接拼 key |
>
> ⚠️ **③ 最阴,而且是"只看返回值"的测试永远抓不到的**:
> DB 软删了、列表里看不见了、配额也退了,但**对象还在桶里继续占存储费**。
> 同仓的 `DeletePrefix` 用的是原始 key(所以它是对的)——
> **两个删除路径行为不一致,就是"删单个删不掉、删整棵能删干净"的线索**。
>
> **回归网(必须抓"真正发出去的那一行")**:
> [wire_test.go](../../server/internal/pkg/oss/aliyun/wire_test.go) 用 `httptest` 起一个
> 假 OSS 端点(SDK 的 endpoint 可以指向任意 http 地址,IP 走 path-style),
> 把 SDK **真正发出去的 XML 请求体**抓下来断言 key 原样、没有 `%2F`。
> ⚠️ **可逆探针**:把 `Delete` 改回 `url.PathEscape(key)` ⇒
> `TestDelete_SendsRawKeyInXMLBody` + `TestDelete_MultipleKeysRaw` **两条都红**,
> 消息就是请求体里那串 `%2F` —— 证明这条网真的在盯。
>
> **内网 / 外网两个 endpoint**(用户明确要求):
>
> | 键 | 用途 | 谁用 |
> |---|---|---|
> | `oss.endpoint` | **外网** | 只用来**拼直链**(给浏览器看) |
> | `oss.endpoint_private` | **内网** | 只给 **SDK 客户端**(同地域 VPC 内流量不走公网) |
>
> ⚠️ **直链永远用外网** —— 直链是给公网上的浏览器看的,给它 `-internal` 地址它访问不到。
> ⚠️ **判据是"连得上",不是"DNS 解析得出来"** —— 真机实测:
> `nslookup oss-cn-shenzhen-internal.aliyuncs.com` **解析成功**(100.118.78.2,
> 阿里云 VPC 内部网段,公网也能解析),但 `dial tcp 100.118.78.2:443` **超时**
> ⇒ 启动时探的是 **TCP 连通性**([aliyun.go](../../server/internal/pkg/oss/aliyun/aliyun.go)
> 的 `probeEndpointReachable`,1.5s 超时),不通自动回落外网 + 打 Warn。
> 只探 DNS 的后果是"启动日志说走内网,然后**每次 OSS 操作都等满 60 秒超时**"。
> ⚠️ SDK 的 urlMaker 在 endpoint **没写 scheme 时默认按 http** ⇒ 统一补 scheme
> (已有 scheme 不动:用户显式写 `http://` 是在说"我要明文")。
>
> **切 `file.provider` 要同时动三件事**(漏一件就出静默故障):
>
> | # | 事项 | 漏了会怎样 |
> |---|---|---|
> | ① | 配置 `file.provider: aliyun` | — |
> | ② | **本机磁盘不再挂成静态路由** | 切之前存的文件直链**全部 404**,而请求确实命中了路由 —— 症状"有的文件能开有的不能",日志里只有一条 `file static fs mounted` |
> | ③ | **存量文件搬进 OSS**(同 key) | 对象不在 OSS 上,直链打不开 |
>
> ②的实现:[staticfs.go](../../server/internal/api/router/staticfs.go) 的 `staticFSTarget`
> 按 provider 判定,[config/file.go](../../server/internal/config/file.go) 的
> `NormalizeProvider` 是**唯一**的归一处(空 = local)。
> ⚠️ 判定必须是"**不是 local 就不挂**" —— "等于 local 才挂"会在 yaml 漏写 provider 时
> 把本机磁盘树挂出去(而文件其实在 OSS 上)。
>
> ③的实测:本机存量 **3 个文件 + 1 个 `info.json`**,一次性搬过去
> (同 key 复制 + `sys_file.storage` 1→2 + `sys_user.avatar` 换成 CDN 直链)。
> ⚠️ **又是一次"写迁移器"的冲动被自己按住了** —— `/server/data/` 是 gitignored 的,
> 新克隆不带旧数据 ⇒ 一次性搬 + 改库就够,**没写任何通用迁移工具**。
>
> **验证**(起真服务 + 真桶,不是"看代码觉得对"):
>
> | 检查 | 结果 |
> |---|---|
> | 启动日志 | `内网 endpoint 连不上…已回落外网` + `file provider ready(provider=aliyun)` + `file static fs 未挂载` |
> | `POST /rapid-admin/v1/biz/files`(multipart) | 200,`file_key = rapid-admin/{uid}/house/{fid}/{name}`(5 段),`storage = 2` |
> | `GET /files/:id/url` | `https://f.3dman.cn/rapid-admin/...`(**没有** `https://https://`) |
> | 直链 GET(浏览器 UA) | **200** · `text/plain` · 内容与上传一致 |
> | 直链 GET(桶 host,不过 CDN) | **200** |
> | `DELETE /files/:id` 后再取 | **404**(桶 host 与 CDN 都是 404)—— **对象真的没了** |
> | 迁移后的存量文件 | `sys_file.storage = 2` ×3;头像 / 封面 / 视频直链各 200 |
> | `GET /folders/:id` 的 `info_json` | 从 OSS 读回原文 ✓ |
>
> ⚠️ **部署侧三个硬要求**(代码保证不了,得写在部署文档里):
> **桶必须公共读**(否则直链 403)、**CDN 必须回源到这个桶**(否则直链 404)、
> ⚠️ **CDN 侧有 UA 白名单** —— 浏览器(Chrome/Firefox/Safari/微信 webview)都 200,
> 但 **curl / Postman / 无 UA 是 302→403** ⇒ **后端读文件内容必须走 SDK(Get 接口),
> 不要去 GET 直链**。
>
> **这次没做**(留档):① **没写 OSS 迁移工具**(同 M136 的理由);
> ② **没做"OSS 请求失败自动切 endpoint"** —— 现在是启动时探一次、之后不变,
> 部署到 VPC 里就自动走内网,中途网络抖不会重新选;
> ③ **没改直链的 UA 白名单问题** —— 那是 CDN 侧配置,代码层面无解,已写进 §19.5;
> ④ **没做 OSS 侧的配额对账**(R2 那条定时任务仍只覆盖 DB 内部一致性)。

---

## 0. 怎么读这份清单

- **每条"已完成"** 必须带 `file_path:行号` 证据(可点击跳转)
- **每条"未完成"** 必须说准:**(a) 当前差什么;(b) 缺它的后果;(c) 实现成本估算**
- 状态符号:
  - ✅ 已完成
  - ⚠️ 部分完成 / 有条件可用
  - ❌ 未完成
- **计数模型(2026-10-26 M92 起,守门网按这套算)** —— 别自己另发明一套:
  - `#### ✅ 已完成(...)` 块里的**每一行**都算**已完成**
    (这些是纯证据表,行上未必有 ✅ 标记)
  - `#### ❌ 未完成(N 项)` 块里,**带 ✅ 的行算已完成**,没带的**才算还开着** ——
    N 必须等于**还开着**的行数(M-xx 落地时只在那行加了个 ✅,没把行从 ❌ 块搬走)
  - `#### ⚠️` / `#### 🔧` / `#### 📋` 块**不计数** —— 那是范围说明 / 修复记录 /
    证据表,行数跟"子项数"不是一回事(§2.10 的 API TODO 表 6 行,代表 23 个 endpoint)
  - 小节百分比 = `round(已完成 / (已完成 + 还开着) × 100)`
  - 守门网:`python scripts/check_backlog_consistency.py` —— 对不上就非零退出
- **百分比一律写成自引用格式 `**92%** (§1.2)`** —— 意思是「这个数字来自 §1.2」。
  §1 / §2 两张总览表和 §6 的「当前完整度」列**每一格都是从别处抄来的数**,
  抄的那格改了、抄来的忘了改,读的人就会看见两个不一样的数(此前 JWT 就是
  §1.2 写 92%、§6 写 90%)。**自引用把「抄来的」变成「指着的」**,抄错立刻暴露。
  范围写法:`**92%** (§2.1-§2.10 合计)` = 这几节合起来的完成度。
- 优先级:
  - **P0** = 12/15 上线阻塞,必做
  - **P1** = 12/15 之后但 Q4 必做
  - **P2** = Q1+ 长期项
- **§8 是「开放化」专项(2026-11-01 新增)** —— §1/§2 问的是"**框架能力有没有**",
  §8 问的是"**别人拿不拿得起来**"(本仓库要作为通用后台管理平台开源)。
  两者**分开计数**:§8 的子项不计入 §1/§2 的完成度。
- 本次盘点范围:
  - 后端 **14 大类**(2026-11-02 起;原盘点基线是 **11 大类 / 4498 行 / 已读 28 文件**)——
    `internal/pkg/` 下现 **52 个一级包**(非测试代码 28782 行 / 测试代码 26358 行)
  - 前端 10 大类,共 **~5800 行**(已读 41 文件)

---

## 1. 后端框架层 14 大类(总览)

> **2026-11-02(第二次更新)**:§1.13 / §1.14 是**这一轮补进来的两类** ——
> 「数据可见性(行级 + 字段级)」与「领域事件总线 / 事务性 outbox」。
> 补的理由不是"发现了新功能",而是**这两类能力此前根本不在清单上**:
> 行级数据范围(M155/M156 规则层与执行层、M157 后台配置界面)已经在仓里落地了,清单里却一个字没提(只在 §1.12 #11 顺带带过);
> 事件总线则是**框架层一个包都没有** —— 缺的东西不记,清单就永远显示"97%"。
> ⇒ **后端合计从 145/149 = 97% 变成 147/162 = 91%:不是变差了,是账本补齐了。**
> **2026-11-02(第三轮,M160)**:dyn 数据面接上数据范围 ⇒ 关掉 §1.12 #11 与 §1.13 #3 两项
> ⇒ 后端合计 **147/162 = 91% → 149/162 = 92%**,§1.12 **67% → 73%**、§1.13 **40% → 60%**。
> **2026-11-02(第四轮,M161)**:业务查询接上数据范围(17 个业务域 / 36 处调用点 + 三张账本 +
> 启动期 fail-fast + 扫源码守门网)⇒ 关掉 §1.13 #2
> ⇒ 后端合计 **149/162 = 92% → 150/162 = 93%**,§1.13 **60% → 80%**。
> **2026-11-02(第五轮,M162-M164)**:字段级那一轴的**服务端一半 + 配置界面**落地 ⇒
> 关掉 §1.13 #4 ⇒ 后端合计 **150/162 = 93% → 151/163 = 93%**,§1.13 **80% → 83%**;
> ⚠️ **§1.13 #5 当时还开着两条尾巴** —— 两个登记表之间没有交叉守门网 / 前端消费那一半(记在 §2.7 #16)。
> **2026-11-02(第六轮,M166)**:交叉守门网补上 —— 行级账本里的每个域都必须在字段层登记
> (启动期 fail-fast + 单测两头守 + **启动期那个调用点**也被扫源码守着)
> ⇒ §1.13 #5 只剩**一条尾巴**(前端消费那一半,记在 §2.7 #16);
> ⇒ **后端合计仍是 151/163 = 93%**,§1.13 仍是 **83%**(5/6 —— 尾巴没关掉就不许涨)。
> **2026-11-02(第七轮,M167)**:给"脱敏收口在出口"补上**机械守卫** ——
> `internal/api/handler/outlet_guard_test.go` **扫源码**(`go/ast`),
> 把"实际写了响应的地方"和"账本上登记的豁免"对账(扫 `internal/api/` 整棵树:
> handler / router / middleware)。
> ⚠️ "每个 handler 都经过这几个出口"此前只是一句**手工核对过的话** ——
> 写一个 `c.JSON(200, rows)` 就绕过去了,响应照常返回,**没有任何报错**。
> ⇒ 现在:绕过出口的写入点必须逐条登记 + 写明理由 + **条数精确**(同一个函数里多一处就红);
> `pkg/response` 的业务出口调用**只在那五个出口函数体内**才算正当(挪出去就红);
> 判据靠**类型**(`*app.RequestContext`)不靠变量名。
> ⇒ **后端合计仍是 151/163 = 93%**,§1.13 仍是 **83%**(5/6 —— 这是对 #4 的**加强**,不是新尾巴)。
> **2026-11-02(第八轮,M168)**:前端消费那一半落地 —— §1.13 #5 / §2.7 #16 同时关掉。
> `X-Field-Policy` 响应头 + Symbol 戳 + axios 拦截器盖章 + 三个共用外壳按报告收窄
> (隐藏的列 / 字段整个不渲染;打码字段从提交载荷剔掉 —— 数据损坏那半个洞一起堵上)。
> ⇒ **后端合计 151/163 = 93% → 152/163 = 93%**,§1.13 **83% → 100%**;
> ⇒ **前端合计 106/116 = 91% → 107/116 = 92%**,§2.7 **88% → 94%**。
> **2026-11-02(第九轮,M169)**:§1.1 #18 补上 —— 平台运营负责人的四个审计菜单不再是死菜单
> (seed 补 `audit:operation:list` / `audit:data:list` / `audit:login:list` 三个页面实际调用的只读码
> + casbin 强制执行层回归网 + 可逆探针实测转红)。
> ⇒ **后端合计 152/163 = 93% → 153/163 = 94%**,§1.1 **89% → 94%**;前端不变 107/116 = **92%**。
> **2026-11-02(第十轮,M170)**:§1.1 #17 第 0+1 步止血网落地 —— `seed.CheckMenuAPIWiring` 启动期守门网> + `menu_api_map.go` 登记表(详见 §1.1 三步表下方的 M170 记录)。第 ③ 步能力清单本体按原判:**等网反复报警之后再决定**。
> **2026-11-02(第十一轮,M171)**:§2.8 #16 可编辑表格落地 —— `useTableEdit` 状态机 + `EditableCell` 组件
> (快照草稿 / 保存竞态代际守卫 / 行消失自愈 / 失败保草稿;16 条测试 + 2 条可逆探针实测转红)。
> ⇒ §2.8 **82% → 86%**;**前端合计 107/116 = 92% → 108/116 = 93%**;后端不变 **153/163 = 94%**。
> **2026-11-02(第十二轮,M172)**:§2.3 #9 请求进度回调落地 —— `RequestOptions.onUploadProgress / onDownloadProgress`
> 一等选项 + `HttpProgress` 归一化(total 拿不到时 null,调用方不用自己判 axios 的 undefined)
> ⇒ §2.3 **92% → 100%**;**前端合计 108/116 = 93% → 109/116 = 94%**;后端不变 **153/163 = 94%**。

| # | 类别 | 关键文件 | 完整度 |
|---|---|---|---|
| 1.1 | RBAC / Casbin 鉴权 | `pkg/authz/` `pkg/casbinx/` `api/middleware/rbac.go` | **100%** (§1.1) |
| 1.2 | JWT 鉴权 / 凭证 | `pkg/auth/` `pkg/loginlock/` | **100%** (§1.2) |
| 1.3 | 中间件链 | `api/middleware/`(24 个) | **100%** (§1.3) |
| 1.4 | 错误码 / 响应 | `pkg/errcode/` `pkg/response/` | **100%** (§1.4) |
| 1.5 | 加密 / PII | `pkg/crypto/` `pkg/piiaudit/` `pkg/cryptaudit/` `pkg/kms/` | **100%** (§1.5) |
| 1.6 | 登录锁 / 验证码 | `pkg/loginlock/` | **100%** (§1.6) |
| 1.7 | 缓存层 | `pkg/authz/cached_loader.go` `pkg/cachex/safe.go` `service/dict_cache.go` | **100%** (§1.7) |
| 1.8 | 日志 / 可观测性 | `pkg/logger/` `api/middleware/{trace,access_log,audit,otel,metrics}.go` `pkg/otelx/` `pkg/metricsx/` | **100%** (§1.8) |
| 1.9 | 短信 / 通知 | `pkg/msn/` `pkg/smslimit/` `pkg/smsaudit/` `pkg/smstpl/` | **100%** (§1.9) |
| 1.10 | 文件存储(OSS) | `pkg/oss/` `pkg/oss/aliyun/` | **100%** (§1.10) |
| 1.11 | 工具集 | `pkg/{sonyflake,pagination,timeutil,desensitize}/` | **100%** (§1.11) |
| 1.12 | 动态业务集合 | `pkg/dynx/` `service/dyn*.go` `api/handler/v1/dyn.go` | **100%** (§1.12) |
| 1.13 | 数据可见性(行级 + 字段级) | `pkg/scope/` `pkg/fieldpolicy/` `service/scope*.go` `service/biz_scope.go` `service/field_policy*.go` `model/scope.go` `model/field_policy.go` `api/handler/v1/scope.go` `api/handler/field_policy.go` | **100%** (§1.13) |
| 1.14 | 领域事件总线 / 事务性 outbox | `pkg/eventx/` | **100%** (§1.14) |

---

## 1.1 ✅ RBAC / Casbin 鉴权(100%)

#### ✅ 已完成(15/17)

| # | 子项 | 证据 |
|---|---|---|
| 1 | 14 角色 seed(11 业务+1 供应商+2 平台) | `server/internal/seed/seed.go` roleList() |
| 2 | 38 menu 资源 | `server/internal/seed/seed.go` menuList() |
| 3 | 192 api 资源 | `server/internal/seed/seed.go` resourceList() |
| 4 | 114 button 资源 | `server/internal/seed/seed.go` buttonList() |
| 5 | 角色-资源绑定矩阵 | `server/internal/seed/seed.go` bindingRules |
| 6 | `sys_casbin_rule` 持久化(rbac_with_domains) | `pkg/casbinx/enforcer.go:1-113` |
| 7 | 5 层鉴权中间件(L0 Auth / L0.5 Principal / L1 RBAC / L2 ProjectScope / L3 Assertor) | `api/middleware/{auth,rbac,principal}.go` |
| 8 | 权限加载缓存(Redis + singleflight) | `pkg/authz/cached_loader.go:1-103` |
| 9 | 断言器(6 步决策流) | `pkg/authz/assertor.go:1-178` |
| 10 | Principal(身份/项目/角色/grant) | `pkg/authz/principal.go:1-149` |
| 11 | DBLoader(caller+grants+overrides) | `pkg/authz/loader.go:1-174` |
| 12 | PermissionCode 模糊匹配 | `pkg/authz/permissions.go:1-30` |
| 13 | casbin sub 命名空间迁移 | server/internal/seed/migrate_casbin_sub_prefix.go |
| 14 | **`is_primary` 业务层唯一约束** ✅M1 | `model/user.go:48` `IsPrimary gorm:"index:idx_user_primary"` + `service/user_impl.go:41-65` `ensureSinglePrimary` 事务清场 + `migrate/migrate.go:148-181` 预清理脏数据(commit 32d7c4b) |
| 15 | **资源代码表 + UI 一致性保障** ✅M2 | `api/router/router.go:118-122` rbac 闭包 `trackRBAC(module,action)` 记账 + `api/router/consistency.go:1-196` `CheckResourceConsistency` 启动期校验注册 route vs `sys_resource.code`(commit 9154937) |

#### ✅ 已完成(1 个)

| # | 子项 | 当前情况 |
|---|---|---|
| 16 | 角色 `is_active` 关闭级联 identity | ✅ M25:同事务级联 `sys_user_identity`;重启用不级联避免误恢复(commit 3c0c1dd) |

#### ❌ 未完成(0 项)

> **暂缓不做**(2026-11-03 用户拍板,框架定位收尾):原 #17 **一份「能力清单」取代三份码表**(menu / button / api)—— 本体是 3-5 天的 seed 结构重构,**不新增框架能力**;漂移的实际风险已由止血网覆盖(M170 `seed.CheckMenuAPIWiring` + `menu_api_map.go` 启动期 fail-fast,落地当天抓出两个真洞)。技术盘点与目标形态见下方 2026-11-02 的分析(**留着,真要做时是现成的施工图**)。
| 18 | ✅ **洞已补**(M169,2026-11-02):平台运营负责人拿到 4 个审计菜单,`audit:*` 接口权一个都没有 | **补的是三个页面实际调用的只读码** —— `filter(resources, "audit:operation:list", "audit:data:list", "audit:login:list")`(seed.go 的 RolePlatformOpsLead 行,与 `web/src/api/audit.ts` 的三个方法一一对应);export:create(写通道)与没配菜单的审计页(full-info / pii / sms / crypto)**不绑** —— 没配菜单的能力不给码。**回归网**:`casbin_matrix_test.go` 的 `TestRun_CasbinPolicies_AuditMenusHaveApiCodes`(绑定三条 + 强制执行层 casbin 放行三条 + 负例五条);**可逆探针实测转红**(拆掉 filter 行 → require.Contains 红);dev.db 已跑 migrate 实测三码落库 | ~~四个入口点进去全 403~~ ⇒ **已收** | ~~P1~~ → 已收 |

> **🔴 2026-11-02 —— §1.1 标的「100% 完成」要打折:三份码表长期漂移,而守门网看不见这种漂移**
>
> M154(commit `4ac397c`)修的是**症状**(用户管理员点了必然 403 的三个菜单),
> **没修结构成因**。结构成因是:`sys_resource.type ∈ {menu, button, api}` 三份独立码表,
> 各自手写、**只在 seed 时对账一次** ⇒ **长期必然裂**。
>
> | 环节 | 实况 |
> |---|---|
> | **三份码表在哪** | `seed.resourceList()`(`seed.go:69`)里 `xxx:yyy` 是 **api**、`button.xxx.yyy` 是 **button** —— **同一个 `[]string`,靠前缀判 type**(`seed.go:652`);**menu** 是另一张 `seed.menuList()`(`seed.go:321`,带 parent/path/icon/sort)。**三处各写一遍** |
> | **对账只在 seed 一次** | `bindingRules` 手写角色↔资源绑定;`filter(menuCodes, "menu.system", ...)` 是**前缀**匹配 —— M154 里"注释说排除系统配置菜单,代码却把 `menu.system.*` 全家桶一起捞进来",**注释与代码说的正好相反** |
> | **启动期守门网查不到** | `CheckResourceConsistency` 只查三件事:①`rbac()` 引用的码在不在 `sys_resource` ②受保护路由数 == `rbac()` 调用次数 ③api 型孤儿码(**只 info 日志**)。**菜单码与 API 码之间没有任何机器可验的关系** ⇒「菜单给了、API 没给」(M154)和「API 给了、菜单不显示」(反方向)**都查不出来** |
> | **为什么一直没被发现** | 两个方向**都不报错**:菜单多给了 → 点进去 403(像功能坏了);菜单少给了 → 界面少一项(像"这角色就该这么少")。⚠️ M147 之后矩阵**真的强制执行**了,但**矩阵本身是手工维护的** —— 强制执行让漂移的**后果**更明显,没有让漂移**更少** |
> | **这个形状已经手工修过 3 次** | ①**M93**:`dict:list` 只绑给 2 个角色,10 个角色下拉全空;②**2026-11-01**:`dashboard:read` 只有 4 个角色有 ⇒ **9/13 的角色点开工作台 4 个接口全 403**(seed 注释 `seed.go:550-564` 白纸黑字记着);③**M154**:用户管理员三个死菜单。**每一次的修法都是"手工往绑定矩阵里补一个码"** —— 补的是**症状**,不是结构。⇒ 这是**系统性**漂移,不是偶发 |
>
> **实测(2026-11-02,只读盘点,未改任何运行时代码)**:
>
> | 量 | 数字 | 说明 |
> |---|---|---|
> | button 码 → api 码的**机械映射**命中率 | **41 / 117** | button 用的是 **UI 动词**(`add` / `edit` / `export` / `import`),api 用的是 **CRUD 动词**(`create` / `update` / `delete`)⇒ `button.user.add` 对应的是 `user:create`,**名字对不上**;`button.X.export` / `button.X.import` **根本没有同名 api 码**;`button.delivery.stage.*` / `button.audit.*.export` 还是**四段**命名 ⇒ **投影不是机械的,必须人工维护一张映射表** |
> | api 模块名与菜单码的重合 | **24 个 api 模块一个都对不上菜单码** | 菜单码是 **UI 路径**(`menu.system.user` ⇒ 父级 `system` + 页 `user`),api 码是**领域**(`user:*`)⇒ **两者在数据里本来就没有关系** ——「哪个菜单背后是哪些接口」这件事**今天只存在于前端页面源码里**(哪个页面 import 了哪个 api、打了哪条 URL) |
> | dev.db 里 seed 已不存在的码 | **6 个** | `upload:list/read/create/delete`(M76 老码,seed 注释说"已被 `file:*` 取代")+ `button.system.user.export` / `button.system.role.import`(旧命名)。**seed 只 upsert 不删** ⇒ 老码永远躺在库里 |
> | 前端真正引用的 button 码 | **117 / 119** | 零引用的 2 个正好是上面那两个旧命名 ⇒ 前端已经改成新命名,**库里的旧码是纯死码** |
>
> **目标形态**:一份**能力(capability)定义**(如 `asset.read` / `asset.export` / `asset.delete`),
> menu / button / api 三种码**全部从它投影出来**,seed 只对账**一份**;
> 角色授权 UI 从「选三种码」简化成「选能力」
> (`web/src/pages/system/role/AssignResourcesDrawer.tsx` 现在是按 type 分 tab 的三棵树)。
> 成本 **3-5 天**(改动面大,但一次性)。
>
> ⚠️ **但"能力清单"不是唯一解,而且不是最急的那个** —— 分三步走,前两步就能止血:
>
> | 步 | 内容 | 成本 | 买到什么 |
> |---|---|---|---|
> | **第 0 步** ✅(M170 **并入第 1 步**) | ~~只读工具~~ **并入**:登记表 `seed/menu_api_map.go` 本身就是事实清单(21 个叶子菜单逐页对着 web/src/pages 核过),`go test ./internal/seed/` 就是跑它的入口 —— 再做一份 scripts/ 只读工具等于第二份真相,恰是守门网要打的东西 | 半天 → 0 | — |
> | **第 1 步** ✅(M170,2026-11-02) | **启动期 fail-fast 守门网落地** —— 挂在 seed 而不是 `CheckResourceConsistency`(理由见下方 M170 记录);**当天就抓出两个真洞** | ~1 天 | ✅ 这一类洞再也不能静默 |
> | **第 2 步** | 能力清单本体(三份码表投影成一份) | 2-4 天 | 维护成本下降 + 授权 UI 简化 + 漂移**从结构上**不可能发生 |
>
> ⚠️ **为什么不建议一步到位**:能力清单会**改授权粒度** —— 现在绑定粒度是**码**
> (可以只给 `user:list` 不给 `user:delete`);改成能力后,`user.read` 投影出的那一组码
> 会被**捆绑**授给 ⇒ **13 角色 × 377 资源的绑定矩阵要整个重推**。
> 而 M147 / M149 两次都在这块翻过车(静默改掉权限面)⇒ 这一步的**风险不在写代码,在重推矩阵**。
> ⇒ **先把第 1 步的网织上,再决定要不要走第 2 步**;网织上之后,
> 漂移从"没人发现"变成"每次都响",那时再花 2-4 步的钱才划算。
>
> ⚠️ **做第 2 步时别只改后端**:**三份码表同时喂着前端**(按钮显隐 / 菜单渲染)与**角色授权 UI**,
> 前端那侧也要跟着改成按能力选,否则会出现「后端有能力、前端没有入口」的**反向漂移**。

> **✅ M170(2026-11-02)—— 第 0+1 步落地:「菜单给了、接口没给」从此启动期 fail-fast**
>
> **三个新文件 + 两处改动**,全部在 `server/internal/seed/`:
> - `menu_api_map.go`:**叶子菜单 → 页面加载必调接口码最小集**(21 个叶子菜单,逐页对着>   web/src/pages 的 useTable fetch + 挂载即发请求核出;按钮后面的动作不算 —— 那是 button 码的事;>   条件触发的请求不进最小集 —— 判宽不判严,**宁可漏报、不许误报**;>   占位页(PagePlaceholder)不登记,长成真页面时必须回来登记)。>   这张表就是「菜单↔接口」唯一一份机器可验的关系 —— 第 0 步的事实清单由它兼任。
> - `menu_api_guard.go`:`CheckMenuAPIWiring(db)` —— 逐角色核对「绑了的菜单的必调码 ⊆ 绑定集」,>   违例**点名角色/菜单/缺码**聚合报错,报错自带修法指引(补码 / 撤菜单 / 新页面先登记)。
> - `seed.go` Run() 第 3.1 步:绑定矩阵定形后立即体检,违例 ⇒ Run() 返回错误 ⇒ migrate/启动失败。
> - `seed.go` RolePropertyManager 行:补 `folder:list` 只读码(守门网**当天抓出的第一个洞**:>   文件列表页挂载即拉文件夹树,folder:create/delete/upload 仍不给 —— 「不需要建文件夹」的本意不变)。
>
> ⚠️ **挂在 seed 不挂 `CheckResourceConsistency`** —— 与原计划(接进 CheckResourceConsistency)不同,> 有意为之:那个守门网只知道「路由↔码」(代码静态关系),不知道「角色↔绑定」(数据库状态),> **而洞是按角色长的**(M154 / #18 都是某角色的死菜单,不是全局缺码)。> 顺带,挂在 seed Run() 让守门网对 **Drawer 运行期改出来的绑定**同样生效(migrate 一跑就验全部角色)。
>
> **自验证设计**:守门网跑在真实 seed 出来的 13 角色 × 全资源矩阵上 —— 绿 = 当前 bindingRules 无死菜单;> 若登记表要求过头(页面其实不调),seed 测试以「角色 X 菜单 Y 缺 Z」**点名红**,误报自己长脚来报。
>
> **测试 4 条**(menu_api_guard_test.go):全量矩阵绿 1 + 可逆探针 2(拆掉 M169 的 audit 三码 ⇒ 红**点名**> role_platform_ops_lead / menu.audit.operation;抽走 dict:list ⇒ 红)+ 报错文案含登记指引 1。> **端到端实证**:对 dev.db 跑 migrate,**守门网当场 exit 1** —— 抓出垃圾测试角色 `ces`> (2026-10-18 Drawer 测试遗留,零用户身份,绑 8 个系统菜单只有 user:list ⇒ 5 个死菜单,已删),> 删后 migrate 通过 —— fail-fast 链路(含 Run() 挂点)在真实路径上验证过,不是只在小测试里绿。

> **🔴 2026-11-02 —— 复查 #17 时撞出 #18:M154 的同形状洞,当前 seed 里就有**
>
> 查 #17 的时候顺手把 14 个角色的绑定全量对了一遍(只读,未改运行时代码),
> 撞到一个**还没修的同类洞**:平台运营负责人(`role_platform_ops_lead`)
> 侧边栏有**四个审计菜单**,`audit:*` 接口权**一个都没有**。
>
> | 环节 | 实况 |
> |---|---|
> | **菜单怎么来的** | `seed.go:971` 给它 `menuByDomain["audit"]` = `filter(menuCodes, "menu.audit")` ⇒ `menu.audit` / `menu.audit.operation` / `menu.audit.dataChange` / `menu.audit.login` |
> | **接口权为什么没有** | 同一行的 api 侧是 `filter(resources, "notice:", "asset:list", "asset:read", "owner:read", "owner:list", ...)` —— **没有 `audit:` 前缀**;`authBaseCodes` 里也没有 ⇒ `audit:*` 绑定为零 |
> | **为什么必 403** | 三条审计路由都挂了码:`rbac("audit:operation","list")` / `("audit:data","list")` / `("audit:login","list")`(`api/router/router.go:501-503`);前端 `pages/audit/operation.tsx` → `auditApi.operationLogs` → `GET /system/audit/operation-logs` |
> | **核实到哪一步** | seed 源码与 `dev.db` **双向核对一致**(该角色的 api 绑定里确实一个 `audit:` 都没有)。⚠️ **未做真机点验**(本轮只做文档整理) |
> | **怎么修** | 给该角色补 `audit:operation:list` / `audit:data:list` / `audit:login:list`(只读三件套,与 `role_readonly` 拿的那套同口径)⇒ **先确认业务上这个角色该不该看审计台**,再决定补码还是撤菜单 |
>
> ⚠️ **它恰好证明了 #17 的必要性**:这个洞**不是**"三份码表漂移"的假设,
> **✅ 修法(M169,2026-11-02)**:选**补码**不是撤菜单 —— seed 给这个角色的业务码(发公告 / 看资源)本就是「跨项目运营」的看护岗,审计菜单是**有意**配的,缺的只是接口权。补三个页面实际调用的只读码(见 #18 行),export:create 与没配菜单的审计页不绑;`TestRun_CasbinPolicies_AuditMenusHaveApiCodes` 把这一角色上的「菜单给了、接口没给」钉死(casbin 强制执行层的放行也一并验了 —— M147 的教训:绑定不能只在显示层是真的)。
> 是**今天线上就有的一个实例**;而启动期守门网**看不见它**
> (`CheckResourceConsistency` 只管 route↔api,不管 menu↔api)。

> **复用价值**:**高** —— RBAC 是任何项目都需要的核心能力;**2026-10-19 M1+M2+M25 commit 后 16 项子项全完成**(当时 100%),**2026-11-02 起 94%** —— 差的一项是 #17(架构项,不阻塞上线;#18 审计死菜单 M169 已收)

> **🔴 2026-10-26(M93)**:读 seed 时查出一个 **P0 权限洞** —— `dict:list`
> **只绑给了 super_admin 和 user_admin**,**10 个角色一个都没有**。
>
> | 环节 | 实况 |
> |---|---|
> | **症状** | 物业工作人员打开工单页,**状态下拉全空**,而且**每开一页弹一次"没有权限"** |
> | **根因** | `bindingRules` 里只有超管 / 用户管理员拿 `dict:` 前缀;`authBase`(所有角色自服务的那一档)里**没有** `dict:list` |
> | **更拧巴的** | 只读员 `role_readonly` **反而有**这个码 —— `filterByRead` 按 `:list` 后缀捞到了。**天天用下拉的 10 个角色没有,不用的人有** |
> | **为什么一直没被发现** | 字典是"代码 enum 的中央仓库",`useDict` 静默 catch 掉 403(`stores/dict.ts`),下拉空着也不报错 —— 只有"没有权限"那声 toast 提示你 |
>
> **修法**:`authBase` 加 `dict:list`(**只读**)。字典是只读契约 ——
> `dict:create` / `update` / `delete` **仍然只归用户管理员 / 超管**。
>
> **守门网**:新增 `TestRun_BindsDictListToAllRoles`(seed 包)—— 13 个角色**全部**必须
> 有 `dict:list`,且**除超管 / 用户管理员外一律不得**有写权限。
> **可逆探针**:把 `authBase` 里的 `dict:list` 抽掉 → 测试立刻红(12 个角色报错)→ 放回 → 绿。
>
> ⚠️ **运维提示**:seed 改动**要跑一次 `cmd/migrate` 才生效**(走的是 seed 绑定那条路);
> 已经跑过的环境不会自动补上这条绑定。

> **🔴 2026-11-01(M147)**:本节标的 **"RBAC/Casbin 100% 完成"是错的** ——
> 挖出来的洞比 M93 那个还大:**绑定矩阵在强制执行层从来没生效过**。
>
> | 环节 | 实况 |
> |---|---|
> | **症状** | seed 给**每个角色**都加了一条通配策略 `p, role:X, global, *, *, allow`,而 casbin 的 `keyMatch2(r.obj, "*")` 编译成 `^*$` —— **恒真** ⇒ **任何角色都能访问任何接口** |
> | **更深的** | 策略行的 obj 是**资源码**(`user:list`),请求的 obj 却传**模块名**(`user`)⇒ `keyMatch2("user","user:list")` = false ⇒ **一条绑定行都命中不了**。这两个洞叠在一起:通配行盖住了粒度错位 |
> | **还叠了第三个** | `keyMatch2` 把 `:xxx` 当 **URL 路径参数**编译成正则 ⇒ 绑了 `user:list` 的角色**也能过 `user:delete`**(模块内串味);`house:*` 这种 deny 规则**永不生效** |
> | **为什么本节标 100% 却没人发现** | 前端按矩阵渲染菜单(别人的菜单少了、按钮少了),后端却谁都能调 —— **矩阵只在「显示」这一层是真的**;而前端菜单少一项**不报错**,只会让人以为"这角色就该这么少" |
>
> **修法**(三处配套 + 一处补数据,**缺一不可**):
>
> 1. `configs/casbin_rbac.conf`:matcher `keyMatch2` → **`keyMatch`**(资源码里 `:` 是字面量,不是路径参数);
> 2. `internal/seed/seed.go` `buildRolePolicies`:**删掉**「每角色一条通配行」;
> 3. `internal/api/middleware/rbac.go` `decideAccess`:请求 obj 传**完整资源码** `module + ":" + action`
>    (跟 grant 路径本来就在用的口径对齐 —— 之前同一个请求走两条路会判出两个结果);
> 4. `internal/seed/seed.go` `authBaseCodes`:**补 `resource:accessible`** ——
>    不补的话该角色登录后**菜单和按钮全空**(前端不报错),而它自己没法恢复
>    (**跟 M93 补 `dict:list` 是同一个形状的洞**)。
>
> ⚠️ **只删通配行会变成"人人访问不了"** —— 因为粒度错位那条(请求 obj 是模块名)
> 决定了本来就没有任何一条绑定行能被命中。⇒ 1/2/3 必须**同一个 commit**。
>
> **守门网**:
> - `server/internal/seed/casbin_matrix_test.go`(**新**)—— 行级(无 `obj=="*"` 的行)
>   + 行为级(只读员 `user:list` 放行 / `user:delete` 拒绝;超管全放行)
>   + 每个角色必须有 `resource:accessible` 但不得有 `resource:create/update/delete`;
> - `server/internal/api/middleware/rbac_test.go` —— 新增
>   `TestDecideAccess_ProductionRequestShape_EnforcesBindingMatrix`(4 个子用例)
>   + `TestDecideAccess_GrantPathUsesSameResourceCode`;
> - `casbin_sync_test.go` 里那条 `TestBuildRolePolicies_PerRoleWildcardPlusBindings`
>   **把 bug 写进了断言**(所以它一直是绿的)⇒ 重命名为
>   `TestBuildRolePolicies_BindingsOnlyNoWildcard`。
>
> **可逆探针**:matcher 改回 `keyMatch2` → 红;恢复通配行 → 红;请求 obj 改回模块名 → 红;
> 三条全还原 → 绿。
>
> ⚠️ **真机已验**:超管 6 个接口全 200;`role_user_admin`(只绑 45 个 api)绑了的 200、
> 没绑的(`config:list` / `activity:list` / `job:list` / `audit:login:list`)全 **403**;
> `resource:accessible` 对它返 394 条资源(45 条 menu.\*)⇒ **菜单没变空**。
>
> ⚠️ **运维提示**:同 M93 —— seed 改动**要跑一次 `cmd/migrate` 才生效**。
> `casbin_rule` 由 seed **全量重写**(先删后写),不需要手写迁移脚本。

> **🔴 2026-11-01(M149)—— 显示层按「当前身份」算(用户报的现象:切身份后界面纹丝不动)**
>
> **症状**:从超级管理员切到用户管理员,**界面完全一样** —— 菜单还是超管那一套
> (45 条),按钮一个没少,点下去才 403。
>
> **根因:显示层与执行层两套真相**
>
> | 层 | 口径 | 位置 |
> |---|---|---|
> | 执行层 | **当前身份**(`caller.RoleCode`) | `internal/api/middleware/rbac.go` `decideAccess`;`internal/pkg/authz/loader.go` `loadCaller` |
> | 显示层(改之前) | **这个用户所有身份的并集** | `service.AccessibleResources`;`service.GetCurrentUser` 的资源树;`service.platformFlags`(⇒ `is_platform_super_admin`);前端 `web/src/stores/permission.ts` |
>
> 并集里只要有超管 ⇒ 显示层就按超管渲染 ⇒ 切到低权限身份后界面**纹丝不动**。
>
> ⚠️ **这不是 M147 造成的**:执行层一直按当前身份判,是显示层**一直**按并集算 ——
> 只是 M147 之前两边都不准(都放行),看不出两套真相;M147 让执行层收紧后才**显形**。
>
> **改法**:后端 `AccessibleResources` / `GetCurrentUser` 的资源树 / `platformFlags`
> 全部按 `caller.RoleCode` 算(**空角色码 ⇒ 空集**,与执行层 `role:` 空角色 deny 一致);
> 前端 `roleCodes` / `permissions` 只取当前身份(**找不到当前身份 ⇒ 空**,倒向拒绝)。
> **M149 阶段契约字段名一个没改** ⇒ 那一步前端零改字段,变的只是派生口径。
> (`identities` 本身仍返回全部 —— 身份切换器要用。**M149-b 之后新增了
> `current_role_code`**,见下面 M149-b 那段。)
>
> ⚠️ **角色绑定查表不过滤 `sys_role.is_active`** —— casbin 停用角色时**不清策略行**,
> 过滤掉停用角色会造出「菜单没了但接口还能调」的**反向**不一致。
>
> **守门网**:`server/internal/service/auth_identity_scope_test.go`(新,5 用例)——
> 核心断言**同一个用户两个身份 ⇒ 菜单必须不同**(这条在改之前**必然红**);
> `web/src/stores/__tests__/permission.store.test.ts` —— 权限码不再含**别的身份**的码。
>
> 完整记录见 `docs/后台权限模型设计V1.0.md` §17.40。
>
> **🔴 2026-11-01(M149-b)—— 前端不许再「猜」当前身份**
>
> M149 之后前端取当前角色是 `identities.find(i => i.id === current_identity_id)`
> —— 那是**猜**,会错两处:
>
> ① 管理员改过该用户的身份 ⇒ 后端 `writeUserIdentitiesFromItems` 是**全量替换**
> (身份行 ID 全变)⇒ 旧 session 的 `current_identity_id` **找不到** ⇒ `roleCodes` 变空
> ⇒ 角色门控(`AuthButton` 的 `roleAnyOf`)整个失效(按钮凭空消失 —— 从"该藏的没藏"
> 变成"该显的不显",两个是同一个洞的两面);
>
> ② 库里角色改了、token 还是旧角色 ⇒ 前端显示新角色、后端按旧角色判。
>
> ⇒ `UserInfo` 新增 **`current_role_code`**(= token claims 里的 `role_code`,
> **与 RBAC 中间件 `decideAccess` 同一个来源**)⇒ 前端不用猜。
> 前端优先用它;老后端不返时回退到「找当前身份」(两条路都拿不到 ⇒ 空,倒向拒绝)。
> 服务端侧:`NewCurrentUserView` 的两个 bool 参数收成**一个** `currentRoleCode`
> (平台标记只在 view 构造时派生一次,唯一口径 `platformFlags`),
> `hasSuperRole` / `hasOpsRole` 两个薄壳删除(零生产调用方)。
>
> ⚠️ **已知缺口(本期不做)**:管理员**降职**某用户后,该用户手里那张 access token
> 仍带**旧** `role_code` ⇒ 执行层仍按旧角色放行,最长到 token 过期(8h)。
> 彻底修它需要 per-user / per-identity 的 JTI 索引(改密码 / 登出吊销整条会话),
> 已在鉴权计划里单列 —— **别把 M149 当成"权限立刻收权"**。
>
> ⚠️ **前端连带影响(别以为 M147"只改了后端")**:后端真的开始强制执行矩阵后,
> 前端凡是「调了没绑的码」的地方,会从"静默成功"变成 **403 + 红色 toast**。
> 全量对账(前端路由可达性 × 角色绑定矩阵 × 页面实际调用的接口)之后,
> **真实缺口只有一个**:审批规则页 `web/src/pages/flow/stage/[stageId].tsx`
> 无条件拉候选审批人名单(`user:list` / `role:list`)⇒ 命中 **C 组**
> (看流程配置:房源 / 资料 / 信息 / 交付管理员 —— 它们有 `flow:read`,
> 但**没有**这两个码)。其余"缺口"都在 `PermButton.*` 门控的按钮后面 ⇒ 无害。
>
> **修法:前端按权限码门控,不给 C 组补码** —— 补码是把"看不见的名单"变成
> "看得见的权限",**扩大权限面**;前端只调自己调得动的接口是**更小**的权限面。
> **守门网**:`web/src/pages/flow/__tests__/stage-rules.test.tsx`(新,**4 个用例**,
> 断言"**请求有没有发出去**"而不是 DOM —— 名单为空时"没权限"和"还没加载完"
> 在 DOM 上长得一模一样)。**可逆探针**:去掉两道 `has(...)` 门 ⇒ 前 3 个用例全红;
> 把 effect 依赖改回 `[has]` ⇒ 第 4 个用例变红(见下)。
>
> ⚠️ **依赖项不能只写 `has`**:`usePermission()` 里的 `has` 是 store 里
> **只创建一次**的函数 ⇒ 引用**永远不变** ⇒ 只跟它走的话,权限集换了
> effect 也不重跑。而**切身份是不刷新页面的**(`IdentitySwitcher` 走
> `fetchAccessible()`,"不 reload 页面")⇒ 组件不重挂载 ⇒
> mount 时那一次判断成了「上一个身份」的判断:
> 「没有 `user:list` → 有」⇒ 名单永远空;「有 → 没有」⇒ 名单留旧数据。
> ⇒ 依赖里加 `permissionCodes`(每次 `setPermissions` 都是**新 Set**)。
>
> 完整记录见 `docs/后台权限模型设计V1.0.md` §17.39。

> **🔴 2026-11-01(M147-e / M147-f / M147-g)—— M147 收尾的真机验证撞出三个洞**
>
> M147 让矩阵**真的生效**之后,前面被通配行盖住的三处不同步**立刻变成 403**。
> 三个洞是**同一个根因**的三张脸:**"绑定矩阵"有两份真相(数据库 vs `casbin_rule`),
> 而它们只在 seed 时对账一次。**
>
> | # | 洞 | 症状 | 根因 | 修法 |
> |---|---|---|---|---|
> | **M147-e** | **自建角色没有自服务码** | 自建角色的身份切过去 → `/auth/me` **403** ⇒ **整个后台不可用**(前端连"我是谁"都拿不到),**且无自愈路径** | `authBaseCodes`(15 个自服务码)只绑给 13 个内置角色;自建角色一个都没有(在 M147 之前,这条洞被通配行盖住了) | 两个写入口都要守:建角色时绑上 + 分配权限时**并进**最终绑定集(覆盖式保存不许抹掉);seed 侧对**所有** active 角色补一遍(覆盖存量) |
> | **M147-f** | **运行时改绑定,casbin 不同步** | 管理员勾了权限 → 界面菜单出来了、按钮出来了,**接口一律 403**,**没有任何报错**(casbin 只是"没匹配到策略行") | `casbin_rule` 是绑定矩阵的**物化副本**,只在 seed / `cmd/migrate` 时整体重建一次;运行时改的是 `sys_role` / `sys_role_resource` ⇒ 前端(按绑定表显示)与后端(按 casbin 判)**两套真相** | 三个写入口(建角色 / 分配权限 / 删角色)+ 一个触发点(资源启用状态变化)全部接上同步;同步写进**调用方的 tx**(绑定与策略行同一事务,失败方向不能是"放行");**刷新内存副本放在事务提交之后** |
> | **M147-g** | **软删的角色仍然占着 code** | 删掉一个角色 → 再建同 code → **500 + 裸 SQL** `UNIQUE constraint failed: sys_role.code` ⇒ **这个 code 再也建不了** | `sys_role.code` 上的 uniqueIndex **不看 `deleted_at`**;唯一性预检用默认 scope ⇒ 查不到软删的行 ⇒ 预检放行 → INSERT 撞约束 | 预检改 `Unscoped()`;活的同 code → **409**(与 user 服务"用户名已存在"同一口径);软删的同 code → **复活**它(删除时绑定/身份已硬删 ⇒ 等于全新角色) |
>
> **附带口径统一**:**「停用资源」不再是死开关**。停用一个资源后菜单消失(`AccessibleResources`
> 过滤 `is_active`)、**接口还能调**(casbin 里那条策略行还在)⇒ 三处口径统一成
> 「只取 `is_active = true`」:① `seed.buildRolePolicies`(整体重建)
> ② `service.roleResourceCodes`(运行时同步)③ `authService.AccessibleResources`(前端来源)。
> ⚠️ 停用是**临时收回,不是解绑** —— 重新启用会把权限还回去。
>
> **守门网**:
> - `server/internal/pkg/casbinx/sync_test.go`(新,4 用例)—— 含一条关键断言:
>   **写完库但没 Reload 时 Enforce 必须仍是 false**(内存模型不会自动读库);
> - `server/internal/service/role_casbin_sync_test.go`(新,7 用例)——
>   期望清单**手抄**(`auth:me` / `resource:accessible` / `dict:list`),
>   **不抄实现里的那份**(§M103 的教训:测试用被测常量构造输入 = 恒绿);
> - `server/internal/service/role_authbase_test.go`(新,4 用例)—— M147-e 的绑定;
> - `server/internal/seed/seed_test.go` —— `TestRun_BindsAuthBaseToNonBuiltinRoles`。
>
> **可逆探针**:casbinx + service 共 **7 条**,revive **2 条** —— 拆掉实现后
> **全部变红**;每次还原后自检全绿。⚠️ 探针本身也会假绿 ⇒
> 「挂起 ≠ 变红」:失败路径死锁比假绿更难查,CI 只等到超时。
>
> ⚠️ **真机已验**(真起服务 + curl):建角色 → 查绑定(15 个自服务码 ✅)→
> 分配权限 → 切身份 → **`GET /auth/me` 200**(M147-f 之前是 403)→
> `/system/resource/accessible?type=menu` 200(45 条菜单)→ `/system/dicts` 200;
> 删掉再建同 code → 复活成功(角色重新出现在列表里,权限全新算)。
> 清理已验:admin 身份还原为 2 个;探针角色软删且 **casbin 行数 0**;
> `casbin_rule` 总行数仍 1152(**0 通配行**)。
>
> ⚠️ **运维提示**:同 M93 —— seed 改动**要跑一次 `cmd/migrate` 才生效**。
>
> 完整记录见 `docs/后台权限模型设计V1.0.md` §17.39。

---

## 1.2 ✅ JWT 鉴权 / 凭证(100%)

#### ✅ 已完成(9/12)

| # | 子项 | 证据 |
|---|---|---|
| 1 | JWT 签发 + 校验(HS256) | `pkg/auth/jwt.go:1-229` |
| 2 | **3 audience**(admin/owner/supplier)3 套独立 secret | `pkg/auth/jwt.go` IssueAudience / ParseAudience |
| 3 | Claims 含 UserID/IdentityID/RoleCode/ProjectIDs + **`typ` / JTI / aud** | `pkg/auth/jwt.go` Claims struct(2026-10-21 补 `typ`,标准 claim 全走嵌入的 `RegisteredClaims`) |
| 4 | Refresh token 机制 | `pkg/auth/jwt.go` + service refresh |
| 5 | **JTI 吊销表(Redis / 内存双实现)** | `pkg/auth/revocation.go` + `redis_store.go` + `memory_store.go` |
| 6 | 密码 bcrypt(cost=12) | `pkg/auth/password.go:1-48` |
| 7 | 密码强度 10-32 位校验 | `pkg/auth/password.go:1-48` |
| 8 | 登录锁(5 次失败 30 分钟) | `pkg/loginlock/loginlock.go` + `pkg/kvstore`(存储后端与限流共用 `kv.mode`) |
| 13 | **作废线(按用户整体强制下线)** | `pkg/auth/invalidate.go` + `RevocationStore.InvalidateBefore/InvalidateFrom`;中间件第二道闸 + `RefreshToken` 自查;三个改密入口触发 |

#### ⚠️ 遗留缺口台账(✅ 5 条已关闭 / 3 条仍开着 —— **不计入本节完成度**)

| # | 缺口 | 现状 | 后果 | 堵法 | 优先级 |
|---|---|---|---|---|---|
| A | ~~**登出只吊销 access,refresh 仍可续命最长 720h**~~ | ~~`Logout` 只对 `caller.JTI` 调一次 `Revoke`,而那是 access token 的 jti~~ | ~~用户点了「退出登录」,refresh token 仍可换发新 access —— 登出只挡住了一半~~ | ✅ **2026-10-24 已修**:会话绑定 sid —— 一次登录生成一个 sid,access / refresh 两张 token 共用它;登出吊销 sid,两张一起死;`RefreshToken` 自查会话吊销态(它走公开路由,过不了中间件) | ✅ |
| A2 | ~~**无法「强制下线其他设备」**~~ | ~~服务端没有该用户会话的索引,只能结束「我自己这一条」(sid)~~ | ~~需求文档 §2.2.1 #6 仍悬空:管理员踢不掉某台设备上的会话~~ | ✅ **2026-10-24 已修**:会话登记簿 = `sys_login_log`(一行一次登录 = 一条会话,靠 `session_id` 关联 sid);`GET /auth/sessions` 列活跃设备 + 历史登录记录,`POST /auth/sessions/revoke` 点名吊销;中间件加**第三道闸**(会话吊销态)—— 少了它,被踢的设备手里那张 access 还能用 8h | ✅ |
| B | **`ResetPasswordBySms` 不吊销任何会话** | ~~该路径没有 `caller`,拿不到当前 jti~~ | ~~短信重置密码后,被盗者的会话全部仍然有效~~ | ✅ **2026-10-21 已修**:推进该用户的作废线,三个改密入口(改密码 / 管理员代重置 / 短信重置)全部按用户整体作废 | ✅ |
| C | **`revocation_mode=memory` 下吊销态是进程内的** | 重启即失效、多个副本互不可见 | 部署成多副本却选了 memory = 吊销形同虚设,且不报错 | 启动打 `auth_revocation_degraded=true`;生产应显式配 `redis`(缺 `redis.addr` 直接启动失败) | 已有护栏 |
| D | **小程序侧(业主端 / 物业工作端 / 供应商端)无空闲超时** | 只有后台 Web 有(`utils/idleTimer.ts`) | 需求文档 §2.2.1 / §7.2.1 / §9.2.1 三处委托仍悬空 | 需求文档已标注「技术方案尚未确定」,首期不实现 | P2 |
| E | ~~**多标签页并发刷新竞态**~~ | ~~两个 tab 同时触发续期,一个成功一个拿着已消费的 refresh 被拒~~ | ~~失败的那个 tab 被误踢登录页~~ | ✅ **2026-10-25 已修(M8)**:前端侧做跨标签页会话同步 —— `utils/tokenSync.ts` 定协议(`BroadcastChannel` 传 token 组,同源天然成立、不落盘不废掉「记住我」),`tokenScheduler.doRefresh()` 续期失败先走恢复链(① 共享存储重读 ② 跨标签页求助)**再决定登不登出**;`api/client.ts` 的 refresh 请求 401 **不再当场清会话**(登不登出交给 doRefresh 走完恢复链) | ✅ |
| F | ~~**切换身份不换 sid ⇒ 旧 refresh 换发旧身份**~~ | ~~切换时服务端只拿得到 **access** 的 jti,拿不到切换**之前**那张 **refresh** 的 jti ⇒ 吊销不了它;三道闸(jti / 会话态 / 作废线)全过 ⇒ 它还能换发一份**旧身份**的 access~~ | ~~用户从高危身份(超管)切到低危身份之后,手里那张旧 refresh **还能继续拿高危身份**,最长 720h(30 天)—— "切身份"在安全边界上等于什么也没做~~ | ✅ **2026-11-01 已修(M148)**:切换身份**换 sid + 吊销旧 sid**(TTL 取 refresh 寿命)+ `sys_login_log` 那一行跟着换 sid ⇒ 旧会话的 access + refresh 一起死,**不管中间轮换过几轮**(轮换只换 jti,sid 不变)。附带修掉 service 层"不改 caller" ⇒ 角色切换审计的 `to_role` 恒等于 `from_role` | ✅ |
| G | **切换身份后 SSE 连接不重建** | `event.go` 的 `subscribableTopics` **连接建立时判一次**;后端只在**角色绑定变更**时广播 `perm_changed`,切换身份**不广播** ⇒ 那条连接上推送的仍是旧身份的数据 | 切身份后旧身份的 topic 仍会推事件给前端(信号不含数据,但仍是旧身份的可见范围) | 要修得动 SSE 的连接生命周期(切换时主动断连 / 重建)。**本期不做**,已在技术方案 §7.7.4.3 记为已知缺口 | P2 |

##### A 的堵法修正(2026-10-21)—— 原来写的「per-user JTI 索引」是错的

原文档给 A 的堵法是「签发时记 `user:{id}:jtis` 集合,吊销时按用户批量」。**这个方案解决不了 A**:

- A 的诉求是"**只踢别的设备,保留本设备**"(需求文档 §2.2.1 #6 的「强制下线其他设备」)。
- 而按用户批量吊销是**全有或全无**的 —— 集合里装的是这个用户的所有 jti,批量吊销会把
  当前设备一起踢掉。要"保留本设备",服务端必须能回答"除这一张之外,这个用户还有哪些
  会话",这需要登记簿(见下),而仅仅一个 jti 集合不够 —— 集合是**无序的**,答不出"除这一张"。

> **2026-10-24 修正:上面这段把两件事混成了一件。**
>
> - **A 本身**是 **per-session** 的:"用户点了退出登录,这条会话的 refresh 就该死"。
>   它**不需要**登记簿 —— sid 就够了(见下节)。A 已关闭。
> - **「强制下线其他设备」**(需求文档 §2.2.1 #6)是 **per-user** 的:要能回答
>   "这个用户还有哪些会话",这才是登记簿的用场。这件事**已于 2026-10-24 落地**
>   (登记簿 = `sys_login_log`,见下方「A2 已关闭」)。
>
> 换句话说:「per-user JTI 索引解决不了 A」这个结论仍然成立,但理由要改成
> 「per-user 索引解决不了 A2」—— 因为 A 要的是 per-session 的精度,
> 而 per-user 索引天生是全有或全无的粒度。**A 和 A2 是两个不同的问题。**

> **注意:作废线解决不了 A2,别拿它顶数。** 2026-10-21 引入的作废线
> (见 §1.2 的「✅ 强制下线」)是**全有或全无**的:一推就是该用户所有会话一起死。
> 拿它去堵 A2,后果是"用户在手机上点了退出,电脑上的会话也一起没了",而且**每次登出
> 都这样**。service 层的 `TestLogout_DoesNotPushInvalidationLine` /
> `TestSwitchIdentity_DoesNotPushInvalidationLine` 两条**否定断言**就是专门钉住这件事的。
>
> A2 最终是靠**登记簿**堵上的(2026-10-24),不是靠作废线 —— 上面这两条否定断言正是
> 防止将来有人拿作废线去顶数。

##### ✅ A 已关闭:会话绑定 sid(2026-10-24 已落地)

| 项 | 内容 |
|---|---|
| 原语 | `pkg/auth/session.go` `NewSessionID` / `RevokeSession` / `IsSessionRevoked` / `SessionRevokeKey` |
| claim | `Claims.SessionID`(`json:"sid,omitempty"`)—— 一次登录生成一次,access / refresh 两张 token 共用 |
| 吊销键 | `sid:{sid}` —— 与 jti 键空间**分开**(吊销存储是一张扁平字符串键集合,不撞不能靠巧合) |
| 登出 | `Logout` 吊销 sid(TTL 取 **refresh** 的寿命,会话里寿命最长的就是它;用 access 的 8h 会让吊销记录先过期而 refresh 还活着) |
| 续期 | `RefreshToken` 第 2.6 步自查会话吊销态 —— **中间件那两道闸查不到 sid**,且 refresh 走公开路由根本不过中间件,少了这一步就白改 |
| 不换 sid 的一处 | refresh 轮换(换了就等于开新会话,登出吊不到轮换后的 refresh) |
| ⚠️ **切换身份换 sid(M148,2026-11-01)** | 切换身份**换 sid 并吊销旧 sid** —— 旧设计"切换身份不换 sid"只考虑了登出,漏了切换时服务端拿不到**旧 refresh 的 jti** ⇒ 吊销不了它 ⇒ 它还能换发一份**旧身份**的 access(jti 没吊 / sid 没变 / 作废线没推,三道闸全过)⇒ 用户从超管切到普通用户之后,手里那张旧 refresh 还能继续拿超管(最长 720h)。现在换 sid + 吊旧 sid(TTL 取 refresh 寿命)+ `sys_login_log` 那一行跟着换 sid ⇒ 旧会话的 access + refresh 一起死,**不管中间轮换过几轮**。附带修掉 service 层"不改 caller" ⇒ 角色切换审计的 `to_role` 恒等于 `from_role` |
| 老 token | 本轮上线前签发的 token 没有 sid(omitempty)→ 登出退回"只吊销 access jti"(即本轮之前的行为,非新缺陷);续期时**补发 sid**,从此这条会话有了绑定。残留窗口 = 上线前签发且一次都没用过的 refresh,随其自然过期关闭 |

##### ✅ A2 已关闭:会话登记簿(2026-10-24 已落地)

| 项 | 内容 |
|---|---|
| 登记簿 | `sys_login_log` —— **不新建 `sys_session` 表**。一次登录 = 一行 = 一条会话,靠 `session_id` 关联 sid;需求要的字段(设备 / IP / 浏览器 / 系统 / 登录时间)这张表全都有,新建一张等于抄一遍 |
| 为什么放 DB | 登记簿必须**跨实例一致** —— 吊销存储的内存实现是进程内的,多实例部署时 A 实例看不见 B 实例上的会话 |
| 新增两列 | `session_id`(空串 = 登录失败的记录)、`last_active_at`(**2026-10-30 M117-4 改**:有请求打在这条会话上就记一次,**登录本身也算一次**,节流 ≤5min;此前只在续期时更新 ⇒ 会话建立后 8 小时内那列恒空) |
| 判活 | `COALESCE(last_active_at, login_at) + refresh_ttl > now` —— 只看 `login_at` 会把「最近刚续过期」的会话误判成死的 |
| 列表接口 | `GET /rapid-admin/v1/auth/sessions` → `{ active, history }`;**不接 `userId`**,只能看自己,接口层面就没有可越权的入口 |
| 吊销接口 | `POST /rapid-admin/v1/auth/sessions/revoke` → `{ revoked: n }`;**归属校验是硬要求**(sid 是能直接递给吊销存储的句柄,不校验就是「任意用户可踢任意用户下线」的 DoS) |
| 不许吊自己 | 当前这条要走 `/auth/logout`(它连带把 access 的 jti 也吊了);在这里吊掉它是「access 还活着、refresh 已死」的中间状态 |
| **第三道闸** | 中间件加会话吊销态。少了它,被踢的设备手里那张 access 还能用 8h(前两道闸都不认「这张 access 是哪条会话发的」) |
| 留痕 | 每次强制下线写 `sys_operation_log`(`module=auth` / `action=revoke-sessions`,描述带 sid 列表) |
| 前端 | 个人中心「登录设备」Tab(`web/src/pages/profile/SessionPanel.tsx`) |

> 需求文档 §2.2.1 #6 的形状是「**保留本设备、只踢别的**」,所以要的是 per-session 的精度 ——
> 作废线(per-user)顶不了这个数,两者别混。sid 解决了「结束我自己这一条」,
> 登记簿补上的是「点名踢掉别人那几条」,两件事分别落在两处。

##### ✅ 强制下线:作废线(2026-10-21 已落地)

| 项 | 内容 |
|---|---|
| 原语 | `pkg/auth/invalidate.go` `TokenInvalidated` / `SessionInvalidated` |
| 存储 | `RevocationStore.InvalidateBefore` / `InvalidateFrom`,Redis(`auth:bl:inv:{uid}`,毫秒)+ 内存双实现 |
| 判定 | `iat + ClockSkewGrace(60s) < 作废线` |
| 中间件 | `middleware/auth.go` 在 jti 吊销之后**加第二道闸**,与前一道互相独立 |
| refresh | `service/auth_impl.go` `RefreshToken` 自己查一次 —— refresh 走公开路由,过不了中间件 |
| 触发点 | 改密码 / 管理员代重置(**推目标用户的线,不再只推自己**) / 短信重置密码 |
| 刻意不触发 | 登出 / 切换身份(per-device 动作,推线是 per-user 的) |

**为什么选时间戳线而不是 epoch 计数器**:计数器一旦随进程重启清零,所有正常用户的
令牌都会被判失效 —— 一个此前**不存在**的 fail-closed 故障类别;而时间戳线丢了只退化成
fail-open(一次强制下线没生效),与吊销表本身既有的降级方向完全一致(见 `revocation.go`
里"重启会复活所有本该失效的 token")。选已知失败方向,不选新的。

**已知边界(不是 bug,是取舍)**:判定带 60s 时钟偏移宽限,所以**改密码前 60 秒内**打开的
会话仍然有效。去掉宽限会让跨实例时钟偏差下的用户拿**刚换的新密码**登不进去,且现场
没有任何可查的线索。`TestChangePassword_SessionsInsideClockSkewGraceSurviveByDesign`
把这条边界钉死,防止将来有人"顺手去掉宽限"。

#### ✅ 已完成(3 个)

| # | 子项 | 当前状态 | 后果 | 优先级 |
|---|---|---|---|---|
| 10 | **跨实例吊销广播** | ✅ M24:`pkg/auth/revocache/` Redis pub/sub 广播 —— 多副本下 A 实例吊销的 jti,B 实例立刻知道,不必等本地缓存过期(commit cd6f256) | 多副本部署时"已登出"的 token 在别的副本上还能用 | P1 ✅ |
| 11 | **异常登出检测**(异地同时登录告警) | 🔴 **M128(2026-10-31)整体重写此行** —— 旧行(M45)描述的是**包存在**,不是**能力在跑**。三条实测:① `MaxConcurrent` 字段**从来没被任何一行读过**(`Detect` 硬编码 `len(active) > 1`),所以"MaxConcurrent=3 默认"是**假的**(配 10 也第 2 端就报);② `LogNotifier.Notify` 函数体只有 `if n.Hook != nil {...}` —— 注释写着"落 stderr",实现里**一行日志都没有**;③ **整个包零生产调用方**(`grep -rn 'internal/pkg/anomalylogin'` 全仓零命中)⇒ 痛点「用户被盗号时无法感知」**一次都没被真正解决过**。**现在**:`pkg/anomalylogin` 阈值真的被读(`maxConcurrent()` 兜底 ≤0 ⇒ 1)+ `LogNotifier` 真的记 WARN;`internal/service/auth_anomaly.go` 把检测接进**登录链路**(成功登录后跑一次,查 `sys_login_log` 里"活动窗口内仍活跃"的其他会话)—— ⚠️ **两条签发 token + 建会话的入口都接了**:密码登录 `Login` **与**短信登录 `LoginBySms`(只接一条 = 留了一条免检通道);`SwitchIdentity` / `RefreshToken` 不接(它们复用 sid,不是新会话)。High 级异常给账号本人发**站内信**(`notifyx.Fanout`);旋钮在 `auth.anomaly.*`(`enabled` / `max_concurrent=3` / `active_window=30m` / `impossible_travel_window=0s`)。⚠️ **口径**:「同时在线」= `COALESCE(last_active_at, login_at)` 落在**活动窗口**内,**不是** `refresh_ttl`(720h=30 天)—— 用后者的话一个 30 天里登录过 4 次的正常人**每次登录都报异常**,阈值形同虚设。检测**永不阻断登录**(查库/投递失败只记日志)。17 + 14 单测 + 3 组可逆探针 | 用户被盗号时无法感知(**已解决**:多端/异地同时登录会收到站内信提醒) | P2 ✅ |
| 12 | **token 主动刷新调度**(access 即将过期时自动续) | ✅ M18:`utils/tokenScheduler.ts` `doRefresh()` 单飞锁 + `scheduleTokenRefresh()` 提前 5min 续期;`setLogin` 后挂上,reset 取消(commit e5c4b1d)。2026-10-21 补:**续期响应里的新 `refresh_token` 必须落盘** —— 后端已改单次使用轮换,不落盘会出现「第一次续期成功、之后一直失败」;同时 4 处 `await import()` 改静态导入,直接调 `useUserStore.setTokens()`。🔴 **2026-10-25 M8 补:续期失败不再直接登出** —— 走跨标签页恢复链(见 §1.2 缺口 E),并加「**兄弟已经换过 token 就不发请求**」的轮换检测(见下方 M8 条) | 调度漏了会让用户被硬踢,表现为「明明在操作却莫名回到登录页」;多标签页场景下旧行为是「开两个标签页必有一个被踢」 | P1 ✅ |

#### ❌ 未完成(0 项)

> **范围外,不做**(2026-11-03 用户拍板,移出账本不进计数):原 #9 **JWT 算法切换 RS256 准备** —— 单体后端签发/验签同进程,secret 不出服务器,RS256(非对称)带不来实际安全增益,反而多一套密钥管理;HS256 够用。哪天拆出"第三方服务要验 token 但不该持有签发权"的架构再重新立项。技术记录:`pkg/rsajwt/` 2026-10-21 已删除(12 个单测但全仓零引用,是一套只被自己的测试调用的假信心;真正在跑的 HS256 `pkg/auth` 当时零测试)。

> **复用价值**:**高** —— 任何项目的鉴权基座
> **P1 #10 已完成**(M24 `pkg/auth/revocache/` 跨实例 pub/sub 广播)
> **P1 #12 已完成**(M18 `utils/tokenScheduler.ts` 5min 提前调度 + 单飞锁)

---

## 1.3 ✅ 中间件层(100%)

#### ✅ 已完成(15/24)

| # | 中间件 | 证据 | 行数 |
|---|---|---|---|
| 1 | Auth(JWT 校验) | `api/middleware/auth.go:1-209`;🔴 **M105 加固**:`ExtractToken` 原先有一句"兼容 query(仅供调试)"的 `?access_token=` 兜底,挂在**所有受保护路由**上 —— token 进 URL 就进 nginx access log / 浏览器历史 / Referer;已删除(SSE 端点另有 `SSEAuth` 的三级取值,不受影响)。详见 §1.3.2 | 209 |
| 2 | Principal(身份加载) | `api/middleware/principal.go:1-86` | 86 |
| 3 | RBAC(决策) | `api/middleware/rbac.go:1-217` | 217 |
| 4 | Trace(X-Request-Id) | `api/middleware/trace.go:1-127`;🔴 **M105 加固**:ID 此前**只写响应头**,而 5 处读点读的是**请求头** → access.log / 审计表 / `Caller.RequestID` / **每个响应体的 traceId** 全是空串;客户端传值也**未消毒**(超长值落进 `size:64` 的列 → 审计写不进去)。详见 §1.3.2 | 127 |
| 5 | AccessLog(含 trace_id / span_id) | `api/middleware/access_log.go:1-35` | 35 |
| 6 | Audit(操作审计) | `api/middleware/audit.go:1-58` | 58 |
| 7 | CSRF(Origin 校验) | `api/middleware/csrf.go:1-125`;🔴 **M105 加固**:通配符 `*.example.com` 此前判的是 `HasSuffix(origin, base)` → **`https://evil-example.com` 也放行**(缺域名标签边界);另订正 `RequiredHeader` 的注释漂移。详见 §1.3.2 | 125 |
| 8 | RateLimit(Redis ZSET 滑动窗口) | `api/middleware/ratelimit.go:1-79`;🔴 **M105 加固**:`dim=user` 而 ctx 里没有 caller 时**静默**降级成按 IP,一个字都不吭 → "限流没按预期生效"查不出来;现补 WARN。详见 §1.3.2 | 79 |
| 9 | Recover(panic) | `api/middleware/recover.go:1-27` | 27 |
| 10 | Middleware(编排) | `api/middleware/middleware.go:1-67` | 67 |
| 11 | **CORS**(`hertz-contrib/cors`,所有中间件之前) | `api/router/router.go:10-37` `h.Use(cors.New(...))` | - |
| 12 | **健康检查** `/healthz` `/readyz` ✅M3 | `api/handler/v1/health.go:1-116` `HealthHandler.Liveness` 进程活 + `Readiness` DB SELECT 1 + Redis PING 500ms 超时(commit ba0aa84) | 116 |
| 13 | **OpenTelemetry trace 导出** | (见 §1.8 #7,挂在本层) `api/middleware/otel.go:1-152` `Otel(serviceName)` 中间件 + `pkg/otelx/otelx.go:1-115` `Init/Shutdown`;🔴 **M105 加固**:trace id 原先写在 `c.Next` **之后** → handler panic 时栈展开跳过它 → **Recover 兜出来的 500 没有 trace id**(而这恰恰是最需要标识的时候)。详见 §1.3.2 | 152+115 |
| 13a | **客户端 IP 解析(可信代理网段)** ✅M105 | `api/middleware/clientip.go:1-127` `ClientIPFunc(trustedCIDRs)` + `config/clientip.go` `ClientIPConfig.TrustedCIDRs` + `main.go` `SetClientIPFunc`;Hertz 默认**信任所有对端**且取 XFF 最左项 → 按 IP 的限流能被一个请求头击穿、审计表的 ip 由客户端自己填。详见 §1.3.2 | 127 |
| 14 | **Timeout(per-route)** + **Breaker(gobreaker)** ✅M5 | `api/middleware/timeout.go:1-84` `Timeout(d)` ctx soft cancel + `api/middleware/breaker.go:1-202` `Breaker(name)` gobreaker per-name manager;挂载点:biz.owner/issue/material 5s+biz.upload 15s+platform.wxtemplate.send 10s(commit 40070d9);🔴 **M105 加固**:超时判据恒假(返回空 200 而不是 504,且熔断不把超时计为失败)、熔断快速失败**没 Abort**(下游照跑、响应被盖成畸形 JSON)、`failure_rate_threshold` 是**死配置键**(配了不生效还无提示)。详见 §1.3.2 | 84+202 |

#### ✅ 已完成(9 个)

| # | 子项 | 价值 | 成本 | 优先级 |
|---|---|---|---|---|
| 15 | Gzip 压缩 | ✅ M9a:`api/middleware/gzip.go` 用 hertz-contrib/gzip 中间件,默认 ≥1024B 启用,compress level 5(commit 63d98a4) | P1 ✅ |
| 15a | 请求体 Gzip 解压 | ✅ M47:`api/middleware/reqgzip.go` `RequestGzip()` 中间件 — 读 Content-Encoding,gzip → 解压改写为 identity + SetBody 写回,identity/空 → 透传,brotil/deflate → 50701 UnsupportedMediaType,gzip 损坏 → 50700 PayloadMalformed,解压后 > 10MB → 413 PayloadTooLarge(防 zip bomb);挂 router.go BodyLimit→Gzip 之间(handler 拿明文透明);2 错误码 PayloadMalformed=50700 / UnsupportedMediaType=50701 + 默认文案;7 单测 -race 干净(无 encoding / identity / gzip 解压中文 / 大小写不敏感 / 损坏 body / 不支持编码 / 空 body 透传) | 中 | P2 ✅ |
| 22 | 安全响应头(SecHeaders) | ✅ M48:`api/middleware/secheaders.go` `SecHeaders(opts...)` 中间件 — 默认开 X-Content-Type-Options:nosniff / X-Frame-Options:DENY / Referrer-Policy:strict-origin-when-cross-origin / X-XSS-Protection:0 / HSTS:max-age=63072000;includeSubDomains(HTTPS 时) / Permissions-Policy 关闭 8 个敏感 API(geolocation/microphone/camera/payment/usb/accelerometer/gyroscope/magnetometer);显式 opt-in CSP(WithCSP)避免 API/SPA 差异;WithFrame/WithHSTS/WithPermissionsPolicy/WithExtraHeader 可定制;删 Server / X-Powered-By 防 stack 泄露;挂 router.go Trace 后(最早保证所有响应带);11 单测 -race 干净(默认 6 头 / FrameSameOrigin / FrameNone 不发 / HSTS off / CSP 注入 / CSP 不设 / PermPolicy 全开空 / 5 项 PermPolicy / ExtraHeader / Strip Server / buildPermPolicy 边界) | 中 | P2 ✅ |
| 16 | 请求体大小限制 | ✅ M9b:`api/middleware/bodylimit.go` 10MB 默认,可配;超限 413(commit 3b9cb95) | P1 ✅ |
| 17 | 幂等键 Idempotency-Key 框架级处理 | ✅ M9c:`pkg/idempotentx/` Redis 锁 30s + 结果缓存 24h;`api/middleware/idempotency.go` 写操作自动注入 key + 处理(commit 93dc607)。**✅ M97 加固**:中间件不再把「业务没跑成」的响应(401/403/413/429,或下游 `c.Abort()`)当幂等结果缓存 —— 旧实现把 401 缓存 24h,前端刷新 token 后**用同一个 key 重发**时重放 401,写操作永远不执行且用户被踢回登录页;键空间拆成 `idem:res:` / `idem:lock:`(旧的 `idem:<k>` / `idem:<k>:lock` 两个命名空间重叠,会互相删对方的 key);panic 时释放锁(旧实现锁挂 30s,重试只拿到误导性的 409);补 `pkg/idempotentx` **第一张**测试网(11 例)+ 中间件 HTTP 级回归网(8 例)—— 详见 §1.3.1 | P1 ✅ |
| 18 | SSE / WebSocket 鉴权 | 中 | 1 天 | P1 ✅ M26 |
| 19 | API 版本协商 | ✅ M43:`pkg/apiverx/` 解析 X-API-Version(v 前缀/小数点后截断/0/-1/字母混合返默认)+ `WithVersion`/`Major` ctx 流转 + `Opt{MaxMajor}` 强校验配置 + `api/middleware/apiver.go` `APIVersion()` 中间件(挂 router.go Gzip→Idempotency 之间,422 返 errcode.VersionNotSupported=50500);handler 用 `apiverx.Major(ctx)` 分支;5 apiverx 单测(含 14 个 Parse 用例含边界)+ 7 apiver middleware 单测 -race 干净 | 中 | P2 ✅ |
| 20 | 接口签名(防重放) | ✅ M44:`pkg/signx/` HMAC-SHA256 原语(拼接 METHOD\nPATH\nTS\nsha256(BODY) → hex sig)+ `ParseHeader` 兼容 t=/ts=/timestamp + sig=/signature + `ValidTS` ±skew 校验 + `Verify` 用 `subtle.ConstantTimeCompare` 防计时攻击;`api/middleware/sign.go` `Sign(secret, opts...)` 写方法(POST/PUT/PATCH/DELETE)生效,GET/HEAD/OPTIONS 透传;3 错误码 SignatureMissing=50600/Expired=50601/Invalid=50602 + 默认文案;`peekBody` 优先 `c.Request.Body()`(缓冲)再 fallback `BodyStream()`(流式),`SetBody` 写回保证下游仍能读;`WithSkew` + `WithClock` 可注入时钟(测试);**不挂全局链**(按需在 route group 启用);7 signx 单测 + 8 sign middleware 单测 -race 干净 | 中 | P2 ✅ |
| 21 | 灰度 header 透传 | ✅ M40:`pkg/grayx/` ctx 流转 `WithReleaseTag`/`ReleaseTag`(3 header 常量 X-Release/X-Canary/X-Cohort)+ `api/middleware/gray.go` `GrayRelease(headerName...)` 入口读 header 注入 ctx + 响应回写;pickTag 优先级 primary > X-Canary > X-Cohort;router.go 在 Trace+Otel 后挂 `GrayRelease("X-Release")`;5 grayx 单测 + 7 middleware 单测 -race 干净(commit 6a1ba5e) | 低 | P2 ✅ |

> **复用价值**:**高** —— 标准中间件栈
> **P0 全部完成** —— 2026-10-19 之后无 P0 中间件待补
> **框架层 P0 已全部完成** —— §3 #10(23 个业务 endpoint)属**业务线**;§2.10 #13 是**框架文件里的业务类型单薄**,仍算框架层欠账(P1),见 §2.10

### 1.3.1 幂等键审计(2026-10-26,M97)

§1.3 #17 从 M9c 落地那天起就标着 ✅,但**那一行从来没被读过**。M97 把它当成一次
"框架级原语的零覆盖"来审 —— 结论是:它站在**所有写接口**的路径上,而它自己**一张
测试都没有**;补网的过程**当场抓出三条真 bug**(不是"补了个网没出事")。

> **🔴 2026-10-26(M97)审计结论**:`pkg/idempotentx/` + `api/middleware/idempotency.go`
> 挂在 `router.go` 的**全局链**上 —— `router.go` 里 245 条路由注册(其中 143 条是写操作)
> 每一个 POST/PUT/PATCH/DELETE 都过它。**它此前零测试覆盖**。补网时抓出的三条真 bug:
>
> - **bug ①(高危):「业务没跑成」的 401 被当成「跑过了」缓存 24h**
>   旧实现是 `if status >= 200 && status < 600 { Save }`。而 401 是**鉴权中间件**产生的
>   (`Auth` 是组级中间件,位于幂等中间件的 `c.Next` **之内**),于是整条链变成:
>   客户端带 key K 发写请求 → 401(比如 access token 刚好过期)→ 服务端把 401 当成
>   "结果"缓存 24h → 前端 axios 拦截器刷新 token 后**用同一个 config 重发**
>   (同一个 config = 同一个 `Idempotency-Key`)→ 服务端命中缓存,把 401 **原样重放**
>   → 业务永远不执行;前端第二次收到 401 又走 `handleUnauthorized()`,**把用户踢回
>   登录页** —— 尽管 token 刚刷新成功、其实完全有效。
>   症状是"**偶尔被踢回登录页**",且必现于"token 刚好过期时点提交"那一刻。
>   **修法**:新增 `cacheableResult(c, status)` 判据 —— ① `c.IsAborted()`(下游中间件
>   拒了 = 框架自己拒的,业务没跑);② 状态码是"拒绝"语义(401/403/413/429)时同样
>   没跑(handler 层的前置守卫不 Abort,只能靠状态码抓)。被拒的响应**不缓存**,
>   **释放锁** —— 客户端拿同一个 key 重试能真的跑起来。
>   ⚠️ **5xx 不在拒绝之列**:handler 抛错返回的 500、超时的 504,业务**可能已经写了
>   数据**,缓存它 = 至多一次(宁可少跑一次,不能重跑)。这条边界不能动。
> - **bug ②(契约级):结果 key 与锁 key 的命名空间重叠**
>   旧实现是 `idem:<k>`(结果)与 `idem:<k>:lock`(锁)。两个命名空间**重叠**:客户端
>   key `"foo:lock"` 的**结果 key** 正好是客户端 key `"foo"` 的**锁 key**。后果是两边
>   互相破坏:`Save("foo")` 会 `DEL "idem:foo:lock"`(删掉 `"foo:lock"` 的结果),
>   `Save("foo:lock")` 会 `SET "idem:foo:lock"`(**覆盖** `"foo"` 的锁)。于是
>   `"foo:lock"` 的结果凭空消失 → 业务**重跑一遍**(幂等要防的正是这个),而且
>   **没有任何报错**。今天没炸过(前端 key 是 UUID,不含冒号),但键空间不是单射就是
>   **契约级的错** —— 下一个用带冒号的 key 的调用方就会踩。
>   **修法**:拆成 `idem:res:` / `idem:lock:` 两个前缀。
> - **bug ③:panic 时锁不释放**
>   旧实现只在"状态码异常"分支里 `Release`。而 panic 走的是 **unwind** ——
>   `c.Next(ctx)` 之后的代码**根本不执行**,于是:业务崩了 → 锁留在 Redis 里(TTL 30s)
>   → 客户端拿同一个 key 重试 → 抢不到锁 → 拿到 **409 "request in flight"**,而
>   实际上**没有任何请求在跑**。一次 panic 让这个写操作 30 秒内彻底发不出去,而且报的是
>   "并发冲突"这种完全误导人的错。**修法**:`defer` 里按 `lockHeld` 标志释放。
>
> **顺带修掉的第 4 条**:`AcquireOrGet` **漏了空 key 守卫**(`Save`/`Release` 都有)。
> 空 key 会去抢 `idem:lock:` 这一把**所有人共用的锁**,而 `Save`/`Release` 因为空 key
> 直接 return —— 那把锁**永远不会被释放**,之后每个空 key 的请求都拿到 409。旧代码的
> 表现是"第一次 acquired=true(看着正常),第二次 ErrInFlight" —— 一条"看起来能跑通、
> 实际把所有人锁死"的假象。
>
> **死代码清理**:`StateRunning`("running")从未被写过 —— "正在跑"这个状态活在**锁 key
> 的存在性**里,结果 key 里永远只有 done / failed。留着那个常量是有害的:它让人以为
> "可以靠读结果的 State 区分在不在跑",于是会写出"结果里 State==running 就当未命中"
> 这种错代码 —— 而结果 key 里压根不会出现 running,那段判断永远不成立,于是"在跑"被
> 当成"已跑完"重放出去。M97 删掉它,并把包注释里那个**从未实现的单 key 状态机**
> (`notexist ─SETNX→ running ─业务完成→ done`)改成描述真正实现的两把 key。
> 另修一处文档漂移:包注释写的是 `X-Idempotency-Key`,而实现读的是 `Idempotency-Key`
> —— 那是全仓**唯一**一处提到 `X-Idempotency-Key` 的地方。

**补网的手段与规模**:

| 层 | 文件 | 用例数 | 覆盖什么 |
|---|---|---|---|
| 原语 | `internal/pkg/idempotentx/idempotentx_test.go` | 11 | 拿到锁 / 命中结果 / 并发在飞 / 释放锁 / 空 key / 键空间单射 / Save 删锁 / 失败态 / 无 Redis 降级 / Redis 挂降级 / TTL / 坏结果当未命中 |
| 中间件 | `internal/api/middleware/idempotency_test.go` | 8(11 子用例) | 下游拒了不占槽位(含 casbin 异常那条 500) / handler 拒了不占槽位 / 2xx 缓存并重放 / 业务 400 缓存 / 同 key 并发 409 / panic 释放锁 / GET 忽略 / 无 key 放行 |

底层用 **miniredis**(进程内**真** Redis:真 RESP 协议 / 真 SETNX / 真 TTL),
不是 mock —— 走的是 go-redis 的真命令路径。中间件那层用 hertz 的 `ut.PerformRequest`
起**真引擎**,所以 401/403/409/abort 这些是**真的响应码**,不是构造出来的。

**可逆探针 11/11 全红**(拆实现看测试会不会红):P1 去掉 `IsAborted()` 判据 /
P2 去掉状态码判据 / P3 永远走缓存分支(回到旧逻辑)/ P4 panic 不释放锁 /
P5 去掉空 key 守卫 / P6 结果 key 退回 `idem:`+k / P7 锁 key 退回 `idem:`+k+`:lock` /
P8 Save 不删锁 / P9 命中结果时忽略缓存 / P10 失败态不写 `StateFailed` /
P11 无 Redis 时不放行。

> ⚠️ **P1 是"被变异的条件不是唯一在挡的"那条坑**:去掉 `IsAborted()` 判据后,
> 401/403/429 那几条子用例**照样是绿的**(它们同时被状态码判据挡住)。所以 P1 补了
> 一条**只能**被 `IsAborted()` 挡住的子用例 —— RBAC 里 casbin 异常那条分支
> (errcode.Internal → **500**,"权限校验失败"):框架自己拒了、业务压根没跑,
> 却是 5xx,按状态码看它是"业务失败,缓存起来"。
> 同理 P11 不能写成"删掉 `defer`" —— 那样 `lockHeld` 变成未使用变量,
> `go test` 直接 **build failed**。**编译失败不是变红**:它证明不了"这条断言抓得住
> 这个 bug"。改成把条件反过来(`if !lockHeld` → `if lockHeld`)才真的红在断言上。

**本次改动没有碰前端**:web 的 axios 拦截器在 401 之后**用同一个 config 重发**
(`web/src/api/client.ts`),同一个 config = 同一个 `Idempotency-Key` —— 那是**前端
该有**的行为(重试必须带同一个 key,否则幂等失效)。**服务端才是"这个 key 能不能
占槽位"的裁判**,改服务端就对所有调用方生效(包括小程序)。

**§1.3 项数不变(23 项,仍 100%)** —— M97 是对 #17 的**加固**,不是新增子项:
把 #17 从"实现了但没人读过"变成"读过了、测过了、还当场修掉三个真 bug"。

### 1.3.2 中间件层审计(2026-10-28,M105)

**起因**:§1.3 从 M9/M26 之后就没再被读过 —— 清单上 14 个 ✅ 是"**写完了**"的 100%,
不是"**验过了**"的 100%。而这一层**整层挂在全局链上**:`Trace` / `Otel` / `AccessLog` /
`Audit` / `CSRF` / `RateLimit` / `Timeout` / `Breaker` / `SSEAuth` 每一个都在
`router.go` 的全局路径上,而**其中 7 个中间件一张测试都没有**。

> 判高危的客观标准是"**挂在全局中间件链上吗**"(见 memory:框架原语零覆盖)。
> 清单上标 ✅ ≠ 那行被读过 —— §1.3 从 M5/M9 之后就没再被读过。

M105 把 §1.3 的每一个 ✅ 都拆开读了一遍,读出 **13 个真 bug + 1 个死配置键**
(其中 1 个是**查证后撤回的假发现**,见下面"被探针推翻的那一条"):

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `api/middleware/timeout.go` `responseUnwritten` | 判据是 `c.Response.StatusCode() < 100` | Hertz 的 `ResponseHeader.StatusCode()` 在**没写过响应时返回 200**(`hertz@v0.9.6 pkg/protocol/header.go:978`),不是 0 → `200 < 100` **恒为假** → 超时分支**永远不成立** → 超时的请求返回**空 body 的 200** 而不是 504;连带:熔断判失败看 `status >= 500`,一个 200 的空响应**不算失败** → 熔断器从不把超时计为失败 → "连续失败 N 次跳闸"对超时场景**完全失效** |
| B2 | `api/middleware/trace.go` | `X-Request-Id` / `X-Trace-Id` 只写**响应头**,而 5 处读点读的是 `c.GetHeader`(= **请求头**) | `access.log` 的 requestId、`sys_operation_log.request_id`、PII 审计表的 request_id、`Caller.RequestID`、以及**每一个 JSON 响应体里的 `traceId` 字段** —— 全是空串。前端拿 traceId 报障,报上来的是空字符串 |
| B3 | `api/middleware/trace.go` | 客户端传的 `X-Request-Id` / `X-Trace-Id` **原样透传** | 长度不限 → 落进 `sys_operation_log.request_id` 的 `size:64` 列 → MySQL 严格模式直接 "Data too long for column" → **这次请求的审计记录写不进去**;字符不限 → 换行 / 控制字符进日志行(日志注入)与响应头 |
| B4 | `api/middleware/otel.go` | trace id 写在 `c.Next(ctx)` **之后** | handler panic → 栈展开跳过那一行 → Recover 兜出来的 500 **没有 trace id** —— 而"出错了却查不到链路标识"恰恰是最需要标识的情况;且只写响应头 → 请求侧一直是 Trace() 放的占位符,audit.log / 审计表 / Caller 里记的是占位符而不是真的 W3C trace_id |
| B5 | `api/middleware/sseauth.go` | 只有 jti 一道闸,注释却写着"与 Auth() 中间件保持同一套吊销判定" | Auth() 有**三道闸**(jti / 会话 sid / 作废线),这里只有第一道。被"强制下线"踢掉的那台设备 —— 它手里那张 access 的 jti 没变、签发时刻也在作废线之前 → jti 吊销**查不出来** → **被踢的设备照样能连 SSE**,还能再活 8 小时。SSE 是长连接,比普通接口活得久,少两道闸的代价更大 |
| B6 | `api/middleware/sseauth.go` | `service.Caller.SessionID` 没填(Auth() 里有) | service 层拿 `caller.SessionID` 判"是不是同一条会话"时,SSE 这条路**永远是空串** |
| B7 | `api/middleware/csrf.go` `originAllowed` | 通配符判的是 `HasSuffix(origin, base)` | `*.example.com` 的 base 是 `example.com` → **`https://evil-example.com` 也以 "example.com" 结尾** → 放行。同理 `notexample.com` / `example.com.evil.com` / `example.computer` / `example.org` 都能过 —— 通配符形同虚设 |
| B8 | `api/middleware/csrf.go` `RequiredHeader` | 注释写"只要带 X-Requested-By: XMLHttpRequest 即视为合法",代码**只判存在、不判值** | 注释与实现不符(文档漂移)。**改注释不改实现**:CSRF 的防线是"这个非简单头必须经过 CORS 预检",跨域页面**根本发不出**这个头,值是什么不影响这个性质;加值判断只会把"头在但值不对"的合法客户端挡在门外,换不来任何安全性 |
| B9 | `api/middleware/ratelimit.go` | `dim=user` 但 ctx 里没有 caller 时**静默**降级成按 IP | "按用户限流"降级成"按 IP 限流" = 同一 NAT 出口的一整栋楼共用一个桶(互相挤兑),换个出口就换个桶 → 限流形同虚设,而且**一个字都不吭** —— "限流没按预期生效"根本查不出来 |
| B10 | `api/middleware/breaker.go` | ~~阈值配成 0 会"永远跳闸"~~ | **⚠️ 查证后撤回的假发现** —— 详见下面"被探针推翻的那一条"。现在改成了"0 = 不因连续失败次数跳闸"的**明确语义** |
| B11 | `api/middleware/auth.go` `ExtractToken` | 有一句"兼容 query(仅供调试)":`?access_token=` | 它挂在**所有受保护路由**上,不是调试开关:token 进 URL → 进 nginx access log、进浏览器历史、进 Referer(跳第三方站点时)。而 SSEAuth 本来就有自己的三级取值(Authorization → ?access_token= → cookie),这条兜底的实际受益者是"手工拿 URL 调接口"的人,代价是**全站多一个 token 泄漏面** |
| B12 | Hertz 默认 client IP | `defaultTrustedCIDRs = 0.0.0.0/0 + ::/0`(**信任所有对端**),且在全信任前提下 `validateHeader` 取 XFF 的**最左**项 | 那一项是**客户端自己填的**:① 按 IP 的限流(`RateLimit(..., "ip")`,登录防爆破就建在这上面)可以被**一个请求头击穿**(每次换一个 XFF = 一个新桶);② 审计表 `sys_pii_access_log.ip`(等保项)记的"谁看了手机号"由客户端自己填,事故复盘时拿到的 IP **没有证据价值** |
| B13 | `api/middleware/breaker.go` 快速失败分支 | 写了响应就 `return`,**没有 `c.Abort()`** | hertz 的 `RequestContext.Next()`(`hertz@v0.9.6 pkg/app/context.go:830`)是个 `for` 循环 —— **不调 `c.Next()` 并不能阻止后面的 handler 执行**,真正能断链的只有 `c.Abort()`。实测:熔断打开后业务 handler **每次仍然被执行**(ran 1→2→3),它的响应盖掉熔断的 503 → 客户端拿到 500 而不是 503;响应体还是 `{"code":50300,...}` 与 handler body **拼在一起**的畸形 JSON → 前端 `JSON.parse` 直接失败。**熔断本该保护的下游,压根没被保护住** |
| B14 | `internal/config/breaker.go` `FailureRateThreshold` | `failure_rate_threshold` 在结构体里声明了、在两份 yaml 里都配了 `0.5`,但**全仓没有任何代码读它** | 运维在 yaml 里看到 `failure_rate_threshold: 0.5`,会理所当然地以为"失败率到 50% 就跳闸" —— 而它**完全不生效**,且**没有任何提示** |

**修法**(一行一处,全在框架层):

- **B1 →** 判据换成 `c.Response.StatusCode() == 200 && len(c.Response.Body()) == 0`
  ("status 还是默认 200 **且** body 为空"才算没写过):handler 写了 200 + 有 body → 不覆盖;
  写了 500 / 403 / 204 → status ≠ 200 → 不覆盖;什么都没写 → 补 **504**。
- **B2 →** `Trace` 把两个 ID **写回请求头**(`c.Request.Header.Set`),顺序放在响应头之后,
  保证"响应头 = 请求头 = 同一个值" —— 5 处读点**一处都不用改**。
- **B3 →** 新增 `sanitizeRequestID`:长度 1..64(= 审计表列宽)、字符集 `[A-Za-z0-9._:-]`,
  不合格就**丢弃客户端的值改用自己生成的** —— 宁可换一个 ID,也不让客户端控制审计字段。
- **B4 →** span 一起就把 trace_id 写进**请求头 + 响应头**(trace_id 在 `Start` 之后立刻确定,
  提前写没有副作用)。
- **B5 →** `SSEAuth` 补齐 Auth() 的另外两道闸:`auth.IsSessionRevoked(ctx, claims.SessionID)`
  (会话吊销)+ `auth.SessionInvalidated(ctx, claims)`(作废线)。
- **B6 →** `Caller.SessionID` 填上 `claims.SessionID`。
- **B7 →** 通配符改判**域名标签边界**:`HasSuffix(origin, "."+base)`,且 base 非空。
- **B8 →** 只改注释,并把"本项目后台前端**不发** X-Requested-With(axios 1.7 默认不带),
  而 Bearer 请求被 `ExemptTokenAuth` 豁免整套 CSRF 校验 —— 所以这条检查在当前流量下从不触发"
  这个事实写进去(**一旦有人关掉 ExemptTokenAuth,所有写请求会集体 403**)。
- **B9 →** 降级保留,但补一条 `L().Warn("ratelimit: user 维度降级为 ip…")`,带 scene + path。
- **B11 →** 删掉 `?access_token=` 兜底,只留 Authorization header。
- **B12 →** 新增 `internal/api/middleware/clientip.go` `ClientIPFunc(trustedCIDRs []string)`
  + `internal/config/clientip.go` `ClientIPConfig.TrustedCIDRs`,
  经 `h.Engine.SetClientIPFunc(...)` 在 `main.go` 装配。默认可信网段 = **回环 + RFC1918 私有段**
  (127.0.0.0/8、::1/128、10.0.0.0/8、172.16.0.0/12、192.168.0.0/16)。
  **不重写算法** —— Hertz 自带的 `app.ClientIPWithOption` 已经是"取最右侧不可信项"的标准口径,
  只换掉那份默认 CIDR 列表(自己重写一遍只会引入第二套口径)。
  ⚠️ 代理挂在**公网 IP** 上时(云 LB 直连应用)对端不在默认网段 → XFF 被忽略 → 按 IP 的限流
  退化成全局限流 → 此时把它的网段写进 `client_ip.trusted_cidrs`。
- **B13 →** 快速失败分支补 `c.Abort()`。
- **B14 →** 启动时打 WARN 说清"这个配置当前未实现"。**不实现它** —— 启用它等于给 5 组业务路由
  (biz.owner / biz.issue / biz.material / biz.upload / platform.wxtemplate)加一条**新的跳闸条件**,
  那是行为变更,不该在一次审计修复里悄悄生效。

**补网的手段与规模**:

| 层 | 文件 | 用例数 | 覆盖什么 |
|---|---|---|---|
| 中间件 | `internal/api/middleware/trace_test.go`(新) | 7 | 无客户端 ID 时自己生成 / **写回请求头** / 合规 ID 透传 / **超长 ID 被拒** / **畸形 ID 被拒** / IP+UA 进 ctx / `sanitizeRequestID` 单元 |
| 中间件 | `internal/api/middleware/timeout_test.go`(新) | 6 | **没写响应 → 504** / handler 写了响应不覆盖 / handler 写了错误码不覆盖 / 快请求不受影响 / d ≤ 0 是 noop / `responseUnwritten` 判据本身 |
| 中间件 | `internal/api/middleware/breaker_test.go`(新) | 7 | **阈值 0 的语义** / 连续失败到阈值跳闸 / **open 时下游一次都不能跑** / 4xx 不算失败 / 全局开关关掉透传 / 未初始化不 panic / **死配置键要留痕** |
| 中间件 | `internal/api/middleware/csrf_test.go`(新) | 7 | **通配符的标签边界** / 精确匹配与 `*` / **伪造 Origin 被中间件拒** / RequiredHeader 必须存在 / Bearer 豁免 / 安全方法不查 / 关掉透传 |
| 中间件 | `internal/api/middleware/ratelimit_test.go`(新) | 4 | **第 max+1 次必须 429** / user 维度的桶键是用户 / 换用户换桶 / **降级要留痕** |
| 中间件 | `internal/api/middleware/sseauth_test.go`(新) | 7 | **会话被吊销 → 拒** / **用户被作废 → 拒** / **Caller.SessionID 有值** / 合法 token 放行 / jti 吊销 → 拒 / 无 token → 拒 / refresh token → 拒 |
| 中间件 | `internal/api/middleware/clientip_test.go`(新) | 10 | **伪造 XFF 被忽略** / Hertz 默认**会**采信(对照组) / 可信代理取最右侧不可信项(3 子用例) / 可信代理也忽略伪造的最左项 / X-Real-IP 兜底 / 畸形 XFF 回落对端 / **CIDR 配错 = 谁都不信** / 显式信任全部 / CIDR 解析 / CIDR 序列化 |
| 扩展 | `internal/api/middleware/auth_test.go` | +1 | **`?access_token=` 打受保护路由必须被拒** |
| 扩展 | `internal/api/middleware/otel_test.go` | +2 | 请求头与响应头的 trace id 一致 / **panic 路径仍带 trace id** |

合计 **51 个新用例**(7 个新文件 + 2 个扩展文件),中间件包从 121 → **172 个用例,全绿**。

⚠️ **三个"写测试时踩出来"的坑**(值得记下来,下次写中间件测试还会踩):

- **`ut.PerformRequest` 走的是 `engine.NewContext()`,不是 `engine.allocateContext()`** ——
  后者才会 `ctx.SetClientIPFunc(engine.clientIPFunc)`。所以 **`e.SetClientIPFunc(...)` 在测试里
  完全不生效**,所有请求的 IP 都是 `0.0.0.0`。想让 IP 可控,要塞一个假 conn:
  `c.SetConn(&fakeConn{remote: &net.TCPAddr{IP: ..., Port: 12345}})`。
- **Hertz 的 `Response.StatusCode()` 没写过响应时返回 200** —— 这正是 B1 的根因,
  也是"断言状态码"类测试最容易写错的地方。
- **`otel.Tracer(...)` 在中间件**构造**时就把 tracer 拿走了** —— 测 trace id 必须**先**装
  `sdktrace.NewTracerProvider()` 再构造中间件,否则 tracer 是 no-op,`SpanContext().IsValid()` 恒 false。

**可逆探针 14/14 全红**(scripts/probe_m105.mjs,拆实现看测试会不会红):
① timeout 判据退回 `StatusCode() < 100` / ② trace 不写回请求头 / ③ trace 不消毒客户端传值 /
④ otel 的 trace id 写回 `c.Next` 之后 / ⑤ sseauth 去掉会话吊销那道闸 /
⑥ sseauth 去掉作废线那道闸 / ⑦ sseauth 的 `Caller.SessionID` 不填 /
⑧ csrf 的通配符退回"无标签边界" / ⑨ ratelimit 的降级不留痕 /
⑩ breaker 不再把 threshold=0 判成"永不跳闸" / ⑪ auth 恢复 `?access_token=` 兜底 /
⑫ client IP 退回"信任所有对端" / ⑬ breaker 的快速失败不 Abort /
⑭ 死配置键不再留痕。

> ⚠️ **被探针推翻的那一条(B10)**:M105 一开始把"阈值配成 0 → 熔断永远跳闸"当成一个真 bug,
> 并写了"夹逼到 ≥1"的修复 + 一条守卫用例。**探针把夹逼逻辑整段拆掉,用例没有变红** ——
> 于是回头去读 `sony/gobreaker@v1.0.0` 的源码,发现:
>
> ```go
> func (cb *CircuitBreaker) onFailure(state State, now time.Time) {
>     case StateClosed:
>         cb.counts.onFailure()             // ← ConsecutiveFailures 先 ++
>         if cb.readyToTrip(cb.counts) {   // ← 才问
> ```
>
> `ReadyToTrip` **只在失败之后**被问,而那时 `ConsecutiveFailures` 已经 ≥ 1 ——
> 所以 `>= 0` 与 `>= 1` **行为完全一样**(都是"第一次失败就跳闸"),那个"夹逼"是**空操作**。
> 原用例只发成功请求,而成功路径根本不问 `ReadyToTrip`,所以它压根没碰到这个分支 ——
> **一条假绿的用例**。
>
> 处理:把 B10 **撤回**(不当成 bug 记),并把 0 的语义**定死**为"不因连续失败次数跳闸"
> —— 这是零风险的(0 在任何配置文件里都没出现过,default=5),同时消掉一个真实的坑:
> "配 0"看起来像"没配 / 不限制",实际却是**最激进**的设置,两者直觉正好相反。
> 现在的用例真的踩在 `ReadyToTrip` 上(连发 3 次失败),探针 ⑩ 拆掉那句 early-return 就变红。
>
> **教训**:"夹逼"这种防御性写法**看起来**在防什么,但只有行为真有差别时才是修复。
> 探针是唯一能分辨"真修复"与"空操作"的东西 —— **用例自己也会假绿**。

**本次改动没有碰前端**:变的全是"这些约定**有没有真的生效**"。

- 唯一动了对外形状的是 **`client_ip.trusted_cidrs`** 这个**新增配置键**(默认回环 + RFC1918),
  前端不感知;同 commit 已把它登记到三处:
  `server/configs/config.example.yaml`(`client_ip:` 段)、`server/configs/config.dev.yaml.example`、
  `docs/配置项清单.md` §8「不上 sys_config 的配置项」+ 技术方案 §4.3 / §9.1 攻击防御矩阵;
- `X-Request-Id` / `X-Trace-Id` 的**取值**变了(客户端传超长/畸形值时不再被原样透传),
  但**格式不变**(还是原来那套 ID),前端不受影响;
- **响应体里的 `traceId` 字段从空串变成真值** —— 这是修复,前端拿它报障从此拿得到东西。

**§1.3 项数 23 → 24**(新增 13a「客户端 IP 解析(可信代理网段)」),仍 100% ——
M105 的其余产出都是对 §1.3 已有中间件的**加固**,不是新增子项。


---

## 1.4 ✅ 错误码 / 响应(100%)

#### ✅ 已完成(9/11)

| # | 子项 | 证据 |
|---|---|---|
| 1 | 业务错误码 1xxxx/2xxxx/3xxxx/5xxxx 分层 | `pkg/errcode/errcode.go:1-241` |
| 2 | 业务码 → HTTP 状态映射 | **✅ M98 收口**:`pkg/errcode/errcode.go` `HTTPStatus()` 是**唯一真源**(窄桶在前:401xx/403xx/404xx/409xx/413xx/429xx + 501/503/504 精确命中,其余落 40000/50000 大桶);`pkg/response/response.go` 与 `cmd/gen-errcode-doc` **都改调它** —— 原来同一个映射有三份拷贝、各错各的 —— 详见 §1.4.1 |
| 3 | 业务码 → 默认文案 | `pkg/errcode/errcode.go` Msg() |
| 4 | 统一响应包装 `{code, message, data, request_id}` | `pkg/response/response.go:1-124` |
| 5 | 业务异常 Error struct | `pkg/errcode/error.go:1-65`(**M98**:`CodeOf` 改用 `errors.As`,被 `%w` 包装的业务码不再掉回 `Internal`) |
| 6 | 客户端 IP 解析(X-Forwarded-For 优先) | **✅ M112**:`pkg/httputil/ip.go` **已删除** —— 它是**零调用方的死代码**,实现还是"无条件取 XFF 最左一元素"(攻击者可控)。真正在用的是 Hertz 的 `ClientIPWithOption`:`api/middleware/clientip.go:93-96` 按 `client_ip.trusted_cidrs` 配置可信代理网段(默认 loopback + RFC1918),`cmd/server/main.go:685` 全局装 `SetClientIPFunc`;所有记录点统一用 `c.ClientIP()`(访问日志 / 审计日志 / 登录审计 / 限流 / trace / OTel / SSE 鉴权)。⚠️ Hertz 默认 `TrustedCIDRs = 0.0.0.0/0 + ::/0` = **信任所有对端** ⇒ XFF 最左元素由攻击者控制 ⇒ 按 IP 限流可被绕过、`sys_pii_access_log.ip`(等保项)可伪造 —— M105 已改成显式白名单 |
| 7 | 响应时间毫秒戳 | `pkg/response/time.go:1-7` |
| 8 | 错误码 i18n 翻译入口(前端 tError) ✅M6 | `web/src/types/api.ts:70-100` `BIZ_CODE_KEYS` 34 码映射 + `web/src/locales/useI18n.ts:79-98` `tError(code, fallback, i18nKeyMap)` 三级 fallback + `web/src/locales/{zh-CN,en-US}.ts` `bizCode.*` 命名空间(commit 7c406c7) |
| 8a | 5xx 状态码显式映射(501/503/504) | **✅ M98 收口**:`pkg/errcode/errcode.go` `ServiceUnavailable=50300` → 503 / `GatewayTimeout=50400` → 504 / `NotImplemented=50100` → 501;原先 `pkg/response/response.go` 的私有 `httpStatus` **漏了 501**(且 413 也漏)—— 详见 §1.4.1 |

#### ✅ 已完成(2 个)

| # | 子项 | 当前缺口 | 价值 | 优先级 |
|---|---|---|---|---|
| 9 | 错误码集中文档化(自动生成 md) | ✅ M13:`server/cmd/gen-errcode-doc` AST 解析 `errcode.go` const + defaultMsg map,输出 `docs/reference/error-codes.md`(按 0 / 2xxxx / 3xxxx / 4xxxx / 5xxxx 段分组,带 HTTP 状态映射);缺文案码自动标"_(无默认文案)_"(commit ca546cd)。**✅ M98**:HTTP 状态列改为**直接调 `errcode.HTTPStatus()`**(原来生成器自己抄了一份,漏了 501/503/504,文档因此在撒谎);文档从 44 码补到 **54 码** —— M98 发现生成器上次跑是 **2026-10-19**,之后新增的 **10 个码**(20600-20603 审批段、50500-50701 框架段)**一个都没进文档**;并在 `errcode` 包补测试守住"文档不许过期" | 中 | P1 ✅ |
| 10 | panic 兜底文案统一 | ✅ M42:`api/middleware/recover.go` 走 `errcode.Msg(errcode.Internal)`(不硬编码) + `runtime/debug.Stack` 落 log + `service.FromContext` 拿 user_id + `response.TraceIDFrom` 拿 trace_id;4 单测覆盖无 panic / panic 返 500 / panic 后下一请求 / ctx 缺 caller | 低 | P2 ✅ |

> **复用价值**:**高** —— 业务码 i18n 是出口产品化的关键

---

### 1.4.1 错误码审计(2026-10-26,M98)

**起因**:M97 挖出幂等键的三个真 bug 之后,按同一条线索继续审"清单上早就标着 ✅ 的
框架原语"。§1.4 从 M13 起就是 **100%**、十一项全绿 —— 但这一节是**错误码体系**,
是全站每一个失败响应的出口,比幂等键更靠底层。

**同一个"业务码 → HTTP 状态码"映射,当时有**三份实现**,而且各错各的**:

| # | 位置 | 状态 | 错在哪 |
|---|---|---|---|
| 1 | `pkg/response/response.go` 的私有 `httpStatus` | **活着的**那份 | 没有 413 桶(`41300` → **400**)、没有 501(`50100` → **500**) |
| 2 | `pkg/errcode/errcode.go` 的导出 `HTTPStatus()` | 零调用方 | 大桶 `40000-41000 → 400` 排在细分桶**前面** → `40100/40300/40400/40900` 全被吃掉返 **400**;`50300/50400` 兜底成 500 |
| 3 | `cmd/gen-errcode-doc` 里的 `httpStatus` 拷贝 | 喂文档 | 4xxxx 对,但缺 501/503/504 → `docs/reference/error-codes.md` 里 50100/50300/50400 全写 500 |

**为什么一直没人发现**:
- 2026-10-18 修过一次(把 `response` 那份的 401/403/404/409 修对),**只修了活着的那一份**;
- 导出的 `HTTPStatus()` **零调用方** —— 没人用它,自然没人发现它错;
- 文档生成器那份的注释写着"与 errcode.HTTPStatus 同步",但它**没同步**;而"文档 = 生成器
  产物"这件事让文档和代码**一致地错**,谁看都像对的;
- §1.4 标着 100% —— **写完就没人再读过**。

**真 bug(在活路径上)**:
- **`41300` → 400**:请求体超限报的是"参数错误",前端弹"请求有误",用户只会反复重试
  同样那个超大请求(真实调用点:`api/middleware/reqgzip.go`)。
- **`50100` → 500**:`BaseHandler.NotImplemented` 的注释写着"返回 501",实际返 500。
  代价很具体 —— 前端 `web/src/api/client.ts` 响应拦截器:
  `case 500` → 弹"服务器内部错误"的通用文案、**根本不看 body 里的业务码**;
  `default` → `tError(业务码)` → 查 i18n。
  `50100` 在 `web/src/locales/zh-CN.ts` 里**是有译文的**("该功能尚未实现"),
  但只要 HTTP 状态是 500,那条译文**永远显示不出来**。
  501("这个 provider 没这个能力,该换条路走")与 500("它炸了,该重试 / 该告警")
  在客户端是两种完全不同的处理,混成 500 等于让前端把能力缺失当故障。

**顺带修掉的第 3 条**:`errcode.CodeOf` 用的是裸类型断言 `err.(*Error)`,**只认最外层**。
服务层用 `fmt.Errorf("...: %w", err)` 包装错误是常态 → 断言失败 → 一律返 `Internal`(500)
→ 真正的业务码(比如 20601 "审批流程配置有误")在包装层就被吃掉。而
`handler.BaseHandler.Fail` 一直用 `errors.As` —— **同一个错误,走 Fail 是 403,
走 CodeOf 是 500**,两条出口两种语义。M98 统一成 `errors.As`。

**收口做法**:`errcode.HTTPStatus()` 成为**唯一实现**(窄桶在前、宽桶在后);
`response` 与文档生成器都改调它;`HTTPRaw`(曾与 `HTTPStatus` 是两份不同的实现)
降级成别名;`BaseHandler.NotImplemented` 改用 `errcode.NotImplemented` 常量而不是写死 `50100`。

**顺带发现的第 4 条:生成文档过期了 10 个码**。`docs/reference/error-codes.md` 上次生成是
**2026-10-19**;从那天到 M98 之间新增的 **10 个码**(20600-20603 审批段、
50500-50701 框架段)**一个都没进文档** —— 生成器是**手工跑的**,没有守门网。
M98 把文档的 HTTP 列改成直接调 `errcode.HTTPStatus()`(不再是拷贝),并在 `errcode`
包补了一条测试:**每个 const 都必须出现在文档里,且那一行的 HTTP 列必须等于代码
算出来的值** —— 文档再过期就会红。

**补网的手段与规模**:

| 层 | 文件 | 用例数 | 覆盖什么 |
|---|---|---|---|
| 原语 | `internal/pkg/errcode/errcode_test.go` | 8 | 特殊码映射表(期望值**写死**)/ 桶边界 / 窄桶不被大桶吃 / `HTTPRaw ≡ HTTPStatus` / 每个码有默认文案 / `CodeOf` 解包装 / 文档不许过期 |
| 出口 | `internal/pkg/response/response_test.go` | 7(11 子用例) | 每个码的**真实** HTTP 状态(hertz `ut.PerformRequest` 起真引擎)/ 501 必须是 501 / 成功·失败·分页三种 body 形状 / `FailError` 保住被包装的业务码 |

**可逆探针 13/13 全红**(拆实现看测试会不会红):P1 整段贴回 HEAD 版的 `HTTPStatus`
(大桶在前)/ P2 删掉 413xx 桶 / P3 `NotImplemented` 改成 500 / P4 `ServiceUnavailable`
改成 500 / P5 `CodeOf` 换回裸类型断言 / P6 删掉 `defaultMsg` 里的 `NotImplemented` /
P7 文档里 50100 的 HTTP 列改成 500 / P8 `Fail` 写死 200 / P9 `Fail` 写死 500 /
P10 `FailWithData` 写死 400 / P11 `FailError` 写死 Internal / P12 `OKWithData` 的
code 改成 1 / P13 `Page` 不再包分页壳。

> ⚠️ **P10 与 P12 是"探针抓到测试自己的漏洞"**:这两条一开始报 GREEN(BAD) ——
> P10 把 `FailWithData` 写死 400,但那条测试用的码是 `40003`(本来就该返 400),
> 所以照样绿;P12 把 `OKWithData` 的 code 改成 1,但那条测试只断言了 `OK()` 那条路由
> 的 code。**测试绿 ≠ 断言盯住了改动** —— 补强之后才真正变红。

> ⚠️ **P5 不能只换函数体**:那样 `errors` 的 import 变成"导入未使用" →
> `go test` 直接 **build failed**。**编译失败不是变红**(它证明不了"这条断言抓得住
> 这个 bug"),探针会报 WRONG-RED。改成把整个文件贴回 HEAD 版才是"照抄原版"的现场。

**本次改动没有碰前端**:501 与 413 在前端的落点确实变了(500 → `default` 分支),
但那正是要的效果 —— 只有 `default` 分支才会拿 body 里的业务码去查 i18n。
重试策略也不受影响:`web/src/utils/retry.ts` 的可重试区间是 `>= 500 && < 600`,
**501 跟 500 在同一个区间里**,重试行为完全一致;413 跟 400 一样不可重试。

**§1.4 项数不变(11 项,仍 100%)** —— M98 是对 #2 / #5 / #8a / #9 的**加固**,
不是新增子项。

---

## 1.5 ✅ 加密 / PII(100%)

#### ✅ 已完成(4/10)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | AES-256-GCM 字段加密 | `pkg/crypto/aesgcm.go:1-69`(M100:密文信封 v1/v2 首字节歧义修掉 —— 见 §1.5.1 B1) | 69 |
| 2 | HMAC-SHA256 盲索引 | `pkg/crypto/blindindex.go:1-47`(M100:**真的校验 key ≥16 字节** —— 见 §1.5.1 B8) | 47 |
| 3 | 全局 cipher + 盲索引注入 | `pkg/crypto/crypto.go:1-69`(**M100 之前从没被注入过**;M100 补 `cmd/server/main.go` 装载 `cfg.Crypto` —— 见 §1.5.1 B24) | 69 |
| 4 | bcrypt 密码哈希(cost=12) | `pkg/auth/password.go:1-48` | 48 |

#### ✅ 已完成(6 个)

| # | 子项 | 当前缺口 | 后果 | 优先级 |
|---|---|---|---|---|
| 5 | **PII 解密审计**(谁在什么时候看了手机号) | ✅ M12:`pkg/piiaudit/` 异步批量写 `sys_pii_access_log`;`crypto.Cipher.DecryptStringAudited` 自动同时落 `piiaudit` + `cryptaudit`(commit 6ed5a4e)。🔴 **M100 加固**:这张表**以前永远只能存 1 行**(主键没自己填)+ ip/ua/trace_id/request_id 三条关联链全断 + nil ctx 会 panic —— 见 §1.5.1 B10/B12/B13/B14/B15/B25 | **P0 ✅** |
| 6 | 密钥轮转(rotation) | ✅ M32:v2 envelope `Base64(0x02|keyVer|nonce|ct)`;`pkg/crypto/registry.go` 1 active + N 老 key 仅解;env `KEY_<TIER>_V1..V255` + `KEY_<TIER>_ACTIVE` 装载(M32)。🔴 **M100 加固**:v1 密文首字节歧义(1/256 永久解不开)+ InitKeys 半更新 + 不查重复版本号 —— 见 §1.5.1 B1/B3/B4 | P1 ✅ |
| 7 | 密钥分级(PII vs 普通) | ✅ M32:`Tier{PII,Secret,Normal}` + `EncryptFor/DecryptFor` 按 tier 路由;老 key 保留仅解(M32) | P1 ✅ |
| 8 | 密钥审计日志 | ✅ M21:`pkg/cryptaudit/` 异步批量写 `sys_crypto_audit_log`;`DecryptStringAudited` / `EncryptStringAudited` 自动落 key_id/version/algorithm/op/byte_count/success(commit 3cb5889)。🔴 **M100 加固**:tiered **主路径**以前一条审计都不写(审计覆盖的是没人用的那条路)+ 这张表也永远只能存 1 行 —— 见 §1.5.1 B6/B16 | P1 ✅ |
| 9 | 加密字段全文搜索(密文上的 LIKE) | ✅ M46:`pkg/searchidx/` n-gram 盲索引原语(Ngrams 切重叠 n 元片段,短串< n 整体 1 token;collapseSpaces 去首尾 + 内部连续空格;unicode 按 rune)+ BlindNgrams(secret, s, n) → []hex(HMAC-SHA256)+ SearchTokens(secret, query, n) → 索引表能直接 IN 的 token + MatchAtLeast(actualHits, queryTokenCount, k) 匹配判定 + MinMatchTokens(n, ratio) 计算最小命中数;n 推荐 4(姓名/手机号)/ 3(长查询);service 层按文档 schema 写:主表 cipher + side 表(gram) bulk insert + 查询 GROUP BY + HAVING COUNT;13 单测 -race 干净(含 4-gram 重叠切 / 中文 rune / 短串兜底 / 同 secret 确定性 / 跨 secret 隔离 / MatchAtLeast 9 用例 / MinMatchTokens 7 用例)。🔴 **2026-10-31(M129)补一句**:这一行说的是**原语库已交付**,**不是"密文搜索已经在跑了"** —— 本包**零生产调用方**(commit `fda277f` 至今没人调)。它要接的是**业务侧**:先要有一个字段走 `crypto` 落密文 + 一张 side 表存 gram,查询侧才有东西可搜。⚠️ 判「这个原语有没有用」要看的不是它的 13 条单测,是**它的调用方**(见 memory:框架原语零覆盖 = 高危) | LIKE 不可用 | P2 ✅ |
| 10 | KMS / Vault 集成 | ✅ M36:`pkg/kms/` Provider interface + Manager(选型 + ListKeys + 灌到 crypto.InitKeys);Local provider 沿用 M32 env 格式(`KEY_<TIER>_V<N>` + `ACTIVE` + `LEGACY`);aliyun/tencent/vault 工厂内 TODO 返 err(留好实现位置);`config/kms.go` + `config.example.yaml` 加 `kms:` 块;`main.go` 删 95 行 env 装载 + 用 `kms.NewManager(ctx, cfg.KMS)`;6 单测覆盖 Local(commit 8a8d9ab)。🔴 **M100 加固**:ACTIVE 写错以前 = **进程 panic 退出**(不是注释承诺的"降级继续")—— 见 §1.5.1 B2/B20/B21/B22/B23 | P1 ✅ |

> **复用价值**:**高** —— 任何项目都要 PII
> **P0 路径已通**:#5 的 `sys_pii_access_log` 表 + service 层切面(M12)已落地

### 1.5.1 加密 / PII 审计(2026-10-26,M100)

**起因**:§1.5 标着 100% —— 但那是"**写完了**"的 100%,不是"**验过了**"的 100%。
本节这四块(`pkg/crypto` / `pkg/piiaudit` / `pkg/cryptaudit` / `pkg/kms`)
在 M100 之前**一条测试都没有**(`piiaudit` / `cryptaudit` / `kms` 三个包 **0 用例**),
而 §1.5 #5 的 PII 审计是**等保项**、#8 的密钥审计是**合规项**、
#3 的"全局 cipher + 盲索引注入"标着 ✅ 却**从来没被注入过**。

> 判高危的客观标准是"**挂在全局路径上吗**"(见 memory:框架原语零覆盖)。
> §1.5 从 M32 / M36 之后就没再被读过。

M100 把 §1.5 的每一个 ✅ 都拆开读了一遍,读出 **25 个真 bug**:

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `pkg/crypto/envelope.go` | v1 密文的第 1 字节是**随机 nonce 的第 1 字节**,而 v2 envelope 的第 1 字节必须**恰好**等于 `0x02` —— 每 256 份 v1 密文有 1 份会被当成 v2 解析 | 那份数据**永久解不开**(不是报错,是丢数据)。PII 是批量加密的:1 万条历史密文里约 39 条中招,每条都要人工捞原始明文重加密 —— 而 PII 恰恰是最不该"人工捞"的字段 |
| B2 | `pkg/crypto/registry.go` `InitKeys` | key 非法 / active 数 != 1 时**直接 panic**(老函数叫 `mustBuildTier`) | 配错密钥 = **进程 panic 退出**,不是 `main.go` 承诺的"降级继续" |
| B3 | `pkg/crypto/registry.go` `InitKeys` | 三个 tier **逐个赋值** | 轮转中途失败 → **半更新 registry**:一半数据用新 key、一半用老 key,信封里标的版本号对不上实际用的 key。轮转正是最需要原子性的时刻 |
| B4 | `pkg/crypto/registry.go` `buildTier` | 不查重复 key version(直接覆盖) | 这种配置能启动,但**老密文会随机解不出来**(取决于 map 遍历顺序) |
| B5 | `pkg/crypto/registry.go` `lookupLegacy` | 裸读包级变量 `legacyEntry`(不拿读锁),而 `InitKeys` 会写它 | 轮转与解密并发 = **data race** |
| B6 | `pkg/crypto/registry.go` `encryptFor`/`decryptFor` | tiered 这条**主路径一条审计都不写**(只有 `crypto.Field()` 那条字段级 Cipher 落 cryptaudit) | 审计覆盖的是**没人用的**那条路,漏掉的是**真在用的**那条 |
| B7 | `pkg/crypto/registry.go` `RotationState` | 轮转探针**写**密钥使用审计 | 探针是体检、不是业务读数据,混进 `sys_crypto_audit_log` 会让人误以为有人在读数据 |
| B8 | `pkg/crypto/blindindex.go` `NewBlindIndex` | "key 至少 16 字节"**只写在注释里,代码一行检查都没有** | `NewBlindIndex(nil)` = **空 key 的 HMAC-SHA256**,是任何人都能算出来的公开哈希 → 拿到库的人对候选明文算一遍就能把手机号 / 身份证反查出来,盲索引"不落明文"的意义当场归零 |
| B9 | `pkg/crypto/crypto.go` `InitBlindIndex` / `InitBlindIndexWithMeta` | 返回 `void`,错误被吞 | 同 B8;调用方**没有机会**知道自己装了个空 key 的索引器 |
| B10 | `pkg/piiaudit/piiaudit.go` `extractFromCtx` | 不填主键(`model.BaseModel.ID` 的 tag 是 `primaryKey;autoIncrement:false` → GORM **不让库自增**,零值 0 原样入库) | id 撞主键 → 第二批往后全部撞 "UNIQUE constraint failed" → 整批重试 3 次后被丢弃 → **`sys_pii_access_log` 永远只能存下 1 行**。而它是 §1.5 #5 的 P0 等保项 —— 表在、行有,但**查不出东西** |
| B11 | `pkg/piiaudit/piiaudit.go` `flush` | 写的是 `r.batch = r.batch[:0]` —— **复用同一块底层数组** | flush 释放锁之后才去 `CreateInBatches`,这期间 `append` 会往同一块数组写新行 → **正在被 GORM 读的那批数据被就地改写**。两个后果:数据竞争(-race 一抓一个准)+ **落库行内容被串**(审计写着"张三看了手机号",字段却是李四那行的)—— 比丢行更坏 |
| B12 | `pkg/piiaudit/piiaudit.go` + `ctx.go` | `Record(nil ctx)` 不兜底(4 处直接 `ctx.Value`) | nil ctx → panic,而审计在**后台 goroutine** 里跑 → **panic = 进程退出** |
| B13 | `pkg/piiaudit/piiaudit.go` `Shutdown` | 直接 `close(global.stopCh)`,没有幂等保护 | `defer piiaudit.Shutdown()` + 显式调一次 = **panic("close of closed channel")** |
| B14 | `pkg/piiaudit/piiaudit.go` `extractFromCtx` | `ip` / `user_agent` 两列**从来不填**(ctx key 早就定义、cryptaudit 一直在读,但**全仓没有任何地方写入**) | 一张"谁在什么时候看了手机号"的审计表,少了"从哪台机器来"这一维 → 等保场景基本没用 |
| B15 | `pkg/piiaudit/ctx.go` `traceIDFromContext` | 只认本包自己的 ctx key,而 `InjectTraceID` **全仓零调用方**;otel 中间件把 trace_id 放在 **span context** 里,两套键谁也读不到谁 | `sys_pii_access_log.trace_id` **恒为空** |
| B16 | `pkg/cryptaudit/cryptaudit.go` `Record` | 同 B10:不填主键 | **`sys_crypto_audit_log` 永远只能存 1 行** |
| B17 | `pkg/cryptaudit/cryptaudit.go` `truncateRunes` | `errMsg[:200]` 是**字节**切片 | 一个中文错误信息(3 字节/字)正好在第 200 字节被劈开 → 落库是**半个汉字** + 乱码尾巴。排障时看到这种 `error_msg`,第一反应是"程序写错了"而不是"被截断了" |
| B18 | `pkg/cryptaudit/cryptaudit.go` `Shutdown` | 同 B13:直接 `close(stopCh)` | 重复 `Shutdown` = **panic** |
| B19 | `pkg/cryptaudit/cryptaudit.go` `Init` | `db == nil` 时**静默关掉**,不打任何日志(`piiaudit` 一直有这条 warn) | 同一件事两种口径:合规项被静默关掉**不留痕** |
| B20 | `pkg/kms/local/local.go` `loadTier` | `ACTIVE` 指向没配的版本时,**两种结局完全相反**:V1 配了 V2 没配 → entries 全是 `Active=false` → crypto 侧 panic(**崩进程**);V1/V2 都没配 → entries 为空 → 该 tier 被**静默禁用** | 同一个错因(ACTIVE 写错),一个崩进程,一个悄悄不加密 —— 而 crypto 侧的 panic 那条(B2)让 `main.go` 承诺的降级路径从来没生效过 |
| B21 | `pkg/kms/local/local.go` `parseVersion` 前 | `ACTIVE` 只认 `"V<N>"`(拿 env 字符串跟 `"V"+i` 直接比) | 老文档只写了 `V<N>`,运维漏个 V 就踩坑 —— 而且踩的正是崩进程那条路 |
| B22 | `pkg/kms/local/local.go` `ListKeys` | 一个 tier 配错**不整体报错**(每个 tier 各管各的) | 半装比整体不装更危险:业务会以为 PII 已经加密了,其实那个 tier 悄悄禁用着 |
| B23 | `pkg/kms/manager.go` `NewManager` | `if err := crypto.InitKeys(...); err != nil` 是**死代码**(老 `InitKeys` 永远返 nil) | 文件头注释承诺的"配错 → `log.Warn` → 服务照常起"是**假的** |
| B24 | `cmd/server/main.go` | 配置里的 `crypto.field_key` / `crypto.blind_key` **全仓没有任何代码读**(`config.CryptoConfig` 解出来了而已) | 清单上 §1.5 #3「全局 cipher + 盲索引注入」标着 ✅,其实**从来没注入过** —— 业务一调 `crypto.Field()` 就是 `panic("not initialized")` |
| B25 | `api/middleware/trace.go` | request_id / ip / user_agent 只写**响应头**,不进 ctx(而 PII 审计链是从 ctx 取值的) | 审计表三条关联链全断:对不上 access.log / 对不上机器 / 对不上 trace |

**修法**(全在框架层,零业务代码):

- **B1 →** `DecryptFor` 的 v2 分支解不开时**回退按 v1 再试一次**。
  v2 是**带认证**的(GCM tag),解不开就是解不开 —— 所以"v2 失败 → 试 v1"是安全的;
  反过来不成立(真 v2 envelope 的首字节永远是 `0x02`,一定先走 v2 分支)。
  唯一理论上的误判是"v1 密文前 2 字节恰好是 `0x02` + 某个已注册版本号,
  且剩下的字节还能通过 GCM 认证" —— 概率约 2^-128,可忽略。
- **B2 / B3 / B4 →** `InitKeys` 改成**返 error 而不是 panic**,且
  **三个 tier + legacy 全部先在局部建好、成功才一次性换上去**(轮转原子性);
  另加**重复版本号**检查。`mustBuildTier` → `buildTier`。
- **B5 →** `lookupLegacy()` 拿 `keyMu.RLock()`。
- **B6 →** tiered 路径(`encryptFor`/`decryptFor`)补密钥使用审计;
  **B7 →** 探针路径走 `audit=false`(新增的内部参数),体检不混进业务审计。
- **B8 / B9 →** `NewBlindIndex` 返 `(*BlindIndex, error)` + 长度检查
  (`BlindIndexMinKeyLen = 16`);`InitBlindIndex` / `InitBlindIndexWithMeta` 返 error。
  ⚠️ **这是导出签名变更** —— 全仓只有 `main.go` 一个调用方,同 commit 一起改。
- **B10 / B16 →** 落库前自己填 `BaseModel{ID: sonyflake.Next()}`(与 cronx / inbox / seed 一致)。
- **B11 →** `r.batch = nil`(让 append 重新分配),与 cryptaudit 原本的写法对齐。
- **B12 →** 4 处 nil 兜底:`Record` 入口 + `stringFromContext` + `requestIDFromContext` + `traceIDFromContext`。
- **B13 / B18 →** `Shutdown` 用 `sync.Once` 包住 close(幂等)。
- **B14 / B25 →** `api/middleware/trace.go` 把 request_id / ip / user_agent **注入 ctx**;
  `piiaudit` 侧补 `ip` / `user_agent` 落库。
- **B15 →** `traceIDFromContext` 先读自己的 key(显式注入优先),读不到再从
  **OTel span context** 取 —— 不依赖"谁去注入",otel 中间件起了 span 就一定有 trace_id。
- **B19 →** `Init(nil)` 打一条 warn(与 piiaudit 同一口径)。
- **B20 / B21 / B22 →** local provider:`ACTIVE` 配了却找不到可用 key → **返 error**;
  `ACTIVE` 接受 `"V2"` / `"v2"` / `"2"`(老文档只写了 `V<N>`,运维漏个 V 就踩坑);
  **任何一个 tier 配错就整体返 error**(半装比整体不装危险)。
- **B23 →** `InitKeys` 真返 error 后,`NewManager` 这个分支**第一次真的生效**:
  `main.go` 打 Warn,服务照常起,只是分级加密不可用。
- **B24 →** `cmd/server/main.go` 新增 `initFieldCrypto(cfg.Crypto)`:
  `field_key` 认 **base64(标准 / 无 padding)或 32 字节原文**(生产由 K8s Secret 注入 base64,
  本地调试可直接写原文);`blind_key` **原样使用**(不 base64 解码),>=16 字节。
  没配(空串)= 不算错误、保持未启用;配了但配错 = 打 Error 且**不初始化**
  (不会拿着错误的 key 把 PII 加密成解不开的东西)。

**顺带修的两处同源缺陷(属 §1.9 #7,不在 §1.5 范围内,但缺陷完全同源、修复只有一行)**:

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| S1 | `pkg/smsaudit/smsaudit.go` `extractFromCtx` | 同 B10:不填主键 | **`sys_sms_send_log` 永远只能存 1 行** |
| S2 | `pkg/smsaudit/smsaudit.go` `flush` | 同 B11:`r.batch = r.batch[:0]` | 数据竞争 + 落库行内容被串 |

⚠️ **§1.9 #7 仍开着 3 处同族缺陷**(本轮**没有**修,记在这里别丢):
`Shutdown` 非幂等 / `Record(nil ctx)` 会 panic / 它有自己私有的
`ctxKeyRequestID`·`ctxKeyTraceID`(`smsaudit.Inject*` 全仓零调用方,
和 B15 一模一样的"两套 ctx key 谁也读不到谁")。

**补网的手段与规模**:

| 层 | 文件 | 用例数 | 覆盖什么 |
|---|---|---|---|
| 加密 | `internal/pkg/crypto/envelope_test.go` | 7 | v1 密文首字节 0x02 **仍能解开** / 短密文拒收 / 长度下界 / 版本号边界 |
| 加密 | `internal/pkg/crypto/registry_m100_test.go` | 9 | `InitKeys` 四条错误路径(非法 key / active 数 / 重复版本 / **半更新**) + tiered 主路径**落**审计 + 轮转探针**不**落审计 + 并发轮转无 race + 盲索引短 key 拒收 + 规范化语义 |
| PII 审计 | `internal/pkg/piiaudit/piiaudit_test.go` | 8 | 100 行落库(**每一行都有独立主键**)/ ip + ua + request_id 落库 / trace_id 从 OTel span 兜底 / nil ctx 不 panic / `Shutdown` 幂等 / 并发 flush 无 race(**且行内容不被串**) |
| 密钥审计 | `internal/pkg/cryptaudit/cryptaudit_test.go` | 5 | 80 行落库(主键 / 字段 / 上下文全落上)/ error_msg 按 rune 截断 / nil ctx / `Shutdown` 幂等 |
| 短信审计 | `internal/pkg/smsaudit/smsaudit_test.go` | 2 | 100 行落库(主键)/ 并发 flush 无 race |
| KMS | `internal/pkg/kms/manager_test.go` | 3 | 配错返 error **且不 panic** / 未知 provider / 正常配置 |
| KMS | `internal/pkg/kms/local/local_m100_test.go` | 4 | ACTIVE 指向缺失版本 / ACTIVE 格式非法 / ACTIVE 写法容错 / 一个 tier 配错整体失败 |
| 接线 | `cmd/server/main_m100_test.go` | 4 | field_key 装载(原文 / base64 两种)/ blind_key 装载 / 配错返 error 且**不初始化** |
| 接线 | `internal/api/middleware/trace_m100_test.go` | 2 | Trace 把 request_id / ip / ua **注入 ctx** |

合计 **44 个新用例**。

⚠️ **写测试时踩出来的三个坑**(不是产品 bug,但值得记下来):

- **`BaseModel.ID` 是 `primaryKey;autoIncrement:false`** —— 这条一旦忘了,
  审计表就永远只有 1 行,而且**不报错**(只是一条 Warn 日志)。
  同源缺陷一次查出三处(`piiaudit` / `cryptaudit` / `smsaudit`)。
- **异步 flush 的阈值会制造偶发红**:`cryptaudit` 攒够 100 行会 `go flush()`
  (**异步**,完成时间不可测),`piiaudit` / `smsaudit` 是同步 flush。
  早期版本用"轮询等行数"(5s 超时)→ 多包并发跑 `-race` 时 sqlite 写入被 CPU 抢占,
  行数到了但超时先到 → **偶发红**。
  现在改成**确定性**:事件数压在 100 以下(不触发异步 flush)+ 显式调
  `flush()` / `cryptaudit.Shutdown()`(同步等最后一次 flush 结束)。
  **轮询等待 = 跟机器抢时间**,能不poll就不poll。
- **断言要按"本用例独占的键"过滤,不能数全表**:三个审计包共用一个全局 recorder,
  同包其它用例的行也在同一张表里。全表计数 → 断言随用例顺序漂移。

**可逆探针 29/29 全红**(scripts/probe_m100.py,拆实现看测试会不会红):
① v1 密文首字节 0x02 被误判成 v2 / ② decodeRaw 不校验长度 / ③ InitKeys key 非法改回 panic /
④ active 数不对改回 panic / ⑤ 不查重复版本号 / ⑥ InitKeys 半更新 / ⑦ legacy 裸读(-race)/
⑧ tiered 主路径不落审计 / ⑨ 轮转探针写审计 / ⑩ 盲索引不校验 key 长度 / ⑪ PII 审计行不填主键 /
⑫ flush 复用底层数组(-race)/ ⑬ Record(nil ctx) 四道兜底全拆 / ⑭ piiaudit Shutdown 非幂等 /
⑮ ip / ua 不落库 / ⑯ trace_id 不从 OTel span 取 / ⑰ cryptaudit 行不填主键 /
⑱ error_msg 按字节截断 / ⑲ cryptaudit Shutdown 非幂等 / ⑳ 短信审计行不填主键 /
㉑ smsaudit flush 复用底层数组(-race)/ ㉒ ACTIVE 指向没配的版本不报错 / ㉓ ACTIVE 只认 V<N> /
㉔ 一个 tier 配错不整体报错 / ㉕ NewManager 吞掉 InitKeys 的错误 /
㉙ ACTIVE 写错 → **进程 panic 退出**(端到端)/ ㉖ cfg.Crypto 不装载 /
㉗ 字段密钥不认 base64 / ㉘ trace 不注入 ctx。

> ⚠️ **探针跑出来的三处"假绿"**(不是产品 bug,是**用例本身太弱**)——
> 这正是"探针会打偏"的第一个实例:
>
> - **⑬⑭⑲ 三条用例最初是假绿**:`Record` / `Shutdown` 开头都有
>   `if global == nil { return }`(老实现)—— 只跑这一条用例时 recorder
>   **根本没初始化**,函数直接返回,那一段代码压根没执行。
>   修法:用例里先 `auditTestDB(t)`(触发 `Init`),再断言。
>   **这是"假绿"最典型的形态**:断言写得再漂亮,被测代码那一段没跑到。
> - **⑬ 必须是"多文件探针"**:M100 的 nil 兜底一共**四道**
>   (`Record` 入口 + `stringFromContext` + `requestIDFromContext` + `traceIDFromContext`),
>   只拆一道的话 panic 会从别的兜底里逃不掉 → 探针测不到"整条链都不兜底"。
>   纪律:拆实现要拆**整条链**,不是拆一半。
> - **㉒ 照抄原版时改了字段类型**:原版是 `zap.String("active", activeVer)`
>   (那时 `activeVer` 是字符串),M100 之后它是 int —— 照抄成 `zap.String`
>   **编译不过**,而**编译不过 = 探针无效**(红在 build 上证明不了"这条断言抓得住这个 bug")。
>   改成 `zap.Int` 才是同一条原版逻辑。

> ⚠️ **一处诚实的边界**:`kms.NewManager` 里那个
> `if err := crypto.InitKeys(...); err != nil` 分支,在**当前唯一的 provider
> (local)下不可达** —— local 交给 crypto 的 key 必然是合法 32B、
> 每个 tier 必然恰好 1 个 active,`InitKeys` 不会返 error;
> 而且 ACTIVE 写错在 local 侧(B20)就被拦下了,`ListKeys` 先一步返 error。
> 所以这一条是**纵深防御**(为将来接 aliyun / vault 准备的),
> **不是**修了一个"活的"bug。真正活着的那条是 **B2 + B20:ACTIVE 写错 = 进程 panic 退出**
> (探针 ㉙ 端到端复现:把 local 与 registry 两处一起退回原版)。

**本次改动没有碰前端** —— 加密链路的对外契约(密文格式 / 表结构 / 配置项名 /
环境变量名)**全部不变**;变的只有"这些契约**有没有真的生效**"。
唯一动了对外形状的是 `crypto.NewBlindIndex` / `InitBlindIndex*` 三个**导出签名**,
它们全仓只有 `main.go` 一个调用方(同 commit 一起改),前端不感知。

**§1.5 项数不变(10 项,仍 100%)** —— M100 是对 #1 / #2 / #3 / #5 / #6 / #8 / #10 的**加固**,
不是新增子项。

---

## 1.6 ✅ 登录锁 / 验证码(100%)

#### ✅ 已完成(5 个)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | PreCheck / AfterFail / Clear | `pkg/loginlock/loginlock.go:1-65` | 65 |
| 2 | 5 次 30 分钟策略 | `pkg/loginlock/loginlock.go` 内部常量 | - |
| 3 | 图形验证码 | `service/captcha.go` + `api/handler/v1/captcha.go`(非 pkg/captcha/) | - |
| 4 | 短信登录限频(Redis) | `pkg/smslimit/`(Redis 固定窗口 per phone + per scene,冷却 60s + 1min 窗 + 1day 窗)—— **已装配**:`cmd/server/main.go` 的 `smslimit.Init` + `service/auth_impl.go` 的 `SendSms` 入口 | - |
| 5 | SMS 验证码 TTL 校验 | `sys_sms_code` 表 + `service/auth_impl.go:VerifySms`(TTL = `sms.rate_limit.code_ttl`,**默认 600 秒 = 10 分钟**) | - |

> **复用价值**:**高**,✅ 完全完成
>
> 🔴 **2026-10-28(M106)补一句**:本节的 ✅ 只覆盖**后端那半**。
> **前端那半(图形验证码框 / 开关判定 / 与短信流程的衔接)本节一条都没记**,
> 而 M106 在前端那半挖出一个 **P0**:后端图形验证码是**严格单次性**的
> (`Get(id, true)` 先删后比),而短信流程要过**两道**验证码闸门,
> 页面却从头到尾用同一张图 ⇒ **短信登录 / 短信重置密码 100% 走不通**
> (只在 `captcha.login_enabled` 与 `captcha.sms_send_enabled` 同时为 true 时发作 ——
> 而那正是 `configs/config.example.yaml` 模板的默认值)。
> 详见 **§2.7.2**。

---

## 1.7 ✅ 缓存层(100%)

#### ✅ 已完成(4/9)

| # | 子项 | 证据 |
|---|---|---|
| 1 | Casbin 权限缓存(Redis) | `pkg/authz/cached_loader.go:1-103` |
| 2 | singleflight 防击穿(authz 业务特定) | `pkg/authz/cached_loader.go` 内部 |
| 3 | Redis 客户端(在 config 层) | `server/internal/config/` `repository/redis.go` |
| 4 | **通用 cache wrapper + 三防** ✅M7 | `pkg/cachex/safe.go:1-286` `Safe.GetOrLoad(ctx,key,ttl,loader)` 三防齐全: singleflight 防击穿 / `jitteredTTL()` ±20% 抖动防雪崩 / `NullSentinel` 占位防穿透;辅助 `Get/Set/SetNX/Del/Expire/Stats`(commit e3a1719)。**✅ M90 收口**:三防的实现收敛成**唯一**的泛型函数 `pkg/cachex/core.go:getOrLoadJSON[T]`,存储后端抽象成 `Backend` 接口(`pkg/cachex/backend.go`),真路径第一次被单测覆盖。**✅ M96 收口**:**Redis 那条真路径**也用 miniredis 跑起来了(`pkg/cachex/redis_backend_test.go` 9 例 + `service/dict_cache_redis_test.go` 4 例),并当场修掉两个真 bug —— 前缀失效的 **glob 越界删除**(`Scanner` 契约从 pattern 改成字面前缀)和 **Redis 原生 TTL 语义与 Backend 契约不一致**(`Set`/`SetNX`/`Expire` 三处归一化) |

#### ✅ 已完成(5/9)

| # | 子项 | 当前缺口 | 价值 | 优先级 |
|---|---|---|---|---|
| 5 | **缓存三防** ✅M7 | 通用版已实现,见 #4 | - | **P0 ✅** |
| 6 | 缓存注解 `@Cacheable` | **已收回(2026-10-26 M95)** —— 这一项**早就在本文档「推迟/删除清单」第 4 条里删过一次**("Go 不像 Java 有 Spring AOP,`@Cacheable` 风格需要 code-gen,违反 Go 惯例,**显式 cachex.Get/Set 更好**",见上方 2026-10-19 batch9 那段),而 §1.7 这行一直没跟着删 —— **同一份文档两处说法打架**。M95 把这个矛盾收了:M95 落地的 `service/dict_cache.go` 就是"显式 cachex.Get/Set"那套的标准形态(typed loader + 显式 key + 显式失效),再加一层注解只是**给零调用方的能力加一层壳 = 冗余**。**不做,不再是框架层欠账。** | 中 | ~~P2~~ **收回** |
| 7 | 本地 LRU + Redis 二级缓存 | ✅ M35:`pkg/cachex/TwoLevel` L1 = hashicorp/golang-lru/v2 + 每 key TTL / L2 = Safe(三防);读 L1 命中无 RTT,miss 走 Safe 命中回写 L1;`Set`/`Delete`/`Get`/`Purge` + `GetStats` 命中统计;L1 默认 5000 cap + 10s TTL(比 L2 短 5-10x 保证一致性);Safe nil 时降级纯 L1;10 单测覆盖 L1 行为(commit c2aaea8) | 中 | P1 ✅ |
| 8 | 缓存预热 | ✅ M39:`cachex/warmer.go` `Warmer{Key,Loader,TTL}` 描述预热项 + `WarmAll(ctx, cache, warmers, opts)` 并发跑;`CacheStore` 接口(Safe/TwoLevel 都 Set 满足);并发限流用 M37 syncx.Semaphore;PerWarmTimeout 默认 30s + 整体 ctx 60s;stats 返 Total/Success/Errored/Duration/PerKey;OnError 默认 logger.Warn 可注入;ctx.Err() 预检防 select 随机性;statsMu 保护 map 并发写;main.go 启动期调 WarmAll(空 list 验证集成点,业务模块按需注册);9 单测 -race 干净 3x 复测(commit 2e1b9ec) | 中 | P2 ✅ |
| 9 | 缓存监控(hit rate / qps / 慢查询) | ✅ M38:`cachex/metrics.go` 注册 4 个 collector 到 metricsx.Registry()(app_cache_ops_total{layer,op,result} / app_cache_op_duration_seconds{op} L2 RTT 直方图 0.1ms..819ms 指数桶 / app_cache_l1_size Gauge L1 写后更新 / app_cache_loader_total{result});safe.go + twolevel.go 全部 Get/Set/Del/GetOrLoad 埋点,recordOp nil-safe(InitMetrics 未调不 panic);main.go `cfg.Metrics.Enabled` 时调 `cachex.InitMetrics(metricsx.Registry())`;8 单测 -race 干净(commit c2b9aef) | 中 | P2 ✅ |

> **复用价值**:**高** —— M7 后通用三防到位,新业务直接 cachex.New(rdb).GetOrLoad
> **P0 全部完成** —— #7 二级缓存(M35)/ #8 预热(M39)/ #9 监控(M38)均已落地
> **2026-10-26(M95)**:本节 9 项**全部完成** —— #6 `@Cacheable` 收回(理由见上),
> #1-#5 / #7-#9 早已落地。**但"代码完备"不等于"业务接上了"** ——
> 那一课记在 §1.7.1 的审计表里,M95 把**字典**那一半接上了。
> **2026-10-26(M96)**:本节项数不变(9 项,仍 100%)—— M96 是对 #4 的**加固**,
> 不是新增子项:把 #4 里"Redis 后端"那一半从"没测过"变成"测过,而且测出两个真 bug"。

### 1.7.1 缓存层审计(2026-10-22)

当时的"89%"是就**代码完备度**说的。读代码之后要补一句:**这套框架的业务接线基本为零**。
(2026-10-26 M95 之后本节 100%,但**这句话仍然成立** —— 变的只是"接上了多少",
不是"接线这件事本身有多容易"。)

> **🔴 2026-10-25 更新(M90)**:下表 6 行里**第 1 行和第 4 行已经不成立**,其余 4 行仍然成立。
> - **第 1 行(cachex 零生产调用方)**:principal 缓存迁进来之后,cachex **第一次有了真正的生产调用方** ——
>   `pkg/authz/cached_loader.go` 走 `cachex.GetOrLoadTypedL2`,每次请求的鉴权画像都经过它。
>   `cachex.Default()` 那个全局单例**仍然零读**(principal 刻意不用它,见下),`TwoLevel` 仍然零业务接入。
> - **第 4 行(双轨架构债)**:**已关闭**。`pkg/authz/cached_loader.go` 自己那套 Redis get/set + singleflight
>   已删(`repository/principal_cache.go` 一并删),三防只剩 `pkg/cachex/core.go` 一份实现。
> - **最后一公里(第 5 行)仍未动**:字典 / 系统配置还是直查 DB —— 接它们必须改业务 service,越界。
> - **真路径仍无覆盖(最后一段)**:这一条本轮**部分**改善 —— 后端抽象之后,
>   三防的**真路径**(内存后端)第一次在单测里跑起来,新增 20+ 用例;但 **Redis 后端的真连仍无单测**
>   (包内约定不依赖真 Redis),这条依然留给 dev 环境端到端。
>   **🔴 2026-10-26(M96)已关闭** —— 见下面 M96 那段:用 miniredis(进程内真 Redis)
>   把 Redis 后端整条跑起来,并当场抓出两个真 bug。
>
> **🔴 2026-10-25 更新(M91)**:审计表之外,读代码又查出**授权缓存失效链**的 4 个缺口
> (其中 2 个是安全级别的:撤销授权后最长 5 分钟仍然放行)。已在本轮修掉,
> 详见上面 M91 那段。**"最后一公里"仍未动** —— 字典 / 系统配置的直查要改业务
> service,越界。
>
> **🔴 2026-10-26 更新(M95)**:**"最后一公里"接上了第一段 —— 字典**。
>
> - **接的是什么**:`service/dict_cache.go` 新增 `DictCache`,字典项与分组列表的读路径
>   走 cachex 的 **L2-only** 通道;写路径(`UpdateDict`)在**写库之后、广播之前**显式失效
>   (`dict:` 前缀删)。`role_impl.go` 的 `ListDict` / `ListDictGroups` 接上,
>   `cmd/server/main.go` 装配 `service.NewDictCache(cachePool.Safe())`。
> - **为什么只用 L2 不用 L1**:`config/cache.go` 自己写着"改了必须马上全网生效的数据
>   要么显式失效各副本,要么就别走 L1" —— L1 是进程内的,写路径只能删掉**本实例**的 L1,
>   多副本下"改完立刻生效"会退化成"最多 L1TTL(10s)之后生效"。与 `authz.CachedLoader` 同一判断。
> - **顺序是死的:写库 → 失效缓存 → 广播事件**。M94 让前端收到 `dict.changed` 就重拉,
>   若此刻后端还吐着缓存里的旧字典,前端重拉回来的就是旧值 —— 症状正是 M94 要修的
>   "改了不生效",而且这次还多了一层"我明明让它刷新了"。该顺序有单测
>   (`role_dict_cache_test.go` 的 `TestM95_UpdateDict_InvalidatesBeforePublish`)守着,
>   也有可逆探针守着(调换顺序 → 该用例变红)。
> - **审计表"自相矛盾"那一行已消**:除了改代码,M95 还把 `internal/config/cache.go` 的
>   注释改了 —— 原文把 dict/config 同时列进"陈旧 10 秒无所谓"和"必须马上全网生效"
>   两边,自相冲突;`internal/pkg/cachex/prefix.go` 里"内存后端不支持扫描"那句也是错的
>   (内存后端实现了 `Scanner`),一并改掉。
> - **仍然没接的**:系统配置直查 DB(`config_center_impl.go` 里 `cache|redis|RDB` 零命中)——
>   **故意不接**:配置里有密钥类字段,把解密后的密文塞进 Redis 是**安全降级**;
>   `WarmAll` 仍然传 `nil` warmers(字典没做预热:预热要在装配期回源一次,
>   而字典的读路径已经有缓存,预热的边际收益低于它带来的"装配期依赖 DB"成本)。
>
> **🔴 2026-10-26 更新(M96)**:M95 把字典挂到了 **Redis 那条真路径**上,
> 而那一侧**一行单测都没有**(见下面最后一段)。M96 补上这条网 ——
> **补网的过程当场抓出两个真 bug**,都不是测试写错,是实现写错。
>
> - **抓出的 bug ①:前缀失效会删掉别的业务的 key(glob 越界)**
>   `Safe.DeletePrefix` 原来把前缀拼成 `前缀 + "*"` 当 **glob pattern** 传给
>   `SCAN MATCH`。Redis 的 MATCH 是 **glob 语法**(`*` `?` `[...]` 都是通配符),
>   于是 `DeletePrefix(ctx, "a*b")` 生成的 pattern 是 `a*b*` —— 中间的 `*`
>   成了通配符,**"axb:2" 之类别的业务的 key 一起被删掉**。
>   内存后端也不对:它取第一个 `*` 之前当前缀,`a*b*` 变成前缀 `"a"`,范围更宽。
>   **症状是"删多了",而且没有任何报错。**
>   ⚠️ **如实说:今天还没炸过** —— 现网两个失效前缀(`dict:`、`principal:<id>:`)
>   都不含 glob 元字符,所以这个 bug **是潜伏的**,不是"线上正在丢数据"。
>   但它是**契约级的错**:接口写的是"前缀",实现做的是"glob pattern",
>   下一个用带元字符前缀的调用方就会踩,而且**没有任何报错**。
>   **修法**:`Scanner` 的契约从"pattern"改成**字面前缀**,由**会 glob 的那个后端**
>   (Redis)自己转义(`escapeGlob`),内存后端改成 `strings.HasPrefix`。
>   两个后端从此行为一致。
> - **抓出的 bug ②:Redis 后端与内存后端在 TTL 语义上不一致**
>   Backend 契约写的是"ttl <= 0 = 不过期" / "Expire 的 ttl <= 0 = 取消过期",
>   但 **Redis 原生语义两处都不一样**:
>   `SET k v EX -1` 直接报错、`EXPIRE key 0` 是**删掉这个 key**、
>   而 go-redis 的 `KeepTTL = -1` 会让 `Set(-1)` 变成 `SET k v KEEPTTL`(**保留旧 TTL**)。
>   后果:字典缓存的兜底 TTL 在 Redis 上**比配置值更早到期**(键会提前消失),
>   `Expire(0)` 在两个后端上是两种行为(一个删 key,一个取消过期)。
>   **修法**:Redis 后端做归一化 —— `Set`/`SetNX` 的 `ttl < 0` 收到 0,
>   `Expire` 的 `ttl <= 0` 翻译成 `PERSIST`。
> - **补网的手段**:**miniredis**(进程内**真** Redis:真 RESP 协议 / 真 SCAN / 真 TTL),
>   **测试专用依赖**,只在 `_test.go` 里 import。mock 掉 `redis.Client` 只能验
>   "我们调了什么",验不了"Redis 会不会这么回"。
>   ⚠️ **miniredis 有一个已知限制**:它**忽略 SCAN 的 COUNT 参数,永远一次返回
>   全部匹配的 key、游标恒为 0** —— 所以"分多批扫描"那条路径它造不出来,
>   由 `prefix_test.go` 里一个**会真的返回非零游标**的假后端守着。
> - **覆盖到的**:`internal/pkg/cachex/redis_backend_test.go`(9 个用例)、
>   `internal/service/dict_cache_redis_test.go`(4 个用例,service → DictCache →
>   Safe → RedisBackend → go-redis → 真 Redis 的**整条链**)。
> - **可逆探针 7/7 全红**(P1 拆转义 / P2 Expire 退回原生 / P3 Set 去归一化 /
>   P4 SetNX 去归一化 / P5 分页只跑第一批 / P6 内存后端退回 `*` 截断 /
>   P7 DeletePrefix 退回拼 pattern)。
>   ⚠️ **P5 第一次探针是假红** —— 拆法写成"删掉 `if next == 0 { break }`",
>   结果 `next` 变成未使用变量,`go test` 直接 build failed。
>   **编译失败不是变红**:它证明不了"这条断言抓得住这个 bug"。
>   改成"保留 if、只在后面加一句 break"才真的红在断言上("应删 6 条,got 2")。

| 项 | 实况 |
|---|---|
| ~~**cachex 生产调用方只有 `main.go` 的 6 行启动装配**~~ | **M90 已改**:principal 缓存迁入后,cachex 有了真正的生产调用方(每次请求的鉴权画像)。但 `cachex.Default()` 全局单例仍零读,`TwoLevel` 仍零业务接入。**M95 再进一步**:`cachex.Default()` 的 **L2 通道**被字典缓存接上了(`cachePool.Safe()`),`TwoLevel` 本身仍然零业务接入(字典刻意只取 L2) |
| **`typed.go` 与 `prefix.go` 整个文件只服务测试** | 泛型封装与前缀失效是 cachex 最核心的两块能力,全无业务接入。`prefix.go` 注释自称 `DeletePrefix` 是"缓存一份实体能不能安全落地的前提"—— 前提建好了,实体没落地。**✅ M91 部分收口**:`prefix.go` 的 `DeletePrefix` 有了**第一个生产调用方**(`authz.CachedLoader.InvalidateUser`,按用户批量失效 principal 缓存);`typed.go` 仍然只服务测试。**✅ M95 收口**:`typed.go` 的 `GetOrLoadTypedL2` 有了**第二个生产调用方**(字典缓存),`prefix.go` 的 `DeletePrefix` 有了**第二个**(字典缓存失效)—— 前提和实体终于都落地了 |
| **`WarmAll` 传的是 `nil` warmers** | main.go 自己写着 "no warmers registered yet"。预热机制跑通了,一条预热项都没注册。**M95 仍为空**:字典缓存走的是懒加载(第一次读回源),预热收益不抵它在装配期引入的 DB 依赖 |
| ~~**真正的业务缓存是另起炉灶的**~~ | **M90 已关闭**:principal 缓存已迁到 cachex,双轨消失,三防(单飞 / null 占位 / TTL 抖动)补齐 |
| **最后一公里缺口 ~~原封不动仍在~~(字典已接)** | **M95 已改**:字典**不再是直查 DB** —— 读走 cachex L2,写路径显式失效。**仍然直查 DB 的**:系统配置(`config_center_impl.go` 里 `cache\|redis\|RDB` 零命中 —— **故意不接**,配置含密钥,解密后的值进 Redis 是安全降级);权限树走 authz 私有缓存(principal 那条线,与本条无关) |
| **~~自相矛盾~~(已消)** | **M95 已改**:`internal/config/cache.go` 的注释原先把 dict/config 同时列进"陈旧 10 秒无所谓"和"必须马上全网生效"两边(自相冲突),已改成单一说法;`internal/pkg/cachex/prefix.go` 里"内存后端不支持扫描"那句是错的(内存后端实现了 `Scanner`),一并改掉 |

**接最后一公里要改业务 service —— 这条边界 M95 越了一次,而且是"有意识地越"。** 理由:字典**不是业务线代码**,它是**框架级的 enum 中央仓库**(每个页面的下拉都读它,`web/src/stores/dict.ts` 还给它做了 30 分钟前端缓存)。它的读写两端都在 `service/role_impl.go` 这个**框架层**文件里,不碰任何业务页面。真正碰业务的是系统配置中心(密钥 / 生效时机),那个仍然不接。`kvstore` 依然是本仓另一个真正完成最后一公里的缓存模块(`api/middleware/ratelimit.go:44` 限流窗口、`pkg/loginlock/loginlock.go:37` 登录失败锁定都真接了)。

测试质量反而是好的:61 个测试,`prefix_test.go:10-40` 有一份 P1–P9 可逆探针表(9/9 全部变红,且抓到过一个真 bug),但探针只覆盖 prefix/typed/build 三块,`twolevel`/`warmer`/`safe`/`metrics`/`kvstore` 无探针记录;L2 路径整条没在单测里跑过,正确性只靠 dev 环境。

**🔴 2026-10-26(M96)对最后半句的更正**:"L2 路径整条没在单测里跑过"—— **已关闭**。
M96 用 miniredis 把 Redis 后端整条跑起来(9 + 4 个用例),并把探针表扩到 **P1–P9 + M96 的 7 条(7/7 全红)**;
其中 **2 条探针当场抓到真 bug**(glob 越界删除 / TTL 语义不一致),不是"补了个网没出事"。

> **🔴 2026-10-25(M90)对最后一段的更正**:上面说"L2 路径整条没在单测里跑过"——
> **属实,而且比想象的严重**:老测试只能塞一个指向 127.0.0.1:6379 的死客户端,
> 本机没有 Redis,于是**每次读都走"连接失败 → 降级回源"那条分支**。
> 测的是降级,不是缓存;singleflight / null 占位 / TTL 抖动三样**一样都没被验过**。
> M90 把存储后端抽象成 `Backend` 接口之后,内存后端让真路径第一次跑起来
> (新增 `core_test.go` / `memory_backend_test.go`,探针 9/9 全红)。
> **Redis 后端的真连仍无单测**(包内约定不依赖真 Redis),这条留给 dev 环境端到端。
>
> **🔴 2026-10-26(M96)已关闭**:上面最后那句"Redis 后端的真连仍无单测"**不再成立**。
> M96 用 **miniredis**(进程内真 Redis)把 Redis 后端整条跑起来
> (`internal/pkg/cachex/redis_backend_test.go` 9 个用例 +
> `internal/service/dict_cache_redis_test.go` 4 个用例),
> 并**当场抓出两个真 bug**:前缀失效的 **glob 越界删除**、
> 以及 Redis 原生 TTL 语义与 Backend 契约**不一致**。
> 详见上面 M96 那段。
>
> ⚠️ **但有一条 miniredis 造不出来** —— 它**忽略 SCAN 的 COUNT**,永远一次返回
> 全部匹配的 key、游标恒为 0。所以"分多批扫描"那条路径由 `prefix_test.go` 里
> 一个**会真的返回非零游标**的假后端守着(那才是唯一能覆盖分页循环的办法)。
> "dev 环境端到端"现在只剩**多副本**那一项没被单测覆盖
> (单进程里所有副本共享一个 Redis,本来就测不出跨副本失效)。

---

## 1.8 ✅ 日志 / 可观测性(100%)

#### 📋 不计入框架清单(业务侧 / 运维侧 —— **不是框架层的欠账**)

| # | 子项 | 说明 | 价值 | 优先级 |
|---|---|---|---|---|
| 10 | 业务指标(登录成功率/API 慢查询) | 框架埋点已就位(BusinessCounter/Histogram),业务方需按需调 | 中 | P1 |
| 11 | 告警规则 | 缺(在 Prometheus/Grafana 侧配,不在 server 范围) | 中 | P1 |
| 12 | 日志聚合(Loki/ELK) | 缺(运维侧搭,不在 server 范围) | 中 | P1 |

#### ✅ 已完成(10/13)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | 结构化日志(zap) | `pkg/logger/logger.go:1-251`;🔴 **M99 加固**:audit / access 两条独立通道此前**一个字节都没写进去**,且即便写进去也**没有结构化字段**(zap 的 hook 把 fields 丢了)—— 改成 core 级路由,详见 §1.8.1 | 251 |
| 2 | 多 Core(info/warn/error/audit/access) | `pkg/logger/logger.go` + `pkg/logger/route.go:1-71`(**core 级路由**,不是 `zap.Hooks` —— hook 拿不到结构化字段);🔴 **M99 加固**,详见 §1.8.1 | 71 |
| 3 | TimeRotator(按天分目录+按大小滚动+压缩) | `pkg/logger/rotator.go:1-290`;🔴 **M99 加固**:重开文件失败后 rotator 会**卡死**(`currentFile` 留 nil 且不复位)→ 之后每次写都失败 → **日志静默全丢**;补 `|| r.currentFile == nil` 自愈。详见 §1.8.1 | 290 |
| 4 | Trace ID 串联请求 | `api/middleware/trace.go:1-33` | 33 |
| 5 | 审计日志(操作/数据/登录/全量) | `api/middleware/audit.go` + `api/middleware/audit_capture.go`;🔴 **M99 加固**:`logger.Audit()` 此前**全仓零调用方** → audit.log **没有任何写入方**(通道建好了没人往里写,文件连创建都不会发生);现由 audit 中间件写入。🔴 **M117-1…4 + M126 补齐采集端**:DB 轨**从未装配**(`AuditSvc` 恒 nil ⇒ 表里一行都没写过)、`request_body` / `request_params` / `response_summary` / `location` 四列**恒空**(通用脱敏器**零生产调用方**)。详见 §1.8.1 / §1.8.2 | 207+243 |
| 6 | 访问日志(JSON,含 trace_id/span_id) | `api/middleware/access_log.go:1-57`;🔴 **M99 加固**:此前写的是**无名** logger → `LoggerName == ""` → 路由跳过 → **access.log 连文件都不会被创建**。详见 §1.8.1 | 57 |
| 7 | **OpenTelemetry trace 导出** ✅M4 | `pkg/otelx/otelx.go:1-177` `Init/Shutdown` OTLP gRPC/HTTP + ParentBased(TraceIDRatioBased) 采样 + BatchSpanProcessor(5s/512 批);`api/middleware/otel.go:1-140` `Otel(serviceName)` 自实现中间件(< 100 行核心 + httpHeadersCarrier 适配 TextMapCarrier);`config/otel.go:1-23` OTelConfig;access_log 加 trace_id/span_id 字段(commit 3b15550);🔴 **M99 加固**:span 名此前用**原始 path**(每个带 ID 的 URL 一个新 span 名 → trace 后端基数爆炸)、`carrier.Set` 写进了**响应头**、OTLP endpoint 带 scheme 时**每次导出都失败**。详见 §1.8.1 | 177+140+23 |
| 7a | OTel 后端:阿里云 ARMS APM(OTLP gRPC:4317) | `configs/config.example.yaml` `otel.endpoint` 留 ARMS 占位 + `insecure=false` 默认(生产前 TODO:填实际 endpoint + 翻 `enabled=true`) | - |
| 8 | **Prometheus metrics** ✅M8 | `pkg/metricsx/metricsx.go:1-451` Init + 4 类 HTTP 指标(app_http_requests_total/duration/in_flight/response_size) + Go runtime + Process + app_info;`api/middleware/metrics.go:1-96` `Metrics()` 采集中间件;`/metrics` 端点(默认路径,可配);`Mount()` 挂在 router;业务埋点 `BusinessCounter/Gauge/Histogram` + `ObserveDuration`(commit 5384c36);🔴 **M99 加固**:响应体大小此前**恒为 0**(`Header.ContentLength()` 对普通 `c.JSON` 从不设置)、404 兜底用**原始 path**(每条探活一个新 series)、业务埋点三个函数**一次调用就 panic**、`/metrics` 的内容协商是**死代码**。详见 §1.8.1 | 451+96 |
| 9 | **健康检查 endpoint** ✅M3 | (见 §1.3 #12) `api/handler/v1/health.go:1-116` `/healthz` 200 liveness + `/readyz` 200/503 readiness 探 DB+Redis(commit ba0aa84) | 116 |

#### ✅ 已完成(2 个)

| # | 子项 | 当前缺口 | 价值 | 优先级 |
|---|---|---|---|---|
| 14 | 慢 SQL 报警 | ✅ M33:`pkg/slowquery/` 包 `db.Logger.Trace` 钩子,`app_slow_sql_total{table,op,is_error}` Prometheus 指标(M33) | P1 ✅ |
| 15 | **/version 构建信息 endpoint** | ✅ M49:`pkg/buildinfo/` 编译期 ldflags 注入 AppName/Version/Commit/BuildTime + Snapshot() 返 Info{app_name, version, commit, build_time, go_version, os, arch};`api/router/router.go` 在 /healthz /readyz 旁挂 GET /version(200, 无 auth);3 单测 -race 干净(默认值 / 注入后 / JSON 序列化);`go build -ldflags "-X rapid-admin/internal/pkg/buildinfo.Version=v1.2.3 -X .../Commit=$(git rev-parse --short HEAD) -X .../BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"` | 中 | P2 ✅ |

#### ✅ 已完成(1 个)

| # | 子项 | 说明 |
|---|---|---|
| 13 | APM 接入(Datadog/SkyWalking) | 已接 ARMS,Datadog/SkyWalking 暂不接入,挪到 P2 |

> **复用价值**:**高** —— OTel + Prom + 健康检查 三大可观测件齐了
> **P0 全部完成** —— 2026-10-19 之后无 P0 可观测性项待补

### 1.8.1 日志 / 可观测性审计(2026-10-26,M99)

**起因**:§1.8 标着 100% —— 但那是"**写完了**"的 100%,不是"**验过了**"的 100%。
本节这四块(`pkg/logger` / `pkg/metricsx` / `pkg/otelx` + 四个可观测中间件)
**全都挂在全局路径上** —— logger 的返回值直接给了 `zap.ReplaceGlobals`,
metrics / otel / access_log / audit 四个中间件在 router 里逐个挂上 ——
而它们此前**一条测试都没有**。

> 判高危的客观标准是"**挂在全局中间件链上吗**"(见 memory:框架原语零覆盖)。
> 清单上标 ✅ ≠ 那行被读过 —— §1.8 从 M4/M8 之后就没再被读过。

M99 把 §1.8 的每一个 ✅ 都拆开读了一遍,读出 **16 个真 bug**:

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `api/middleware/access_log.go` | 写的是**无名** logger(`L().Info`) | `zapcore.Entry.LoggerName == ""` → 路由直接跳过 → **access.log 连文件都不会被创建**(rotator 是懒创建的) |
| B2 | `pkg/logger` `Audit()` | 全仓**零调用方** | **audit.log 没有任何写入方** —— 通道建好了没人往里写 |
| B3 | `api/middleware/metrics.go` | 响应体大小取 `c.Response.Header.ContentLength()` | hertz 只在**文件下载**和测试辅助里写这个字段,普通 `c.JSON` 走 render → AppendBody,**从不设置** → `app_http_response_size_bytes` **恒为 0** |
| B4 | `pkg/metricsx` 三个业务埋点函数 | 都声明返回 `prometheus.Counter`/`Gauge`/`Observer`,可缓存进 `bizCollectors` 的是 **Vec 类型**,结尾 `actual.(prometheus.Counter)` 断言必失败 | **每一次调用都 panic** —— 把 HEAD 版的三个函数原样抄出来实测:`*prometheus.CounterVec is not prometheus.Counter: missing method Add` / `*prometheus.GaugeVec is not prometheus.Gauge: missing method Add` / `*prometheus.HistogramVec is not prometheus.Observer: missing method Observe` —— 三个函数**没有一个能跑通**;编译器早说过 `*prometheus.CounterVec does not implement prometheus.Counter` |
| B5 | `pkg/metricsx` `ObserveDuration` | 拿 `prometheus.Observer` 再断言成 `prometheus.Histogram` | **必 panic**(而且它内部调的 `BusinessHistogram` 先炸);而且它传的 `labelNames` 是**标签名**,Observe 要的是**标签值**,接口本身就不可用 |
| B6 | `pkg/metricsx` 业务埋点注册表 | 同名指标跨种类 / 跨标签集不提前拦 | ⚠️ 反过来:旧实现里 Prometheus 的 `duplicate metrics collector registration` / `inconsistent label cardinality` **根本走不到**(接口转换那一炸更早)—— 新实现要在**注册前**就拦,给一句带 kind 和两边 label 集的人话 |
| B7 | `pkg/logger/rotator.go` `Write` | rotate 判据只有 `currentDay != today` | `rotateSize()` 在"重开文件失败"路径上留下 `currentFile == nil` 但 `currentDay` 仍是今天 → 下次写**跳过 rotate** → 每次写返 `ErrInvalid` → **日志从此全部静默丢失**(不是 panic,更难查) |
| B8 | `pkg/logger` audit/access 的 encoder | 用 `zap.NewProductionEncoderConfig()` 现建 | ts 是 **epoch 秒**,而 info/warn/error 是 ISO8601 → 同一个进程三个文件**两种时间格式**,运维得自己心算 |
| B9 | `pkg/logger/rotator.go` 过期目录判定 | 目录名 `time.Parse` 成 UTC 再和本地 `now` 比 | 两边时区不一致 → "删不删"取决于当天几点,最多飘 14 小时;同一份配置在两地表现不同 |
| B10 | `pkg/logger` `SyncFlush()` | `return nil` **空壳** | 名字承诺"同步所有 rotator",实现什么都不做 → 退出时最后几条日志丢 |
| B11 | `api/middleware/otel.go` `otelRoutePattern` | 用**原始 path** | 每个带 ID 的 URL 一个新 span 名 → trace 后端**基数爆炸**;而 metrics 那边的同类拷贝**收敛得好好的** —— 两份拷贝一份对一份错 |
| B12 | `pkg/metricsx` `/metrics` | Accept 头**精确匹配** + OpenMetrics 分支不 `Close()` | 真实 Prometheus 发的是复合头(`application/openmetrics-text; version=1.0.0,text/plain;...`)→ 精确匹配永不成立 → 协商是**死代码**;即便走进去,OpenMetrics 报文缺 `# EOF` → 客户端判定报文非法 |
| B13 | `api/middleware/metrics.go` 404 兜底 | 兜底用**原始 path** | 每个探活 URL 一条新 series(`path="/.env"`、`path="/wp-admin/x"`)→ Prometheus 基数爆炸 |
| B14 | `api/middleware/otel.go` `carrier.Set` | 写的是 `c.Header(k, v)` | 那是**响应头** → 每个响应多一个 `traceparent` 内部头,而**请求头里反而没有**(carrier 的 Get 读的是请求头)—— 读写不对称 = carrier 坏了 |
| B15 | `pkg/otelx` OTLP endpoint | `WithEndpoint` 收到带 scheme 的串 | `otlptracehttp.WithEndpoint` **只吃 host:port** → 实际地址变成 `http://http://host:port/v1/traces` → **每次导出都失败且静默** |
| B16 | `pkg/logger` 路由用 `zap.Hooks` | zap 的 hook 拿不到每次调用的结构化字段 | `zapcore.hooked.Write(ent Entry, _ []Field)` **把 fields 丢了** → audit.log / access.log 里只剩 level/ts/logger/caller/msg,**path / method / status / user 全没了** |

**B1 / B2 / B16 是同一条链上的三段**:路由规则是"`LoggerName` 对得上就分流到独立文件",
而 B1 让 access 那一侧**没有分流源**,B2 让 audit 那一侧**没有分流源**,
B16 让**即便分流到了也写不出内容**。三段叠在一起 = 两份日志文件
**既没有来源、也没有内容** —— 而清单上这两项一直标着 ✅。

**修法**(一行一处,全在框架层):

- **B16 → 新增 `pkg/logger/route.go` 的 `namedCore`**:在 **core 层**按
  `zapcore.Entry.LoggerName` 选核心,`Write(ent, fields)` 时字段原样带过去。
  **不能用 `zap.Hooks`** —— hook 的签名是 `func(zapcore.Entry) error`,拿不到 fields。
  `With()` 必须把 `name` 一起带走(忘了的话包装一 `With` 就失效 → **所有日志都进 audit.log**)。
- **B1 / B2 →** `AccessLog` 中间件改写 `logger.Access().Info(...)`,
  `Audit` 中间件改写 `logger.Audit().Info(...)`(都是 `Named(...)` 派生的具名 logger)。
  审计中间件现在**文件轨与 DB 轨并行**:文件轨不依赖 `AuditSvc` ——
  "审计服务没挂"不该等于"没有审计"。
- **B3 →** 响应大小改取 `len(c.Response.Body())`。
- **B4 / B5 →** 三个业务埋点函数一律返回**具名 Vec 类型**
  (`*CounterVec` / `*GaugeVec` / `*HistogramVec`),调用方自己 `WithLabelValues(...)`。
  ⚠️ 这是**破坏性签名变更**,但全仓**零调用方**(只有包注释里的示例)→ 零风险。
- **B6 →** 注册前查元信息表,拦下"同名跨种类 / 跨标签集",给一句人话。
- **B7 →** rotate 判据补 `|| r.currentFile == nil` → 写失败后能自愈重开。
- **B8 →** audit/access 与 info/warn/error **共用同一份 encoderCfg**。
- **B9 →** 过期判定改成**日期串字典序比较**(目录名就是 `2006-01-02`,定长 → 字典序 == 时间序),
  规则与时区、与运行时刻都无关;判定抽成纯函数 `expiredDir(name, cutoff)`,可被穷举测试。
- **B10 →** `SyncFlush()` 真的遍历已登记 rotator 调 `Sync()`,错误往上抛;
  另加 `Close()` 关句柄(Windows 上不关句柄,日志目录删不掉)。
- **B11 / B13 →** 统一走 `routePath(c)`:命中路由用**路由模板**,未命中用**固定标签**
  `unmatchedPath`(**不用**原始 path)。otel.go 里那份同名不同行为的拷贝删掉。
- **B12 →** 改用官方同款 `expfmt.NegotiateIncludingOpenMetrics`,OpenMetrics 分支补
  `enc.Close()`(写 `# EOF`)。
- **B14 →** `carrier.Set` 改成 `c.Request.Header.Set(...)`。
- **B15 →** `splitEndpoint` 剥掉 scheme,`WithEndpoint` 只拿 `host:port`;
  `config/otel.go` 的注释同步改成"只写 host:port"。

**补网的手段与规模**:

| 层 | 文件 | 用例数 | 覆盖什么 |
|---|---|---|---|
| 日志 | `internal/pkg/logger/logger_test.go` | 11 | audit/access 各自落自己的文件 + 不串台 + 无名 logger 不进独立通道 + **结构化字段不丢** + ts 统一 ISO8601 + `SyncFlush` **把错误真的抛出来** + 5 个 rotator 都登记 + `Close` 后能自愈 |
| 日志 | `internal/pkg/logger/rotator_test.go` | 6 | 按天分目录 / 按大小滚动 / 压缩 / 过期目录判定 / **rotate 失败后自愈** / `Sync` 报错可见 |
| 指标 | `internal/pkg/metricsx/metricsx_test.go` | 12 | 4 类 HTTP 指标 / 业务埋点三个函数**真的能用** / 同名跨种类跨标签集提前 panic / `/metrics` **OpenMetrics 协商 + `# EOF`** / 零子节点的 Vec **不进 Gather** |
| 追踪 | `internal/pkg/otelx/otelx_test.go` | 10 | `Init/Shutdown` / HTTP exporter **真的打到 collector**(httptest 收)/ endpoint 剥 scheme / 采样率 / 全局 propagator |
| 中间件 | `internal/api/middleware/{metrics,access_log,audit,otel}_test.go` | 18 | 指标中间件(6:响应大小 / 路由模板 / 404 兜底 / in-flight)/ 访问日志(3:落 access.log + 带 trace_id)/ 审计(4:落 audit.log + 带业务字段)/ OTel(5:span 名用模板 + traceparent 进**请求**头) |
| 设施 | `internal/api/middleware/observability_test.go` | 共享 | 包级 `TestMain`:装 logger + metricsx registry + 静音 hertz 路由日志;抓 `/metrics` 报文、读日志文件的共用工具 |

合计 **57 个新用例**。

⚠️ **两个"写测试时踩出来"的坑**(不是 bug,但值得记下来):

- **`github.com/prometheus/client_model` 是间接依赖**(只为 `dto`),直接 import 就得动
  `go.mod` —— 为了一条断言动依赖不划算。改成**抓 `/metrics` 的 exposition 文本**来断言,
  反而更接近采集器真正看到的东西。
- **Vec 零子节点 = `Gather()` 里根本没有它**(连 `# TYPE` 行都没有)→ 断言"指标存在"
  必须先 `WithLabelValues` 造一个子节点,否则是假绿。

**可逆探针 18/18 全红**(scripts/probe_m99.py,拆实现看测试会不会红):
① access 改回无名 logger / ② audit 改回无名 logger / ③ 响应大小改回 `ContentLength` /
④ `BusinessHistogram` 必 panic / ⑤ `ObserveDuration` 必 panic / ⑥ 删掉同名跨种类拦截 /
⑦ rotator 少判 `currentFile == nil` / ⑧ audit/access 换回默认 encoder /
⑨ 过期判定改回解析成 time / ⑩ `SyncFlush` 退回空壳 / ⑪ span 名改回原始 path /
⑫ 404 兜底改回原始 path / ⑬ `carrier.Set` 改回写响应头 / ⑭ OTLP endpoint 不剥 scheme /
⑮ 路由改回 `zap.Hooks`(照抄 M99 之前的实现)/ ⑯ `/metrics` 退回精确匹配 / ⑰ 不调 `enc.Close()` /
⑱ `BusinessCounter` 必 panic(照抄旧实现那句类型断言)。

> ⚠️ **⑮ 是"照抄原版"才成立的一条**:探针必须把整段路由**换成 M99 之前的 hook 实现**,
> 而不是"随便写个错的 hook" —— 写错的版本会编译不过(`declared and not used`),
> 那是 **WRONG-RED**(红在 build 上,证明不了"这条断言抓得住这个 bug")。

> ⚠️ **④⑤ 同样是照抄**:探针里的 `actual.(prometheus.Observer)` 是从**旧实现源码**里抄的,
> 不是重写的 —— 重写的版本 panic 在别的地方,测的就不是同一个 bug 了。

> ⚠️ **⑨ 只在边界上才有差别**:字典序比较和 time 比较在"等于 cutoff"时结论相反
> (字典序 `<=` 保留,`time.Before` 丢弃)→ 这条测试的用例必须**卡在边界上**,
> 否则换成什么实现都绿。

**本次改动没有碰前端**:可观测性链路的对外契约(端点路径 / 指标名 / 采样率 / 日志字段名)
**全部不变** —— 变的只有"这些契约**有没有真的生效**"。唯一动了对外形状的是
`metricsx.Business*` 的返回类型,而它**零调用方**,前端不感知。

**§1.8 项数不变(13 项,仍 100%)** —— M99 是对 #1 / #2 / #3 / #5 / #6 / #7 / #8 的**加固**,
不是新增子项。

---

### 1.8.2 审计采集端补写(2026-10-30,M117-1…4 + M126)

**起因**:§1.8 #5 标着 ✅,M99 也把它拆开读过一遍。**M117 那一轮(M117-1…4,
2026-10-30)修掉了这条链路上的头两段**,但第三段没人碰:

| # | 位置 | 现象 | 谁修的 |
|---|---|---|---|
| C1 | `middleware.AuditSvc` | **全仓没有第二处赋值** ⇒ 中间件里 `if AuditSvc == nil { return }` **每次提前返回** ⇒ **`sys_operation_log` 一行都没被中间件写过**。dev 库里那 11 行全是 service 层直写的(`auth` / `user` 这种逻辑名),没有一行是路由路径 —— 这跟"中间件的 `module` = 路由路径"对得上 | ✅ **M117-1**(`7c06005`):改成在 `router.Register` 里 `SetAuditService(svc.Audit)` 单一装配点,并补 `router/audit_wiring_test.go` |
| C2 | `pkg/desensitize`(`RedactJSON` / `RedactQuery`) | 落地时带了一整套单测,但**零生产调用方** ⇒ 审计表 `request_body` / `request_params` 两列**恒空**;前端抽屉里那两行永远是"空值"。前端注释里"仓库里目前**没有**通用的请求体脱敏器"这句话**在 M117-1 落地当天就已经不成立** | 🔴 **M126**(本节) |
| C3 | `middleware.Audit` | 只写了 `module` / `action` / `ip` / `ua` / `duration` / `status` | `request_body` / `request_params` / `response_summary` / `location` 四列**恒空** —— 审计表最该回答的两个问题("这次改了什么" / "这次动了哪个对象")**一个都答不了** | 🔴 **M126**(本节) |

> 判高危的客观标准仍然是"**挂在全局中间件链上吗**"(见 memory:框架原语零覆盖)。
> 审计中间件挂在**整个 `/rapid-admin/v1` 组**上 ⇒ 三条全中。
>
> ⚠️ **C2 的形状值得记**:一个"带一整套单测的通用工具"落地了、清单上也没有欠账,
> 但**没有任何东西调用它** —— 它在自己的包里测得再绿,也不产生任何行为。
> 这跟 memory 里"框架原语零覆盖 = 高危"是同一条,只是这次的主角不是**原语**,
> 而是一个**工具函数**:清单判它"已实现"(有代码有测试),实际上它是死的。

**M126 的修法**(全部在框架层,业务线零改动):

- **新增 `api/middleware/audit_capture.go`** —— 采集端独立成文件,理由是
  `audit.go` 回答"**什么时候记**",这个文件回答"**记什么、怎么脱敏**";
  两者混在一起的后果是改采集逻辑要去翻"记不记"的判断,改"记不记"又要去翻
  脱敏词表 —— 迟早有一处改漏。
- **三条硬规矩**(每条都有对应的可逆探针):
  1. 请求体 / query 一律先过 `desensitize`(词表见 `pkg/desensitize/body.go`);
  2. **响应体不落原文** —— 只落"骨架"(`code` + `data` 的**字段名**);
  3. 解析不出来 / 不是 JSON / 超 8KB 上限 ⇒ 落**空串**,**绝不落原文**。
- **四列的语义**(定死,避免前后端各理解一半):

  | 列 | 内容 | 为什么不是别的 |
  |---|---|---|
  | `request_body` | 脱敏后的请求体 JSON | 非 JSON / >8KB / 解析失败 ⇒ 空串 |
  | `request_params` | 脱敏后的 query | 与 `request_body` **同一份词表** —— 否则同一个 `token` 在 body 里被遮、在 query 里没被遮,就是一个洞 |
  | `response_summary` | 响应**骨架** `{"code":0,"data_keys":[...]}` | 响应体里全是解密后的手机号 / 身份证号;技术方案 §9.5 对这一列的定义只写了一个词"**摘要**" ⇒ 字段名骨架正好落在"摘要"这个词上 |
  | `location` | 路径参数(如 `id=9527`) | 没有路径参数的路由(如 POST 新建)⇒ **空** —— 猜错了比空着更坏 |

- **`request_url` 改用脱敏后的 query**:这一列是**明文列**,而 query 里可能有
  `keyword=张三` 这种 PII;审计表保留 5 年、可批量导出 ⇒ 明文 PII 落进去
  等于把高危字段复制到更容易被读的地方。
  ⚠️ `request_url` 与 `request_params` 因此**有重叠** —— 这是**故意的**:
  前者是给人看的完整地址,后者是给检索用的参数表。
- **采集必须在 `c.Next()` 之前** —— 理由见下面「两条 hertz 语义」。
- **前端**:抽屉补上 `request_body` 这一行(后端写了、页面不显示 = 白写),
  并把"这三列恒空 / 仓库里没有通用脱敏器"那段**已经过期的注释**改掉
  (`web/src/pages/audit/operation.tsx`)。

#### ⚠️ 两条 hertz 语义(这一轮查实的,原代码注释写错了)

1. **`RequestContext.Next` 是循环,不是"调用下一个 handler 一次"**:

   ```go
   ctx.index++
   for ctx.index < int8(len(ctx.handlers)) { ctx.handlers[ctx.index](c, ctx); ctx.index++ }
   ```

   ⇒ **漏调 `c.Next()` 不会让 handler 不跑**(engine 自己的 Next 循环接着跑),
   但会让**中间件剩下的代码跑在 handler 之前** ⇒ 审计行写在请求发生之前:
   `status` 恒为 1(默认 200)、`response_summary` 恒空、`error_msg` 恒空。
   ⚠️ M126 之前的代码注释写的是"漏调 = 静默不执行"—— **方向反了**,已改。

2. **`Request.SetBody` / `SetBodyString` 会把 `bodyRaw` 置 nil**
   (内部走 `BodyBuffer()`,hertz `pkg/protocol/request.go:156`)
   ⇒ handler 一旦改写请求体(解密中间件 / 字段归一化),`c.Next()` **之后**
   取到的就是**改写后**的那份,客户端原始字节没了。
   ⇒ 这才是"采集必须在 `c.Next()` 之前"**唯一真正说得清的理由**:
   审计要记的是"客户端发了什么",不是"handler 把 body 改成了什么"。

   (默认 `StreamRequestBody = false` 时 body 在跑 handler **之前**就被读进
   `req.bodyRaw`,`Request.Body()` 是只读 getter ⇒ **提前读安全**;
   流式 body 下 `Body()` 会把流读干 ⇒ 代码显式判 `IsBodyStream()` 并跳过。)

#### 补网的手段与规模

| 层 | 文件 | 用例数 | 覆盖什么 |
|---|---|---|---|
| 服务端 | `internal/api/middleware/audit_capture_test.go` | 14 | 请求体**脱敏后**落库 / 明文密码与手机号**不出现** / handler 仍绑得到 body / 非 JSON 不落 / JSON 解析失败不落 / `charset=utf-8` 要认 / 超 8KB 不落 / query 脱敏 / `request_url` 用脱敏后的 query / 响应摘要**只是骨架** / 非 JSON 响应不落 / location 落路径参数 / 无路径参数时空 / 读请求的 handler 照常跑 |
| 前端 | `web/src/pages/audit/__tests__/operation.test.tsx` | 4 | 抽屉里有「请求体」这一行且是 wire 上的 `request_body` / M126 起那四行都有值 / **故意不放的两列**(`request_method` / `cost_ms`)不能出现 / 文案来自 i18n |

合计 **18 个新用例**(M126 之前,这个页面**一条测试都没有**)。

**可逆探针 13/13 全红**:
服务端 8 条(scripts/probe_m126.py)、前端 5 条(scripts/probe_m126.mjs)。

| # | 拆法 | 必须变红 |
|---|---|---|
| ① | 采集整个删掉 | `TestAudit_WritesRequestBodyRedacted` |
| ② | `RedactJSON` 换成"原样返回" | 同上(明文密码必须消失) |
| ③ | 把采集挪到 `c.Next()` 之后 | `TestAudit_RecordsBodyAsClientSentIt` |
| ④ | `RedactQuery` 换成"原样返回" | `TestAudit_WritesRequestParamsRedacted` / `TestAudit_RequestURLUsesRedactedQuery` |
| ⑤ | 响应摘要落响应体原文 | `TestAudit_ResponseSummaryIsSkeleton` |
| ⑥ | `auditLocation` 删掉 | `TestAudit_WritesLocation` |
| ⑦ | `Content-Type` 只认精确的 `application/json` | `TestAudit_JSONBodyWithCharset` |
| ⑧ | 中间件漏调 `c.Next()` | `TestAudit_FailedRequestRecordedAsFailure` / `TestAudit_ResponseSummaryIsSkeleton` |
| ⑨ | `request_body` 那行读成驼峰 | 「抽屉里有「请求体」这一行」 |
| ⑩ | 把 `request_body` 那行整个删掉 | 同上 |
| ⑪ | 把 `request_method` 加回抽屉 | 「故意不放的两列」 |
| ⑫ | 把 `cost_ms` 加回抽屉 | 同上 |
| ⑬ | `request_body` 的文案写死中文 | 「文案来自 i18n」 |

> ⚠️ **探针 ③ 的预期第一版是错的**(写成"挪到 `Next()` 之后 ⇒ 请求体用例变红"),
> 实测**一条都没红** —— 因为默认配置下 body 早就被缓冲进 `bodyRaw`,`Next()`
> 之后照样取得到。**是探针把实现读错了,不是实现对了。**
> 为此补了 `TestAudit_RecordsBodyAsClientSentIt`(handler 改写 body 的场景)
> 才把这条不变量钉住。
> ⇒ **探针的预期也会错** —— 探针红了只能证明"网能抓住这个 bug",
> 探针**没红**说明的是"我的预期不对",**不能**说明"这处实现是对的"。

**M117-1…4 在这一轮里的位置**(它们是 C1 / C2 / C3 的**前半段**,M126 是后半段):

| commit | 做了什么 | 留下的缺口 |
|---|---|---|
| `25705b5` M117-1 | 通用脱敏器 `pkg/desensitize` | **零生产调用方**(C2) |
| `7c06005` M117-1 | 修:审计中间件的 DB 轨从未装配 | `AuditSvc` 装配上了,但**四列仍恒空**(C3) |
| `32e62de` M117-2 | 审计中间件补写字段 + 读侧 LEFT JOIN 回填 username | 只补了 `request_url` 等,**采集四列没动** |
| `2cf4fd4` M117-3 | 登录日志 browser / os 两列(表里一直有,没人写) | —— |
| `40146ec` M117-4 | 登录日志「最后活动」(`last_active_at` 按活动节流写) | —— |
| **M126** | **采集端**:四列补写 + 前端展示 + 文档 | —— |

**技术方案同步**:§9.5 的 DDL 是**概念设计**,落地表的列名与它不一样
(`resource` 未实现 / `method` 故意不写 / `path` 落在 `module` /
`status_code` 落在 `status`)。已在**技术方案 §9.5.1** 补一张
「各列的**实际**语义」对照表,并写明**哪些写入策略尚未落地**
(无 Kafka 削峰 / 无冷归档分表 / 数据变更日志不是 GORM Hook 写的)。

**§1.8 项数不变(13 项,仍 100%)** —— M126 是对 #5 的**加固**,不是新增子项。

---

## 1.9 ✅ 短信 / 通知(100%)

#### ✅ 已完成(6/14)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | 阿里云短信 provider | `pkg/msn/aliyun/aliyun.go:1-101` | 101 |
| 2 | Console provider(dev 模式打印) | `pkg/msn/console/console.go:1-23` | 23 |
| 3 | Factory 切换 | `pkg/msn/factory.go:1-5` + `pkg/msn/sender.go:1-31` | 36 |
| 4 | RateLimiter(per minute/day) | `pkg/smslimit/smslimit.go`(Redis 固定窗口 per phone + per scene;**M112 修过"每次续 TTL ⇒ 计数永不过期 ⇒ 超限手机号被永久拉黑"那个真 bug**)。🔴 **2026-10-31(M129)订正**:本行此前写的是 `pkg/sms/provider.go:1-112` —— 那个包**零生产调用方、零单测**,已于 M129 删除;真正在用的是 `pkg/smslimit`(已装配在 `main.go` + `auth_impl.go`) | 214 |
| 5 | VerifyStore(5 分钟 TTL) | `sys_sms_code` 表 + `service/auth_impl.go:VerifySms`。🔴 **2026-10-31(M129)订正**:本行此前写的是 `pkg/sms/provider.go`(已删);且 **TTL 口径此前写错** —— 旧 VerifyStore 是 5 分钟,**真正在用的 `sms.rate_limit.code_ttl` 默认是 600 秒 = 10 分钟** | - |
| 6 | Mock provider | `pkg/msn/console/console.go:1-26`(dev 模式打印到日志;`pkg/emailx` / `pkg/pushx` 各有自己的 Mock)。🔴 **2026-10-31(M129)订正**:本行此前写的是 `pkg/sms/mock.go:1-17`(已删)—— 那个 Mock 用的是标准库 `log.Printf`,**没走本仓的 zap logger** | 26 |

#### ✅ 已完成(8 个)

| # | 子项 | 当前缺口 | 优先级 |
|---|---|---|---|
| 7 | 短信发送审计(谁发了什么) | ✅ M14:`pkg/smsaudit/` 装饰 `msn.Sender`,自动计 send 耗时 + 写 `sys_sms_send_log` 表;`main.go` 给 `smsSender` 套 `smsaudit.Wrap` 一次到位(commit 474bfd0)。🔴 **M100 顺手修了两处同源缺陷**(与 §1.5 #5/#8 的两个兄弟包完全同源,修复各只有一行):① 落库不填主键 → **`sys_sms_send_log` 永远只能存 1 行**;② flush 复用底层数组(`r.batch[:0]`)→ 数据竞争 + 落库行内容被串。⚠️ **仍有 3 处同族缺陷没修**(本轮范围在 §1.5,别丢):`Shutdown` 非幂等(重复调 → `panic("close of closed channel")`)/ `Record(nil ctx)` 会 panic(后台 goroutine → 进程退出)/ 它有**自己私有的** `ctxKeyRequestID`·`ctxKeyTraceID`,`smsaudit.Inject*` 全仓零调用方(与 §1.5.1 B15 一模一样的"两套 ctx key 谁也读不到谁") | P1 ✅ |
| 8 | 短信限流(per phone / per template) | ✅ M15:`pkg/smslimit/` Redis 固定窗口(per phone + per scene,冷却 60s + 1min 窗 + 1day 窗);rdb nil 时内部 no-op(commit 42ab280)。🔴 **M112(2026-10-29)修正:"固定窗口"是假的** —— 旧实现每次计数都 `INCR` + **无条件 `EXPIRE`** ⇒ TTL 每次都被往后推 ⇒ 计数器**永不过期**;而**被拒的请求也会 INCR** ⇒ 第 11 条短信(per_day=10)被拒时又把 24h 的 TTL 续上 ⇒ **这个手机号在这个场景下被永久拉黑**(每次重试都续一次 TTL,永远不会自己恢复)。已改为 Lua 原子脚本(`INCR` + 仅当 count==1 才 `EXPIRE`),窗口 key 换前缀 `sms:win:v2:*`(旧 key 里带着被推爆的计数,自然过期);另修:注释写"0 = 不限"但代码把 0 当默认值 ⇒ 统一口径为**0/负数 = 用默认值(1 条/分、10 条/天),不解释为"不限"**(限流是安全阀,漏配必须落在"限住"这一侧)。补 16 单测 + 6 条可逆探针 | P1 ✅ |
| 9 | 邮件 provider | ✅ M51:`pkg/emailx/` 抽象 Sender(From/To/CC/BCC/Subject/HTML/Text/Headers/Attachment)+ Aliyun DirectMail(HTTP REST,HMAC-SHA1 签名)+ 通用 SMTP(net/smtp,支持 SSL/STARTTLS)+ Console(dev 打印)+ Mock(测试断言);Message.IsValid() 字段校验;EmailError{Code,Message,Retry};**零外部依赖**(只 net/http + net/smtp);15+ 单测 -race 干净(消息校验/SMTP 必填/aliyun percent encode 边界/rand nonce 长度/MIME 头/Mock 记录 + Reset/Console 打印)。🔴 **2026-10-25 接线(M4a)** —— 此前"✅ P2"只代表**代码完备**,零生产调用方;现新增 `pkg/notifyx/` 多通道分发器(Dispatcher/Channel/EmailChannel/Resolver/Fanout,22 单测 + 7 条可逆探针),`main.go` 装 `email` 通道并把 `notifyx.Fanout` 塞进 `Registry.InboxNotifier` —— **审批链的站内信自动多一条邮件通道,审批引擎一行没改**(Fanout 实现 `inbox.Notifier`,装配处是接口)。三条设计纪律:**未知 provider 不回落 console**(配错直接不装配 —— 悄悄落到 console 会让运维以为"邮件发出去了");**没有邮箱 = skipped 不是 failed**(后台账号大量不填邮箱,那是常态);**收件人邮箱做格式校验**(不像邮箱一律当没有,防的是 PII 加密落地后把 Base64 密文当地址发出去) | P2 ✅ |
| 10 | App Push provider | ✅ M52:`pkg/pushx/` 抽象 Sender(Title/Body/Targets + iOS/Android/HMS 高级字段)+ 阿里云 EMNS(HTTP REST,SignatureMethod=HMAC-SHA1,POST cloudpush.aliyuncs.com Action=Push)+ Console(Mock)+ Mock(测试);Target 4 类(account/device/alias/tag);Notification.IsValid() 校验;PushError{Code,Message,Retry};**零外部依赖**;chunkTargets 自动按 100 切批;11+ 单测 -race 干净(消息校验 7 例/Mock 记录+Reset+FailOn/EMNS 必填/签名+nonce/chunk 切批/Console 打印)。⚠️ **零接线**:`main.go` 从不 Init 它,全仓非测试代码 0 调用方(性质同 §1.9 #11 的 `InboxNotifier`:框架能力已交付,等业务侧装配)—— **有 12 个测试却没接线,别当成"推送能力已通"**。🔴 **2026-10-25 部分接线(M4a)**:`main.go` 已按 `push.provider` 装配 sender 并挂到 `Registry.PushSender`,配置块 `push:` 两份模板补齐;**但仍无投递目标** —— 全仓没有设备 token 表。**故意不把它注册成 `notifyx` 通道**:一条"永远跳过"的通道在监控上看着像通了,比没接更糟。补齐需要:设备 token 表 + 迁移 + 采集端点 + RBAC 码,属独立一轮(另:`pkg/pushx` 的 `ConsoleSender` 此前 `Out` 是个空 `bytes.Buffer`,输出被吞 —— 老测试只读 `Output()` 所以一直绿,已修为真打印) | P2 ✅ |
| 11 | 站内信 | ✅ M34:`pkg/inbox/` `Notifier` interface + `Manager` DB 持久化 + 列表/未读数/标已读;`sys_inbox_message` + `sys_inbox_user_prefs` 2 表;handler 4 endpoint(`/rapid-admin/v1/biz/inbox/messages` 列表 + `unread-count` + `messages/:id/read` + `mark-all-read`);`Registry.InboxNotifier` 业务模块投递入口;3 perm 码 `inbox:list`/`inbox:read`/`inbox:mark_read`;9 单测(commit 5e60f79);**M74 起前端全部接上**(角标 M72 + 列表弹层 M74),并把 `List` 从 `[]map[string]any` 改成 typed DTO 修掉 ID 裸数字/时间两种格式/null 与空串混用三个 wire 缺陷。🔴 **2026-10-24 修真 bug:id 改由应用侧生成**(`newRow` + `sonyflake.Next()`)—— 原来 `Notify`/`NotifyBatch` 用 map 插入**不写 id**,而 `BaseModel.ID` 声明了 `autoIncrement:false`,MySQL DDL **没有 AUTO_INCREMENT** → 严格模式下整条投递路径报 1364、非严格模式下全部 id=0(`MarkRead(userID,0)` 会把这个人的所有消息一起标已读);夹具(SQLite `INTEGER PRIMARY KEY` 自动填值 + `AUTOINCREMENT`)把 bug 盖住了,现已改成跟生产一致(`id BIGINT NOT NULL`)。⚠️ **业务模块侧仍零调用方**(`Registry.InboxNotifier` 注入了没人用,`asset.go` 的 `notify_inbox` 是孤字段)—— 属业务侧 M75+;**框架侧的数据来源有两条**:M88 的审批超时巡检 + **M3 的审批动作**(新待办→审批人 / 结果→发起人,`ref_type=approval`;⚠️ M3 给 `ApprovalEvent` 加了 `Result bool` 用来分「会签的中间票」与「整单通过」,**不广播**,wire 契约未变)。🔴 **2026-10-25(M4a)**:`Registry.InboxNotifier` 现在挂的是 `notifyx.Fanout` 而不是裸 `inbox.Manager` —— 站内信照旧落库,**顺带**走额外交付通道(当前只有邮件)。业务侧零调用方这一点不变(`asset.go` 的 `notify_inbox` 仍是孤字段) | P1 ✅ |
| 12 | 通知中心(用户拉未读) | ✅ M34:同 §1.9 #11,`/rapid-admin/v1/biz/inbox/unread-count` + Redis pub/sub `inbox:user:<id>` 推未读变更(前端已接 SSE 实时刷新,M72 `useEventStream`);rdb nil 时 pub/sub 自动降级(commit 5e60f79) | P1 ✅ |
| 13 | 短信配置热替换(配置中心 → 生效配置 → 阿里云) | ✅ C6(2026-10-29):**此前配置中心里那 9 个 `aliyun.sms.*` 只存不读** —— 面板上能看能改,但 sender 只在启动时读一次配置文件 ⇒ **运营换了密钥/模板,短信照发不误,而且没有任何报错**(典型的"面板是摆设")。本轮接线:`msn.Resolver`(每次发送前解析一次当前生效配置)+ `msn.DynamicSender`(按解析出的 provider 选后端)+ `aliyun.AliyunBackend`(凭证指纹 AK\|SK\|Region 变了才重建 client,没变就复用)。**合并规则 = "运营改过的才算数"**:面板值生效 ⇔ 该行未停用 且 值非空 且 值 ≠ 该行自带的默认值 —— 因为 `seedAllSysConfigs` 跳过已存在的行,存量库里那 9 行可能还是老模板号这种**假值**,"面板一律优先"会把刚配好的配置文件盖回假值;反过来"没人改过"就自动让位给配置文件,运营真改过的依然生效,**清空某一格 = 退回配置文件**(唯一的撤销手段)。⚠️ 读配置中心失败 ⇒ **退回配置文件 + 打 Warn**,不返回错误(配置中心抖一下不该让所有人收不到登录验证码)。⚠️ 面板上的 `rate_limit.*` **没有进配置中心**(限流参数仍只能改 yaml),且配置中心的双签工作流与这一组**没有接上**(仍是"唯一写通道 = PATCH update")。补 16 个单测 + 9 条可逆探针(全红) | P1 ✅ |
| 14 | 短信模板审核流(状态机 + 登记册 + 守卫 + 后台页) | ✅ M115(2026-10-30):**此前平台完全不知道模板在服务商那边过没过审** —— 服务商拒发只回一句 `isv.SMS_TEMPLATE_ILLEGAL`,用户看到的是"发送失败",平台侧查无此案。补齐四层:① `pkg/smstpl/` 状态机(`unknown/pending/reviewing/approved/rejected/cancelled/disabled`)+ 登记册(`sys_sms_template` / `sys_sms_template_log` 2 表,落"谁在什么时候给了什么结论、为什么");② `Guard` 守卫 —— 按模板状态 + 平台级策略(`off` 放行 / `warn` 只告警 / `block` 拦下,默认 `block`)决定验证码发不发得出去;③ `Syncer` —— 阿里云 `GetSmsTemplate` 查询真实审核状态,10 分钟 cronx 定时同步;④ 后台页(看状态 / 人工停用止血 / 手动同步 / 流转历史),与定时任务**同一个 `Runtime.SyncOnce` 入口**。四个场景:`login` / `reset_password` / `change_phone` / `view_full_phone`。两条硬规则:**人工无权判定通过**(恢复只落到"待审核",最终结论等服务商下一次同步)、**人工停用不被服务商结论覆盖**(服务商说通过也翻不动人工停用)。⚠️ **console provider 压根没有审核状态**(`ErrNoFetcher`)—— 状态只能是 `unknown`,**不是"通过"**(把"查不到"当"通过"是这类系统最贵的错) | P1 ✅ |

> **复用价值**:**中**

---

## 1.10 ✅ 文件存储 / OSS(100%)

#### ✅ 已完成(4/10)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | Provider 接口 | `pkg/oss/provider.go:1-41` | 41 |
| 2 | Mock provider(dev) | `pkg/oss/mock.go:1-32` | 32 |
| 3 | **阿里云 OSS provider** ✅M10 | `pkg/oss/aliyun/aliyun.go:1-280` `Provider`:`SignUploadToken(ctx,dir,ttl,maxSize)` PostObject 直传凭证(后端签 base64 policy + HMAC-SHA1,前端 form POST 不需要 SDK)+ `SignDownloadURL` **稳定直链**(不签名、不带过期时间)+ `Delete` 批量删除(**单桶**,不再按 public/ 前缀分桶);`SafeKey` 防 path traversal;依赖 `github.com/aliyun/aliyun-oss-go-sdk v3.0.2`(commit 073bd61) | 280 |
| 4 | **下载直链**(不是预签名) | 🔴 **2026-10-30 M80 订正**:`SignDownloadURL` **不再签 GET presigned** —— 返**稳定直链**(配了 CDN 域名就用 CDN,否则 bucket 域名 + key),`ttl` 参数**不生效**(签名 URL 一过期,页面上的图就裂);桶 ACL 必须是 **public-read**,私密靠"key 猜不到"(uid 是雪花 id + 对象名带随机后缀),不靠 bucket ACL | - |

#### ✅ 已完成(6 个)

| # | 子项 | 当前缺口 | 价值 | 优先级 |
|---|---|---|---|---|
| 5 | 分片上传 | ✅ M53:`oss.MultipartProvider` InitMultipartUpload(返 upload_id+part_size)/ UploadPart(ETag 记录)/ ListParts(断点续传)/ CompleteMultipartUpload / AbortMultipartUpload;aliyun 实现走原生 SDK InitiateMultipartUpload/UploadPart(bytes.Reader,int64,partNumber)/ListUploadedParts/CompleteMultipartUpload(UploadPart 数组)/AbortMultipartUpload;MultipartMinSize 100MB / MultipartPartSize 10MB / MultipartMaxPartNum 10000;🔴 **2026-10-30 M80 订正**:`pickBucket` **单桶**(所有 key 落同一个桶,不再按 public/ 前缀分流),`bucketsForPrefix` 也只返那一个桶;**零业务耦合** | P2 ✅ |
| 6 | 断点续传 | ✅ M53:同 §1.10 #5,ListParts 返已上传 []oss.Part 数组(PartNumber+ETag+Size),客户端补传缺失即可;InitOption 可设 ContentType/Metadata/ACL | P2 ✅ |
| 7 | 文件病毒扫描 | ✅ M53:`oss.ScanProvider` Scan/GetScanResult;aliyun 当前 stub(返 ErrUnsupported,提示"need Aliyun CIM 凭证");CIM 凭证配后业务方接 Aliyun CloudThreatDetection SDK 即可,**框架占位** | P2 ✅ |
| 8 | 缩略图生成 | ✅ M53:`oss.ThumbnailProvider` GetThumbURL(key,size) 走 OSS Image Processing ?x-oss-process=image/resize,{size} 拼接;size 默认 200x200 | P2 ✅ |
| 9 | 访问权限 / 防盗链 | ✅ M53:`oss.HotlinkProvider` SignToken(绑 key+expire,base64(json)+hmac-sha1)+ Verify(校验 key/expire/sig,subtle.ConstantTimeCompare 风格 hmac.Equal);token 格式 `payload.sig` | P2 ✅ |
| 10 | 存储配额 / CDN 刷新 | ✅ M53:`oss.QuotaProvider` GetUsage/SetLimit + `oss.CDNProvider` Refresh/Purge/Prefetch;aliyun 当前 stub(返 ErrUnsupported 提示"need Aliyun CDN"),**框架占位**;配额计数业务侧可用 `kvstore.Counter`(`pkg/kvstore/kvstore.go:53`,RedisCounter 多实例 / MemoryCounter 单实例)实现 —— 原先此处与 aliyun stub 的报错文案都写的是 `cachex.Counter`,而 **cachex 里根本没有 Counter 这个类型**,是条指向幻影 API 的悬空指引,commit 6aec7a0 已改 | P2 ✅ |

> **复用价值**:**中** —— P0 已实装,后续 P2 等业务量上去再做
> **P0 全部完成** —— 2026-10-19 之后无 P0 OSS 项待补

---

## 1.11 ✅ 工具集(100%)

#### ✅ 已完成(5/11)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | Sonyflake 分布式 ID(17 位,约 9.7e16) | `pkg/sonyflake/snowflake.go:1-281` **🔴 M112**:机号以前会撞(没传 MachineID ⇒ 库用"私网 IPv4 低 16 位" ⇒ 同低 16 位的机器 / 同节点副本机号相同 ⇒ **生成出完全相同的 ID** ⇒ 主键重复),兜底 ID 用 `UnixNano()` 也不唯一(本机实测 10000 次里 9999 次相同);现在自算机号(IP低16 XOR 主机名哈希 XOR pid)+ `SONYFLAKE_MACHINE_ID` 显式指定 + 结构化兜底 ID + 17 条单测 | 281 |
| 2 | 分页(PageRequest/PageResponse) | `pkg/pagination/page.go:1-54` **🔴 M112**:`Offset()` 以前不归一化 ⇒ `Page=0` ⇒ `OFFSET -20` 在 repository 层炸;现在自己兜底(对副本归一化,无副作用)+ 8 条单测 | 54 |
| 3 | 响应包装(OK/Fail) | `pkg/response/response.go:1-136` | 136 |
| 4 | 时间(AsiaShanghai) | `pkg/timeutil/time.go:1-22` | 22 |
| 5 | 脱敏(Phone/IDCard/Name/Email/BankCard/ContractNo) | `pkg/desensitize/desensitize.go:1-227` **🔴 M112**:盲索引用的是**写在仓库里的公开 key**(⇒ 拿到库的人能反查手机号),现在走 `crypto.blind_key`;掩码边界(短手机号 / 两字姓名 / 多字节邮箱)一并修 + 20 条单测 | 227 |

#### ✅ 已完成(6 个)

| # | 子项 | 当前缺口 | 价值 | 优先级 |
|---|---|---|---|---|
| 6 | 参数校验器(validator 框架集成) | ✅ M20:`pkg/validatorx/` go-playground/validator 集成,`RegisterTagNameFunc` 走 json tag(前端对得上),`Struct()` + `Explain()` 拆失败字段中文短句,15 个内置规则 + `RegisterCustom` 业务追加;`api/handler/handler.go:119` `BindAndValidate` 装饰器自动跑(commit 5be77af)。**🔴 M112**:`Explain` 原来用**类型断言**而非 `errors.As` ⇒ 任何一层 `%w` 包装都让**字段名整个丢掉**(用户只看到"参数校验失败");`BindAndValidate` 无条件拼 `field + ": " + msg` ⇒ 认不出字段时消息变成 `": 参数校验失败"`;`RegisterBuiltins` 把注册错误 `_ =` 丢掉(实测 v10.30.5 重复注册**既不 panic 也不报错**,注释写的"会 panic"是错的)。三者均已修 + 17 条单测。**⚠️ 仍开着:全仓 DTO 一个 `validate` tag 都没有**(hertz 默认校验 tag 是 `vd`,仓库用的是 `c.BindJSON` 不是 `BindAndValidate`)⇒ 这个组件目前**基本没人用**,`BindAndValidate` 只在极少数 handler 上 | 中 | P1 ✅ |
| 7 | 并发原语(WorkerPool / Semaphore / Singleflight) | ✅ M37:`pkg/syncx/` WorkerPool(固定大小 goroutine 池 + 任务队列,Submit 满时阻塞背压,Close 优雅等队列清空 / CloseNow 立即退出丢弃剩余,panic 内部 recover,SubmitCtx ctx cancel 支持,Stats 统计)+ Semaphore(chan 实现 + Acquire 阻塞 / TryAcquire 非阻塞 / Release 多次安全 / Close 后返 ErrSemaphoreClosed);**17 单测** -race 干净(commit 432e03d 落地 + c100262 补 2 条,原 13) | 中 | P2 ✅ |
| 8 | 重试 / 退避(指数回退) | ✅ M29:`pkg/retryx/retryx.go` `Do(ctx,Opts,fn)` 泛型包装,full jitter,默认 predicate 拒 `context.Canceled` / `DeadlineExceeded`(M29) | P1 ✅ |
| 9 | 限流组件(令牌桶 / 漏桶纯库) | ✅ M41:`pkg/ratelimitx/` TokenBucket(capacity + rate,无锁 atomic.Int64 + lastRefill unix nanos,tokens * 1e6 浮点精度;refill 推进 lastRefill 防 idle 突满抢并发;Allow/AllowN/Available/Stats)+ LeakyBucket(capacity + rate 严格平滑输出;drain 推进 lastDrain;Level 自动 drain)+ KeyedBucket(per-key 桶池,LRU maxKeys + 空闲 TTL 后台 reaper 1min 扫;Get 持锁保证 exp 续期并发写安全;Allow/Len/Stop);与现有 middleware/ratelimit 互补(Redis 分布式 vs 进程内极快);12 单测 -race 干净 3x 复测(commit `e60912b`)。🔴 **2026-10-31(M129)补一句**:这一行说的是**原语库已交付**,**不是"进程内限流已经在跑了"** —— 本包**零生产调用方**(commit `e60912b` 至今没人调)。它要接的是**具体端点**(现在线上限流走的是 `api/middleware/ratelimit.go` 的 Redis 那套,两者互补不冲突)。⚠️ 判「这个原语有没有用」要看的不是它的 12 条单测,是**它的调用方**(见 memory:框架原语零覆盖 = 高危) | 中 | P2 ✅ |
| 10 | 后端错误码 i18n(后端返 code,前端翻) | ✅ M6:`web/src/types/api.ts:70-100` `BIZ_CODE_KEYS` 34 码映射;`web/src/locales/useI18n.ts:79-98` `tError(code, fallback)` 三级 fallback(i18nKeyMap → 翻译 → 后端 msg → `code N`);`api/client.ts:143-153` 拦截器调 tError 替原 data.message(commit 7c406c7) | 中 | P1 ✅ |
| 11 | 安全随机 token / 邀请码 | ✅ M50:`pkg/randomx/` 全走 crypto/rand(线程安全 + 加密学安全),4 类字符集 CharsetURLSafe/AlphaNum/NoConfusing(无 0/O/Q/1/I/L)/Numeric;Token(n) / FromCharset / NumericCode / AlnumCode / InviteCode / Hex(n) / Base64(n) / PINCode / MustToken / String 统一 API;Entropy(n, charsetLen) 估算 bit 熵;统一替代散落的 fmt.Sprintf("%d", rand.Int()) / uuid.New().String();15 单测 -race 干净(URL-safe 字符集 / 1000 token 互不重 / 数字 + 长度 / NoConfusing / Hex/Base64 字符集 / Charset 空 fallback / Entropy 6 用例 / MustToken no panic / String 默认 32) | 中 | P2 ✅ |

> **复用价值**:**中**

---

### 1.11.1 并发原语审计(2026-10-22,commit c100262)

"✅ 已完成"是就**代码完备**说的。读代码之后有三件事清单上没写:

| 项 | 实况 |
|---|---|
| **WorkerPool 零生产调用方** | `NewWorkerPool` / `SubmitCtx` / `CloseNow` / `PoolStats` 全仓只出现在 `syncx` 自己的测试里。`Semaphore` 只有一个调用方:`cachex/warmer.go:131`(且只走 `Acquire`+`Release` 这条最简单的路)。框架层原语**先落地、后接线**,这是既定顺序,不是缺陷 —— 但别把"✅"读成"已有模块在用它" |
| **`Stats().Workers` 报的是队列大小** | 原写法 `Workers: cap(p.tasks)` —— 默认构造(size=4, queue=64)下监控面板显示"64 个 worker"。字段带 json tag,错值会被原样发出去。已修(c100262) |
| **panic 曾被完全静默吞掉** | `_ = recover()` 不记日志,而 `executed.Add(1)` 无条件执行 —— 崩掉的任务计成"已执行",Stats 全绿。已改为打日志(panic 值 + stack)。**刻意不加 `panicked` 计数字段**:零调用方的结构体上没人读,等第一个调用方接进来再定义不迟 |

**并发上限此前零覆盖**:池子的全部意义就是"固定大小",而 15 个旧测试唯一碰 `Running` 的是"排空后应为 0"(测收尾不测上限)。`i < cfg.Size` 改成 `i < 1`,旧测试全部照过。已补 `TestWorkerPool_ConcurrencyCap`(探针实测变红:`并发未在 2s 内打满:Running=1,want 4`)。

**刻意没做的两件**(读代码后判断是冗余,不是遗漏):

- `SubmitCtx` 也会阻塞却不计 `blocked`,与 `Submit` 口径不一致。补这个要加非阻塞快进,会改"ctx 取消 vs 有空位同时就绪"的判定方向 —— 为一个零调用方的 API 改语义不值当。只把 `PoolStats.Blocked` 的注释写准("只统计 Submit 路径"),不假装它覆盖全部阻塞。
- `PoolConfig` 是导出类型但没有构造函接收它。读完发现 `cfg.Size` / `cfg.QueueSize` 在构造里真的被读了,**它不是死代码**,删它是纯 churn。

### 1.11.2 内存池:评估过,不引入(2026-10-22)

曾被问"框架层的内存池模块要不要做"。**结论:不做**,理由三条:

1. **Go 已经有内置内存池** —— `sync.Pool`(Go 1.3 起)。Go 官方 Code Review Comments 对它的定位是 *"use it for freelists, not for caching"*。再写一个 `poolx.New[T]()` 包装 `sync.Pool`,等于把标准库原样抄一遍还多一层 API。
2. **Go 的取舍是刻意的:分配便宜,交给 GC**。手写对象池的典型后果是忘记 `Reset()`(下一个使用者拿到脏数据,极难查)、忘记 `Put`(池子永远空)、池内对象被外部持有(悬垂引用)。
3. **真正成立的使用场景只有一类**:持有 C 侧大块内存、构造代价高的对象 —— `gzip.Writer`(flate 窗口 ~1.2MB)、编解码器、大 scratch buffer。**本仓库这类场景已覆盖**:`api/middleware/gzip.go:60` 的 `gzipBufPool` 是全仓**唯一**的 `sync.Pool`,就一处用,内联即可,不该被抽成模块。

至于"内存"本身的管理,Go 里的对应物是 `GOMEMLIMIT` / `GOGC`,那是**部署参数不是模块**(全仓当前零配置)。

> 本节留在文档里是为了**销账**:否则下次盘点还会把它当成欠账捡起来。

---

## 1.12 ✅ 动态业务集合(100%)

> **本节是 2026-11-02 补的文档盲区** —— 本清单此前**完全没有 dyn 条目**,
> 而 `docs/业务表单设计器设计方案V1.0.md` 描述的整套能力(M151-M153)早已落地。
> 跟 M67 那次一样:**清单漏记 ≠ 没做**,但读清单排期的人会漏掉一整块能力。
>
> **2026-11-02(第二次更新)**:①本节的四个计数块从 `###` 改成 `####` ——
> **守门网只认 `####`**(`scripts/check_backlog_consistency.py` 的 `SUB_RE`),
> 原来用 `### 1.12.1` 写的那两块**守门网根本没扫到** ⇒ 本节的 91% 是纯人工维护的数字,
> 改错了不会红。改成 `####` 之后它第一次进了机器校验。②补进 4 项欠账
> (字段类型 / 导入导出 / 行级操作 / 审批适配器)⇒ **91% → 67%**。

**定位**:让**不懂代码的人**在后台里设计一张业务表(比如"设备巡检记录"),
设计完就自动有:物理表、增删改查接口、菜单、权限码、列表管理页。

⚠️ **它不是"表单引擎",是"动态业务表"** —— 落的是**真实的数据库表**,不是 JSON blob,
所以能被 SQL 查、被报表统计、被别的模块 JOIN。
(选型过程见 `docs/业务表单设计器设计方案V1.0.md` §0.2:
SurveyJS 设计器收费 / Formily 官方设计器锁死 antd4 ⇒ 只能自研。)

#### ✅ 1.12.1 已完成(10 个)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | 动态集合原语层(字段类型注册表 / 归一化 / 反归一化 / 命名规则 / 动作清单) | `pkg/dynx/{dynx,normalize,denormalize,naming,options,actions}.go` + 341 行单测 | 1349 |
| 2 | 集合与字段元数据管理(建表 / 软删 / 停用 / 复活 / 默认值与选项集校验) | `service/dyn_impl.go` | 632 |
| 3 | 记录数据面 CRUD(分页 / 关键字 / 排序白名单 / 软删 / owner 过滤) | `service/dyn_records.go` | 494 |
| 4 | 运行期登记资源码 + 菜单 + 绑创建者角色 + 同步 casbin 策略 | `service/dyn_resource.go` | 292 |
| 5 | 设计面 HTTP API(**10 条**,每条恰好一个 `rbac()` ⇒ 启动期一致性检查过) | `api/router/router.go:548-558` + `api/handler/v1/dyn.go` | 571 |
| 6 | 数据面 HTTP API(**6 条**,不挂 `rbac()`,handler 层 `dynGuard` 默认拒绝) | `api/router/router.go:793-798` + `handler/v1/dyn.go` 的 `dynGuard` | — |
| 7 | **两套资源码形状**:设计面四段 `dyn:design:*` / 数据面三段 `dyn:<集合码>:<动作>` | `pkg/dynx/actions.go` `RecordCode` / `RecordModule` + `pkg/dynx/naming.go` `ValidateCode`(**集合码不可能含 `:` ⇒ 撞不了**) | 197 |
| 8 | 物理表字段列**可空**(M153)—— "没填"落成 NULL 而不是零值 | `internal/model/dyn.go` + `service/dyn_impl.go` 建表逻辑(commit `e0caf5d`) | 83 |
| 9 | 前端业务表单设计器(集合列表 + 字段编辑 + 选项集编辑) | `web/src/pages/system/dyn/{index.tsx,OptionsEditor.tsx}` | 831 |
| 10 | 前端通用记录页(搜索 / 新增 / 编辑 / 详情 / 删除,**列与表单全按字段定义生成**) | `web/src/pages/dyn/{index.tsx,recordForm.ts}` + `web/src/api/dyn.ts` | 802 |

#### ❌ 1.12.2 未完成(0 项)

> **暂缓第二期**(2026-11-03 用户拍板框架收尾评估,移出计数;技术盘点全文保留,真要做时是现成施工图):
> - 原 #12 **字段类型补齐**(关联表 / 子表 / 附件 / 人员 / 公式 / 唯一约束)—— 6 个子特性是**产品级工作量**(JOIN 查 / 一对多子表 / 公式引擎各是一块);**10 种基础类型已覆盖"动态业务表"的核心闭环**,扩展类型是加法不是地基。另有一条顺手账:select 选项集不绑字典(选项存字段自己的 JSON 列,本仓明明有字典中心)—— 做扩展类型时一起收。
> - 原 #15 **审批适配器**(任意 dyn 集合当审批对象)—— 引擎本身已通用(`BizHandler` SPI),缺的是适配器;但要碰审批引擎 SPI + handler 层权限判定(仓铁律:service 不能 import middleware),急不得,单独立项。
>
> **原文留档(施工图)**:
> - 原 #12:`pkg/dynx/dynx.go` 只注册了 10 种基础类型(text / longtext / int / number / bool / date / datetime / select / multiselect / json)。缺的:关联到另一张集合(JOIN 查)、一对多子表、附件(框架已有文件系统 M80)、人员选择器、公式字段、唯一约束(建表时加索引)。⚠️ select 的选项集不绑字典 —— 选项存在字段自己的 JSON 列上(`model/dyn.go:70-71`),而本仓有一整套字典中心 ⇒ 同一个"状态"在两个集合里要各维护一份;`internal/pkg/dynx/` 与 `service/dyn_*.go` 里 `dict` 一处引用都没有。
> - 原 #15:通用审批系统(`service/approval_engine_impl.go` + `BizHandler` 接口)只接了 `digital_material` 一条线 —— service/approval_material.go 写死"只碰 `biz_digital_material` 一张表" ⇒ 别人 clone 下来之后,审批引擎不能给自己的 dyn 集合用(引擎本身是通用的,缺的是"dyn 集合 → BizHandler"的通用适配器 + 审批对象的注册入口)。

| # | 子项 | 当前缺口 | 价值 | 优先级 |
|---|---|---|---|---|
| 11 | **数据范围规则接动态集合数据面** | ✅ **M160 已接上**(2026-11-02)。①M159 补的**纯 SQL 出口**(`scope.ApplySQL`)用上了 —— `service/dyn_scope.go` `dynScopeCond()` → `scopeService.EffectiveScope` + `scope.ApplySQL`。②**物理表只有 `owner_id` 一维** ⇒ "本项目""本部门"表达不出来 ⇒ **响亮报错**(不是静默跳过 —— 丢掉"或"组里最后一个分支会让整段过滤变成"不过滤" ⇒ 数据越权)。③**列表与单条都接了**(`recordWhere` + `recordScope`)。④**条件与参数绑在一起**(`dynCond{sql, args}`)—— 参数错位不报错,只是查出错的数据。⑤**业务域现算**(`dynx.RecordModule(code)`),不读 ctx(读 ctx 会在"没走 handler"时回落成默认规则组 ⇒ `dyn:<编码>` 上配的规则整组不生效且无报错)。⑥**测试** 8 条 + **7 条可逆探针实测全部转红** | ~~高~~ 已实现 | ~~P1~~ → 已收 |
| 13 | ✅ **批量导入 / 导出**(**M177,2026-11-03 已落地**,CSV 通道) | 服务端按条件导出**全量** CSV(复用 list 的筛选/排序/数据范围查询构建;BOM + label 列头 + **公式注入防护** `csvGuardCell`,文本类 `=+-@` 开头加 `'` 前缀、数字负值不加 —— 与前端 exportCSV 同口径);导入 CSV 表头认字段标识**或**显示名(导出文件不改表头可直接导回),逐行走元数据校验链(归一化/选项集/required),语义 `mode=all`(默认,一坏全不进)| `mode=skip`(跳错行),错误行报告(行号/字段/原因),行上限 5000/5MB;不引 excelize(Excel 明确不支持)。前端导入 Upload + 结果 Modal、导出 blob 下载。9 条新单测含导出→删→导回 round-trip;可逆探针:拆注入防护 → 用例红 → 复原绿 | 中 | P1 ✅ |
| 14 | ✅ **行级操作**(回收站 / 恢复 / 批量删 / 批量改字段)(**M177,2026-11-03 已落地**) | 恢复(`Unscoped` 置 `deleted_at=NULL`;物理表无唯一约束 ⇒ 无冲突可撞,直接恢复)+ 回收站视图(`recordWhere` 加 Trash 开关,行内动作换"恢复")+ 批量删(范围过滤)+ 批量改单字段(**走元数据校验链**,与单条 update 同一条归一化/选项集路径,未知字段/选项集外/必填 nil 全拒);`dynInsertRecord` 从 CreateRecord 抽出,导入与单条新建**同一条装配**(必填/默认值/显式 NULL 单源)。资源码 5 个新动作 `dyn:<集合码>:<动作>` 全走 handler 层 `dynGuard`(仓铁律 service 不碰 middleware);老集合启动期 `BackfillRecordResources` 幂等回填,**新码只绑给绑过对应老码的角色**(restore/batch_delete→delete、batch_update→update、export→list、import→create),不白送只读角色;main.go fail-fast。前端回收站开关 + BatchActionBar 批量删/改 + locale 27 key(zh/en 同 commit) | 中 | P1 ✅ |

#### ⚠️ 1.12.3 遗留缺口台账(**不计入本节完成度**)

| 项 | 说明 |
|---|---|
| 设计器页**零前端单测** | `web/src/pages/system/dyn/`(724 行)没有任何用例;只有 API 层(`api/__tests__/dyn.test.ts`)与 `recordForm` 的纯规则有测。⚠️ 判"这个页面有没有被读过"要看用例,不看行数 |
| 物理表数量无上限 | 建一千个集合 = 一千张表;没有配额,也没有"孤儿表"回收 |
| 字段类型缺"附件 / 人员 / 关联表" | 框架已有文件系统(M80)与用户体系,接进来是自然的下一步。**2026-11-02 起已升格为正式欠账 §1.12 #12**(本行留档,不再单独计) |
| 集合不能导入 / 导出 | 批量建表场景需要。**2026-11-02 起已升格为正式欠账 §1.12 #13**(本行留档,不再单独计) |
| 没有"设计器操作审计" | 谁在什么时候改了哪个字段,目前只有通用操作日志 |
| §2.7 #15「通用 schema 驱动表单组件」**只被部分覆盖** | M152 的表单生成器**与动态集合耦合**(字段定义来自 `sys_dyn_field`),**不是**一个"业务页声明个 schema 就能用"的通用组件 ⇒ §2.7 #15 **仍然开着** |

#### ⚠️ 1.12.4 两条设计约束(写进来是因为它们会再被改坏)

| 约束 | 为什么 |
|---|---|
| **集合编码 / 字段标识 / 字段类型三样不可改** | 它们分别是物理表名、物理列名、记录 JSON 的键的来源 ⇒ 改了等于换表 + 换权限。要改走"新建 → 搬数据 → 停用旧的"(停用≠删除,物理表与数据保留) |
| **数据面的资源码是运行期拼出来的** | 集合编码由用户起 ⇒ 编译期枚举不了 ⇒ 前端路由守卫写不了,闸门只能在后端;`dynGuard` 里的 `CanAccess` **默认拒绝** —— 拿不到 `dyn:<集合码>:<动作>` 就进不去 |

---

## 1.13 ✅ 数据可见性(行级 + 字段级)(100%)

> **本节是 2026-11-02 补进来的第二块盲区** —— 行级数据范围(M155 / M156)
> **已经在仓里落地两天了**,而清单此前一个字没提(只在 §1.12 #11 顺带带过一句)。
> 跟 §1.12 同一个毛病:**清单漏记 ≠ 没做**,但漏记的那块**没人排期、没人验、没人知道它只做了一半**。

**定位**:权限有三个轴,现在**三个都做实了**(字段级那一轴的**服务端一半 + 配置界面**已落地;
**前端消费那一半**(M168)也已落地 —— 表格列 / 表单字段按规则显隐 —— 见 §2.7 #16)。

| 轴 | 问的问题 | 归谁 |
|---|---|---|
| ① 功能权限 | 「这个人**能不能**调这个接口 / 点这个按钮」 | §1.1 RBAC / Casbin |
| ② 行级数据范围 | 「这个人**能看哪些行**」 | **本节**(规则层 + 配置界面 + 纯 SQL 出口 + **dyn 数据面接线** + **业务查询接线**全部已落地) |
| ③ 字段级可见性 | 「这个人**能看哪些字段**」 | **本节**(规则层 + **执行层** + **配置界面** + **前端消费**(M168)全部已落地) |

> ⚠️ **②和③不是一回事,别混**:② 决定一行给不给你看;③ 决定一行里的某列给不给你看。
> `docs/业务表单设计器设计方案V1.0.md:720` 已经写明这件事:「数据范围是**行级**的;字段级要另做一套」。

#### ✅ 已完成(2 个)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | **行级规则模型 + 解析 + 落查询**的框架原语(M155 + M156 + M159) | ①**原语包** `internal/pkg/scope/`(8 文件:`scope.go` 规则模型 + `Resolve` / `apply.go` 落 SQL(GORM 链式)/ `sql.go` **纯 SQL 出口** / `module.go` 业务域登记 / `context.go` ctx 传递 + 3 个测试文件)。②**规则存储** `internal/model/scope.go`(`sys_data_scope`,唯一键 `uk_ds(role_id, module, scope_type)`)—— 规则类型 all / project / dept / dept_and_child / self / custom。③**规则服务** `internal/service/scope.go` + `scope_impl.go`(角色↔业务域的规则 CRUD + 二级缓存 + 失效)。④**接口** `internal/api/handler/v1/scope.go` + 3 条路由(`api/router/router.go:467-469`:`GET/PUT /roles/:id/scopes`、`GET /scope-modules`)。⑤**业务域注入**:`api/middleware/rbac.go:84` `c.Next(scope.WithModule(ctx, module))` + `api/handler/v1/dyn.go:541` `scope.WithModule(ctx, dynx.RecordModule(code))`。⑥**落查询**:`service/scope_impl.go:449-467` `Apply(ctx, tx, cols)` → `scope.Apply` 拼 `clause.Expr`。⑦**纯 SQL 出口**(M159):`scope.ApplySQL(s, cols)` → `(SQL 片段, 参数)`—— **给"自己拼 WHERE"的地方用**(动态业务集合的数据面就是这种)。**两个出口由同一个函数编译**,不会漂移(漂移的后果:GORM 那条路过滤了、裸 SQL 那条路没过滤,而两边都不报错)。⑧**示例** `server/examples/scope/main.go`。**commit**:M155 `3386cbb`(规则层)+ M156 `607ba90`(执行层)+ M159(纯 SQL 出口) | - |
| 2 | **后台配置界面**:角色页「数据范围」抽屉(M157)—— 规则层落地两天了,**没人能配** | ①**API** `web/src/api/scope.ts`(3 条接口;**复用 `role:read`/`role:assign`,不开新资源码** —— 新开码要进 seed 资源表 + 13 角色绑定矩阵,漏绑一个就是那个角色整个按钮不渲染)。②**纯逻辑** `web/src/pages/system/role/scopeRules.ts`(分组 / 「全部数据」判定 / 校验 / 差量 / 集合比较)—— 单独一个文件,才能豁免 react-refresh 那条 lint。③**抽屉** `web/src/pages/system/role/DataScopeDrawer.tsx`(左业务域菜单 + 右规则编辑 + 三条语义提示)。④**入口** `web/src/pages/system/role/index.tsx` 行内动作「数据范围」(与「分配权限」并排但**不是一回事**)。⑤**测试** 50 条(纯逻辑 33 + 组件 17),**4 条可逆探针实测全部转红**(藏菜单 / 全量发 / 把 UI 哨兵 key 当业务域 / readOnly 仍可编辑)。**commit**:M157 `f5c051f` | 698 |

#### ❌ 未完成(0 项 —— 全部已收)

| # | 子项 | 当前缺口 | 后果 | 优先级 |
|---|---|---|---|---|
| 2 | **业务查询没接线**(框架层做完了,业务层一行没改) | ✅ **M161 已接上**(2026-11-02)。①**接线是声明式的**:`internal/service/biz_scope.go` 三张账本(`bizScopeSpecs` 业务表 / `bizScopeCtxWired` 走 ctx 业务域那条路 / `bizScopeUnwired` **明确没有**行级范围 + 原因)—— 业务域字符串**只有一处**,不再两处各写一个字面量。②**17 个业务域、36 处调用点**(17 列表 + 17 单条 + `delivery.GetCurrentStage` 的兜底那条)。③**列表与单条都接** —— 只过滤列表的话,用户仍能**按 id 打开**看不见的记录。④**维度落不下去 ⇒ 响亮报错**,不静默跳过(丢掉"或"组里最后一个分支 ⇒ 整段过滤变成"不过滤" ⇒ 数据越权)。⑤**配置期就拒**:`ReplaceRoleScopes` 校验"这一维在这个业务域上落不落得下去";后端候选列表每个域还报得出 `supported_types`(前端按它过滤下拉框)。⑥**不声明 ⇒ 启动失败**:`service.CheckScopeWiring()` 在 `cmd/server/main.go` 里 `log.Fatal`。⑦**两条守门网**:启动期比"路由登记的业务域 vs 三张账本";测试期(`biz_scope_wiring_test.go`)**扫源码**比"账本里登记的业务域 vs 真的 `applyBizScope` 调用点"。⑧**测试** 29 条(接线 25 + 扫源码守卫 3 + 路由层 1)+ **9 条可逆探针实测全部转红**(拆掉列表的 scope 接线 / 拆掉 Get 的 scope 接线 / 让 `checkDims` 静默 / 把列模板映射错 / 让 `CheckScopeWiring` 失效 / 前端忽略 `supported_types` / 后端不报 `supported_types` / 拆掉 family 的接线 / 改错业务域字面量) | ~~今天的项目隔离不是鉴权,是前端便利~~ ⇒ **已接上** | ~~P1~~ → 已收 |
| 3 | **动态集合数据面接不上** | ✅ **M160 已接上**(2026-11-02)。①**纯 SQL 出口**用上了:`service/dyn_scope.go` `dynScopeCond()` → `scopeService.EffectiveScope` + `scope.ApplySQL`(**不走 `ScopeService.Apply`** —— 它吐 GORM 链式,而且从 ctx 读业务域;dyn 的 service 方法自己就知道集合编码,从 ctx 读会在"没走 handler"时回落成默认规则组 ⇒ `dyn:<编码>` 上配的规则整组不生效且无报错)。②**物理表只有 `owner_id` 一维** ⇒ "本项目""本部门"**表达不出来** ⇒ 响亮报错(不是静默跳过)—— 跳过"或"组里最后一个分支会让整段过滤变成"不过滤" ⇒ **数据越权**。③**列表与单条都接了**(`recordWhere` + `recordScope`):只过滤列表的话,用户仍能**按 id 打开**看不见的记录。④**条件与参数绑在一起**(`dynCond{sql, args}`):以前是两个平行切片,加一条带自己参数的条件时顺序极易错,而**参数错位不报错**,只是查出错的数据。⑤**业务域现算**(`dynx.RecordModule(code)`),不读 ctx。⑥**测试** 8 条 + **7 条可逆探针实测全部转红**(不接 scope / 只接列表不接单条 / 静默跳过不表达出来的维度 / 参数顺序反了 / 没配规则也当成有限制 / 拆掉 owner_scoped / 破坏默认组回落) | ~~dyn 集合 = 行级数据范围**完全失效**~~ ⇒ **已接上** | ~~P1~~ → 已收 |
| 4 | **字段级可见性 / 脱敏** | ✅ **M162 / M163 / M164 已落地**(2026-11-02)。①**规则层** `internal/pkg/fieldpolicy`(三态模式 visible / masked / hidden + 七种打码方式 + **逐字段叠加** `Overlay` + 纯函数不碰 DB)+ `sys_field_policy` 存储(唯一键 `uk(role_id, module, field)`)+ `service/field_policy*.go`(规则 CRUD + **候选从响应模型反射**(`json` tag 名,排除 `-`)+ **配置期就拒三件事**:业务域不存在 / 字段不在候选里 / 打码猜不出方式)。②**执行层** `internal/api/handler/field_policy.go` `maskData` —— 收口在 `BaseHandler.OK` / `OKWithMessage` / `OKID` / `Page` / `PageWith`(⚠️ **"绕不过去"这件事现在是 M167 的机械守卫盯着的**,不是一句口头承诺);**失败一律闭**(查不到规则就 500,不是原样放行 —— 放行 = 一次 DB 抖动就把敏感字段放出去);**没配规则 ⇒ 响应逐字节不变**;`UseNumber` 防雪花主键被 float64 截断成另一个 id。③**配置界面**(M164)角色页行内动作「字段可见性」⇒ 抽屉(左业务域菜单 + 右字段表):`web/src/api/fieldPolicy.ts` + `web/src/pages/system/role/fieldPolicyRules.ts`(纯逻辑单独成文件)+ `FieldPolicyDrawer.tsx`;**复用 `role:read`/`role:assign`,不开新资源码**(新码要进 seed 资源表 + 13 角色绑定矩阵,漏绑一个 = 那个角色整个按钮不渲染)。④**手册** `server/docs/security/fieldpolicy.md`(2026-11-02 新写)。⑤**测试** 后端(原语 + service + handler)+ 前端(纯逻辑 26 + 组件 22 = **48 条**);前端 **15 条**、后端 **6 条**可逆探针实测全部转红。⑥**交叉守门网**(M166)—— 行级账本(`bizScopeSpecs` / `bizScopeCtxWired`)里的每个业务域都必须在字段层登记(`bizFieldDomains`):`CheckFieldPolicyWiring()` **启动期 fail-fast** + `TestBizFieldDomain_行级账本里的每个域都在字段层登记` + 纯函数单测。⚠️ **反方向**曾经没有网 ⇒ 行级层新增一个域而没登记进字段层 ⇒ ① 执行层早退(`fieldPolicyDomainSupported`)⇒ 该域响应**完全不过脱敏**;② 配置期拒「业务域不存在」⇒ 管理员**也配不出来**;⇒ **配了不生效,且没有任何报错**。⑦**启动期 fail-fast 的调用点也有网**(`cmd/server/main_startup_check_test.go` **扫源码**:检查被删掉、或 `log.Fatal` 被换成 `log.Warn` ⇒ 用例红 —— 网本身有人守还不够,**网还在不在跑**也要有人守)⑧**出口绕不过去也有网**(M167)—— `internal/api/handler/outlet_guard_test.go` **扫源码**(`go/ast`):把"实际写了响应的地方"和"账本上登记的豁免"对账(扫 `internal/api/` 整棵树:handler / router / middleware)。⚠️ "脱敏收口在出口"此前只是一句**手工核对过的话** —— 写一个 `c.JSON(200, rows)` 就绕过去了,响应照常返回,**没有任何报错**。⇒ 现在:绕过出口的写入点必须逐条登记 + 写明理由 + **条数精确**(同一个函数里多一处就红);`pkg/response` 的业务出口调用**只在那五个出口函数体内**才算正当(挪出去就红)。⚠️ 判据靠**类型**(`*app.RequestContext`)不靠变量名,`c` / `ctx` / `rc` 都认。今天账本 **10 条**(健康检查 / 就绪探针 / 文件字节流 / SSE 信号流 / `/version` / 中间件三处),**8 条可逆探针实测全部转红**(加一处 `c.JSON` / 加一处 `fmt.Fprintf(c,…)` / 加一处 `response.OKWithData` / 把出口里的调用挪到别的函数 / 删一条豁免 / 条数改错 / 加过期豁免 / 出口改名) | ~~「客服部看不见手机号」这类要求做不了~~ ⇒ **服务端那一半已做实** | ~~P1~~ → 已收(余 #5 一条尾巴) |
| 5 | ✅ **前端消费那一半**(M168,2026-11-02 已落地 —— 归 §2.7 #16 的那一半,这里记账) | ①**核心纪律**(M149 那条「显示层不许自己算,必须读执行层那个权威字段」):报告由**执行层**(响应出口)算好,前端**只读** —— 不重抄规则表、前端不出现业务域字符串、业务页零改动。②**传输**:`X-Field-Policy: hidden=a,b;masked=c` 响应头(只在配了规则时出现;**规则没命中也照样带** —— 报的是「规则说了什么」,不是「这次改了什么」)+ 响应数据上的 **Symbol 戳**(`Symbol.for('rapid-admin.fieldPolicy')`,`enumerable: true` —— Object.keys / JSON.stringify / for...in 都看不见,但 `{...row}` 展开**带得走**;递归盖章信封 + 每行,深度 8 + WeakSet 防环)。③**盖章点**在 axios 拦截器(`web/src/api/client.ts`):读头 → 解析 → 盖章 → **按请求发起时的页面路径归档**(页面重拼了行对象时兜底;路径不匹配 → null —— 宁可没保护,不串页)。④**三个共用外壳消费**:`ManagementTable` 隐藏的**列整个不渲染**(「列还在、格是空的」的修法);`EditModal` 隐藏项不渲染 / 打码项变只读;`ViewDrawer` 隐藏项不渲染。⑤⚠️ **打码字段必须从提交载荷里剔掉**(`EditModal` 的 `omitRestrictedFields`)—— antd `validateFields()` 会把 disabled 字段的值照常收进来,原样提交 = `138****1234` 覆盖真实手机号 = **数据损坏**(有测试守着)。⑥**CORS `ExposeHeaders`** 补了 `X-Field-Policy`(跨域 JS 看不见未白名单的响应头 —— 漏了就是静默 undefined;`router.go` 顺手抽成 `corsConfig(cfg)` 纯函数 + 3 条直测)。⑦**没有报告 ⇒ 一切照旧** —— 绝大多数页面没配规则,零开销快路径(20 处 ManagementTable / 15 处 EditModal / 13 处 ViewDrawer 的「不变」路径全部有测试锁住)。⑧**测试**:后端 8 条(报告模型 3 + 响应头 5)+ CORS 3 条 + 前端 60 条(utils 23 / 纯逻辑 17 / 拦截器端到端 5 / useTable 通道 4 / 组件 11);**2 条可逆探针实测转红**(拆掉拦截器盖章 → 2 条红;拆掉 EditModal 载荷剔除 → 1 条红)。⑨**手册**:`server/docs/security/fieldpolicy.md` 「前端消费那一半(M168)」一节 | ~~列还在、格是空的 + 打码值可被原样提交回写~~ ⇒ **已收** | ~~P2~~ → 已收 |

> **⚠️ #2 收尾时要说准的几件事(别把它读成"隔离已经上线了")**:
> - ✅ **M161(2026-11-02)已接上** —— 17 个业务域、36 处调用点(17 列表 + 17 单条)。
>   **但接线 ≠ 有人配了规则** —— 规则表里一行没有的话,
>   `EffectiveScope` 返回零值 ⇒ **行为与接线前完全一样**(不过滤)。
>   这是"收窄层"的设计,不是 bug。
> - ⚠️ **今天在 dev 库里看不出效果** —— `biz_project` **0 行**、两条身份的 `project_id` **全是 0**
>   ⇒ 没有"项目 A / 项目 B"可跨,也没有"只属于项目 10 的身份"。
>   ⇒ **接线的真实效果要等有项目身份才验得到**(单测里用真 sqlite 验过,见 M161 的 29 条用例)。
> - ⚠️ **"不配规则 = 不过滤"这条默认语义没变** —— `scope.Apply` 的第一句还是
>   `if !s.Restrictive || s.All { return tx, nil }`。
>   变的是**"忘了接"不再可能**:每个业务域必须在一张账本里登记,
>   登记了却没被调用 ⇒ `biz_scope_wiring_test.go` **扫源码**当场变红。
>   ⇒ 框架强制**不是靠"忘了接就当没这回事"**,是靠**两张网**(启动期账本核对 + 测试期源码核对)。
> - ⚠️ **多租户上线前仍要做的一件事** —— 给相关角色**真的配一条规则**。
>   接线和配规则是两步,清单只管到第一步。
>
> **⚠️ 另外两条必须说清的现状(别拿"框架层做完了"当"功能可用")**:
> ① **开发库里 `sys_data_scope` 表还不存在** —— M155/M156 落地后没跑 `cmd/migrate`;
>   而且 `sys_user_identity` 现有 2 行的 `project_id` **全是 0**
>   ⇒ 「本项目」这条路今天对谁都匹配不到行(安全方向)。
> ② **手册里已经把这话说清楚了** —— `server/docs/security/scope.md`(2026-11-02 新写)的「注意事项」
> 第一条就是「**规则层上线不改变任何现有查询的结果**」。
> ⚠️ **M160 / M161 之后那几段改了**:dyn 的数据面**已经接上**了(纯 SQL 出口 + dyn 侧接线),
> 手册现在写的是「动态集合的数据面:只有『归属人』一维」——
> 包括"本项目/本部门 在 dyn 上表达不出来 ⇒ 响亮报错"这条;
> M161 又补了一节「业务查询接上数据范围」,写清三张账本 + 各表能表达哪几维 +
> 「落不下去 ⇒ 响亮报错」+ 两条守门网。
> ⇒ **文档与实现仍然是一致的**(不是"手册吹牛",也不是"清单说半截话")。

> **⚠️ #4 收尾时要说准的几件事(别把它读成"字段级已经全线做完了")**:
> - ✅ **服务端那一半做实了** —— 规则层(M162)+ 执行层(M163)+ 配置界面(M164)+ 手册一章。
>   ⇒ 「客服部看不见手机号」这类要求**现在配得出来、也真的生效**。
> - ⚠️ **"配得出来"不等于"有人配了"** —— `sys_field_policy` 表里一行没有的话,
>   `EffectiveFieldPolicy` 返回空策略 ⇒ **响应与框架落地前逐字节相同**(不过滤)。
>   这是"收窄层"的设计,不是 bug(与行级数据范围同一条约定)。
> - ✅ **前端那一半也已落地**(M168,2026-11-02 —— 见 §2.7 #16 与本节 #5)——
>   三个共用外壳按报告收窄:隐藏的列 / 字段整个不渲染,打码字段变只读且**从提交载荷里剔掉**
>   (antd `validateFields()` 会把 disabled 字段的值照常收进来 —— 原样提交 = 打码值覆盖真实值 = 数据损坏)。
> - ✅ **两个登记表之间的交叉守门网已补上**(M166)—— 见 #4 ⑥⑦。行级账本里的每个域都必须在字段层登记,
>   **两个方向都有网**(字段层 ⊆ 行级层:老用例;行级层 ⊆ 字段层:M166 新增),
>   且**启动期的 fail-fast 调用点**也被扫源码守着(网还在不在跑,也要有人守)。
>   ⚠️ 与 M161 撞出的 `family` 同一类洞("登记了"不等于"接上了"),现已用两张网堵住。
> - ✅ **出口绕不过去也有网**(M167)—— 见 #4 ⑧。`internal/api/handler/outlet_guard_test.go`
>   **扫源码**(`go/ast`):绕过出口的写入点必须逐条登记 + 写明理由 + **条数精确**。
>   ⚠️ "脱敏收口在出口"此前只是一句**手工核对过的话** —— 写一个 `c.JSON(200, rows)`
>   就绕过去了,响应照常返回,**没有任何报错**。⇒ 现在这类洞会**红**。
> - ⚠️ **它只做"收窄",不做"放宽"** —— 能把**已经在响应里**的字段打码或删掉;
>   **不能**把本来就不在响应里的字段(如 `json:"-"` 的加密手机号)变出来。
>   ⇒ 要让某个角色看到某个字段,得业务侧先把值放进响应 —— 那是业务决定,不是框架决定。
> - ⚠️ **别把它与另外两件事混**:① `pkg/desensitize` 只做**日志脱敏**(写日志时打码),
>   **不按角色遮字段**;② 前端 `SensitiveField` 是"所有人一起打码",
>   不是"按角色决定能不能看"。**按角色决定可见性的唯一通道是这一层。**

---

## 1.14 ✅ 领域事件总线 / 事务性 outbox(100%)

> **本节记的是"框架层一个包都没有"的能力** —— 不是"做了一半",是**完全没做**(2026-11-03 **M175 已全部落地**,以下 ❌ 记录转为账目)。
> 之所以要单开一节:材料把它和 RBAC、数据范围并列为"通用后台框架该有的横切能力",
> 而清单此前**连一行都没提** ⇒ 清单显示"后端 97%",实际上有一整类能力**根本不存在**。

**定位**:模块之间"发事件"的能力。落地前的现状是**跨模块只能直接 import service 调用** ——
耦合、可能形成循环依赖、失败要一起回滚、"谁在等谁"全靠调用方自己记。

#### ✅ 已完成(4 个)

| # | 子项 | 证据 |
|---|---|---|
| 1 | ✅ **事件定义 + 发布 / 订阅原语**(`pkg/eventx`,**M175,2026-11-03**) | `internal/pkg/eventx/eventx.go`:类型化事件名 `Name`(点号分段)+ `Event{Name, ID, Payload}`(**信号不带业务正文**,与 push 同纪律)+ `Bus` 进程内订阅 / 发布(单 handler panic 隔离);`PublishAsync` / `Async` 把 `context.WithoutCancel + go func() + recover()` 三件套收口为**全仓唯一实现**(两个手写发布者的重复拷贝就此消灭)。34 条包测试 |
| 2 | ✅ **事务性 outbox**(业务事务内落表 + 投递器 + 投递状态) | `sys_event_outbox` 表(`model/event_outbox.go`;⚠️ ID 由 sonyflake 显式发放 —— 探针实测 BaseModel `autoIncrement:false` 不赋值第二行就撞主键)+ `eventx/outbox.go` 的 `Enqueue(ctx, tx, …)`(**真·事务内落表**,回滚探针用例证明)+ `deliverer.go` 投递器:**ticker goroutine(默认 1s)而非 cronx**(cron 5 段最小粒度 1 分钟,会把"改字典 → 前端刷新"从即时变分钟级;`Tick` 已是可单测单元)。状态机三态 `pending → delivered / dead`(**没有 failed** —— 失败行仍是 pending,由 attempts + next_retry_at 表达);退避 5s×2^n 封顶 5m,8 次进死信;**`ErrTransportUnavailable` 退避但不烧次数**(Redis 宕机不该把积压事件送进死信);at-least-once 语义注释明示,精确一次走 Deduper;已投递行 24h 清理,死信不自动清 |
| 3 | ✅ **消费侧幂等 + 重试 / 死信** | `eventx/dedupe.go` 的 `Deduper.Once`(Memory + Redis SETNX 两实现):TTL 内已成功消费 ⇒ `ErrDuplicate`;**fn 失败不记录**(可重试);Redis 挂 = 退化成不去重(不因去重层故障丢事件)。生产侧 `idem_key` 唯一索引 + `INSERT ON CONFLICT DO NOTHING` |
| 4 | ✅ **把现成的手写发布者收口** | `approval_publisher.go` / `dict_publisher.go`:零参构造保留(直推通道,M88/M94 行为**逐字节相同**,现有 12 条测试一行未改全绿);新增 `WithOutbox` 变体,**main.go 生产装配已切换**。落表失败(DB 抖动)⇒ 退回直推不丢事件;`WireBytesEqualDirectPath` 用例证明两条通道 payload **逐字节相同**(前端"广播后重拉 total"语义不变);唯一变化:outbox 路径推送晚 ≤1 个轮询间隔。**7 条接缝测试** |

> **测试**:eventx 包 34 条 + service 接缝 7 条;**6 条可逆探针实测全部转红→照抄原版复原→绿**(①拆 Async 的 WithoutCancel ⇒ 4 条红;②投递器失败静默吞成 delivered ⇒ `FailureBacksOff` 红 —— **主会话独立复验,同样红**;③拆 Enqueue 幂等键冲突处理 ⇒ 红;④拆 MemoryDeduper 去重检查 ⇒ 红;⑤publisher 落表失败吞掉不退回直推 ⇒ 红;⑥outbox 载荷包一层 map 破坏线字节 ⇒ `WireBytesEqualDirectPath` 红)。`go build` / `go vet` / `-race` 全绿(全量 service 套件 829s,必须 `-timeout 20m`);dev 库 `cmd/migrate` 实测建表 + seed + 守门网全过

#### ❌ 未完成(0 项 —— 全部已收)

> **成本(实付)**:3-4 天估算 → 单 agent 一次会话交付。

---

## 2. 前端框架层 10 大类(总览)

| # | 类别 | 关键文件 | 完整度 |
|---|---|---|---|
| 2.1 | Auth Store / useAuth | `stores/user.ts` `hooks/useAuth.ts` | **100%** (§2.1) |
| 2.2 | Permission Store / usePermission | `stores/permission.ts` `hooks/usePermission.ts` | **100%** (§2.2) |
| 2.3 | Axios Client / HTTP | `api/client.ts` `utils/request.ts` `hooks/useRequest.ts` | **100%** (§2.3) |
| 2.4 | Dict / Config Store + hook | `stores/dict.ts` `stores/config.ts` `hooks/useDict.ts` `hooks/useConfig.ts` | **100%** (§2.4) |
| 2.5 | 错误处理 / 通知 | `api/client.ts` 拦截器 + antd `message`/`notification` | **100%** (§2.5) |
| 2.6 | i18n | `locales/{zh-CN,en-US,useI18n,registry}.ts` | **100%** (§2.6) |
| 2.7 | 表单组件 | `components/common/{EditModal,ViewDrawer,SearchForm,...}.tsx` | **100%** (§2.7) |
| 2.8 | 表格 / 布局 / 路由 | `components/{common,layout}/*` + `hooks/useDragSort.ts` | **100%** (§2.8) |
| 2.9 | Hooks(useTable/useRequest/useDict/useConfig) | `hooks/`(6 文件) | **100%** (§2.9) |
| 2.10 | Types / API 包装 | `types/*` `api/*`(10 个文件) | **100%** (§2.10) |

---

## 2.1 ✅ Auth Store / useAuth(100%)

#### ✅ 已完成(9 个)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | Zustand Store | `stores/user.ts`(**2026-10-21 整体重写**) | 287 |
| 2 | 双后端持久化(记住我 → localStorage / 否则 → sessionStorage) | `utils/storage.ts` `activeBackend()` + `clearSessionFrom()` / `purgeLegacyKeys()`;`stores/user.ts` `writeTokens()`(**硬编码 TTL 常量已删,一律取服务端 `expires_in` / `refresh_expires_in`**) | - |
| 3 | `hydrateUserStore()` 启动恢复 | `stores/user.ts:263-276`(先跑 `purgeLegacyKeys()` 再读)🔴 **M108**:判据从"blob 里写着 isLoggedIn"改成"**同一后端里确实还有 access 或 refresh token**"(见 §2.1.1 A2) | - |
| 4 | useAuth hook(全字段 + hasIdentity + switchIdentity + logout) | `hooks/useAuth.ts:1-27` | 27 |
| 5 | IdentitySwitcher(高敏角色 SMS 二次验证) | `components/layout/IdentitySwitcher.tsx:1-173` | 173 |
| 6 | AuthButton(perm + roleAnyOf) | `components/auth/AuthButton.tsx:1-45` | 45 |
| 7 | **空闲超时 30min**(2026-10-21 新增) | `utils/idleTimer.ts` `isIdleExpired()` 纯判定 + `startIdleTimer()`(15s 轮询) + `handleIdleTimeout()`;活动时间戳落 **localStorage** 以多标签页共享;`main.tsx` 启动时挂载。🔴 **M108**:基线改成**会话级**(登录/启动恢复时对齐、登出时清),超时处理加**重入守卫**,跳转带 `redirect`(见 §2.1.1) | 143 |
| 8 | **跨标签页会话同步**(2026-10-25 M8 新增) | `utils/tokenSync.ts` 纯协议(`TokenSync` 状态机 + `createBroadcastTokenChannel()` 通道工厂,**不碰 store** —— 协议单测才好写);`utils/tokenScheduler.ts` 接线(`getTokenSync()` 单例 + 恢复链 + 轮换检测);`stores/user.ts` 补 `getRefreshTokenExpiresAt()` / `getPersistedUserId()`;`main.tsx` **启动即订阅**(广播消息不排队,订阅晚了就错过) | 258 |
| 9 | Header 集成 + 退出登录 | `components/layout/Header.tsx:44-64` | - |

> 🔴 **M110 补记**:登录**密码摘要链**(`utils/crypto.ts` —— 前端 `sha256("rapid-admin::" + 明文)` → 64 位 hex,
> 后端 bcrypt 比对)此前**零覆盖**,而它的降级路径**不是 SHA-256** ⇒ 非安全上下文(局域网 HTTP)下**登录必然失败**。
> 已修 + 补 10 条用例(NIST 标准向量 + 与 WebCrypto 逐输入对拍)⇒ **前后端契约不变**,见 **§2.3.2 B1**。

> **复用价值**:**高** —— 任何项目都需,✅ 完整

---

### 2.1.1 会话持久化 / 空闲超时层审计(2026-10-29,M108)

**起因**:§2.1 标着 **100%**,但这一层的**两个地基文件一条网都没有**:

- `web/src/utils/storage.ts` —— 全站**唯一**的会话落盘出口(记住我 / 续期 TTL / 清理)—— **零覆盖**;
- `web/src/utils/idleTimer.ts` —— 空闲超时的**全部**实现 —— **零覆盖**;
- `web/src/stores/user.ts` 的持久化片段(`hydrateUserStore` / `setLogin`)—— 只有 M8 那批
  跨标签页用例擦到边,**判定分支本身没测过**。

> 判高危的客观标准还是"**挂在全局路径上吗**"(见 memory:框架原语零覆盖)。
> 这一层同时握着三个不可逆动作:**吊销后端会话 / 清本地凭证 / 跳登录页**,
> 而且它是「记住我」能不能落地的**唯一裁决者**。

M108 把这一层逐行读了一遍,读出 **2 个 P1 + 4 个 P2**。

---

**P1 机理 #1:空闲基线跨会话残留 ⇒「记住我」的持久会话实际只值 30 分钟**

技术方案 §7.7.4.15 对这两件事的措辞是:

| 机制 | 文档承诺的是 |
|---|---|
| 空闲超时 | "**开着不动**会不会被踢" |
| 「记住我」 | "**浏览器关掉也在**" |

这两句话在**同一节**里,但旧实现让它们**互相抵消**:

- 空闲时间戳(`LAST_ACTIVE_AT`)落 **localStorage**,**跨会话、跨浏览器重启一直残留**;
- 计时器只在**应用运行时**存在 —— 浏览器关掉的那几个小时**没有任何代码在跑**;
- 于是:昨天 18:00 关掉浏览器,今天 09:00 打开 ⇒ 第一次轮询(15s)就判定
  "空闲 15 小时 > 30min" ⇒ **刚恢复的会话被踢下线**;
- 服务端那边 refresh token 明明还活 30 天(`refresh_ttl: 720h`)。

⇒ **用户勾了「记住我」,实际只能保住 30 分钟**,而且没有任何报错 —— 只是"莫名其妙又要登录"。

而且这不止影响跨浏览器重启:**登出后重新登录**同样中招(旧时间戳躺在盘上,
新会话一起来就被自己上一次会话的活动时间判死)。

**判据(采用哪一边)**:以 §7.7.4.15 表格自己的措辞为准 ——
**浏览器关掉的那段时间不该算进空闲预算**。理由是它有客观依据:
计时器那会儿根本没在跑,"用户没在用"这件事在应用不运行期间**无从观测**。

⇒ 修法:空闲基线是**会话的一部分**。

| 时机 | 动作 |
|---|---|
| 登录成功(`setLogin`) | 基线对齐到现在(必须在 `clearSessionFrom()` **之后**,那一步会把它清掉) |
| 启动恢复会话(`hydrateUserStore`) | 基线对齐到现在 —— 恢复出来的会话**重新起算** |
| 登出 / 401 重置 | 基线清掉(`LAST_ACTIVE_AT` 进 `SESSION_KEYS`,`clearSessionFrom()` 一并清) |

落盘仍走 **localStorage**(不是会话所在的那个后端):空闲判定要**多标签页共享** ——
A 标签页挂着不动时,B 标签页有操作就该给整个会话续命(见 `utils/idleTimer.ts` 头部说明)。
**共享的落点不变,变的只是"它归不归这次会话管"。**

---

**P1 机理 #2:持久化的 USER blob 没有 TTL ⇒ token 全过期后仍判"已登录"**

`STORAGE_KEYS.USER` 是**给页面首屏少发一次 `/me` 请求**用的缓存,它是**没有 TTL 的**
(信封里的 `expire_at` 缺省 = 永不过期)。而 token 有 TTL。于是会出现这个状态:

> **token 全过期了,blob 还在。**

旧 `hydrateUserStore()` 只看 `persisted.isLoggedIn` ⇒ 内存态被置成"已登录",可实际上
**一个凭证都没有**。后果链条:

```
AuthGuard 放行 → 受保护页面渲染出来
  → 每个请求都因为没有 Authorization 头而 401
  → 401 → doRefresh() → 没有 refresh token → handleUnauthorized()
  → 弹"登录已过期" + 跳登录页
```

⇒ 用户看到的不是"干脆停在登录页",是"**首页闪一下,然后被踢回登录页**"。

**判据(改成)**:同一个后端里**确实还有 access 或 refresh token**,才算还有会话。

> ⚠️ **这条不能改成"access token 必须有效"** —— access 过期但 refresh 活着是
> **正常的"需要续期"场景**,`bootstrapTokenRefresh()` 正是靠它接手。
> 判据只看**凭证在不在**,不看**凭证新不新**。

顺带:没有凭证却躺着一份 blob ⇒ **孤儿**(会话早就过期/被吊销)。顺手删掉 ——
它里面有用户资料(PII),留着没意义,还会让下一次 hydrate 再误判一次"已登录"。

---

**其余 4 条**(逐条核实):

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B3 | `idleTimer.ts` `handleIdleTimeout()` | **没有重入守卫** | **P2**:15s 轮询可能在 `await logoutApi()` 的网络往返期间再次触发(此时 `isLoggedIn` 还是 true —— `reset()` 还没执行)⇒ **重复发 `/auth/logout`、重复跳登录页**。网络越慢越容易撞上 |
| B4 | `idleTimer.ts` 跳登录页的 URL | 只带 `?reason=idle`,**丢掉当前路径** | **P2**:401 路径(`api/client.ts` 的 `handleUnauthorized`)一直是带 `redirect` 的 —— 两条路径行为不一致,空闲被踢的人重新登录后**被丢回首页**,得自己再点回去 |
| B5 | `storage.ts` `get()` 的 catch 分支 | 只 `console.error` + `return null`,**不删坏 key** | **P2**:坏数据(手改过 / 被别的版本写坏 / 被截断)永远占着那个 key ⇒ 每次读都刷一条 error 日志;而且调用方**分不清"没这数据"和"这数据坏了"** —— 两个都返回 `null` |
| B6 | `storage.ts` 的 `getAcross()` | 零调用方,但**有毒** | **P2**:它是"记住我好像没实现"那个 P0 的**历史成因** —— 拿它读 token 时,浏览器里残留的上一份 localStorage token 会把本次 sessionStorage 登录顶掉。见下方"处置" |

---

**修法**(框架层,零业务代码):

| # | 修法 |
|---|---|
| A1 | `stores/user.ts` 加 `touchIdleBaseline()`(内部 helper),在 `setLogin()` 与 `hydrateUserStore()` 里各调一次;`LAST_ACTIVE_AT` 进 `utils/storage.ts` 的 `SESSION_KEYS` |
| A2 | `hydrateUserStore()` 加 `hasCredential` 判据(同一后端里 access 或 refresh token 至少有一个还在);孤儿 blob 直接删 |
| B3 | `idleTimer.ts` 加模块级 `handling` 布尔守卫(`stopIdleTimer()` 一并复位) |
| B4 | 抽 `buildIdleLoginUrl()`(导出,便于直接测"redirect 不丢"):`/login?reason=idle&redirect=<encodeURIComponent(pathname+search)>` |
| B5 | `get()` 的 catch 分支里删掉坏 key(删失败也不能把读炸掉 —— 配额/隐私模式下 `removeItem` 会抛) |
| B6 | **删掉 `getAcross`**,并在原地留注释说明它为什么有毒(防止下一个人把它捡回来) |

---

**零调用方 / 边界处置表**(主动交代,不装死)

| 对象 | 处置 | 理由 |
|---|---|---|
| `storage.getAcross()` | **删除**(并留"为什么删"的注释) | 与 §2.4.1 的 `stores/config.ts` **刻意相反**:那个是**接口权限不够**(不是能力重复)⇒ 保留+修好+测住;这个是**纯冗余 + 有毒** —— 它本身**就是**一个已修 P0 的历史成因,留着就等于把那个 P0 的复现方式留在工具箱里。零调用方 + 已进历史 ⇒ 删 |
| `idleTimer.stopIdleTimer()` | **保留**(生产代码**零调用方** —— `main.tsx` 只调 `startIdleTimer`) | 它是 start/stop 这一对里的 stop:计时器是**模块级**状态,不给一个"停"的出口,将来任何"暂停计时"(比如打开弹窗时不算空闲)都只能去改模块内部变量。而且测试的 `afterEach` 靠它复位模块级状态(含本轮新加的 `handling` 守卫)—— 没有它,`handling` 一旦为 true 就再也复位不了 |
| `storage.Store.clear()` | **保留,但加警告**(生产代码**零调用方**) | 它清的是"本后端下所有带 `rapid-admin:` 前缀的 key"—— **会连 zustand persist 写的 `rapid-admin:theme`(装着 theme / lang / sidebar_collapsed)和 `rapid-admin:tabs` 一起清掉**(那两个 key 由 persist 直写 localStorage,值是裸值不是信封)。⚠️ 本轮已在源码里补上这条警告。**清会话要用 `clearSessionFrom()`,别用这个** |
| `storage.ts` 的信封格式 `{value, expire_at}` | **不动**(已在文件头写死警告) | 它与 zustand persist 的**裸值**格式不兼容 —— `storage.get(THEME)` 会返回 `undefined`(连 `null` 都不是)。theme / lang / sidebar_collapsed / tabs **各有主人**,不许走本 Store |

> ⚠️ **为什么删 `getAcross` 而不是留着修好**:它的问题**不在实现**,在**语义** ——
> "两个后端都算对"这件事对 token 根本不成立(token 的位置**就是**会话的位置)。
> 修它 = 保留一个"看起来能用、实际会复现 P0"的 API。

---

**回归网**:**3 份新用例文件,26 条前端用例**:

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/utils/__tests__/storage.test.ts` | 10 | 前缀幂等(`fullKey` 不重复拼);TTL 过期后 key 被删;坏 JSON 自愈(删 key + 返回 null);`activeBackend()` 以 REFRESH_TOKEN 为锚点(平局偏向 session);`clearSessionFrom()` 两个后端都清(**含 LAST_ACTIVE_AT**);`SESSION_KEYS` 含 `LAST_ACTIVE_AT`;`purgeLegacyKeys()` 清双前缀孤儿;**`getAcross` 不再导出**;**清会话不误伤 persist 的 key**(theme / tabs);**`store.clear()` 会误伤**(锁住那个已知的坑) |
| `web/src/utils/__tests__/idleTimer.test.ts` | 9 | 判定边界(29:59 不超时 / 30:00 超时 / 从没记录过不超时);活动事件刷新时间戳;**不重入**(网络往返期间只发一次登出);超时后本地凭证清干净;`buildIdleLoginUrl()` 带 redirect;未登录不触发超时;已登录且过期才触发;**P1 回归**:关掉浏览器 4 小时再打开 ⇒ **不会被立刻踢下线** |
| `web/src/stores/__tests__/user.session.test.ts` | 7 | `setLogin` 对齐空闲基线;`logout` 清掉基线;**token 全过期 + 只有 blob ⇒ 不算已登录**(且孤儿 blob 被删);refresh token 还活着 ⇒ 会话恢复;恢复时重新对齐基线;身份解析;sessionStorage 后端的会话 |

**可逆探针**:scripts/probe_m108.mjs,**9 条探针,实测全部红在指定用例上,还原后全绿**:

| # | 探针 | 打红的用例 |
|---|---|---|
| ① | 把 `LAST_ACTIVE_AT` 从 `SESSION_KEYS` 摘掉 | 登出清会话 / logout 清基线 |
| ② | hydrate 不再重置空闲基线 | 关掉浏览器再打开 / 恢复时重置基线 |
| ③ | `setLogin` 不再对齐基线 | setLogin 对齐基线 |
| ④ | hydrate 退回只看 `persisted.isLoggedIn` | token 全过期只有 blob 不算已登录 |
| ⑤ | `get()` 不再自愈(照抄改动前的原版) | 坏 JSON 被删 |
| ⑥ | `handleIdleTimeout` 退回**改动前的原版**(照抄 `git show HEAD:`) | 不重入 |
| ⑦ | `buildIdleLoginUrl` 不带 redirect | 跳转带 redirect |
| ⑧ | 把 `getAcross` 复活 | 不再导出 getAcross |
| ⑨ | **覆盖网探针**:把 `THEME` 加进 `SESSION_KEYS`(让清会话误伤 persist 的 key) | 清会话不会误伤 persist 写的 key |

> ⚠️ **⑨ 是"覆盖网探针",不是修复探针**:它守的那条边界(`clearSessionFrom()`
> 不碰 persist 的 key)**本来就是对的**。探它是为了排除"**用例自己也会假绿**"——
> 一个永远绿的用例和一个永远红的用例同样没用。

> ⚠️ **探针锚点要容忍 CRLF**(老规矩):`storage.ts` / `stores/user.ts` 是 CRLF,
> 锚点里的 `\n` 要写成 `\r?\n` 才打得到 —— 否则脚本报"探针没打中"。
> **"探针没打中"和"测试没红"是两回事**,前者更危险(它会让人以为这条修复不承重)。
>
> ⚠️ **Windows 专属坑**(本轮沿用 M107 的写法):`execFileSync('npx', …)` 必须带
> `shell: true`(npx 实际是 npx.cmd,否则 ENOENT 被 catch 吞掉 ⇒ 输出 0 个用例 ⇒
> **报成"绿"**)。脚本里 0 个用例一律**抛错**,绝不判 GREEN。

**验证**(2026-10-29):

| 项 | 结果 |
|---|---|
| 全量 vitest | **59 文件 / 443 用例 全绿** |
| `npx tsc --noEmit` | **50 个错误 = 基线不变**,本轮碰过的文件 **0 个** |
| `npx eslint`(本轮 6 个文件) | **0 problems**(顺手清掉 1 个 `consistent-type-imports`) |
| `npx prettier --check`(本轮 3 个**新**文件) | **全合规** |
| `npx prettier --check`(本轮改过的 3 个源文件) | `storage.ts` **合规**;`idleTimer.ts` / `stores/user.ts` **不合规 —— 但全是 HEAD 就有的老问题**(见下),本轮新增的行**没有一条**被 prettier 挑出来 |
| scripts/probe_m108.mjs | **9/9 红在指定用例上,还原后全绿** |
| `scripts/check_backlog_consistency.py` | ✅ 自洽 |

> ⚠️ **prettier 那条要说清楚(Windows 上有个坑)**:
>
> 1. **本仓 `.gitattributes` 是 `* text=auto` + `core.autocrlf=true`** ⇒ **提交进仓的是 LF**,
>    但 Windows 的工作区是 CRLF。而 prettier 配的是 `endOfLine: "lf"` ⇒
>    **在 Windows 上直接 `prettier --check` 这些文件,一律报"不合规"** ——
>    哪怕它一行都没改。**这个信号是假的。**
>    有意义的检查是**先把 CRLF 归一化成 LF 再查**(那才是 git 会存进去的内容)。
> 2. 归一化之后:`storage.ts` **合规**;`idleTimer.ts` 和 `stores/user.ts` 不合规,
>    但**挑出来的全是本轮没碰的老行**:
>    - `idleTimer.ts`:`isIdleExpired(...)` 签名超 100 列 + 文件末尾缺换行;
>    - `stores/user.ts`:`getRefreshTokenExpiresAt` 与 `writeTokens` 的签名超宽 + 文件末尾缺换行。
> 3. ⇒ **故意不跑 `prettier --write`**:它会把这些老行一起重排 ⇒ **几十行与本轮无关的 diff**。
>    本轮**只保证自己新增的行是 prettier 风格的**,不顺手改别人的行。
>
> ⚠️ 同理,`git diff` 里的 `warning: LF will be replaced by CRLF` 也**不是**本轮引入的问题 ——
>    那是 `text=auto` 在 Windows 上的正常提示。

---

**遗留**(明确不在本轮范围,写下来不假装解决):

| # | 遗留 | 为什么不在本轮 |
|---|---|---|
| L1 | **小程序侧没有空闲超时** | 本轮只审后台 Web(§2 是前端框架层);小程序是另一套运行时,超时的"用户还在不在"判定口径也不同 |
| L2 | **登出只吊销 access token,refresh token 仍可续命最多 720h** | 需要 per-user JTI 索引(吊销存储抽象那轮),与本轮无关 |
| L3 | **`logoutApi()` 失败时 token 没被吊销** | 网络不通时**只能**本地清理 —— 这是**刻意选择**(宁可服务端还留着 token,也不能让用户被本地踢死却没清干净凭证)。已在 `handleIdleTimeout` 注释里写明 |
| L4 | **`stores/ui.ts` / `stores/tabs.ts` 的 persist 格式与本 Store 不兼容** | 各自有主人(zustand persist 直写),本轮只补注释警告,**不动**(动它属于另一个 store 的地盘) |

---

## 2.2 ✅ Permission Store / usePermission(100%)

#### ✅ 已完成(7/9)

| # | 子项 | 证据 |
|---|---|---|
| 1 | Zustand Store(menus/permissionCodes/roleCodes) | `stores/permission.ts:41-105` |
| 2 | `buildMenuTree` 树化(兼容 parentId/parent_id) | 🔴 **M102 修正证据**:这个函数**不在 store 里** —— 2026-10-25 已挪到 `utils/resourceTree.ts:27-53`(分配权限 Drawer 也要用);之前清单写的 `stores/permission.ts:48-75` 是**过期**的。M102 补了 9 条用例(此前零覆盖) |
| 3 | `fetchAccessible` 三资源并行(menu/api/button) | `stores/permission.ts:53-86` 🔴 **M102**:以前它**自己内联**写了一遍 store,而语义相同的 `setPermissions` **零调用方** —— 两条写路径迟早分叉;现在统一走 `setPermissions`,并补了用例 |
| 4 | `has` / `hasAny` / `hasAll` / `hasRole` | `stores/permission.ts:95-104` |
| 5 | PermissionGuard | `components/auth/PermissionGuard.tsx:1-37` ✅ M102 对照组(它一直是对的) |
| 6 | RoleGuard | `components/auth/RoleGuard.tsx:1-36` |
| 7 | RowActions(行内 perm 控制) | `components/common/RowActions.tsx` 🔴 **M102**:以前用 `usePermissionStore.getState()` 读权限 —— **不订阅**,权限变了不重渲染(见 §2.2.1 B1) |

#### ⚠️ 部分完成

| # | 子项 | 当前情况 |
|---|---|---|
| 8 | 按钮级权限精细化 | **框架侧已齐**(M102 实测):`fetchAccessible` 拉 `type=button` 并并进 `permissionCodes`(有用例);前端 `PermButton` 常量 113 个**与后端 seed 的 132 个 `button.*` 码全部对得上**(无孤儿码、无缺码);消费端 `RowActions` / `AuthButton` 的 perm 过滤**现在是响应式的**。**剩下的只是业务页逐个接入** —— 那是业务线的活,框架层不写 |

#### ❌ 未完成(0 项)

| # | 子项 | 当前缺口 | 优先级 |
|---|---|---|---|
| 9 | 权限实时刷新(运营改角色 → 前端自动更新) | ✅ M22:后端 `/auth/perm-stream` SSE(Redis pub/sub)+ 前端订阅 + 收到 `perm_changed` 事件自动 `fetchAccessible()` 重拉 + 3s 兜底重连;`BasicLayout` 顶层挂载(整个登录态生命周期);(commit 5407557)🔴 **M102 修正证据**:订阅端是 `web/src/hooks/useEventStream.ts`(M72 起**取代并删除了** `usePermissionStream.ts`,清单里写的那个文件**早就不存在**);`useEventStream.ts:461` `onPermChanged` → `fetchAccessible()`。🔴 **M102 修好了这个功能最关键的一环**:以前 store 更新了但**三个消费组件不重渲染**(见 §2.2.1 B1)—— 即"实时刷新"只刷新了数据、没刷新界面 | P1 ✅ |
| 10 | ✅ **数据级权限(行级数据范围)**(2026-11-03 收编:本行"框架层无概念"已过时 —— 框架侧由 §1.13 全链路落地) | 行级数据范围 = **§1.13 的轴②**,规则模型(`pkg/scope/`:all / project / dept / dept_and_child / self / custom)+ `sys_data_scope` 存储 + 角色页「数据范围」抽屉(M157)+ GORM / 纯 SQL 双出口(M155/M156/M159)+ dyn 与业务查询接线(M161:17 业务域 / 36 调用点)。行内"记录归属"这类校验属**业务规则**,框架提供挂点(RBAC 中间件 + handler 层)+ IDOR 审计已收(2026-10-17,Batch 2) | P2 ✅ |

> **复用价值**:**高**

### 2.2.1 Permission Store / 权限层审计(2026-10-27,M102)

**起因**:§2.2 标着 89% —— 但整层权限(菜单树 / 权限码 / 角色码 / 三个消费组件 / 启动加载器)
**一条用例都没有**:`web/src/stores/__tests__/` 下只有 dict / tabs / user 三个 store 的测试,
`stores/permission.ts` **零覆盖**;`utils/resourceTree.ts`(建树函数)**零覆盖**;
`components/auth/__tests__/` 目录**根本不存在**。

> 判高危的客观标准还是"**挂在全局路径上吗**"(见 memory:框架原语零覆盖)。
> §2.2 从 M22(2026-10-19)之后就没再被读过 —— 而这期间权限层被改过好几轮。

M102 把 §2.2 的每一个 ✅ 都拆开读了一遍,读出 **4 个真问题**:

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `components/common/RowActions.tsx` `components/auth/AuthButton.tsx` `router/guards.tsx` `RoutePermissionGuard` | 这三个组件用 `usePermissionStore.getState()` 读权限 —— zustand 的 `getState()` 是**一次性快照,不建立订阅**;而同一层的 `PermissionGuard` / `RoleGuard` 用的是 `usePermission()`(**订阅**) | 权限变了(store 里已经是新的)但**组件不重渲染**:切换身份后,新身份没有的"删除"**按钮还在**,刚授予的按钮不出现;SSE 收到 `perm_changed` 同样不更新 —— **"权限实时刷新"这个功能对这三个组件从来没生效过**(store 刷新了,界面没刷新);路由级守卫不更新 = **撤掉的权限还在放行**、新身份进不去的页面照样进得去。点这些"僵尸按钮"只会换来后端一个 403,用户看到的是"按钮点了没反应" |
| B2 | `router/guards.tsx` `PermissionLoader` 的 `catch {}` | 以前是 `catch { reset(); ... }` —— **任何**失败都当"登录态失效",而且错误被 `catch {}` **整个吞掉**,日志里一个字都没有 | 网络闪断 / 后端 500 → **清 token + 跳登录页**,用户得重新输密码(一次瞬时抖动不该把人踢下线);错误还被吞掉,事后查无可查。而同一个错误在 `useEventStream` 的 `onPermChanged` 里是明确"**不清登录态**"的 —— **两处语义不一致** |
| B3 | `stores/permission.ts` `fetchAccessible` | 它**自己内联**写了一遍 store(建树 / 建 Set / 写 roleCodes 三件事各写一遍),而语义完全相同的公开方法 `setPermissions` **零调用方** | 两条写路径迟早分叉:改了其中一条忘了另一条,症状是"有的入口刷新了权限、有的入口没刷新",**而且没有任何报错**。M102 把 `fetchAccessible` 收口到 `setPermissions`,并加了一条断言"只有一条写路径"的用例(探针 ⑥ 就是拆它) |
| B4 | `types/permission.ts` `Resource.visible` | 前端声明了 `visible` 字段,Sidebar 里还有 `.filter((r) => r.visible !== false)` | 后端 `model.SysResource` **没有 `visible` 列**(`server/internal/model/user.go:114`,只有 `is_active`)→ 那句 filter **今天恒为真**,是个空过滤;而注释里"运营可以隐藏单个菜单"容易被当成**已实现的能力**。M102 把注释改成如实描述,并在 `resourceTree.test.ts` 里钉了一条"visible=false 依然出现在树里"的用例 —— 等哪天后端真加了这一列,这条会红,提醒同步改前端 |

**另外发现并确认**(**不是** bug,记下来是为了别再"顺手改坏"):

- **`roleCodes` / `permissionCodes` 取的是「所有身份」的并集** —— 这跟后端
  `authService.AccessibleResources`(`server/internal/service/auth_impl.go:1214`)**完全一致**:
  后端也是 `Where("user_id = ? AND is_active = ?")` 取该用户**全部活跃身份**的并集,
  **不看 token 里的 IdentityID**。所以前端**不能**擅自收窄成"只当前身份" ——
  那样会把用户合法拥有的按钮收掉。**这一条两边必须一直保持一致。**
- **前端 `PermButton` 的 113 个常量与后端 seed 的 132 个 `button.*` 码全部对得上**
  —— 没有孤儿码(前端有、后端没有),也没有缺码(后端有、前端没有)。这条是 M102 实测的。

**修法**(全在框架层,零业务代码):

- **B1 →** 三处统一改用 `usePermission()`(订阅 `permissionCodes` / `roleCodes`),
  和 `PermissionGuard` / `RoleGuard` 同一套机制。**对外行为零变化** —— 只是从"永远读到旧值"变成"读到当前值"。
- **B2 →** `PermissionLoader` 按错误性质分开:
  - **会话失效(401 / 40100 / 40101 / 40102)→ 立刻清登录态,不重试。**
    ⚠️ 401 这一族有**三个**业务码不是一个:后端 `errcode.go:188` 把 `40100-40199` 整个映射成 HTTP 401,
    body 里带的是具体码(40100 未登录 / 40101 token 过期 / 40102 token 无效),
    而 `api/client.ts` 的 `normalizeAxiosError` **优先给业务码** → 只认 40100 的话,
    **最常见的 token 过期(40101)会走重试分支**:白等 800ms + 白打一次请求,然后才清登录态。
  - **其他错误(网络 / 5xx)→ 退避 800ms 后重试一次**;还失败才清登录态。
  - 两条路径都 `console.error` 留痕(老代码是 `catch {}`,一个字都没有)。
  - `setLoading(false)` 挪到 `.finally()`,免得重试期间就把 loading 关了。
- **B3 →** `fetchAccessible` 末尾改成 `get().setPermissions({...})`,只有一条写路径。
- **B4 →** `Resource.visible` 的注释改成如实描述("后端没这列,今天恒为真"),并补测试钉住现状。

**顺手带掉的一个小雷**(在同一段代码里,不是上面四项之一):

- `PermissionLoader` 的 `<Spin size="large" tip={t('common.loading')} />` —— antd 5 的 `tip`
  **只在 nest / fullscreen 模式下才渲染成文案**,默认模式下它是**死代码**:
  "加载中..."从来没显示过,取而代之的是每次启动都打一条
  `[antd: Spin] tip only work in nest or fullscreen pattern.` 警告。改成自己渲染文案。

**回归网**:**4 份新用例,28 条**(此前这一层是 0 条):

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/components/auth/__tests__/permission.reactivity.test.tsx` | 5 | **B1**:`AuthButton` / `RowActions` / `RoutePermissionGuard` 撤权后必须立刻消失;**反向**(授权后出现)也盯;**对照组**:`PermissionGuard` 本来就是响应的 —— 它证明"测法本身有效" |
| `web/src/stores/__tests__/permission.store.test.ts` | 8 | **B3**:`fetchAccessible` 建树 / 并集 / roleCodes / **只走 `setPermissions` 一条写路径**;失败时不清空已有权限;`reset`;四个判定方法 |
| `web/src/utils/__tests__/resourceTree.test.ts` | 9 | `buildMenuTree`(此前零覆盖):建树 / 排序 / 父不在列表当根 / `is_active=false` 连子树一起消失 / camelCase 兼容 / **`visible` 现在不参与过滤**(现状如实记录) |
| `web/src/router/__tests__/permissionLoader.test.tsx` | 6 | **B2**:401 → 立刻清、**不重试**;40100/40101/40102 → 同样不重试;5xx → 重试一次、重试成功就**不踢登录页**;重试还失败 → 才清(共 2 次尝试);没有 token → 什么都不做;**加载文案必须真的显示出来** |

> ⚠️ **对照组是必须的**:第一版探针把**四个组件全判成"不响应"** ——
> 连本来正确的 `PermissionGuard` 也"坏了"。原因是 **React 18 在 `act()` 之外不会同步刷新 DOM**
> (store 更新根本没落地),于是测法本身有问题,不是代码有问题。
> **把 `usePermissionStore.setState(...)` 包进 `act()` 之后才测出真相。**
> 这跟 memory 里那条"可逆探针自己也会假绿"是同一类 —— **测法要先自证**。

**可逆探针 7/7 全红**(scripts/probe_m102.mjs —— 拆实现看测试会不会红;每条探针跑完**逐字节**校验还原,
还原后再全量跑一遍确认回到绿):

| # | 拆掉什么 | 变红的用例 |
|---|---|---|
| ① | `RowActions` 的 `usePermission()` → `usePermissionStore.getState()` | `RowActions:撤权后行内按钮消失` / `RowActions:授权后按钮出现(反向也要盯)` |
| ② | `AuthButton` 的 `usePermission()` → `usePermissionStore.getState()` | `AuthButton:撤权后按钮消失` |
| ③ | `RoutePermissionGuard` 的 `usePermission()` → `usePermissionStore.getState()` | `RoutePermissionGuard:撤权后跳到 /forbidden` |
| ④ | loader 的 `catch` 退回 `catch { reset(); ... }`(任何失败都当掉线 / 不重试 / 不留痕) | `网络 / 5xx → 退避后重试一次…` / `网络 / 5xx 重试一次还是失败 → 才清登录态` / `会话失效(HTTP 401)→ 立刻清登录态,不重试` |
| ⑤ | `isAuthFailure` 只认 401 / 40100(漏掉 40101 / 40102) | `会话失效(业务码 40100 / 40101 / 40102)→ 同样不重试` |
| ⑥ | `fetchAccessible` 退回内联 `set({...})`(两条写路径并存) | `**只有一条写路径**:fetchAccessible 必须经过 setPermissions` |
| ⑦ | `<Spin tip={...} />`(退回 antd 那个默认模式下不渲染的写法) | `启动加载期间要显示加载文案` |

> 7 条探针合计让 **10 条**用例变红(去重后)。
> 剩下 **18 条故意是绿的** —— 它们描述的是**没被改动的行为**(建树 / 排序 / 判定方法 /
> 重试成功后不清登录态 / 没有 token 时不请求 等)。它们是**对照组**:
> 没有它们,那 10 条可能只是"断言写得太松"。

> ⚠️ **探针自己也踩过一个坑(已修)**:探针脚本第一次跑,④ 号探针直接报
> "**探针没打中**" —— 因为本仓的文件是 **CRLF** 行尾,而正则里写的是 `\n`。
> **"探针没打中"和"测试没红"是两回事**:前者是探针坏了(必须抛错,不能判 GREEN),
> 后者才是真结论。改成 `\r?\n` 后 7 条全红。

> ⚠️ **一处诚实的边界**:`PermissionLoader` 的"重试一次"只覆盖**启动路径**这一处。
> 页面运行期间的请求失败由 `api/client.ts` 的重试链管(M30 / M101),
> 两条链**各管各的**,M102 没有把它们合并 —— 那属于 §2.3 的范围。
>
> 另外:**M102 没有碰后端**。前端权限层的对外契约(权限码取并集 / 按钮级 perm 串 /
> 角色码)**全部不变**;变的只有"这些契约**有没有真的生效**"。

**§2.2 项数不变(9 项,仍 89%)** —— M102 是对 #1 / #2 / #3 / #7 / #9 的**加固**,
不是新增子项(#10 数据级权限仍是 P2 未完成;#8 按钮级精细化**框架侧已齐**,剩下的是业务页逐个接入)。

> **复用价值**:**高** —— 权限层是任何后台的公共件,"权限变了界面要跟着变"这条链以前是**断的**

---

## 2.3 ✅ Axios Client / HTTP(100%)

#### ✅ 已完成(13/13)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | 拦截器 5 道 | `api/client.ts:1-228` 🔴 **M101 重写错误拦截器**:以前是"重试失败 → `catch {}` 吞掉新错误 → 继续用第一次的 status",现在改成"内层只判定、外层统一提示" —— 见 §2.3.1 B2 | 228 |
| 2 | X-Request-Id 注入 | `api/client.ts:89-91` 🔴 **M101 改成大小写不敏感**:以前用 `headers['X-Request-Id']` 精确匹配,调用方写小写 `x-request-id` 会被**顶掉** —— 见 §2.3.1 B6 | - |
| 3 | Authorization 注入(双后端读) | `api/client.ts:93-98` | - |
| 4 | Idempotency-Key 注入(写操作) | `api/client.ts:100-107` 🔴 **M101 同 B6**:顶掉调用方的 key = **防重复提交失效** | - |
| 5 | 业务码 / 401 / 403 / 5xx 区分 | `api/client.ts:160-180`(成功)+ `181-322`(失败)🔴 **M101**:失败那条路以前**不看 silent**、且重试时弹两次 —— 见 §2.3.1 B1/B2 | - |
| 6 | silent option(自定义错误处理) | `api/client.ts:217` 🔴 **M101**:以前 `silent` **只在业务码那条路生效**,HTTP 403/5xx 照弹 —— 见 §2.3.1 B1 | - |
| 7 | X-Client / X-Client-Version 标识 | `api/client.ts:114-115` | - |
| 8 | 请求重试(网络抖动自动 retry 1 次) | ✅ M30:`utils/retry.ts` 网络错误/5xx/408/429 自动重试 3 次,full jitter,GET/HEAD 默认,写操作默认不重(M30)🔴 **M101 加固**:重试走 `instance.request(config)` → **完整重跑一遍拦截器链**,于是①内层也弹一次 toast(**一次失败弹两个红条**)②内层抛的**新错误**被外层 `catch {}` 整个吞掉 → 报出去的是**第一次**失败的码 —— 见 §2.3.1 B2 | P1 ✅ |
| 9 | **请求进度回调(上传 / 下载)** ✅ M172(2026-11-02) | `api/progress.ts`(`HttpProgress` + 纯函数 `normalizeProgress`)+ `api/client.ts` `RequestOptions.onUploadProgress / onDownloadProgress`(一等选项,五个 http 方法全部接上)。①**归一化**:axios 的 `total` 在无 Content-Length 时是 undefined(chunked / 分片第二块起),统一成 `total: null / percent: null`(调用方不用再自己判);percent 0-100 整数不破百。②**以前是假可用**:原生 onUploadProgress 靠 `extends AxiosRequestConfig` 透传,类型上存在但没人知道能用(全仓只有 ImageUploader 绕道裸 XHR)—— 现在文档化 + 归一化包装(`toAxiosConfig`,快路径零污染:没配回调的请求 config 里不出现进度键)。③⚠️ M30 重试会重发请求 ⇒ 进度从头再报(已写进类型注释)。④**测试 6 条**(纯函数 3 + 端到端 2 + 快路径 1);**可逆探针实测转红→复原→绿**(拆掉归一化包装 ⇒ 端到端那条红 —— 回调拿到的是裸事件)。 | - |
| 10 | **请求取消 AbortController** ✅M11 | `api/client.ts:178-225` `RequestOptions.signal?: AbortSignal` + `http.withSignal(signal)` 便捷工厂;`hooks/useRequest.ts:1-110` 内部自动管理 AbortController,deps 变化/组件卸载自动 abort 上一次 in-flight,新增 `cancel()` 显式取消(commit 9639fd5) | - |
| 11 | 多 baseURL 切换(后台/业主/物业) | ✅ M31:`api/baseURL.ts` Channel + `getBaseURL(channel)`;RequestOptions 加 `baseURL?` per-request 覆盖(M31)🔴 **M101 加固**:①per-request 覆盖那段是**死代码**(条件恒假)→ 传通道名打出的是**相对当前 origin 的相对路径**,静默 404;②`withChannel` 改的是**全局** baseURL 且中间有 await → 两个并发块**交错** → **owner 的请求打到 supplier 通道上**;③`baseURL.ts` 的注释写着 `http.withBaseURL(...)` —— 这个方法**从来不存在** —— 见 §2.3.1 B3/B4/B5 | P1 ✅ |
| 12 | 分页参数契约统一 | ✅ 2026-10-25:后端只认 `page` + `size`(`BaseHandler.GetPage`),**全仓不读 `page_size`**;而前端三处写法不一(`useTable` 写 `pageSize` / `PageRequest` 声明 `page_size` / `approval` 自己翻 `size`)→ **全仓 useTable 页面的"每页条数"选择器都是失效的**(选了 50 条,后端按默认 20 返回;页码是对的,因为 `page` 三方一致,所以特别难发现)。已修:`api/client.ts` 出口统一翻译(`normalizeQueryParams`)+ `PageRequest.size` / `PageResponse.size` 对齐后端 `PageData` + `useTable` 内部改用 `size`。顺带修好 5 处"一次拉全"实际只拿到 20 条的调用 | P1 ✅ |
| 13 | 请求竞态 / 取消语义 | ✅ M6(2026-10-25):`useTable` 以前是"谁回来谁写 state"→ 慢请求晚回会把新数据覆盖成旧数据(页面显示上一次筛选的结果);改成**请求序号**,只有最后一次发起的请求才有资格写 state(顺带治好了旧请求把新请求的 loading 关掉)。`useTable` 的 fetch 失败以前是 unhandled rejection → 现在进 `error` 状态、**不清空已有数据**。`api/client.ts` 里被 abort 的请求以前会落到最后的 else 分支弹 `message.error("canceled")` → 现在 `isCanceled()` 前置拦截、静默 reject(错误对象带 `canceled: true`)。`useRequest` 的 AbortController 曾是**假的**(signal 从没传给 service,abort 只让 hook 忽略结果、网络请求照跑到底,而文档却写着"已自动集成")→ 现在 signal 作为 service 的最后一个参数传出来 | P1 ✅ |

#### ❌ 未完成(0 项 —— 全部已收)

| # | 子项 | 当前缺口 | 优先级 |
|---|---|---|---|

### 2.3.1 Axios / HTTP 链路审计(2026-10-26,M101)

**起因**:§2.3 标着 92% —— 但 #1 / #2 / #4 / #5 / #6 / #8 / #11 这七项标着 ✅ 的,
**没有一条有过自己的用例**:整个 `web/src/api/__tests__/` 里只有
`client.test.ts`(只测了分页参数归一化)和 `client.refresh401.test.ts`(只测 401 分工)。
**HTTP 层自己是"零覆盖"的那一层** —— 而它是全站所有请求的唯一入口。

> 判高危的客观标准还是"**挂在全局路径上吗**"(见 memory:框架原语零覆盖)。
> §2.3 从 M30 / M31 之后就没再被读过。

M101 把 §2.3 的每一个 ✅ 都拆开读了一遍,读出 **6 个真 bug**:

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `api/client.ts` 错误拦截器 | `silent` **只在成功拦截器(业务码那条路)被读**,错误拦截器(HTTP 4xx/5xx)从头到尾**没看过它** | 后端把业务码 40300 映射成 **HTTP 403**(`server/internal/pkg/errcode/errcode.go:190`)→ 走的就是错误拦截器 → **`silent` 形同虚设**。活着的受害者:`api/inbox.ts` 的 `unreadCount` —— 它挂在 Header 上、**每个登录用户**都会打,注释里明写"没有 inbox:read 的角色拿到的是 403,不静默的话,页面上每刷新一次就弹一个红色的'无权限'";那个红条是真的会弹 |
| B2 | `api/client.ts` 错误拦截器 + `utils/retry.ts` | 重试走的是 `instance.request(config)` → **完整重跑一遍拦截器链**;①内层的错误拦截器**自己也弹一次 toast**;②内层抛的**新错误**被外层的 `catch {}` **整个吞掉**,改用**第一次**失败的 status 继续走 | ①一次失败弹**两个**红条;②调用方拿到的 `error.code` 是**过期的**(实测:第一次 500、重试后 400,报出去的是 `50001`,真实原因是 `40001`)—— 页面按错误的码做分支处理,排查时会被带偏 |
| B3 | `api/client.ts` 请求拦截器 | per-request baseURL 覆盖那段的条件是 `override !== config.baseURL`,而 `override` **就是** `config.baseURL`(axios 已经把 per-request 的 baseURL 合并进 config 了)→ 条件**永远为假**,整段**从来没执行过** | 传 `baseURL: 'owner'` 时请求打到 `"owner/system/users"` —— 那是**相对当前页面 origin 的相对路径**,不是 `/owner/v1/system/users`(实测 `instance.getUri()` = `"owner/system/users"`)。**静默 404,零报错** —— 多通道(M31)标着 P1 ✅,实际上一调就错 |
| B4 | `api/client.ts` `withChannel` | 改的是**全局** `instance.defaults.baseURL`,而且是「改 → `await fn()` → 还原」,中间那个 await 让出执行权 | 两个并发的 `withChannel` 块**交错** → **owner 的请求打到 supplier 通道上**(实测:`ownerBase` 拿到的是 `/supplier/v1`);而且还原顺序也乱 → **全局默认通道被永久留在错误的通道上**(实测:块结束后 baseURL 停在 `/owner/v1`)。owner 的 token 去打 supplier 的接口,轻则 404,重则**跨端数据串了** |
| B5 | `api/baseURL.ts` 文件头注释 | 用法第 3 条写的是 `http.withBaseURL('owner', () => ...)` | 这个方法**从来就不存在**(导出名是 `withChannel`,而且是独立导出、不挂在 `http` 对象上);第 1 条的 `http.setDefaultChannel` 同样不存在。照着注释写的人会拿到 undefined —— 而 §2.3 #11 标着 P1 ✅ |
| B6 | `api/client.ts` 请求拦截器 | 查调用方有没有设过头用的是 `config.headers['X-Request-Id']` —— **精确匹配**;而 axios 的 `AxiosHeaders` **查找**是大小写不敏感的(`utils.findKey` 两边都 lowerCase),**存储**却保留调用方写的大小写 | 调用方写小写 `x-request-id` / `idempotency-key`(HTTP 头本来就大小写不敏感)→ 被当成"没设过"→ **重新生成 → 顶掉调用方的值**。对 X-Request-Id 只是关联 id 换了;对 **Idempotency-Key 是「防重复提交失效」** —— 后端按 key 去重,key 换了就等于没去重 |

**修法**(全在框架层,零业务代码):

- **B1 →** 错误拦截器读 `error.config.silent`,并把**所有** HTTP 层提示(`403 / 404 / 5xx / 默认 / 网络错误`)统一收口到 `notifyHttpError` / `notifyNetworkError`,
  由 `silent` 统一开关。**401 不受 silent 影响** —— 会话失效不是页面自己的事,必须照常登出。
  业务码那条路(成功拦截器)的行为**一个字没动**。
- **B2 →** 错误拦截器重写成「**内层只做判定、不做提示;外层做全部**」:
  - `utils/retry.ts` 在重发前给 config 打 `_isRetryAttempt = true`;
  - 带这个标的帧**不弹提示、不规范化**,把**原始 AxiosError** 原样抛给最外层;
  - 最外层 `catch` 到 axios 错误时**改用它**(而不是第一次那个),于是提示**只弹一次**、且用的是**最后一次**失败的码。
  - 401 的两条分支(刷新重发 / refresh 自身 401)语义**保持原样**,只是把内联的错误对象换成 `normalizeAxiosError(err)`(字段形状不变)。
- **B3 →** 请求拦截器里按 `isChannelName(v)` 区分:通道名 → `getBaseURL(channel)`;以 `/` 开头(路径)或带 scheme(完整 URL)→ **原样用**。
- **B4 →** `withChannel` 加一把**互斥锁**:同一时刻只允许一个块,后到的排队等前一个跑完(含它的 finally 还原)。
  ⚠️ **残留风险(改不掉的,已在代码注释里写明)**:块**外面**发起的请求,只要恰好落在块执行的那段时间里,照样会拿到块内的默认通道 ——
  因为通道是全局的。要彻底避免只有两条路:① 请求一律显式传 `baseURL`;② 不传 baseURL 的请求统一走一个固定通道。
  **单请求覆盖(B3 那条)没有这个问题**,能用就用它。
  另外,M101 顺手把 `withChannel` / `setDefaultChannel` / `getDefaultChannel` / `listChannels`
  **零调用方**这件事记进了清单 —— M31 那套多通道机制**至今没有任何业务页在用**。
- **B5 →** 注释改成真实存在的导出名(`setDefaultChannel` / `withChannel`),并注明它们是**独立导出、不在 `http` 对象上**。
- **B6 →** 新增 `getHeader(headers, name)`:优先走 `AxiosHeaders.get()`(内部就是大小写不敏感的 `findKey`),
  退化路径用 `Object.keys().find(k => k.toLowerCase() === name.toLowerCase())`。

**回归网**:`web/src/api/__tests__/client.m101.test.ts` —— **20 条用例**,每条对应上表一个结论:

- B1:`silent:true` + HTTP 403 → 不弹 toast(但仍然 reject);**对照组**:不带 silent → 弹 1 次(没有这条,上面那条就可能只是"永远不弹");
  `silent:true` + HTTP 500 → 不弹;业务码那条路(HTTP 200 + code ≠ 0)不受影响;**silent 不抑制 401**(会话照常失效)。
- B2:一次失败**只弹 1 个** toast;reject 出去的是**最后一次**的码(`40001` 而不是 `50001`);403 不弹第二次;
  重试链里内层的 401 不会被外层再弹一次;重试链最多发 4 次(1 + 3)。
- B3:`baseURL: 'owner'` → `/owner/v1`;完整 URL 与路径原样用。
- B4:两个并发块各自看到自己的通道;块结束后默认通道还原。
- B6:小写 / 大写 `X-Request-Id` 都保留;小写 `idempotency-key` 保留;没设过才自动生成。

**可逆探针 7/7 全红**(scripts/probe_m101.mjs —— 拆实现看测试会不会红;每条探针跑完**逐字节**校验还原,
还原后再全量跑一遍确认回到绿):

| # | 拆掉什么 | 变红的用例 |
|---|---|---|
| ① | `silent` 在错误拦截器里的读取(`const silent = false`) | `silent:true + HTTP 403 → 不弹 toast` / `silent:true + HTTP 500 → 不弹 toast` |
| ② | **整套"内层只判定"机制**(retry.ts 的标记 + 内层提前返回 + 外层透传,三处一起) | `一次失败只弹一个 toast` / `reject 出去的是最后一次的码` |
| ②b | 外层对**已规范化**错误的原样透传 | `重试链里内层的 401 不会被外层再弹一次错误提示` |
| ③ | 外层改用最后一次的错误(`err = e` → `err = error`) | `reject 出去的是最后一次的码` |
| ④ | per-request baseURL 的通道名解析 | `通道名 'owner' → 该通道的 baseURL` |
| ⑤ | `withChannel` 的互斥锁 | `两个并发 withChannel 块各自看到自己的通道` |
| ⑥ | `getHeader` 的大小写不敏感查找(退回精确匹配) | `小写 'x-request-id' 不会被顶掉` / `小写 'idempotency-key' 不会被顶掉` |

> 7 条探针合计让 **9 条**用例变红(去重后;② 让 3 条红、其中一条与 ②b 的那条重叠)。
> 剩下 **11 条故意是绿的** —— 它们描述的是**没被改动的行为**(不带 silent 时的 403、
> 业务码那条路、401 的处理、重试次数上限、完整 URL / 路径原样用、大写头名、自动生成头)。
> 它们是**对照组**:没有它们,那 9 条可能只是"断言写得太松"。

> ⚠️ **探针 ② 一开始报 GREEN(BAD) —— 这是"探针会打偏"的第二个实例**:
> "只弹一次 toast"这个修复有**两道独立的闸**:
>   (a) 内层带 `_isRetryAttempt` → 不提示、不规范化,原样抛给外层;
>   (b) 外层 catch 到**已规范化**的对象 → 原样透传,不再提示。
> 只拆 (a):内层照样提示,但它抛的是**已规范化**的对象 → 外层走 (b) 透传 → **还是只弹一次**;
> 只拆 (b):内层带标记 → 不提示 → 外层拿到的是原始 AxiosError → **也只弹一次**。
> **两条都得拆,才等于"改动前没有这套机制"。**
> 这条教训跟 M100 的"拆实现要拆**整条链**,不是拆一半"是同一条,只是这次那道链在**两个文件**里
> (标记在 `utils/retry.ts`、判定在 `api/client.ts`)。探针 ② 因此是**三处一起改**。

> ⚠️ **一处诚实的边界**:`withChannel` 的互斥锁**只保证块与块之间不交错**,
> 挡不住"块外面恰好在块执行期间发起的请求"拿到块内的通道(见上面 B4 的残留风险)。
> 这是**全局通道**这个设计本身的天花板,不是锁能解决的;真正的解法是全面改用
> per-request `baseURL`(B3 那条)。M101 只把能挡的挡了,没假装全解决。
>
> 另外:**M101 没有碰后端**。前端 HTTP 层的对外契约(请求头名、错误对象字段
> `{code, message, requestId, data}`、重试次数与退避参数、通道名的取法)**全部不变**;
> 变的只有"这些契约**有没有真的生效**"。

**§2.3 项数不变(13 项,仍 92%)** —— M101 是对 #1 / #2 / #4 / #5 / #6 / #8 / #11 的**加固**,
不是新增子项(#9 请求进度回调仍是 P2 未完成)。

> **复用价值**:**高** —— 2026-10-19 之后 #10 取消能力也齐了,业务层零成本拿到

---

### 2.3.2 前端 utils 层零覆盖审计(2026-10-29,M110)

**起因**:§2.3.1(M101)把 `api/client.ts` 的**拦截器链**拆开读了一遍,但它没往下走一层 ——
`api/client.ts` 自己只是一层壳,活都甩给 `web/src/utils/` 下的几个文件。
这一层此前**一条网都没有**:

| 文件 | 挂在哪 | M110 之前的状态 |
|---|---|---|
| `utils/crypto.ts` | **§2.1 的登录密码链** | **零覆盖**;而且**整份清单里从来没提过这个文件**(连它存在都没记) |
| `utils/format.ts` | §2.5 / §2.6 的展示链 + §2.7 `SensitiveField` | **零覆盖** |
| `utils/retry.ts` | **§2.3 #8 重试**(全站每个请求的失败路径) | **零覆盖**(M101 只间接碰过它的标记字段) |
| `utils/request.ts` | **§2.3 #2 / #4 的 request_id 与幂等键** | **零覆盖** |
| `api/client.ts` 的取消分支 | §2.3 #10 / #13 取消语义 | M101 补了网,但**没覆盖 `isCanceled` 与 retry 的取消语义** |
| `utils/sentry.ts`(顺手补的) | §2.5 #5 错误上报的**噪声过滤** | M109 审过,但它的 `beforeSend` **只过滤 AbortError** —— 认不出框架自己的取消形状(B7) |

> 判高危的客观标准还是"**挂在全局路径上吗**"(见 memory:框架原语零覆盖)。
> 这 5 个文件里 **4 个**挂在全局路径上:`crypto.ts` 决定**能不能登录**,
> `retry.ts` 决定**每个请求失败怎么走**,`request.ts` 决定**日志能不能秒查**。

**七个真 bug 一览**

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `utils/crypto.ts` 的降级路径 | `crypto.subtle` **只在安全上下文可用**(MDN:SubtleCrypto — Secure context),而 vite dev server 配的是 `server.host: '0.0.0.0'` ⇒ 同事从 `http://<局域网IP>:5173` 打开就是**非安全上下文** ⇒ `crypto.subtle` 是 `undefined` ⇒ 降级到自制的 `fallbackHash()` —— **那不是 SHA-256**(自造的 64 位级哈希,输出也不是 64 位 hex) | 后端拿它去 bcrypt 比对**必然失败** ⇒ **登录永远报"账号或密码错误"**,而且错误信息完全指不到真因(前端以为自己摘要了,后端看到的是一串垃圾)。⚠️ 本仓 vite dev 的默认访问姿势**就是**局域网 IP —— 这不是"极端场景",是**同事的日常** |
| B2 | `utils/format.ts` 的 `formatDate.relative` | 写的是 `dayjs(v).fromNow()`,而全仓 `grep dayjs.extend` = **0 处** —— `relativeTime` 插件**从来没装过** ⇒ `fromNow` 这个方法根本不存在 | 一调就炸:`TypeError: dayjs(...).fromNow is not a function`。活着的调用方:`components/layout/InboxBell.tsx`(站内信时间)。⚠️ tsc 早就报了这条(`src/utils/format.ts(55,59) TS2339`),但它混在几十条基线错误里没人看 —— **tsc 报了 ≠ 有人修了** |
| B3 | `utils/format.ts` 的掩码 | 固定切法(手机号 3+4 / 身份证 6+4 / 银行卡 4+4)遇到**比标准长度短**的输入时,中段被 `slice` 吃光 ⇒ **一个字符都藏不住**:`mask.phone('1234567')` → `'123****4567'` —— 看着打了码,原值一字不差 | 活着的调用方:`components/common/SensitiveField.tsx`(脱敏 + 眼睛切换)⇒ **脱敏形同虚设**,而且越短的值越"假" |
| B4 | `utils/retry.ts` 的退避被打断 | `sleep()` 抛的是 `DOMException('Aborted','AbortError')` —— DOMException **有** `code` 属性(= 20)⇒ `client.ts` 的 `isNormalizedError()` 判它是"已规范化" ⇒ 原样 reject ⇒ 调用方拿到 `{ code: 20, name: 'AbortError' }` | 与 client 自己的 `{ code: 'CANCELED', canceled: true }` **不同形** ⇒ 同一个"取消"有**两副面孔** ⇒ 业务层判不出"这是取消,不是失败" ⇒ 被取消的请求被当成失败上报(错误噪音 / 弹红条) |
| B5 | `api/client.ts` 的 `isCanceled()` | 它只认 `ERR_CANCELED` / `CanceledError` / `__CANCEL__` 三种,而**本文件自己的取消分支** reject 的是 `{ code: 'CANCELED', canceled: true }` ⇒ **认不出自己规范化出来的取消对象** | `isCanceled` 是**导出给业务层用**的判断函数 ⇒ 业务层拿它判框架的取消**恒 false** ⇒ 被取消的请求被当成真失败 |
| B7 | `utils/sentry.ts` 的噪声过滤 | `beforeSend` 只滤 `AbortError` / 值里含 `'aborted'` —— 而**框架自己的取消对象** `{ code: 'CANCELED', message: 'canceled', canceled: true }`(真源 `utils/retry.ts` 的 `canceledError()`)**两个特征都不占** ⇒ 认不出它 | 用户主动取消的请求**仍会被当成"错误"上报到 Sentry**。⚠️ 这条**一半是 M110 自己造成的**:M110 把退避被打断那条路径也统一成了 `CANCELED` 形状(此前是 DOMException)⇒ 缺口从一条路径扩到两条。⚠️ 另一条 axios-abort 路径在 HEAD 就是这个形状 ⇒ 那是**先于 M110 就存在的缺口** | 
| B6 | `utils/request.ts` 的降级路径 | `crypto.randomUUID` **同样只在安全上下文存在** ⇒ HEAD 的降级是 `Date.now().toString(36) + '-' + Math.random().toString(36).slice(2,14)` ⇒ `Math.random` **不是密码学安全的随机源** | 这个 ID 同时是 **Idempotency-Key**(`api/client.ts` 自动给写操作注入)⇒ 而后端 `idempotentx` 的 key 空间是**全局**的(`resultKey(idemKey)` / `lockKey(idemKey)` 直接由原始 key 派生,**不按用户隔离**)⇒ 键一旦撞上/被猜中,后端会把**别人的响应体**原样返给后到的那个人(不是"挡掉",是"串号")。⚠️ 诚实的边界:这**不是**"后端会丢弃不合规 id" —— HEAD 那条路径的字符集/长度恰好合规(≤64 且落在 `[A-Za-z0-9._:-]`),后端 sanitizer 收它;问题在**随机性**和**全局命名空间**这两头 |

**修法**(框架层,零业务代码):

| # | 修法 | 落点 |
|---|---|---|
| B1 | 降级路径改成**纯 JS 的真 SHA-256**(`sha256Pure`),与 WebCrypto **逐字节相同** | `utils/crypto.ts`;回归网用 **NIST 标准向量** + 与 `crypto.subtle` 逐输入对拍钉死。**前后端契约不变**(仍是 `sha256("rapid-admin::" + 明文)` = 64 位小写 hex)⇒ 老用户不用改密码 |
| B2 | 改用 `Intl.RelativeTimeFormat`(浏览器原生、locale 感知、零依赖) | `utils/format.ts`;**没去装 `dayjs.extend(relativeTime)`** —— 那个插件是 CJS `export =`,而本仓 tsconfig 既没 `esModuleInterop` 也没 `allowSyntheticDefaultImports` ⇒ 默认导入过不了类型检查 ⇒ 为了一个函数改全仓 tsconfig 不划算 |
| B3 | 掩码改成**按比例缩放**:标准切法只在"藏得住 ≥ 1/3"时用,否则 head/tail 各缩到 `floor(len/4)` | `utils/format.ts` 的 `maskMiddle()`;**标准长度的输出逐字节不变**(`138****1234` / `110101********1234` / `6222 **** **** 7890`) |
| B4 / B5 | 取消形状的**真源**收成一处:`utils/retry.ts` 的 `canceledError()`;`client.ts` 引用它(反过来会让两个文件**循环依赖**);`isCanceled()` 补上 `code === 'CANCELED'` / `canceled === true` 两路 | `utils/retry.ts` + `api/client.ts` |
| B7 | `beforeSend` 补一档"框架自己的取消":认 `exc.value` 是对象时 `code === 'CANCELED'`、认字符串化形态里的 `'"CANCELED"'`(**带引号**,免得误伤正文里恰好写着 CANCELED 的错误消息)、认 `exc.type === 'CanceledError'` | `utils/sentry.ts`;**这一档是框架内部的形状**(不是业务错误码)⇒ 不违反 M109 遗留里"业务码白名单归业务层"的边界 |
| B6 | 降级改用 `crypto.getRandomValues`(**所有上下文都有**,不受安全上下文限制),按 RFC 4122 手工置 version(4)/variant 位拼 UUID v4 | `utils/request.ts`;顺带让**不管从哪个地址打开,request_id 的形状一致**(日志里不再有两种格式) |

**顺手修的 5 条 tsc 错误(类型层,运行时是对的)**

`api/client.ts` 的 5 个方法原来写成 `instance.get<unknown, ApiResponse<T>>(…)`。
axios 的签名是 `get<T = any, R = AxiosResponse<T>, D = any>(…): Promise<R>` ——
**第二个泛型 `R` 把 `AxiosResponse` 顶掉了** ⇒ 返回类型成了 `Promise<ApiResponse<T>>`
⇒ `r` 是 `ApiResponse<T>` ⇒ `r.data` 是 `T` ⇒ 再取 `.data` 就报
"Property 'data' does not exist on type 'T'"。
运行时其实是对的(axios 返回的确实是 `AxiosResponse`),**只是类型写错了** ——
编译器因此没法校验 `r.data.data` 这一层,改错一个字段名没人会发现。
改成只传第一个泛型:`R` 回落成 `AxiosResponse<ApiResponse<T>>` ⇒ 类型对得上。

**回归网**:**5 份新用例文件,79 条 + 1 份老文件(`sentry.test.ts`)补 1 条 = 80 条新增**:

| 文件 | 条数 | 锁住什么 |
|---|---|---|
| `utils/__tests__/crypto.test.ts` | 10 | NIST 标准向量 + 与 Node `createHash('sha256')` 对拍 + 填充边界(55/56/57/63/64/65/119/120/121 字节)+ **降级路径 === WebCrypto 路径** + 前后端契约 |
| `utils/__tests__/format.test.ts` | 30 | 相对时间(过去/将来/单位选择/locale/en-US/"现在")+ 绝对时间 + 数字格式化 + **掩码(标准长度 + "短输入也必须真的藏住东西")** + truncate |
| `utils/__tests__/retry.test.ts` | 19 | 重试策略(哪些该重试、哪些绝对不能重试)+ `retryMax` / `retry:false` / `retry:true` / 写操作默认不重 + **退避被打断 ⇒ 对外形状** |
| `utils/__tests__/request.test.ts` | 10 | UUID v4 形状 + 1000 次不重 + **三条降级路径都被后端 `sanitizeRequestID` 收下**(规则照抄 Go 那份)+ getRandomValues 路径**不碰** `Math.random` |
| `api/__tests__/client.m110.test.ts` | 10 | `isCanceled` 六种判法 + axios abort ⇒ 对外形状 + **被取消不弹 toast** + 退避被打断 ⇒ 形状与上面**完全一致** + 5 个 http 方法的解包契约 |
| `utils/__tests__/sentry.test.ts`(M109 的老文件,+1 条) | 1 | `beforeSend` 滤掉框架自己的取消形状(对象形态 / 字符串化形态 / `CanceledError` 三种)+ **正文里恰好写着 CANCELED 的普通错误不能被误伤** |

**可逆探针**:scripts/probe_m110.mjs,**8 条探针,实测全部红在指定用例上,还原后全绿**:

| # | 拆掉什么 | 变红的用例 |
|---|---|---|
| ① | `sha256()` 的降级路径退回自制的 `fallbackHash` | `降级路径` |
| ② | 降级路径干脆不存在(没有 `subtle` 就抛) | `降级路径` |
| ③ | `formatDate.relative` 退回 `dayjs(v).fromNow()` | `formatDate.relative` |
| ④ | 掩码退回固定切法 | `短输入也必须真的藏住东西` |
| ⑤ | 退避被打断退回抛 `DOMException` | `不是 DOMException` / `退避被打断` |
| ⑥ | `isCanceled` 退回三路判断 | `认得出框架自己的取消对象` |
| ⑦ | `request_id` 的降级退回 `Math.random` | `getRandomValues` |
| ⑧ | `beforeSend` 退回只滤 AbortError(删掉框架自己的取消那一档) | `框架自己的取消形状` |

> ⚠️ **探针 ① ② 的写法有个坑**(踩过):往源文件**中间**插一段代码要写"定位到某个注释块"的位置正则,
> 而那个正则里的 `\*\/` 一旦写歪,正则字面量会一路吞到下一个 `/` —— **把源文件改成语法错误**,
> 探针"打偏"比"打不中"更难查。所以脚本改成:**从 `git show HEAD:…` 里把 `fallbackHash` 的原文照抄下来追加到文件末尾**,
> 并给 `runTest()` 加了一条"变换失败 ⇒ 判定为无效,绝不判 GREEN"的分支。
> 脚本里还有一条老规矩:**0 个用例 = 直接抛错**(Windows 上 `execFileSync('npx', …)` 没有 `shell: true` 会 ENOENT,
> 异常被 catch 吞掉 → 输出 0 个用例 → 报成"绿")。

**验证**(2026-10-29):

| 门禁 | 结果 |
|---|---|
| `npx tsc --noEmit` | **42 个错误 = 基线不变**,本轮碰过的文件 **0 个** |
| `npx eslint`(本轮 12 个文件) | **7 个错误 = HEAD 就有的老问题**(client.ts 的 2 条 `consistent-type-imports` 在 import 行、format.ts 的 5 条 `eqeqeq` 在 `formatNumber` 里),本轮**新增 0 个** |
| `npx prettier --check`(本轮 5 个**新**文件) | **全合规** |
| `npx prettier --check`(本轮改过的 5 个源文件,先归一化成 LF) | `crypto.ts` / `format.ts` / `request.ts` **合规**;`retry.ts` / `client.ts` **不合规 —— 但全是 HEAD 就有的老问题**(把两边都归一化后逐行比对:差异**只有**本轮那些有意改动) |
| `npx vitest run`(全量) | **69 个文件 / 550 条全绿**(470 基线 + 80 新增) |
| scripts/probe_m110.mjs | **8/8 红在指定用例上,还原后全绿** |
| `scripts/check_backlog_consistency.py` | ✅ 自洽 |

> ⚠️ **prettier 那条还是那个 Windows 坑**(同 §2.1.1 / §2.3.1 的说明):
> 本仓 `.gitattributes` 是 `* text=auto` + `core.autocrlf=true` ⇒ **提交进仓的是 LF**,
> 但 Windows 工作区是 CRLF,而 prettier 配的是 `endOfLine: "lf"` ⇒
> **在 Windows 上直接 `prettier --check` 这些文件一律报"不合规"** —— 哪怕它一行都没改。**这个信号是假的。**
> 有意义的检查是**先把 CRLF 归一化成 LF 再查**,而且要比**归一化后的 HEAD** 才算数。
> ⇒ **故意不跑 `prettier --write`**:它会把 HEAD 就有的老行一起重排 ⇒ 几十行与本轮无关的 diff。

**遗留**(明确不在本轮范围,写下来不假装解决):

| # | 遗留 | 为什么不在本轮 |
|---|---|---|
| L1 | **前端摘要 ≠ 传输加密** | 非安全上下文里密码本来就在链路上明文传,这一层摘要只是不让它进 nginx access log / 浏览器历史。要防中间人必须上 HTTPS —— 那是部署形态的事 |
| L2 | **后端幂等 key 空间是全局的**(不按用户隔离) | 属**后端** `idempotentx` 的设计;本轮只保证前端生成的键**随机且形状统一**,不越界改后端 |
| L3 | **`dayjs` 的 `relativeTime` 插件仍然没装** | 装它要动全仓 tsconfig(`esModuleInterop`);`Intl.RelativeTimeFormat` 已覆盖需求且零依赖 |
| L4 | **脱敏的"藏多少"仍是前端约定** | 真正的脱敏应在后端(返回时就该是掩码后的值);前端这层是 UI 兜底,**不能当安全边界** |
| L6 | **脱敏没有 lint 兜底** | 技术方案 §9.3.4 原先写着"ESLint 规则禁止直接把 `phone/idCard/realName/bankCard` 绑定到文本节点" —— **那条规则并不存在**(`web/.eslintrc.cjs` 的 `no-restricted-syntax` 管的是硬编码字典枚举)。已把技术方案订正为"约定 + 回归网",**lint 规则本身留给后续** |
| L5 | **`utils/` 下其余文件**(`storage.ts` M108 / `tokenScheduler.ts` / `tokenSync.ts` / `sentry.ts` M109 已审) | 本轮只审这 5 个;其余按 M108 / M109 的结论 |

**§2.3 项数不变(13 项,仍 92%)** —— M110 是对 #2 / #4 / #8 / #10 / #13 的**加固**,不新增子项。

> **复用价值**:**高** —— 密码摘要 / 取消语义 / 脱敏 / 请求原语是**任何**前端项目都要重写一遍的东西

---

## 2.4 ✅ Dict / Config Store + hook(100%)

#### ✅ 已完成(7 个)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | DictStore(30 分钟缓存) | `stores/dict.ts:1-107` | 107 |
| 2 | ConfigStore(10 分钟缓存) | `stores/config.ts:1-74` | 74 |
| 3 | useDict(groupCode)(options + getLabel + getColor) | `hooks/useDict.ts:1-83` | 83 |
| 4 | useConfig / useConfigValue | `hooks/useConfig.ts:1-33` | 33 |
| 5 | parseColor(description 解析) | `hooks/useDict.ts:38-47` | - |
| 6 | DictGroupCode 联合类型(29 组) | `types/dict.ts:1-77` | 77 |
| 7 | **字典缓存作废**(`invalidate` + SSE `dict.changed` 接线)✅M94 | `stores/dict.ts` `invalidate()` + `hooks/useEventStream.ts` `EVENT.dictChanged`;服务端 `push.TopicDict` / `service/dict_publisher.go`;**只删 `lastFetchAt`、保留 `dicts`**(删了下拉会瞬间空) | - |

> **复用价值**:**高** —— 业务页全靠它,✅ 完整
>
> ⚠️ **第 7 项(M94)不是"多一个按钮",是给 30 分钟缓存开了个后门** ——
>   字典改了以后,别的页面最长 30 分钟看不到;现在服务端广播 `dict.changed`,
>   前端收到就作废该组缓存。
>   ⚠️ **它依赖 `useDict` 的 effect 条件 + `fetchDict` 的 TTL 判断两处实现细节** ——
>   改那两处时这里必须一起改,否则"作废"会退化成"静默不生效"(症状:字典改了但下拉还是旧值,
>   **且没有任何报错**)。回归网:`web/src/stores/__tests__/dict.store.test.ts`。

---

### 2.4.1 字典 / 平台配置链路审计(2026-10-28,M107)

**起因**:§2.4 标着 **100%**,但这一层**几乎一条网都没有**:

- `hooks/useDict.ts` —— **11 个业务页的下拉都靠它** —— **零覆盖**;
- `hooks/useConfig.ts` + `stores/config.ts` —— **零覆盖**;
- `api/config.ts` 的 `dictApi` —— **零覆盖**;
- `pages/system/dict/index.tsx` —— 整页**零覆盖**(M94 只给 store 补了 8 条)。

而核出来的东西里,有**一条是规范早就承诺过、代码从来没做**的。

---

**P1 机理:「临时下线某个字典项」是个**死开关****

`docs/数据字典管理规范.md` 规则 2 写着:

> **正确路径**:在字典管理页点"编辑",把 `is_active` 改为 false(禁用)。
> - 前端 `useDict()` 默认只返回 `is_active=true` 的项 → 自动隐藏

核实结果(逐行读过,不是推演):

| 位置 | 过滤 `is_active` 吗 |
|---|---|
| `hooks/useDict.ts` 的 `options` | ❌ **不过滤**(每一项都 map 进 options) |
| 后端 `service/role_impl.go` 的 `ListDict` | ❌ **不过滤**(`tx.Where("group_code = ?")` 就完了) |
| 后端 `ListDictGroups`(分组列表) | ✅ 过滤(`Where("is_active = ?", true)`) |

⇒ 运营在字典管理页把一项「禁用」之后,**11 个业务页的下拉里它还在**。
**那个开关是死的 —— 而规范承诺过它该生效。**

> ⚠️ **为什么过滤放在前端而不是后端**:`GET /system/dicts` **一个端点**同时服务两条路 ——
>   `dictApi.byGroup`(业务页下拉)和 `dictApi.list`(字典管理页浏览全表)。
>   后端过滤 ⇒ 管理页**看不到禁用项** ⇒ 运营**永远恢复不了**
>   (规范规则 2 明写"想恢复直接改回 `is_active=true`")。
>   而规范那句话说的就是"**前端 `useDict()`**" ⇒ **过滤点就在 `useDict`**。
>
> ⚠️ **口径**:下线 = **不再提供新选择,既有数据照常可读** ⇒
>   `options` 过滤,`getLabel` / `getColor` / `raw` **保留禁用项** ——
>   历史记录里已经存了这个 value,表格要照常显示中文标签和颜色;
>   全局滤掉的话,老记录会变成一串裸 value("closed" 而不是"已关闭")。

---

**其余 7 条**(逐条核实):

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `hooks/useDict.ts` 的 `options` | 不过滤 `is_active` | **P1**:"临时下线"是死开关(详见上) |
| B2 | `hooks/useDict.ts` 的 `fetchDict(...).catch(() => undefined)` + `loading: !dicts` | 拉失败被吞成"还在加载" | **P1**:下拉**永远转圈**,分不清"没数据"还是"拉挂了"(同 M106 `CaptchaField` 那一类) |
| B3 | `api/config.ts` 的 `DictItem.is_active: number` | wire 上是 Go `bool` 的 true/false(`model.SysDict.IsActive bool`) | **P2**:**类型撒谎**。页面只能写 `v === 1 \|\| v === true` 兜底 —— 兜底**掩盖**了撒谎(2026-10-18 踩过同一个坑:全表 113 行显示成"禁用") |
| B4 | `api/config.ts` 的 `dictApi.byGroup` | 手拼 `?groupCode=${groupCode}`,**不编码** | **P2**:**哑弹**(现在的 30 个组 code 都是下划线,线上没炸;哪天组 code 带上 `&` / `#` 就静默查错组) |
| B5 | `hooks/useConfig.ts` 的 `useConfigValue` | 订阅 `s.getValue` —— store 里**稳定不变**的函数引用 | **P2**:zustand 用 `Object.is` 比较 selector 的返回值 ⇒ **订阅永不触发** ⇒ **组件永远不会因配置到达而重渲染** ⇒ `useConfigValue` 一直返回默认值 |
| B6 | `hooks/useConfig.ts` 的 `fetchAll().catch(() => undefined)` + `loading: !item` | 同 B2 | **P2**:永远转圈 |
| B7 | `stores/config.ts` 的 `fetchAll` | `page_size: 1000` 指望"一次拉全量",而后端 `pagination.Normalize()` 把 `size` 截到 `MaxSize = 100` | **P2**:**静默截断** —— 超过 100 条配置项,`configMap` 少一半,**没有任何报错**(现在 17 条,看不出来) |
| B8 | `stores/config.ts` 的 `getValue` | 无脑 `JSON.parse(item.value)`,**绕过**服务端给的 `value_type` | **P2**:`value_type='string'`、值是 `"123"` → 给出**数字 123**;值是 `"true"` → 给出**布尔**。**类型真源被绕过** |

---

**修法**(框架层,零业务代码):

| # | 修法 |
|---|---|
| B1 | `useDict` 的 `options` 加 `.filter((d) => d.is_active !== false)`;`getLabel` / `getColor` / `raw` 不动。**用 `!== false` 而不是 `=== true`**:字段缺失时**保留**该项 —— 宁可多显示一个选项,也不要把整组下拉清空 |
| B2 | `useDict` 加显式 `failed` 态:失败 → `loading: false` + 暴露 `error`。**顺带修掉"组件切组后旧请求回来把状态写脏"**(effect 里用 `cancelled` 标志) |
| B3 | `DictItem.is_active` 改成 **`boolean`**;字典页删掉 `isActiveEnabled` 兜底和 `as any`,表单层(antd Select 的 option value 只收 `string \| number`)用 `toFormIsActive` / `toWireIsActive` 两个**纯函数**显式转换 |
| B4 | `byGroup` 改用 params 对象:`http.get('/system/dicts', { groupCode })`,编码交给 axios(和同文件的 `list` 走同一条出口) |
| B5 | `useConfigValue` 改成订阅 **值本身**(`s.configMap[code]`),解析抽到 `parseConfigValue`,`useConfigValue` 建在 `useConfig` 上(不重复实现) |
| B6 | 同 B2 |
| B7 | `fetchAll` **循环翻页直到拿齐**(短页 = 最后一页;`fetched >= total` 也停;`MAX_PAGES` 兜底防死循环) |
| B8 | `parseConfigValue` **按 `value_type` 解析**(`int` → Number / `bool` → true·false / `json` → JSON.parse / 其余 → 原样字符串),与后端 `model.ConfigValueType` 一一对应 |

> ⚠️ **B3 为什么值得抽成两个纯函数**:字典页整页**一条测试都没有**,转换逻辑藏在
> `onSubmit` 里,改坏了没人拦。抽出来不是为了好看,是为了**能测**。
> 而且这条边界**必须是 boolean**:后端 `UpdateDict` 走 `h.BindJSON(c, &body)` 绑到
> `model.SysDict`,Go 的 `encoding/json` 把 `1` 往 bool 字段塞会**直接报错**
> ("cannot unmarshal number into Go struct field ... of type bool")——
> 不是"当成启用",是**整条 PATCH 请求挂掉**。
>
> ⚠️ **两个纯函数放在 `web/src/pages/system/dict/isActive.ts`,不是页面组件文件里**:
> 页面文件里导出非组件会触发 `react-refresh/only-export-components` 警告,
> 而本仓 `npm run lint` 是 `--max-warnings 0`(**警告即红**)。
> 顺带的好处:用例直接 import 纯模块,不用把整个页面 + antd 拖进测试。

> ⚠️ **后端为什么一行没动**:
> - `ListDict` 不过滤是**对的** —— 字典管理页要看到禁用项才能恢复;
> - `ListDictGroups` 过滤是**对的** —— 分组列表本来就只列"还有启用项"的组
>   (注意:某个组的项**全被禁用**时,那个组会从管理页的**分组下拉**里消失,
>   但管理页默认"查全部",那一组仍然在列表里、仍然能恢复);
> ⇒ 后端两个接口**不矛盾**,矛盾的是**前端 `useDict` 跟规范承诺不一致**。

---

**零调用方的说明(主动交代,不装死)**

`stores/config.ts` + `hooks/useConfig.ts` **零调用方**。原因**不是"忘了接"**,是**接口权限**:

- `GET /platform/configs` 挂的是 `rbac("config", "list")`;
- 而 `config:*` **只绑给 super_admin**(见 memory:`config-rbac-super-admin-only`);
- ⇒ 普通业务页调它只会拿到 403 ⇒ 这套 hook 现在**结构性地**没有业务调用方。

**处置:保留 + 修好 + 测住 + 标明边界(不删)。** 理由:

1. 它是仓里**唯一**"组件里读平台配置"的入口,删了将来还要加回来;
2. 真正缺的是**面向业务的配置读接口** —— 那是**后端**的活,
   删前端 hook 解决不了(后端不给普通角色开读权限,前端怎么写都是 403);
3. 与 §1.7 `@Cacheable` 那种"收回"的区别:那个是**纯冗余**(缓存已有别的实现),
   这里**不是** —— 它是**接口权限不够**,不是**能力重复**。

⚠️ 代价要说清楚:这意味着 `useConfig` / `useConfigValue` 的正确性**只由本轮补的网保证**,
没有真实业务调用方在生产里替它兜底。**将来若有业务页接它,先确认那个页面的角色
读得到 `/platform/configs`**,否则会拿到 403 —— 而 hook 会把它变成 `error: true`(不静默)。

---

**回归网**:**5 份新用例文件 + 1 份改写,33 条前端用例**(另加 1 个新源码模块
`web/src/pages/system/dict/isActive.ts`,装 B3 的两个纯函数):

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/hooks/__tests__/useDict.test.tsx` | 7 | **B1**:禁用的项不进 options / **仍查得到标签** / **仍保留颜色** / raw 保留 / 字段缺失时保留;**B2**:拉失败 → 不再永远 loading + `error` 为 true |
| `web/src/hooks/__tests__/useConfig.test.tsx` | 7 | **B5**:配置到达后值从默认值变成真实值(旧实现永远停在默认值);**B6**:拉失败 → 不再永远 loading;**B8**:按 `value_type` 解析(string / int / bool / json) |
| `web/src/stores/__tests__/config.store.test.ts` | 10 | **B7**:超过 100 条**循环翻页**直到拿齐 / 刚好一页不多请求 / 10 分钟缓存窗口 / `force`;**B8**:`getValue` 按 `value_type` 解析 |
| `web/src/api/__tests__/config.api.test.ts` | 3 | **B4**:`byGroup` 走 params 而不是手拼 query / URL 里不含 `?` `&` / `list` 两条出口一致 |
| `web/src/pages/system/dict/__tests__/isActive.test.ts` | 6 | **B3**:表单层 1/0 ↔ wire 层 boolean 的双向转换 / 往返一致 / **wire 层必须是 boolean** |

**顺带改写**:`web/src/stores/__tests__/dict.store.test.ts` 的 fixture
`is_active: 1` → `true`(类型撒谎的同伙 —— M94 那 8 条用例当时也在用错的类型)。

**可逆探针**:scripts/probe_m107.mjs,**9 条探针,实测全部红在指定用例上,还原后全绿**
(探针合计让 13 条用例变红)。其中 ① ② ⑧ ⑨ 是**覆盖网探针**
(`parseConfigValue` 与 `fetchAll` 的翻页逻辑本轮**改过**,但"改错了会被新网抓住"
这件事要靠探针证明)。

> ⚠️ **探针锚点要容忍 CRLF**(老规矩):Windows 上这些源文件是 CRLF,
> 锚点里的 `\n` 要写成 `\r?\n` 才打得到 —— 否则脚本报"探针没打中"。
> **"探针没打中"和"测试没红"是两回事**,前者更危险(它会让人以为这条修复不承重)。
> 本轮第一次跑就撞上了(⑧ 的 `switch` 块锚点),改成 `\r?\n` 后命中。

**验证**(2026-10-28):

| 项 | 结果 |
|---|---|
| 全量 vitest | **56 文件 / 417 用例 全绿** |
| `npx tsc --noEmit` | **50 个错误 = 基线不变**,本轮碰过的文件 **0 个** |
| `npx eslint`(本轮 11 个文件) | **0 problems**(顺手清掉 8 个 `consistent-type-imports` + 1 个 `no-explicit-any`) |
| `npx prettier --check`(本轮新文件 + HEAD 本就合规的文件) | 全合规 |
| scripts/probe_m107.mjs | **9/9 红在指定用例上,还原后全绿** |
| `scripts/check_backlog_consistency.py` | ✅ 自洽 |

> ⚠️ **仓库整体 lint 基线是红的**(172 errors / 146 warnings),本轮**没动**那部分 ——
> 只保证**本轮碰过的文件是干净的**。别把"改了 8 个 import 风格"当成"lint 绿了"。
>
> ⚠️ **eslint 的 `--fix` 会往 import 尾巴塞分号**,而本仓 prettier 是 `semi: false`
> —— autofix 完必须再 `prettier --write` 一遍,否则风格和仓库不一致。
> (本轮踩过:eslint autofix 加了 5 个分号,prettier 一把抹掉。)

---

## 2.5 ✅ 错误处理 / 通知(100%)

#### ✅ 已完成(8/8)

| # | 子项 | 证据 |
|---|---|---|
| 1 | `message` / `notification` 统一处理 | `api/client.ts:130-152` |
| 2 | 401 自动登出(跳 /login) | `api/client.ts:154-167` |
| 3 | tKey 非 hook 版 lookup | `locales/useI18n.ts:55-65` |
| 4 | **业务码 → 文案 i18n** ✅M6 | `locales/useI18n.ts:79-98` `tError(code, fallback)` 三级 fallback: i18nKeyMap → 翻译 → fallback(后端 message) → `code N`;`api/client.ts:143-153` `handleBizError` 调 tError 替原 data.message;4xxxx 走 message / 其他走 notification(commit 7c406c7) |
| 5 | **错误上报(Sentry)** ✅M27 / **M109 复核** | `web/src/utils/sentry.ts`:**全局捕获只有一道**(M109 删掉自装的第二份监听器 —— SDK 的 `globalHandlers` 集成已经接管 `window.onerror` / `window.onunhandledrejection`);React 渲染错误由 `components/common/ErrorBoundary` 捕获 → `reportError()`;opt-in DSN + dev 模式 no-op;`stores/user.ts` / `stores/ui.ts` 变化自动 sync context(role 取自**当前身份**的 `role_code`,M109 修正);`beforeSend` 过滤 AbortError(M11 noise)且 value 非字符串时不炸(M109 修正)(commit 5dbabf7 + M109) |
| 6 | **错误重试提示** ✅M7 | 错误态随 `tableProps` 交给表格外壳(`TableErrorChannel`),**业务页零改动**;没数据 → empty 区是降级占位(加载失败 + 重试按钮);有旧数据 → 表格上方一条提示条(旧数据留着);点重试 = 重新拉当前查询。落点:`components/common/TableErrorState.tsx` + `ManagementTable.tsx` + `DataTable.tsx` |
| 7 | **降级 UI** ✅M7 | `FallbackPlaceholder` 从**零调用方的死码**变成降级占位的实现体(`TableErrorState` 的 empty 形态);断网恢复自动重载一次 |
| 8 | **网络监听(navigator.onLine)+ 离线检测** ✅M19/M64/M7 | `useOnline` + `OfflineBanner`(顶栏提示);M7 补上最后一环 —— **断网恢复自动重载**(此前 `OfflineBanner` 只提示不做事,没有任何组件接"组件外的事");并修掉 `useOnline` 回调身份导致的监听反复重绑(换绑窗口期会漏掉 `online` 事件)。⚠️ **M109:离线检测不再挂在 eventBus 上** —— `utils/eventBus.ts` 全仓零调用方,已删除(见 §2.5.1 处置表) |

> **复用价值**:**中** —— 错误处理这一类已收官(§7.7.9 / §7.7.10 是实现细节)

---

### 2.5.1 错误上报链 / 错误边界装配审计(2026-10-29,M109)

**起因**:§2.5 标着 **100%**,但这一层的**地基文件一条网都没有**:

- `web/src/utils/sentry.ts` —— 全站**唯一**的错误上报通道(Sentry 接入 / 用户上下文 / 手动上报入口)—— **零覆盖**;
- `web/src/components/common/ErrorBoundary.tsx` —— React 渲染错误的**唯一**捕获点 —— **零覆盖**;
- `web/src/main.tsx` 里的**错误边界装配**(ConfigProvider / 边界 / App 三者的相对位置)—— **零覆盖**,而且装配本身写着**三处假注释**。

> 判高危的客观标准还是"**挂在全局路径上吗**"(见 memory:框架原语零覆盖)。
> 这一层同时握着两个不可逆动作:**把错误送到运维眼前** 和 **崩了之后用户看到什么**。
> 前者坏了 = 线上出事了没人知道;后者坏了 = 白屏。

M109 把这一层逐行读了一遍,读出 **6 个 P1 + 3 个 P2**。

---

**九条问题一览**

| # | 位置 | 现象 | 后果 | 级别 |
|---|---|---|---|---|
| B1 | `utils/sentry.ts` 全局捕获 | 自装 `window.addEventListener('error' / 'unhandledrejection')`,而 SDK 的 `globalHandlers` 集成**接管**了 `window.onerror` / `window.onunhandledrejection` | 每个未捕获错误 / 未处理 rejection **上报两次**(Sentry 里两条几乎相同的事件) | P1 |
| B2 | `main.tsx` 装配次序 | 业务边界在 `LangRoot` **外面** ⇒ 边界捕获时把 children 换掉渲染 fallback ⇒ fallback 落在 `ConfigProvider` **外面** | 深色主题用户看到**白底**错误页;英文用户看到 antd 默认英文 | P1 |
| B3 | `main.tsx` 外层边界 fallback | `fallback={() => <LocalErrorBoundary><App/></LocalErrorBoundary>}` | 崩溃后**把整棵 App 重挂一遍**(状态全丢、副作用重跑)+ **丢掉 LangRoot**(主题 / 语言全没) | P1 |
| B4 | `utils/sentry.ts` `syncUserContext()` | `role: user.roleCode` —— `UserInfo` 上**根本没有**这个字段 | role 恒 `undefined` ⇒ **按角色筛错误筛不出来** | P1 |
| B5 | `utils/sentry.ts` store 订阅 | 只盯 `userInfo?.id`;而切换身份只改 `currentIdentity` | 切完身份 Sentry 里的 role **停在旧身份上** ⇒ 按角色筛**会筛错** | P1 |
| B6 | `utils/sentry.ts` `beforeSend` | `exc?.value?.includes('aborted')` —— `value` **不保证是字符串** | 抛出的不是 Error 时 `number.includes` 是 undefined ⇒ **TypeError**;而 `beforeSend` 抛异常 = 这条事件被 Sentry 内部 catch 掉直接丢弃 ⇒ **"过滤器自己炸了"反而把真实错误吞了** | P1 |
| B7 | `utils/sentry.ts` `Sentry.init()` | `sendDefaultPii: false` | Sentry v11 **已经没有这个选项**(实测 v11.8.0 的 core / browser / react 全仓 **0 处引用**)⇒ 它**什么也没做**,却让 `tsc` 报 TS2353(基线错误之一) | P2 |
| B8 | `utils/sentry.ts` 头注释 | "PII 字段不上报(request body / response data 关闭 beforeSend 钩子过滤)" | **假的** —— `beforeSend` 只过滤 AbortError,不过滤任何字段;v11 里 PII **默认**不上报(SDK 不再自动附带),不是因为有个钩子在过滤 | P2 |
| B9 | `ErrorBoundary.tsx` 的 `fallback` prop | `ReactNode \| ((err, reset) => ReactNode)` 靠 `typeof === 'function'` 区分 | **组件也是函数** ⇒ `fallback={MyFallback}`(最自然的一种)被**当普通函数调用** ⇒ 组件里的 hooks 全失效 ⇒ React 抛 "Invalid hook call" ⇒ **兜底 UI 自己把树炸了**,整页白屏 | P2 |

---

**P1 机理 #1(B1):全局未捕获错误被上报两次**

Sentry v11 的 `globalHandlers` 集成是 `@sentry/browser` 的**默认集成**,它做的事是:

```
@sentry/core/build/cjs/instrument/globalError.js
  → worldwide.GLOBAL_OBJ.onerror = function (...) { ... }    ← **覆盖属性**
  → 原函数打上 __SENTRY_INSTRUMENTED__ = true
@sentry/core/build/cjs/instrument/globalUnhandledRejection.js
  → worldwide.GLOBAL_OBJ.onunhandledrejection = function (...) { ... }   ← 同样是覆盖
```

关键点:`window.onerror` 这个**属性本身就是 `error` 事件的监听器**。
所以本文件自己再 `window.addEventListener('error', …)` 一份 ⇒ **两个监听器都会跑**
⇒ 每个未捕获错误在 Sentry 里**落两条几乎相同的事件**。

⇒ 修法:**全局捕获只留一道** —— 交给 SDK,本文件不再自装监听器。

依据不是"听说 SDK 会装",而是**装完之后能观测到**:`globalThis.onerror.__SENTRY_INSTRUMENTED__ === true`
(回归网 `utils/__tests__/sentry.global.test.ts` 跑的是**真 SDK**,不 mock)。

> ⚠️ 断言要打 `globalThis` 而不是 `window`:vitest 的 jsdom 里两者**不是同一个对象**
> (`populateGlobal` 是把 window 的属性**拷贝**到 Node 全局上)⇒ 断言 `window.onerror`
> 永远拿到的是拷贝时的初值。真实浏览器里 `globalThis === window`,两者等价。

---

**P1 机理 #2(B2 / B3):边界装配的次序错了 ⇒ 白底错误页 + 把 App 重挂一遍**

React 的错误边界**就近捕获**:只有**最内层**那个边界会拿到错误,外层边界**永远看不到**。
而错误边界捕获时,会把**自己的 children 换掉**渲染 fallback ⇒
**fallback 拿到的是"边界所在位置"的 provider 上下文**。

改动前的装配(`git show HEAD:web/src/main.tsx`):

```
<SentryErrorBoundary fallback={() => <LocalErrorBoundary><App/></LocalErrorBoundary>}>
  <LocalErrorBoundary>          ← ← ← 业务边界在 LangRoot **外面**
    <LangRoot>                  ← ConfigProvider 在边界**里面**
      <App/>
    </LangRoot>
  </LocalErrorBoundary>
</SentryErrorBoundary>
```

三个后果:

1. **深色主题用户看到白底错误页** —— 业务边界在 `LangRoot` 外面 ⇒ 它渲染的 fallback
   落在 `ConfigProvider` **外面** ⇒ 拿到的是 antd 默认主题(`#1677ff`),不是本站的品牌橙;
2. **外层边界的 fallback 会把整棵 App 重挂一遍** ⇒ App 重新挂载(状态全丢、副作用重跑),
   而且**丢掉了 LangRoot**(主题 / 语言全没);
3. **main.tsx 那句注释"外层 SentryErrorBoundary:有 Sentry 时上报"是假的** ——
   React 边界就近捕获,外层边界**根本拿不到**内层的错误;真正在上报的是内层边界的 `reportError`。

⇒ 修法:装配抽成独立组件 `components/common/AppErrorBoundary.tsx`,三层:

```
<ErrorBoundary fallback={<FatalErrorUI/>}>   ← ① 最外层安全网(纯 HTML,不依赖 antd)
  <LangRoot>                                ← ② ConfigProvider(在边界**之上**)
    <ErrorBoundary>                         ← ③ 业务边界(antd 降级 UI + reportError)
      <App/>
    </ErrorBoundary>
  </LangRoot>
</ErrorBoundary>
```

- ① 存在的意义是"**连渲染环境都出问题了**"(ConfigProvider 崩了 / antd 自身抛错)
  ⇒ 它的 fallback **刻意不依赖 antd**:用 antd 渲染兜底 = **用坏掉的东西兜坏掉的东西**
  ⇒ 二次抛错 ⇒ 白屏(白屏是用户能看到的最后一种失败);
- ② ConfigProvider 放在边界**之上** ⇒ 降级 UI **继承主题与语言**;
- ③ 业务边界唯一负责"捕获 + 上报 + 渲染 antd 降级 UI",上报路径**只有一条**(`reportError`)。

> `LangRoot` 是从 `main.tsx` **逐字**搬到 `components/common/LangRoot.tsx` 的 ——
> `main.tsx` 在**模块作用域**就 `createRoot().render()`,没法进单测。
> 搬的时候用脚本断言"除 `export ` 外**逐字节相同**"(⚠️ 手抄必抄错:antd token 一大把)。

---

**P1 机理 #3(B4 / B5):上报的用户上下文里 role 恒为 undefined**

`syncUserContext()` 里写的是 `role: user.roleCode` —— 而 `UserInfo` 上**根本没有** `roleCode` 字段
(真正的角色在 **identity** 上:同一个用户在不同项目里可以是不同角色)。
⇒ Sentry 里的 role 恒为 `undefined` ⇒ **按角色筛错误筛不出来**。

而且 store 订阅只盯 `userInfo?.id`,而切换身份(`switchIdentity`)**只改 `currentIdentity`**
⇒ 切完身份,Sentry 里的 role 还停在**旧身份**上 ⇒ 按角色筛**会筛错**。

⇒ 修法:role 取自**当前身份**的 `role_code`,订阅同时盯 `userInfo.id` **与** `currentIdentity.id`。

---

**修法**(框架层,零业务代码):

| # | 修法 |
|---|---|
| A1 | `utils/sentry.ts` 删掉自装的两个 `window.addEventListener`;文件头写明"全局捕获只有一道"及其依据(`__SENTRY_INSTRUMENTED__`) |
| A2 | `beforeSend` 里先做类型收窄:`const value = typeof exc?.value === 'string' ? exc.value : ''` |
| A3 | `Sentry.setUser({ …, role: state.currentIdentity?.role_code })` |
| A4 | store 订阅同时盯 `userInfo.id` **与** `currentIdentity.id` |
| A5 | 删掉 `sendDefaultPii`(死选项)+ 改掉头注释里两处假话(PII 那句、tree-shake 那句) |
| A6 | `ErrorBoundary` 拆成 `fallback`(纯节点)与 `renderFallback`((err, reset) => ReactNode);运行时再加一道 —— 传进来的若是**函数**(JS 调用方 / `any` 逃逸),退回默认 UI + `console.warn`,**绝不当普通函数调用** |
| A7 | 装配抽成 `components/common/AppErrorBoundary.tsx`:外层安全网(纯 HTML `FatalErrorUI`)→ `LangRoot` → 业务边界 |
| A8 | `LangRoot` 从 `main.tsx` 逐字搬到 `components/common/LangRoot.tsx` |
| A9 | 删掉 `utils/eventBus.ts`(零调用方)与 `utils/index.ts` 里的再导出 |

> A6 拆完之后,`fallback={MyFallback}` 在**编译期**就报错 ——
> `@types/react` 18.3.31 的 `ReactFragment = Iterable<ReactNode>`,**函数不是 ReactNode**
> ⇒ 不必等到运行时才发现"组件被当函数调用"。

---

**零调用方 / 边界处置表**(主动交代,不装死)

| 对象 | 处置 | 理由 |
|---|---|---|
| `utils/eventBus.ts` | **删除**(连同 `utils/index.ts` 里的再导出) | 全仓**零调用方**(自 `0431415` 引入后一直没人用);而且 §2.5 清单里曾把它当成"已有事件机制"的**假证据** —— 离线检测的真源是 `useOnline`(navigator.onLine),`EVENTS.NETWORK_OFFLINE` 从来没人发过。留着一份没人用的"跨组件通信"承诺,下一个人需要事件机制时只会**照着它写**,不会去看真源 |
| `utils/index.ts` 的 barrel | **保留** | 零调用方,但它是**导出面**(不是能力重复),留着无害 |
| `utils/sentry.ts` 的 `ErrorBoundary` 再导出(Sentry 版) | **删除** | 零调用方;而且 React 边界**就近捕获**,外层边界永远拿不到内层的错误 ⇒ 这个导出只会给人"上报已覆盖"的错觉 |
| `sendDefaultPii` | **删除** | v11 已无此选项(实测 0 处引用),留着既没用又让 tsc 报 TS2353 |
| `Sentry.ErrorBoundary` 组件 | **不再使用**(但不删依赖) | `@sentry/react` 还要 import(它带 `captureException` / `addBreadcrumb`);`ErrorBoundary` 只是 SDK 的一个组件,不用它不等于要换 SDK |

---

**回归网**:**5 份新用例文件,27 条前端用例**:

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/utils/__tests__/sentry.test.ts` | 12 | DSN 空 / dev 模式 no-op;`reportError` 降级 `console.error`;init 恰好一次;**init 之后不再自装 window 监听器**;`beforeSend` 过滤 AbortError / aborted;**`exc.value` 非字符串时不炸**;role 取自当前身份;**切身份后 role 更新**;登出 `setUser(null)`;`reportError` → `captureException`;`reportBreadcrumb`;**不传死选项 `sendDefaultPii`** |
| `web/src/utils/__tests__/sentry.global.test.ts` | 1 | **真 SDK**(不 mock):init 之后 `globalThis.onerror.__SENTRY_INSTRUMENTED__ === true`(以及 `onunhandledrejection`)—— 这条是"可以把自装监听器删掉"的**依据** |
| `web/src/components/common/__tests__/errorBoundary.test.tsx` | 8 | 正常渲染;子组件抛错 → 默认 fallback + `reportError({tag, componentStack})`;`fallback` 传节点原样渲染;`renderFallback` 拿到 `(error, reset)`;**`fallback` 传组件不会被当普通函数调用**(退回默认 UI + warn);点"重试" → `onReset` + children 重新渲染;`ErrorFallbackUI` 显示错误文案;`FatalErrorUI` 是纯 HTML |
| `web/src/components/common/__tests__/appErrorBoundary.test.tsx` | 3 | App 抛错 → **业务边界**的 fallback(不是致命兜底)+ 只上报一次;**fallback 继承 LangRoot 的 ConfigProvider**(`data-color-primary === '#ff6600'`,不是默认的 `#1677ff`);无错误时不触发上报 |
| `web/src/components/common/__tests__/appErrorBoundary.fatal.test.tsx` | 3 | LangRoot 抛错 → 渲染 `FatalErrorUI`(纯 HTML);**最外层兜底不重挂整棵 App**(App 的挂载次数 = 0);这一层的错误**照样上报** |

**可逆探针**:scripts/probe_m109.mjs,**9 条探针,实测全部红在指定用例上,还原后全绿**:

| # | 探针 | 打红的用例 |
|---|---|---|
| ① | 把自装的 `window error / unhandledrejection` 监听装回去(照抄 HEAD) | init 之后没有自己再装 window 监听器 |
| ② | `beforeSend` 退回 `exc?.value?.includes('aborted')`(照抄 HEAD) | `exc.value` 非字符串时不炸 |
| ③ | role 退回 `user.roleCode`(照抄 HEAD) | role 取自当前身份 |
| ④ | store 订阅退回只看 `userInfo.id`(照抄 HEAD) | 切换身份后 role 跟着更新 |
| ⑤ | 把 `sendDefaultPii: false` 加回来(照抄 HEAD) | init 参数符合约定 |
| ⑥ | `fallback` 退回"是函数就当普通函数调用"(照抄 HEAD) | fallback 传组件不会被当普通函数调用 |
| ⑦ | 装配退回"业务边界在 LangRoot 外面"(照抄 HEAD 的次序) | fallback 继承 LangRoot 的 ConfigProvider |
| ⑧ | 外层 fallback 改回重挂 children(照抄 HEAD 的语义) | 最外层兜底不重挂整棵 App |
| ⑨ | `FatalErrorUI` 改用 antd `Result` 渲染 | 渲染 FatalErrorUI(纯 HTML) |

> ⚠️ **探针锚点要容忍 CRLF**(老规矩):`utils/sentry.ts` / `ErrorBoundary.tsx` 是 CRLF,
> 锚点里的 `
` 要写成 `?
` 才打得到 —— 否则脚本报"探针没打中"。
> **"探针没打中"和"测试没红"是两回事**,前者更危险(它会让人以为这条修复不承重)。
>
> ⚠️ **用例标题里有 `**` 会被当成强调标记** —— 探针里匹配用例名只能用**不含 `**` 的片段**
> (例:`**不会**被当普通函数调用` ⇒ 只能匹配 `被当普通函数调用`)。
>
> ⚠️ **Windows 专属坑**(沿用 M107 / M108 的写法):`execFileSync('npx', …)` 必须带
> `shell: true`(npx 实际是 npx.cmd,否则 ENOENT 被 catch 吞掉 ⇒ 输出 0 个用例 ⇒
> **报成"绿"**)。脚本里 0 个用例一律**抛错**,绝不判 GREEN。

---

**验证**(2026-10-29):

| 项 | 结果 |
|---|---|
| 全量 vitest | **64 文件 / 470 用例 全绿** |
| `npx tsc --noEmit` | **48 个错误 = 基线 50 − 2**(修掉 `sendDefaultPii` 的 TS2353 与 `roleCode` 的 TS2339),本轮碰过的文件 **0 个** |
| `npx eslint`(本轮 14 个文件) | **0 problems**(顺手清掉 3 个 `consistent-type-imports`) |
| `npx prettier --check`(本轮**新**文件) | **全合规** |
| `npx prettier --check`(本轮改过的老文件) | `locales/*.ts` / `stores/user.ts` **不合规 —— 但全是 HEAD 就有的老问题**,本轮新增的行**没有一条**被 prettier 挑出来 |
| scripts/probe_m109.mjs | **9/9 红在指定用例上,还原后全绿** |
| `scripts/check_backlog_consistency.py` | ✅ 自洽 |

> ⚠️ **prettier 那条要说清楚(Windows 上有个坑)**:
>
> 1. **本仓 `.gitattributes` 是 `* text=auto` + `core.autocrlf=true`** ⇒ **提交进仓的是 LF**,
>    但 Windows 的工作区是 CRLF。而 prettier 配的是 `endOfLine: "lf"` ⇒
>    **在 Windows 上直接 `prettier --check` 这些文件,一律报"不合规"** ——
>    哪怕它一行都没改。**这个信号是假的。**
>    有意义的检查是**先把 CRLF 归一化成 LF 再查**(那才是 git 会存进去的内容)。
> 2. 归一化之后:`locales/zh-CN.ts` / `locales/en-US.ts` / `stores/user.ts` 不合规,
>    但**挑出来的全是本轮没碰的老行**(超宽的对象字面量、文件末尾缺换行)。
> 3. ⇒ **故意不跑 `prettier --write`**:它会把这些老行一起重排 ⇒ **几十行与本轮无关的 diff**。
>    本轮**只保证自己新增的行是 prettier 风格的**,不顺手改别人的行。

---

**遗留**(明确不在本轮范围,写下来不假装解决):

| # | 遗留 | 为什么不在本轮 |
|---|---|---|
| L1 | **Sentry 只接了后台 Web** | 小程序侧是另一套运行时,SDK 选型 / 初始化方式都不同 |
| L2 | **`beforeSend` 只过滤 AbortError** | 业务错误码(4xxxx)的噪音过滤是**业务层**的白名单,框架层不猜 |
| L3 | **Session Replay 默认关闭**(`VITE_SENTRY_REPLAYS_SAMPLE_RATE` 缺省 0) | 框架层只做错误追踪;Replay 会录到用户操作,要不要开是**产品决策** |
| L4 | **`release` 取 `VITE_APP_VERSION`,构建没注入时是 `unknown`** | 需要 CI 在构建时注入版本号(不在前端代码里解决) |
| L5 | **没有上传 source map** | 线上堆栈是压缩后的;要配 Sentry 侧的构建上传,不在本轮 |

---

## 2.6 ✅ i18n(100%)

#### ✅ 已完成(10/10)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | zh-CN | `locales/zh-CN.ts:1-805` | 805 |
| 2 | en-US | `locales/en-US.ts:1-784` | 784 |
| 3 | useI18n hook(stable t 引用) | `locales/useI18n.ts:67-82` | - |
| 4 | tKey helper(非 hook 场景) | `locales/useI18n.ts:55-65` | - |
| 5 | 占位符插值 `{n}` | `locales/useI18n.ts:35-40` | - |
| 6 | 资源菜单 namespace(`resourceMenu.*` / `resource.*`) | `locales/zh-CN.ts:399-438` | - |
| 7 | ✅ **懒加载语言包**(**M173,2026-11-02 已落地**) | ①**zh-CN 静态、en-US 动态**:zh-CN 是默认语言 + 全部同步查找的兜底(tKey / tError 被 axios 拦截器等**非 React 模块同步调用**,等不了动态 import)⇒ 常驻;en-US(~2500 行)此前在 `useI18n.ts` / `index.ts` barrel 里被**静态 import** 打进主 chunk ⇒ 改走 `locales/registry.ts` 的 `import('./en-US')`,vite 自动拆独立异步 chunk(**实测 build:`en-US-*.js` 70.51 kB / gzip 23.70 kB,不再进主包** —— 多数中文用户首屏省下这一口)。②**同步查找降级**:包没到时 `getDict(lang)` 返 undefined → 回落 zh-CN,英文用户短暂看中文,到了自动切(宁可闪中文,不阻塞首渲染)。③**到达信号**:`useSyncExternalStore` 版本号(注册表 5 行订阅三件套)—— 包到达 → `useI18n` 组件**不刷新页面**自动重渲染成英文;触发点挂在 `LangRoot` 的 `useEffect([lang])`(持久化恢复的初始 lang 不经过 `setLang`,但一定经过挂载 —— 单一触发点;失败静默回落中文,registry 清掉 pending 下次重试)。④**并发去重 + 失败可重试**:同语言只发一次 import(pending map),失败清 pending(网络抖一次不该永久打死英文)。⑤**barrel 纪律**:`locales/index.ts` 不再 re-export enUS(barrel 被 ~50 个组件引用,静态 re-export 会把它拽回主包,懒加载整个白做;需要 enUS 的直接引模块文件)。⑥测试 10 条(registry 6:全新实例 per-case / 幂等 / 失败重试 / 版本信号;端到端 4:同步回落中文 → 包到达自动切英文 → 缓存命中)+ **可逆探针实测转红**(拆掉 registry 的 version+1/listeners 通知 → 「包到达自动重渲染」那条红 —— 这正是懒加载最容易烂掉的静默形态:英文用户永远停在中文兜底且无报错)。⚠️ **mock 纪律**:测"失败重试"必须用 `vi.doMock` 现注册现用 —— 顶层 `vi.mock` 工厂结果被缓存,`vi.resetModules()` 不清,**fail 开关拨了也不生效**(实测假绿,错误还被打包成 vitest 自己的文案)。⑦**跟进改造**(同日第二笔 commit):tError 的英文契约随懒加载变化(包没到回落中文)—— biz-code-i18n 两条用例改为先 `await ensureLocale('en-US')`(真实 app 里 LangRoot / `setLang` 都会触发);`setLang` 自身也加 `ensureLocale` 触发(幂等,非 React 场景的保险);顺手收掉 **M164 遗留**的「列标题必须落 field.\*/common.\* 共享组」违规 —— FieldPolicyDrawer 4 个列键从 `page.role.fieldPolicy.*Column` 归位到 `field.fieldName / field.visibility / field.maskType / common.remark`,死键删除(src/locales 全目录 47 条测试全绿) | 中 | ~~P2~~ → 已收 |
| 8 | 语言切换 UI | `components/layout/LanguageSwitcher.tsx`(顶栏下拉,P1 ✅ M19) | - |
| 9 | antd 内置文案切语言 | `LangRoot.tsx` ConfigProvider locale 跟随 lang(P1 ✅ M19) | - |
| 10 | 日期/数字 locale | 数字走 `toLocaleString(locale)`;`formatDate.relative` 用 `Intl.RelativeTimeFormat`(locale 感知)(P2 ✅ M19 + M28;🔴 M110 加固见 §2.3.2 B2) | - |

#### ❌ 未完成(0 项 —— 全部已收)

> **复用价值**:**高**

---

## 2.7 ✅ 表单组件(100%)

#### ✅ 已完成(15/16)

| # | 组件 | 证据 | 行数 |
|---|---|---|---|
| 1 | EditModal(9 种 field type) | `components/common/EditModal.tsx:1-228` | 228 |
| 2 | ViewDrawer(Descriptions) | `components/common/ViewDrawer.tsx:1-82` 🔴 **M111**:收回死 prop `ViewField.span`(Descriptions column 固定为 1 ⇒ 本来就占满一行;传 >1 还会触发 antd 的 span 不匹配警告)+ 补 8 条回归网 —— 见 §2.7.3 | 82 |
| 3 | SearchForm(input/select/date/number) | `components/common/SearchForm.tsx:1-131` | 131 |
| 4 | SearchSection(浅灰底 + L1 阴影容器) | `components/common/SearchSection.tsx:1-56` 🔴 **M111**:收回死 prop `fullWidth`(组件体从来没读过它)+ 补 4 条回归网 —— 见 §2.7.3 | 56 |
| 5 | SensitiveField(脱敏 + 眼睛切换) | `components/common/SensitiveField.tsx:1-79` 🔴 **M110**:它依赖的 `mask.*` 此前对**短输入一个字符都藏不住**(看着打了码,原值一字不差)⇒ 脱敏形同虚设,已修 + 补回归网 —— 见 §2.3.2 B3。🔴 **M111**:`auto` 自动识别把**恰好 18 个字符的邮箱**当成身份证脱敏;眼睛按钮没有可访问名 —— 见 §2.7.3 | 79 |
| 6 | ImageUploader(OSS 直传) | `components/common/ImageUploader.tsx:1-125` | 125 |
| 7 | SmsCodeGuard(Modal 二次验证) | `components/common/SmsCodeGuard.tsx:1-248`(🔴 **M104 重写**:scene 必填 + 验证码交回调用方 + delegate 模式,见 §2.7.1) | 248 |
| 8 | SmsSendButton(localStorage 冷却持久化) | `components/common/SmsSendButton.tsx:1-188`(🔴 **M104 重写**:scene 必填 + 冷却按 scene 分桶,见 §2.7.1) | 188 |
| 9 | CaptchaField(刷新 + 校验) | `components/common/CaptchaField.tsx:1-189`(🔴 **M106 加固**:拉取失败不再永远转圈、失败保留旧图,见 §2.7.2) | 189 |
| 10 | SecretInput(系统配置用) | `components/common/SecretInput.tsx:1-66` | 66 |
| 11 | ConfigDiffView(原值 vs 新值) | `components/common/ConfigDiffView.tsx:1-96` | 96 |
| 12 | ConfirmDialog(替代 Modal.confirm) | `components/common/ConfirmDialog.tsx:1-97` | 97 |
| 13 | RowActions(行内 perm) | `components/common/RowActions.tsx:1-90` | 90 |
| 14 | PageHeader(@ant-design/pro) | `components/common/PageHeader.tsx:1-44` | 44 |
| 16 | ✅ 字段权限联动(**M168,2026-11-02 已落地**) | 服务端那一半 = M162 / M163 / M164(详见 §1.13 #4);**前端这一半 M168**:①报告由**执行层算好、前端只读**(M149 纪律)—— `X-Field-Policy` 响应头 + Symbol 戳,axios 拦截器盖章 + 按请求发起时的页面归档(页面重拼了行对象时兜底;路径不匹配 → null,不串页)。②**三个共用外壳消费**:`ManagementTable` **隐藏的列整个不渲染**;`EditModal` 隐藏项不渲染、打码项变只读且**从提交载荷里剔掉**(光只读不够 —— `validateFields()` 会把 disabled 字段的值照常收进来,原样提交 = 打码值覆盖真实值 = **数据损坏**);`ViewDrawer` 隐藏项不渲染。③**没有报告 ⇒ 一切照旧**(零业务页改动;20 / 15 / 13 处调用点的「不变」路径全部有测试锁住)。④前端 60 条测试 + **2 条可逆探针实测转红**。详见 §1.13 #5 | 中 | ~~P2~~ → 已收 |

#### ❌ 未完成(0 项)

| # | 子项 | 当前缺口 | 价值 | 优先级 |
|---|---|---|---|---|
| 15 | ✅ **通用 schema 驱动表单组件**(**M178,2026-11-03 已落地**) | `components/common/SchemaForm.tsx` + `schemaFormRules.ts`(纯逻辑单文件,豁免 react-refresh):业务页**声明字段 schema 数组**(13 种类型 input/textarea/password/number/select/radio/checkbox/switch/date/datetime/time/custom…)即渲染完整 antd 表单,**与 `sys_dyn_field` 完全解耦**(M152 的动态表单管动态集合,这个管静态业务页)。要点:hidden 字段用 `<Form.Item hidden>` 保值进提交值、隐藏期不挂 rules(看不见的 required 拦提交=死表单);form 双通道(prop + forwardRef);required 文案 `${label}不能为空` + 文本类 whitespace。39 用例 + 可逆探针(required 分支改坏 → 7 条红 → 恢复绿)。⚠️ 文件名不能用 `schemaForm.ts` —— vite extensions `.ts` 先于 `.tsx` + Windows 大小写不敏感 ⇒ `import './SchemaForm'` 会解析到纯逻辑文件(实测探针确认,同 `avatar.ts`/`UserAvatar.tsx` 命名规则) | 中 | P2 ✅ |

### 2.7.1 表单 / 短信组件审计(2026-10-28,M104)

**起因**:§2.7 标着 88% —— 但**短信这一条链路从来没活过**。
§2.7 的 14 个"✅ 已完成"里,`SmsCodeGuard` / `SmsSendButton` 两个是**短信二次验证**的入口,
而它们在 2026-10-28 之前:

- `components/common/__tests__/` 下**一条短信用例都没有**(只有 EditModal / ViewDrawer /
  ManagementTable / BatchActionBar / SecretInput 五个文件);
- `constants/__tests__/` 目录当时根本不存在(M103 建的);
- 前端**没有任何地方**把后端的 scene 白名单表达出来 —— 前后端各写各的。

M104 把 §2.7 的 14 个组件逐个读了一遍,重点读**前后端短信链路**(登录 / 找回密码 /
个人中心改密 / 身份切换),读出 **11 个真问题**:

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `web/src/api/auth.ts` `sendSmsCode` + 三个调用页 | 前端发的 scene 是 `login-sms` / `forgot-password` / `switch-identity` / `change-password` | **后端一个都不认**(`validScene()` 只认 `login` / `reset_password` / `change_phone` / `view_full_phone`)→ 每个 `POST /public/auth/sms/send` 都返 400「未知 scene」。**短信一条都没发出去过** —— 登录页的「短信验证码登录」和找回密码页是**两条死路**。写错不编译报错、不崩,只是功能从来没活过 |
| B2 | `SmsCodeGuard` / `SmsSendButton` 的 `bizType` prop | 这个 prop **从来没被传进** `sendSmsCode` / `verifySmsCode` | 与 B1 同源:B1 是"传错了",B2 是"**压根没传**"。后端收到的 scene 是 `undefined` |
| B3 | `SmsCodeGuard` 的 `onVerified` | 回调**不带验证码** | 调用方(`IdentitySwitcher`)只能传 `undefined` 给后端 → 后端直接拒「切换到高敏角色需要短信验证码」。组件里那句注释「后端暂不严格」**是假的** —— 后端一直很严 |
| B4 | `SmsCodeGuard` + `IdentitySwitcher` | 本地先验一次,后面再拿同一个码去 `switchIdentity` | 后端 `VerifySms` / `SwitchIdentity` **校验通过即置 `used_at`** → 第二次必然拿到「短信验证码已使用」。**双重消费** |
| B5 | `service/auth_view.go` `decryptPII` | 是个 stub,恒返回 `""` | `/auth/me` 的 phone 恒空 → 前端拿不到手机号 → 整条短信二次验证链路断掉。`sys_user.phone` 这一期**本来就是明文落库**,所以这里不是"缺解密",是"把值扔了" |
| B6 | `service/auth_impl.go` `SwitchIdentity` 的验证码查询 | `WHERE code = ? AND scene = ?` —— **不带 phone** | 任何人可以走**公开**接口 `/public/auth/sms/send` 给**自己的**手机号发一条 `scene=change_phone` 的码(`change_phone` 对公众开放),拿到码之后拿来切**任意账号**的高敏身份 —— **二次验证形同虚设**。这是本轮唯一一个**安全洞**,不是显示问题 |
| B7 | `SmsCodeGuard` 的倒计时 | `setInterval` 拿了句柄就没管过 | 组件卸载后那个 timer 还在每秒跑一次 `setState` |
| B8 | `SmsCodeGuard` 的 `cloneElement` | `cloneElement(trigger, { onClick: () => setOpen(true) })` | 把 trigger **自己**的 `onClick` 冲掉了(埋点 / 埋单 / disabled 判断全丢) |
| B9 | `SmsCodeGuard` 的长度校验 | `code.length < 4` | 4 位、5 位都能过这关,然后被后端拒 —— 用户已经输了,提示却来得莫名其妙。后端 `generateCode` 恒 `%06d`,就是 6 位 |
| B10 | `pages/profile/index.tsx` 改密弹窗 | 拿 `userInfo.phone`(**脱敏**的 `138****5678`)去调公开发码接口 | 公开接口 `validPhone` 要求 11 位纯数字 → 必被拒「手机号格式错误」→ **个人中心从来改不了密码**。前端只有脱敏号,**服务端代发是唯一出路** |
| B11 | `SmsSendButton` 的提示语 | 字典模板是 `验证码已发送({scene})`,代码传的是 `{ bizType: ... }` | 占位符替换不上 → 界面上出现字面量「验证码已发送({scene})」。(这条是 M104 自己改出来的,守门网当场抓住 —— 见下方"探针自己抓到自己") |

**修法**(框架层 + 短信链路,零业务代码):

- **B1 / B2 →** 新建 `web/src/constants/sms.ts` 作为**前端唯一的 scene 契约**:
  `SmsScene` 枚举(4 个) + `SMS_SCENES` + `SMS_SCENE_LABEL_KEY`(i18n key)+ `SMS_CODE_LEN`。
  `sendSmsCode` / `verifySmsCode` 的 scene 参数改成**必填的 `SmsScene`** ——
  类型层面就锁死,传错编译期就红。`bizType` 这个 prop **删掉**(不是改名 —— 改名的
  时候旧调用点会跟着编译通过,只有删掉才断干净)。
- **B3 →** `onVerified(code)` 把验证码交回调用方。
- **B4 →** 加 `mode: 'guard' | 'delegate'`:
  `guard`(默认)= 本地先验一次;`delegate` = **不**本地验,把码交给后续那个后端调用消费。
  身份切换走 `delegate`(码由 `switchIdentity` 消费)。
- **B5 →** `decryptPII` 改成**原样返回**(`sys_user.phone` 这一期是明文落库),
  并留 TODO:等 PII 真正加密后接 `crypto.DecryptFor`。
- **B6 →** 规则收口成**一处**实现 `consumeSmsCode(ctx, phone, scene, code)`:
  `WHERE code = ? AND scene = ? AND phone = ?` + `used_at` 检查 + TTL 检查 +
  条件更新置 `used_at`(并发双提交只有一条能成功)。`SwitchIdentity` / `VerifyAccountSms`
  共用它,不会各自漂移。
- **B7 →** timer 句柄进 `timerRef`,`useEffect(() => stopCountdown, [])` 卸载时清。
- **B8 →** `cloneElement` 里**组合**而不是替换:`trigger.props.onClick?.(e)` 然后再 `setOpen(true)`。
- **B9 →** 长度改成**严格等于** `SMS_CODE_LEN`(6)。
- **B10 →** 新增**服务端代发**的一对接口(取代原先那个 `/auth/switch-identity/sms`):
  | 接口 | 作用 |
  |---|---|
  | `POST /rapid-admin/v1/auth/sms/account/send` | 给**当前账号登记的手机号**发验证码(手机号由服务端从 `sys_user` 取,前端不传) |
  | `POST /rapid-admin/v1/auth/sms/account/verify` | 校验并消费发给当前账号登记手机号的验证码 |

  场景白名单 = `{change_phone, reset_password, view_full_phone}`
  (**不含 `login`** —— 登录场景是给"还没绑到账号上的手机号"用的,自助发没有意义)。
  限流:发 5 次/分钟/用户,验 10 次/分钟/用户(6 位码在 5 分钟 TTL 内的爆破空间)。
  新增 RBAC 资源码 `auth:account-sms`,并进 `authBase`(所有非超管后台角色都有)。

> ⚠️ **身份切换的短信场景复用 `change_phone`** —— 阿里云那边**只有 4 个模板**
> (`login` / `view_full_phone` / `reset_password` / `change_phone`,见 `docs/配置项清单.md`),
> **没有 `switch_identity` 模板**。后端 `validScene()` 也只认这 4 个。
> 技术方案里那份 5 scene 列表(带 `switch_identity`)是**过期的**,本轮已改。

**顺带修掉的**:`web/src/locales/{zh-CN,en-US}.ts` 新增 `smsScene` 段(4 个场景各一句人话)——
界面里不该出现内部标识(以前是「验证码已发送 (login-sms)」)。

**主动收回 / 记下来的**(不假装解决):

| 东西 | 现状 |
|---|---|
| 个人中心改密的短信闸门 | **已撤销(2026-10-29,M113)** —— 用户明确要求「修改密码不要发送短信验证码」,前端那道闸门**整条删掉**(`SmsCodeGuard` 从改密弹窗里移除,`CHANGE_PASSWORD_SMS_SCENE` 常量一并删除,§2.7.1 的 B10 那条随之作废)。**后端契约一个字没动**:`POST /auth/password` 从来就只认 `old_password` + `new_password`(旧密码由 service 层 bcrypt 比对)—— 那道门**从来只是纯前端的**,而且是**坏的**(B10:短信压根发不出去)⇒ 它当时挡在门外的人比它保护的人多。⚠️ 原来记的那句担忧「绕过前端直接调 `/auth/password` 就能改密」**现在不再是缺陷** —— 它本来就不产生任何服务端约束;真要给"改密"加服务端二次验证,仍然是**改 `ChangePassword` 接口的鉴权语义**,单独一轮。**身份切换那边的服务端代发短信**(`auth:sms/account/send` / `verify`)不受影响,继续用 |
| `sys_sms_code.phone` 是明文(size:16) | 这一期就这样(收件号码本来就要能被查到)。等 PII 加密那一轮一起处理 |
| `switch_identity` 模板 | **不存在**。要独立模板得去阿里云申请,不是代码问题 |

**回归网**:**3 份新文件 + 1 份扩写,36 条前端用例 + 12 条后端用例**:

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/constants/__tests__/smsScene.test.ts` | 8 | **B1 的契约网**:**直接解析后端三处源码**(`model/sms.go` / `pkg/msn/sender.go` / `service/auth_impl.go` 的 `validScene()` 白名单),逐条比对前端 `SmsScene` —— 后端加/改 scene,前端立刻红;前端那套"后端从来没认过"的旧 scene 一条都不许残留;每个 scene 都有 i18n 名(zh/en 两份对称);场景名不许等于 scene 原值 |
| `web/src/components/common/__tests__/smsCodeGuard.test.tsx` | 13 | **B2/B3/B4/B7/B8/B9**:scene 传给发码**和**校验;`onVerified` 拿到的是验证码本身;校验不过不回调;delegate 模式**不**本地校验(同一个码不能消费两次);4 位 / 5 位被拦、6 位放行;trigger 自己的 `onClick` 照常触发;卸载时 `setInterval` 被清掉 |
| `web/src/components/common/__tests__/smsSendButton.test.tsx` | 11 | **B1/B11**:scene 传给 `sendSmsCode`;冷却 key 按 scene 分桶(登录页发过了,找回密码页照样能发);冷却期内禁用并显示剩余秒数;刷新页面读回冷却;手机号格式不对不发;提示语是**场景的人话**、不是内部标识、也不是没替换上的 `{scene}` |
| `web/src/components/layout/__tests__/identitySwitcher.test.tsx` | 4 → 4(扩写 mock) | M103 那 4 条**原样保留**;mock 工厂补 `sendAccountSms`(组件改成服务端代发) |
| `server/internal/service/auth_sms_switch_test.go` | 12 | **B6 / B10 / B11**:切换高敏身份的码**必须**发到账号登记的手机号(别的手机号的码一律拒);没登记手机号 → 拒绝高敏切换;自助发码只落在登记手机上;场景不在白名单 → 拒**且不落库**;校验消费(单次);`/auth/me` 的 phone 是**脱敏但非空** |

**可逆探针**:scripts/probe_m104.mjs,**11 条探针,实测全部红在指定用例上,还原后全绿**
(探针合计让 16 条用例变红)。

> ⚠️ **B11 是 M104 自己改出来的 bug,靠 grep 抓的,不是靠测试**:
> M104 把 `sms.codeSent` 的占位符从 `{bizType}` 改名成 `{scene}`,
> 但 `SmsSendButton` 里传的还是 `{ bizType: ... }` —— 两边对不上,占位符替换不上,
> 界面上出现的是字面量「验证码已发送({scene})」。
> **写测试的时候正好读了一遍字典模板,顺手发现两边对不上**(改完先 grep 再跑测试)。
> 现在那条用例把 `not.toContain('{scene}')` 钉住了,以后再对不上会红。
> ⚠️ 这正是「改字段必须同 commit 改 server + web + doc」那条纪律的一个变种:
> **改的是 i18n 占位符的名字,也得同 commit 改调用点** —— 只是这次"doc"那一侧
> 变成了"字典",而字典和调用点在同一个前端仓里,grep 一下就能发现。

> ⚠️ **探针自己踩过一个坑(已修)**:探针 ⑤ 一开始把 `if (mode === 'guard')`
> 改成 `if (mode !== 'delegate')` —— 那个改法**恰好等价于原逻辑**(delegate 还是不验),
> 于是探针"绿了",差点被当成"这条修复不承重"。改成 `mode === 'guard' || mode === 'delegate'`
> (= 照抄 M104 之前"一律本地验"的行为)才真的打红。
> **"探针打不中"和"测试没红"是两回事**,前者更危险 —— 它会让人以为这条修复是多余的。

> ⚠️ **一条 Windows 专属的坑(踩过)**:Go 侧的 `go test -run 'A|B'` **不能开 `shell: true`** ——
> cmd.exe 会把 `|` 当管道,命令变成 `go test ... -run A|B` → cmd 去找一个叫 `B` 的可执行文件 →
> 报「'B' 不是内部或外部命令」,**退出码非 0**。看起来像"测试红了",
> 其实是**命令根本没跑起来**(假红,比假绿更坑)。探针里改用不带 shell 的 `execFileSync`。

**§2.7 子项数不变(16 项,14 完成 / 2 还开着,仍 88%)** —— M104 是对 #7 / #8 的
**重写 + 加固**,不是新增子项。

### 2.7.2 图形验证码 / 短信链路审计(2026-10-28,M106)

**起因**:§1.6 标着 100%("登录锁 / 验证码 ✅ 完全完成")、§2.7 标着 88% —— 但
**图形验证码这条链路的前端一半一条用例都没有**:

- `components/common/__tests__/` 下 5 个文件,**没有 CaptchaField**;
- `stores/publicConfig.ts` —— 决定"登录页渲不渲染验证码框"的那个 store —— **零覆盖**;
- §1.6 里那个"图形验证码 ✅"记的是**后端**那半(`service/captcha.go` +
  `api/handler/v1/captcha.go`),前端这半从来没人读过。

后端那半是完备的。**前端这半有一个 P0:同一张图形验证码被消费两次。**

**P0 机理**(逐行核实的,不是推演):

后端 `captchaStore.Verify(id, answer, clear=true)` → `Get(id, true)` 里
**先 `delete(s.data, id)` 再比对**(`server/internal/service/captcha.go:44-61`)
⇒ **无论验证码对不对,这个 id 都已经被从服务端 store 里删掉了** ⇒ 严格单次性。

而短信流程里有**两个**受验证码保护的请求,**scene 还不同**:

| # | 请求 | scene | 受哪个开关管 |
|---|---|---|---|
| ① | `POST /public/auth/sms/send` | `sms_send` | `captcha.sms_send_enabled` |
| ② | `POST /public/auth/login-sms` | `login` | `captcha.login_enabled` |
| ②'| `POST /public/auth/reset-password-sms` | `login` | `captcha.login_enabled` |

修复前,页面把**同一个** `{id, code}` 先后发给 ① 和 ②
⇒ ② 拿到的是一张**已经被 ① 消费掉**的验证码
⇒ 「图形验证码错误或已过期」⇒ **短信登录 / 短信重置密码 100% 走不通**。

> ⚠️ **只在 `captcha.login_enabled` 与 `captcha.sms_send_enabled` 同时为 true 时发作** ——
> 而那正是 `configs/config.example.yaml:215-217` **模板里的默认值**
> (`login_enabled: true` + `sms_send_enabled: true`);
> 本地 configs/config.dev.yaml:128-130 把 `login_enabled` **关掉了**
> (见 memory:`captcha-split-toggle-2026-10-21`),所以
> **本地测不出来、按模板部署就炸** —— 这是"模板与 dev 配置不对称"的典型坑。

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `components/common/SmsSendButton.tsx` | 没有"这次请求带走了验证码"的出口 —— 组件只管发短信,不知道图形验证码是**一次性**的 | **P0**:父页面无从得知该换图,必然重放一张已作废的验证码 |
| B2 | `pages/login/index.tsx`(SMS tab)/ `pages/forgot-password/index.tsx` | 两条短信流程各自都要过**两道**验证码闸门(① 发码 + ② 提交),页面却从头到尾用同一张图 | **P0**:② 必失败。短信登录 / 短信重置密码**两条路都是死的** |
| B3 | `components/common/CaptchaField.tsx` 的渲染分支 | `loading \|\| !image ? <Spin/> : <img/>` —— 拉取失败后 `image` 一直是空串 | **P1**:界面**永远转圈**,用户看到的是"还在加载"而请求早就断了;没有报错、没有重试提示,唯一出路是刷新整页 |
| B4 | `stores/publicConfig.ts` | 三态语义(未知按"开")是对的,但**零覆盖** | 这条**没改**(是对的),但它是"决定渲不渲染验证码框"的那个 store —— 判高危的客观标准是**挂在全局路径上吗**,它是 |

**修法**(框架层,零业务代码):

- **B1 →** `SmsSendButton` 加 `onCaptchaConsumed?: () => void` 出口,
  在 `handleClick` 的 `finally` 里调 —— **成功和失败都调**
  (后端先删后比,**失败也消费掉了**这张验证码)。只调成功是漏的:失败那一次
  用户手里也只剩一张废图。
  - 什么时候**不**调:本次请求压根没带 captcha(没什么可换);`beforeSend` 被拒
    (请求根本没发出去,验证码没被消费)。
- **B2 →** 两个页面各接一根线:`onCaptchaConsumed={() => setXxxCaptchaKey(Date.now())}`
  (`refreshKey` 变化 ⇒ `CaptchaField` 重拉 ⇒ 输入框清空、用户重填)。
- **B3 →** `CaptchaField` 加显式 `failed` 态:失败且手里没旧图 ⇒ 渲染"点我重试"的
  `ReloadOutlined`(容器本来就能点),不再永远转圈。
  **失败时不清旧图** —— 这次失败的是"拉新图",服务端手里那张旧验证码**并没有被消费**,
  用户对着旧图输的码仍然有效,清掉等于白白逼用户重填。
- **B4 →** 不改实现,只补网(见下)。

> ⚠️ **后端为什么一行没动**:单次性是**安全属性**(防重放),不是 bug。
> "发短信"这个动作必然消耗掉当前这张验证码 ⇒ 前端刷新是**对的做法**。
> 把后端改成"校验失败不删除"反而开了重放的口子。

**回归网**:**4 份新文件 + 1 份扩写,37 条前端用例**:

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/pages/login/__tests__/smsCaptcha.test.tsx` | 3 | **B2 的端到端网**:发短信用的是当前这张;**发完必须换一张新图**;**提交登录那一步用的是"另一张"**(`expect(used.id).not.toBe(sendCaptcha.id)` —— 断言的是"结构上不可能重放",不是"调用了某个函数") |
| `web/src/pages/forgot-password/__tests__/smsCaptcha.test.tsx` | 1 | **B2 的第二条链路**:找回密码页发完短信也要换图(它是 §2.7.2 B2 表里的 ②') |
| `web/src/components/common/__tests__/smsSendButton.test.tsx` | 11 → 15 | **B1**:发送成功 → 通知换图;发送失败 → **也**通知换图;没带验证码 → 不通知;`beforeSend` 被拒 → 不通知(验证码没被消费) |
| `web/src/components/common/__tests__/captchaField.test.tsx` | 8 | **B3**:失败后**不再永远转圈**(断言 `.ant-spin` 不存在);失败后显示"点我重试"图标;点一下能真的重试;**失败时保留旧图**;两个开关都关时一次请求都不打 |
| `web/src/stores/__tests__/publicConfig.store.test.ts` | 10 | **B4**:三态语义(初始 null 按"开");后端说关 → 真关;登录关/发短信开 → 短信流程**仍要**验证码;**拉失败 → 保持 null(不把校验"意外关掉")**;字段缺失 → 按"开";字段类型是字符串 `"false"` → 仍按"开";"失败方向要选烦人的那个" |

**可逆探针**:scripts/probe_m106.mjs,**8 条探针,实测全部红在指定用例上,还原后全绿**
(探针合计让 13 条用例变红)。其中 ⑦ ⑧ 是**覆盖网探针**(publicConfig 那个 store
本轮**没改**,只是第一次被测到 —— 探针证明"改错了会被新网抓住")。

> ⚠️ **断言写法上的两个坑**(老规矩,这里是新踩的):
> - antd 5 的 Button 会在两个汉字之间**自动插空格**("登录" → "登 录")
>   ⇒ 匹配要用 `/登\s*录/`,精确字符串找不到。
> - antd 5 的 **Tabs 懒挂载** ⇒ 登录页初始在"账号密码" tab,那个 tab 里也有一张
>   CaptchaField,切到"短信登录" tab 会**再挂一张** ⇒ 短信 tab 用的是**第 2 张**图。
>   **验证码 id 序号绝不能写死** —— 写死会变成一条**假网**(它测的是"恰好第 2 次拉图",
>   不是"换了一张新图")。用例改成动态读 mock 的调用记录。

> ⚠️ **探针锚点要容忍 CRLF**(踩过):Windows 上这些源文件是 CRLF,
> 锚点里的 `\n` 换成 `\r?\n` 才打得到 —— 否则脚本报"探针没打中"。
> **"探针没打中"和"测试没红"是两回事**,前者更危险(它会让人以为这条修复不承重)。

**顺带修的测试基建问题:全量跑时的"假红"超时**(不是本轮引入的,但挡住了本轮的检查)

现象:全量跑(`npx vitest run`,51 个文件并行)时**随机**有一两条**页面级**用例报
`Test timed out` —— 而**单独跑同一个文件是绿的**。

| | 用例数 | 单独跑耗时 | 最慢的单条 |
|---|---|---|---|
| `src/pages/system/user/__tests__/index.test.tsx` | 11 | 61s | 8s |
| `src/pages/system/role/__tests__/AssignResourcesDrawer.test.tsx` | 7 | 30s | — |

根因**不是用例写得笨**:jsdom 里 antd 的 CSS-motion / 焦点陷阱 / rc-select 都要靠
**真定时器**推进,而 `@testing-library` 的 `waitFor` 又是按**真实时间**轮询 ——
**机器一忙,每一步都跟着变慢**。而超时上限是 vitest 默认的 **5s**
(`AssignResourcesDrawer` 那份压根没写超时),另 4 个文件各自写着 `}, 30000)`。
上限比实测耗时还紧 ⇒ 全量并行时必然撞线。

修法(**超时收口成唯一真源**):

- `web/vitest.config.ts` 加 `testTimeout: 60_000` + `hookTimeout: 60_000`
  (60s = 实测最差 46s 再留 30% 余量);
- 删掉 4 个文件里 6 处冗余的 `}, 30000)` 覆盖(approval / flow / permission / user×3)
  —— **它们会盖掉全局值**,全局改了也没用。现在**超时只有一个真源,要调就调 config**。

> ⚠️ **踩过的一个岔路**:先怀疑是"并行度太高"⇒ 试了 `--maxWorkers=8`。
> 结果是**墙钟时间更长**(485s vs 317s),而且失败的用例**换了一条** ——
> 说明这不是"并发抢资源"能解决的,就是**超时上限本身定得太紧**。
> ⚠️ 顺带记一个 vitest 的坑:**只传 `--maxWorkers` 不传 `--minWorkers` 会直接报
> `options.minThreads and options.maxThreads must not conflict`**,
> 然后**跑出 "no tests" + 退出码 0** —— **假绿**,比假红更坑
> (和 `probe_m101.mjs` 里"0 个用例 = 抛错"的守门网是同一个道理)。

**§2.7 子项数不变(16 项,14 完成 / 2 还开着,仍 88%)** —— M106 是对 #9(CaptchaField)
的**加固 + 补网**,不是新增子项。

### 2.7.3 展示 / 错误态组件审计(2026-10-29,M111)

**起因**:M110 修 `mask.*` 的时候顺带发现,`SensitiveField` 这条脱敏链**一条用例都没有** ——
`mask.*` 有 30 条用例(纯函数),但**把它包起来、决定"要不要脱敏 / 脱成什么样"的组件本身零覆盖**。
同一批清点的还有 `ViewDrawer` / `SearchSection` / `TableErrorState` / `FallbackPlaceholder`:
前两个是 §2.7 里"详情页怎么显示"的一环(**业务页 12 个 ViewDrawer 调用方全靠它**),
后两个的清单条目在 **§2.5**(错误处理链的最后一环),本轮一并补网。

M111 把这五个读了一遍,读出 **2 个真 bug + 2 个死 prop**:

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| C1 | `SensitiveField` 的 `type='auto'` 分支 | 旧顺序是"11 位手机 → 18 位身份证 → 含 `@` 邮箱" | **恰好 18 个字符的邮箱**会先撞上"18 位身份证"分支,被脱成 `zhangs********.com`(身份证样式)而不是 `z***@exam.com`(邮箱样式)。用户看到的是一串星号 —— 既认不出是邮箱,**又保留了域名的形状**,脱敏反而泄露了信息。`@` 是**最确定的信号**(不可能出现在手机号 / 身份证里),必须放第一位判断 |
| C2 | `SensitiveField` 的两个眼睛按钮 | 按钮只有 `<EyeOutlined />` 图标,**没有可访问名** | 读屏软件只能念"按钮",用户不知道按下去是"看明文"还是"隐藏"。两个按钮图标相反、语义相反,却没有名字区分 |
| C3 | `ViewField.span` | 类型是 `number`,注释写"占一整行(默认 true)"—— 两处都不对;而这个抽屉的 `Descriptions` **column 固定为 1** | 这个 prop **没有任何作用**(每个字段本来就占满一行);传 `span={2}` 还会让 antd 判定"span 之和与 column 不匹配"并在控制台打 dev 警告(antd 的 Descriptions `useRow`)。12 个调用方**零个**传过它 ⇒ **死 prop**,收回 |
| C4 | `SearchSection.fullWidth` | 类型声明里有,组件体**从来没读过它** | 容器本来就是 `display:flex`,子元素自己决定宽度,没有"占满整宽"这回事。两个调用方(ManagementTable / system/config)也都没传过 ⇒ **死 prop**,收回 |

**修法**:

- **C1 →** `auto` 分支改成**先判 `@`**,再判 11 位手机、18 位身份证,最后兜底姓名。
- **C2 →** 两个眼睛按钮各加 `aria-label`(`component.sensitiveField.viewPlain` / `.hide`)。
- **C3 →** 删掉 `ViewField.span` 字段和 `Descriptions.Item` 上的 `span` 属性(TS 层面挡住误用),
  理由同时写在文件头。
- **C4 →** 删掉 `SearchSectionProps.fullWidth`,理由同时写在文件头。

**`TableErrorState` / `FallbackPlaceholder` 本轮没有改动** —— 只补了回归网。
它们是 §2.5 错误处理链的**最后一环**(M7 落的),此前**一条用例都没有**:
两种形态(`empty` 占位 / `strip` 提示条)、`strip` 必须是 **warning 级**(旧数据还在,不是错误级)、
`FallbackPlaceholder` 文案走 i18n、`error` 是 Error 对象还是字符串、有没有 `onRetry` ——
这些以前全靠"看一眼觉得对"。现在锁住了。

**回归网**(本轮 §2.7 新增 **4 份用例,41 条**):

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/components/common/__tests__/sensitiveField.test.tsx` | 17 | 四种显式 `type` 的脱敏形态 / auto 自动识别 / **C1**:恰好 18 个字符的邮箱按邮箱脱敏 / **C2**:眼睛按钮有可访问名,明文态下叫"隐藏" / 权限优先于 `defaultVisible` / 无值时不给眼睛按钮 |
| `web/src/components/common/__tests__/tableErrorState.test.tsx` | 12 | `TableErrorState` 两种形态 / `strip` 是 warning 级 / 无 `onRetry` 时不给按钮 / `FallbackPlaceholder` 文案走 i18n(英文环境下不能是中文硬编码)/ `error` 是 Error 对象还是字符串 / `variant` 决定图标 / `height` 决定最小高度 |
| `web/src/components/common/__tests__/viewDrawer.test.tsx` | 8 | 开关 / label + 值 / 空值占位 / `data` 为 null 降级成空状态 / `render` 回调拿到 `(原始值, 整行数据)` / `extra` / 关闭回调 / **C3**:不读 `span`(不触发 antd 的 span 不匹配警告) |
| `web/src/components/common/__tests__/searchSection.test.tsx` | 4 | 渲染子元素 / `search-section` 类名(ManagementTable 的用例靠它认"同一行")/ hover 升级 L2 阴影、移开回 L1 / `style` 追加在默认样式之后(调用方能覆盖) |

**可逆探针**:C1 / C2 / C3 三条各有一条探针(见 scripts/probe_m111.mjs ⑫ ⑬ ⑭),
拆掉修复看对应用例会不会红 —— **全红**,还原后全绿。
C4 是"收回 prop",没有行为变化 ⇒ 不需要探针(它守的是 TS 类型,编译期就红)。

**§2.7 子项数不变(16 项,14 完成 / 2 还开着,仍 88%)** —— M111 是对 #2 / #4 / #5 的
**修正 + 补网**,不是新增子项。

---

## 2.8 ✅ 表格 / 布局 / 路由(100%)

#### ✅ 已完成(12 个)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | ManagementTable(官方 auto-height 写法) | `components/common/ManagementTable.tsx:1-194` | 194 |
| 2 | DataTable(轻量版) | `components/common/DataTable.tsx:1-54` | 54 |
| 3 | BasicLayout(白系 + 毛玻璃 + flex 滚动) | `components/layout/BasicLayout.tsx:1-106` | 106 |
| 4 | Header(品牌 + IdentitySwitcher + 全屏) | `components/layout/Header.tsx:1-168` | 168 |
| 5 | Sidebar(动态菜单 + iconMap + 选中态) | `components/layout/Sidebar.tsx:1-190` | 190 |
| 6 | Breadcrumb(最长前缀匹配 + 子页兜底) | `components/layout/Breadcrumb.tsx:1-68` | 68 |
| 7 | 菜单 ↔ 路由对齐(**不是** meta 驱动) | 🔴 **M103 修正**:前端路由表**根本没有 `meta` 字段**(`router/routes.tsx` 全文 0 处 `meta`)—— 菜单是**后端 `sys_resource` 驱动**的(`usePermissionStore.menus`),两边靠 #11 那条守门网对齐,不是靠路由 meta。见 §2.8.1 B4 | - |
| 8 | 404 / 403 错误页 | `web/src/pages/error/{403,404,500}.tsx` | - |
| 9 | 路由懒加载 | `routes.tsx` React.lazy | - |
| 10 | BatchActionBar(批量操作条) | `components/common/BatchActionBar.tsx:1-69`(M5b) | 69 |
| 11 | 菜单 ↔ 路由对齐守门网 | `router/__tests__/menu-route-parity.test.ts`(解析后端 `seed.go` 的菜单 path,逐条比对前端路由表;后端有菜单、前端没路由 → 立刻红) | 108 |
| 12 | ✅ **拖拽排序**(**M174,2026-11-03 已落地**) | `hooks/useDragSort.ts`(**零新依赖**,原生 HTML5 DnD)+ 纯函数 `reorder`(摘后插语义,越界 clamp,from===to 返原引用)。**核心状态机:拖起即快照出"工作副本",真身在 drop 前一个字不动** —— dragover 实时改预览(拖到哪列表让位到哪),drop 提交预览 + onSort 一次,cancel/dragend 未命中直接丢预览还原。**拖拽态真身在 ref、state 做镜像**:HTML5 DnD 里 drop 之后浏览器**无渲染间隔**立刻派发 dragend,只存 state 会把"已放置"误判成"未放置"再取消一次。rowProps 内部兜住两个必踩坑(dragover 不 preventDefault 则 drop 永不发生;dragstart 不 setData 则 Firefox 不发起拖拽)。受控(items 在调用方,drop 只回调)/ 非受控(hook 自持序)双模;canDrag 前置门;onSort 不吞错。**测试 22 条**(reorder 4 / 状态机 11 / DOM 集成 7 —— jsdom 不实现 DnD 默认行为,手动 `dispatchEvent` 走 React 合成事件 + 手工挂假 dataTransfer);**2 条可逆探针实测转红→复原→绿**(探针 A:drop 提交换 origin(预览当没发生)⇒ 4 条红;探针 B:dragover 索引改对 origin 算 ⇒ 「连续经过两个目标」恰好 1 条红)。主会话复验:探针 A 独立重打,同样 4 条红 | 中 | ~~P2~~ → 已收 |

> **2026-10-26 修**:`/issue/list` 这条路由以前**根本不存在** ——
> 后端 seed 里 `menu.issue.list` 的 path 是 `/issue/list`(2026-10-18 commit 82fd624 改的,
> 之前和 `menu.issue` 一样都是 `/issue`),侧边栏点「问题列表」跳过去就落到
> `path: '*'` 兜底 → **404 页**。commit `01c0d2e` 补上路由 + 守门网。
> ⚠️ 菜单和路由**必须同 commit 改**:只改一边就会出现这种"点菜单进不去"的洞。

#### ❌ 未完成(0 项)

> **范围外,不做**(2026-11-03 用户拍板):原 #20 **移动端适配** —— PC 后台,移动端不是当前目标,
> **移出账本不进计数**(此前 "当前只做 PC" 的设计决策维持,见 §0 历史记录)。
> 哪天出现真实的手机使用场景(如"领导用手机审批")再重新立项。

| # | 子项 | 当前缺口 | 价值 | 优先级 |
|---|---|---|---|---|
| 10 | 表格虚拟滚动(大数据量) | 🔴 **M111 修正:这条以前是假的** —— M56 写的"Antd 5 内置虚拟滚动生效"**从来没生效过**:antd 5 的虚拟滚动是**显式开关**(`virtual: true`),旧 hook 只设了 `scroll.y` ⇒ 1000 行就是 1000 个 DOM 节点。现在真接上了(`tableProps.virtual`),且满足 rc-table 的硬约束:**虚拟模式下 `scroll.x` 必须是数字**(非数字会 warning 并**塌成 1px 宽`)⇒ 取可见列宽之和;与树形模式互斥(展开后行数会变,固定行高会错位)。见 §2.8.2 B1 | P2 ✅ |
| 11 | 列设置 / 列宽记忆 | ✅ M56:`useTablePro` 列状态(visible+width)存 localStorage(`tablepro:<storageKey>` JSON);新列自动补;`ColumnSettings` Modal UI(checkbox 显隐 + InputNumber 调宽 + 恢复默认);key 不匹配自动过滤。🔴 **M111 修正**:换 `storageKey` 不重读存储(状态串到下一张表且永远回不去)/ `hideable:false` 的列能被旧存储永久藏起(而面板里它的复选框是禁用的 ⇒ 用户没法勾回来)/ 面板与表格对"不可隐藏"的判断不一致。见 §2.8.2 B5、B6 | P2 ✅ |
| 13 | 导出 / 打印 | ✅ M56:`useTablePro.exportCSV(filename?)` 纯前端 Blob+download;BOM 头保 Excel 中文不乱码;CSV escape(逗号/引号/换行自动加引号);支持 nested dataIndex 数组。🔴 **M111 修正**:表头是 ReactNode 时导出成 **`[object Object]`** / 用户数据以 `=`/`+`/`-`/`@` 开头时**原样导出**(Excel 当公式执行)/ 锚点没挂到 document 上(部分浏览器点了没反应)。见 §2.8.2 B2、B3、B7 | P1 ✅ |
| 15 | 树形表格 | ✅ M56:`useTablePro({ treeMode: true })` 自动接 Antd Table expandable,rowExpandable 判断 children 非空,defaultExpandAll。🔴 **M111**:与虚拟滚动**互斥**(展开后行数会变,固定行高的虚拟列表会错位)⇒ 树形优先(关虚拟),见 §2.8.2 B1 | P2 ✅ |
| 14 | 行选择 + 批量操作 | ✅ M5b:`useTableSelection` hook(行选择状态机 + **跨页选择不被 antd 静默吃掉**)+ `BatchActionBar` 组件("已选 N 项" + 取消选择);用户管理页已迁出自己写的 `selectedIds` + 手搓 `rowSelection` | 中 | P1 ✅ |
| 16 | ✅ 可编辑表格(**M171,2026-11-02 已落地**) | `hooks/useTableEdit.ts`(行内编辑状态机)+ `components/common/EditableCell.tsx`(视图/编辑器切换单元格)。**业务页只写"哪个字段用什么编辑器 + 怎么落库",状态机全在框架层** —— 四个它替业务页踩掉的坑:①**保存竞态用代际计数裁决**:保存飞着时用户取消/换行,迟到的成败回调**无权**再动编辑态(裸 useState 必把新行的编辑态关掉);②**草稿是快照不是活引用** —— startEdit 那一刻浅拷贝,轮询/SSE 重拉不许冲掉正在编辑的值,落库 patch **只含真改过的字段**(整行覆盖 = 用旧值顶回别人的改动);③**编辑中的行从 dataSource 消失 ⇒ 自动收摊**(继续编辑不存在的行,保存必败且看不懂);④**失败不吞**:saveEdit 抛原异常、编辑态与草稿原样保留(可直接改完再存),toast 归调用方。EditableCell 用 cloneElement 注入 value/onChange(编辑器类型随便换,antd 控件与原生 input 都认 —— event.target.value 提取与 antd Form 默认 getValueFromEvent 同口径)。**测试 16 条**(状态机 13 + 组件 3);**2 条可逆探针实测转红→复原→绿**(拆代际守卫 ⇒ 「迟到的成功」红;失败路径清编辑态 ⇒ 「迟到的失败」红)。 | 中 | ~~P2~~ → 已收 |
| 18 | 多 tab 页签 | ✅ M17:`stores/tabs.ts` tab stack + scrollY/formState keepAlive;路由 `meta.keepAlive` 启停(commit 95f020b)🔴 **M103 修正**:`meta.keepAlive` **这条路从来不存在**(路由表没有 `meta` 字段)—— 真正活着的是 `stores/tabs.ts` 的**按 path 软缓存**(scrollY / formState);2026-10-25 多 tab 标签条下线后只剩这一件事,见 §2.8.1 B5 | P1 ✅ |
| 19 | 主题切换(亮/暗) | ✅ M16:`useUIStore.theme` light/dark;`ConfigProvider algorithm` + CSS 变量覆盖;Header 顶栏切换按钮(commit 97e7a20) | P1 ✅ |
| 21 | ✅ **保存的列表视图**(筛选 + 排序 + 列宽 打包存下来)(**M176,2026-11-03 已落地**) | `hooks/useSavedViews.ts`:通用命名快照 hook —— save/apply/remove/rename/clearActive,`state` 泛型由调用方组装(筛选+排序+列状态一起打包),localStorage `savedview:<storageKey>`。健壮性全有测试:JSON 损坏重置 / 坏元素过滤 / **storageKey 变化重读**(M111 同款坑有专门用例)/ 写失败内存态可用 / `viewsRef` 同步镜像防连续写互顶 / 同名覆盖=原位替换保留原 id 与 createdAt / 超 max(20)从最旧端挤。21 用例 + 可逆探针(sanitizeViews 四条守卫拆掉 → 3 条红 → 恢复绿)。业务页接入 = 传 storageKey + 把当前视图状态交给 save/apply,框架层不碰业务页 | 中 | P2 ✅ |

> **复用价值**:**高**

### 2.8.1 布局 / 路由壳层审计(2026-10-28,M103)

**起因**:§2.8 标着 86% —— 但**壳层**这一层几乎零覆盖:
`components/layout/__tests__/` 下只有 `pageName.test.ts` 这一个**纯函数**文件 ——
Sidebar / Header / BasicLayout / Breadcrumb / **IdentitySwitcher** 一条用例都没有,
而这四个组件**挂在每一个路由上**;`constants/roles.ts` **零覆盖**
(`constants/__tests__/` 目录当时根本不存在);`stores/tabs.ts` 有 7 条用例,
但只测了 open / patchCache 的"正常路径",没测"撑不撑得住"。

> 判高危的客观标准还是"**挂在全局路径上吗**"(见 memory:框架原语零覆盖)。

M103 把 §2.8 的壳层逐个读了一遍,读出 **5 个真问题**:

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `components/layout/Sidebar.tsx` | 菜单展开状态只给了 `defaultOpenKeys` | antd Menu → rc-menu `useMergedState(defaultOpenKeys, { value: openKeys })` → rc-util 的 `useControlledState` **只在挂载那一刻**用默认值初始化内部 state,之后 `defaultOpenKeys` 再变也不跟随。**实测复现**:从 `/system/user` 切到 `/owner/list` 之后,「业主管理」这一组**不展开** —— 当前页在侧边栏里根本看不见,用户看到的是"菜单丢了",**不报错、不白屏** |
| B2 | `constants/roles.ts` | 前端那套 role code **10/13 个对不上后端**(`role_platform_super_admin` / `role_business_lead` / `role_house_manager` / `role_material_reviewer` 之类) | 这些 code **后端从来没签发过**。写错的后果不是"名字显示不对",而是**三个真功能同时死掉**:① **高敏身份切换的短信二次验证**(`IdentitySwitcher` 的 `HIGH_RISK_ROLES`)—— 真超管不在名单里 → **切超管身份免二次验证**(这是安全洞,不是显示问题);② 用户列表的「重置密码」行内按钮(`pages/system/user/index.tsx:273` 的 `roleAnyOf: [RoleCode.SUPER_ADMIN]`)—— `hasRole` 恒 false → **这个按钮对谁都渲染不出来**(功能整个死掉);③ 路由守卫的「或超管」兜底(`routes.tsx` 里 **13 处** `anyOf`)—— 同样恒不命中 |
| B3 | `router/guards.tsx` `RoutePermissionGuard` | 守卫只查 `permissionCodes`,**不查 `roleCodes`** | 角色 code 与权限码是两套**不相交**的命名空间(`role_*` vs `user:list` / `button.*`),`routes.tsx` 那 13 处 `anyOf={[PermSystem.USER_LIST, RoleCode.SUPER_ADMIN]}` 语义是"有权限**或**是超管" —— 那一支**恒不命中**。B2 修好之前是"**双重恒 false**";B2 修好之后这里不改,就变成"**改对了一半**":超管仍然进不了那些页面 |
| B4 | 清单 §2.8 #7 / #18 | 写着"路由 `meta` 驱动菜单" / "路由 `meta.keepAlive` 启停" | `web/src/router/routes.tsx` 全文 **0 处 `meta`** —— 这两条描述的功能**从来不存在**。真的机制是:菜单由**后端 `sys_resource`** 驱动(`usePermissionStore.menus`),两边靠 §2.8 #11 那条守门网对齐;软缓存是 `stores/tabs.ts` 的**按 path 记录**,**没有开关** |
| B5 | `stores/tabs.ts` `open()` | 缓存槽位**无上界**,而且整个列表 persist 到 localStorage | `open(key)` 的 key 就是 pathname,而路由表里有**动态路由**(`flow/:id` / `flow/:id/stage/:stageId` / `approval/:id`)—— 每打开一张不同的审批单就是一条**永远不会被回收**的槽位。逛一天审批,localStorage 里堆几百条 `{key, cache:{}}`;每次渲染 BasicLayout 还要 `tabs.find(...)` 线性扫一条越来越长的数组(**只增不减的写放大**) |

**修法**(全在框架层,零业务代码):

- **B1 →** Sidebar 自己持有 `openKeys` state:路由变化时把当前页的祖先 key **并进去**
  (不是替换 —— 替换会把用户手动展开的其它分组一起收掉),并把 `onOpenChange` 接上
  (受控模式下不接的话用户点不动菜单)。
- **B2 →** `constants/roles.ts` **按后端三个互相印证的来源重写**:
  `server/internal/model/enums.go` 的常量定义、`server/internal/seed/seed.go` 的 `roleList()`
  (真正落库的那 **13** 个)、`docs/后台权限模型设计V1.0.md` §1499。
  `ROLE_NAME_MAP` 同步换成 **i18n key**(不是文案)。
  - ⚠️ `model/enums.go` 里还有第 14 个常量 `role_auditor` —— 它是**兼容旧名的别名,
    从来没被 seed 过**(后端 seed 测试里专门断言了这一点),所以前端**没有** `AUDITOR`。
- **B2 的另一面 →** `IdentitySwitcher` 的角色名以前是 `ROLE_NAME_MAP[code] || role_name` ——
  `ROLE_NAME_MAP` 的值是 **i18n key**(`role.superAdmin`),**不是文案**。以前查表几乎次次落空
  (前端那套 code 是错的)→ 一直回落到后端返回的中文 `role_name`,**反而"看着正常"**;
  B2 修对之后这个洞才露出来 —— 用户会看到裸的 `role.superAdmin`。改成统一走 `t(key)`。
- **B3 →** `satisfied = (code) => has(code) || hasRole(code)`。
- **B5 →** 上限 `MAX_CACHED_PATHS = 100`:超限时**优先淘汰"没有缓存内容"的那一条**
  (它的 cache 是空的,淘汰等于零损失);只有在**每一条都有缓存**时才淘汰最旧的那条
  (无脑淘汰最旧的,有可能正好把"刚离开的那一页"的滚动位置写丢)。
  rehydrate 时**截断到上限** —— 修好上限之前已经写进 localStorage 的几百条旧槽位,
  光加上限不会自动清理它们。

**顺带修掉的**:`src/mock/index.ts` 的角色名写死中文 —— 撞了字典里 `role.superAdmin`
的文案,触发「写死中文 i18n 守门网」。改成从字典取(`tKey`)。
同一处还发现 mock 的 `roleCode` 是 `super_admin` —— **后端从来没签发过**这个 code,
跟 B2 是同一类错。

**主动收回的两项**(零调用方;理由同时写在代码里):

| 收回的东西 | 为什么 |
|---|---|
| `CONFIG_CENTER_ALLOWED_ROLES` | 零调用方,而且**语义是错的**:配置中心的准入不是"这三个角色",而是 RBAC 资源 `config:*` **只绑给 `role_super_admin`** 一个角色(见后端 seed + 角色资源绑定矩阵)。留着它等于给后来人一个"改这个数组就能改配置中心准入"的错觉 |
| `ROLE_TYPE_MAP`(platform / business 分类) | 零调用方:分类的权威在后端 `sys_role.type` 列。前端要分类时应该直接读后端字段,而不是在前端再维护一份**可能漂移**的映射表 —— 这正是 B2 的成因模式 |

**记下来的死码 / 空壳**(M103 **没有删** —— 超出本轮范围,记下来是为了别再"顺手改坏"):

| 东西 | 现状 |
|---|---|
| `src/mock/index.ts` | `installMock()` 挂在 `main.tsx:24` 上,但函数体只有一句 `console.info` —— **没有覆盖任何 axios adapter**。`.env.development` 里 `VITE_USE_MOCK=true`、README 写着"启用 mock",但**开发期请求照样打真后端**。`mocks` / `matchMock` / `mockResponse` 三个导出零调用方 |
| `components/common/{DataTable,TablePro,ColumnSettings}.tsx` + `hooks/useTablePro.ts` | 都在 `components/common/index.ts` 的 barrel 里导出,但**没有业务页 import**(`useTablePro` 只被 `ColumnSettings` 当**类型**引用)。是"框架已备好、业务还没接"的公共件 —— **不是 bug**,但清单里给它们标 P1/P2 ✅ 时容易被读成"已经在用" |

**回归网**:**5 份新用例 + 1 份扩写,37 条**(此前 `components/layout/` 只有 `pageName` 一个纯函数文件):

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/components/layout/__tests__/sidebar.test.tsx` | 8 | **B1**:只认 `menu.` 前缀(api / button 资源混进来也不渲染)/ 按 sort 升序 / 父菜单高亮是 `ant-menu-submenu-selected`(**不是** item-selected)/ 换路由选中态跟着换 / **切到另一个顶级菜单后那一组父菜单必须自动展开** / 切走之后原来那组可以收起 |
| `web/src/constants/__tests__/roles.test.ts` | 7 | **B2 的契约网**:**直接解析后端 `model/enums.go` + `seed.go` 的 `roleList()`**,逐条比对前端 `RoleCode` —— 后端加角色 / 改 code,前端立刻红;旧 code 一条都不许残留;`role_auditor` 不许混进来;`ROLE_NAME_MAP` 覆盖全部角色且不多不少;每个角色名在 zh / en 两份 locale 里都查得到(不能 `t()` 落空显示 key) |
| `web/src/router/__tests__/routeGuards.test.tsx` | 7 | **B3**:权限码放行 / 没权限跳 `/forbidden` / `allOf` 要全有;**只有角色码、没有任何权限码也放行** / "有权限**或**是超管"两边各命中一半都放行 / 既没权限又不是超管 → 跳 forbidden / `allOf` 里写角色码也算数 |
| `web/src/components/layout/__tests__/identitySwitcher.test.tsx` | 4 | **B2 的安全面**:切超管身份**必须先过短信二次验证**(超管不在高敏名单 = 安全洞);非高敏角色直接切(不弹窗);角色名显示的是**翻译后的文案**而不是 i18n key(头部 + 下拉两处) |
| `web/src/stores/__tests__/tabs.store.test.ts` | 7 → **11** | 原有 7 条(登记 / 去重 / 不清已有缓存 / scrollY 往返 / 合并而非覆盖 / 打不存在的 path 是空操作 / 多个 path 互相独立)**原样保留**;新增 4 条守 **B5**:超过上限就淘汰 / 优先淘汰没有缓存内容的那条 / 刚开的那条一定在列表里 / rehydrate 把 300 条存量截到 100(保留最新) |

> ⚠️ **一条测试自己也会假绿(已修)**:`identitySwitcher.test.tsx` 原来用 `RoleCode.SUPER_ADMIN`
> **常量**去构造身份 —— 常量一改,身份上的 `role_code` 跟着改,"高敏名单里有没有它"永远是"有",
> 测的就不是"前后端是否一致",而是"常量自己跟自己一致" → **恒绿,探针打不中**。
> 改成**后端字面量** `'role_super_admin'` 之后,探针 ② 才真的能打红。
> 这跟 memory 里"探针自己也会假绿"是同一类 —— **测法要先自证**。

**可逆探针 6/6 全红**(scripts/probe_m103.mjs —— 拆实现看测试会不会红;每条探针跑完**逐字节**校验还原,
还原后再全量跑一遍确认回到绿):

| # | 拆掉什么 | 变红的用例 |
|---|---|---|
| ① | Sidebar 退回 `defaultOpenKeys`(挂载时一次性) | `切到另一个顶级菜单后,那一组父菜单也必须自动展开` |
| ② | `RoleCode.SUPER_ADMIN` 退回 `role_platform_super_admin` | `前端 RoleCode 与后端 roleList() 落库的 code 完全一致` / `那套"后端从来没签发过"的旧 code 一条都不许残留` / `切超管身份必须先过短信二次验证` |
| ③ | 路由守卫退回只查 `has(code)`(不查 `hasRole`) | `只有角色码、没有任何权限码 → 照样放行` / `"有权限 **或** 是超管" —— 两边各命中一半都放行` / `allOf 里写角色码也算数` |
| ④ | IdentitySwitcher 角色名退回"直接渲染 i18n key"(不过 `t()`) | `显示的是翻译后的角色名,不是 i18n key` / `下拉里的角色名也是翻译后的文案` |
| ⑤ | `stores/tabs.ts` 去掉上限 | `超过上限就淘汰,不会无限增长` / `淘汰优先挑"没有缓存内容"的条目(丢了也不亏)` / `rehydrate 时把超上限的存量截断` |
| ⑥ | mock 角色名退回写死中文 | `写死的中文不得与字典里已有文案重复` |

> 6 条探针合计让 **15 条**用例变红(去重后,**实测**不是估)。
> 剩下 **29 条故意是绿的** —— 它们描述的是**没被改动的行为**(菜单排序 / 旧 code 残留检查 /
> `allOf` 语义 / scrollY 往返 / 守门网自检 等)。它们是**对照组**:
> 没有它们,那 15 条可能只是"断言写得太松"。

> ⚠️ **探针自己踩过一个坑(已修)**:第一版探针 ① 只改了 `useMemo` 的 **destructure**,
> 没改 **return 的属性名** → `defaultOpenKeys` 取到 `undefined` → 菜单**一个分组都不展开**,
> 一次红了 5 条(比原 bug 更狠)。**"探针打得更狠"不等于"探针忠实"** ——
> 照抄原版要连**属性名**一起抄。改完 ① 只红 3 条(其中 2 条是"子菜单没渲染 ⇒
> 找不到子项文字"的连带后果),与真 bug 一致。

> ⚠️ **M103 没有碰后端**。前端角色常量改成后端签发的真值之后,
> 对外契约(后端 `sys_role.code` / `roleList()`)**一个字都没动**;
> 变的只有"前端**认不认得**这些 code"。

**§2.8 子项数不变(21 项,18 完成 / 3 还开着,仍 86%)** —— M103 是对 #5 / #7 / #18 的
**加固 + 修正描述**,不是新增子项。

### 2.8.2 表格框架审计(2026-10-29,M111)

**起因**:§2.8 给表格框架打了四个 ✅(#10 虚拟滚动 / #11 列设置 / #13 导出 / #15 树形),
但**这一层一条用例都没有**:

- `web/src/hooks/__tests__/` 下**没有 `useTablePro`**;
- `web/src/components/common/__tests__/` 下**没有 `TablePro` / `ColumnSettings` / `DataTable`**;
- M103 已经把这一层记成"框架已备好、业务还没接"的公共件 ——
  **零调用方 + 零用例 = 契约不存在**。清单却给它们打了 ✅,
  于是"**已实现**"和"**能跑**"被混为一谈。

⚠️ 判高危的客观标准还是"**挂在全局路径上吗**"(见 memory:框架原语零覆盖)。
这一层**暂时没挂在任何业务页上**(所以整体是 P2 不是 P0),但它是
§2.8 #10/#11/#13/#15 四条能力**唯一的实现体** —— 清单打 ✅,实现体却空转。

M111 把这一层逐行读了一遍,读出 **8 个真问题**:

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `hooks/useTablePro.ts` | 虚拟滚动**从来没打开过** —— antd 5 的虚拟滚动是**显式开关**(`virtual: true`),旧 hook 只设了 `scroll.y` | §2.8 #10 承诺的"超阈值自动启用"**是假的**:1000 行就是 1000 个 DOM 节点,虚拟滚动要解决的那个问题一个都没解决。而且 rc-table 的 `VirtualTable` 要求 **`scroll.x` 必须是数字**(非数字会 warning 并**塌成 1px 宽**)⇒ 只补 `virtual: true` 会让表格宽度变成 1px,比不做还糟 |
| B2 | `useTablePro.exportCSV` | 表头写的是 `escape(c.title)`,而 `title` 的类型是 `React.ReactNode` | 只要列标题不是纯字符串(带图标 / 加粗 / i18n 组件),导出的表头就是 **`[object Object]`**,Excel 里完全没法看 |
| B3 | `useTablePro.exportCSV` | 单元格只转义了逗号 / 引号 / 换行 | 以 `=` / `+` / `-` / `@` 开头的**字符串**被 Excel 当成**公式执行**(`=cmd\|'/c calc'!A1` 这类)。导出的是业务数据(备注 / 昵称 / 标题都可能由用户输入)⇒ 别人双击打开就可能被执行 |
| B4 | `useTablePro` + `TablePro` | hook 的 `rowKey` 参数**取了不用**(`_rowKey`);`TablePro` 里 `useMemo` 又把**函数形式**静默丢掉(`typeof rowKey === 'string' ? rowKey : undefined`) | 类型**明明声明了**函数形式 ⇒ TS 不报错,只有运行时才坏:所有行的 React key 变成 `undefined` ⇒ 勾选状态串行 / 分页回来行错位 / 编辑抽屉带错行 |
| B5 | `useTablePro` + `ColumnSettings` | `hideable: false` 的列,表格侧只看存储里的 `visible`,面板侧复选框却是**禁用的** | 旧版本 localStorage 把它存成 `visible: false` 之后,这一列**永久消失**,而用户**没法勾回来**(复选框禁用)—— 只有"恢复默认"能救,而很多人根本不知道那个按钮在哪 |
| B6 | `useTablePro` | 只有 `useState` 的初始化器读一次 localStorage | 同一个组件实例换 `storageKey`(比如按项目切表格)之后,列状态还停在上一个表格上 —— 而且**永远回不去**(只有刷新页面才重置) |
| B7 | `useTablePro.exportCSV` | `a.click()` 直接调,锚点**没挂到 document 上** | 游离元素上的 click 在部分浏览器(尤其 Firefox)不触发下载 ⇒ 表现是"点了导出按钮,什么也没发生",**且不报错** |
| B8 | `DataTable` | `locale` 写在 `<Table {...rest} {...tableProps} locale={...} />` 的**最后**,没出错时值是 `undefined` | **把 `rest.locale` 覆盖成了 undefined** ⇒ `<DataTable locale={{ emptyText: '自定义' }} />` 静默失效 |

**修法**(全在框架层,零业务代码):

- **B1 →** `tableProps.virtual = !treeMode && dataSource.length > virtualThreshold`;
  虚拟模式下 `scroll.x` 改成**可见列宽之和**(数字),非虚拟模式保持 `'max-content'`;
  **虚拟滚动与树形模式互斥**(展开后行数会变,固定行高的虚拟列表会错位)⇒ 树形优先。
- **B2 →** 新增 `nodeToText()`:按节点类型逐层取文本(字符串 / 数字 / 数组 / `props.children`)。
- **B3 →** `csvCell()` 加一档:**只对字符串**加 `'` 前缀 —— 数字 `-5` 不加,
  否则 Excel 里负数变文本(左对齐、不能求和)。
- **B4 →** hook 把 `rowKey` **原样透出**到 `tableProps.rowKey`,`TablePro` 直接展开给 `<Table>` —— 一处真源。
- **B5 →** 表格侧 `if (!s.visible && c.hideable !== false) return null`;`setVisible` 对不可隐藏的列无效;
  面板侧 `checked={hideable ? s.visible : true}` —— **面板与表格必须一致**。
- **B6 →** `storageKeyRef` 记住"上一次读的是哪个 key",只在 key 真的变了才重读。
- **B7 →** 锚点 `appendChild → click → removeChild`,`revokeObjectURL` 延迟 100ms
  (立刻 revoke 会打断正在进行的下载)。
- **B8 →** 把 `locale` 从 `rest` 里**摘出来**,没出错时原样还给 antd;出错且无数据时错误态占位优先。

**顺带修掉的**(不是 bug,是"看着像 bug"):

- `useTablePro` 补列的 `setState` updater 里**写了 localStorage** —— 副作用写在 updater 里,
  React 18 StrictMode 会把 updater 调两次 ⇒ 写两遍(而且写的是闭包里的旧值)。移到下面的 effect 里写。
- `DataTable` 的 `<Card bordered={false}>` —— `bordered` 在 antd 5.29 **已废弃**
  (控制台打 deprecated 警告),等价的新写法是 `variant="borderless"`。
- `ColumnSettings` 的 `{c.title as string}` —— `title` 是 `React.ReactNode`,
  类型断言把它骗成 string;去掉断言后 ReactNode 正常渲染(顺带少一条 `no-explicit-any`)。

**主动收回的**(零调用方;理由同时写在代码里):

| 收回的东西 | 为什么 |
|---|---|
| `ViewField.span` | 零调用方(12 个调用方一个都没传)。而这个抽屉的 `Descriptions` **column 固定为 1** ⇒ 每个字段本来就占满一行,`span` 没有任何作用;更糟的是传 `span={2}` 会让 antd 判定"span 之和与 column 不匹配"并在控制台打 dev 警告。类型是 `number` 而注释写"占一整行(默认 true)"—— 两处都不对 |
| `SearchSection.fullWidth` | 零调用方(ManagementTable / system/config 两个调用方都没传)。组件体**从来没读过它** —— 容器本来就是 `display:flex`,子元素自己决定宽度,没有"占满整宽"这回事。一个不生效的开关留在公开接口上,只会让人以为"传了就能占满" |

**回归网**:**7 份新用例,100 条**(此前这一层**一条都没有**):

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/hooks/__tests__/useTablePro.test.tsx` | 33 | **B1**:超阈值 → `virtual: true` / 虚拟模式下 `scroll.x` **是数字**且等于可见列宽之和 / 不超阈值 → 不开虚拟 / 树形 → 关虚拟(互斥)。**B2**:表头取 ReactNode 的**文本**(不是 `[object Object]`)。**B3**:公式注入被中和 + 数字不加前缀 + 字符串形式的负号仍加前缀。**B4**:字符串 / 函数两种 rowKey 都原样透出。**B5**:`hideable:false` 的列照样渲染 + `setVisible` 对它无效。**B6**:换 `storageKey` → 重新读那一份。**B7**:点击那一刻锚点**在 document 里**。**B8**:一列都没显示时**不生成 blob** |
| `web/src/components/common/__tests__/tablePro.test.tsx` | 16 | 渲染 / 工具栏三个开关 / 导出生成 blob / `exportFilename` 覆盖文件名 / **B4**:`rowKey` 传函数也必须生效 / **B1**:超阈值渲染出 `ant-table-virtual`、树形优先 / **B5**:`hideable:false` 的列复选框禁用且旧 localStorage 藏不住它 / 列宽写回存储 |
| `web/src/components/common/__tests__/dataTable.test.tsx` | 10 | 渲染 / `card` 开关 / `searchForm` + `toolbar` 插槽 / `rowKey` 传函数也生效 / **B8**:调用方传的 `locale` 不被吞掉(但出错且无数据时错误态占位优先)/ `error` + `onRetry` 不漏到 DOM 上 |
| `web/src/components/common/__tests__/sensitiveField.test.tsx` | 17 | 四种显式 `type` 的脱敏形态 / auto 自动识别 / **恰好 18 个字符的邮箱按邮箱脱敏**(以前被当成身份证)/ **眼睛按钮有可访问名**(以前只有图标,读屏念不出来)/ 权限优先于 `defaultVisible` |
| `web/src/components/common/__tests__/tableErrorState.test.tsx` | 12 | `TableErrorState` 两种形态 / `strip` 是 warning 级(旧数据还在)/ `FallbackPlaceholder` 文案走 i18n / `error` 是 Error 对象还是字符串 / 有无 `onRetry` |
| `web/src/components/common/__tests__/viewDrawer.test.tsx` | 8 | 开关 / label + 值 / 空值占位 / `data` 为 null 降级 / `render` 回调拿到 `(原始值, 整行数据)` / `extra` / 关闭回调 / **不读 `span`**(不触发 antd 的 span 不匹配警告) |
| `web/src/components/common/__tests__/searchSection.test.tsx` | 4 | 渲染子元素 / `search-section` 类名(ManagementTable 的用例靠它认"同一行")/ hover 升级 L2 阴影 / `style` 追加在默认样式之后 |

**可逆探针 14/14 全红**(scripts/probe_m111.mjs —— 拆实现看测试会不会红;每条探针跑完**逐字节**校验还原,
还原后再全量跑一遍确认回到绿):

| # | 拆掉什么 | 变红的用例 |
|---|---|---|
| ① | `tableProps.virtual` 整个摘掉 | `行数超过阈值 → virtual: true` / `行数超过阈值 → 走虚拟表格` |
| ② | 虚拟模式的 `scroll.x` 退回字符串 | `scroll.x 必须是` |
| ③ | CSV 表头退回 `String(title)` | `表头取列标题的` |
| ④ | CSV 公式注入不再中和 | `公式注入被中和` |
| ⑤ | `rowKey` 不再透出到 `tableProps` | `rowKey 原样透出` |
| ⑥ | `hideable:false` 的列重新能被存储藏起来 | `visible:false → 这一列照样渲染` / `hideable:false 的列` |
| ⑦ | 换 `storageKey` 不再重读存储 | `换 storageKey` |
| ⑧ | CSV 锚点不再挂到 document 上 | `锚点必须在 document 里` |
| ⑨ | `TablePro` 退回 HEAD 整份文件 | `rowKey 传函数也必须生效` |
| ⑩ | `ColumnSettings` 退回 HEAD 整份文件 | `hideable:false 的列:复选框禁用` |
| ⑪ | `DataTable` 退回 HEAD 整份文件 | `调用方传的 locale 不会被吞掉` |
| ⑫ | `SensitiveField` 退回 HEAD 整份文件 | `恰好 18 个字符的邮箱` |
| ⑬ | 眼睛按钮的 `aria-label` 摘掉 | `眼睛按钮有可访问名` |
| ⑭ | `ViewDrawer` 退回 HEAD 整份文件(又开始读 `span`) | `不读 span` |

> ⚠️ **探针自己踩过一个坑(已修)**:④ 最初用**精确串**替换 `const guarded = …` 那两行,
> 结果被 prettier 重排成单行后**打不中** —— 脚本按"打不中 = 无效"抛错退出(而不是误报成 GREEN)。
> 改成正则(同时兼容换行版和单行版)后打中。
> ⚠️ 写这个正则时又踩了一次:`\-` 匹配的是**减号**,而源码里那个字符是
> **`\` + `-` 两个字符**(正则字面量里的转义写法)⇒ 必须写 `\\-`。
> **探针打不中比探针打偏更危险** —— 所以"打不中"一律抛错,绝不判 GREEN。

**§2.8 子项数不变(21 项,18 完成 / 3 还开着,仍 86%)** —— M111 是对 #10 / #11 / #13 / #15 的
**修正描述 + 把实现补成真能跑**,不是新增子项。

---

### 2.8.3 个人中心 / 用户资料链路审计(2026-10-29,M113)

**起因**:用户 2026-10-29 报个人中心(`/profile`)五件事 —— ①头像加载失败要有默认头像;
②「为什么不能修改个人基本信息?」;③改密不要短信验证码(旧密码 + 新密码 + 确认即可);
④历史登录记录的表格要像 `ManagementTable` 那样自己滚;⑤「二次验证(MFA)是什么东西?用来做什么?」。
(随后又追加两件:**⑥「没有点击编辑时候,为什么基本信息里面几个输入框都不显示内容」** —— 见 B14;
**⑦「基本信息编辑保存以后,会自动刷新左侧 card 里面的信息内容,但是左侧有时候会显示不对」** —— 见 B15 / B16。)

这七句**全是界面层的抱怨**,但逐行读下来,底下压着 **16 个真问题**:
其中 4 个是**后端契约层面的**(视图缺字段 / 邮箱脱敏 / 更新接口返回裸 `SysUser` / 操作人永远空白),
它们让前端**无论怎么改都改不对**。

⚠️ 这一层的判高危标准还是老那条("**挂在全局路径上吗**",见 memory:框架原语零覆盖):
`CurrentUserView` / `PATCH /auth/profile` **挂在每一个后台账号的每一次登录与每一次改资料上** ⇒ P0。

#### 真问题表

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| B1 | `web/src/pages/system/user/index.tsx` + `pages/profile/index.tsx` | 直接用 antd `<Avatar src={row.avatar}>`,而 antd 5 的 Avatar **只要 `src` 是字符串就一定先渲染 `<img>`**(`antd/es/avatar/avatar.js`:`src` → `hasImageElement` → `icon` → `children`) | `src=""` 在浏览器里 = **裂图**(而且**空 src 不触发 `onError`** ⇒ antd 的 icon 兜底**永远等不到**)。⇒ 没设过头像的用户,个人中心是 80px 大裂图,用户管理列表里**每一个**都是裂图 |
| B2 | 同上 | 兜底文字用 `nickname?.[0]` | JS 的 `str[0]` 是 **UTF-16 码元**下标 ⇒ emoji / 𠮷 这类**代理对**字符只拿到**半个代理对**(渲染成豆腐块)。⚠️ 中文不会中招(它在 BMP 里)—— 详见下面「探针自己踩的坑」 |
| B3 | `web/src/pages/profile/index.tsx` | `<Form disabled={!editingProfile}>` 把「编辑 / 保存 / 取消」三个按钮**写在 Form 里面** | antd 5 的 Form 用 `DisabledContextProvider` 包住子树(`antd/es/form/Form.js:140`),Button 从 Context 取禁用态(`antd/es/button/button.js:119-120`)⇒ **只读态时「编辑」按钮自己也是灰的** ⇒ 永远进不了编辑态 ⇒ **基本信息根本改不了**。**这就是用户报②的真凶**(与 RBAC 无关,后端契约一直是通的) |
| B4 | `server/internal/service/auth_view.go` | `CurrentUserView` **压根没有 `real_name` 字段** | 前端表单回填 `userInfo?.real_name` 恒 `undefined` ⇒ **真实姓名输入框永远是空的** ⇒ 这个字段从来没被任何人在界面上填过 |
| B5 | 同上 | `Email: desensitize.Email(decryptPII(u.Email))` | 个人中心把邮箱**脱敏后**回填进**可编辑的**输入框 ⇒ 用户改个昵称点保存,后端收到的 email 是 `z******@qq.com` ⇒ **真邮箱被掩码串覆盖**。手机号继续脱敏(口径不同,见技术方案 §9.3.2) |
| B6 | `server/internal/service/auth.go` + `auth_impl.go` + `handler/v1/auth.go` | `UpdateProfile` 返回的是**裸 `*model.SysUser`** | `model.SysUser` 的 `phone`/`email` 是 `json:"-"`(**根本不返**),`identities` / `is_platform_super_admin` / `current_identity_id` **压根不存在** ⇒ 前端 `setUserInfo(updated)` 整份替换 ⇒ **身份列表、超管标记、当前身份、手机号全被洗成空**(症状:改一次昵称,右上角超管标签没了、"我的身份"空了)。技术方案 §7.7.4.6 一直写的就是"Response Data: UserInfo" —— **是实现偏离了契约,不是契约写错了** |
| B7 | `web/src/pages/profile/index.tsx` | 改密弹窗第一屏是 `SmsCodeGuard`(发短信 + 校验),通过后才露出密码表单 | 那条短信链路在个人中心**从来没能发出去过短信** ⇒ **个人中心实际改不了密码**。用户③明确要求「不要短信验证码」⇒ 前端那道闸门**整条删掉**(`CHANGE_PASSWORD_SMS_SCENE` 常量一并删)。⚠️ **后端契约一个字没动** —— 旧密码 + 新密码这条通道一直通,被挡住的只是前端 |
| B8 | `web/src/pages/profile/SessionPanel.tsx` | 历史登录记录表格写死 `HISTORY_SCROLL_Y = 360` | ①分辨率一变就对不上(1080p 留白 / 2K 屏挤扁);②整页出现**竖向滚动条** —— 用户看到的不是"表格自己滚"而是"整个界面在滚" |
| B9 | 同上 + `web/src/styles/global.css` | 这一页**没有高度链**(Row → Col → Card → Tabs → TabPane 每层都是内容撑开) | 容器高 = 内容自然高度 ⇒ `scroll.y ≈ 全部行高` ⇒ 表格永远滚不起来。而且是**自指的循环**:`scroll.y` 决定容器高,容器高又决定 `scroll.y` ⇒ 以前**算不出来**。补上 `ManagementTable` 那条同款高度链才量得动 |
| B10 | `web/src/pages/profile/index.tsx` | MFA 只有一个「未开启」标签 + 一个禁用按钮,tooltip 写"Phase 6 真实化" | 用户完全不知道 MFA 是什么、用来防什么 ⇒ ⑤问的就是这个 |
| B11 | `web/src/pages/profile/index.tsx` | 「基本信息」页签里有一个**只读**的手机号输入框 | 与「安全设置 → 手机绑定」那张卡显示**同一个值** ⇒ 同一个信息在两处露,改一处另一处就过期。用户要求删掉 |
| B12 | `server/internal/service/auth_impl.go` | `hasSuperRole` / `hasOpsRole` 与 `GetCurrentUser` 里那段判定**各写一遍** | B6 把超管标记挪进 `UpdateProfile` 的返回路径之后,这段判定就有**两份活的实现** —— 今天两边一致,明天有人加一个"平台运维"角色码只改一处,就出现"登录时是运维、改完资料标签没了" |
| B13 | `server/internal/service/config_center_impl.go` | 4 处查询都是裸 `u.real_name AS user_name`,**没有 fallback** | 后台账号的 `real_name` **默认是空的**(seed 只给 super_admin 填了"超级管理员"),而个人中心在 M113 之前**压根不能填** ⇒ 审计日志「操作人」列、配置项「最后修改人」列**长期显示空白** ⇒ 管理员根本看不出是谁改的配置 |
| B14 | `web/src/pages/profile/index.tsx` | `profileForm` 的值**只在 `startEditProfile`(点「编辑」那一下)被写入**,而 `<Form>` 上**没有 `initialValues`** | **首帧表单是空的** ⇒ 只读态三个输入框(昵称 / 真实姓名 / 邮箱)全是空的 ⇒ 用户看到的是"这个账号没有昵称 / 真实姓名 / 邮箱",而不是"这里显示的是我的资料"。用户 2026-10-29 追加报的第六件 |
| B15 | `web/src/pages/profile/index.tsx` 左侧账号摘要卡片 | 三处取值层的错:①`userInfo.email ?? '-'` —— **`??` 只兜 null/undefined,空串不兜**;②角色标签第三个条件只判 `!is_platform_super_admin`;③`role_name ?? '-'` 同 ① | ①**没填邮箱的账号(超管 seed 就是)**那一格渲染成**空白单元格**,和旁边有值的行对不齐;②**平台运维账号同时挂「平台运营员」和「普通用户」两个互斥标签**;③身份角色名为空时那一格是空白。⚠️ 三处都是"**有数据的账号看不出来、没数据的账号看得出**" —— 正好对上用户说的"**有时候**显示不对" |
| B16 | 同上 + `web/src/styles/global.css` | 左侧卡片 `height: '100%'` ⇒ 卡片被**拉成整列高**,内容只有半屏时底下留一大片空白 | 用户报"左卡片显示不对",**手改成 `maxHeight: '100%'`** —— **照单全收**(两张 Card 现在都用 `maxHeight: '100%'`)。⚠️ **卡片"具体多高"由用户手调**(2026-10-29 用户明确:"这个界面大小那些等下我手动改样式")—— 代码只保证**结构**对(`min-height:0` / flex 链 / overflow 归属)。⚠️ **之前写的"实测 400px/102px/368px/232px"是手搭复制件上的数字,已删** —— 复制件(临时 HTML)不是真实页面(antd 真 DOM + 布局壳层),真页面上这条链的百分比高度能不能解析取决于壳层给不给**确定高度**,复制件给不出这个前提 ⇒ 那组数字既证明不了"对"也证明不了"错",只是**看起来很精确**。教训:**手搭的替身 ≠ 被测对象** |

#### 修法(全在框架层,零业务代码)

- **B1 →** 新组件 `web/src/components/common/UserAvatar.tsx`:
  **空 src 压根不渲染 `<img>`** + 自己管 `onError` 回落 + 首字母/图标兜底。
  ⚠️ 兜底图标必须走 antd 的 **`icon` prop**(不是 `children`)——
  antd 的渲染顺序是 `icon` **优先于** `children`,两者只能给一个。
  ⚠️ `onError` 必须 **`return false`** —— 否则 antd 会把 `isImgExist` 置 false 并渲染它自己的 `icon`,
  我们要的是首字母不是图标。
  ⚠️ `src` 变了要**重置「失败」标记**(否则换了一张能加载的图也永远停在失败态)。
- **B2 →** `avatarInitial()` 按**码点**取(`Array.from`),不按 UTF-16 码元取;后台账号的 `nickname` 走同一个函数。
  ⚠️ 这两个**纯函数**放在 `components/common/avatar.ts` 而不是组件文件里 ——
  一是能单独测,二是别触发 `react-refresh/only-export-components` 警告
  (跟 `pages/system/dict/isActive.ts` 同一个套路)。
- **B3 →** 「编辑 / 保存 / 取消」三个按钮**移到 `<Form>` 外面**:`disabled` 只作用于三个输入框
  (只读态确实该灰),动作按钮永远可点。
- **B4 / B5 →** `CurrentUserView` 补 `RealName` 字段;`Email` 返**明文**(自己的邮箱、只有本人可见、
  管理员列表本来就是明文 ⇒ 脱敏只发生在"自己看自己"这一处,既不一致又挡不住人)。
  **手机号仍然脱敏。**
- **B6 →** `UpdateProfile` 返回类型从 `*model.SysUser` 改成 `*CurrentUserView`;
  新增 `currentUserView()` 取"用户 + 身份 + 平台标记"。
  ⚠️ **不用 `GetCurrentUser`**:那个方法还会把**整棵资源树**查出来(超管是"全部 active 资源"),
  为了返一个 user 拉全量资源,代价与风险都不成比例。
- **B7 →** 弹窗直接就是 旧密码 / 新密码 / 确认新密码(前端 `changePassword(old, new)` 一直是两个参数)。
- **B8 / B9 →** 补高度链(根 div → Row(`flex:1/min-height:0`)→ Col → Card(`body` flex:1/min-height:0)
  → Tabs → `.ant-tabs-content-holder{flex:1 1 0%}`),
  行区高度 = **容器高 − 表头高 − 分页高**,容器高用 `ResizeObserver` 跟。
  量的是**容器**(它的高度只由 flex 剩余空间决定,**与表格自身无关**)⇒ 不会自指。
  ⚠️ `min-height:0` 是 flex 子项能收缩的前提(`min-height:auto` = 内容高度会把整条链重新顶开)。
  ⚠️ antd 的 Tabs 根节点是 `display:flex`、`.ant-tabs-content-holder` 是 `flex:auto`
  —— `flex:auto` 的 basis 是 `auto`,与 `height:100%` **循环依赖**
  ⇒ 必须显式改成 `flex:1 1 0%`。
- **B10 →** MFA 卡片加一段说明:是什么(登录除了密码还要再验一次身份)、用来防什么(密码被别人知道了也不够)、当前什么状态。
- **B11 →** 删掉「基本信息」里的只读手机号输入框;手机号**只**在「安全设置」露一次。
  ⚠️ 返回视图里 `phone` **仍然带着**(脱敏值)—— 那是「安全设置」那张卡的数据源,
  删掉的是**重复的那一处界面**,不是这个字段。
- **B12 →** 判定口径抽成 `platformFlags(identities) (isSuper, isOps bool)`,
  `GetCurrentUser` 与 `currentUserView` 都调它;`hasSuperRole` / `hasOpsRole` 降级成**薄壳**
  (保留是为了不动 4 个老调用方 + `auth_test.go` 里那组语义断言)。
- **B13 →** `operatorNameSQL = COALESCE(NULLIF(u.real_name,''), NULLIF(u.nickname,''), u.username)`,
  口径与**审批流**(`approval_query_impl.go`)一致 —— 那边早就是这三段 fallback。
- **B14 →** 用 **effect 把 `userInfo` 的真值同步进表单**(`userInfo` 变化且**不在编辑态**时写一次)。
  ⚠️ **为什么不用 `initialValues`**:antd 的 `initialValues` **只在 Form 挂载那一刻读一次**,
  而 `userInfo` 是登录后由 `/auth/me` **异步**灌进 store 的 —— F5 刷新个人中心时首帧它多半还是 `undefined`
  ⇒ `initialValues` 那一刻拿到的就是空 ⇒ 会得到"刷新一次就好了、再刷又空了"。
  ⚠️ **编辑态不写** —— 否则 token 续期把 store 换掉(userInfo 换新对象)时,
  会把**用户正在改的内容悄悄冲掉**(用户以为改好了,一保存又存回旧值)。
  ⚠️ `setFieldsValue` 走 rc-field-form 的 `source:'external'` 分支(`Field.js:148`):**只清错、不触发校验**
  ⇒ 只读态不会冒出一片红色的"必填"提示。
  顺带修掉一个同源的小毛病:点「取消」回到只读态时,表单以前**留着用户改到一半的内容**(没人重置它),
  现在 effect 会把它**还原成当前真值**。
- **B15 →** 三处取值都改成"**空串也算没有**"的口径:
  ①邮箱 `userInfo.email ?? '-'` → `userInfo.email || '-'`(⚠️ `??` 不兜空串 —— 空串会渲染成**空白单元格**);
  ②角色标签第三个条件 `!is_platform_super_admin` → `!is_platform_super_admin && !is_platform_ops`
  ⇒ 三个标签(超管 / 平台运营员 / 普通用户)**互斥**;
  ③`role_name ?? '-'` → `role_name || '-'`。
  ⚠️ 一律用 `||` 而不是 `??`:**后端视图里"没有这个值"的表现就是空串**(不是 null),
  `??` 兜不住。昵称/账号名的兜底(`nickname ?? username`)同样改成 `||`。
- **B16 →** **两张 Card 都用 `maxHeight: '100%'`(用户手改,照单全收)**,
  **卡片具体多高归用户手调** —— 代码只保证结构(`min-height:0` / flex 链 / overflow 归属)。⚠️ 注释里**不再写"实测数字"**:之前那组数字是手搭复制件上的,不是真实页面的。
  ⚠️ 理由是 flex 的**布局时机**,不是审美:
  `max-height` 是"布局**之后**才截断" ⇒ **只有被截断那个盒子的直接子元素上的滚动容器
  知道自己被截了多少**。左卡片的滚动容器(body)正好是直接子元素 ⇒ 照样能滚;
  右卡片的 Tabs 那条链隔了**两层**(Card → body → Tabs → holder → 内层 `height:100%`),
  内层因为"父高不确定"退化成 auto ⇒ 页签内容比列高时**底下被裁掉且滚不到**。
  ⇒ 两张卡片的高度写法**故意不一样**,代码注释和 `global.css` 都写明了这一点与实测数字。

#### 顺带修掉的(不是 bug,是"看着像 bug")

- `CurrentUserView` 的 `Email` 字段注释原来写的是"已脱敏" —— 改成明文之后注释必须同步,
  否则下一个人会以为脱敏还在,又把 `desensitize.Email` 加回去。
- `web/src/pages/profile/index.tsx` 里 `real_name` 的取法原来是 `(userInfo as any)?.real_name`
  —— `any` 一去掉就变成**编译期**信号:后端视图缺字段 ⇒ `tsc` 直接报错,不用等用户发现输入框是空的。
- `web/src/constants/sms.ts` 的 `CHANGE_PASSWORD_SMS_SCENE` 常量随 B7 一起删
  (零调用方的常量留着,下一个人会以为改密还要短信)。

#### 主动收回的

| 收回的东西 | 为什么 |
|---|---|
| 「基本信息」里的手机号输入框 | 用户明确要求删(手机号走「安全设置 → 手机绑定」)。**不是**因为它没用 —— 它跟安全设置那张卡显示同一个值,是**重复**而不是**缺失** |
| 改密的短信二次验证 | 用户明确要求。⚠️ 这**不是**"降级安全",而是**换一条同样安全的通道**:改密要**旧密码**,比短信码更强(短信码可能被截获,旧密码不会)。**身份切换那边的服务端代发短信**(`auth:sms/account/send` / `verify`)不受影响,继续用 |

#### 「真实姓名」改完之后用在哪里(用户追问)

`real_name` 不是摆设,一共三个消费点:

1. **审批流**(`approval_query_impl.go:420-433`)—— 显示发起人姓名,口径同样是
   **真实姓名 → 昵称 → 账号** 三段 fallback。
2. **配置中心**(`config_center_impl.go`)—— 审计日志「操作人」+ 配置项「最后修改人」。
   B13 修的就是这里。⚠️ 表里落库的 `operator_username` 是**账号**,与界面上的显示名不是一回事:
   **追责**看 `operator_id`/`operator_username`,**给人看的**那一列按上面的口径现算。
3. **用户管理页**(`user_impl.go:204` / `:298`)—— keyword 模糊匹配(`username`/`nickname`/`phone`/`real_name`)
   + 详情抽屉的「真实姓名」字段。

⚠️ 顺带也说明了一件事:**这个字段以前基本没人维护**(个人中心压根不能填 —— B3/B4)⇒ 所以 B13 那一列一直是空的。

#### 回归网:**48 条**(此前这一层**一条都没有**)

| 文件 | 条数 | 守什么 |
|---|---|---|
| `web/src/pages/profile/__tests__/profile.test.tsx` | 21 | **B3**:「编辑」按钮**必须是可点的**(真凶)/ **B4**:真实姓名回填真值 / **B5**:邮箱回填**真邮箱**、不是掩码串 / **B6**:保存时 nickname+real_name+email 一起发出去 / **B6**:保存成功后身份列表+超管标记**不能被洗空** / **B7**:弹窗直接就是旧密码+新密码+确认(没有短信那一步)/ 提交只带两个参数 / 两次新密码不一致拦下来不调接口 / **B10**:MFA 讲清是什么、防什么、当前什么状态 / **B8**:行区高度跟着容器走、不是写死的常数 / **B11**:「基本信息」页签里没有手机号 / 「安全设置」里仍然看得到已绑定的手机号 / **B14**:不点「编辑」时昵称+真实姓名+邮箱**就已经是当前值** / **B14**:只读态输入框**禁用但有内容** / **B14**:点「取消」回到只读态**还原成当前值**(不是编辑到一半的) / **B14**:编辑态下 store 被换掉**不能冲掉正在改的内容** / **B15**:没有邮箱时那一格显示 **"-"**(不是空白单元格)/ **B15**:有邮箱时显示真邮箱(对照组)/ **B15**:平台运维**不**同时挂「普通用户」/ **B15**:普通用户照旧挂「普通用户」(对照组)/ **B15**:角色名为空时那一格兜底成 "-" |
| `web/src/components/common/__tests__/userAvatar.test.tsx` | 14 | **B1**:空 src / undefined / 空白 src 都不渲染 `<img>` / 有 src 渲染 `<img>` / 加载失败回落成首字母 / **换头像后失败标记必须重置** / 连名字都没有时退回通用人形图标 / **B2**:中文取第一个字 / **emoji 与扩展汉字(代理对)不能被切成半个** / 英文取首字母并大写 / 空名⇒空串 / 同名同色、色板不退化(纯函数在 `components/common/avatar.ts`,单独测) |
| `server/internal/service/auth_profile_m113_test.go` | 10(函数) | **B4**:视图里有 `real_name` / **B5**:邮箱**不脱敏**、手机号**仍然脱敏** / wire 形状(字段名与 `json` tag 对齐 `web/src/types/user.ts`) / **B6**:更新后视图带 identities / 带超管标记 / 带运维标记 / 邮箱往返不被掩码串污染 / 真实姓名往返 / 真实姓名允许为空 |
| `server/internal/service/config_center_m113_test.go` | 3(函数) | **B12**:`platformFlags` 六种身份组合(普通用户 / 超管 / 运维员 / 运维主管 / 超管+运维 / 空)/ **B13**:操作人三段 fallback / **B13**:操作人**永远不是空白** |

> 后端 13 个测试函数、含 6 个子测试 ⇒ `go test -v` 口径下 **19 条**。

⚠️ **B16(布局那条)没有前端用例** —— jsdom **没有布局引擎**,`height` / `maxHeight` 的差别
在 jsdom 里量不出来(所有元素的 `offsetHeight` 恒为 0)。⇒ 这一条**用 headless Chrome 实测**:
把这一页的高度链(根 div → Row → Col → Card → body)照搬成一个最小 HTML,
在真实浏览器里量 `offsetHeight` / `clientHeight` / `scrollHeight`,六个场景
(左卡片 × {height, maxHeight} × {内容短, 内容高} + 右卡片 Tabs 链 × {height, maxHeight})
逐个量。数字写进了代码注释和本节,**不靠"我觉得"**。

#### 可逆探针 17/17 全红(scripts/probe_m113.mjs —— 拆实现看测试会不会红;每条探针跑完**逐字节**校验还原,
还原后再全量跑一遍确认回到绿):

| # | 拆掉什么 | 变红的用例 |
|---|---|---|
| ① | `if (!url \|\| broken)` → `if (false)`(头像兜底) | `src 是空串:不渲染 <img>` / `图片加载失败:回落成首字母` …(6 条) |
| ② | `Array.from(trimmed)[0]` → `trimmed[0]` | `emoji / 扩展汉字(代理对)不能被切成半个` |
| ③ | 删掉 `useEffect(() => setBroken(false), [url])` | `换了一张新头像:失败标记必须重置` |
| ④ | 把「编辑/保存/取消」塞回 `<Form disabled>` 里面 | `「编辑」按钮必须是**可点的**` …(5 条) |
| ⑤ | 把 HEAD 那道短信闸门搬回来 | `弹窗里直接就是旧密码 / 新密码 / 确认新密码` …(3 条) |
| ⑥ | 删掉 MFA 那个 `<Alert>` | `界面上要讲清楚它是什么、用来干什么、当前什么状态` |
| ⑦ | `scroll={{ y: historyScrollY }}` → `scroll={{ y: 360 }}` | `行区高度跟着容器高度走,不是一个写死的常数` |
| ⑧ | 把手机号 Form.Item 加回「基本信息」 | `「基本信息」页签里没有手机号字段` |
| ⑨ | `Email: decryptPII(u.Email)` → `desensitize.Email(decryptPII(u.Email))` | 视图不脱敏 / 邮箱往返 / 前端回填真邮箱 …(3 条) |
| ⑩ | 删掉 `CurrentUserView` 的 `RealName` 字段 | **整包编译不过** —— 这条网的形态就是编译不过 |
| ⑪ | `NewCurrentUserView(&u, identities, isSuper, isOps, …)` → `(&u, nil, false, false, 0)` | 视图带 identities / 带超管标记 / 带运维标记 …(3 条) |
| ⑫ | `platformFlags` 漏掉 `RolePlatformOpsLead` | `platformFlags` / `带运维标记` |
| ⑬ | `operatorNameSQL` → 裸 `"u.real_name"` | 操作人 fallback / 操作人永远不是空白 |
| ⑭ | 删掉「把 `userInfo` 同步进表单」的 effect(= 回到 HEAD 的写法) | `没点「编辑」时,昵称 / 真实姓名 / 邮箱就已经是当前值` |
| ⑮ | 同步时**不看编辑态**(拿掉 `if (editingProfile) return`) | `编辑态下 userInfo 被换掉(比如 token 续期),**不能冲掉正在改的内容**` |
| ⑯ | `userInfo.email \|\| '-'` → `userInfo.email ?? '-'`(回到 HEAD 的写法) | `账号没有邮箱时,那一格显示 "-" 而不是空白` |
| ⑰ | 标签条件拿掉 `&& !userInfo.is_platform_ops`(回到 HEAD 的写法) | `平台运维账号不同时挂「普通用户」标签` |

> ⚠️ ⑭ 与 ⑮ 是**一对**:⑭ 打掉"只读态有值",⑮ 打掉"编辑态不被冲掉"。
> 只写一个守卫(`if (editingProfile) return`)时,两条探针正好各打中一条 ——
> **少写一条守卫 = 少一条探针**,这类"半个开关"最容易在重构里被抹掉。

> ⚠️ ⑯ / ⑰ 是**另一对**:⑯ 打掉"空串兜底",⑰ 打掉"标签互斥"。
> 这两处是**取值层**的错(`??` 兜不住 `''` / 两个标签互斥没做),跟 ⑭ / ⑮ 的"守卫层"不是一类 ——
> 所以 ⑯ / ⑰ 各自都配了一条**对照组**用例(有邮箱显示真邮箱 / 普通用户照旧挂"普通用户"标签),
> 免得"探针红了"其实是"用例本身写错了"。

> ⚠️ **探针 ② 自己踩过的坑(已修)** —— 这条是本轮**最值得记**的一条。
> 它最初写成「首字母退回**按字节**切 ⇒ 中文名乱码」,打不中(实测 GREEN(BAD))。
> 逐条核实才发现**前提是错的**:
>
>   · JS 的 `str[0]` 是 **UTF-16 码元**下标 —— JS 字符串**压根没有"字节下标"这回事**;
>   · 中文 / 拉丁文在 **BMP** 里只占**一个**码元 ⇒ `str[0]` 拿到的就是**完整的「张」**
>     ⇒ 断言照样通过 ⇒ 探针**打不中**;
>   · 真正会坏的是**代理对**字符(emoji、`𠮷` 这类 supplementary plane)——
>     它们占**两个**码元,`str[0]` 只拿到**半个代理对** ⇒ 渲染成豆腐块(U+FFFD)。
>
> ⇒ 修法有两条,一起做:①把 `UserAvatar.tsx` / `IdentitySwitcher.tsx` /
> `pages/system/user/index.tsx` / `userAvatar.test.tsx` 里那几处
> **「按字节切 ⇒ 中文乱码」的错话**改成准确的说法(码点 vs 码元 + 代理对);
> ②把用例改成拿**代理对**来证伪(中文那条留着当底线,但它**不能**证伪 `str[0]`)。
> ⚠️ 这正是 memory 里那条「**探针打不中比探针打偏更危险**」的又一次复现 ——
> 一条**前提写错**的探针,比没有探针更危险:它给出一个假的"已覆盖"信号。

**§2.8 子项数不变(21 项,18 完成 / 3 还开着,仍 86%)** —— M113 是对 #1(`ManagementTable`
那条 auto-height 写法)在**个人中心**的落地 + 补一个可复用的 `UserAvatar` 展示组件,
**不是新增子项**。

### 2.8.4 个人中心 · 「手机绑定」换绑手机号(C5,2026-10-29)

**起因**:个人中心「安全设置 → 手机绑定」那张卡里,「换绑手机」是一个 **disabled 的按钮**,
tooltip 写着「Phase 6 真实化」⇒ **这条链路从来没通过过**(SRS §2.2.3 的"输入新手机号并完成新号验证"落不了地)。

本轮做成真的:填**新**手机号 → 验证码发到**新号** → 填码 → 确认换绑。
后端契约见技术方案 §7.7.4.19 / §7.7.4.20;权限侧见《后台权限模型设计V1.0》§17.36。

#### 真问题表

| # | 位置 | 现象 | 后果 |
|---|---|---|---|
| C1 | `web/src/pages/profile/index.tsx` | 「换绑手机」按钮 `disabled` + tooltip「Phase 6 真实化」 | 用户**永远换不了手机号** —— 而"手机号是登录/找回密码的根",用户一旦换号就与账号失联 |
| C2 | 同上 | 弹窗里**没有**新手机号 / 验证码两个输入框 | 整条链路只有壳 |
| C3 | 同上 | 弹窗里只有一张"已绑定手机号"的只读卡片 | 用户没有地方填新号 |
| C4 | 后端 | **没有**"给新号发码 / 用新号上的码换绑"的端点 | 前端做不出来 |
| C5 | 后端 `auth_impl.go` | `phone` 与 `phone_hash` 是两个字段 | 只写 `phone` 的话,「按手机号短信登录 / 短信重置密码」**直接失配**(那两条按 `phone_hash` 查人) ⇒ 换绑成功反而**把账号的登录能力搞没了** |
| C6 | 后端 | 新号**可能已经被别的账号占用** | 两个账号共用一个号 ⇒ 那条短信会被消费到**先写入**的那个账号头上,后写的那个永远收不到 |
| C7 | 后端 | 换绑后**旧号上残留的验证码**没人作废 | 手机号是会回收的:旧号上残留的码若还在有效期内,等旧号被绑到别的账号,那条码就能登进**新主人**的账号 |
| C8 | 前端 | 换绑成功后如果只改 `phone` 一个字段 | 身份列表 / 超管标记还留在旧对象里(这次侥幸没事,下次就出事)—— 与 M113 的 B6 **同一个教训** |
| C9 | 前端 | 手机号格式不对也要等后端拒 | 用户点了两次按钮、白等两秒(后端也会拒,但那是"白等") |

#### 修法(框架层,零业务代码)

**后端**:

- 两个新端点:`POST /rapid-admin/v1/auth/phone/code`(发码给**新号**)、
  `PATCH /rapid-admin/v1/auth/phone`(换绑)。资源码 `auth:bind-phone`,进 `authBase`(自助能力)。
- **手机号由前端传,不是服务端取** —— 与已有的 `/auth/sms/account/send` **反着来**:
  换绑要证明的是"用户占着**新**号",而服务端手上只有旧号;
  用旧号验证只能证明"用户还占着旧号",证明不了换绑真正要证明的那件事。
- **新旧号相同直接拒绝** —— 否则用户在"换绑"弹窗里发的那条码,正好也是"切高敏身份"弹窗里能用的那条,
  一个码两处用,事后说不清是谁在用。
- **`phone` + `phone_hash` 双写**(C5);**新号占用检查**查两条(`phone_hash` + `phone` 兜底老数据)(C6);
  **旧号上未使用的码一律作废**,且作废放在写库**之前**(C7)。
- 返回 `CurrentUserView`(不是裸 `SysUser`)—— 前端整份替换 store,少返字段等于把它洗空(同 M113 的 B6)。

**前端**:

- 弹窗:新手机号 + 6 位验证码两个字段,「发送验证码」60s 冷却。
- 换绑成功后 `setUserInfo(updated)` **整份替换**(C8)。
- 手机号格式前端先拦一道(C9)。

#### 守门网

| 文件 | 条数 | 守的是什么 |
|---|---|---|
| `server/internal/service/auth_bind_phone_test.go` | 9 | 新号发码 / 换绑 / 双写 / 新旧号相同拒绝 / 新号被占用拒绝 / 旧号码作废 / 验证码一次性 / 未知场景 |
| `web/src/pages/profile/__tests__/profile.test.tsx` | 6 | 按钮不再是禁用的 / 弹窗里有两个输入框 / **码发给的是新号**(不是旧号) / 格式不对不发请求 / 提交带新号+码 / 成功后身份列表没被洗空 |
| `server/internal/seed/seed_test.go` `TestRun_BindsAuthBaseToAllRoles` | 3 | 资源池里有这个码 + 每个角色都绑上了 + 期望清单没少项(见《后台权限模型设计V1.0》§17.36) |

#### 可逆探针

前端(拆掉实现看测试会不会红):

| # | 拆掉什么 | 变红的用例 |
|---|---|---|
| ① | 把 `<Button disabled>` 加回去 | 「按钮不再是禁用的」 |
| ② | 把 `handleSendBindCode` 里的 phone 换成 `userInfo.phone` | 「发给后端的是**新**手机号」 |
| ③ | 把 `setUserInfo(updated)` 改成只改 `phone` | 「身份列表没被洗空」 |
| ④ | 把内层 Form.Item 的 `name="code"` 去掉 | 「确认换绑时…」变红(实测:`expected [ '13900001111', undefined ] to deeply equal [ '13900001111', '123456' ]`) |

> ⚠️ **④ 这条探针第一次打的时候是绿的** —— 因为当时的写法是
> "把 `name="code"` 从内层**提到外层** Form.Item"。查下来是**那条注释本身写错了机制**:
> 它声称"注入落到 `<Space.Compact>` 身上 ⇒ 输入框拿不到值 ⇒ 提交时 code 恒为 undefined"。
> 实测(antd 5.29.3)**不是这样**:`Space.Compact` 把注入的 `onChange` 原样透传给它的根 `div`,
> 而 React 的 `onChange` 是**会冒泡的合成事件**(原生 `input` 事件)⇒ 输入框里的值
> **照样冒泡到 div → 进 form store**。差别不是"能不能提交",而是输入框会退化成**非受控**组件
> (`value` 落在 div 上,输入框收不到 ⇒ `resetFields` / `setFieldValue` 清不掉它显示的内容)。
> ⇒ 注释按实测改写;**两层写法保留**(受控),但不再声称"少了就提交不了"。
> ⚠️ 这条是 memory 里「**探针打不中比探针打偏更危险**」的又一次复现 ——
> 一条**前提写错**的探针,给出一个假的"已覆盖"信号。

---

---

## 2.9 ✅ Hooks(100%)

#### ✅ 已完成(7 个)

| # | Hook | 证据 | 行数 |
|---|---|---|---|
| 1 | useAuth | `hooks/useAuth.ts:1-27` | 27 |
| 2 | usePermission | `hooks/usePermission.ts:1-32` | 32 |
| 3 | useTable(useTableResult + tableProps) | `hooks/useTable.ts:1-119` | 119 |
| 4 | useDict(options + getLabel + getColor) | `hooks/useDict.ts:1-83` | 83 |
| 5 | useRequest(serviceRef 模式) | `hooks/useRequest.ts:1-75` | 75 |
| 6 | useConfig / useConfigValue | `hooks/useConfig.ts:1-33` | 33 |
| 7 | useTableSelection(行选择 + 跨页保留) | `hooks/useTableSelection.ts:1-226`(M5b) | 226 |

> **复用价值**:**高**,✅ 完整

---

## 2.10 ✅ Types / API 包装(100%)

#### ✅ 已完成(12 个)

| # | 子项 | 证据 | 行数 |
|---|---|---|---|
| 1 | 通用 API 类型(ApiResponse / PageRequest / BizCode) | `types/api.ts:1-80` | 80 |
| 2 | 审计日志类型 | `types/audit.ts:1-36` | 36 |
| 3 | 字典 DictGroupCode 联合类型(29 组) | `types/dict.ts:1-84` | 84 |
| 4 | 权限 / 角色 / 资源 / 部门 | `types/permission.ts:1-82` | 82 |
| 5 | 用户 / 身份 | `types/user.ts:1-81` | 81 |
| 6 | 平台配置(ConfigItem/ConfigGroup) | `types/config.ts:1-105` | 105 |
| 7 | Auth API 11 个接口 | `api/auth.ts:1-105` | 105 |
| 8 | User API 8 个接口 | `api/user.ts:1-68` | 68 |
| 9 | Role/Resource/Department API 14 个 | `api/role.ts:1-135` | 135 |
| 10 | Dict/Config API 8 个 | `api/config.ts:1-109` | 109 |
| 11 | Upload API 3 个(token + 直传 + 中转) | `api/upload.ts:1-139` | 139 |
| 12 | api/index.ts 命名空间聚合 | `api/index.ts:1-22` | 22 |

#### ⚠️ 部分完成(API TODO 标记 — 共 23 个 endpoint 缺)

> **2026-10-26 起这张表由守门网维护**,不再是手工 grep 的快照:
> `web/src/api/__tests__/api-route-parity.test.ts` 解析后端 `server/internal/api/router/router.go`、
> 用 TypeScript AST 扫前端**全部** `http.<verb>()` 调用,凡是打不到后端路由的
> 都必须进白名单;反过来,白名单里的每一条都必须在源码里带 TODO 注释
> (机器可读的清单 + 给人看的注释,**两侧都得有**)。
> **白名单只允许缩小** —— 后端补一个端点,测试立刻要求把它从表里删掉。
> 缺口是"后端欠一个端点",不是"前端路径写错了",所以**这条网不管业务实现**。

按文件分组(每个 TODO 都对应 1 个待注册 endpoint;行号 = `web/src/api/` 当前行):

| 文件 | 缺 | 涉及模块 | 待注册 endpoint |
|---|---|---|---|
| api/asset.ts | 10 | 资产/资料/交付/公告 | `POST /biz/assets/:id/unassign`(54) / `POST /biz/assets/:id/offline`(64) / `DELETE /biz/materials/:id`(156) / `POST /biz/materials/:id/publish`(173) / `GET /biz/materials/:id/download`(180) / `GET /biz/deliveries/:id`(210) / `GET /biz/deliveries/:id/stage/:stage/records`(228) / `POST /biz/deliveries/:id/acceptance`(235) / `GET /biz/notices/:id`(258) / `GET /biz/notices/:id/activity/stats`(290) |
| api/issue.ts | 4 | 问题工单 | `PATCH /biz/issues/:id`(74) / `DELETE /biz/issues/:id`(81) / `GET /biz/issues/:id/comments`(105) / `GET /biz/issues/statistics`(114) |
| api/house.ts | 3 | 楼栋/房屋 | `GET /biz/buildings/:id`(62) / `POST /biz/houses/:id/bind-owner`(119) / `POST /biz/houses/:id/unbind-owner`(127) |
| api/owner.ts | 2 | 业主 | `GET /biz/owners/:id/houses`(64) / `POST /biz/owners/:id/unbind-wechat`(70) |
| `api/role.ts` | 1 | 角色 | `GET /system/roles/:id/users`(73) |
| **合计** | **20** | - | - |

**从上一版(34 个)里删掉的 11 条 —— 后端已经实现了,前端 TODO 是过时的**:

| 原 TODO | 后端实现 | 处理 |
|---|---|---|
| `api/user.ts` `POST /system/users/batch-delete` | router.go:258 | TODO 已删(2026-10-26 之前) |
| `api/role.ts` `GET /system/roles/:id` | router.go:266 | TODO 已删(2026-10-26 之前) |
| `api/role.ts` `GET /system/resources` | router.go:272 | TODO 已删(2026-10-26) |
| `api/role.ts` `GET /system/departments/:id` | router.go:285 | TODO 已删(2026-10-26 之前) |
| api/house.ts `GET /biz/projects/simple` | router.go(已注册) | 那条 TODO 本来就是"跟后端确认"不是缺口,后端已有 |
| `api/upload.ts` `POST /biz/uploads`(中转) | M80 改成 `/biz/files` + `/biz/folders` | 老端点已退役,前端已重写 |
| api/asset.ts 公告 create | router.go:426 | TODO 已删(2026-10-26) |
| api/asset.ts 公告 update / remove | router.go:427 / 428 | TODO 已删(2026-10-26) |
| `api/dashboard.ts` 趋势图 / 待办 / 消息 | router.go:599 / 600 / 601 | TODO 已删(2026-10-26);**2026-10-30 M116 前端才真的接上**(此前只是清了后端源码里的 TODO 注释,前端一次都没调过这三个接口) |

**另有一处不是"缺端点",是"路径撞车"(2026-10-26 新发现)**:
`GET /biz/issues/statistics` 在后端会打到 `GET /biz/issues/:id`(router.go:443),
把字面量 `statistics` 当成问题 id → 返"问题不存在",**不是 404**,比 404 更难查。
补后端端点是安全的(Hertz 路由树**静态段优先于 `:param`**,hertz@v0.9.6
`pkg/route/tree.go:476-491`),两条路由可以共存。详见 api/issue.ts 里的注释。

#### 🔧 2026-10-26 修:前端 API 调用漏 group 前缀(77 处,必然 404)

前端 API 路径是**相对 baseURL**(`/rapid-admin/v1`)的,后端把业务端点挂在
`biz := api.Group("/biz")` 组下 —— 差一个 `/biz`,每一个都必然 404。
`asset / house / issue / owner / upload` 五个模块**全部中招**,
静默到诡异:axios 收到 404 → 弹个"资源不存在"的 toast → 页面照常渲染 → CI 全绿。

用户报"问题列表打开是 404",查下来是**两处独立失效叠加**:
① 前端路由表里根本没有 `/issue/list` 这条路由(见 §2.8,commit 01c0d2e);
② 这 77 处 API 调用全漏 group 前缀(本次修复)。
两者都修完,页面才真的能打开 —— 只修一个还是 404。

新增守门网 `web/src/api/__tests__/api-route-parity.test.ts`(6 条断言),
配套扫描器 `web/src/test/apiRouteScan.ts`。
**可逆探针**:去掉任一调用前的 `/biz` → 立刻红,并报出是哪个文件的哪一行
打到了哪个不存在的路径。

#### ❌ 未完成(0 项)

> **范围外,不做**(2026-11-03 用户拍板,移出账本不进计数):原 #13 **业主/家庭/资料/资产/问题等业务 API 类型** —— 类型定义本身是**业务线契约**,不是框架原语;生成机制已就绪(M23 `pkg/swaggerx/` 自动生成 OpenAPI 3.0,见 #14),业务页按需补类型即可。

| # | 子项 | 当前缺口 | 优先级 |
|---|---|---|---|
| 14 | OpenAPI / Swagger 客户端生成 | ✅ M23:`pkg/swaggerx/` 从 h.Routes() 自动生成 OpenAPI 3.0 + Swagger UI(commit 8968a46) | P1 ✅ |
| 15 | 用户详情缺 `departments` 字段 | ✅ M5d(2026-10-25):`GET /system/users/:id` 返 `departments[]`(全部部门 + `is_primary` 主部门标记,主部门排最前);`Get` 的返回类型从 `*model.SysUser` 改成 `*service.UserDetail`(**嵌入** SysUser,JSON 形状不变只多一个字段)。前端编辑弹窗的"部门"从只读改回可编辑 —— 回填用详情里的**全部**部门,发出去的 `department_ids` 是改完之后完整的全集(后端是清空重建式全量替换,少一个就洗掉一个),没改一个字段都不发。**顺带修了一个同源的洞**:`UserListItem` 里 `department_id` / `department_name` 从没人赋值 → 前端"部门"列恒为空,现在按主部门填上(一条 SQL 取本页所有用户,不是 N+1) | P1 ✅ |

> **复用价值**:**高** —— 业务 API 类型是后端契约落地的关键
> **P1 #14 已完成**(M23 `pkg/swaggerx/`)

---

## 3. ✅ 框架层 11 个 P0 风险(100%,原 11 项 —— #10 业务 endpoint 属业务线已移出)

按"上线阻塞度"排:

#### ✅ 已完成(10 个)

| # | 风险 | 类别 | 状态 | Commit | 阻塞点 |
|---|---|---|---|---|---|
| 1 | **OpenTelemetry trace 导出** | 1.8 | ✅ M4(**M99 加固**:span 名基数 / carrier 写错头 / OTLP endpoint 带 scheme) | 3b15550 / M99 | 上线后出问题无法排查 |
| 2 | **Prometheus metrics + 健康检查** | 1.8 | ✅ M3 + M8(**M99 加固**:响应大小恒 0 / 404 基数 / 业务埋点必 panic / 内容协商死代码) | ba0aa84 / 5384c36 / M99 | 上线后不知死活 |
| 3 | **CORS 中间件** | 1.3 | ✅ 已实装(stale doc,v2.0 误标) | (e64f713 之前已合) | 前后端分离必须 |
| 4 | **超时熔断 per-route** | 1.3 | ✅ M5(**M105 加固**:超时判据恒假 → 返回空 200 而不是 504、熔断不把超时计为失败;熔断快速失败**没 Abort** → 下游照跑且响应被盖成畸形 JSON;`failure_rate_threshold` 是死配置键) | 40070d9 / M105 | 下游故障拖垮 |
| 5 | **PII 解密审计** | 1.5 | ✅ M12(**M100 加固**:这张表**以前永远只能存 1 行** + ip/ua/trace_id/request_id 三条关联链全断 + nil ctx 会 panic) | 6ed5a4e / M100 | 法规要求(等保) |
| 6 | **`is_primary` 唯一约束** | 1.1 | ✅ M1 | 32d7c4b | 数据一致性 |
| 7 | **缓存三防** | 1.7 | ✅ M7(**M90 收口**:唯一实现 + 迁入生产) | e3a1719 / M90 | 高并发场景必备 |
| 8 | **阿里云 OSS provider** | 1.10 | ✅ M10 | 073bd61 | 业务页需要 |
| 9 | **资源代码表 + UI 一致性保障** | 1.1 | ✅ M2 | 9154937 | 新接口漏挂 casbin |
| 11 | **后端错误码 i18n** | 1.4 + 2.5 | ✅ M6 | 7c406c7 | 业主端英文显示 |

#### ❌ 未完成(0 项)

> **范围外,不做**(2026-11-03 确认,移出账本不进计数):原 #10 **后端业务 API 23 个 endpoint 补完** —— 业务线,非框架欠账(本节口径说明本来就是这么写的:框架层自己该做的都做完了)。

> **P0 进度**:框架级 10/10 **全部收口**;原 #10(23 个业务 endpoint,~5-7 天)属**业务线**,不在框架层账本内
> **重大节点**:所有"框架级 P0"全部扫完,只剩业务实现
> **口径说明(2026-10-26 M92)**:这一节按"**上线阻塞度**"排,不是"框架层欠账"排 ——
> #10 要补的是业务接口,**框架层自己该做的都做完了**。
> **2026-11-02 补一条保留意见**:**框架层还有一件没接线的事**(§1.13 #2 —— 业务查询的项目过滤
> 仍由**调用方**传,不传就是不过滤)。它**不算上线阻塞**(首期单租户、库里也只有一个项目),
> **但多租户上线之前必做** —— 所以它进 §1.13 而不是本节。
> ⚠️ **2026-11-02(M161)之后这条保留意见作废**:业务查询**已经接上数据范围**了
> (17 个业务域 / 36 处调用点,规则从**身份**推导,不由调用方传);
> 多租户上线前要做的**只剩"给相关角色真的配一条规则"** —— 那是配置动作,不是框架欠账。
> **(#10 在下面的 ❌ 块里 —— 块按状态分组,不按编号排序)**

---

## 4. 框架层 14 个 P1 价值项(100%)

> **2026-10-26(M92)状态对账**:这一节原来是 2026-10-19 的**排期表**(估成本、按 Q4-Q1 推)。
> 逐条核对代码之后:**12 项已落地**、1 项是**业务线**(框架侧已就位)、
> **只剩 1 项真没做**(#13 短信模板审核流)。下面的「状态」列是核对后的结论,不是照抄旧表。
>
> **2026-10-30(M115)结项**:#13 短信模板审核流**已落地**(见 §1.9 #14)——
> **框架层 14 个 P1 项全部做完**,这一节不再有 ❌ 块。

按"可复用价值高"排:

#### ✅ 已完成(13 个)

| # | 项 | 类别 | 价值 | 成本 | 状态 |
|---|---|---|---|---|---|
| 1 | 通用 cache wrapper(三级缓存) | 1.7 | 高 | 3 天 | ✅ M7 三防(`pkg/cachex/safe.go`)+ M35 L1 LRU / L2 Redis 二级(`pkg/cachex/twolevel.go`) |
| 2 | 错误码集中文档化 + i18n | 1.4 + 2.5 | 高 | 2 天 | ✅ M13 自动生成 `docs/reference/error-codes.md`(**54 码**)+ M6 前端 `tError` 三级 fallback;**M98** 收口三份拷贝 → `errcode.HTTPStatus()` 单一真源,文档补齐 10 个漏码 |
| 3 | 密钥轮转 + KMS 集成 | 1.5 | 高 | 5 天 | ✅ M32 轮转 / 分级 / 多 key 共存 + M36 `pkg/kms/` Provider 抽象(aliyun/tencent/vault 留 TODO 位) |
| 4 | OpenAPI / Swagger 文档生成 | 2.10 | 高 | 2 天 | ✅ M23 `pkg/swaggerx/` 从 `h.Routes()` 自动生成 OpenAPI 3.0 + Swagger UI |
| 5 | 表格导出 / 列设置 | 2.8 | 中 | 3 天 | ✅ M56 `useTablePro.exportCSV()` + `ColumnSettings` 弹窗(显隐 / 列宽 / 恢复默认);**M111 复核**:这一层此前**一条网都没有**,而实现体本身有 8 个真 bug —— 虚拟滚动**从来没打开过**、CSV 表头是 `[object Object]`、公式注入不中和、`rowKey` 死参数、`hideable:false` 的列能被旧存储永久藏起、换 `storageKey` 不重读、锚点没挂 document、`locale` 被吞 —— 全部已修 + 补 100 条用例(见 §2.8.2) |
| 6 | 路由多 tab | 2.8 | 中 | 2 天 | ✅ M17 `stores/tabs.ts` tab stack + 路由 `meta.keepAlive` 缓存 |
| 7 | 主题切换(亮 / 暗) | 2.8 | 中 | 1 天 | ✅ M16 `useUIStore.theme` + `ConfigProvider` algorithm + CSS 变量覆盖 |
| 8 | 错误上报(Sentry) | 2.5 | 中 | 1 天 | ✅ M27 `web/src/utils/sentry.ts` opt-in DSN;**M109 复核**:全局捕获**只留一道**(SDK 接管) + role 取自当前身份 + 死选项清理 |
| 10 | 通知中心(站内信 + 邮件) | 1.9 | 中 | 5 天 | ✅ M34 站内信(`pkg/inbox/`)+ M51 邮件(`pkg/emailx/`)+ M4a `pkg/notifyx/` 多通道分发器接线 |
| 11 | 短信发送审计 | 1.9 | 中 | 1 天 | ✅ M14 `pkg/smsaudit/` 装饰 Sender,落 `sys_sms_send_log` |
| 12 | 短信限流(per phone / per template) | 1.9 | 中 | 1 天 | ✅ M15 `pkg/smslimit/` Redis 固定窗口(per phone + per scene);**🔴 M112 修正**:旧实现的"固定窗口"每次都续 TTL ⇒ 计数器永不过期 ⇒ 超限手机号被**永久拉黑**,已改 Lua 原子脚本(TTL 只在创建时设一次) |
| 13 | 短信模板审核流 | 1.9 | 中 | 2 天 | ✅ **M115**(2026-10-30):`pkg/smstpl/` 状态机 + 登记册(2 表)+ 守卫(off/warn/block 三档策略)+ 阿里云 `GetSmsTemplate` 查询 + 10 分钟 cronx 定时同步 + 后台页(看/停用/同步/历史);**硬规则**:人工无权判定通过、人工停用不被服务商结论覆盖;console provider 无审核状态 ⇒ 只能 `unknown`(见 §1.9 #14) |
| 14 | Idempotency-Key 框架级处理 | 1.3 | 中 | 1 天 | ✅ M9c `pkg/idempotentx/` Redis 锁 30s + 结果缓存 24h + 写操作中间件;**M97 加固**:不缓存「业务没跑成」的 401/403/429(旧实现缓存 401 → 前端刷新 token 后用同一个 key 重发被重放 → 写操作永远不执行)+ 键空间单射 + panic 释放锁;补 19 个单测;**M110 加固(前端侧)**:客户端生成的键此前在非安全上下文下降级到 `Math.random`(不是密码学安全的随机源)⇒ 改用 `crypto.getRandomValues` 拼 UUID v4(见 §2.3.2 B6) |

#### ⚠️ 业务线(1 个 —— 框架侧已就位,不计入框架层完成度)

| # | 项 | 类别 | 价值 | 成本 | 说明 |
|---|---|---|---|---|---|
| 9 | 业务级 metrics 埋点 | 1.8 | 中 | 3 天 | 框架埋点原语已给(`metricsx.BusinessCounter` / `Gauge` / `Histogram` + `ObserveDuration`),**埋点内容(登录成功率 / 业务慢查询)属业务线** —— 同 §1.8 的 📋 块 |

#### ❌ 未完成(0 项)

> **2026-10-30(M115)**:#13 短信模板审核流落地后,这一节**清零**。
> (此前唯一的欠账就是它 —— 框架层 14 个 P1 项现在全部做完。)

> **总成本**:原盘点估 **~32 天 / 2 人** → **已全部落地**
> **框架层还开着的**:**无**(P1 段清零;P2 段见 §5)
> **不计入框架层欠账**:§4 #9(业务埋点内容属业务线)

---

## 5. ✅ 框架层 P2 长期项(100%,原 11 项 —— #1 RS256 / #6 异地多活 / #10 移动端 / #11 能力清单 / #9 画布式设计器已拍板移出)

> **2026-10-26(M92)状态对账**:10 项里 **6 项已落地**、**4 项仍开着**
> (RS256 / 异地多活 / 表单设计器 / 移动端)。后两项取决于业务形态,不是"欠着的活"。
> **2026-11-02**:第 5 项开着 —— 「一份能力清单取代三份码表」(§1.1 #17);
> **10 项 → 11 项,60% → 55%**。
>
> **2026-11-02(第二次)**:外部材料建议「**RS256 / 异地多活 / 移动端 —— 明确不建议现在加**」——
> **与本清单原判一致**(这三项本来就判过是"按原定计划没排进首期",不是欠账),**维持原判不动**。
> 但材料点的另外三项**没有**进 §5,理由各不相同:
> ① **「领域事件总线」⇒ 新开 §1.14** —— 它是**框架能力本体**(不是"锦上添花的长期项"),
> 放 P2 这个位置等于把它埋掉(§5 的口径就是"按原定计划没排进首期");
> ② **「一份能力清单」⇒ 已在 §1.1 #17 + §5 #11**(不重复入账);
> ③ **「数据范围规则」⇒ 已在 §1.13** —— 框架层 M155/M156 **两天前刚落地**,
> 欠的是**接线**(不是"框架层没有")。

#### ✅ 已完成(6 个)

| # | 项 | 类别 | 备注 | 状态 |
|---|---|---|---|---|
| 2 | token 主动刷新调度 | 1.2 | 用户体验优化 | ✅ M18 `utils/tokenScheduler.ts` 单飞锁 + 提前 5min 续期(2026-10-25 M8 补跨标签页恢复链) |
| 3 | 灰度发布 / 蓝绿 | 1.3 | 上规模再做 | ✅ **框架侧** M40 `pkg/grayx/` 灰度 header 透传 + cohort 打标;**蓝绿部署本身属运维** |
| 4 | 链路采样率 | 1.8 | 上规模再做 | ✅ M4 `otel.sample_ratio` 可配 + `sdktrace.TraceIDRatioBased` |
| 5 | APM 接入(Datadog/SkyWalking) | 1.8 | 看预算 | ✅ **阿里云 ARMS APM**(OTLP gRPC:4317);Datadog / SkyWalking 不再接(§1.8 #7a) |
| 7 | SSE / WebSocket 鉴权 | 1.3 | 看业务 | ✅ M26 `api/middleware/sseauth.go`(Header / query / cookie 三级兜底);**M105 加固**:此前只有 jti 一道闸 → 被"强制下线"踢掉的设备**照样能连 SSE**(还能再活 8 小时);现补齐会话吊销 + 作废线两道闸 |
| 8 | 接口签名 | 1.3 | 高安全场景 | ✅ M44 `pkg/signx/` HMAC-SHA256 + `api/middleware/sign.go` |

#### ❌ 未完成(0 项)

| # | 项 | 类别 | 备注 | 状态 |
|---|---|---|---|---|
| 9 | 拖拽表单 / 流程设计器 | 2.7 | 业务可视化 | ✅ **收口**(2026-11-03):①"声明即用"那一半已落地 —— **M178 通用 SchemaForm**(§2.7 #15,静态业务页声明 schema 数组即出表单);②剩余的**拖拽画布式搭建**与**审批流程可视化设计器**属**产品级搭建器**而非框架原语,经用户拍板移出账本(范围外,见下方说明) |
> - 原 #6 **异地多活** —— 部署形态,不是框架代码,单机/主备够用前不立项。
> - 原 #9 剩余部分(**拖拽画布式**搭建 / **审批流程可视化设计器**)—— 产品级搭建器而非框架原语;"声明即用"那一半已由 M178 通用 SchemaForm 落地(§2.7 #15)。
> - 原 #11 **一份「能力清单」取代三份码表**(menu / button / api)—— 本体**暂缓不做**:3-5 天 seed 结构重构不新增框架能力,漂移风险已由止血网覆盖(§1.1 的范围外说明);技术盘点与目标形态全文保留在 §1.1。

> **范围外,不做**(2026-11-03 用户拍板,移出账本不进计数):
> - 原 #1 **JWT 切换 RS256** —— 单体后端签发/验签同进程,secret 不出服务器,RS256(非对称)带不来实际安全增益反而多一套密钥管理;HS256 够用。哪天拆出"第三方服务要验 token 但不该持有签发权"的架构再重新立项(技术记录见 §1.2)。
> - 原 #10 **移动端 / 响应式** —— PC 后台,移动端不是当前目标(详见 §2.8 的范围外说明)。

> **口径**:P2 是"可后置"清单 —— 55% 不代表"欠着 45% 的债",而是 5 项**按原定计划就没排进首期**。

---

## 6. 框架层 10 类 ★★★ 高价值可复用模块(100%)

如果一个框架"成熟到能套其他项目",这 10 类必须做扎实:

> **「当前完整度」列是自引用的** —— `**92%** (§1.2)` 里的百分比**必须等于**那一节
> 标题上的百分比;`scripts/check_backlog_consistency.py` 逐格核对。
> **改了一节忘了改这里,守门网直接红。**

#### ✅ 已完成(10 个 —— 达 90% 以上)

| # | 模块 | 当前完整度 | 距"成熟"差什么 |
|---|---|---|---|
| 1 | **RBAC 鉴权** | **100%** (§1.1) | 无 —— 「能力清单本体」(原 §1.1 #17)已于 2026-11-03 经用户拍板**暂缓不做**(止血网 M170 已覆盖漂移风险,见 §1.1 范围外说明);其余 17 项子项全完成(菜单 / 角色 / 资源码 / 按钮粒度 / 权限实时刷新;审计死菜单 #18 M169 已补) |
| 2 | **JWT + 多 audience** | **100%** (§1.2) | 无 —— RS256 已于 2026-11-03 经用户拍板移出账本(范围外,见 §5 范围外说明) |
| 3 | **可观测性** | **100%** (§1.8) | 框架侧无欠账;业务埋点内容 / 告警规则 / 日志聚合属**业务与运维侧**(§1.8 的 📋 块) |
| 4 | **缓存层** | **100%** (§1.7) | 框架侧无欠账(2026-10-26 M95:`@Cacheable` 收回 + 字典接上 cachex L2;**M96**:Redis 真路径用 miniredis 补上单测,当场修掉 glob 越界删除 + TTL 语义不一致两个真 bug)。**业务侧仍在接**:系统配置直查 DB(故意不接,含密钥)/ 预热项仍为空 |
| 5 | **错误码体系** | **100%** (§1.4) | 无 —— 11 项全完成(自动生成 `docs/reference/error-codes.md` + 前端 i18n 映射) |
| 6 | **PII 加密** | **100%** (§1.5) | 无 —— 10 项全完成(轮转 / 分级 / KMS / 密文盲索引搜索);envelope 物理密钥归档属运维侧 |
| 7 | **前端框架层**(通用组件等 10 大类) | **100%** (§2.1-§2.10 合计) | 无 —— 保存的列表视图(**M176 useSavedViews**)/ 通用 SchemaForm(**M178**)/ 数据级权限(§2.2 #10 收编 §1.13)全部收口;移动端与业务 API 类型经用户拍板移出(范围外) |
| 8 | **i18n** | **100%** (§2.6) | 无 —— 10 项全完成(懒加载语言包 **M173**:en-US 拆独立异步 chunk / 语言切换 UI / antd locale / 日期数字 locale) |
| 9 | **数据可见性**(行级 + 字段级) | **100%** (§1.13) | 无 —— 行级(M155/M156/M157/M160/M161)与字段级(M162/M163/M164 + **前端消费 M168**)两轴全部落地;交叉守门网 M166 + 出口守门网 M167 都在 |
| 10 | **领域事件总线 / 事务性 outbox** | **100%** (§1.14) | 无 —— **M175(2026-11-03)**:`pkg/eventx` 原语 + outbox 表与投递器(退避 / 死信 / at-least-once)+ 消费幂等 Deduper + 两个手写 publisher 收口(`WithOutbox` 变体,线字节对拍);6 条可逆探针全部转红→复原→绿 |

#### ❌ 未完成(0 项 —— 全部已收)

> **结论**:这 10 类做到 90%+,框架就能"成熟稳定,套其他项目"。
> **2026-11-01 附注**:本节量的是「**框架能力**」的成熟度;
> 「**能不能被别人拿起来用**」另见 **§8(开源通用化 5 项,当前 100%)** ——
> 那 5 项不计入 §1/§2 的完成度,两张表的口径不同。
> **2026-10-26(M95)现状**:**8 类全部达 90% 以上**(RBAC 100% / 可观测性 100% / 错误码 100% / PII 100% / 缓存层 100% / 前端框架层 92% / JWT 92% / i18n 90%)—— **达标线达成**。
> **2026-11-02(第一次)**:RBAC 由 100% 降到 **89%** —— §1.1 #17「一份能力清单取代三份码表」入账,
> 并在复查时撞出 §1.1 #18(平台运营负责人的四个审计菜单零接口权 ⇒ 必 403)。
> **2026-11-02(第二次)**:本节从 **8 类扩到 10 类** —— 补进「数据可见性」(§1.13)与
> 「领域事件总线」(§1.14)。**8 类达标、2 类不达标** ⇒
> **「成熟稳定」这个结论现在不成立**;补完这两类即成立(§1.13 主要是**接线**,§1.14 是**新建**)。
> ⚠️ **这次扩类不是"降低标准"** —— 这两类此前**根本不在清单上**,
> 不记就等于"不存在":一类在仓里只做了一半(§1.13 规则层已落地、业务层没接),
> 一类**一个包都没有**(§1.14)。
> **2026-11-02(第三次,M161)**:§1.13 的"业务层没接"**已经接上**
> (17 个业务域 / 36 处调用点 + 三张账本 + 启动期 fail-fast + 扫源码守门网)
> ⇒ §1.13 **60% → 80%**,**只剩字段级**没做 ⇒ 那一类当时**只差字段级**就达标。
> **2026-11-02(第四次,M162-M164)**:字段级那一轴的**服务端一半 + 配置界面**已落地
> (规则层 + 执行层 + 角色页「字段可见性」抽屉 + 手册一章 `server/docs/security/fieldpolicy.md`)
> ⇒ §1.13 **80% → 83%**,当时还剩**两条尾巴**(两个登记表之间没有交叉守门网 / 前端消费那一半)。
> ⚠️ **前端那一半记在 §2.7 #16** —— 它是前端框架层的欠账,本节只记服务端这一侧。
> ⇒ 这一类**还没到 90% 达标线**,差的就是那两条尾巴。
> **2026-11-02(第五次,M166)**:两个登记表之间的**交叉守门网**补上了
> (行级账本里的每个域都必须在字段层登记:启动期 fail-fast + 单测两头都守着,
> 且**启动期那个调用点**也被扫源码守着)
> ⇒ §1.13 #5 只剩**一条尾巴**(前端消费那一半)⇒ **百分比仍是 83%**(5/6,没动 —— 尾巴没关掉就不许涨)。
> **2026-11-02(第六次,M167)**:§1.13 #4 再补一层 —— **出口绕不过去也有网**
> (`internal/api/handler/outlet_guard_test.go` **扫源码**:绕过出口的写入点必须逐条登记 +
> 写明理由 + **条数精确**;今天账本 10 条,**8 条可逆探针实测全部转红**)
> ⇒ **百分比仍是 83%**(5/6)—— 这是对 #4 的**加强**,不是新尾巴;
> #5 的尾巴(前端消费那一半)没关掉,就不许涨。
> **2026-11-02(第七次,M168)**:前端消费那一半落地 ⇒ §1.13 #5 / §2.7 #16 同时关掉
> ⇒ §1.13 **83% → 100%** —— **这一类达标**(9/10);前端合计 106/116 → 107/116 = **92%**。
> **2026-10-26(M92)存档**:**7 类已达 90% 以上**,当时**只剩缓存层 89%**,卡在 `@Cacheable` 注解这一个 P2 上(M95 已把它收回)。
> **2026-10-19 进展**(M1-M11,历史存档):RBAC 100% / 错误码 95% / 可观测性 100% / 缓存 90% / i18n 80%,当时只有 5 类达标或接近。
> **合计**:后端框架层 §1.1-§1.14 = **159/159 = 100%**;前端框架层 §2.1-§2.10 = **114/114 = 100%**。
> (后端 97% → 91%:**不是变差了,是账本补齐了** —— 新补进来的 13 个子项此前一个都没被数过。)
> **最大残留**:框架层欠账**全部收口或移出**(2026-11-03,第十七轮)——
> 「能力清单本体」(§1.1 原 #17)与「审批适配器 / 字段类型补齐」(§1.12 原 #15 / #12)**暂缓**、
> RS256 / 异地多活 / 移动端 / 画布式设计器 / 业务 API 类型 / 业务 endpoint **范围外移出**(各有墓碑注记);
> (**审计死菜单 §1.1 #18 已收** —— M169 补三个只读码,2026-11-02;
> **数据可见性 §1.13 已 100% 收口** —— 前端消费那一半 M168 于 2026-11-02 落地;
> **领域事件总线 §1.14 已 100% 收口** —— M175,2026-11-03);
> 23 个业务 endpoint(§3 原 #10)属**业务线**,不算框架层欠账。

---

## 7. 12/15 之前框架补完路径(2 人 × 4 周)

### Week 1(P0 风险 1-4)— ✅ 2026-10-19 全部完成
- Day 1-2:OpenTelemetry ✅(M4 3b15550)+ 健康检查 ✅(M3 ba0aa84)+ Prometheus ✅(M8 5384c36)
- Day 3:CORS ✅(已在 router.go:10-37,2026-10-19 之前实装)
- Day 4-5:超时熔断 per-route ✅(M5 40070d9)

### Week 2(P0 风险 5-7)— ✅ 2026-10-19 全部完成
- Day 6-8:PII 解密审计(等保要求)— ✅ M12(commit 6ed5a4e)
- Day 9:`is_primary` 唯一约束 ✅(M1 32d7c4b)
- Day 10:缓存三防 ✅(M7 e3a1719)

### Week 3(P0 风险 8-9)— ✅ 2026-10-19 全部完成
- Day 11-12:阿里云 OSS provider ✅(M10 073bd61)
- Day 13-14:资源代码表 + UI 一致性校验 ✅(M2 9154937)

### Week 4(P0 风险 10-11)— 🟡 1/2 完成
- Day 15-20:后端业务 API 补完(23 个 endpoint,按 6 个文件分布)— ❌ 待做(大头)
- Day 21:后端错误码 i18n ✅(M6 7c406c7)

> 4 周做不完的进 Q4-Q1 backlog(§4)
> **当前 P0 缺口**:**仅 23 个业务 endpoint 补完(#10)**—— 大头,业务线
> **本批次额外完成**(非 P0 但顺带做):Gzip 压缩(M9a 63d98a4)/ 请求体大小限制(M9b 3b9cb95)/ Idempotency-Key(M9c 93dc607)/ 前端 AbortController(M11 9639fd5)/ 站内信(M34 5e60f79)/ 二级缓存(M35 c2aaea8)/ KMS(M36 8a8d9ab)/ 并发原语(M37 432e03d)/ 缓存监控(M38 c2b9aef)/ 缓存预热(M39 2e1b9ec)/ 灰度(M40 6a1ba5e)/ 限流(M41)/ panic 兜底(M42)/ API 版本(M43)/ 接口签名(M44)/ 异地登录检测(M45)/ 加密搜索(M46)/ 请求体 gzip(M47)/ 安全响应头(M48)/ /version(M49)/ 安全 token(M50)

---

## 8. 开源通用化 5 项(100%)

> **2026-11-01 新增**:本仓库要作为**通用后台管理平台**开源,
> 让别人"clone 下来 → 改个名字 → 就能做自己的后台"。
> 这一节记的是**开放化**的欠账 —— 它跟 §1/§2 是**两回事**:
> §1/§2 问的是"**这个框架能力有没有**",§8 问的是"**别人拿不拿得起来**"。
> 一个能力可以"有"但"没法用"(swagger 就是典型:能自动生成、能挂 UI,
> 但没鉴权闸、没字段级 schema、资产在 Go module 里、后台也点不进去)——
> 那正是 §8 要记的东西。
>
> ⚠️ **§8 的 5 个子项横跨前后端,不计入 §1.1-§1.14 / §2.1-§2.10 的完成度** ——
> 那两张表算的是"框架能力",§8 算的是"可交付性"。两张表的数**不因本节改动**。
> (§8.2 是 2026-11-02 新增的**开放化补记**,同样不计入 §8 的 5 项 —— 见该小节说明。)

#### ✅ 已完成(5 个)

| # | 子项 | 证据 |
|---|---|---|
| 1 | Swagger 自动生成 OpenAPI 3.0 + UI 挂载 | `server/internal/pkg/swaggerx/`(6 文件 1005 行:`spec.go` 574 / `mount.go` 211 / `auth.go` 95 / `rbac_codes.go` 21 / `json.go` 9 / 测试另计)+ `server/swagger/`(UI 资产内嵌,`embed.go` 42 行)+ `server/internal/api/router/router.go` 挂 `/swagger`;M23(commit 8968a46)。**2026-11-01 实测**:`:8888/swagger/doc.json` → 200 / 350385B(183 paths / 252 operations),`/swagger/index.html` → 200 |
| 2 | **Swagger 开放化补齐**(5 个子项,见 §8.1) | **2026-11-01 全部落地**:①鉴权闸(G1)+ ②标题可配置(G2,随 #5 一并解决)+ ③**字段级 schema**(G3)+ ④资产本地化(G4)+ ⑤后台菜单入口(G5)。**字段级 schema(G3)**:①**AST 分析器 + 生成器** `server/cmd/gen-swagger-schema/`(`analyze.go` + `main.go`)—— 静态读源码,找出每个 handler 用 `h.BindJSON(c, &X)` 绑的是哪个 DTO;**不引 x/tools/go/packages**(理由同 `internal/pkg/jsonid`),代价是类型判定按名字猜。②**生成物** `server/internal/api/handler/v1/swagger_dto_gen.go`(591 行)—— `RequestDTOs`(handler 方法 → DTO)+ `RequestFieldDocs`(组件名 → 字段说明,取自源码**行尾注释**:本仓 DTO 的字段语义就写在那一行,没有集中的字段字典)。③**运行期反射渲染器** `server/internal/pkg/swaggerx/schema.go` —— json tag 决定字段名、`,string` 决定 wire 上是不是字符串、指针 → nullable、嵌入结构体 → 字段提升、`time.Time`/`sql.NullTime`/`gorm.DeletedAt` → date-time、**ID 以字符串上 wire**。④**接线** `spec.go`(每条写接口挂 `x-request-schema`)+ `router.go` 的 `swaggerx.SetSchemaSource(...)`。⑤**诚实配套**:`x-schema-coverage` 把覆盖率写进文档(实测 **routes=183 / write=125 / with=104 / 83%**)—— 覆盖率取决于源码怎么写,把它写进去是为了让"这份文档的字段契约有多全"变成可核对的数字。**测试**:`cmd/gen-swagger-schema/main_test.go` 4 例(**漂移网**:重跑生成器跟仓库里那份逐字比)+ `internal/pkg/swaggerx/schema_test.go` 8 例 + `internal/api/router/swagger_schema_test.go` 5 例(端到端);**4 条可逆探针实测全红**(拆 SetSchemaSource 接线 / HandlerKey 不剥 `-fm` / `isRequired` 改成猜 omitempty / 扰动生成物)。⚠️ **写测试时挖出一个真 bug**:`jsonid.IDList` 的元素曾被渲染成 `integer`(而 wire 上是字符串)—— "有 MarshalJSON 就是字符串"那条判定放在了 `structSchema` 里,`jsonid.ID` 的 Kind 是 Uint64,整数分支先返回了,**根本走不到那条判定**;已把判定提到 `baseSchema` 的 kind 分支之前 |
| 3 | **GitBook 格式框架文档** | **2026-11-01 已落地**。①**位置**:`server/docs/` —— GitBook(HonKit)源目录,**56 页**。②**结构**:`README.md`(落地页)+ `SUMMARY.md`(目录,GitBook 的硬要求)+ `book.json`(标题 / 插件 / 样式)+ `styles/website.css` + `favicon.ico`;`getting-started/`(快速开始 / 配置体系 / 框架约定)× 3 + `reference/`(包速查表 / 示例目录 / 配置项全表)× 3 + **49 个框架模块各一章**(按 7 组分目录:基础设施 17 / 存储与缓存 5 / 安全与合规 11 / 审计 3 / 消息与通知 9 / 调度与可观测 3 / 接口文档 1)。③**每章统一五段式**:**功能 / 配置 / 使用方法 / 注意事项 / 相关** —— 「相关」一律带源码路径 + `go run ./examples/<包名>` + 相对链接。④**依赖顺序兑现**:#4 先落地,#3 后写 ⇒ **文档里引用的示例全部是已经能跑的代码**。⑤**构建**:`server/docs/package.json` 固定 `honkit@6.2.5` + 8 个 classic `gitbook-plugin-*`(honkit **不自带**这些插件,必须 npm 装)⇒ `npm install && npm run build` 出静态站到 server/_book/(gitignore),`npm run serve` 本地预览。**构建实测**:`found 56 pages / 4 asset files / generation finished with success`。⑥**漂移网** `scripts/check_docs_links.py` —— ①SUMMARY 列的页面都存在 ②每个 .md 都被 SUMMARY 收录(孤儿页 ⇒ 红)③章节里所有相对链接都能解析 ④引用的 `internal/pkg/<包>` 目录存在 ⑤引用的 `examples/<示例>` 目录存在;**实测通过:56 页 / 56 条目录项 / 链接与包引用全部可解析**。⑦**写法纪律**:章节里的每个反引号标识符逐个 grep 过源码(yaml 键逐行对 `configs/config.example.yaml`)—— 过程中**纠出自己写的 3 处错**:authz 的断言 API 名字(`authz.Require` 不存在,真接口是 `NewGenericAssertor().AssertCan`)、错误码段位(`PayloadTooLarge` 是 `41300` 不是 `41xxx`)、`job.time_zone` 键名(真键是 `job.timezone`);另修 `getting-started` 里 4 处与代码不符的事实(登录路径 `/rapid-admin/v1/public/auth/login`、健康检查 `/healthz`、mysql 键名 `username`/`dbname`、jwt 键名 `admin_secret` 且**无默认值**、"首次登录强制改密"这个机制**根本不存在**)。⑧**诚实配套**:每章的「注意事项」写的是**源码里挖出来的真坑**(不是泛泛建议)—— 如 `emailx.UseTLS` 声明了但**从来没有 `tls.Dial`**(465 + use_tls 不建隐式 TLS)、`randomx.FromCharset` 在字符集长度为 256 倍数时**除零 panic**、`retryx.Multiplier<=1` 被静默改成 2、`metricsx` 的 `metrics.enabled:false` **并不停止采集**、`apiverx` 超限返 **500 不是 422**、`grayx` 的中间件**没挂在任何路由上**、`signx` 的 `middleware.Sign` **零调用方**、`idempotentx` 在 Redis 没配时 **fail-open**(幂等静默失效)且**前端每个写请求自动生成新 UUID ⇒ 双击提交 = 两个 key = 幂等不生效** |
| 4 | **examples 目录最小示例** | **2026-11-01 已落地**。①**目录与约定**:`server/examples/<包名>/main.go` —— 一包一个目录、`package main` + `func main()`,可 `go run ./examples/<包名>` 单独跑;`server/examples/doc.go` 写清约定;**`server/examples/README.md` 是索引表**(49 个包 → 这个示例演示什么,分 7 组)。②**覆盖度**:`internal/pkg` 下 **49 个一级包目录,49 个示例,一个不缺**。③**漂移网** `server/examples/examples_test.go` 4 条测试 —— ①每个框架包都要有同名示例目录 ②**反向漂移**(没有多余示例目录)③每个示例都必须是 `package main` + `func main()`(`go/parser` 校验)④**不许 import 业务包**(`internal/service` / `internal/model` / `internal/api` / `cmd` —— 别人 clone 下来要能编译;`internal/config` **不在禁名单里**,它是框架自己的配置层)。④**示例是真跑的代码**:49 个示例**逐个 `go run` 冒烟全过**;需要 Redis / DB / OSS 的用内存实现或 `miniredis` 顶替(不真连,没配也能跑通)。⑤**审计三兄弟手写 DDL** —— piiaudit / cryptaudit / smsaudit 的表在生产上由 AutoMigrate 建,示例里手写 `CREATE TABLE` + 裸 SQL 回查,因为框架示例不许 import 业务模型。⑥**每个示例的文件头注释就是它的说明书**(解决什么问题 / 关键点 / 跑法)—— GitBook 文档(#3)直接引用这些注释,**文档写的示例必须是已经能跑的代码**。⚠️ **写示例时挖出两个真 bug(已修)**:`internal/pkg/smsaudit` —— ①`Shutdown()` **不幂等**,第二次调用 `panic("close of closed channel")`(兄弟包 piiaudit / cryptaudit 在 M100 已改幂等,本包漏了;而"`defer Shutdown()` + 显式调一次"是很容易写出来的组合)②**ctx key 自成一套** —— 全仓唯一注入 request_id / ip / ua 的地方是 `internal/api/middleware/trace.go:92-94`,用的是 **piiaudit** 的 key,而 smsaudit 读自己的私有 key ⇒ `sys_sms_send_log.request_id` / `trace_id` **恒为空**;ip / user_agent **从来没读过** ⇒ 那两列也恒为空(一条短信审计只剩"发了什么",没有"谁发的、哪次请求、从哪台机器来")。**修法**:`smsaudit/ctx.go` 的 `Inject*` 转发给 piiaudit + `extractFromCtx` 改读 piiaudit 的 key(**顺带继承 piiaudit 的 OTel span context 兜底**)+ `Shutdown` 改成幂等(`sync.Once` 放在 Recorder 上,不是包级变量 —— 换 global 也不会误伤别的实例)。**回归测试**:`smsaudit_ctx_test.go`(ctx 传播 + ctx 为 nil 不 panic)+ `smsaudit_shutdown_test.go`(幂等)—— **3 条探针先写后修,实测全红**(request_id 空 / nil ctx panic / close of closed channel)。⚠️ 另有一例同源收获:**pkg/push 的 consumer 缓冲计数漂移**(跟得上的 SSE 连接第 64 条事件就永久卡死,commit `f6d4225`)也是写示例时挖出来的 |
| 5 | **平台名可配置**(web + server) | **2026-11-01 已落地**。①**配置源**:`server/internal/config/branding.go`(新)—— `BrandingConfig{AppName, AppTitle}` + `TabTitle()` 派生,挂在 `server/internal/config/config.go` 的 `Server.Branding`;`server/configs/config.example.yaml` 给了 `branding:` 块(留空 = "没配")。②**下发**:品牌名必须在**登录之前**拿到(登录页标题就是它),而 `sys_config` 读接口挂 `rbac("config","list")` 只有 super_admin 能用 ⇒ 走白名单接口 `GET /rapid-admin/v1/public/config`(`server/internal/api/handler/v1/config.go` 的 `PublicConfigView` 加 `app_name` / `app_title`)。③**消费方**:web 的 `stores/publicConfig.ts` + `components/common/LangRoot.tsx`(浏览器标题)+ `components/layout/Header.tsx`(顶栏品牌)+ `pages/login/index.tsx`(登录页标题)。④**回退**:后端没配 → 下发**空串**(不是 null)→ 前端落回 i18n 语言包(`common.appTitle` / `layout.brandLong` / `login.title`),中英文各自正确,**不在服务端写死中文默认值**。⑤**Swagger 标题复用同一套配置**(`router.swaggerDocTitle(cfg)`,见 §8.1 G2)。**测试**:`server/internal/config/branding_test.go`(派生顺序)+ `server/internal/api/handler/v1/public_config_test.go`(下发 / 空串 / 白名单只 4 个字段)+ `server/internal/api/router/swagger_title_test.go`(派生顺序 + 不许写死项目名)+ `web/src/components/common/__tests__/langRoot.branding.test.tsx`(后端配了 / 没配 / 异步到货)+ `web/src/stores/__tests__/publicConfig.store.test.ts`(品牌字段 + 类型守卫) |

#### ❌ 未完成(0 项)

> §8 的 5 个子项已全部落地(2026-11-01)。**没有欠账。**
>
> **合计**:**5 个子项,已完成 5 个(100%)**;剩余 0 项。
> ⚠️ **成本口径回顾**:两项之间有依赖 —— **#2 的 G2(标题可配置)已随 #5 一并落地**
> (复用同一套 `branding:` 配置源,没做两遍),G1/G3/G4/G5 属于 #2 内部;
> **#4(examples)先于 #3(GitBook 文档)** —— 文档里写的示例必须是**已经能跑的**代码。
> **§8 已收官**(2026-11-01);后续开放化需求另起小节记,别往这张表里塞。

### 8.1 Swagger 开放化:5 个子项的现状(对应 #2)

| # | 子项 | 现状 | 证据 | 缺它的后果 | 成本 |
|---|---|---|---|---|---|
| G1 | `/swagger/*` 无鉴权闸 | ✅ | **2026-11-01 已落地**。①**闸本身**:`server/internal/pkg/swaggerx/auth.go`(新)—— `BasicAuth(cfg)` 返回中间件;`subtle.ConstantTimeCompare` **用户名与密码都比**(只比密码的话用户名就成了公开信息);401 **必带 `WWW-Authenticate: Basic realm=...`**(少了这个头浏览器不弹框,用户只看到一个空 401 页);密码里带冒号按**第一个**冒号切(按最后一个切会把 "a:b:c" 切成 用户 "a:b" / 密码 "c")。②**三态配置**:`server/internal/config/swagger.go`(新)—— `mode=auto`(默认:配了固定凭据就用固定的,没配就**本次启动随机生成**,安全默认 + 零配置 + 不拒绝启动)/ `mode=basic`(**必须**配凭据,否则 `main.go` fail-fast **拒绝启动**,不降级成不设防)/ `mode=none`(显式关 + WARN)。⚠️ **`ResolveAuth` 幂等** —— `main.go` 先调一次做 fail-fast、`router.Register` 再调一次拿结论,两次若各生成一个随机密码,日志里印的就不是生效的那个。③**接线**:`router.go` 把闸挂在**三条路由前面**(doc.json / 静态资产 / 根跳转)—— 静态资产也要凭据,否则 UI 能加载但 spec 拉不到。④**整条端点可下线**:`swagger.enabled=false`。**测试**:`auth_test.go` 8 例(放行 / 密码错 / 用户名错 / 无头 / 坏格式 / 密码带冒号 / 未启用返 nil)+ `config/swagger_test.go` 6 例(三模式 / 幂等 / mode 大小写空格);**10 条可逆探针实测全红**(拆闸 / 只比密码 / 去掉 WWW-Authenticate / 按最后一个冒号切 / 不幂等 / basic 不报错 / 路由没执行) | — | — |
| G2 | 标题硬编码项目名 | ✅ | **2026-11-01 随 §8 #5 一并解决**:标题改由 `router.swaggerDocTitle(cfg)` 派生(优先级 `branding.app_title` > `branding.app_name` > `system.app_name`),`server/internal/api/router/router.go` 里的字面量已删;`server/internal/api/router/swagger_title_test.go` 4 例锁住派生顺序 + 1 例断言"不许写死项目名"(`assert.NotContains(got, "Rapid Admin")`) | — | — |
| G3 | 字段级 schema 全缺 | ✅ | **2026-11-01 已落地**。①**路线**:**AST 分析器 + 生成物 + 漂移测试**(不是 swaggo 注解,也不是手写注册表)。⚠️ **为什么不是手写一张"路由 → DTO"的表**:那张表就是**第二份真相** —— handler 里把 `service.CreateUserRequest` 换成别的类型,表不会跟着变,**而且不会报错**,只会让 Swagger UI 里的字段悄悄对不上实际接口。这里让**源码自己当真相**,生成物只做搬运。②**分析器** `server/cmd/gen-swagger-schema/analyze.go` —— 静态读 handler 源码,认 `var req service.XxxRequest` + `h.BindJSON(c, &req)` 两种写法;绑了**多个不同**类型 ⇒ 判为"猜不准"并跳过(不猜)。**路由 → handler 的映射不用解析 router 源码** —— hertz 运行期就给了 `h.Routes()` 里的 `Handler = utils.NameOfFunction(...)`,形如 `…/v1.(*UserHandler).Create-fm`,剥掉前缀与 `-fm` 即 `(*UserHandler).Create`。③**生成物** `server/internal/api/handler/v1/swagger_dto_gen.go`(591 行)—— `RequestDTOs`(109 个 handler → DTO)+ `RequestFieldDocs`(45 个类型带字段说明)。④**运行期渲染器** `server/internal/pkg/swaggerx/schema.go` —— 反射把 Go 类型翻译成 OpenAPI 3.0:json tag 定字段名 / `,string` 定 wire 类型 / 指针 → nullable / 嵌入结构体 → 字段提升 / `time.Time`·`sql.NullTime`·`gorm.DeletedAt` → date-time / **ID 以字符串上 wire**。⑤**诚实配套**:每条写接口挂 `x-request-schema`(组件名),文档级挂 `x-schema-coverage`(覆盖率)。⚠️ **必填性只认显式标签**(`validate:"required"` / `binding:"required"`)—— 本仓 DTO 大多没这两个标签,而"没有 omitempty"**不等于**"必填"(`CreateUserRequest.Nickname` 就是反例);猜错的后果很具体:**Swagger UI 会拦着不让提交**没填的必填字段。宁可少标必填(服务端会返回明确报错),也不要多标。⑥**漂移网** `cmd/gen-swagger-schema/main_test.go` —— 重跑生成器跟仓库里那份**逐字比**,不一致就红(改了 DTO 却没重跑生成器 ⇒ 这里红)。**测试**:漂移 4 例 + 渲染器 8 例 + 端到端 5 例;**4 条可逆探针实测全红**(拆 `SetSchemaSource` 接线 / `HandlerKey` 不剥 `-fm` / `isRequired` 改成猜 omitempty / 扰动生成物)。⚠️ **写测试时挖出一个真 bug**:`jsonid.IDList` 的元素曾被渲染成 `integer`(wire 上其实是字符串)—— "有 MarshalJSON 就是字符串"那条判定被放在 `structSchema` 里,而 `jsonid.ID` 的 Kind 是 Uint64,**整数分支先返回了,根本走不到那条判定**;已把判定提到 `baseSchema` 的 kind 分支之前。**实测覆盖率**:`routes=183 / write=125 / with=104 / 83%` | — | — |
| G4 | swagger-ui 资产不在项目内 | ✅ | **2026-11-01 已落地**。①**资产进仓**:`server/swagger/ui/`(swagger-ui 5.10.5 官方发行版,Apache-2.0,许可证全文随资产收录)—— `index.html` / `swagger-ui*.js` / `swagger-ui.css` / `favicon-*.png` / `oauth2-redirect.html` 全套,**`//go:embed` 打进二进制**(`server/swagger/embed.go`)⇒ 资产不依赖 Go module cache,任意 CWD / Docker 都可用。②**`index.html` 与 `swagger-initializer.js` 是本仓改写的**:标题中文化 + `lang="zh-CN"` + **保证零外链**(内网/离线部署时外链请求会挂住,页面渲染被拖到超时),spec 指向相对路径 `./doc.json`(挂载点改了不用改文件)。③**丢掉的 .map**:4.6MB source map 对本仓无用却让体积翻倍 ⇒ 不收录(测试钉住)。④**依赖瘦身**:`go mod tidy` 摘掉 `github.com/swaggo/files` + `github.com/swaggo/swag` + `github.com/hertz-contrib/swagger` 三个依赖。⑤**挂载自己实现**:`mount.go` 自己发静态资产(**Content-Type 必须对** —— `.js` 返 text/plain 会被浏览器拒绝执行 ⇒ 页面一片空白且控制台无明显报错),用 `fs.ValidPath` 挡路径穿越;`/swagger` 根路径 302 跳 `index.html`(浏览器访问目录不会自动补 index.html)。**测试**:`server/swagger/embed_test.go` 5 例(文件齐全 / 无 .map / index.html 无外链 / initializer 指向 ./doc.json / 许可证在仓)+ `mount_test.go` 6 例(跳转 / Content-Type / doc.json 合法 OpenAPI / 穿越打不穿 / 鉴权全覆盖 / 路由真被执行);**6 条可逆探针实测全红**(拆 ValidPath 闸 / 绕过鉴权 / 改错 Content-Type / 不跳 index.html) | — | — |
| G5 | 后台无 swagger 入口 | ✅ | **2026-11-01 已落地**。①**菜单**:seed 加**顶级**菜单 `menu.apiDocs`(Path `/api-docs`、Icon `api`、Sort 120)—— 它指向的是**后端另一个端点**(`/swagger`),不是本站的一个列表页,所以**没有二级**。②**前端路由**:`web/src/router/routes.tsx` 加 `/api-docs` → `web/src/pages/api-docs/index.tsx`(新)—— 页里一个按钮在**新标签页**打开 `/swagger/index.html`,并给出两个可复制的地址(UI 与 OpenAPI 描述文件)。⚠️ **为什么不用 iframe**:`/swagger` 默认带 BasicAuth(G1),iframe 里遇到挑战时浏览器**不会**弹用户名/密码框 ⇒ 用户看到的是一个空白框架;整页跳过去则会让"后退"在文档页与后台之间打转。③**开发期代理**:`web/vite.config.ts` 加 `/swagger` —— 不加的话 `/swagger/index.html` 打到 vite 自己,而 vite 的 **SPA fallback** 对未知路径返回 **index.html + 200** ⇒ 浏览器把**后台的登录页**当成 Swagger UI 显示出来(**全程零报错**)。④**图标 / i18n**:`web/src/constants/resourceIcons.ts` 登记 `api`(不登记 ⇒ 菜单渲染成没有图标的裸文字);`locales/zh-CN.ts` + `en-US.ts` 加 `resourceMenu.menu_apiDocs` 与 `apiDocs.*`。⑤**默认只绑 super_admin**:菜单可见 ≠ 看得到接口清单 —— 真正的闸是 G1 那道 BasicAuth;别的角色可在「角色管理 → 资源绑定」里给 `menu.apiDocs` 打勾(seed 的绑定**只加不删**,不会冲掉运营手工加的)。**测试**:`server/internal/seed/api_docs_menu_test.go`(形状 + 默认绑定范围 + **反向断言**"别的角色默认拿不到")+ `web/src/api/__tests__/swagger-proxy.test.ts`(后端挂载点 / vite 代理 / 前端常量**三处对齐**)+ `web/src/pages/api-docs/__tests__/index.test.tsx`(5 例:href / 新标签页 / 两个地址 / 中英文文案)**2026-11-03 更新**:①⑤ 描述的「顶级菜单」已摘除(用户拍板 —— 文档不是与业务域并列的导航层级),入口收敛到右上角帮助按钮下拉(web/src/components/layout/helpMenu.ts);seed 守门网改为 api_docs_menu_test.go 的 TestApiDocsMenu_NotInMenuList(不许回到菜单)。/api-docs 路由、/swagger BasicAuth、vite 代理均不变。 | — | — |

> **G3 的口径说明(已落地,留档)**:参考项目(`hertz-antd-admin-saas`)用的是 **swaggo 注解 codegen**
> (它那边的 `hertz-antd-admin-saas/server/docs/docs.go` + 静态 `swagger.json`),
> 字段级 schema 是**注解里写出来的**;
> 本仓走的是**自动生成**(从 `h.Routes()` 推 + AST 读源码 + 运行期反射),
> 路径/方法/参数位置/字段名/字段类型/字段语义**全部自动对**,
> 人手一行都不用写 —— 代价是必须维护一个生成器(以及它的漂移测试)。
> ⚠️ **覆盖率 83% 是真实数字,不是宣传语** —— 剩下 17% 是"猜不准"的那些 handler
> (绑了多个不同 DTO、或绑的方式 analyzer 认不出),它们在生成文件末尾被列出来,
> 在 `x-schema-coverage` 里被计入分母。**把没做到的也写进文档**,是这一节一贯的做法。

### 8.2 开放化补记:手册缺两章 + 审批适配器(2026-11-02 新增)

> ⚠️ **本小节不计入 §8 的 5 项**(那 5 项是 2026-11-01 收官时的口径)。
> 这里是**收官之后新发现的开放化欠账** —— 记下来是因为 §8 自己的规矩:
> 「后续开放化需求另起小节记」。

| # | 子项 | 现状 | 缺它的后果 | 成本 |
|---|---|---|---|---|
| O1 | **手册补两章**:通用审批系统 / 文件系统模块 | 手册是 `server/docs/`(HonKit),体例是「`internal/pkg/` 下**一个包一章**」。**`scope` 的章节本轮已经补上了**(`server/docs/security/scope.md`,2026-11-02 新写)—— 而且写得**诚实**:「注意事项」里明写「**规则层上线不改变任何现有查询的结果**」——
(M160 之后那段改成写 dyn 的实际接法与限制:「**动态集合的数据面:只有『归属人』一维**」)⇒ 与 §1.13 对得上。**还缺的两块不是"漏写"而是"没地方写"**:**通用审批系统**在 `internal/service/approval*.go`(不是包 ⇒ "一包一章"的体例覆盖不到)、**文件系统模块**(M80)也在 service 层(`storage/` 组 5 章是 cachex / kvstore / media / oss / searchidx) | 别人 clone 下来**查不到"审批引擎怎么用"**,也查不到"文件树 / 封面 / 存储路径怎么用" —— 而这两个恰恰是"拿起来就能用"的关键件;框架手册少的那一章,别人就只能去读源码 | 1-1.5 天 |
| O2 | **审批引擎能挂任意 dyn 集合** | 通用审批系统**只接了 `digital_material` 一条线** —— internal/service/approval_material.go 写死"只碰 `biz_digital_material` 一张表"。⇒ 审批引擎**不能**给自己的动态集合用(引擎本身是通用的,`BizHandler` 接口就是为这个留的) | 别人 clone 下来,审批引擎是个**只能跑一种业务的半成品**;而引擎明明是通用的 —— 缺的是"dyn 集合 → BizHandler"的通用适配器 + 审批对象注册入口(与 **§1.12 #15** 是同一件事,这里记"开放化"那一半) | 1-2 天 |

> **⚠️ 顺带查出一条"守门网漏掉了什么"(值得单独记)**:
> 手册有两张网 —— ①`server/docs/docs_test.go` 守的是「**源改了有没有重新构建**」(指纹比对);
> ②`scripts/check_docs_links.py` 守的是「SUMMARY 列的页面都存在 / 每个 .md 都被收录(孤儿页 ⇒ 红)/
> 链接能解析 / 引用的 `internal/pkg/<包>` 目录存在」。
> ⚠️ **两张网都不守"每个框架包都有一章"** ⇒ `scope` 那一章是**人手补上的**,
> **漏掉的时候不会让任何一条网变红** —— 这跟 §1.12 那次(`###` 写成 `####` 之前,守门网扫不到那一节)
> 是**同一个形状**:**清单 / 文档漏记的东西,机器不会替人记**。
> ⇒ 建议给 `check_docs_links.py` 补一条**正向**检查:`internal/pkg/` 下每个一级包都必须在 SUMMARY 里有一章
> (跟已有的"反向漂移检查"配对)。

---

## 附录 A:文件清单(本次盘点涉及的所有框架层文件)

### 后端 `server/internal/` 共 11207 行(70 文件)

> ⚠️ **M112(2026-10-29)按当前真实行数重算** —— 原快照是 2026-10-19 盘点的数字,
> 期间框架层长了不少(原来 28 个文件 / 4498 行,现在 70 个文件 / 11207 行)。
> **`pkg/httputil/` 已删除**(M112:零调用方死代码,且"取 XFF 最左一元素"的实现
> 是个 footgun —— 真正在用的是 Hertz 的 `ClientIPWithOption`,见 §1.4 第 6 行)。
> 🔴 **`pkg/sms/` 也已删除**(M129,2026-10-31):同样是**零生产调用方、零单测**的
> 死包 —— 它的四块能力(RateLimiter / VerifyStore / Provider 注册表 / Mock)
> 全部有活着的替代品:`pkg/smslimit/`(限流,已装配)、
> `sys_sms_code` 表 + `service.VerifySms`(验证码校验)、
> `pkg/msn/`(Sender 抽象 + `Resolver`/`DynamicSender`)、`pkg/msn/console/`(dev 打印)。
> ⇒ 清单里 §1.6 #4/#5 与 §1.9 #4/#5/#6 五处指向它的条目已在 M129 改指到真实现。

```
pkg/auth/         jwt.go(390) / revocation.go(148) / redis_store.go(168)
                  memory_store.go(136) / session.go(115) / invalidate.go(92)
                  password.go(48) / revocache/revocache.go(235)
pkg/authz/        loader.go(199) / cached_loader.go(155) / principal.go(149)
                  assertor.go(178) / permissions.go(30)
pkg/casbinx/      enforcer.go(113)
pkg/crypto/       registry.go(479) / aesgcm.go(148) / crypto.go(134)
                  envelope.go(103) / blindindex.go(72) / rand.go(15)
pkg/errcode/      errcode.go(231) / error.go(65)
pkg/logger/       rotator.go(290) / logger.go(251) / route.go(71)
pkg/loginlock/    loginlock.go(57)
pkg/msn/          aliyun/aliyun.go(103) / console/console.go(26)
                  factory.go(5) / sender.go(33)
pkg/oss/          local/local.go(987) / path.go(359) / advanced.go(240)
                  mock.go(221) / upload.go(145) / provider.go(117)
                  aliyun/aliyun.go(642) / aliyun/advanced.go(336)
pkg/pagination/   page.go(54)
pkg/response/     response.go(124) / time.go(7)
pkg/sonyflake/    snowflake.go(281)
pkg/timeutil/     time.go(22)
pkg/desensitize/  desensitize.go(227)

api/middleware/   gzip.go(248) / rbac.go(234) / idempotency.go(206)
                  breaker.go(202) / secheaders.go(184) / otel.go(152)
                  sseauth.go(145) / bodylimit.go(144) / sign.go(128)
                  clientip.go(126) / csrf.go(125) / trace.go(127)
                  auth.go(209) / principal.go(100) / metrics.go(96)
                  reqgzip.go(86) / audit.go(86) / timeout.go(84)
                  ratelimit.go(79) / recover.go(69) / apiver.go(64)
                  gray.go(59) / access_log.go(57) / middleware.go(67)
```

> ⚠️ **M112 新增的 6 个包不在上表里**(它们是 M100~M112 之间长出来的):
> `pkg/{smslimit,validatorx,cachex,cronx,signx,randomx}/` 等。完整目录树见
> `server/README.md`。上表只保留 2026-10-19 首批盘点覆盖的包,便于对照历史。

### 前端 `web/src/` 共 ~5800 行(41 文件)

```
stores/           user.ts(186) / permission.ts(138) / config.ts(74)
                  dict.ts(58) / ui.ts(60)

hooks/            useAuth.ts(27) / usePermission.ts(32)
                  useTable.ts(119) / useDict.ts(83) / useRequest.ts(75)
                  useConfig.ts(33)

utils/            crypto.ts(48) / format.ts(78)
                  request.ts(19) / storage.ts(117) / validators.ts(53)

locales/          zh-CN.ts(805) / en-US.ts(784) / useI18n.ts(86)

api/              client.ts(202) / auth.ts(105) / user.ts(68)
                  role.ts(135) / config.ts(109) / upload.ts(139)
                  house.ts / owner.ts / asset.ts / issue.ts
                  dashboard.ts / index.ts(22)

types/            api.ts(80) / audit.ts(36) / dict.ts(84)
                  permission.ts(82) / user.ts(81) / config.ts(105)

components/auth/  AuthButton.tsx(45) / PermissionGuard.tsx(37)
                  RoleGuard.tsx(36)

components/common/ EditModal.tsx(228) / ViewDrawer.tsx(82)
                  ManagementTable.tsx(194) / DataTable.tsx(54)
                  SearchForm.tsx(131) / SearchSection.tsx(56)
                  SensitiveField.tsx(79) / ImageUploader.tsx(125)
                  SmsCodeGuard.tsx(148) / SmsSendButton.tsx(175)
                  CaptchaField.tsx(153) / RowActions.tsx(90)
                  ConfirmDialog.tsx(97) / SecretInput.tsx(66)
                  ConfigDiffView.tsx(96) / PageHeader.tsx(44)
                  index.ts(26)

components/layout/ BasicLayout.tsx(106) / Header.tsx(168)
                  Sidebar.tsx(190) / Breadcrumb.tsx(68)
                  IdentitySwitcher.tsx(173)
```

---

## 附录 B:证据核验规则

为保证"已完成"项的真实可信,每条证据必须满足:
- **文件存在** — `file_path` 可在仓库中找到
- **行号范围** — 标记的函数 / 类 / 类型在该范围内
- **行为可验证** — 至少一处调用方 / 测试 / 业务页引用

`未完成`项必须满足:
- **说清差什么** — 不是"没做",而是"差什么、为什么没做"
- **后果明确** — 不做会出什么问题
- **有成本估算** — 人天为单位

---

**文档结束**

> **下一步(2026-11-01 更新)**:§3 的 11 个 P0 风险只剩 #10(23 个业务 endpoint,属业务线),
> 框架侧已清零。**§8 开源通用化 5 项已全部完成(2026-11-01)** ——
> #4 examples、#5 平台名可配置、#2 Swagger 开放化 5 个子项、#3 GitBook 手册(56 页)均已落地。
> 每项单独 commit,落地后在本文对应行打 ✅(与 §1/§2 的记法一致)。
>
> **下一步(2026-11-02 更新)**:框架层还开着**三项** —— RS256(§1.2 #9)、
> **「一份能力清单取代三份码表」(§1.1 #17 / §5 #11,架构项)**,
> 以及**平台运营负责人的审计死菜单**(§1.1 #18)。
> 后者的结构成因是:`sys_resource.type ∈ {menu, button, api}` 三份独立码表各自手写、
> **只在 seed 时对账一次**,而启动期守门网 `CheckResourceConsistency` **查不出菜单↔API 的漂移**
> —— M154 的三个死菜单(菜单给了、API 没给 ⇒ 点进去就 403)就是这么长出来的;
> 反方向(API 给了、菜单不显示)同样会漂,同样没人会发现。
> ⚠️ **同类洞已经手工修过 3 次**(M93 `dict:list` / 2026-11-01 `dashboard:read` / M154),
> 每次都是"往绑定矩阵里补一个码"—— **补的是症状**。
> ⇒ 建议**分三步**:①半天做只读体检工具(出事实清单)②~1 天把体检口径接进启动期守门网
> (**这一类洞再也不能静默**)③2-4 天做能力清单本体(**只在网织上、反复报警之后再决定**)。
> ⚠️ 第 ③ 步会**改授权粒度**(绑定从"码"变成"能力",`user.read` 投影出的那组码会被捆绑授给)
> ⇒ 13 角色 × 377 资源的绑定矩阵要**整个重推**,风险不在写代码、在重推矩阵。
> **✅ M170(2026-11-02):①② 步已落地** —— 登记表 + 启动期 fail-fast 守门网> (`seed.CheckMenuAPIWiring` + `menu_api_map.go`,挂在 seed Run() 绑定矩阵定形之后;> 挂在 seed 不挂 `CheckResourceConsistency` 的原因:那个守门网只知道「路由↔码」,不知道「角色↔绑定」,> **而洞是按角色长的**)。当场抓出两个真洞:property_manager 文件列表页缺 `folder:list`;dev.db 垃圾测试角色> `ces`(5 个系统菜单零接口权,已删)。第 ③ 步能力清单本体仍按原判:**等网反复报警之后再决定**。

> **下一步(2026-11-02 第二次更新)**:这一轮不是"又加一项",是把**「通用后台框架还缺什么」整个重排了一遍**。
> 下面按 **①该做且便宜 ②该做但排在首期之后 ③不建议现在做 ④做之前必须先做的两件事** 给出结论。
>
> **① 该做、而且最便宜(合计约 1.5 天,不占首期工期,但必须排期)**
> - ~~**§1.13 #2 业务查询没接线(P1;多租户上线前必做)**~~ —— ✅ **M161 已完成**(2026-11-02)。
>   这一轮**唯一一个"框架层已经做完了、只差把业务侧那 17 处手写 filter 换掉"**的洞,
>   也是唯一一个**安全方向**的洞:过滤条件此前**由调用方从 HTTP 参数传**
>   ⇒ 不传 `projectId` 就是**不过滤**,传了也**不校验你是不是那个项目的人**。
>   **落地方式**:17 个业务域 / 36 处调用点 + 三张账本(`service/biz_scope.go`)
>   + 启动期 fail-fast(`CheckScopeWiring`) + **扫源码守门网**
>   (`biz_scope_wiring_test.go` —— 抓到过一处真 bug:`family` 登记了却没接)。
>   ⚠️ **剩下的是配置动作不是框架欠账**:多租户上线前给相关角色**真的配一条规则**。
> - ✅ **§1.1 #18 平台运营负责人的四个审计菜单零接口权(P1)** —— ✅ M169 已收(2026-11-02):seed 补三个只读码 + casbin 强制执行层回归网,当场消掉一个「点进去就 403」。
> - **§1.1 #17 能力清单 —— 只做三步里的第 ② 步(守门网,~1 天)** —— 把"菜单给了、API 没给"这一类洞
>   从**静默**变成**启动失败**。这一步最值:它不碰授权粒度,只加检查。
>
> **② 该做、但排在首期之后(Q4)**
> - **§1.14 领域事件总线(3-4 天)** —— 新建,首期用不上;但**不记就会有第三处手写的 goroutine 发布**
>   (现在两处:`approval_publisher.go` / `dict_publisher.go`,同样的三件套抄了两遍)。
> - **§1.12 #12-#15 动态集合四项**(字段类型补齐 / 导入导出 / 行级操作 / 审批适配器)—— 各 1-2 天。
> - **§1.13 #5 字段级的两条尾巴(≈1-2 天)** —— ✅ 字段级的**服务端那一半**已于
>   M162 / M163 / M164 落地(规则层 + 执行层 + 配置界面 + 手册);
>   剩下两件:① **两个登记表之间没有交叉守门网**(行级账本新增业务域 ⇒ 字段级对它静默失效,
>   且管理员**配不出来**,≈0.5 天,加一条扫账本的测试);
>   ② **前端消费那一半**(表格列 / 表单字段按规则显隐,归 §2.7 #16,≈1-2 天)。
>   ⚠️ **遮字段必须在服务端做**(已做实),前端置灰只是第二道。
> - **§8.2 手册补三章 + 审批适配器(2-3 天)** —— 开放化收尾;顺带给 `check_docs_links.py` 补一条
>   「每个框架包都要有一章」的**正向**检查(现在两张网都不守这条,`scope` 漏章不会让任何网变红)。
>
> **③ 不建议现在做(维持原判)**
> - **RS256 / 异地多活 / 移动端** —— 本来就判过是"按原定计划没排进首期",不是欠账(材料与清单判断一致)。
>
> **④ 做上面任何一条"改授权粒度"的活之前,必须先解决的两件事**
> - **风险不在写代码,在重推绑定矩阵** —— M147 已经在这上面翻过一次(通配行 + keyMatch2 + 请求 obj 传模块名,
>   三个洞叠在一起 ⇒ 矩阵在强制执行层**从来没生效过**;三处配套改动必须同一 commit),
>   M149 又翻过一次(显示层按所有身份并集算、执行层按当前身份判 ⇒ 两套真相)。
>   ⇒ 任何改粒度的项,落地时**必须同 commit 改三处**:后端判定 / 绑定矩阵 / 前端显示层;
>   并且要有「矩阵改了但运行时没同步」的回归网 —— casbin 策略与 `sys_role_resource` 不同步,
>   症状是"勾了权限却 403",不报错、只生效一半。
> - ~~**"框架强制"不能靠自觉**~~ —— ✅ **M161 之后已经落地**,原来那条排序建议照做完了:
>   ①「声明 + 启动期校验」→ `bizScopeSpecs` / `bizScopeCtxWired` / `bizScopeUnwired` 三张账本
>   + `CheckScopeWiring()`(`cmd/server/main.go` 里 `log.Fatal`);
>   ②「改 17 处业务 filter」→ 17 个业务域 / 36 处调用点。
>   ⚠️ **但还差最后一块** —— 启动期那张网只比"路由登记的业务域 vs 三张账本",
>   **抓不到"账本里登记了、实际没调用"** ⇒ 补了第二张网(扫源码)。
>   **两条网都绿的,才算"接上了"。**
