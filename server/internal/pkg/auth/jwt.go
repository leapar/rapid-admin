// JWT 签发 / 解析 / 校验 —— 三受众独立 secret
// 文档 §7.7.4.14(JWT claim 约定与老 token 处置)
// ⚠️ 原注释写的"§9.6"指向的是加密章节,与本文件无关,2026-10-21 修正。
package auth

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"rapid-admin/internal/pkg/randomx"
)

// Audience 受众
type Audience string

const (
	AudienceAdmin    Audience = "admin"
	AudienceOwner    Audience = "owner"
	AudienceSupplier Audience = "supplier"
)

// AllAudiences 所有合法受众
func AllAudiences() []Audience {
	return []Audience{AudienceAdmin, AudienceOwner, AudienceSupplier}
}

// IsValidAudience 校验 audience 合法性
func IsValidAudience(a Audience) bool {
	for _, x := range AllAudiences() {
		if x == a {
			return true
		}
	}
	return false
}

// TokenType token 用途声明
//
// 为什么必须有:在没有这个 claim 之前,SignAccess / SignRefresh / SignShort
// 三者只差一个 TTL,产出的 claim 完全一致 —— 于是 **refresh token 能当 access
// token 用**,而它的有效期是 720h。一个从日志/缓存/URL 里泄露的 refresh token
// 等同于 30 天的万能通行证。声明用途并在解析时强制比对,是这类凭证体系的基本
// 要求。
type TokenType string

const (
	// TokenTypeAccess 访问令牌 —— 只能打到受保护路由
	TokenTypeAccess TokenType = "access"
	// TokenTypeRefresh 刷新令牌 —— 只能用于换发新 access token
	TokenTypeRefresh TokenType = "refresh"
	// TokenTypeShort 短时令牌(如"查看完整手机号"的一次性凭证)
	TokenTypeShort TokenType = "short"
)

// 解析期的错误。用 sentinel 而不是拼字符串,调用方用 errors.Is 判定。
var (
	// ErrLegacyToken 老 token 没有 typ claim(本轮上线前签发的)
	ErrLegacyToken = errors.New("legacy token: missing typ claim")
	// ErrTokenTypeMismatch token 用途与要求的用途不符
	ErrTokenTypeMismatch = errors.New("token type mismatch")
	// ErrTokenRevoked token 已被吊销
	ErrTokenRevoked = errors.New("token revoked")
)

// Claims 自定义 JWT 声明
//
// ⚠️ 这里只放**业务** claim。标准 claim(aud / exp / iat / nbf / iss / jti)一律走
// 嵌入的 jwt.RegisteredClaims,不要在 Claims 上再声明同名同 tag 的字段 ——
// encoding/json 的规则是**浅层胜出**,后加的自定义字段会把 RegisteredClaims 里的
// 那个彻底遮蔽(marshal / unmarshal 两边都收不到),留下一段看着能跑、实际是死代码
// 的赋值。本文件历史上就踩过两次:`Audience`(json:"aud")遮蔽 RegisteredClaims.Audience、
// `JTI`(json:"jti")遮蔽 RegisteredClaims.ID。两处都已经移除,业务代码改用下面的
// Audience() / JTI() 访问器。
type Claims struct {
	UserID     uint64   `json:"uid"`
	IdentityID uint64   `json:"iid,omitempty"`
	Username   string   `json:"usr"`
	RoleCode   string   `json:"role,omitempty"`
	ProjectIDs []uint64 `json:"pj,omitempty"`

	// SessionID 会话 id(sid)。同一时刻该会话的 access / refresh 两张 token
	// 共用同一个 sid。
	//
	// ⚠️ **M148(2026-11-01):切换身份会换 sid** —— 换身份时旧 sid 被吊销、
	//   新 sid 随新 token 一起发出去。
	//
	//   以前是"切换身份不换 sid",理由是"换的是同一台设备上的身份,不是新会话"。
	//   那个理由**只考虑了登出**,漏了更要紧的那一头:切换时服务端只拿得到
	//   **access** 的 jti,拿不到切换**之前**那张 **refresh** 的 jti ⇒ 吊销不了它
	//   ⇒ 它还能换发一份**旧身份**的 access(jti 没吊 / sid 没变 / 作废线没推,
	//   三道闸全过)。
	//   ⇒ 危害:用户从高危身份切到低危身份之后,手里那张旧 refresh 还能继续
	//   拿高危身份,最长 720h。"切身份"在安全边界上等于什么也没做。
	//
	//   sid 是**会话级**判据,refresh 续期时必查 ⇒ 吊掉旧 sid,那条会话的
	//   access + refresh 一起死,**不管中间轮换过几轮**(轮换只换 jti,sid 不变)。
	//
	// 它存在的理由有两条:
	//  1. **让"登出"能同时结束这条会话的 refresh token** —— 登出时后端只拿得到
	//     access token 的 jti,拿不到 refresh 的 jti,除非两者之间有一条可查的
	//     关联,sid 就是那条关联。
	//  2. **让"切换身份"能结束切换之前那条会话** —— 同上,服务端拿不到旧
	//     refresh 的 jti,只能靠 sid 这个会话级句柄。
	//
	// omitempty:老 token 没有这个 claim,解析侧按"没有会话绑定"处理
	// (降级语义见 session.go 的说明)。
	SessionID string `json:"sid,omitempty"`

	// TokenType 本 token 的用途,签发时由 Manager 写入,解析时强制比对
	TokenType TokenType `json:"typ"`

	jwt.RegisteredClaims
}

