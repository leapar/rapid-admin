/**
 * 语音管理 —— 音色登记册 + 克隆台账 + 文本合成(2026-11-06)
 *
 * # 页面编排
 *
 *   三个 tab,各管一件事:
 *   · 音色列表(VoiceRegistry)—— sys_tts_voice 登记册,内置音色由后端
 *     seed 自动登记(**只补缺不覆盖**),这里是运营改展示信息 + 试听;
 *   · 克隆记录(ClonesTab)—— sys_voice_clone 台账,克隆是异步动作,
 *     状态 DEPLOYING → OK / UNDEPLOYED 要手动「刷新状态」同步;
 *   · 文本合成(SynthPanel)—— 选音色 + 写文本,整段试听 / 下载 / 流式试听。
 *
 * # 权限口径
 *
 * 菜单与路由只挂 super_admin(voice_* / tts_* 码只有它有);
 * 页内的按钮粒度交给权限码(「克隆音色」= voice_clone:create,
 * 试听 = tts:convert)—— 没有克隆权限的人仍然可以看音色、试听。
 */
import { useState } from 'react'
import type { FC } from 'react'
import { Button, Tabs } from 'antd'
import { AudioOutlined } from '@ant-design/icons'
import { PageHeader } from '@/components/common/PageHeader'
import { useI18n } from '@/locales'
import { usePermission } from '@/hooks/usePermission'
import { PermVoice } from '@/constants/permissions'
import VoiceRegistry from './VoiceRegistry'
import ClonesTab from './ClonesTab'
import SynthPanel from './SynthPanel'
import CloneModal from './CloneModal'

const VoicePage: FC = () => {
  const { t } = useI18n()
  const { has } = usePermission()
  const [tab, setTab] = useState('voices')
  const [cloneOpen, setCloneOpen] = useState(false)

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <PageHeader description={t('page.voice.description')} />
      <Tabs
        activeKey={tab}
        onChange={setTab}
        tabBarExtraContent={
          has(PermVoice.CLONE_CREATE) && (
            <Button type="primary" icon={<AudioOutlined />} onClick={() => setCloneOpen(true)}>
              {t('page.voice.cloneVoice')}
            </Button>
          )
        }
        items={[
          { key: 'voices', label: t('page.voice.tabVoices'), children: <VoiceRegistry active={tab === 'voices'} /> },
          { key: 'clones', label: t('page.voice.tabClones'), children: <ClonesTab active={tab === 'clones'} /> },
          { key: 'synth', label: t('page.voice.tabSynth'), children: <SynthPanel active={tab === 'synth'} /> },
        ]}
      />
      <CloneModal open={cloneOpen} onClose={() => setCloneOpen(false)} onCloned={() => setTab('clones')} />
    </div>
  )
}

export default VoicePage
