/**
 * 当前用户 / 身份 hook
 */
import { useMemo } from 'react'
import { useUserStore } from '@/stores/user'

export function useAuth() {
  const userInfo = useUserStore((s) => s.userInfo)
  const currentIdentity = useUserStore((s) => s.currentIdentity)
  const isLoggedIn = useUserStore((s) => s.isLoggedIn)
  const switchIdentity = useUserStore((s) => s.switchIdentity)
  const logout = useUserStore((s) => s.logout)

  return useMemo(
    () => ({
      userInfo,
      currentIdentity,
      isLoggedIn,
      hasIdentity: (id: string) =>
        userInfo?.identities?.some((i) => i.id === id) || false,
      switchIdentity,
      logout,
    }),
    [userInfo, currentIdentity, isLoggedIn, switchIdentity, logout]
  )
}
