// 字段级可见性 · 响应出口 —— M163(2026-11-02)
//
// # 这套用例守的是"规则真的作用到了上 wire 的那份 JSON 上"
//
// 规则层(M162)只回答"这个角色在这个业务域上哪些字段该打码/该删掉";
// 本层(执行层)负责把它作用到响应出口(BaseHandler.OK / Page / PageWith)。
//
// 最要紧的五条:
//  1. **命中规则 ⇒ 响应里那个键真的被打码/删掉** —— 不是"查了规则就完事"
//  2. **没配规则 / 规则没命中 ⇒ 响应逐字节不变**(走原来的渲染路径)
//  3. **查规则出错 ⇒ 500,不是原样放行** —— 放行 = 一次数据库抖动就泄露
//  4. **分页出口只脱敏 list,不动 total/page/size** —— 元数据不是业务行
//  5. **业务域取不到 ⇒ 直接透传** —— 公开接口/豁免路由没有角色,没有规则
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	拆法                                                      必须变红
//	───────────────────────────────────────────────────────  ─────────────────────
//	maskData 开头直接 `return nil, false, nil`                 TestOK_脱敏后响应带打码值
//	OK 里的 masked 分支删掉(永远 OKWithData)                  TestOK_脱敏后响应带打码值
//	PageWith 里的 masked 分支删掉                               TestPageWith_脱敏的是list不是total
//	maskEngine 里的 c.Set(scope.ModuleKey, ...) 删掉            TestOK_脱敏后响应带打码值
//	  (等价于删掉 rbac.go / dynGuard 里那行)
//	maskData 里查规则出错改成"放行"                              TestOK_查规则失败返500
//	PageWith 改成脱敏整个信封(把 total 也喂给 maskData)        TestPageWith_脱敏的是list不是total
package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/fieldpolicy"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/service"
)

// fakeFieldPolicySvc 字段可见性规则的测试替身
//
// ⚠️ **只实装 EffectiveFieldPolicy** —— 响应出口只用这一个方法;
// 其余三个是配置面,真出 bug 时 panic 比"静默返回 nil"好查。
type fakeFieldPolicySvc struct {
	policy     fieldpolicy.Policy
	err        error
	calls      int
	lastModule string
}

func (f *fakeFieldPolicySvc) EffectiveFieldPolicy(
	_ context.Context, _ *service.Caller, module string,
) (fieldpolicy.Policy, error) {
	f.calls++
	f.lastModule = module
	return f.policy, f.err
}

func (f *fakeFieldPolicySvc) ListRoleFieldPolicies(context.Context, uint64) ([]service.FieldPolicyView, error) {
	panic("配置面,响应出口的测试不该调到")
}

func (f *fakeFieldPolicySvc) ReplaceRoleFieldPolicies(context.Context, uint64, string, []service.FieldPolicyInput) error {
	panic("配置面,响应出口的测试不该调到")
}

func (f *fakeFieldPolicySvc) ListFieldPolicyModules(context.Context) ([]service.FieldPolicyModuleView, error) {
	panic("配置面,响应出口的测试不该调到")
}

// maskCaller 测试用调用者
var maskCaller = &service.Caller{UserID: 42, RoleCode: "role_ops"}

// maskEngine 起一个「注入业务域 + caller → 跑出口」的引擎
//
// ⚠️ **业务域与 caller 都在中间件里写** —— 与生产一致:
// rbac() 写 scope.ModuleKey,Auth() 写 service.CallerKey。
// (探针:把 c.Set(scope.ModuleKey, ...) 删掉 = 把生产里那行删掉。)
func maskEngine(svc service.FieldPolicyService, module string, run func(h *BaseHandler, c *app.RequestContext)) *route.Engine {
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(func(ctx context.Context, c *app.RequestContext) {
		if module != "" {
			c.Set(scope.ModuleKey, module)
		}
		c.Set(service.CallerKey, maskCaller)
		c.Next(ctx)
	})
	h := New(&service.Registry{FieldPolicy: svc})
	e.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		run(h, c)
	})
	return e
}

