// SmsTemplateService 短信模板审核状态 —— M115
//
// # 分工
//
//	pkg/smstpl  —— 状态机 / 登记册 / 守卫 / 同步器(框架层,不碰业务)
//	本 service  —— 编排:把"人工动作"翻译成状态机认可的流转,把登记册翻译成页面能读的视图
//
// # ⚠️ 三条硬规则在本层的落法
//
//  1. **只有服务商能判"通过/驳回"** —— 本层的人工写通道只有两个:
//     Disable(→ disabled)与 Enable(→ pending)。没有任何一个接口能把状态置成
//     approved / rejected。状态机(machine.go)还会再挡一次,双保险。
//  2. **人工停用的模板,服务商同步不能自动复活** —— 本层不提供"强制恢复"通道。
//     Enable 走的是"→ pending"(不是 approved),所以它**不会**伪造一个通过结论。
//  3. **换了模板号,原审核状态立即作废** —— ApplyManual 在动手前先对号
//     (见 smstpl.Runtime.ApplyManual 的注释):否则运营点了"停用"却拦不住任何东西。
//
// # ⚠️ 为什么"停用"要在生效配置里配了模板号才允许
//
//	没配模板号的场景本来就不会发短信,给它建一条登记记录没有意义;
//	更糟的是:登记册里那条空号记录会在**运营后来配上模板号**的那一刻
//	被 Register 判成"换号" ⇒ 状态重置 ⇒ **刚点的停用凭空消失**。
//	⇒ 没号就拒绝,并把话说明白。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/smstpl"
)

// SmsTemplateService 短信模板审核状态
type SmsTemplateService interface {
	// List 全部场景的模板状态(固定四个场景,顺序稳定)
	List(ctx context.Context) (*SmsTemplateListView, error)
	// Get 单个场景的详情
	Get(ctx context.Context, scene string) (*SmsTemplateView, error)
	// Disable 人工停用某个场景的模板(止血手段)
	Disable(ctx context.Context, caller *Caller, scene, reason string) (*SmsTemplateView, error)
	// Enable 恢复某个场景的模板
	//
	// ⚠️ **恢复 = 置为"待审核"(pending),不是"已通过"(approved)** ——
	// 人工无权判定通过(硬规则 1)。恢复后下一次同步会把服务商的真实结论带回来。
	Enable(ctx context.Context, caller *Caller, scene, reason string) (*SmsTemplateView, error)
	// Sync 手动跑一轮同步(与定时任务同一个入口)
	Sync(ctx context.Context) (*SmsTemplateSyncView, error)
	// History 某个场景的流转历史(最新在前)
	History(ctx context.Context, scene string, limit int) ([]SmsTemplateHistoryView, error)
}

// ─────────── View DTO ───────────

// SmsTemplateListView 列表响应
//
// ⚠️ **不是裸数组** —— 守卫策略与当前通道是**进程级**状态,
// 每个场景一样,放数组里就得让前端去 items[0] 里取(空数组时取不到)。
type SmsTemplateListView struct {
	Items []SmsTemplateView `json:"items"`
	// Policy 守卫策略 off / warn / block
	Policy string `json:"policy"`
	// Provider 当前生效的短信通道
	Provider string `json:"provider"`
}

