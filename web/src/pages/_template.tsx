/**
 * 业务页面占位模板
 *
 * 用法:
 *   import { PagePlaceholder } from '../_template'
 *   const { t } = useI18n()
 *   export default function BuildingList() {
 *     return (
 *       <PagePlaceholder
 *         module="house"
 *         page="building"
 *         title={t('page.building.title')}
 *         description={t('page.building.description')}
 *         features={[t('page.building.f1'), t('page.building.f2')]}
 *         permissions={[...]}
 *       />
 *     )
 *   }
 *
 * 国际化约定:
 *   - 内部 4 个固定文案 (骨架提示 / 模块路径 / 依赖权限 / 计划功能 / 占位) 全部走 t()
 *   - title / description / features 由调用方用 t() 包好后传入(本组件不重复翻译)
 *
 * ⚠️ **这里的 title 不是"页头标题"** —— 页头标题归面包屑管,页内一律不渲染
 *   (2026-10-25 起 PageHeader 不再接受 title)。本组件的 title 只是**一段
 *   文本**:调用方没传 description 时,它被拼进兜底描述句子里
 *   (见下面的 placeholder.defaultDescription)。
 */
import React from 'react'
import { Empty, Card, Typography, Space, Tag, Alert } from 'antd'
import { ExperimentOutlined } from '@ant-design/icons'
import { PageHeader } from '@/components/common'
import { useI18n } from '@/locales'

const { Text, Paragraph } = Typography

export interface PagePlaceholderProps {
  module: string
  page: string
  title: string
  description?: string
  /** 此页面将实现的功能点(由调用方 t() 后传入) */
  features?: string[]
  /** 此页面依赖的权限码 */
  permissions?: string[]
}

export const PagePlaceholder: React.FC<PagePlaceholderProps> = ({
  module,
  page,
  title,
  description,
  features = [],
  permissions = [],
}) => {
  const { t } = useI18n()
  return (
    <>
      <PageHeader
        description={description || t('placeholder.defaultDescription', undefined, { title })}
      />
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message={t('placeholder.skeletonTitle')}
        description={t('placeholder.skeletonDesc')}
      />
      <Card>
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Paragraph>
            <Text strong>{t('placeholder.modulePath')}:</Text> <Tag>{module}</Tag>
            <Text strong style={{ marginLeft: 16 }}>{t('placeholder.pageId')}:</Text> <Tag color="blue">{page}</Tag>
          </Paragraph>

          {permissions.length > 0 && (
            <Paragraph>
              <Text strong>{t('placeholder.dependsOnPermissions')}:</Text>
              <div style={{ marginTop: 8 }}>
                {permissions.map((p) => (
                  <Tag key={p} color="purple">{p}</Tag>
                ))}
              </div>
            </Paragraph>
          )}

          {features.length > 0 && (
            <Paragraph>
              <Text strong>{t('placeholder.plannedFeatures')}:</Text>
              <ol style={{ marginTop: 8, paddingLeft: 20 }}>
                {features.map((f, i) => (
                  <li key={i}>{f}</li>
                ))}
              </ol>
            </Paragraph>
          )}

          <Empty
            image={<ExperimentOutlined style={{ fontSize: 64, color: '#1677ff' }} />}
            description={t('placeholder.placeholderDesc')}
          />
        </Space>
      </Card>
    </>
  )
}

export default PagePlaceholder
