// 动态集合的数据面 —— 通用记录 CRUD
//
// # 记录长什么样
//
// 物理表上的行是「系统列 + 用户列」,对外的 JSON 是:
//
//	{
//	  "id": "1730000000000000000",   -- 字符串(本仓 uint64 上 wire 的统一口径)
//	  "owner_id": "1001",
//	  "owner_name": "张三",           -- 创建人显示名(取不到时是空串)
//	  "created_at": "2026-11-01T00:30:00Z",
//	  "updated_at": "2026-11-01T00:30:00Z",
//	  "<字段标识>": <值>,             -- 键是用户起的字段标识,不是物理列名
//	  ...
//	}
//
// ⚠️ **JSON 的键是字段标识,不是物理列名(f_xxx)** ——
// 前端拿到的是用户写的名字,不用知道物理层长什么样。
//
// # 值的进出都过 dynx
//
// 写:前端 JSON → dynx.Normalize → 落库
// 读:落库 → dynx.Denormalize → 前端 JSON
//
// ⚠️ **不认识的字段名直接报错** ——
// 静默丢弃的话,用户改了个字段名提交,后端返回"成功",
// 而数据根本没存进去。这种错要当场炸。
package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/dynfilter"
	"rapid-admin/internal/pkg/dynx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
)

// dynTimeLayout 落库的时间格式(UTC ISO-8601)
const dynTimeLayout = "2006-01-02T15:04:05Z"

// dynRecordCtx 一次记录操作需要的上下文(集合 + 在用字段)
type dynRecordCtx struct {
	col    *model.SysDynCollection
	fields []model.SysDynField
	byKey  map[string]model.SysDynField
}

// loadRecordCtx 取集合与在用字段
func (s *dynService) loadRecordCtx(ctx context.Context, code string) (*dynRecordCtx, error) {
	col, err := s.GetCollectionByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return s.recordCtxFromCol(ctx, col)
}

// recordCtxFromCol 记录上下文装配(集合行 → 在用字段 + 索引 map)
func (s *dynService) recordCtxFromCol(ctx context.Context, col *model.SysDynCollection) (*dynRecordCtx, error) {
	if !col.IsActive {
		return nil, errcode.New(errcode.NotFound, "集合不存在")
	}
	fields, err := s.activeFields(ctx, col.ID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]model.SysDynField, len(fields))
	for _, f := range fields {
		byKey[f.FieldKey] = f
	}
	return &dynRecordCtx{col: col, fields: fields, byKey: byKey}, nil
}

// selectCols SELECT 的列清单(系统列 + 在用字段的列)
func (rc *dynRecordCtx) selectCols() []string {
	cols := []string{
		"`" + dynColID + "`",
		"`" + dynColOwnerID + "`",
		"`" + dynColCreatedAt + "`",
		"`" + dynColUpdatedAt + "`",
	}
	for _, f := range rc.fields {
		cols = append(cols, "`"+f.Column+"`")
	}
	return cols
}

// toView 行 → 对外的 JSON 形状
//
// ⚠️ **owner_name 是"创建人"的显示名,不是 owner_id 的另一种写法** ——
//
//	owner_id 是数字 id,界面上显示一串数字没人认得;
//	显示名取不到(用户被删了 / id 是 0)时留空串,前端回落到显示 owner_id。
//
// ⚠️ **files 是附件字段的展示信息(原文件名 + 直链)** ——
// 字段值本身只是文件的 key,看不出原文件名,也拼不出 CDN 前缀。
// ⇒ 这两样由后端解析好放在 `_files` 里(见 dynFileRefs)。
//
// ⚠️ **rels 是 relation 字段的显示标签(键 = `字段标识|id`)** ——
// 关联字段存的是目标记录 id,界面上显示一串数字没人认得;
// 由后端批量解析(dynRels)后按行挑出各自的标签,放 `_rels`。
// 取不到标签的行不给键,前端回落显示 id。
func (rc *dynRecordCtx) toView(row map[string]any, ownerNames map[string]string, files map[string]FileRef, rels map[string]string) map[string]any {
	ownerID := dynIDString(row[dynColOwnerID])
	out := map[string]any{
		"id":         dynIDString(row[dynColID]),
		"owner_id":   ownerID,
		"owner_name": ownerNames[ownerID],
		"created_at": dynText(row[dynColCreatedAt]),
		"updated_at": dynText(row[dynColUpdatedAt]),
	}
	for _, f := range rc.fields {
		out[f.FieldKey] = dynx.Denormalize(dynx.Type(f.Type), row[f.Column])
	}
	// ⚠️ **只有真有附件字段才放 `_files`** ——
	// 没有附件的记录不该多一个空对象(前端要多判一次"这个键在不在")。
	if len(files) > 0 {
		out["_files"] = files
	}
	// ⚠️ **_rels 按行挑出各自的关联标签** ——
	// dynRels 给的是"字段|id → 标签"的全批映射,这里只放本行引用到的那些;
	// 没有一个 relation 字段取到标签就不加键(同 `_files` 的省键口径)。
	if len(rels) > 0 {
		m := make(map[string]string, 2)
		for _, f := range rc.fields {
			if dynx.Type(f.Type) != dynx.TypeRelation {
				continue
			}
			id := dynText(row[f.Column])
			if id == "" {
				continue
			}
			if label, ok := rels[f.FieldKey+"|"+id]; ok {
				m[f.FieldKey] = label
			}
		}
		if len(m) > 0 {
			out["_rels"] = m
		}
	}
	return out
}

