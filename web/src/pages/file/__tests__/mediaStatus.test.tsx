/**
 * 媒体「浏览器能不能播」的前端呈现(M145d)
 *
 * ## 这条网守的是什么
 *
 * 后端 `pkg/media` 判好了两件事:
 *   · 这个文件浏览器**能不能直接播**(`media_reason` 空 = 能播);
 *   · 转码走到哪一步了(`transcode_status`)。
 *
 * 前端要把这个状态**显示出来** —— 用户传完一个 WMV,列表里跟别的文件
 * 长得一模一样,点开才发现播不了,那就是"平台上传视频有问题"的观感。
 *
 * ## ⚠️ **前端不判"能不能播"** —— 这条是这个文件存在的前提
 *
 * 前端当然可以问浏览器 `video.canPlayType('video/mp4; codecs="avc1"')`。
 * 本项目**不这么做**,因为那是"**这一个浏览器**"的答案:
 *   · Chrome 说能播 HEVC、Safari 能、Firefox 依平台 —— 用户不会为了
 *     看一个视频去换浏览器;后端判的是"主流浏览器都能播"这档,两边口径不同;
 *   · canPlayType 只返 "" / "maybe" / "probably",**不告诉你为什么**;
 *   · 前端判出"播不了"之后**仍然播不了** —— 要能播得转码,那是后端的活。
 *
 * ⇒ 前端只**搬运**后端判好的结果。这里守的是"搬运对了、显示出来了"。
 *
 * ## ⚠️ **顺序不能反:先看转码状态,再看原文件播不播得了**
 *
 * 转码完成之后文件**是能播的**(走 `playback_url`)。这时候还标
 * "浏览器不支持"就是在骗用户。
 */
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { App } from 'antd'
import {
  mediaStatusOf,
  playbackSrcOf,
  MEDIA_STATUS_LABEL_KEY,
  TRANSCODE_NONE,
  TRANSCODE_PENDING,
  TRANSCODE_RUNNING,
  TRANSCODE_DONE,
  TRANSCODE_FAILED,
} from '../mediaStatus'
import { MediaStatusTag } from '../MediaStatusTag'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

/** 按点分路径取值,取不到返 undefined */
function pick(dict: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((acc, k) => {
    if (acc === null || acc === undefined) return undefined
    if (typeof acc !== 'object') return undefined
    return (acc as Record<string, unknown>)[k]
  }, dict)
}

describe('mediaStatusOf · 状态映射', () => {
  it('非媒体 / 浏览器本来就能播 ⇒ none(什么都不显示)', () => {
    expect(mediaStatusOf({ transcode_status: TRANSCODE_NONE })).toBe('none')
    expect(mediaStatusOf({ transcode_status: TRANSCODE_NONE, media_reason: '' })).toBe('none')
    expect(mediaStatusOf(null)).toBe('none')
    expect(mediaStatusOf(undefined)).toBe('none')
  })

  it('原文件播不了、又没有转码 ⇒ unplayable', () => {
    expect(
      mediaStatusOf({ transcode_status: TRANSCODE_NONE, media_reason: '视频编码 MPEG-4 Part 2 浏览器不支持' }),
    ).toBe('unplayable')
  })

  it('转码中 / 待转码 ⇒ running / pending', () => {
    expect(mediaStatusOf({ transcode_status: TRANSCODE_RUNNING })).toBe('running')
    expect(mediaStatusOf({ transcode_status: TRANSCODE_PENDING })).toBe('pending')
  })

  it('转码完成 ⇒ done(**不是 unplayable** —— 有副本就能播)', () => {
    // ⚠️ 这一条是顺序的红线:media_reason 还在(那是**原文件**的判据),
    //   但文件已经能播了 —— 标成 unplayable 就是在骗用户。
    expect(
      mediaStatusOf({ transcode_status: TRANSCODE_DONE, media_reason: '容器 MKV 浏览器不支持' }),
    ).toBe('done')
  })

  it('转码失败 ⇒ failed(原文件本来能播的话不算失败)', () => {
    expect(
      mediaStatusOf({ transcode_status: TRANSCODE_FAILED, media_reason: '容器 FLV 浏览器不支持' }),
    ).toBe('failed')
    expect(mediaStatusOf({ transcode_status: TRANSCODE_FAILED })).toBe('none')
  })
})

describe('playbackSrcOf · 预览该用哪个直链', () => {
  it('转码完成 ⇒ 用副本(原文件可能压根不是浏览器能播的格式)', () => {
    expect(
      playbackSrcOf({
        transcode_status: TRANSCODE_DONE,
        playback_url: '/rapid-admin/3/default/2/__playback__1.mp4',
        url: '/rapid-admin/3/default/2/a.flv',
      }),
    ).toBe('/rapid-admin/3/default/2/__playback__1.mp4')
  })

  it('没转 / 转失败 ⇒ 用原文件(总得让人下到原文件)', () => {
    expect(
      playbackSrcOf({
        transcode_status: TRANSCODE_FAILED,
        playback_url: '/x.mp4',
        url: '/rapid-admin/3/default/2/a.flv',
      }),
    ).toBe('/rapid-admin/3/default/2/a.flv')
    expect(playbackSrcOf({ transcode_status: TRANSCODE_NONE, url: '/a.mp4' })).toBe('/a.mp4')
    expect(playbackSrcOf(null)).toBe('')
  })
})

describe('MediaStatusTag · i18n 键必须中英两侧都在', () => {
  // ⚠️ 这五个键是**字面量**(不是模板串拼的),所以可以直接对着查。
  //   拼出来的键一旦拼错,查不到时 t() 原样返回那串键 ⇒ 界面上显示
  //   "page.fileList.mediaUnplaable",而 locale-parity(只比两边键集合)
  //   **发现不了** —— 两边同时拼错时两边键集合是一样的。
  it('五个状态的键在 zh-CN / en-US 里都真实存在', () => {
    for (const [kind, key] of Object.entries(MEDIA_STATUS_LABEL_KEY)) {
      expect(pick(zhCN, key), `zh-CN 缺 ${kind} 的键 ${key}`).toBeDefined()
      expect(pick(enUS, key), `en-US 缺 ${kind} 的键 ${key}`).toBeDefined()
    }
  })
})

describe('MediaStatusTag · 渲染', () => {
  const renderTag = (props: Parameters<typeof MediaStatusTag>[0]) =>
    render(
      <App>
        <MediaStatusTag {...props} />
      </App>,
    )

  it('none ⇒ 什么都不渲染', () => {
    const { container } = renderTag({ transcode_status: TRANSCODE_NONE })
    expect(container.textContent).toBe('')
  })

  it('unplayable ⇒ 显示"浏览器不支持",悬浮给出后端那句原因', () => {
    renderTag({
      transcode_status: TRANSCODE_NONE,
      media_reason: '视频编码 MPEG-4 Part 2 浏览器不支持',
    })
    expect(screen.getByText('浏览器不支持')).toBeInTheDocument()
    expect(screen.getByTestId('file-media-status-unplayable')).toBeInTheDocument()
  })

  it('转码中 / 已转码 ⇒ 显示对应文案', () => {
    const { unmount } = renderTag({ transcode_status: TRANSCODE_RUNNING })
    expect(screen.getByText('转码中')).toBeInTheDocument()
    unmount()

    renderTag({ transcode_status: TRANSCODE_DONE })
    expect(screen.getByText('已转码')).toBeInTheDocument()
  })
})
