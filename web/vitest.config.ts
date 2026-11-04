/**
 * Vitest 配置 —— 后台管理平台前端单测
 *
 * ⚠️ **为什么单独一个文件,而不是往 vite.config.ts 里塞 `test` 段**:
 *   vitest 的 `defineConfig` 从 'vitest/config' 导入,而 vite.config.ts 用的是
 *   'vite' 的 —— 如果把 test 段塞进 vite.config.ts,那个文件就得依赖 vitest,
 *   于是 `pnpm install --prod` 之后 `vite build` 会因为解析不到 vitest 直接炸。
 *   生产构建不该被测试框架绑架。
 *
 * ⚠️ **配置体复用 vite.config.ts 的 createViteConfig 工厂**:
 *   路径别名(`@` → src)只写一份。两处各写一遍必然漂移,而漂移的后果是
 *   **构建通过、测试却解析不到模块** —— 探针失灵,测试变假绿。
 */
import { realpathSync } from 'node:fs'
import { defineConfig, mergeConfig } from 'vitest/config'
import { createViteConfig } from './vite.config'

/**
 * ⚠️ **root 必须取「规范化」的绝对路径,不能用 process.cwd()** ——
 *   这是本文件里最容易被后人「顺手改回去」的一行,改回去会直接让整个套件变红。
 *
 * 现象(Windows 专属):
 *   在仓库目录(小写盘符 `d:\...`)下跑测试 → 3 个组件用例全红:
 *     TypeError: Cannot read properties of null (reading 'useMemo')
 *     Warning: Invalid hook call. Hooks can only be called inside of ...
 *   把同一个目录写成大写盘符 `D:\...` → 全绿。
 *
 * 根因(逐条实测过,不是推测):
 *   1. vite 解析应用源码里的 `import 'react'` 时,拼出来的路径沿用 **root 字符串
 *      的大小写** → `d:\...\node_modules\.pnpm\react@18.3.0\...\react.development.js`
 *   2. 而 react-dom 内部的 `require('react')` 走的是 **Node 的 CJS 解析器**,
 *      它会 realpath 穿过 pnpm 建的 junction → 拿到 **盘符大写** 的路径:
 *      `D:\...\node_modules\.pnpm\react@18.3.0\...\react.development.js`
 *   3. **同一个文件、两个路径字符串** → Node 的模块缓存按路径字符串建键 →
 *      加载了**两份 react** → react-dom 给 A 派发 hook,A 的 dispatcher 恒为 null
 *      → 任何用到 hook 的组件在渲染时炸
 *
 * 为什么不是「多装了一份 react」:node_modules 里只有一份 react@18.3.0,
 * 两份纯粹是**同一个文件被当成两个模块**。
 *
 * 修法:把 root 固定成 `fs.realpathSync.native()` 的结果。
 *   - `realpathSync.native` = Node 内部 CJS 解析器用的那套 realpath,
 *     会把**盘符和每一段目录**都还原成磁盘上的真实大小写(实测 `d:` → `D:`)
 *   - `realpathSync`(JS 版)会**保留传入时的写法**(`d:` 还是 `d:`)—— 用它无效
 *   - `path.resolve()` 同样保留写法 —— 用它也无效
 *
 * 取 `__dirname`(配置文件自身所在目录)而不是 `process.cwd()`:
 *   万一从仓库根目录跑 `pnpm --filter web test`,cwd 不是 web/,root 会跑偏。
 */
const CANONICAL_ROOT = realpathSync.native(__dirname)

