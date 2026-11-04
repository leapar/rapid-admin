// 短信模板登记册 —— M115
//
// 登记册是**运行期的权威状态**(谁在用哪张模板、这张模板过没过审),
// 持久化只是它的快照(见 store.go)。
//
// ⚠️ **登记册不存"模板号的唯一真源"** ——
// 模板号存在配置中心(aliyun.sms.template.*),那是运营改的地方;
// 登记册里的 Code 只是**同步那一刻的快照**,用来发现"配置改了但登记册没跟上"。
package smstpl

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrNotRegistered 场景没登记
var ErrNotRegistered = errors.New("smstpl: 场景未登记")

// historyLimit 内存里保留的流转历史条数上限
//
// ⚠️ **有上限** —— 同步任务每分钟跑一次,一轮 4 个场景,一年下来几万条。
// 内存里留全量迟早把进程吃满。落库的完整历史在 sys_sms_template_log 表里。
const historyLimit = 500

// Template 登记册里的一条(一个场景一行)
type Template struct {
	// Scene 场景(login / reset_password / ...)
	Scene string `json:"scene"`
	// Code 当前登记的模板号(同步那一刻的快照)
	Code string `json:"code"`
	// Status 审核状态
	Status Status `json:"status"`
	// Reason 服务商给的备注 / 驳回理由
	Reason string `json:"reason"`
	// Source 当前状态是谁给的
	Source Source `json:"source"`
	// ReviewedAt 最后一次被服务商确认的时间
	ReviewedAt time.Time `json:"reviewed_at"`
	// UpdatedAt 登记册这一行最后变动的时间
	UpdatedAt time.Time `json:"updated_at"`
}

// Transition 一次状态流转
//
// ⚠️ **Code 必填** —— 审核的是**模板号**,不是场景。
//
//	同一个场景换过模板号之后,历史里全是同一个 scene,
//	不记 code 就分不清"哪一次流转说的是哪个号"。
type Transition struct {
	Scene  string `json:"scene"`
	Code   string `json:"code"`
	From   Status `json:"from"`
	To     Status `json:"to"`
	Source Source `json:"source"`
	Reason string `json:"reason"`
	// Operator 操作人(人工流转时 = 运维的登录名;服务商流转时为空)
	//
	// ⚠️ **Source 说"是人还是服务商",Operator 说"是哪个运维"** ——
	//
	//	两个都要:只有 Source 时,"人工停用"这一行不知道是谁停的,审计链断在这儿;
	//	只有 Operator 时,服务商同步过来的那些行全是空,看不出是谁给的状态。
	Operator string    `json:"operator"`
	At       time.Time `json:"at"`
}

// Registry 登记册(并发安全)
type Registry struct {
	mu    sync.RWMutex
	items map[string]*Template
	hist  []Transition
	now   func() time.Time // 可注入,单测用
}

// NewRegistry 构造
func NewRegistry() *Registry {
	return &Registry{items: make(map[string]*Template), now: time.Now}
}

// SetClock 注入时钟(单测用;生产不调)
func (r *Registry) SetClock(f func() time.Time) {
	if f == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = f
}

func (r *Registry) clock() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

