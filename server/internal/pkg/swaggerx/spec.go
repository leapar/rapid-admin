// swaggerx —— 框架级 OpenAPI 3.0 文档生成器
//
// 设计取舍(为什么不用 swag init):
//   - swag init 需要给每个 handler 加 6-10 行 @Summary/@Param/@Success 注释
//   - 项目 185+ 受保护路由,人工注释 6-10x 行 ≈ 1500 行标注 + swag 工具链依赖
//   - 我们已经从 h.Routes() 拿到 {Method, Path, Handler} + rbac tracking 拿到 {module, action}
//   - 二者结合即可自动生成 OpenAPI 3.0 spec,质量足以做"接口目录"用途
//
// 用法:
//  1. 在 main.go 里:swaggerx.Mount(h, "/swagger", swaggerx.Info{...})
//  2. 访问 http://host:port/swagger/index.html 看 UI
//  3. 访问 http://host:port/swagger/doc.json 拿原始 OpenAPI 3.0
//
// 后续可逐步给关键 handler 加注释,让 swag init 输出更详细的 spec,
// 那个文档在 docs/ 目录,本文件生成的是"自动兜底"版。
package swaggerx

import (
	"reflect"
	"sort"
	"strings"

	"github.com/cloudwego/hertz/pkg/app/server"
)

// Info 描述 API 自身
type Info struct {
	Title       string // 文档标题(如"我的平台 - 后台 API")
	Description string
	Version     string // 1.0.0
	BaseURL     string // 用于 server URL 提示(如 https://api.example.com)
}

// Component 是 OpenAPI 3.0 标准 components 块
type Components struct {
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes"`
	Schemas         map[string]Schema         `json:"schemas,omitempty"`
}

// SecurityScheme 简化版(只覆盖我们用的 Bearer)
type SecurityScheme struct {
	Type         string `json:"type"`   // "http" / "apiKey" / "oauth2"
	Scheme       string `json:"scheme"` // "bearer"
	BearerFormat string `json:"bearerFormat,omitempty"`
}

// Schema 简化版 — 用 map[string]any 转,避免写 100+ DTO
type Schema = map[string]any

// PathItem OpenAPI Path Item 对象(按 method key 存 Operation)
type PathItem struct {
	Get     *Operation `json:"get,omitempty"`
	Post    *Operation `json:"post,omitempty"`
	Put     *Operation `json:"put,omitempty"`
	Patch   *Operation `json:"patch,omitempty"`
	Delete  *Operation `json:"delete,omitempty"`
	Options *Operation `json:"options,omitempty"`
	Head    *Operation `json:"head,omitempty"`
}

// Operation OpenAPI Operation 对象
type Operation struct {
	Tags        []string              `json:"tags,omitempty"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	OperationID string                `json:"operationId,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
	// 扩展字段:RBAC code(框架字段,OpenAPI 标准用 x- 前缀)
	XRbacCode string `json:"x-rbac-code,omitempty"`
	// XRequestSchema 这条接口的请求体组件名(如 "service.CreateUserRequest")
	//
	// 空 = 没拿到字段级 schema(请求体仍是空 object)。覆盖率汇总在文档级的
	// x-schema-coverage 里 —— 不藏。
	XRequestSchema string `json:"x-request-schema,omitempty"`
}

// SchemaCoverage 字段级 schema 的覆盖情况
//
// ⚠️ **为什么要写进文档**:本仓的请求体 schema 是**自动生成**的,覆盖率
// 取决于源码里 handler 怎么写。把它写进 doc.json,是为了让"这份文档的
// 字段契约有多全"变成可核对的数字,而不是靠人相信。
type SchemaCoverage struct {
	// Routes 文档里的路由总数
	Routes int `json:"routes"`
	// WriteRoutes 带请求体的路由数(POST/PUT/PATCH)
	WriteRoutes int `json:"write_routes"`
	// WithRequestSchema 拿到了字段级 schema 的路由数
	WithRequestSchema int `json:"with_request_schema"`
	// Percent 覆盖率(整数百分比)
	Percent int `json:"percent"`
}

// Parameter OpenAPI Parameter 对象
type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"` // "path" / "query" / "header" / "cookie"
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Schema      Schema `json:"schema,omitempty"`
}

// RequestBody
type RequestBody struct {
	Description string               `json:"description,omitempty"`
	Required    bool                 `json:"required,omitempty"`
	Content     map[string]MediaType `json:"content"`
}

