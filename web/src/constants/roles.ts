/**
 * 角色常量 —— **必须与后端 `sys_role.code` 逐字一致**
 *
 * 不要在前端硬编码角色判断,应该用权限码 Permission Code;
 * 只有"这个人是不是超管"这种**角色层面**的问题才用这里的常量。
 *
 * ⚠️ **M103(2026-10-28)重写:这里的值以前是错的,10/13 个对不上后端。**
 *
 *   旧值形如 `role_platform_super_admin` / `role_business_lead` —— 那套 code
 *   **后端从来没有签发过**。后端真正的角色 code 有三个互相印证的来源:
 *     · `server/internal/model/enums.go`   —— 常量定义
 *     · `server/internal/seed/seed.go` 的 `roleList()` —— 真正落库的那 13 个
 *     · `docs/后台权限模型设计V1.0.md` §1499 —— 设计文档同一套
 *
 *   写错的后果不是"显示个名字不对",而是**三个真功能同时死掉**:
 *     ① **高敏身份切换的短信二次验证**(IdentitySwitcher 的 HIGH_RISK_ROLES)
 *        —— 超管不在名单里 → **切超管身份免验证**,这是安全洞;
 *     ② **用户列表的「重置密码」行内按钮**(`roleAnyOf: [RoleCode.SUPER_ADMIN]`)
 *        —— `hasRole` 永远 false → **这个按钮对谁都渲染不出来**(功能整个死掉);
 *     ③ **路由守卫的「或超管」兜底**(routes.tsx 里 13 处 `anyOf`)
 *        —— 同样恒不命中。
 *
 *   **这份清单由 `src/constants/__tests__/roles.test.ts` 直接解析后端
 *   `model/enums.go` + `seed.go` 的 `roleList()` 逐条比对** ——
 *   后端加角色 / 改 code,前端立刻红。
 */

/**
 * 角色 code 常量(3 个:框架开源版保留的平台级角色)
 *
 * ⚠️ `model/enums.go` 里还有一个兼容旧名的别名 `role_auditor` ——
 *   **从来没被 seed 过**,所以这里也没有。
 */
export enum RoleCode {
  /** 超级管理员(最高权限,可进入平台配置中心) */
  SUPER_ADMIN = 'role_super_admin',
  /** 平台运营员 */
  PLATFORM_OPS = 'role_platform_ops',
  /** 只读查看员 */
  READONLY = 'role_readonly',
}

/**
 * 角色名 (i18n key,**不是**已翻译文案)
 * 消费方: `t(ROLE_NAME_MAP[roleCode])`
 *
 * ⚠️ 键必须是**后端真的会在 `identities[].role_code` 里返回的值**。
 *   以前这份表按另一套(根本没落地的)code 建,于是 10/13 次查不到 →
 *   回落成后端返回的中文 name → **英文环境下身份切换器显示中文角色名**。
 */
export const ROLE_NAME_MAP: Record<string, string> = {
  [RoleCode.SUPER_ADMIN]: 'role.superAdmin',
  [RoleCode.READONLY]: 'role.readonly',
  [RoleCode.PLATFORM_OPS]: 'role.platformOps',
}

/* ---------------------------------------------------------------------------
 * M103 主动收回的两项(理由写在 docs/框架层未完成项清单v1.md §2.8.1):
 *
 *   `CONFIG_CENTER_ALLOWED_ROLES` —— 零调用方,而且**语义是错的**:
 *     配置中心的准入不是"这三个角色",而是 RBAC 资源 `config:*`
 *     只绑给 `role_super_admin` 一个角色(见后端 seed + 角色资源绑定矩阵)。
 *     留着它等于给后来人一个"改这个数组就能改配置中心准入"的错觉。
 *
 *   `ROLE_TYPE_MAP`(platform / business 分类)—— 零调用方:
 *     分类的权威在后端 `sys_role.type` 列;前端要分类时应该直接读后端字段,
 *     而不是在前端再维护一份可能漂移的映射表。
 * ------------------------------------------------------------------------- */
