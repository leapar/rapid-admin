/**
 * 工作台(仪表盘)
 *
 * ## ⚠️ KPI 卡片字段名是 snake_case
 *
 *   页面读的是 `overview.file_total` 这类 wire 字段名。axios 层是**纯透传**
 *   (`client.ts` 里 `r.data.data as T`,零改名),TypeScript 类型是前端自己写的
 *   —— 读成驼峰不报错、页面照常渲染,只是值恒为 undefined ⇒ 卡片永远是 "-"。
 *
 * ## ⚠️ 看板内容**按域权限过滤**(服务端算,前端只照着画)
 *
 *   服务端把**没权限的域整个从响应里省掉**
 *   (`Overview` 的字段是 `*int64` + `omitempty`
 *   ⇒ JSON 里**压根没有这个 key**),前端据此决定"这张卡片**要不要画**"。
 *
 *   ⚠️ **前端不自己判权限**(显示层不许自己算,必须读执行层那个权威字段):
 *
 *   - 前端能拿到的权限码来自 `/resource/accessible`(**绑定矩阵** = 显示层真相);
 *   - 后端算可见性用的是 casbin(**执行层**真相);
 *   - 运行时改绑定 → casbin 同步之间有一个窗口,两者会分叉。
 *
 *   ⇒ 前端**只认"服务端给了没有"**,不查自己的权限清单。
 *
 *   ⚠️ 判"在不在"必须用 `!== undefined`,**不能用真值判断** ——
 *     **0 也要画**("看得见但这个月一个都没有" ≠ "看不见")。
 */
import { useMemo } from 'react'
import type { FC, ReactNode } from 'react'
import { Row, Col, Card, Statistic, Empty, Space, Typography, Progress, Button, theme } from 'antd'
import {
  FolderOutlined,
  FileOutlined,
  HddOutlined,
  UploadOutlined,
  UserOutlined,
  ReloadOutlined,
  DashboardOutlined,
} from '@ant-design/icons'
import { useRequest } from '@/hooks/useRequest'
import { dashboardApi } from '@/api/dashboard'
import type { ServerMetrics } from '@/api/dashboard'
import { DASHBOARD_KPI_KEYS } from './domains'
import type { DashboardKpiKey } from './domains'
import { PageHeader } from '@/components/common'
// ⚠️ echarts 只服务看板,不进 common 桶文件
import { LoginHeatmap } from '@/components/common/LoginHeatmap'
import { useI18n } from '@/locales'
import { usePermission } from '@/hooks/usePermission'
import { formatNumber } from '@/utils/format'

const { Text } = Typography

/** 一次请求的三态 */
type LoadState = 'pending' | 'ready' | 'failed'

/**
 * 请求三态
 *
 * ⚠️ **不能用 `loading` 判"有没有数据"** —— `useRequest` 的 `loading`
 *   在**首帧还是 false**(effect 还没跑),首帧就会把"还没拿到"当成
 *   "服务端返回了空" ⇒ 空态闪一下。
 *
 * ⚠️ **失败要和"什么都没有"分开** —— 失败说"加载失败",
 *   没权限说"当前身份没有可查看的看板数据",两句话不能混。
 */
function loadState<T>(data: T | undefined, error: unknown): LoadState {
  if (error !== null && error !== undefined) return 'failed'
  return data === undefined ? 'pending' : 'ready'
}

/** 参考样式里的「标签: 值」行(运行环境 / 磁盘 / 内存明细共用) */
function EnvRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div style={{ display: 'flex', padding: '4px 0', fontSize: 13 }}>
      <Text type="secondary" style={{ width: 130, flexShrink: 0 }}>
        {label}:
      </Text>
      <Text>{value}</Text>
    </div>
  )
}

/** 月份名(热力图顶部标签)—— 字面量 key ⇒ i18n 守门网扫得到 */
const HEATMAP_MONTH_KEYS = [
  'dashboard.heatmap.month1',
  'dashboard.heatmap.month2',
  'dashboard.heatmap.month3',
  'dashboard.heatmap.month4',
  'dashboard.heatmap.month5',
  'dashboard.heatmap.month6',
  'dashboard.heatmap.month7',
  'dashboard.heatmap.month8',
  'dashboard.heatmap.month9',
  'dashboard.heatmap.month10',
  'dashboard.heatmap.month11',
  'dashboard.heatmap.month12',
] as const

/** 星期名(echarts dayLabel 约定:下标 0 = 周日;行序周一在首) */
const HEATMAP_DAY_KEYS = [
  'dashboard.heatmap.daySun',
  'dashboard.heatmap.dayMon',
  'dashboard.heatmap.dayTue',
  'dashboard.heatmap.dayWed',
  'dashboard.heatmap.dayThu',
  'dashboard.heatmap.dayFri',
  'dashboard.heatmap.daySat',
] as const

