/**
 * MultiDeptTreeSelect —— 多选部门选择器(框架层通用组件,M57)
 *
 * 能力:
 *  1. Tree 选多个部门(支持 treeCheckable,父子节点独立勾选)
 *  2. ⭐ 主部门标记(从已选项里再选一个;没选时自动取第一项)
 *  3. 拉取部门树(GET /system/departments/tree)内置
 *  4. 受控 value(外部传 {departmentIds, primaryDepartmentId})
 *  5. 可选只读(显示已选,不可改)
 *
 * 用法:
 *  const [val, setVal] = useState({ departmentIds: [], primaryDepartmentId: undefined })
 *  <MultiDeptTreeSelect value={val} onChange={setVal} />
 *
 *  // Form 用(与 EditModal field type='multiDept' 集成):
 *  <Form.Item name="multiDept" ...>
 *    <MultiDeptTreeSelect />
 *  </Form.Item>
 */
import React, { useEffect, useMemo, useState } from 'react'
import { TreeSelect, Space, Tag, Spin, Empty } from 'antd'
import type { DataNode } from 'antd/es/tree'
import { StarFilled, StarOutlined } from '@ant-design/icons'
import { departmentApi, DepartmentTreeNode } from '@/api/role'
import { useI18n } from '@/locales'

export interface MultiDeptValue {
  departmentIds: string[]
  primaryDepartmentId?: string
}

export interface MultiDeptTreeSelectProps {
  value?: MultiDeptValue
  onChange?: (v: MultiDeptValue) => void
  /** 只读(显示已选,不可改) */
  readOnly?: boolean
  /** 占位 */
  placeholder?: string
  /** 拉取自定义(测试可注入) */
  fetcher?: () => Promise<DepartmentTreeNode[]>
  /** 限制最多选几个(默认不限) */
  maxCount?: number
  /**
   * DOM id(Form.Item 会把它传下来)
   *
   * ⚠️ 2026-10-25:以前这个 id 被吞了 —— Form.Item 的 label 写 htmlFor="multiDept",
   *    组件却没往 DOM 上放这个 id → label 指向一个不存在的控件,
   *    点标签聚焦不了、无障碍也读不出这个字段。往下透传即可。
   */
  id?: string
}

/**
 * 把 DepartmentTreeNode 转 Antd DataNode(递归)
 *
 * ⚠️ 2026-10-25 修一个真 bug:TreeSelect 的树节点**必须同时有 value 和 label**
 *    (rc-tree-select 的默认 fieldNames 是 {label:'label', value:'value'},
 *    而纯 Tree 的默认是 {title:'title'} —— 两者不一样!)
 *    以前这里只给了 key + title:
 *      · 勾选后 rc-tree-select 拿 node.value 建索引 → 索引不到 → 渲染时
 *        `labeledValueItem.label` 直接抛 TypeError(新增用户弹窗当场崩)
 *      · label 取不到 → 树上显示的是 undefined
 *    现在 value / label / key / title 四个都给,Tree 和 TreeSelect 都能用。
 */
function toTreeData(nodes: DepartmentTreeNode[] | undefined): DataNode[] {
  if (!nodes) return []
  return nodes.map((n) => ({
    key: String(n.id),
    value: String(n.id),
    label: n.name,
    title: n.name,
    disabled: !n.is_active,
    children: toTreeData(n.children),
  }))
}

/** 从树中按 id 找 name(平铺遍历) */
function findName(nodes: DepartmentTreeNode[] | undefined, id: string): string | undefined {
  if (!nodes) return undefined
  for (const n of nodes) {
    if (String(n.id) === id) return n.name
    const sub = findName(n.children, id)
    if (sub) return sub
  }
  return undefined
}

