/**
 * 媒体「浏览器能不能播」的状态判定 —— 前端侧(M145d)
 *
 * # 这个文件解决什么
 *
 * 后端 `server/internal/pkg/media/playback.go` 已经判好了:
 *   · 这个文件浏览器**能不能直接播**(`media_reason` 空 = 能播);
 *   · 要不要转、转成什么样(`transcode_status`)。
 *
 * 前端要做的只有两件事:
 *   1. 把这个状态**显示出来**(用户上传完就该知道"这个文件浏览器播不了");
 *   2. 决定预览时用原文件还是用 `playback_url`。
 *
 * # ⚠️ **前端不再判"能不能播"**
 *
 * 前端当然可以问浏览器:`video.canPlayType('video/mp4; codecs="avc1"')`。
 * 本项目**不这么做**,理由有三条,每条都会导致"看起来对、实际错":
 *
 *   1. **canPlayType 是"这一个浏览器"的答案**。Chrome 说能播 HEVC,
 *      Safari 能、Firefox 依平台 —— 用户不会为了看一个视频去换浏览器。
 *      后端判的是"**主流浏览器都能播**"这档,两边口径不同。
 *   2. **canPlayType 不告诉你"为什么"**。它只返 "" / "maybe" / "probably",
 *      没有一句能给人看的解释。后端的 `media_reason` 是"视频编码 MPEG-4 Part 2
 *      浏览器不支持"这样一句话。
 *   3. **前端判了也没用**。真要能播,得**转码**(H.264 + AAC / MP4)——
 *      那是后端的活(ffmpeg)。前端"判出播不了"之后仍然播不了,
 *      只会多一个"这个文件有问题"的提示,没有解决方案。
 *
 * ⇒ 所以这里的 `mediaStatusOf` **只做状态映射**,不做任何编解码判断。
 *   判据表全在服务端那一张,前端只搬运。
 *
 * # ⚠️ **判据和后端必须一致**
 *
 * 后端 `model.TranscodeStatus`:0=不需要 / 1=待转 / 2=转码中 / 3=完成 / 4=失败。
 * 这里用**同名同值**的常量,改一边就要改另一边 ——
 * 所以那几个数字写成本文件里的具名常量,不在别处写裸数字。
 */

/** 转码状态 —— 与后端 model.TranscodeStatus 同值 */
export const TRANSCODE_NONE = 0
export const TRANSCODE_PENDING = 1
export const TRANSCODE_RUNNING = 2
export const TRANSCODE_DONE = 3
export const TRANSCODE_FAILED = 4

/**
 * 媒体状态种类
 *
 * · `none` —— 不是媒体,或者浏览器本来就能播(没什么可提示的);
 * · `unplayable` —— **浏览器播不了,而且没有可播副本**(原文件 + 转码都没了);
 * · `pending` / `running` —— 转码还没好;
 * · `done` —— 有可播副本了;
 * · `failed` —— 转码失败,原文件还是播不了。
 */
export type MediaStatusKind =
  | 'none'
  | 'unplayable'
  | 'pending'
  | 'running'
  | 'done'
  | 'failed'

/** 判定媒体状态需要的最小输入(只要两个字段,别整个 FileView) */
export interface MediaStatusInput {
  /** 为什么浏览器播不了(空 = 能直接播) */
  media_reason?: string
  /** 转码状态 */
  transcode_status?: number
}

/**
 * 媒体状态 —— 一个文件在"能不能播"这件事上的处境。
 *
 * ⚠️ **`unplayable` 只在"没有可播副本"时才是 unplayable** ——
 *   转码完成(3)之后文件**是能播的**(走 `playback_url`),
 *   这时候还标"浏览器不支持"就是**在骗用户**。
 *   顺序不能反:先看转码状态,再看原文件播不播得了。
 */
export function mediaStatusOf(file: MediaStatusInput | null | undefined): MediaStatusKind {
  if (!file) return 'none'
  switch (file.transcode_status) {
    case TRANSCODE_DONE:
      // 有可播副本了 —— 文件能播
      return 'done'
    case TRANSCODE_FAILED:
      // 转码失败 ⇒ 回到原文件,原文件播不播得了就是全部
      return file.media_reason ? 'failed' : 'none'
    case TRANSCODE_RUNNING:
      return 'running'
    case TRANSCODE_PENDING:
      return 'pending'
    default:
      // 没在转(0)⇒ 看原文件能不能播
      return file.media_reason ? 'unplayable' : 'none'
  }
}

/**
 * 这个文件预览时该用哪个直链。
 *
 * ⚠️ **有可播副本就用副本** —— 原文件可能压根不是浏览器能播的格式
 *   (WMV / FLV / MKV / MPEG-4 Part 2),拿原文件去 `<video src>` 就是"点了没反应"。
 *
 * ⚠️ **转码失败时用原文件** —— 副本不存在,总得让人下到原文件。
 */
export function playbackSrcOf(
  file: (MediaStatusInput & { playback_url?: string; url?: string }) | null | undefined,
): string {
  if (!file) return ''
  if (file.transcode_status === TRANSCODE_DONE && file.playback_url) return file.playback_url
  return file.url ?? ''
}

/**
 * 每个状态对应哪个 i18n 键 —— **写字面量,不用模板串拼**。
 *
 * ⚠️ 拼出来的键(`t(\`...media${x}\`)`)查不到时**原样返回那串键**,
 *   而拼错的键两边字典可能**同时没有** ⇒ locale-parity(只比两边键集合)
 *   也发现不了 ⇒ 界面上直接显示 "page.fileList.mediaUnplaable"。
 *   写字面量就能被 grep 到,也有专门的用例盯着这五个键两边都在。
 *
 * ⚠️ **这张表在本文件(纯数据),不在组件文件里** —— 组件文件再导一个
 *   非组件的常量会踩 `react-refresh/only-export-components`
 *   (组件文件里混着非组件导出,热更新会整页失效)。
 */
export const MEDIA_STATUS_LABEL_KEY: Record<Exclude<MediaStatusKind, 'none'>, string> = {
  unplayable: 'page.fileList.mediaUnplayable',
  pending: 'page.fileList.mediaTranscodePending',
  running: 'page.fileList.mediaTranscoding',
  done: 'page.fileList.mediaTranscoded',
  failed: 'page.fileList.mediaTranscodeFailed',
}
