/**
 * 字段级可见性(列级权限)配置抽屉 —— M164(2026-11-02)
 *
 * ## 它管的是"能看到一行里的哪些字段",不是"能调哪些接口",也不是"能看到哪些行"
 *
 * 「分配权限」抽屉管的是**能不能调这个接口**(RBAC 资源码)。
 * 「数据范围」抽屉管的是**接口放行之后,能看到哪些行**(行级)。
 * 这个抽屉管的是**能看到的那一行里,哪些字段会被打码或删掉**(列级)。
 *
 * ⚠️ **三者是分开配的,而且必须分开** ——
 *   管理员在「分配权限」里勾了 `user:list`,只说明他能调用户列表接口;
 *   他能看到全平台的用户还是本部门的用户,是数据范围说了算;
 *   他看到的那一行里的手机号是明文还是打码,是这里说了算。
 *   混在一起的后果与"以为没配 = 不限制"一样:以为配了列级就收窄了数据,
 *   线上该藏的字段照原样吐出去。
 *
 * ## 后端契约(三条,写在前端之前先对齐)
 *
 *   - `GET  /system/field-policy-modules`     → 有哪些业务域可以配、每个域有哪些字段
 *   - `GET  /system/roles/:id/field-policies`  → 这个角色配了什么
 *   - `PUT  /system/roles/:id/field-policies`  → 全量替换**某个业务域**上的规则
 *
 * ⚠️ **业务域与字段候选都不许前端手打** ——
 *   业务域配错 ⇒ 规则落到默认规则组 ⇒ 静默失配;
 *   字段名配错 ⇒ 规则作用在一个不存在的键上 ⇒ 静默不生效。
 *   ⇒ 两者都只在**配置期**就拒(见 server/internal/service/field_policy_impl.go),
 *     前端也在保存前先拒一遍,好让错误精确落在**那一行**上。
 *
 * ## 页面必须替管理员回答的问题
 *
 *   1. 我在业务域里把某个字段设成「明文可见」,**默认组那条还算不算**?
 *      → **逐字段叠加**:业务域只覆盖它自己写了规则的那些字段 ⇒ 算覆盖。
 *   2. 「隐藏」是置空还是删键?                → **删键**(连字段名都不出现)。
 *   3. 我配的「打码」到底会打成什么样?        → 猜不出来时兜底是**整串打码**。
 *
 * ⇒ 这三条都由后端 `internal/pkg/fieldpolicy` 的语义决定,页面上原样复述,
 *   不是"文案美化"。
 */