// MediaType
type MediaType struct {
	Schema Schema `json:"schema,omitempty"`
}

// Response
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// OpenAPI 完整 spec 结构
type OpenAPI struct {
	OpenAPI    string                `json:"openapi"` // "3.0.3"
	Info       map[string]string     `json:"info"`
	Servers    []Server              `json:"servers,omitempty"`
	Tags       []Tag                 `json:"tags,omitempty"`
	Paths      map[string]*PathItem  `json:"paths"`
	Components Components            `json:"components"`
	Security   []map[string][]string `json:"security,omitempty"`
	// XSchemaCoverage 字段级 schema 的覆盖情况(诚实配套:覆盖率不藏着)
	XSchemaCoverage *SchemaCoverage `json:"x-schema-coverage,omitempty"`
}

// Server
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Tag
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// BuildSpec 从 Hertz 路由表构建 OpenAPI 3.0 spec
//   - 跳过 /swagger/*、/healthz、/readyz、/metrics(框架/健康/指标端点不进业务文档)
//   - 按 URL 段自动分 tag: /rapid-admin/v1/public → "Public", /rapid-admin/v1/system/users → "User" 等等
//   - path 形如 :id 自动转 OpenAPI {id}
//   - rbac code 来自 Router 包的 rbacTracking 共享状态(router.SetRBACCodes 注入)
func BuildSpec(h *server.Hertz, info Info, rbacCodes map[string]bool) *OpenAPI {
	if h == nil {
		return &OpenAPI{OpenAPI: "3.0.3"}
	}

	infoMap := map[string]string{
		"title":       defaultIfEmpty(info.Title, "Home Assistant API"),
		"description": info.Description,
		"version":     defaultIfEmpty(info.Version, "1.0.0"),
	}

	spec := &OpenAPI{
		OpenAPI: "3.0.3",
		Info:    infoMap,
		Paths:   make(map[string]*PathItem),
		Components: Components{
			SecuritySchemes: map[string]SecurityScheme{
				"BearerAuth": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "JWT",
				},
			},
			Schemas: map[string]Schema{
				"ApiResponse": {
					"type": "object",
					"properties": map[string]any{
						"code":       map[string]any{"type": "integer", "description": "业务错误码;0 表示成功"},
						"message":    map[string]any{"type": "string", "description": "错误文案"},
						"data":       map[string]any{"description": "业务数据;成功时为响应,失败时为 null"},
						"request_id": map[string]any{"type": "string", "description": "请求 ID,日志关联用"},
					},
					"required": []string{"code", "message", "request_id"},
				},
				"PageRequest": {
					"type": "object",
					"properties": map[string]any{
						"page": map[string]any{"type": "integer", "default": 1, "minimum": 1, "description": "页码,从 1 开始"},
						"size": map[string]any{"type": "integer", "default": 20, "minimum": 1, "maximum": 200, "description": "每页条数"},
					},
				},
				"PageResponse": {
					"type": "object",
					"properties": map[string]any{
						"list":  map[string]any{"type": "array", "items": map[string]any{}},
						"total": map[string]any{"type": "integer"},
						"page":  map[string]any{"type": "integer"},
						"size":  map[string]any{"type": "integer"},
					},
				},
			},
		},
		Security: []map[string][]string{
			{"BearerAuth": {}},
		},
	}

	if info.BaseURL != "" {
		spec.Servers = []Server{{URL: info.BaseURL, Description: "生产环境"}}
	}

	// 已知 tag 列表(按 URL 段归类),用于 sidebar 排序
	knownTags := []Tag{
		{Name: "Public", Description: "公开接口(无需登录)"},
		{Name: "Auth", Description: "鉴权与会话管理"},
		{Name: "User", Description: "用户管理"},
		{Name: "Role", Description: "角色管理"},
		{Name: "Resource", Description: "资源(权限)管理"},
		{Name: "Dept", Description: "部门管理"},
		{Name: "Dict", Description: "数据字典"},
		{Name: "Audit", Description: "审计日志"},
		{Name: "Project", Description: "项目管理"},
		{Name: "Building", Description: "楼栋管理"},
		{Name: "Layout", Description: "户型管理"},
		{Name: "House", Description: "房屋管理"},
		{Name: "Owner", Description: "业主管理"},
		{Name: "Family", Description: "家庭组"},
		{Name: "Asset", Description: "资产管理"},
		{Name: "Material", Description: "资料管理"},
		{Name: "Notice", Description: "公告管理"},
		{Name: "Delivery", Description: "交付流程"},
		{Name: "Inspection", Description: "验房"},
		{Name: "Issue", Description: "问题工单"},
		{Name: "Supplier", Description: "供应商管理"},
		{Name: "Contract", Description: "合同管理"},
		{Name: "Device", Description: "IoT 设备"},
		{Name: "Feedback", Description: "反馈与建议"},
		{Name: "Guide", Description: "使用指南"},
		{Name: "Activity", Description: "活动管理"},
		{Name: "Upload", Description: "文件上传(OSS)"},
		{Name: "Config", Description: "平台配置中心"},
		{Name: "WxTemplate", Description: "微信模板消息"},
		{Name: "Dashboard", Description: "看板统计"},
		{Name: "Platform", Description: "平台运营专属"},
	}
	tagSeen := make(map[string]bool, len(knownTags))
	for _, t := range knownTags {
		tagSeen[t.Name] = false
	}

	// 走路由表
	seenTagOrder := []string{}
	tagMap := map[string]string{
		"public":        "Public",
		"auth":          "Auth",
		"users":         "User",
		"roles":         "Role",
		"resources":     "Resource",
		"resource":      "Resource",
		"departments":   "Dept",
		"depts":         "Dept",
		"dicts":         "Dict",
		"audit":         "Audit",
		"projects":      "Project",
		"buildings":     "Building",
		"layout-types":  "Layout",
		"houses":        "House",
		"owners":        "Owner",
		"families":      "Family",
		"assets":        "Asset",
		"asset-assigns": "Asset",
		"materials":     "Material",
		"notices":       "Notice",
		"deliveries":    "Delivery",
		"inspections":   "Inspection",
		"issues":        "Issue",
		"suppliers":     "Supplier",
		"contracts":     "Contract",
		"devices":       "Device",
		"feedbacks":     "Feedback",
		"guides":        "Guide",
		"activities":    "Activity",
		"uploads":       "Upload",
		"configs":       "Config",
		"wx-templates":  "WxTemplate",
		"dashboard":     "Dashboard",
	}

	routes := h.Routes()
	// 字段级 schema 渲染器(整个 spec 共用一个 —— 组件区要跨路由累积)
	builder := NewSchemaBuilder(currentFieldDocs())
	// 覆盖率统计
	var writeRoutes, withSchema int

	for _, r := range routes {
		method := strings.ToUpper(r.Method)
		path := r.Path

		// 跳过框架/健康/指标端点
		if strings.HasPrefix(path, "/swagger") ||
			path == "/healthz" || path == "/readyz" ||
			strings.HasPrefix(path, "/metrics") {
			continue
		}

		// 必须以 /rapid-admin/v1 开头
		if !strings.HasPrefix(path, "/rapid-admin/v1/") && path != "/rapid-admin/v1" {
			continue
		}

		// 路径转 OpenAPI 风格
		openAPIPath := hertzPathToOpenAPI(path)
		// 提取 tag: /rapid-admin/v1/<group>/<resource>[/...]
		parts := strings.Split(strings.Trim(path, "/"), "/")
		// parts: ["admin","v1","<group>","<resource>",...]
		var tag string
		if len(parts) >= 3 {
			// /rapid-admin/v1/public/auth/login → 第三个段是 group
			// /rapid-admin/v1/system/users → 第三个段是 system,第四个是 users
			group := parts[2]
			if group == "public" || group == "system" || group == "biz" || group == "platform" {
				if len(parts) >= 4 {
					if m, ok := tagMap[parts[3]]; ok {
						tag = m
					} else {
						tag = strings.Title(parts[3])
					}
				} else {
					tag = strings.Title(group)
				}
			} else {
				// /rapid-admin/v1/auth/... 这种(直接挂在 api group)
				if m, ok := tagMap[group]; ok {
					tag = m
				} else {
					tag = strings.Title(group)
				}
			}
		} else {
			tag = "Public"
		}

		// 推断 RBAC code
		rbacCode := inferRBACCode(path, parts, rbacCodes)

		// 构建 Operation
		op := &Operation{
			Tags:        []string{tag},
			Summary:     buildSummary(path, method),
			Description: buildDescription(path, method),
			OperationID: buildOperationID(method, openAPIPath),
			Responses:   buildResponses(method, path),
			Security: []map[string][]string{
				{"BearerAuth": {}},
			},
			XRbacCode: rbacCode,
		}

		// 提取 path 参数
		if params := extractPathParams(path); len(params) > 0 {
			op.Parameters = params
		}

		// 写/非 GET 自动加 requestBody
		if method != "GET" && method != "DELETE" {
			writeRoutes++
			body := &RequestBody{
				Description: "JSON 请求体",
				Required:    true,
				Content: map[string]MediaType{
					"application/json": {
						Schema: Schema{"type": "object"},
					},
				},
			}
			// 字段级 schema:从生成物里按 handler 方法名取请求体 DTO 类型,
			// 再用反射渲染成 OpenAPI schema(见 schema.go)。
			//
			// ⚠️ **取不到就保留空 object** —— 不猜、不编。取不到的原因会
			// 反映在文档级的 x-schema-coverage 里(覆盖率是真实数字)。
			if dto, ok := lookupRequestDTO(HandlerKey(r.Handler)); ok {
				if t := reflect.TypeOf(dto); t != nil {
					schema := builder.SchemaOf(t)
					body.Content["application/json"] = MediaType{Schema: schema}
					if name := refName(schema); name != "" {
						op.XRequestSchema = name
						withSchema++
					}
				}
			}
			op.RequestBody = body
		}

		// 加入 spec
		if _, exists := spec.Paths[openAPIPath]; !exists {
			spec.Paths[openAPIPath] = &PathItem{}
		}
		setPathItemOperation(spec.Paths[openAPIPath], method, op)

		// tag 顺序
		if !tagSeen[tag] {
			tagSeen[tag] = true
			seenTagOrder = append(seenTagOrder, tag)
		}
	}

	// 按出现顺序输出 tag
	tagSet := make(map[string]bool)
	for _, t := range knownTags {
		if tagSeen[t.Name] {
			spec.Tags = append(spec.Tags, t)
			tagSet[t.Name] = true
		}
	}
	// 兜底:出现未在 knownTags 列表的 tag
	for _, t := range seenTagOrder {
		if !tagSet[t] {
			spec.Tags = append(spec.Tags, Tag{Name: t})
			tagSet[t] = true
		}
	}

	// 字段级 schema 的组件区:把渲染器收进来的具名类型并进 components.schemas
	for name, s := range builder.Components() {
		spec.Components.Schemas[name] = s
	}
	// 覆盖率(诚实配套:数字是真实的,不是宣传)
	cov := &SchemaCoverage{
		Routes:            len(spec.Paths),
		WriteRoutes:       writeRoutes,
		WithRequestSchema: withSchema,
	}
	if writeRoutes > 0 {
		cov.Percent = withSchema * 100 / writeRoutes
	}
	spec.XSchemaCoverage = cov

	return spec
}

