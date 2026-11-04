/**
 * 文件 / 文件夹 / 分片上传 API —— M80 文件系统
 *
 * 路径规则:后端 `/rapid-admin/v1/biz/files`、`/rapid-admin/v1/biz/folders`(见 server/internal/api/router/router.go)
 * ⚠️ 2026-10-26 修:下面这些调用以前**漏了 group 前缀**,每一个都必然 404
 *   (写 `/assets` 打出去是 `/rapid-admin/v1/assets`,后端在 `/rapid-admin/v1/biz/assets`),
 *   而且完全静默(axios 弹个 toast,CI 全绿)。现在统一成"后端完整路径
 *   去掉 baseURL";下面注释里的 `/xxx` 是**组内相对路径**,实际请求要带上
 *   上面的 group 前缀。守门网:src/api/__tests__/api-route-parity.test.ts
 *
 * ⚠️ 契约细节(改之前先读,否则一定会写出一个 undefined):
 *  1. **所有 id 都是字符串**。后端是 `json:"...,string"`(uint64 直出会被 JS
 *     的 Number 精度截断,超过 2^53 的 id 末位全变 0)。传回去也要用字符串。
 *  2. **字段名是 snake_case**,与后端 DTO 的 json tag 一一对应,前端不做改名。
 *  3. **`POST /files` 一条路由两种语义**,按 Content-Type 分流:
 *     `multipart/form-data` = 服务端中转;`application/json` = OSS 直传后登记。
 *     见 server/internal/api/handler/v1/file.go 的 Upload。
 *  4. **key 由服务端生成**。前端永远不要自己拼 key —— 拼了就能越权写别人的目录。
 *  5. **别把签名 URL 当成持久地址存**。默认 15m 过期,存进数据库就是一颗
 *     定时的 403。用 resolveFileUrl 现取(见该函数的注释)。
 *
 * 历史:这份文件之前叫 upload.ts 但打的是 M76 之前的 `/biz/uploads/token`
 * 那套占位凭证,签名函数返回字符串 'placeholder',直传从来没成功过一次;
 * 对应的两条后端路由在 M80 已经删除。现在这套是真实可用的契约。
 */
import { http, instance } from './client'

/* ====================== 类型(与后端 DTO 对齐) ====================== */

/** 上传通道。前端按它决定"直接怼 OSS"还是"POST 给后端" */
export type UploadMode = 'proxy' | 'direct'

/** 直传凭证 —— 后端 oss.UploadToken */
export interface UploadToken {
  mode: UploadMode
  /** 服务端定好的完整 key,前端不要自己拼 */
  key: string
  /** 单文件字节上限 */
  max_size: number
  /** 过期时刻(unix 秒) */
  expire_at: number
  /** 哪个 provider 签的:local / oss。排障时用来区分行为差异 */
  provider: string
  /** ---- 以下仅 direct(OSS)模式 ---- */
  host?: string
  bucket?: string
  access_key_id?: string
  policy?: string
  signature?: string
  security_token?: string
}

/** 文件视图 —— 后端 service.FileView */
export interface FileView {
  id: string
  folder_id: string
  user_id: string
  biz: string
  /** 存储里的对象 key,不是 URL */
  file_key: string
  file_name: string
  file_size: number
  content_type: string
  etag: string
  storage: number
  is_cover: boolean
  status: number
  created_at: string
  /**
   * 访问地址(**稳定直链**)。
   *
   * ⚠️ **列表里也有**(M134 起)—— 缩略图视图要把它塞进 `<img src>`,
   *   逐条调 getFileUrl 的话一页就是 N 个往返。
   *   详情和三个上传出口(中转 / 直传登记 / 分片完成)也有。
   *
   * 存储侧只有一棵树、一个桶 —— 没有"公开/私有"两套形态,所以**每个文件
   * 都有直链**。而且它是**稳定**的(不带签名、不带过期时间),可以直接塞进
   * `<img src>`。
   *
   * ⚠️ **可能为空**:签不出来(对象没了 / provider 没配挂载点)时后端留空,
   *   前端据此显示占位图标 —— 不是错误。
   */
  url?: string
  /**
   * 音视频时长(**毫秒**)。0 = 没探到。
   *
   * ⚠️ **0 不代表"零秒",代表"不知道"** —— 后端在三种情况下都返 0:
   *   不是音视频、没装 ffmpeg、探测失败。**不是错误**,前端据此不显示时长。
   *
   * ⚠️ **不要在前端按扩展名猜时长** —— 后端探出来的才是真的。
   */
  duration_ms: number
  /**
   * 视频封面直链(能直接塞进 `<img src>`)。**空 = 没有封面**。
   *
   * ⚠️ **只有视频才有** —— 音频的"封面"是图标(前端按扩展名画),
   *   后端不抽帧(抽出来的波形图对唱片封面没意义,还白占一份存储)。
   *
   * ⚠️ **后端是 `omitempty`** —— 没有封面时这个字段**整个不在**,
   *   所以类型上可选,不是空串。
   */
  cover_url?: string

