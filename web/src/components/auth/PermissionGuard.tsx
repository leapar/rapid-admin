/**
 * PermissionGuard —— 权限码守卫
 *
 * 用法:
 *   <PermissionGuard code="user:list:delete" fallback={<NoAccess/>}>
 *     <Button danger>删除</Button>
 *   </PermissionGuard>
 */
import React from 'react'
import { usePermission } from '@/hooks/usePermission'

export interface PermissionGuardProps {
  /** 权限码(module:resource:action) */
  code?: string
  /** 满足任一即可 */
  anyOf?: string[]
  /** 满足所有 */
  allOf?: string[]
  /** 不通过时渲染的 fallback */
  fallback?: React.ReactNode
  children: React.ReactNode
}

export const PermissionGuard: React.FC<PermissionGuardProps> = ({
  code,
  anyOf,
  allOf,
  fallback = null,
  children,
}) => {
  const { has, hasAny, hasAll } = usePermission()
  let pass = true
  if (code) pass = pass && has(code)
  if (anyOf && anyOf.length) pass = pass && hasAny(anyOf)
  if (allOf && allOf.length) pass = pass && hasAll(allOf)
  return <>{pass ? children : fallback}</>
}