// dynFileRefs 批量解析这一批记录里的附件(原文件名 + 直链)
//
// ⚠️ **一次 IN 查询,不按 key 逐个查** ——
// 一页 20 条记录、每条 2 个附件 = 40 次查询,那是 N+1。
//
// ⚠️ **解析不到就不给这一项** ——
// 文件被删了 / key 写错了,前端显示原始 key 并标成"文件已失效"。
// 不报错(展示用的东西取不到就降级)。
//
// ⚠️ **没有装文件服务时返回空** ——
// 单测 / 精简装配下 dyn 仍然能用(附件字段的值照常读写,只是没有展示信息)。
func (s *dynService) dynFileRefs(ctx context.Context, rc *dynRecordCtx, rows []map[string]any) map[string]FileRef {
	if s.files == nil || rc == nil || len(rows) == 0 {
		return nil
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		for _, f := range rc.fields {
			if f.Type != string(dynx.TypeFile) {
				continue
			}
			v := dynText(row[f.Column])
			if v != "" {
				keys = append(keys, v)
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	refs, err := s.files.ResolveKeys(ctx, keys)
	if err != nil {
		logger.L().Warn("dyn: 解析附件失败,附件将只显示 key",
			zap.Int("file_count", len(keys)),
			zap.Error(err))
		return nil
	}
	return refs
}

// dynRowFiles 取一行记录自己的附件展示信息
//
// ⚠️ **按这一行的值过滤** ——
// 解析结果是一批 key 的映射,而每条记录只有自己的那几个附件。
func dynRowFiles(rc *dynRecordCtx, row map[string]any, refs map[string]FileRef) map[string]FileRef {
	if len(refs) == 0 || rc == nil {
		return nil
	}
	out := make(map[string]FileRef)
	for _, f := range rc.fields {
		if f.Type != string(dynx.TypeFile) {
			continue
		}
		key := dynText(row[f.Column])
		if key == "" {
			continue
		}
		ref, ok := refs[key]
		if !ok {
			continue
		}
		out[f.FieldKey] = ref
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// dynRels 批量解析这一批记录里的 relation 字段显示标签
//
// ⚠️ **每个 relation 字段一次 IN 查询,不按行逐个查** —— 与 dynFileRefs 同款防 N+1。
//
// ⚠️ **无条件展开(不另设权限闸)** —— 口径同 owner_name:
// 设计者选定的那个显示字段是"渲染这一条记录"的一部分,
// 批量暴露只发生在候选项接口(relationFieldOptions),那边有资源码闸门。
//
// ⚠️ **取不到标签就不给这一项** ——
// 目标记录被删 / 显示字段停用且没有文本字段可回退 ⇒ 前端回落显示 id,
// 不报错(展示用的东西取不到就降级,同 dynFileRefs)。
func (s *dynService) dynRels(ctx context.Context, rc *dynRecordCtx, rows []map[string]any) map[string]string {
	out := make(map[string]string)
	for _, f := range rc.fields {
		if dynx.Type(f.Type) != dynx.TypeRelation {
			continue
		}
		opts, err := dynx.ParseOptions(f.Options)
		if err != nil || opts.Relation == nil {
			continue
		}
		target := strings.TrimSpace(opts.Relation.Target)
		if target == "" {
			continue
		}
		// 去重收集本字段引用到的全部 id
		ids := make([]string, 0, len(rows))
		seen := make(map[string]bool, len(rows))
		for _, row := range rows {
			id := dynText(row[f.Column])
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			continue
		}
		var col model.SysDynCollection
		if err := s.db.WithContext(ctx).Where("code = ? AND is_active = ?", target, true).First(&col).Error; err != nil {
			// 目标集合被停用 / 不存在 ⇒ 标签取不到,前端显示 id
			continue
		}
		displayCol := s.relationDisplayColumn(ctx, &col, opts.Relation.DisplayField)
		if displayCol == "" {
			continue
		}
		iargs := make([]any, 0, len(ids))
		for _, id := range ids {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				continue
			}
			iargs = append(iargs, n)
		}
		if len(iargs) == 0 {
			continue
		}
		ph := strings.TrimSuffix(strings.Repeat("?, ", len(iargs)), ", ")
		sql := "SELECT `id`, COALESCE(`" + displayCol + "`,'') AS opt_label FROM `" + col.PhysTable +
			"` WHERE `deleted_at` IS NULL AND `id` IN (" + ph + ")"
		var rrows []map[string]any
		if err := s.db.WithContext(ctx).Raw(sql, iargs...).Scan(&rrows).Error; err != nil {
			logger.L().Warn("dyn: 关联标签查询失败,列表将显示 id",
				zap.String("field_key", f.FieldKey),
				zap.String("target", target),
				zap.Error(err))
			continue
		}
		for _, r := range rrows {
			id := dynValueText(r["id"])
			label := dynValueText(r["opt_label"])
			if id == "" || label == "" {
				// 空标签不给键 ⇒ 前端回落显示 id
				continue
			}
			out[f.FieldKey+"|"+id] = label
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// dynOwnerNames 批量解析 owner_id → 用户显示名
//
// ⚠️ **显示名口径与全仓一致** —— 真实姓名 → 昵称 → 账号,第一个非空的胜出
//
//	(同 config_center_impl.go 的 operatorNameSQL)。
//	只取 real_name 的话,后台账号的 real_name 长期是空的 ⇒ 这一列永远是空白。
//
// ⚠️ **key 是字符串形式的 id** —— 调用方手上就是 dynIDString 的产物,
//
//	两边形态必须一致(approval_query_impl.go 的 userNames 同理)。
//
// ⚠️ **为什么在后端解析而不是前端拉用户列表** ——
//
//	用户列表接口要 `user:list` 权限,业务人员(恰恰是 dyn 数据面的主力)没有 ⇒
//	前端拿不到名字,这一列对绝大多数人永远是空的。
//
// 🔴 **取不到名字不许让请求失败** —— 记录已经落库了,显示名只是列表里的一列,
//
//	前端会回落到显示 owner_id。只记日志(同 approval_inbox.go 的旁路纪律)。
func (s *dynService) dynOwnerNames(ctx context.Context, ids []string) map[string]string {
	out := make(map[string]string, len(ids))
	uniq := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, raw := range ids {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out
	}
	var rows []model.SysUser
	if err := s.db.WithContext(ctx).
		Select("id", "username", "nickname", "real_name").
		Where("id IN ?", uniq).
		Find(&rows).Error; err != nil {
		logger.L().Warn("dyn: 查询创建人显示名失败,这一列将显示 id",
			zap.Int("owner_count", len(uniq)),
			zap.Error(err))
		return out
	}
	for _, u := range rows {
		name := strings.TrimSpace(u.RealName)
		if name == "" {
			name = strings.TrimSpace(u.Nickname)
		}
		if name == "" {
			name = u.Username
		}
		out[strconv.FormatUint(u.ID, 10)] = name
	}
	return out
}

// ListRecords 记录列表
func (s *dynService) ListRecords(ctx context.Context, caller *Caller, code string, q DynRecordQuery) (*model.PageResponse, error) {
	if _, err := requireUser(caller); err != nil {
		return nil, err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return nil, err
	}
	return s.listRecordsInto(ctx, caller, rc, q)
}

// ── 公开只读(2026-11-03)──────────────────────────────────────────
//
// 集合 public_read = true 时,未登录经 /rapid-admin/v1/public/dyn/** 读。
// 合成调用方 RoleCode = "public" 什么权限都没有 ⇒
// 行级(默认无)、relation 选项(读不到 → option_readable=false)全部走
// 既有降级路径,不需要为新 caller 写一行特判。

// publicCaller 公开只读请求的合成调用方(UserID=0,无角色无权限)
func publicCaller() *Caller {
	return &Caller{UserID: 0, RoleCode: "public", Username: "public"}
}

// publicRecordCtx 公开只读闸门 + 记录上下文:
// 集合在用 + public_read + 非 owner_scoped,不满足按未登录处理
// (不区分"不存在"和"不能公开读",不给枚举探测留信号)。
func (s *dynService) publicRecordCtx(ctx context.Context, code string) (*dynRecordCtx, error) {
	col, err := s.PublicReadCollection(ctx, code)
	if err != nil {
		return nil, err
	}
	return s.recordCtxFromCol(ctx, col)
}

// PublicListRecords 公开只读列表(与 ListRecords 同一套查询口径,含筛选 DSL)
//
// ⚠️ **公开路径强制看正常视图(Trash 恒 false)** ——
// trash 开关本身是"能删的才配恢复"的回收站入口,是登录用户的工具;
// 翻成 SQL 就是 `deleted_at IS NOT NULL`,放行等于让匿名请求翻回收站。
func (s *dynService) PublicListRecords(ctx context.Context, code string, q DynRecordQuery) (*model.PageResponse, error) {
	q.Trash = false
	rc, err := s.publicRecordCtx(ctx, code)
	if err != nil {
		return nil, err
	}
	return s.listRecordsInto(ctx, publicCaller(), rc, q)
}

// PublicGetRecord 公开只读详情
func (s *dynService) PublicGetRecord(ctx context.Context, code string, id uint64) (map[string]any, error) {
	rc, err := s.publicRecordCtx(ctx, code)
	if err != nil {
		return nil, err
	}
	return s.getRecordInto(ctx, publicCaller(), rc, id)
}

// listRecordsInto 列表查询主体(登录 / 公开两条路径共用;rc 由调用方装好)
func (s *dynService) listRecordsInto(ctx context.Context, caller *Caller, rc *dynRecordCtx, q DynRecordQuery) (*model.PageResponse, error) {
	page, size := normPage(q.Page, q.Size, 200)
	where, args, err := s.recordWhere(ctx, rc, caller, q)
	if err != nil {
		return nil, err
	}
	orderCol, orderDir, err := rc.resolveOrder(q.OrderBy, q.OrderDir)
	if err != nil {
		return nil, err
	}
	// fields 裁剪(M3)—— 序列化后摘键,零 SQL 侵入;先解析(坏参数早失败)
	picked, err := rc.pickFieldSet(q.Fields)
	if err != nil {
		return nil, err
	}

	from := "`" + rc.col.PhysTable + "`"
	// skipTotal(M3)—— 大表上 COUNT 是最贵的一条,"总共多少条"只有第一页有用;
	// 调用方明确不要就跳过,total 用 -1 表达"没统计"(0 是合法的统计结果,
	// 不能拿它当"没查"的哨兵,否则空集合和跳过统计分不出来)。
	var total int64 = -1
	if !q.SkipTotal {
		countSQL := "SELECT COUNT(*) FROM " + from + " WHERE " + strings.Join(where, " AND ")
		if err := s.db.WithContext(ctx).Raw(countSQL, args...).Scan(&total).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "统计记录数失败").WithDetail(err.Error())
		}
	}

	listSQL := "SELECT " + strings.Join(rc.selectCols(), ", ") + " FROM " + from +
		" WHERE " + strings.Join(where, " AND ") +
		" ORDER BY " + orderCol + " " + orderDir + " LIMIT ? OFFSET ?"
	listArgs := make([]any, 0, len(args)+2)
	listArgs = append(listArgs, args...)
	listArgs = append(listArgs, size, (page-1)*size)
	rows, err := s.queryRows(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询记录失败").WithDetail(err.Error())
	}
	// 创建人显示名 —— 一次批量查完,不为每行发一次请求
	ownerIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		ownerIDs = append(ownerIDs, dynIDString(row[dynColOwnerID]))
	}
	ownerNames := s.dynOwnerNames(ctx, ownerIDs)
	// 附件的展示信息 —— 一次批量查完,不为每行发一次请求;
	// 裁剪集合里没有附件字段时整步跳过(白省一次文件服务的往返)
	var fileRefs map[string]FileRef
	if picked == nil || rc.pickedHasFile(picked) {
		fileRefs = s.dynFileRefs(ctx, rc, rows)
	}
	// 关联字段的显示标签 —— 每个 relation 字段一次 IN 查询(防 N+1);
	// 裁剪集合里没有 relation 字段时 dynRels 内部整步跳过
	rels := s.dynRels(ctx, rc, rows)
	list := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		view := rc.toView(row, ownerNames, dynRowFiles(rc, row, fileRefs), rels)
		// 裁剪(M3):不在集合里的用户字段摘掉(系统键 id/owner_id/... 恒保留)
		if picked != nil {
			for _, f := range rc.fields {
				if !picked[f.FieldKey] {
					delete(view, f.FieldKey)
				}
			}
		}
		list = append(list, view)
	}
	// 审批徽标(2026-11-03)—— 集合绑了审批模板时,行上挂 `_approval`
	// (没有审批单的行不加 key;查询失败只记日志,不拦列表)
	s.attachApprovalBadges(ctx, rc, list)
	return &model.PageResponse{List: list, Total: total, Page: page, Size: size}, nil
}

// dynCond 一条 WHERE 条件(条件文本 + 它自己的参数)
//
// ⚠️ **条件与参数必须绑在一起** ——
// 以前是两个平行切片([]string + []any),靠"两边的 append 顺序必须对齐"维系。
// 加一条**带自己参数**的条件(数据范围片段)时,那个顺序极容易错 ——
// 而参数错位**不报错**,只是查出错的数据(拿到别人的行)。
//
// ⇒ 改成"一条条件带一组参数",最后再摊平。
type dynCond struct {
	// sql 条件文本(占位符的值由 args 给)
	sql string
	// args 这条条件自己的参数(顺序与 sql 里的 ? 一一对应)
	args []any
}

// dynWhere 把条件列表摊平成 (WHERE 片段列表, 参数列表)
//
// ⚠️ **摊平后参数顺序 = 条件顺序** ——
// SQL 里 ? 的取值顺序就是参数顺序,两者必须严格对应。
func dynWhere(conds []dynCond) ([]string, []any) {
	where := make([]string, 0, len(conds))
	args := make([]any, 0, len(conds)*2)
	for _, c := range conds {
		where = append(where, c.sql)
		args = append(args, c.args...)
	}
	return where, args
}

// recordWhere 组装 WHERE 条件与参数
func (s *dynService) recordWhere(ctx context.Context, rc *dynRecordCtx, caller *Caller, q DynRecordQuery) ([]string, []any, error) {
	// ⚠️ **trash 开关是恢复动作的入口** ——
	// 软删的记录从普通列表里消失了,不给"只看已删"的开关,恢复按钮没有落点。
	// 回收站视图里数据范围(owner_scoped / dynScopeCond)照常生效:
	// "能删的才配恢复",看不见的行恢复不了。
	deleted := "`" + dynColDeletedAt + "` IS NULL"
	if q.Trash {
		deleted = "`" + dynColDeletedAt + "` IS NOT NULL"
	}
	conds := []dynCond{{sql: deleted}}
	// 行级权限:owner_scoped 的集合只看得见自己创建的记录
	if rc.col.OwnerScoped && !callerCanCrossUserRead(caller) {
		conds = append(conds, dynCond{sql: "`" + dynColOwnerID + "` = ?", args: []any{caller.UserID}})
	}
	if owner := strings.TrimSpace(q.Owner); owner != "" {
		uid, err := strconv.ParseUint(owner, 10, 64)
		if err != nil {
			return nil, nil, errcode.New(errcode.BadRequest, "owner 必须是数字")
		}
		conds = append(conds, dynCond{sql: "`" + dynColOwnerID + "` = ?", args: []any{uid}})
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		ors := make([]string, 0, len(rc.fields))
		kwArgs := make([]any, 0, len(rc.fields))
		for _, f := range rc.fields {
			if !dynTextSearchable(dynx.Type(f.Type)) {
				continue
			}
			// ⚠️ **COALESCE(col, '') 是刻意的,不是随手写的** ——
			//   列可空(NULL = 没填)之后,`col LIKE ?` 在 col 为 NULL 时
			//   求值是 **NULL**,不是 false —— SQL 的三值逻辑。
			//
			//   **就这一组而言,NULL 与 false 的效果是一样的**:
			//   `TRUE OR NULL` 仍是 TRUE,`FALSE OR NULL` 是 NULL,
			//   而 WHERE 把 NULL 当"不成立" ⇒ 两种情况都不命中。
			//   (所以别指望"去掉 COALESCE 会让记录搜不到"—— 不会。)
			//
			//   真正的理由是 **NULL 会传染**:这一组将来被 AND 到别的条件上时,
			//   `NULL AND FALSE` = NULL ⇒ 整条 WHERE 变成 NULL,行被吞掉。
			//   把它抹成 '' 让表达式的真值恒为 TRUE/FALSE,是给未来留的余量。
			//
			//   ⚠️ **NULL 文本列是会真实出现的** ——
			//   给一个已有数据的集合**新加字段**时,老行的这一列就是 NULL
			//   (见 addColumn:新列可空,老行填 NULL)。
			ors = append(ors, "COALESCE(`"+f.Column+"`, '') LIKE ?")
			kwArgs = append(kwArgs, like)
		}
		if len(ors) > 0 {
			conds = append(conds, dynCond{sql: "(" + strings.Join(ors, " OR ") + ")", args: kwArgs})
		}
	}
	// 数据范围(行级权限)—— M160
	scopeFrag, scopeArgs, err := s.dynScopeCond(ctx, caller, rc.col.Code)
	if err != nil {
		return nil, nil, err
	}
	if scopeFrag != "" {
		conds = append(conds, dynCond{sql: scopeFrag, args: scopeArgs})
	}
	// 按字段多值筛选(M3)—— 白名单类型 + 类型化解析,append 在数据范围之后
	filterConds, err := s.filterConds(rc, q)
	if err != nil {
		return nil, nil, err
	}
	conds = append(conds, filterConds...)
	// 筛选表达式(M4)—— 与 f_ 多值筛选是 AND 关系,不是二选一
	if flt := strings.TrimSpace(q.Filter); flt != "" {
		sql, fargs, err := dynfilter.Compile(flt, rc.filterLookup)
		if err != nil {
			return nil, nil, errcode.New(errcode.BadRequest, "筛选表达式:"+err.Error())
		}
		conds = append(conds, dynCond{sql: sql, args: fargs})
	}
	where, args := dynWhere(conds)
	return where, args, nil
}

// filterLookup 筛选表达式的字段白名单(在用字段 + 系统列)
//
// ⚠️ **系统列也要进白名单** ——
// `created_at >= '2026-11-01'` 这种按时间段的筛法是高级筛选的高频用法;
// 白名单里没有它们的话,同样的语义只能绕道关键字搜索。
func (rc *dynRecordCtx) filterLookup(key string) (dynfilter.Field, bool) {
	if f, ok := rc.byKey[key]; ok {
		return dynfilter.Field{Column: f.Column, Type: dynx.Type(f.Type)}, true
	}
	switch key {
	case dynColID, dynColOwnerID:
		return dynfilter.Field{Column: key, Type: dynx.TypeInt}, true
	case dynColCreatedAt, dynColUpdatedAt:
		return dynfilter.Field{Column: key, Type: dynx.TypeDateTime}, true
	}
	return dynfilter.Field{}, false
}

// resolveOrder 解析排序字段与方向
//
// ⚠️ **排序字段必须在已知集合里** ——
// 排序字段会拼进 SQL,虽然这里只允许白名单里的列,
// 但"传进来什么就拼什么"是不能接受的(哪怕当前拼的都是安全的)。
func (rc *dynRecordCtx) resolveOrder(orderBy, orderDir string) (string, string, error) {
	col := "`" + dynColCreatedAt + "`"
	if ob := strings.TrimSpace(orderBy); ob != "" {
		if f, ok := rc.byKey[ob]; ok {
			col = "`" + f.Column + "`"
		} else {
			switch ob {
			case dynColID, dynColOwnerID, dynColCreatedAt, dynColUpdatedAt:
				col = "`" + ob + "`"
			default:
				return "", "", errcode.New(errcode.BadRequest, "排序字段 "+ob+" 不存在")
			}
		}
	}
	dir := "DESC"
	if strings.EqualFold(strings.TrimSpace(orderDir), "asc") {
		dir = "ASC"
	}
	return col, dir, nil
}

// GetRecord 记录详情
func (s *dynService) GetRecord(ctx context.Context, caller *Caller, code string, id uint64) (map[string]any, error) {
	if _, err := requireUser(caller); err != nil {
		return nil, err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return nil, err
	}
	return s.getRecordInto(ctx, caller, rc, id)
}

// getRecordInto 记录详情主体(登录 / 公开两条路径共用)
func (s *dynService) getRecordInto(ctx context.Context, caller *Caller, rc *dynRecordCtx, id uint64) (map[string]any, error) {
	where, args, err := s.recordScope(ctx, rc, caller, id)
	if err != nil {
		return nil, err
	}
	sql := "SELECT " + strings.Join(rc.selectCols(), ", ") + " FROM `" + rc.col.PhysTable +
		"` WHERE " + where
	rows, err := s.queryRows(ctx, sql, args...)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询记录失败").WithDetail(err.Error())
	}
	if len(rows) == 0 {
		return nil, errcode.New(errcode.NotFound, "记录不存在")
	}
	ownerNames := s.dynOwnerNames(ctx, []string{dynIDString(rows[0][dynColOwnerID])})
	fileRefs := s.dynFileRefs(ctx, rc, rows)
	rels := s.dynRels(ctx, rc, rows)
	view := rc.toView(rows[0], ownerNames, dynRowFiles(rc, rows[0], fileRefs), rels)
	// 审批徽标(2026-11-03)—— 同列表口径,没有审批单不加 key
	s.attachApprovalBadges(ctx, rc, []map[string]any{view})
	return view, nil
}

// CreateRecord 新建记录
//
// ⚠️ **落库装配在 dynInsertRecord**(与 CSV 导入共用同一份)——
// 必填 / 默认值 / 显式 NULL 的规则只能有一份,两份必然漂移。
func (s *dynService) CreateRecord(ctx context.Context, caller *Caller, code string, payload map[string]any) (uint64, error) {
	if _, err := requireUser(caller); err != nil {
		return 0, err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return 0, err
	}
	if err := rc.rejectUnknownFields(payload); err != nil {
		return 0, err
	}
	return s.dynInsertRecord(ctx, s.db, rc, caller.UserID, payload)
}

// UpdateRecord 改记录(部分更新:只改请求里出现的字段)
func (s *dynService) UpdateRecord(ctx context.Context, caller *Caller, code string, id uint64, payload map[string]any) error {
	if _, err := requireUser(caller); err != nil {
		return err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return err
	}
	if err := rc.rejectUnknownFields(payload); err != nil {
		return err
	}
	sets := make([]string, 0, len(rc.fields)+1)
	args := make([]any, 0, len(rc.fields)+3)
	for _, f := range rc.fields {
		raw, present := payload[f.FieldKey]
		if !present {
			continue
		}
		v, err := s.normalizeField(ctx, f, raw)
		if err != nil {
			return err
		}
		if v == nil && f.Required {
			return errcode.New(errcode.BadRequest, f.Label+" 是必填的")
		}
		sets = append(sets, "`"+f.Column+"` = ?")
		args = append(args, v)
	}
	sets = append(sets, "`"+dynColUpdatedAt+"` = ?")
	args = append(args, time.Now().UTC().Format(dynTimeLayout))
	where, whereArgs, err := s.recordScope(ctx, rc, caller, id)
	if err != nil {
		return err
	}
	args = append(args, whereArgs...)
	sql := "UPDATE `" + rc.col.PhysTable + "` SET " + strings.Join(sets, ", ") + " WHERE " + where
	res := s.db.WithContext(ctx).Exec(sql, args...)
	if res.Error != nil {
		return errcode.New(errcode.Internal, "更新记录失败").WithDetail(res.Error.Error())
	}
	// ⚠️ **RowsAffected == 0 不等于"记录不存在"** ——
	// MySQL 的 affected rows 是"**变了**的行数",值没变就是 0
	// (改了个一样的值,或者同一秒内改两次)。
	// ⇒ 另查一次存在性再下结论。
	if res.RowsAffected == 0 {
		exists, err := s.recordExists(ctx, rc, caller, id)
		if err != nil {
			return err
		}
		if !exists {
			return errcode.New(errcode.NotFound, "记录不存在")
		}
	}
	return nil
}

// DeleteRecord 删记录(软删)
func (s *dynService) DeleteRecord(ctx context.Context, caller *Caller, code string, id uint64) error {
	if _, err := requireUser(caller); err != nil {
		return err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return err
	}
	// ⚠️ **被其它集合的 relation 字段引用中的记录不可删** ——
	// 挡而不是级联/置空:级联会把"删一条巡检记录"放大成"连带删掉设备台账",
	// 置空则悄悄丢业务事实。报错里列出引用方,让用户先处理引用。
	if err := s.rejectReferencedRecords(ctx, rc.col.Code, []uint64{id}); err != nil {
		return err
	}
	where, args, err := s.recordScope(ctx, rc, caller, id)
	if err != nil {
		return err
	}
	sql := "UPDATE `" + rc.col.PhysTable + "` SET `" + dynColDeletedAt + "` = ? WHERE " + where
	full := make([]any, 0, len(args)+1)
	full = append(full, time.Now().UTC().Format(dynTimeLayout))
	full = append(full, args...)
	res := s.db.WithContext(ctx).Exec(sql, full...)
	if res.Error != nil {
		return errcode.New(errcode.Internal, "删除记录失败").WithDetail(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return errcode.New(errcode.NotFound, "记录不存在")
	}
	return nil
}

// recordScope 单条记录的范围条件(未删 + 归属 + 数据范围)
//
// ⚠️ **数据范围也必须落在单条上,不能只落在列表上** ——
// 只过滤列表的话,用户仍然可以**按 id 打开**一条他看不见的记录
// (GetRecord / UpdateRecord / DeleteRecord 走的是这里)。
// 那等于行级权限只挡了"翻页",没挡"直取"。
func (s *dynService) recordScope(ctx context.Context, rc *dynRecordCtx, caller *Caller, id uint64) (string, []any, error) {
	conds := []dynCond{
		{sql: "`" + dynColID + "` = ?", args: []any{id}},
		{sql: "`" + dynColDeletedAt + "` IS NULL"},
	}
	if rc.col.OwnerScoped && !callerCanCrossUserRead(caller) {
		conds = append(conds, dynCond{sql: "`" + dynColOwnerID + "` = ?", args: []any{caller.UserID}})
	}
	scopeFrag, scopeArgs, err := s.dynScopeCond(ctx, caller, rc.col.Code)
	if err != nil {
		return "", nil, err
	}
	if scopeFrag != "" {
		conds = append(conds, dynCond{sql: scopeFrag, args: scopeArgs})
	}
	where, args := dynWhere(conds)
	return strings.Join(where, " AND "), args, nil
}

// recordExists 记录是否存在(含归属校验)
func (s *dynService) recordExists(ctx context.Context, rc *dynRecordCtx, caller *Caller, id uint64) (bool, error) {
	where, args, err := s.recordScope(ctx, rc, caller, id)
	if err != nil {
		return false, err
	}
	sql := "SELECT COUNT(*) FROM `" + rc.col.PhysTable + "` WHERE " + where
	var cnt int64
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&cnt).Error; err != nil {
		return false, errcode.New(errcode.Internal, "查询记录失败").WithDetail(err.Error())
	}
	return cnt > 0, nil
}

// rejectUnknownFields 拒绝不认识的字段名
func (rc *dynRecordCtx) rejectUnknownFields(payload map[string]any) error {
	for k := range payload {
		// ⚠️ **以 `_` 开头的键直接忽略** ——
		//   读记录时后端会在视图里多给一个 `_files` 映射(附件的原文件名 + 直链),
		//   而"读出来 → 改两个字段 → 原样提交"是最常见的用法。
		//   回传 `_files` 不该被当成"未知字段"拒掉。
		//
		//   忽略全部 `_` 前缀而不是只放行 `_files`:字段标识必须以小写字母开头
		//   (dynx.ValidateFieldKey)⇒ **真字段不可能长成 `_` 开头**,
		//   所以这个放行不会漏掉任何真字段的校验。
		if strings.HasPrefix(k, "_") {
			continue
		}
		if _, ok := rc.byKey[k]; !ok {
			return errcode.New(errcode.BadRequest, "字段 "+k+" 不存在")
		}
	}
	return nil
}

// normalizeField 按字段定义归一化一个值
func (s *dynService) normalizeField(ctx context.Context, f model.SysDynField, raw any) (any, error) {
	opts, err := dynx.ParseOptions(f.Options)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "字段 "+f.Label+" 的选项集配置坏了").WithDetail(err.Error())
	}
	v, err := dynx.Normalize(dynx.Type(f.Type), raw, opts)
	if err != nil {
		return nil, errcode.New(errcode.BadRequest, f.Label+": "+err.Error())
	}
	if err := s.checkFileExists(ctx, f, v); err != nil {
		return nil, err
	}
	if err := s.checkRelationTarget(ctx, f, opts, v); err != nil {
		return nil, err
	}
	return v, nil
}

