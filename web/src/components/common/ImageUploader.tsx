/**
 * ImageUploader —— 图片上传(服务端中转 / 超过阈值自动切分片)
 *
 * ⚠️ **onChange 交出的是文件 id,不是 URL**。这不是偷懒:地址归存储管,
 * 文件删了/换桶了地址就失效,而 id 才是稳定的引用。要显示图就现取
 * (resolveFileUrl),要长期引用就存 id。
 *
 * 组件内部用一个 id → url 的临时表把预览拼起来:上传响应里带了 url(后端
 * 在三个上传出口都签好了),所以刚传完不用再打一次请求;刷新页面后表是空的,
 * 会为当前这批 id 补拉一次。
 *
 * ⚠️ **表里可能有 `blob:` 地址,组件负责 revoke**。见下面 resolve 的注释。
 */
import type React from 'react';
import { useEffect, useRef, useState } from 'react'
import type { UploadProps } from 'antd';
import { Upload, message } from 'antd'
import { PlusOutlined, LoadingOutlined } from '@ant-design/icons'
import { uploadApi, resolveFileUrl, revokeFileUrl } from '@/api/upload'
import { UPLOAD_LIMITS } from '@/constants/app'
import { useI18n } from '@/locales'

export interface ImageUploaderProps {
  /** 文件 id 列表(单图为 string,多图为 string[]) */
  value?: string | string[]
  onChange?: (value: string | string[] | undefined) => void
  /** 多图模式 */
  multiple?: boolean
  /**
   * 分类,只影响**前端**的类型/大小校验。
   * ⚠️ 它**不是**后端路径里的业务段 —— 那是下面的 `biz`,两个是不同的东西。
   */
  bizType?: 'image' | 'video' | 'file'
  /** 后端路径的业务英文名(第二段)。不传 = 后端用调用方的默认业务段 */
  biz?: string
  /** 目标文件夹 id。不传 = 后端在该 biz 下懒建默认文件夹 */
  folderId?: string
  /** 最大数量(multiple 时) */
  maxCount?: number
  /** 自定义上传(若不传,默认走服务端中转) */
  customUpload?: UploadProps['customRequest']
  /** 已上传列表禁用 */
  disabled?: boolean
  /** 提示文案 */
  hint?: string
}