  /* ====================== M145:浏览器能不能播 ====================== */

  /**
   * 原始容器名(mp4 / webm / mkv / avi / asf / flv …)。
   *
   * ⚠️ **后端是 `omitempty`** —— 非媒体文件 / 没探到时**整个字段不在**。
   */
  media_container?: string
  /** 原始视频编码(h264 / hevc / vp9 / mpeg4 …)。同上,可能整个不在。 */
  media_video_codec?: string
  /** 原始音频编码(aac / mp3 / opus / wmav2 …)。无音轨 = 整个字段不在。 */
  media_audio_codec?: string
  /**
   * 为什么浏览器播不了(一句人话)。**空 = 浏览器能直接播**。
   *
   * ⚠️ **这是后端判好的,前端不要再判一遍** ——
   *   判据在 `server/internal/pkg/media/playback.go`(容器 + 编码两张表),
   *   前端再抄一份 = 后端改一次要同步改两处,迟早对不上。
   *   前端只负责**把它显示出来**。
   */
  media_reason?: string
  /**
   * 浏览器可播副本的直链(M145 转码产物)。
   *
   * ⚠️ **空串 = 没有副本** —— 三种情况:浏览器本来就能播(不需要转)、
   *   还在转、或者转失败了。前端据此决定"用原文件还是用副本"。
   */
  playback_url?: string
  /**
   * 转码状态(后端 model.TranscodeStatus):
   * 0=不需要转 / 1=待转 / 2=转码中 / 3=已完成 / 4=失败。
   *
   * ⚠️ **0 不代表"没转过",代表"不需要转"** —— 浏览器本来就能播的文件
   *   从头到尾都是 0。
   */
  transcode_status?: number
}

/** 文件夹视图 —— 后端 service.FolderView */
export interface FolderView {
  id: string
  user_id: string
  biz: string
  name: string
  parent_id: string
  cover_key: string
  cover_file_id: string
  file_count: number
  total_size: number
  is_default: boolean
  status: number
  created_at: string
  /**
   * 完整路径,形如 "素材/图纸"(**含自身**)。
   *
   * ⚠️ **多级文件夹之后光有 name 是不够的** —— 同一个用户下可以同时存在
   *   "A/图纸" 和 "B/图纸",下拉框里只显示 "图纸" 分不清是哪个。
   *   这段路径是后端按**祖先链**算好的,前端直接拿来当 label,
   *   **不要自己拼**(拼错了就变成两套路径算法,界面上对不上)。
   *
   * ⚠️ 根级文件夹的 path 就是它自己的名字(没有前导 "/")——
   *   判"是不是根级"要看 parent_id,不要看 path。
   */
  path: string
  /** 直接子文件夹数量(不含更深层) */
  child_count: number
  /**
   * 封面直链(能直接塞进 `<img src>`)。空串 = 没封面。
   *
   * ⚠️ **是稳定直链,不是签名 URL** —— 签名 URL 过期封面就裂图。
   * ⚠️ 封面文件被删 / 存储没初始化时是**空串**,不是报错。
   */
  cover_url: string
}

