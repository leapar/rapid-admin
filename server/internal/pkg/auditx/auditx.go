// Package auditx —— 数据变更审计(sys_data_change_log)的自动写入器
//
// # 为什么是这个形态
//
// 技术方案 §9.5 给数据变更日志定的写法是「GORM Hook AfterUpdate」,但一直没落地
// —— 全仓唯一的写入方是业主勘误(owner_impl.go 显式 Create),审计中心的
// 「数据变更」页常年空表。2026-11-03 用户拍板:由框架层补齐,业务 service
// 一行不用改(业务线不写)。
//
// 形态:对**白名单表**注册 GORM 回调(create/update/delete),操作人从
// service ctx 取(Auth 中间件已经把 Caller 注入进去,见 middleware/auth.go
// 的 WithCaller → db.WithContext 链路),before/after 快照落
// sys_data_change_log。**与业务写同一个连接/事务**(审计行跟着业务写一起
// 提交或回滚 —— 回滚了审计行也不存在,证据一致性)。
//
// # 白名单:为什么不是所有表都记
//
// 审计的价值密度差很远:
//   - 记:业务主数据(项目/楼栋/房源/业主/供应商/合同)+ 权限主体
//     (账号/角色)—— 这些是「谁在什么时候把 X 改成了 Y」要当证据用的;
//   - 不记:日志/会话/验证码类表(本身就是流水,再记一层是套娃)、
//     sys_role_resource(分配权限是整批清空重建,一勾一大片,噪音;
//     操作日志已覆盖「谁动了分配」)、审批状态机(有 sys_approval_log)、
//     文件/文件夹(高频且界面可查)。
//
// ⚠️ **动态表单记录(dyn_* 物理表)记不到** —— dynx 的记录写路径是裸 SQL
// (Exec,不走 GORM 回调),钩子物理上打不着。要在 dynx 的
// CreateRecord/UpdateRecord/DeleteRecord 里显式调 auditx.RecordChange
// (Dyn 改造时接,不急)。
package auditx

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

// Operation 操作类型(与 model.SysDataChangeLog.Operation 对齐)
const (
	OpCreate = 1
	OpUpdate = 2
	OpDelete = 3
)

// TableRule 一张表的审计规则
type TableRule struct {
	// Table 精确表名(优先匹配)
	Table string
	// Prefix 前缀匹配(动态表单的物理表名 dyn_<编码> 是运行时建的,列不全)
	Prefix string
	// Skip 额外跳过的列(不进 before/after 快照)
	Skip []string
}

// defaultRules 默认白名单(2026-11-03 首批:业务主数据 + 权限主体)
var defaultRules = []TableRule{
	{Table: "biz_project"},
	{Table: "biz_building"},
	{Table: "biz_house"},
	{Table: "biz_user"}, // 业主档案(PII 列已全库加密,快照里是密文 —— 证据价值在"确实改过")
	{Table: "biz_supplier"},
	{Table: "contract"},
	{Table: "sys_user"},
	{Table: "sys_role"},
}

// globalSkips 全局跳过的列 —— 落进 5 年期证据表的快照里没有读者、只有风险
//
//	凭证类:password / password_hash / secret / token
//	密钥材料:*_nonce / *_aad / *_hash(AES-GCM 的 nonce / AAD / 盲索引)
//	审计表自己的时间戳/软删列记了也没意义,一并跳过
var globalSkips = map[string]bool{
	"password":      true,
	"password_hash": true,
	"secret":        true,
	"token":         true,
	"created_at":    true,
	"updated_at":    true,
	"deleted_at":    true,
}

// suffixSkips 按后缀跳过(盲索引 / 加密材料列名不固定)
var suffixSkips = []string{"_nonce", "_aad", "_hash"}

// Config 装配参数
type Config struct {
	// LookupCaller 从 ctx 取操作人。nil 或查不到 ⇒ 记为 system
	// (seed / migrate / cron 后台写没有请求方,记 system 是诚实的)。
	LookupCaller func(ctx context.Context) (userID uint64, username string)
	// Logger 审计写入失败时打 warn(不影响业务写)
	Logger *zap.Logger
	// Rules 白名单。nil / 空 = 用 defaultRules
	Rules []TableRule
	// MaxValueLen 快照里单个字符串值的截断长度(默认 1000,防大字段撑爆证据表)
	MaxValueLen int
}

