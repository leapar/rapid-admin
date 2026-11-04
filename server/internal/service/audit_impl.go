// AuditService 真实实现 —— 审计日志查询 + 写入
// 文档 §10.4 + §6.7
//
// 设计要点:
//  1. 4 类日志:操作日志/数据变更/登录/敏感信息查看
//  2. 写入由中间件统一调 Record*,本期前端列表查询直接用 List*
//  3. ExportOperationLogs:本期 stub,返回 key + 占位,后续 phase 接 OSS 导出
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/pkg/sonyflake"
)

// ===== 审计日志的用户名回填(2026-10-30)=====
//
// # 为什么需要回填
//
// 审计日志的 `username` 列**不是每条都写**:
//
//   - 中间件那条路径写(见 middleware/audit.go,2026-10-30 起)
//   - service 层直写的那几条直写时**没有 caller 上下文**,不写
//   - 历史上这一列压根不存在
//
// ⇒ 审计中心「用户」那一列对着一批行**永远是空的**。
//
// # 为什么是**读侧** LEFT JOIN,而不是"写的时候补齐"
//
// ① 历史行已经落库了,写侧补不回来;
//
// ② **用户可能改名**。回填用的是**当前**的用户名,而行里自己存的那份(写的时候拍的)才是**当时**的名字 —— 所以优先级是"行里的优先,为空才回填"。审计要回答的是"这条记录对应谁",不是"他现在叫什么"。
//
// # ⚠️ WHERE 里的列名**必须带表名前缀**
//
// JOIN 进来的 sys_user 也有 `username` / `id` / `created_at` / `deleted_at`,不写前缀在 SQLite 里直接报 `ambiguous column name: username`。
//
// # ⚠️ JOIN 进来的 sys_user **不过滤软删除**
//
// 这是**故意的**:被删掉的用户,审计记录里仍然要能看出"当时是谁"。(软删除的行照样 JOIN 上,这是 LEFT JOIN + 裸 JOIN 条件的自然结果。)
const (
	// opLogUserJoin 操作日志 → 用户
	opLogUserJoin = "LEFT JOIN sys_user ON sys_user.id = sys_operation_log.user_id"
	// loginLogUserJoin 登录日志 → 用户
	loginLogUserJoin = "LEFT JOIN sys_user ON sys_user.id = sys_login_log.user_id"
	// opLogUsernameExpr 操作日志的"显示用用户名"
	opLogUsernameExpr = "COALESCE(NULLIF(sys_operation_log.username, ''), sys_user.username)"
	// loginLogUsernameExpr 登录日志的"显示用用户名"
	loginLogUsernameExpr = "COALESCE(NULLIF(sys_login_log.username, ''), sys_user.username)"
)

// auditService 真实实现
type auditService struct {
	db *gorm.DB
	// scope 数据范围(行级权限,M155)。**可以是 nil** —— 那时不过滤。
	//
	// ⚠️ **注入的是同一个 ScopeService 实例**(main.go 里 scopeSvc 复用),
	// 不是另造一个 —— 另造一个会让缓存键各算各的,
	// "改了规则立刻失效"就退化成"最多 5 分钟后生效"。
	scope ScopeService
}

// NewAuditService 构造
//
// ⚠️ **scopeSvc 传 nil 是合法的** —— 退化成"不做行级过滤"。
func NewAuditService(db *gorm.DB, scopeSvc ScopeService) AuditService {
	return &auditService{db: db, scope: scopeSvc}
}

// opLogScopeColumns 操作日志表上的数据范围列
//
// ⚠️ **日志行没有 dept_id / project_id 列** ——
// 归属信息在**操作人**身上,而操作人的部门/项目在关联表里:
// 项目 → sys_user_identity.project_id
// 部门 → sys_user_department.dept_id
// 所以这里写的是子查询模板。
//
// ⚠️ **"本部门"对审计日志意味着什么** ——
// 部门管理员只看得到**本部门的人**的操作记录,
// 不是"本部门发生过的操作"。前者可实现,后者不可(日志里没有部门快照)。
var opLogScopeColumns = scope.Columns{
	Project: "sys_operation_log.user_id IN (SELECT user_id FROM sys_user_identity WHERE project_id IN ? AND is_active = 1)",
	Dept:    "sys_operation_log.user_id IN (SELECT user_id FROM sys_user_department WHERE dept_id IN ?)",
	Self:    "sys_operation_log.user_id IN ?",
}

