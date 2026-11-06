/**
 * 克隆音色弹窗 —— multipart 上传参考音频 → POST /biz/voice-clones
 *
 * 单独一个文件:克隆表单里有文件上传,EditModal 的 EditField 体系
 * 覆盖不了(Upload 控件 + 校验),与其在 fields 里塞一堆 render 分支,
 * 不如一个独立的 Modal 直接用 Form 组件。
 */
import { useState } from 'react'
import type { FC } from 'react'
import { Form, Input, Modal, Select, Upload, App, Alert } from 'antd'
import type { UploadFile } from 'antd'
import { InboxOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'
import { voiceApi } from '@/api/voice'
import type { VoiceCloneForm } from '@/types/voice'

interface Props {
  open: boolean
  onClose: () => void
  onCloned: () => void
}

const CloneModal: FC<Props> = ({ open, onClose, onCloned }) => {
  const { t } = useI18n()
  const { message } = App.useApp()
  const [form] = Form.useForm<VoiceCloneForm>()
  const [fileList, setFileList] = useState<UploadFile[]>([])
  const [submitting, setSubmitting] = useState(false)

  const reset = () => {
    form.resetFields()
    setFileList([])
  }

  const handleOk = async () => {
    const vals = await form.validateFields()
    const file = fileList[0]?.originFileObj
    if (!file) {
      message.warning(t('page.voice.audioRequired'))
      return
    }
    setSubmitting(true)
    try {
      await voiceApi.cloneVoice(vals, file)
      message.success(t('page.voice.cloned'))
      reset()
      onClose()
      onCloned()
    } catch {
      // 失败提示由 axios 拦截器统一弹(含"失败也落了一条 FAILED 台账"的说明空间)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      title={t('page.voice.cloneVoice')}
      open={open}
      onCancel={() => {
        reset()
        onClose()
      }}
      onOk={handleOk}
      okText={t('common.submit')}
      cancelText={t('common.cancel')}
      confirmLoading={submitting}
      width={560}
      destroyOnHidden
    >
      <Alert
        type="info"
        showIcon
        message={t('page.voice.cloning')}
        style={{ marginBottom: 16, display: submitting ? undefined : 'none' }}
      />
      <Form<VoiceCloneForm>
        form={form}
        layout="vertical"
        initialValues={{ ttsType: 'aliyun-bailian', sex: 0, targetModel: '', prefix: '', languageHints: '' }}
      >
        <Form.Item
          name="name"
          label={t('field.name')}
          rules={[{ required: true, whitespace: true, message: t('page.voice.nameRequired') }]}
        >
          <Input />
        </Form.Item>
        <Form.Item label={t('page.voice.audio')} required>
          <Form.Item
            name="audio"
            valuePropName="fileList"
            getValueFromEvent={(e) => (Array.isArray(e) ? e : e?.fileList)}
            rules={[{ required: true, message: t('page.voice.audioRequired') }]}
            noStyle
          >
            <Upload.Dragger
              accept=".wav,.mp3,.m4a,.pcm,.aac,.flac,.ogg"
              maxCount={1}
              beforeUpload={() => false}
              fileList={fileList}
              onChange={({ fileList: fl }) => setFileList(fl)}
            >
              <p className="ant-upload-drag-icon">
                <InboxOutlined />
              </p>
              <p className="ant-upload-text">{t('page.voice.audioRequired')}</p>
            </Upload.Dragger>
          </Form.Item>
          <div style={{ marginTop: 6, color: 'var(--app-text-tertiary)', fontSize: 12 }}>
            {t('page.voice.audioHint')}
          </div>
        </Form.Item>
        <Form.Item name="gender" label={t('page.voice.gender')}>
          <Input placeholder={t('page.voice.genderPh')} />
        </Form.Item>
        <Form.Item name="sex" label={t('page.voice.sex')}>
          <Select
            options={[
              { value: 0, label: '-' },
              { value: 1, label: t('page.voice.male') },
              { value: 2, label: t('page.voice.female') },
            ]}
          />
        </Form.Item>
        <Form.Item name="ttsType" label={t('page.voice.ttsType')}>
          <Select
            options={[
              { value: 'aliyun-bailian', label: 'aliyun-bailian' },
              { value: 'bailian', label: 'bailian' },
            ]}
          />
        </Form.Item>
        <Form.Item name="targetModel" label={t('page.voice.targetModel')}>
          <Input placeholder={t('page.voice.targetModelPh')} />
        </Form.Item>
        <Form.Item name="prefix" label={t('page.voice.prefix')}>
          <Input placeholder={t('page.voice.prefixPh')} />
        </Form.Item>
        <Form.Item name="languageHints" label={t('page.voice.languageHints')}>
          <Input placeholder={t('page.voice.languageHintsPh')} />
        </Form.Item>
        <Form.Item name="description" label={t('field.description')}>
          <Input.TextArea rows={2} />
        </Form.Item>
      </Form>
    </Modal>
  )
}

export default CloneModal
