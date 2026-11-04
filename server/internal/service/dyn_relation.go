// dyn_relation.go —— 单选 / 多选的「关联表选项来源」
//
// # 这层解决什么
//
// 单选 / 多选的选项以前只有一种来源:设计器里手工逐条填。
// 于是"所属部门""负责人"这类**本来就在系统表里**的选项,
// 只能被一份一份地抄进设计器 —— 抄一遍就多一份不同步的真相。
//
// ⇒ 这里给第二种来源:**关联系统表**。
// 选项在**读表单结构的时候现查** ⇒ 系统表里新增一行,表单里立刻多一个选项。
//
// # ⚠️ **权限是第一道闸,不是最后一道**
//
// 关联表的选项是**现查**的 ⇒ "能不能读到那张表"必须在**查之前**判,
// 而不是"查了再说"(先偷看再问能不能看)。
//
// 三层闸门,顺序固定:
//
//	① 资源码:能不能调那张表的列表接口(user:list / role:list / dept:list)
//	② 数据范围:能看到那张表的哪些行(本部门 / 仅本人)
//	③ 降级:读不到就返回空选项 + 标记,绝不让整个表单结构失败
//
// # ⚠️ **判定在 handler 层做,不在这里做**
//
// 权限判定走 `middleware.CanAccess` —— **执行层的唯一一份实现**。
// 而 `internal/api/middleware` 依赖 `internal/service`,
// service 反过来 import 就是**循环依赖**。
// ⇒ handler 判完把结果(`readable`)传进来,与看板可见性同一套路。
//
// # ⚠️ **写入时不校验"这个值在不在表里"**
//
// 手工选项集在写入时校验成员资格(值必须在那份选项集里)——
// 因为手工选项集是"死的",写进去的值永远应该在里面。
//
// 关联表是"活的":表里的行会被删。⇒ 若也校验成员资格,
// **一条历史记录只要它引用的那一行被删了,这条记录就再也存不进去任何修改**
// (用户只想改个备注,后端报"不是合法选项")——
// 这比"多存了一个野值"严重得多。
//
// ⇒ 关联表来源只校验值的形状(非空文本)。
// 代价:能存进表里不存在的 id ⇒ 列表页那一格显示空白标签。
// (设计文档 §2.10 的取舍记录)

package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/dynx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/scope"
)

// relationOptionLimit 一个关联表来源最多取多少个候选
//
// ⚠️ **超了要截断并标记,不能静默截断** ——
// 用户配了"部门"来源,部门有 500 个,下拉框里只有 200 个,
// 而他不知道少了 300 个 ⇒ 他会以为"部门只有这些"。
const relationOptionLimit = 200

// DynOptionSet 一个字段的选项集(读的时候解析出来的)
//
// ⚠️ **手工选项集与关联表来源共用这一个结构** ——
// 前端只认"这个字段有哪些候选",不关心候选是哪来的。
// 来源信息另给(Readable / RequiredPerm),让前端能说明"为什么是空的"。
type DynOptionSet struct {
	// Choices 候选值(手工配置的那份,或关联表查出来的那份)
	Choices []dynx.Choice `json:"choices"`
	// Source 来源 key(空串 = 手工配置的选项集)
	Source string `json:"source,omitempty"`
	// Readable 关联表来源时:当前调用方能不能读那张表。
	//
	// ⚠️ **手工选项集恒为 true** —— 它是设计者写死在字段定义里的,
	// 不存在"读不到"这回事。
	Readable bool `json:"readable"`
	// RequiredPerm 读不到时缺哪个权限码(前端提示用)
	RequiredPerm string `json:"required_perm,omitempty"`
	// Truncated 候选被截断了(关联表来源,行数超过上限)
	Truncated bool `json:"truncated,omitempty"`
}

// relationScopeColumns 来源表上的数据范围列
//
// ⚠️ **"user" 复用用户列表那一套,不改口径** ——
// 同一个人,在「用户管理」页看到的和在下拉框里看到的必须是同一批人。
// 两处各写一套必然漂移,而漂移的表现是"下拉框里有个人,用户管理页里没有"。
var relationScopeColumns = map[string]scope.Columns{
	"user": userScopeColumns,
}

