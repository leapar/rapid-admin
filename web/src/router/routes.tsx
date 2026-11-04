/**
 * 路由表
 *
 * 路由懒加载:每个页面单独 chunk,首屏不加载业务代码
 *
 * 任何守卫(anyOf)用的 code 必须与 server/internal/seed/seed.go resourceList() 一致
 * 详见 web/src/constants/permissions.ts
 */
import { lazy, Suspense } from 'react'
import type { ComponentType } from 'react'
import { Navigate } from 'react-router-dom'
import { Spin } from 'antd'
import { BasicLayout } from '@/components/layout'
import { AuthGuard, GuestGuard, RoutePermissionGuard, PermissionLoader } from './guards'
import {
  PermSystem,
  PermFile,
  PermAudit,
  PermFlow,
  PermApproval,
  PermSmsTemplate,
  PermDyn,
} from '@/constants/permissions'
import { RoleCode } from '@/constants/roles'

/* ====================== 懒加载 ====================== */

const lazyLoad = (loader: () => Promise<{ default: ComponentType }>) => {
  const C = lazy(loader)
  return (
    <Suspense fallback={<Spin size="large" style={{ display: 'block', margin: '100px auto' }} />}>
      <C />
    </Suspense>
  )
}

/* ====================== 页面占位 ====================== */

// 登录
const Login = lazy(() => import('@/pages/login'))

// Dashboard
const Dashboard = lazy(() => import('@/pages/dashboard'))

// 系统管理
const UserList = lazy(() => import('@/pages/system/user'))
const RoleList = lazy(() => import('@/pages/system/role'))
const PermissionList = lazy(() => import('@/pages/system/permission'))
const DepartmentList = lazy(() => import('@/pages/system/department'))
const ConfigList = lazy(() => import('@/pages/system/config'))
const ConfigAudit = lazy(() => import('@/pages/system/config/audit'))

// 审计
const AuditOperation = lazy(() => import('@/pages/audit/operation'))
const AuditDataChange = lazy(() => import('@/pages/audit/data-change'))
const AuditLogin = lazy(() => import('@/pages/audit/login'))

// 错误页
const Forbidden = lazy(() => import('@/pages/error/403'))
const NotFound = lazy(() => import('@/pages/error/404'))
const ServerError = lazy(() => import('@/pages/error/500'))

/* ====================== 路由配置 ====================== */

