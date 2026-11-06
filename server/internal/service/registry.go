// ServiceRegistry service 总注册(由 main.go 注入)
package service

import (
	"rapid-admin/internal/pkg/emailx"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/pushx"
)

// Registry 所有 service 的集中访问点(handler 通过这个取)
type Registry struct {
	Auth   AuthService
	User   UserService
	Role   RoleService
	Audit  AuditService
	Config ConfigCenterService
	// File / Folder / Upload 是 M80 的文件系统三件套。
	//
	// ⚠️ **Upload 不再是"发个 OSS 凭证"的占位 service** —— M76 之前它是,
	// 现在它是分片上传会话管理(设计文档 §8.2)。取直传凭证走的是
	// `File.UploadToken`,取私有下载 URL 走的是 `File.URL`,两条老路由
	// (`POST /biz/uploads/token` / `GET /biz/uploads/url`)已随之退役。
	File       FileService
	Folder     FolderService
	Upload     UploadService
	Dashboard  DashboardService
	WxTemplate WxTemplateService
	Guide      GuideService
	Captcha    CaptchaService
	// VoiceClone 音色克隆台账(语音能力,voicex)
	VoiceClone VoiceCloneService
	// TtsVoice TTS 音色登记册 + 文本合成(语音能力,voicex)
	TtsVoice TtsVoiceService
	// Asr 语音识别(语音能力,asrx:一句话 + 实时会话)
	Asr AsrService
	// §1.9 #11+#12 P1 —— 站内信 + 通知中心
	Inbox InboxService
	// Job 定时任务管理(M68)。与其它 service 不同,它只读 cronx 的内存态:
	// 任务注册表在代码里,DB 只有运行态覆盖,所以这里没有 CRUD 只有覆盖更新。
	Job JobService
	// SmsTemplate 短信模板审核状态(M115)。
	//
	// ⚠️ **它背后是 pkg/smstpl 的运行期装配(Runtime)** ——
	// 登记册 / 守卫 / 同步器都在那儿,后台页面、同步任务、发送链路共用同一份。
	// 这里只做"把人工动作翻译成状态机认可的流转"。
	SmsTemplate SmsTemplateService
	// InboxNotifier 业务模块投递站内信入口(nil 时业务降级为不投递)
	//
	// ⚠️ **装配期塞的是 notifyx.Fanout,不是 inbox.Manager** ——
	//   Fanout.inner 是 inbox.Manager,站内信语义完全不变,
	//   但业务侧任何一次投递都会顺带走过额外交付通道(邮件)。
	//   通道装在**投递口**上,而不是让每个业务模块自己记得发邮件。
	InboxNotifier inbox.Notifier
	// §1.9 #9 P2 —— 邮件通道本体
	//
	// ⚠️ **可能是 nil**(email.provider 为空串 / 未知值)——
	//   业务侧拿到 nil 应当**降级为不投递**,不要 panic。
	//   站内信那条路(InboxNotifier)是独立的,邮件没了站内信照发。
	EmailSender emailx.Sender
	// §1.9 #10 P2 —— App 推送通道本体
	//
	// ⚠️ **同样是 nil 可能**;而且推送要的是**设备令牌**,
	//   全仓还没有那张登记册 —— 业务侧拿到它之前先别用。
	PushSender pushx.Sender
	// 通用审批系统(M86)。**三个 service 各管一段,不要合并**:
	//   - ApprovalFlow  流程模板 CRUD(配置面)
	//   - Approval      审批单查询 + 建草稿(读面)
	//   - ApprovalEngine 写面(提交/审批/作废/改草稿)—— 走实例行锁(§3.5)
	//
	// ⚠️ **ApprovalEngine 挂在 registry 上是因为四个写端点要走它**,
	// 而它们和查询不在同一个 service 里(查询是纯读的,写要加锁)。
	Approval       ApprovalService
	ApprovalFlow   ApprovalFlowService
	ApprovalEngine ApprovalEngine
	// Dyn 动态业务集合(M150)—— 用户在「业务表单设计器」里设计的业务表。
	//
	// ⚠️ **一个 service 管两面** —— 设计面(集合/字段的增删改 = 建表加列)
	// 与数据面(记录的增删改查)共用同一份字段定义,
	// 拆成两个就得让其中一个去查另一个的私有方法。
	Dyn DynService
	// Scope 数据范围(行级权限)规则(M155)。
	//
	// ⚠️ **它与 Role 不是一回事** ——
	// Role 管"这个角色能不能调这个接口"(sys_role_resource + casbin),
	// Scope 管"接口放行后他能看到哪些行"(sys_data_scope)。
	// 两者分开是刻意的:混在一起就会出现
	// "以为配了数据范围就等于限了数据"的误解。
	Scope ScopeService
	// FieldPolicy 字段级可见性(列级权限)规则(M162)。
	//
	// ⚠️ **它是 Scope 的姊妹轴,不是替代品** ——
	// Scope 管"接口放行后他能看到哪些**行**"(sys_data_scope),
	// FieldPolicy 管"那一行里的哪些**字段**给他看"(sys_field_policy)。
	// 能看到某行 ≠ 能看到那行的每个字段。
	//
	// ⚠️ **它只收窄,不放宽** ——
	// 它能把已经在响应里的字段打码或删掉,
	// 但**不能**把本来就不在响应里的字段(如 `json:"-"` 的加密手机号)变出来。
	FieldPolicy FieldPolicyService
}
