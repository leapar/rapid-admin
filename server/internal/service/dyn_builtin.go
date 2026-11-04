// 内置集合的启动期播种 —— 框架自带一个通用示例集合(公告)
//
// # 这一刀解决什么
//
// 动态集合(dyn)是本框架的"业务表单设计器"能力:运营在界面上建集合,
// 后端自动建物理表、登记资源码、同步 casbin。为了开箱可用,
// 启动期幂等播种一个内置集合作为样例:
//
//	· `notice` 公告管理 —— "结构化台账 + CRUD + 送审"的参考实现
//
// # 为什么在启动期播种,而不是放 internal/seed
//
// 建集合走的是 dynService 的服务逻辑(建物理表、登记资源码、同步 casbin),
// 而 seed 包只管 sys_* 表,拿不到装配好的 dynService(它还依赖审批引擎、文件解析器)。
// 放在 main 装配完成之后,依赖天然齐全。
//
// # 幂等口径(三条)
//
//  1. **集合编码已存在 ⇒ 整个定义跳过** —— 运营可能已经改名 / 绑了审批模板 /
//     停了某些字段,启动期一律不覆盖(这里不是"对账",是"首次播种")。
//  2. **字段按 (collection, field_key) 逐个补** —— 防半截播种
//     (上次启动建到一半失败):缺的补上,在的不动。
//  3. **绑定幂等** —— bindResourcesToRole 走 OnConflict DoNothing。
//
// # 明确不做(写清楚,防止后人"补齐")
//
//	· **不迁移旧数据** —— 内置集合是新表(dyn_notice),从零开始录。
//	· **不绑审批模板** —— 台账默认没有流转;运营在设计器里随时可以绑。
//	· 业务方接入自己的模块时,照这个样例在 builtinCollections 里加自己的集合,
//	  或全部删掉不留(框架对"零内置集合"无假设)。
//
package service

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/dynx"
	"rapid-admin/internal/pkg/errcode"
)

// builtinCollectionDef 一个内置集合的完整定义
type builtinCollectionDef struct {
	// code 集合编码(同时是物理表名 dyn_<code> 与资源码来源,建了就不可改)
	code string
	// name 集合显示名(⚠️ 刻意与旧菜单名不同字,侧边栏两个菜单并存时肉眼可分)
	name        string
	description string
	icon        string
	sort        int
	// roles 资源绑给哪些业务角色(超管绕过绑定不用绑)
	roles []string
	// fields 字段定义(平铺口径 —— 层级外键承接是 §11.2 Phase 2b 的事)
	fields []DynFieldInput
}

// mustChoices 造 select 选项集 JSON(值与显示名相同 —— 台账场景不需要"存代号")
func mustChoices(labels ...string) string {
	choices := make([]dynx.Choice, 0, len(labels))
	for _, l := range labels {
		choices = append(choices, dynx.Choice{Value: l, Label: l})
	}
	b, err := json.Marshal(map[string]any{"choices": choices})
	if err != nil {
		// 常量拼接不可能失败;真失败了启动期 fail-fast 比存个坏选项集强
		panic("builtin dyn options: " + err.Error())
	}
	return string(b)
}

// dynx.Choice 在 options.go 里定义,这里确认形状(value/label)即可

var builtinCollections = []builtinCollectionDef{
	{
		code:        "notice",
		name:        "公告管理",
		description: "内置示例集合:公告的录入与送审,展示动态集合「台账 + 送审」的标准用法",
		icon:        "notice",
		sort:        86,
		roles:       []string{model.RolePlatformOps},
		fields: []DynFieldInput{
			{FieldKey: "title", Label: "标题", Type: string(dynx.TypeText), Required: true, Sort: 1},
			{FieldKey: "notice_type", Label: "公告类型", Type: string(dynx.TypeSelect), Required: true,
				Options: mustChoices("通知", "营销", "活动"), DefaultValue: "通知", Sort: 2},
			{FieldKey: "content", Label: "内容", Type: string(dynx.TypeLongText), Sort: 3},
			{FieldKey: "priority", Label: "优先级", Type: string(dynx.TypeSelect), Required: true,
				Options: mustChoices("普通", "重要", "紧急"), DefaultValue: "普通", Sort: 4},
			{FieldKey: "publish_at", Label: "发布时间", Type: string(dynx.TypeDateTime), Sort: 5},
			{FieldKey: "status", Label: "状态", Type: string(dynx.TypeSelect), Required: true,
				Options: mustChoices("草稿", "待发布", "已发布", "已撤回"), DefaultValue: "草稿", Sort: 6},
			{FieldKey: "attachment", Label: "附件", Type: string(dynx.TypeFile), Sort: 7},
		},
	},
}

