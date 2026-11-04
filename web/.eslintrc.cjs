/**
 * ESLint 配置 —— rapid-admin-web
 *
 * 字典约束(2026-10-18 L2):
 *   - 禁止硬编码 enum 映射(应改用 useDict('groupCode'))
 *   - 与 server/internal/seed/seed.go mustDicts 镜像
 *   - 镜像定义在 web/src/types/dict.ts DictGroupCode
 *
 * 豁免方式:
 *   1. 改用 useDict('<groupCode>')(推荐)
 *   2. 加 `// dict-allow` 行内注释(用于字典管理页本身)
 *   3. 加到 .eslintrc.cjs dictAllowlist 数组(仅限真"非字典 enum"场景)
 */
const FORBIDDEN_DICT_KEYS = [
  // === user/role(后台) ===
  'active', 'disabled', 'locked',
  'platform', 'business',
  // === owner 审核 ===
  'owner_pending', 'owner_approved', 'owner_rejected',
  // === owner 实名认证状态(2026-10-18 加) ===
  'verified', 'need_manual',
  // === material 资料(强制分人,SRS 1.2.5) ===
  'material_draft', 'material_pending', 'material_approved', 'material_published', 'material_revoked',
  'draft', 'published', 'archived', 'review',
  // === issue 问题(简化 P0) ===
  'issue_open', 'issue_in_progress', 'issue_closed',
  'open', 'in_progress', 'closed',
  // === issue type(2026-10-18 加) ===
  'inspection', 'complaint', 'repair',
  // === issue priority(2026-10-18 加) ===
  'low', 'medium', 'high', 'urgent',
  // === notice 通知类型 ===
  'notice_system', 'notice_delivery', 'notice_activity', 'project',
  'system', 'delivery', 'activity',
  // === notice priority(2026-10-18 加) ===
  'normal',
  // === config 配置中心 ===
  'config_pending', 'config_approved', 'config_rejected', 'config_activated',
  'pending', 'approved', 'rejected', 'activated', 'expired',
  // === config value type ===
  'string', 'int', 'bool', 'json', 'secret',
  // === config effective type ===
  'immediate', 'next_restart', 'scheduled',
  // === audit 审计(2026-10-18 加) ===
  'success', 'fail',
]

// 真"非字典 enum"白名单(只在 switch 流程分支、路由常量等场景用)
//   现有:无(2026-10-18 初始)。按需添加,但必须写明原因
const dictAllowlist = [
  // 例子: 'homepage'  // router 常量,非业务 enum
]

const escapedKeys = [...FORBIDDEN_DICT_KEYS, ...dictAllowlist]
  .map((k) => k.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  .join('|')

// esquery 不支持 key.value=/regex/ attribute matcher(实测 8.x 失败)
// 解决方案:为每个 forbidden key 单独写一个 selector,数组形式
//
// selector 关键约束(避免误伤 i18n / type 注解):
//   - 必须出现在"顶层 const xxx = { ... }"中
//   - xxx 名称以 _MAP / _OPTIONS / _LIST / _TABLE 结尾(明显的"映射表"语义)
//   - 排除属性值是函数的 Property(method shorthand)
//
// esquery 不支持 :has 父级 ancestor 链 + regex 组合的优雅写法,
// 退而求其次:用 VariableDeclarator[init=ObjectExpression] 收紧
//   - 1) const xxx_MAP = { active: { ... } } → 拦
//   - 2) type 注解 Array<{ active: ... }> → init 不是 ObjectExpression,不拦
//   - 3) i18n locale 文件 bindingStatus: { pending: 'Pending' } 在 ObjectExpression value
//      里,init 是 ObjectExpression 但其属性值是字符串 Literal,value 不是 ObjectExpression,不拦
// 限定变量名以 _MAP / _OPTIONS / _LIST / _TABLE 结尾(明显的"映射表"语义)
const dictVarNamePattern = '(_MAP|_OPTIONS|_LIST|_TABLE|_DICT|_TYPE|_STATUS)$'

// 关键收紧:Property 的 value 必须是 ObjectExpression(嵌套对象),i18n 那种 string value 才不会误伤
const identKeySelector = `VariableDeclarator[init.type="ObjectExpression"][id.name=/${dictVarNamePattern}/] > ObjectExpression > Property[key.type="Identifier"][key.name=/^(?:${escapedKeys})$/][value.type="ObjectExpression"]`

// Literal key(带引号字符串)需要逐个展开
const literalKeySelectors = [...FORBIDDEN_DICT_KEYS, ...dictAllowlist].map(
  (k) => `VariableDeclarator[init.type="ObjectExpression"][id.name=/${dictVarNamePattern}/] > ObjectExpression > Property[key.type="Literal"][key.value="${k}"][value.type="ObjectExpression"]`
)

module.exports = {
  root: true,
  env: { browser: true, es2022: true, node: true },
  extends: [
    'eslint:recommended',
    'plugin:@typescript-eslint/recommended',
    'plugin:react/recommended',
    'plugin:react/jsx-runtime',
    'plugin:react-hooks/recommended',
    'prettier',
  ],
  ignorePatterns: ['dist', 'node_modules', '.eslintrc.cjs', 'vite.config.ts', 'src/__dict_test__/**'],
  parser: '@typescript-eslint/parser',
  parserOptions: {
    ecmaVersion: 'latest',
    sourceType: 'module',
    ecmaFeatures: { jsx: true },
  },
  settings: { react: { version: '18.3' } },
  plugins: ['react-refresh', '@typescript-eslint', 'react', 'react-hooks'],
  rules: {
    'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
    '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    '@typescript-eslint/no-explicit-any': 'warn',
    '@typescript-eslint/consistent-type-imports': ['error', { prefer: 'type-imports' }],
    'react/prop-types': 'off',
    'no-console': ['warn', { allow: ['warn', 'error', 'info'] }],
    'no-debugger': 'warn',
    'prefer-const': 'warn',
    'eqeqeq': ['error', 'always'],

    // === 字典约束 L2(2026-10-18 新增)===
    // 禁止 ObjectExpression 内出现以 dict enum key 为键、且 value 是 ObjectExpression 的属性
    //   触发模式:`{ active: { color: 'green' }, disabled: {...} }` 这种 STATUS_MAP 风格
    //   期望:改用 useDict('<groupCode>'),参见 web/src/hooks/useDict.ts
    //
    // 实际工程里 99% 是简写 key(active: { ... } 形式,无引号),用 identKeySelector 一发命中
    // 带引号 'active': { ... } 形式虽然少见,也为每个 key 单独展开 selector
    'no-restricted-syntax': [
      'error',
      {
        selector: identKeySelector,
        message: '❌ 硬编码 enum 映射。改用 useDict("<groupCode>")(web/src/hooks/useDict.ts),字典清单见 web/src/types/dict.ts。若真需豁免加 // dict-allow 注释',
      },
      ...literalKeySelectors.map((sel) => ({
        selector: sel,
        message: '❌ 硬编码 enum 映射(引号 key)。改用 useDict("<groupCode>"),若真需豁免加 // dict-allow 注释',
      })),
    ],
  },
}
