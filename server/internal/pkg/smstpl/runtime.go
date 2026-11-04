// 运行期装配 —— M115
//
// # 这个文件解决什么
//
// 「这个场景的短信现在能不能发」这个结论,需要四样东西凑齐:
//
//	登记册(Registry)     —— 这张模板过没过审(运行期的权威状态)
//	生效配置(ConfigResolver) —— 这个场景现在用哪个模板号(配置中心那份)
//	状态查询器(Fetcher)   —— 服务商那边怎么说(阿里云)
//	守卫策略(Policy)     —— 拦还是只告警
//
// 而它们分别住在四个地方:前两个在本包 / 第三个在 msn/aliyun(要凭证才能建)/
// 第四个在配置文件。**三处调用方**(后台管理页、每 10 分钟的同步任务、发送链路)
// 都要用同一份装配。
//
// ⇒ 这里把它们捆成一个 Runtime,让三处共用。
//
// # ⚠️ 为什么不在 service 里拼
//
// service 拼一遍、cron 任务拼一遍、发送链路拼一遍 ⇒ 配置解析规则、凭证来源、
// 策略档位各写一份,迟早漂移。而"漂移"的代价不是报错,是**行为不一致**:
// 比如后台页面按 block 判、发送链路按 off 判,运营以为自己停了模板,
// 结果短信照发。
//
// # ⚠️ 同步与人工动作要串行
//
// 同步任务每 10 分钟跑一次,后台页面随时可能有人点"停用"。
// 两者都改同一本登记册 ⇒ 必须串行,否则:
// 同步刚把状态写成 approved,人工的 disabled 紧跟着被"没变化"的判断吃掉
// (Apply 见到同状态+同理由就不记历史)。
package smstpl

import (
	"context"
	"errors"
	"sync"
	"time"

	"rapid-admin/internal/config"
)

// ErrNoFetcher 当前短信通道没有模板审核状态查询器
//
// ⚠️ **这不是故障,是"这个 provider 没这个能力"** ——
//
//	console provider 只把验证码打到日志,压根没有模板审核状态可言。
//	调用方要能区分"服务商挂了"(该重试)和"这个通道不查审核"(别重试了),
//	所以单独一个哨兵,而不是塞进通用 error。
var ErrNoFetcher = errors.New("smstpl: 当前短信通道不支持查询模板审核状态")

// ConfigResolver 取"当前生效"的短信配置
//
// base = 配置文件里那一份(打底);返回值 = 配置中心覆盖后的生效配置。
// 与 service.ConfigCenterService.ResolveSMSConfig 的签名一致 ⇒ 方法值可直接用。
type ConfigResolver func(ctx context.Context) (config.SMSConfig, error)

// FetcherFactory 按生效配置造一个服务商状态查询器
//
// ⚠️ **每轮重新造,不要缓存** —— 配置中心换过凭证之后,抱着旧 fetcher 去查,
// 查的是旧账号的模板(而且会一直查错下去)。
//
// 返回 (nil, nil) 表示"当前 provider 没有模板审核状态可查"(比如 console)。
type FetcherFactory func(ctx context.Context, cfg config.SMSConfig) (Fetcher, error)

// RuntimeOptions 装配参数
type RuntimeOptions struct {
	// Registry 登记册;为 nil 时自动建一个空的
	Registry *Registry
	// Store 持久化;为 nil 时只留内存(不落库)
	Store Store
	// Resolve 取生效配置;为 nil 时没有模板号可用(场景码表为空)
	Resolve ConfigResolver
	// NewFetcher 造状态查询器;为 nil 时同步不了(ErrNoFetcher)
	NewFetcher FetcherFactory
	// Policy 守卫策略;默认 block(见 ParsePolicy 的注释)
	Policy Policy
}

// Runtime 运行期装配
type Runtime struct {
	reg      *Registry
	store    Store
	resolve  ConfigResolver
	newFetch FetcherFactory
	policy   Policy
	guard    *Guard

	// mu 串行化「同步一轮」与「人工流转」—— 两者改同一本登记册
	mu sync.Mutex

	// failMu 保护下面两个"上一轮结果"字段
	//
	// ⚠️ **不能用 mu** —— mu 是同步/人工流转的串行锁,
	//   页面读"上次失败原因"时不能跟一轮同步互相等(同步要发起网络请求)。
	failMu sync.RWMutex
	// lastFailures 上一轮同步各场景的失败原因(scene → 原因)
	lastFailures map[string]string
	// lastSyncAt 上一轮同步的时间(零值 = 从没跑过)
	lastSyncAt time.Time
}

