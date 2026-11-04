// 动态集合数据面的行级批量操作与批量导入导出(§1.12.2 #13 + #14)
//
// # 这一层有什么
//
//	RestoreRecord      单条恢复(回收站)
//	BatchDeleteRecords 批量软删
//	BatchUpdateRecords 批量修改单字段
//	ExportRecords      按当前筛选导出全量 CSV
//	ImportRecords      导入 CSV(错误行报告)
//
// # ⚠️ 恢复为什么不需要查重
//
// 物理表的唯一性约束只有主键 id(见 dyn_impl.go 的 createTable:
// 只建了 PRIMARY KEY + owner/deleted 两个普通索引)——
// 「业务字段唯一」这条约束在 dyn 上**不存在**,新建本来就不查重,
// 恢复自然也没有"与现存记录冲突"的问题可撞。
// (id 是 sonyflake 生成的,恢复的是**同一行**,主键不会撞。)
//
// # ⚠️ 批量修改必须走单条 update 的同一条归一化链
//
// 批量接口最容易犯的错是"为了快,绕过校验直接拼 SQL"——
// 单条 update 里 dynx.Normalize 挡住的那些(选项集外的值、
// 认不出来的布尔、非法 JSON)会从批量口整个漏进去。
// ⇒ 这里复用 normalizeField(与 CreateRecord / UpdateRecord 同一份)。
//
// # ⚠️ 权限判定不在这一层
//
// service 不能 import api/middleware(循环依赖,仓库铁律),
// 资源码判定(`dyn:<集合码>:<动作>`)在 handler 层的 dynGuard 做;
// 这一层的职责是**行级**的范围:owner_scoped 过滤 + 数据范围(dynScopeCond)
// 落在每一条 UPDATE 的 WHERE 上 —— 批量口**不许**变成越权口:
// 拿一串别人的 id 来批量删,WHERE 里的归属条件会把看不见的行滤掉。
package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/dynx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"

	"gorm.io/gorm"
)

// dynBatchMaxIDs 单次批量操作的 id 数上限
//
// ⚠️ 批量接口的 ids 直接进 IN 子句,不设上限的话
// 一次塞 10 万个 id 就是拿 SQL 长度打后端。
const dynBatchMaxIDs = 1000

// dynImportMaxRows 单次导入的数据行数上限
//
// ⚠️ 导入是"整批先校验再入库"(见 ImportRecords),行全部要过一遍归一化;
// 不设上限的话一个 50 万行的 CSV 能把校验和内存一起拖死。
const dynImportMaxRows = 5000

// dynImportModeAll / dynImportModeSkip 导入的两种语义
//
//	all(默认):整批全有才进 —— 任何一行有任何错误,**一行都不入库**
//	skip      :跳过错误行,只入库合法行
const (
	dynImportModeAll  = "all"
	dynImportModeSkip = "skip"
)

// dynImportError 错误行报告里的一条
type DynImportError struct {
	// Row 数据行号(第 1 行数据 = 1;表头行的问题记 0)
	Row int `json:"row"`
	// Field 出错的字段标识(表头问题 = 表头原文)
	Field string `json:"field"`
	// Message 错误说明
	Message string `json:"message"`
}

// DynImportResult 导入结果
type DynImportResult struct {
	// Total 读到的数据行数(不含表头)
	Total int `json:"total"`
	// Inserted 实际入库的行数
	Inserted int `json:"inserted"`
	// Skipped 因错误跳过的行数(all 模式下任何错误时 = Total)
	Skipped int `json:"skipped"`
	// Errors 错误行报告(all 模式下是**全部**错误,skip 模式下是被跳过的那些)
	Errors []DynImportError `json:"errors"`
}

