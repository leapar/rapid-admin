// 部门 seed(给"系统管理 → 部门管理"页看真实数据 + 演示用)
//
// 2026-10-18 落地:
//   - 用户反馈:部门管理页"暂无数据" — 之前没有任何 seed,前端表格空
//   - 部门 = 组织架构,本应是租户/集团上线时录入的"基础数据",但 demo 环境需要默认有数据
//     才能演示搜索/排序/树形/新增子部门
//   - 真实生产环境(集团租户级)下,租户管理员会自己建自己的部门;本 seed 只用于 dev 环境
//   - 部门表 sys_department 有 0 行时,前端用户管理页"部门"下拉也是空的,本 seed 也修了这个问题
//
// 设计原则:
//   - 16 节点,3 层(L0 集团 + L1 中心 + L2 部/组)
//   - 中性示例:RapidAdmin 集团(研发/运营/工程三线),可直接当 demo 数据,也可照抄改成自己的组织架构
//   - 树形:parent_id 严格指向上一级 ID,前端 tree 接口直接消费
//   - code 唯一(用于业务引用),保持稳定
//   - sort 控制同级排序
//   - leader_id 绑 L1 中心(7 个)→ admin;L2 部门不设(由接入方上线时指定)
//
// 运行: go run ./cmd/seed_dept -config configs/config.dev.yaml
package main

import (
	"flag"
	"fmt"
	"log"

	"gorm.io/gorm"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
	"rapid-admin/internal/repository"
)

// deptSeed 一条记录 = 一个部门
//   插入顺序:必须先父后子(ID 由 sonyflake 顺序生成,parent_id 在子节点才能填)
type deptSeed struct {
	code    string // 业务 code(唯一,用于外部关联)
	name    string // 中文名
	parent  string // 父节点 code(空 = 根)
	sort    int
	leader  uint64 // 负责人 user_id,0 = 未设
}

