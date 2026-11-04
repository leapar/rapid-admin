/**
 * AuthButton —— 带权限守卫的 antd Button
 *
 * 用法(替换 inlineToolBar / 任意位置的 antd <Button>):
 *   import { AuthButton } from '@/components/auth/AuthButton'
 *   <AuthButton type="primary" perm={PermButton.USER_ADD} icon={<PlusOutlined />} onClick={...}>新增</AuthButton>
 *
 * 设计:
 *   - 跟 RowActions 的 `perm` 行为完全一致(不通过则整 button 不渲染)
 *   - 比 disabled 更严格:权限缺失就完全隐藏(避免用户点了又报错)
 *   - 透传所有 antd ButtonProps(type / icon / onClick / disabled / danger / ...)
 *   - roleAnyOf: 满足任一角色才能渲染(超管类敏感操作用)
 *   - 不通过时返回 null(配合 Drawer 实时调 permission)
 *
 * 历史:2026-10-18 工具栏 perm 化时新增。
 *   - 前:每个 inlineToolBar Button 走 RouteGuard 路由级(粒度太粗)
 *   - 后:用 AuthButton 加 perm 字段,跟 RowActions 形成一致的 button 粒度控制
 */
import type React from 'react'
import { Button, type ButtonProps } from 'antd'
import { usePermission } from '@/hooks/usePermission'

export interface AuthButtonProps extends ButtonProps {
  /** 权限码(不传=总可见) */
  perm?: string
  /** 满足任一角色 code 才能渲染 */
  roleAnyOf?: string[]
}

export const AuthButton: React.FC<AuthButtonProps> = ({
  perm,
  roleAnyOf,
  ...rest
}) => {
  /**
   * ⚠️ M102:必须用 `usePermission()`(订阅),不能用 `usePermissionStore.getState()`。
   *
   * `getState()` 是一次性快照,**不订阅** → 权限变了组件不重渲染。
   * 本组件的注释本来就写着"不通过时返回 null(**配合 Drawer 实时调 permission**)",
   * 而它恰恰是**唯一不响应**的那个 —— 抽屉里改完权限,工具栏按钮纹丝不动。
   */
  const { has, hasRole } = usePermission()

  if (perm && !has(perm)) return null
  if (roleAnyOf && roleAnyOf.length && !roleAnyOf.some((r) => hasRole(r))) {
    return null
  }

  return <Button {...rest} />
}

export default AuthButton
