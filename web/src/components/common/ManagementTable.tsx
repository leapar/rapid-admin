/**
 * ManagementTable —— 通用管理页表格外壳
 *
 * 集成:
 *   - PageHeader (顶部描述;**页内大标题 2026-10-25 起不再渲染** —— 标题交给布局里的面包屑)
 *   - SearchSection (搜索栏)
 *   - Card: 可选 inlineToolBar (动作按钮) + Antd Table
 *
 * 滚动策略 (官方 table-demo-auto-height 思路,2026-10-18 收敛):
 *   - 等 Table 首次 layout 完成(用 .ant-table-body 的 scroll 事件当"布局完成"信号,
 *     避免 ResizeObserver 反馈循环 setScrollY→DOM 变化→RO→setScrollY 的"慢慢缩小")
 *   - 测 Table 根容器高度 - thead 高 - pagination 高 = scroll.y
 *   - getBoundingClientRect + getComputedStyle marginTop/marginBottom 算元素真实占用高度
 *   - 2026-10-26: **不再留 30 buffer** —— 分页行的下边距已在 global.css 里归零
 *     (.ant-table-wrapper .ant-table-pagination.ant-pagination { margin-bottom: 0 }),
 *     翻页器下面不再有那截 16px 空白,腾出来的高度直接让给行区
 *   - scroll.y 初始给 400,避免 antd 5 首次渲染把 .ant-table-body max-height 设为 0
 *   - 500ms 兜底:数据少 body 不可滚时,scroll 事件不会触发,必须定时器兜底
 *
 * 用法:
 *   <div style={{ height: '100%' }}>
 *     <ManagementTable columns={...} tableProps={tableProps} />
 *   </div>
 */
import React from 'react'
import { Table, Input, Card, Space } from 'antd'
import type { TableProps, GetRef } from 'antd'
// ⚠️ **ColumnsType 必须从 'antd/es/table' 取,不能从 'antd' 根取** ——
//   antd 5.29 的根导出里没有 ColumnsType(TS2305)。
//   src/pages/file/list.tsx、folder.tsx、profile/SessionPanel.tsx 都是这么写的,
//   跟着它们走,别自创一套。
import type { ColumnsType } from 'antd/es/table'
import { SearchOutlined, ReloadOutlined } from '@ant-design/icons'
import { PageHeader } from './PageHeader'
import { SearchSection } from './SearchSection'
import { TableErrorState } from './TableErrorState'
import { filterColumnsByReport } from './fieldPolicy'
import { useI18n } from '@/locales'
import type { TableErrorChannel } from '@/hooks/useTable'