func (s *auditService) normPage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

// ===== 操作日志 =====

func (s *auditService) ListOperationLogs(ctx context.Context, q OperationLogQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	// LEFT JOIN sys_user:给"行里没写 username"的那些行回填(见文件头的说明)
	tx := s.db.WithContext(ctx).Model(&model.SysOperationLog{}).Joins(opLogUserJoin)
	if q.UserID > 0 {
		tx = tx.Where("sys_operation_log.user_id = ?", q.UserID)
	}
	if q.Module != "" {
		tx = tx.Where("sys_operation_log.module = ?", q.Module)
	}
	if q.Action != "" {
		tx = tx.Where("sys_operation_log.action = ?", q.Action)
	}
	if q.IP != "" {
		tx = tx.Where("sys_operation_log.ip = ?", q.IP)
	}
	if q.Status != nil {
		tx = tx.Where("sys_operation_log.status = ?", *q.Status)
	}
	if q.Keyword != "" {
		// ⚠️ LIKE '%kw%' 用不上索引 —— 审计表是**只写不改**的,
		//   运营查历史靠时间范围收窄,单次全表扫的量级可以接受。
		//   真要优化,上 FTS5,别在这里拼索引技巧。
		//
		// ⚠️ **整组必须自己加括号** —— 这一坨是 OR 连接的,
		//   跟别的条件(用户/状态/时间)是 AND 关系。少了括号就是
		//   `user_id = 5 AND (a LIKE ? OR b LIKE ?)` 变成
		//   `user_id = 5 AND a LIKE ? OR b LIKE ?` —— **OR 漏出 AND 组**,
		//   非特权用户搜关键字就能看到别人的行。
		kw := "%" + q.Keyword + "%"
		tx = tx.Where(
			"("+opLogUsernameExpr+" LIKE ? OR sys_operation_log.module LIKE ? OR sys_operation_log.action LIKE ? OR sys_operation_log.description LIKE ?)",
			kw, kw, kw, kw)
	}
	if q.StartAt != nil {
		tx = tx.Where("sys_operation_log.created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("sys_operation_log.created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	// 行级权限(角色上配的数据范围规则,M155)
	//
	// ⚠️ **放在 Count 之前** —— 计数与取数必须用**同一条** tx,
	// 否则总数与列表对不上(页数算错,甚至翻到没有数据的页)。
	tx, err := applyScope(ctx, s.scope, tx, opLogScopeColumns)
	if err != nil {
		return nil, err
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询操作日志失败").WithDetail(err.Error())
	}
	var rows []model.SysOperationLog
	// ⚠️ Select 放在 Count **之后** —— 反过来也不会出错(GORM 的 Count 自己
	//   会改写成 count(*),实测过),但放后面读起来顺:先数总数,再取这一页。
	if err := tx.Select("sys_operation_log.*, " + opLogUsernameExpr + " AS username").
		Order("sys_operation_log.id DESC").Offset((page - 1) * size).Limit(size).
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询操作日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

func (s *auditService) GetOperationLog(ctx context.Context, id uint64) (*model.SysOperationLog, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var l model.SysOperationLog
	if err := s.db.WithContext(ctx).First(&l, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "日志不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询日志失败").WithDetail(err.Error())
	}
	return &l, nil
}

// ===== 数据变更日志 =====

func (s *auditService) ListDataChangeLogs(ctx context.Context, q DataChangeLogQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysDataChangeLog{})
	if q.TableName != "" {
		tx = tx.Where("table_name = ?", q.TableName)
	}
	if q.RecordID > 0 {
		tx = tx.Where("record_id = ?", q.RecordID)
	}
	if q.UserID > 0 {
		tx = tx.Where("user_id = ?", q.UserID)
	}
	if q.Operation != nil {
		tx = tx.Where("operation = ?", *q.Operation)
	}
	if q.Keyword != "" {
		tx = tx.Where("table_name LIKE ?", "%"+q.Keyword+"%")
	}
	if q.StartAt != nil {
		tx = tx.Where("created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询数据变更日志失败").WithDetail(err.Error())
	}
	var rows []model.SysDataChangeLog
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询数据变更日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

func (s *auditService) GetDataChangeLog(ctx context.Context, id uint64) (*model.SysDataChangeLog, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var l model.SysDataChangeLog
	if err := s.db.WithContext(ctx).First(&l, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "日志不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询日志失败").WithDetail(err.Error())
	}
	return &l, nil
}

// ===== 登录日志 =====

func (s *auditService) ListLoginLogs(ctx context.Context, q LoginLogQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	// LEFT JOIN sys_user:登录日志自己**总是**写了 username,
	// 但失败登录(用户不存在 / 没填用户名)那几行可能是空的 —— 回填兜一下。
	tx := s.db.WithContext(ctx).Model(&model.SysLoginLog{}).Joins(loginLogUserJoin)
	if q.UserID > 0 {
		tx = tx.Where("sys_login_log.user_id = ?", q.UserID)
	}
	if q.Success != nil {
		if *q.Success {
			tx = tx.Where("sys_login_log.status = ?", 1)
		} else {
			tx = tx.Where("sys_login_log.status = ?", 0)
		}
	}
	if q.IP != "" {
		tx = tx.Where("sys_login_log.ip = ?", q.IP)
	}
	if q.Keyword != "" {
		tx = tx.Where("("+loginLogUsernameExpr+" LIKE ?)", "%"+q.Keyword+"%")
	}
	if q.StartAt != nil {
		tx = tx.Where("sys_login_log.login_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("sys_login_log.login_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询登录日志失败").WithDetail(err.Error())
	}
	var rows []model.SysLoginLog
	if err := tx.Select("sys_login_log.*, " + loginLogUsernameExpr + " AS username").
		Order("sys_login_log.id DESC").Offset((page - 1) * size).Limit(size).
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询登录日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

// ===== 系统运行日志(2026-11-03)=====

// ListSystemLogs 系统运行日志分页查询(慢 SQL / 慢接口 / 5xx / panic)
//
// 不 JOIN sys_user —— 这张表的 user_id 只是"出事时谁在操作"的线索,
// 列表主视角是"什么时间 / 哪个接口 / 什么错",人名可空。
func (s *auditService) ListSystemLogs(ctx context.Context, q SystemLogQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysSystemLog{})
	if q.Level != "" {
		tx = tx.Where("level = ?", q.Level)
	}
	if q.Source != "" {
		tx = tx.Where("source = ?", q.Source)
	}
	if q.Keyword != "" {
		kw := "%" + q.Keyword + "%"
		tx = tx.Where("(path LIKE ? OR message LIKE ?)", kw, kw)
	}
	if q.StartAt != nil {
		tx = tx.Where("created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询系统日志失败").WithDetail(err.Error())
	}
	var rows []model.SysSystemLog
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询系统日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

// systemLogDetailMax 详情列截断长度
//
// Detail 是 text 列(panic 栈可能几十 KB)。审计价值在前几十行,
// 超长部分截断 —— 宁可截断也别让一条日志撑爆行宽。
const systemLogDetailMax = 8000

// RecordSystemLog 系统运行日志写入 —— **异步 + 绝不影响业务请求**
//
// 三条硬约束(调用方都在请求主路径上:AccessLog / Recover / slowquery.OnSlow):
//  1. **异步**:goroutine + context.Background(调用方的 ctx 会随请求结束被取消)
//  2. **超时兜底**:写库挂 3s deadline,慢了就放弃
//  3. **不 panic 不报错**:recover 兜底,写失败只 Warn 日志 ——
//     "日志写不进去"不该变成"业务接口 500"或把调用方带崩
func (s *auditService) RecordSystemLog(entry *SystemLogEntry) {
	if entry == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.L().Error("audit: 系统日志写入 panic,已隔离", zap.Any("panic", r))
			}
		}()
		log := &model.SysSystemLog{
			Level:      entry.Level,
			Source:     entry.Source,
			Method:     entry.Method,
			Path:       entry.Path,
			Message:    entry.Message,
			Detail:     entry.Detail,
			TraceID:    entry.TraceID,
			UserID:     entry.UserID,
			DurationMs: entry.DurationMs,
		}
		if len(log.Detail) > systemLogDetailMax {
			log.Detail = log.Detail[:systemLogDetailMax] + "\n…(已截断)"
		}
		log.ID = sonyflake.Next()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.db.WithContext(ctx).Create(log).Error; err != nil {
			logger.L().Warn("audit: 系统日志写入失败(已丢弃)",
				zap.String("source", entry.Source), zap.Error(err))
		}
	}()
}

