/**
 * 附件字段的控件 —— 上传 / 显示 / 清除
 *
 * ⚠️ **字段里存的是文件的 key(不带域名的路径)**,不是 URL ——
 *   URL 里带域名(本地部署是 `/rapid-admin/...`,CDN 部署是 `https://cdn.../...`),
 *   域名会变 ⇒ 存 URL 等于把"换 CDN"变成一次数据迁移。
 *   存 key 是**两种 provider 下形状一致**的那一个
 *   (local 的直链 = `/` + key,OSS 的直链 = `<CDN 前缀>` + `/` + key)。
 *   权威:docs/业务表单设计器设计方案V1.0.md §2.9。
 *
 * ⚠️ **上传必须走文件服务,不许让用户手填路径** ——
 *   手填就是让用户自己声明"这个文件存在",后端没法保证它真的存在
 *   (后端只做形状校验 + 存在性校验,见 dynx.normalizeFile)。
 *
 * ⚠️ **刚上传的文件还没有展示信息** ——
 *   `_files` 是后端读**记录**时解析出来的,新传的文件不在里面。
 *   ⇒ 组件自己记一份"刚上传的那一份",显示时优先用它。
 *
 * ⚠️ **这是个受控组件** —— 上传完调 `onChange(key)` 之后,
 *   界面上要等**宿主把新值传回来**才会变(宿主的 value 不变 ⇒ 界面不变)。
 *   真实页面里那个宿主是 EditModal 的表单(表单值变了 → 重新渲染这个控件)。
 *
 * ⚠️ **上传按钮按 `file:upload` 权限码控可见性** ——
 *   上传走 M80 的 `POST /biz/files`,那个接口要 `file:upload`。
 *   没有它就 403 ⇒ 与其让人点了吃一个红 toast,不如不渲染(同文件列表页口径)。
 *   ⚠️ **但已有的文件名照样显示** —— 读是另一回事,不给上传权不等于不给看。
 */
import { useState } from 'react'
import type { FC } from 'react'
import { App, Button, Space, Typography, Upload } from 'antd'
import { DeleteOutlined, FileOutlined, UploadOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'
import { uploadFile } from '@/api/upload'
import { usePermissionStore } from '@/stores/permission'
import { PermFile } from '@/constants/permissions'
import type { DynFileRef } from '@/types/dyn'

const { Text } = Typography

/** 附件的 biz 段 —— 文件系统按它分文件夹 */
const DYN_FILE_BIZ = 'dyn'

export interface FileFieldControlProps {
  /** 当前值(文件 key)。空串 = 没传 */
  value?: string
  /** 后端解析出来的展示信息(原文件名 + 直链) */
  file?: DynFileRef
  /** 变更回调(传新的 key;传空串 = 清除) */
  onChange?: (v: string) => void
}

export const FileFieldControl: FC<FileFieldControlProps> = ({ value, file, onChange }) => {
  const { t } = useI18n()
  const { message } = App.useApp()
  /**
   * ⚠️ **必须订阅,不能用 `usePermissionStore.getState()`** ——
   *   getState 是一次性读取,不订阅变化 ⇒ 权限刷新后按钮不会跟着变
   *   (M102 同款坑:三处 getState 导致"权限实时刷新"只刷新数据不刷新界面)。
   */
  const canUpload = usePermissionStore((s) => s.has(PermFile.UPLOAD))
  const [uploading, setUploading] = useState(false)
  /**
   * ⚠️ **只记"刚上传的那一份"** ——
   *   `file` 是后端从**这条记录**解析出来的,新传的文件它当然没有。
   *   而 `value` 一变(用户又传了一个),这份就必须作废 ⇒ 用 key 对齐。
   */
  const [fresh, setFresh] = useState<{ key: string; ref: DynFileRef } | null>(null)

  const key = typeof value === 'string' ? value.trim() : ''
  const shown: DynFileRef | undefined = fresh && fresh.key === key ? fresh.ref : file
  const displayName = shown?.name || key

  const pick = async (fileObj: File) => {
    setUploading(true)
    try {
      const res = await uploadFile(fileObj, { biz: DYN_FILE_BIZ })
      const ref: DynFileRef = { name: res.file_name, url: res.url ?? '' }
      setFresh({ key: res.file_key, ref })
      onChange?.(res.file_key)
    } catch {
      message.error(t('page.dyn.records.file.uploadFailed'))
    } finally {
      setUploading(false)
    }
  }

  return (
    <Space direction="vertical" size={4} style={{ width: '100%' }}>
      <Space size={8} wrap>
        {canUpload ? (
          <Upload
            showUploadList={false}
            // ⚠️ **返回 false = 不让 antd 自己上传** ——
            //   我们要走 uploadFile(它带 biz / folder_id / 记账口径),
            //   antd 默认的 action 是它自己的 xhr,不带这些。
            beforeUpload={(f) => {
              void pick(f)
              return false
            }}
          >
            <Button size="small" icon={<UploadOutlined />} loading={uploading}>
              {key ? t('page.dyn.records.file.reupload') : t('page.dyn.records.file.upload')}
            </Button>
          </Upload>
        ) : (
          // ⚠️ **按钮没了要说一句为什么** —— 否则用户以为控件坏了。
          //   (读是另一回事:下面那个文件名照样显示。)
          <Text type="secondary" style={{ fontSize: 12 }}>
            {t('page.dyn.records.file.noUploadPerm')}
          </Text>
        )}
        {key ? (
          <Button
            size="small"
            type="text"
            danger
            icon={<DeleteOutlined />}
            onClick={() => {
              setFresh(null)
              onChange?.('')
            }}
          >
            {t('page.dyn.records.file.clear')}
          </Button>
        ) : null}
      </Space>

      {key ? (
        <Space size={6}>
          <FileOutlined />
          {shown?.url ? (
            <a href={shown.url} target="_blank" rel="noreferrer">
              {displayName}
            </a>
          ) : (
            <Text type="warning">{displayName}</Text>
          )}
          {!shown ? (
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t('page.dyn.records.file.missing')}
            </Text>
          ) : null}
        </Space>
      ) : null}
    </Space>
  )
}

export default FileFieldControl