// refName 从 $ref 里取出组件名
//
// `$ref: "#/components/schemas/service.CreateUserRequest"` →
// "service.CreateUserRequest"。不是 $ref 就返回空。
func refName(s Schema) string {
	ref, ok := s["$ref"].(string)
	if !ok {
		return ""
	}
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}
	return strings.TrimPrefix(ref, prefix)
}

// hertzPathToOpenAPI /rapid-admin/v1/users/:id → /rapid-admin/v1/users/{id}
func hertzPathToOpenAPI(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if strings.HasPrefix(seg, ":") {
			parts[i] = "{" + seg[1:] + "}"
		}
		if strings.HasPrefix(seg, "*") {
			// 通配符转 OpenAPI 的 "remaining path"
			parts[i] = "{remaining}"
		}
	}
	return strings.Join(parts, "/")
}

// extractPathParams 提取 :id 形式 path 参数
func extractPathParams(p string) []Parameter {
	var out []Parameter
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ":") {
			out = append(out, Parameter{
				Name:     seg[1:],
				In:       "path",
				Required: true,
				Schema:   Schema{"type": "string"},
			})
		}
	}
	return out
}

// inferRBACCode 从 path 推断 RBAC code,与 rbacCodes map 匹配
// 优先精确匹配,如 /rapid-admin/v1/system/users/:id  → "user:read"
// 不在 map 里则空字符串
func inferRBACCode(path string, parts []string, rbacCodes map[string]bool) string {
	if rbacCodes == nil {
		return ""
	}
	// 启发式:tag 名 + ":list" / ":read" / ":update" / ":delete" / ":create"
	// 实际更精细的 mapping 需要 handler 名解析,这里给 best-effort 提示
	// 业务层可后续通过 rbacTracking 共享 state 注入(见 router.go)
	if len(parts) < 3 {
		return ""
	}

	// 优先用 group 内 2 段匹配(更精确),回退 1 段
	if len(parts) >= 4 {
		resource := parts[3]
		for action := range actionGuesses {
			code := resource + ":" + action
			if rbacCodes[code] {
				return code
			}
		}
	}
	if len(parts) >= 3 {
		group := parts[2]
		if group == "public" {
			return ""
		}
		for action := range actionGuesses {
			code := group + ":" + action
			if rbacCodes[code] {
				return code
			}
		}
	}
	return ""
}