// ===== 敏感信息查看 =====

func (s *auditService) ListFullInfoViews(ctx context.Context, q FullInfoViewQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.BizAuditFullInfoView{})
	if q.UserID > 0 {
		tx = tx.Where("user_id = ?", q.UserID)
	}
	if q.OwnerID > 0 {
		tx = tx.Where("target_id = ?", q.OwnerID)
	}
	if q.Field != "" {
		tx = tx.Where("field = ?", q.Field)
	}
	if q.StartAt != nil {
		tx = tx.Where("created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询敏感查看日志失败").WithDetail(err.Error())
	}
	var rows []model.BizAuditFullInfoView
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询敏感查看日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

// ===== 数据导出审计 =====

func (s *auditService) ListDataExports(ctx context.Context, q DataExportLogQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.BizAuditDataExportLog{})
	if q.UserID > 0 {
		tx = tx.Where("user_id = ?", q.UserID)
	}
	if q.BusinessType != "" {
		tx = tx.Where("business_type = ?", q.BusinessType)
	}
	if q.StartAt != nil {
		tx = tx.Where("created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询导出审计失败").WithDetail(err.Error())
	}
	var rows []model.BizAuditDataExportLog
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询导出审计失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

func (s *auditService) RecordDataExport(ctx context.Context, log *model.BizAuditDataExportLog) error {
	if log == nil {
		return errcode.New(errcode.BadRequest, "log 不能为空")
	}
	if log.ID == 0 {
		log.ID = sonyflake.Next()
	}
	return s.db.WithContext(ctx).Create(log).Error
}

