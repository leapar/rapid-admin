/**
 * 附件字段的控件 —— 上传 / 显示 / 清除(2026-11-02)
 *
 * # ⚠️ 字段里存的是**文件的 key**(不带域名的路径),不是 URL
 *
 *   URL 里带域名(本地部署是 `/rapid-admin/...`,CDN 部署是 `https://cdn.../...`),
 *   域名会变 ⇒ 存 URL 等于把"换 CDN"变成一次数据迁移。
 *   权威:docs/业务表单设计器设计方案V1.0.md §2.9。
 *
 * # 这一层要挡住的三种"静默"
 *
 *   1. **上传失败却把值写进去了** —— 用户会以为文件传上去了,
 *      而后端一保存就报"文件不存在"。
 *   2. **刚上传的文件没有名字可显示** —— `_files` 是后端读**记录**时解析出来的,
 *      新传的文件不在里面 ⇒ 控件必须自己记一份。
 *   3. **文件失效了却看不出��** —— 记录里那个 key 已经被删,
 *      界面得说一声,而不是显示一个点不开的链接。
 *
 * # 可逆探针(拆掉实现看测试会不会红)
 *
 *   · 上传失败时也调 onChange                  → 「上传失败不写值」红
 *   · 上传时用别的 biz(不按 dyn 分文件夹)     → 「上传带 dyn biz」红
 *   · 刚上传的文件显示成 key 而不是原文件名    → 「刚上传的文件显示原文件名」红
 *   · 文件失效时不提示                        → 「文件失效有提示」红
 *   · 清除按钮不清值                          → 「清除」红
 *   · 没有 file:upload 还渲染上传按钮          → 「没有上传权限:不渲染上传按钮」红
 *   · 没有 file:upload 时把已有文件名也藏了      → 「没有上传权限:仍然显示已有文件」红
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { App } from 'antd'
import { FileFieldControl } from '../FileFieldControl'
import { uploadFile } from '@/api/upload'
import type { FileView } from '@/api/upload'
import type * as UploadModule from '@/api/upload'
import { usePermissionStore } from '@/stores/permission'
import { PermFile } from '@/constants/permissions'
import type { DynFileRef } from '@/types/dyn'

vi.mock('@/api/upload', async (importOriginal) => {
  const actual = await importOriginal<typeof UploadModule>()
  return { ...actual, uploadFile: vi.fn() }
})

const uploadMock = vi.mocked(uploadFile)

/** 造一个文件 */
function makeFile(name = '户型图.pdf'): File {
  return new File([new Uint8Array([1, 2, 3])], name, { type: 'application/pdf' })
}

/** 文件服务返回的那份视图(只填本用例关心的字段) */
function fileView(over: Partial<FileView> = {}): FileView {
  return {
    id: 'f1',
    folder_id: '0',
    user_id: 'u1',
    biz: 'dyn',
    file_key: 'rapid-admin/1/dyn/0/a.pdf',
    file_name: '户型图.pdf',
    file_size: 3,
    content_type: 'application/pdf',
    etag: 'e',
    storage: 0,
    is_cover: false,
    status: 1,
    created_at: '2026-11-02T00:00:00Z',
    duration_ms: 0,
    url: '/rapid-admin/1/dyn/0/a.pdf',
    ...over,
  }
}

/** 那个隐藏的 `<input type="file">` */
function fileInput(container: HTMLElement): HTMLInputElement {
  const input = container.querySelector('input[type="file"]') as HTMLInputElement
  expect(input).not.toBeNull()
  return input
}

function renderControl(props: Partial<React.ComponentProps<typeof FileFieldControl>> = {}) {
  const onChange = vi.fn()
  const utils = render(
    <App>
      <FileFieldControl onChange={onChange} {...props} />
    </App>,
  )
  return { ...utils, onChange }
}

/**
 * 受控的宿主 —— 值存在 state 里,onChange 把它写回去
 *
 * ⚠️ **附件控件是受控的** —— 传进来的 `value` 不变,界面上就什么都不会变。
 *   真实页面里 EditModal 的表单就是那个宿主(表单值变了 → 重新渲染这个控件)。
 */
function Harness({ initial = '', file }: { initial?: string; file?: DynFileRef }) {
  const [v, setV] = useState(initial)
  return <FileFieldControl value={v} file={file} onChange={setV} />
}

beforeEach(() => {
  vi.clearAllMocks()
  // ⚠️ **默认给 file:upload** —— 上传按钮按这个权限码控可见性。
  //   默认不给的话,下面每一条上传用例都会因为"按钮没渲染"而假红。
  usePermissionStore.setState({ permissionCodes: new Set([PermFile.UPLOAD]) })
})