// checkFileExists 附件字段:值必须是一个**真的存在的文件**
//
// ⚠️ **只校验"存在",不校验"归属"** ——
// 附件字段的值是一把 key(M80 口径:私密靠 key 不可猜),
// 管理员替别人录数据时挂一个别人传的文件是合法的;
// 而"只有上传者能挂"会让管理员根本录不进去。
//
// ⚠️ **查不到就 400** —— 形状校验挡不住"指向死文件的路径",
// 存进去之后列表页那个链接点开是 404,用户会以为是系统坏了。
// 写的时候就说清楚"这个文件不存在",比事后看不懂强。
//
// ⚠️ **解析服务不可用时降级放行** ——
// 文件服务没装 / 查询报错 ⇒ 放行(只做形状校验)。
// "附件服务暂时查不动"不该让整个表单存不进去(旁路纪律)。
func (s *dynService) checkFileExists(ctx context.Context, f model.SysDynField, v any) error {
	if dynx.Type(f.Type) != dynx.TypeFile || s.files == nil {
		return nil
	}
	key, _ := v.(string)
	if key == "" {
		return nil
	}
	refs, err := s.files.ResolveKeys(ctx, []string{key})
	if err != nil {
		logger.L().Warn("dyn: 附件存在性校验失败,按形状放行",
			zap.String("field_key", f.FieldKey),
			zap.Error(err))
		return nil
	}
	if _, ok := refs[key]; !ok {
		return errcode.New(errcode.BadRequest, f.Label+": 文件不存在(请重新上传)")
	}
	return nil
}

