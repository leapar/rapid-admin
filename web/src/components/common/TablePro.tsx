/**
 * TablePro —— 增强表格(§2.8 #10/11/12/13 P2)
 *
 * 在 Antd Table 之上集成 4 大能力:
 *  1. 虚拟滚动(> 100 行自动启用)
 *  2. 列设置(显示/隐藏 + 列宽,localStorage 记忆)
 *  3. 树形数据
 *  4. 导出 CSV
 *
 * 用法:
 *  <TablePro
 *    storageKey="user-list"
 *    columns={[
 *      { key: 'name', title: '姓名', dataIndex: 'name', width: 120 },
 *      { key: 'phone', title: '手机', dataIndex: 'phone', width: 150 },
 *    ]}
 *    dataSource={rows}
 *  />
 *
 * 列设置 UI 通过 ColumnSettings 组件独立渲染(在工具栏里弹)
 */
import { useState } from 'react'
import { Table, Button, Space, Tooltip } from 'antd'
import { SettingOutlined, DownloadOutlined } from '@ant-design/icons'
import type { TableProps } from 'antd'
import { useTablePro, TableProColumn } from '@/hooks/useTablePro'
import { ColumnSettings } from './ColumnSettings'
import { useI18n } from '@/locales'

export interface TableProProps<T = any> {
  /** localStorage 记忆 key(必填,同 key 自动恢复列设置) */
  storageKey: string
  /** 列定义 */
  columns: TableProColumn<T>[]
  /** 数据源 */
  dataSource: T[]
  /**
   * 行 key 字段(默认 id)
   *
   * ⚠️ **M111 修正:以前传函数会被静默丢掉**
   *   旧实现是 `typeof rowKey === 'string' ? rowKey : undefined` ——
   *   函数形式直接变成 `undefined` ⇒ antd 回退到默认行 key(数据里的
   *   `key` 字段,通常也没有)⇒ **所有行的 React key 变成 undefined**,
   *   表现是:勾选状态串行、分页回来行错位、编辑抽屉带错行。
   *   而 `rowKey` 的类型**明明声明了函数形式** ⇒ TS 不报错,
   *   只有运行时才坏。现在函数形式原样透传给 `<Table>`。
   */
  rowKey?: string | ((record: T) => string)
  /** 虚拟滚动阈值(行数超过即启用,默认 100) */
  virtualThreshold?: number
  /** 行高(默认 54) */
  rowHeight?: number
  /** 树形模式(行含 children 字段) */
  treeMode?: boolean
  /** 树形默认展开所有 */
  defaultExpandAll?: boolean
  /** 是否显示工具栏(列设置 / 导出) */
  showToolbar?: boolean
  /** 是否显示列设置按钮 */
  showColumnSettings?: boolean
  /** 是否显示导出按钮 */
  showExport?: boolean
  /** Antd Table 透传 */
  onRow?: TableProps<T>['onRow']
  loading?: boolean
  size?: 'small' | 'middle' | 'large'
  /** 导出文件名前缀(默认 storageKey) */
  exportFilename?: string
}

export function TablePro<T extends Record<string, any>>(props: TableProProps<T>) {
  const {
    storageKey,
    columns,
    dataSource,
    rowKey = 'id',
    virtualThreshold = 100,
    rowHeight = 54,
    treeMode = false,
    defaultExpandAll = false,
    showToolbar = true,
    showColumnSettings = true,
    showExport = true,
    onRow,
    loading = false,
    size = 'middle',
    exportFilename,
  } = props

  const [settingsOpen, setSettingsOpen] = useState(false)
  const { t } = useI18n()

  const {
    columns: appliedColumns,
    tableProps,
    columnSettings,
    exportCSV,
  } = useTablePro<T>({
    storageKey,
    columns,
    dataSource,
    rowKey,
    virtualThreshold,
    rowHeight,
    treeMode,
    defaultExpandAll,
  })

  return (
    <div>
      {showToolbar && (showColumnSettings || showExport) && (
        <div style={{ marginBottom: 12, display: 'flex', justifyContent: 'flex-end' }}>
          <Space>
            {showExport && (
              <Tooltip title={t('component.tablePro.exportAria')}>
                <Button
                  icon={<DownloadOutlined />}
                  onClick={() => exportCSV(exportFilename)}
                  disabled={dataSource.length === 0}
                >
                  {t('component.tablePro.exportCsv')}
                </Button>
              </Tooltip>
            )}
            {showColumnSettings && (
              <Tooltip title={t('component.tablePro.columnSettingsAria')}>
                <Button icon={<SettingOutlined />} onClick={() => setSettingsOpen(true)}>
                  {t('component.tablePro.columnSettings')}
                </Button>
              </Tooltip>
            )}
          </Space>
        </div>
      )}
      <Table<T>
        {...(tableProps as any)}
        columns={appliedColumns}
        dataSource={dataSource as any}
        onRow={onRow}
        loading={loading}
        size={size}
        bordered={false}
      />
      {showColumnSettings && (
        <ColumnSettings
          open={settingsOpen}
          onClose={() => setSettingsOpen(false)}
          columns={columnSettings.all}
          states={columnSettings.states}
          onVisibleChange={columnSettings.setVisible}
          onWidthChange={columnSettings.setWidth}
          onReset={columnSettings.reset}
        />
      )}
    </div>
  )
}