// maskedPhonePolicy 把 phone 打成 138****1234 的策略
func maskedPhonePolicy() fieldpolicy.Policy {
	return fieldpolicy.NewPolicy([]fieldpolicy.Rule{
		{Field: "phone", Mode: fieldpolicy.ModeMasked, MaskType: fieldpolicy.MaskPhone},
	})
}

// ===== maskData(响应出口的核心) =====

func TestMaskData_没有业务域时透传(t *testing.T) {
	h := New(&service.Registry{FieldPolicy: &fakeFieldPolicySvc{policy: maskedPhonePolicy()}})
	c := app.NewContext(0)
	c.Set(service.CallerKey, maskCaller)
	// ⚠️ 故意不设 scope.ModuleKey —— 公开接口/豁免路由就是这个状态

	res, err := h.maskData(c, map[string]any{"phone": "13800001234"})
	require.NoError(t, err)
	assert.False(t, res.changed, "没有业务域 = 没有规则 = 透传")
}

func TestMaskData_未登录时透传(t *testing.T) {
	h := New(&service.Registry{FieldPolicy: &fakeFieldPolicySvc{policy: maskedPhonePolicy()}})
	c := app.NewContext(0)
	c.Set(scope.ModuleKey, "owner")
	// ⚠️ 故意不设 service.CallerKey —— 没登录 ⇒ 没有角色 ⇒ 没有规则

	res, err := h.maskData(c, map[string]any{"phone": "13800001234"})
	require.NoError(t, err)
	assert.False(t, res.changed, "没有 caller = 没有角色 = 透传")
}

func TestMaskData_空策略透传(t *testing.T) {
	h := New(&service.Registry{FieldPolicy: &fakeFieldPolicySvc{policy: fieldpolicy.NewPolicy(nil)}})
	c := app.NewContext(0)
	c.Set(scope.ModuleKey, "owner")
	c.Set(service.CallerKey, maskCaller)

	res, err := h.maskData(c, map[string]any{"phone": "13800001234"})
	require.NoError(t, err)
	assert.False(t, res.changed, "空策略 = 什么都不做 = 透传")
}

func TestMaskData_规则没命中时不重序列化(t *testing.T) {
	fake := &fakeFieldPolicySvc{policy: fieldpolicy.NewPolicy([]fieldpolicy.Rule{
		{Field: "not_exist", Mode: fieldpolicy.ModeHidden},
	})}
	h := New(&service.Registry{FieldPolicy: fake})
	c := app.NewContext(0)
	c.Set(scope.ModuleKey, "owner")
	c.Set(service.CallerKey, maskCaller)

	res, err := h.maskData(c, map[string]any{"phone": "13800001234"})
	require.NoError(t, err)
	// ⚠️ **规则配了但响应里没有那个键 ⇒ 必须返回"没改过"** ——
	// 否则调用方会走重序列化路径,键序被打乱,响应"每次都不一样"没法比对。
	assert.False(t, res.changed, "规则没命中也必须走原渲染路径")
	assert.Equal(t, 1, fake.calls, "规则还是要查的 —— 命不命中是查过才知道")
}

func TestMaskData_命中时打码(t *testing.T) {
	h := New(&service.Registry{FieldPolicy: &fakeFieldPolicySvc{policy: maskedPhonePolicy()}})
	c := app.NewContext(0)
	c.Set(scope.ModuleKey, "owner")
	c.Set(service.CallerKey, maskCaller)

	res, err := h.maskData(c, map[string]any{"phone": "13800001234", "name": "张三"})
	require.NoError(t, err)
	require.True(t, res.changed)
	s := string(res.raw)
	assert.Contains(t, s, "138****1234", "手机号必须被打码")
	assert.NotContains(t, s, "13800001234", "原文不许残留")
	assert.Contains(t, s, "张三", "没规则的字段原样保留")
}