// checkRelationTarget relation 字段:引用的目标记录必须真实存在
//
// ⚠️ **与 checkFileExists 的降级口径相反:查不动就拒绝写入** ——
// 文件服务是旁路(没装 dyn 也能用),而 DB 是这个功能的命根子;
// "查不动就放行"等于把关卡整个拆掉。
//
// ⚠️ **只校验"存在",不校验"数据范围可见"** ——
// 与附件的"只校验存在不校验归属"同一条纪律:
// 管理员替别人录数据时引用一条自己看不见的记录是合法的;
// 而候选下拉框那边已经按数据范围过滤过了。
//
// ⚠️ **create / update / batch / import 全走 normalizeField** ⇒ 这里一处拦截,
// 四条写路径自动继承(归一化单入口的红利)。
func (s *dynService) checkRelationTarget(ctx context.Context, f model.SysDynField, opts dynx.Options, v any) error {
	if dynx.Type(f.Type) != dynx.TypeRelation || opts.Relation == nil || v == nil {
		return nil
	}
	target := strings.TrimSpace(opts.Relation.Target)
	if target == "" {
		return nil
	}
	sql := "SELECT COUNT(*) FROM `" + dynx.PhysTableName(target) + "` WHERE `deleted_at` IS NULL AND `id` = ?"
	var cnt int64
	if err := s.db.WithContext(ctx).Raw(sql, v).Scan(&cnt).Error; err != nil {
		return errcode.New(errcode.Internal,
			f.Label+": 关联校验失败(目标集合 "+target+" 当前不可用)").WithDetail(err.Error())
	}
	if cnt == 0 {
		return errcode.New(errcode.BadRequest,
			f.Label+": 关联的记录不存在(可能已被删除),请重新选择")
	}
	return nil
}