// ===== 写入 =====

func (s *auditService) RecordOperation(ctx context.Context, log *model.SysOperationLog) error {
	if log == nil {
		return errcode.New(errcode.BadRequest, "log 不能为空")
	}
	if log.ID == 0 {
		log.ID = sonyflake.Next()
	}
	return s.db.WithContext(ctx).Create(log).Error
}

func (s *auditService) RecordDataChange(ctx context.Context, log *model.SysDataChangeLog) error {
	if log == nil {
		return errcode.New(errcode.BadRequest, "log 不能为空")
	}
	if log.ID == 0 {
		log.ID = sonyflake.Next()
	}
	return s.db.WithContext(ctx).Create(log).Error
}

func (s *auditService) RecordLogin(ctx context.Context, log *model.SysLoginLog) error {
	if log == nil {
		return errcode.New(errcode.BadRequest, "log 不能为空")
	}
	if log.ID == 0 {
		log.ID = sonyflake.Next()
	}
	return s.db.WithContext(ctx).Create(log).Error
}

func (s *auditService) RecordFullInfoView(ctx context.Context, log *model.BizAuditFullInfoView) error {
	if log == nil {
		return errcode.New(errcode.BadRequest, "log 不能为空")
	}
	if log.ID == 0 {
		log.ID = sonyflake.Next()
	}
	return s.db.WithContext(ctx).Create(log).Error
}

