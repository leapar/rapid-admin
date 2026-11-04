/**
 * API 文档入口页(2026-11-01 · 开源通用化 §8 G5)
 *
 * ## 这个页面为什么只是"一个入口"
 *
 * Swagger UI **不是这个前端的一部分** —— 它由后端挂在 `/swagger` 下
 * (见 server/internal/api/router/router.go 的 swaggerx.Mount 与
 * server/swagger/ui/)。所以这一页只做两件事:
 *
 *   1. 告诉用户文档在哪(页面上直接给出可复制的地址)
 *   2. 给一个按钮,在**新标签页**打开它
 *
 * ## 为什么是"新标签页"而不是 iframe
 *
 * `/swagger` 整条路径默认带 **BasicAuth**(见
 * server/internal/pkg/swaggerx/auth.go)。iframe 里遇到 BasicAuth 挑战时,
 * 浏览器**不会**弹那个用户名/密码框 —— 用户看到的是一个空白框架,
 * 而且控制台不一定有明显报错。顶层标签页才会正常弹框。
 *
 * 顺带一提:整页跳过去也不合适 —— 那会让浏览器的"后退"在
 * "文档页 → 后台 → 文档页"之间打转。
 *
 * ## ⚠️ 路径不许改
 *
 * `SWAGGER_UI_PATH` / `SWAGGER_SPEC_PATH` 住在 `@/constants/swagger`,
 * 必须与后端的挂载点一致。后端那边改了挂载点,这里要跟着改 ——
 * web/src/api/__tests__/swagger-proxy.test.ts 会把两边的挂载点对齐着验。
 */
// ⚠️ **import type 而不是 import** —— React 在本文件里只当类型用(React.FC),
//   实值导入会被 @typescript-eslint/consistent-type-imports 判成 error。
//   (仓里另有 44 个页面还在用实值导入,那是**它们**的历史账,不在本文件加。)
import type React from 'react'
import { Alert, Button, Card, Descriptions, Space, Typography } from 'antd'
import { ApiOutlined } from '@ant-design/icons'
import { PageHeader } from '@/components/common'
import { SWAGGER_SPEC_PATH, SWAGGER_UI_PATH } from '@/constants/swagger'
import { useI18n } from '@/locales'

const ApiDocs: React.FC = () => {
  const { t } = useI18n()

  return (
    <>
      <PageHeader description={t('apiDocs.description')} />
      <Card>
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Alert
            type="info"
            showIcon
            message={t('apiDocs.authTitle')}
            description={
              <Space direction="vertical" size={4}>
                <span>{t('apiDocs.authDesc')}</span>
                <span>{t('apiDocs.disabledHint')}</span>
              </Space>
            }
          />

          <Button
            type="primary"
            size="large"
            icon={<ApiOutlined />}
            href={SWAGGER_UI_PATH}
            target="_blank"
            rel="noopener noreferrer"
          >
            {t('apiDocs.open')}
          </Button>

          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label={t('apiDocs.uiPath')}>
              <Typography.Text code copyable>
                {SWAGGER_UI_PATH}
              </Typography.Text>
            </Descriptions.Item>
            <Descriptions.Item label={t('apiDocs.specPath')}>
              <Typography.Text code copyable>
                {SWAGGER_SPEC_PATH}
              </Typography.Text>
            </Descriptions.Item>
          </Descriptions>
        </Space>
      </Card>
    </>
  )
}

export default ApiDocs