// rejectReferencedRecords 记录删除保护:被在用 relation 字段引用中的记录不可删
//
// ⚠️ **block 而不是级联/置空** —— 升级路线 §2 M5 的拍板:
// 引用是业务事实;级联删除会把"删一条记录"放大成"连带删掉引用方的数据",
// 置空则悄悄丢掉引用关系。⇒ 挡下来,报错里列出是谁在引用。
//
// ⚠️ **自引用一并拦** —— 集合引用自己时,"删父任务"同样会造成
// 子任务指向一条已删记录;口径与跨集合引用一致,不特判。
func (s *dynService) rejectReferencedRecords(ctx context.Context, code string, ids []uint64) error {
	refs, err := s.relationReferrers(ctx, code)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
	iargs := make([]any, 0, len(ids))
	for _, id := range ids {
		iargs = append(iargs, id)
	}
	hits := make([]string, 0, 2)
	for _, r := range refs {
		sql := "SELECT COUNT(*) FROM `" + dynx.PhysTableName(r.Code) +
			"` WHERE `deleted_at` IS NULL AND `" + r.Column + "` IN (" + ph + ")"
		var cnt int64
		if err := s.db.WithContext(ctx).Raw(sql, iargs...).Scan(&cnt).Error; err != nil {
			return errcode.New(errcode.Internal, "引用检查失败").WithDetail(err.Error())
		}
		if cnt > 0 {
			hits = append(hits, "《"+r.Name+"》"+r.FieldLabel)
		}
	}
	if len(hits) > 0 {
		return errcode.New(errcode.BadRequest,
			"记录被以下关联字段引用,请先处理引用方再删除:"+strings.Join(hits, "、"))
	}
	return nil
}

