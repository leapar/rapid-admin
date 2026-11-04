/**
 * 「点整行进入」的**保护** —— 判定这次点击该不该被行 / 卡片处理器接住
 *
 * ## 为什么需要它
 *
 * 给列表加"点整行进入"很便宜,但**加错了就是事故**:
 * 行里还坐着「删除 / 详情 / 新建子文件夹」这些按钮,以及名字链接、
 * 封面缩略图(点它会放大预览)、若干标签 —— 它们的点击**会冒泡到行上**。
 * 不挡一下的话,用户点「删除」会顺带跳进那个文件夹:
 * 确认弹窗一闪就没,层级还多了一层 ⇒ 用户看着就是"按钮坏了"。
 *
 * ## 判据是 DOM 祖先链(`closest`),不是 React 组件树
 *
 * 下拉菜单(卡片右上角的「⋯」)是 **portal** 渲染的 —— DOM 上它不在行 / 卡片里,
 * 但 **React 事件沿组件树冒泡** ⇒ 菜单项的点击照样会冒到行 / 卡片的 onClick 上。
 * 按 DOM 祖先链判,`closest('.ant-dropdown')` 正好把这类"看着不相干、
 * 却真的冒上来"的点击挡住。
 *
 * ## 为什么单独一个文件
 *
 * 纯函数不能跟组件同文件(`react-refresh/only-export-components`,
 * 而本仓 lint 是 `--max-warnings 0`)。同 `rowAction.ts` 一个道理。
 */
import type { HTMLAttributes, MouseEvent } from 'react'

/** 有 `closest` 的东西 —— 测试里会拿普通对象 / 假元素来试 */
interface ClosestCapable {
  closest?: (selector: string) => Element | null
}

/**
 * "点它自己就有意义"的元素 —— CSS 选择器
 *
 * ⚠️ `.ant-dropdown` 必须在里面(理由见文件头)。
 */
const INTERACTIVE_SELECTOR = [
  'a', // 名字链接 / 面包屑
  'button', // 行内操作按钮 / 卡片右上角的「⋯」
  'input',
  'textarea',
  'select',
  'label',
  '[role="button"]',
  '[contenteditable="true"]',
  '.ant-image', // 封面缩略图(点它会放大预览)
  '.ant-tag', // 标签(所在目录 / 默认 / 数量)
  '.ant-checkbox-wrapper', // 行选择框
  '.ant-radio-wrapper',
  '.ant-switch',
  '.ant-dropdown', // 「⋯」菜单展开出来的浮层(portal)
].join(',')

/**
 * 这次点击是不是落在"点它自己就有意义"的元素上?
 *
 * · `true`  → **不该**由行 / 卡片处理器接住(让它自己处理)
 * · `false` → 可以接住(行 / 卡片进入)
 *
 * ⚠️ **判据是 DOM 祖先链,不是 React 组件树** —— 见文件头。
 */
export function isInteractiveTarget(target: EventTarget | null): boolean {
  if (!target) return false
  const el = target as unknown as ClosestCapable
  const closest = el.closest
  // ⚠️ `closest.call(el, …)` 而不是 `el.closest(…)` ——
  //   取出来再 call,`this` 才还是那个元素;顺便躲开 TS 的可选属性收窄。
  if (typeof closest !== 'function') return false
  return closest.call(el, INTERACTIVE_SELECTOR) !== null
}

/**
 * 「点整行进入」的行 props —— 直接交给 `<Table onRow={…}>`
 *
 * ⚠️ **antd 5 的表格没有默认的行悬停底色** —— `rowHoverBg` 只是个
 *   component token(antd/es/table/style/index.js:205),**没有落成
 *   任何 CSS 规则**。所以"这一行可以点"必须自己给两个信号:
 *   `cursor: pointer`(行内)+ 悬停底色(见 `styles/global.css` 的
 *   `.row-clickable`)。只做前者的话,用户根本看不出哪一行能点。
 *
 * @param enter 点这一行时做什么(通常是"进入这个文件夹")
 */
export function enterOnRow<T>(enter: (row: T) => void) {
  return (row: T): HTMLAttributes<HTMLElement> => ({
    onClick: (e: MouseEvent<HTMLElement>) => {
      // ⚠️ **行内自带交互的元素(按钮 / 名字链接 / 封面预览 / 标签)不接** ——
      //   否则点「删除」会顺带把文件夹打开(见文件头)。
      if (isInteractiveTarget(e.target)) return
      enter(row)
    },
    style: { cursor: 'pointer' },
    className: 'row-clickable',
  })
}