// Audience 返回 token 的受众(读标准 RegisteredClaims.Audience)
func (c *Claims) Audience() Audience {
	if len(c.RegisteredClaims.Audience) == 0 {
		return ""
	}
	return Audience(c.RegisteredClaims.Audience[0])
}

// JTI 返回 token 的唯一标识(读标准 RegisteredClaims.ID)
func (c *Claims) JTI() string { return c.RegisteredClaims.ID }

// Fork 复制**业务** claim,产出一个全新的、不带任何标准 claim 的结构。
//
// # 为什么需要它
//
// sign() 通过指针**无条件覆写**整个 RegisteredClaims(iat / exp / aud / jti),
// 且每次生成**新的随机 jti**。所以复用同一个结构签两次,坏的不是"两张 token
// 撞号",而是**内存里那份 claims 最终停留在第二张(后签的那张)token 上**:
//
//	base, _ := ParseRefresh(tok)
//	access,  _ := SignByAudience(base.Audience(), base)    // base 变成 access 的
//	refresh, _ := RefreshByAudience(base.Audience(), base) // base 又变成 refresh 的
//
// 两张 token 的 JTI **是不同**的(各随机一次,实测确认)。但此后再读
// base.JTI() / base.ExpiresAt,拿到的都是**刚签出来的新 refresh** 的值,
// 而不是调用方以为自己还握着的那张旧 token。
//
// # 后果(全部实测过,不是推演)
//
// 在 refresh 轮换里(RefreshToken 第 4~5 步):
//  - `Revoke(claims.JTI(), ...)` 吊掉的是**刚签出的新 refresh**,不是被消费
//    的旧 token → 轮换形同虚设,同一个 refresh 可以无限换发;
//  - 同时**新 refresh 一出生就是吊销态** → 用户续期成功、下次续期立刻被拒,
//    被踢回登录页 —— 一个只在真机上偶发的死循环。
//  - 吊销 TTL 也取错:算的是新 refresh 的 720h,而不是旧 token 的剩余寿命。
//
// 这不是"小心一点"能避免的:复用指针是个看起来完全合理的写法,而且
// **签发全部成功、不报错**,只在签发之后的下一行开始悄悄指错对象。
// 修法是让写错在类型层面做不出来 —— 要签两次就得 fork 两次。
func (c *Claims) Fork() *Claims {
	// ProjectIDs 是切片,浅拷贝共享底层数组,这里刻意不深拷贝:
	// 它只被读(sign 原样序列化)。真要防误改,该另给一个名字里带 Deep 的。
	//
	// ⚠️ SessionID **必须**在这里复制。漏了它的后果是"会话绑定在第一次
	// refresh 之后就断了":轮换时新签的两张 token 没有 sid,于是登出只能吊销
	// access 的 jti,refresh 照旧能续命 —— 正好是 sid 这个机制要消灭的那个洞。
	// 而且它**不会报错**:签发照样成功,只是 quietly 少了一个字段。
	// 见 session.go 的 TestClaims_ForkCarriesSessionID。
	return &Claims{
		UserID:     c.UserID,
		IdentityID: c.IdentityID,
		Username:   c.Username,
		RoleCode:   c.RoleCode,
		ProjectIDs: c.ProjectIDs,
		SessionID:  c.SessionID,
	}
}

