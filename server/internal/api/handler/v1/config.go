// 平台配置中心 handler
// 2026-10-18 收窄:仅保留 update 通道
//   - PATCH /configs/:id (DirectUpdate) — UI 表单保存
//   - GET 类(列表/详情/groups/audits/export-logs/stats/health) — 只读
//   - 删除:DirectCreate / DirectDelete / DirectUpsert / GetConfigPlain / ReloadConfig
//          / ImportPreview / Import / Export / KeysRotation + ListHotReload
//   - 删除:ConfigChangeRequest workflow 死函数(handler 层 9 个全清)
package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/config"
	"rapid-admin/internal/service"
)

type ConfigHandler struct{ *handler.BaseHandler }

func NewConfigHandler(r *service.Registry) *ConfigHandler {
	return &ConfigHandler{BaseHandler: handler.New(r)}
}

func (h *ConfigHandler) rc(c *app.RequestContext) *service.Caller { return h.BaseHandler.RequireCaller(c) }

// PublicConfigView 登录前可读的运行时开关 + 品牌信息白名单
//
//	**白名单按结构体字段强制**:只有显式写在这里的字段才会出现在响应里。
//	想往外放开关必须先在这里加字段并注释"为什么这条可以匿名读",
//	而不是像 sys_config 那样整表拉 —— 后者挂在 Auth() 下,
//	未登录时前端根本拿不到,而登录页恰恰需要知道验证码开没开。
//
//	**返原始的两个分项开关,不在服务端算"要不要显示"** ——
//	不同页面关心的组合不一样(密码登录只看登录开关,短信流程两个都要),
//	把组合逻辑留在服务端会让前端只能照抄,新增场景时反而看不出该用哪个。
type PublicConfigView struct {
	// CaptchaLoginEnabled 登录类验证码开关。密码登录页据此决定渲不渲染验证码框。
	CaptchaLoginEnabled bool `json:"captcha_login_enabled"`
	// CaptchaSmsSendEnabled 发短信验证码(防刷量闸门)开关。
	// 短信登录 / 找回密码页需要它 —— 那些流程要先过 /public/auth/sms/send。
	CaptchaSmsSendEnabled bool `json:"captcha_sms_send_enabled"`
	// AppName 平台全名。**空串 = 没配** —— 前端据此回退到本地语言包。
	// 匿名可读的理由:品牌名本来就印在登录页上,不是秘密。
	AppName string `json:"app_name"`
	// AppTitle 浏览器标签页标题。**空串 = 没配** —— 前端回退到语言包。
	AppTitle string `json:"app_title"`
}

// PublicConfig GET /rapid-admin/v1/public/config 匿名开关 + 品牌信息下发
//
//	刻意只返开关和品牌名,不返任何**配置值** —— 这里一旦开始塞业务配置
//	(尤其是 sys_config 里那些密钥 / endpoint),就成了未授权信息泄露面。
//
//	品牌名(platform name)是唯一新增的非布尔字段:开源通用化要求"改个名字就能用",
//	而品牌名必须在**登录之前**就拿到(登录页标题就是它)—— 挂在 Auth() 下的
//	sys_config 读接口未登录时拿不到。
func (h *ConfigHandler) PublicConfig(ctx context.Context, c *app.RequestContext) {
	cfg := config.Get()
	var v PublicConfigView
	if cfg != nil {
		v = PublicConfigView{
			CaptchaLoginEnabled:   cfg.Captcha.LoginEnabled,
			CaptchaSmsSendEnabled: cfg.Captcha.SmsSendEnabled,
			AppName:               cfg.Branding.AppName,
			AppTitle:              cfg.Branding.TabTitle(),
		}
	} else {
		// config 未加载(单测直接 new handler)—— 按"都要"处理,理由同 config.CaptchaEnabled
		v = PublicConfigView{CaptchaLoginEnabled: true, CaptchaSmsSendEnabled: true}
	}
	h.OK(c, v)
}

func (h *ConfigHandler) ListConfigs(ctx context.Context, c *app.RequestContext) {
	q := service.ConfigListQuery{Group: c.Query("group"), Keyword: c.Query("keyword")}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Config.ListConfigsView(ctx, q)
	if err != nil { h.Fail(c, err); return }
	h.Page(c, res.List, res.Total)
}

func (h *ConfigHandler) GetConfig(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id"); if err != nil { h.Fail(c, err); return }
	v, err := h.Services.Config.GetConfigView(ctx, id)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, v)
}

func (h *ConfigHandler) GetConfigByKey(ctx context.Context, c *app.RequestContext) {
	key := c.Param("key")
	v, err := h.Services.Config.GetConfigByKeyView(ctx, key)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, v)
}