// parseIDs 把前端传来的 id 字符串数组转成 uint64
//
// ⚠️ **空串 / 非数字直接拒绝,不静默跳过** ——
// 静默跳过的话,用户以为删了 N 条,实际删了 N-1 条,
// 而"少删的那条"恰恰是他点名要删的。
func parseIDs(raw []string) ([]uint64, error) {
	if len(raw) == 0 {
		return nil, errcode.New(errcode.BadRequest, "ids 不能为空")
	}
	if len(raw) > dynBatchMaxIDs {
		return nil, errcode.New(errcode.BadRequest,
			fmt.Sprintf("单次批量操作不能超过 %d 个 id", dynBatchMaxIDs))
	}
	out := make([]uint64, 0, len(raw))
	for _, r := range raw {
		id, err := strconv.ParseUint(strings.TrimSpace(r), 10, 64)
		if err != nil || id == 0 {
			return nil, errcode.New(errcode.BadRequest, "id "+r+" 不是合法的记录 id")
		}
		out = append(out, id)
	}
	return out, nil
}

// batchWhere 批量操作的 WHERE(id 集合 + 未删 + 归属 + 数据范围)
//
// ⚠️ **与单条的 recordScope 同一套范围条件,只是 id 从"="换成"IN"** ——
// 批量口漏掉任何一条范围条件,都等于给越权操作开了一个批量通道。
func (s *dynService) batchWhere(ctx context.Context, rc *dynRecordCtx, caller *Caller, ids []uint64, deleted bool) (string, []any, error) {
	not := ""
	if deleted {
		not = "NOT "
	}
	conds := []dynCond{
		{sql: "`" + dynColID + "` IN ?", args: []any{ids}},
		{sql: "`" + dynColDeletedAt + "` IS " + not + "NULL"},
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

// RestoreRecord 恢复一条软删的记录(回收站)
//
// ⚠️ **只恢复"真的在回收站里"的行** ——
// WHERE 里是 `deleted_at IS NOT NULL`:对没删过的记录调恢复,
// 不是"无事发生"而是 404(用户点了按钮没反应,和"这条不存在"是两回事)。
//
// ⚠️ **恢复前不查重** —— 理由见文件头(物理表没有业务唯一约束可撞)。
func (s *dynService) RestoreRecord(ctx context.Context, caller *Caller, code string, id uint64) error {
	if _, err := requireUser(caller); err != nil {
		return err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return err
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	where, args, err := s.batchWhere(ctx, rc, caller, []uint64{id}, true)
	if err != nil {
		return err
	}
	sql := "UPDATE `" + rc.col.PhysTable + "` SET `" + dynColDeletedAt + "` = NULL, `" +
		dynColUpdatedAt + "` = ? WHERE " + where
	full := append([]any{time.Now().UTC().Format(dynTimeLayout)}, args...)
	res := s.db.WithContext(ctx).Exec(sql, full...)
	if res.Error != nil {
		return errcode.New(errcode.Internal, "恢复记录失败").WithDetail(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return errcode.New(errcode.NotFound, "记录不存在或不在回收站")
	}
	return nil
}

// BatchDeleteRecords 批量软删,返回实际删掉的行数
//
// ⚠️ **返回的是"看得见的行里删掉的行数"** ——
// ids 里混着别人看不见的记录时,那些行被范围条件滤掉,
// 返回数 < len(ids) 是**正确行为**(不是报错)。
func (s *dynService) BatchDeleteRecords(ctx context.Context, caller *Caller, code string, ids []string) (int64, error) {
	if _, err := requireUser(caller); err != nil {
		return 0, err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return 0, err
	}
	idsUint, err := parseIDs(ids)
	if err != nil {
		return 0, err
	}
	// ⚠️ **被引用的记录不可批量删** —— 与单条删除同一份检查,口径不分叉
	if err := s.rejectReferencedRecords(ctx, rc.col.Code, idsUint); err != nil {
		return 0, err
	}
	where, args, err := s.batchWhere(ctx, rc, caller, idsUint, false)
	if err != nil {
		return 0, err
	}
	sql := "UPDATE `" + rc.col.PhysTable + "` SET `" + dynColDeletedAt + "` = ? WHERE " + where
	full := append([]any{time.Now().UTC().Format(dynTimeLayout)}, args...)
	res := s.db.WithContext(ctx).Exec(sql, full...)
	if res.Error != nil {
		return 0, errcode.New(errcode.Internal, "批量删除失败").WithDetail(res.Error.Error())
	}
	return res.RowsAffected, nil
}

// BatchUpdateRecords 批量修改单字段,返回实际改掉的行数
//
// ⚠️ **字段必须走元数据校验 + 同一条归一化链** ——
// 字段必须在在用字段清单里(停用的字段列还在,但批量口不许再写它,
// 与单条 update 的口径一致);值走 normalizeField(类型归一化 +
// 选项集校验 + 附件存在性),必填字段不许批量改成空。
func (s *dynService) BatchUpdateRecords(ctx context.Context, caller *Caller, code string, ids []string, field string, value any) (int64, error) {
	if _, err := requireUser(caller); err != nil {
		return 0, err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return 0, err
	}
	idsUint, err := parseIDs(ids)
	if err != nil {
		return 0, err
	}
	f, ok := rc.byKey[strings.TrimSpace(field)]
	if !ok {
		return 0, errcode.New(errcode.BadRequest, "字段 "+field+" 不存在或已停用")
	}
	v, err := s.normalizeField(ctx, f, value)
	if err != nil {
		return 0, err
	}
	if v == nil && f.Required {
		return 0, errcode.New(errcode.BadRequest, f.Label+" 是必填的")
	}
	where, args, err := s.batchWhere(ctx, rc, caller, idsUint, false)
	if err != nil {
		return 0, err
	}
	sql := "UPDATE `" + rc.col.PhysTable + "` SET `" + f.Column + "` = ?, `" +
		dynColUpdatedAt + "` = ? WHERE " + where
	full := append([]any{v, time.Now().UTC().Format(dynTimeLayout)}, args...)
	res := s.db.WithContext(ctx).Exec(sql, full...)
	if res.Error != nil {
		return 0, errcode.New(errcode.Internal, "批量修改失败").WithDetail(res.Error.Error())
	}
	return res.RowsAffected, nil
}

// ============================================================================
// 导出(CSV)
// ============================================================================

// ExportRecords 按当前筛选条件导出**全量**记录为 CSV(写入 w)
//
// ⚠️ **复用 list 的筛选 / 排序 / 数据范围** ——
// 界面上筛选出的"这批数据"和导出的"这批数据"必须是同一批:
// 这里直接复用 recordWhere(关键字 / owner / owner_scoped / 数据范围)
// 与 resolveOrder(排序白名单),只是**不带 LIMIT** ——
// 导出的是全量,不是当前页。
//
// ⚠️ **带 BOM 头** —— Excel 打开无 BOM 的 UTF-8 CSV 会中文乱码。
//
// ⚠️ **公式注入防护** —— 见 csvGuardCell:文本值以 = + - @ 等开头时加 `'` 前缀,
// 与前端 useTablePro 的 exportCSV 同一套语义(前端导出的防注入不该在后端导出里缺席)。
//
// ⚠️ **列头用字段 label** —— 导出给人看的;导入也认 label(见 ImportRecords),
// 所以导出的文件可以改完直接导回来。
func (s *dynService) ExportRecords(ctx context.Context, caller *Caller, code string, q DynRecordQuery, w io.Writer) error {
	if _, err := requireUser(caller); err != nil {
		return err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return err
	}
	where, args, err := s.recordWhere(ctx, rc, caller, q)
	if err != nil {
		return err
	}
	orderCol, orderDir, err := rc.resolveOrder(q.OrderBy, q.OrderDir)
	if err != nil {
		return err
	}
	sql := "SELECT " + strings.Join(rc.selectCols(), ", ") + " FROM `" + rc.col.PhysTable +
		"` WHERE " + strings.Join(where, " AND ") +
		" ORDER BY " + orderCol + " " + orderDir
	rows, err := s.queryRows(ctx, sql, args...)
	if err != nil {
		return errcode.New(errcode.Internal, "查询导出数据失败").WithDetail(err.Error())
	}

	// BOM 头:Excel 打开 UTF-8 CSV 不加 BOM 会中文乱码(与前端 exportCSV 同款)
	if _, err := w.Write([]byte("\uFEFF")); err != nil {
		return errcode.New(errcode.Internal, "写出 CSV 失败").WithDetail(err.Error())
	}
	cw := csv.NewWriter(w)
	header := make([]string, 0, len(rc.fields))
	for _, f := range rc.fields {
		header = append(header, f.Label)
	}
	if err := cw.Write(header); err != nil {
		return errcode.New(errcode.Internal, "写出 CSV 失败").WithDetail(err.Error())
	}
	for _, row := range rows {
		cells := make([]string, 0, len(rc.fields))
		for _, f := range rc.fields {
			cells = append(cells, csvGuardCell(dynx.Type(f.Type), dynx.Denormalize(dynx.Type(f.Type), row[f.Column])))
		}
		if err := cw.Write(cells); err != nil {
			return errcode.New(errcode.Internal, "写出 CSV 失败").WithDetail(err.Error())
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return errcode.New(errcode.Internal, "写出 CSV 失败").WithDetail(err.Error())
	}
	return nil
}

// csvGuardCell 一个值 → CSV 单元格文本(带公式注入防护)
//
// ⚠️ **防护只对"文本类"的值生效** ——
// 数字(-5)、日期(2026-11-01)以 "-" / 数字开头是**正常值**,
// 加前缀会让 Excel 里的负数变成字符串(左对齐、不能求和)。
// 真正会被 Excel 当公式执行的形状只有**文本**单元格里的 = + - @ 开头 ——
// 与前端 csvCell 的口径一致(那边也只对字符串加前缀)。
func csvGuardCell(t dynx.Type, v any) string {
	if v == nil {
		return ""
	}
	var s string
	switch tv := v.(type) {
	case string:
		s = tv
	case bool:
		s = strconv.FormatBool(tv)
	case []string:
		// 多选:写 JSON 数组文本 —— 导入侧 toStringList 认这个形状,能原样导回
		b, _ := json.Marshal(tv)
		s = string(b)
	case []any:
		b, _ := json.Marshal(tv)
		s = string(b)
	default:
		s = fmt.Sprint(v)
	}
	// JSON 类型的值也可能是字符串(denormJSON 对非对象原样返回)
	switch t {
	case dynx.TypeText, dynx.TypeLongText, dynx.TypeSelect, dynx.TypeFile, dynx.TypeJSON:
		if len(s) > 0 && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@' || s[0] == '\t' || s[0] == '\r') {
			return "'" + s
		}
	}
	return s
}

// ============================================================================
// 导入(CSV)
// ============================================================================

// ImportRecords 导入 CSV 记录
//
// # 语义(二选一,由 mode 参数决定)
//
//	all(默认):**整批全有才进** —— 先把所有行都校验一遍,
//	            任何一行有任何错误 ⇒ **一行都不入库**,错误行报告原样返回。
//	            适合"手工整理好的表",宁可不来也别来半批。
//	skip:       跳过错误行,只入库合法行;报告里说明每条被跳过的原因。
//	            适合"从别的系统迁数据,容忍个别行不合法"。
//
// # 表头
//
// 每一列认**字段标识**或**显示名**(导出写的是显示名,所以导出的文件
// 不改表头就能导回来)。表头里有认不出的列 ⇒ 整个文件拒绝
// (静默忽略的话,用户把列名打错一个字,那一列的数据就悄悄丢了)。
//
// # 单元格
//
// 空单元格 = "没填"(走默认值;必填字段空 ⇒ 该行报错),
// 非空单元格走与单条 create **同一套** normalizeField 归一化。
func (s *dynService) ImportRecords(ctx context.Context, caller *Caller, code string, data []byte, mode string) (*DynImportResult, error) {
	if _, err := requireUser(caller); err != nil {
		return nil, err
	}
	rc, err := s.loadRecordCtx(ctx, code)
	if err != nil {
		return nil, err
	}
	if mode == "" {
		mode = dynImportModeAll
	}
	if mode != dynImportModeAll && mode != dynImportModeSkip {
		return nil, errcode.New(errcode.BadRequest, "mode 只能是 all(默认)或 skip")
	}

	// label → 字段(显示名可能重复:重复的 label 只认第一个,后面那个匹配不到就算了)
	byLabel := make(map[string]model.SysDynField, len(rc.fields))
	for _, f := range rc.fields {
		if _, dup := byLabel[f.Label]; !dup {
			byLabel[f.Label] = f
		}
	}

	cr := csv.NewReader(bytes.NewReader(data))
	cr.TrimLeadingSpace = true
	// ⚠️ **可变列数交给手工检查** —— csv 默认在列数不一致时报错,
	// 但错误信息不带字段名,用户看不懂;关掉它,由下面逐行检查给出报告。
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = false
	records, err := cr.ReadAll()
	if err != nil {
		return nil, errcode.New(errcode.BadRequest, "CSV 解析失败:"+err.Error())
	}
	if len(records) == 0 {
		return nil, errcode.New(errcode.BadRequest, "CSV 是空的(第一行应是表头)")
	}
	if len(records)-1 > dynImportMaxRows {
		return nil, errcode.New(errcode.BadRequest,
			fmt.Sprintf("单次导入不能超过 %d 行(当前 %d 行)", dynImportMaxRows, len(records)-1))
	}

	// ---- 表头映射 ----
	header := records[0]
	var headerErrs []DynImportError
	mapped := make([]model.SysDynField, 0, len(header))
	seen := make(map[string]struct{}, len(header))
	for i, cell := range header {
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cell), "\uFEFF"))
		if name == "" {
			headerErrs = append(headerErrs, DynImportError{Row: 0, Field: fmt.Sprintf("第 %d 列", i+1), Message: "表头有空列"})
			mapped = append(mapped, model.SysDynField{})
			continue
		}
		f, ok := rc.byKey[name]
		if !ok {
			f, ok = byLabel[name]
		}
		if !ok {
			headerErrs = append(headerErrs, DynImportError{Row: 0, Field: name, Message: "表头这一列不是任何字段(既不是字段标识也不是显示名)"})
			mapped = append(mapped, model.SysDynField{})
			continue
		}
		if _, dup := seen[f.FieldKey]; dup {
			headerErrs = append(headerErrs, DynImportError{Row: 0, Field: name, Message: "表头有两列都映射到字段 " + f.FieldKey})
			mapped = append(mapped, model.SysDynField{})
			continue
		}
		seen[f.FieldKey] = struct{}{}
		mapped = append(mapped, f)
	}
	if len(seen) == 0 {
		return nil, errcode.New(errcode.BadRequest, "表头里没有任何一个能认出来的字段(列头应为字段标识或显示名)")
	}

	// ---- 逐行校验 ----
	res := &DynImportResult{Total: len(records) - 1, Errors: headerErrs}
	type pendingRow struct {
		payload map[string]any
	}
	var pending []pendingRow
	for i, record := range records[1:] {
		rowNo := i + 1
		if len(record) != len(header) {
			res.Errors = append(res.Errors, DynImportError{
				Row: rowNo, Field: "",
				Message: fmt.Sprintf("这一行有 %d 个单元格,与表头的 %d 列对不上", len(record), len(header)),
			})
			continue
		}
		payload := make(map[string]any, len(mapped))
		var rowErrs []DynImportError
		for j, f := range mapped {
			// 表头问题列(空列 / 未知列 / 重复列)在这个 payload 里没有落点
			if f.FieldKey == "" {
				continue
			}
			cell := strings.TrimSpace(record[j])
			if cell == "" {
				continue // 空单元格 = 没填(必填在下面统一查)
			}
			v, err := s.normalizeField(ctx, f, cell)
			if err != nil {
				rowErrs = append(rowErrs, DynImportError{Row: rowNo, Field: f.FieldKey, Message: err.Error()})
				continue
			}
			payload[f.FieldKey] = v
		}
		// 必填校验:与 create 同口径(在用字段里 required 的,没值就是错)
		for _, f := range rc.fields {
			if !f.Required {
				continue
			}
			if _, present := payload[f.FieldKey]; present {
				continue
			}
			// 默认值能兜的就不报错(与 create 一致:没带字段时填默认值)
			if f.DefaultValue != "" {
				continue
			}
			rowErrs = append(rowErrs, DynImportError{Row: rowNo, Field: f.FieldKey, Message: f.Label + " 是必填的"})
		}
		if len(rowErrs) > 0 {
			res.Errors = append(res.Errors, rowErrs...)
			continue
		}
		pending = append(pending, pendingRow{payload: payload})
	}

	// ---- 入库 ----
	//
	// ⚠️ **入库的是"通过校验的行"(pending),不是"全部行"** ——
	//   all 模式:有任何错误(表头错也算)⇒ 一行都不入库;
	//   skip 模式:无论有没有错误行,合法行都进。
	if mode == dynImportModeAll && len(res.Errors) > 0 {
		res.Skipped = res.Total
		return res, nil
	}
	if len(pending) > 0 {
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, p := range pending {
				if _, err := s.dynInsertRecord(ctx, tx, rc, caller.UserID, p.payload); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if mode == dynImportModeAll {
		res.Inserted = len(pending)
	} else {
		res.Inserted = len(pending)
		res.Skipped = res.Total - res.Inserted
	}
	return res, nil
}

// dynInsertRecord 落一行记录(与 CreateRecord 共用同一条装配)
//
// ⚠️ **从 CreateRecord 抽出来的**,行为必须逐字一致:
// 必填没带 → 报错;没带且没默认值 → 显式写 NULL(不是"跳过这一列")。
func (s *dynService) dynInsertRecord(ctx context.Context, db *gorm.DB, rc *dynRecordCtx, ownerID uint64, payload map[string]any) (uint64, error) {
	now := time.Now().UTC().Format(dynTimeLayout)
	newID := sonyflake.Next()
	cols := []string{"`" + dynColID + "`", "`" + dynColOwnerID + "`", "`" + dynColCreatedAt + "`", "`" + dynColUpdatedAt + "`"}
	args := []any{newID, ownerID, now, now}
	for _, f := range rc.fields {
		raw, present := payload[f.FieldKey]
		if !present {
			if f.Required {
				return 0, errcode.New(errcode.BadRequest, f.Label+" 是必填的")
			}
			if f.DefaultValue == "" {
				cols = append(cols, "`"+f.Column+"`")
				args = append(args, nil)
				continue
			}
			raw = f.DefaultValue
		}
		v, err := s.normalizeField(ctx, f, raw)
		if err != nil {
			return 0, err
		}
		if v == nil && f.Required {
			return 0, errcode.New(errcode.BadRequest, f.Label+" 是必填的")
		}
		cols = append(cols, "`"+f.Column+"`")
		args = append(args, v)
	}
	sql := "INSERT INTO `" + rc.col.PhysTable + "` (" + strings.Join(cols, ", ") + ") VALUES (" +
		strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
	if err := db.WithContext(ctx).Exec(sql, args...).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "创建记录失败").WithDetail(err.Error())
	}
	return newID, nil
}
