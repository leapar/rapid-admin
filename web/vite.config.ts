import { defineConfig, loadEnv, type UserConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// Vite 配置 —— 后台管理平台前端
// 技术栈: Vite 5 + React 18 + TypeScript 5 + Antd 5
// 后端代理: 开发环境 /rapid-admin/v1/* -> Hertz 后端 (默认 8888 端口)
//
// ⚠️ **配置体抽成 createViteConfig 工厂,不是随手重构** ——
//   vitest.config.ts 要复用同一份配置(尤其是 `@` 别名)。
//   别名在两处各写一遍 = 迟早漂移:改了 vite.config.ts 忘了改 vitest.config.ts,
//   于是单测里 `@/` 解析到别处或解析失败,而**构建照常通过** ——
//   探针失灵,测试变成假绿。
//   抽工厂之后,别名只有这一个来源。
export function createViteConfig(mode: string): UserConfig {
  const env = loadEnv(mode, process.cwd(), '')

  return {
    plugins: [react()],

    // 路径别名
    resolve: {
      alias: {
        '@': path.resolve(__dirname, 'src'),
      },
    },

    // 开发服务器
    server: {
      port: 5173,
      host: '0.0.0.0',
      open: true,
      // 后端代理(开发环境跨域解决)
      proxy: {
        '/rapid-admin/v1': {
          target: env.VITE_API_PROXY_TARGET || 'http://localhost:8888',
          changeOrigin: true,
          // 不重写路径,直接转发 /rapid-admin/v1/* 到后端
        },
        '/api/v1': {
          target: env.VITE_API_PROXY_TARGET || 'http://localhost:8888',
          changeOrigin: true,
        },
        // OSS / 文件下载(开发期可能需要)
        '/oss': {
          target: env.VITE_OSS_PROXY_TARGET || 'http://localhost:8888',
          changeOrigin: true,
        },
        /**
         * ⚠️ **文件静态直链的挂载点 —— 不加这条,开发期全是裂图,而且零报错。**
         *
         * 单桶单树模型下,local provider 给的直链是**同源相对路径**
         * `/rapid-admin/...`(后端把整棵树挂在 `/rapid-admin` 上)。前端页面在
         * vite(5173),后端在 8888,于是这个相对路径打到的是 **vite 自己** ——
         * 而 vite 的 SPA fallback 对未知路径返回的是 **index.html + 200**。
         *
         * ⚠️ **200 + HTML 在 `<img src>` 里不报错**:浏览器把它当"解码失败",
         * 表现为图片裂开,控制台最多一条 decode 报错,后端日志里**一次请求都没有**
         * (请求根本没到后端)。粘贴到地址栏更坑:看到的是登录页(SPA 认路由),
         * 很容易被当成"404"。
         *
         * 所以这条代理是**必需项**,不是"开发期方便"。
         *
         * ⚠️ 前缀必须跟后端 `file.path_root` 一致(默认 `rapid-admin`)—— 那边改了
         *   这边也要改。改后端那个键时,同步改这里。static-mount-proxy.test.ts 盯着
         *   这件事(它读的是后端 config.go 的默认值,不是抄一份)。
         */
        '/rapid-admin': {
          target: env.VITE_API_PROXY_TARGET || 'http://localhost:8888',
          changeOrigin: true,
          // 不重写路径 —— 后端认的就是 `/rapid-admin/...` 这个完整 key
        },
        /**
         * ⚠️ **Swagger UI 的挂载点 —— 不加这条,开发期点「API 文档」看到的是登录页。**
         *
         * Swagger UI 由**后端**挂在 `/swagger` 下(不是前端的一部分),
         * 而开发期页面在 vite(5173)、后端在 8888 —— 两个 origin。
         *
         * 不加这条代理的后果**不是报错**,而是更迷惑的东西:
         * `/swagger/index.html` 打到 vite 自己 ⇒ vite 的 **SPA fallback**
         * 对未知路径返回 **index.html + 200** ⇒ 浏览器把**后台的登录页**
         * 当成 Swagger UI 显示出来。用户看到的是一个登录框,完全不知道
         * 「文档」其实在另一个进程上。
         *
         * 生产环境同源部署(Caddy 把整个域名 reverse_proxy 给后端),
         * 这条代理就是它在开发期的等价物。
         *
         * ⚠️ 前缀必须跟后端 `swaggerx.Mount(h, "/swagger", ...)` 一致 ——
         *   改后端挂载点要同步改这里。swagger-proxy.test.ts 盯着这件事
         *   (它读的是后端 router.go 的挂载点,不是抄一份)。
         */
        '/swagger': {
          target: env.VITE_API_PROXY_TARGET || 'http://localhost:8888',
          changeOrigin: true,
          // 不重写路径 —— 后端认的就是 `/swagger/...` 这个完整路径
        },
        /**
         * ⚠️ **GitBook 手册的挂载点 —— 不加这条,开发期点「使用教程」看到的是 404。**
         *
         * GitBook 由**后端**挂在 server 根(`/docs/*`,router.go 的
         * `docs.Mount(h, "/docs", ...)`,go:embed server/docs/site)。
         * 不加代理的后果:浏览器直连 vite 自己,`/docs` 不是前端路由
         * ⇒ 直接 404(比 /swagger 的"假登录页"诚实,但一样是坏的)。
         * (2026-11-03 用户直报:使用教程跳 localhost:5173/docs/ 是 404。)
         *
         * ⚠️ 前缀必须跟后端 `docs.Mount` 的挂载点一致 —— 改后端挂载点要
         *   同步改这里。docs-proxy.test.ts 盯着这件事(读的是后端
         *   router.go 的挂载点,不是抄一份)。
         */
        '/docs': {
          target: env.VITE_API_PROXY_TARGET || 'http://localhost:8888',
          changeOrigin: true,
          // 不重写路径 —— 后端认的就是 `/docs/...` 这个完整路径
        },
      },
    },

    // 生产构建
    build: {
      target: 'es2020',
      outDir: 'dist',
      assetsDir: 'assets',
      sourcemap: mode === 'development',
      // 拆包策略(把第三方库拆出,减少单文件体积)
      rollupOptions: {
        output: {
          manualChunks: {
            'react-vendor': ['react', 'react-dom', 'react-router-dom'],
            'antd-vendor': ['antd', '@ant-design/icons'],
            'echarts-vendor': ['echarts', 'echarts-for-react'],
            'utils-vendor': ['axios', 'dayjs', 'zustand'],
          },
          // 静态资源分类目录
          chunkFileNames: 'assets/js/[name]-[hash].js',
          entryFileNames: 'assets/js/[name]-[hash].js',
          assetFileNames: (assetInfo) => {
            const name = assetInfo.name?.split('.')?.pop() ?? ''
            if (name.match(/(png|jpg|jpeg|gif|svg|webp)$/)) {
              return 'assets/img/[name]-[hash][extname]'
            }
            if (name.match(/(woff|woff2|ttf)$/)) {
              return 'assets/fonts/[name]-[hash][extname]'
            }
            if (name.match(/(css)$/)) {
              return 'assets/css/[name]-[hash][extname]'
            }
            return 'assets/[name]-[hash][extname]'
          },
        },
      },
      // 超过 2MB 警告
      chunkSizeWarningLimit: 2000,
    },

    // CSS 配置
    css: {
      modules: {
        localsConvention: 'camelCaseOnly',
      },
      preprocessorOptions: {
        less: {
          // Antd 5 主题变量注入
          javascriptEnabled: true,
        },
      },
    },

    // 全局常量
    define: {
      __APP__: JSON.stringify(env.VITE_APP_NAME || 'rapid-admin'),
      __VERSION__: JSON.stringify(process.env.npm_package_version || '1.0.0'),
    },

    // 优化依赖
    optimizeDeps: {
      include: ['react', 'react-dom', 'react-router-dom', 'antd', 'axios', 'zustand'],
    },
  }
}

export default defineConfig(({ mode }) => createViteConfig(mode))