/** 文件夹详情 = 视图 + 文件列表 */
export interface FolderDetail extends FolderView {
  files: FileView[]
  /** info.json 原文,是生成物,前端别依赖它的字段 */
  info_json?: string
  /**
   * 祖先链,**从根到直接上级**(不含自己)。
   *
   * ⚠️ **面包屑靠它** —— 前端只有"当前文件夹 id",要显示
   *   "素材 / 图纸 / 户型"这条链就得问后端要:分页列表里未必有祖先,
   *   前端自己拼是不可行的。
   */
  ancestors: FolderView[]
  /** 直接子文件夹 */
  children: FolderView[]
}

/**
 * 文件夹列表结果 = 通用分页 + 归属人的展示名
 *
 * ⚠️ **owner_name 是后端算好的**(real_name → nickname → username)——
 *   跨用户看别人的空间时,面包屑要显示"这是谁的空间"。
 *   空串 = 查不到(用户被删 / 名字字段全空)—— 前端退回显示 user_id。
 */
export interface FolderPageResult extends PageResult<FolderView> {
  owner_name: string
}

/** 存储配额 —— 后端 service.QuotaView */
export interface QuotaView {
  used_bytes: number
  /** 0 = 不限 */
  quota_bytes: number
  /** 显式告诉前端"不限额",别让它靠 quota_bytes==0 去猜 */
  unlimited: boolean
  remain_bytes: number
}

/**
 * 用户层 —— 后端 service.FolderUserView
 *
 * ⚠️ **这是"谁有文件夹"那一层**,不是文件夹本身。
 *   存储侧的目录形态是 `rapid-admin/{uid}/{biz}/{fid}/`,第一段就是用户 ——
 *   文件夹管理页照着这个结构分层:用户 → 该用户的文件夹 → 子文件夹。
 */
export interface FolderUserView {
  user_id: string
  username: string
  nickname: string
  real_name: string
  /**
   * 展示名 —— **后端算好的**(real_name → nickname → username)
   *
   * ⚠️ **前端别自己拼** —— 三级优先级在前端写一遍,后端再改就会出现
   *   "两边显示的名字不一样"。
   */
  display_name: string
  /** 这个用户名下的文件夹数 */
  folder_count: number
  /** 这个用户名下的文件数 */
  file_count: number
  /** 这个用户名下的文件总字节 */
  total_size: number
}

/** 用户层分页结果 —— 后端 service.FolderUserPageResult */
export interface FolderUserPageResult {
  list: FolderUserView[]
  total: number
  page: number
  size: number
  /**
   * 调用者能不能跨用户读(super_admin = true)
   *
   * ⚠️ **前端据此决定要不要显示用户层** —— 普通用户直接进自己的文件夹,
   *   多一层"用户"对他是纯噪音。
   */
  cross_user: boolean
  /**
   * 调用者自己的 id
   *
   * ⚠️ **据此判断"当前看的空间是不是我的"** —— 别人的空间只能读
   *   (后端写路径会 404,前端只是把按钮藏起来)。
   */
  current_user_id: string
}

/** 分片信息 —— 后端 service.PartInfo */
export interface PartInfo {
  part_number: number
  etag: string
  size: number
}

/** 分片初始化结果 —— 后端 service.MultipartInitResp */
export interface MultipartInitResp {
  session_id: string
  key: string
  /**
   * 实际生效的分片大小。
   * ⚠️ **必须按这个值切,不能按自己请求里带的值切** —— 服务端可能因为
   * "片数会超过 10000 片"把片长抬大,按错值切合并出来的内容顺序会全乱。
   */
  part_size: number
  part_num: number
  expires_at: number
}

export interface PageResult<T> {
  list: T[]
  total: number
  page: number
  size: number
}

/* ====================== 上传参数 ====================== */