// EnsureBuiltinCollections 启动期幂等播种内置业务集合
//
// ⚠️ **失败 fail-fast(调用方 log.Fatal)** —— 半截播种的集合
// 和 M151 那批"缺资源码的死接口"是同一类病:不报错,就是不能用。
func (s *dynService) EnsureBuiltinCollections(ctx context.Context) error {
	for i := range builtinCollections {
		def := &builtinCollections[i]
		if err := s.seedBuiltinCollection(ctx, def); err != nil {
			return errcode.New(errcode.Internal, "播种内置集合 "+def.code+" 失败").WithDetail(err.Error())
		}
	}
	return nil
}

func (s *dynService) seedBuiltinCollection(ctx context.Context, def *builtinCollectionDef) error {
	// 幂等①:集合编码已存在(含运营后来建的)⇒ 不重建,但**字段仍要逐个补** ——
	// 半截播种(上次启动建了集合、字段建到一半进程被杀)会留下
	// "集合在、字段缺"的状态,只有这里能救回来。
	// 不覆盖的口径只针对集合行本身(名字/图标/模板/绑定),字段是"缺了补上"。
	var colPtr *model.SysDynCollection
	var colRow model.SysDynCollection
	err := s.db.WithContext(ctx).Where("code = ?", def.code).First(&colRow).Error
	switch {
	case err == nil:
		// 已存在 ⇒ 走下面的字段补齐,不重建集合
		colPtr = &colRow
	case err == gorm.ErrRecordNotFound:
		colID, cerr := s.createCollectionCore(ctx, def.roles[0], &DynCollectionInput{
			Code:        def.code,
			Name:        def.name,
			Description: def.description,
			Icon:        def.icon,
			Sort:        def.sort,
		})
		if cerr != nil {
			return cerr
		}
		if colPtr, err = s.GetCollection(ctx, colID); err != nil {
			return err
		}
	default:
		return errcode.New(errcode.Internal, "查询集合失败").WithDetail(err.Error())
	}

	// 幂等②:字段逐个补。
	//
	// ⚠️ **查活行之前先查 Unscoped 总数** —— 运营可能**故意删过**(停用)某个字段:
	// DeleteField 是软删,物理列还在。只查活行的话,播种会把它当"缺"重新建 ——
	// 复活运营明确删掉的字段是越权;而且 ADD COLUMN 撞上还活着的列会直接报错,
	// 症状是"服务器起不来,报 duplicate column",没人知道是播种干的。
	for fi := range def.fields {
		in := &def.fields[fi]
		var ever int64
		if err := s.db.WithContext(ctx).Unscoped().Model(&model.SysDynField{}).
			Where("collection_id = ? AND field_key = ?", colPtr.ID, in.FieldKey).
			Count(&ever).Error; err != nil {
			return errcode.New(errcode.Internal, "查询字段失败").WithDetail(err.Error())
		}
		if ever > 0 {
			continue
		}
		if _, err := s.createFieldCore(ctx, colPtr, in); err != nil {
			return err
		}
	}

	// 幂等③:绑给全部业务角色(createCollectionCore 已绑第一个,这里补齐其余;
	// DoNothing 幂等,重复绑无害)。
	//
	// ⚠️ **必须重新按码取资源 id** —— createCollectionCore 不返回 id,
	// 而 registerCollectionResources 内部拿到的 ids 也不对外。
	codes := dynCollectionCodes(def.code)
	var resources []model.SysResource
	if err := s.db.WithContext(ctx).Where("code IN ?", codes).Find(&resources).Error; err != nil {
		return errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
	}
	ids := make([]uint64, 0, len(resources))
	for _, r := range resources {
		ids = append(ids, r.ID)
	}
	for _, role := range def.roles[1:] {
		if err := s.bindResourcesToRole(ctx, role, ids); err != nil {
			return err
		}
	}
	return nil
}
