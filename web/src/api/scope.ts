/**
 * 数据范围(行级权限)规则 API —— M157(2026-11-02)
 *
 * 路径规则: 后端 /rapid-admin/v1/system/{scope-modules,roles/:id/scopes}
 * 详见 server/internal/api/router/router.go:467-469
 *
 * ## 这一族接口解决什么问题
 *
 * 本框架的鉴权只回答"这个角色**能不能调这个接口**"(RBAC + casbin)。
 * 数据范围回答的是"接口放行之后,他**能看到哪些行**"。
 *
 * ⚠️ **两者是两件事,页面上也不该混在一起** ——
 * 管理员在「分配权限」里勾了 `user:list`,只说明他能调用户列表接口;
 * 他能看到全平台的用户还是本部门的用户,是数据范围说了算。
 * 混在一起的后果是"以为没配 = 不限制",线上数据边界形同虚设。
 *
 * ## 为什么复用 role:read / role:assign,不开新的资源码
 *
 * 数据范围就是**角色配置的一部分**,跟"这个角色能调哪些接口"摆在同一个页子上、
 * 受同一批人管。后端也是这么定的(见 server/internal/api/handler/v1/scope.go 的注释):
 * 新开 `scope:*` 一族码要进 seed 的资源表 + 13 个角色的绑定矩阵,
 * 漏绑一个角色就是那个角色 403(而且红 toast 说的是另一个码,极难排查)。
 *
 * ⇒ 前端按钮也复用 `button.role.assign`,**不新开按钮码**。
 */
import { http } from './client'

/**
 * 一条数据范围规则(读写共用同一形状)
 *
 * ⚠️ **字段名是 snake_case** —— 后端 DTO 就是 snake_case
 * (server/internal/service/scope.go 的 ScopeRuleView/ScopeRuleInput)。
 * 本仓 Go 的 encoding/json **只做大小写不敏感匹配、不认下划线**,
 * 发 camelCase 后端会收到空值(见 memory:service DTO 缺 json tag 致 snake_case 字段 binding 失败)。
 */
export interface ScopeRule {
  /** 业务域(空串 = 默认规则组) */
  module: string
  /** 范围类型: all / project / dept / dept_and_child / self / custom */
  scope_type: string
  /** 自定义范围的列名(仅 scope_type=custom 时有意义) */
  custom_column?: string
  /** 自定义范围的允许取值(仅 scope_type=custom 时有意义) */
  custom_values?: string[]
  /** 备注(给管理员看的) */
  remark?: string
}

/** 一个可选的业务域(下拉框候选项) */
export interface ScopeModule {
  /** 业务域字符串(存库 / 传参都用它) */
  module: string
  /** 中文名(资源名或集合名) */
  label: string
  /** 候选来源: api(接口权限码) / dyn(动态业务集合) */
  source: string
  /**
   * 这个业务域支持的数据范围类型
   *
   * ⚠️ **前端必须按它过滤维度下拉框** —— 后端在**配置期**就拒
   * "这一维在这张表上落不下去"的组合(楼栋/户型/房屋/项目/业主/家庭
   * 没有归属人列 ⇒ 「仅本人」「本部门」配不上去)。
   * 不按它过滤 = 让管理员配一个注定被拒的组合,
   * 用户看到的是"我明明能选",点保存却报错。
   *
   * ⚠️ **空/缺省 = 全部支持** —— 老后端没有这个字段,
   * 此时把下拉框清空就变成"什么都配不了"。
   */
  supported_types?: string[]
}

/**
 * 保存一条规则的入参
 *
 * ⚠️ **module 空串 = 默认规则组** —— 前端必须原样传空串,
 * 不能传 undefined / 不传字段(那会变成 "null" 字符串或缺字段)。
 */
export interface ScopeRuleInput {
  scope_type: string
  custom_column?: string
  custom_values?: string[]
  remark?: string
}

export const scopeApi = {
  /**
   * 可配置的业务域候选 —— GET /system/scope-modules
   *
   * ⚠️ **候选由后端推导,前端不许手打业务域字符串** ——
   * 配错一个字符,规则会**静默不生效**(退化成"全部数据"),
   * 而且不报错、不留日志(见 server/internal/pkg/scope/module.go)。
   */
  listModules: () => http.get<ScopeModule[]>('/system/scope-modules'),

  /**
   * 某角色配的全部规则(所有业务域)—— GET /system/roles/:id/scopes
   */
  listRoleScopes: (roleId: string) =>
    http.get<ScopeRule[]>(`/system/roles/${roleId}/scopes`),

  /**
   * 全量替换某角色在某业务域上的规则 —— PUT /system/roles/:id/scopes
   *
   * ⚠️ **全量替换,不是增量** —— body 里给的那一组就是该业务域上的全部规则;
   * 发空数组 = 清空该业务域的规则(回到默认组 / 不限制)。
   */
  replaceRoleScopes: (roleId: string, module: string, rules: ScopeRuleInput[]) =>
    http.put<void>(`/system/roles/${roleId}/scopes`, { module, rules }),
}
