// 种子数据 —— 13 角色 + 资源 + 角色-资源绑定 + 超级管理员账号 + 字典(dicts) + Casbin policy 同步
// 文档 §6.1
// 由 cmd/seed 或 scripts/seed 调用
//
// 2026-10-18:新增字典 seed(16 P0) + SeedDictGuard()
//   - 字典与 RBAC 解耦,只服务于前端 useDict(groupCode) 数据源
//   - SeedDictGuard 在 Run() 末尾被调用,缺字典时 panic 阻断后续开发
//   - 与前端 web/src/types/dict.ts DictGroupCode 镜像(任何新字典必须双向登记)
package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/casbinx"
	"rapid-admin/internal/pkg/sonyflake"
)

// roleList 13 角色(全部带 role_ 前缀,与 model/enums.go 一致)
//
//	2026-10-17:加 Type(platform/business)区分平台级与业务级,文档 §6.4
//	- 平台:超管 / 平台运营主管 / 平台运营员(跨项目,管后台)
//	- 业务:其他 10 个(项目内,做交付/物业/资料管理)
func roleList() []struct {
	Code  string
	Name  string
	Level int
	Type  string
} {
	return []struct {
		Code  string
		Name  string
		Level int
		Type  string
	}{
		// 开源口径:框架自带 3 个内置角色(超管直通全量 / 平台运营员框架读写 /
		// 只读查看员框架只读)。业务方接入自己的模块后,在这里加自己的内置角色。
		{model.RoleSuperAdmin, "超级管理员", 0, model.RoleTypePlatform},
		{model.RolePlatformOps, "平台运营员", 1, model.RoleTypePlatform},
		{model.RoleReadOnly, "只读查看员", 2, model.RoleTypePlatform},
	}
}

// resourceList 资源
//   - 命名空间:menu.* / button.* / *:* 分别是 menu/button/api 三类
//   - menu.* 走 menuResources 单独 seed(带 parent/path/icon/sort)
//   - button.* 和 *:* 在主循环里按前缀判 type
//   - button 资源用于细粒度按钮级权限(导出/导入/批量等)
//     2026-10-18 起加,12/15 前要覆盖所有业务页的常用动作
func resourceList() []string {
	return []string{
		// user/role
		"user:list", "user:read", "user:create", "user:update", "user:delete", "user:unlock", "user:toggle",
		"role:list", "role:read", "role:create", "role:update", "role:delete", "role:assign",
		"resource:list", "resource:read", "resource:tree", "resource:accessible", "resource:create", "resource:update", "resource:delete",
		"dept:list", "dept:read", "dept:tree", "dept:create", "dept:update", "dept:delete",
		"dict:list", "dict:create", "dict:update", "dict:delete",
		// 文件系统(M80,设计文档 §8)。⚠️ **取代 M76 之前的 `upload:*`** ——
		// 老码只覆盖"发个凭证"的占位实现,而且 `upload:delete` 从来没有对应路由
		// (死码)。新码按 §8.1/§8.2/§8.3 的实际路由一一对应:
		//   file:*   —— 文件本体 + 直传凭证 + 分片
		//   folder:* —— 文件夹;`folder:upload` 是"设封面"这一个动作,单独一个
		//               码是为了不给"文件夹管理"的人顺带开一个写文件的口子
		"file:list", "file:read", "file:upload", "file:delete",
		"folder:list", "folder:create", "folder:delete", "folder:upload",
		// config(2026-10-18 收窄:系统配置由 super_admin 独占,无工作流)
		"config:list", "config:read", "config:create", "config:update", "config:delete",
		"config:audit", "config:health", "config:stats",
		// job(M68 定时任务)。同样只绑 super_admin,理由比 config 更硬:
		// 手动触发跑的是**代码里注册的任务**,点一下 = 执行一段服务端逻辑,
		// 等于把任意代码段的执行权写进了角色配置表。
		//   run:read 单独一个 code:sys_job_run 里有 trace_id / 错误堆栈,
		//   将来要放开给运维时可以只给"看执行记录"而不给"手动触发"。
		//   没有 create / delete —— 任务本体只在代码里 cronx.Register。
		"job:list", "job:read", "job:update", "job:trigger", "job:run:read",
		// sms_template(M115 短信模板审核状态)。**只绑 super_admin**,理由同 config/job:
		// "停用某个场景的模板" = 全平台该场景的验证码**立刻发不出去**,
		// 这是平台级的止血开关,不是业务域操作。
		//   list / read —— 看状态与流转历史
		//   update —— 停用 / 恢复(**两个动作共用一个码**,理由见 router.go 的注释)
		//   sync —— 手动触发一轮同步(与定时任务同一个入口)
		//   没有 create / delete —— 模板本体不在平台里(那是阿里云的模板号)。
		"sms_template:list", "sms_template:read", "sms_template:update", "sms_template:sync",
		// audit
		"audit:operation:list", "audit:operation:read",
		"audit:data:list", "audit:data:read",
		"audit:login:list", "audit:login:read",
		// 系统运行日志(2026-11-03)—— 慢 SQL / 慢接口 / 5xx / panic
		"audit:system:list", "audit:system:read",
		"audit:full-info:list", "audit:full-info:read",
		"audit:export:list", "audit:export:read", "audit:export:create",
		// §1.5 #5 P0 —— 框架级 PII 解密访问审计
		"audit:pii:list", "audit:pii:read",
		// §1.9 #7 P1 —— 短信发送审计
		"audit:sms:list", "audit:sms:read",
		// §1.5 #8 P1 —— 密钥使用审计
		"audit:crypto:list", "audit:crypto:read",
		// §1.9 #11+#12 P1 —— 站内信 + 通知中心
		"inbox:list", "inbox:read", "inbox:mark_read",
		// guide(指南/帮助中心)—— 框架保留的示例业务模块
		"guide:list", "guide:read", "guide:create", "guide:update", "guide:delete",
		// 语音能力(voicex,2026-11-06 移植)—— 音色克隆 + TTS
		//   voice_clone:refresh     —— 拉 DashScope query_voice 同步克隆状态
		//   voice_clone:remote_list —— 拉远端音色列表(不写库,对账用)
		//   tts:convert             —— 文本合成(流式回音频 / 落 OSS)
		// ⚠️ 这些是平台级能力(同 wxtemplate:配错/滥用影响全平台),只绑超管线。
		"voice_clone:list", "voice_clone:read", "voice_clone:create",
		"voice_clone:update", "voice_clone:delete", "voice_clone:refresh", "voice_clone:remote_list",
		"tts:convert",
		"tts_voice:list", "tts_voice:read", "tts_voice:create", "tts_voice:update", "tts_voice:delete",
		"dashboard:read",
		// 服务器资源监控(CPU/内存/磁盘,2026-11-03)—— **只绑平台运营线**:
		//   服务器内部信息(goroutine 数 / Go 版本 / 磁盘水位)不广发给业务角色,
		//   与 config:* / job:* 同一条"平台级止血开关不广发"的纪律。
		//   super_admin 直通全量(mergeConcat(resources, ...)),无需在这里单绑。
		"dashboard:server",
		"wxtemplate:list", "wxtemplate:read", "wxtemplate:create", "wxtemplate:update", "wxtemplate:delete", "wxtemplate:send",
		// === 通用审批系统(M83,设计文档 §8.1)===
		// ⚠️ **这里 8 个 API 码(另加下面按钮级 `button.flow.*` 3 个 = §8.1 点名的 11 个)** ——
		// §8.1 是逐个点名写死的,不多不少。
		// M86 起路由已挂(`/rapid-admin/v1/system/flows` 5 条 + `/rapid-admin/v1/system/approvals` 7 条),
		// 每个用到的码都在下面的列表里 —— **少一个启动就 fail-fast**
		// (`CheckResourceConsistency` 检查(1):rbac() 引用的码必须在 sys_resource 里)。
		// ⚠️ **检查(3)对"seed 有码但没有 route 引用"只打 info 日志**,所以多留码不报错,
		// 但那是给"先落码后挂路由"用的窗口期,不是常态。
		//   flow:*     —— 流程模板配置。写只有 super_admin(§8.4 D 组),
		//                  理由同下面的 config:* / job:*:配错一个模板 = 全平台
		//                  每条业务记录都走错审批链。
		//   approval:* —— 审批单本身。⚠️ **广发是安全的**:没有 box=all(§8.2),
		//                  拿了这个码也只能看到自己的 todo/mine/done 三盒。
		//                  ⚠️ 一旦把 box=all 加回来,这里广发立刻变成越权(§12 第 9 条)。
		"flow:list", "flow:read", "flow:create", "flow:update", "flow:delete",
		"approval:list", "approval:read", "approval:create",

		// === 动态业务集合(M151,业务表单设计器)===
		//
		// ⚠️ **权限码是四段** —— 数据面(某个业务表的记录 CRUD)的码是三段
		// `dyn:<集合编码>:<动作>`,集合编码由用户起(规则见 internal/pkg/dynx)。
		// 设计面若也用三段(`dyn:collection:list`),用户建一个叫 "collection"
		// 的集合就会**撞码**:那个集合的 `dyn:collection:list` 与设计面的
		// "列出所有集合"是同一个字符串 ⇒ 建表的人顺手拿到了读全部集合定义的能力。
		// ⇒ 设计面一律四段(`dyn:design:...`),与三段的数据面**形状不同**,撞不了
		// (集合编码里不可能出现 ":")。
		//
		//	design:collection:* —— 集合本身(建表 / 改表 / 删表)
		//	design:field:*      —— 字段(加列 / 改列 / 删列)
		//	design:type:list    —— 字段类型清单(前端下拉框的数据源,不许前端硬编码)
		//
		// ⚠️ **数据面的码不在这里** —— 那些是**运行期生成**的
		// (集合创建时登记,见 internal/service/dyn_resource.go)。
		// seed 是编译期的东西,写死就等于"业务表必须提前设计好"—— 那就不是设计器了。
		"dyn:design:collection:list", "dyn:design:collection:read",
		"dyn:design:collection:create", "dyn:design:collection:update",
		"dyn:design:collection:delete",
		"dyn:design:field:list", "dyn:design:field:create",
		"dyn:design:field:update", "dyn:design:field:delete",
		"dyn:design:type:list",

		// === 按钮级权限(2026-10-18 12/15 上线对齐) ===
		//   命名空间:button.<module>.<action>
		//   标准动作:add / edit / delete / export / import / batch / audit / approve / reject / reset / toggle / reset-password / change-password / preview / download / print / archive / restore
		//   每个业务菜单按 4-7 个常用动作配置,覆盖率达 100%
		// user / role / resource / dept / dict / config
		"button.user.add", "button.user.edit", "button.user.delete", "button.user.export", "button.user.import", "button.user.reset-password", "button.user.unlock", "button.user.toggle",
		"button.role.add", "button.role.edit", "button.role.delete", "button.role.assign", "button.role.export",
		"button.resource.add", "button.resource.edit", "button.resource.delete", "button.resource.add-child",
		"button.dept.add", "button.dept.edit", "button.dept.delete", "button.dept.import", "button.dept.export",
		"button.dict.add", "button.dict.edit", "button.dict.delete", "button.dict.import", "button.dict.export",
		// job(M68)。**没有 add / delete** —— 任务不能建也不能删,只能改运行态覆盖。
		//   toggle(启停)与 edit(改频率/名称)拆成两个:改频率会影响生产调度,
		//   停用只是止血,处置力度不同,分开授权才不会"为了止血顺手把频率改了"。
		//   trigger 独立于 edit:能改配置不代表有权立刻跑一遍。
		"button.job.edit", "button.job.toggle", "button.job.trigger", "button.job.reset-default", "button.job.view-run",
		// sms_template(M115)。**没有 edit** —— 这张表上没有"编辑",只有四个动作。
		//   disable / enable —— 止血与恢复。**两个都放在 update 那个 API 码后面**,
		//                     但按钮级分开授权:能停不能恢复的角色不成立
		//                     (见 router.go 里"停用/恢复共用一个 update 码"的说明)。
		//   sync  —— 手动拉一轮服务商的真实审核状态
		//   history —— 看流转历史(谁在什么时候停用的)
		"button.sms_template.disable", "button.sms_template.enable", "button.sms_template.sync", "button.sms_template.history",
		// audit
		"button.audit.operation.export", "button.audit.data.export", "button.audit.login.export",
		// flow(M83)。⚠️ **只有配置页这三个**。
		// 审批单上的 6 个动作按钮(通过/驳回/打回/取消/提交/重提)**一律不设权限码** ——
		// 它们的可见性由**数据**决定(我手里有没有 pending task),不是 RBAC(§8.1)。
		// 权限码表达不了"这张单该谁审",硬绑只会出现"有权限但没待办的人看见按钮"
		// 或者反过来。**这三个是例外**,因为配置页里权限驱动确实是对的:
		// 有 flow:update 不代表能删。
		"button.flow.add", "button.flow.edit", "button.flow.delete",
		// auth
		//   me / logout:前端启动必调 /auth/me 拿用户+资源,退出必调 /auth/logout
		//   change-password / reset-password / update-profile:改自己的密码/资料
		//   switch-identity:切身份
		//   account-sms:自助短信验证码(2026-10-28 M104)——
		//   发/校验发给**自己账号登记手机号**的验证码。手机号由服务端从
		//   sys_user 取,前端只有脱敏号(138****5678)发不出去 —— 没有它,
		//   切高敏身份和个人中心改密的短信二次验证都是死的。
		//   自助能力,所以跟 me 同组。
		//   sessions / revoke-sessions:会话与设备管理(SRS §2.2.1 #6)——
		//   看自己的活跃设备 + 强制下线**别的**设备。自助能力,所以跟 me 同组。
		//   bind-phone:换绑手机号(2026-10-29 C5)——
		//   发验证码给**新号** + 用新号上的码完成换绑。手机号由前端传
		//   (要换绑的是新号,服务端手上只有旧号)。自助能力,所以跟 me 同组。
		//   全部对应 router.go 的 rbac("auth", ...);少登记一个,
		//   启动时 CheckResourceConsistency 会 fail-fast(线上表现为 403)
		"auth:me", "auth:logout", "auth:change-password", "auth:reset-password",
		"auth:switch-identity", "auth:update-profile", "auth:account-sms",
		"auth:bind-phone",
		"auth:sessions", "auth:revoke-sessions",
	}
}