export interface ManagementTableProps<T = unknown> {
  pageDescription?: string
  searchPlaceholder?: string
  onSearch?: (keyword: string) => void
  onReset?: () => void
  searchExtra?: React.ReactNode
  /**
   * 行内工具栏(刷新 / 新增 之类)—— **渲染在搜索区的右侧**(2026-10-25)
   *
   * 之前它渲染在 Card 里、表格正上方,跟搜索框上下叠成两行。
   * 现在跟搜索框同一行:左边搜索组件,右边工具栏(左组 flex:1 撑开,
   * 工具栏被顶到最右)。这是后台列表页的通行排版。
   *
   * ⚠️ 搬上来之后 `showSearchBar` 必须把 inlineToolBar 也算进去 ——
   *   否则"只有工具栏、没有搜索框"的页面会连工具栏一起消失。
   */
  inlineToolBar?: React.ReactNode
  /**
   * 页头**下方**的额外内容(渲染顺序:PageHeader → headerExtra → 搜索区 → 表格)
   *
   * 2026-10-25 新增。字典管理页要在页头下面放一条"字典只读"的说明 Alert ——
   *   它原来直接写在 dict/index.tsx 里、挂在 `<ManagementTable>` **外面**,
   *   而页头是本组件内部渲染的,所以那条 Alert 实际落在了页头描述**上面**。
   *   要让它待在页头下面,得由本组件在页头之后开一个口子。
   *
   * ⚠️ **本组件被 17 个页面共用**(用户/角色/权限/字典/任务/文件/业主/公告/
   *   资料/工单/审批/流程/审计/配置审计……)。**绝不能把某个具体页面的文案
   *   写死在这里** —— 那会漏到每一个列表页上去(用户管理页顶上弹出
   *   "字典是代码 enum 的中央仓库"就是这种事故)。页面专属内容一律由调用方
   *   通过这个 prop 传进来。
   */
  headerExtra?: React.ReactNode
  columns: ColumnsType<T>
  /**
   * 表格 props
   *
   * 除了 Antd 自己的 prop,还带一条**框架通道** `error` / `onRetry` / `fieldPolicy`
   * (useTable 把最近的失败、重试回调和字段可见性报告塞在这里,
   * 见 hooks/useTable.ts)。
   * 本组件会**先把这些字段摘掉**再展开给 `<Table>` ——
   * 它们不是 Antd 的 prop,漏下去会变成 DOM 上的未知属性。
   */
  /**
   * ⚠️ **`onRow` 在这个 Pick 里**(2026-11-01 用户提的「文件夹管理列表要支持点击进入」)
   *
   *   「点整行进入」的实现方式就是把 `onRow` 递给 `<Table>` ——
   *   rc-table 会把返回的 `onClick` / `className` / `style` 落到 `<tr>` 上
   *   (见 rc-table `Body/BodyRow.js`:`rowProps` 整个展开到行元素)。
   *
   *   之前这个 Pick 里**没有** `onRow` ⇒ 页面想给行加点击只能整个绕开
   *   `ManagementTable` 自己画表格 ⇒ 搜索区 / 工具栏 / 错误态全得复制一份。
   *
   *   ⚠️ **页面自己负责挡掉行内的交互元素**(按钮 / 名字链接 / 封面预览 / 标签)——
   *   共享判定见 `components/common/rowClick.ts`。
   */
  tableProps: Pick<
    TableProps<T>,
    'dataSource' | 'loading' | 'pagination' | 'onChange' | 'rowSelection' | 'rowKey' | 'onRow'
  > &
    TableErrorChannel
  /** 表格横向滚阈值(列多时传) */
  scrollX?: number
  /**
   * **自定义列表体**(2026-10-31 M134)
   *
   * 传了就用它替换 `<Table>`,**外壳不变**(页头 / 搜索区 / 工具栏 / Card)。
   *
   * ⚠️ **为什么要有这个口子**:文件列表要能切「表格 / 缩略图」两种视图,
   *   而两种视图**共用同一套搜索区和工具栏**(上传按钮、配额、文件夹筛选
   *   都在里面)。把搜索区复制一份到另一个视图里 = 两份上传逻辑、两份
   *   筛选状态,迟早对不上。
   *
   * ⚠️ **本组件被 17 个页面共用** —— 这个口子必须**通用**:
   *   只准传节点,不许在这里写任何页面专属的东西。
   *
   * ⚠️ 传了之后**分页要自己渲染**(表格那份分页器随 `<Table>` 一起没了),
   *   错误态也一样(`error` / `onRetry` 还在 tableProps 里,自己取)。
   */
  body?: React.ReactNode
}