type auditor struct {
	cfg     Config
	rules   []TableRule
	skipAll map[string]bool // 语义占位:skip() 直接查 globalSkips,留字段为了将来按实例覆盖
	log     *zap.Logger
}

// Install 注册数据变更审计回调。在 DB 初始化后调一次(与 slowquery.Install 同期)。
//
// ⚠️ 只应对**服务运行时**的 db 调:cmd/migrate 建表灌种子不应被审计
// (migrate 用自己的 db 实例,不 Install 就天然隔离)。
func Install(db *gorm.DB, cfg Config) error {
	rules := cfg.Rules
	if len(rules) == 0 {
		rules = defaultRules
	}
	log := cfg.Logger
	if log == nil {
		log = zap.NewNop()
	}
	a := &auditor{
		cfg:     cfg,
		rules:   rules,
		skipAll: globalSkips,
		log:     log,
	}
	_ = a // skipAll 目前仅语义占位:skip() 直接查 globalSkips,留字段是为了将来按实例覆盖
	// 操作人实现同时服务钩子(write)与显式通道(RecordChange)
	callerLookup = cfg.LookupCaller

	cc := db.Callback().Create().After("*")
	if err := cc.Register("auditx:after_create", a.afterCreate); err != nil {
		return err
	}
	if err := db.Callback().Update().Before("*").Register("auditx:before_update", a.beforeWrite); err != nil {
		return err
	}
	if err := db.Callback().Update().After("*").Register("auditx:after_update", a.afterUpdate); err != nil {
		return err
	}
	if err := db.Callback().Delete().Before("*").Register("auditx:before_delete", a.beforeWrite); err != nil {
		return err
	}
	if err := db.Callback().Delete().After("*").Register("auditx:after_delete", a.afterDelete); err != nil {
		return err
	}
	return nil
}

// matched 表是否在白名单里
func (a *auditor) matched(table string) (TableRule, bool) {
	if table == "" {
		return TableRule{}, false
	}
	for _, r := range a.rules {
		if r.Table == table {
			return r, true
		}
	}
	for _, r := range a.rules {
		if r.Prefix != "" && strings.HasPrefix(table, r.Prefix) {
			return r, true
		}
	}
	return TableRule{}, false
}

// skip 列是否不进快照
func (a *auditor) skip(rule TableRule, col string) bool {
	if globalSkips[col] {
		return true
	}
	for _, suffix := range suffixSkips {
		if strings.HasSuffix(col, suffix) {
			return true
		}
	}
	for _, s := range rule.Skip {
		if s == col {
			return true
		}
	}
	return false
}

// pkOf 从 statement 取单条主键(批量写 / map 写取不到 ⇒ 不审计)
//
// ⚠️ 两条取法都要有:`db.Delete(r)` / `Save(r)` 的 Dest 是结构体(走 ①);
// 但 `Model(r).Update("col", v)` 的 Dest 是 map ⇒ ReflectValue 不是 struct,
// 主键只能从 **Model** 上取(走 ②)—— 少了这条,所有 update 钩子会静默失明。
func pkOf(stmt *gorm.Statement) uint64 {
	if stmt.Schema == nil || stmt.Schema.PrioritizedPrimaryField == nil {
		return 0
	}
	f := stmt.Schema.PrioritizedPrimaryField
	// ① Dest 是结构体(单行 Save / Delete / Create)
	if stmt.ReflectValue.Kind() == reflect.Struct {
		v, isZero := f.ValueOf(stmt.Context, stmt.ReflectValue)
		if !isZero {
			if id, ok := v.(uint64); ok {
				return id
			}
		}
	}
	// ② Model 是结构体且带主键(Model(r).Update(map) 这类单行写)
	if mv := reflect.ValueOf(stmt.Model); mv.IsValid() {
		for mv.Kind() == reflect.Ptr {
			mv = mv.Elem()
		}
		if mv.Kind() == reflect.Struct {
			v, isZero := f.ValueOf(stmt.Context, mv)
			if !isZero {
				if id, ok := v.(uint64); ok {
					return id
				}
			}
		}
	}
	return 0
}

