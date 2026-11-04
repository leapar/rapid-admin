// field_policy.go —— 响应出口的字段级可见性 —— M163(2026-11-02)
//
// # 这一层是"规则"与"响应"之间唯一的桥
//
// 规则层(M162)只回答"这个角色在这个业务域上,哪些字段该打码/该删掉";
// 这一层负责**把它作用到真正上 wire 的那份 JSON 上**。
//
// # ⚠️ **为什么落在响应出口,而不是每个业务 service**
//
// 逐个业务 service 加一行 ⇒
//
//	① 漏一个接口 = 那个接口的敏感字段裸奔(而且**没有任何报错**)
//	② 每个 service 都要多一个参数,业务代码被框架污染
//
// ⇒ 收口在 `BaseHandler.OK` / `Page` —— **每个 handler 都经过这两个出口**。
// 全仓绕过它们的地方只有 4 处(dyn 的两个分页出口 + health 的两个直写),
// 已在 dyn.go 里改掉;health 那两个写的是健康检查,没有业务数据。
//
// # ⚠️ **"没配规则 ⇒ 响应逐字节不变"**
//
// 没配规则 / 规则没命中 / 这个域不支持 ⇒ 一律走原来的渲染路径,
// **不重新序列化** ⇒ 响应与"没有这一层"时完全一致。
//
// # ⚠️ **失败一律"闭" —— 宁可报错,不许悄悄放行**
//
// 脱敏失败时原样返回 = 一次数据库抖动就把敏感字段放出去,
// 而且**日志里一条痕迹都没有**。
// ⇒ 这一层出错直接 500。
// (与行级数据范围"落不下去就报错"同一条纪律。)

package handler

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/fieldpolicy"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/service"
)

// FieldPolicyHeader 响应头名 —— 前端靠它知道"这次响应里哪些字段被收窄了"
//
// # ⚠️ **这是 M168 的"执行层 → 显示层"通道**
//
// 服务端"隐藏"是**删键** ⇒ 响应里**根本没有那个键** ⇒
// 前端的表格列定义还列着它 ⇒ **列还在、格是空的**;
// 表单里那个字段也还显示 ⇒ 用户还能把**打码后的值**原样提交回去(数据损坏)。
//
// ⇒ 前端要靠这个头把列/字段藏起来。
//
// ⚠️ **为什么不另开一个"查我的可见性"接口** ——
// 那条路要求前端知道**业务域字符串**(与路由层 `rbac(module, action)`
// 的第一个参数逐字相同),而业务域在前端是**散落的字面量**:
// 写错 ⇒ 静默失配(前端按 A 域的规则去藏 B 域的列)。
//
// ⇒ **让执行层把结果随响应一起带回来** ——
// 谁执行的脱敏谁最清楚,而且**前端一行字都不用改**。
const FieldPolicyHeader = "X-Field-Policy"

// encodeFieldPolicyReport 把报告编成响应头的值
//
// 格式:`hidden=phone,id_card;masked=real_name`
//
// ⚠️ **分隔符冲突的字段名会被丢掉** ——
// 字段名来自 Go 的 json tag 或动态集合的 `field_key`
// (后端用 `^[a-z][a-z0-9_]{0,63}$` 校验过),**不可能**含逗号/分号/等号。
// ⇒ 这里再挡一道是兜底:万一将来有别的来源,
// 最坏结果是"那一列没被藏起来"(值仍然是脱敏后的,**不构成泄露**)。
func encodeFieldPolicyReport(r fieldpolicy.Report) string {
	var b strings.Builder
	if names := safeFieldNames(r.Hidden); len(names) > 0 {
		b.WriteString("hidden=")
		b.WriteString(strings.Join(names, ","))
	}
	if names := safeFieldNames(r.Masked); len(names) > 0 {
		if b.Len() > 0 {
			b.WriteByte(';')
		}
		b.WriteString("masked=")
		b.WriteString(strings.Join(names, ","))
	}
	return b.String()
}

// safeFieldNames 过滤掉含分隔符的字段名
func safeFieldNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		if strings.ContainsAny(n, ",;=") {
			continue
		}
		out = append(out, n)
	}
	return out
}

// setFieldPolicyHeader 把"这次响应收窄了哪些字段"写进响应头
//
// ⚠️ **必须在写 body 之前调** —— Hertz 在 body 落盘时才把响应头刷出去。
//
// ⚠️ **没有收窄任何字段时就不设这个头** ——
// 绝大多数响应不该多一个头(前端要判空、日志要干净、契约要稳定)。
func (h *BaseHandler) setFieldPolicyHeader(c *app.RequestContext, report fieldpolicy.Report) {
	if c == nil || report.Empty() {
		return
	}
	v := encodeFieldPolicyReport(report)
	if v == "" {
		return
	}
	c.Response.Header.Set(FieldPolicyHeader, v)
}

