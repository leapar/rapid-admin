// ConfigCenterService 真实实现 —— 平台配置中心
// 2026-10-18 收窄:仅保留 update 一条写通道
//   - PATCH /configs/:id (DirectUpdate) — UI 表单保存,唯一活的写
//   - 用户不会创建/删除配置项(走 seed.go + 软删 is_active=false)
//   - 查:ListConfigsView / GetConfigView / GetConfigByKeyView / ListGroups / ListConfigAudit
//   - 死代码已清:workflow 8 个函数 + DirectCreate/Delete/Upsert + Reload + GetConfigPlain
//     + Import/Export + KeysRotation + Diff + ListChangeRequestsView
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// configCenterService 真实实现
type configCenterService struct {
	db *gorm.DB
}

// operatorNameSQL 操作人姓名的取值口径(M113,2026-10-29)
//
// ⚠️ **为什么不能直接取 `u.real_name`**
//
//	以前 4 处查询都是 `u.real_name AS user_name` —— 而后台账号的
//	real_name **默认是空的**(seed 只给 super_admin 填了"超级管理员",
//	其余账号都是空)⇒ 审计日志的「操作人」列、配置项的「最后修改人」列
//	**长期显示空白**,管理员根本看不出是谁改的配置。
//
//	口径与审批流保持一致(`approval_query_impl.go` 那边早就是三段 fallback):
//	真实姓名 → 昵称 → 账号,第一个非空的胜出。
//
//	⇒ 顺带也说明了一件事:**「真实姓名」这个字段以前基本没人维护**
//	(个人中心压根不能填 —— 见 M113),所以这一列一直是空的。
const operatorNameSQL = "COALESCE(NULLIF(u.real_name, ''), NULLIF(u.nickname, ''), u.username)"

// NewConfigCenterService 构造
func NewConfigCenterService(db *gorm.DB) ConfigCenterService {
	return &configCenterService{db: db}
}

func (s *configCenterService) normPage(page, size int) (int, int) {
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

// ===== 配置项查询 =====

func (s *configCenterService) ListConfigs(ctx context.Context, q ConfigListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysConfig{})
	if q.Group != "" {
		tx = tx.Where("config_group = ?", q.Group)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("config_key LIKE ? OR config_name LIKE ? OR description LIKE ?", like, like, like)
	}
	if q.IsSecret != nil {
		tx = tx.Where("is_sensitive = ?", *q.IsSecret)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询配置失败").WithDetail(err.Error())
	}
	var rows []model.SysConfig
	if err := tx.Order("config_group ASC, sort ASC, id ASC").
		Offset((page - 1) * size).Limit(size).
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询配置失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

func (s *configCenterService) GetConfig(ctx context.Context, id uint64) (*model.SysConfig, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var c model.SysConfig
	if err := s.db.WithContext(ctx).First(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "配置不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询配置失败").WithDetail(err.Error())
	}
	return &c, nil
}

func (s *configCenterService) GetConfigByKey(ctx context.Context, key string) (*model.SysConfig, error) {
	if key == "" {
		return nil, errcode.New(errcode.BadRequest, "key 必填")
	}
	var c model.SysConfig
	if err := s.db.WithContext(ctx).Where("config_key = ?", key).First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "配置不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询配置失败").WithDetail(err.Error())
	}
	return &c, nil
}

// GetDecryptedValue 取解密值(本期直接返回 ValueEncrypted 字段值;后续 phase 接 crypto)
func (s *configCenterService) GetDecryptedValue(ctx context.Context, key string) (string, error) {
	c, err := s.GetConfigByKey(ctx, key)
	if err != nil {
		return "", err
	}
	if !c.IsActive {
		return "", errcode.New(errcode.StateInvalid, "配置已停用")
	}
	return c.ValueEncrypted, nil
}

// ===== 审计查询 =====
// ===== 变更流 =====


// ===== 审计查询 =====