var actionGuesses = map[string]struct{}{
	"list": {}, "read": {}, "create": {}, "update": {},
	"delete": {}, "audit": {}, "export": {},
}

// buildSummary 自动生成简要 summary(没注释时的兜底)
func buildSummary(path, method string) string {
	// 取最后一段作主语
	parts := strings.Split(strings.Trim(path, "/"), "/")
	subject := ""
	if len(parts) > 0 {
		subject = parts[len(parts)-1]
		if strings.HasPrefix(subject, ":") {
			subject = "单条资源"
		}
	}
	switch method {
	case "GET":
		if strings.Contains(path, ":") {
			return "查询" + subject
		}
		return "查询" + subject + "列表"
	case "POST":
		return "创建" + subject
	case "PATCH", "PUT":
		return "更新" + subject
	case "DELETE":
		return "删除" + subject
	default:
		return method + " " + path
	}
}

// buildDescription 简短的描述
func buildDescription(path, method string) string {
	return "该接口为自动生成,详细字段语义请参考 server/internal/api/handler/v1/ 对应 handler。\n\nPath: `" + path + "`"
}

// buildOperationID 生成稳定的 operationId
func buildOperationID(method, path string) string {
	clean := strings.ReplaceAll(strings.ReplaceAll(path, "/", "_"), ":", "")
	clean = strings.ReplaceAll(clean, "{", "")
	clean = strings.ReplaceAll(clean, "}", "")
	clean = strings.Trim(clean, "_")
	return strings.ToLower(method) + "_" + clean
}