func TestMaskData_hidden是整个键删掉(t *testing.T) {
	h := New(&service.Registry{FieldPolicy: &fakeFieldPolicySvc{
		policy: fieldpolicy.NewPolicy([]fieldpolicy.Rule{
			{Field: "phone", Mode: fieldpolicy.ModeHidden},
		}),
	}})
	c := app.NewContext(0)
	c.Set(scope.ModuleKey, "owner")
	c.Set(service.CallerKey, maskCaller)

	res, err := h.maskData(c, map[string]any{"phone": "13800001234", "name": "张三"})
	require.NoError(t, err)
	require.True(t, res.changed)
	s := string(res.raw)
	// ⚠️ **hidden 是删键,不是置空** —— 字段名本身可能就是信息。
	assert.NotContains(t, s, `"phone"`, "hidden 必须把键整个删掉,不是置成 null")
	assert.Contains(t, s, "张三")
}

func TestMaskData_查规则失败要报错(t *testing.T) {
	h := New(&service.Registry{FieldPolicy: &fakeFieldPolicySvc{err: errors.New("db down")}})
	c := app.NewContext(0)
	c.Set(scope.ModuleKey, "owner")
	c.Set(service.CallerKey, maskCaller)

	res, err := h.maskData(c, map[string]any{"phone": "13800001234"})
	// ⚠️ **失败一律闭** —— 原样放行 = 一次数据库抖动就把敏感字段吐出去,
	// 而且日志里一条痕迹都没有。
	require.Error(t, err, "查规则失败必须报错,不许悄悄放行")
	assert.False(t, res.changed)
	var ec *errcode.Error
	require.True(t, errors.As(err, &ec))
	assert.Equal(t, errcode.Internal, ec.Code)
}

func TestMaskData_业务域原样传给规则层(t *testing.T) {
	fake := &fakeFieldPolicySvc{policy: fieldpolicy.NewPolicy(nil)}
	h := New(&service.Registry{FieldPolicy: fake})
	c := app.NewContext(0)
	// ⚠️ **带冒号的业务域** —— audit:operation 从资源码反推会被切成 audit,
	// 这里钉住"原样传"(与行级数据范围同一类洞)。
	c.Set(scope.ModuleKey, "audit:operation")
	c.Set(service.CallerKey, maskCaller)

	_, err := h.maskData(c, map[string]any{"id": 1})
	require.NoError(t, err)
	assert.Equal(t, "audit:operation", fake.lastModule, "业务域必须原样传给规则层")
}

// ===== requestCtx(取 context.Context 的桥) =====

func TestRequestCtx_缺省时退化成Background(t *testing.T) {
	ctx := requestCtx(app.NewContext(0))
	require.NotNil(t, ctx)
	_, hasDeadline := ctx.Deadline()
	assert.False(t, hasDeadline, "Background 没有截止时间")
	_, ok := service.FromContext(ctx)
	assert.False(t, ok, "Background 里没有 caller")
}

func TestRequestCtx_取到的是中间件存的那份(t *testing.T) {
	c := app.NewContext(0)
	// 模拟 Auth 中间件存进去的那份(带着 caller)
	c.Set(service.CtxKey, service.WithCaller(context.Background(), maskCaller))

	ctx := requestCtx(c)
	got, ok := service.FromContext(ctx)
	require.True(t, ok, "中间件存进去的 ctx 必须原样取得回来")
	assert.Equal(t, maskCaller, got)
}

func TestRequestCtx_类型不对时兜底(t *testing.T) {
	c := app.NewContext(0)
	c.Set(service.CtxKey, "not-a-context")

	ctx := requestCtx(c)
	require.NotNil(t, ctx)
	_, ok := service.FromContext(ctx)
	assert.False(t, ok, "类型不对必须退化成 Background,不能 panic")
}

// ===== 出口(OK / PageWith) =====