// ResolveFieldOptions 解析一组字段的选项集
//
// ⚠️ **readable 是调用方(handler)给的权限判定结果** ——
// key 是来源 key,值是"当前调用方能不能读那张表"。
// 判定走 middleware.CanAccess,在 handler 层做(见文件头的说明)。
//
// ⚠️ **任何一个字段解析失败都不许拖垮其它字段** ——
// 一个字段的选项集坏了 / 那张表查不到 ⇒ 那个字段的选项是空的,
// 其余字段照常返回。整页表单因为一个下拉框读不出来就白屏,
// 是比"这个下拉框是空的"严重得多的事故。
func (s *dynService) ResolveFieldOptions(
	ctx context.Context,
	caller *Caller,
	fields []model.SysDynField,
	readable map[string]bool,
) (map[string]DynOptionSet, error) {
	out := make(map[string]DynOptionSet, len(fields))
	for i := range fields {
		f := &fields[i]
		out[f.FieldKey] = s.resolveOneOptionSet(ctx, caller, f, readable)
	}
	return out, nil
}

// resolveOneOptionSet 解析一个字段的选项集
func (s *dynService) resolveOneOptionSet(
	ctx context.Context,
	caller *Caller,
	f *model.SysDynField,
	readable map[string]bool,
) DynOptionSet {
	opts, err := dynx.ParseOptions(f.Options)
	if err != nil {
		// ⚠️ **选项集坏了就当它没有选项** —— 不报错:
		// 选项集是设计期的东西,坏了也不该让录入页整页打不开。
		logger.L().Warn("dyn: 选项集解析失败,按空选项处理",
			zap.String("field_key", f.FieldKey),
			zap.String("collection_id", strconv.FormatUint(f.CollectionID, 10)),
			zap.Error(err))
		return DynOptionSet{Choices: []dynx.Choice{}, Readable: true}
	}
	// 手工选项集:原样返回;relation 字段走"目标集合记录"那条来源
	if !opts.HasSource() {
		if dynx.Type(f.Type) == dynx.TypeRelation && opts.Relation != nil {
			return s.relationFieldOptions(ctx, caller, f, opts.Relation, readable)
		}
		return DynOptionSet{Choices: opts.Choices, Readable: true}
	}
	src, ok := dynx.LookupRelationSource(opts.Source.Key)
	if !ok {
		// ⚠️ **来源不在注册表里了**(老配置被删)⇒ 降级,不报错
		logger.L().Warn("dyn: 选项来源不在注册表里,按不可读处理",
			zap.String("field_key", f.FieldKey),
			zap.String("source", opts.Source.Key))
		return DynOptionSet{
			Choices:      []dynx.Choice{},
			Source:       opts.Source.Key,
			Readable:     false,
			RequiredPerm: "",
		}
	}
	// ⚠️ **权限闸门在查表之前** —— 先问能不能看,再看
	if !readable[src.Key] {
		return DynOptionSet{
			Choices:      []dynx.Choice{},
			Source:       src.Key,
			Readable:     false,
			RequiredPerm: src.ResourceCode,
		}
	}
	choices, truncated, err := s.relationChoices(ctx, caller, src)
	if err != nil {
		// ⚠️ **查表失败 ⇒ 降级成空选项**,不让整个表单结构失败
		logger.L().Warn("dyn: 关联表选项查询失败,按空选项处理",
			zap.String("source", src.Key),
			zap.String("field_key", f.FieldKey),
			zap.Error(err))
		return DynOptionSet{Choices: []dynx.Choice{}, Source: src.Key, Readable: true}
	}
	return DynOptionSet{
		Choices:   choices,
		Source:    src.Key,
		Readable:  true,
		Truncated: truncated,
	}
}

// relationChoices 查一张来源表的候选项
//
// ⚠️ **值列与显示名列来自注册表,不是用户输入** ——
// SQL 是拼出来的,拼进去的每个标识符都必须来自白名单。
func (s *dynService) relationChoices(
	ctx context.Context,
	caller *Caller,
	src dynx.RelationSource,
) ([]dynx.Choice, bool, error) {
	where := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if src.WhereSQL != "" {
		where = append(where, "("+src.WhereSQL+")")
	}
	// ⚠️ **数据范围在静态条件之后** —— 参数顺序必须与占位符顺序一致,
	// 而静态条件里没有占位符,所以直接 append 是安全的。
	frag, sargs, err := s.relationScopeCond(ctx, caller, src)
	if err != nil {
		return nil, false, err
	}
	if frag != "" {
		where = append(where, frag)
		args = append(args, sargs...)
	}
	// ⚠️ **多取一行用来判断"有没有被截断"** ——
	// 只按上限查的话,查满 200 行与"刚好 200 行"分不出来,
	// 用户永远不知道自己漏看了后面的。
	//
	// ⚠️ **别名避开 `value` / `label` 这类词** ——
	// `value` 在 MySQL 里是保留字;别名撞保留字时两种方言的解析还不一样。
	sql := "SELECT t.`" + src.ValueColumn + "` AS opt_value, " + src.LabelSQL + " AS opt_label FROM `" +
		src.Table + "` t"
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	sql += " ORDER BY opt_label ASC, t.`" + src.ValueColumn + "` ASC LIMIT " +
		strconv.Itoa(relationOptionLimit+1)

	// ⚠️ **扫进 map,不扫进结构体** ——
	// 取值列的类型随表而变(id 是整数,别的来源可能是字符串),
	// 结构体字段写死一种类型就有一半的来源扫不进去
	// (GORM 会报 "unsupported data type")。
	var rows []map[string]any
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, false, err
	}
	truncated := len(rows) > relationOptionLimit
	if truncated {
		rows = rows[:relationOptionLimit]
	}
	out := make([]dynx.Choice, 0, len(rows))
	for _, r := range rows {
		value := dynValueText(r["opt_value"])
		if value == "" {
			continue
		}
		out = append(out, dynx.Choice{Value: value, Label: dynValueText(r["opt_label"])})
	}
	return out, truncated, nil
}

