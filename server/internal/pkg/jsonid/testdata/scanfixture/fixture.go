package fixture

// 这个文件是扫描器的**已知答案测试**输入。
//
// 每种情况都摆在这里,TestScanDirFindsKnownBadTags 逐条断言"该抓的抓到、
// 不该抓的别抓"。没有它的话,一个永远返回空列表的扫描器也能让棘轮测试一直绿 ——
// 那样这个"防护"就是自欺欺人。
//
// 它不需要能编译:testdata/ 目录被 go 工具和 ScanDir 都主动跳过,
// 生产扫描绝不会把这些算成违规;测试里直接把 ScanDir 指向这个目录。
type embedded struct{ NoTag uint64 }

// ID 是本包自定义的类型,fixture 里用本地占位类型代替(免得 import 成环)
type ID uint64

// IDList 同理
type IDList []ID

type Fixture struct {
	// ---- 应该被抓(整数 + id 名 + 没有 ,string)----

	ID     uint64 `json:"id"`
	UserID uint64 `json:"user_id"`

	// ---- 应该被抓:切片即使打了 ,string 也不算数 ----
	// encoding/json 的 string 选项只作用于 string/float/int/bool 标量,
	// 对数组无效 —— 这是 IDList 存在的唯一理由。
	IdentityIDs []uint64 `json:"identity_ids,omitempty"`

	// 指针类型也要能认出来
	ParentID *uint64 `json:"parent_id"`

	// ---- 不该被抓 ----

	// 非 id 语义的字段名
	Name string `json:"name"`
	Sort int    `json:"sort"`

	// 已经是 jsonid 类型
	Typed    ID     `json:"typed_id"`
	TypedIDs IDList `json:"typed_ids"`

	// 标量 + ,string 是合法的另一种解法(本仓库最终统一走 jsonid.ID,
	// 但扫描器不该把它判成违规)
	WithStringTag uint64 `json:"with_string_id,string"`

	// 令牌类"看起来像 id"但本来就是字符串的字段
	CaptchaID string `json:"captcha_id"`
	TraceID   string `json:"trace_id"`
	RequestID string `json:"request_id"`
	OpenID    string `json:"open_id"`

	// 没有 json tag
	NoTag uint64

	// ---- "_by" 后缀也是 ID(操作人 user_id),第一版规则漏了这一整类 ----
	// 同名的非 ID 字段是 string,靠 integerish 的类型判定自动排除
	CreatedBy uint64 `json:"created_by"`
	ReviewBy  uint64 `json:"review_by"`

	OrderBy         string `json:"order_by"`             // 排序列名,不是 ID
	LastChangedName string `json:"last_changed_by_name"` // 展示名,不是 ID

	// 匿名嵌入:必须跳过(它没有具名字段名)
	embedded
}
