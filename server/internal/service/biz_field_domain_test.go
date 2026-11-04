// biz_field_domain.go 登记表自身的自检 —— M162(2026-11-02)
//
// # 这套用例守的是"字段级可见性的登记表没跟现实脱节"
//
// ⚠️ **登记表脱节的后果是静默的** ——
//
//	登记了错的模型 ⇒ 候选字段与真实响应不符 ⇒
//	  管理员配了一个不存在的字段 ⇒ 规则作用不到任何键上 ⇒ **什么都不发生**
//
// 页面看起来一切正常,后台看起来一切正常,只有"敏感字段漏出去了"这一个后果。
//
// # 守的是三件事
//
//  1. **业务域不能是孤儿** —— 登记在字段层却没登记在行级层的业务域,
//     说明两边对"有哪些业务域"的理解已经分叉了。
//  2. **主要模型的表名必须与行级账本一致** —— 登记错模型 ⇒ 候选与真实响应不符。
//  3. **真正该管的敏感字段必须配得出来** —— 候选里得有它,而且得被标成敏感。
package service

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/fieldpolicy"
)

// TestBizFieldDomain_没有重复登记
func TestBizFieldDomain_没有重复登记(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range bizFieldDomains {
		require.NotEmpty(t, d.module, "业务域不能为空")
		require.NotEmpty(t, d.models, "业务域 %s 没登记模型 ⇒ 候选字段为空 ⇒ 配了也不生效", d.module)
		assert.False(t, seen[d.module], "业务域 %s 登记了两次", d.module)
		seen[d.module] = true
	}
	assert.NotEmpty(t, bizFieldDomains)
}

// TestBizFieldDomain_每个域都在行级账本里
//
// ⚠️ **业务域字符串是两根轴共用的** ——
// 路由层 `rbac(module, action)` 的第一个参数是唯一权威。
// 字段层登记了一个行级层没有的业务域 ⇒ 两边对业务域集合的理解分叉了。
//
// ⚠️ **bizScopeUnwired 里的域不能出现在这里** ——
// 那些是"明确没有业务行"的域(登录/配置/字典…),
// 它们压根没有可以打码的响应行。
func TestBizFieldDomain_每个域都在行级账本里(t *testing.T) {
	for _, d := range bizFieldDomains {
		if sp, ok := bizScopeSpecFor(d.module); ok {
			require.NotEmpty(t, sp.table, "业务域 %s 在 bizScopeSpecs 里但没写表名", d.module)
			continue
		}
		_, inCtx := bizScopeCtxWired[d.module]
		assert.True(t, inCtx,
			"业务域 %s 登记在字段层,但行级层的三张账本里都没有它 —— "+
				"要么该在 bizScopeSpecs / bizScopeCtxWired 里登记,要么就不该出现在字段层",
			d.module)
		assert.NotContains(t, bizScopeUnwired, d.module,
			"业务域 %s 在 bizScopeUnwired 里(明确没有业务行)⇒ 不该有字段级可见性", d.module)
	}
}

// TestBizFieldDomain_主要模型的表名与行级账本一致
//
// ⚠️ **这是"登记错模型"的守卫** ——
// 业务域 "owner" 在行级层管的是 biz_user 表;
// 字段层若登记成 model.HouseOwner(表 biz_house_owner),
// 候选字段就全是 house_id / relation / share_ratio ——
// 真实响应里的 real_name 压根不在候选里 ⇒ 配不了 ⇒ 漏。
func TestBizFieldDomain_主要模型的表名与行级账本一致(t *testing.T) {
	for _, d := range bizFieldDomains {
		sp, ok := bizScopeSpecFor(d.module)
		if !ok {
			continue // 走 ctx 业务域那条路的没有表名可对
		}
		primary, ok := d.models[0].(interface{ TableName() string })
		require.True(t, ok, "业务域 %s 的主要模型 %T 必须有 TableName()", d.module, d.models[0])
		assert.Equal(t, sp.table, primary.TableName(),
			"业务域 %s 的主要模型表名与行级账本不一致 —— 登记错模型了", d.module)
	}
}