func TestOK_脱敏后响应带打码值(t *testing.T) {
	e := maskEngine(&fakeFieldPolicySvc{policy: maskedPhonePolicy()}, "owner",
		func(h *BaseHandler, c *app.RequestContext) {
			h.OK(c, map[string]any{"phone": "13800001234", "name": "张三"})
		})

	w := ut.PerformRequest(e, "GET", "/x", nil)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"code":0`, "外壳必须还是统一响应")
	assert.Contains(t, body, "138****1234", "上 wire 的那份必须真的打了码")
	assert.NotContains(t, body, "13800001234", "原文不许上 wire")
	assert.Contains(t, body, "张三")
}

func TestOK_空策略响应逐字节不变(t *testing.T) {
	e := maskEngine(&fakeFieldPolicySvc{policy: fieldpolicy.NewPolicy(nil)}, "owner",
		func(h *BaseHandler, c *app.RequestContext) {
			h.OK(c, map[string]any{"phone": "13800001234"})
		})

	w := ut.PerformRequest(e, "GET", "/x", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "13800001234", "没配规则 ⇒ 原样返回")
}

func TestOK_查规则失败返500(t *testing.T) {
	e := maskEngine(&fakeFieldPolicySvc{err: errors.New("db down")}, "owner",
		func(h *BaseHandler, c *app.RequestContext) {
			h.OK(c, map[string]any{"phone": "13800001234"})
		})

	w := ut.PerformRequest(e, "GET", "/x", nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"脱敏失败必须 500 —— 放行 = 一次数据库抖动就泄露")
	assert.Contains(t, w.Body.String(), `"code":50000`, "业务码必须是 Internal")
}

func TestPageWith_脱敏的是list不是total(t *testing.T) {
	e := maskEngine(&fakeFieldPolicySvc{
		policy: fieldpolicy.NewPolicy([]fieldpolicy.Rule{
			{Field: "phone", Mode: fieldpolicy.ModeHidden},
		}),
	}, "owner",
		func(h *BaseHandler, c *app.RequestContext) {
			h.PageWith(c, []any{
				map[string]any{"id": "94883951113533126", "phone": "13800001234"},
				map[string]any{"id": "94883951113533127", "phone": "13900005678"},
			}, 2, 1, 20)
		})

	w := ut.PerformRequest(e, "GET", "/x", nil)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.NotContains(t, body, `"phone"`, "list 里的 phone 必须整个删掉")
	// ⚠️ **total/page/size 是元数据,不许动** —— 动了前端分页器直接算错总页数。
	assert.True(t, strings.Contains(body, `"total":2`), "total 必须原样保留,实际 %s", body)
	assert.True(t, strings.Contains(body, `"size":20`), "size 必须原样保留,实际 %s", body)
	// ⚠️ **sonyflake 主键不许被截断** —— 17 位数超出 float64 的 2^53。
	assert.Contains(t, body, "94883951113533126", "大整数 id 不许走 float64 中转")
}

func TestPageWith_空策略响应逐字节不变(t *testing.T) {
	e := maskEngine(&fakeFieldPolicySvc{policy: fieldpolicy.NewPolicy(nil)}, "owner",
		func(h *BaseHandler, c *app.RequestContext) {
			h.PageWith(c, []any{map[string]any{"phone": "13800001234"}}, 1, 1, 20)
		})

	w := ut.PerformRequest(e, "GET", "/x", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "13800001234", "没配规则 ⇒ 原样返回")
}

// ===== X-Field-Policy 响应头(M168:执行层 → 显示层的那条通道) =====

// TestOK_响应带字段可见性头
//
// ⚠️ **这条头是"前端消费那一半"的全部依据** ——
// 服务端"隐藏"是删键 ⇒ 响应里根本没有那个键 ⇒
// 前端要靠这个头才知道"这一列该藏起来"。
func TestOK_响应带字段可见性头(t *testing.T) {
	e := maskEngine(&fakeFieldPolicySvc{policy: maskedPhonePolicy()}, "owner",
		func(h *BaseHandler, c *app.RequestContext) {
			h.OK(c, map[string]any{"phone": "13800001234", "name": "张三"})
		})

	w := ut.PerformRequest(e, "GET", "/x", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "masked=phone", w.Header().Get(FieldPolicyHeader),
		"必须告诉前端:phone 这个字段被打码了")
}

// TestOK_没配规则时没有那个头
//
// ⚠️ **绝大多数响应不该多一个头** ——
// 前端要判空、日志要干净、契约要稳定。
func TestOK_没配规则时没有那个头(t *testing.T) {
	e := maskEngine(&fakeFieldPolicySvc{policy: fieldpolicy.NewPolicy(nil)}, "owner",
		func(h *BaseHandler, c *app.RequestContext) {
			h.OK(c, map[string]any{"phone": "13800001234"})
		})

	w := ut.PerformRequest(e, "GET", "/x", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get(FieldPolicyHeader), "没配规则 ⇒ 不该有这个头")
}

// TestOK_规则没命中也照样带头
//
// ⚠️ **这是 M168 最容易漏的一条** ——
// "规则配了,但这次响应里没有那个字段"⇒ 响应**逐字节没变**。
// 若此时**不**带头 ⇒ 前端拿不到报告 ⇒ **那一列照样留着**(空列)
// ⇒ 「列还在、格是空的」这个症状**没治好**。
func TestOK_规则没命中也照样带头(t *testing.T) {
	fake := &fakeFieldPolicySvc{policy: fieldpolicy.NewPolicy([]fieldpolicy.Rule{
		{Field: "not_exist", Mode: fieldpolicy.ModeHidden},
	})}
	e := maskEngine(fake, "owner",
		func(h *BaseHandler, c *app.RequestContext) {
			h.OK(c, map[string]any{"name": "张三"})
		})

	w := ut.PerformRequest(e, "GET", "/x", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "张三", "响应本身不该变")
	assert.Equal(t, "hidden=not_exist", w.Header().Get(FieldPolicyHeader),
		"规则说了什么就要报什么 —— 前端要靠它把那一列藏起来")
}

// TestPageWith_响应带字段可见性头
func TestPageWith_响应带字段可见性头(t *testing.T) {
	e := maskEngine(&fakeFieldPolicySvc{
		policy: fieldpolicy.NewPolicy([]fieldpolicy.Rule{
			{Field: "phone", Mode: fieldpolicy.ModeHidden},
			{Field: "real_name", Mode: fieldpolicy.ModeMasked, MaskType: fieldpolicy.MaskName},
		}),
	}, "owner",
		func(h *BaseHandler, c *app.RequestContext) {
			h.PageWith(c, []any{
				map[string]any{"id": "1", "phone": "13800001234", "real_name": "张三"},
			}, 1, 1, 20)
		})

	w := ut.PerformRequest(e, "GET", "/x", nil)
	require.Equal(t, http.StatusOK, w.Code)
	got := w.Header().Get(FieldPolicyHeader)
	assert.Contains(t, got, "hidden=phone")
	assert.Contains(t, got, "masked=real_name")
}

// TestEncodeFieldPolicyReport_格式与分隔符兜底
func TestEncodeFieldPolicyReport_格式与分隔符兜底(t *testing.T) {
	// 空报告 ⇒ 空串(调用方据此不设头)
	assert.Equal(t, "", encodeFieldPolicyReport(fieldpolicy.Report{}))

	// 只有 hidden
	assert.Equal(t, "hidden=phone",
		encodeFieldPolicyReport(fieldpolicy.Report{Hidden: []string{"phone"}}))

	// 两种都有 ⇒ 分号隔开
	got := encodeFieldPolicyReport(fieldpolicy.Report{
		Hidden: []string{"phone", "id_card"},
		Masked: []string{"real_name"},
	})
	assert.Equal(t, "hidden=phone,id_card;masked=real_name", got)

	// ⚠️ **含分隔符的字段名要被丢掉** ——
	// 字段名来自 json tag 或动态集合的 field_key(后端校验过),
	// 不可能含这些字符;这里再挡一道是兜底。
	got = encodeFieldPolicyReport(fieldpolicy.Report{
		Hidden: []string{"a,b", "ok", "c;d", "e=f"},
	})
	assert.Equal(t, "hidden=ok", got,
		"含分隔符的字段名必须丢掉(否则整个头解析不出来)")
}