// Manager 单个受众的 JWT 管理器
type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	shortTTL   time.Duration
	issuer     string
	audience   Audience
}

// NewManager 创建一个受众的 manager
func NewManager(secret, issuer string, accessTTL, refreshTTL, shortTTL time.Duration, aud Audience) *Manager {
	return &Manager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		shortTTL:   shortTTL,
		issuer:     issuer,
		audience:   aud,
	}
}

// Audience 返回此 manager 服务的受众
func (m *Manager) Audience() Audience { return m.audience }

// AccessTTL access token 有效期
func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

// RefreshTTL refresh token 有效期
func (m *Manager) RefreshTTL() time.Duration { return m.refreshTTL }

// ShortTTL 短时 token 有效期
func (m *Manager) ShortTTL() time.Duration { return m.shortTTL }

// SignAccess 签发 access token
func (m *Manager) SignAccess(c *Claims) (string, error) {
	return m.sign(c, TokenTypeAccess, m.accessTTL)
}

// SignRefresh 签发 refresh token
func (m *Manager) SignRefresh(c *Claims) (string, error) {
	return m.sign(c, TokenTypeRefresh, m.refreshTTL)
}

// SignShort 签发短时 token(查看完整手机号等)
func (m *Manager) SignShort(c *Claims) (string, error) {
	return m.sign(c, TokenTypeShort, m.shortTTL)
}

// jtiRandomBytes JTI 的随机字节数。16 字节 = 128 bit,碰撞概率在
// 单实例累计 10 亿次签发的量级下仍低于 10^-9。
const jtiRandomBytes = 16

// sign 签发。typ / JTI / aud / iss 全部由本方法**权威写入**,不信任调用方传的值。
//
// 三个原因:
//  1. aud 以本 manager 为准 —— secret 在谁手里,audience 就只能是它,
//     不存在"用 owner 的 secret 签一个 aud=admin 的 token"这种自相矛盾的产物。
//  2. JTI 无条件覆盖。吊销链路(登出 / 改密码 / 轮换 refresh)全部以 JTI 为键,
//     一旦某个签发点"忘了设",它签出去的 token 就永远吊销不掉,而且是静默失效。
//     让 sign() 保证这一点,比在每个调用点写注释可靠。
//  3. typ 必须由签发方法决定,调用方无从干预。
func (m *Manager) sign(c *Claims, typ TokenType, ttl time.Duration) (string, error) {
	now := time.Now()
	c.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    m.issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		Audience:  jwt.ClaimStrings{string(m.audience)},
		ID:        randomx.Token(jtiRandomBytes),
	}
	c.TokenType = typ
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return tok.SignedString(m.secret)
}

// Parse 解析 + 校验 audience + 校验 token 用途
//
// expect 是**必填**的:调用方必须明确说明"我要一个什么用途的 token"。
// 不提供"不校验类型"的旁路 —— 那等于把 typ 这个声明重新变成摆设。
func (m *Manager) Parse(token string, expect TokenType) (*Claims, error) {
	t, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := t.Claims.(*Claims)
	if !ok || !t.Valid {
		return nil, errors.New("invalid token")
	}
	got := c.Audience()
	if got != m.audience {
		return nil, fmt.Errorf("audience mismatch: expect %s got %s", m.audience, got)
	}
	// 老 token(typ 缺失)一律拒收,不提供兼容解析。
	// 兼容分支等于让"refresh 可当 access 用"这个洞一直活着,而且这里没有迁移窗口
	// 的概念 —— 一个 720h 的兼容窗口不是迁移,是永久双模解析。
	if c.TokenType == "" {
		return nil, ErrLegacyToken
	}
	if c.TokenType != expect {
		return nil, fmt.Errorf("%w: expect %s got %s", ErrTokenTypeMismatch, expect, c.TokenType)
	}
	return c, nil
}

// ParseAccess 解析 access token
func (m *Manager) ParseAccess(token string) (*Claims, error) {
	return m.Parse(token, TokenTypeAccess)
}

