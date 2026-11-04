/**
 * 短信模板审核状态 API —— M115 配套
 * 2026-10-30 新建
 *
 * 路径: 后端 /rapid-admin/v1/platform/sms-templates (router.go 的 platform 组)
 *
 * ⚠️ **停用 / 恢复的 reason 走 query,不是 body** ——
 *   后端 handler 是 `c.Query("reason")`(不是 c.BindJSON),而且
 *   **没有 body 也能提交**(前端允许不填原因)。所以这里必须把 reason
 *   拼进 URL,而不是塞进 body —— 塞 body 后端根本不读,填了等于没填,
 *   而且**不报错**:页面显示"已停用",历史里却没有原因。
 *
 * ⚠️ **URL 模板里的字面量不能拆到变量里** ——
 *   `web/src/api/__tests__/api-route-parity.test.ts` 靠扫 `http.<verb>(...)`
 *   的**第一个实参是不是字面量/模板串**来核对路径打不打得通后端路由。
 *   写成 `http.post(buildUrl(scene))` 的话那条网就扫不到这次调用 ⇒
 *   路径打错会变成"点一下 404,CI 全绿"。所以 URL 一律内联在 http.* 的
 *   第一个实参里。
 */
import { http } from './client'
import type {
  SmsTemplateItem,
  SmsTemplateListView,
  SmsTemplateHistoryItem,
  SmsTemplateSyncResult,
} from '@/types/smsTemplate'

/**
 * 把 reason 拼成 query 片段
 *
 * ⚠️ 空 reason **不带 ? 号**(不是 `?reason=`)—— 后端 `c.Query("reason")`
 *   两种都返空串,但少一个字符少一处分歧。
 *
 * ⚠️ 必须 encodeURIComponent —— reason 是用户手写的中文,
 *   直接拼进 URL 会被浏览器编码成 %E4%B8%AD... ，后端拿到的是编码后的串。
 */
function reasonQuery(reason?: string): string {
  const v = (reason ?? '').trim()
  return v ? `?reason=${encodeURIComponent(v)}` : ''
}

export const smsTemplateApi = {
  /**
   * 四个场景的模板状态 + 守卫策略 + 当前通道
   * 后端: GET /platform/sms-templates
   */
  list: () => http.get<SmsTemplateListView>('/platform/sms-templates'),

  /**
   * 单个场景详情
   * 后端: GET /platform/sms-templates/:scene
   */
  detail: (scene: string) => http.get<SmsTemplateItem>(`/platform/sms-templates/${scene}`),

  /**
   * 流转历史(最新在前)
   * 后端: GET /platform/sms-templates/:scene/history?limit=50
   *
   * ⚠️ 后端把 limit 夹在 [1, 200],默认 50 —— 这里传 50,不传也行,
   *   传了是为了"页面上看到多少条"是确定值,而不是"后端默认值哪天变了
   *   页面长度也跟着变"。
   */
  history: (scene: string, limit = 50) =>
    http.get<SmsTemplateHistoryItem[]>(`/platform/sms-templates/${scene}/history`, { limit }),

  /**
   * 手动跑一轮同步(与定时任务**同一个入口** `smstpl.Runtime.SyncOnce`)
   * 后端: POST /platform/sms-templates/sync
   *
   * ⚠️ 当前通道不支持查询审核状态时(console provider)后端返 400
   *   "当前短信通道(console)不支持查询模板审核状态" —— 这不是故障,
   *   弹提示即可,别当成"同步坏了"。
   */
  sync: () => http.post<SmsTemplateSyncResult>('/platform/sms-templates/sync'),

  /**
   * 人工停用某个场景的模板(止血手段)
   * 后端: POST /platform/sms-templates/:scene/disable?reason=
   *
   * ⚠️ **停用不是"永久"** —— 服务商同步**不会**自动恢复人工停用的模板
   *   (硬规则 2),必须人工点"恢复"。
   */
  disable: (scene: string, reason?: string) =>
    http.post<SmsTemplateItem>(`/platform/sms-templates/${scene}/disable${reasonQuery(reason)}`),

  /**
   * 人工恢复(落到"待审核",**不是**"已通过")
   * 后端: POST /platform/sms-templates/:scene/enable?reason=
   *
   * ⚠️ 人工无权判定通过(硬规则 1)—— 恢复后要等下一轮同步把服务商的
   *   真实结论带回来。所以按钮文案不能写成"启用",得让人知道
   *   "恢复之后还要等服务商确认"。
   */
  enable: (scene: string, reason?: string) =>
    http.post<SmsTemplateItem>(`/platform/sms-templates/${scene}/enable${reasonQuery(reason)}`),
}