// snapshotRow 按主键把整行捞成 列→值(与业务写同一连接,事务内一致)
func snapshotRow(tx *gorm.DB, table string, id uint64) (map[string]any, error) {
	rows, err := tx.Session(&gorm.Session{NewDB: true}).
		Table(table).Where("id = ?", id).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	m := make(map[string]any, len(cols))
	for i, c := range cols {
		v := raw[i]
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		m[c] = v
	}
	return m, nil
}

// sanitize 过掉跳过列、截断长值 —— 返回可直接 JSON 的快照
func (a *auditor) sanitize(rule TableRule, m map[string]any) map[string]any {
	maxLen := a.cfg.MaxValueLen
	if maxLen <= 0 {
		maxLen = 1000
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if a.skip(rule, k) {
			continue
		}
		if s, ok := v.(string); ok && len(s) > maxLen {
			v = s[:maxLen] + "…(截断)"
		}
		out[k] = v
	}
	return out
}

// write 落一行审计(与业务写同一 tx;失败只记日志,不炸业务)
//
// ⚠️ 这里再 Create 时,**本包自己的回调会再触发** —— sys_data_change_log
// 不在白名单,matched 直接 false,天然无递归。
func (a *auditor) write(tx *gorm.DB, table string, rule TableRule, op int, id uint64, before, after map[string]any, ctx context.Context) {
	var uid uint64
	var uname string
	if a.cfg.LookupCaller != nil {
		uid, uname = a.cfg.LookupCaller(ctx)
	}
	if uname == "" {
		uname = "system"
	}
	var beforeJSON, afterJSON string
	if before != nil {
		if b, err := json.Marshal(before); err == nil {
			beforeJSON = string(b)
		}
	}
	if after != nil {
		if b, err := json.Marshal(after); err == nil {
			afterJSON = string(b)
		}
	}
	row := &model.SysDataChangeLog{
		BaseModel:      model.BaseModel{ID: sonyflake.Next()},
		TableNameField: table,
		RecordID:       id,
		Operation:      op,
		Before:         beforeJSON,
		After:          afterJSON,
		UserID:         uid,
		Username:       uname,
	}
	if err := tx.Session(&gorm.Session{NewDB: true}).Create(row).Error; err != nil {
		a.log.Warn("数据变更审计写入失败(不影响业务)",
			zap.String("table", table), zap.Uint64("id", id), zap.Error(err))
	}
}

// beforeWrite(update/delete 共用):把改动前的整行暂存进 statement
func (a *auditor) beforeWrite(tx *gorm.DB) {
	rule, ok := a.matched(tx.Statement.Table)
	if !ok {
		return
	}
	id := pkOf(tx.Statement)
	if id == 0 {
		return // 批量写 / 无主键:取证不了,放过(操作日志那层有记录)
	}
	before, err := snapshotRow(tx, tx.Statement.Table, id)
	if err != nil {
		// 行可能真的不存在(并发删)—— 审计留空比炸业务强
		a.log.Debug("数据变更审计取 before 快照失败", zap.String("table", tx.Statement.Table), zap.Error(err))
		return
	}
	tx.Statement.InstanceSet("auditx:before", before)
	tx.Statement.InstanceSet("auditx:rule", rule)
}

// afterCreate 新增:after = 整行
func (a *auditor) afterCreate(tx *gorm.DB) {
	rule, ok := a.matched(tx.Statement.Table)
	if !ok {
		return
	}
	id := pkOf(tx.Statement)
	if id == 0 {
		return
	}
	after, err := snapshotRow(tx, tx.Statement.Table, id)
	if err != nil {
		a.log.Debug("数据变更审计取 after 快照失败", zap.String("table", tx.Statement.Table), zap.Error(err))
		return
	}
	a.write(tx, tx.Statement.Table, rule, OpCreate, id, nil, a.sanitize(rule, after), tx.Statement.Context)
}

