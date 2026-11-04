// 持久化 + 同步器 —— M115
//
// # 三个角色的分工
//
//	Registry —— **运行期的权威状态**(谁在用哪张模板、这张模板过没过审)
//	Store    —— **快照**(DB)。进程重启后从它把状态读回来
//	Syncer   —— 把服务商侧的真实审核态拉回来,写进 Registry,再落库
//
// # ⚠️ 为什么 Store 是接口而不是直接写 gorm
//
//	这个包是**框架层**:状态机、登记册、守卫都不该跟某个数据库绑死
//	(测试里用内存实现,不用起 DB)。GORM 实现放在同包的 sqlstore.go。
package smstpl

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// ErrNoStore 没配持久化
var ErrNoStore = errors.New("smstpl: 没有配置持久化")

// Store 持久化接口
//
// ⚠️ **SaveTemplates 是"覆盖式"的** —— 传进来的每一行都会被 upsert,
//
//	没传的行不动(不会被删)。删一行是另一回事(场景下线),不在这个接口里。
type Store interface {
	// Load 读出全部登记(启动时装载用)
	Load(ctx context.Context) ([]Template, error)
	// SaveTemplates 覆盖式保存登记
	SaveTemplates(ctx context.Context, items []Template) error
	// AppendLogs 追加流转历史(**只追加,不覆盖**)
	AppendLogs(ctx context.Context, logs []Transition) error
	// ListLogs 读某个场景的流转历史(最新在前);scene 为空读全部
	ListLogs(ctx context.Context, scene string, limit int) ([]Transition, error)
}

// Fetcher 服务商侧的审核状态查询
//
// ⚠️ **实现方在别的包**(msn/aliyun)—— 这个包不认识阿里云,
//
//	只认"给我一个模板号,告诉我它过没过审"。
type Fetcher interface {
	// FetchTemplateStatus 查模板审核状态
	//
	// 返回值里的 Status 必须是**归一后的** smstpl.Status(实现方负责把
	// 服务商的原始码翻译过来);Reason 是服务商给的备注 / 驳回理由。
	//
	// ⚠️ **服务商给了一个不认识的状态码 ⇒ 返回 error** ——
	// 不许瞎猜一个状态(比如"不认识就算通过")。瞎猜的后果是
	// 一个新状态被当成 approved 写进登记册,页面显示"已通过",
	// 而实际上那张模板从来没通过审核。
	FetchTemplateStatus(ctx context.Context, templateCode string) (ProviderStatus, error)
}

// ProviderStatus 服务商侧的审核状态
type ProviderStatus struct {
	// Status 归一后的状态
	Status Status
	// Reason 服务商给的备注 / 驳回理由
	//
	// ⚠️ **状态码映射不出来时,实现方应该返回 error,而不是把原始码塞进这里** ——
	//	"服务商说了一个我们不认识的码"是**故障**(映射表漏了新状态),
	//	不是一个状态。混进 Reason 的话,页面会显示一句人话,
	//	而同步任务会以为这是一次正常的状态更新,故障就此被吞掉。
	Reason string
}

// SyncResult 一轮同步的结果
type SyncResult struct {
	// Registered 模板号变了(旧状态被作废)的场景
	Registered []string `json:"registered"`
	// Updated 状态变了(拿到新结论)的场景
	Updated []string `json:"updated"`
	// Unchanged 状态没变的场景数
	Unchanged int `json:"unchanged"`
	// Failed 拉取失败 / 状态被拒绝的场景(状态**保持原样**,没被改坏)
	Failed []string `json:"failed"`
	// Attempted 本轮**真正去查了**的场景数(配了模板号的那种)
	//
	// ⚠️ **为什么要单独记一个数** —— 判"这一轮是不是全挂了"需要它:
	//
	//	"失败数 == 尝试数" ⇒ 这一轮一个状态都没定下来 ⇒ **系统性失败**,
	//	定时任务必须记 failed(而不是"单个场景失败不影响任务"那种 success)。
	//
	// 只看 len(Failed) 是不够的 —— 一个场景都没配模板号时 Failed 也是空的,
	// 但那不是"全挂了",是"没什么可查"。
	Attempted int `json:"attempted"`
	// Failures 每个失败场景的**原因**
	//
	// ⚠️ **以前原因被扔掉了** —— 只记"哪个场景失败",不记"为什么"。
	//
	// 后果:同步全挂时,定时任务记 success(单个场景失败不算任务失败),
	// 页面上几个场景一律"未同步",**而没有任何地方写着为什么**。
	// 运维看着一条绿色的"成功"记录 + 一片"未同步",无从下手
	// —— 这正是用户 2026-10-30 报的现象。
	Failures []SceneFailure `json:"failures"`
	// Undetermined 没能确定状态的场景数(拉取失败 / 返回了不认识的状态)
	//
	// ⚠️ **与 Failed 的区别** —— Failed 里还混着"状态机按规则拒绝流转":
	//   服务商**已经给了结论**(比如"审核通过"),只是规则不让写
	//   (人工停用的模板不许被服务商复活)⇒ 那一行状态是**定下来了**的。
	//
	//   判"这一轮是不是全挂了"要看的是这个数,不是 len(Failed)。
	Undetermined int `json:"undetermined"`
}

// SceneFailure 一个场景的同步失败
type SceneFailure struct {
	// Scene 场景码
	Scene string `json:"scene"`
	// Reason 失败原因(人话)
	Reason string `json:"reason"`
}