// TestBizFieldDomain_每个域都有候选字段
//
// ⚠️ **候选为空 = 这个域配不了任何规则** ——
// 页面上会显示一个空的下拉框,管理员以为是自己还没配。
func TestBizFieldDomain_每个域都有候选字段(t *testing.T) {
	for _, d := range bizFieldDomains {
		cands := wireFieldCandidates(d.models...)
		assert.NotEmpty(t, cands, "业务域 %s 没有候选字段", d.module)

		seen := map[string]bool{}
		for _, c := range cands {
			assert.NotEmpty(t, c.field, "业务域 %s 有空字段名", d.module)
			assert.False(t, seen[c.field], "业务域 %s 的候选字段 %s 重复了", d.module, c.field)
			seen[c.field] = true
		}
	}
}

// TestBizFieldDomain_真正该管的敏感字段配得出来
//
// ⚠️ **这条是"这套机制到底有没有用"的判据** ——
// 若某个域的敏感字段压根不在候选里,管理员就配不了它,
// 那么"字段级可见性"在这个域上就是一句空话。
//
// ⚠️ **只查这几个已知的** —— 不是所有字段都该敏感
// (机构名打码了就没法看了)。
func TestBizFieldDomain_真正该管的敏感字段配得出来(t *testing.T) {
	cases := []struct {
		module string
		field  string
	}{
		// SysUser.Phone 是 json:"-"(加密列不经 wire 出去)⇒ 不进候选;
		// 框架自带域里的敏感字段以 guide.contact_phone 为样本。
		{module: "guide", field: "contact_phone"},
	}
	for _, c := range cases {
		d, ok := bizFieldDomainFor(c.module)
		require.True(t, ok, "业务域 %s 没登记", c.module)
		cands := wireFieldCandidates(d.models...)
		var hit *fieldCandidate
		for i := range cands {
			if cands[i].field == c.field {
				hit = &cands[i]
				break
			}
		}
		require.NotNil(t, hit, "业务域 %s 的敏感字段 %s 不在候选里 ⇒ 配不了 ⇒ 漏", c.module, c.field)
		assert.True(t, hit.sensitive, "业务域 %s 的字段 %s 应当被标成敏感", c.module, c.field)
		assert.NotEqual(t, fieldpolicy.MaskAuto, hit.maskType,
			"业务域 %s 的字段 %s 应当能自动推断出打码方式", c.module, c.field)
	}
}

// TestBizFieldDomain_候选字段名与模型上的json标签一致
//
// ⚠️ **这是一条"防手抄"的守卫** ——
// 候选是反射出来的,不是手写的;但**模块列表接口返回的字段名**
// 必须与模型上的 json tag 逐字相同,
// 否则管理员照着页面上的名字填进规则,规则却作用不到任何键上。
func TestBizFieldDomain_候选字段名与模型上的json标签一致(t *testing.T) {
	for _, d := range bizFieldDomains {
		cands := wireFieldCandidates(d.models...)
		names := make([]string, 0, len(cands))
		for _, c := range cands {
			names = append(names, c.field)
		}
		sort.Strings(names)
		assert.NotContains(t, names, "id", "业务域 %s 不该把主键 id 摆进候选", d.module)
		assert.NotContains(t, names, "password_hash", "业务域 %s 不该把密码哈希摆进候选", d.module)
	}
}