export default mergeConfig(
  // 复用构建配置(别名 / 插件 / define)
  createViteConfig('test'),
  defineConfig({
    // 覆盖 vite.config.ts 里隐含的 root(= process.cwd()),见上方说明
    root: CANONICAL_ROOT,

    test: {
      // DOM 环境:组件测试要 jsdom
      environment: 'jsdom',
      environmentOptions: {
        jsdom: {
          // 与开发服务器同源 —— 有的代码读 location.origin 判断环境
          url: 'http://localhost:5173/',
        },
      },

      // ⚠️ **不开启 globals** —— 测试里显式 `import { describe, it, expect } from 'vitest'`。
      //   开了 globals 还要在 tsconfig 里加 "vitest/globals" 类型,
      //   而全局名会掩盖「这个 describe 从哪来」,TS 也少一道编译期检查。
      globals: false,

      setupFiles: ['./src/test/setup.ts'],

      /**
       * ⚠️ **单条用例的超时:120s(2026-10-30 M121 从 60s 提上来)** —— 不是拍脑袋,是量出来的。
       *
       * 之前用的是 vitest 默认的 **5s**,而这个仓里有几份**页面级**用例
       * (antd Modal + Select + Tree 在 jsdom 里真渲染)单条就要 **10~45s**:
       *   · `src/pages/system/user/__tests__/index.test.tsx` 11 条 = 61s(单独跑)
       *   · `src/pages/system/role/__tests__/AssignResourcesDrawer.test.tsx` 7 条 = 30s(单独跑)
       * 原因不是用例写得笨:jsdom 里 antd 的 CSS-motion / 焦点陷阱 / rc-select
       * 都要靠真定时器推进,而 @testing-library 的 `waitFor` 又是按真实时间轮询 ——
       * **机器一忙,每一步都跟着变慢**。
       *
       * 后果是**假红**:全量跑(99 个文件并行)时,随机一两条页面级用例撞上限就报
       * "Test timed out" —— 单独跑同一个文件是绿的。
       * **假红比假绿更坑**:它让人以为"刚改的东西把别的用例搞坏了"。
       *
       * ⚠️ **60s 不够(M121 实测)**:60s 是按"单独跑"量出来的(最差 46s + 30% 余量),
       *   但**全量并行**时同一个文件要慢好几倍 ——
       *   2026-10-30 两次全量跑实测:
       *     · `src/pages/system/user/__tests__/index.test.tsx` 11 条 = **101s**(4 个文件并行时)
       *     · `src/pages/system/permission/__tests__/resourceEditFields.test.tsx` 14 条 = **126s**
       *   两次全量跑各有 4 条用例报 "Test timed out in 60000ms",
       *   而**这 4 个文件单独跑全是绿的**(48/48)。
       *   ⇒ 60s 是按"不并行"定的,拿去扛 99 文件并行必然不够。
       *   现在按**满载实测值**(单条 17s ~ 整文件 126s)留 2 倍余量 → 120s。
       *
       * ⚠️ **内层还有一道**:@testing-library 的 `waitFor` 有自己的超时
       *   (默认 1000ms),那是**另一个旋钮**,M106 没管到它 ——
       *   满载时先炸的其实是它。见 `src/test/setup.ts` 里的 `configure({ asyncUtilTimeout })`。
       *   **两个旋钮要一起看**:内层超了还能被外层兜住,反过来不行 ⇒ 内层(10s)必须 < 外层(120s)。
       *
       * ⚠️ **只有这一处决策点**:以前有 4 个文件各自写着 `}, 30000)`
       *   (approval / flow / permission / user 三个 describe)—— 那会**盖掉**
       *   全局值,于是全局改了也没用。现在这些冗余覆盖全部删掉,
       *   超时只有一个真源。**要调就调这里。**
       */
      testTimeout: 120_000,
      hookTimeout: 120_000,

      // 只收 src 下的测试文件(node_modules / dist 由 default exclude 挡掉)
      include: ['src/**/*.{test,spec}.{ts,tsx}'],

      // CSS 不参与处理(antd 5 是 CSS-in-JS,不需要真实样式表)
      css: false,

      /**
       * ⚠️ **关掉依赖预打包(optimizer)** —— 这是本配置里最容易踩的一个坑。
       *
       * vite.config.ts 里有 `optimizeDeps.include: ['react', 'react-dom', ...]`
       * (开发服务器预打包用)。本文件 merge 了那份配置,于是**测试环境也**
       * 会把 react / react-dom 预打包成独立 chunk:
       *
       *   · 应用代码 `import { useMemo } from 'react'` → 走预打包产物(实例 A)
       *   · react-dom 内部 require('react')          → 走原始包(实例 B)
       *
       * A ≠ B ⇒ react-dom 渲染时给 A 派发 hook,A 的 dispatcher 一直是 null:
       *
       *   TypeError: Cannot read properties of null (reading 'useMemo')
       *   Warning: Invalid hook call. Hooks can only be called inside of ...
       *
       * ⚠️ **web 和 ssr 两个开关都要关** —— vitest 跑的是 SSR 管线,
       *   只关 web 不管用(我第一次就漏了 ssr,现象照旧)。
       *   关掉之后 react / react-dom 都走原生 require,全局只有一份。
       *
       * ⚠️ **别再叠加 `server.deps.inline` 去"补一刀"** —— 实测无效:
       *   react-dom 被 inline 之后,@testing-library/react 仍然是外部包,
       *   它照样会用 Node 的 CJS 解析器再加载一份 react,双 React 照旧。
       *   真正的根因是**路径大小写**,见上面 CANONICAL_ROOT。
       */
      deps: {
        optimizer: {
          web: { enabled: false },
          ssr: { enabled: false },
        },
      },

      // 每个用例之间自动恢复 spy —— 防止上一个用例的 mock 漏到下一个
      restoreMocks: true,

      coverage: {
        provider: 'v8',
        reporter: ['text', 'html', 'lcov'],
        include: ['src/**/*.{ts,tsx}'],
        exclude: [
          'src/test/**',
          'src/**/__tests__/**',
          'src/**/*.d.ts',
          'src/main.tsx',
          'src/vite-env.d.ts',
        ],
        // ⚠️ **暂时不设阈值** —— 现在只有 1 个模块有测试,
        //   阈值定在 70% 的话 `test:coverage` 必红,那是噪音不是信号。
        //   等覆盖率基线建立(测试铺到各模块)之后再定,到时写进这一段。
      },
    },
  }),
)
