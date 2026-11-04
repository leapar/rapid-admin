/**
 * ViewDrawer —— 通用查看抽屉
 *
 * 用法:
 *   <ViewDrawer<User>
 *     title="用户详情"
 *     open={!!viewing}
 *     data={viewing}
 *     fields={[
 *       { name: 'username', label: '账号' },
 *       { name: 'nickname', label: '昵称' },
 *       { name: 'phone', label: '手机', render: (v) => <SensitiveField type="phone" value={v} /> },
 *       { name: 'status', label: '状态', render: (v) => <Tag>{statusMap[v]}</Tag> },
 *       { name: 'createdAt', label: '创建时间', render: (v) => dayjs(v).format('YYYY-MM-DD HH:mm') },
 *     ]}
 *     onClose={() => setViewing(null)}
 *   />
 *
 * ⚠️ **M111 收回:`ViewField.span`**
 *   以前 `ViewField` 有个 `span?: number`,注释写"占一整行(默认 true)"——
 *   两处都不对:类型是 number 不是 boolean,而这个抽屉的 Descriptions
 *   **column 固定为 1** ⇒ 每个字段本来就占满一行,`span` 没有任何作用;
 *   更糟的是传 `span={2}` 会让 antd 判定"span 之和与 column 不匹配"
 *   并在控制台打 dev 警告(antd 的 Descriptions useRow)。
 *   全仓 12 个调用方**零个**传过 span ⇒ 直接收回这个口子(TS 层面挡住误用)。
 */
import React from 'react'
import { Drawer, Descriptions, Empty } from 'antd'
import {
  getLatestFieldPolicyReport,
  readFieldPolicyReport,
  type FieldPolicyReport,
} from '@/utils/fieldPolicy'
import { filterViewFieldsByReport } from './fieldPolicy'

export interface ViewField<T = Record<string, unknown>> {
  name: string
  label: string
  /** 自定义渲染(默认显示原值) */
  render?: (value: any, data: T) => React.ReactNode
}

export interface ViewDrawerProps<T = Record<string, unknown>> {
  title: React.ReactNode
  open: boolean
  data?: T | null
  fields: ViewField<T>[]
  onClose: () => void
  width?: number
  /** 底部 extra 内容(按钮等) */
  extra?: React.ReactNode
}

export function ViewDrawer<T = Record<string, unknown>>({
  title,
  open,
  data,
  fields,
  onClose,
  width = 640,
  extra,
}: ViewDrawerProps<T>) {
  /**
   * 字段级可见性报告(M168)
   *
   * # 报告从哪来(两级)
   *
   *   ① **盖在 `data` 行对象上的 Symbol 戳** —— 权威来源:
   *      表格里的行就是服务端响应里的那一份(api/client.ts 盖过章)。
   *   ② **本页面最近一次响应的报告**(兜底)—— 页面若把行对象重新拼过,
   *      戳就丢了;用页面归档的那份(按路径归档,不会串页)。
   *
   * ⚠️ **打码的字段照常显示** —— 值已经是服务端打码后的;
   *   这里只藏"服务端删掉键"的那些字段(留着就是"标签在、值是 -",
   *   跟表格"列还在、格是空的"同一个毛病)。
   *   没有报告时一行字段都不动(零开销快路径)。
   */
  const fieldPolicyReport: FieldPolicyReport | null =
    readFieldPolicyReport(data) ?? getLatestFieldPolicyReport()
  const visibleFields = filterViewFieldsByReport(fields, fieldPolicyReport)
  return (
    <Drawer title={title} open={open} onClose={onClose} width={width} destroyOnHidden extra={extra}>
      {data ? (
        <Descriptions column={1} bordered size="small">
          {visibleFields.map((f) => {
            const raw = (data as any)[f.name]
            const value = f.render ? f.render(raw, data) : (raw ?? '-')
            return (
              <Descriptions.Item key={f.name} label={f.label}>
                {value}
              </Descriptions.Item>
            )
          })}
        </Descriptions>
      ) : (
        <Empty />
      )}
    </Drawer>
  )
}

export default ViewDrawer