// TestBizFieldDomain_行级账本里的每个域都在字段层登记
//
// ⚠️ **这是"反方向"的那张网** ——
// 上面那条 `TestBizFieldDomain_每个域都在行级账本里` 守的是
// 「字段层 ⊆ 行级层」;**反方向**曾经没有网 ⇒
//
//	行级账本新增一个业务域,而没登记进 bizFieldDomains
//	⇒ ① 执行层早退(fieldPolicyDomainSupported)⇒ 该域的响应**完全不过脱敏**
//	⇒ ② 配置期拒「业务域不存在」⇒ 管理员**也配不出来**
//	⇒ 配了不生效,而且**没有任何报错**。
//
// ⚠️ **判据是"有没有业务行",不是"有没有敏感字段"** ——
// 一个域今天没有手机号,不等于它永远没有;模型加一个字段,没人会记得回来登记。
func TestBizFieldDomain_行级账本里的每个域都在字段层登记(t *testing.T) {
	for _, sp := range bizScopeSpecs {
		_, ok := bizFieldDomainFor(sp.module)
		assert.True(t, ok,
			"业务域 %s 接了行级数据范围,却没在 bizFieldDomains 里登记 —— "+
				"后果:该域的响应完全不过脱敏(执行层早退),管理员也配不出来(配置期拒「业务域不存在」),"+
				"⇒ 配了不生效,且没有任何报错",
			sp.module)
	}
	for m := range bizScopeCtxWired {
		_, ok := bizFieldDomainFor(m)
		assert.True(t, ok,
			"业务域 %s 走 ctx 业务域那条路接了行级数据范围,却没在 bizFieldDomains 里登记 —— "+
				"后果:该域的响应完全不过脱敏,管理员也配不出来", m)
	}
}

// TestCheckFieldPolicyWiring_真实账本通过
//
// ⚠️ **启动期 fail-fast 的那条网,在真账本上必须是绿的** ——
// 不然每次启动都 log.Fatal,等于把整个服务装上了。
func TestCheckFieldPolicyWiring_真实账本通过(t *testing.T) {
	require.NoError(t, CheckFieldPolicyWiring())
}

// TestCheckFieldPolicyWiring_漏登记要报错
//
// ⚠️ **这条用例守的是"那张网真的存在"** ——
// 拆掉 `checkFieldPolicyWiring` 里的比较逻辑,这条必须变红。
//
// ⚠️ **用假的账本喂,不改包级变量** ——
// 改包级变量就得恢复,漏一次就把后面的用例带歪(而且是**顺序依赖**的坑)。
func TestCheckFieldPolicyWiring_漏登记要报错(t *testing.T) {
	// 少登记一个域 ⇒ 必须被抓出来
	short := bizFieldDomains[:len(bizFieldDomains)-1]
	last := bizFieldDomains[len(bizFieldDomains)-1].module
	err := checkFieldPolicyWiring(short, bizScopeSpecs, bizScopeCtxWired)
	require.Error(t, err, "少登记了业务域 %s 却没报错 ⇒ 那张网不存在", last)
	assert.Contains(t, err.Error(), last)

	// 全登记 ⇒ 不报错
	require.NoError(t, checkFieldPolicyWiring(bizFieldDomains, bizScopeSpecs, bizScopeCtxWired))

	// 行级账本里多出来的域 ⇒ 也要被抓出来(方向反了同样不行)
	extra := bizScopeSpec{module: "news", table: "biz_news"}
	err = checkFieldPolicyWiring(bizFieldDomains, append(bizScopeSpecs, extra), bizScopeCtxWired)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "news")

	// 走 ctx 那条路的域漏登记 ⇒ 也要被抓出来
	err = checkFieldPolicyWiring(
		bizFieldDomains,
		bizScopeSpecs,
		map[string]bizScopeCtxSpec{"news": {columnsVar: "x"}},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "news")
}

// TestCheckFieldPolicyWiring_错误消息说清后果
//
// ⚠️ **报错必须说清"不登记会怎样"** ——
// 只说"缺登记"的话,改的人不知道严重性,会当成"补一行就行"的小事。
func TestCheckFieldPolicyWiring_错误消息说清后果(t *testing.T) {
	short := bizFieldDomains[:len(bizFieldDomains)-1]
	err := checkFieldPolicyWiring(short, bizScopeSpecs, bizScopeCtxWired)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "脱敏")
	assert.Contains(t, msg, "配不出来")
}