// dynValueText 把一列的值转成文本
//
// ⚠️ **id 是数字 ⇒ 存进记录的是它的十进制字符串** ——
// 单选字段的列是 VARCHAR,值统一用字符串形态,
// 这样"手工选项集的值"与"关联表的值"是同一种形态(前端不用分支处理)。
func dynValueText(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []byte:
		return strings.TrimSpace(string(t))
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

// relationScopeCond 来源表上的数据范围条件
//
// ⚠️ **返回空串 = 不过滤** —— 与 pkg/scope 的约定一致。
//
// ⚠️ **业务域用来源的模块名现算,不从 ctx 读** ——
// ctx 里那个业务域是**当前集合**的(`dyn:<集合码>`),
// 而这里要的是**来源表**的业务域(`user`)。读 ctx 会套错规则。
func (s *dynService) relationScopeCond(
	ctx context.Context,
	caller *Caller,
	src dynx.RelationSource,
) (string, []any, error) {
	cols, ok := relationScopeColumns[src.Key]
	if !ok || s.scopeSvc == nil || caller == nil {
		// 没配数据范围列 / 没装 scope 服务 ⇒ 不过滤
		return "", nil, nil
	}
	sc, err := s.scopeSvc.EffectiveScope(ctx, caller, src.Module)
	if err != nil {
		return "", nil, err
	}
	frag, args, err := scope.ApplySQL(&sc, cols)
	if err != nil {
		return "", nil, errcode.New(errcode.Internal,
			"数据范围规则在选项来源 "+src.Key+" 上落不下去").WithDetail(err.Error())
	}
	return frag, args, nil
}

// relationFieldOptions relation 字段的候选项(目标集合的记录)
//
// ⚠️ **与系统表来源同一套三层闸门,一层不少** ——
// ① 资源码:能不能进目标集合的列表页(dyn:<目标>:list)
// ② 数据范围:目标集合配了数据范围规则就照常注入
//
//	(dynScopeCond 按目标集合码现算模块,与 relationScopeCond 同理,不从 ctx 读)
//
// ③ 降级:目标集合被停用 / 查询失败 ⇒ 空选项,不让表单结构失败
//
// ⚠️ **Source 带 `collection:` 前缀** ——
// 前端用它区分"系统表来源"与"集合来源"。
func (s *dynService) relationFieldOptions(
	ctx context.Context,
	caller *Caller,
	f *model.SysDynField,
	rel *dynx.RelationOptions,
	readable map[string]bool,
) DynOptionSet {
	target := strings.TrimSpace(rel.Target)
	if target == "" {
		return DynOptionSet{Choices: []dynx.Choice{}, Readable: true}
	}
	key := "collection:" + target
	// ① 权限闸门在查表之前 —— 同 resolveOneOptionSet 的顺序,不是"查了再问"
	if !readable[key] {
		return DynOptionSet{
			Choices:      []dynx.Choice{},
			Source:       key,
			Readable:     false,
			RequiredPerm: "dyn:" + target + ":list",
		}
	}
	// ③ 目标集合不在了 / 被停用 ⇒ 降级空选项 ——
	// 这是设计问题(集合被下线),不是当前调用方的权限问题,不给 perm 提示。
	var col model.SysDynCollection
	err := s.db.WithContext(ctx).Where("code = ? AND is_active = ?", target, true).First(&col).Error
	if err != nil {
		return DynOptionSet{Choices: []dynx.Choice{}, Source: key, Readable: true}
	}
	displayCol := s.relationDisplayColumn(ctx, &col, rel.DisplayField)
	// ⚠️ **集合里没有任何可显示的文本字段 ⇒ 候选标签回落成 id** ——
	// 下拉框里显示一串 id 不好看,但比"下拉框整个是空的"强:
	// 用户至少还能选,选了存的是合法引用。
	if displayCol == "" {
		sql := "SELECT `id` AS opt_value, `id` AS opt_label FROM `" + col.PhysTable +
			"` WHERE `deleted_at` IS NULL"
		args := make([]any, 0, 4)
		if frag, sargs, err := s.dynScopeCond(ctx, caller, target); err != nil {
			logger.L().Warn("dyn: 关联字段候选的数据范围失败,按空选项处理",
				zap.String("field_key", f.FieldKey),
				zap.String("target", target),
				zap.Error(err))
			return DynOptionSet{Choices: []dynx.Choice{}, Source: key, Readable: true}
		} else if frag != "" {
			sql += " AND " + frag
			args = append(args, sargs...)
		}
		var rows []map[string]any
		if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
			logger.L().Warn("dyn: 关联字段候选查询失败,按空选项处理",
				zap.String("field_key", f.FieldKey),
				zap.String("target", target),
				zap.Error(err))
			rows = nil
		}
		return s.buildRelationChoices(rows, key, target)
	}
	sql := "SELECT `id` AS opt_value, COALESCE(`" + displayCol + "`,'') AS opt_label FROM `" +
		col.PhysTable + "` WHERE `deleted_at` IS NULL"
	args := make([]any, 0, 4)
	if frag, sargs, err := s.dynScopeCond(ctx, caller, target); err != nil {
		// ⚠️ **数据范围算不出来 ⇒ 降级,不猜"全部可见"** ——
		// 猜错就是越权泄露(别人部门的记录进了下拉框)。
		logger.L().Warn("dyn: 关联字段候选的数据范围失败,按空选项处理",
			zap.String("field_key", f.FieldKey),
			zap.String("target", target),
			zap.Error(err))
		return DynOptionSet{Choices: []dynx.Choice{}, Source: key, Readable: true}
	} else if frag != "" {
		sql += " AND " + frag
		args = append(args, sargs...)
	}
	sql += " ORDER BY opt_label ASC, opt_value ASC LIMIT " + strconv.Itoa(relationOptionLimit+1)
	var rows []map[string]any
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		// ⚠️ **查表失败 ⇒ 降级成空选项**,不让整个表单结构失败
		logger.L().Warn("dyn: 关联字段候选查询失败,按空选项处理",
			zap.String("field_key", f.FieldKey),
			zap.String("target", target),
			zap.Error(err))
		rows = nil
	}
	return s.buildRelationChoices(rows, key, target)
}