// queryRows 执行查询并把每一行读成 map
func (s *dynService) queryRows(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.WithContext(ctx).Raw(sql, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, 16)
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// pickFieldSet 解析 fields 裁剪参数 → 字段标识集合(nil = 不裁剪)
//
// ⚠️ **引用不存在的字段直接报错,不静默忽略** ——
// 静默忽略的话,调用方拿到的列比它以为的少还不报错
// (同仓口径:不认识的字段名要当场炸)。
func (rc *dynRecordCtx) pickFieldSet(raw string) (map[string]bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	set := make(map[string]bool)
	for _, p := range strings.Split(raw, ",") {
		k := strings.TrimSpace(p)
		if k == "" {
			continue
		}
		if _, ok := rc.byKey[k]; !ok {
			return nil, errcode.New(errcode.BadRequest, "字段 "+k+" 不存在")
		}
		set[k] = true
	}
	if len(set) == 0 {
		return nil, nil
	}
	return set, nil
}

// pickedHasFile 裁剪集合里有没有附件字段(决定要不要做附件解析)
func (rc *dynRecordCtx) pickedHasFile(picked map[string]bool) bool {
	for _, f := range rc.fields {
		if picked[f.FieldKey] && dynx.Type(f.Type) == dynx.TypeFile {
			return true
		}
	}
	return false
}

// maxFilterKeys / maxFilterValues 一次查询的筛选上限
//
// ⚠️ **没有上限的 IN 列表就是一条 DoS 通道** ——
// 一次请求塞一万个别名进来,IN 子句的解析和执行都是平方级的疼。
const (
	maxFilterKeys   = 10
	maxFilterValues = 50
)

// filterConds 按字段多值筛选(M3)→ 条件列表
//
// ⚠️ **键排序后逐个处理** —— map 遍历顺序随机,排序让同一份请求
// 生成的 SQL 恒定(日志可比对、计划可复用)。
//
// ⚠️ **值按字段类型解析成真类型再进 IN** ——
// int 列拿字符串去比,MySQL 会做隐式转换(SQLite 又是另一套),
// 隐式转换既吃索引又可能比出错的结果;解析失败的直接报错。
func (s *dynService) filterConds(rc *dynRecordCtx, q DynRecordQuery) ([]dynCond, error) {
	if len(q.Filters) == 0 {
		return nil, nil
	}
	if len(q.Filters) > maxFilterKeys {
		return nil, errcode.New(errcode.BadRequest,
			fmt.Sprintf("一次最多按 %d 个字段筛选", maxFilterKeys))
	}
	keys := make([]string, 0, len(q.Filters))
	for k := range q.Filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	conds := make([]dynCond, 0, len(keys))
	for _, key := range keys {
		f, ok := rc.byKey[key]
		if !ok {
			return nil, errcode.New(errcode.BadRequest, "字段 "+key+" 不存在")
		}
		if !dynFilterable(dynx.Type(f.Type)) {
			return nil, errcode.New(errcode.BadRequest,
				"字段 "+f.Label+" 的类型不支持按值筛选")
		}
		// 值清单:去空、去重、限量
		vals := make([]string, 0, len(q.Filters[key]))
		seen := make(map[string]struct{}, len(q.Filters[key]))
		for _, raw := range q.Filters[key] {
			v := strings.TrimSpace(raw)
			if v == "" {
				continue
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			vals = append(vals, v)
		}
		if len(vals) == 0 {
			continue
		}
		if len(vals) > maxFilterValues {
			return nil, errcode.New(errcode.BadRequest,
				fmt.Sprintf("字段 %s 一次最多筛 %d 个值", f.Label, maxFilterValues))
		}
		args := make([]any, 0, len(vals))
		for _, v := range vals {
			av, err := filterArg(dynx.Type(f.Type), v)
			if err != nil {
				return nil, errcode.New(errcode.BadRequest, f.Label+": "+err.Error())
			}
			args = append(args, av)
		}
		ph := strings.TrimSuffix(strings.Repeat("?, ", len(vals)), ", ")
		conds = append(conds, dynCond{
			sql:  "`" + f.Column + "` IN (" + ph + ")",
			args: args,
		})
	}
	return conds, nil
}

// filterArg 单个筛选值按字段类型解析成进 SQL 的真类型
func filterArg(t dynx.Type, v string) (any, error) {
	switch t {
	case dynx.TypeInt, dynx.TypeRelation:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("筛选值 %q 必须是整数", v)
		}
		return n, nil
	case dynx.TypeNumber:
		fl, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("筛选值 %q 必须是数字", v)
		}
		return fl, nil
	case dynx.TypeBool:
		switch strings.ToLower(v) {
		case "true", "1":
			return int64(1), nil
		case "false", "0":
			return int64(0), nil
		default:
			return nil, fmt.Errorf("筛选值 %q 必须是 true / false", v)
		}
	default:
		// text / longtext / select / date / datetime —— 落库就是文本,原样比
		return v, nil
	}
}