export const MultiDeptTreeSelect: React.FC<MultiDeptTreeSelectProps> = ({
  value,
  onChange,
  readOnly = false,
  placeholder,
  fetcher,
  maxCount,
  id,
}) => {
  const { t } = useI18n()
  const [tree, setTree] = useState<DepartmentTreeNode[] | undefined>(undefined)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (fetcher) {
      setLoading(true)
      fetcher()
        .then(setTree)
        .catch(() => setTree([]))
        .finally(() => setLoading(false))
      return
    }
    setLoading(true)
    departmentApi
      .tree()
      .then((res) => setTree((res as DepartmentTreeNode[]) ?? []))
      .catch(() => setTree([]))
      .finally(() => setLoading(false))
  }, [fetcher])

  const treeData = useMemo(() => toTreeData(tree), [tree])
  const currentIds = value?.departmentIds ?? []
  const primaryId = value?.primaryDepartmentId

  const handleChange = (newIds: string[]) => {
    let nextPrimary = primaryId
    // 主部门被取消 → 自动取第一项
    if (nextPrimary && !newIds.includes(nextPrimary)) {
      nextPrimary = newIds[0]
    }
    // 主部门未设 → 自动取第一项
    if (!nextPrimary && newIds.length > 0) {
      nextPrimary = newIds[0]
    }
    // 全清空 → 主部门也清空
    if (newIds.length === 0) {
      nextPrimary = undefined
    }
    onChange?.({ departmentIds: newIds, primaryDepartmentId: nextPrimary })
  }

  const handleSetPrimary = (id: string) => {
    onChange?.({ departmentIds: currentIds, primaryDepartmentId: id })
  }

  // ⚠️ `tree === undefined` 也要转 Spin(不只是 loading):
  //   树是异步拉的,首帧 treeData 还是空的 —— 这时候把 value 传给 TreeSelect,
  //   rc-tree 会发现 value 里的 key 在树里找不到,打一句
  //   "Warning: Tree missing follow keys: '100', '101'"。
  //   (2026-10-25 M5d 打开"编辑部门"之后必现:回填的部门 id 在树到位之前就有了)
  //   树没到位就不渲染选择器 —— 既消掉告警,也不会短暂显示一个空树。
  if (loading || tree === undefined) {
    return <Spin />
  }
  if (readOnly) {
    if (currentIds.length === 0) {
      return (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={t('component.multiDept.empty')}
        />
      )
    }
    return (
      <Space wrap id={id}>
        {currentIds.map((id) => {
          const isPrimary = id === primaryId
          return (
            <Tag
              key={id}
              color={isPrimary ? 'gold' : 'default'}
              icon={isPrimary ? <StarFilled /> : undefined}
            >
              {findName(tree, id) ?? id}
              {!readOnly && currentIds.length > 1 && (
                <span
                  style={{ marginLeft: 6, cursor: 'pointer' }}
                  onClick={(e) => {
                    e.preventDefault()
                    handleSetPrimary(id)
                  }}
                  title={
                    isPrimary
                      ? t('component.multiDept.alreadyPrimary')
                      : t('component.multiDept.setPrimary')
                  }
                >
                  {isPrimary ? <StarFilled style={{ color: '#faad14' }} /> : <StarOutlined />}
                </span>
              )}
            </Tag>
          )
        })}
      </Space>
    )
  }

  return (
    <div>
      <TreeSelect
        id={id}
        multiple
        treeCheckable
        treeDefaultExpandAll={false}
        showCheckedStrategy="SHOW_CHILD"
        treeData={treeData}
        value={currentIds}
        onChange={handleChange}
        placeholder={placeholder ?? t('component.multiDept.placeholder')}
        style={{ width: '100%' }}
        maxTagCount={3}
        {...(maxCount ? { treeCheckStrictly: false } : {})}
      />
      {currentIds.length > 1 && (
        <div style={{ marginTop: 8, fontSize: 12, color: 'var(--app-text-tertiary)' }}>
          {t('component.multiDept.selectedHint', undefined, {
            n: currentIds.length,
            name: findName(tree, primaryId ?? '') ?? primaryId ?? t('component.multiDept.autoFirst'),
          })}
          <a
            style={{ marginLeft: 8 }}
            onClick={(e) => {
              e.preventDefault()
              const next = currentIds[0]
              if (next) handleSetPrimary(next)
            }}
          >
            {t('common.switch')}
          </a>
        </div>
      )}
    </div>
  )
}

export default MultiDeptTreeSelect
