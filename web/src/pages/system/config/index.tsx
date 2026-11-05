/**
 * 系统配置 —— 配置项列表
 *
 * 2026-10-18 收尾:
 *   - 系统配置由 super_admin 独占(routes.tsx RoutePermissionGuard only super_admin + config:read)
 *   - 非 super_admin 路由层直接 403,本页不再做"只读"分支(那是死码)
 *   - Form 只装 value 一个字段
 *   - config_name / is_active / is_sensitive 移到 Card title 只读展示
 *
 * 结构:
 *   - 每个 config_group 一个 Collapse.Panel
 *   - Panel 内每个 config item 一个 Card
 *   - Card title 展示:config_key / 敏感 / config_name / 已启用-已停用 / 版本 / 最后改
 *   - Card body 一个 Form.Item: value(敏感走 SecretInput,非敏感走 Input.TextArea)
 *   - Panel footer:保存本组 + 重置
 *
 * 2026-10-26:
 *   - Collapse 套进 Card(Collapse 自己 bordered={false},外框交给 Card),
 *     跟其他列表页(ManagementTable 的白底圆角卡)观感一致
 *   - 分组之间的分隔线:antd 用 colorBorder(#eef0f4)画,跟白底几乎同色 → 等于没画;
 *     换成 --app-table-header-line,见 styles/global.css 的 .config-collapse
 *   - 搜索框改成**受控**(value + onChange,跟 ManagementTable 一样),
 *     并加 autoComplete="off";过滤改成边打边筛(列表是前端过滤,17 条,
 *     不需要回车)。起因是搜索框里会凭空出现一个 "admin":
 *     Chrome 存着 http://localhost:5173/login 的账号,而这页有
 *     type="password" 的敏感配置框(SecretInput)→ Chrome 把整页当登录页,
 *     把页面上唯一的文本框(就是搜索框)当"用户名框",自动填那个账号。
 *     **框里显示的字是 Chrome 画的预览,DOM 的 value 其实是空的** ——
 *     所以下面列表还是全量,"框里的字"和"过滤状态"对不上。
 *     根因在 SecretInput(它已不用 type="password"),搜索框这边的
 *     autoComplete="off" + 受控是第二道防线。
 */
import React, { useState, useEffect, useMemo } from 'react'
import { Collapse, Form, Input, Tag, Space, Button, Card, App, Empty, Tooltip } from 'antd'
import { ReloadOutlined, HistoryOutlined } from '@ant-design/icons'
import { SecretInput } from '@/components/common/SecretInput'
import { PageHeader } from '@/components/common/PageHeader'
import { SearchSection } from '@/components/common/SearchSection'
import { useDict } from '@/hooks/useDict'
import { useI18n } from '@/locales'
import { configDictI18nKey, configItemI18nKey } from '@/locales/configKeys'
import { useNavigate } from 'react-router-dom'
import { configApi } from '@/api/config'
import type { ConfigItem, ConfigGroupKV } from '@/types/config'

// ===== 子组件:单个 group 的折叠面板 + Form =====
interface GroupConfigFormProps {
  group: string
  items: ConfigItem[]
  onSaved: () => void
}

