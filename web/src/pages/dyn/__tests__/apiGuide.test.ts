/**
 * 「API 接入」说明的内容生成 —— 单测
 *
 * ⚠️ **这份说明是给用户抄走直接调接口的** ——
 *   路径错了 / 权限码错了 / 请求体的键写错了,抄的人调不通,
 *   而**后台没有任何东西会红** —— 弹窗只是把几串字渲染出来,
 *   编译不过、lint 不过、页面不报错。错得毫无声息。
 *   ⇒ 它的正确性只能靠单测守着。
 *
 * ⚠️ **权威在两处,本文件只做对照**:
 *   路径     → server/internal/api/router/router.go 的 biz 组
 *   权限码   → server/internal/pkg/dynx/actions.go(唯一真相)
 *   请求体   → server/internal/pkg/dynx/normalize.go(归一化口径)
 *
 * ⚠️ **本文件不 import 组件** —— 内容生成是纯函数,
 *   组件那一层(弹窗/表格)另有页面级用例覆盖。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { parseGoRoutes, findMatch } from '@/test/apiRouteScan'
import {
  buildCurlExample,
  buildDynEndpoints,
  buildResponseExample,
  buildSamplePayload,
  buildSamplePayloadText,
  buildSdkSnippet,
  dynCollectionBasePath,
  sampleValueFor,
} from '../apiGuide'
import type { DynSchemaField } from '@/types/dyn'

/** 仓库根(web/src/pages/dyn/__tests__ → 上五级) */
const REPO_ROOT = join(__dirname, '..', '..', '..', '..', '..')
/** 后端路由表 */
const ROUTER_GO = join(REPO_ROOT, 'server', 'internal', 'api', 'router', 'router.go')
/** 后端动作清单(唯一真相) */
const ACTIONS_GO = join(REPO_ROOT, 'server', 'internal', 'pkg', 'dynx', 'actions.go')

const ROUTES = parseGoRoutes(readFileSync(ROUTER_GO, 'utf8'))

/** 造一个 schema 字段 */
function field(over: Partial<DynSchemaField> = {}): DynSchemaField {
  return {
    field_key: 'title',
    label: '标题',
    type: 'text',
    required: false,
    options: [],
    default_value: '',
    sort: 0,
    ...over,
  }
}

/**
 * 弹窗里印的路径 → 后端路由模板路径
 *
 * ⚠️ **集合码换成 `:collection`、占位符换成 `:id`** ——
 *   后端路由表里写的是参数名,弹窗里写的是具体集合码,
 *   两边要能对上,得先把两边都换成"参数形态"再比。
 */
function toRouteTemplate(path: string, code: string): string {
  return path.split(`/${code}/`).join('/:collection/').replace(/\{id\}/g, ':id')
}