// buildResponses 标准 401/403/500 + 200
func buildResponses(method, path string) map[string]Response {
	out := map[string]Response{
		"200": {
			Description: "成功",
			Content: map[string]MediaType{
				"application/json": {Schema: Schema{"$ref": "#/components/schemas/ApiResponse"}},
			},
		},
		"401": {Description: "未登录或 token 已过期"},
		"403": {Description: "无权限访问"},
		"500": {Description: "服务器内部错误"},
	}
	if method == "GET" {
		// 列表接口有 PageResponse
		if !strings.Contains(path, ":") {
			out["200"] = Response{
				Description: "成功",
				Content: map[string]MediaType{
					"application/json": {Schema: Schema{
						"type": "object",
						"properties": map[string]any{
							"code":       map[string]any{"type": "integer"},
							"message":    map[string]any{"type": "string"},
							"data":       map[string]any{"$ref": "#/components/schemas/PageResponse"},
							"request_id": map[string]any{"type": "string"},
						},
					}},
				},
			}
		}
	}
	return out
}

// setPathItemOperation 把 op 放进 PathItem 对应 method 槽
func setPathItemOperation(pi *PathItem, method string, op *Operation) {
	switch method {
	case "GET":
		pi.Get = op
	case "POST":
		pi.Post = op
	case "PUT":
		pi.Put = op
	case "PATCH":
		pi.Patch = op
	case "DELETE":
		pi.Delete = op
	case "OPTIONS":
		pi.Options = op
	case "HEAD":
		pi.Head = op
	}
}

// defaultIfEmpty s 为空返回 fallback
func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// SortPathsStable 在 marshal 前对 paths 做排序,让 spec 输出稳定(git diff 友好)
func SortPathsStable(spec *OpenAPI) {
	keys := make([]string, 0, len(spec.Paths))
	for k := range spec.Paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sorted := make(map[string]*PathItem, len(keys))
	for _, k := range keys {
		sorted[k] = spec.Paths[k]
	}
	spec.Paths = sorted
}