func (s *configCenterService) ListConfigAudit(ctx context.Context, q ConfigAuditListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysConfigAudit{})
	if q.ConfigKey != "" {
		// ⚠️ **子串匹配,不是精确匹配** —— 审计页那个框是"搜索",精确匹配等于没有搜索
		//   (用户输 "access_key" 想看的是所有含这个片段的键)。
		//   用 INSTR 而不是 LIKE:config_key 里全是下划线(access_key / sign_name),
		//   LIKE 的 '_' 是单字符通配符 ⇒ 搜 "access_key" 会把 "accessXkey" 一起命中。
		//   INSTR 是纯子串查找(SQLite / MySQL 都支持),不吃通配符,也不用写转义。
		tx = tx.Where("INSTR(sys_config_audit.config_key, ?) > 0", q.ConfigKey)
	}
	if q.ConfigGroup != "" {
		// ⚠️ **用子查询,不用 LIKE 'group.%'** ——
		//   "这个组下有哪些配置项"是 sys_config 的事实(配置项会增删),
		//   前缀匹配会把"组名恰好是另一个组前缀"的键也算进来(aliyun.sms vs aliyun.smsx)。
		//   子查询是精确的:这个组现在有哪些 key,就只返回这些 key 的审计。
		tx = tx.Where(
			"sys_config_audit.config_key IN (SELECT config_key FROM sys_config WHERE config_group = ?)",
			q.ConfigGroup,
		)
	}
	if q.UserID > 0 {
		tx = tx.Where("sys_config_audit.user_id = ?", q.UserID)
	}
	if q.Action != "" {
		tx = tx.Where("sys_config_audit.action = ?", q.Action)
	}
	if q.StartAt != nil {
		tx = tx.Where("sys_config_audit.created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("sys_config_audit.created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询配置审计失败").WithDetail(err.Error())
	}
	// 2026-10-18:LEFT JOIN sys_user 拿 user_name 给前端直接展示
	type row struct {
		model.SysConfigAudit
		UserName string `gorm:"column:user_name"`
	}
	var rows []row
	if err := tx.
		Select("sys_config_audit.*, " + operatorNameSQL + " AS user_name").
		Joins("LEFT JOIN sys_user u ON u.id = sys_config_audit.user_id").
		Order("sys_config_audit.id DESC").
		Offset((page - 1) * size).Limit(size).
		Scan(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询配置审计失败").WithDetail(err.Error())
	}
	// 把 user_name 拼到 view DTO 上,前端读 .user_name
	type viewRow struct {
		model.SysConfigAudit
		UserName string `json:"user_name"`
	}
	views := make([]viewRow, 0, len(rows))
	for _, r := range rows {
		views = append(views, viewRow{SysConfigAudit: r.SysConfigAudit, UserName: r.UserName})
	}
	return &model.PageResponse{List: views, Total: total, Page: page, Size: size}, nil
}

// ListHotReload 本期:暂未独立建表,用 sys_config_export_log 兜底
func (s *configCenterService) ListHotReload(ctx context.Context, q HotReloadListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysConfigExportLog{})
	if q.ConfigKey != "" {
		// 导出日志无 config_key 字段,按 exportType 过滤
		tx = tx.Where("export_type = ?", "preview")
	}
	if q.StartAt != nil {
		tx = tx.Where("created_at >= ?", time.Unix(*q.StartAt, 0))
	}
	if q.EndAt != nil {
		tx = tx.Where("created_at <= ?", time.Unix(*q.EndAt, 0))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询热加载日志失败").WithDetail(err.Error())
	}
	var rows []model.SysConfigExportLog
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询热加载日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

// sonyflakeHex6 sonyflake id 转 6 字符 hex(给请求号)
func sonyflakeHex6(id uint64) string {
	const hex = "0123456789ABCDEF"
	if id == 0 {
		return "000000"
	}
	out := make([]byte, 0, 6)
	for i := 0; i < 6; i++ {
		out = append([]byte{hex[id&0xF]}, out...)
		id >>= 4
	}
	return string(out)
}

// ===== 增强(本期实装)=====

// Stats 按 group / 状态统计
func (s *configCenterService) Stats(ctx context.Context) (map[string]any, error) {
	var rows []struct {
		ConfigGroup string
		Count       int64
		ActiveCount int64
	}
	if err := s.db.WithContext(ctx).Model(&model.SysConfig{}).
		Select("config_group, COUNT(*) as count, SUM(CASE WHEN is_active = 1 THEN 1 ELSE 0 END) as active_count").
		Group("config_group").Scan(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "统计失败").WithDetail(err.Error())
	}
	// 待审变更
	var pendingChg int64
	s.db.WithContext(ctx).Model(&model.SysConfigChangeRequest{}).
		Where("status = ?", "pending").Count(&pendingChg)

	out := map[string]any{
		"totalConfigs":      0,
		"byGroup":           rows,
		"pendingChanges":    pendingChg,
	}
	var total int64
	for _, r := range rows {
		total += r.Count
	}
	out["totalConfigs"] = total
	return out, nil
}

