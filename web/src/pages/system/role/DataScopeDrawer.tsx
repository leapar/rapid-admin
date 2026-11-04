/**
 * 数据范围(行级权限)配置抽屉 —— M157(2026-11-02)
 *
 * ## 它管的是"能看到哪些行",不是"能调哪些接口"
 *
 * 「分配权限」抽屉管的是**能不能调这个接口**(RBAC 资源码)。
 * 这个抽屉管的是**接口放行之后,能看到哪些行**(数据范围)。
 *
 * ⚠️ **两者是分开配的,而且必须分开** ——
 * 管理员在「分配权限」里勾了 `user:list`,只说明他能调用户列表接口;
 * 他能看到全平台的用户还是本部门的用户,是这里说了算。
 * 混在一起的后果是"以为没配 = 不限制",线上数据边界形同虚设。
 *
 * ## 后端契约(三条,写在前端之前先对齐)
 *
 *   - `GET  /system/scope-modules`     → 有哪些业务域可以配(**候选由后端推导**)
 *   - `GET  /system/roles/:id/scopes`  → 这个角色配了什么(所有业务域)
 *   - `PUT  /system/roles/:id/scopes`  → 全量替换**某个业务域**上的规则
 *
 * ⚠️ **业务域候选不许前端手打** —— 配错一个字符,规则**静默不生效**
 *   (退化成"全部数据"),而且不报错、不留日志。
 *   ⇒ 下拉框的候选项一律来自后端,默认组(空串)由前端自己补一项。
 *
 * ⚠️ **写接口是"全量替换",不是"增量"** ——
 *   发过来的那组规则就是该业务域上的**全部**规则;发空数组 = 清空。
 *   ⇒ 保存时只发**改动过的**业务域(见 scopeRules.changedModules)。
 *
 * ## 页面必须替管理员回答的三个问题
 *
 *   1. 我配的这些规则,**彼此是什么关系**?   → 「或」(全部满足任一条即可)
 *   2. 我配的这个业务域,**默认组还参不参与**? → 不参与("就近覆盖")
 *   3. 这条规则**实际会怎么算**?              → 「全部数据」一票放行,其余规则空转
 *
 * ⇒ 这三条都由后端 `internal/pkg/scope` 的语义决定,页面上原样复述,
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
  App,
} from 'antd'
import { PlusOutlined, DeleteOutlined, SaveOutlined } from '@ant-design/icons'
import { useI18n, tError } from '@/locales'
import { scopeApi } from '@/api/scope'
import type { ScopeRule, ScopeModule } from '@/api/scope'
import type { Role } from '@/types/permission'
import {
  DEFAULT_MODULE,
  buildModuleOptions,
  defaultScopeType,
  groupByModule,
  hasAllType,
  isCustomRule,
  supportedTypesFor,
  validateRule,
  ruleToInput,
  changedModules,
} from './scopeRules'

/**
 * 左侧菜单里代表"默认规则组"的 key
 *
 * ⚠️ **不能用空串当 Menu 的 key** —— antd Menu 的 key 走的是
 * `key === undefined ? String(index) : key` 那条路,空串是个合法的 key
 * 但在 `selectedKeys` / `onClick` 里与"没选中"难以区分。
 * ⇒ 用一个业务域不可能取到的哨兵(`@` 开头,模块名都是标识符/冒号)。
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
  /** true = 只读:内置/超管角色查看用,禁用一切输入 + 隐藏保存/添加/删除 */
  readOnly?: boolean
}

