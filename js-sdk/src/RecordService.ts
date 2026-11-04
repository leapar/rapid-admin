/**
 * 集合记录服务 —— 一个实例对应一个集合的数据面
 *
 * # 路径与后端一一对应(router.go,勿改拼写)
 *
 * 登录态(正常数据面):
 *
 *	GET    /rapid-admin/v1/biz/dyn/:collection/schema
 *	GET    /rapid-admin/v1/biz/dyn/:collection/records
 *	GET    /rapid-admin/v1/biz/dyn/:collection/records/:id
 *	POST   /rapid-admin/v1/biz/dyn/:collection/records
 *	PATCH  /rapid-admin/v1/biz/dyn/:collection/records/:id
 *	DELETE /rapid-admin/v1/biz/dyn/:collection/records/:id
 *	PUT    /rapid-admin/v1/biz/dyn/:collection/records/:id/restore
 *	POST   /rapid-admin/v1/biz/dyn/:collection/records/:id/approval
 *	POST   /rapid-admin/v1/biz/dyn/:collection/records/batch-delete
 *	POST   /rapid-admin/v1/biz/dyn/:collection/records/batch-update
 *
 * 未登录(公开只读镜像,仅当集合开了 public_read;2026-11-03):
 *
 *	GET    /rapid-admin/v1/public/dyn/:collection/schema
 *	GET    /rapid-admin/v1/public/dyn/:collection/records
 *	GET    /rapid-admin/v1/public/dyn/:collection/records/:id
 *
 * 每个动作的权限码见 README(权限在服务端判,SDK 只负责把 403 的
 * 业务信息原样抛出来,不吞、不猜)。
 */
import type { DynClient } from './Client'
import {
  type CollectionSchema,
  type RecordList,
  type RecordListOptions,
  type RecordModel,
} from './types'

export class RecordService {
  constructor(
    private client: DynClient,
    /** 集合编码(用户在设计器里起的那个) */
    public readonly collection: string,
  ) {}

  /** 集合数据面根(相对 baseURL,不含 /records) */
  protected collectionRoot(): string {
    // ⚠️ **编码要 encode** —— 集合编码虽然限制为安全字符,但那是后端校验的事,
    // SDK 自己把每一层路径段都转义是零成本的防线。
    return `/biz/dyn/${encodeURIComponent(this.collection)}`
  }

  /** 记录集合基路径(相对 baseURL) */
  protected baseCRUDPath(): string {
    return `${this.collectionRoot()}/records`
  }

  /** 公开只读镜像根(仅读;后端按集合的 public_read 开关放行) */
  private publicBase(): string {
    return `/public/dyn/${encodeURIComponent(this.collection)}`
  }

  /**
   * 读动作按登录态选根:未登录走公开只读镜像,登录走正常数据面。
   *
   * ⚠️ **只切 GET,写动作永远走正常数据面** —— 公开路径没有写,
   * 集合没开 public_read 时匿名读公开镜像会得到 401(不区分
   * "不存在"和"没公开",是服务端的口径)。
   */
  private readRoot(): string {
    return this.client.authStore.isValid ? this.collectionRoot() : this.publicBase()
  }

  /** 读记录的基路径(两棵树上 list / getOne 都挂在 /records 下) */
  private readRecordsBase(): string {
    return `${this.readRoot()}/records`
  }

  /** 集合结构(字段标识 / 类型 / 选项)。表单要动态渲染就先调这个。 */
  async getSchema(options?: { signal?: AbortSignal }): Promise<CollectionSchema> {
    return this.client.send(`${this.readRoot()}/schema`, {
      method: 'GET',
      signal: options?.signal,
    })
  }

  /** 分页列表 */
  async getList(options?: RecordListOptions & { signal?: AbortSignal }): Promise<RecordList> {
    return this.client.send(this.readRecordsBase(), {
      method: 'GET',
      query: buildListQuery(options),
      signal: options?.signal,
    })
  }

  /**
   * 自动翻页拉全量(按 total 循环 getList,直到取完)。
   * ⚠️ 大集合会拉很多页 —— 只是导数据 / 对账用的,列表页请用 getList。
   */
  async getFullList(
    options?: Omit<RecordListOptions, 'page' | 'skipTotal'> & { signal?: AbortSignal; batchSize?: number },
  ): Promise<RecordModel[]> {
    const { batchSize = 200, signal, ...rest } = options ?? {}
    const out: RecordModel[] = []
    for (let page = 1; ; page++) {
      const res = await this.getList({ ...rest, page, size: batchSize, signal })
      out.push(...res.list)
      if (out.length >= res.total || res.list.length === 0) {
        break
      }
    }
    return out
  }

