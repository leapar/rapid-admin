/**
 * 媒体「浏览器能不能播」徽标 —— M145d
 *
 * # 为什么要这个徽标
 *
 * 后端判完"这个文件浏览器播不了"之后,前端**必须把它说出来** ——
 * 用户上传完一个 WMV,列表里跟别的文件长得一模一样,点开才发现播不了,
 * 那就是"平台上传视频有问题"的观感。**没有报错、没有提示**,最坏的一种状态。
 *
 * # ⚠️ **表格视图与缩略图视图必须共用这一个**
 *
 * 同一个状态在两个视图里显示成两个样子,是"同一件事两套说法"的开始 ——
 * 改了一处忘了另一处,用户会以为它们说的是不同的东西。
 *
 * # ⚠️ **判据不在前端**
 *
 * 这里只**搬运**后端判好的结果(见 `mediaStatus.ts` 的说明):
 * 容器/编码两张表在 `server/internal/pkg/media/playback.go`,
 * 前端再抄一份 = 后端改一次要同步改两处。
 */
import type React from 'react'
import { Tag, Tooltip } from 'antd'
import { useI18n } from '@/locales'
import { mediaStatusOf, MEDIA_STATUS_LABEL_KEY, type MediaStatusKind } from './mediaStatus'

/** 徽标需要的最小输入 */
export interface MediaStatusTagProps {
  media_reason?: string
  media_container?: string
  media_video_codec?: string
  media_audio_codec?: string
  transcode_status?: number
}

/** 每个状态对应哪个颜色 —— 集中在这一处,别散在 JSX 里 */
const TAG_COLOR: Record<Exclude<MediaStatusKind, 'none'>, string> = {
  // ⚠️ 橙色 = "这东西有问题,但有转码在救" —— 不是红色(红色会让人以为文件坏了)
  unplayable: 'warning',
  pending: 'default',
  running: 'processing',
  done: 'success',
  failed: 'error',
}

/**
 * 每个状态对应哪个 i18n 键 —— **写字面量,不用模板串拼**。
 *
 * ⚠️ 拼出来的键(`t(\`...media${x}\`)`)查不到时**原样返回那串键**,
 *   而拼错的键两边字典可能**同时没有** ⇒ locale-parity(只比两边键集合)
 *   也发现不了 ⇒ 界面上直接显示 "page.fileList.mediaUnplaable"。
 *   写字面量就能被 grep 到,也有专门的用例盯着这五个键两边都在。
 *
 * ⚠️ **这张表在 `mediaStatus.ts` 里,不在这儿** —— 本文件导出了组件,
 *   再导一个常量会踩 `react-refresh/only-export-components`
 *   (组件文件里混着非组件导出,热更新会整页失效)。
 */
export const MediaStatusTag: React.FC<MediaStatusTagProps> = (props) => {
  const { t } = useI18n()
  const kind = mediaStatusOf(props)
  if (kind === 'none') return null

  const label = t(MEDIA_STATUS_LABEL_KEY[kind])

  /**
   * 悬浮说明
   *
   * ⚠️ **unplayable 悬浮显示后端给的那句话** ——
   *   `media_reason` 是后端拼的"视频编码 MPEG-4 Part 2 浏览器不支持",
   *   前端自己再拼一句 = 两份说法。
   */
  const tip =
    kind === 'unplayable'
      ? (props.media_reason ?? label)
      : kind === 'done'
        ? `${label}(原始:${describeOriginal(props)})`
        : label

  return (
    <Tooltip title={tip}>
      <Tag
        color={TAG_COLOR[kind]}
        style={{ marginInlineEnd: 0 }}
        data-testid={`file-media-status-${kind}`}
      >
        {label}
      </Tag>
    </Tooltip>
  )
}

/** 原始容器 / 编码,拼成一句人能看懂的 */
function describeOriginal(p: MediaStatusTagProps): string {
  const parts: string[] = []
  if (p.media_container) parts.push(p.media_container)
  if (p.media_video_codec) parts.push(p.media_video_codec)
  if (p.media_audio_codec) parts.push(p.media_audio_codec)
  return parts.join(' · ') || '-'
}