// ListGroups GET /configs/groups 拉去重 group 列表(前端 group filter)
func (h *ConfigHandler) ListGroups(ctx context.Context, c *app.RequestContext) {
	groups, err := h.Services.Config.ListGroups(ctx)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, groups)
}

// DirectUpdate PATCH /configs/:id super_admin 直改更新
//   2026-10-18 收窄:删 reason 字段(UI 没输入框,前端硬塞无意义)
func (h *ConfigHandler) DirectUpdate(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil { return }
	id, err := h.GetID(c, "id")
	if err != nil { h.Fail(c, err); return }
	var body struct {
		ConfigName    *string `json:"config_name"`
		Description   *string `json:"description"`
		Value         *string `json:"value"`
		ValueType     *string `json:"value_type"`
		IsSensitive   *bool   `json:"is_sensitive"`
		IsRequired    *bool   `json:"is_required"`
		IsReadonly    *bool   `json:"is_readonly"`
		EnvVarName    *string `json:"env_var_name"`
		Sort          *int    `json:"sort"`
		RegexValidate *string `json:"regex_validate"`
		MinLength     *int    `json:"min_length"`
		MaxLength     *int    `json:"max_length"`
		IsActive      *bool   `json:"is_active"`
		Remarks       *string `json:"remarks"`
	}
	if err := h.BindJSON(c, &body); err != nil { h.Fail(c, err); return }
	patch := map[string]any{}
	if body.ConfigName != nil { patch["config_name"] = *body.ConfigName }
	if body.Description != nil { patch["description"] = *body.Description }
	if body.Value != nil { patch["value_encrypted"] = *body.Value }
	if body.ValueType != nil { patch["value_type"] = *body.ValueType }
	if body.IsSensitive != nil { patch["is_sensitive"] = *body.IsSensitive }
	if body.IsRequired != nil { patch["is_required"] = *body.IsRequired }
	if body.IsReadonly != nil { patch["is_readonly"] = *body.IsReadonly }
	if body.EnvVarName != nil { patch["env_var_name"] = *body.EnvVarName }
	if body.Sort != nil { patch["sort"] = *body.Sort }
	if body.RegexValidate != nil { patch["regex_validate"] = *body.RegexValidate }
	if body.MinLength != nil { patch["min_length"] = *body.MinLength }
	if body.MaxLength != nil { patch["max_length"] = *body.MaxLength }
	if body.IsActive != nil { patch["is_active"] = *body.IsActive }
	if body.Remarks != nil { patch["remarks"] = *body.Remarks }
	if err := h.Services.Config.DirectUpdate(ctx, caller, id, patch); err != nil { h.Fail(c, err); return }
	h.OK(c, nil)
}

// ListConfigAudit 配置审计日志
//   Phase 1.5 IDOR 修复:仅 super_admin 可指定 userId;其他角色强制 userId = caller.UserID
//   2026-10-18 收窄:audit action 只剩 update(view 已删 + create/delete 已删)
//   2026-10-29 修:query 参数名跟前端对不上 ——
//     前端发的是 `config_key`(wire 名,snake_case),这里读的是 `configKey`,
//     Hertz 的 Query 是**大小写敏感**的 ⇒ 读出来永远是空串 ⇒ **过滤从来没生效过**
//     (前端搜索框和 group 过滤都跟着一起失灵)。
//     现在按 wire 名读:`config_key` / `config_group`。
func (h *ConfigHandler) ListConfigAudit(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	q := service.ConfigAuditListQuery{
		ConfigKey:   c.Query("config_key"),
		ConfigGroup: c.Query("config_group"),
		UserID:      h.MustUint(c, "userId", 0),
		Action:      c.Query("action"),
	}
	if q.UserID > 0 && !caller.HasRole("role_super_admin") {
		q.UserID = caller.UserID
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Config.ListConfigAudit(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// ListExportLogs 导出日志(走 sys_config_export_log 表,本期 0 数据源,接口保留以备 P1+)
func (h *ConfigHandler) ListExportLogs(ctx context.Context, c *app.RequestContext) {
	q := service.HotReloadListQuery{ConfigKey: c.Query("config_key")}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Config.ListHotReload(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// Stats GET /rapid-admin/v1/platform/configs/stats
func (h *ConfigHandler) Stats(ctx context.Context, c *app.RequestContext) {
	res, err := h.Services.Config.Stats(ctx)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, res)
}

// Health GET /rapid-admin/v1/platform/configs/health
func (h *ConfigHandler) Health(ctx context.Context, c *app.RequestContext) {
	res, err := h.Services.Config.Health(ctx)
	if err != nil { h.Fail(c, err); return }
	h.OK(c, res)
}
