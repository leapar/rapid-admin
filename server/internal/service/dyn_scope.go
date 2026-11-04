// dyn_scope.go —— 动态业务集合的数据面接上「数据范围(行级权限)」 —— M160(2026-11-02)
//
// # 这层解决什么
//
// 动态集合的物理表是**用户自己在设计器里建出来的** ——
// 归属关系最乱(谁建的?属于哪个部门?哪个项目?),恰恰是最需要行级权限的地方。
//
// 而在 M160 之前,它**完全不受数据范围管**:
// 唯一的行级过滤是 `owner_scoped` 那道写死的 `owner_id = caller.UserID`,
// 而且它只对"在集合设计里勾了仅本人"的集合生效。
//
// ⇒ 后台「角色 → 数据范围」里给 `dyn:<集合编码>` 配的规则,
// 对动态集合**配了不生效**(清单 §1.13 #3 记的就是这条)。
//
// # ⚠️ **dyn 的物理表只有一维归属:owner_id**
//
// 系统列只有 id / owner_id / created_at / updated_at / deleted_at,
// 用户字段一律带 `f_` 前缀 —— **没有 project_id,也没有 dept_id**。
//
// ⇒ 「本项目」/「本部门」这两种范围在 dyn 上**表达不出来**。
// 配了会**响亮报错**(见 checkDynScopeDims),而不是静默跳过那一条子句。
//
// # ⚠️ **为什么"跳过那一条子句"是危险动作**
//
// 可见集 = `{ r : 其它条件(r) ∧ (A(r) ∨ B(r)) }` ——
// 丢掉"或"组里的**一个**分支,可见集**变窄**(A 行 ⊆ A∪B 行),
// 方向上是安全的,但仍然是错的:管理员配的"本项目 或 仅本人"被悄悄改成了"仅本人",
// 而**他看不到任何报错**,只会觉得"有些数据怎么不见了"。
//
// 真正危险的是**丢掉最后一个分支** ——
// 那时整段过滤变成"不过滤" ⇒ 可见集 = **全部行** ⇒ **数据越权**。
//
// 而"角色只配了一条规则"恰恰是**最常见**的配置
// (就配一个「仅本人」或一个「本部门」,谁会配两条?)⇒
// **最容易踩中的正是最危险的那一种**。
//
// ⇒ 所以表达不出来就报错,绝不静默跳过。
//
// (pkg/scope 那一层也有同一道闸:`ApplySQL` 在"有限制但一条子句都表达不出来"时
//  返回 `1 = 0` 而不是空串 —— **匹配不到任何行** vs **不过滤**,差一个安全事故。)
//
// # ⚠️ **「仅本人」与 owner_scoped 是两回事,别混**
//
//   - owner_scoped:集合设计里的一个开关,**只对非超管**生效
//   - 数据范围「仅本人」:角色上的规则,**跟是不是超管无关**
//
// 两者是**且**的关系:两条都成立才看得见。配了不冲突。
//
// # ⚠️ **业务域用集合编码现算,不从 ctx 读**
//
// handler 的 dynGuard 也往 ctx 里写了业务域(scope.WithModule),
// 但这里**不读 ctx** —— service 方法自己就知道集合编码(code 参数),
// 而 ctx 里有没有、是不是同一个集合,取决于调用方走没走 handler。
// 直接调 service 的场景(内部任务 / 单测)ctx 里没有业务域 ⇒
// 读到空串 = 回落"默认规则组" ⇒ `dyn:<编码>` 上配的规则**整组不生效**,
// 而且**没有任何报错**。
//
// ⇒ 用同一个构造函数(dynx.RecordModule)现算,跟 handler 那边逐字相同。

package service

import (
	"context"
	"strings"

	"rapid-admin/internal/pkg/dynx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/scope"
)