describe('buildDynEndpoints · 接口清单', () => {
  it('六条接口一个不多一个不少', () => {
    const eps = buildDynEndpoints('demo')
    expect(eps.map((e) => `${e.method} ${e.path}`)).toEqual([
      'GET /biz/dyn/demo/schema',
      'GET /biz/dyn/demo/records',
      'GET /biz/dyn/demo/records/{id}',
      'POST /biz/dyn/demo/records',
      'PATCH /biz/dyn/demo/records/{id}',
      'DELETE /biz/dyn/demo/records/{id}',
    ])
  })

  it('权限码就是 dyn:<集合码>:<动作>', () => {
    // dynx.RecordCode(collection, action) = "dyn:" + collection + ":" + action
    const eps = buildDynEndpoints('demo')
    for (const e of eps) {
      expect(e.perm.startsWith('dyn:demo:')).toBe(true)
      // 集合码不许出现在动作段里(例如 dyn:demo:demo:list)
      expect(e.perm.split(':')).toHaveLength(3)
    }
  })

  it('用到的动作全在后端动作清单里(不能凭空多出一个码)', () => {
    // ⚠️ 后端 actions.go 是唯一真相 —— 多一个动作 ⇒ 那条资源码
    //   在 sys_resource 里根本不存在 ⇒ 角色页没这一行可勾 ⇒
    //   谁也绑不上 ⇒ 接口对所有非超管 403 且**一条错误日志都没有**。
    const go = readFileSync(ACTIONS_GO, 'utf8')
    const known = new Set(
      [...go.matchAll(/Action\w+\s*=\s*"(\w+)"/g)].map((m) => m[1]!),
    )
    expect(known.size).toBeGreaterThan(0)

    const bad: string[] = []
    for (const e of buildDynEndpoints('demo')) {
      const action = e.perm.split(':')[2]!
      if (!known.has(action)) bad.push(e.perm)
    }
    expect(bad).toEqual([])
  })

  it('表单结构接口归 list 管,不另开一个码', () => {
    // ⚠️ dynx/actions.go 的 ActionList 注释里写死了这件事:
    //   字段定义是"渲染列表页"的前提,能看列表却看不到字段 = 一张空表格 + 403。
    const eps = buildDynEndpoints('demo')
    const schema = eps.find((e) => e.path.endsWith('/schema'))
    expect(schema?.perm).toBe('dyn:demo:list')
  })

  it('每条接口的说明键都在同一个命名空间下', () => {
    for (const e of buildDynEndpoints('demo')) {
      expect(e.descKey.startsWith('page.dyn.records.apiGuide.')).toBe(true)
    }
  })

  it('路径与后端 router.go 一一对应(方法 + 路径都要对得上)', () => {
    // ⚠️ 这是本文件最有价值的一条 ——
    //   弹窗里的路径是**给人抄的**,写错了没人会发现:
    //   弹窗照常渲染,页面照常工作,只有抄走的人拿到 404。
    //   后端 biz 组只有这六条路由(见 router.go)。
    const code = 'demo'
    const bad: string[] = []
    for (const e of buildDynEndpoints(code)) {
      const tpl = toRouteTemplate(e.path, code)
      if (!findMatch(ROUTES, e.method, tpl)) {
        bad.push(`${e.method} ${e.path}   ← 后端没有这条路由`)
      }
    }
    expect(bad).toEqual([])
  })

  it('这个网不是空的(后端路由表解析没失效)', () => {
    // 开源抽离裁掉业务路由后,路由表从 200+ 降到 173 —— 阈值跟着新口径走,
    // 这条网守的是「解析器没失效」,不是路由数量。
    expect(ROUTES.length).toBeGreaterThan(150)
    expect(ROUTES.some((r) => r.path === '/rapid-admin/v1/biz/dyn/:collection/records')).toBe(true)
    expect(ROUTES.some((r) => r.path === '/rapid-admin/v1/biz/dyn/:collection/records/:id')).toBe(true)
    expect(ROUTES.some((r) => r.path === '/rapid-admin/v1/biz/dyn/:collection/schema')).toBe(true)
  })
})

describe('sampleValueFor · 示例值要符合后端归一化口径', () => {
  it('整型/小数给数字,开关给布尔', () => {
    expect(sampleValueFor(field({ type: 'int' }))).toBe(0)
    expect(sampleValueFor(field({ type: 'number' }))).toBe(0)
    expect(sampleValueFor(field({ type: 'bool' }))).toBe(false)
  })

  it('日期/日期时间给后端 parseTime 认得的格式', () => {
    // dynx.parseTimeString 认 RFC3339 / "2006-01-02T15:04:05" /
    // "2006-01-02 15:04:05" / "2006-01-02" 四种
    expect(sampleValueFor(field({ type: 'date' }))).toBe('2026-01-01')
    expect(sampleValueFor(field({ type: 'datetime' }))).toBe('2026-01-01T00:00:00Z')
  })

  it('单选/多选取选项集里的第一个值(取不到就是空)', () => {
    const opts = [
      { value: 'a', label: '甲' },
      { value: 'b', label: '乙' },
    ]
    expect(sampleValueFor(field({ type: 'select', options: opts }))).toBe('a')
    expect(sampleValueFor(field({ type: 'multiselect', options: opts }))).toEqual(['a'])
    // 没有选项集时不能瞎编一个值 —— normalizeSelect 会拒
    expect(sampleValueFor(field({ type: 'select', options: [] }))).toBe('')
    expect(sampleValueFor(field({ type: 'multiselect', options: [] }))).toEqual([])
  })

  it('JSON 给对象,文本给尖括号占位', () => {
    expect(sampleValueFor(field({ type: 'json' }))).toEqual({})
    expect(sampleValueFor(field({ type: 'text', field_key: 'row1' }))).toBe('<row1>')
    expect(sampleValueFor(field({ type: 'longtext', field_key: 'body' }))).toBe('<body>')
  })

  /**
   * ⚠️ **附件的示例值必须是"文件 key",不能是 URL** ——
   *   后端 dynx.normalizeFile 拒带域名的值(400「文件路径不能带域名」)。
   *   抄走示例却调不通,比不给示例更糟。
   */
  it('附件给 <file_key> 占位(不是 URL)', () => {
    expect(sampleValueFor(field({ type: 'file', field_key: 'plan' }))).toBe('<file_key>')
  })
})