describe('FileFieldControl · 显示', () => {
  it('没传文件时:只有「上传文件」按钮', () => {
    renderControl()
    expect(screen.getByRole('button', { name: /上\s*传\s*文\s*件/ })).toBeInTheDocument()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('有文件时:显示原文件名 + 可点开的直链', () => {
    renderControl({
      value: 'rapid-admin/1/dyn/0/a.pdf',
      file: { name: '户型图.pdf', url: '/rapid-admin/1/dyn/0/a.pdf' },
    })
    const link = screen.getByRole('link', { name: '户型图.pdf' })
    expect(link).toHaveAttribute('href', '/rapid-admin/1/dyn/0/a.pdf')
  })

  /**
   * ⚠️ **文件失效了要说一声** ——
   *   记录里那个 key 已经被删(后端解析不到),界面得告诉用户,
   *   而不是显示一个点不开的链接。
   */
  it('有 key 但解析不到文件时:显示 key + 「文件已失效」提示', () => {
    renderControl({ value: 'rapid-admin/1/dyn/0/gone.pdf' })
    expect(screen.getByText(/文\s*件\s*已\s*失\s*效/)).toBeInTheDocument()
    expect(screen.getByText('rapid-admin/1/dyn/0/gone.pdf')).toBeInTheDocument()
  })
})

describe('FileFieldControl · 上传', () => {
  it('选文件后:走文件服务(biz=dyn),并把 key 交出去', async () => {
    uploadMock.mockResolvedValue(fileView())
    const { container, onChange } = renderControl()
    const file = makeFile()
    fireEvent.change(fileInput(container), { target: { files: [file] } })

    await waitFor(() => expect(uploadMock).toHaveBeenCalled())
    expect(uploadMock).toHaveBeenCalledWith(file, { biz: 'dyn' })
    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith('rapid-admin/1/dyn/0/a.pdf'),
    )
  })

  /**
   * ⚠️ **刚上传的文件要显示原文件名** ——
   *   `_files` 是后端读**记录**时解析出来的,新传的文件不在里面
   *   ⇒ 控件必须自己记一份,否则用户传完只看到一个 key。
   */
  it('刚上传的文件:显示原文件名(不是 key)', async () => {
    uploadMock.mockResolvedValue(fileView())
    const { container } = render(
      <App>
        <Harness />
      </App>,
    )
    fireEvent.change(fileInput(container), { target: { files: [makeFile()] } })
    await waitFor(() => expect(screen.getByRole('link', { name: '户型图.pdf' })).toBeInTheDocument())
  })

  /**
   * ⚠️ **上传失败不能写值** ——
   *   写了值而后端一保存就报"文件不存在",用户两头都懵。
   */
  it('上传失败:不写值,并给出错误提示', async () => {
    uploadMock.mockRejectedValue(new Error('boom'))
    const { container, onChange } = renderControl()
    fireEvent.change(fileInput(container), { target: { files: [makeFile()] } })

    await waitFor(() => expect(uploadMock).toHaveBeenCalled())
    await waitFor(() => expect(screen.getByText(/上\s*传\s*失\s*败/)).toBeInTheDocument())
    expect(onChange).not.toHaveBeenCalled()
  })
})

describe('FileFieldControl · 清除', () => {
  it('点「清除」:交出空串(表示"这个字段没有值")', () => {
    const { onChange } = renderControl({
      value: 'rapid-admin/1/dyn/0/a.pdf',
      file: { name: '户型图.pdf', url: '/x' },
    })
    fireEvent.click(screen.getByRole('button', { name: /清\s*除/ }))
    expect(onChange).toHaveBeenCalledWith('')
  })
})

describe('FileFieldControl · 上传权限', () => {
  /**
   * ⚠️ **没有 `file:upload` 就不渲染上传按钮** ——
   *   上传走 `POST /biz/files`,那个接口要 `file:upload`。
   *   渲染出来只能让人点了吃一个 403。
   */
  it('没有上传权限:不渲染上传按钮', () => {
    usePermissionStore.setState({ permissionCodes: new Set() })
    renderControl()
    expect(screen.queryByRole('button', { name: /上\s*传\s*文\s*件/ })).toBeNull()
    expect(screen.getByText(/没\s*有\s*上\s*传\s*文\s*件\s*的\s*权\s*限/)).toBeInTheDocument()
  })

  /**
   * ⚠️ **读是另一件事** —— 没有上传权,不代表看不见已有的附件。
   */
  it('没有上传权限:仍然显示已有文件(读不受影响)', () => {
    usePermissionStore.setState({ permissionCodes: new Set() })
    renderControl({
      value: 'rapid-admin/1/dyn/0/a.pdf',
      file: { name: '户型图.pdf', url: '/rapid-admin/1/dyn/0/a.pdf' },
    })
    expect(screen.getByRole('link', { name: '户型图.pdf' })).toBeInTheDocument()
  })
})