// maskResult 字段级可见性作用在一次响应上的结果
type maskResult struct {
	// raw 脱敏后的字节(changed=false 时为 nil)
	raw json.RawMessage
	// changed 有没有真的改过
	changed bool
	// report 这条策略会收窄哪些字段(前端据此决定列/字段显隐)
	//
	// ⚠️ **即使 changed=false 也要带** ——
	// "规则配了但这次响应里没有那个字段"也是一条有效信息:
	// 前端要靠它把那一列藏起来(否则就是"列还在、格是空的")。
	report fieldpolicy.Report
}

// maskData 按本次请求的业务域给响应数据打码
//
// 返回 (脱敏结果, 错误)
//
// ⚠️ **"没改过"与"没配规则"都返回 changed=false** ——
// 调用方据此走原来的渲染路径,保证响应逐字节不变。
//
// ⚠️ **业务域从 Hertz 的 KV 取** ——
// 见 pkg/scope 的 ModuleKey 注释:ctx 里那份是派生 ctx,响应出口读不到。
func (h *BaseHandler) maskData(c *app.RequestContext, data any) (maskResult, error) {
	if h == nil || h.Services == nil || h.Services.FieldPolicy == nil {
		return maskResult{}, nil
	}
	module := requestModule(c)
	if module == "" {
		return maskResult{}, nil
	}
	caller := h.callerOf(c)
	if caller == nil {
		// 没登录的请求(公开接口)⇒ 没有角色 ⇒ 没有规则
		return maskResult{}, nil
	}
	policy, err := h.Services.FieldPolicy.EffectiveFieldPolicy(requestCtx(c), caller, module)
	if err != nil {
		// ⚠️ **闭**:查不到规则就不放行。
		// (放行 = 一次数据库抖动就把敏感字段原样吐出去,且无迹可寻。)
		zap.L().Error("字段级可见性:规则查询失败,响应未脱敏",
			zap.String("module", module),
			zap.String("path", string(c.Request.URI().Path())),
			zap.Error(err))
		return maskResult{}, errcode.New(errcode.Internal,
			"字段级可见性规则查询失败,响应未脱敏").WithDetail(err.Error())
	}
	if policy.Empty() {
		return maskResult{}, nil
	}
	report := policy.Report()
	raw, err := json.Marshal(data)
	if err != nil {
		return maskResult{}, errcode.New(errcode.Internal,
			"字段级可见性:序列化响应失败").WithDetail(err.Error())
	}
	out, changed, err := policy.ApplyJSON(raw)
	if err != nil {
		return maskResult{}, errcode.New(errcode.Internal,
			"字段级可见性:脱敏失败").WithDetail(err.Error())
	}
	if !changed {
		// ⚠️ **规则配了但这个响应里没有那个字段** ⇒ 不重新序列化
		// (重新序列化会打乱键序,让响应"每次都不一样"没法比对)。
		// ⚠️ **但报告照样要带出去** —— 前端要靠它把那一列藏起来。
		return maskResult{report: report}, nil
	}
	return maskResult{raw: json.RawMessage(out), changed: true, report: report}, nil
}

// requestModule 取本次请求的业务域
//
// ⚠️ **取不到就返空串** ——
// 没有业务域 = 没有规则(内部调用 / 公开接口 / 豁免路由)。
func requestModule(c *app.RequestContext) string {
	if c == nil {
		return ""
	}
	v, ok := c.Get(scope.ModuleKey)
	if !ok {
		return ""
	}
	m, _ := v.(string)
	return m
}

// callerOf 取本次请求的调用者(**不判定未登录**)
//
// ⚠️ **与 RequireCaller 的区别** ——
// RequireCaller 未登录会直接给请求返 401;这里只是"取不到就当没有角色",
// 用于脱敏这种"尽力而为但失败要报错"的场合。
func (h *BaseHandler) callerOf(c *app.RequestContext) *service.Caller {
	if c == nil {
		return nil
	}
	v, ok := c.Get(service.CallerKey)
	if !ok {
		return nil
	}
	caller, _ := v.(*service.Caller)
	return caller
}

// requestCtx 取本次请求的 context.Context
//
// ⚠️ **为什么还要单独存一份** ——
//
// `*app.RequestContext` **不是** context.Context
// (Hertz v0.9.6:没有 Deadline / Done / Err),
// 中间件链上派生出来的 ctx 也**取不回来** ——
// 而查规则要一个 context.Context(GORM 的 WithContext、cachex 的加载器都收它)。
//
// ⇒ 由 Auth 中间件存进 Hertz 的 KV(见 service.CtxKey),这里取出来。
//
// ⚠️ **取不到就退化成 Background** ——
//
// 那是"没装中间件 / 公开路由 / 单测直接构造 RequestContext"的情形。
// 代价只是丢掉"客户端断连时取消查询"这一层,**不影响正确性**。
func requestCtx(c *app.RequestContext) context.Context {
	if c == nil {
		return context.Background()
	}
	v, ok := c.Get(service.CtxKey)
	if !ok {
		return context.Background()
	}
	ctx, ok := v.(context.Context)
	if !ok || ctx == nil {
		return context.Background()
	}
	return ctx
}