export interface UploadOptions {
  /** 业务英文名(路径第二段)。**不传用后端默认业务段** */
  biz?: string
  /** 目标文件夹 id。0/不传 = 后端在该 biz 下懒建默认文件夹 */
  folderId?: string
  /** 原始文件名。默认取 `file.name` */
  fileName?: string
  /** 超过这个大小自动走分片。默认 100MB(与后端 multipart_threshold_mb 对齐) */
  multipartThreshold?: number
  /** 分片并发数,默认 3 */
  concurrency?: number
}

export type ProgressFn = (percent: number) => void

const DEFAULT_MULTIPART_THRESHOLD = 100 * 1024 * 1024
/** 单分片上限 = 后端 BodyLimit 给 /files/* 配的 20MB。调大要先改后端 */
const DEFAULT_PART_SIZE = 8 * 1024 * 1024

/* ====================== 直传凭证 ====================== */

/** 申请直传凭证。local 模式下返回 `mode: 'proxy'`(应该改走 uploadFile) */
export function getUploadToken(params: {
  biz?: string
  folderId?: string
  fileName: string
  contentType?: string
  fileSize?: number
}) {
  return http.post<UploadToken>('/biz/files/upload-token', {
    biz: params.biz ?? '',
    folder_id: params.folderId ?? '0',
    file_name: params.fileName,
    content_type: params.contentType ?? '',
    file_size: params.fileSize ?? 0,
  })
}

/* ====================== 单次上传(服务端中转) ====================== */

/**
 * 单文件上传 —— multipart POST 给后端,后端写存储 + 记账 + 返回带 URL 的 FileView。
 *
 * ⚠️ **不要手写 `Content-Type: multipart/form-data`**。少了 axios 自动补的
 * boundary,后端 `c.FormFile("file")` 直接解析失败,而报错是"缺少文件字段 file"
 * —— 看着像字段名写错了,实际是 boundary 丢了。
 */
export function uploadFile(
  file: File,
  opts: UploadOptions = {},
  onProgress?: ProgressFn
): Promise<FileView> {
  const form = new FormData()
  form.append('file', file, opts.fileName || file.name)
  if (opts.biz) form.append('biz', opts.biz)
  if (opts.folderId && opts.folderId !== '0') form.append('folder_id', opts.folderId)

  return http.post<FileView>('/biz/files', form, {
    timeout: 120000,
    onUploadProgress: (e) => {
      if (onProgress && e.total) onProgress(Math.round((e.loaded / e.total) * 100))
    },
  })
}

/* ====================== OSS 直传 ====================== */

/**
 * 直传 OSS:把后端签好的 policy 原样 POST 给 OSS。
 *
 * ⚠️ **policy 和 signature 一律用后端下发的,前端不许自己签**。前端拿不到
 * Secret,自己"签"出来的签名 OSS 一定拒。之前这个文件里的 buildSignature()
 * 返回字符串 'placeholder',等于直传从来没通过过。
 *
 * 用之前先看 token.mode:'proxy' 时不该调这个(后端中转)。
 */
export function directUploadToOss(
  file: File,
  token: UploadToken,
  onProgress?: ProgressFn
): Promise<{ key: string }> {
  if (token.mode !== 'direct') {
    return Promise.reject(new Error('当前存储是服务端中转模式,应改用 uploadFile'))
  }
  const { host, access_key_id: akid, policy, signature, security_token: sts } = token
  // ⚠️ 这里**必须解构出局部变量**再判空:直接 `token.access_key_id` 在
  // Promise 回调里访问,TS 收不到外面 if 的收窄结果(optIONAL 属性穿过闭包
  // 就丢类型),报错还是"可能 undefined"。
  if (!host || !akid || !policy || !signature) {
    return Promise.reject(new Error('直传凭证字段缺失(后端未下发 policy/signature)'))
  }
  return new Promise((resolve, reject) => {
    const form = new FormData()
    form.append('key', token.key)
    form.append('OSSAccessKeyId', akid)
    form.append('policy', policy)
    form.append('Signature', signature)
    if (sts) form.append('x-oss-security-token', sts)
    form.append('file', file)

    const xhr = new XMLHttpRequest()
    xhr.open('POST', host, true)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) {
        onProgress(Math.round((e.loaded / e.total) * 100))
      }
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve({ key: token.key })
      } else {
        reject(new Error(`OSS 直传失败: ${xhr.status} ${xhr.responseText?.slice(0, 200) || xhr.statusText}`))
      }
    }
    xhr.onerror = () => reject(new Error('OSS 直传网络错误'))
    xhr.send(form)
  })
}

