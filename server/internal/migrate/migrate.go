// 数据库迁移 —— AutoMigrate 全量模型表
// 文档 §4.2 / §5
package migrate

import (
	"fmt"
	"sort"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// All 全量模型列表(按表名稳定排序,便于 diff)
// 新增 model 时,在此追加即可
func All() []any {
	return []any{
		// system
		&model.SysUser{},
		&model.SysUserIdentity{},
		&model.SysRole{},
		&model.SysResource{},
		&model.SysRoleResource{},
		&model.SysDepartment{},
		&model.SysUserDepartment{},
		&model.SysDict{},
		&model.SysCasbinRule{},
		// M155 —— 数据范围(行级权限)规则
		//
		// ⚠️ **它是"角色"的附属配置,不是"角色"本身** ——
		// 与 sys_role_resource 同级(都是"角色配了什么"),
		// 但语义完全不同:那管"能不能调",这张管"能看到哪些行"。
		&model.SysDataScope{},

		// M162 —— 字段级可见性(列级权限)规则
		//
		// ⚠️ **它与 sys_data_scope 是两根轴,不是同一个东西的两种写法** ——
		// sys_data_scope 管"能看到哪些**行**";
		// 这张表管"那一行里的哪些**字段**能看到"。
		// 能看到某行 ≠ 能看到那行的每个字段。
		//
		// ⚠️ **同样是"角色的附属配置"** —— 与 sys_role_resource 同级,
		// 语义不同:那管"能不能调",这张管"能看到什么"。
		&model.SysFieldPolicy{},

		// audit
		&model.SysOperationLog{},
		&model.SysDataChangeLog{},
		&model.SysLoginLog{},
		// 系统运行日志(2026-11-03)—— 慢 SQL / 慢接口 / 5xx / panic 落库
		&model.SysSystemLog{},
		&model.BizAuditFullInfoView{},
		&model.BizAuditDataExportLog{},
		// §1.5 #5 P0 —— 框架级 PII 字段解密访问日志
		&model.SysPIIAccessLog{},
		// §1.5 #8 P1 —— 密钥使用审计
		&model.SysCryptoAuditLog{},

		// sms
		&model.SysSmsCode{},
		// §1.9 #7 P1 —— 短信发送审计日志
		&model.SysSmsSendLog{},
		// M115 —— 短信模板审核状态 + 流转历史
		// ⚠️ sys_sms_template 的 scene 是**唯一索引**,它是"一个场景一行"的保证 ——
		// 同步任务并发跑两遍时靠它收敛,而不是靠应用层的读改写。
		&model.SysSmsTemplate{},
		&model.SysSmsTemplateLog{},
		&model.RoleSwitchAudit{},
		// platform config
		&model.SysConfig{},
		&model.SysConfigChangeRequest{},
		&model.SysConfigAudit{},
		&model.SysConfigExportLog{},

		// auth extension(Phase 2 — 资源级授权 / 用户 override / 功能开关)
		&model.PermissionGrant{},
		&model.UserPermissionOverride{},
		&model.UserFeatureFlag{},

		// wx template
		&model.WxTemplate{},
		&model.WxTemplateSendLog{},

		// guide
		&model.Guide{},

		// 语音能力(voicex)—— 音色克隆台账 + TTS 音色登记册
		&model.SysVoiceClone{},
		&model.SysTtsVoice{},

		// §1.9 #11+#12 P1 —— 站内信 + 通知中心
		&model.InboxMessage{},
		&model.InboxUserPrefs{},

		// cronx 定时任务调度(M67)
		// sys_job_run 同时兼作去重 Layer 0,索引 idx_job_started 是关键 ——
		// 删掉它 Layer 0 会退化成全表扫。别在别处重复定义这两张表。
		&model.SysJob{},
		&model.SysJobRun{},

		// 文件系统模块(M79)—— 表里**不含文件内容**,只存 key 和元信息。
		// 内容在 local 磁盘或 OSS 上。⚠️ sys_user 的两个配额列
		// (storage_used_bytes / storage_quota_bytes)加在 SysUser 上,
		// AutoMigrate 自动补列,不需要在这里单列。
		&model.SysFolder{},
		&model.SysFile{},
		&model.SysUploadSession{},

		// 通用审批系统(M83)—— 七张表,全部 sys_ 前缀(不碰 biz_ 命名空间)。
		// ⚠️ **唯一索引也来自 struct tag**(GORM 的 Migrator 走 schema.ParseIndexes),
		// 所以下面不需要额外写 DDL —— 反过来说,**tag 写错 DDL 就是错的,而且没人报错**。
		// 防线在 model/approval_test.go:四条唯一索引各有一条"真插重复行"的断言。
		&model.SysApprovalTemplate{},
		&model.SysApprovalStage{},
		&model.SysApprovalAssigneeRule{},
		&model.SysApprovalInstance{},
		&model.SysApprovalTask{},
		&model.SysApprovalLog{},
		&model.SysApprovalRound{},

		// 动态业务集合(M150)—— 用户在「业务表单设计器」里设计的业务表。
		//
		// ⚠️ **这里只登记"设计的元数据"** ——
		// 业务数据本身在运行期建的物理表里(dyn_<code>),
		// 不走 AutoMigrate(那些表是手写 DDL 建的,方言相关)。
		&model.SysDynCollection{},
		&model.SysDynField{},

		// 领域事件事务性 outbox —— 事件先落表、后投递(eventx 投递器轮询这张表)。
		&model.SysEventOutbox{},
	}
}

// Run AutoMigrate 全量表
//   - db:        *gorm.DB
//   - dryRun:    true=只打印 SQL 不真正执行
//   - verbose:   true=打印每张表
func Run(db *gorm.DB, dryRun, verbose bool) error {
	all := All()
	// 排序后逐个迁移,顺序更稳定
	type named struct {
		Table string
		Model any
	}
	ns := make([]named, 0, len(all))
	for _, m := range all {
		if tabler, ok := m.(interface{ TableName() string }); ok {
			ns = append(ns, named{Table: tabler.TableName(), Model: m})
		} else {
			ns = append(ns, named{Table: fmt.Sprintf("%T", m), Model: m})
		}
	}
	sort.Slice(ns, func(i, j int) bool { return ns[i].Table < ns[j].Table })

	for _, n := range ns {
		if dryRun {
			// dry-run:仅依赖 gorm.Session{DryRun: true},让 AutoMigrate 不真正发 SQL
			if err := db.Session(&gorm.Session{DryRun: true}).AutoMigrate(n.Model); err != nil {
				return fmt.Errorf("migrate(dryRun) %s: %w", n.Table, err)
			}
			fmt.Printf("[migrate:dryRun] %s planned (run without -dry-run to apply)\n", n.Table)
			continue
		}
		if err := db.AutoMigrate(n.Model); err != nil {
			return fmt.Errorf("migrate %s: %w", n.Table, err)
		}
		if verbose {
			fmt.Printf("[migrate] OK %s\n", n.Table)
		}
	}

	return nil
}
