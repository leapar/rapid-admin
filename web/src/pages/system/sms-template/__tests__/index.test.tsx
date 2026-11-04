/**
 * 短信模板审核状态页(M115)
 *
 * ## 这条网守的是什么
 *
 *   这个页面上有**四个"看起来对、其实会静默错"**的地方,每一个都不会报错:
 *
 *   1. **场景名用后端返的 `scene_label`** —— 那是后端写死的中文
 *      ("登录验证码"),英文环境下整列露馅。前端必须走 i18n
 *      (constants/sms 的 SMS_SCENE_LABEL_KEY)。
 *   2. **状态标签在前端写死一张 status → 文案/颜色的表** —— 字典
 *      (sms_template_status)是运营能改的,前端那份副本必然漂移。
 *      必须走 useDict。
 *   3. **"恢复"按钮在非停用状态也点得了** —— 后端会拒
 *      ("该场景当前不是停用状态,无需恢复"),用户白点一次。
 *   4. **同步之后不重拉列表** —— 用户点了"同步",页面纹丝不动,
 *      于是以为同步没生效,又点一次。
 *
 *   另外一条:**同步结果里的 `failed` 不能当成"失败"**——
 *   里面混着"服务商查询失败"和"状态机按规则拒绝流转"
 *   (人工停用的模板,服务商说通过 —— 硬规则 2 在起作用,不是故障)。
 *   所以提示要说"没能确定状态",不是"失败"。
 *
 * ## ⚠️ **等的是"渲染出来",不是"请求发出去了"**(2026-10-31)
 *
 *   下面 4 条断言原本都长这样:
 *
 *   ```ts
 *   await waitFor(() => expect(listMock).toHaveBeenCalled())   // ← 等的是"请求发出去了"
 *   expect(screen.getByText('审核通过')).toBeInTheDocument()    // ← 却立刻断言"表格已经渲染出来了"
 *   ```
 *
 *   `listMock` 是 `vi.fn()` —— `toHaveBeenCalled()` 在**调用发生的那一刻**
 *   就为真;而组件还要 `await listMock()` → setState → React 重新渲染,
 *   中间隔着**至少一个微任务 + 一次渲染**。
 *
 *   ⇒ **满载时**(全量跑 104 个文件)这个间隔会被拉长到超过同步断言的时机 ⇒
 *     `Unable to find an element with the text: 审核通过`。
 *     而**单独跑这个文件是绿的** —— 这就是 commit `73ad299` 记过的"满载假红"。
 *     **假红比假绿更坑**:让人以为"刚改的东西把别的用例搞坏了",
 *     于是去查一个根本不存在的问题,还得多跑一遍 12 分钟的全量套件。
 *
 *   ⇒ 正确写法是**等渲染结果**,不是等请求:
 *
 *   ```ts
 *   await waitFor(() => expect(screen.getByText('审核通过')).toBeInTheDocument())
 *   ```
 *
 *   ⚠️ 这 4 条**已经红过两次**(2026-10-31 全量跑实测,两次都落在本文件),
 *   所以这不是"可能发生",是"已经在发生"。
 *
 *   ### 同一个毛病的另一种长相:`rowOf()` 是**抛异常**的
 *
 *   下面 3 条原本是"等请求 → 立刻 `within(rowOf('login'))`":
 *
 *   ```ts
 *   await waitFor(() => expect(listMock).toHaveBeenCalled())
 *   expect(within(rowOf('login')).getByRole('button', { name: '停用' })).toBeEnabled()
 *   ```
 *
 *   `rowOf()` 在行还没画出来时抛 "表格里没有 scene=login 那一行" ——
 *   那个错**看起来像真 bug**(像是页面把行渲染丢了),其实只是没等。
 *   ⇒ 用下面的 `waitForRow()`。
 *
 *   ⚠️ 还有一条是**假绿**:"没有按钮权限时按钮不渲染" ——
 *   表格还没画的时候自然一个按钮都没有,断言白绿。
 *   ⇒ 先 `await waitForRow('login')` 把表格等出来,再断言按钮不存在。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 sceneLabel 改成返回 row.scene_label → 「场景名走 i18n」红;
 *   · 把状态标签改成直接渲染 v → 「状态标签走字典」红;
 *   · 去掉 enable 的 `disabled: row.status !== 'disabled'` → 「恢复按钮」红;
 *   · 去掉 sync finally 里的 load() → 「同步后重拉」红;
 *   · 把 failed 分支删掉(只判 updated/registered)→ 「failed 不当失败」红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor, fireEvent, screen, within } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import SmsTemplateList from '../index'
import { smsTemplateApi } from '@/api/smsTemplate'
import { usePermissionStore } from '@/stores/permission'

vi.mock('@/api/smsTemplate', () => ({
  smsTemplateApi: {
    list: vi.fn(),
    detail: vi.fn(),
    history: vi.fn(),
    sync: vi.fn(),
    disable: vi.fn(),
    enable: vi.fn(),
  },
}))

/**
 * 字典 hook 打桩 —— 真实实现会去后端拉 sys_dict,
 * 在这个测试里只会打出一堆无意义的网络错误(而且 label 拿不到)。
 *
 * ⚠️ 这里给的是**与真实字典不同的文案**(比如 approved → 「已通过」),
 *   故意跟 status 字面量长得不一样 ——
 *   页面若把 status 原值直接渲染出来,测试就会发现「页面上出现了 approved」。
 */