// RecordRoleSwitch 2026-10-19 M62:身份/角色切换审计
//   - 高危操作专用 handler 调用(双因素身份切换)
//   - 必传 UserID + ToRole,FromRole/Reason 可空
//   - 同时双写 sys_operation_log(兜底,通用操作日志);biz_role_switch_audit 是业务专用细表
func (s *auditService) RecordRoleSwitch(ctx context.Context, log *model.RoleSwitchAudit) error {
	if log == nil {
		return errcode.New(errcode.BadRequest, "log 不能为空")
	}
	if log.UserID == 0 {
		return errcode.New(errcode.BadRequest, "user_id 不能为空")
	}
	if log.ToRole == "" {
		return errcode.New(errcode.BadRequest, "to_role 不能为空")
	}
	if log.ID == 0 {
		log.ID = sonyflake.Next()
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 业务专用细表(biz_role_switch_audit,独立 from/to/project/reason)
		if err := tx.Create(log).Error; err != nil {
			return err
		}
		// 2. 兜底操作日志(sys_operation_log,通用列表展示)
		//
		// ⚠️ **2026-10-30 补 Username / UserRole,并修 status 恒为 1** ——
		//
		//	① Username / UserRole 此前**恒空** ⇒ 审计中心「用户」那一列
		//	   对身份切换记录**永远是空的**。(中间件那条路径从 2026-10-30
		//	   起也写这两列了。)
		//	   Username 从 sys_user 按 id 反查 —— RoleSwitchAudit 表里没有
		//	   这一列,加列 = 改表结构 + 迁移 dev 库,不值当(它只是"顺手记一笔")。
		//
		//	② UserRole 用 **FromRole**(切换**前**的角色)—— 那才是"这条
		//	   记录发生时操作者是谁"。ToRole 是切换**后**的角色,写到这里
		//	   是张冠李戴。
		//
		//	③ status 此前**写死 1(成功)** —— 而 handler 在切换失败时把
		//	   ToRole 写成 "FAIL:<原因>"。于是**失败的切换被记成成功** ——
		//	   而失败的切换恰恰是审计最要看的那批(有人想切但没切成)。
		username := lookupUsername(ctx, tx, log.UserID)
		status := 1
		errMsg := ""
		if strings.HasPrefix(log.ToRole, model.RoleSwitchFailPrefix) {
			status = 0
			errMsg = strings.TrimPrefix(log.ToRole, model.RoleSwitchFailPrefix)
		}
		genericLog := &model.SysOperationLog{
			BaseModel:   model.BaseModel{ID: sonyflake.Next()},
			UserID:      log.UserID,
			Username:    username,
			UserRole:    log.FromRole,
			Module:      "auth",
			Action:      "switch-identity",
			IP:          log.IP,
			UserAgent:   log.UA,
			Status:      status,
			ErrorMsg:    errMsg,
			Description: "from=" + log.FromRole + " to=" + log.ToRole + " reason=" + log.Reason,
		}
		return tx.Create(genericLog).Error
	})
}

// lookupUsername 按 user id 反查用户名(审计回填用)
//
// ⚠️ **必须传事务里的 tx,不能用 s.db** ——
//
// s.db 在事务里会**另开一条连接**。SQLite 的 `:memory:` 库是**按连接隔离**的(每条连接一个独立的空库),另开连接就是一句 "no such table: sys_user"。文件库 / MySQL 上不会报这个错,但那次查询**不在事务里** —— 事务回滚了它也回滚不了(读本来不用回滚,但它可能读到别的连接还没提交的数据)。
//
// ⚠️ **db 为 nil 时返回空串** —— NewAuditService(nil) 是单测里的常见构造,这里 panic 会把"审计服务没装配"这种配置问题变成运行时崩溃。
//
// 查不到(用户被硬删 / id 不存在)也返回空串:审计表不该因为一个人不存在就写不进去 —— 那条记录本身才是要紧的。
func lookupUsername(ctx context.Context, db *gorm.DB, userID uint64) string {
	if db == nil || userID == 0 {
		return ""
	}
	var name string
	if err := db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", userID).Pluck("username", &name).Error; err != nil {
		return ""
	}
	return name
}