export const ImageUploader: React.FC<ImageUploaderProps> = ({
  value,
  onChange,
  multiple = false,
  bizType = 'image',
  biz,
  folderId,
  maxCount = 9,
  disabled,
  hint,
}) => {
  const { t } = useI18n()
  const [loading, setLoading] = useState(false)
  const [urlMap, setUrlMap] = useState<Record<string, string>>({})
  // ⚠️ **urlMap 的 ref 镜像**。回收 objectURL 要在 effect body 里做(不能在
  // cleanup 里,理由见下面),而 cleanup 阶段读不到最新的 state —— 直接读
  // state 拿到的是上一次渲染的快照,会把刚加进来的 id 当成"已移除"撤掉。
  const urlMapRef = useRef<Record<string, string>>({})

  const ids = normalizeValue(value)
  // ⚠️ 依赖用拼出来的字符串而不是数组本身:normalizeValue 每次渲染都新造一个
  // 数组,拿它当依赖会变成"改一次 state 触发一次 effect"的死循环。
  const idKey = ids.join(',')

  // 卸载时兜底:组件没了,ref 里剩下的 objectURL 一个都不能留。
  useEffect(
    () => () => {
      for (const u of Object.values(urlMapRef.current)) revokeFileUrl(u)
      urlMapRef.current = {}
    },
    []
  )

  useEffect(() => {
    const current = idKey ? idKey.split(',') : []
    const keep = new Set(current)

    // 1) 回收已经不在列表里的地址。
    //    ⚠️ **必须在 effect body 里做,不能塞进 cleanup** —— cleanup 会在
    //    idKey 的**任何一次**变化时触发(加一张图也会),那时还在列表里的
    //    URL 也会被 revoke 掉,表现是"加完一张图,之前所有图都裂了"。
    const prev = urlMapRef.current
    let dropped = false
    const retained: Record<string, string> = {}
    for (const [id, url] of Object.entries(prev)) {
      if (keep.has(id)) retained[id] = url
      else {
        dropped = true
        revokeFileUrl(url)
      }
    }
    if (dropped) {
      urlMapRef.current = retained
      setUrlMap(retained)
    }

    // 2) 补拉缺的
    const missing = current.filter((id) => !urlMapRef.current[id])
    if (missing.length === 0) return
    let alive = true
    Promise.all(
      missing.map((id) =>
        // ⚠️ 用 resolveFileUrl 而不是 getFileUrl:前者拿不到直链时会自动退到
        // blob 中转,塞进 <img src> 始终是一张能显示的图。
        resolveFileUrl(id)
          .then((url) => [id, url] as const)
          // 单个 id 取不到不该让整列表空掉:先占位,用户能重试
          .catch(() => [id, ''] as const)
      )
    ).then((pairs) => {
      if (!alive) return
      // ⚠️ 结果回来时列表可能又变了。已经被移出列表的 id 在这一步不能
      // 再写回 map —— 那样它的 objectURL 永远没人 revoke。
      const stillThere = new Set(idKey ? idKey.split(',') : [])
      const next = { ...urlMapRef.current }
      for (const [id, url] of pairs) {
        if (stillThere.has(id)) next[id] = url
        else revokeFileUrl(url)
      }
      urlMapRef.current = next
      setUrlMap(next)
    })
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [idKey])

  // 上传成功后把响应里带的 url 塞进表。拿不到时留空串 —— 下一轮 effect
  // 会用 resolveFileUrl 补上。
  const rememberUrl = (id: string, url?: string) => {
    const next = { ...urlMapRef.current, [id]: url ?? '' }
    urlMapRef.current = next
    setUrlMap(next)
  }

  const fileList = ids.map((id) => ({
    uid: id,
    name: id,
    status: 'done' as const,
    url: urlMap[id],
  }))

  const handleBeforeUpload: UploadProps['beforeUpload'] = (file) => {
    // 类型校验
    // ⚠️ 显式标成 `readonly string[]`:UPLOAD_LIMITS 是 `as const`,元素类型是
    // 字面量联合,`.includes(file.type)`(file.type 是普通 string)会推成 never。
    // 这里只放宽本地变量,不去改共享常量的类型 —— 那会影响所有读它的页面。
    const allowed: readonly string[] | undefined =
      bizType === 'image'
        ? UPLOAD_LIMITS.IMAGE_TYPES
        : bizType === 'video'
        ? UPLOAD_LIMITS.VIDEO_TYPES
        : undefined
    if (allowed && !allowed.includes(file.type)) {
      message.error(t('upload.fileTypeNotSupported', undefined, { type: file.type }))
      return Upload.LIST_IGNORE
    }
    // 大小校验
    const maxSize = bizType === 'image'
      ? UPLOAD_LIMITS.IMAGE_MAX_SIZE
      : bizType === 'video'
      ? UPLOAD_LIMITS.VIDEO_MAX_SIZE
      : UPLOAD_LIMITS.FILE_MAX_SIZE
    if (file.size > maxSize) {
      const size = (maxSize / 1024 / 1024).toFixed(0)
      message.error(t('upload.fileTooLarge', undefined, { size }))
      return Upload.LIST_IGNORE
    }
    return true
  }

  const customRequest: UploadProps['customRequest'] = async (options) => {
    const { file, onProgress, onSuccess, onError } = options
    try {
      setLoading(true)
      // 小文件走单次中转,超过阈值自动切分片 —— 判断在 uploadFileAuto 里,
      // 组件这层不重复实现一遍阈值逻辑(两处各写一个数必然会对不上)
      const view = await uploadApi.uploadFileAuto(
        file as File,
        { biz, folderId },
        (p) => onProgress?.({ percent: p })
      )
      rememberUrl(view.id, view.url)
      onSuccess?.(view)
      const next = multiple ? [...ids, view.id] : [view.id]
      onChange?.(multiple ? next : next[0])
    } catch (err) {
      onError?.(err as Error)
      message.error(t('message.uploadFailed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <Upload
      listType="picture-card"
      fileList={fileList}
      multiple={multiple}
      maxCount={multiple ? maxCount : 1}
      beforeUpload={handleBeforeUpload}
      customRequest={customRequest}
      onRemove={(f) => {
        // ⚠️ 只改表单值,**不删文件**。删除是独立的破坏性动作(会把存储里的
        // 对象和配额一起退掉),不能挂在"用户点了个 ×"上。
        const next = ids.filter((x) => x !== f.uid)
        onChange?.(multiple ? next : next[0])
      }}
      disabled={disabled}
    >
      {(multiple ? fileList.length >= maxCount : fileList.length >= 1) ? null : (
        <div>
          {loading ? <LoadingOutlined /> : <PlusOutlined />}
          <div style={{ marginTop: 8, fontSize: 12 }}>{hint || t('upload.defaultHint')}</div>
        </div>
      )}
    </Upload>
  )
}

function normalizeValue(v?: string | string[]): string[] {
  if (Array.isArray(v)) return v.filter(Boolean)
  return v ? [v] : []
}

export default ImageUploader