// codeActionLabel 末段 action 转中文 label
//   - button.user.export  → "导出"
//   - button.user.batch-import → "批量导入"
//   - 缺省 → 原 action 全大写
//   - 用于 sys_resource.name 字段(资源管理页表格列展示)
func codeActionLabel(action string) string {
	m := map[string]string{
		"add": "新增", "edit": "编辑", "delete": "删除", "export": "导出", "import": "导入",
		"batch": "批量操作", "batch-import": "批量导入", "audit": "审核", "approve": "通过", "reject": "拒绝",
		"reset": "重置", "toggle": "启用/停用", "reset-password": "重置密码", "unlock": "解锁",
		"change-password": "修改密码", "preview": "预览", "download": "下载", "print": "打印",
		"account-sms": "账号短信验证",
		"bind-phone":  "换绑手机号",
		"archive":     "归档", "restore": "恢复", "publish": "发布", "withdraw": "撤回", "submit": "提交",
		"review": "审核", "assign": "分配", "accept": "受理", "reject-handover": "拒绝移交",
		"handover": "移交", "start-work": "开始处理", "reopen": "重新打开", "close": "关闭",
		"add-child": "添加子级", "activate": "激活", "cancel": "取消",
		// M115 短信模板审核状态
		"disable": "停用", "enable": "启用", "sync": "同步", "history": "流转历史",
	}
	if v, ok := m[action]; ok {
		return v
	}
	return action
}

// MenuDef 菜单资源定义(Phase 5 M2:菜单 seed 补完)
//   - Code:  菜单唯一 code,菜单与 api 资源共享 code 唯一索引,所以菜单 code 用 menu.* 命名空间
//   - Path:  前端路由路径(供 Sidebar 跳转 + selectedKey 匹配)
//   - Icon:  图标 key,前端 Sidebar.iconMap 用 (后端不存 ReactNode,只存字符串)
//   - Sort:  排序,数值小在前
//   - ParentCode: 父菜单 code,空 = 顶级
//   - Name:  中文名,前端 i18n 失败时 fallback
type MenuDef struct {
	Code       string
	Name       string
	Path       string
	Icon       string
	Sort       int
	ParentCode string
}

// menuList 后台侧边栏菜单 —— 与 web/src/router/routes.tsx 一一对应
//
// 设计原则:
//  1. 顶级菜单 5 个:dashboard + system + file + audit + approvalMgmt
//     (开源口径:业务菜单由业务方接入自己的模块后在这里登记)
//  2. 二级菜单与 routes.tsx 的二级 path 对齐
//  3. 菜单 code 用 menu.* 命名空间,与 api code 区分(避免 seed unique 冲突)
//  4. 菜单权限由"父菜单上挂的 api 资源"间接决定:M5/M6 样板通过
//     sys_role_resource 把 menu.* 资源绑到角色(用现有 bindingRules 改造)
//  5. super_admin 通过 RBAC 中间件直通,无需挂 menu.*
//  6. **分组菜单的父节点必须和子菜单绑给同一批角色** —— 前端 buildMenuTree
//     遇到"父节点不在可访问集合里"的子节点会把它**当顶级**
//     (web/src/utils/resourceTree.ts:40)⇒ 子菜单平级浮出去,分组形同虚设。
func menuList() []MenuDef {
	return []MenuDef{
		// 顶级
		{Code: "menu.dashboard", Name: "工作台", Path: "/", Icon: "dashboard", Sort: 0},

		// 系统管理
		{Code: "menu.system", Name: "系统管理", Path: "/system", Icon: "system", Sort: 10},
		{Code: "menu.system.user", Name: "用户管理", Path: "/system/user", Icon: "user", Sort: 11, ParentCode: "menu.system"},
		{Code: "menu.system.role", Name: "角色管理", Path: "/system/role", Icon: "role", Sort: 12, ParentCode: "menu.system"},
		{Code: "menu.system.permission", Name: "权限资源", Path: "/system/permission", Icon: "permission", Sort: 13, ParentCode: "menu.system"},
		{Code: "menu.system.department", Name: "部门管理", Path: "/system/department", Icon: "dept", Sort: 14, ParentCode: "menu.system"},
		{Code: "menu.system.dict", Name: "数据字典", Path: "/system/dict", Icon: "dict", Sort: 15, ParentCode: "menu.system"},
		{Code: "menu.system.config", Name: "系统配置", Path: "/system/config", Icon: "config", Sort: 16, ParentCode: "menu.system"},
		{Code: "menu.system.job", Name: "定时任务", Path: "/system/job", Icon: "job", Sort: 17, ParentCode: "menu.system"},
		// M115 短信模板审核状态。Sort 18 接在「定时任务」后面 ——
		//   这两件事是**同一条线**上的东西(定时任务负责同步状态,这个页负责看/止血),
		//   挨着放比塞到别处好找。
		// ⚠️ **Path 必须和 web/src/router/routes.tsx 的 path 对上** ——
		//   两侧不一致的症状是「菜单点了跳 404」(见 web/src/router/__tests__/menu-route-parity.test.ts)。
		// ⚠️ **Icon 'sms' 必须在 web/src/constants/resourceIcons.ts 登记**,
		//   否则侧边栏这一项渲染成没有图标的裸文字。
		{Code: "menu.system.smsTemplate", Name: "短信模板", Path: "/system/sms-template", Icon: "sms", Sort: 18, ParentCode: "menu.system"},
		// M151 业务表单设计器(动态业务集合)。Sort 19 接在「短信模板」后面。
		//
		// ⚠️ **Path 必须和 web/src/router/routes.tsx 的 path 对上** ——
		//   两侧不一致的症状是「菜单点了跳 404」(见 web/src/router/__tests__/menu-route-parity.test.ts)。
		// ⚠️ **Icon 'table' 必须在 web/src/constants/resourceIcons.ts 登记**,
		//   否则侧边栏这一项渲染成没有图标的裸文字。
		//
		// ⚠️ **运行时生成的集合菜单(`menu.dyn.<code>`)不在这里** ——
		//   那是集合创建时登记的(见 internal/service/dyn_resource.go),
		//   父节点是运行期建的「自定义业务」(menu.dyn,path 为空 = 纯分组)。
		//   它的 path 是 `/dyn/<集合码>`,前端对应路由是 `/dyn/:code`
		//   (见 web/src/router/routes.tsx)。
		{Code: "menu.system.dyn", Name: "业务表单设计器", Path: "/system/dyn", Icon: "table", Sort: 19, ParentCode: "menu.system"},

		// 皮肤管理(P3 编辑器 MVP,2026-11-05)。Sort 20 接在设计器后面 ——
		//   ⚠️ **Path 必须和 web/src/router/routes.tsx 的 path 对上**(system/skin),
		//   不一致的症状是「菜单点了跳 404」(menu-route-parity.test.ts 在守)。
		//   ⚠️ **Icon 'skin' 必须在 web/src/constants/resourceIcons.ts 登记**(已登记 BgColorsOutlined)。
		{Code: "menu.system.skin", Name: "皮肤管理", Path: "/system/skin", Icon: "skin", Sort: 20, ParentCode: "menu.system"},

		// 文件(M82 前端页)。Sort 35 排在「房源」和「数字资产」中间:
		// 它是后面几个业务域都要用的底座(传附件/存封面),但自己不是业务域。
		// ⚠️ **Path 必须和 web/src/router/routes.tsx 里的 path 对上** ——
		// Sidebar 用的是后端这个 Path,两边不一致的症状是「菜单点了跳 404」。
		{Code: "menu.file", Name: "文件管理", Path: "/file", Icon: "file", Sort: 35},
		{Code: "menu.file.list", Name: "文件列表", Path: "/file/list", Icon: "file", Sort: 36, ParentCode: "menu.file"},
		{Code: "menu.file.folder", Name: "文件夹管理", Path: "/file/folder", Icon: "file", Sort: 37, ParentCode: "menu.file"},

		// 审计
		{Code: "menu.audit", Name: "审计中心", Path: "/audit", Icon: "audit", Sort: 90},
		{Code: "menu.audit.operation", Name: "操作日志", Path: "/audit/operation", Icon: "audit", Sort: 91, ParentCode: "menu.audit"},
		{Code: "menu.audit.dataChange", Name: "数据变更", Path: "/audit/data-change", Icon: "audit", Sort: 92, ParentCode: "menu.audit"},
		{Code: "menu.audit.login", Name: "登录日志", Path: "/audit/login", Icon: "audit", Sort: 93, ParentCode: "menu.audit"},
		{Code: "menu.audit.system", Name: "系统日志", Path: "/audit/system", Icon: "audit", Sort: 94, ParentCode: "menu.audit"},

		// 审批(M83)。⚠️ **Sort 从 100 起,不能复用 92/93** —— 上面审计中心
		// 已经占了。Sort 不唯一,撞号不会报错,菜单顺序会随机漂;V1.0 就是这么分配的。
		//
		// ⚠️ **2026-11-01 重组:两个顶级菜单收进「审批管理」**(用户拍板:
		//   「我的审批、审批流程两个菜单应该放到审批管理的菜单里面」)。原来它们是
		//   两个平级顶级菜单,侧边栏里孤零零两行,和「系统管理」那种带子项的分组
		//   不在一个层级上。
		//
		// ⚠️ **子菜单的 path 一个都不能改,父菜单也不能占用子菜单的 path** ——
		//   三个理由,每一个都足以单独否掉"顺手改成 /approval/mine"这种改法:
		//   ① 站内信 / 邮件里的审批深链是**后端拼的** `/approval/{id}`
		//      (internal/service/approval_inbox.go:195、approval_scan.go:265/300)
		//      —— 改了就等于全平台已经发出去的待办链接全部 404;
		//   ② 侧边栏菜单项的 key 是 `r.path || r.code`(Sidebar.tsx:62),
		//      两个菜单撞 path = 撞 key = antd Menu 里 key 重复;
		//   ③ 「我的审批」详情页是 `/approval/:id`,父菜单若也叫 `/approval`,
		//      父子抢同一条路由。
		//   ⇒ 父菜单自己占一个 path(`/approval-manage`),点它跳回「我的审批」。
		//     这与「系统管理 /system → /system/user」那 10 个分组同一套规矩。
		{Code: "menu.approvalMgmt", Name: "审批管理", Path: "/approval-manage", Icon: "approvalMgmt", Sort: 100},
		// ⚠️ **todo/mine/done 三个列表是同一个页面的三个 Tab,不是三个路由**(§9.1)。
		//   拆成三个二级菜单等于三份一模一样的路由配置,以后改一处漏两处。
		{Code: "menu.approval", Name: "我的审批", Path: "/approval", Icon: "approval", Sort: 101, ParentCode: "menu.approvalMgmt"},
		{Code: "menu.flow", Name: "审批流程", Path: "/flow", Icon: "flow", Sort: 102, ParentCode: "menu.approvalMgmt"},

		// ⚠️ **menu.apiDocs(API 文档)2026-11-03 起不再是菜单**(2026-11-01 §8 G5 曾作为
		//   顶级菜单进过 seed)。入口改在**右上角帮助按钮的下拉菜单**里
		//   (web/src/components/layout/helpMenu.ts):「API 文档」跳 /api-docs,
		//   「使用教程」新标签页开 /docs(GitBook)。页面本身仍是独立路由
		//   (web/src/router/routes.tsx 的 'api-docs'),不走侧边栏 ——
		//   文档入口属于"随处可得的帮助",不属于"导航结构"。
		//   api_docs_menu_test.go 钉住了"不许回到菜单"这个决定。
	}
}

