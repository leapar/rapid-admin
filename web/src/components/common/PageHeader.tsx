/**
 * PageHeader —— 页面顶部描述条
 *
 * ⚠️ **2026-10-25:页内大标题全线退场**
 *   标题的唯一权威是布局里的面包屑(BasicLayout → Breadcrumb,按路由自动
 *   生成菜单名)。页内再写一遍就是同一句话说两遍 —— 用户:
 *   「所有 <PageHeader> 的地方都不要传 title」。
 *
 *   所以本组件**不再接受 title**:
 *     - title / subTitle / extra / breadcrumb 四个 prop 全部删除
 *       (title 删掉后它们都是零调用方 —— extra / breadcrumb 本来就没人传)
 *     - 组件只剩 description,永远只渲染一行描述文本
 *
 *   为什么是"删掉"而不是"留着但注释里写'新页面不要传'":
 *   cb31790 处理 ManagementTable.pageTitle 时选的是后者。实测这种约束守不住
 *   —— 只要 prop 还在,下一个人写 title= 的时候编译器一句话都不会说,
 *   重复的标题就回来了。结构性不给传,才是真不给。
 *
 *   (面包屑那边不受影响:面包屑自己按路由生成页面名,
 *    见 components/layout/pageName.ts。)
 */
import React from 'react'
import { Typography } from 'antd'

const { Text } = Typography

export interface PageHeaderProps {
  /**
   * 页面描述 —— 标题交给面包屑,这里只留一句说明
   */
  description?: string
}

export const PageHeader: React.FC<PageHeaderProps> = ({ description }) => {
  return (
    <div style={{ marginBottom: 16 }}>
      {description && (
        <Text type="secondary" style={{ display: 'block', fontSize: 11 }}>
          {description}
        </Text>
      )}
    </div>
  )
}

export default PageHeader