const Dashboard: FC = () => {
  const { t } = useI18n()
  const { has } = usePermission()
  // 环形进度线色跟随皮肤品牌主色(2026-11-05 皮肤沉浸式,原为写死 antd 蓝)
  const { token: { colorPrimary } } = theme.useToken()

  const {
    data: overview,
    error: overviewError,
  } = useRequest(() => dashboardApi.overview())
  const { data: heatmap, loading: heatmapLoading } = useRequest(() => dashboardApi.loginHeatmap(182))

  // ⚠️ 服务器资源卡按 dashboard:server 显隐(2026-11-03)——
  //   服务器内部信息只给平台运营线(super_admin 直通),与路由的 rbac 同一个码。
  //   useRequest 没有 ready 参数 ⇒ 没权限时 service 直接 resolve(undefined),
  //   请求根本不发(与 M147「没有权限码就别调」同一纪律)。
  const showServerMetrics = has('dashboard:server')
  const {
    data: metrics,
    loading: metricsLoading,
    refresh: refreshMetrics,
  } = useRequest(() => (showServerMetrics ? dashboardApi.serverMetrics() : Promise.resolve<ServerMetrics | undefined>(undefined)))

  const overviewState = loadState(overview, overviewError)

  /**
   * KPI 卡片
   *
   * ⚠️ `key` 是 **wire 上的 snake_case 字段名** —— 读成驼峰就恒为 undefined
   *   (见文件头)。
   *
   * ⚠️ 用 `Record<DashboardKpiKey, …>` 而不是数组 —— Record **要求每个键都在**
   *   ⇒ 后端加了指标而这里忘了加 label ⇒ **编译期就红**;
   *   数组的话少一个键只是"运行时不画",静默。
   */
  const kpiMeta = useMemo<
    Record<DashboardKpiKey, { label: string; icon: ReactNode; color: string; formatter?: (v: number) => string }>
  >(
    () => ({
      // ── 文件系统(M80)统计(2026-11-03)──
      file_total: {
        label: t('dashboard.kpi.fileTotal'),
        icon: <FileOutlined />,
        color: '#2f54eb',
      },
      file_today: {
        label: t('dashboard.kpi.fileToday'),
        icon: <UploadOutlined />,
        color: '#52c41a',
      },
      file_storage_bytes: {
        label: t('dashboard.kpi.fileStorageBytes'),
        icon: <HddOutlined />,
        color: '#fa8c16',
        // 后端给的是原始字节数,格式化成 B/KB/MB/GB(与文件列表页同一把尺子)
        formatter: (v: number) => formatNumber.fileSize(v),
      },
      folder_total: {
        label: t('dashboard.kpi.folderTotal'),
        icon: <FolderOutlined />,
        color: '#13c2c2',
      },
      // 当前在线(2026-11-03)—— 会话登记簿聚合,纯计数
      user_online: {
        label: t('dashboard.kpi.userOnline'),
        icon: <UserOutlined />,
        color: '#52c41a',
      },
    }),
    [t],
  )

  /** 键的顺序 = `domains.ts` 里的清单顺序(= 后端结构体字段顺序) */
  const kpis = useMemo(
    () => DASHBOARD_KPI_KEYS.map((key) => ({ key, ...kpiMeta[key] })),
    [kpiMeta],
  )

  /**
   * **该画哪几张 KPI 卡片**
   *
   * ⚠️ 判据是"**服务端给了没有**"(字段缺省 = 没有该域的权限),
   *   不是前端自己的权限清单 —— 见文件头。
   *
   * ⚠️ `!== undefined` 而不是真值判断 —— **0 也要画**。
   */
  const visibleKpis = useMemo(() => {
    if (overviewState === 'pending') return kpis // 还没拿到 → 先把骨架铺出来
    if (overviewState === 'failed') return [] // 失败 → 不铺"-"的假卡片
    return kpis.filter((s) => overview?.[s.key] !== undefined)
  }, [kpis, overview, overviewState])

  /** 服务端给了响应,但一个域都看不到 —— 空态(≠ 加载失败) */
  const nothingVisible = overviewState === 'ready' && visibleKpis.length === 0

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', padding: '0 10px' }}>
      <PageHeader description={t('dashboard.description')} />

      {/* ===== 概览区的两种"什么都没有" ===== */}
      {overviewState === 'failed' && (
        <Card variant="borderless">
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('common.loadFailed')} />
        </Card>
      )}
      {nothingVisible && (
        <Card variant="borderless">
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={t('dashboard.empty.noVisibleData')}
          />
        </Card>
      )}

      {/* ===== KPI 卡片(只画服务端给了的) ===== */}
      {visibleKpis.length > 0 && (
        <Row gutter={[16, 16]} style={{ marginTop: overviewState === 'failed' ? 16 : 0 }}>
          {visibleKpis.map((s) => (
            <Col xs={12} sm={8} lg={6} key={s.key}>
              <Card loading={overviewState === 'pending'} variant="borderless">
                <Statistic
                  title={s.label}
                  value={overview?.[s.key] ?? '-'}
                  // formatter:带 formatter 的指标(存储占用)把原始值换成人话
                  //   —— 只在值真的是数字时套(加载中是 '-',不能交给 formatter)
                  formatter={
                    s.formatter && typeof overview?.[s.key] === 'number'
                      ? () => s.formatter!(overview![s.key] as number)
                      : undefined
                  }
                  prefix={<span style={{ color: s.color }}>{s.icon}</span>}
                  style={{ whiteSpace: 'nowrap' }}
                />
              </Card>
            </Col>
          ))}
        </Row>
      )}

      {/* ===== 每日登录热力图(2026-11-03)===== */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24}>
          <Card title={t('dashboard.heatmap.title')} variant="borderless">
            <LoginHeatmap
              points={heatmap ?? []}
              loading={heatmapLoading}
              emptyText={t('dashboard.heatmap.empty')}
              legendStart={t('dashboard.heatmap.less')}
              legendEnd={t('dashboard.heatmap.more')}
              monthNames={HEATMAP_MONTH_KEYS.map((k) => t(k))}
              dayNames={HEATMAP_DAY_KEYS.map((k) => t(k))}
              tooltipLabel={(date, count) =>
                t('dashboard.heatmap.tooltip', undefined, { date, n: count })
              }
            />
          </Card>
        </Col>
      </Row>

      {/* ===== 服务器资源(2026-11-03;dashboard:server 才显示)===== */}
      {showServerMetrics && (
        <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
          <Col xs={24}>
            <Card
              title={
                <Space size={6}>
                  <DashboardOutlined />
                  {t('dashboard.server.title')}
                </Space>
              }
              variant="borderless"
              extra={
                <Button
                  type="text"
                  size="small"
                  icon={<ReloadOutlined />}
                  onClick={() => refreshMetrics()}
                >
                  {t('dashboard.server.refresh')}
                </Button>
              }
            >
              {/*
                # 六张内嵌卡的排布(2026-11-03,用户拍板"要规整")
                同类配对,行内左右两卡用 height:100% 拉齐:
                  第 1 行  运行环境 | Redis      (纯键值行,高矮一致)
                  第 2 行  磁盘     | 内存       (明细 + 环形进度,同构)
                  第 3 行  系统磁盘             (整宽,每块盘一行)
                  第 4 行  CPU 每核             (整宽,双列 ⇒ 20 核只占 10 行)
              */}
              {metrics ? (
                <Row gutter={[16, 16]}>
                  {/* ===== 运行环境 ===== */}
                  <Col xs={24} md={12}>
                    <Card type="inner" size="small" title={t('dashboard.server.env')} style={{ height: '100%' }}>
                      <EnvRow label={t('dashboard.server.os')} value={metrics.os} />
                      <EnvRow
                        label={t('dashboard.server.cpuProcessors')}
                        value={String(metrics.cpu_cores)}
                      />
                      <EnvRow label={t('dashboard.server.compiler')} value={metrics.compiler} />
                      <EnvRow label={t('dashboard.server.goVersion')} value={metrics.go_version} />
                      <EnvRow
                        label={t('dashboard.server.goroutines')}
                        value={String(metrics.num_goroutine)}
                      />
                    </Card>
                  </Col>
                  {/* ===== Redis 概览(只给聚合数字,不列 key)===== */}
                  <Col xs={24} md={12}>
                    <Card type="inner" size="small" title={t('dashboard.server.redis')} style={{ height: '100%' }}>
                      <EnvRow
                        label={t('dashboard.server.redisStatus')}
                        value={
                          metrics.redis?.available ? (
                            <Text style={{ color: '#52c41a' }}>{t('dashboard.server.available')}</Text>
                          ) : (
                            <Text style={{ color: '#f5222d' }}>{t('dashboard.server.unavailable')}</Text>
                          )
                        }
                      />
                      <EnvRow label="version" value={metrics.redis?.version || '-'} />
                      <EnvRow
                        label={t('dashboard.server.redisKeys')}
                        value={metrics.redis ? metrics.redis.keys.toLocaleString() : '-'}
                      />
                      <EnvRow
                        label={t('dashboard.server.redisMemory')}
                        value={
                          metrics.redis?.available
                            ? formatNumber.fileSize(metrics.redis.used_memory_bytes)
                            : '-'
                        }
                      />
                    </Card>
                  </Col>
                  {/* ===== 磁盘(数据盘 —— 日志 / 上传文件所在的那块)===== */}
                  <Col xs={24} md={12}>
                    <Card type="inner" size="small" title={t('dashboard.server.disk')} style={{ height: '100%' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 24 }}>
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <EnvRow
                            label={t('dashboard.server.dataDisk')}
                            value={
                              <Text
                                style={{ fontSize: 12 }}
                                ellipsis={{ tooltip: metrics.disk_path }}
                              >
                                {metrics.disk_path}
                              </Text>
                            }
                          />
                          <EnvRow
                            label={t('dashboard.server.total')}
                            value={formatNumber.fileSize(metrics.disk_total_bytes)}
                          />
                          <EnvRow
                            label={t('dashboard.server.used')}
                            value={formatNumber.fileSize(metrics.disk_used_bytes)}
                          />
                          <EnvRow
                            label={t('dashboard.server.free')}
                            value={formatNumber.fileSize(
                              metrics.disk_total_bytes - metrics.disk_used_bytes,
                            )}
                          />
                        </div>
                        <Progress
                          type="circle"
                          percent={Math.round(metrics.disk_percent)}
                          size={110}
                          strokeColor={colorPrimary}
                        />
                      </div>
                    </Card>
                  </Col>
                  {/* ===== 内存 ===== */}
                  <Col xs={24} md={12}>
                    <Card type="inner" size="small" title={t('dashboard.server.memory')} style={{ height: '100%' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 24 }}>
                        <div style={{ flex: 1 }}>
                          <EnvRow
                            label={t('dashboard.server.total')}
                            value={formatNumber.fileSize(metrics.mem_total_bytes)}
                          />
                          <EnvRow
                            label={t('dashboard.server.used')}
                            value={formatNumber.fileSize(metrics.mem_used_bytes)}
                          />
                        </div>
                        <Progress
                          type="circle"
                          percent={Math.round(metrics.mem_percent)}
                          size={110}
                          strokeColor={colorPrimary}
                        />
                      </div>
                    </Card>
                  </Col>
                  {/* ===== 系统磁盘(整机各分区 —— 数据盘之外的 C/D 盘等)===== */}
                  <Col xs={24}>
                    <Card type="inner" size="small" title={t('dashboard.server.systemDisks')}>
                      {(metrics.disks ?? []).length === 0 ? (
                        <Text type="secondary" style={{ fontSize: 12 }}>
                          {t('dashboard.server.noDisks')}
                        </Text>
                      ) : (
                        (metrics.disks ?? []).map((d) => (
                          <div
                            key={d.mount}
                            style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '4px 0' }}
                          >
                            <Text style={{ width: 56, flexShrink: 0, fontSize: 12 }}>
                              {d.mount}
                            </Text>
                            <Progress
                              percent={Math.round(d.percent)}
                              size="small"
                              strokeColor={colorPrimary}
                              style={{ flex: 1, margin: 0 }}
                            />
                            <Text type="secondary" style={{ flexShrink: 0, fontSize: 12 }}>
                              {formatNumber.fileSize(d.used_bytes)} /{' '}
                              {formatNumber.fileSize(d.total_bytes)}
                            </Text>
                          </div>
                        ))
                      )}
                    </Card>
                  </Col>
                  {/* ===== CPU(每核进度条;整宽 + 双列,20 核只占 10 行)===== */}
                  <Col xs={24}>
                    <Card type="inner" size="small" title={t('dashboard.server.cpu')}>
                      <EnvRow
                        label={t('dashboard.server.cores')}
                        value={String(metrics.cpu_cores)}
                      />
                      <Row gutter={[24, 8]} style={{ marginTop: 8 }}>
                        {(metrics.percpu_percent ?? []).map((p, i) => (
                          <Col xs={24} md={12} key={i}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                              <Text type="secondary" style={{ width: 64, flexShrink: 0, fontSize: 12 }}>
                                {t('dashboard.server.coreN', undefined, { n: i + 1 })}
                              </Text>
                              <Progress
                                percent={Math.round(p)}
                                size="small"
                                strokeColor={colorPrimary}
                                style={{ flex: 1, margin: 0 }}
                              />
                            </div>
                          </Col>
                        ))}
                      </Row>
                    </Card>
                  </Col>
                </Row>
              ) : (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={metricsLoading ? t('common.loading') : t('common.loadFailed')}
                />
              )}
            </Card>
          </Col>
        </Row>
      )}

    </div>
  )
}

export default Dashboard