// ParseRefresh 解析 refresh token
func (m *Manager) ParseRefresh(token string) (*Claims, error) {
	return m.Parse(token, TokenTypeRefresh)
}

// ParseShort 解析短时 token
func (m *Manager) ParseShort(token string) (*Claims, error) {
	return m.Parse(token, TokenTypeShort)
}

// Multi 三受众管理器集合
type Multi struct {
	mu sync.RWMutex
	m  map[Audience]*Manager
}

// NewMulti 从 3 套 secret 构造 Multi
func NewMulti(adminSecret, ownerSecret, supplierSecret, issuer string,
	accessTTL, refreshTTL, shortTTL time.Duration) *Multi {

	mp := &Multi{m: make(map[Audience]*Manager, 3)}
	mp.m[AudienceAdmin] = NewManager(adminSecret, issuer, accessTTL, refreshTTL, shortTTL, AudienceAdmin)
	mp.m[AudienceOwner] = NewManager(ownerSecret, issuer, accessTTL, refreshTTL, shortTTL, AudienceOwner)
	mp.m[AudienceSupplier] = NewManager(supplierSecret, issuer, accessTTL, refreshTTL, shortTTL, AudienceSupplier)
	return mp
}

// Get 取得某个 audience 的 manager
func (p *Multi) Get(a Audience) (*Manager, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	m, ok := p.m[a]
	return m, ok
}

// MustGet 取得 manager,失败 panic(用于 main 启动时已确保全部配置)
func (p *Multi) MustGet(a Audience) *Manager {
	m, ok := p.Get(a)
	if !ok {
		panic(fmt.Sprintf("auth.Multi: no manager for audience %s", a))
	}
	return m
}

// All 列出所有 manager
func (p *Multi) All() []*Manager {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Manager, 0, len(p.m))
	for _, x := range p.m {
		out = append(out, x)
	}
	return out
}

// parse 按期望用途解析:先不验签地读出 aud 选出 manager(即选出正确的 secret),
// 再交给该 manager 做完整校验。
//
// ⚠️ 未验签嗅探出来的 aud 只用来**选 secret**,不作为任何信任依据 —— 真正的
// 校验由 m.Parse 完成(它会重读 aud 并与 m.audience 比对)。所以伪造 aud 只能把
// 请求路由到错误的 secret 上,结果必然验签失败,拿不到任何权限。
func (p *Multi) parse(token string, expect TokenType) (*Claims, error) {
	sniff := jwt.NewParser(jwt.WithoutClaimsValidation())
	c := &Claims{}
	if _, _, err := sniff.ParseUnverified(token, c); err != nil {
		return nil, err
	}
	aud := c.Audience()
	if aud == "" {
		return nil, errors.New("missing audience claim")
	}
	m, ok := p.Get(aud)
	if !ok {
		return nil, fmt.Errorf("unknown audience: %s", aud)
	}
	return m.Parse(token, expect)
}

// ParseAnyAccess 解析任意受众的 **access** token(受保护路由中间件用)
func (p *Multi) ParseAnyAccess(token string) (*Claims, error) {
	return p.parse(token, TokenTypeAccess)
}

// ParseRefresh 解析任意受众的 **refresh** token(续期用)
func (p *Multi) ParseRefresh(token string) (*Claims, error) {
	return p.parse(token, TokenTypeRefresh)
}

// SignByAudience 按 audience 选 manager 签发 access token
func (p *Multi) SignByAudience(a Audience, c *Claims) (string, error) {
	m, ok := p.Get(a)
	if !ok {
		return "", fmt.Errorf("unknown audience: %s", a)
	}
	return m.SignAccess(c)
}

// RefreshByAudience 按 audience 选 manager 签发 refresh
func (p *Multi) RefreshByAudience(a Audience, c *Claims) (string, error) {
	m, ok := p.Get(a)
	if !ok {
		return "", fmt.Errorf("unknown audience: %s", a)
	}
	return m.SignRefresh(c)
}

// 全局 Multi(由 main 注入)
var defaultMulti *Multi

// SetDefault 注入全局 Multi
func SetDefault(p *Multi) { defaultMulti = p }

// Default 取全局 Multi
func Default() *Multi { return defaultMulti }
