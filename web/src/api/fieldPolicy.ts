/**
 * 字段级可见性(列级权限)规则 API —— M164(2026-11-02)
 *
 * 路径规则: 后端 /rapid-admin/v1/system/{field-policy-modules,roles/:id/field-policies}
 * 详见 server/internal/api/router/router.go:476-478
 *
 * ## 这一族接口解决什么问题
 *
 * 本框架有三根**互相独立**的权限轴:
 *
 *   ① 能调这个接口吗        → RBAC + casbin(资源码那一族)
 *   ② 能看哪些行            → 数据范围(scope.ts)
 *   ③ 能看一行里的哪些字段   → **本文件**(字段级可见性)
 *
 * ⚠️ **② 与 ③ 不是一回事,页面上也不能混** ——
 *   数据范围放行"这个业主这一行给你看",不等于"这一行里的手机号也给你看"。
 *   行级是"整行给不给你",列级是"一行里的某列给不给你"。
 *   混在一起的后果与"以为没配 = 不限制"一样:以为配了列级就收窄了数据,
 *   线上该藏的字段照原样吐出去。
 *
 * ## 为什么复用 role:read / role:assign,不开新的资源码
 *
 * 字段级可见性是**角色配置的一部分**,它跟"这个角色能调哪些接口"
 * 摆在同一个页面上、受同一批人管。后端也是这么定的
 * (见 server/internal/api/handler/v1/field_policy.go 的注释):
 * 新开 `field_policy:*` 一族码要进 seed 的资源表 + 13 个角色的绑定矩阵,
 * 漏绑一个就是那个角色 403(而且红 toast 说的是另一个码,极难排查)。
 *
 * ⇒ 前端按钮也复用 `button.role.assign`,**不新开按钮码**。
 */
import { http } from './client'

/**
 * 一条字段级可见性规则(读写共用同一形状)
 *
 * ⚠️ **字段名是 snake_case** —— 后端 DTO 就是 snake_case
 * (server/internal/service/field_policy.go 的 FieldPolicyView/FieldPolicyInput)。
 * 本仓 Go 的 encoding/json **只做大小写不敏感匹配、不认下划线**,
 * 发 camelCase 后端会收到空值(见 memory:service DTO 缺 json tag 致 snake_case 字段 binding 失败)。
 */
export interface FieldPolicy {
  /** 业务域(空串 = 默认规则组) */
  module: string
  /** 字段名(响应 JSON 里的键名) */
  field: string
  /** 可见性模式: visible / masked / hidden */
  mode: string
  /** 打码方式(空串 = 按字段名自动推断;仅 mode=masked 时有意义) */
  mask_type?: string
  /** 备注(给管理员看的) */
  remark?: string
}

/**
 * 一个可配置的字段候选项
 *
 * ⚠️ **候选由后端推导,前端不许手打字段名** ——
 * 字段名配错 ⇒ 规则作用在一个不存在的键上 ⇒ **静默不生效**
 * (后端在**配置期**就拒这种组合,见 service/field_policy_impl.go 的 parseFieldPolicyInputs)。
 */
export interface FieldPolicyField {
  /** 字段名(响应 JSON 里的键名) */
  field: string
  /** 中文名(动态集合取字段标签;业务表没有标签来源,就是字段名本身) */
  label: string
  /**
   * 这个字段**推荐**的打码方式(空串 = 按字段名猜不出来)
   *
   * ⚠️ **空串时配「打码」必须显式指定打码方式** ——
   *   后端在配置期就拒"打码但猜不出方式"的组合(运行期兜底是"整串打码",
   *   那会让管理员看到一堆意料之外的星号,而他以为自己配的是"手机号打码")。
   */
  mask_type: string
  /** 是否命中内置的敏感字段表(只用于排版,不是安全边界) */
  sensitive: boolean
}

/** 一个可配置的业务域(左侧菜单的候选项) */
export interface FieldPolicyModule {
  /** 业务域字符串(存库 / 传参都用它) */
  module: string
  /** 中文名(资源名或集合名) */
  label: string
  /** 候选来源: api(业务表)/ dyn(动态业务集合)/ default(默认规则组) */
  source: string
  /** 这个业务域的字段候选 */
  fields: FieldPolicyField[]
}

/**
 * 保存一条规则的入参
 *
 * ⚠️ **module 空串 = 默认规则组** —— 前端必须原样传空串,
 * 不能传 undefined / 不传字段(那会变成 "null" 字符串或缺字段)。
 */
export interface FieldPolicyInput {
  field: string
  mode: string
  mask_type?: string
  remark?: string
}

export const fieldPolicyApi = {
  /**
   * 可配置的业务域候选 + 每个域的字段候选 —— GET /system/field-policy-modules
   *
   * ⚠️ **默认规则组(module 为空串)由后端一并给出** ——
   *   它的候选是**所有业务域候选的并集**(默认组的规则会作用在每一个业务域上,
   *   而各域的字段不一样),排序也按字段名排好了。
   */
  listModules: () => http.get<FieldPolicyModule[]>('/system/field-policy-modules'),

  /**
   * 某角色配的全部规则(所有业务域)—— GET /system/roles/:id/field-policies
   */
  listRoleFieldPolicies: (roleId: string) =>
    http.get<FieldPolicy[]>(`/system/roles/${roleId}/field-policies`),

  /**
   * 全量替换某角色在某业务域上的规则 —— PUT /system/roles/:id/field-policies
   *
   * ⚠️ **全量替换,不是增量** —— body 里给的那一组就是该业务域上的全部规则;
   * 发空数组 = 清空该业务域的规则(回到默认组 / 不限制)。
   */
  replaceRoleFieldPolicies: (roleId: string, module: string, policies: FieldPolicyInput[]) =>
    http.put<void>(`/system/roles/${roleId}/field-policies`, { module, policies }),
}