// AllFailed 本轮是不是**一个状态都没定下来**
//
// ⚠️ **判据是"尝试数 > 0 且没定下来的数 == 尝试数"** ——
//
//	没配模板号(尝试数 0)⇒ 不是失败,是"没什么可查";
//	只挂了一个场景 ⇒ 单个场景失败,不影响其它场景(设计意图);
//	状态机按规则拒绝流转 ⇒ 状态是**定下来了**的,不算"没定下来"。
func (r SyncResult) AllFailed() bool {
	return r.Attempted > 0 && r.Undetermined == r.Attempted
}

// FailureReason 取某个场景的失败原因(没有就空串)
func (r SyncResult) FailureReason(scene string) string {
	for _, f := range r.Failures {
		if f.Scene == scene {
			return f.Reason
		}
	}
	return ""
}

// Syncer 同步器
type Syncer struct {
	reg   *Registry
	fetch Fetcher
	store Store
}

// NewSyncer 构造
func NewSyncer(reg *Registry, fetch Fetcher, store Store) *Syncer {
	return &Syncer{reg: reg, fetch: fetch, store: store}
}

// Registry 同步器用的登记册
func (s *Syncer) Registry() *Registry { return s.reg }

// SyncOnce 跑一轮同步
//
// 参数 codes 是"场景 → 模板号"(通常来自 `SceneCodes(生效配置)`)。
//
// ⚠️ **单个场景拉取失败不影响其它场景** ——
// 服务商抖一下不应该让四个场景的状态全丢。失败的那个场景:
//   - 状态**保持原样**(绝不改成 rejected / unknown —— 拉不到 ≠ 被驳回);
//   - 记进结果里的 Failed;
//   - 下一轮(10 分钟后)自动重试。
//
// ⚠️ **登记册里没有的场景不会被清掉** ——
// 运营把某个模板号清空了,登记册里那一行还在(状态是旧号的结论)。
// 这是**故意的**:清空模板号 = 停止发送,不是"忘掉这张模板"。
// 真要忘掉,靠 sync 任务里显式清理(当前不提供这个动作)。
//
// 返回值 err 只在**系统性失败**时非 nil(登记册没配 / 落库失败);
// 单个场景的失败在 result.Failed 里。
func (s *Syncer) SyncOnce(ctx context.Context, codes map[string]string) (SyncResult, error) {
	var res SyncResult
	if s == nil || s.reg == nil {
		return res, errors.New("smstpl: 同步器缺少登记册")
	}

	scenes := make([]string, 0, len(codes))
	for sc := range codes {
		scenes = append(scenes, sc)
	}
	sort.Strings(scenes)

	// 1. 登记 / 对号(模板号变了 → 旧状态作废)
	var logs []Transition
	for _, sc := range scenes {
		code := codes[sc]
		if code == "" {
			// 没配模板号的场景不进登记册(见 SceneCodes 的注释)
			continue
		}
		changed, tr, err := s.reg.Register(sc, code)
		if err != nil {
			res.Failed = append(res.Failed, sc)
			res.Failures = append(res.Failures, SceneFailure{Scene: sc, Reason: "登记失败:" + err.Error()})
			continue
		}
		if changed {
			res.Registered = append(res.Registered, sc)
		}
		if !tr.At.IsZero() {
			logs = append(logs, tr)
		}
	}

	// 2. 拉服务商状态
	for _, sc := range scenes {
		code := codes[sc]
		if code == "" {
			continue
		}
		// 这一轮真的去查了这个场景
		res.Attempted++
		if s.fetch == nil {
			res.Failed = append(res.Failed, sc)
			res.Undetermined++
			res.Failures = append(res.Failures, SceneFailure{Scene: sc, Reason: "没有配置状态查询器"})
			continue
		}
		st, err := s.fetch.FetchTemplateStatus(ctx, code)
		if err != nil {
			// ⚠️ **状态保持原样** —— 拉不到 ≠ 被驳回
			res.Failed = append(res.Failed, sc)
			res.Undetermined++
			res.Failures = append(res.Failures, SceneFailure{Scene: sc, Reason: err.Error()})
			continue
		}
		if !st.Status.Known() {
			res.Failed = append(res.Failed, sc)
			res.Undetermined++
			res.Failures = append(res.Failures, SceneFailure{Scene: sc, Reason: "服务商返回了不认识的状态"})
			continue
		}
		// ⚠️ operator 留空 —— 服务商那一行的 Source 已经是 "provider",
		// 再写一遍"阿里云"是冗余;Operator 只记"哪个运维点的"。
		changed, tr, err := s.reg.Apply(sc, st.Status, SourceProvider, st.Reason, "")
		if err != nil {
			// 典型场景:人工停用的模板,服务商说"通过" —— 状态机拒绝复活。
			// 这不是失败,是**按规则拒绝**;状态保持 disabled。
			//
			// ⚠️ **不算"没定下来"** —— 服务商的结论已经拿到了(审核通过),
			//   只是规则不让写。Undetermined 保持不动。
			res.Failed = append(res.Failed, sc)
			res.Failures = append(res.Failures, SceneFailure{Scene: sc, Reason: "状态机按规则拒绝流转:" + err.Error()})
			continue
		}
		if changed {
			res.Updated = append(res.Updated, sc)
			if !tr.At.IsZero() {
				logs = append(logs, tr)
			}
		} else {
			res.Unchanged++
		}
	}

	// 3. 落库
	if s.store != nil {
		if err := s.store.SaveTemplates(ctx, s.reg.List()); err != nil {
			return res, fmt.Errorf("smstpl: 保存模板登记失败: %w", err)
		}
		if len(logs) > 0 {
			if err := s.store.AppendLogs(ctx, logs); err != nil {
				return res, fmt.Errorf("smstpl: 写模板流转历史失败: %w", err)
			}
		}
	}
	return res, nil
}