// Health 配置中心健康(基础:能否 SELECT)
func (s *configCenterService) Health(ctx context.Context) (map[string]any, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.SysConfig{}).Count(&n).Error; err != nil {
		return map[string]any{
			"status": "down",
			"error":  err.Error(),
		}, nil
	}
	return map[string]any{
		"status":   "ok",
		"configs":  n,
		"checkedAt": time.Now().Format(time.RFC3339),
	}, nil
}


// ListGroups 2026-10-18:取所有有数据的 config_group 列表(给前端 group filter 用)
func (s *configCenterService) ListGroups(ctx context.Context) ([]ConfigGroupKV, error) {
	type row struct {
		ConfigGroup string
		Count       int64
	}
	var rows []row
	if err := s.db.WithContext(ctx).Model(&model.SysConfig{}).
		Select("config_group, COUNT(*) as count").
		Group("config_group").
		Order("config_group ASC").
		Scan(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询配置分组失败").WithDetail(err.Error())
	}
	out := make([]ConfigGroupKV, 0, len(rows))
	for _, r := range rows {
		out = append(out, ConfigGroupKV{Group: r.ConfigGroup, Count: int(r.Count)})
	}
	return out, nil
}

func (s *configCenterService) DirectUpdate(ctx context.Context, caller *Caller, id uint64, patch map[string]any) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if !caller.HasAnyRole(model.RoleSuperAdmin) {
		return errcode.New(errcode.Forbidden, "仅 super_admin 可直接更新")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 必填")
	}
	if len(patch) == 0 {
		return errcode.New(errcode.BadRequest, "patch 不能为空")
	}
	var cfg model.SysConfig
	if err := s.db.WithContext(ctx).First(&cfg, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "配置不存在")
		}
		return errcode.New(errcode.Internal, "查询配置失败").WithDetail(err.Error())
	}
	oldValue := cfg.ValueEncrypted
	// 白名单 patch
	allowed := map[string]bool{
		"config_name": true, "description": true, "value_encrypted": true,
		"value_type": true, "is_sensitive": true, "is_required": true, "is_readonly": true,
		"regex_validate": true, "min_length": true, "max_length": true,
		"env_var_name": true, "sort": true, "is_active": true, "remarks": true,
	}
	updates := map[string]any{}
	for k, v := range patch {
		if !allowed[k] {
			continue
		}
		updates[k] = v
	}
	if len(updates) == 0 {
		return errcode.New(errcode.BadRequest, "patch 字段全部不在白名单")
	}
	// 改 value 时同步 preview
	if v, ok := updates["value_encrypted"].(string); ok {
		isSens := cfg.IsSensitive
		if vs, ok2 := updates["is_sensitive"].(bool); ok2 {
			isSens = vs
		}
		updates["value_preview"] = previewValueLocal(v, isSens)
	}
	now := model.Time{Time: time.Now()}
	updates["version"] = gorm.Expr("version + 1")
	updates["last_changed_by"] = caller.UserID
	updates["last_changed_at"] = &now
	if err := s.db.WithContext(ctx).Model(&cfg).Updates(updates).Error; err != nil {
		return errcode.New(errcode.Internal, "更新配置失败").WithDetail(err.Error())
	}
	// 写审计
	newValue, _ := updates["value_encrypted"].(string)
	if newValue == "" {
		newValue = cfg.ValueEncrypted
	}
	_ = s.db.WithContext(ctx).Create(&model.SysConfigAudit{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		ConfigID:  cfg.ID,
		ConfigKey: cfg.ConfigKey,
		Action:    model.ConfigAuditActionUpdate,
		UserID:    caller.UserID,
		UserRole:  caller.RoleCode,
		IP:        caller.IP,
		Before:    oldValue,
		After:     newValue,
	}).Error
	return nil
}


