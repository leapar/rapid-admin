/**
 * 「API 接入」弹窗 —— 告诉用户这个集合怎么用 API 接进去
 *
 * # 为什么要有这个弹窗
 *
 * 设计器建好一个集合之后,**接口是自动生成的**,但这件事用户看不见。
 * 他真正缺的是一份能直接抄的东西:
 *
 *   · 路径怎么拼
 *   · 需要哪个权限码(去角色配置页勾哪一行)
 *   · 请求体的键叫什么(**字段标识**,不是数据库列名)
 *   · 一条能直接跑的 curl
 *
 * ⚠️ **内容由 apiGuide.ts 生成,不在组件里写死** ——
 *   路径 / 权限码 / 请求体都得跟着集合走(集合编码是用户起的、字段是运行期的),
 *   组件里写死一份就等于"文档说的"和"真的接口"会分叉。
 *
 * ⚠️ **这一层不碰任何写接口** —— 它只是一份说明。
 *   列出来的 6 条接口各自的权限由后端按集合判定(见后端 dynGuard)。
 */
import { useMemo } from 'react'
import type { FC } from 'react'
import { Alert, Descriptions, Modal, Space, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { getBaseURL } from '@/api/baseURL'
import { useI18n } from '@/locales'
import type { DynSchemaField } from '@/types/dyn'
import {
  buildCurlExample,
  buildDynEndpoints,
  buildResponseExample,
  buildSamplePayloadText,
  buildSdkSnippet,
  dynCollectionBasePath,
  type DynEndpoint,
} from './apiGuide'

/** HTTP 方法 → 徽标颜色 */
const METHOD_COLOR: Record<DynEndpoint['method'], string> = {
  GET: 'blue',
  POST: 'green',
  PATCH: 'orange',
  DELETE: 'red',
}

export interface ApiGuideModalProps {
  open: boolean
  /** 集合编码 */
  code: string
  /** 在用的字段(决定请求体的键) */
  fields: DynSchemaField[]
  onClose: () => void
}

/** 代码块 —— 固定字体 + 可横向滚动 + 可复制 */
function CodeBlock({ text }: { text: string }) {
  return (
    <pre
      style={{
        margin: 0,
        padding: 12,
        borderRadius: 8,
        background: 'var(--app-surface-bg)',
        border: '1px solid var(--app-border-secondary)',
        fontSize: 12,
        lineHeight: 1.6,
        overflow: 'auto',
        maxHeight: 280,
      }}
    >
      {text}
    </pre>
  )
}

const ApiGuideModal: FC<ApiGuideModalProps> = ({ open, code, fields, onClose }) => {
  const { t } = useI18n()

  // ⚠️ **baseURL 取真实的那个** —— 不同部署的后台 API 根路径可以不同
  //   (VITE_API_BASE_ADMIN),写死 /rapid-admin/v1 的话,别人抄走就调不通。
  const baseURL = useMemo(() => getBaseURL(), [])

  const endpoints = useMemo(() => buildDynEndpoints(code), [code])
  const payloadText = useMemo(() => buildSamplePayloadText(fields), [fields])
  const curlText = useMemo(
    () => buildCurlExample(baseURL, code, payloadText),
    [baseURL, code, payloadText],
  )
  const sdkText = useMemo(() => buildSdkSnippet(baseURL, code), [baseURL, code])

  const columns = useMemo<ColumnsType<DynEndpoint>>(
    () => [
      {
        title: t('field.method'),
        dataIndex: 'method',
        width: 90,
        render: (m: DynEndpoint['method']) => <Tag color={METHOD_COLOR[m]}>{m}</Tag>,
      },
      {
        title: t('field.path'),
        dataIndex: 'path',
        render: (p: string) => (
          <Typography.Text code copyable={{ text: `${baseURL}${p}` }}>
            {p}
          </Typography.Text>
        ),
      },
      {
        title: t('common.permCode'),
        dataIndex: 'perm',
        width: 200,
        render: (p: string) => (
          <Typography.Text code copyable={{ text: p }}>
            {p}
          </Typography.Text>
        ),
      },
      {
        title: t('common.desc'),
        dataIndex: 'descKey',
        render: (k: string) => t(k),
      },
    ],
    [t, baseURL],
  )

  return (
    <Modal
      open={open}
      title={t('page.dyn.records.apiGuide.title')}
      onCancel={onClose}
      footer={null}
      width={880}
      destroyOnHidden
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Alert
          type="info"
          showIcon
          message={t('page.dyn.records.apiGuide.intro')}
          description={t('page.dyn.records.apiGuide.bodyKey')}
        />

        <Descriptions column={1} size="small" bordered>
          <Descriptions.Item label={t('page.dyn.records.apiGuide.collectionCode')}>
            <Typography.Text code copyable>
              {code}
            </Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label={t('page.dyn.records.apiGuide.basePath')}>
            <Typography.Text code copyable>
              {`${baseURL}${dynCollectionBasePath(code)}`}
            </Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label={t('page.dyn.records.apiGuide.auth')}>
            <Typography.Text code>Authorization: Bearer &lt;token&gt;</Typography.Text>
          </Descriptions.Item>
        </Descriptions>

        <Typography.Title level={5} style={{ margin: 0 }}>
          {t('page.dyn.records.apiGuide.endpoints')}
        </Typography.Title>
        <Table<DynEndpoint>
          rowKey={(r) => `${r.method} ${r.path}`}
          columns={columns}
          dataSource={endpoints}
          size="small"
          pagination={false}
        />

        <Typography.Title level={5} style={{ margin: 0 }}>
          {t('page.dyn.records.apiGuide.fields')}
        </Typography.Title>
        {fields.length === 0 ? (
          <Typography.Text type="secondary">
            {t('page.dyn.records.noSchema')}
          </Typography.Text>
        ) : (
          <Space direction="vertical" size={2} style={{ width: '100%' }}>
            {fields.map((f) => (
              <Typography.Text key={f.field_key}>
                <Typography.Text code copyable>
                  {f.field_key}
                </Typography.Text>
                <Typography.Text type="secondary">
                  {'  '}
                  {/* ⚠️ 类型名复用设计器那一组(page.dyn.fieldType.*)——
                      兜底给类型标识本身:后端加了新类型而字典里还没有时,
                      至少还能看出这是个什么类型,而不是显示一串裸 key。 */}
                  {f.label} · {t(`page.dyn.fieldType.${f.type}`, f.type)} ·{' '}
                  {f.required ? t('field.required') : t('common.optional')}
                </Typography.Text>
              </Typography.Text>
            ))}
          </Space>
        )}

        <Typography.Title level={5} style={{ margin: 0 }}>
          {t('page.dyn.records.apiGuide.example')}
        </Typography.Title>
        <CodeBlock text={curlText} />

        <Typography.Title level={5} style={{ margin: 0 }}>
          {t('page.dyn.records.apiGuide.requestBody')}
        </Typography.Title>
        <CodeBlock text={payloadText} />

        <Typography.Title level={5} style={{ margin: 0 }}>
          {t('page.dyn.records.apiGuide.response')}
        </Typography.Title>
        <CodeBlock text={buildResponseExample()} />

        <Typography.Title level={5} style={{ margin: 0 }}>
          {t('page.dyn.records.apiGuide.sdkTitle')}
        </Typography.Title>
        <Typography.Text type="secondary">
          {t('page.dyn.records.apiGuide.sdkDesc')}
        </Typography.Text>
        <CodeBlock text={sdkText} />
      </Space>
    </Modal>
  )
}

export default ApiGuideModal
