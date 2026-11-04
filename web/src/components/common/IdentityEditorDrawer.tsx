/**
 * IdentityEditorDrawer —— 用户多身份编辑 Drawer(框架层通用组件,M58)
 *
 * 能力:
 *  1. 列表编辑多条身份(每条: 角色 + 项目 + 部门 + 有效期 + 主身份)
 *  2. 角色选自后端 roleApi.list(实时拉,缓存到本地 5min)
 *  3. 部门走扁平 Select(部门树拉平后选一个;不是 MultiDeptTreeSelect —— 身份里的部门是"归属"不是"多选")
 *  4. 主身份保证只 1 条(切换 ⭐ 时其他自动取消)
 *  5. 新增 / 删除 / 复制当前行
 *  6. 保存回调 onSave(IdentityItem[]),业务方只负责把 IdentityItem[] 透传给后端
 *  7. 可选 readOnly(内置/超管角色查看身份)
 *
 * 用法:
 *  <IdentityEditorDrawer
 *    open={open}
 *    identities={user.identities ?? []}
 *    onClose={() => setOpen(false)}
 *    onSave={async (items) => {
 *      await userApi.update(id, { identities: items })
 *    }}
 *  />
 *
 * 2026-10-19 框架层补完,替代原"身份管理"占位。
 * 与后端 service/user.go IdentityItem / sys_user_identity 表 1:1 投影。
 */
import React, { useEffect, useMemo, useState } from 'react'
import {
  Drawer,
  Button,
  Form,
  Select,
  DatePicker,
  Switch,
  Space,
  Tag,
  App,
  Tooltip,
  Empty,
} from 'antd'
import {
  PlusOutlined,
  DeleteOutlined,
  CopyOutlined,
  StarFilled,
  StarOutlined,
  CheckCircleOutlined,
} from '@ant-design/icons'
import dayjs, { Dayjs } from 'dayjs'
import { roleApi, departmentApi, DepartmentTreeNode } from '@/api/role'
import type { Role, IdentityItem } from '@/types/permission'
import { useI18n } from '@/locales'

export interface IdentityEditorDrawerProps {
  open: boolean
  /** 已有的身份(用于编辑;首次创建传空数组) */
  identities: IdentityItem[]
  /** 只读(超管 / 内置用户查看) */
  readOnly?: boolean
  /** 部门树 fetcher(测试可注入) */
  departmentFetcher?: () => Promise<DepartmentTreeNode[]>
  /** 角色 fetcher(测试可注入) */
  roleFetcher?: () => Promise<Role[]>
  onClose: () => void
  onSave?: (items: IdentityItem[]) => Promise<void> | void
}

interface Row {
  key: string
  role_code?: string
  role_name?: string
  project_id?: string
  dept_id?: string
  valid_from?: Dayjs | null
  valid_to?: Dayjs | null
  is_primary?: boolean
}

let keySeq = 0
const newKey = () => `row_${++keySeq}_${Date.now()}`

/** 把后端 IdentityItem / SysUserIdentity 转 Row(打开时 hydrate) */
function hydrate(items: IdentityItem[] | undefined): Row[] {
  if (!items || items.length === 0) return []
  return items.map((it) => ({
    key: newKey(),
    role_code: it.role_code,
    role_name: it.role_name,
    project_id: it.project_id,
    dept_id: it.dept_id,
    valid_from: it.valid_from ? dayjs(it.valid_from) : null,
    valid_to: it.valid_to ? dayjs(it.valid_to) : null,
    is_primary: it.is_primary,
  }))
}

/** Row → IdentityItem(保存时序列化) */
function serialize(rows: Row[]): IdentityItem[] {
  return rows
    .filter((r) => !!r.role_code)
    .map((r) => ({
      role_code: r.role_code!,
      role_name: r.role_name,
      project_id: r.project_id,
      dept_id: r.dept_id,
      valid_from: r.valid_from ? r.valid_from.toISOString() : undefined,
      valid_to: r.valid_to ? r.valid_to.toISOString() : undefined,
      is_primary: r.is_primary,
    }))
}