// SmsTemplateView 单个场景的模板状态
type SmsTemplateView struct {
	Scene      string `json:"scene"`
	SceneLabel string `json:"scene_label"`

	// TemplateCode 当前生效的模板号(配置中心那份)
	TemplateCode string `json:"template_code"`
	// RegisteredCode 登记册里登记的模板号(上一次同步时的快照)
	RegisteredCode string `json:"registered_code"`
	// CodeMismatch 两个号不一致 —— 登记册里的审核结论**不适用于当前这个号**,
	// 守卫按"不确定"放行(见 smstpl.Guard.Evaluate 的第 3 条口径)
	CodeMismatch bool `json:"code_mismatch"`
	// Configured 配置中心里配了模板号
	Configured bool `json:"configured"`
	// Registered 登记册里有这一行(同步过一次)
	Registered bool `json:"registered"`

	// Status 审核状态(unknown / pending / reviewing / approved / rejected / cancelled / disabled)
	//
	// ⚠️ **中文标签不在这里返** —— 由前端按字典组 sms_template_status 渲染
	// (与 wxtemplate_status 同一条规矩:字典是 enum 的中央仓库)。
	Status string `json:"status"`
	// Reason 服务商给的备注 / 驳回理由,或人工填的原因
	Reason string `json:"reason"`
	// Source 当前状态是谁给的(manual / provider)
	Source string `json:"source"`

	// Blocked 确定发不出去(守卫会拦:rejected / cancelled / disabled)
	Blocked bool `json:"blocked"`
	// Usable 审核通过(这张模板可以用)
	Usable bool `json:"usable"`

	// ReviewedAt 服务商最后一次确认这个状态的时间(RFC3339,空串 = 从未确认过)
	ReviewedAt string `json:"reviewed_at"`
	// UpdatedAt 登记册这一行最后变动的时间(RFC3339)
	UpdatedAt string `json:"updated_at"`

	// LastError 上一轮同步这个场景**失败的原因**(空串 = 上一轮没失败)
	//
	// ⚠️ **为什么要有这个字段** —— 页面上只显示状态("未同步")的话,
	//   运维看到的是一片"未同步",**却不知道为什么**。
	//   有了它,页面能直接说"上次同步失败:模板号在阿里云不存在"这种话。
	//
	// ⚠️ **只保留最近一轮** —— 不做历史累积(见 smstpl.Runtime.LastFailure)。
	LastError string `json:"last_error"`
}

// SmsTemplateHistoryView 一条流转历史
type SmsTemplateHistoryView struct {
	Scene        string `json:"scene"`
	SceneLabel   string `json:"scene_label"`
	TemplateCode string `json:"template_code"`
	// From 流转前的状态
	From string `json:"from"`
	// To 流转后的状态
	To string `json:"to"`
	// Source manual / provider
	Source string `json:"source"`
	// Operator 操作人(人工流转时 = 运维的登录名;服务商流转时为空)
	Operator string `json:"operator"`
	// Reason 原因 / 备注
	Reason string `json:"reason"`
	// At 发生时间(RFC3339)
	At string `json:"at"`
}

// SmsTemplateSyncView 一轮同步的结果
type SmsTemplateSyncView struct {
	// Registered 模板号变了(旧状态被作废)的场景
	Registered []string `json:"registered"`
	// Updated 状态变了(拿到新结论)的场景
	Updated []string `json:"updated"`
	// Unchanged 状态没变的场景数
	Unchanged int `json:"unchanged"`
	// Failed 拉取失败 / 状态被拒绝的场景(状态保持原样)
	Failed []string `json:"failed"`
	// Failures 每个失败场景的**原因**
	//
	// ⚠️ **以前只有场景名,没有原因** —— 前端只能弹一句
	//   "部分场景没能确定状态:login,reset_password",
	//   运维还是不知道为什么。加上原因之后,提示里能带上一句人话。
	Failures []SmsTemplateSyncFailure `json:"failures"`
}

// SmsTemplateSyncFailure 一个场景的同步失败
type SmsTemplateSyncFailure struct {
	// Scene 场景码
	Scene string `json:"scene"`
	// Reason 失败原因
	Reason string `json:"reason"`
}

// ─────────── 实现 ───────────

type smsTemplateService struct{ rt *smstpl.Runtime }

// NewSmsTemplateService 构造
func NewSmsTemplateService(rt *smstpl.Runtime) SmsTemplateService {
	return &smsTemplateService{rt: rt}
}