/**
 * 直传后登记元数据。
 *
 * ⚠️ **大小由后端从存储里 Stat 出来,不采信客户端**。所以这里不用(也不该)
 * 传 file_size —— 传了也是被忽略的。
 */
export function registerDirectUpload(params: {
  key: string
  folderId?: string
  fileName?: string
  contentType?: string
}) {
  return http.post<FileView>('/biz/files', {
    key: params.key,
    folder_id: params.folderId ?? '0',
    file_name: params.fileName ?? '',
    content_type: params.contentType ?? '',
  })
}

/* ====================== 分片上传 ====================== */

/** 初始化分片会话。⚠️ 之后**一律按 resp.part_size 切**,别用请求里带的值 */
export function initMultipart(params: {
  biz?: string
  folderId?: string
  fileName: string
  fileSize: number
  contentType?: string
  partSize?: number
}) {
  return http.post<MultipartInitResp>('/biz/files/mp/init', {
    biz: params.biz ?? '',
    folder_id: params.folderId ?? '0',
    file_name: params.fileName,
    file_size: params.fileSize,
    content_type: params.contentType ?? '',
    part_size: params.partSize ?? 0,
  })
}

/**
 * 上传一个分片。
 *
 * ⚠️ **body 是裸字节,不套 multipart** —— 后端这条路每个文件要打几十上百次,
 * 每次多编一层 multipart 只是白烧 CPU 和内存。session / part number 走 header。
 */
export function uploadPart(sessionId: string, partNumber: number, chunk: Blob): Promise<PartInfo> {
  return http.post<PartInfo>('/biz/files/mp/part', chunk, {
    headers: {
      'X-Session-Id': sessionId,
      'X-Part-Number': String(partNumber),
      'Content-Type': 'application/octet-stream',
    },
    timeout: 120000,
  })
}

/** 查已传分片 —— 断点续传 / 失败重试的依据 */
export function getUploadedParts(sessionId: string): Promise<PartInfo[]> {
  return http.get<PartInfo[]>('/biz/files/mp/parts', undefined, {
    headers: { 'X-Session-Id': sessionId },
  })
}

/** ⚠️ **不带分片清单**。后端一律从存储取权威清单,客户端上报的清单是事故源 */
export function completeMultipart(sessionId: string): Promise<FileView> {
  return http.post<FileView>('/biz/files/mp/complete', { session_id: sessionId }, { timeout: 120000 })
}

export function cancelMultipart(sessionId: string): Promise<void> {
  return http.delete<void>(`/biz/files/mp/${sessionId}`)
}

/**
 * 大文件整体上传:init → 按 part_size 切 → 逐片传 → complete。
 *
 * 超过 opts.multipartThreshold(默认 100MB)自动切分片,否则走单次中转。
 * 传片并发数默认 3 —— 再高对本仓的服务端没有收益(每个分片都要写锁 + 记账),
 * 只会把带宽和浏览器连接数一起打满。
 *
 * ⚠️ 失败时**不自动 cancel 会话**:会话 TTL(默认 24h)到期后由清理任务回收,
 * 留着它是为了让用户能续传。调用方想要立即释放就自己 catch 后调 cancelMultipart。
 */
export async function uploadFileAuto(
  file: File,
  opts: UploadOptions = {},
  onProgress?: ProgressFn
): Promise<FileView> {
  const threshold = opts.multipartThreshold ?? DEFAULT_MULTIPART_THRESHOLD
  if (file.size <= threshold) {
    return uploadFile(file, opts, onProgress)
  }
  return uploadMultipart(file, opts, onProgress)
}