import { useEffect, useMemo, useState } from 'react'
import type { FC } from 'react'
import {
  Drawer,
  Menu,
  Button,
  Select,
  Input,
  Space,
  Tag,
  Alert,
  Empty,
  Spin,
  Table,
  App,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { SaveOutlined } from '@ant-design/icons'
import { useI18n, tError } from '@/locales'
import { fieldPolicyApi } from '@/api/fieldPolicy'
import type { FieldPolicyField, FieldPolicyModule } from '@/api/fieldPolicy'
import type { Role } from '@/types/permission'
import {
  DEFAULT_MODULE,
  INHERIT_MODE,
  FIELD_POLICY_MASK_TYPES,
  FIELD_POLICY_MODES,
  buildModuleOptions,
  changedModules,
  defaultMaskType,
  findPolicyIssues,
  groupByModule,
  toInputs,
  validatePolicy,
  type FieldPolicyDraft,
  type PolicyMap,
} from './fieldPolicyRules'

/**
 * 左侧菜单里代表"默认规则组"的 key
 *
 * ⚠️ **不能用空串当 Menu 的 key** —— 空串是合法的 key,
 *   但在 `selectedKeys` / `onClick` 里与"没选中"难以区分。
 *   ⇒ 用一个业务域不可能取到的哨兵(`@` 开头,模块名都是标识符/冒号)。
 *
 * ⚠️ **存库的 module 仍然是空串** —— 哨兵只活在菜单里,
 *   传出去之前必须换回空串(后端 `knownFieldModules` 只认空串)。
 */
const MENU_KEY_DEFAULT = '@default'

const menuKeyToModule = (key: string): string => (key === MENU_KEY_DEFAULT ? DEFAULT_MODULE : key)
const moduleToMenuKey = (module: string): string =>
  module === DEFAULT_MODULE ? MENU_KEY_DEFAULT : module

interface Props {
  open: boolean
  role: Role | null
  onClose: () => void
  onSaved?: () => void
  /** true = 只读:内置/超管角色查看用,禁用一切输入 + 隐藏保存 */
  readOnly?: boolean
}

export const FieldPolicyDrawer: FC<Props> = ({
  open,
  role,
  onClose,
  onSaved,
  readOnly = false,
}) => {
  const { t } = useI18n()
  const { message } = App.useApp()

  const [modules, setModules] = useState<FieldPolicyModule[]>([])
  /** 后端返回的原样 —— **保存时用它算差量**,不能拿草稿自己比自己 */
  const [original, setOriginal] = useState<PolicyMap>({})
  /** 页面上的草稿(改的是它,不是 original) */
  const [draft, setDraft] = useState<PolicyMap>({})
  const [active, setActive] = useState<string>(DEFAULT_MODULE)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  /** 打开时拉候选 + 该角色的规则 */
  useEffect(() => {
    if (!open || !role) return
    setLoading(true)
    Promise.all([
      fieldPolicyApi.listModules(),
      fieldPolicyApi.listRoleFieldPolicies(String(role.id)),
    ])
      .then(([mods, rules]) => {
        const grouped = groupByModule(Array.isArray(rules) ? rules : [])
        setModules(buildModuleOptions(Array.isArray(mods) ? mods : []))
        setOriginal(grouped)
        setDraft(grouped)
        // ⚠️ 落在**第一个有规则的**业务域上,而不是永远落在默认组 ——
        //   管理员刚配完"业主信息"再打开,落回默认组会让人以为规则丢了。
        const withRules = Object.keys(grouped).find((k) => Object.keys(grouped[k] ?? {}).length > 0)
        setActive(withRules ?? DEFAULT_MODULE)
      })
      .catch((e) => {
        message.error(
          t('page.role.fieldPolicy.loadFailed', undefined, { msg: tError(e?.code ?? 0, e?.message ?? e) }),
        )
        setModules([])
        setOriginal({})
        setDraft({})
      })
      .finally(() => setLoading(false))
  }, [open, role, message, t])

  /** 当前业务域的候选项 */
  const activeModule = useMemo(
    () => modules.find((m) => m.module === active),
    [modules, active],
  )

  /** 当前业务域的字段候选(字段名 → 候选项) */
  const candidateMap = useMemo(() => {
    const map: Record<string, FieldPolicyField> = {}
    for (const f of activeModule?.fields ?? []) map[f.field] = f
    return map
  }, [activeModule])

  /**
   * 当前业务域要显示的字段行
   *
   * ⚠️ **候选 ∪ 已配规则的字段** ——
   *   只显示候选的话,一条**字段已经不在候选里**的旧规则(比如动态集合删了那个字段)
   *   就看不见了 ⇒ 管理员删不掉它 ⇒ 每次保存都被后端拒(配置期就拒"这个域没有这个字段")。
   *   ⇒ 已配但不在候选里的字段必须渲染出来,并且能删。
   */
  const rows = useMemo(() => {
    const names = new Set<string>()
    for (const f of activeModule?.fields ?? []) names.add(f.field)
    for (const f of Object.keys(draft[active] ?? {})) names.add(f)
    return Array.from(names).sort()
  }, [activeModule, draft, active])

  /** 某个字段当前的规则(undefined = 没有规则 = 走默认组) */
  const policyOf = (field: string): FieldPolicyDraft | undefined => draft[active]?.[field]

  /** 某个字段当前这一行有没有错 —— 精确到行,不等到保存才报 */
  const rowError = (field: string): string | null => {
    const policy = policyOf(field)
    if (!policy) return null
    const candidate = candidateMap[field]
    // ⚠️ 后端**没给候选**时不当成"字段非法"(见 fieldPolicyRules.findPolicyIssues 的注释)
    if (!candidate && (activeModule?.fields?.length ?? 0) > 0) {
      return 'page.role.fieldPolicy.errors.fieldNotCandidate'
    }
    return validatePolicy(policy, candidate)
  }

  /** 改一条规则的字段(打码方式 / 备注) */
  const patchPolicy = (field: string, patch: Partial<FieldPolicyDraft>) => {
    setDraft((prev) => {
      const list = { ...(prev[active] ?? {}) }
      const cur = list[field]
      // ⚠️ 越界时**直接不动** —— 半个规则会让 mode 消失,后端解析不出来
      if (!cur) return prev
      list[field] = { ...cur, ...patch }
      return { ...prev, [active]: list }
    })
  }

  /**
   * 改可见性模式
   *
   * ⚠️ **「跟随默认组」= 删掉这条规则**,不是"配成 visible" ——
   *   "没有规则"= 继承默认组对这个字段的限制;
   *   "配成 visible"= 覆盖默认组对这个字段的限制(让它重新可见)。
   *   两者混成一个 ⇒ 管理员想把某个字段改回"跟着默认组"时,
   *   默认组那条限制跟着消失了 —— 而他以为只是取消了这个域的规则。
   */
  const setMode = (field: string, mode: string) => {
    setDraft((prev) => {
      const list = { ...(prev[active] ?? {}) }
      if (mode === INHERIT_MODE) {
        delete list[field]
        return { ...prev, [active]: list }
      }
      const cur = list[field]
      const next: FieldPolicyDraft = {
        mode,
        mask_type: '',
        remark: cur?.remark ?? '',
      }
      // ⚠️ 切到「打码」时预填推荐的打码方式(猜不出来时兜底「整串打码」)
      if (mode === 'masked') {
        const kept = (cur?.mask_type ?? '').trim()
        next.mask_type = kept || defaultMaskType(candidateMap[field])
      }
      list[field] = next
      return { ...prev, [active]: list }
    })
  }

  /** 打码方式下拉框的选项 */
  const maskOptions = (candidate: FieldPolicyField | undefined) => {
    const list = FIELD_POLICY_MASK_TYPES.map((v) => ({
      value: v as string,
      label: t(`page.role.fieldPolicy.mask.${v}`),
    }))
    // ⚠️ 「自动(按字段名推断)」只在**猜得出来**时才给 ——
    //   猜不出来时选它会被后端在配置期拒(那正是要在页面上提前说清的事)。
    if ((candidate?.mask_type ?? '').trim()) {
      list.unshift({ value: '', label: t('page.role.fieldPolicy.mask.auto') })
    }
    return list
  }

  /** 可见性模式下拉框的选项 */
  const modeOptions = useMemo(() => {
    const inheritLabel =
      active === DEFAULT_MODULE
        ? t('page.role.fieldPolicy.mode.defaultInherit')
        : t('page.role.fieldPolicy.mode.inherit')
    return [
      { value: INHERIT_MODE, label: inheritLabel },
      ...FIELD_POLICY_MODES.map((v) => ({
        value: v as string,
        label: t(`page.role.fieldPolicy.mode.${v}`),
      })),
    ]
  }, [active, t])

  /** 左侧菜单项 —— 默认组排第一,带规则条数 */
  const menuItems = useMemo(
    () =>
      modules.map((m) => {
        const count = Object.keys(draft[m.module] ?? {}).length
        const isDefault = m.module === DEFAULT_MODULE
        return {
          key: moduleToMenuKey(m.module),
          label: (
            <Space size={6}>
              <span>
                {isDefault ? t('page.role.fieldPolicy.defaultModuleLabel') : m.label || m.module}
              </span>
              {count > 0 && <Tag color="blue">{count}</Tag>}
              {m.source === 'dyn' && <Tag color="purple">dyn</Tag>}
            </Space>
          ),
        }
      }),
    [modules, draft, t],
  )

  /** 表格列 */
  const columns: ColumnsType<{ field: string }> = [
    {
      title: t('field.fieldName'),
      dataIndex: 'field',
      width: 260,
      render: (_: unknown, row) => {
        const candidate = candidateMap[row.field]
        const errKey = rowError(row.field)
        return (
          <Space direction="vertical" size={2} style={{ width: '100%' }}>
            <Space size={6} wrap>
              {/* ⚠️ className 是**定位钩子**,不只是样式 ——
                  rc-select 会在无障碍隐藏列表里放一个**原值**的 role="option"
                  (值是 "phone" 时它就渲染成 "phone"),同一个词会出现在 DOM 里两次,
                  按文本找行会撞车。类名是唯一能精确指认"这一行的字段名"的办法。 */}
              <span className="field-policy-field-name" style={{ fontFamily: 'monospace' }}>
                {row.field}
              </span>
              {candidate?.label && candidate.label !== row.field && (
                <span style={{ color: 'var(--app-text-secondary)' }}>{candidate.label}</span>
              )}
              {candidate?.sensitive && (
                <Tag color="red">{t('page.role.fieldPolicy.sensitive')}</Tag>
              )}
              {!candidate && (activeModule?.fields?.length ?? 0) > 0 && (
                <Tag color="orange">{t('page.role.fieldPolicy.notCandidate')}</Tag>
              )}
            </Space>
            {errKey && (
              <span style={{ color: 'var(--app-error, #cf1322)', fontSize: 12 }}>{t(errKey)}</span>
            )}
          </Space>
        )
      },
    },
    {
      title: t('field.visibility'),
      dataIndex: 'mode',
      width: 190,
      render: (_: unknown, row) => (
        <Select
          style={{ width: '100%' }}
          value={policyOf(row.field)?.mode ?? INHERIT_MODE}
          disabled={readOnly}
          onChange={(v) => setMode(row.field, v)}
          options={modeOptions}
        />
      ),
    },
    {
      title: t('field.maskType'),
      dataIndex: 'mask',
      width: 190,
      render: (_: unknown, row) => {
        const policy = policyOf(row.field)
        // ⚠️ **只有「打码」才需要选打码方式** —— 其它模式下打码方式没有意义,
        //   占着位置只会让人以为这里也要配。
        if (!policy || policy.mode !== 'masked') return '-'
        return (
          <Select
            style={{ width: '100%' }}
            value={policy.mask_type ?? ''}
            disabled={readOnly}
            onChange={(v) => patchPolicy(row.field, { mask_type: v })}
            options={maskOptions(candidateMap[row.field])}
          />
        )
      },
    },
    {
      title: t('common.remark'),
      dataIndex: 'remark',
      width: 200,
      render: (_: unknown, row) => (
        <Input
          value={policyOf(row.field)?.remark ?? ''}
          disabled={readOnly}
          placeholder={t('page.role.fieldPolicy.remarkPlaceholder')}
          onChange={(e) => {
            // ⚠️ 备注改在「没有规则」的字段上 ⇒ 顺手把规则建起来,
            //   否则备注改了却什么都不显示(草稿里没这条 ⇒ 保存时也不发)。
            if (!policyOf(row.field)) {
              setMode(row.field, 'visible')
              patchPolicy(row.field, { remark: e.target.value })
              return
            }
            patchPolicy(row.field, { remark: e.target.value })
          }}
        />
      ),
    },
  ]

  /** 保存:只发改动过的业务域,逐个 PUT(顺序发,避免缓存失效互相踩) */
  const save = async () => {
    if (!role) return
    const changed = changedModules(original, draft)
    if (changed.length === 0) {
      message.info(t('page.role.fieldPolicy.noChange'))
      onClose()
      return
    }
    // ⚠️ **前端先拒一遍配置期就不合法的东西** ——
    //   后端拒的是整组(一个字段不合法 ⇒ 这个域的这次 PUT 全废),
    //   管理员只看到"保存失败",不知道是哪一条。
    const issues = findPolicyIssues(modules, draft, changed)
    if (issues.length > 0) {
      message.error(
        t('page.role.fieldPolicy.saveRejected', undefined, { n: issues.length }),
      )
      return
    }
    setSaving(true)
    try {
      for (const m of changed) {
        await fieldPolicyApi.replaceRoleFieldPolicies(
          String(role.id),
          m,
          toInputs(draft[m] ?? {}),
        )
      }
      message.success(t('page.role.fieldPolicy.saveSuccess', undefined, { n: changed.length }))
      onSaved?.()
      onClose()
    } catch (e: unknown) {
      // ⚠️ 后端抛的可能是 { code, message } 也可能是 Error
      const err = e as { message?: string } | null
      message.error(err?.message || t('common.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  const ruleCount = Object.keys(draft[active] ?? {}).length

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width={960}
      title={
        role
          ? readOnly
            ? t('page.role.fieldPolicy.viewTitle', undefined, { name: role.name })
            : t('page.role.fieldPolicy.title', undefined, { name: role.name })
          : t('page.role.fieldPolicy.title', undefined, { name: '' })
      }
      destroyOnHidden
      extra={
        <Space>
          {readOnly ? (
            <Button type="primary" onClick={onClose}>
              {t('common.close')}
            </Button>
          ) : (
            <>
              <Button onClick={onClose}>{t('common.cancel')}</Button>
              <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={save}>
                {t('common.save')}
              </Button>
            </>
          )}
        </Space>
      }
    >
      {loading ? (
        <div style={{ textAlign: 'center', padding: 60 }}>
          <Spin />
        </div>
      ) : (
        <div style={{ display: 'flex', gap: 16, minHeight: 420 }}>
          {/* 左:业务域 */}
          <div style={{ width: 240, flexShrink: 0, borderRight: '0.5px solid var(--app-surface-border)' }}>
            <div style={{ padding: '0 8px 8px', fontWeight: 600, color: 'var(--app-text-secondary)' }}>
              {t('page.role.fieldPolicy.menuTitle')}
            </div>
            {/*
             * ⚠️ **菜单永远渲染** —— 默认规则组永远可配。
             *   后端一个业务域都没登记时,默认组是**唯一**能配的东西,
             *   把它藏掉等于一个都配不了。
             */}
            <div style={{ maxHeight: 'calc(100vh - 200px)', overflow: 'auto' }}>
              <Menu
                mode="inline"
                selectedKeys={[moduleToMenuKey(active)]}
                items={menuItems}
                onClick={(e) => setActive(menuKeyToModule(e.key))}
              />
            </div>
          </div>

          {/* 右:字段规则 */}
          <div style={{ flex: 1, minWidth: 0 }}>
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              {/* 语义提示:默认组 vs 具体业务域,两者的叠加关系不同 */}
              {active === DEFAULT_MODULE ? (
                <Alert
                  type="info"
                  showIcon
                  message={t('page.role.fieldPolicy.defaultModuleTip')}
                  description={t('page.role.fieldPolicy.defaultModuleDesc')}
                />
              ) : (
                <Alert
                  type="info"
                  showIcon
                  message={t('page.role.fieldPolicy.moduleTip')}
                  description={t('page.role.fieldPolicy.moduleDesc')}
                />
              )}

              <Alert
                type="warning"
                showIcon
                message={t('page.role.fieldPolicy.overlayHint')}
                description={t('page.role.fieldPolicy.overlayHintDesc')}
              />

              <Alert
                type="warning"
                showIcon
                message={t('page.role.fieldPolicy.hiddenHint')}
                description={t('page.role.fieldPolicy.hiddenHintDesc')}
              />

              <Alert
                type="info"
                showIcon
                message={t('page.role.fieldPolicy.maskHint')}
                description={t('page.role.fieldPolicy.maskHintDesc')}
              />

              {rows.length === 0 ? (
                <Empty description={t('page.role.fieldPolicy.emptyFields')} />
              ) : (
                <Table<{ field: string }>
                  size="small"
                  rowKey="field"
                  columns={columns}
                  dataSource={rows.map((field) => ({ field }))}
                  pagination={false}
                  scroll={{ y: 360 }}
                />
              )}

              <div style={{ fontSize: 12, color: 'var(--app-text-tertiary, rgba(0,0,0,0.45))' }}>
                {t('page.role.fieldPolicy.ruleCount', undefined, { n: ruleCount })}
              </div>
            </Space>
          </div>
        </div>
      )}
    </Drawer>
  )
}