// maxReasonLen 原因字段的上限
//
// ⚠️ **和 DB 列宽对齐**(sys_sms_template_log.reason 是 size:500)——
// 超长会被 MySQL 截断(严格模式直接报错),而截断掉的是"为什么停用"这句话。
const maxReasonLen = 500

func (s *smsTemplateService) List(ctx context.Context) (*SmsTemplateListView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	cfg, err := s.rt.EffectiveConfig(ctx)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "读取短信配置失败").WithDetail(err.Error())
	}
	codes := smstpl.SceneCodes(cfg)
	out := &SmsTemplateListView{
		Items:    make([]SmsTemplateView, 0, len(smstpl.Scenes)),
		Policy:   string(s.rt.Policy()),
		Provider: cfg.Provider,
	}
	for _, sc := range smstpl.Scenes {
		out.Items = append(out.Items, s.view(sc, codes[sc]))
	}
	return out, nil
}

func (s *smsTemplateService) Get(ctx context.Context, scene string) (*SmsTemplateView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if !validSmsScene(scene) {
		return nil, errcode.New(errcode.BadRequest, "未知的短信场景:"+scene)
	}
	cfg, err := s.rt.EffectiveConfig(ctx)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "读取短信配置失败").WithDetail(err.Error())
	}
	v := s.view(scene, smstpl.SceneCodes(cfg)[scene])
	return &v, nil
}

func (s *smsTemplateService) Disable(ctx context.Context, caller *Caller, scene, reason string) (*SmsTemplateView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if !validSmsScene(scene) {
		return nil, errcode.New(errcode.BadRequest, "未知的短信场景:"+scene)
	}
	code, err := s.requireTemplate(ctx, scene)
	if err != nil {
		return nil, err
	}
	tpl, err := s.rt.ApplyManual(ctx, scene, smstpl.StatusDisabled, trimReason(reason), operatorName(caller))
	if err != nil {
		return nil, mapSmsTemplateError(err)
	}
	v := s.viewFrom(scene, tpl, code)
	return &v, nil
}

func (s *smsTemplateService) Enable(ctx context.Context, caller *Caller, scene, reason string) (*SmsTemplateView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if !validSmsScene(scene) {
		return nil, errcode.New(errcode.BadRequest, "未知的短信场景:"+scene)
	}
	code, err := s.requireTemplate(ctx, scene)
	if err != nil {
		return nil, err
	}
	// ⚠️ **只有停用状态才谈得上"恢复"** —— 否则这个接口会变成
	// "把 approved 降级成 pending"的暗门(approved → pending 在状态机里是合法的,
	// 于是运营误点一次"恢复",刚通过的模板就掉回"待审核",下一次同步前守卫
	// 依然放行,但页面显示的状态已经不对了)。
	cur, ok := s.rt.Registry().Get(scene)
	if !ok {
		return nil, errcode.New(errcode.NotFound, "该场景尚未登记模板,无需恢复")
	}
	if cur.Status != smstpl.StatusDisabled {
		return nil, errcode.New(errcode.StateInvalid,
			"该场景当前不是停用状态,无需恢复")
	}
	tpl, err := s.rt.ApplyManual(ctx, scene, smstpl.StatusPending, trimReason(reason), operatorName(caller))
	if err != nil {
		return nil, mapSmsTemplateError(err)
	}
	v := s.viewFrom(scene, tpl, code)
	return &v, nil
}

func (s *smsTemplateService) Sync(ctx context.Context) (*SmsTemplateSyncView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	// 先取一次生效配置:失败时的提示里要带上"是哪个通道不支持"
	provider := ""
	if cfg, err := s.rt.EffectiveConfig(ctx); err == nil {
		provider = cfg.Provider
	}
	res, err := s.rt.SyncOnce(ctx)
	if err != nil {
		if errors.Is(err, smstpl.ErrNoFetcher) {
			return nil, errcode.New(errcode.BadRequest,
				"当前短信通道("+provider+")不支持查询模板审核状态")
		}
		return nil, errcode.New(errcode.Internal, "同步模板审核状态失败").WithDetail(err.Error())
	}
	return &SmsTemplateSyncView{
		Registered: nonNil(res.Registered),
		Updated:    nonNil(res.Updated),
		Unchanged:  res.Unchanged,
		Failed:     nonNil(res.Failed),
		Failures:   syncFailures(res.Failures),
	}, nil
}