export const routes = [
  /* 公开路由 */
  {
    path: '/login',
    element: (
      <GuestGuard>
        {lazyLoad(() => import('@/pages/login'))}
      </GuestGuard>
    ),
  },
  {
    path: '/forgot-password',
    element: (
      <GuestGuard>
        {lazyLoad(() => import('@/pages/forgot-password'))}
      </GuestGuard>
    ),
  },

  /* 鉴权路由 */
  {
    path: '/',
    element: (
      <PermissionLoader>
        <AuthGuard>
          <BasicLayout />
        </AuthGuard>
      </PermissionLoader>
    ),
    children: [
      { index: true, element: lazyLoad(() => import('@/pages/dashboard')) },

      /* 个人中心 — 所有登录用户均可访问 */
      { path: 'profile', element: lazyLoad(() => import('@/pages/profile')) },

      /*
       * API 文档入口(2026-11-01 · 开源通用化 §8 G5)
       *
       * ⚠️ **path 必须与后端 seed 的 menu.apiDocs.Path 对上**(后端写的是
       *   /api-docs)—— 两侧不一致的症状是「菜单点了跳 404」,
       *   见 web/src/router/__tests__/menu-route-parity.test.ts。
       *
       * ⚠️ **这里刻意不加 RoutePermissionGuard**:
       *   · 这一页**不含任何机密数据** —— 它只有一个按钮,指向后端的
       *     /swagger;真正的接口清单在那个端点后面,那里有**独立的
       *     BasicAuth 闸**(server/internal/pkg/swaggerx/auth.go)。
       *   · 后端菜单默认只把 menu.apiDocs 绑给 super_admin,但运营可以在
       *     「角色管理 → 资源绑定」里给它别的角色。前端再加一道闸,就会出现
       *     「菜单看得见、点进去 403」—— 那正是本仓最讨厌的那种不一致。
       */
      { path: 'api-docs', element: lazyLoad(() => import('@/pages/api-docs')) },

      /* 一级菜单: 缺总览路由,默认跳首个子页(避免 breadcrumb 死链 404) */
      { path: 'system', element: <Navigate to="/system/user" replace /> },
      {
        path: 'system/user',
        element: (
          <RoutePermissionGuard anyOf={[PermSystem.USER_LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/user'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'system/role',
        element: (
          <RoutePermissionGuard anyOf={[PermSystem.ROLE_LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/role'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'system/permission',
        element: (
          <RoutePermissionGuard anyOf={[PermSystem.RESOURCE_LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/permission'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'system/department',
        element: (
          <RoutePermissionGuard anyOf={[PermSystem.DEPT_LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/department'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'system/dict',
        element: (
          <RoutePermissionGuard anyOf={[PermSystem.DICT_LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/dict'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'system/config',
        element: (
          <RoutePermissionGuard anyOf={[PermSystem.CONFIG_READ, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/config'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'system/config/audit',
        element: (
          <RoutePermissionGuard anyOf={[PermSystem.CONFIG_READ, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/config/audit'))}
          </RoutePermissionGuard>
        ),
      },
      {
        // M69 定时任务。执行记录是抽屉不是独立路由(脱离 job_key 的「所有执行记录」
        // 在几十个任务规模下只会让人分不清在看谁),所以这里只有这一条。
        path: 'system/job',
        element: (
          <RoutePermissionGuard anyOf={[PermSystem.JOB_LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/job'))}
          </RoutePermissionGuard>
        ),
      },
      {
        // M115 短信模板审核状态。
        //
        // ⚠️ **path 必须与后端 seed 的 menu.system.smsTemplate.Path 对上**
        //   (后端写的是 /system/sms-template)—— 两侧不一致的症状是
        //   「菜单点了跳 404」,见 web/src/router/__tests__/menu-route-parity.test.ts。
        //
        // ⚠️ 流转历史是**抽屉**不是独立路由(必须按 scene 过滤才有意义),
        //   所以这里只有这一条路由。
        path: 'system/sms-template',
        element: (
          <RoutePermissionGuard anyOf={[PermSmsTemplate.LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/sms-template'))}
          </RoutePermissionGuard>
        ),
      },
      {
        // M152 业务表单设计器(动态业务集合的设计面)。
        //
        // ⚠️ **path 必须与后端 seed 的 menu.system.dyn.Path 对上**
        //   (后端写的是 /system/dyn)—— 两侧不一致的症状是
        //   「菜单点了跳 404」,见 web/src/router/__tests__/menu-route-parity.test.ts。
        path: 'system/dyn',
        element: (
          <RoutePermissionGuard anyOf={[PermDyn.COLLECTION_LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/system/dyn'))}
          </RoutePermissionGuard>
        ),
      },

      /* 文件(M82)—— 框架页,菜单资源 menu.file / menu.file.list / menu.file.folder */
      { path: 'file', element: <Navigate to="/file/list" replace /> },
      {
        path: 'file/list',
        element: (
          <RoutePermissionGuard anyOf={[PermFile.LIST]}>
            {lazyLoad(() => import('@/pages/file/list'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'file/folder',
        element: (
          <RoutePermissionGuard anyOf={[PermFile.FOLDER_LIST]}>
            {lazyLoad(() => import('@/pages/file/folder'))}
          </RoutePermissionGuard>
        ),
      },

      /* 通用审批(M83/M87)—— 流程配置三级路由 + 审批单两级路由
       *
       * ⚠️ **审批流程是三级路由**:`/flow` → `/flow/:id` → `/flow/:id/stage/:stageId`
       *   —— 模板 / 关卡 / 规则三层各是一张表。**不是三层塞进一个页面**:
       *   声明式 EditField[] 表达不了嵌套,塞一起也让人分不清在改哪一层。
       *
       * ⚠️ **2026-11-01:两个菜单收进「审批管理」**(用户拍板)。侧边栏的层级是
       *   父节点 `menu.approvalMgmt`(path `/approval-manage`)+ 两个二级
       *   `menu.approval`(`/approval`)、`menu.flow`(`/flow`)。
       *
       *   ⚠️ **父节点的 path 没有前缀住两个子节点,子节点的 path 一个都没改** ——
       *     因为站内信 / 邮件里的审批深链是**后端拼的** `/approval/{id}`
       *     (internal/service/approval_inbox.go:195、approval_scan.go:265/300),
       *     改子节点 path = 全平台已经发出去的待办链接全部 404。
       *     父菜单因此自己占一个 path,点它跳回「我的审批」——
       *     与「系统管理 /system → /system/user」那 10 个分组同一套规矩。
       *
       *   三级路由的二三级**不出现在侧边栏**,它们是从父页点进去的。
       */
      { path: 'approval-manage', element: <Navigate to="/approval" replace /> },
      {
        path: 'flow',
        element: (
          <RoutePermissionGuard anyOf={[PermFlow.LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/flow/index'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'flow/:id',
        element: (
          <RoutePermissionGuard anyOf={[PermFlow.READ, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/flow/[id]'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'flow/:id/stage/:stageId',
        element: (
          <RoutePermissionGuard anyOf={[PermFlow.READ, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/flow/stage/[stageId]'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'approval',
        element: (
          <RoutePermissionGuard anyOf={[PermApproval.LIST, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/approval/index'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'approval/:id',
        element: (
          <RoutePermissionGuard anyOf={[PermApproval.READ, RoleCode.SUPER_ADMIN]}>
            {lazyLoad(() => import('@/pages/approval/[id]'))}
          </RoutePermissionGuard>
        ),
      },

      /* 审计 */
      { path: 'audit', element: <Navigate to="/audit/operation" replace /> },
      {
        path: 'audit/operation',
        element: (
          <RoutePermissionGuard anyOf={[PermAudit.OPERATION_LIST]}>
            {lazyLoad(() => import('@/pages/audit/operation'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'audit/data-change',
        element: (
          <RoutePermissionGuard anyOf={[PermAudit.DATA_LIST]}>
            {lazyLoad(() => import('@/pages/audit/data-change'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'audit/login',
        element: (
          <RoutePermissionGuard anyOf={[PermAudit.LOGIN_LIST]}>
            {lazyLoad(() => import('@/pages/audit/login'))}
          </RoutePermissionGuard>
        ),
      },
      {
        path: 'audit/system',
        element: (
          <RoutePermissionGuard anyOf={[PermAudit.SYSTEM_LIST]}>
            {lazyLoad(() => import('@/pages/audit/system'))}
          </RoutePermissionGuard>
        ),
      },

      /*
       * 通用业务记录页(M152)—— **数据面**,路径是运行期菜单给的
       *
       * ⚠️ **path 必须与运行期登记的集合菜单 path 对上** ——
       *   集合创建时后端登记的菜单是 `menu.dyn.<集合码>`,path 为 `/dyn/<集合码>`
       *   (见 server/internal/service/dyn_resource.go)⇒ 这里必须是 `/dyn/:code`。
       *   两侧不一致的症状是「菜单点了跳 404」。
       *
       * ⚠️ **这里刻意不加 RoutePermissionGuard** ——
       *   集合编码是**用户起名、运行期才有的**,权限码 `dyn:<集合码>:<动作>`
       *   编译期枚举不了(见 @/api/dyn 的 dynRecordPerm)⇒ 任何写死的 anyOf
       *   都表达不了"这个集合能不能看"。
       *   ⇒ 闸门在后端(dynGuard → middleware.CanAccess),前端按运行期拼出来的
       *   权限码控制**按钮的可见性**(见 @/pages/dyn/index.tsx)。
       *   与 api-docs 路由同一个道理:前端少一道闸,后端那道才是真的。
       */
      { path: 'dyn/:code', element: lazyLoad(() => import('@/pages/dyn')) },
    ],
  },

  /* 错误页 */
  { path: '/forbidden', element: lazyLoad(() => import('@/pages/error/403')) },
  { path: '/500', element: lazyLoad(() => import('@/pages/error/500')) },

  /* 兜底 */
  { path: '*', element: lazyLoad(() => import('@/pages/error/404')) },
]