// driverHolder 由 main 注入当前数据库驱动
var driverHolder = "mysql"

// SetDriver 由 cmd/seed / cmd/server 在调用 Run 之前注入驱动名
// seed 据此决定是否跳过 casbin SavePolicy(SQLite dev 模式)
func SetDriver(d string) { driverHolder = d }

func currentDriver() string { return driverHolder }

// authBaseCodes 每个登录角色都该有的"自服务"资源码
//
// ⚠️ **提到包级是为了让守门网能直接遍历它**(`seed_test.go`
// `TestRun_BindsAuthBaseToAllRoles`):守门网拿它逐个查"在不在资源池里"、
// "每个角色绑上没有"。清单留在函数里的话,守门网只能自己抄一份 ——
// 而抄的那份和真的这份**一起**少一个码时,测试会全绿。
//
//	auth:*                —— 改自己 / 切身份 / 看自己的设备 / 换绑手机号
//	inbox:*               —— 站内信(handler 按 caller.UserID 收窄,不是越权)
//	dict:list             —— 字典只读(业务页的下拉全靠它)
//	resource:accessible   —— 拉"我能访问哪些资源"(前端菜单/按钮全靠它)
//	dashboard:read        —— 打开工作台(2026-11-01,见下面那段说明)
//
// 完整说明见 Run() 里那段注释。
var authBaseCodes = []string{
	"auth:me", "auth:logout",
	"auth:update-profile", "auth:change-password", "auth:reset-password",
	"auth:switch-identity",
	// 自助短信验证码(M104)。所有后台角色都要能给自己发/验验证码 ——
	// 它是"切高敏身份"和"个人中心改密"的必经之路,不是某类角色的特权。
	"auth:account-sms",
	// 换绑手机号(C5)。同理 —— 换自己的号是自助能力。
	"auth:bind-phone",
	"auth:sessions", "auth:revoke-sessions",
	// 站内信(自服务)—— 见上面的说明
	"inbox:list", "inbox:read", "inbox:mark_read",
	// 字典只读 —— 见上面的说明(M93)
	"dict:list",
	// ⚠️ 看板只读(2026-11-01)—— 与 dict:list / resource:accessible 同一类。
	//
	//	「工作台」菜单(menu.dashboard)绑给**每一个**登录角色(commonMenus),
	//	而看板的 4 个接口此前只有 **4 个角色**有 dashboard:read
	//	(super_admin / role_readonly / platform_ops_lead / platform_ops ——
	//	role_readonly 是被 `filterByRead` 按 `:read` 后缀顺手捞进来的)
	//	⇒ **9/13 的角色点开工作台 = 4 个接口全 403**,
	//	页面一片空白 + 一串红 toast。
	//	这就是 M149 那一类问题的另一个面:**菜单看得见、接口调不了**。
	//
	//	这里给"能打开工作台"开闸是安全的,因为看板的**内容**已经按域权限过滤了
	//	(服务端算,见 internal/service/dashboard_visibility.go):
	//	没有 owner:list 的角色,响应里**压根没有 owner_total 这个字段**。
	//	⇒ 接口开闸 ≠ 数据放开。
	"dashboard:read",
	// ⚠️ M147:拉自己的权限清单 —— 与 dict:list 同一类(M93 补 dict:list 就是同一个洞)。
	//
	//   `/system/resource/accessible` 返的是"**当前 caller** 能访问哪些资源",
	//   前端拿它渲染侧边栏菜单和按钮。**不给的话**:该角色登录后菜单和按钮**全空**
	//   (前端不报错 —— 只是"什么都没有"),而它自己却没有任何办法恢复。
	//
	//   为什么之前没人发现:通配策略行(`role:X, global, *, *, allow`)让**所有角色**
	//   都能调这个接口。M147 把通配行删掉之后,这个洞才会露出来 ——
	//   所以必须**同一个 commit** 一起补,否则修洞修出新洞。
	//
	//   ⚠️ 只给 accessible(读自己的),**不给** resource:list / resource:tree /
	//   resource:create / resource:update / resource:delete ——
	//   那是"权限管理页"的能力,归用户管理员 / 超管。
	"resource:accessible",
}

// AuthBaseResourceCodes 返回"每个登录角色都该有的自服务资源码"清单(**副本**)
//
// ⚠️ **为什么要导出给 service 层**(M147):
//
//	service 层在**建角色**和**分配权限**两处必须保证这份清单不会被漏绑 /
//	被抹掉 —— 少了 auth:me,那个身份切过去之后前端连"我是谁"都拿不到,
//	整个后台是空的;少了 resource:accessible,菜单和按钮全空。
//
//	清单留在包里的话,service 只能自己抄一份 —— 而抄的那份和真的这份
//	**一起**少一个码时,谁都不会发现(测试也照抄的是同一份)。
//
//	⚠️ 返回**副本**:调用方拿到的是切片,直接改会污染包级变量。
func AuthBaseResourceCodes() []string {
	out := make([]string, len(authBaseCodes))
	copy(out, authBaseCodes)
	return out
}