vi.mock('@/hooks/useDict', () => ({
  useDict: () => ({
    options: [],
    raw: [],
    loading: false,
    error: false,
    getLabel: (v: string | number) =>
      ({
        approved: '审核通过',
        rejected: '已驳回',
        disabled: '人工停用',
        pending: '待审核',
        unknown: '状态未知',
      })[String(v)] ?? String(v),
    getColor: (v: string | number) =>
      ({ approved: 'green', rejected: 'red', disabled: 'orange' })[String(v)] ?? 'default',
  }),
}))

const listMock = vi.mocked(smsTemplateApi.list)
const syncMock = vi.mocked(smsTemplateApi.sync)
const disableMock = vi.mocked(smsTemplateApi.disable)
const enableMock = vi.mocked(smsTemplateApi.enable)

/**
 * 列表响应 —— 四个场景各挑一种状态,把分支都铺开
 *
 * ⚠️ `scene_label` 故意填后端写死的中文("登录验证码"),
 *   页面要是渲染了它,下面的断言会抓到。
 */
const LIST_VIEW = {
  items: [
    {
      scene: 'login',
      scene_label: '登录验证码',
      template_code: 'SMS_1001',
      registered_code: 'SMS_1001',
      code_mismatch: false,
      configured: true,
      registered: true,
      status: 'approved',
      reason: '',
      source: 'provider',
      blocked: false,
      usable: true,
      reviewed_at: '2026-10-29T08:00:00Z',
      updated_at: '2026-10-29T08:00:00Z',
      // 上一轮同步失败的原因(状态列只说"未同步",这一列说为什么)
      last_error: '服务商超时',
    },
    {
      // 没配模板号 —— 停用按钮应当禁用
      scene: 'reset_password',
      scene_label: '重置密码',
      template_code: '',
      registered_code: '',
      code_mismatch: false,
      configured: false,
      registered: false,
      status: 'unknown',
      reason: '',
      source: 'manual',
      blocked: false,
      usable: false,
      reviewed_at: '',
      updated_at: '',
      last_error: '',
    },
    {
      // 人工停用 —— 恢复按钮应当可点
      scene: 'change_phone',
      scene_label: '换绑手机',
      template_code: 'SMS_1003',
      registered_code: 'SMS_1003',
      code_mismatch: false,
      configured: true,
      registered: true,
      status: 'disabled',
      reason: '服务商驳回',
      source: 'manual',
      blocked: true,
      usable: false,
      reviewed_at: '2026-10-29T09:00:00Z',
      updated_at: '2026-10-29T09:00:00Z',
      last_error: '',
    },
    {
      // 模板号对不上 —— 登记册里的结论是旧号的
      scene: 'view_full_phone',
      scene_label: '查看完整手机号',
      template_code: 'SMS_1004',
      registered_code: 'SMS_9999',
      code_mismatch: true,
      configured: true,
      registered: true,
      status: 'rejected',
      reason: '模板内容有误',
      source: 'provider',
      blocked: true,
      usable: false,
      reviewed_at: '2026-10-29T10:00:00Z',
      updated_at: '2026-10-29T10:00:00Z',
      last_error: '',
    },
  ],
  policy: 'block',
  provider: 'aliyun',
}

/** 按 scene 找到那一行(用 <code> 里的原始 scene 定位 —— 与语言无关) */
function rowOf(scene: string): HTMLElement {
  const rows = Array.from(document.querySelectorAll('.ant-table-tbody tr'))
  const row = rows.find((r) =>
    Array.from(r.querySelectorAll('code')).some((c) => c.textContent === scene),
  )
  if (!row) throw new Error(`表格里没有 scene=${scene} 那一行`)
  return row as HTMLElement
}

/**
 * 等某一行**渲染出来**
 *
 * ⚠️ `rowOf()` 在行还没画出来时是**抛异常**的("表格里没有 scene=xxx 那一行"),
 *   所以不能"等请求发出去了"就直接 `rowOf(...)` ——
 *   那只证明了请求发出去了,证明不了表格已经画出来。
 *   满载时这个间隔足够长 ⇒ 直接抛,而且抛的是一个**看起来像真 bug** 的错。
 *
 *   ⇒ 等的必须是**要断言的那个东西本身**。
 */