export const DataScopeDrawer: FC<Props> = ({
  open,
  role,
  onClose,
  onSaved,
  readOnly = false,
}) => {
  const { t } = useI18n()
  const { message } = App.useApp()

  const [modules, setModules] = useState<ScopeModule[]>([])
  /** 后端返回的原样 —— **保存时用它算差量**,不能拿草稿自己比自己 */
  const [original, setOriginal] = useState<Record<string, ScopeRule[]>>({})
  /** 页面上的草稿(改的是它,不是 original) */
  const [draft, setDraft] = useState<Record<string, ScopeRule[]>>({})
  const [active, setActive] = useState<string>(DEFAULT_MODULE)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  /** 打开时拉候选 + 该角色的规则 */
  useEffect(() => {
    if (!open || !role) return
    setLoading(true)
    Promise.all([scopeApi.listModules(), scopeApi.listRoleScopes(String(role.id))])
      .then(([mods, rules]) => {
        const grouped = groupByModule(Array.isArray(rules) ? rules : [])
        setModules(buildModuleOptions(Array.isArray(mods) ? mods : []))
        setOriginal(grouped)
        setDraft(grouped)
        // ⚠️ 落在**第一个有规则的**业务域上,而不是永远落在默认组 ——
        //   管理员刚配完"用户管理"再打开,落回默认组会让人以为规则丢了。
        const withRules = Object.keys(grouped).find((k) => (grouped[k] ?? []).length > 0)
        setActive(withRules ?? DEFAULT_MODULE)
      })
      .catch((e) => {
        message.error(
          t('page.role.scope.loadFailed', undefined, { msg: tError(e?.code ?? 0, e?.message ?? e) }),
        )
        setModules([])
        setOriginal({})
        setDraft({})
      })
      .finally(() => setLoading(false))
  }, [open, role, message, t])

  /** 当前业务域上的规则 */
  const activeRules = draft[active] ?? []

  /**
   * 当前业务域的候选项 —— 拿它的 supported_types 用
   *
   * ⚠️ **业务域可能不在 modules 里**(草稿里有过一个后端没登记的域)——
   *   那时 supportedTypesFor 返回全部类型,退回到旧行为。
   */
  const activeModule = useMemo(
    () => modules.find((m) => m.module === active),
    [modules, active],
  )

  /** 当前业务域支持的维度 —— 下拉框只给这些 */
  const activeTypes = useMemo(() => supportedTypesFor(activeModule), [activeModule])

  /**
   * 维度下拉框的选项
   *
   * ⚠️ **已存的规则里若有"这个业务域现在不支持"的类型,也要列出来** ——
   * 直接不列 ⇒ Select 显示裸值(看着像坏了),管理员会以为规则丢了。
   * (真的会丢:默认组的可用维度是"所有业务域的交集",新建一个动态集合
   *   就可能让交集变小 —— 这时候老规则还在,但已经不该再被推荐。)
   */
  const typeOptions = useMemo(() => {
    const list = [...activeTypes]
    for (const r of draft[active] ?? []) {
      if (r.scope_type && !list.includes(r.scope_type)) list.push(r.scope_type)
    }
    return list.map((v) => ({ value: v, label: t(`page.role.scope.type.${v}`) }))
  }, [activeTypes, draft, active, t])

  /** 左侧菜单项 —— 默认组永远排第一 */
  const menuItems = useMemo(() => {
    const opts = buildModuleOptions(modules)
    return opts.map((m) => {
      const count = (draft[m.module] ?? []).length
      const isDefault = m.module === DEFAULT_MODULE
      return {
        key: moduleToMenuKey(m.module),
        label: (
          <Space size={6}>
            <span>{isDefault ? t('page.role.scope.defaultModuleLabel') : m.label || m.module}</span>
            {count > 0 && <Tag color="blue">{count}</Tag>}
            {m.source === 'dyn' && <Tag color="purple">dyn</Tag>}
          </Space>
        ),
      }
    })
  }, [modules, draft, t])

  /**
   * 后端登记了几个业务域(不含前端补的默认组)
   *
   * ⚠️ **为 0 的时候,默认规则组是"唯一"能配的东西** ——
   *   这时候把它藏掉(之前的 Empty 分支)等于**一个都配不了**。
   */
  const backendModuleCount = useMemo(
    () => modules.filter((m) => m.module !== DEFAULT_MODULE).length,
    [modules],
  )

  /** 改一条规则 */
  const patchRule = (index: number, patch: Partial<ScopeRule>) => {
    setDraft((prev) => {
      const list = [...(prev[active] ?? [])]
      // ⚠️ `noUncheckedIndexedAccess` 下 list[index] 是 `ScopeRule | undefined`。
      //   越界时**直接不动**(而不是把规则改成半个)—— 半个规则会让
      //   scope_type 消失,后端解析不出来,页面上的东西与实际生效的不一致。
      const cur = list[index]
      if (!cur) return prev
      list[index] = { ...cur, ...patch }
      return { ...prev, [active]: list }
    })
  }

  /** 加一条规则 */
  const addRule = () => {
    setDraft((prev) => ({
      ...prev,
      // ⚠️ **默认类型必须这个业务域支持** —— 默认给 'self' 的话,
      // 在 house(没有归属人列)上点"添加规则"就配不出来了。
      [active]: [
        ...(prev[active] ?? []),
        { module: active, scope_type: defaultScopeType(activeTypes) },
      ],
    }))
  }

  /** 删一条规则 */
  const removeRule = (index: number) => {
    setDraft((prev) => {
      const list = [...(prev[active] ?? [])]
      list.splice(index, 1)
      return { ...prev, [active]: list }
    })
  }

  /** 换业务域 */
  const switchModule = (key: string) => setActive(menuKeyToModule(key))

  /** 保存:只发改动过的业务域,逐个 PUT(顺序发,避免缓存失效互相踩) */
  const save = async () => {
    if (!role) return
    const changed = changedModules(original, draft)
    if (changed.length === 0) {
      message.info(t('page.role.scope.noChange'))
      onClose()
      return
    }
    setSaving(true)
    try {
      for (const m of changed) {
        await scopeApi.replaceRoleScopes(
          String(role.id),
          m,
          (draft[m] ?? []).map(ruleToInput),
        )
      }
      message.success(t('page.role.scope.saveSuccess', undefined, { n: changed.length }))
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

  /** 这组规则里有没有"全部数据"(有 ⇒ 其余规则空转) */
  const allPresent = hasAllType(activeRules)

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width={860}
      title={
        role
          ? readOnly
            ? t('page.role.scope.viewTitle', undefined, { name: role.name })
            : t('page.role.scope.title', undefined, { name: role.name })
          : t('page.role.scope.title', undefined, { name: '' })
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
              {t('page.role.scope.menuTitle')}
            </div>
            {/*
             * ⚠️ **菜单永远渲染** —— 默认规则组永远可配。
             *   之前这里是 `menuItems.length <= 1 ? <Empty/> : <Menu/>`,
             *   而默认组自己就占了 1 项 ⇒ **后端一个业务域都没登记时,
             *   菜单整个不渲染** ⇒ 默认组(那时是唯一能配的东西)也配不了。
             *   "没东西可选" ≠ "没有可配的项" —— 默认组永远在。
             */}
            <div style={{ maxHeight: 'calc(100vh - 200px)', overflow: 'auto' }}>
              <Menu
                mode="inline"
                selectedKeys={[moduleToMenuKey(active)]}
                items={menuItems}
                onClick={(e) => switchModule(e.key)}
              />
            </div>
            {backendModuleCount === 0 && (
              <div
                style={{
                  padding: '8px',
                  fontSize: 12,
                  color: 'var(--app-text-tertiary, rgba(0,0,0,0.45))',
                }}
              >
                {t('page.role.scope.emptyModules')}
              </div>
            )}
          </div>

          {/* 右:规则 */}
          <div style={{ flex: 1, minWidth: 0 }}>
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              {/* 语义提示:默认组 vs 具体业务域,两者参与关系不同 */}
              {active === DEFAULT_MODULE ? (
                <Alert
                  type="info"
                  showIcon
                  message={t('page.role.scope.defaultModuleTip')}
                  description={t('page.role.scope.defaultModuleDesc')}
                />
              ) : (
                <Alert
                  type="info"
                  showIcon
                  message={t('page.role.scope.moduleTip')}
                  description={t('page.role.scope.moduleDesc')}
                />
              )}

              <Alert
                type="warning"
                showIcon
                message={t('page.role.scope.ruleHint')}
                description={t('page.role.scope.ruleHintDesc')}
              />

              {/* 「全部数据」一票放行 */}
              {allPresent && (
                <Alert
                  type="warning"
                  showIcon
                  message={t('page.role.scope.allWarning')}
                  description={t('page.role.scope.allWarningDesc')}
                />
              )}

              {activeRules.length === 0 ? (
                <Empty description={t('page.role.scope.emptyRules')} />
              ) : (
                activeRules.map((rule, idx) => {
                  const errKey = validateRule(rule)
                  return (
                    <div
                      key={idx}
                      style={{
                        border: '0.5px solid var(--app-surface-border)',
                        borderRadius: 8,
                        padding: 12,
                      }}
                    >
                      <Space direction="vertical" size={8} style={{ width: '100%' }}>
                        <Space wrap>
                          <Select
                            style={{ width: 170 }}
                            value={rule.scope_type}
                            disabled={readOnly}
                            onChange={(v) => patchRule(idx, { scope_type: v })}
                            options={typeOptions}
                          />
                          {isCustomRule(rule) && (
                            <Input
                              style={{ width: 180 }}
                              placeholder={t('page.role.scope.customColumnPlaceholder')}
                              value={rule.custom_column ?? ''}
                              disabled={readOnly}
                              onChange={(e) => patchRule(idx, { custom_column: e.target.value })}
                            />
                          )}
                          {isCustomRule(rule) && (
                            <Select
                              mode="tags"
                              style={{ width: 220 }}
                              placeholder={t('page.role.scope.customValuesPlaceholder')}
                              value={rule.custom_values ?? []}
                              disabled={readOnly}
                              onChange={(v) => patchRule(idx, { custom_values: v as string[] })}
                            />
                          )}
                          <Input
                            style={{ width: 200 }}
                            placeholder={t('page.role.scope.remarkPlaceholder')}
                            value={rule.remark ?? ''}
                            disabled={readOnly}
                            onChange={(e) => patchRule(idx, { remark: e.target.value })}
                          />
                          {!readOnly && (
                            <Button
                              danger
                              size="small"
                              icon={<DeleteOutlined />}
                              onClick={() => removeRule(idx)}
                            >
                              {t('page.role.scope.removeRule')}
                            </Button>
                          )}
                        </Space>
                        {errKey && !readOnly && (
                          <div style={{ color: 'var(--app-error, #cf1322)', fontSize: 12 }}>
                            {t(errKey)}
                          </div>
                        )}
                      </Space>
                    </div>
                  )
                })
              )}

              {!readOnly && (
                <Button icon={<PlusOutlined />} onClick={addRule}>
                  {t('page.role.scope.addRule')}
                </Button>
              )}
            </Space>
          </div>
        </div>
      )}
    </Drawer>
  )
}