// Run 执行业务初始化(供 cmd/seed 与 scripts/seed 调用)
//   - db:  *gorm.DB
//   - casbinModelPath:  Casbin model 文件路径
//   - superAdminPassword:  超级管理员初始密码(若为空则使用 "Admin@2026")
func Run(db *gorm.DB, casbinModelPath, superAdminPassword string) error {
	if superAdminPassword == "" {
		superAdminPassword = "Admin@2026"
	}
	roles := roleList()
	resources := resourceList()

	// 1. 角色
	//    ⚠️ **整批放一个事务里**(2026-10-31,M138):
	//      老实现 13 个角色 = **13 次提交**,SQLite 每次提交都要 fsync
	//      (Windows 上一次几毫秒)⇒ 光这一步 66ms。
	//      事务里跑完 = 1 次提交。**语义一条没变**:还是逐条 upsert,
	//      只是提交时机从"每条一次"变成"整批一次"。
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, r := range roles {
			var existing model.SysRole
			if err := tx.Where("code = ?", r.Code).First(&existing).Error; err == nil {
				// 2026-10-17:seed 时强制把内置角色的 IsActive 写回 true,
				//   防止运营误关后 seed 永远不会再打开(原实现只更新 Name/Sort)
				//   + 同步 Type / IsBuiltin(新字段)
				existing.Name = r.Name
				existing.Sort = r.Level
				existing.Type = r.Type
				existing.IsBuiltin = true
				existing.IsActive = true
				if err := tx.Save(&existing).Error; err != nil {
					return fmt.Errorf("save role %s: %w", r.Code, err)
				}
			} else {
				if err := tx.Create(&model.SysRole{
					BaseModel: model.BaseModel{ID: sonyflake.Next()},
					Code:      r.Code,
					Name:      r.Name,
					Type:      r.Type,
					Sort:      r.Level,
					IsBuiltin: true,
					IsActive:  true,
				}).Error; err != nil {
					return fmt.Errorf("create role %s: %w", r.Code, err)
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Println("[seed] roles upserted:", len(roles))

	// 2. api + button 资源
	//   - 命名空间:button.* → type=button;其余 → type=api
	//   - 命名(name):button.user.export → "用户导出" / api 保持原 code
	//   - 2026-10-18 起:细粒度按钮权限,供前端 <Button perm="button.X"> 控制显隐
	//    ⚠️ **整批放一个事务里**(2026-10-31,M138):
	//      稳态下这块只有 343 次 SELECT(不写库)⇒ 已经是 16ms;
	//      但**首次建库**是 343 次 Create = 343 次提交 ⇒ 那一次要好几秒。
	//      放进事务里 = 1 次提交。
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, code := range resources {
			var existing model.SysResource
			if err := tx.Where("code = ?", code).First(&existing).Error; err == nil {
				continue
			}
			var t string
			var name string
			if strings.HasPrefix(code, "button.") {
				t = "button"
				// 末段取中文 label,例如 button.user.export → "导出"
				parts := strings.Split(code, ".")
				name = codeActionLabel(parts[len(parts)-1])
			} else {
				t = "api"
				name = code
			}
			if err := tx.Create(&model.SysResource{
				BaseModel: model.BaseModel{ID: sonyflake.Next()},
				Code:      code,
				Name:      name,
				Type:      t,
				IsActive:  true,
			}).Error; err != nil {
				return fmt.Errorf("create resource %s: %w", code, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Println("[seed] api+button resources upserted:", len(resources))

	// 2.5 菜单资源(Phase 5 M2)
	//   - 写入 sys_resource (type=menu),code 用 menu.* 命名空间
	//   - 第一次写入需要 2 遍:第一遍写所有 menu 拿 ID,第二遍回填 parent_id
	//   - 已存在的 code 走 update path/icon/sort/parent_id/name(允许运营调整)
	menuByCode := make(map[string]*model.SysResource, len(menuList()))
	//    ⚠️ **整批放一个事务里**(2026-10-31,M138):
	//      47 个菜单逐条 Save ⇒ **47 次提交** ⇒ 实测 **184ms**
	//      (每次提交 SQLite 要 fsync,Windows 上一次几毫秒)。
	//      这是 migrate 里第二大的热点。事务里跑完 = 1 次提交。
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, m := range menuList() {
			var existing model.SysResource
			if err := tx.Where("code = ?", m.Code).First(&existing).Error; err == nil {
				existing.Name = m.Name
				existing.Path = m.Path
				existing.Icon = m.Icon
				existing.Sort = m.Sort
				existing.Type = "menu"
				existing.IsActive = true
				if err := tx.Save(&existing).Error; err != nil {
					return fmt.Errorf("update menu %s: %w", m.Code, err)
				}
				menuByCode[m.Code] = &existing
				continue
			}
			created := &model.SysResource{
				BaseModel: model.BaseModel{ID: sonyflake.Next()},
				Code:      m.Code,
				Name:      m.Name,
				Path:      m.Path,
				Icon:      m.Icon,
				Sort:      m.Sort,
				Type:      "menu",
				IsActive:  true,
			}
			if err := tx.Create(created).Error; err != nil {
				return fmt.Errorf("create menu %s: %w", m.Code, err)
			}
			menuByCode[m.Code] = created
		}
		// 回填 parent_id(顶级菜单 parent_id=0)
		for _, m := range menuList() {
			if m.ParentCode == "" {
				continue
			}
			parent, ok := menuByCode[m.ParentCode]
			if !ok {
				return fmt.Errorf("menu %s 父菜单 %s 不存在", m.Code, m.ParentCode)
			}
			child := menuByCode[m.Code]
			if child.ParentID != parent.ID {
				child.ParentID = parent.ID
				if err := tx.Save(child).Error; err != nil {
					return fmt.Errorf("set parent for %s: %w", m.Code, err)
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Println("[seed] menus upserted:", len(menuList()))

	// 3. 角色-资源 关联
	//   - 任何 admin 角色都自带 auth:* 自服务接口
	//     me / logout  —— 前端启动与退出的通用调用,缺了直接白屏 / 退不出去
	//     update-profile / change-password / reset-password —— 改自己
	//     switch-identity —— 切身份
	//     sessions / revoke-sessions —— 看自己的活跃设备 + 强制下线别的设备
	//   - 任何 admin 角色都自带 inbox:* 三个码(M3)
	//     list / read / mark_read —— 站内信是**自服务**的:handler 把每一条
	//     查询都按 caller.UserID 收窄(handler/v1/inbox.go),所以"能调 inbox:list"
	//     只等于"能看**自己**的收件箱",不构成越权。
	//     ⚠️ **不给的后果**:除了超管 nobody 看得到自己的站内信 —— 而站内信
	//     是审批通知(M3)唯一的落地处,不给就等于这条通知链在普通账号上断了。
	//   - 任何登录角色都自带 dict:list(只读字典)—— 2026-10-26 M93 补
	//     字典是"代码 enum 的中央仓库",业务页的下拉 / 标签全靠它渲染
	//     (web/src/hooks/useDict.ts → GET /system/dicts)。
	//     ⚠️ **不给的后果(这是 M93 修的真 bug)**:`dict:list` 此前**只绑给了
	//       super_admin 和 user_admin**(user_admin 靠 `filter(resources, "dict:")`
	//       顺带拿到),**10 个角色一个都没有** —— 于是物业工作人员打开工单页,
	//       状态下拉**全空**,而且每开一页弹一次"没有权限"(403 toast)。
	//       更拧巴的是:只读员**反而有**这个码(filterByRead 按 ":list" 后缀捞到了),
	//       真正天天用下拉的 10 个角色没有。
	//       这就是"按后缀猜权限"和"按用途声明权限"打架的现场。
	//     **只给 list,不给 create/update/delete** —— 字典是只读契约,
	//       写通道仍然只有用户管理员 / 超管(字典管理页的编辑按钮)。
	//   - super_admin 已经包含全部 resources,无需重复加
	//   - 菜单资源按业务域挂:domain 顶级 + 全部二级
	authBase := authBaseCodes
	// 菜单 code 集(供 bindingRules 复用)
	menuCodes := make([]string, 0, len(menuList()))
	for _, m := range menuList() {
		menuCodes = append(menuCodes, m.Code)
	}
	// 公共菜单:任何登录用户都能看 dashboard
	commonMenus := filter(menuCodes, "menu.dashboard")

	// === 审批(M83,设计文档 §8.4 的 A/B/C/D 四组)===
	//
	// ⚠️ **为什么抽成变量而不是把码抄进每个角色的字面量里**:
	// 四个组之间有**包含关系**(B = A + approval:create,D = C + flow:写 + button.flow.*),
	// 抄 N 遍必然漂移 —— 而漂移了**没人知道**:`assignRoleResources` 对查不到的
	// 角色码是静默 `return`(§8.6 末),少抄一个组的症状是「该角色看不到审批页」,
	// 没有报错、没有日志。
	approvalView := filter(resources, "approval:list", "approval:read") // A 组:看审批
	approvalEdit := filter(resources, "approval:create")                // B 组增量:发起
	// C 组(flow:list/read + menu.flow)与 D 组(flow:create/update/delete + button.flow.*)
	// 不在这儿声明:开源内置角色里只有 super_admin 拿到,走的是 bindingRules 里的
	// 全量绑定 —— 再单独声明一遍就是同一份绑定的两个来源,将来删资源容易只删一处。
	// ⚠️ **「我的审批」必须和它的父菜单「审批管理」成对发** ——
	//   前端 buildMenuTree 遇到"父节点不在可访问集合里"的子节点,会把它**当顶级**
	//   (web/src/utils/resourceTree.ts:40)⇒ 只发子菜单的话,「我的审批」会平级
	//   浮到侧边栏最外层,而「审批管理」这一组谁也看不见 —— **而且不报错**。
	//   「我的审批」每个角色都有 ⇒ 父菜单也每个角色都有。
	//   (TestApprovalMenuParentIsBound 钉住这条,免得将来加角色时漏掉父菜单。)
	approvalMenus := exactMenus(menuCodes, "menu.approval", "menu.approvalMgmt")

	// mergeConcat 工具:把多个 []string 拼成一个新 slice(避免 append 多 variadic 嵌套)
	mergeConcat := func(slices ...[]string) []string {
		total := 0
		for _, s := range slices {
			total += len(s)
		}
		out := make([]string, 0, total)
		for _, s := range slices {
			out = append(out, s...)
		}
		return out
	}

	bindingRules := map[string][]string{
		// 开源口径:3 个内置角色 × 框架资源。
		// super_admin 直通全量(mergeConcat(resources, menuCodes));
		// platform_ops 框架读写(config / job / sms_template / flow 写通道仍归超管 ——
		//   "平台级止血开关不广发"的纪律不变);
		// readonly 框架只读。
		model.RoleSuperAdmin: mergeConcat(resources, menuCodes),
		model.RolePlatformOps: mergeConcat(
			filter(resources,
				"user:", "role:", "resource:", "dept:", "dict:",
				"file:", "folder:", "guide:",
				// 审计只放各页面实际调用的 list/read 对;
				// export:create(写通道)与没配菜单的审计页(pii / sms / crypto / full-info)不绑
				"audit:operation:", "audit:data:", "audit:login:", "audit:system:",
				"wxtemplate:", "inbox:",
				"dyn:design:", "dashboard:",
				// 审批:看单 + 发起;流程配置(flow:list/read/create/update/delete +
				// button.flow.*)只归超管 —— 配错一个模板 = 全平台每条业务记录都走错审批链。
				"approval:",
				"button.user.", "button.role.", "button.resource.", "button.dept.",
				"button.dict.", "button.audit.",
			),
			// 服务器资源监控 —— 平台运营线的"运维眼睛"
			[]string{"dashboard:server"},
			authBase, commonMenus,
			userAdminSystemMenus(menuCodes),
			filter(menuCodes, "menu.audit", "menu.file"),
			// 审批 B 组(§8.4 说明 2):能发起/重提,配不了流程
			approvalView, approvalEdit, approvalMenus,
		),
		model.RoleReadOnly: mergeConcat(
			filterByRead(resources),
			authBase, commonMenus,
			// filterByRead 命中 file:list / file:read,所以只读员**能列文件也能看详情**,
			// 但没有 file:delete / file:upload —— 页面里对应按钮会自动不渲染。
			exactMenus(menuCodes, "menu.file", "menu.file.list", "menu.file.folder"),
			filter(menuCodes, "menu.audit"),
			// 审批 A 组(§8.4)。⚠️ **不给 flowMenus** —— 只读员没有 flow:* 菜单入口,
			// 所以「我的审批」里能看单、看进度,但点不进流程配置页。
			approvalView, approvalMenus,
		),
		// ⚠️ **role_super_admin 不在这里出现** —— 它上面那条
		// mergeConcat(resources, menuCodes) 已经把全部 api + 全部菜单给了,
		// D 组(flow:create/update/delete + button.flow.*)自然包含在内。
		// 再写一遍等于给同一个角色两份来源,将来删资源容易只删一处。
	}
	for role, codes := range bindingRules {
		assignRoleResources(db, role, codes)
	}

	// ⚠️ M147:自服务能力必须覆盖**每一个**角色,不只内置的那 13 个。
	//
	//   bindingRules 里写死的是内置角色;业务上自建的角色
	//   (is_builtin=false,走「角色管理」页建的)不在里面 ⇒ 它们拿不到
	//   auth:me / resource:accessible / dict:list 这些"每个登录角色都该有"的能力。
	//
	//   M147 把通配策略行删掉之后,这件事从"看不出来"变成"整个角色不可用"
	//   (切到那个身份 → /auth/me 就 403 → 前端拿不到用户信息 → 后台全空)。
	//   ⇒ 这段把自服务码补到**所有**角色上(只补缺,不删已有)。
	//
	//   ⚠️ 这一段**只管存量**(跑 migrate 的那一刻库里已有的角色);
	//   **运行期新建**的角色靠 service 层建角色时直接绑(见
	//   internal/service/role_impl.go 的 bindAuthBaseResources)——
	//   两边都要,只做一边就会留下时间窗。
	var allRoleCodes []string
	if err := db.Model(&model.SysRole{}).Where("is_active = ?", true).Pluck("code", &allRoleCodes).Error; err == nil {
		for _, rc := range allRoleCodes {
			assignRoleResources(db, rc, authBaseCodes)
		}
	}
	fmt.Println("[seed] role-resource bindings upserted")

	// 3.1 菜单↔接口绑定一致性守门网(§1.1 #17 第 ② 步,M170,2026-11-02)
	//     绑定矩阵定完形后立即体检:「菜单给了、接口没给」= 点进去 403 ——
	//     M93 / M154 / dashboard / #18 四次同类洞全是靠人偶然发现的,从此启动期 fail-fast。
	//     任何一角色绑了 menuRequiredAPIs 里的菜单却缺必调码 ⇒ Run() 返回错误,migrate/启动失败。
	if err := CheckMenuAPIWiring(db); err != nil {
		return err
	}

	// 4. 超级管理员账号
	//    存储格式:bcrypt(SHA-256("rapid-admin::" + 明文))
	//    与前端契约一致(web/src/utils/crypto.ts preHashPassword 固定 salt='rapid-admin')
	var superAdmin model.SysUser
	if err := db.Where("username = ?", "admin").First(&superAdmin).Error; err != nil {
		h := sha256.Sum256([]byte("rapid-admin::" + superAdminPassword))
		pwd, _ := auth.HashPassword(hex.EncodeToString(h[:]))
		superAdminID := sonyflake.Next()
		if err := db.Create(&model.SysUser{
			BaseModel:     model.BaseModel{ID: superAdminID},
			Username:      "admin",
			PasswordHash:  pwd,
			Nickname:      "超级管理员",
			IsActive:      true,
			MustChangePwd: true,
			RealName:      "超级管理员",
		}).Error; err != nil {
			return fmt.Errorf("create super admin: %w", err)
		}
		// 4.1 默认身份(super_admin,全平台)
		if err := db.Create(&model.SysUserIdentity{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			UserID:    superAdminID,
			RoleCode:  model.RoleSuperAdmin,
			RoleName:  "超级管理员",
			IsActive:  true,
			IsPrimary: true,
			ValidFrom: model.Time{Time: time.Now()},
		}).Error; err != nil {
			return fmt.Errorf("create super admin identity: %w", err)
		}
		fmt.Println("[seed] super admin created (username=admin, default pwd=" + superAdminPassword + ")")
	}

	// 5. Casbin policy 同步
	//
	//    ⚠️ 2026-10-31(M146a):**SQLite 也不再跳过了**。
	//    原来跳过是因为 gorm-adapter 的 SavePolicy 在 SQLite 下会卡住
	//    (transaction + dbresolver clause 不兼容),但跳过的代价是
	//    "dev 上除超管外所有角色一律 403" ⇒ 切身份必然失败。
	//    现在绕开 SavePolicy 直接写行,两种驱动策略内容一致。
	//    详见 syncCasbinPolicies 的注释。
	if err := syncCasbinPolicies(db, casbinModelPath); err != nil {
		return err
	}

	// 6. 字典 seed(2026-10-18 新增)
	//    16 P0 字典,任何 enum 落地必须先在这里 seed,否则前端 useDict 拿到空数组
	if err := seedAllDicts(db); err != nil {
		return fmt.Errorf("seed dicts: %w", err)
	}

	// 7. 字典完整性校验(必做)
	//    mustDicts 里任何 groupCode 缺 seed 时阻断,迫使开发者在加新 enum 时同步加字典
	if err := SeedDictGuard(db); err != nil {
		return err
	}

	// 8. 平台配置 seed(2026-10-18 新增,配置中心落地)
	//    17 个 key:aliyun.sms 9 + aliyun.oss 8
	//    业务级(monitor / business.*)走 P1+ 阶段,本期不 seed
	//    基础设施类(mysql/redis/jwt/crypto)仍走 yaml,严禁上 sys_config
	//    文档:docs/配置项清单.md
	if err := seedAllSysConfigs(db); err != nil {
		return fmt.Errorf("seed sys configs: %w", err)
	}

	// 9. 部门 seed(2026-10-19 新增,概念梳理§1.2)
	//     - 一级 7 个,对应 SRS §2.1 + §6.2 业务域
	//     - 二级 18 个,按业务细化(运营可继续往下加)
	//     - upsert by code:已存在则更新 name/sort/is_active;不存在则建 + 回填 parent_id
	//     - 不强制 seed:运营可在"部门管理"页手工建/改/删;seed 提供的只是首启最小集
	if err := seedDepartments(db); err != nil {
		return fmt.Errorf("seed departments: %w", err)
	}

	// 10. 审批案例模板 seed
	//     请假 / 报销 / 采购(会签+或签混合)三条,**默认停用** ——
	//     是给「审批流程配置」页当起手案例的,不是可用配置;
	//     运营把规则改成自己的组织后再启用。规则全用 dept_leader
	//     (不依赖角色/人员运行期数据),upsert 不碰 is_active 与关卡。
	if err := seedApprovalCaseTemplates(db); err != nil {
		return fmt.Errorf("seed approval case templates: %w", err)
	}

	fmt.Println("[seed] done")
	return nil
}

// syncCasbinPolicies 同步 casbin policy 到 casbin_rule
//
//	sub 命名空间:所有角色策略加 role: 前缀(Phase 1.1.5)
//
// # ⚠️ 2026-10-31(M146a):SQLite **不再跳过**
//
//	原来这里对 SQLite 直接 return,理由是"adapter SavePolicy 已知 hang"。
//	**那个卡死是真的** —— gorm-adapter 的 SavePolicy 在一个事务里做
//	truncate,而 truncate 走的是**另一个连接**:SQLite 上两边抢同一把写锁,
//	事务这边等锁、连接那边等事务结束 ⇒ 互等,永远不返回。
//
//	但**跳过的代价更大**:casbin_rule 表在 dev 上永远是空的 ⇒
//	**除超管(RBAC 中间件直通)外的所有角色一律 403**。
//	表现为"切身份"必然失败 —— 切到任何非超管身份,`/auth/me` 都 403,
//	前端再把 403 当成"登录态失效"清掉会话 ⇒ **切完就退出**(M146b 修的就是这条)。
//
//	所以现在**绕开 SavePolicy**:直接把策略行写进 casbin_rule,再 Reload。
//	两种驱动写出来的**策略内容完全一致**(buildRolePolicies 是唯一来源),
//	dev 与生产不再有行为差异。
func syncCasbinPolicies(db *gorm.DB, casbinModelPath string) error {
	if err := casbinx.Init(db, casbinModelPath); err != nil {
		return fmt.Errorf("init casbin: %w", err)
	}
	policies := buildRolePolicies(db)
	if currentDriver() == "sqlite" {
		// ⚠️ SQLite:不能走 SavePolicy(见上面的死锁说明),直接写行。
		if err := writePoliciesDirect(db, policies); err != nil {
			return fmt.Errorf("write casbin policy(sqlite): %w", err)
		}
		// 让**本进程**的 enforcer 立刻看到新策略(服务重启后也会 LoadPolicy)。
		//
		// ⚠️ **失败只 warn 不报错** —— enforcer 是进程级单例
		//   (casbinx.Init 是进程级单例,首次成功后绑定那个库),它绑的是**第一次** Init 的那个库。
		//   一行式进程(cmd/migrate / cmd/seed)里两者是同一个库,Reload 一定成功;
		//   但 seed 被当库函数反复调用时(单测),enforcer 可能绑在别的库上,
		//   Reload 就会失败 —— 那不是"策略没写进去"(行已经写进 db 了),
		//   只是**本进程**的内存副本旧了。策略的真相源是库里的行,
		//   下一个进程启动时 LoadPolicy 自然读到新的。
		if err := casbinx.Reload(); err != nil {
			fmt.Printf("[seed] warn: casbin policy reload failed(%v); rows are written, next process will load them\n", err)
		}
		fmt.Printf("[seed] casbin policies synced (sqlite direct, %d rules)\n", len(policies))
		return nil
	}
	enf := casbinx.Enforcer()
	enf.ClearPolicy()
	for _, p := range policies {
		_, _ = enf.AddPolicy(toAny(p)...)
	}
	if err := enf.SavePolicy(); err != nil {
		return fmt.Errorf("save casbin policy: %w", err)
	}
	fmt.Printf("[seed] casbin policies synced (%d rules)\n", len(policies))
	return nil
}

// buildRolePolicies 算出「每个角色能访问哪些资源」的策略行。
//
// ⚠️ **这是策略内容的唯一来源** —— SQLite 与 MySQL 两条路都调它,
// 所以「dev 与生产策略不一致」这类洞没有藏身处。
//
// 行格式(与 casbin 的 p = sub, dom, obj, act, eft 对齐):
//
//	role:<role_code>, global, <resource_code>, *, allow
//
// # ⚠️ M147:这里**不再**有「每角色一条通配行」
//
// 老实现给每个角色加一条 `role:<code>, global, *, *, allow`。
// casbin 的 keyMatch2(r.obj, "*") 恒真 ⇒ **任何角色都能访问任何接口**,
// 「角色-资源绑定矩阵」在强制执行层是一张空壳(前端按矩阵显示菜单,
// 后端却谁都能调)。安全上这是**全站越权**。
//
// 老实现的注释说这条通配行是"历史设计,见 docs/后台权限模型设计V1.0.md §6.2" ——
// **§6.2 全文没有这条**(`git log -S` 显示它比 `role:` 前缀改造还早,
// 是绑定矩阵成型之前的**启动期占位**,矩阵建好后没人回来删)。
//
// ⚠️ 删掉它必须**配套**改另外两处,否则从"人人能访问"变成"人人访问不了":
//  1. `configs/casbin_rbac.conf` 的 matcher:keyMatch2 → keyMatch
//     (资源码里的 ":" 是字面量,不是 URL 路径参数)
//  2. `internal/api/middleware/rbac.go` decideAccess:请求 obj 传**完整资源码**
//
// ⚠️ **角色名 / 资源名一次性查完**(老实现每个绑定查两遍库,
// 400 多个绑定 = 800+ 次查询)。查不到的绑定**跳过**,与老实现同语义。
func buildRolePolicies(db *gorm.DB) [][]string {
	// 容量按"绑定数"量级给(一千多行);老实现的 len(roleList())*4 是按
	// "每角色一条通配 + 少量绑定"估的,现在通配行没了,这个估计偏小两个数量级。
	policies := make([][]string, 0, 1024)

	var roles []model.SysRole
	if err := db.Find(&roles).Error; err == nil {
		var resources []model.SysResource
		// ⚠️ M147-f:**只取启用中的资源** —— 停用的资源不该继续放行。
		//
		// 口径必须与三处一致,否则"运行时"与"重建"会算出两套权限:
		//   ① 这里(整体重建)
		//   ② service 层的运行时同步(roleResourceCodes)
		//   ③ authService.AccessibleResources(前端菜单/按钮的来源)
		//
		// ③ 本来就过滤 is_active ⇒ 停用一个资源的**界面表现**是"菜单消失、
		// 接口还在"(死开关)。这里补上过滤,三处才是一套口径。
		if err := db.Where("is_active = ?", true).Find(&resources).Error; err == nil {
			roleCodeByID := make(map[uint64]string, len(roles))
			for _, r := range roles {
				roleCodeByID[r.ID] = r.Code
			}
			resCodeByID := make(map[uint64]string, len(resources))
			for _, r := range resources {
				resCodeByID[r.ID] = r.Code
			}
			var bindings []model.SysRoleResource
			db.Find(&bindings)
			for _, b := range bindings {
				roleCode, resCode := roleCodeByID[b.RoleID], resCodeByID[b.ResourceID]
				if roleCode == "" || resCode == "" {
					continue
				}
				// role: 前缀 = Casbin sub 类型命名空间(role: / user: / grant:)
				// r.Code 本身是 role_house_admin(下划线前缀),叠加后是 role:role_house_admin 双前缀
				// 丑但正确,见 docs/后台权限模型设计V1.0.md §6.2 + Phase 1.1.5 决策
				//
				// ⚠️ M147-f:行格式走 casbinx.PolicyRow(唯一来源)——
				//   运行时同步(casbinx.SyncRolePolicies)也调它,两处不许各抄一份。
				policies = append(policies, casbinx.PolicyRow(roleCode, resCode))
			}
		}
	}
	return policies
}

// toAny 把一行策略转成 casbin 的 AddPolicy 要的 []interface{}。
//
// ⚠️ casbin v3 的 AddPolicy 签名是 `params ...interface{}`,不是 ...string ——
// 传 []string 编译不过。
func toAny(row []string) []interface{} {
	out := make([]interface{}, len(row))
	for i, v := range row {
		out[i] = v
	}
	return out
}

// writePoliciesDirect 直接把策略行写进 casbin_rule(绕开 adapter 的 SavePolicy)。
//
// ⚠️ **行结构必须与 gorm-adapter 的 CasbinRule 完全一致**
// (id / ptype / v0..v5)—— 直接用 adapter 导出的那个结构体,
// 不自己抄一份:抄的那份一旦列名对不上,adapter 的 LoadPolicy 读出来
// 全是空串,策略"有 N 条"却一条都不生效,而且**不报错**。
//
// ⚠️ **先删后写** —— 旧行留着的话,角色被删 / 资源被改名之后,
// 老策略还在库里继续放行(这是"改了权限却不生效"的经典来源)。
func writePoliciesDirect(db *gorm.DB, policies [][]string) error {
	// ⚠️ 表可能还不存在 —— adapter 是**懒创建**的(第一次 AddPolicy/SavePolicy
	//   才建表),而这里是直接写行,不经过 adapter。
	//   AutoMigrate 用的是 adapter 自己导出的结构体 ⇒ 建出来的列名与 adapter
	//   读的时候一模一样(自己抄一份结构体就有可能对不上,见上面的注释)。
	if err := db.AutoMigrate(&gormadapter.CasbinRule{}); err != nil {
		return err
	}
	// ⚠️ 表名用 adapter 自己的(CasbinRule.TableName() = "casbin_rule"),
	//   不要自己写字符串 —— 换了 adapter 的默认表名就会写错表。
	if err := db.Exec("DELETE FROM " + gormadapter.CasbinRule{}.TableName()).Error; err != nil {
		return err
	}
	rows := make([]gormadapter.CasbinRule, 0, len(policies))
	for _, p := range policies {
		r := gormadapter.CasbinRule{Ptype: "p"}
		vals := []*string{&r.V0, &r.V1, &r.V2, &r.V3, &r.V4, &r.V5}
		for i := 0; i < len(vals) && i < len(p); i++ {
			*vals[i] = p[i]
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		return nil
	}
	return db.CreateInBatches(&rows, 200).Error
}

// assignRoleResources 给角色绑资源(**只补缺的,不删已有的**)。
//
// ⚠️ **语义(改批量化之后一条都不能变)**:
//
//	· 角色查不到 ⇒ **静默 return**(不建角色、不报错)—— 见 approval_seed_test.go
//	  的 TestUnknownRoleCode_BindsNothing:这条"静默"是刻意设计的,
//	  真正的防线是 bindingRules 里不许写不存在的角色码;
//	· 资源码查不到 ⇒ 跳过;
//	· 已经绑过了 ⇒ 跳过(**只补缺**);
//	· 唯一索引(uk_rr)冲突 ⇒ 忽略。
//
// ⚠️ **为什么改成批量(2026-10-31,M138)**:老实现对**每个 (角色, 码) 对查两次库**
//
//	(先按 code 查 resource_id,再 count 有没有这条绑定)——
//	13 个角色 × 约 200 个码 ≈ **5200 次 SQL**,SQLite 上要 1.1 秒。
//	而这一步在 **F5 的前置任务里每次都要跑** ⇒ "启动慢"里有整整一秒在这儿。
//	⇒ 现在:资源 id **一次全查**建 map(1 次),该角色现有绑定**一次查完**建 set
//	(1 次),差集在内存里算,最后批量插入。
//	查询次数:5200 → 13×2 + 13 次批量插入。
func assignRoleResources(db *gorm.DB, role string, codes []string) {
	var roleID uint64
	if err := db.Table("sys_role").Where("code = ?", role).Select("id").Scan(&roleID).Error; err != nil || roleID == 0 {
		return
	}

	// ⚠️ **资源 id 一次全查**(老实现是一个 code 查一次):
	//   用 Table() 而不是模型查询 ⇒ 不带软删除 scope —— 与老实现一致
	//   (软删掉的资源在老实现里也能被绑上,这里保持同样的行为)。
	var resRows []struct {
		ID   uint64
		Code string
	}
	if err := db.Table("sys_resource").Select("id, code").Find(&resRows).Error; err != nil {
		return
	}
	resIDByCode := make(map[string]uint64, len(resRows))
	for _, r := range resRows {
		resIDByCode[r.Code] = r.ID
	}

	// ⚠️ **现有绑定一次查完**:
	//   ⚠️ 只按 role_id 查(不带 resource_id 条件)—— 老实现的 count 也是
	//     "role_id + resource_id"命中即跳过,**不区分 project_id**。
	//     而 uk_rr 是 (role_id, resource_id, project_id) 三列唯一,
	//     SQLite 里 NULL 不参与唯一性判定 ⇒ 老库里可能有 project_id=NULL 的行,
	//     只按两列去重才能避免插出**重复绑定**(这正是老实现 count 的语义)。
	var bound []uint64
	if err := db.Table("sys_role_resource").Where("role_id = ?", roleID).Pluck("resource_id", &bound).Error; err != nil {
		return
	}
	boundSet := make(map[uint64]struct{}, len(bound))
	for _, id := range bound {
		boundSet[id] = struct{}{}
	}

	missing := make([]model.SysRoleResource, 0, len(codes))
	// ⚠️ seen 防的是"同一个资源码在 codes 里出现两次"⇒ 一次插入里撞唯一索引
	seen := make(map[uint64]struct{}, len(codes))
	for _, code := range codes {
		resID, ok := resIDByCode[code]
		if !ok || resID == 0 {
			continue
		}
		if _, dup := seen[resID]; dup {
			continue
		}
		seen[resID] = struct{}{}
		if _, has := boundSet[resID]; has {
			continue
		}
		// 注:SysRoleResource schema 中无 role_code 字段,只有 role_id
		missing = append(missing, model.SysRoleResource{
			BaseModel:  model.BaseModel{ID: sonyflake.Next()},
			RoleID:     roleID,
			ResourceID: resID,
		})
	}
	if len(missing) == 0 {
		return
	}
	// ⚠️ **批量插入 + ON CONFLICT DO NOTHING**:
	//   老实现靠"逐条插入,撞唯一索引就 continue"兜底;批量必须显式声明
	//   冲突忽略,否则**整批**会失败(SQLite / MySQL / PG 都支持这个语义)。
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(missing, 200).Error; err != nil {
		// 批量失败不吞:退回逐条,保持"冲突忽略、其它错误照样报"的语义
		for _, m := range missing {
			if err := db.Create(&m).Error; err != nil {
				continue
			}
		}
	}
}

// userAdminSystemMenus 平台运营员在 system 域里该拿的菜单。
//
// ⚠️ **必须是精确匹配,不能是前缀匹配** ——
// filter 是**前缀**匹配:`"menu.system"` 会把 `menu.system.*` 全家桶
// (config / job / smsTemplate / dyn)一起捞进来。
//
// 而平台运营员的 API 码里**一个都没有**:
// config:* —— 只有 super_admin(另有 readonly 的 list/read)
// job:* —— 只有 super_admin(另有 readonly 的 list/read)
// sms_template:* —— 只有 super_admin(另有 readonly 的 list/read)
// ⇒ 那三个菜单点进去必然是 **403**(菜单可见 ≠ 接口有权),
// 而"菜单可见性"与"接口权限"是**两份独立的授权列表**。
//
// ⚠️ **menu.system.dyn 必须显式列出** ——
// 它以前是被 "menu.system" 这个前缀**顺带**捞进来的,
// 换成精确匹配后不列就会掉(平台运营员有 dyn:design:*)。
//
// ⚠️ **为什么提成独立函数** ——
// "菜单可见性"与"接口权限"是两条独立路径,只测最终状态的话,
// 把这个函数改回前缀匹配**照样是绿的**。
// ⇒ 提成函数是为了能**单独**测它。
func userAdminSystemMenus(menuCodes []string) []string {
	return exactMenus(menuCodes,
		"menu.system", "menu.system.user", "menu.system.role", "menu.system.permission",
		"menu.system.department", "menu.system.dict",
		// M151:业务表单设计器(设计面 API 码 dyn:design:* 已给)
		"menu.system.dyn",
	)
}

// exactMenus **精确**挑菜单码,不做前缀匹配。
//
// ⚠️ 存在的理由:filter() 是前缀匹配,而菜单码是**点分**的,`"menu.file"` 会把
// `menu.file.folder` 一起捞进来。用 filter 写"只给列表页"的意图,实际拿到的是
// 三层全给 —— 然后物业经理点进文件夹页,每个请求 403。
// (这个 bug 是 TestRun_BindsFileMenuByPermission 抓出来的,不是推演出来的。)
func exactMenus(list []string, codes ...string) []string {
	want := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		want[c] = struct{}{}
	}
	var out []string
	for _, s := range list {
		if _, ok := want[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

func filter(list []string, prefixes ...string) []string {
	var out []string
	for _, s := range list {
		for _, p := range prefixes {
			if p == s || (len(s) >= len(p) && s[:len(p)] == p) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func filterByRead(list []string) []string {
	var out []string
	for _, s := range list {
		if hasSuffix(s, ":read") || hasSuffix(s, ":list") || hasSuffix(s, ":tree") {
			out = append(out, s)
		}
	}
	return out
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// ============================================================================
// 字典 seed(2026-10-18 新增,用于前端 useDict(groupCode) 数据源)
//
// 约束(必读):
//  1. 任何 enum 落地前,必须先在 mustDicts + seedAllDicts 双向登记
//  2. 前端 web/src/types/dict.ts 的 DictGroupCode 镜像 mustDicts
//     增删 groupCode 必须 server + web 同步提交
//  3. SeedDictGuard 在 Run() 末尾被调用,缺字典时 panic
//  4. label 字段填中文(前端 useDict 直接展示),多语言方案留作 P1
// ============================================================================

// mustDicts 必须 seed 的字典组(契约)
//
// 新增 enum 流程:
//  1. 在此添加 groupCode
//  2. 在 seedAllDicts 添加种子项
//  3. 同步 web/src/types/dict.ts DictGroupCode
//  4. 跑 `go run ./cmd/seed` 验证不 panic
//  5. 提交(同一 PR 内 server + web 同步)
//
// 2026-10-18:从 9 组扩到 15 组(原 9 P0 + 6 P1 支撑业务页 STATUS_MAP 迁移)
//
//	2026-10-18 加:issue_type / issue_priority / owner_binding_status /
//	              audit_op_status / notice_priority / config_change_status(扩 expired)
//
// 2026-10-18 晚加 P2 14 组(本 commit):支撑 Phase 6 业务页
//
//	common_status / house_status / contract_status / delivery_status /
//	delivery_issue_status / delivery_stage / activity_status /
//	activity_signup_status / feedback_status / guide_status /
//	device_online_status / device_bind_status / wxtemplate_status
//	+ config_value_type 补 cert_pem / private_key_pem
var mustDicts = []string{
	// 用户/角色
	"user_status", "role_type",
	// 审计
	"audit_op_status",
	// 系统日志级别(2026-11-03 审计中心「系统日志」页)—— info / warn / error
	"sys_log_level",
	// 2026-10-30 审计中心"数据变更"页新增 —— operation 是代码 enum(1/2/3),
	// 按"字典 = 代码 enum 契约中央仓库"的规矩必须登记,不能在前端硬编码文案
	"audit_change_operation",
	// 配置中心
	"config_change_status", "config_value_type", "config_effective_type",
	// P2 通用
	"common_status",
	// P2 微信模板
	"wxtemplate_status",
	// M115 短信模板审核状态(与 pkg/smstpl.Status 的 7 个字面量一一对应)
	"sms_template_status",
	// 资源(权限点)类型 — 2026-10-18 资源管理页接入
	"resource_type",
	// 配置 UI Label — 2026-10-18 系统配置 Collapse 表单 label 来源
	"config_ui_label",
}

// dictGroupLabels 字典分组的显示名(中文)
//
// 2026-10-18: 字典管理页"业务类型"下拉之前只显示英文 groupCode
//   - 现在按下面这张表渲染 label(中文)
//   - 新增 group 时必须同步登记(单一来源,跟 mustDicts 一起维护)
//   - 缺登记时 fallback 到 groupCode 本身
//   - i18n 多语言留作 P1
var dictGroupLabels = map[string]string{
	// 用户/角色
	"user_status": "用户状态",
	"role_type":   "角色类型",
	// 审计
	"audit_op_status":        "审计操作状态",
	"sys_log_level":          "系统日志级别",
	"audit_change_operation": "数据变更操作类型",
	// 配置中心
	"config_change_status":  "配置变更状态",
	"config_value_type":     "配置值类型",
	"config_effective_type": "配置生效方式",
	// P2 通用
	"common_status": "通用状态",
	// P2 微信模板
	"wxtemplate_status": "微信模板状态",
	// M115 短信模板审核状态
	"sms_template_status": "短信模板审核状态",
	// 资源(权限点)类型
	"resource_type": "资源类型",
	// 配置 UI Label(2026-10-18 系统配置 Collapse 表单用)
	"config_ui_label": "配置 UI Label",
}

// DictGroupLabel 2026-10-18: 取字典分组的显示名,缺登记时 fallback 到 groupCode
func DictGroupLabel(code string) string {
	if v, ok := dictGroupLabels[code]; ok {
		return v
	}
	return code
}

// DictGroupLabels 2026-10-18: 暴露整张 label 表(给 ListDictGroups 用,按 groupCode 升序保证稳定)
func DictGroupLabels() []DictGroupKV {
	out := make([]DictGroupKV, 0, len(dictGroupLabels))
	for code, label := range dictGroupLabels {
		out = append(out, DictGroupKV{Code: code, Label: label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// DictGroupKV 2026-10-18: groupCode + 显示名,给 /dicts/groups 端点用
type DictGroupKV struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}
type dictItem struct {
	GroupCode   string
	ItemKey     string
	ItemValue   string
	Label       string
	Sort        int
	Description string // 'color:green # 备注' 形式,前端 useDict.getColor 解析 'color:xxx' 部分
}

// seedAllDicts 16 P0 字典种子
//   - 幂等:已存在的更新,缺失的创建
//   - 与 mustDicts 一一对应(groupCode 子集)
//   - description 字段约定:'color:<antd-tag-color> # 备注',前端 getColor 解析
func seedAllDicts(db *gorm.DB) error {
	items := []dictItem{
		// user_status
		{"user_status", "active", "active", "正常", 1, "color:green"},
		{"user_status", "disabled", "disabled", "停用", 2, "color:default"},
		{"user_status", "locked", "locked", "锁定", 3, "color:red"},

		// role_type
		{"role_type", "platform", "platform", "平台级", 1, "color:purple"},
		{"role_type", "business", "business", "业务级", 2, "color:cyan"},

		// audit_op_status(2026-10-18 加,业务页 audit/operation.tsx STATUS_MAP 1/0)
		{"audit_op_status", "success", "1", "成功", 1, "color:green"},
		{"audit_op_status", "fail", "0", "失败", 2, "color:red"},

		// audit_change_operation(2026-10-30 加,审计中心"数据变更"页 operation 1/2/3)
		// ⚠️ 跟 model.SysDataChangeLog.Operation 的取值一一对应,改后端要同 commit 改这里
		{"audit_change_operation", "create", "1", "新增", 1, "color:green"},
		{"audit_change_operation", "update", "2", "修改", 2, "color:blue"},
		{"audit_change_operation", "delete", "3", "删除", 3, "color:red"},

		// sys_log_level(2026-11-03 加,审计中心「系统日志」页 level 列)
		// ⚠️ 跟 sys_system_log.level 的取值一一对应(中间件钩子只写 warn / error;
		// info 留给未来低噪音来源,列表默认不筛 = 全看)
		{"sys_log_level", "info", "info", "信息", 1, "color:blue"},
		{"sys_log_level", "warn", "warn", "警告", 2, "color:orange"},
		{"sys_log_level", "error", "error", "错误", 3, "color:red"},

		// config_change_status
		{"config_change_status", "pending", "pending", "待审核", 1, "color:orange"},
		{"config_change_status", "approved", "approved", "已通过", 2, "color:green"},
		{"config_change_status", "rejected", "rejected", "已驳回", 3, "color:red"},
		{"config_change_status", "activated", "activated", "已生效", 4, "color:blue"},
		{"config_change_status", "expired", "expired", "已过期", 5, "color:default"},

		// config_value_type
		{"config_value_type", "string", "string", "字符串", 1, "color:default"},
		{"config_value_type", "int", "int", "整数", 2, "color:blue"},
		{"config_value_type", "bool", "bool", "布尔", 3, "color:purple"},
		{"config_value_type", "json", "json", "JSON", 4, "color:cyan"},
		{"config_value_type", "secret", "secret", "密钥", 5, "color:red"},
		// 2026-10-18 加:enum ConfigTypeCertPEM/ConfigTypePrivateKey
		{"config_value_type", "cert_pem", "cert_pem", "证书 PEM", 6, "color:gold"},
		{"config_value_type", "private_key_pem", "private_key_pem", "私钥 PEM", 7, "color:red"},

		// config_effective_type
		{"config_effective_type", "immediate", "immediate", "立即生效", 1, "color:green"},
		{"config_effective_type", "next_restart", "next_restart", "下次重启", 2, "color:orange"},
		{"config_effective_type", "scheduled", "scheduled", "定时生效", 3, "color:blue"},

		// ==== P2 groupCodes(2026-10-18 晚) ====

		// common_status(通用 0 停用 1 启用 2 归档,对应 model.BizStatus)
		{"common_status", "disabled", "0", "停用", 1, "color:default"},
		{"common_status", "enabled", "1", "启用", 2, "color:green"},
		{"common_status", "archived", "2", "归档", 3, "color:gold"},

		// wxtemplate_status(微信模板:active/disabled/draft)
		{"wxtemplate_status", "active", "active", "启用", 1, "color:green"},
		{"wxtemplate_status", "disabled", "disabled", "停用", 2, "color:default"},
		{"wxtemplate_status", "draft", "draft", "草稿", 3, "color:gold"},
		// sms_template_status(M115)—— 与 pkg/smstpl.Status 的 7 个字面量一一对应。
		//
		// ⚠️ **cancelled 不许合并进 rejected** —— 服务商侧确实有"取消审核"这一档,
		// 运维看到"已驳回"会去改模板内容,看到"审核已取消"要办的是另一件事
		// (多半是重新送审)。合并成一个状态,历史里就分不清这两种事了。
		//
		// ⚠️ **unknown 不是"未知/异常"** —— 它是"还没同步过"。守卫对它是**放行**的
		// (不确定 ≠ 确定不可用),所以页面上它是灰色,不是红色。
		{"sms_template_status", "unknown", "unknown", "未同步", 1, "color:default # 还没同步过服务商状态"},
		{"sms_template_status", "pending", "pending", "待审核", 2, "color:orange # 已登记,还没拿到服务商的结论"},
		{"sms_template_status", "reviewing", "reviewing", "审核中", 3, "color:blue # 服务商正在审"},
		{"sms_template_status", "approved", "approved", "已通过", 4, "color:green # 审核通过,可以发"},
		{"sms_template_status", "rejected", "rejected", "已驳回", 5, "color:red # 服务商驳回,发不出去"},
		{"sms_template_status", "cancelled", "cancelled", "审核已取消", 6, "color:purple # 服务商撤回了这次审核,需重新送审"},
		{"sms_template_status", "disabled", "disabled", "已停用", 7, "color:default # 人工停用,服务商同步不会自动恢复"},
		// resource_type(资源类型:menu/button/api — 2026-10-18 资源管理页接入)
		{"resource_type", "menu", "menu", "菜单", 1, "color:blue"},
		{"resource_type", "button", "button", "按钮", 2, "color:cyan"},
		{"resource_type", "api", "api", "接口", 3, "color:purple"},

		// ==== 配置 UI Label(2026-10-18) ====
		// 系统配置 Collapse 表单用 — 单一来源,字典管理页可改 label 联动 UI
		//
		// 2026-10-18 收窄:form 只装 value 1 个字段,config_name / is_active 移到 Card title
		// 只读展示(纯显示文案,seed 决定;17 个 key 多为必需,关掉即系统挂)
		{"config_ui_label", "config.value", "config.value", "当前值", 1, "color:default"},
	}
	// ⚠️ **一次查全表 + 只写真正变了的**(2026-10-31,M138)
	//
	//   老实现:每条字典 **一次 SELECT + 一次 UPDATE**(125 条 = 250 次语句),
	//   而且**每次 UPDATE 都单独提交一次**(SQLite 每次提交要 fsync,
	//   Windows 上一次几毫秒)⇒ 实测 **723ms**。
	//   这段在**每次 F5 的前置 migrate 里都要跑一遍** ⇒ "启动慢"里有它。
	//
	//   新实现:
	//     1. **一次 SELECT** 把现有字典全查出来,内存里建 map;
	//     2. 内存比对,**只写真正变了的**(字段全同 ⇒ 一条 SQL 都不发);
	//     3. 新增的行**批量插入**,整批放一个事务里(1 次提交)。
	//   ⇒ **稳态(库里已是最新)= 1 次 SELECT + 0 次写**。
	//
	//   ⚠️ **`updated_at` 不再每次都跳**:老实现无条件 UPDATE ⇒ 每次 seed
	//     都刷新时间戳,于是"这条字典什么时候被改过"永远是最近一次启动的时刻。
	//     现在只在**内容真的变了**时才写 —— 内容没变却动时间戳是噪声。
	//
	//   ⚠️ **语义一条都没变**:缺 → 建(同字段同默认值);在 → 改成 seed 的值
	//     (label / sort / description / item_value 可演进,is_active 强制 true);
	//     软删的行 GORM 的 scope 自动排除 ⇒ 跟老实现一样当作"缺"(会新建一条)。
	var existing []model.SysDict
	if err := db.Find(&existing).Error; err != nil {
		return fmt.Errorf("query dicts: %w", err)
	}
	byKey := make(map[string]*model.SysDict, len(existing))
	for i := range existing {
		row := &existing[i]
		k := dictKey(row.GroupCode, row.ItemKey)
		// ⚠️ 老实现用 `First()` —— 取的是**主键最小**的那条。
		//   这里保持一致(万一历史数据里有重复的 group_code+item_key)。
		if prev, ok := byKey[k]; ok && prev.ID <= row.ID {
			continue
		}
		byKey[k] = row
	}

	var inserts []model.SysDict
	var updates []model.SysDict
	// ⚠️ **同一批 seed 里 key 重复**的处理:老实现是"第一条建、第二条改它"
	//   (最终值 = 后者)。这里必须同样:后一条覆盖前一条的字段。
	pending := make(map[string]int, len(items))
	for _, it := range items {
		k := dictKey(it.GroupCode, it.ItemKey)
		if idx, dup := pending[k]; dup {
			// 同一个 key 在这一批里出现两次 ⇒ 合并成一条(后者覆盖)
			inserts[idx].ItemValue = it.ItemValue
			inserts[idx].Label = it.Label
			inserts[idx].Sort = it.Sort
			inserts[idx].Description = it.Description
			inserts[idx].IsActive = true
			continue
		}
		row, ok := byKey[k]
		if !ok {
			inserts = append(inserts, model.SysDict{
				BaseModel:   model.BaseModel{ID: sonyflake.Next()},
				GroupCode:   it.GroupCode,
				ItemKey:     it.ItemKey,
				ItemValue:   it.ItemValue,
				Label:       it.Label,
				Sort:        it.Sort,
				Description: it.Description,
				IsActive:    true,
			})
			pending[k] = len(inserts) - 1
			continue
		}
		if row.ItemValue == it.ItemValue && row.Label == it.Label &&
			row.Sort == it.Sort && row.Description == it.Description && row.IsActive {
			// 内容完全一致 ⇒ **一条 SQL 都不发**
			continue
		}
		u := *row
		u.ItemValue = it.ItemValue
		u.Label = it.Label
		u.Sort = it.Sort
		u.Description = it.Description
		u.IsActive = true
		updates = append(updates, u)
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		if len(inserts) > 0 {
			if err := tx.CreateInBatches(&inserts, 100).Error; err != nil {
				return fmt.Errorf("create dicts: %w", err)
			}
		}
		for i := range updates {
			u := &updates[i]
			if err := tx.Model(u).Updates(map[string]any{
				"item_value":  u.ItemValue,
				"label":       u.Label,
				"sort":        u.Sort,
				"description": u.Description,
				"is_active":   true,
			}).Error; err != nil {
				return fmt.Errorf("update dict %s/%s: %w", u.GroupCode, u.ItemKey, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Println("[seed] dicts upserted:", len(items),
		"(new:", len(inserts), "changed:", len(updates), ")")
	return nil
}

// dictKey 字典唯一键(group_code + item_key)的 map key
//
//	用 \x00 分隔 —— group_code / item_key 里不可能出现 NUL,
//	所以不会像用空格或逗号分隔那样出现"拼接歧义"。
func dictKey(groupCode, itemKey string) string {
	return groupCode + "\x00" + itemKey
}

// SeedDictGuard 字典完整性校验
//   - mustDicts 中任何 group_code 缺 seed 时返回 error
//   - 任何 caller(Run / cmd/seed / 测试)收到 error 应阻断
//   - 设计目的:加新 enum 忘了 seed 字典时,启动会立即失败,迫使开发者补齐
func SeedDictGuard(db *gorm.DB) error {
	for _, gc := range mustDicts {
		var n int64
		if err := db.Model(&model.SysDict{}).
			Where("group_code = ? AND is_active = ?", gc, true).
			Count(&n).Error; err != nil {
			return fmt.Errorf("query dict guard %s: %w", gc, err)
		}
		if n == 0 {
			return fmt.Errorf(
				"❌ 字典缺失: group_code=%q 未在 sys_dict 中 seed\n"+
					"   → 请在 server/internal/seed/seed.go 的 mustDicts + seedAllDicts 双向登记\n"+
					"   → 同步 web/src/types/dict.ts 的 DictGroupCode 联合类型\n"+
					"   → 参考: docs/数据字典清单.md",
				gc,
			)
		}
	}
	return nil
}

// ============================================================================
// 平台配置 seed(2026-10-18 新增,配置中心落地)
//
// 范围(严格,严禁越界):
//   - 12/15 上线 seed 17 个 key:aliyun.sms 9 + aliyun.oss 8
//   - 业务级(monitor / business.*) P1+ 阶段再补,本期不 seed
//   - 基础设施类(mysql/redis/jwt/crypto/casbin/cors/ratelimit/audit)严禁上 sys_config
//
// 行为约定:
//   - 首次启动:全部 INSERT
//   - 二次启动:已有 key 不覆盖(避免覆盖运营改过的值),只 INSERT 新增的
//   - 强制 reset(开发用):删表 sys_config 后重跑
//
// 文档:docs/系统配置功能规范.md + docs/配置项清单.md
// ============================================================================

// SysConfigSeedItem 配置项种子定义
type SysConfigSeedItem struct {
	Group       string
	Key         string
	Name        string
	Description string
	ValueType   string
	Value       string
	IsSensitive bool
	IsRequired  bool
	MinLength   int
	MaxLength   int
	Regex       string
	EnvVar      string
	Sort        int
}

// mustSysConfigs 必须 seed 的配置项(契约,12/15 上线 17 个)
//
//	增删/修改必须同时改 docs/配置项清单.md,严禁脱节
//	增 key 流程:docs/配置项清单.md 加一行 → mustSysConfigs 加一条 → 重跑 seed
var mustSysConfigs = []SysConfigSeedItem{
	// ===== aliyun.sms (9) =====
	{Group: "aliyun.sms", Key: "aliyun.sms.provider", Name: "短信服务提供方", Description: "aliyun / console,console 用于本地 dev", ValueType: "string", Value: "aliyun", EnvVar: "SMS_PROVIDER", Sort: 1},
	{Group: "aliyun.sms", Key: "aliyun.sms.access_key", Name: "短信 AccessKey ID", Description: "阿里云 RAM AccessKey ID", ValueType: "secret", Value: "", IsSensitive: true, IsRequired: true, MinLength: 16, MaxLength: 64, EnvVar: "SMS_ACCESS_KEY", Sort: 2},
	{Group: "aliyun.sms", Key: "aliyun.sms.access_secret", Name: "短信 AccessKey Secret", Description: "阿里云 RAM AccessKey Secret", ValueType: "secret", Value: "", IsSensitive: true, IsRequired: true, MinLength: 16, MaxLength: 64, EnvVar: "SMS_ACCESS_SECRET", Sort: 3},
	{Group: "aliyun.sms", Key: "aliyun.sms.sign_name", Name: "短信签名", Description: "阿里云短信平台报备过的签名", ValueType: "string", Value: "RapidAdmin", IsRequired: true, MaxLength: 32, EnvVar: "SMS_SIGN_NAME", Sort: 4},
	{Group: "aliyun.sms", Key: "aliyun.sms.region", Name: "短信 API Region", Description: "阿里云短信服务 region", ValueType: "string", Value: "cn-hangzhou", EnvVar: "SMS_REGION", Sort: 5},
	{Group: "aliyun.sms", Key: "aliyun.sms.template.login", Name: "登录验证码模板", Description: "登录场景短信模板 code", ValueType: "string", Value: "SMS_100000000", EnvVar: "SMS_TEMPLATE_LOGIN", Sort: 10},
	{Group: "aliyun.sms", Key: "aliyun.sms.template.view_full_phone", Name: "查看完整手机号模板", Description: "查看完整手机号短信模板 code", ValueType: "string", Value: "SMS_100000000", EnvVar: "SMS_TEMPLATE_VIEW_PHONE", Sort: 11},
	{Group: "aliyun.sms", Key: "aliyun.sms.template.reset_password", Name: "重置密码模板", Description: "重置密码短信模板 code", ValueType: "string", Value: "SMS_100000000", EnvVar: "SMS_TEMPLATE_RESET_PWD", Sort: 12},
	{Group: "aliyun.sms", Key: "aliyun.sms.template.change_phone", Name: "换绑手机模板", Description: "换绑手机短信模板 code", ValueType: "string", Value: "SMS_100000000", EnvVar: "SMS_TEMPLATE_CHANGE_PHONE", Sort: 13},

	// ===== aliyun.oss (7) =====
	{Group: "aliyun.oss", Key: "aliyun.oss.provider", Name: "OSS 服务提供方", Description: "aliyun / mock,mock 用于本地 dev", ValueType: "string", Value: "aliyun", EnvVar: "OSS_PROVIDER", Sort: 20},
	{Group: "aliyun.oss", Key: "aliyun.oss.endpoint", Name: "OSS Endpoint", Description: "内网/外网 endpoint,生产用内网", ValueType: "string", Value: "oss-cn-hangzhou.aliyuncs.com", EnvVar: "OSS_ENDPOINT", Sort: 21},
	{Group: "aliyun.oss", Key: "aliyun.oss.access_key", Name: "OSS AccessKey ID", Description: "RAM AccessKey ID,推荐用子账号只授 OSS 权限", ValueType: "secret", Value: "", IsSensitive: true, IsRequired: true, MinLength: 16, MaxLength: 64, EnvVar: "OSS_ACCESS_KEY", Sort: 22},
	{Group: "aliyun.oss", Key: "aliyun.oss.access_secret", Name: "OSS AccessKey Secret", Description: "RAM AccessKey Secret", ValueType: "secret", Value: "", IsSensitive: true, IsRequired: true, MinLength: 16, MaxLength: 64, EnvVar: "OSS_ACCESS_SECRET", Sort: 23},
	{Group: "aliyun.oss", Key: "aliyun.oss.bucket", Name: "文件 bucket", Description: "**所有文件都在这一个桶里**,必须是公共读(public-read):直链是不带签名的稳定 URL", ValueType: "string", Value: "rapid-admin-prod", EnvVar: "OSS_BUCKET", Sort: 24},
	{Group: "aliyun.oss", Key: "aliyun.oss.cdn_domain", Name: "CDN 加速域名", Description: "bucket 的 CDN 加速域名", ValueType: "string", Value: "cdn.rapid-admin.cn", EnvVar: "OSS_CDN_DOMAIN", Sort: 26},
	{Group: "aliyun.oss", Key: "aliyun.oss.upload_token_ttl", Name: "STS 凭证有效期(秒)", Description: "前端直传 STS 临时凭证有效期", ValueType: "int", Value: "3600", MinLength: 60, MaxLength: 86400, EnvVar: "OSS_UPLOAD_TOKEN_TTL", Sort: 27},
}

// seedAllSysConfigs 配置项种子
//   - 幂等:已存在则跳过(不覆盖运营改过的值)
//   - 缺失则创建
//   - 12/15 上线必跑(由 Run() 第 9 步触发)
func seedAllSysConfigs(db *gorm.DB) error {
	// 先清已下线的键(必须在建新键之前:config_key 上有唯一索引,
	// 软删的行还占着键,后面再建同名键会撞唯一约束)。
	if err := pruneRetiredSysConfigs(db); err != nil {
		return err
	}
	for _, c := range mustSysConfigs {
		var existing model.SysConfig
		err := db.Where("config_key = ?", c.Key).First(&existing).Error
		if err == nil {
			// 已存在:跳过,避免覆盖运营已改的值
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("query sys_config %s: %w", c.Key, err)
		}
		// 创建
		now := model.Time{Time: time.Now()}
		cfg := &model.SysConfig{
			BaseModel:             model.BaseModel{ID: sonyflake.Next()},
			ConfigKey:             c.Key,
			ConfigGroup:           c.Group,
			ConfigName:            c.Name,
			Description:           c.Description,
			ValueType:             model.ConfigValueType(c.ValueType),
			ValueEncrypted:        c.Value, // 本期明文落库,P1+ 接 AES
			ValuePreview:          previewValue(c.Value, c.IsSensitive),
			IsSensitive:           c.IsSensitive,
			RegexValidate:         c.Regex,
			MinLength:             c.MinLength,
			MaxLength:             c.MaxLength,
			DefaultValueEncrypted: c.Value,
			IsRequired:            c.IsRequired,
			IsReadonly:            false,
			EnvVarName:            c.EnvVar,
			Sort:                  c.Sort,
			IsActive:              true,
			Version:               1,
			LastChangedAt:         &now,
		}
		if err := db.Create(cfg).Error; err != nil {
			return fmt.Errorf("create sys_config %s: %w", c.Key, err)
		}
	}
	fmt.Println("[seed] sys_configs upserted:", len(mustSysConfigs))
	return nil
}

// retiredSysConfigKeys **已下线**的配置项 key。
//
// ⚠️ 为什么要显式清理:seedAllSysConfigs 的策略是"建缺失的、不动已存在的",
// 于是下线过的键会永远留在库里 —— 配置中心照常把它们显示出来,读配置的人
// 会以为"还在生效",而服务端早就不认了(单桶模型下只有 oss.bucket 一个桶)。
//
//	只列**能证明已经下线**的 key;运营自己加的自定义 key 一律不动。
//
//	2026-10-30:文件存储从"admin/public 双桶"改成"单桶",这两个键退役。
var retiredSysConfigKeys = []string{
	"aliyun.oss.bucket_admin",
	"aliyun.oss.bucket_public",
}

// pruneRetiredSysConfigs 删掉已下线的配置项行(幂等)。
//
// ⚠️ **硬删**而不是软删:config_key 上有唯一索引,软删的行还占着键,
// 而这些键退役后不会再被重建 —— 留着一条"看不见但占坑"的行没有任何价值。
func pruneRetiredSysConfigs(db *gorm.DB) error {
	if len(retiredSysConfigKeys) == 0 {
		return nil
	}
	res := db.Unscoped().
		Where("config_key IN ?", retiredSysConfigKeys).
		Delete(&model.SysConfig{})
	if res.Error != nil {
		return fmt.Errorf("prune retired sys_config: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		fmt.Printf("[seed] retired sys_configs pruned: %d rows\n", res.RowsAffected)
	}
	return nil
}

// previewValue 生成值预览(敏感值脱敏,非敏感原样)
//   - 本期简单实现,后续接真加密后由 service.previewValue 接管
func previewValue(v string, sensitive bool) string {
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

// ============================================================================
// 部门 seed(2026-10-19 新增,概念梳理 §1.2 拍板)
//
// 设计要点:
//  1. 字段对齐:SysDepartment.code 唯一索引,name 中文,sort 排序,is_active=true
//  2. 树形:7 个一级 + 18 个二级;后续运营可在部门管理页继续往下加三级/四级
//  3. upsert 语义:
//     - code 已存在 → 更新 name/sort/is_active(parent_id 不重置,避免运营手调后被覆盖)
//     - code 不存在 → 新建(2 遍:第一遍拿到 ID,第二遍回填 parent_id)
//  4. leader_id 不 seed(部门负责人是用户管理的概念,部门 seed 阶段尚无运营数据)
//  5. 不强制:运营可删/可改;删了之后下次 seed 不会再建(由 IsActive 决定)
//     删的语义是 code 没了 → 跳过;想复活就建同名部门,seed 不会强干预
//  6. 失败回滚:任何一条 dept 失败 → 整体 return err,不让半残 seed 继续
//
// 关联文档:docs/概念梳理-用户角色部门权限v1.md §1.2
// ============================================================================

// DeptDef 部门定义
//   - Code:  唯一索引键,英文 snake_case 短码
//   - Name:  中文,前端部门管理页直接展示
//   - Sort:  排序,同级从小到大
//   - ParentCode: 父部门 code,空 = 一级
type DeptDef struct {
	Code       string
	Name       string
	Sort       int
	ParentCode string
}

// deptList 7 个一级 + 18 个二级部门
//
//	一级映射业务域(对应 11 业务角色 + 1 供应商 + 2 平台运维):
//	  - 平台运营中心   → 平台运维主管 / 平台运维员
//	  - 用户管理部     → 用户管理员
//	  - 房源管理部     → 房源管理员
//	  - 资料管理部     → 资料管理员
//	  - 信息管理部     → 信息管理员
//	  - 交付运营部     → 交付管理员 / 专业审核员 / 只读查看员
//	  - 物业管理部     → 物业经理 / 物业工作人员
//	供应商挂"合作方"子部门,可在部门管理页手工建到任意一级部门下
func deptList() []DeptDef {
	return []DeptDef{
		// 一级
		{Code: "platform_ops", Name: "平台运营中心", Sort: 10},
		{Code: "user_mgmt", Name: "用户管理部", Sort: 20},
		{Code: "house_mgmt", Name: "房源管理部", Sort: 30},
		{Code: "material_mgmt", Name: "资料管理部", Sort: 40},
		{Code: "info_mgmt", Name: "信息管理部", Sort: 50},
		{Code: "delivery_mgmt", Name: "交付运营部", Sort: 60},
		{Code: "property_mgmt", Name: "物业管理部", Sort: 70},

		// 平台运营中心 - 二级
		{Code: "platform_ops.lead", Name: "运营主管组", Sort: 11, ParentCode: "platform_ops"},
		{Code: "platform_ops.cs", Name: "客服组", Sort: 12, ParentCode: "platform_ops"},

		// 用户管理部 - 二级
		{Code: "user_mgmt.account", Name: "账号组", Sort: 21, ParentCode: "user_mgmt"},
		{Code: "user_mgmt.role", Name: "角色权限组", Sort: 22, ParentCode: "user_mgmt"},
		{Code: "user_mgmt.dept", Name: "组织架构组", Sort: 23, ParentCode: "user_mgmt"},

		// 房源管理部 - 二级
		{Code: "house_mgmt.project", Name: "项目管理组", Sort: 31, ParentCode: "house_mgmt"},
		{Code: "house_mgmt.building", Name: "楼栋管理组", Sort: 32, ParentCode: "house_mgmt"},
		{Code: "house_mgmt.layout", Name: "户型管理组", Sort: 33, ParentCode: "house_mgmt"},

		// 资料管理部 - 二级
		{Code: "material_mgmt.review", Name: "资料审核组", Sort: 41, ParentCode: "material_mgmt"},
		{Code: "material_mgmt.publish", Name: "资料发布组", Sort: 42, ParentCode: "material_mgmt"},

		// 信息管理部 - 二级
		{Code: "info_mgmt.notice", Name: "公告组", Sort: 51, ParentCode: "info_mgmt"},
		{Code: "info_mgmt.activity", Name: "活动组", Sort: 52, ParentCode: "info_mgmt"},
		{Code: "info_mgmt.asset", Name: "数字资产组", Sort: 53, ParentCode: "info_mgmt"},

		// 交付运营部 - 二级
		{Code: "delivery_mgmt.stage", Name: "阶段管理组", Sort: 61, ParentCode: "delivery_mgmt"},
		{Code: "delivery_mgmt.inspection", Name: "验房组", Sort: 62, ParentCode: "delivery_mgmt"},
		{Code: "delivery_mgmt.acceptance", Name: "交付组", Sort: 63, ParentCode: "delivery_mgmt"},

		// 物业管理部 - 二级
		{Code: "property_mgmt.manager", Name: "经理组", Sort: 71, ParentCode: "property_mgmt"},
		{Code: "property_mgmt.staff", Name: "工作人员组", Sort: 72, ParentCode: "property_mgmt"},
	}
}

// seedDepartments 部门 upsert
//
//	与 menu 同模式:先建一遍拿 ID,再回填 parent_id
//	顺序:deptList 已按 ParentCode 是否为空排好序(一级在前),所以一次遍历就够
func seedDepartments(db *gorm.DB) error {
	depts := deptList()
	deptByCode := make(map[string]*model.SysDepartment, len(depts))

	// ⚠️ **整批放一个事务里**(2026-10-31,M138):
	//   25 个部门逐条 Save ⇒ 25 次提交 ⇒ 实测 70ms。同菜单那次,一个道理。
	if err := db.Transaction(func(tx *gorm.DB) error {
		// 第一遍:按 code 查找/创建,拿 ID
		for _, d := range depts {
			var existing model.SysDepartment
			if err := tx.Where("code = ?", d.Code).First(&existing).Error; err == nil {
				// 存在 → 更新 name/sort/is_active(不动 parent_id,运营手调保护)
				existing.Name = d.Name
				existing.Sort = d.Sort
				existing.IsActive = true
				if err := tx.Save(&existing).Error; err != nil {
					return fmt.Errorf("update dept %s: %w", d.Code, err)
				}
				deptByCode[d.Code] = &existing
				continue
			}
			// 不存在 → 新建(parent_id 暂置 0,第二轮回填)
			created := &model.SysDepartment{
				BaseModel: model.BaseModel{ID: sonyflake.Next()},
				Name:      d.Name,
				Code:      d.Code,
				Sort:      d.Sort,
				IsActive:  true,
			}
			if err := tx.Create(created).Error; err != nil {
				return fmt.Errorf("create dept %s: %w", d.Code, err)
			}
			deptByCode[d.Code] = created
		}

		// 第二遍:回填 parent_id(一级 parent_id 保持 0)
		for _, d := range depts {
			if d.ParentCode == "" {
				continue
			}
			parent, ok := deptByCode[d.ParentCode]
			if !ok {
				return fmt.Errorf("dept %s 父部门 %s 不存在", d.Code, d.ParentCode)
			}
			child := deptByCode[d.Code]
			if child.ParentID != parent.ID {
				child.ParentID = parent.ID
				if err := tx.Save(child).Error; err != nil {
					return fmt.Errorf("set parent for %s: %w", d.Code, err)
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}

	fmt.Println("[seed] departments upserted:", len(depts))
	return nil
}