// relationDisplayColumn 解析目标集合的显示列
//
// 设计期(ValidateRelationConfig)校验过它存在;运行期只做宽容回退 ——
// 显示字段被停用 ⇒ 回落第一个在用的文本字段;都没有 ⇒ 空串(候选标签回落成 id)。
// 不让"管理员停了个字段"把别的集合的录入页打挂(旁路纪律)。
func (s *dynService) relationDisplayColumn(ctx context.Context, col *model.SysDynCollection, displayField string) string {
	fields, err := s.activeFields(ctx, col.ID)
	if err != nil || len(fields) == 0 {
		return ""
	}
	want := strings.TrimSpace(displayField)
	if want != "" {
		for i := range fields {
			if fields[i].FieldKey == want {
				return fields[i].Column
			}
		}
	}
	for i := range fields {
		if dynx.Type(fields[i].Type) == dynx.TypeText {
			return fields[i].Column
		}
	}
	return ""
}

// buildRelationChoices 查询行 → 候选项(含截断标记)
//
// ⚠️ **标签为空的行要给标签** —— 目标记录的显示列恰好是空串时,
// 下拉框会出现一个"空白选项"(选了也不知道选了谁)⇒ 标签回落成 id。
func (s *dynService) buildRelationChoices(rows []map[string]any, key, target string) DynOptionSet {
	truncated := len(rows) > relationOptionLimit
	if truncated {
		rows = rows[:relationOptionLimit]
	}
	choices := make([]dynx.Choice, 0, len(rows))
	for _, r := range rows {
		value := dynValueText(r["opt_value"])
		if value == "" {
			continue
		}
		label := dynValueText(r["opt_label"])
		if label == "" {
			label = value
		}
		choices = append(choices, dynx.Choice{Value: value, Label: label})
	}
	return DynOptionSet{
		Choices:   choices,
		Source:    key,
		Readable:  true,
		Truncated: truncated,
	}
}