// afterUpdate 修改:只记**变化的列**(diff),没变就不记
func (a *auditor) afterUpdate(tx *gorm.DB) {
	table := tx.Statement.Table
	if _, ok := a.matched(table); !ok {
		return
	}
	if tx.Statement.RowsAffected == 0 {
		return
	}
	id := pkOf(tx.Statement)
	if id == 0 {
		return
	}
	rawBefore, ok := tx.Statement.InstanceGet("auditx:before")
	if !ok {
		return // before 没取到(如行当时不存在),不硬凑
	}
	beforeMap, _ := rawBefore.(map[string]any)
	rule, _ := a.matched(table)

	after, err := snapshotRow(tx, table, id)
	if err != nil {
		a.log.Debug("数据变更审计取 after 快照失败", zap.String("table", table), zap.Error(err))
		return
	}
	sanBefore := a.sanitize(rule, beforeMap)
	sanAfter := a.sanitize(rule, after)

	changed := make(map[string]any, len(sanAfter))
	for k, newVal := range sanAfter {
		oldVal, existed := sanBefore[k]
		if !existed || !reflect.DeepEqual(oldVal, newVal) {
			changed[k] = map[string]any{"old": oldVal, "new": newVal}
		}
	}
	if len(changed) == 0 {
		return // 一列没变(比如只是 touch 了 updated_at)不记
	}
	a.write(tx, table, rule, OpUpdate, id, nil, changed, tx.Statement.Context)
}

// afterDelete 删除:before = 整行(after 为空 —— 行没了)
func (a *auditor) afterDelete(tx *gorm.DB) {
	table := tx.Statement.Table
	if _, ok := a.matched(table); !ok {
		return
	}
	if tx.Statement.RowsAffected == 0 {
		return
	}
	id := pkOf(tx.Statement)
	if id == 0 {
		return
	}
	rawBefore, ok := tx.Statement.InstanceGet("auditx:before")
	if !ok {
		return
	}
	beforeMap, _ := rawBefore.(map[string]any)
	rule, _ := a.matched(table)
	a.write(tx, table, rule, OpDelete, id, a.sanitize(rule, beforeMap), nil, tx.Statement.Context)
}

// RecordChange 显式记录一条数据变更(给裸 SQL 写路径用 —— GORM 钩子打不着
// 的地方,如 dynx 的动态表单记录;操作人从 ctx 取,口径与钩子一致)。
//
// before / after 传 nil 表示"没有这一侧"(新增 before=nil,删除 after=nil)。
func RecordChange(ctx context.Context, tx *gorm.DB, table string, op int, id uint64, before, after map[string]any) error {
	if table == "" || id == 0 {
		return nil
	}
	var uid uint64
	var uname string
	uid, uname, _ = callerOf(ctx)
	if uname == "" {
		uname = "system"
	}
	encode := func(m map[string]any) string {
		if m == nil {
			return ""
		}
		// 先把 []byte 归一成 string(json.Marshal 对 []byte 会 base64,可读性差)
		for k, v := range m {
			if b, ok := v.([]byte); ok {
				m[k] = string(b)
			}
		}
		b, err := json.Marshal(m)
		if err != nil {
			return ""
		}
		return string(b)
	}
	return tx.WithContext(ctx).Create(&model.SysDataChangeLog{
		BaseModel:      model.BaseModel{ID: sonyflake.Next()},
		TableNameField: table,
		RecordID:       id,
		Operation:      op,
		Before:         encode(before),
		After:          encode(after),
		UserID:         uid,
		Username:       uname,
	}).Error
}

// callerOf 由 Install 注入的 LookupCaller 提供实现;裸调用(没 Install)时
// 退化成 system —— 审计宁可记 system 也不记成别人。
var callerLookup func(ctx context.Context) (uint64, string)

// SetCallerLookup 注册"从 ctx 取操作人"的实现(main.go 装配时调)。
func SetCallerLookup(fn func(ctx context.Context) (uint64, string)) {
	callerLookup = fn
}

func callerOf(ctx context.Context) (uint64, string, bool) {
	if callerLookup == nil {
		return 0, "system", true
	}
	uid, uname := callerLookup(ctx)
	return uid, uname, true
}
