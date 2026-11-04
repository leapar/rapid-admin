/**
 * 权限判断 hook
 *
 * 业务组件里这样用:
 *   const { has, hasAny, hasAll, hasRole } = usePermission()
 *   {has('user:list:view') && <Button>查看</Button>}
 */
import { useMemo } from 'react'
import { usePermissionStore } from '@/stores/permission'

export function usePermission() {
  // 通过 selector 拿,避免大对象订阅
  const permissionCodes = usePermissionStore((s) => s.permissionCodes)
  const roleCodes = usePermissionStore((s) => s.roleCodes)
  const hasFn = usePermissionStore((s) => s.has)
  const hasAnyFn = usePermissionStore((s) => s.hasAny)
  const hasAllFn = usePermissionStore((s) => s.hasAll)
  const hasRoleFn = usePermissionStore((s) => s.hasRole)

  return useMemo(
    () => ({
      permissionCodes,
      roleCodes,
      has: hasFn,
      hasAny: hasAnyFn,
      hasAll: hasAllFn,
      hasRole: hasRoleFn,
    }),
    [permissionCodes, roleCodes, hasFn, hasAnyFn, hasAllFn, hasRoleFn]
  )
}