// toView model.SysConfig → ConfigItemView 转换(脱敏 + 联表 username)
//   注意:不在这里联表查 user(避免 N+1),改用 LEFT JOIN 一次性取出
func (s *configCenterService) toView(c *model.SysConfig, lastChangedByName string) *ConfigItemView {
	return &ConfigItemView{
		ID:                c.ID,
		ConfigGroup:       c.ConfigGroup,
		ConfigKey:         c.ConfigKey,
		ConfigName:        c.ConfigName,
		Description:       c.Description,
		ValueType:         c.ValueType,
		Value:             previewValueLocal(c.ValueEncrypted, c.IsSensitive),
		ValuePreview:      c.ValuePreview,
		IsSensitive:       c.IsSensitive,
		RegexValidate:     c.RegexValidate,
		MinLength:         c.MinLength,
		MaxLength:         c.MaxLength,
		IsRequired:        c.IsRequired,
		IsReadonly:        c.IsReadonly,
		EnvVarName:        c.EnvVarName,
		Sort:              c.Sort,
		IsActive:          c.IsActive,
		Remarks:           c.Remarks,
		Version:           c.Version,
		LastChangedBy:     c.LastChangedBy,
		LastChangedByName: lastChangedByName,
		LastChangedAt:     c.LastChangedAt,
	}
}

// ListConfigsView 2026-10-18:list 接口返 View DTO(解决 value 隐藏问题)
func (s *configCenterService) ListConfigsView(ctx context.Context, q ConfigListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysConfig{})
	if q.Group != "" {
		tx = tx.Where("config_group = ?", q.Group)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("config_key LIKE ? OR config_name LIKE ? OR description LIKE ?", like, like, like)
	}
	if q.IsSecret != nil {
		tx = tx.Where("is_sensitive = ?", *q.IsSecret)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询配置失败").WithDetail(err.Error())
	}
	// LEFT JOIN sys_user 拿 last_changed_by_name
	type row struct {
		model.SysConfig
		LastChangedByName string `gorm:"column:last_changed_by_name"`
	}
	var rows []row
	if err := tx.
		Select("sys_config.*, " + operatorNameSQL + " AS last_changed_by_name").
		Joins("LEFT JOIN sys_user u ON u.id = sys_config.last_changed_by").
		Order("sys_config.config_group ASC, sys_config.sort ASC, sys_config.id ASC").
		Offset((page - 1) * size).Limit(size).
		Scan(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询配置失败").WithDetail(err.Error())
	}
	views := make([]*ConfigItemView, 0, len(rows))
	for i := range rows {
		views = append(views, s.toView(&rows[i].SysConfig, rows[i].LastChangedByName))
	}
	return &model.PageResponse{List: views, Total: total, Page: page, Size: size}, nil
}

// GetConfigView 2026-10-18:detail 接口返 View DTO
func (s *configCenterService) GetConfigView(ctx context.Context, id uint64) (*ConfigItemView, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 必填")
	}
	type row struct {
		model.SysConfig
		LastChangedByName string `gorm:"column:last_changed_by_name"`
	}
	var r row
	if err := s.db.WithContext(ctx).
		Table("sys_config").
		Select("sys_config.*, " + operatorNameSQL + " AS last_changed_by_name").
		Joins("LEFT JOIN sys_user u ON u.id = sys_config.last_changed_by").
		Where("sys_config.id = ?", id).
		Scan(&r).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询配置失败").WithDetail(err.Error())
	}
	if r.ID == 0 {
		return nil, errcode.New(errcode.NotFound, "配置不存在")
	}
	return s.toView(&r.SysConfig, r.LastChangedByName), nil
}

// GetConfigByKeyView 2026-10-18:按 key 查 View DTO
func (s *configCenterService) GetConfigByKeyView(ctx context.Context, key string) (*ConfigItemView, error) {
	if key == "" {
		return nil, errcode.New(errcode.BadRequest, "key 必填")
	}
	type row struct {
		model.SysConfig
		LastChangedByName string `gorm:"column:last_changed_by_name"`
	}
	var r row
	if err := s.db.WithContext(ctx).
		Table("sys_config").
		Select("sys_config.*, " + operatorNameSQL + " AS last_changed_by_name").
		Joins("LEFT JOIN sys_user u ON u.id = sys_config.last_changed_by").
		Where("sys_config.config_key = ?", key).
		Scan(&r).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询配置失败").WithDetail(err.Error())
	}
	if r.ID == 0 {
		return nil, errcode.New(errcode.NotFound, "配置不存在")
	}
	return s.toView(&r.SysConfig, r.LastChangedByName), nil
}

// previewValueLocal 2026-10-18:本地脱敏预览(避免和 seed.previewValue 命名冲突)
//   DirectUpdate 改 value 时同步生成 preview 用
func previewValueLocal(v string, sensitive bool) string {
	if v == "" {
		return ""
	}
	if sensitive {
		if len(v) <= 8 {
			return "***"
		}
		return v[:4] + "***" + v[len(v)-4:]
	}
	return v
}
