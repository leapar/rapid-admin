/**
 * 测试超时预算的回归网 —— M121
 *
 * # 这条守的是什么
 *
 * 满载跑全量套件(99 个文件并行)时,会**随机**红几条页面级用例,
 * 而**单独跑那几个文件全是绿的**。两次全量跑红的还**不是同一批**。
 *
 * 这不是玄学,是**两个超时旋钮有一个没拧**:
 *
 * | 旋钮 | 管谁 | 谁在管 | 本仓的值(改之前) |
 * |---|---|---|---|
 * | `testTimeout` | 整条用例的上限 | vitest | 60s(M106 从 5s 提上来的) |
 * | `asyncUtilTimeout` | **每一次 waitFor 的上限** | @testing-library/dom | **1s(库默认值,从来没配过)** |
 *
 * M106 只动了外层。内层那个 1s 是 @testing-library/dom 的默认值,
 * 本仓 32 个文件、347 处 `waitFor` 全都吃它。
 *
 * 满载时一次 antd 渲染要好几秒(实测单条用例 5~17s),
 * `waitFor` 等满 1s 就放弃,报的是:
 *
 *     Unable to find an element with the text: ...
 *
 * —— 看着像"断言写错了",其实是**机器慢了**。
 * **假红比假绿更坑**:它让人以为"刚改的东西把别的用例搞坏了",
 * 于是去查一个根本不存在的问题。
 *
 * # 为什么这个文件本身是必要的
 *
 * 这两个旋钮都是**配置**,不是代码 —— 改坏了不会有人报错,
 * 只会让套件"随机"红,而"随机红"很容易被解释成"环境问题"然后忽略掉。
 * 所以要有一道网:旋钮被调回去时,这里要红。
 *
 * # 断言怎么写才不是自欺欺人
 *
 * 内层用 **live 读**(`getConfig()` 读的是 @testing-library/dom 的**真实**当前配置),
 * 不是复述 setup.ts 里写的常量 —— 复述常量只能证明"文件里有这行字",
 * 证明不了"它真的生效了"。
 *
 * 外层(`vitest.config.ts` 的 testTimeout)只能**读源码文本**:
 * vitest 没把运行时的 testTimeout 暴露给用例。
 * 这一条是本文件里唯一一条"读文本"的断言,注释里写明了它证明不了什么。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { getConfig } from '@testing-library/react'

/** 内层预算的下限:低于它,满载时 waitFor 就会假红 */
const MIN_ASYNC_UTIL_TIMEOUT = 10_000

/** 外层预算的下限 */
const MIN_TEST_TIMEOUT = 120_000

/**
 * 从 vitest.config.ts 源码里抠出 testTimeout 的值
 *
 * ⚠️ 这一条是**读文本**的断言 —— vitest 没把运行时的 testTimeout
 *   暴露给用例,没有别的办法。
 *   它能证明的是"配置里写着这个数",
 *   证明不了"vitest 真的按它执行"(那要靠实测)。
 */
function declaredTestTimeout(): number {
  const src = readFileSync(join(__dirname, '..', '..', 'vitest.config.ts'), 'utf8')
  const m = src.match(/testTimeout:\s*([\d_]+)/)
  expect(m, 'vitest.config.ts 里找不到 testTimeout —— 是不是被改掉了?').not.toBeNull()
  return Number((m as RegExpMatchArray)[1]?.replace(/_/g, ''))
}

describe('测试超时预算(M121)', () => {
  describe('内层:waitFor 的 asyncUtilTimeout', () => {
    it('已经被显式配到 ≥ 10s —— 不是 @testing-library 的 1s 默认值', () => {
      // live 读真实配置:setup.ts 里的 configure 有没有真的生效,这里说了算
      const cfg = getConfig()
      expect(cfg.asyncUtilTimeout).toBeGreaterThanOrEqual(MIN_ASYNC_UTIL_TIMEOUT)
    })

    it('不是库的默认值 1000ms(默认值 = 没配过)', () => {
      expect(getConfig().asyncUtilTimeout).not.toBe(1000)
    })
  })

  describe('外层:vitest 的 testTimeout', () => {
    it('配到 ≥ 120s —— 满载时单条页面级用例实测会超 60s', () => {
      expect(declaredTestTimeout()).toBeGreaterThanOrEqual(MIN_TEST_TIMEOUT)
    })

    it('内层必须严格小于外层 —— 否则内层那道等于没有', () => {
      // 内层超了还能被外层兜住;反过来(内层比外层还长)就等于没配内层
      expect(getConfig().asyncUtilTimeout).toBeLessThan(declaredTestTimeout())
    })
  })

  describe('不许退回默认值', () => {
    it('testTimeout 不能是 vitest 的默认 5000ms', () => {
      expect(declaredTestTimeout()).not.toBe(5000)
    })
  })
})
