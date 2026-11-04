/**
 * RoleGuard —— 角色守卫
 *
 * 用法:
 *   <RoleGuard anyOf={[RoleCode.SUPER_ADMIN, RoleCode.USER_ADMIN]}>
 *     <Button>敏感操作</Button>
 *   </RoleGuard>
 */
import React from 'react'
import { usePermission } from '@/hooks/usePermission'

export interface RoleGuardProps {
  /** 满足任一角色即可 */
  anyOf?: string[]
  /** 满足所有角色 */
  allOf?: string[]
  fallback?: React.ReactNode
  children: React.ReactNode
}

export const RoleGuard: React.FC<RoleGuardProps> = ({
  anyOf,
  allOf,
  fallback = null,
  children,
}) => {
  const { roleCodes } = usePermission()
  let pass = true
  if (anyOf && anyOf.length) {
    pass = pass && anyOf.some((r) => roleCodes.includes(r))
  }
  if (allOf && allOf.length) {
    pass = pass && allOf.every((r) => roleCodes.includes(r))
  }
  return <>{pass ? children : fallback}</>
}