// NewRuntime 构造
func NewRuntime(opts RuntimeOptions) *Runtime {
	reg := opts.Registry
	if reg == nil {
		reg = NewRegistry()
	}
	policy := ParsePolicy(string(opts.Policy))
	return &Runtime{
		reg:      reg,
		store:    opts.Store,
		resolve:  opts.Resolve,
		newFetch: opts.NewFetcher,
		policy:   policy,
		guard:    NewGuard(reg, policy),
	}
}

// Registry 登记册(守卫 / 页面读的是同一个)
func (r *Runtime) Registry() *Registry {
	if r == nil {
		return nil
	}
	return r.reg
}

// Guard 发送前守卫
func (r *Runtime) Guard() *Guard {
	if r == nil {
		return nil
	}
	return r.guard
}

// Policy 守卫策略
func (r *Runtime) Policy() Policy {
	if r == nil {
		return PolicyBlock
	}
	return r.policy
}

// EffectiveConfig 取"当前生效"的短信配置
func (r *Runtime) EffectiveConfig(ctx context.Context) (config.SMSConfig, error) {
	if r == nil || r.resolve == nil {
		return config.SMSConfig{}, errors.New("smstpl: 没有配置解析器")
	}
	return r.resolve(ctx)
}

// SceneCodes 生效配置 → 场景到模板号
func (r *Runtime) SceneCodes(ctx context.Context) (map[string]string, error) {
	cfg, err := r.EffectiveConfig(ctx)
	if err != nil {
		return nil, err
	}
	return SceneCodes(cfg), nil
}

// Load 从 DB 装载登记册(启动时调**一次**)
//
// ⚠️ **启动时必须调** —— 不调的话,进程重启后登记册是空的,
// 人工停用的模板会被下一轮同步"复活"(服务商说通过 → 状态 approved)。
// 硬规则 2(人工停用的模板,服务商同步不能自动复活)在重启后就失效了。
func (r *Runtime) Load(ctx context.Context) error {
	if r == nil || r.store == nil {
		return nil
	}
	items, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	r.reg.Load(items)
	return nil
}