export async function uploadMultipart(
  file: File,
  opts: UploadOptions = {},
  onProgress?: ProgressFn
): Promise<FileView> {
  const init = await initMultipart({
    biz: opts.biz,
    folderId: opts.folderId,
    fileName: opts.fileName || file.name,
    fileSize: file.size,
    contentType: file.type,
    partSize: DEFAULT_PART_SIZE,
  })
  const partSize = init.part_size || DEFAULT_PART_SIZE
  const total = Math.max(1, Math.ceil(file.size / partSize))
  const concurrency = Math.max(1, opts.concurrency ?? 3)

  let done = 0
  const bump = () => {
    done += 1
    if (onProgress) onProgress(Math.min(99, Math.round((done / total) * 100)))
  }

  // 固定数量的 worker 各自顺序取下一个片号 —— 比"每片一个 Promise.all"
  // 稳:后者在 1000 片时会一次开 1000 个请求,浏览器连接队列直接排死。
  let next = 1
  const worker = async () => {
    for (;;) {
      const idx = next++
      if (idx > total) return
      await uploadPart(init.session_id, idx, file.slice((idx - 1) * partSize, idx * partSize))
      bump()
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, total) }, worker))

  const view = await completeMultipart(init.session_id)
  onProgress?.(100)
  return view
}

/* ====================== 文件列表 / 详情 / 删除 ====================== */

export interface FileListQuery {
  folderId?: string
  biz?: string
  status?: number
  keyword?: string
  page?: number
  size?: number
}

export function listFiles(q: FileListQuery = {}): Promise<PageResult<FileView>> {
  return http.get<PageResult<FileView>>('/biz/files', {
    folder_id: q.folderId,
    biz: q.biz,
    status: q.status,
    keyword: q.keyword,
    page: q.page,
    size: q.size,
  } as Record<string, unknown>)
}

export function getFileDetail(id: string): Promise<FileView> {
  return http.get<FileView>(`/biz/files/${id}`)
}

/**
 * 取访问地址。ttl 单位**分钟**,0/不传 = 后端默认(15m)。
 *
 * 传超过上限的值不会报错,后端会夹到 max_download_ttl。
 *
 * ⚠️ `opts.silent` = 失败**不弹 toast**(401 的处理照旧,那是会话失效,
 *   跟这条无关)。`resolveFileUrl` 走 fallback 时会开 —— 那条路上
 *   "拿不到直链"是**预期内**的结论,弹个红条只会误导用户。
 */
export function getFileUrl(
  id: string,
  ttlMinutes?: number,
  opts?: { silent?: boolean },
): Promise<{ url: string }> {
  return http.get<{ url: string }>(`/biz/files/${id}/url`, { ttl: ttlMinutes }, opts)
}

/**
 * 取一个**能直接塞进 `<img src>`** 的地址。
 *
 * ⚠️ **单桶单树模型下,每个文件都有直链**。`/files/:id/url` 是主路:
 * local 是 `/rapid-admin/...` 同源路径(静态挂载整棵树),OSS 是
 * `https://{bucket}.{endpoint}/{key}`。两种情况下浏览器都自己去拉,
 * 不占应用服务器的带宽。
 *
 * ⚠️ **返回的可能是 `blob:` 开头的地址,调用方负责 revoke**。
 * 见 `revokeFileUrl`。直链不用 revoke(它是个普通 URL,不是本地资源)。
 *
 * ⚠️ 失败时 reject 而不是返回空串 —— 空串塞进 `<img src>` 表现是
 * "图片裂了但没有错误信息",调用方无从判断该不该重试。
 *
 * ⚠️ **blob 中转只是兜底,不是主路**。直链拿不到时(静态挂载没配 /
 * 后端还是旧二进制)退到 `/files/:id/content` —— 而那条路**必须带
 * Authorization 头**,`<img src>` 带不了,只能 axios 拉成 blob 再
 * createObjectURL。
 *
 *   所以这里把 `/url` 的失败**吞掉当成"没有 url"**,而不是让它冒泡。
 *   ⚠️ 吞的只是**"这条路由给不出直链"**,不是所有失败 —— `/content` 那条路
 *   仍然会把真故障(文件已删 / 401)原样抛出去,调用方照样拿得到错误。
 */