const GroupConfigForm: React.FC<GroupConfigFormProps> = ({ group, items, onSaved }) => {
  const { message } = App.useApp()
  const { t } = useI18n()
  const { getLabel } = useDict('config_ui_label')
  const [form] = Form.useForm()
  const [saving, setSaving] = useState(false)
  const [initialized, setInitialized] = useState(false)

  /**
   * 表单 label —— **i18n 优先,字典兜底**。
   *
   * ⚠️ 2026-10-29 修「当前值没有 i18n」:
   *   以前直接 `getLabel('config.value')` ⇒ 显示的是**字典里存的中文**("当前值"),
   *   英文环境下照样是中文(字典的 label 是运营可改的中文,不是译文)。
   *   现在先查 i18n(`page.config.dict.config_value`),查不到才回落字典 ——
   *   字典"运营可改、没译文的新字段跟着变"这个设计**没丢**。
   *
   *   key 的派生规则在 `locales/configKeys.ts`(单独一个文件,守门网 import 的是它)。
   */
  const fieldLabel = (code: string) => t(configDictI18nKey(code), getLabel(code))

  /**
   * 配置项名称 —— **i18n 优先,DB 的 config_name 兜底**。
   *
   * ⚠️ 同上:config_name 是 DB 里的中文("CDN 加速域名" / "OSS 服务提供方"),
   *   以前直接渲染 ⇒ 英文环境显示中文。
   *   i18n 词条见 locales/*.ts 的 `page.config.item.*`,
   *   词条齐全性由 __tests__/config-i18n.test.ts 守着(对着后端 seed 的 key 清单查)。
   */
  const itemLabel = (it: ConfigItem) => t(configItemI18nKey(it.config_key), it.config_name)

  // 初始化 form initialValues — 只装 value 字段
  useEffect(() => {
    if (items.length === 0) return
    const initial: Record<string, any> = {}
    for (const it of items) {
      const base = `${it.id}.`
      // 敏感值留空(后端 pointer 风格,留空 = 保持原值)
      initial[`${base}value`] = it.is_sensitive ? '' : it.value
    }
    form.setFieldsValue(initial)
    setInitialized(true)
  }, [items, form])

  const onFinish = async (vals: Record<string, any>) => {
    setSaving(true)
    try {
      const patches: { id: string; body: any }[] = []
      for (const it of items) {
        const p: any = {}
        const base = `${it.id}.`

        // value: 敏感 / 非敏感都按"非空就传"原则(空 = 不改)
        const newVal = vals[`${base}value`]
        if (typeof newVal === 'string' && newVal.length > 0) {
          p.value = newVal
        }

        if (Object.keys(p).length === 0) continue
        patches.push({ id: it.id, body: { ...p } })
      }

      if (patches.length === 0) {
        message.info(t('page.config.noChanges'))
        return
      }

      // 并行 PATCH,逐条区分成功 / 失败
      const results = await Promise.allSettled(patches.map((p) => configApi.update(p.id, p.body)))
      const failed = results.filter((r) => r.status === 'rejected')
      if (failed.length > 0) {
        message.error(
          t('page.config.savedPartial', undefined, {
            n: patches.length - failed.length,
            m: failed.length,
          }),
        )
        // eslint-disable-next-line no-console
        console.error('Failed config patches:', failed)
      } else {
        message.success(t('page.config.savedCount', undefined, { n: patches.length }))
      }
      onSaved()
    } finally {
      setSaving(false)
    }
  }

  return (
    <Form form={form} layout="vertical" onFinish={onFinish} component={false} initialValues={{}}>
      {!initialized && <Empty description={t('common.loading')} />}
      {initialized &&
        items.map((it) => {
          const base = `${it.id}.`
          return (
            <Card
              key={it.id}
              size="small"
              style={{ marginBottom: 12 }}
              title={
                <Space wrap size="small">
                  <code style={{ fontSize: 12 }}>{it.config_key}</code>
                  {it.is_sensitive && (
                    <Tag color="red" style={{ fontSize: 10 }}>
                      {t('page.config.sensitive')}
                    </Tag>
                  )}
                  <span style={{ color: 'var(--app-text)', fontSize: 13 }}>{itemLabel(it)}</span>
                  {it.is_active ? (
                    <Tag color="green" style={{ fontSize: 10 }}>
                      {t('common.enabled')}
                    </Tag>
                  ) : (
                    <Tag style={{ fontSize: 10 }}>{t('common.disabled')}</Tag>
                  )}
                  <span style={{ color: 'var(--app-text-tertiary)', fontSize: 11 }}>
                    v{it.version}
                    {t('page.config.lastChanged')}
                    {it.last_changed_by_name || '-'}
                    {it.last_changed_at
                      ? ` · ${it.last_changed_at.slice(0, 16).replace('T', ' ')}`
                      : ''}
                  </span>
                </Space>
              }
            >
              <Form.Item
                // 2026-10-26:类名挂在这里而不是 Form 上 —— Form 用了 component={false},
                //   不渲染 <form> 元素,自己的 className 会被 rc-field-form 丢掉。
                //   用途见 styles/global.css:本页表单项下边距归零(卡片里不留 24px 空白)。
                className="config-form-item"
                name={`${base}value`}
                label={fieldLabel('config.value')}
                extra={it.is_sensitive ? t('page.config.sensitiveHint') : undefined}
              >
                {it.is_sensitive ? (
                  /* 占位提示由 SecretInput 自己兜底(common.placeholder.keepUnchanged) */
                  <SecretInput />
                ) : (
                  <Input.TextArea
                    rows={2}
                    placeholder={it.value || t('common.placeholder.currentlyEmpty')}
                    autoSize={{ minRows: 2, maxRows: 6 }}
                  />
                )}
              </Form.Item>
            </Card>
          )
        })}

      {initialized && (
        <Form.Item className="config-form-item" style={{ marginTop: 16 }}>
          <Space>
            <Button type="primary" htmlType="submit" loading={saving}>
              {t('page.config.saveGroup', undefined, { n: items.length })}
            </Button>
            <Button
              onClick={() => {
                form.resetFields()
                message.info(t('page.config.resetDone'))
              }}
            >
              {t('common.reset')}
            </Button>
          </Space>
        </Form.Item>
      )}
    </Form>
  )
}