// dynFilterable 该类型是否开放按值筛选
//
// ⚠️ **multiselect 不开放** —— 落库是 JSON 文本,IN 精确匹配的语义是错的
// (用户要的是"包含",不是"整串等于");json / file 同理没有"按值等于"可言。
// relation 开放:落库是目标记录 id(BIGINT),与 int 同型。
func dynFilterable(t dynx.Type) bool {
	switch t {
	case dynx.TypeText, dynx.TypeLongText, dynx.TypeSelect,
		dynx.TypeInt, dynx.TypeNumber, dynx.TypeBool,
		dynx.TypeDate, dynx.TypeDateTime, dynx.TypeRelation:
		return true
	default:
		return false
	}
}

// dynTextSearchable 该类型是否参与关键字模糊匹配
//
// ⚠️ **只搜文本类** ——
// 在整数/日期列上做 LIKE 在两个方言下行为不一致
// (MySQL 会把数字转成字符串再比,SQLite 的类型亲和性又是另一回事),
// 而且搜"2026"命中所有日期列没什么意义。
func dynTextSearchable(t dynx.Type) bool {
	switch t {
	case dynx.TypeText, dynx.TypeLongText, dynx.TypeSelect:
		return true
	default:
		return false
	}
}

// dynIDString uint64 类型的列 → 字符串
func dynIDString(v any) string {
	switch t := v.(type) {
	case int64:
		return strconv.FormatInt(t, 10)
	case []byte:
		return string(t)
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// dynText 值 → 文本
func dynText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}