// Register 登记 / 更新某个场景的模板号
//
// ⚠️ **模板号变了 ⇒ 审核状态立即作废,回到 unknown** ——
// 这是包注释「硬规则 3」。审核的是**模板号**:运营把 SMS_244570823 换成 SMS_999,
// 新号从没审过,拿旧号的"已通过"去担保新号就是假保证。
// 旧状态会作为一条流转记进历史(理由写明换号前后的号码),不是静默覆盖。
//
// 返回值:
//   - changed = 登记册这一行是否发生了实质变化(新建 / 换了号 / 状态被重置)
//   - tr     = 本次记进历史的流转;**没记历史时是零值**(At.IsZero() 为 true)
func (r *Registry) Register(scene, code string) (changed bool, tr Transition, err error) {
	if scene == "" {
		return false, Transition{}, ErrEmptyScene
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	old, exists := r.items[scene]
	if !exists {
		r.items[scene] = &Template{
			Scene:     scene,
			Code:      code,
			Status:    StatusUnknown,
			Source:    SourceManual,
			UpdatedAt: now,
		}
		// ⚠️ **新建不记历史** —— "从没有到 unknown"不是一次状态变化,
		//   记进去只会让历史里全是噪音。变化是从"有状态"开始的。
		return true, Transition{}, nil
	}
	if old.Code == code {
		return false, Transition{}, nil
	}
	// 模板号变了 → 状态作废
	from := old.Status
	oldCode := old.Code
	old.Code = code
	old.Status = StatusUnknown
	old.Reason = fmt.Sprintf("模板号变更:%s → %s,原审核状态作废", oldCode, code)
	old.Source = SourceManual
	old.UpdatedAt = now
	tr = Transition{
		Scene: scene, Code: code, From: from, To: StatusUnknown,
		Source: SourceManual, Reason: old.Reason, At: now,
	}
	r.appendHistory(tr)
	return true, tr, nil
}

// Apply 应用一次状态流转
//
// ⚠️ **状态没变且理由没变 ⇒ 不记历史**(changed=false) ——
// 服务商每次同步都会报同一个状态,如果每次都记一条,历史会被"已通过 → 已通过"刷满,
// 真正的那次变化反而找不到。
//
// ⚠️ **理由变了要记** —— 驳回理由被服务商补充说明了,那是新信息。
//
// ⚠️ **operator 是"哪个运维"** —— 人工流转时填登录名,服务商流转时留空
// (那一行的 Source 已经是 "provider",再写一遍"阿里云"是冗余)。
//
// 返回值里的 tr 是本次记进历史的流转;没记历史时是零值(At.IsZero() 为 true)。
func (r *Registry) Apply(scene string, to Status, src Source, reason, operator string) (changed bool, tr Transition, err error) {
	if scene == "" {
		return false, Transition{}, ErrEmptyScene
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	old, ok := r.items[scene]
	if !ok {
		return false, Transition{}, fmt.Errorf("%w: %s", ErrNotRegistered, scene)
	}
	if err := CanTransition(old.Status, to, src); err != nil {
		return false, Transition{}, err
	}
	now := r.now()
	statusSame := old.Status == to
	reasonSame := old.Reason == reason
	if statusSame && reasonSame {
		// 没变化 —— 但"最后确认时间"要更新(页面上要显示"服务商最后确认于何时")
		old.ReviewedAt = now
		old.UpdatedAt = now
		return false, Transition{}, nil
	}
	from := old.Status
	old.Status = to
	old.Reason = reason
	old.Source = src
	old.ReviewedAt = now
	old.UpdatedAt = now
	tr = Transition{
		Scene: scene, Code: old.Code, From: from, To: to,
		Source: src, Reason: reason, Operator: operator, At: now,
	}
	r.appendHistory(tr)
	return true, tr, nil
}

// Get 取某个场景的登记
func (r *Registry) Get(scene string) (Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[scene]
	if !ok {
		return Template{}, false
	}
	return *t, true
}

// List 全部登记(按场景名排序,输出稳定)
func (r *Registry) List() []Template {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Template, 0, len(r.items))
	for _, t := range r.items {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scene < out[j].Scene })
	return out
}

// History 某个场景的流转历史(最新在前)
func (r *Registry) History(scene string) []Transition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Transition, 0, 8)
	for i := len(r.hist) - 1; i >= 0; i-- {
		if r.hist[i].Scene == scene {
			out = append(out, r.hist[i])
		}
	}
	return out
}

// AllHistory 全部流转历史(最新在前)
func (r *Registry) AllHistory() []Transition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Transition, 0, len(r.hist))
	for i := len(r.hist) - 1; i >= 0; i-- {
		out = append(out, r.hist[i])
	}
	return out
}

// Load 覆盖式装载(DB 快照回填)
//
// ⚠️ **不清空已有行** —— 装载只是"把 DB 里的状态灌进来",
// 装载之后 Register 还会用生效配置对一遍号。
// 清空会让"DB 里有、内存里没有"变成"内存里也没有",把状态弄丢。
func (r *Registry) Load(items []Template) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range items {
		if t.Scene == "" {
			continue
		}
		cp := t
		r.items[t.Scene] = &cp
	}
}

// appendHistory 追加流转(调用方已持锁)
func (r *Registry) appendHistory(t Transition) {
	r.hist = append(r.hist, t)
	if len(r.hist) > historyLimit {
		r.hist = r.hist[len(r.hist)-historyLimit:]
	}
}
