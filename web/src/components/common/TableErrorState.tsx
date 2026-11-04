/**
 * TableErrorState —— 列表的错误态(2026-10-25 M7)
 *
 * 为什么需要它:
 *   M6 让 useTable 把失败记进 `error`(以前是未处理的 rejection,
 *   页面既不知道失败了、也没地方显示)。但**状态没人看等于没有** ——
 *   api/client.ts 会弹一次 toast,那是"发生了什么"的告知;
 *   用户真正需要的是"**再来一次**"这个动作,以及"这次没加载出来"的位置感。
 *
 * 两种形态,按"表里还有没有旧数据"分:
 *   · 'empty' —— 一条数据都没有(首屏就失败)→ 用降级占位(Empty + 重试),
 *     放在表格的 empty 区里(替换"暂无数据"),因为此时表格本身没有信息量
 *   · 'strip' —— 还有旧数据(刷新失败)→ 表格上方一条安静的提示条,
 *     **旧数据继续留着**(洗白比不新更糟),只提示"这次没刷新成功"
 *
 * ⚠️ 两种形态都**不再弹第二个 toast** —— 拦截器已经弹过一次,
 *   再弹一次是噪音(这跟 M6 的结论是同一条纪律)。
 */
import { Alert, Button } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { FallbackPlaceholder } from './FallbackPlaceholder'
import { useI18n } from '@/locales'

export interface TableErrorStateProps {
  /**
   * 'empty' = 降级占位(表里没数据了)
   * 'strip' = 提示条(表里还有旧数据)
   */
  variant: 'empty' | 'strip'
  /** 重试回调(不传就不显示重试按钮) */
  onRetry?: () => void
}

export function TableErrorState({ variant, onRetry }: TableErrorStateProps) {
  const { t } = useI18n()
  const text = t('common.loadFailed')

  if (variant === 'empty') {
    return <FallbackPlaceholder variant="network" text={text} onRetry={onRetry} height={160} />
  }

  return (
    <Alert
      type="warning"
      showIcon
      message={text}
      action={
        onRetry ? (
          <Button size="small" icon={<ReloadOutlined />} onClick={onRetry}>
            {t('common.retry')}
          </Button>
        ) : undefined
      }
      style={{ marginBottom: 12, flexShrink: 0 }}
    />
  )
}

export default TableErrorState
