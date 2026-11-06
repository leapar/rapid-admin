// ============================================================================
// 叶子菜单 → 页面加载必调接口码 最小集(M170,2026-11-02)
//
// 这张表是 `CheckMenuAPIWiring`(menu_api_guard.go)的数据源 ——
// 「角色绑了菜单,就必须绑该菜单页面**加载时必调**的 api 码」。
//
// # 口径(判宽不判严:宁可漏报,不许误报)
//
//   - **必调** = 页面挂载即发、不调就没数据:useTable 的 fetch + useEffect/Promise.all
//     挂载即发的请求(逐页对着 web/src/pages 核过,2026-11-02);
//     **按钮后面的动作不算**(那是 button 码的事,页面用 usePermission 按码隐藏按钮,
//     缺动作码页面照样能开)。
//   - **最小集**:条件触发的请求不进表 —— 例:system/dyn 页挂载后自动选中第一个
//     collection 才发 `dyn:design:field:list`,一个 collection 都没有时永不发;
//     file/list 页 `folder_id` 在 URL 里才发 getFolder。只把**无条件**发的码记进表。
//   - **占位页不进表**:owner/audit、owner/correction、house/*、asset/*、
//     material/review、material/publish、delivery/*、notice/activity、apiDocs
//     目前是 PagePlaceholder(零 API 调用)或静态页,没有可要求的码 ——
//     等它们长成真页面时**必须**回来登记(守门网对没登记的菜单不猜、不报)。
//   - dict/config 的 useDict/useConfigValue 自助拉取走 `dict:list`(authBase 全员有),
//     不逐页重复记。
//
// # 每行对着哪里核的
//
// 页面调用:web/src/pages/**(useTable fetch + 挂载即发);
// 接口码:server/internal/api/router/router.go 的 rbac(module, action) ⇒ `module:action`。
package seed

// 叶子菜单 → 页面加载必调接口码最小集。
var menuRequiredAPIs = map[string][]string{
	// 工作台:overview / trend / login-heatmap / server-metrics 都是 dashboard:read(read)或 dashboard:server(server-metrics)
	"menu.dashboard": {"dashboard:read"},

	// ── 系统管理 ────────────────────────────────────────────────
	"menu.system.user":        {"user:list"},
	"menu.system.role":        {"role:list"},
	"menu.system.permission":  {"resource:tree"},
	"menu.system.department":  {"dept:tree"},
	"menu.system.dict":        {"dict:list"}, // groups + list 两个接口都是 dict:list
	"menu.system.config":      {"config:list"},
	"menu.system.job":         {"job:list", "job:read"}, // list + runtime 挂载 Promise.all
	"menu.system.smsTemplate": {"sms_template:list"},
	// field:list 只在自动选中 collection 后才发 → 不进最小集
	"menu.system.dyn": {"dyn:design:collection:list", "dyn:design:type:list"},

	// 语音管理:登记册列表挂载即发;克隆台账在页内按需切换才拉
	// (voice_clone:list 是条件触发 → 不进最小集,同 file/list 的 folder:get 口径)
	"menu.system.voice": {"tts_voice:list"},

	// ── 文件管理 ────────────────────────────────────────────────
	// list:列表 + 配额(file:read)+ 文件夹树(folder:list)挂载即发
	"menu.file.list": {"file:list", "file:read", "folder:list"},
	// folder:列表 + 成员(listFolderUsers → folder:list)+ 详情(folder:list)
	"menu.file.folder": {"folder:list"},

	// ── 审计中心 ────────────────────────────────────────────────
	"menu.audit.operation":  {"audit:operation:list"},
	"menu.audit.dataChange": {"audit:data:list"},
	"menu.audit.login":      {"audit:login:list"},
	// 系统日志(2026-11-03):慢 SQL / 慢接口 / 5xx / panic
	"menu.audit.system": {"audit:system:list"},

	// ── 审批 ──────────────────────────────────────────────────
	"menu.approval": {"approval:list"},
	"menu.flow":     {"flow:list"},
}
