/**
 * CronInput —— 「人话描述」渲染回归网(M120 / 2026-10-30 补)
 *
 * # 这条网守的是什么
 *
 * `cronDescribe.ts` 的解析有 68 条用例守着,但那 68 条守的是
 * **"表达式 → 一句话"这个纯函数**。中间还隔着一段:**这句话有没有被
 * 渲染到界面上**。
 *
 * 2026-10-30 用户报的是:
 *
 *   「这个定时器调度表达式 7,17,27,37,47,57 * * * * 你翻译为接下来 5 次:
 *    2026-10-30 18:17:00 / … —— 它应该意思是:每月每日每星期每小时的
 *    第 7、17、27、37、47、57 分钟各执行一次」
 *
 * 也就是说:界面上**只有「接下来 5 次」**,那句人话**没出现**。
 *
 * ⚠️ **纯函数绿 ≠ 界面上有** —— 中间隔着:
 *   · 组件有没有把 `describeCron` 的结果放进 JSX;
 *   · `useI18n().t` 是不是真的能取到那些键;
 *   · 键名在两侧字典里是不是对得上。
 * 任何一环断了,纯函数用例照样全绿,而界面上就是空的。
 *
 * ⇒ 这里渲染**真的组件**,断言**真的屏幕上**有那句话。
 *
 * # 为什么顺带把"预览接口失败"也钉住
 *
 * 人话描述是**前端本地算的**,不依赖预览接口。预览接口挂了(后端 400 /
 * 网络断),描述**照样要显示** —— 它是给运营看的"这条任务什么时候跑",
 * 是本地规则,不是服务端数据。
 * 反过来,如果描述依赖接口,后端一挂,运营看到的就只剩一个红色的报错,
 * 反而更看不懂。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { useUIStore } from '@/stores/ui'

// ⚠️ 预览接口要 mock 掉 —— 它是后端接口,单测里不该真发请求。
//   失败/成功两种都要能造出来(见下面两个 describe)。
const cronPreview = vi.fn()
vi.mock('@/api/job', () => ({
  jobApi: {
    cronPreview: (...args: unknown[]) => cronPreview(...args),
  },
}))

import { CronInput } from '../CronInput'

describe('CronInput · 人话描述(M120)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useUIStore.setState({ lang: 'zh-CN' })
    cronPreview.mockResolvedValue([
      '2026-10-30 18:17:00',
      '2026-10-30 18:27:00',
      '2026-10-30 18:37:00',
      '2026-10-30 18:47:00',
      '2026-10-30 18:57:00',
    ])
  })

  it('「7,17,27,37,47,57 * * * *」下面要出现那句话', async () => {
    render(<CronInput value="7,17,27,37,47,57 * * * *" />)

    // ⚠️ **逐字**断言,不用"包含某个词" ——
    //   "包含"式断言对漏字、顺序错乱都是绿的(见 cronDescribe.test.ts 文件头)
    await waitFor(() =>
      expect(
        screen.getByText('每月每日每星期每小时的第 7、17、27、37、47、57 分钟各执行一次'),
      ).toBeInTheDocument(),
    )
  })

  it('那句和「接下来 5 次」是**同时**在的 —— 两个信息互补,不是二选一', async () => {
    render(<CronInput value="7,17,27,37,47,57 * * * *" />)

    await waitFor(() =>
      expect(
        screen.getByText('每月每日每星期每小时的第 7、17、27、37、47、57 分钟各执行一次'),
      ).toBeInTheDocument(),
    )
    // 「接下来 5 次」是 M69 就有的(M69 的 5 次预览),不许被 M120 顶掉
    await waitFor(() => expect(screen.getByText(/接下来\s*5\s*次/)).toBeInTheDocument())
    expect(screen.getByText(/2026-10-30 18:17:00/)).toBeInTheDocument()
  })

  it('预览接口失败 → 人话描述**照样显示**(它是本地规则,不是服务端数据)', async () => {
    cronPreview.mockRejectedValue(new Error('boom'))

    // ⚠️ 用 `0 5 * * 1` 而不是 `0 3 * * *` ——
    //   预设按钮里有一个标签就叫「每天 03:00」,而 `0 3 * * *` 的描述
    //   **也是**「每天 03:00」→ getByText 会撞上两个元素(Found multiple)。
    //   用例挑表达式时要避开预设标签,否则测的是"重名"不是"渲染"。
    render(<CronInput value="0 5 * * 1" />)

    await waitFor(() => expect(screen.getByText('每周一 05:00')).toBeInTheDocument())
  })

  it('描述不出来的时候**一句都不显示**(不许拿"大概是…"糊弄)', async () => {
    // `0 3 1 * 1` = 日和星期同时受限 → robfig 是 OR 语义,含义不唯一 → 不描述
    const { container } = render(<CronInput value="0 3 1 * 1" />)

    // ⚠️ M132:等**预览结果上屏**("接下来 5 次"),不等"接口被调了"。
    //   请求发出 ≠ 结果被处理 —— 结果回来之后组件还要再渲染一轮,
    //   那一轮里才可能冒出一句"大概是…"(本地描述算不出来时拿预览结果
    //   兜一句,是最容易写出来的这种错)。
    //   只等请求 ⇒ 断言跑在结果回来之前 ⇒ 白绿。
    await waitFor(() => expect(screen.getByText(/接下来\s*5\s*次/)).toBeInTheDocument())

    // ⚠️ M132:先查"**描述位上有没有节点**",再查具体文案 ——
    //   查文案的话,兜底那句换个措辞("每月 1 号和每周一 03:00"之类)
    //   就查不出来了,而"确实显示了一句猜测"这件事还是发生了 ⇒ 断言白绿。
    //   描述位是组件根节点下的 `Typography.Text`(span.ant-typography);
    //   「接下来 5 次」那句也是 Typography.Text,但它**套在 Spin 容器里**,
    //   不是根节点的直接子节点 ⇒ 按"根节点的直接子节点"筛就能把它排除掉。
    //   `ant-typography-paragraph` 是 defaultExpr 那行(本用例没传 defaultExpr)。
    const root = container.firstElementChild as HTMLElement
    const descNodes = Array.from(root.children).filter(
      (el) =>
        el.classList.contains('ant-typography') &&
        !el.classList.contains('ant-typography-paragraph'),
    )
    expect(descNodes).toHaveLength(0)

    expect(screen.queryByText(/每月每日每星期/)).not.toBeInTheDocument()
    // ⚠️ 只能查**不是预设标签**的那句 ——
    //   「每周一 03:00」「每天 03:00」都是预设按钮的文案,它们本来就在
    expect(screen.queryByText('每月 1 日 03:00')).not.toBeInTheDocument()
  })

  it('英文侧也要出得来(字典对齐,不然切语言就变成键名)', async () => {
    useUIStore.setState({ lang: 'en-US' })

    render(<CronInput value="7,17,27,37,47,57 * * * *" />)

    await waitFor(() =>
      expect(
        screen.getByText(
          'Every hour at minutes 7, 17, 27, 37, 47, 57 on every day of every week of every month',
        ),
      ).toBeInTheDocument(),
    )
  })
})