describe('buildSamplePayload · 请求体的键是字段标识', () => {
  it('键是 field_key,不是物理列名 f_xxx', () => {
    // ⚠️ 发 f_row1 会得到"字段 f_row1 不存在"(400) ——
    //   后端按字段标识找列,物理列名只对排障有意义。
    const payload = buildSamplePayload([field({ field_key: 'row1' }), field({ field_key: 'row2' })])
    expect(Object.keys(payload)).toEqual(['row1', 'row2'])
    expect(payload).not.toHaveProperty('f_row1')
  })

  it('格式化文本是合法 JSON', () => {
    const text = buildSamplePayloadText([field({ field_key: 'row1' })])
    expect(() => JSON.parse(text)).not.toThrow()
    expect(JSON.parse(text)).toEqual({ row1: '<row1>' })
  })
})

describe('buildCurlExample · curl 能直接跑', () => {
  it('请求体压成一行(多行 JSON 塞进 -d 会被 shell 拆成多条命令)', () => {
    const payloadText = buildSamplePayloadText([
      field({ field_key: 'row1' }),
      field({ field_key: 'row2', type: 'int' }),
    ])
    const curl = buildCurlExample('/rapid-admin/v1', 'demo', payloadText)

    const dataLine = curl.split('\n').find((l) => l.includes('-d '))!
    expect(dataLine).toBeDefined()
    // -d '...' 里面不能再有换行
    expect(dataLine).not.toMatch(/\\n/)
    // 压完之后仍是一份合法 JSON
    const json = /-d '(.*)'$/.exec(dataLine)![1]!
    expect(() => JSON.parse(json)).not.toThrow()
    expect(JSON.parse(json)).toEqual({ row1: '<row1>', row2: 0 })
  })

  it('带上了 baseURL、鉴权头和 Content-Type', () => {
    const curl = buildCurlExample('/rapid-admin/v1', 'demo', '{"row1":"x"}')
    expect(curl).toContain("curl -X POST '/rapid-admin/v1/biz/dyn/demo/records'")
    expect(curl).toContain("-H 'Authorization: Bearer <token>'")
    expect(curl).toContain("-H 'Content-Type: application/json'")
  })
})

describe('buildResponseExample · 响应骨架', () => {
  it('画的是骨架不是真数据,但必须含创建人字段', () => {
    const obj = JSON.parse(buildResponseExample())
    expect(obj.code).toBe(0)
    expect(obj.data).toHaveProperty('id')
    expect(obj.data).toHaveProperty('owner_id')
    // ⚠️ owner_name 是本次新增的字段(后端读的时候从 sys_user 补的),
    //   骨架里没有它 ⇒ 用户会以为接口不返创建人。
    expect(obj.data).toHaveProperty('owner_name')
    expect(obj.data).toHaveProperty('created_at')
    expect(obj.data).toHaveProperty('updated_at')
  })
})

describe('buildSdkSnippet · JS SDK 片段', () => {
  it('baseURL 与集合码是真值,包名与 js-sdk/ 对齐', () => {
    const snippet = buildSdkSnippet('/rapid-admin/v1', 'demo')
    expect(snippet).toContain("new DynClient('/rapid-admin/v1')")
    expect(snippet).toContain("client.collection('demo')")
    // ⚠️ 片段里的每个方法调用都得在 js-sdk/src/RecordService.ts 真实存在
    expect(snippet).toContain('.getList(')
    expect(snippet).toContain('.create(')
    expect(snippet).toContain("from '@rapid-admin/dyn-sdk'")
  })

  it('片段里不写死字段标识(字段是运行期的,抄走才知道)', () => {
    const snippet = buildSdkSnippet('/rapid-admin/v1', 'demo')
    expect(snippet).toContain('/* <field_key>: <value> */')
  })
})

describe('dynCollectionBasePath · 基路径', () => {
  it('不带结尾斜杠', () => {
    // ⚠️ 多一个斜杠在多数网关下会被判成 404,
    //   而错误信息指向"路由不存在",跟真正的原因对不上。
    const p = dynCollectionBasePath('demo')
    expect(p).toBe('/biz/dyn/demo')
    expect(p.endsWith('/')).toBe(false)
  })

  it('拼出来的完整路径与 curl 里的一致', () => {
    const curl = buildCurlExample('/rapid-admin/v1', 'demo', '{}')
    expect(curl).toContain(`/rapid-admin/v1${dynCollectionBasePath('demo')}/records`)
  })
})