// syncFailures 框架层的失败原因 → service 层的视图
func syncFailures(in []smstpl.SceneFailure) []SmsTemplateSyncFailure {
	out := make([]SmsTemplateSyncFailure, 0, len(in))
	for _, f := range in {
		out = append(out, SmsTemplateSyncFailure{Scene: f.Scene, Reason: f.Reason})
	}
	return out
}

func (s *smsTemplateService) History(ctx context.Context, scene string, limit int) ([]SmsTemplateHistoryView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if !validSmsScene(scene) {
		return nil, errcode.New(errcode.BadRequest, "未知的短信场景:"+scene)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	trs, err := s.rt.History(ctx, scene, limit)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "读取模板流转历史失败").WithDetail(err.Error())
	}
	out := make([]SmsTemplateHistoryView, 0, len(trs))
	for _, t := range trs {
		out = append(out, SmsTemplateHistoryView{
			Scene:        t.Scene,
			SceneLabel:   smstpl.SceneLabel(t.Scene),
			TemplateCode: t.Code,
			From:         string(t.From),
			To:           string(t.To),
			Source:       string(t.Source),
			Operator:     t.Operator,
			Reason:       t.Reason,
			At:           formatSmsTime(t.At),
		})
	}
	return out, nil
}

// ready 运行期没装配时统一返回"服务不可用"
//
// ⚠️ **不是 panic 也不是 500** —— "短信模板审核流没装配"是一个**部署状态**
// (比如某个极简部署没起 DB),不是代码炸了。503 + 明确文案比 500 好定位。
func (s *smsTemplateService) ready() error {
	if s == nil || s.rt == nil {
		return errcode.New(errcode.ServiceUnavailable, "短信模板审核流未装配")
	}
	return nil
}

// requireTemplate 这个场景有没有"可停用的模板"
//
// ⚠️ **判据是"登记册里有号"或"生效配置里有号"** —— 两者取或:
//
//	登记册有号 ⇒ 之前配过(哪怕现在配置中心读不到),停用它有意义;
//	配置有号   ⇒ 从没同步过,但号是真的,停用它也有意义。
//
//	两者都没有 ⇒ 这个场景压根不发短信,建登记记录只会制造上面注释里说的
//	"停用凭空消失"。
//
// 返回值是生效配置里的模板号(可能为空串 —— 那表示"只有登记册快照里有号")。
func (s *smsTemplateService) requireTemplate(ctx context.Context, scene string) (string, error) {
	codes, err := s.rt.SceneCodes(ctx)
	if err != nil {
		return "", errcode.New(errcode.Internal, "读取短信配置失败").WithDetail(err.Error())
	}
	if codes[scene] != "" {
		return codes[scene], nil
	}
	// 配置中心现在没号,但登记册里还留着上一次同步时的号 ——
	// 说明之前配过(可能刚被清空),停用它依然有意义。
	if tpl, ok := s.rt.Registry().Get(scene); ok && tpl.Code != "" {
		return "", nil
	}
	return "", errcode.New(errcode.BadRequest, "该场景未配置模板号,没有可停用的模板")
}

// view 组装一个场景的视图
func (s *smsTemplateService) view(scene, effectiveCode string) SmsTemplateView {
	tpl, ok := s.rt.Registry().Get(scene)
	if !ok {
		return SmsTemplateView{
			Scene:        scene,
			SceneLabel:   smstpl.SceneLabel(scene),
			TemplateCode: effectiveCode,
			Configured:   effectiveCode != "",
			Status:       string(smstpl.StatusUnknown),
			LastError:    s.rt.LastFailure(scene),
		}
	}
	return s.viewFrom(scene, tpl, effectiveCode)
}