export async function resolveFileUrl(id: string, ttlMinutes?: number): Promise<string> {
  let url = ''
  try {
    const r = await getFileUrl(id, ttlMinutes, { silent: true })
    url = r?.url ?? ''
  } catch {
    // ② 旧版后端:拿不到直链不是故障,交给下面的 `/content` 中转兜底。
    url = ''
  }
  if (url) return url
  return fetchFileContentUrl(id)
}

/** 走中转拉字节,返回 objectURL */
export async function fetchFileContentUrl(id: string): Promise<string> {
  // ⚠️ **不能用 http.get** —— 它把响应体当 ApiResponse 解包,而这里是二进制。
  // 直接用 instance 才能拿到原始 Blob。响应拦截器认得 blob(没有 code 字段,
  // 走"兼容非标准响应"那条),所以鉴权 / 401 刷新依然生效。
  const resp = await instance.get<Blob>(`/files/${id}/content`, {
    responseType: 'blob',
  })
  return URL.createObjectURL(new Blob([resp.data]))
}

/** revoke 掉 resolveFileUrl 造出来的 objectURL。普通 URL 传进来是无害的空操作 */
export function revokeFileUrl(url: string | undefined): void {
  if (url && url.startsWith('blob:')) URL.revokeObjectURL(url)
}

/** 这个地址是不是本地 objectURL(需要 revoke) */
export function isObjectUrl(url: string | undefined): boolean {
  return !!url && url.startsWith('blob:')
}

export function deleteFile(id: string): Promise<void> {
  return http.delete<void>(`/biz/files/${id}`)
}

/** blob 下载后延迟 revoke 的时长(ms)。给足浏览器读完的时间 */
const REVOKE_DELAY_MS = 30_000

/**
 * 下载一个文件到本地 —— 列表页的「下载」按钮走这里。
 *
 * ⚠️ **两条路,判据是 URL 同不同源**,不是"能不能拿到 URL":
 *
 *  1. 拿到**同源** URL(local 是 `/rapid-admin/...` 同源路径):直接 `<a href download>`。
 *     同源时浏览器的 `download` 属性**生效**,文件名和"是下载而不是打开"
 *     都能保证。
 *  2. OSS 直链是**跨源**的:不能挂 `download` —— 浏览器会忽略它,`<a>`
 *     退化成"导航过去",于是 zip / docx 变成"在标签页里打开"而不是下载。
 *     跨源走 `target=_blank`,剩下的交给服务端响应头决定(OSS 直链一般会
 *     inline 预览,这正是我们要的)。
 *
 * ⚠️ **blob 分支必须延迟 revoke**:立即 revoke 会让点击瞬间就失效,
 * 症状是"点了没反应"(没有任何报错)。30s 足够读完,也不会攒太多。
 */