export function ManagementTable<T extends object = any>({
  pageDescription,
  searchPlaceholder,
  onSearch,
  onReset,
  searchExtra,
  inlineToolBar,
  headerExtra,
  columns,
  tableProps,
  scrollX,
  body,
}: ManagementTableProps<T>) {
  const { t } = useI18n()
  const [keyword, setKeyword] = React.useState('')
  /** 有自定义列表体时,表格不渲染(测高那套逻辑也就不该跑) */
  const hasBody = body !== undefined && body !== null

  /**
   * 错误态(2026-10-25 M7)
   *
   * ⚠️ `error` / `onRetry` 是 useTable 的**框架通道**,不是 Antd 的 prop
   *   —— 展开给 `<Table>` 之前必须摘掉,否则会漏到 DOM 上
   *   (React 会对未知属性报警,Antd Table 也不该收到它们)。
   *
   * 两种形态:
   *   · 表里没数据了 → 用 `locale.emptyText` 把表格自带的"暂无数据"
   *     换成降级占位(错误 + 重试)。**不把 Table 摘掉** ——
   *     摘掉会让下面那段 auto-height 测量拿到 0,scroll.y 变成 0,
   *     数据回来时表格高度是 0 → **行看不见**(比空表格还糟)。
   *   · 表里还有旧数据 → 表格上方一条安静的提示条,旧数据继续留着。
   */
  const { error, onRetry, fieldPolicy, ...antdTableProps } = tableProps
  const hasData = (antdTableProps.dataSource?.length ?? 0) > 0
  const showError = error !== null && error !== undefined

  /**
   * 字段级可见性(M168):把"服务端已经删掉键"的列藏掉
   *
   * ⚠️ **报告来自服务端响应**(useTable 从响应上读回),前端不自己算规则 ——
   *   服务端"隐藏"是删键 ⇒ 不藏列的话就是"列还在、格是空的"。
   *   没有报告时原样返回(零开销快路径,行为与 M168 之前逐字节相同)。
   */
  const visibleColumns = React.useMemo(
    () => filterColumnsByReport(columns, fieldPolicy ?? null),
    [columns, fieldPolicy],
  )

  // 官方 auto-height 模式
  const tableRef = React.useRef<GetRef<typeof Table>>(null)
  // 初始给个非 0 值,避免 antd 5 首次渲染把 .ant-table-body max-height 设为 0
  // RO 回调里会用真实值覆盖
  const [scrollY, setScrollY] = React.useState(400)

  /**
   * 测元素在 flex 父级里实际占据的高度 = box height + marginTop + marginBottom
   * (flex 容器里 marginTop / marginBottom 都会撑开父级高度,跟 block 一样)
   */
  const getHeight = (target: string | HTMLElement): number => {
    const el =
      typeof target === 'string'
        ? tableRef.current?.nativeElement?.querySelector<HTMLElement>(target)
        : target
    if (!el) return 0
    const styles = getComputedStyle(el)
    const marginTop = Number.parseFloat(styles.marginTop) || 0
    const marginBottom = Number.parseFloat(styles.marginBottom) || 0
    return el.getBoundingClientRect().height + marginTop + marginBottom
  }

  React.useEffect(() => {
    // ⚠️ **自定义列表体时整段跳过** —— 没有 <Table> 就没有 .ant-table-body,
    //   下面的测量全是 0,跑了也只是白跑。
    //
    //   ⚠️ **依赖里必须有 hasBody**:页面可能**先渲染网格、再切回表格**
    //   (视图模式是记在 localStorage 里的,上次停在网格模式就是这个顺序)。
    //   依赖不带上它的话,表格是后挂上去的,这段 effect 不会再跑一次 ⇒
    //   scroll.y 永远停在初始的 400 ⇒ **表格不撑满卡片**(下面一大截空白)。
    if (hasBody) return
    const root = tableRef.current?.nativeElement
    if (!root) return

    const measure = () => {
      const total = getHeight(root)
      const header = getHeight('.ant-table-thead')
      const pagination = getHeight('.ant-table-pagination')
      // 2026-10-26: 不再减 30 buffer —— 分页行下边距已在 global.css 归零,
      // 翻页器没有那截 16px 空白可留,多余的高度直接让给行区
      setScrollY(Math.max(0, Math.floor(total - header - pagination)))
    }

    // 等 Table 首次 layout 完成:onScroll 触发说明 .ant-table-body 已经可滚
    // 这一刻 thead/body/pagination 都已就位,measure 数据稳定,不会触发 RO feedback loop
    //
    // ⚠️ 变量叫 `tableBody` 而不是 `body` —— `body` 是上面那个 prop(自定义列表体),
    //   同名遮蔽会让"这段代码在量什么"读不出来。
    const tableBody = root.querySelector('.ant-table-body') as HTMLElement | null
    let triggered = false
    const onReady = () => {
      if (triggered) return
      triggered = true
      // 推一帧,等 antd 5 内部状态稳定再 setScrollY
      requestAnimationFrame(measure)
    }
    if (tableBody) {
      // scroll 事件只在 body 真正可滚时触发,等价于"布局完成"
      tableBody.addEventListener('scroll', onReady, { once: true, passive: true })
    }
    // 兜底:有些页面数据少 body 不可滚,500ms 后强制 measure
    const fallback = setTimeout(onReady, 500)

    return () => {
      if (tableBody) tableBody.removeEventListener('scroll', onReady)
      clearTimeout(fallback)
    }
  }, [hasBody])

  // ⚠️ inlineToolBar 也要算进来:只有它、没别的搜索元素时,搜索区照样要渲染
  //   (否则工具栏会跟着 searchBar 一起消失)。
  const showSearchBar = onSearch || onReset || searchExtra || inlineToolBar
  const searchBar = showSearchBar && (
    <SearchSection>
      {/* 左侧:搜索组件(搜索框 / 重置 / 搜索区附加项) */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          flexWrap: 'wrap',
          // flex:1 把右侧工具栏顶到最右; minWidth:0 让它能被压窄(不撑破容器)
          flex: '1 1 auto',
          minWidth: 0,
        }}
      >
        {onSearch && (
          <Input.Search
            placeholder={searchPlaceholder ?? t('common.search')}
            allowClear
            enterButton={<SearchOutlined />}
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onSearch={(v) => onSearch(v.trim())}
            style={{ width: 320 }}
          />
        )}
        {onReset && (
          <a
            onClick={() => {
              // ⚠️ **必须把输入框里自己那份 keyword 也清掉** ——
              //   只调 onReset 的话:页面状态清了、输入框里还留着字,
              //   用户看着框里的词以为还在过滤(实际已经不过滤了)。
              //   这是"UI 与状态对不上"的一类 —— 17 个列表页都走这条路。
              setKeyword('')
              onReset()
            }}
            style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}
          >
            <ReloadOutlined /> {t('common.reset')}
          </a>
        )}
        {searchExtra && <Space size={8} wrap>{searchExtra}</Space>}
      </div>
      {/* 右侧:行内工具栏(刷新 / 新增 之类)—— 2026-10-25 从 Card 里搬上来 */}
      {inlineToolBar && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flex: '0 0 auto' }}>
          {inlineToolBar}
        </div>
      )}
    </SearchSection>
  )

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      <div style={{ flexShrink: 0 }}>
        <PageHeader description={pageDescription} />
      </div>
      {/* 页头下方的额外内容(字典管理页的"字典只读"说明 Alert 走这个槽位) */}
      {headerExtra && <div style={{ flexShrink: 0 }}>{headerExtra}</div>}
      {searchBar}
      <Card
        variant="borderless"
        style={{
          flex: 1,
          minHeight: 0,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
        }}
        styles={{
          // Antd 5 Card 即使没 title 也会渲染一个 52px minHeight 的 head 容器
          header: { display: 'none' },
          body: {
            flex: 1,
            minHeight: 0,
            padding: 16,
            display: 'flex',
            flexDirection: 'column',
          },
        }}
      >
        {/* ⚠️ inlineToolBar 2026-10-25 已搬到搜索区右侧,不再渲染在 Card 里 */}
        {/* 有旧数据时:表格上方一条提示条(数据继续留着,不洗白) */}
        {showError && hasData && <TableErrorState variant="strip" onRetry={onRetry} />}
        {hasBody ? (
          /**
           * ⚠️ **自定义列表体**(M134):表格本体被换掉了。
           *
           *   分页器、错误占位都随 `<Table>` 一起没了 —— **由 body 自己负责**
           *   (文件页的缩略图视图自己渲染分页器和错误态)。
           */
          body
        ) : (
          <Table<T>
            ref={tableRef}
            columns={visibleColumns}
            rowKey={(r: any) => (r as any).id ?? (r as any).ID}
            size="middle"
            // height: 100% 让 antd 5 把 .ant-table-wrapper 撑满 Card body 剩余空间
            style={{ height: '100%' }}
            scroll={{ ...(scrollX ? { x: scrollX } : {}), y: scrollY }}
            {...antdTableProps}
            // 没数据 + 失败 → 把表格自带的"暂无数据"换成降级占位(错误 + 重试)
            locale={
              showError && !hasData
                ? { emptyText: <TableErrorState variant="empty" onRetry={onRetry} />}
                : undefined
            }
          />
        )}
      </Card>
    </div>
  )
}

export default ManagementTable