async function waitForRow(scene: string): Promise<void> {
  // rowOf 在行还没画出来时抛异常 —— waitFor 会把它当成"还没好",继续等
  await waitFor(() => {
    expect(rowOf(scene)).toBeTruthy()
  })
}

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <SmsTemplateList />
      </MemoryRouter>
    </App>,
  )
}

describe('短信模板审核状态页(M115)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue(LIST_VIEW as never)
    syncMock.mockResolvedValue({
      registered: [],
      updated: [],
      unchanged: 4,
      failed: [],
    } as never)
    disableMock.mockResolvedValue(LIST_VIEW.items[0] as never)
    enableMock.mockResolvedValue(LIST_VIEW.items[2] as never)
    // 行操作按钮按 perm 显隐 —— 放行本页的四个按钮
    usePermissionStore.setState({
      permissionCodes: new Set([
        'button.sms_template.disable',
        'button.sms_template.enable',
        'button.sms_template.sync',
        'button.sms_template.history',
        'sms_template:list',
      ]),
      roleCodes: ['role_super_admin'],
    })
  })

  it('场景名走 i18n,不用后端写死的中文 scene_label', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // i18n 的场景名(constants/sms → locales 的 smsScene 组)
    // ⚠️ 等**渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitFor(() => expect(screen.getByText('短信登录')).toBeInTheDocument())
    // 后端 scene_label 写死的是「登录验证码」—— 页面不许显示它
    expect(screen.queryByText('登录验证码')).toBeNull()
  })

  it('状态标签走字典,不直接渲染 status 字面量', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // 字典里的 approved → 「审核通过」
    // ⚠️ 等**渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitFor(() => expect(screen.getByText('审核通过')).toBeInTheDocument())
    // 页面不许把 status 原值露出来
    expect(screen.queryByText('approved')).toBeNull()
  })

  it('没配模板号的场景不能停用(后端会拒:没有可停用的模板)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ 等**行渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitForRow('login')
    await waitForRow('reset_password')
    expect(within(rowOf('login')).getByRole('button', { name: '停用' })).toBeEnabled()
    expect(within(rowOf('reset_password')).getByRole('button', { name: '停用' })).toBeDisabled()
  })

  it('只有停用状态才谈得上恢复', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ 等**行渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitForRow('change_phone')
    await waitForRow('login')
    expect(within(rowOf('change_phone')).getByRole('button', { name: '恢复' })).toBeEnabled()
    expect(within(rowOf('login')).getByRole('button', { name: '恢复' })).toBeDisabled()
  })

  it('没有按钮权限时,行操作按钮不渲染(而不是渲染出来点一下吃 403)', async () => {
    usePermissionStore.setState({
      permissionCodes: new Set(['sms_template:list']),
      roleCodes: ['role_ops'],
    })
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ **先等表格渲染出来** —— 不然这条是**假绿**:
    //   表格还没画,自然一个按钮都没有,断言白绿。
    //   (顺带:这也从根上避免了"等请求 ≠ 等渲染"的假红。)
    await waitForRow('login')
    expect(screen.queryByRole('button', { name: '停用' })).toBeNull()
    expect(screen.queryByRole('button', { name: '恢复' })).toBeNull()
  })

  it('点同步之后要重拉列表(否则用户看到的是旧状态)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(1))

    // ⚠️ 按钮名用**正则**:antd 图标会渲染成 <span role="img" aria-label="sync">,
    //   参与无障碍名计算 —— 精确匹配 '立即同步' 找不到(真名是 "sync 立即同步")。
    fireEvent.click(screen.getByRole('button', { name: /立即同步/ }))

    await waitFor(() => expect(syncMock).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(2))
  })

  it('同步没能确定状态时提示"没能确定状态",不是"失败"', async () => {
    // failed 里混着两种东西:服务商查询失败,和状态机按规则拒绝流转
    // (人工停用的模板,服务商说通过 —— 硬规则 2 在起作用,不是故障)
    syncMock.mockResolvedValue({
      registered: [],
      updated: [],
      unchanged: 3,
      failed: ['change_phone'],
    } as never)

    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    fireEvent.click(screen.getByRole('button', { name: /立即同步/ }))

    // ⚠️ **不能**用 getByText(/change_phone/):表格里那一行的 <code> 也是 change_phone,
    //   两个节点都匹配。要断言的是「提示里带了哪个场景」。
    // ⚠️ 提示是异步挂上去的(message 有过渡动画)—— 必须等,不能直接查。
    const toast = await waitFor(() => screen.getByText(/没能确定状态/))
    expect(toast.textContent).toContain('change_phone')
  })

  it('停用时把填的原因交给 api 层(后端读的是 query,不是 body)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ 等**行渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitForRow('login')
    fireEvent.click(within(rowOf('login')).getByRole('button', { name: '停用' }))

    // 弹窗打开 → 填原因 → 提交
    const textarea = await waitFor(() => {
      const el = document.querySelector('.ant-modal textarea') as HTMLTextAreaElement | null
      expect(el).not.toBeNull()
      return el!
    })
    fireEvent.change(textarea, { target: { value: '服务商驳回' } })
    // ⚠️ 提交按钮要在**弹窗里**找 —— 弹窗的确定按钮文案也是「停用」,
    //   跟行操作按钮同名(全局找会命中两个)。
    // ⚠️ **用正则**:antd 的 primary 按钮会把两个汉字之间插空格("停 用"),
    //   精确匹配 '停用' 找不到(行操作按钮是 link 类型,不插空格,所以那边能用精确匹配)。
    const modal = document.querySelector('.ant-modal') as HTMLElement
    fireEvent.click(within(modal).getByRole('button', { name: /停\s*用/ }))

    await waitFor(() => expect(disableMock).toHaveBeenCalledTimes(1))
    expect(disableMock).toHaveBeenCalledWith('login', '服务商驳回')
  })

  it('守卫策略横幅把当前策略摆出来(off / warn / block 三档)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // policy = 'block' → 横幅标题是「守卫策略:拦截」
    // ⚠️ 用**完整字符串**而不是 /拦截/:横幅的描述里也出现了「拦截」
    //   ("决定放行、告警还是拦截"),正则会命中两个节点。
    // ⚠️ 等**渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitFor(() => expect(screen.getByText('守卫策略:拦截')).toBeInTheDocument())
  })

  // ---- 搜索区(用户 2026-10-30 报:searchsection 里没有搜索输入框)----

  it('搜索区里真的有搜索输入框', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // ⚠️ **这条守的是"搜索区渲染了,但里面是空的"** ——
    //   ManagementTable 的 showSearchBar = onSearch || onReset || searchExtra || inlineToolBar,
    //   只传 inlineToolBar 时搜索区照样渲染,**但 Input.Search 只在传了 onSearch 时才渲染**
    //   ⇒ 用户看到的就是一个没有输入框的搜索区。
    const input = document.querySelector('.ant-input-search input') as HTMLInputElement | null
    expect(input).not.toBeNull()
  })

  it('搜索按场景 / 模板号 / 状态过滤', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const input = document.querySelector('.ant-input-search input') as HTMLInputElement
    fireEvent.change(input, { target: { value: 'SMS_1001' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })

    // 命中行还在,没命中的行没了
    await waitFor(() => expect(rowOf('login')).toBeTruthy())
    expect(() => rowOf('change_phone')).toThrow()
  })

  it('搜索能按"上次失败原因"命中(用户就是搜"为什么没同步")', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const input = document.querySelector('.ant-input-search input') as HTMLInputElement
    fireEvent.change(input, { target: { value: '服务商超时' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })

    await waitFor(() => expect(rowOf('login')).toBeTruthy())
    expect(() => rowOf('change_phone')).toThrow()
  })

  it('重置把过滤清掉(而且输入框里的字也要清掉)', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const input = document.querySelector('.ant-input-search input') as HTMLInputElement
    fireEvent.change(input, { target: { value: 'SMS_1001' } })
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })
    await waitFor(() => expect(() => rowOf('change_phone')).toThrow())

    fireEvent.click(screen.getByText('重置'))

    await waitFor(() => expect(rowOf('change_phone')).toBeTruthy())
    // ⚠️ **输入框里的字也要清掉** —— 只清页面状态不清输入框,
    //   用户看着框里的词以为还在过滤(实际已经不过滤了)。
    await waitFor(() =>
      expect((document.querySelector('.ant-input-search input') as HTMLInputElement).value).toBe(''),
    )
  })

  it('上一轮同步失败的原因显示出来(不然只看到"未同步")', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    // 后端把失败原因带回来(SmsTemplateView.LastError)
    // ⚠️ 等**渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitFor(() => expect(screen.getByText('服务商超时')).toBeInTheDocument())
  })

  it('同步提示里带上失败原因(不然还是不知道为什么没同步)', async () => {
    syncMock.mockResolvedValue({
      registered: [],
      updated: [],
      unchanged: 0,
      failed: ['login'],
      failures: [{ scene: 'login', reason: '模板号在阿里云不存在' }],
    } as never)

    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    fireEvent.click(screen.getByRole('button', { name: /立即同步/ }))

    const toast = await waitFor(() => screen.getByText(/没能确定状态/))
    expect(toast.textContent).toContain('模板号在阿里云不存在')
  })
})