// ===== 主页面 =====
const ConfigList: React.FC = () => {
  const navigate = useNavigate()
  const { t } = useI18n()

  const [groups, setGroups] = useState<ConfigGroupKV[]>([])
  const [items, setItems] = useState<ConfigItem[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [activeKeys, setActiveKeys] = useState<string[]>([])

  // 加载所有配置项(不分页 — 当前只 17 条,后续扩到 200 条也无需分页)
  const loadAll = async () => {
    setLoading(true)
    try {
      const [gs, list] = await Promise.all([
        configApi.groups(),
        // 一次性拉全,page_size 设为 1000
        configApi.list({ page: 1, page_size: 1000 } as any),
      ])
      setGroups(gs)
      setItems(((list as any).list || list || []) as ConfigItem[])
      setActiveKeys((prev) => (prev.length > 0 ? prev : gs[0]?.group ? [gs[0].group] : []))
    } catch {
      // 错误由拦截器处理
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadAll()
  }, [])

  // 按 group 分组
  const itemsByGroup = useMemo(() => {
    const m: Record<string, ConfigItem[]> = {}
    for (const it of items) {
      const g = it.config_group
      if (!m[g]) m[g] = []
      m[g].push(it)
    }
    return m
  }, [items])

  // 关键字过滤
  const filteredGroups = useMemo(() => {
    if (!keyword.trim()) return groups
    const kw = keyword.toLowerCase()
    return groups
      .map((g) => {
        const matched = (itemsByGroup[g.group] || []).filter(
          (it) =>
            it.config_key.toLowerCase().includes(kw) ||
            (it.config_name || '').toLowerCase().includes(kw),
        )
        return { ...g, _matched: matched.length, _items: matched }
      })
      .filter((g) => g._matched > 0)
  }, [groups, itemsByGroup, keyword])

  return (
    <div style={{ height: '100%', padding: '0 4px', display: 'flex', flexDirection: 'column' }}>
      {/* 顶部 */}
      <PageHeader description={t('page.config.description')} />
      {/* 2026-10-18:搜索区统一用 SearchSection 容器(浅灰底 + L1 阴影 + 边缘高光 + 圆角 14 + hover 升级 L2) */}
      <SearchSection>
        {/*
          2026-10-26 搜索框改成受控:
            · value + onChange —— 框里显示什么,永远等于 keyword(过滤状态)。
              以前这一页是全站唯一一处既没有 value 也没有 onChange 的
              Input.Search,框里的字和过滤状态可以是两回事。
            · autoComplete="off" —— 别让浏览器把这个框当用户名框。
            · 过滤改成边打边筛(纯前端过滤,17 条配置项,不用等回车)。
          ⚠️ 光靠这两条**挡不住** Chrome 的密码管理器:只要页面上还有
          type="password"(SecretInput),Chrome 就把整页当登录页,把页面上
          唯一的文本框当"用户名框",自动填它存着的账号(实测:localhost:5173
          的 admin 凭空出现在这个框里,而 DOM 的 value 是空的 —— 那是浏览器
          画的预览,不是真实输入值)。根因在 SecretInput,见它的注释。
        */}
        <Input.Search
          placeholder={t('common.placeholder.searchConfig')}
          allowClear
          style={{ width: 280 }}
          autoComplete="off"
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          onSearch={(v) => setKeyword(v.trim())}
        />
        <Button icon={<ReloadOutlined />} onClick={loadAll} loading={loading}>
          {t('common.refresh')}
        </Button>
        {/* 2026-10-18:删"全部审计日志"按钮,每个 group 已经有"审计日志"链接(extra 槽) + audit 页"清除 group 过滤"按钮覆盖全部场景 */}
      </SearchSection>

      {filteredGroups.length === 0 && !loading && (
        <Empty description={keyword ? t('page.config.emptyNoMatch') : t('page.config.emptyNone')} />
      )}

      {/*
        2026-10-26:Collapse 套进 Card —— 跟其他列表页(ManagementTable 的白底圆角卡)
        观感一致;Collapse 自己 bordered={false} 去掉第二层外框,外框交给 Card,
        分组之间的分隔线见 styles/global.css 的 .config-collapse。
      */}
      <Card
        variant="borderless"
        style={{ overflow: 'hidden', padding: '14px 16px' }}
        styles={{
          body: { padding: '0', height: '100%', overflow: 'auto' },
        }}
      >
        <Collapse
          className="config-collapse"
          bordered={false}
          accordion={false}
          activeKey={activeKeys}
          onChange={(k) => setActiveKeys((k as string[]) || [])}
        >
          {filteredGroups.map((g) => {
            const groupItems = (g as any)._items ? (g as any)._items : itemsByGroup[g.group] || []
            return (
              <Collapse.Panel
                key={g.group}
                header={
                  <Space>
                    <Tag color="blue">{g.group}</Tag>
                    <span style={{ color: 'var(--app-text-secondary)' }}>
                      {t('page.config.itemCount', undefined, { n: groupItems.length })}
                    </span>
                  </Space>
                }
                extra={
                  <Tooltip title={t('page.config.auditTooltip')}>
                    <Button
                      type="link"
                      size="small"
                      icon={<HistoryOutlined />}
                      onClick={(e) => {
                        e.stopPropagation()
                        navigate(`/system/config/audit?configGroup=${g.group}`)
                      }}
                    >
                      {t('tab.page.configAudit')}
                    </Button>
                  </Tooltip>
                }
              >
                <GroupConfigForm group={g.group} items={groupItems} onSaved={loadAll} />
              </Collapse.Panel>
            )
          })}
        </Collapse>
      </Card>
    </div>
  )
}

export default ConfigList