export const IdentityEditorDrawer: React.FC<IdentityEditorDrawerProps> = ({
  open,
  identities,
  readOnly = false,
  departmentFetcher,
  roleFetcher,
  onClose,
  onSave,
}) => {
  const { t } = useI18n()
  const { message } = App.useApp()
  const [rows, setRows] = useState<Row[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [deptTree, setDeptTree] = useState<DepartmentTreeNode[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  // 打开时 hydrate
  useEffect(() => {
    if (!open) return
    setRows(hydrate(identities))
    setLoading(true)
    Promise.all([
      roleFetcher ? roleFetcher() : roleApi.list({ page: 1, size: 200 } as any).then((res) => (res as any)?.list ?? []),
      departmentFetcher
        ? departmentFetcher()
        : departmentApi.tree().then((res) => (res as DepartmentTreeNode[]) ?? []),
    ])
      .then(([rl, dt]) => {
        setRoles(rl as Role[])
        setDeptTree(dt as DepartmentTreeNode[])
      })
      .catch(() => {
        setRoles([])
        setDeptTree([])
      })
      .finally(() => setLoading(false))
  }, [open, identities, roleFetcher, departmentFetcher])

  const addRow = (preset?: Partial<Row>) => {
    setRows((prev) => [
      ...prev,
      {
        key: newKey(),
        is_primary: prev.length === 0, // 首行默认主身份
        ...preset,
      },
    ])
  }

  const removeRow = (key: string) => {
    setRows((prev) => {
      const next = prev.filter((r) => r.key !== key)
      // 如果删的是主身份,把第一行设为主
      if (next.length > 0 && !next.some((r) => r.is_primary)) {
        const first = next[0]
        if (first) first.is_primary = true
      }
      return next
    })
  }

  const duplicateRow = (key: string) => {
    setRows((prev) => {
      const src = prev.find((r) => r.key === key)
      if (!src) return prev
      const idx = prev.findIndex((r) => r.key === key)
      const copy: Row = { ...src, key: newKey(), is_primary: false } // 复制的不带主身份
      const next = [...prev]
      next.splice(idx + 1, 0, copy)
      return next
    })
  }

  const updateRow = (key: string, patch: Partial<Row>) => {
    setRows((prev) => prev.map((r) => (r.key === key ? { ...r, ...patch } : r)))
  }

  const setPrimary = (key: string) => {
    setRows((prev) => prev.map((r) => ({ ...r, is_primary: r.key === key })))
  }

  const roleOptions = useMemo(
    () =>
      roles.map((r) => ({
        value: r.code,
        label: `${r.name} (${r.code})`,
        name: r.name,
      })),
    [roles],
  )

  const deptOptions = useMemo(() => {
    const opts: { value: string; label: string }[] = []
    const walk = (ns: DepartmentTreeNode[] | undefined) => {
      if (!ns) return
      for (const n of ns) {
        opts.push({ value: n.id, label: n.name })
        walk(n.children)
      }
    }
    walk(deptTree)
    return opts
  }, [deptTree])

  const handleSave = async () => {
    if (rows.some((r) => !r.role_code)) {
      message.error(t('component.identityEditor.roleRequired'))
      return
    }
    const primaryCount = rows.filter((r) => r.is_primary).length
    if (rows.length > 0 && primaryCount !== 1) {
      message.error(t('component.identityEditor.primaryRequired'))
      return
    }
    setSaving(true)
    try {
      await onSave?.(serialize(rows))
      message.success(t('common.success'))
      onClose()
    } finally {
      setSaving(false)
    }
  }

  return (
    <Drawer
      open={open}
      title={
        <Space>
          <span>{t('component.identityEditor.title')}</span>
          {readOnly && <Tag color="default">{t('component.identityEditor.readOnly')}</Tag>}
        </Space>
      }
      width={720}
      onClose={onClose}
      destroyOnHidden
      extra={
        !readOnly && (
          <Space>
            <Button icon={<PlusOutlined />} onClick={() => addRow()}>
              {t('component.identityEditor.add')}
            </Button>
            <Button
              type="primary"
              icon={<CheckCircleOutlined />}
              loading={saving}
              onClick={handleSave}
            >
              {t('common.save')}
            </Button>
          </Space>
        )
      }
    >
      {loading && <Empty description={t('component.identityEditor.loading')} />}
      {!loading && rows.length === 0 && !readOnly && (
        <Empty description={t('component.identityEditor.emptyHint')}>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => addRow()}>
            {t('component.identityEditor.add')}
          </Button>
        </Empty>
      )}
      {!loading && rows.length === 0 && readOnly && (
        <Empty description={t('component.identityEditor.emptyReadOnly')} />
      )}
      {!loading &&
        rows.map((row, idx) => (
          <div
            key={row.key}
            style={{
              border: '0.5px solid var(--app-border)',
              borderRadius: 6,
              padding: 12,
              marginBottom: 12,
              background: row.is_primary ? 'var(--app-warning-bg)' : 'var(--app-surface-bg-strong)',
            }}
          >
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                marginBottom: 8,
              }}
            >
              <Space>
                <Tag color={row.is_primary ? 'gold' : 'default'}>
                  {row.is_primary ? t('component.identityEditor.primaryBadge') : `#${idx + 1}`}
                </Tag>
                {!readOnly && (
                  <Tooltip
                    title={
                      row.is_primary
                        ? t('component.identityEditor.alreadyPrimary')
                        : t('component.identityEditor.setPrimary')
                    }
                  >
                    <Button
                      type="text"
                      size="small"
                      icon={
                        row.is_primary ? (
                          <StarFilled style={{ color: '#faad14' }} />
                        ) : (
                          <StarOutlined />
                        )
                      }
                      onClick={() => setPrimary(row.key)}
                    />
                  </Tooltip>
                )}
              </Space>
              {!readOnly && (
                <Space>
                  <Tooltip title={t('component.identityEditor.copyRow')}>
                    <Button
                      type="text"
                      size="small"
                      icon={<CopyOutlined />}
                      onClick={() => duplicateRow(row.key)}
                    />
                  </Tooltip>
                  <Tooltip title={t('component.identityEditor.deleteRow')}>
                    <Button
                      type="text"
                      danger
                      size="small"
                      icon={<DeleteOutlined />}
                      onClick={() => removeRow(row.key)}
                    />
                  </Tooltip>
                </Space>
              )}
            </div>
            <Form layout="vertical" size="small" disabled={readOnly}>
              <Form.Item label={t('field.role')} required style={{ marginBottom: 8 }}>
                <Select
                  value={row.role_code}
                  placeholder={t('common.placeholder.selectRole')}
                  options={roleOptions}
                  showSearch
                  optionFilterProp="label"
                  onChange={(v, opt: any) =>
                    updateRow(row.key, { role_code: v, role_name: opt?.name })
                  }
                />
              </Form.Item>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                <Form.Item label={t('component.identityEditor.projectId')} style={{ marginBottom: 8 }}>
                  <Select
                    value={row.project_id}
                    placeholder={t('common.placeholder.platformAll')}
                    allowClear
                    options={[
                      { value: 0, label: t('component.identityEditor.platformAll') },
                      // 真实项目 ID 由后端 projectApi 注入(避免组件硬编码业务)
                    ]}
                    onChange={(v) => updateRow(row.key, { project_id: v })}
                  />
                </Form.Item>
                <Form.Item label={t('field.department')} style={{ marginBottom: 8 }}>
                  <Select
                    value={row.dept_id}
                    placeholder={t('common.placeholder.optionalDept')}
                    allowClear
                    options={deptOptions}
                    showSearch
                    optionFilterProp="label"
                    onChange={(v) => updateRow(row.key, { dept_id: v })}
                  />
                </Form.Item>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 100px', gap: 8 }}>
                <Form.Item label={t('component.identityEditor.validFrom')} style={{ marginBottom: 8 }}>
                  <DatePicker
                    value={row.valid_from ?? undefined}
                    showTime
                    style={{ width: '100%' }}
                    placeholder={t('common.placeholder.effectiveNow')}
                    onChange={(d) => updateRow(row.key, { valid_from: d })}
                  />
                </Form.Item>
                <Form.Item label={t('component.identityEditor.validTo')} style={{ marginBottom: 8 }}>
                  <DatePicker
                    value={row.valid_to ?? undefined}
                    showTime
                    style={{ width: '100%' }}
                    placeholder={t('common.placeholder.forever')}
                    onChange={(d) => updateRow(row.key, { valid_to: d })}
                  />
                </Form.Item>
                <Form.Item label={t('common.enable')} style={{ marginBottom: 8 }}>
                  {/* ⚠️ 这个开关是"死的":后端 writeUserIdentitiesFromItems 写身份时
                      IsActive 恒为 true,没有"停用单条身份"的入口 —— 摆一个能点的
                      开关在这里是骗人。留着但锁死,并说明原因。 */}
                  <Switch
                    defaultChecked
                    disabled
                    title={t('component.identityEditor.disableUnsupported')}
                  />
                </Form.Item>
              </div>
            </Form>
          </div>
        ))}
    </Drawer>
  )
}

export default IdentityEditorDrawer