// ===== 导出 =====

func (s *auditService) ExportOperationLogs(ctx context.Context, q OperationLogQuery) (string, error) {
	// 本期:返回占位 key;后续 phase 接 OSS 导出 + 异步任务
	// 直接复用 ListOperationLogs 拉全量(不分页)
	q.Page = 1
	q.Size = 10000
	_, err := s.ListOperationLogs(ctx, q)
	if err != nil {
		return "", err
	}
	return "exports/operation-logs-" + time.Now().Format("20060102150405") + ".xlsx", nil
}

// ===== 框架级 PII 解密访问日志(§1.5 #5 P0) =====

func (s *auditService) ListPIIAccessLogs(ctx context.Context, q PIIAccessLogQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysPIIAccessLog{})
	if q.UserID > 0 {
		tx = tx.Where("user_id = ?", q.UserID)
	}
	if q.ResourceType != "" {
		tx = tx.Where("resource_type = ?", q.ResourceType)
	}
	if q.ResourceID > 0 {
		tx = tx.Where("resource_id = ?", q.ResourceID)
	}
	if q.Field != "" {
		tx = tx.Where("field = ?", q.Field)
	}
	if q.Action != "" {
		tx = tx.Where("action = ?", q.Action)
	}
	if q.StartAt != nil {
		tx = tx.Where("created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询 PII 访问日志失败").WithDetail(err.Error())
	}
	var rows []model.SysPIIAccessLog
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询 PII 访问日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

// ===== 短信发送审计(§1.9 #7 P1) =====

func (s *auditService) ListSmsSendLogs(ctx context.Context, q SmsSendLogQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysSmsSendLog{})
	if q.Phone != "" {
		tx = tx.Where("phone = ?", q.Phone)
	}
	if q.Scene != "" {
		tx = tx.Where("scene = ?", q.Scene)
	}
	if q.Provider != "" {
		tx = tx.Where("provider = ?", q.Provider)
	}
	if q.Success != nil {
		if *q.Success {
			tx = tx.Where("success = ?", 1)
		} else {
			tx = tx.Where("success = ?", 0)
		}
	}
	if q.StartAt != nil {
		tx = tx.Where("created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询短信发送日志失败").WithDetail(err.Error())
	}
	var rows []model.SysSmsSendLog
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询短信发送日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

// ===== 密钥使用审计(§1.5 #8 P1) =====

func (s *auditService) ListCryptoAuditLogs(ctx context.Context, q CryptoAuditLogQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysCryptoAuditLog{})
	if q.UserID > 0 {
		tx = tx.Where("user_id = ?", q.UserID)
	}
	if q.KeyID != "" {
		tx = tx.Where("key_id = ?", q.KeyID)
	}
	if q.Operation != "" {
		tx = tx.Where("operation = ?", q.Operation)
	}
	if q.Algorithm != "" {
		tx = tx.Where("algorithm = ?", q.Algorithm)
	}
	if q.ResourceType != "" {
		tx = tx.Where("resource_type = ?", q.ResourceType)
	}
	if q.ResourceID > 0 {
		tx = tx.Where("resource_id = ?", q.ResourceID)
	}
	if q.Success != nil {
		if *q.Success {
			tx = tx.Where("success = ?", 1)
		} else {
			tx = tx.Where("success = ?", 0)
		}
	}
	if q.StartAt != nil {
		tx = tx.Where("created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询密钥审计日志失败").WithDetail(err.Error())
	}
	var rows []model.SysCryptoAuditLog
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询密钥审计日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}