export async function downloadFile(id: string, fileName: string): Promise<void> {
  const { url } = await getFileUrl(id)
  const click = (href: string, opts: { download?: string; blank?: boolean }) => {
    const a = document.createElement('a')
    a.href = href
    if (opts.download) a.download = opts.download
    if (opts.blank) {
      a.target = '_blank'
      a.rel = 'noreferrer'
    }
    // Firefox 要求 anchor 在 DOM 里才响应 click
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  if (url && url.startsWith('/')) {
    click(url, { download: fileName })
    return
  }
  if (url) {
    click(url, { blank: true })
    return
  }

  const resp = await instance.get<Blob>(`/files/${id}/content`, { responseType: 'blob' })
  const blobUrl = URL.createObjectURL(resp.data)
  click(blobUrl, { download: fileName })
  setTimeout(() => URL.revokeObjectURL(blobUrl), REVOKE_DELAY_MS)
}

export function getFileQuota(): Promise<QuotaView> {
  return http.get<QuotaView>('/biz/files/quota')
}

/* ====================== 文件夹 ====================== */

export function listFolders(
  q: {
    biz?: string
    keyword?: string
    parentId?: string
    /** 看哪个用户的空间;不传 = 自己的(只有超管能传别人的) */
    userId?: string
    page?: number
    size?: number
  } = {},
) {
  return http.get<FolderPageResult>('/biz/folders', {
    biz: q.biz,
    // ⚠️ keyword 是后端 FolderListQuery.Keyword 的 wire 名 ——
    //   后端接不住这个参数的话,搜索框"能打字、没反应"。
    keyword: q.keyword,
    /**
     * ⚠️ **parent_id 是三态,别用空串代替"不传"**:
     *   · 不传(undefined) = **不限层级**(平铺列全部)—— 老调用方(下拉框)靠这个;
     *   · "0" = **只列根级**;
     *   · 某个 id = **只列它的直接子级**。
     *
     *   axios 会把 undefined 的参数整个丢掉(不写进 query string),
     *   所以 undefined = 后端收到"没传" —— 这正是我们要的。
     *   ⚠️ 传 "0" 时**必须是字符串**:后端 DTO 是 `json:"parent_id,string"`,
     *   传数字会被 Go 的 encoding/json 拒掉("invalid use of ,string struct tag")。
     */
    parent_id: q.parentId,
    /**
     * ⚠️ **user_id 不传 = 列调用方自己的**(后端三态,和 parent_id 一个套路)。
     *   只有超管能传别人的 id —— 后端对普通用户返 403。
     *
     *   axios 会把 undefined 的参数整个丢掉 ⇒ undefined = 后端收到"没传"。
     */
    user_id: q.userId,
    page: q.page,
    size: q.size,
  } as Record<string, unknown>)
}

/**
 * 用户层:列出"有文件夹的用户"
 *
 * ⚠️ **普通用户只会拿到自己那一行**(判定在后端)—— 前端不自己猜角色,
 *   前端那份 role code 和后端对不上过(见 M103 审计)。
 */
export function listFolderUsers(q: { keyword?: string; page?: number; size?: number } = {}) {
  return http.get<FolderUserPageResult>('/biz/folders/users', {
    keyword: q.keyword,
    page: q.page,
    size: q.size,
  })
}

/**
 * 返回新建文件夹的 id(字符串)
 *
 * ⚠️ **parentId 不传 = 建在根级**;建子文件夹时传上级 id。
 *   建子文件夹**不要传 biz** —— 后端会继承上级的业务段
 *   (传了不一致的 biz 后端会直接报错)。
 */
export function createFolder(params: {
  biz?: string
  name: string
  parentId?: string
}): Promise<{ id: string }> {
  return http.post<{ id: string }>('/biz/folders', {
    biz: params.biz ?? '',
    name: params.name,
    // ⚠️ 必须是字符串("0" = 根级)—— 后端 DTO 是 `json:"parent_id,string"`
    parent_id: params.parentId ?? '0',
  })
}

export function getFolder(id: string): Promise<FolderDetail> {
  return http.get<FolderDetail>(`/biz/folders/${id}`)
}

/** ⚠️ **连文件夹里的文件一起删**,不可恢复 */
export function deleteFolder(id: string): Promise<void> {
  return http.delete<void>(`/biz/folders/${id}`)
}

/** 把文件夹里的某个文件设为封面(封面也是按"文件夹一张"存的,不是每文件一张) */
export function setFolderCover(folderId: string, fileId: string): Promise<FolderView> {
  return http.post<FolderView>(`/biz/folders/${folderId}/cover`, { file_id: fileId })
}

/* ====================== 命名空间导出(给业务侧用) ====================== */

export const uploadApi = {
  getUploadToken,
  uploadFile,
  uploadFileAuto,
  uploadMultipart,
  directUploadToOss,
  registerDirectUpload,
  initMultipart,
  uploadPart,
  getUploadedParts,
  completeMultipart,
  cancelMultipart,
  listFiles,
  getFileDetail,
  getFileUrl,
  resolveFileUrl,
  revokeFileUrl,
  downloadFile,
  deleteFile,
  getFileQuota,
  listFolders,
  listFolderUsers,
  createFolder,
  getFolder,
  deleteFolder,
  setFolderCover,
}