  /** 取一条(不存在 = 404) */
  async getOne(id: string, options?: { signal?: AbortSignal }): Promise<RecordModel> {
    return this.client.send(`${this.readRecordsBase()}/${encodeURIComponent(id)}`, {
      method: 'GET',
      signal: options?.signal,
    })
  }

  /**
   * 新建,返回新记录的 id。
   *
   * ⚠️ **body 的键是字段标识(field_key),不是显示名** ——
   * 发显示名会得到"字段 xxx 不存在"(400)。
   */
  async create(
    body: RecordModel,
    options?: { signal?: AbortSignal },
  ): Promise<{ id: string }> {
    return this.client.send(this.baseCRUDPath(), {
      method: 'POST',
      body,
      signal: options?.signal,
    })
  }

  /** 更新(只改请求里出现的字段),返回 { id } */
  async update(
    id: string,
    body: RecordModel,
    options?: { signal?: AbortSignal },
  ): Promise<{ id: string }> {
    return this.client.send(`${this.baseCRUDPath()}/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body,
      signal: options?.signal,
    })
  }

  /** 删除(软删,进回收站;要恢复用 restore) */
  async delete(id: string, options?: { signal?: AbortSignal }): Promise<void> {
    await this.client.send(`${this.baseCRUDPath()}/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal: options?.signal,
    })
  }

  /**
   * 从回收站恢复。
   * ⚠️ 没删过的记录调恢复是 404 —— "点了没反应"先查这条是否真的在回收站。
   */
  async restore(id: string, options?: { signal?: AbortSignal }): Promise<void> {
    await this.client.send(`${this.baseCRUDPath()}/${encodeURIComponent(id)}/restore`, {
      method: 'PUT',
      signal: options?.signal,
    })
  }

  /** 送审(集合绑定了审批模板才有意义;权限沿用 update)。返回审批实例 id。 */
  async submitApproval(
    id: string,
    options?: { signal?: AbortSignal },
  ): Promise<{ id: string }> {
    return this.client.send(`${this.baseCRUDPath()}/${encodeURIComponent(id)}/approval`, {
      method: 'POST',
      signal: options?.signal,
    })
  }

  /** 批量删除,返回删除条数 */
  async batchDelete(
    ids: string[],
    options?: { signal?: AbortSignal },
  ): Promise<{ count: number }> {
    return this.client.send(`${this.baseCRUDPath()}/batch-delete`, {
      method: 'POST',
      body: { ids },
      signal: options?.signal,
    })
  }

  /**
   * 批量改**一个**字段,返回改了多少条。
   * ⚠️ 服务端一次只收一个字段(field + value),要改多个字段请逐条 update。
   */
  async batchUpdate(
    ids: string[],
    field: string,
    value: unknown,
    options?: { signal?: AbortSignal },
  ): Promise<{ count: number }> {
    return this.client.send(`${this.baseCRUDPath()}/batch-update`, {
      method: 'POST',
      body: { ids, field, value },
      signal: options?.signal,
    })
  }
}

/**
 * list 查询参数 → 查询串
 *
 * ⚠️ **空值不进查询串** —— `keyword=` 这种空参数后端当"没给"处理没问题,
 * 但 `f_xxx=` 会被当"按空值筛"(语义完全不同),统一从源头掐掉。
 */
export function buildListQuery(options?: RecordListOptions): Record<string, string> {
  if (!options) {
    return {}
  }
  const q: Record<string, string> = {}
  const set = (key: string, value: unknown) => {
    if (value !== undefined && value !== null && value !== '') {
      q[key] = String(value)
    }
  }
  set('page', options.page)
  set('size', options.size)
  set('keyword', options.keyword)
  set('filter', options.filter)
  set('order_by', options.orderBy)
  set('order_dir', options.orderDir)
  set('owner', options.owner)
  if (options.skipTotal) {
    q.skip_total = '1'
  }
  if (options.trash) {
    q.trash = '1'
  }
  if (options.fields) {
    const fields = Array.isArray(options.fields)
      ? options.fields.filter((f) => f !== '')
      : [options.fields]
    if (fields.length > 0) {
      q.fields = fields.join(',')
    }
  }
  // 简单筛选:f_<字段>=v1,v2(多值逗号分隔,与后端 dynRecordQueryFromQuery 对齐)
  for (const [key, value] of Object.entries(options.filters ?? {})) {
    const values = (Array.isArray(value) ? value : [value]).filter((v) => v !== '')
    if (key !== '' && values.length > 0) {
      q[`f_${key}`] = values.join(',')
    }
  }
  return q
}