// viewFrom 用登记册里的一条 + 生效模板号组装视图
func (s *smsTemplateService) viewFrom(scene string, tpl smstpl.Template, effectiveCode string) SmsTemplateView {
	v := SmsTemplateView{
		Scene:          scene,
		SceneLabel:     smstpl.SceneLabel(scene),
		TemplateCode:   effectiveCode,
		RegisteredCode: tpl.Code,
		Registered:     true,
		Configured:     effectiveCode != "",
		CodeMismatch:   effectiveCode != "" && tpl.Code != "" && effectiveCode != tpl.Code,
		Status:         string(tpl.Status),
		Reason:         tpl.Reason,
		Source:         string(tpl.Source),
		Blocked:        tpl.Status.Blocked(),
		Usable:         tpl.Status.Usable(),
		ReviewedAt:     formatSmsTime(tpl.ReviewedAt),
		UpdatedAt:      formatSmsTime(tpl.UpdatedAt),
		LastError:      s.rt.LastFailure(scene),
	}
	// 配置中心读不到时(effective 为空),退回登记册快照 ——
	// 页面上至少要看得见"上一次同步时用的那个号",而不是一片空白。
	if v.TemplateCode == "" {
		v.TemplateCode = tpl.Code
	}
	return v
}

// validSmsScene 场景名是否合法
func validSmsScene(scene string) bool {
	for _, sc := range smstpl.Scenes {
		if sc == scene {
			return true
		}
	}
	return false
}

// operatorName 取操作人登录名
func operatorName(caller *Caller) string {
	if caller == nil {
		return ""
	}
	return caller.Username
}

// trimReason 规整人工填的原因(去空白 + 截断到列宽)
func trimReason(reason string) string {
	return truncateRunes(strings.TrimSpace(reason), maxReasonLen)
}

// truncateRunes 按字符截断(不是按字节 —— 按字节会把中文切成半个字)
func truncateRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}

// formatSmsTime 时间 → RFC3339(零值 = 空串)
//
// ⚠️ **零值返空串而不是 1970** —— 页面上显示"1970-01-01T00:00:00Z"很吓人,
// 而"从未确认过"用空串表达更准确。
func formatSmsTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// nonNil 空切片返空切片(JSON 里是 [] 而不是 null)
func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// mapSmsTemplateError 把状态机的哨兵错误映射到本仓 errcode
//
// ⚠️ **三类错误的处置完全不同,不能合并成一句"操作失败"** ——
//
//	ErrManualCannotJudge   → 人工想判通过/驳回:这是**设计约束**,要说明白
//	ErrIllegalTransition    → 状态机不认:多半是并发(两个人同时点)
//	ErrProviderCannotRevive → 服务商想复活人工停用的:这是硬规则 2 在起作用
func mapSmsTemplateError(err error) error {
	switch {
	case errors.Is(err, smstpl.ErrManualCannotJudge):
		return errcode.New(errcode.StateInvalid,
			"人工不能把模板标成已通过/已驳回(审核结论只能来自服务商)")
	case errors.Is(err, smstpl.ErrProviderCannotRevive):
		return errcode.New(errcode.StateInvalid,
			"人工停用的模板,服务商同步不会自动恢复(需要人工点恢复)")
	case errors.Is(err, smstpl.ErrIllegalTransition):
		return errcode.New(errcode.StateInvalid, "当前状态不允许该操作").WithDetail(err.Error())
	case errors.Is(err, smstpl.ErrEmptyScene):
		return errcode.New(errcode.BadRequest, "场景不能为空")
	case errors.Is(err, smstpl.ErrNotRegistered):
		return errcode.New(errcode.NotFound, "该场景尚未登记模板")
	}
	return errcode.New(errcode.Internal, "操作失败").WithDetail(err.Error())
}
