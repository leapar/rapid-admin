/**
 * 文本合成面板 —— 选音色 + 写文本 → 整段试听 / 下载 / 流式试听
 *
 * # 三条纪律
 *
 *   1. **合成走 /tts/convert**(权限码 tts:convert),voice 传登记册的
 *      **本地 id**(纯数字)—— 后端 resolveVoice 会查登记册换出真正的
 *      voice_id 并定通道;前端不用猜 edge 还是 bailian。
 *   2. **edge 音色禁用 wav** —— Edge 通道只出 mp3,后端对 format=wav
 *      显式报错;前端直接把选项禁掉,别让用户撞到 400 才知道。
 *   3. **流式的意义是边合成边收** —— 这里逐块计数字节数(进度可见),
 *      收完再播;想真·边收边播的集成方拿 convertStreamRaw 的原始
 *      Response 自己接 MediaSource,不在页面里做。
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import type { FC } from 'react'
import { Alert, App, Button, Card, Input, InputNumber, Radio, Select, Space } from 'antd'
import { DownloadOutlined, PlayCircleOutlined, SoundOutlined } from '@ant-design/icons'
import { useI18n } from '@/locales'
import { voiceApi, convertOnce, convertStream } from '@/api/voice'
import { PermVoice } from '@/constants/permissions'
import { usePermission } from '@/hooks/usePermission'
import type { TtsVoice } from '@/types/voice'

const PAGE_SIZE = 200 // 一次拉全量音色进下拉(seed 内置两百来条,一页够)

const SynthPanel: FC<{ active: boolean }> = ({ active }) => {
  const { t } = useI18n()
  const { message } = App.useApp()
  const { has } = usePermission()
  const canConvert = has(PermVoice.CONVERT)

  const [voices, setVoices] = useState<TtsVoice[]>([])
  const [voiceId, setVoiceId] = useState<string | undefined>(undefined)
  const [text, setText] = useState('')
  const [format, setFormat] = useState<'wav' | 'mp3'>('mp3')
  const [provider, setProvider] = useState<'' | 'bailian' | 'edge'>('')
  const [volume, setVolume] = useState(0)
  const [rate, setRate] = useState(0)
  const [busy, setBusy] = useState<'once' | 'stream' | 'download' | null>(null)
  const [streamBytes, setStreamBytes] = useState(0)
  const audioRef = useRef<HTMLAudioElement | null>(null)

  const selected = voices.find((v) => v.id === voiceId)
  const isEdge = provider === 'edge' || (provider === '' && selected?.tts_type === 'edge')

  useEffect(() => {
    if (active && voices.length === 0) {
      voiceApi
        .listVoices({ page: 1, size: PAGE_SIZE })
        .then((res) => setVoices(res.list))
        .catch(() => setVoices([])) // 拦截器弹错
    }
  }, [active, voices.length])

  useEffect(() => {
    return () => {
      audioRef.current?.pause()
      if (audioRef.current) URL.revokeObjectURL(audioRef.current.src)
    }
  }, [])

  const playBlob = useCallback((blob: Blob) => {
    audioRef.current?.pause()
    if (audioRef.current) URL.revokeObjectURL(audioRef.current.src)
    const url = URL.createObjectURL(blob)
    const audio = new Audio(url)
    audioRef.current = audio
    void audio.play()
  }, [])

  const buildParams = useCallback(() => {
    if (!voiceId) {
      message.warning(t('page.voice.selectVoiceFirst'))
      return null
    }
    if (!text.trim()) {
      message.warning(t('page.voice.textRequired'))
      return null
    }
    return {
      text: text.trim(),
      voice: voiceId,
      ...(provider ? { provider } : {}),
      format: isEdge ? 'mp3' : format,
      volume: volume === 0 ? '+0%' : `${volume > 0 ? '+' : ''}${volume}%`,
      rate: rate === 0 ? '+0%' : `${rate > 0 ? '+' : ''}${rate}%`,
    }
  }, [voiceId, text, provider, format, volume, rate, isEdge, message, t])

  const run = async (kind: 'once' | 'stream' | 'download') => {
    const params = buildParams()
    if (!params) return
    setBusy(kind)
    setStreamBytes(0)
    try {
      let blob: Blob
      if (kind === 'stream') {
        blob = await convertStream(params, (_chunk, received) => setStreamBytes(received))
      } else {
        blob = await convertOnce(params)
      }
      if (kind === 'download') {
        const ext = params.format === 'wav' ? 'wav' : 'mp3'
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `tts-${Date.now()}.${ext}`
        a.click()
        URL.revokeObjectURL(url)
      } else {
        playBlob(blob)
      }
      message.success(t('page.voice.synthDone'))
    } catch {
      // 拦截器弹错(百炼未配 synth_url 是显式报错,不是静默)
    } finally {
      setBusy(null)
    }
  }

  if (!canConvert) {
    // 路由已挂 super_admin,走到这里基本是权限被运行时撤掉 —— 不渲染面板
    return null
  }

  return (
    <Card>
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Space wrap size={12} style={{ width: '100%' }}>
          <Select
            style={{ width: 320 }}
            placeholder={t('page.voice.selectVoice')}
            showSearch
            optionFilterProp="label"
            value={voiceId}
            onChange={setVoiceId}
            options={voices.map((v) => ({
              value: v.id,
              label: `${v.name}(${v.voice_id})`,
            }))}
          />
          <Radio.Group value={provider} onChange={(e) => setProvider(e.target.value)}>
            <Radio.Button value="">{t('page.voice.providerAuto')}</Radio.Button>
            <Radio.Button value="bailian">{t('page.voice.providerBailian')}</Radio.Button>
            <Radio.Button value="edge">{t('page.voice.providerEdge')}</Radio.Button>
          </Radio.Group>
          <Radio.Group
            value={format}
            onChange={(e) => setFormat(e.target.value)}
            disabled={isEdge}
          >
            <Radio.Button value="wav">wav</Radio.Button>
            <Radio.Button value="mp3">mp3</Radio.Button>
          </Radio.Group>
        </Space>
        {isEdge && <Alert type="info" showIcon message={t('page.voice.edgeWavHint')} />}
        {!isEdge && provider === 'bailian' && (
          <Alert type="info" showIcon message={t('page.voice.bailianHint')} />
        )}
        <Input.TextArea
          rows={4}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={t('page.voice.textPh')}
          maxLength={2000}
          showCount
        />
        <Space wrap size={12}>
          <InputNumber
            addonBefore={t('page.voice.volume')}
            min={-100}
            max={100}
            value={volume}
            onChange={(v) => setVolume(Number(v) || 0)}
            style={{ width: 180 }}
          />
          <InputNumber
            addonBefore={t('page.voice.rate')}
            min={-100}
            max={100}
            value={rate}
            onChange={(v) => setRate(Number(v) || 0)}
            style={{ width: 180 }}
          />
        </Space>
        <Space wrap size={12}>
          <Button
            type="primary"
            icon={<PlayCircleOutlined />}
            loading={busy === 'once'}
            onClick={() => run('once')}
          >
            {t('page.voice.play')}
          </Button>
          <Button
            icon={<DownloadOutlined />}
            loading={busy === 'download'}
            onClick={() => run('download')}
          >
            {t('page.voice.download')}
          </Button>
          <Button
            icon={<SoundOutlined />}
            loading={busy === 'stream'}
            onClick={() => run('stream')}
          >
            {busy === 'stream'
              ? t('page.voice.streaming', undefined, { n: streamBytes })
              : t('page.voice.streamPlay')}
          </Button>
        </Space>
      </Space>
    </Card>
  )
}

export default SynthPanel