// SyncOnce 跑一轮同步(解析生效配置 → 造查询器 → 同步 → 落库)
//
// ⚠️ **返回值 err 只在系统性失败时非 nil** ——
//
//	单个场景拉不到状态不算失败:它进 SyncResult.Failed,状态保持原样,
//	下一轮自动重试(见 Syncer.SyncOnce 的注释)。
func (r *Runtime) SyncOnce(ctx context.Context) (SyncResult, error) {
	if r == nil {
		return SyncResult{}, errors.New("smstpl: 运行期未装配")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg, err := r.EffectiveConfig(ctx)
	if err != nil {
		return SyncResult{}, err
	}

	// ⚠️ **先问"这个通道能不能查",再问"有没有东西要查"** ——
	//
	//	顺序反了的话:console 通道 + 没配模板号 ⇒ 一句"成功,更新 0 个",
	//	运维看着像"同步过了、没问题",而真相是这个通道压根没有模板审核状态。
	//	(console 也可以配 templates.* —— dev 配置里就配了。)
	//
	//	先问能力 ⇒ 明确告诉调用方"这个通道不支持",调用方决定是提示还是跳过。
	var fetch Fetcher
	if r.newFetch != nil {
		fetch, err = r.newFetch(ctx, cfg)
		if err != nil {
			return SyncResult{}, err
		}
	}
	if fetch == nil {
		// ⚠️ 清掉上一轮的失败原因 —— 当前通道压根不支持查询,
		//   上一轮留下的"上次同步失败:xxx"已经是旧消息了。
		r.recordOutcome(SyncResult{})
		return SyncResult{}, ErrNoFetcher
	}

	codes := SceneCodes(cfg)
	if len(codes) == 0 {
		// 一个模板号都没配 ⇒ 没有可同步的东西(不是失败)
		r.recordOutcome(SyncResult{})
		return SyncResult{}, nil
	}
	res, err := NewSyncer(r.reg, fetch, r.store).SyncOnce(ctx, codes)
	// ⚠️ **失败原因必须留下来** —— 页面只显示状态("未同步")的话,
	//   运维看到的是一片"未同步"却不知道为什么。
	r.recordOutcome(res)
	return res, err
}

// recordOutcome 记下这一轮的结果(失败原因 + 时间)
func (r *Runtime) recordOutcome(res SyncResult) {
	if r == nil {
		return
	}
	r.failMu.Lock()
	defer r.failMu.Unlock()
	r.lastSyncAt = time.Now()
	r.lastFailures = make(map[string]string, len(res.Failures))
	for _, f := range res.Failures {
		r.lastFailures[f.Scene] = f.Reason
	}
}

// LastFailure 某个场景上一轮同步失败的原因(没失败就是空串)
//
// ⚠️ **只保留一轮** —— 不做历史累积:
//
//	失败原因只用来回答"上次为什么没同步上",堆历史只会让页面越来越吵。
func (r *Runtime) LastFailure(scene string) string {
	if r == nil {
		return ""
	}
	r.failMu.RLock()
	defer r.failMu.RUnlock()
	return r.lastFailures[scene]
}

// LastSyncAt 上一轮同步的时间(零值 = 从没跑过)
func (r *Runtime) LastSyncAt() time.Time {
	if r == nil {
		return time.Time{}
	}
	r.failMu.RLock()
	defer r.failMu.RUnlock()
	return r.lastSyncAt
}

// History 读某个场景的流转历史(最新在前)
//
// ⚠️ **优先读 DB,内存只作兜底** —— 内存里的历史有上限(historyLimit = 500),
//
//	而且**进程重启就没了**。落库的 sys_sms_template_log 才是完整历史。
//	没配 Store 时(单测 / 极简部署)退回内存历史。
func (r *Runtime) History(ctx context.Context, scene string, limit int) ([]Transition, error) {
	if r == nil {
		return nil, errors.New("smstpl: 运行期未装配")
	}
	if r.store != nil {
		return r.store.ListLogs(ctx, scene, limit)
	}
	if scene == "" {
		return r.reg.AllHistory(), nil
	}
	return r.reg.History(scene), nil
}

// ApplyManual 人工流转(停用 / 恢复)并落库
//
// ⚠️ **人工不能判"通过/驳回"** —— 目标状态只可能是 pending / disabled,
// 状态机(machine.go)会再挡一次;这里不做额外判断,让状态机当唯一判据。
//
// ⚠️ **换号时先对号再流转** ——
//
//	配置中心把模板号换了(SMS_1 → SMS_2),登记册里的快照还是旧号。
//	这时候点"停用":不先对号的话,登记册停在 SMS_1/disabled,
//	而发送时用的是 SMS_222 ⇒ 守卫按"号不一致 ⇒ 放行"处理
//	(guard.go 第 3 条口径)⇒ **运营点了停用却拦不住任何东西**。
//	Register 在换号时会把状态重置成 unknown —— 这正是要的:
//	旧号的通过/驳回结论不能担保新号。
//
// ⚠️ **生效配置里没有模板号时不动登记册的快照** ——
//
//	与同步任务同一口径:"清空模板号 = 停止发送,不是忘掉这张模板"。
//	(同步任务只遍历配了号 的场景,不会把老号抹掉。)
func (r *Runtime) ApplyManual(ctx context.Context, scene string, to Status, reason, operator string) (Template, error) {
	if r == nil {
		return Template{}, errors.New("smstpl: 运行期未装配")
	}
	if scene == "" {
		return Template{}, ErrEmptyScene
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	codes, err := r.SceneCodes(ctx)
	if err != nil {
		return Template{}, err
	}
	code := codes[scene]

	var logs []Transition
	cur, ok := r.reg.Get(scene)
	switch {
	case !ok:
		// 第一次登记(登记册里还没有这个场景)
		if _, _, err := r.reg.Register(scene, code); err != nil {
			return Template{}, err
		}
	case code != "" && cur.Code != code:
		// 配置换了号 ⇒ 先对号(旧结论作废),再走流转
		_, tr, err := r.reg.Register(scene, code)
		if err != nil {
			return Template{}, err
		}
		if !tr.At.IsZero() {
			logs = append(logs, tr)
		}
	}

	changed, tr, err := r.reg.Apply(scene, to, SourceManual, reason, operator)
	if err != nil {
		return Template{}, err
	}
	if changed && !tr.At.IsZero() {
		logs = append(logs, tr)
	}

	if r.store != nil {
		if err := r.store.SaveTemplates(ctx, r.reg.List()); err != nil {
			return Template{}, err
		}
		if len(logs) > 0 {
			if err := r.store.AppendLogs(ctx, logs); err != nil {
				return Template{}, err
			}
		}
	}
	tpl, _ := r.reg.Get(scene)
	return tpl, nil
}