// dynScopeColumns dyn 物理表上的作用域列
//
// ⚠️ **只有「归属人」一维** —— dyn 物理表没有 project_id / dept_id 列
// (系统列只有 id / owner_id / created_at / updated_at / deleted_at)。
//
// ⇒ 「本项目」/「本部门」在这里**故意留空**:
// 留空不是"不过滤",而是"这一维落不到这张表上" ——
// scope.ApplySQL 会据此报错(pkg/scope/sql.go 的 listExpr),
// 而我们提前一步给出更懂业务的提示(见 checkDynScopeDims)。
//
// ⚠️ **列名带反引号** —— 与 dyn 其余 SQL 一致(MySQL / SQLite 都认)。
func dynScopeColumns() scope.Columns {
	return scope.Columns{
		Self: "`" + dynColOwnerID + "` IN ?",
	}
}

// checkDynScopeDims 检查这条作用域用到的维度在 dyn 上表达得出来
//
// ⚠️ **表达不出来要响亮报错,不能静默跳过那一条** —— 见文件头的说明。
//
// # ⚠️ **判据是"解析出来的取值",不是"规则里写了什么"**
//
// 「本部门」规则 + 调用方**没有部门** ⇒ 解析出来的 DeptIDs 是空的
// ⇒ 这一维不产生任何子句 ⇒ 最终匹配不到任何行(**安全方向**)。
// 而「本部门」规则 + 调用方**有部门** ⇒ 这一维表达不出来 ⇒ 报错。
//
// ⇒ 同一条规则,不同的人看到的结果不同(一个报错、一个空列表)。
// 这是刻意的:报错的那些人是"配置真的坏了",空列表的那些人是"他本来就没有可见数据"。
func checkDynScopeDims(sc *scope.Scope, code string) error {
	if sc == nil {
		return nil
	}
	var missing []string
	if len(sc.ProjectIDs) > 0 {
		missing = append(missing, "本项目")
	}
	if len(sc.DeptIDs) > 0 {
		missing = append(missing, "本部门")
	}
	if len(missing) == 0 {
		return nil
	}
	return errcode.New(errcode.Internal,
		"集合 "+code+" 配了「"+strings.Join(missing, "、")+"」数据范围,"+
			"但动态集合的物理表没有项目/部门维度的列(只有归属人 owner_id 一维)").
		WithDetail("动态集合能用的数据范围:仅本人、自定义(自定义列填物理列名,如 f_project)")
}

// dynScopeCond 记录查询上的数据范围条件
//
// ⚠️ **返回空串 = 不过滤** —— 与 pkg/scope 的约定一致。
//
// 而 "1 = 0" = 看得见零行,那也是合法返回值,调用方照样拼进 WHERE。
// 两者的区别见 pkg/scope/sql.go 的文件头。
//
// # ⚠️ **不走 ScopeService.Apply,自己拼** ——
// 两个原因:
//  1. Apply 吐的是 GORM 链式,dyn 是裸 SQL,形状接不上(M159 补的 ApplySQL 才是给它的)
//  2. Apply 从 ctx 读业务域 —— dyn 的 service 方法自己就知道集合编码,
//     走 ctx 会引入"调用方没走 handler ⇒ 业务域为空 ⇒ 规则整组不生效"的坑
//
// ⇒ 这里显式取作用域(EffectiveScope)+ 显式编译(scope.ApplySQL),
// 语义与 scopeService.Apply 完全一致(后者就是这两步 + 一个 GORM 外壳)。
func (s *dynService) dynScopeCond(ctx context.Context, caller *Caller, code string) (string, []any, error) {
	if s.scopeSvc == nil || caller == nil {
		// 没装配 scope 服务 ⇒ 不过滤(与 scope_impl.go 的 applyScope helper 同一套兜底)
		return "", nil, nil
	}
	sc, err := s.scopeSvc.EffectiveScope(ctx, caller, dynx.RecordModule(code))
	if err != nil {
		return "", nil, err
	}
	if err := checkDynScopeDims(&sc, code); err != nil {
		return "", nil, err
	}
	frag, args, err := scope.ApplySQL(&sc, dynScopeColumns())
	if err != nil {
		return "", nil, errcode.New(errcode.Internal,
			"数据范围规则在集合 "+code+" 上落不下去").WithDetail(err.Error())
	}
	return frag, args, nil
}