func main() {
	cfgPath := flag.String("config", "configs/config.dev.yaml", "配置文件路径")
	flag.Parse()

	if err := config.Load(*cfgPath); err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg := config.Get()
	if _, err := repository.NewDB(cfg.Database, cfg.MySQL); err != nil {
		log.Fatalf("init db: %v", err)
	}
	db := repository.GetDB()

	// 1. 先清空旧部门(避免重复 seed 累积;真实环境禁用此 seed,无副作用)
	//    2026-10-18:用 Unscoped 硬删,BaseModel 有 DeletedAt,默认 soft-delete 会留行
	//    导致 code unique 冲突。dev seed 才硬删,prod 走 migrate 不受影响
	if err := db.Unscoped().Where("1 = 1").Delete(&model.SysDepartment{}).Error; err != nil {
		log.Fatalf("clear departments: %v", err)
	}
	// 顺手清掉用户-部门关联(防止 FK 悬空;本 seed 不造 user-dept 关系,纯演示)
	if err := db.Unscoped().Where("1 = 1").Delete(&model.SysUserDepartment{}).Error; err != nil {
		log.Fatalf("clear user-dept: %v", err)
	}

	// 2. 定义 16 节点部门树(按"先父后子"顺序排列)
	//    中性示例:RapidAdmin 集团(通用集团组织架构)
	//    leader 0 = 未设,非 0 = 绑到 sys_user.id
	//    本 seed 只给 L0 集团 + L1 中心 7 个节点配 leader(都指向 super_admin)
	//    L2 部门不绑(由接入方上线时指定,避免演示数据看起来太假)
	seeds := []deptSeed{
		// ----- L0:集团根 -----
		{"RA_GROUP", "RapidAdmin 集团", "", 1, 0}, // root 不绑(无意义),下面查 admin 后填

		// ----- L1:集团直属中心 -----
		{"RA_PRESIDENT", "总裁办", "RA_GROUP", 1, 0},
		{"RA_DIGITAL", "数字化中心", "RA_GROUP", 2, 0},
		{"RA_OPERATIONS", "运营管理中心", "RA_GROUP", 3, 0},
		{"RA_ENGINEERING", "工程项目中心", "RA_GROUP", 4, 0},
		{"RA_IT", "信息技术中心", "RA_GROUP", 5, 0},
		{"RA_HR", "人力资源中心", "RA_GROUP", 6, 0},
		{"RA_FINANCE", "财务中心", "RA_GROUP", 7, 0},

		// ----- L2:数字化中心 — 研发体系 -----
		{"RA_DIGITAL_PRODUCT", "产品部", "RA_DIGITAL", 1, 0},
		{"RA_DIGITAL_RND", "研发部", "RA_DIGITAL", 2, 0},
		{"RA_DIGITAL_QA", "测试部", "RA_DIGITAL", 3, 0},
		{"RA_DIGITAL_OPS", "运维部", "RA_DIGITAL", 4, 0},

		// ----- L2:运营管理中心 -----
		{"RA_OPS_CUSTOMER", "客户运营部", "RA_OPERATIONS", 1, 0},
		{"RA_OPS_SUPPORT", "客户服务部", "RA_OPERATIONS", 2, 0},

		// ----- L2:工程项目中心 — 在建项目 -----
		{"RA_PROJ_1", "项目一部", "RA_ENGINEERING", 1, 0},
		{"RA_PROJ_2", "项目二部", "RA_ENGINEERING", 2, 0},
		{"RA_PROJ_3", "项目三部", "RA_ENGINEERING", 3, 0},
	}

	// 2.1 查 super_admin 用户 ID(sonyflake 生成,不同 DB 不同,不能在 seed 里硬编码)
	//     demo 环境只有 admin 一个真用户,其他都是 e2e 测试临时建的
	var adminUser model.SysUser
	if err := db.Where("username = ?", "admin").First(&adminUser).Error; err != nil {
		log.Fatalf("query admin user: %v (need to run seed first)", err)
	}
	// 给 L0 + L1 全部 7 个中心节点配 leader = admin
	// L2 部门保留 leader=0,演示 '未设' 占位
	seeds[0].leader = adminUser.ID
	for i := 1; i <= 7; i++ {
		seeds[i].leader = adminUser.ID
	}

	// 3. 第一遍:插入所有节点,先生成 sonyflake ID
	//    用 map[code]id 收集节点 ID,供第二轮填 parent_id
	idByCode := map[string]uint64{}
	dept := make([]model.SysDepartment, 0, len(seeds))
	for _, s := range seeds {
		d := model.SysDepartment{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			ParentID:  0, // 第二轮补
			Name:      s.name,
			Code:      s.code,
			Sort:      s.sort,
			LeaderID:  s.leader,
			IsActive:  true,
		}
		idByCode[s.code] = d.ID
		dept = append(dept, d)
	}

	// 4. 第二遍:补 parent_id(用第一轮拿到的 ID)
	for i, s := range seeds {
		if s.parent == "" {
			continue
		}
		pid, ok := idByCode[s.parent]
		if !ok {
			log.Fatalf("parent code not found: %s (child: %s)", s.parent, s.code)
		}
		dept[i].ParentID = pid
	}

	// 5. 批量插入
	if err := db.Create(&dept).Error; err != nil {
		log.Fatalf("insert departments: %v", err)
	}
	fmt.Printf("[seed_dept] inserted %d departments\n", len(dept))

	// 6. 验证 — 打印树形结构(L0 → L1 → L2)
	fmt.Println("\n[verify] 部门树形结构:")
	printTree(db, 0, 0)
}

// printTree 递归打印部门树
func printTree(db *gorm.DB, parentID uint64, depth int) {
	var rows []model.SysDepartment
	q := db.Where("is_active = ?", true).Order("sort ASC, id ASC")
	if parentID == 0 {
		q = q.Where("parent_id = ? OR parent_id IS NULL", 0)
	} else {
		q = q.Where("parent_id = ?", parentID)
	}
	if err := q.Find(&rows).Error; err != nil {
		fmt.Printf("  (load error: %v)\n", err)
		return
	}
	for _, r := range rows {
		prefix := ""
		for i := 0; i < depth; i++ {
			prefix += "  "
		}
		fmt.Printf("%s├─ [%s] %s (id=%d)\n", prefix, r.Code, r.Name, r.ID)
		printTree(db, r.ID, depth+1)
	}
}
