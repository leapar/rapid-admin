// AuthService 实现 —— Login 完整版,其余方法先 stub
// 文档 §8.1
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/desensitize"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/loginlock"
	"rapid-admin/internal/pkg/msn"
	"rapid-admin/internal/pkg/smslimit"
	"rapid-admin/internal/pkg/sonyflake"
	"rapid-admin/internal/pkg/uainfo"
)

// sha256Hex 计算 SHA-256 并返回十六进制字符串
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// invalidateUserSessions 推进 userID 的作废线,作废该时刻之前签发的全部令牌。
// 改密码 / 重置密码的强制下线唯一落点(见 pkg/auth/invalidate.go)。
//
// # 为什么是"这个用户全部"而不是"当前这一张"
//
// 逐 jti 吊销能处理"眼前这一张",而改密码事故里真正要踢掉的恰恰是**别的**那几张
// —— 盗号者的会话。用户发现自己被盗后做的第一件事就是改密码,而只踢当前这张的
// 实现等于告诉用户"你做了该做的事,但没用"。
//
// # 为什么只记日志不返错
//
// 调用点都是"密码已经在库里改成功了"之后才走到这里。此时返错会让用户看到"改密
// 失败"、拿着已经被作废的旧密码重试,于是他以为自己没改成功、于是保留旧密码 ——
// 把一次成功的安全操作变成一次失败的体验,而且他还留在被改过密码的旧会话里。
// 所以:写不进去就 ERROR 记死,让告警能捞到,不改变接口的成功/失败语义。
func invalidateUserSessions(ctx context.Context, userID uint64, why string) {
	if userID == 0 {
		return
	}
	if err := auth.DefaultRevocation().InvalidateFrom(ctx, userID, time.Now()); err != nil {
		logger.L().Error("推进作废线失败:该用户的存量会话未被强制下线",
			zap.Uint64("user_id", userID), zap.String("why", why), zap.Error(err))
	}
}

// AuthService 鉴权服务实现
type authService struct {
	db        *gorm.DB
	jwt       *auth.Multi
	sms       msn.Sender
	smsCfg    config.SMSConfig
	lock      time.Duration // 登录失败锁定时长
	codeRand  *rand.Rand
	codeRandM sync.Mutex
	// touch 会话活动节流闸(M117-4)
	//
	// ⚠️ **零值可用** —— touchGate 内部惰性建 LRU,
	// 所以漏初始化只是"闸门永远开着"(多写几次库),不会 panic、也不会静默不写。
	touch touchGate
	// anomalyCfg 异常登录检测配置(M128)
	//
	// ⚠️ **零值 = 检测关闭**(Enabled 的 bool 零值是 false)。
	//   所以它必须由 NewAuthService **显式传进来** —— 漏传的后果是
	//   "检测静默不跑",而症状与"跑了但没发现异常"完全无法区分。
	//   这就是它做成**必填构造参数**、而不是可选 setter 的原因。
	anomalyCfg config.AnomalyConfig
	// inbox 站内信通道(M128)—— 给异常登录的用户可见提醒。
	//
	// 可以为 nil(单测 / 精简部署)⇒ 只记日志,不投递站内信。
	inbox inbox.Notifier
}

// NewAuthService 构造
//
// ⚠️ anomalyCfg / inbox 是**必填参数**(M128),不是可选 setter。
//
// 理由:异常登录检测一旦接进登录链路,"忘了接"的后果是**静默不跑**,
// 而症状("日志里一条异常都没有")与"跑了但没发现异常"**完全无法区分** ——
// 这是最难查的一类故障。做成必填参数 ⇒ 漏接是**编译错误**。
func NewAuthService(
	db *gorm.DB,
	jwt *auth.Multi,
	smsSender msn.Sender,
	smsCfg config.SMSConfig,
	anomalyCfg config.AnomalyConfig,
	inboxNotifier inbox.Notifier,
) AuthService {
	return &authService{
		db:         db,
		jwt:        jwt,
		sms:        smsSender,
		smsCfg:     smsCfg,
		anomalyCfg: anomalyCfg,
		inbox:      inboxNotifier,
		lock:       15 * time.Minute,
		codeRand:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// ===== Login =====

// Login 登录流程
//  1. 校验必填 + 登录失败锁定(Redis,本地 dev 无 Redis 时降级)
//  2. 按 username 查 sys_user
//  3. 检查 LockedUntil / IsActive / IsLeave
//  4. 校验密码(明文 = SHA-256(前端明文),库 = bcrypt(SHA-256))
//  5. 加载身份列表,选主身份(若没有则用首个 active 身份)
//  6. 写 sys_login_log + 更新 LastLoginAt/LastLoginIP/FailedCount
//  7. 签发 access + refresh token
//  8. 清登录失败计数
func (s *authService) Login(ctx context.Context, req LoginRequest) (*AuthTokens, error) {
	if err := MustNoEmpty("username", req.Username); err != nil {
		return nil, err
	}
	if err := MustNoEmpty("password", req.Password); err != nil {
		return nil, err
	}

	// 1. 登录失败锁定预检(Redis 不可用时降级放行)
	if err := loginlock.PreCheck(ctx, req.Username, req.LoginIP, 10); err != nil {
		s.recordLoginLog(ctx, 0, req.Username, req, 0, err.Error(), "")
		return nil, err
	}

	// 2. 查用户
	var u model.SysUser
	err := s.db.WithContext(ctx).
		Where("username = ?", req.Username).
		First(&u).Error
	if err != nil {
		// 用户不存在 + 密码错 都返回 LoginFailed,避免账号枚举
		if err == gorm.ErrRecordNotFound {
			loginlock.AfterFail(ctx, req.Username, req.LoginIP, s.lock)
			s.recordLoginLog(ctx, 0, req.Username, req, 0, "用户不存在", "")
			return nil, errcode.New(errcode.LoginFailed, "账号或密码错误")
		}
		return nil, errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}

	// 3. 状态检查
	if u.IsLeave {
		loginlock.AfterFail(ctx, req.Username, req.LoginIP, s.lock)
		s.recordLoginLog(ctx, u.ID, u.Username, req, 0, "已离职", "")
		return nil, errcode.New(errcode.NoActiveIdentity, "账号已离职,无法登录")
	}
	if !u.IsActive {
		loginlock.AfterFail(ctx, req.Username, req.LoginIP, s.lock)
		s.recordLoginLog(ctx, u.ID, u.Username, req, 0, "已停用", "")
		return nil, errcode.New(errcode.NoActiveIdentity, "账号已停用")
	}
	if u.LockedUntil != nil && !u.LockedUntil.IsZero() && u.LockedUntil.Time.After(time.Now()) {
		s.recordLoginLog(ctx, u.ID, u.Username, req, 0, "账号已锁定", "")
		return nil, errcode.New(errcode.UserLocked, "账号已锁定,请稍后再试")
	}

	// 4. 校验密码
	//    前端契约:web/src/utils/crypto.ts preHashPassword
	//      preHashPassword(plain) = sha256("rapid-admin::" + plain) → hex
	//    前端已做一次 SHA-256,服务端不再二次 hash,直接 bcrypt-compare
	//    seed 时也按同样规则:bcrypt(sha256("rapid-admin::" + 明文))
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		loginlock.AfterFail(ctx, req.Username, req.LoginIP, s.lock)
		// 失败次数 +1;达到阈值本地落库锁定(无 Redis 时也兜底)
		newCount := u.FailedCount + 1
		updates := map[string]any{"failed_count": newCount}
		if newCount >= 10 {
			until := time.Now().Add(s.lock)
			updates["locked_until"] = model.Time{Time: until}
		}
		s.db.WithContext(ctx).Model(&model.SysUser{}).
			Where("id = ?", u.ID).Updates(updates)
		s.recordLoginLog(ctx, u.ID, u.Username, req, 0, "密码错误", "")
		return nil, errcode.New(errcode.LoginFailed, "账号或密码错误")
	}

	// 4.5 登录二次验证 MFA(M150)
	//
	//   位置在**密码验对之后、签发 token 之前**:发码权 = "知道密码",
	//   密码不对的人连"能不能发码"都轮不到谈(它也挡住了拿 MFA 发码做
	//   账号枚举 —— 用户不存在走不到这里)。
	//
	//   三种走向:
	//     a) 没绑手机号 → **fail-closed**(MFA 开着却验不了 = 不放行),
	//        报错点名 recovery 路径(管理员关 MFA),不留"永久锁死且无人知晓"的账。
	//     b) 没带码 → 自动发一条(scene=mfa,发到登记手机号)+ 返 MfaRequired(20109)。
	//        它**不是登录失败**:不进 loginlock 计数、不记"密码错误"。
	//        发码撞冷却(SmsSendTooFrequent)不算错 —— 上一条码大概率还没用,
	//        照样返 20109,用户把那条码填上就行。
	//     c) 带了码 → 消费(scene=mfa + 登记手机号,单次性,见 consumeSmsCode),
	//        错码按**登录失败**处理(计数 + 锁定):对 MFA 步骤暴力试码,
	//        与对密码暴力试码是同一种攻击。
	if u.MfaEnabled {
		phone := decryptPII(u.Phone)
		if phone == "" {
			s.recordLoginLog(ctx, u.ID, u.Username, req, 0, "mfa_enabled_no_phone", "")
			return nil, errcode.New(errcode.MfaRequired,
				"该账号已开启二次验证但未绑定手机号,请联系管理员关闭二次验证")
		}
		if req.MfaCode == "" {
			if err := s.SendSms(ctx, phone, model.SmsSceneMFA); err != nil {
				// 冷却期内不拦 —— 十有八九是上一条码还在(用户刚重试过);
				// 其它发送失败(模板被服务商驳回 / 通道故障)必须如实报,
				// 静默返 20109 会让用户对着一个永远不会到达的码干等。
				if errcode.CodeOf(err) != errcode.SmsSendTooFrequent {
					return nil, err
				}
			}
			s.recordLoginLog(ctx, u.ID, u.Username, req, 0, "mfa_challenge", "")
			return nil, errcode.New(errcode.MfaRequired, "该账号已开启二次验证,请输入短信验证码")
		}
		if err := s.consumeSmsCode(ctx, phone, model.SmsSceneMFA, req.MfaCode); err != nil {
			// 与"密码错误"分支同一套兜底:Redis loginlock 之外,DB 侧也要
			// 计数 + 达阈值落库锁定 —— 不然 Redis 不可用时对 MFA 码的暴力
			// 试码完全没有闸门(loginlock 降级是 no-op,DB 计数是最后一道)。
			newCount := u.FailedCount + 1
			updates := map[string]any{"failed_count": newCount}
			if newCount >= 10 {
				until := time.Now().Add(s.lock)
				updates["locked_until"] = model.Time{Time: until}
			}
			s.db.WithContext(ctx).Model(&model.SysUser{}).
				Where("id = ?", u.ID).Updates(updates)
			loginlock.AfterFail(ctx, req.Username, req.LoginIP, s.lock)
			s.recordLoginLog(ctx, u.ID, u.Username, req, 0, "mfa_code_wrong", "")
			return nil, err
		}
	}

	// 5. 加载身份
	var identities []model.SysUserIdentity
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", u.ID, true).
		Order("is_primary DESC, id ASC").
		Find(&identities).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询身份失败").WithDetail(err.Error())
	}
	if len(identities) == 0 {
		s.recordLoginLog(ctx, u.ID, u.Username, req, 0, "无可用身份", "")
		return nil, errcode.New(errcode.NoActiveIdentity, "账号未配置可用身份")
	}

	// 选主身份(已按 is_primary DESC 排序)
	primary := identities[0]
	projectIDs := collectProjectIDs(identities)

	// 6. 更新登录态
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", u.ID).Updates(map[string]any{
		"last_login_at": model.Time{Time: now},
		"last_login_ip": req.LoginIP,
		"failed_count":  0,
		"locked_until":  nil,
	}).Error; err != nil {
		// 写失败不影响登录结果,只记日志
		logger.L().Warn("update last_login_at failed", zap.Uint64("user_id", u.ID), zap.Error(err))
	}

	// 7. 签发 token
	//    注意 Claims 里不再有 Audience 字段:aud 由 SignByAudience 选的 manager
	//    权威写入(secret 在谁手里 aud 就是谁),调用方不参与。
	//    两次签发各用一份 Fork():sign() 会覆写整个 RegisteredClaims(含 jti),
	//    复用同一个结构签两次,内存里那份 base 会停留在**第二张**(refresh)上 ——
	//    此后 base.JTI() 读到的是新 token 的值,不是刚消费掉的那张旧 token。
	//    见 auth.Claims.Fork(含实测后果)。
	//
	//    SessionID 两张 token 共用同一个:它是"这条会话"的身份,**轮换不换**
	//    (换了就断链,登出又变回只吊销一半)。
	//    ⚠️ **切换身份会换**(M148)—— 见 SwitchIdentity / rotateSession。
	//    见 pkg/auth/session.go。
	base := &auth.Claims{
		UserID:     u.ID,
		IdentityID: primary.ID,
		Username:   u.Username,
		RoleCode:   primary.RoleCode,
		ProjectIDs: projectIDs,
		SessionID:  auth.NewSessionID(),
	}
	accessToken, err := s.jwt.SignByAudience(auth.AudienceAdmin, base.Fork())
	if err != nil {
		return nil, errcode.New(errcode.Internal, "签发 access token 失败").WithDetail(err.Error())
	}
	refreshToken, err := s.jwt.RefreshByAudience(auth.AudienceAdmin, base.Fork())
	if err != nil {
		return nil, errcode.New(errcode.Internal, "签发 refresh token 失败").WithDetail(err.Error())
	}
	ttl := s.jwt.MustGet(auth.AudienceAdmin).AccessTTL()
	rttl := s.jwt.MustGet(auth.AudienceAdmin).RefreshTTL()

	// 8. 清登录失败计数
	_ = loginlock.Clear(ctx, req.Username, req.LoginIP)

	// 9. 写登录成功日志
	s.recordLoginLog(ctx, u.ID, u.Username, req, 1, "ok", base.SessionID)

	// 10. 异常登录检测(M128)
	//
	//   ⚠️ 必须放在**登录成功之后**、且**不阻断登录**:
	//     - 放在之前:密码还没校验对就查会话,等于给撞库的人送情报;
	//     - 放在之后:检测到"多端同时在线"时,本次登录已经合法签发了 token ——
	//       检测是**提醒**,不是准入闸门(准入闸门是 loginlock / captcha / RBAC)。
	//
	//   detectLoginAnomaly 内部**不返回错误** —— 检测失败只记日志,
	//   绝不让"安全增强"把正常登录搞挂。
	//
	//   这一步在 M128 之前**根本不存在**:`pkg/anomalylogin` 从 M45 就在仓里,
	//   有 172 行单测、清单标着 "P2 ✅",但**零生产调用方** ⇒
	//   "用户被盗号时无法感知"这条痛点一次都没被真正解决过。
	s.detectLoginAnomaly(ctx, u.ID, base.SessionID, req, now)

	return &AuthTokens{
		Token:            accessToken,
		RefreshToken:     refreshToken,
		ExpiresIn:        int(ttl.Seconds()),
		RefreshExpiresIn: int(rttl.Seconds()),
		// 传 primary.ID 而不是 0:登录时已经选好了主身份,告诉前端"当前是哪个",
		// 前端就不必再靠 identities[0] 猜一次。传 0 会让前端在多身份用户身上
		// 有一次"显示的身份 ≠ 实际 token 里的身份"的窗口。
		User: NewCurrentUserView(&u, identities, primary.RoleCode, primary.ID),
	}, nil
}

// collectProjectIDs 收集所有 active 身份的项目 id
func collectProjectIDs(ids []model.SysUserIdentity) []uint64 {
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id.ProjectID > 0 {
			out = append(out, id.ProjectID)
		}
	}
	return out
}

// recordLoginLog 写 sys_login_log
//
// # sessionID 参数
//
// 成功登录时传**这一行对应的会话 sid**(与两张 token 共用的那个,见
// pkg/auth/session.go);失败记录传空串 —— 失败时压根没有会话,凭空造一个
// 只会让"活跃会话"列表里出现从未存在过的设备。
//
// 这一行是**会话登记簿**的数据源(见 model.SysLoginLog 的说明):
// 「查看活跃设备」「强制下线其他设备」都从这张表读。
func (s *authService) recordLoginLog(ctx context.Context, userID uint64, username string, req LoginRequest, status int, msg string, sessionID string) {
	// 不阻塞主流程
	//
	// ⚠️ 2026-10-30(M117-3):**browser / os 两列此前恒空** ——
	//   表里有这两列(model.SysLoginLog.Browser / .OS),但全仓**没有写入方**。
	//   审计中心那两列于是永远是空的 ⇒ 现在从 UA 解析出来填上
	//   (解析规则见 internal/pkg/uainfo)。
	ua := uainfo.Parse(req.UserAgent)
	loginAt := model.Time{Time: time.Now()}
	rec := &model.SysLoginLog{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    userID,
		Username:  username,
		IP:        req.LoginIP,
		// ⚠️ 三列都要**截到列宽** ——
		//   device 装的是 **UA 原文**(不是设备型号),微信那种能到 200+ 字符;
		//   browser / os 是解析出来的名字(短,但规则表以后可能加长名字)。
		//   MySQL 严格模式下超长直接报 "Data too long for column" ⇒
		//   **整条登录日志写不进去**(只在日志里留一条 warn)⇒ 审计链断一条。
		//   SQLite 不校验长度,所以本地开发期**看不出**这个问题 —— 上生产才炸。
		//   列宽:device=128 / browser=64 / os=64(见 model.SysLoginLog)。
		Device:    uainfo.Clip(req.UserAgent, 128),
		Browser:   uainfo.Clip(ua.Browser, 64),
		OS:        uainfo.Clip(ua.OS, 64),
		Status:    status,
		Message:   msg,
		LoginAt:   loginAt,
		SessionID: sessionID,
	}
	// ⚠️ M117-4:**登录本身就是一次活动** —— 成功登录时这一列立刻有值。
	//
	// 不写的话,"登录之后一直没再操作"的会话,「最后活动」显示**空白**
	// (直到下一次有请求打进来)。而登录时刻**就是**那次活动 ——
	// 空白会让人误以为"登录之后就没动过",与事实不符。
	//
	// 失败记录**不写**:失败时压根没有会话,给不存在的会话记活动是假的。
	if status == 1 {
		rec.LastActiveAt = &loginAt
	}
	if err := s.db.WithContext(ctx).Create(rec).Error; err != nil {
		logger.L().Warn("record login log failed",
			zap.Uint64("user_id", userID), zap.String("username", username), zap.Error(err))
	}
}

// ===== 以下方法本期仅占位,后续按需补全 =====

// Logout 登出
//  1. 吊销**整条会话**(access + refresh 一起):
//     1a. 会话 sid(2026-10-24 新增)—— 补上"登出只挡住一半"那个洞。
//     以前只吊销 access 的 jti,那张 refresh 还活着,最长 720h。
//     1b. 当前 caller 的 jti(走 auth.RevocationStore;没有 Redis 时用进程内实现,
//     **不再是 skip** —— 2026-10-21 W5 之前这里是 `if bl != nil`,恒假,从未执行过)
//  2. 写 sys_login_log.LogoutAt(找最近一条无 LogoutAt 的记录)
//
// # sid 解决了什么、没解决什么
//
// 解决的:登出**真的结束**了这条会话。access 与 refresh 共用一个 sid,
// 登出时吊销 sid,两张 token 一起死。用户点了"退出登录",那张 refresh 换不动了。
//
// 没解决的(仍是范围外):「强制下线**其他**设备」—— 那要能**列出**该用户的
// 全部会话并点名踢掉其中几张,需要会话登记簿(user → {sid, device, issued_at, ip}
// 的索引)。sid 只解决了"结束我自己这一条",没解决"点名踢别人那几条"。
//
// # 这里**刻意不**推进作废线(2026-10-21)
//
// 作废线能一次性作废该用户的全部令牌,但它是**全有或全无**的 —— 一推就是全部设备
// 一起下线。而这里要的是 SRS §2.2.1 #6 的「可强制下线**其他**设备」,即保留本设备、
// 只踢别的。这两件事形状相反:前者是把整条线往前挪,后者必须能点名踢掉某几张。
//
// 所以在登出时推作废线,是一个**看起来更安全、实际制造事故**的「修复」:用户在手机上
// 点了退出登录,结果电脑上的会话也一起没了,于是他又要登一次 —— 而每次登出都这样。
//
// 参见 docs/框架层未完成项清单v1.md 缺口 A。
func (s *authService) Logout(ctx context.Context, caller *Caller) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	// 1. 吊销**整条会话**(access + refresh 一起)
	//
	//    1a. 会话本身(sid):这是"登出真的结束了会话"的那一步。
	//        只吊销 access 的 jti 是不够的 —— 那张 refresh 还活着,最长 720h,
	//        用户点了退出登录之后照样能换发新 access(见 pkg/auth/session.go)。
	//        TTL 取 **refresh 的寿命**(会话里寿命最长的就是它):用 access 的 8h
	//        的话,吊销记录会先过期,而 refresh 还活着,等于没吊。
	//    1b. 当前 access 的 jti:保留原行为(access 立刻失效,不必等会话态传播)。
	//
	//    两步都**不阻塞** logout:登出是用户诉求,吊销写不进去也不能把人
	//    卡在登录页外面。但日志级别不同 —— 1a 失败意味着"这次登出只做了一半"
	//    (refresh 还活着),那是安全事件,记 ERROR 让告警能捞到;
	//    1b 失败只是 access 多活一会儿(它自己有 8h 上限),记 WARN。
	mgr := s.jwt.MustGet(auth.Audience(caller.Audience))
	ttl := mgr.AccessTTL()
	if err := auth.RevokeSession(ctx, caller.SessionID, mgr.RefreshTTL()); err != nil {
		logger.L().Error("吊销会话失败:登出只做了一半,refresh token 仍可续期",
			zap.String("sid", caller.SessionID),
			zap.Uint64("user_id", caller.UserID), zap.Error(err))
	}
	// 外层的 `if caller.JTI != ""` 已删(W5):sign() 无条件写 JTI(W3 起),
	// 那层守卫恒真,只会掩盖"吊销没执行"。
	if err := auth.DefaultRevocation().Revoke(ctx, caller.JTI, ttl); err != nil {
		// revoke 失败不阻塞 logout(降级策略):登出是用户诉求,
		// 吊销写不进去也不能把人卡在登录页外面
		logger.L().Warn("revoke jti failed",
			zap.String("jti", caller.JTI), zap.Error(err))
	}
	// 2. 写 sys_login_log.LogoutAt —— **按 sid 精确定位本会话那一行**
	//
	//    ⚠️ 旧实现是「取该用户最近一条 logout_at IS NULL 的记录」,那是**猜**。
	//    两台设备同时在线时 B 的登录行比 A 新,于是 A 在 A 上点退出登录,
	//    被盖上 logout_at 的是 **B 的行**:A 的会话永远显示"未登出",
	//    B 的会话明明还活着却显示"已登出"。登记簿一错,"活跃设备"列表就是错的,
	//    "强制下线"也点不准人(SRS §2.2.1 #6)。
	//
	//    现在按 sid 定位 —— sid 来自 token,唯一对应一行。
	//
	//    降级(老 token 没有 sid,本轮上线前签发):退回"最近一条**没有 sid 的**
	//    成功登录行"。加 session_id = '' 这个限定,是为了不让老 token 的登出
	//    盖掉新会话的行 —— 那正是原来那个 bug 的本体。
	//
	//    status = 1 的限定:登录失败的行永远没有 logout_at,原来会被当成
	//    "最近一条未登出的记录"盖上登出时间(把一条失败记录标成"已登出")。
	now := model.Time{Time: time.Now()}
	logQ := s.db.WithContext(ctx).Model(&model.SysLoginLog{}).
		Where("user_id = ? AND status = 1 AND logout_at IS NULL", caller.UserID)
	if caller.SessionID != "" {
		logQ = logQ.Where("session_id = ?", caller.SessionID)
	} else {
		logQ = logQ.Where("session_id = ?", "")
	}
	if err := logQ.Order("login_at DESC").Limit(1).
		Update("logout_at", &now).Error; err != nil {
		// 写失败不阻塞 logout,只记日志
		logger.L().Warn("write logout_at failed",
			zap.Uint64("user_id", caller.UserID), zap.Error(err))
	}
	return nil
}

// RefreshToken 用 refresh token 换发新的一对 token(单次使用 + 轮换)。
//
// # 流程(W7)
//  1. ParseRefresh —— 用途不是 refresh 的直接拒(W3 起 typ 声明)
//  2. 查吊销 —— **这一步才让「轮换」有意义**:没有它,旧 refresh 可以无限重放,
//     轮换只是发了一堆新 token 而旧的照样能用
//  3. 校验用户仍在册在岗
//  4. 每个 token 一份**全新**的 Claims:复用同一个结构签两次,内存里那份 claims
//     会停留在第二张 token 上,第 5 步的 Revoke(claims.JTI()) 就吊错了对象 ——
//     吊掉新签的、放过旧的(见 auth.Claims.Fork)
//  5. 吊销刚被消费掉的旧 refresh JTI,TTL 用它**自己的剩余寿命**
//  6. 签发失败返错,不返空串
//
// 另有一道闸**故意不在编号里**,因为它是插进来的、且漏掉它的后果完全静默:
// **查作废线** —— 改密码 / 重置密码后的强制下线。refresh 走的是公开路由
// /rapid-admin/v1/public/auth/refresh(见 PublicPaths),**不经过中间件**,所以中间件
// 那道闸在这里完全够不着。少了它,改密码对 refresh 侧等于无效:access 是死了,
// 可攻击者手里那张 refresh 还能换出一张 iat 落在作废线之后的**全新** access,
// 一路畅通 —— 改密码变成了只踢掉半个账号。
//
// # 重放检测
//
// 第 2 步命中意味着同一个 refresh token 被用了两次。在**单次使用**的语义下
// 这不正常:要么是 token 泄露后被两边同时用,要么是客户端并发重试撞车。
// 只记 WARN —— 真要"检测到重放就吊掉该用户全部会话"需要会话登记簿
// (user → {jti, device, issued_at} 索引),那是另一件事(范围外),别在这里假装已做。
func (s *authService) RefreshToken(ctx context.Context, refreshToken string) (*AuthTokens, error) {
	claims, err := s.jwt.ParseRefresh(refreshToken)
	if err != nil {
		return nil, errcode.New(errcode.TokenInvalid, "refresh token 无效")
	}
	// 2. 旧 refresh 是否已被消费掉(轮换时被吊销,或登出时被吊销)
	revoked, err := auth.DefaultRevocation().IsRevoked(ctx, claims.JTI())
	if err != nil {
		// 查不了就当它没被吊销 —— 与中间件同一取向:存储故障不该把人全踢下线。
		// 但这里会削弱"单次使用",所以必须留痕。
		logger.L().Warn("refresh: 吊销查询失败,按未吊销处理(轮换保证被削弱)",
			zap.String("jti", claims.JTI()), zap.Error(err))
	} else if revoked {
		logger.L().Warn("refresh token 重放:该 jti 已被消费过",
			zap.String("jti", claims.JTI()),
			zap.Uint64("user_id", claims.UserID))
		return nil, errcode.New(errcode.TokenInvalid, "refresh token 已失效,请重新登录")
	}
	// 2.5 作废线校验 —— 与中间件同一道闸,这里不能省。
	//
	//    中间件只拦 access token。refresh token 走的是公开路由
	//    (/rapid-admin/v1/public/auth/refresh,见 PublicPaths),**不经过中间件**。
	//    所以少了这一步,改密码对 refresh 侧完全无效:access 是死了,可攻击者
	//    手里那张 refresh 最长还能换 720h 的新 access —— 换出来的新 access 同样
	//    iat 在作废线之后,一路畅通到天荒地老。改密码变成了"只踢掉了半个账号"。
	if invalid, err := auth.SessionInvalidated(ctx, claims); err != nil {
		logger.L().Warn("refresh: 作废线查询失败,按未作废处理(强制下线被绕过)",
			zap.Uint64("user_id", claims.UserID), zap.Error(err))
	} else if invalid {
		return nil, errcode.New(errcode.TokenInvalid, "登录状态已失效,请重新登录")
	}
	// 2.6 会话吊销校验 —— 补上"登出只挡住一半"那个洞。
	//
	//     登出时吊销的是**整条会话**(sid),而中间件那道闸查的是 jti,
	//     查不到 sid;refresh 又恰好走公开路由、不经过中间件 ——
	//     所以**只有这里**会查会话吊销态。少了它:用户点了退出登录,
	//     access 死了,可那张 refresh 还能换发新 access,最长 720h(30 天)。
	//
	//     查不到(存储故障)按未吊销放行,与上面两道闸同一取向:存储抖一下
	//     不该把人全踢下线。但代价是"这次登出可能没生效",必须留痕。
	if claims.SessionID != "" {
		if revoked, err := auth.IsSessionRevoked(ctx, claims.SessionID); err != nil {
			logger.L().Warn("refresh: 会话吊销查询失败,按未吊销处理(登出可能被绕过)",
				zap.String("sid", claims.SessionID), zap.Error(err))
		} else if revoked {
			logger.L().Warn("refresh: 会话已登出,拒绝续期",
				zap.String("sid", claims.SessionID), zap.Uint64("user_id", claims.UserID))
			return nil, errcode.New(errcode.TokenInvalid, "登录状态已失效,请重新登录")
		}
	}
	// 3. 校验 user 仍然存在 + active
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, claims.UserID).Error; err != nil {
		return nil, errcode.New(errcode.Unauthorized, "用户不存在")
	}
	if !u.IsActive || u.IsLeave {
		return nil, errcode.New(errcode.NoActiveIdentity, "账号已停用")
	}
	// 续期响应必须带上完整 identities —— 之前这里直接吐 *model.SysUser,
	// 那个结构里没有 identities 字段,前端续期后身份列表静默消失(表现为
	// 切换身份入口突然没了)。现在统一走 AuthTokens。
	var identities []model.SysUserIdentity
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", u.ID, true).
		Order("is_primary DESC, id ASC").
		Find(&identities).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询身份失败").WithDetail(err.Error())
	}

	// 4. 两个 token 各签各的 —— 复用同一个结构签两次后,内存里那份 claims 会
	//    停留在第二张(下签)token 上,第 5 步吊销时会吊错对象(见 auth.Claims.Fork)。
	//
	//    会话绑定(sid):轮换**不换** sid —— 换了就等于开了一条新会话,
	//    登出时按旧 sid 吊销就吊不到轮换后那张 refresh,洞又回来了。
	//    老 token 没有 sid,在这里补一个:从这一刻起这条会话有了绑定,
	//    以后的登出能一次结束它(见 pkg/auth/session.go 的降级说明)。
	sid := claims.SessionID
	if sid == "" {
		sid = auth.NewSessionID()
	}
	base := claims.Fork()
	base.SessionID = sid
	aud := claims.Audience()
	access, err := s.jwt.SignByAudience(aud, base.Fork())
	if err != nil {
		return nil, errcode.New(errcode.Internal, "签发 access token 失败").WithDetail(err.Error())
	}
	// 6. 这里原本是 `refresh, _ :=` —— 吞掉错误,签发失败会返 200 + 空串,
	//    前端把空串当新 refresh 存下来,下次续期带着空 token 去,报的还是
	//    "refresh 无效",真正的错因被埋掉了。
	refresh, err := s.jwt.RefreshByAudience(aud, base.Fork())
	if err != nil {
		return nil, errcode.New(errcode.Internal, "签发 refresh token 失败").WithDetail(err.Error())
	}

	// 5. 吊销刚被消费的旧 refresh。TTL 用它**自己的剩余寿命**而不是
	//    整段 RefreshTTL():token 自然过期后吊销记录就该消失,
	//    记满 720h 会让吊销存储里堆满早就没意义的条目。
	ttl := s.jwt.MustGet(aud).AccessTTL()
	rttl := s.jwt.MustGet(aud).RefreshTTL()
	if remain := time.Until(claims.ExpiresAt.Time); remain > 0 {
		if err := auth.DefaultRevocation().Revoke(ctx, claims.JTI(), remain); err != nil {
			// 吊销写不进去意味着旧 token 还能重放。这比"续期失败"严重,
			// 但也不能因此把用户挡在外面 —— 记 ERROR 让告警能捞到。
			logger.L().Error("refresh 轮换:旧 token 吊销失败,该 refresh token 仍可重放",
				zap.String("jti", claims.JTI()), zap.Error(err))
		}
	}
	// 7. 标记这条会话"刚刚还活着"(会话登记簿的心跳)
	//
	//    只在**续期**时更新,不是每个请求都写 —— 续期每 ~8h 一次,不是热路径。
	//    这一列的用途是判"会话是否还过期":refresh token 每次轮换都重新起一个
	//    720h 窗口,只看 login_at 会把"40 天前登录、昨天刚续期"的会话误判成死的
	//    (见 model.SysLoginLog 的三个时间字段说明)。
	//
	//    写失败**不阻塞续期**:它只影响"活跃设备"列表的展示精度,不影响安全
	//    (会话的死活由吊销存储说了算,不由这一列说了算)。
	if sid != "" {
		// ⚠️ M117-4:这一列此前**只在续期时**更新 ⇒ 会话建立后的整整 8 小时里
		//   它是空的(审计中心「最后活动」对当天所有登录都是空白)。
		//   现在统一走 touchSession:有请求打在这条会话上就记一次活动,
		//   两层节流(进程内闸 + DB 条件更新)保证不会每个请求写一次库。
		if _, err := s.touchSession(ctx, claims.UserID, sid); err != nil {
			logger.L().Warn("refresh: 更新 last_active_at 失败(仅影响活跃设备列表精度)",
				zap.String("sid", sid), zap.Error(err))
		}
	}
	return &AuthTokens{
		Token:            access,
		RefreshToken:     refresh,
		ExpiresIn:        int(ttl.Seconds()),
		RefreshExpiresIn: int(rttl.Seconds()),
		User:             NewCurrentUserView(&u, identities, claims.RoleCode, claims.IdentityID),
	}, nil
}

// ChangePassword 修改自己的密码
//  1. 校验旧密码(前端已 pre-hash,直接 CheckPassword 即可)
//  2. 不做密码强度校验:前端责任(service 收到的是 SHA-256 hex,无法判断原始强度)
//  3. 哈希新密码(同样基于 pre-hash,确保与 Login 流程一致)
//  4. revoke 当前 jti(本设备立刻失效)+ 推进作废线(该用户全部在途令牌失效)
//
// 第 4 步的两半不能互相替代,详见 invalidateUserSessions 的注释:
// 只 revoke 当前 jti 时,同一用户其它设备上的会话(尤其是盗号者的)毫发无损。
//
// 设计取舍: 与 §9.8 文档的"密码强度校验"看似冲突,但实际是同源问题——
//
//	Login/ChangePassword 都走前端 pre-hash,所以服务端永远只能看到 SHA-256 hex,
//	强度校验必须放在前端。 这是与前端 crypto.ts preHashPassword 契约一致。
func (s *authService) ChangePassword(ctx context.Context, caller *Caller, oldPwd, newPwd string) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if err := MustNoEmpty("oldPassword", oldPwd); err != nil {
		return err
	}
	if err := MustNoEmpty("newPassword", newPwd); err != nil {
		return err
	}
	if oldPwd == newPwd {
		return errcode.New(errcode.InvalidParam, "新密码不能与旧密码相同")
	}
	// pre-hash 后的 hex 必为 64 字符(防止前端传空串/截断)
	if len(newPwd) != 64 {
		return errcode.New(errcode.InvalidParam, "新密码格式错误")
	}

	// 1. 加载 user
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, caller.UserID).Error; err != nil {
		return errcode.New(errcode.NotFound, "用户不存在")
	}

	// 2. 校验旧密码
	if !auth.CheckPassword(u.PasswordHash, oldPwd) {
		return errcode.New(errcode.LoginFailed, "旧密码错误")
	}

	// 3. 哈希新密码
	newHash, err := auth.HashPassword(newPwd)
	if err != nil {
		return errcode.New(errcode.Internal, "密码哈希失败").WithDetail(err.Error())
	}

	// 4. 更新(同时清 must_change_pwd 标志 + 清失败计数)
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", caller.UserID).
		Updates(map[string]any{
			"password_hash":   newHash,
			"must_change_pwd": false,
			"failed_count":    0,
			"locked_until":    nil,
		}).Error; err != nil {
		return errcode.New(errcode.Internal, "更新密码失败").WithDetail(err.Error())
	}

	// 5. revoke 当前 jti(W5:守卫已删,见 Logout 里的说明)
	ttl := s.jwt.MustGet(auth.Audience(caller.Audience)).AccessTTL()
	if err := auth.DefaultRevocation().Revoke(ctx, caller.JTI, ttl); err != nil {
		logger.L().Warn("revoke jti on change-password failed",
			zap.String("jti", caller.JTI), zap.Error(err))
	}
	// 6. 推进作废线 —— 踢掉该用户的**全部**在途令牌,不只是第 5 步那一张。
	//    第 5 步单独用着的时候,改密码只踢掉"当前这张",盗号者的会话照旧有效。
	invalidateUserSessions(ctx, caller.UserID, "change-password")
	return nil
}

// ResetPassword 重置密码(管理员代重置 或 自助改密)
//   - 深度防御:handler 已做 IDOR 拦截(target == caller.UserID 或 caller 是 super_admin)
//   - 这里再校一次,防止 handler 逻辑被绕过
//   - 强度校验由前端负责(同 ChangePassword 的设计取舍)
//   - 推进**目标用户**的作废线(无条件);仅当改的是自己时再 revoke 当前 jti
func (s *authService) ResetPassword(ctx context.Context, caller *Caller, targetUserID uint64, newPwd string) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if err := MustNoEmpty("newPassword", newPwd); err != nil {
		return err
	}
	if len(newPwd) != 64 {
		return errcode.New(errcode.InvalidParam, "新密码格式错误")
	}

	// 1. 深度防御:权限再校
	if targetUserID != caller.UserID && !caller.HasRole("role_super_admin") {
		return errcode.New(errcode.IdorAttempt, "只能重置自己的密码")
	}

	// 2. 加载 target
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, targetUserID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.NotFound, "目标用户不存在")
		}
		return errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}

	// 3. 哈希新密码
	newHash, err := auth.HashPassword(newPwd)
	if err != nil {
		return errcode.New(errcode.Internal, "密码哈希失败").WithDetail(err.Error())
	}

	// 4. 更新(清 must_change_pwd + 失败计数)
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", targetUserID).
		Updates(map[string]any{
			"password_hash":   newHash,
			"must_change_pwd": true, // 代重置后必须改密
			"failed_count":    0,
			"locked_until":    nil,
		}).Error; err != nil {
		return errcode.New(errcode.Internal, "更新密码失败").WithDetail(err.Error())
	}

	// 5. 推进**目标用户**的作废线。
	//
	//    ⚠️ 这里推的是 targetUserID 而不是 caller.UserID,而且**无条件**执行 ——
	//    之前的实现把整段吊销包在 `if targetUserID == caller.UserID` 里,于是
	//    "管理员重置了某个用户的密码"对那个用户的存量会话毫无影响:密码变了,
	//    但他所有设备上的登录全部照旧有效,直到自然过期(最长 720h)。
	//    管理员代重置是最常走的一条路径,漏掉它等于这个功能在主场景下没接线。
	invalidateUserSessions(ctx, targetUserID, "reset-password")

	// 6. revoke 当前 jti(仅当改的是自己,立即下线)
	//    保留这一条不是为了"再踢一次",而是为了让调用方的**这一次请求**立刻
	//    失败;作废线是"下一次请求才生效",两者的时间差差一次往返。
	if targetUserID == caller.UserID {
		ttl := s.jwt.MustGet(auth.Audience(caller.Audience)).AccessTTL()
		if err := auth.DefaultRevocation().Revoke(ctx, caller.JTI, ttl); err != nil {
			logger.L().Warn("revoke jti on reset-password failed",
				zap.String("jti", caller.JTI), zap.Error(err))
		}
	}
	return nil
}

func (s *authService) SendSms(ctx context.Context, phone, scene string) error {
	if !validPhone(phone) {
		return errcode.New(errcode.InvalidParam, "手机号格式错误")
	}
	if !validScene(scene) {
		return errcode.New(errcode.InvalidParam, "未知 scene: "+scene)
	}
	now := time.Now()

	// 1. 限流(§1.9 #8):Redis per phone+scene 冷却 + 1m/1d 窗口
	//   - 优先 Redis(快,精确)
	//   - Redis 不可用时 no-op 放行(下面 DB 子句作为兜底)
	if err := smslimit.Allow(ctx, phone, scene); err != nil {
		return err
	}

	// 1.5 DB 兜底:同 phone+scene 冷却期内不能再次发送
	//   Redis 故障时由 DB 阻止;双保险
	coolDown := s.coolDownSeconds()
	var latest model.SysSmsCode
	err := s.db.WithContext(ctx).
		Where("phone = ? AND scene = ?", phone, scene).
		Order("send_at DESC").
		First(&latest).Error
	if err == nil {
		if int(now.Sub(latest.SendAt.Time).Seconds()) < coolDown {
			return errcode.New(errcode.SmsSendTooFrequent, "短信发送太频繁, 请稍后再试")
		}
	} else if err != gorm.ErrRecordNotFound {
		return errcode.New(errcode.Internal, "查询验证码记录失败").WithDetail(err.Error())
	}

	// 2. 生成 6 位数字验证码
	code := s.generateCode()

	// 3. UPSERT sys_sms_code
	rec := model.SysSmsCode{
		Phone:  phone,
		Scene:  scene,
		Code:   code,
		SendAt: model.Time{Time: now},
	}
	// 先看有没有"未使用"的旧记录,有就更新, 没有就新建
	var existing model.SysSmsCode
	err = s.db.WithContext(ctx).
		Where("phone = ? AND scene = ? AND used_at IS NULL", phone, scene).
		First(&existing).Error
	switch err {
	case nil:
		rec.ID = existing.ID
		rec.BaseModel = existing.BaseModel
		if err := s.db.WithContext(ctx).Save(&rec).Error; err != nil {
			return errcode.New(errcode.Internal, "更新验证码记录失败").WithDetail(err.Error())
		}
	case gorm.ErrRecordNotFound:
		rec.ID = sonyflake.Next()
		if err := s.db.WithContext(ctx).Create(&rec).Error; err != nil {
			return errcode.New(errcode.Internal, "写入验证码记录失败").WithDetail(err.Error())
		}
	default:
		return errcode.New(errcode.Internal, "查询验证码记录失败").WithDetail(err.Error())
	}

	// 4. 下发短信
	if err := s.sms.Send(ctx, phone, code, scene); err != nil {
		logger.L().Error("sms.Send failed",
			zap.String("phone", phone), zap.String("scene", scene), zap.Error(err))
		return errcode.New(errcode.Internal, "短信下发失败: "+err.Error())
	}
	return nil
}

func (s *authService) VerifySms(ctx context.Context, phone, scene, code string) (bool, error) {
	if !validPhone(phone) {
		return false, errcode.New(errcode.InvalidParam, "手机号格式错误")
	}
	if code == "" {
		return false, errcode.New(errcode.SmsCodeInvalid, "请输入验证码")
	}

	// 1. 取最近一条未过期的记录
	var rec model.SysSmsCode
	err := s.db.WithContext(ctx).
		Where("phone = ? AND scene = ?", phone, scene).
		Order("send_at DESC").
		First(&rec).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, errcode.New(errcode.SmsCodeInvalid, "短信验证码不存在")
		}
		return false, errcode.New(errcode.Internal, "查询验证码失败").WithDetail(err.Error())
	}

	// 2. 已使用
	if rec.UsedAt != nil && !rec.UsedAt.IsZero() {
		return false, errcode.New(errcode.SmsCodeInvalid, "短信验证码已使用")
	}

	// 3. 已过期
	ttl := s.codeTTL()
	if time.Since(rec.SendAt.Time) > ttl {
		return false, errcode.New(errcode.SmsCodeInvalid, "短信验证码已过期")
	}

	// 4. 不匹配
	if rec.Code != code {
		return false, errcode.New(errcode.SmsCodeInvalid, "短信验证码错误")
	}

	// 5. 标记已用 (单次性: 条件 update, WHERE used_at IS NULL 防御并发双提交)
	//    - 写入失败必须返回 error (不再 silent-log), 否则 DB 抖动下验证码会被复用
	//    - RowsAffected == 0 说明并发请求已先一步标记, 视同"已使用"
	now := model.Time{Time: time.Now()}
	res := s.db.WithContext(ctx).Model(&model.SysSmsCode{}).
		Where("id = ? AND used_at IS NULL", rec.ID).
		Update("used_at", &now)
	if res.Error != nil {
		return false, errcode.New(errcode.Internal, "标记验证码已用失败").WithDetail(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return false, errcode.New(errcode.SmsCodeInvalid, "短信验证码已使用")
	}
	return true, nil
}

// ===== 内部 helper =====

// generateCode 6 位数字验证码
func (s *authService) generateCode() string {
	s.codeRandM.Lock()
	defer s.codeRandM.Unlock()
	// [000000, 999999] 然后转为 6 位字符串
	n := s.codeRand.Intn(1000000)
	return fmt.Sprintf("%06d", n)
}

// coolDownSeconds 短信冷却秒数(从 config 读, 兜底 60s)
func (s *authService) coolDownSeconds() int {
	if s.smsCfg.RateLimit.WindowSeconds > 0 {
		return s.smsCfg.RateLimit.WindowSeconds
	}
	return 60
}

// codeTTL 验证码有效期(从 config 读, 兜底 10min)
//
// ⚠️ **兜底值跟阿里云模板文案对齐(10 分钟)**:SMS_244570823 的正文写着
// 「该验证码10分钟内有效」—— 服务端只活 5 分钟的话,用户照短信提示重试就会失败。
func (s *authService) codeTTL() time.Duration {
	if s.smsCfg.RateLimit.CodeTTL > 0 {
		return time.Duration(s.smsCfg.RateLimit.CodeTTL) * time.Second
	}
	return 10 * time.Minute
}

// validPhone 11 位 1[3-9]xxxxxxxxx
func validPhone(phone string) bool {
	if len(phone) != 11 || phone[0] != '1' || phone[1] < '3' || phone[1] > '9' {
		return false
	}
	for _, c := range phone {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// validScene scene 必须是已知枚举
func validScene(scene string) bool {
	switch scene {
	case string(msn.SceneLogin),
		string(msn.SceneResetPassword),
		string(msn.SceneChangePhone),
		string(msn.SceneViewFullPhone),
		string(msn.SceneMFA):
		return true
	default:
		return false
	}
}

// SwitchIdentity 切换身份
//  1. 校验 identityID 归属 userID
//  2. 目标身份 active + 在有效期内
//  3. 高敏角色必须 SMS 二次验证(smsCode 非空)
//  4. 用新 identity 签发 token(**换 sid**,M148)
//  5. 换会话:吊销旧 sid + 当前 jti + 登记簿换 sid + 改 caller(见 rotateSession)
func (s *authService) SwitchIdentity(ctx context.Context, caller *Caller, identityID uint64, smsCode string) (*AuthTokens, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}
	if identityID == 0 {
		return nil, errcode.New(errcode.BadRequest, "identityId 不能为空")
	}

	// 1. 加载目标 identity
	var target model.SysUserIdentity
	if err := s.db.WithContext(ctx).First(&target, identityID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "身份不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询身份失败").WithDetail(err.Error())
	}
	if target.UserID != caller.UserID {
		return nil, errcode.New(errcode.IdorAttempt, "该身份不属于当前用户")
	}
	if !target.IsActive {
		return nil, errcode.New(errcode.NoActiveIdentity, "目标身份已停用")
	}
	now := time.Now()
	if target.ValidFrom.Time.After(now) {
		return nil, errcode.New(errcode.StateInvalid, "目标身份未到生效时间")
	}
	if !target.ValidTo.IsZero() && target.ValidTo.Time.Before(now) {
		return nil, errcode.New(errcode.StateInvalid, "目标身份已过期")
	}

	// 2. 加载当前用户(为了拿登记手机号,给高敏角色的短信校验用)
	//
	//    ⚠️ M104:这一步**提前**了 —— 原来用户是在第 4 步才查的,
	//    而短信校验在第 2 步,那时手上没有手机号,于是校验只能"不绑手机号"。
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, caller.UserID).Error; err != nil {
		return nil, errcode.New(errcode.NotFound, "用户不存在")
	}
	callerPhone := decryptPII(u.Phone)

	// 3. 高敏角色:SMS 二次验证(仅限**登记了手机号**的账号)
	//
	//    ⚠️ 2026-11-03 用户拍板:没登记手机号的账号**免验直接切**。
	//    M104 时期这里是硬拒的 —— 但那等于"没绑手机号的账号永远切不进
	//    高敏身份",而绑号那条路(个人中心换绑)又依赖短信可用,对
	//    测试环境 / 没配短信通道的部署是一条死路。角色本身已经在
	//    sys_user_identity 里授予(能进来 = 已有合法登录会话),
	//    二次验证验的是"换身份"这个动作,不是"授权"本身。
	if isHighRiskRole(target.RoleCode) && callerPhone != "" {
		if smsCode == "" {
			return nil, errcode.New(errcode.SmsCodeInvalid, "切换到高敏角色需要短信验证码")
		}
		// ⚠️ **M104(2026-10-28)修:验证码必须绑在账号自己的手机上。**
		//
		//   以前这里是 `WHERE code = ? AND scene = ?` —— **不带 phone**。
		//   那意味着:任何人可以走公开接口 /public/auth/sms/send 给**自己的**
		//   手机号发一条 scene=change_phone 的码(那 4 个 scene 里 change_phone
		//   对公众开放),拿到码之后拿来切任意账号的高敏身份 —— 二次验证形同虚设。
		//
		//   现在多一个 phone 条件:sys_sms_code 那行的 phone 就是当初发短信时
		//   的收件号码(明文,size:16),而这里比的是**账号登记的手机号**。
		//   攻击者能给自己发码,但那条码的 phone 不是受害账号的 → 查不到。
		//
		//   规则本体在 consumeSmsCode(与 VerifyAccountSms 共用同一处实现)。
		if err := s.consumeSmsCode(ctx, callerPhone, model.SmsSceneChangePhone, smsCode); err != nil {
			return nil, err
		}
	}

	// 3. 收集所有 active 身份 projectIDs
	//
	// Order 与 Login / RefreshToken 保持一致:主身份在前,其余按 id 升序。
	// 前端在"没有持久化的当前身份"时会回落到 identities[0],排序不一致会
	// 让人切换一次身份后又被刷回另一个身份。
	var allIdentities []model.SysUserIdentity
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", caller.UserID, true).
		Order("is_primary DESC, id ASC").
		Find(&allIdentities).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询身份失败").WithDetail(err.Error())
	}
	projectIDs := collectProjectIDs(allIdentities)

	// 4. user 已在第 2 步加载(为了拿登记手机号做短信校验)

	// 5. 签发新 token
	//
	// 两次签发**必须用两个独立结构**:sign() 会覆写整个 RegisteredClaims(含 jti),
	// 复用同一个 claims 签两次,内存里那份 claims 会停留在第二张(下签)token 上。
	// 见 auth.Claims.Fork。
	//
	// ⚠️ **M148(2026-11-01):SessionID 这次要换。**
	//
	//   以前是"沿用当前会话的 sid" —— 理由是"切换身份是同一台设备上的身份切换,
	//   不是新会话"。那个理由**只考虑了登出**,漏了更要紧的那一头:
	//
	//   切换时服务端只拿得到 **access** 的 jti,拿不到切换**之前**那张
	//   **refresh** 的 jti ⇒ 吊销不了它。于是旧 refresh 续期时:
	//
	//     jti 吊销态  :没被吊(只吊了 access 那张)   ✓ 通过
	//     会话吊销态  :sid 没变(还是同一条会话)     ✓ 通过
	//     作废线      :切换身份不推进               ✓ 通过
	//     ⇒ 三道闸全过,旧 refresh 能换发一份**旧身份**的 access
	//
	//   危害:用户从高危身份切到低危身份(超管 → 普通用户)之后,手里那张旧
	//   refresh **还能继续拿超管**,最长 720h(30 天)。"切身份"这个动作在
	//   安全边界上等于什么也没做。
	//
	//   修法:**吊销旧 sid + 发新 sid**。sid 是**会话级**判据,refresh 续期时
	//   必查(RefreshToken 第 2.6 步)⇒ 旧 sid 一吊,那条会话的 access +
	//   refresh 一起死,**不管中间轮换过几轮**(轮换只换 jti,sid 不变)。
	//   换 sid 之后登出 / 强制下线照旧按 sid 定位(登记簿那一行也跟着换,见下)。
	oldSID := caller.SessionID
	newSID := auth.NewSessionID()
	base := &auth.Claims{
		UserID:     u.ID,
		IdentityID: target.ID,
		Username:   u.Username,
		RoleCode:   target.RoleCode,
		ProjectIDs: projectIDs,
		SessionID:  newSID,
	}
	access, err := s.jwt.SignByAudience(auth.AudienceAdmin, base.Fork())
	if err != nil {
		return nil, errcode.New(errcode.Internal, "签发 access token 失败").WithDetail(err.Error())
	}
	// 原本是 `refresh, _ :=`:吞错 → 返 200 + 空 refresh_token,
	// 前端把空串当新 token 存下来,下次续期带着空串去,报的还是"refresh 无效",
	// 真正错因被埋掉。
	refresh, err := s.jwt.RefreshByAudience(auth.AudienceAdmin, base.Fork())
	if err != nil {
		return nil, errcode.New(errcode.Internal, "签发 refresh token 失败").WithDetail(err.Error())
	}
	ttl := s.jwt.MustGet(auth.AudienceAdmin).AccessTTL()
	rttl := s.jwt.MustGet(auth.AudienceAdmin).RefreshTTL()

	// 6~8. 换会话
	//
	//    ⚠️ 顺序:**先签好新 token,再吊旧的** —— 反过来的话,吊销失败就变成
	//    新旧都死(用户被踢下线,而且手里没有新 token 可用)。
	//
	//    四步各自的展开与理由见 rotateSession 的注释。
	s.rotateSession(ctx, caller, oldSID, newSID, &target, ttl, rttl)

	// 切换身份后前端要立刻知道"当前身份"变成哪个,否则会在 nextTick 用
	// identities[0] 覆盖掉这里的 target.ID(切回第一个身份)。所以这里必须传
	// target.ID 而不是 0。
	return &AuthTokens{
		Token:            access,
		RefreshToken:     refresh,
		ExpiresIn:        int(ttl.Seconds()),
		RefreshExpiresIn: int(rttl.Seconds()),
		User:             NewCurrentUserView(&u, allIdentities, target.RoleCode, target.ID),
	}, nil
}

// rotateSession 切换身份时的"会话换代" —— 2026-11-01(M148)。
//
// 四步,**顺序不能乱**:
//
//  1. **吊销旧 sid** —— 切换之前那条会话的 access + refresh 一起死。
//     TTL 取 **refresh 的寿命**(会话里寿命最长的就是它)—— 用 access 的 8h
//     的话,吊销记录会先过期,而旧 refresh 还活着,等于没吊
//     (这正是 Logout 里同一个坑)。
//     ⚠️ 失败**不阻塞切换**:新 token 已经签好,用户切过去是成功的。
//     但这意味着"切换前那张 refresh 还能换发旧身份"—— 那是安全事件,
//     记 ERROR 让告警能捞到(WARN 会被日常噪声淹掉)。
//  2. **吊销当前 access 的 jti** —— 保留原行为(access 立刻失效,
//     不必等会话态传播)。
//  3. **登记簿(sys_login_log)那一行跟着换 session_id** ——
//     ⚠️ **必须换**:一次登录 = 一行 = 一条会话,靠 session_id 关联 sid。
//     不换的话三处一起失灵:
//     - touchSession 找不到行 ⇒ last_active_at 永远不更新,「活跃设备」列表里这条会话看起来一直是死的
//     - Logout 按 sid 定位写 logout_at ⇒ 找不到行,列表里那条会话永远显示"未登出"(幽灵会话)
//     - 「强制下线」按 sid 吊销 ⇒ 吊的是已经作废的旧 sid,报"会话不存在或不属于当前用户",根本踢不掉
//     ⚠️ 换不进去**不阻塞切换**:它只影响"活跃设备列表"的精度,不影响安全
//     (第 1 步的吊销已经做了)。但要留痕 —— 否则就是"列表莫名其妙不对",
//     没人查得到原因。
//  4. **把 caller 改成"切换后的身份"** ——
//     ⚠️ **M148:以前这里什么都不改**,而 handler 那行
//     `toRole := caller.RoleCode // SwitchIdentity 内部已改 caller.RoleCode`
//     一直以为"内部改了" ⇒ 角色切换审计的 to_role **恒等于** from_role
//     ⇒ biz_role_switch_audit 那张表永远记不出"角色变了"
//     (而它存在的意义就是记这个)。
//     caller 是**每请求一个**的临时对象(中间件从 token 解析出来的),
//     改它不会影响别的请求。中间件的鉴权判定在 handler **之前**就跑完了,
//     改完的 RoleCode 只影响"事后记审计"那几行。
func (s *authService) rotateSession(
	ctx context.Context,
	caller *Caller,
	oldSID, newSID string,
	target *model.SysUserIdentity,
	ttl, rttl time.Duration,
) {
	// oldSID 为空 = 老 token 没有会话绑定(见 pkg/auth/session.go 的降级说明),
	// 那种会话没有 sid 可吊,只能靠下面第 2 步吊 access 的 jti。
	hasOld := oldSID != "" && oldSID != newSID

	// 1. 吊销旧 sid(旧会话的 access + refresh 一起死)
	if hasOld {
		if err := auth.RevokeSession(ctx, oldSID, rttl); err != nil {
			logger.L().Error("吊销旧会话失败:切换前那张 refresh 仍可换发旧身份的 access",
				zap.String("sid", oldSID), zap.Uint64("user_id", caller.UserID), zap.Error(err))
		}
	}

	// 2. 吊销当前 access 的 jti
	if err := auth.DefaultRevocation().Revoke(ctx, caller.JTI, ttl); err != nil {
		logger.L().Warn("revoke jti on switch-identity failed",
			zap.String("jti", caller.JTI), zap.Error(err))
	}

	// 3. 登记簿那一行跟着换 sid
	if hasOld {
		if err := s.db.WithContext(ctx).Model(&model.SysLoginLog{}).
			Where("user_id = ? AND session_id = ?", caller.UserID, oldSID).
			Update("session_id", newSID).Error; err != nil {
			logger.L().Warn("更新登记簿 session_id 失败(活跃设备列表会不准)",
				zap.String("old_sid", oldSID), zap.String("new_sid", newSID),
				zap.Uint64("user_id", caller.UserID), zap.Error(err))
		}
	}

	// 4. 把 caller 改成"切换后的身份"
	caller.IdentityID = target.ID
	caller.RoleCode = target.RoleCode
	caller.RoleName = target.RoleName
}

// accountSmsScenes 自助短信(发给**自己账号登记的手机号**)允许的 scene
//
//	为什么 `login` 不在里面:login 场景是"手机号还没绑到这个账号"时用的
//	(POST /public/auth/login-sms),自助发给自己登记的号没有意义 ——
//	而且会让人误以为"我已经用短信登录过了"。
//
//	剩下的三个正好覆盖两个前端场景:
//	  · change_phone   → 切到高敏身份(码由 SwitchIdentity 消费)
//	  · reset_password → 个人中心改密码(码由 VerifyAccountSms 消费)
//	  · view_full_phone→ 查完整手机号(脱敏反查)
//	  · mfa            → 个人中心开/关登录二次验证(M150,码由 EnableMfa /
//	                     DisableMfa 消费 —— ⚠️ VerifyAccountSms 不行,它会
//	                     先把码烧掉,后面的开关调用拿到的是"已使用")
var accountSmsScenes = map[string]struct{}{
	model.SmsSceneChangePhone:   {},
	model.SmsSceneResetPassword: {},
	model.SmsSceneViewFullPhone: {},
	model.SmsSceneMFA:           {},
}

// SendAccountSms 给当前账号登记的手机号发验证码(2026-10-28 M104)
//
// 为什么不让前端自己发(POST /public/auth/sms/send):
//
//  1. **前端没有明文手机号**。/auth/me 只返脱敏值(138****5678),
//     而 validPhone 要求 11 位纯数字 —— 前端拿着脱敏号发,后端直接
//     「手机号格式错误」。这两条链路在 M104 之前从来没走通过:
//     切高敏身份(IdentitySwitcher)和个人中心改密码(/profile)都是死的。
//  2. **验证码要绑在账号登记的手机号上**。由服务端取 phone 并落进
//     sys_sms_code.phone,消费方校验时用同一个 phone 查 ——
//     攻击者给自己发的码(phone 不是受害账号的)就查不到。
//
// scene 由调用方指定(必须在 accountSmsScenes 白名单里)。
// 「切高敏身份」用 change_phone:阿里云短信模板只有 4 个
// (aliyun.sms.template.{login,view_full_phone,reset_password,change_phone}),
// 没有单独的 switch_identity 模板;权限模型设计V1.0.md §1494 也是这么写的。
func (s *authService) SendAccountSms(ctx context.Context, caller *Caller, scene string) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if _, ok := accountSmsScenes[scene]; !ok {
		return errcode.New(errcode.InvalidParam, "该场景不支持自助发送验证码")
	}
	phone, err := s.accountPhone(ctx, caller.UserID)
	if err != nil {
		return err
	}
	// 发短信(与公开发短信走同一套限流 + 冷却 + 落库)
	// SendSms 内部会:
	//   · 校验 scene 合法(上面已白名单过,这里是双保险)
	//   · smslimit.Allow(phone, scene) —— per phone+scene 冷却
	//   · DB 兜底冷却(Redis 挂掉时也拦得住)
	//   · UPSERT sys_sms_code(phone, scene, code) —— **phone 落库**,
	//     这正是消费方绑定手机号时比对的那一列
	return s.SendSms(ctx, phone, scene)
}

// VerifyAccountSms 校验并消费发给当前账号登记手机号的验证码(2026-10-28 M104)
//
// 与公开的 VerifySms 的区别只有一个:**手机号由服务端取**,前端不传。
// 校验规则完全一致 —— 绑定手机号那条规则只有 consumeSmsCode 一处实现。
func (s *authService) VerifyAccountSms(ctx context.Context, caller *Caller, scene, code string) (bool, error) {
	if caller == nil || caller.UserID == 0 {
		return false, errcode.New(errcode.Unauthorized, "未登录")
	}
	if _, ok := accountSmsScenes[scene]; !ok {
		return false, errcode.New(errcode.InvalidParam, "该场景不支持自助校验验证码")
	}
	phone, err := s.accountPhone(ctx, caller.UserID)
	if err != nil {
		return false, err
	}
	if err := s.consumeSmsCode(ctx, phone, scene, code); err != nil {
		return false, err
	}
	return true, nil
}

// accountPhone 取账号登记的手机号(明文,本期 sys_user.phone 就是明文落库)
//
// 空号 / 格式不对都返 InvalidParam —— 调用方要区分"没这个账号"和
// "这个账号没法发短信",所以用户不存在走 NotFound。
func (s *authService) accountPhone(ctx context.Context, userID uint64) (string, error) {
	phone, err := s.rawAccountPhone(ctx, userID)
	if err != nil {
		return "", err
	}
	if phone == "" {
		return "", errcode.New(errcode.InvalidParam, "账号未登记手机号,无法使用短信验证")
	}
	if !validPhone(phone) {
		return "", errcode.New(errcode.InvalidParam, "账号登记的手机号格式错误")
	}
	return phone, nil
}

// rawAccountPhone 取账号登记的手机号**原文**(可能是空串)
//
//	与 accountPhone 的区别只有一个:**不要求"有号"**。
//	换绑手机号那条链路(C5)必须能处理"当前压根没绑手机号"的账号
//	—— 那恰恰是最需要换绑的一批人。
//
//	⚠️ 账号不存在 → NotFound;查询出错 → Internal。
//	  这两种要分开:调用方要区分"没这个账号"和"这个账号查不出来"。
//	  (老实现把两种都报成 NotFound —— 数据库抖一下就变成"用户不存在"。)
func (s *authService) rawAccountPhone(ctx context.Context, userID uint64) (string, error) {
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, userID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", errcode.New(errcode.NotFound, "用户不存在")
		}
		return "", errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	return decryptPII(u.Phone), nil
}

// ===== 换绑手机号(2026-10-29 C5)=====
//
// 这条链路和「自助短信」(SendAccountSms)的方向正好相反:
//
//	自助短信   → 码发给【旧号】(服务端从 sys_user 取),证明"用户还占着旧号"
//	换绑手机号 → 码发给【新号】(前端传),证明"用户占着新号"
//
// 换绑要证明的恰恰是**新号**(旧号马上就要失效了,证明它没意义),
// 所以新号必须由前端传 —— 服务端手上只有旧号。
//
// ⚠️ **scene 复用 change_phone,靠 phone 维度天然隔离**:
//   两条链路的验证码都落在 sys_sms_code,靠 (phone, scene) 区分。
//   切高敏身份那条的 phone = 旧号,换绑这条的 phone = 新号,
//   而新旧号**不允许相同**(见下)⇒ 两个集合永不相交。
//
// ⚠️ **新旧号相同直接拒绝**:否则用户在"换绑"弹窗里发的那条码,
//   正好也是"切高敏身份"弹窗里能用的那条 —— 一个码两处用,事后说不清是谁在用。

// SendBindPhoneCode 给新手机号发验证码(换绑用)
func (s *authService) SendBindPhoneCode(ctx context.Context, caller *Caller, phone string) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if !validPhone(phone) {
		return errcode.New(errcode.InvalidParam, "手机号格式错误")
	}
	current, err := s.rawAccountPhone(ctx, caller.UserID)
	if err != nil {
		return err
	}
	if current != "" && current == phone {
		return errcode.New(errcode.InvalidParam, "新手机号与当前手机号相同")
	}
	if err := s.ensurePhoneAvailable(ctx, phone, caller.UserID); err != nil {
		return err
	}
	// 复用 SendSms —— 限流 / 冷却 / 落库 / 下发都在那一处实现,不在这里重写一遍:
	//   · 限流:per phone+scene 冷却(Redis)+ DB 兜底冷却
	//   · 落库:sys_sms_code(phone=**新号**, scene=change_phone, code)
	//     —— 消费方 BindPhone 按新号查,绑定关系就成立在这里
	return s.SendSms(ctx, phone, model.SmsSceneChangePhone)
}

// BindPhone 换绑手机号
//
//	短信二次验证只发生在"新号"这一侧 —— 这是 SRS §2.2.3 的原话:
//	「输入新手机号并完成新号验证」。旧号不再单独验证(它马上就要失效了)。
//
//	⚠️ **phone 与 phone_hash 必须一起写**(M100 审计定的硬约束):
//	  只写 phone 的话,"按手机号短信登录 / 短信重置密码"会直接失配 ——
//	  那两条流程是按 phone_hash 查人的(见 LoginBySms / ResetPasswordBySms)。
func (s *authService) BindPhone(ctx context.Context, caller *Caller, phone, code string) (*CurrentUserView, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}
	if !validPhone(phone) {
		return nil, errcode.New(errcode.InvalidParam, "手机号格式错误")
	}
	current, err := s.rawAccountPhone(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	if current != "" && current == phone {
		return nil, errcode.New(errcode.InvalidParam, "新手机号与当前手机号相同")
	}
	if err := s.ensurePhoneAvailable(ctx, phone, caller.UserID); err != nil {
		return nil, err
	}
	// 1. 校验并消费发给**新号**的验证码
	//    (一次性:用错号 / 过期 / 已用过,都拒 —— 规则只有 consumeSmsCode 一处实现)
	if err := s.consumeSmsCode(ctx, phone, model.SmsSceneChangePhone, code); err != nil {
		return nil, err
	}
	// 2. 旧号上所有未使用的验证码一律作废
	//
	//	为什么:旧号马上就不是这个账号的号了,而**手机号是会回收的**。
	//	旧号上残留的码(scene=login 之类)如果还在有效期内,
	//	等旧号被绑到别的账号上,那条码就能登进**新主人**的账号。
	//	⇒ 换绑成功 = 旧号上的一切作废。
	//
	//	放在写库**之前**:作废失败就整体失败,不会留下"号改了但旧码还能用"的中间态。
	if current != "" && current != phone {
		if err := s.invalidateSmsCodes(ctx, current); err != nil {
			return nil, errcode.New(errcode.Internal, "作废旧手机号验证码失败").WithDetail(err.Error())
		}
	}
	// 3. 双写:phone(明文)+ phone_hash(盲索引)
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", caller.UserID).
		Updates(map[string]any{
			"phone":      phone,
			"phone_hash": desensitize.PhoneHash(phone),
		}).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "更新手机号失败").WithDetail(err.Error())
	}
	// 4. 返最新视图 —— 前端整份替换 store,少返字段等于把它洗空(同 UpdateProfile)
	return s.currentUserView(ctx, caller)
}

// ensurePhoneAvailable 新号不能已经被别的账号占用
//
//	为什么要查:phone_hash 是"按手机号找人"的唯一依据
//	(短信登录 / 短信重置密码都靠它)。两个账号共用一个号 ⇒
//	那条短信会被消费到**先写入**的那个账号头上,后写的那个永远收不到。
//
//	两条都查:phone_hash 是盲索引(新写入的数据都有),
//	phone 是明文(老数据 / 手工改库的兜底)—— 少查一条就漏掉一批历史账号。
func (s *authService) ensurePhoneAvailable(ctx context.Context, phone string, selfUserID uint64) error {
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id <> ? AND (phone_hash = ? OR phone = ?)",
			selfUserID, desensitize.PhoneHash(phone), phone).
		Count(&cnt).Error; err != nil {
		return errcode.New(errcode.Internal, "校验手机号失败").WithDetail(err.Error())
	}
	if cnt > 0 {
		return errcode.New(errcode.Conflict, "该手机号已被其他账号绑定")
	}
	return nil
}

// invalidateSmsCodes 作废某个手机上所有未使用的验证码
func (s *authService) invalidateSmsCodes(ctx context.Context, phone string) error {
	now := model.Time{Time: time.Now()}
	return s.db.WithContext(ctx).Model(&model.SysSmsCode{}).
		Where("phone = ? AND used_at IS NULL", phone).
		Update("used_at", &now).Error
}

// consumeSmsCode 校验并消费一条短信验证码(code + scene + phone 三者全等)
//
//	⚠️ **phone 必带 —— 这是 M104(2026-10-28)补上的那条约束。**
//
//	以前 SwitchIdentity 里是 `WHERE code = ? AND scene = ?` —— **不带 phone**。
//	那意味着:任何人可以走公开接口 /public/auth/sms/send 给**自己的**手机号
//	发一条 scene=change_phone 的码(那 4 个 scene 里 change_phone 对公众开放),
//	拿到码之后拿来切**任意账号**的高敏身份 —— 二次验证形同虚设。
//
//	现在多一个 phone 条件:sys_sms_code 那行的 phone 就是当初发短信时的
//	收件号码(明文,size:16),而这里比的是**账号登记的手机号**。
//	攻击者能给自己发码,但那条码的 phone 不是受害账号的 → 查不到。
//
//	一次性:校验通过即置 used_at;并发双提交只有一条能成功。
func (s *authService) consumeSmsCode(ctx context.Context, phone, scene, code string) error {
	if code == "" {
		return errcode.New(errcode.SmsCodeInvalid, "请输入验证码")
	}
	var rec model.SysSmsCode
	err := s.db.WithContext(ctx).
		Where("code = ? AND scene = ? AND phone = ?", code, scene, phone).
		Order("send_at DESC").
		First(&rec).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.SmsCodeInvalid, "短信验证码错误或已过期")
		}
		return errcode.New(errcode.Internal, "查询验证码失败").WithDetail(err.Error())
	}
	if rec.UsedAt != nil && !rec.UsedAt.IsZero() {
		return errcode.New(errcode.SmsCodeInvalid, "短信验证码已使用")
	}
	if time.Since(rec.SendAt.Time) > s.codeTTL() {
		return errcode.New(errcode.SmsCodeInvalid, "短信验证码已过期")
	}
	// 标记已用 (单次性: 条件 update + 失败上报, 不再 silent)
	now := model.Time{Time: time.Now()}
	res := s.db.WithContext(ctx).Model(&model.SysSmsCode{}).
		Where("id = ? AND used_at IS NULL", rec.ID).
		Update("used_at", &now)
	if res.Error != nil {
		return errcode.New(errcode.Internal, "标记验证码已用失败").WithDetail(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return errcode.New(errcode.SmsCodeInvalid, "短信验证码已使用")
	}
	return nil
}

// ===== 登录二次验证 MFA(M150)=====
//
// # 因子与前提
//
// 因子 = "绑定手机收码"(scene=mfa)。前提 = 账号已登记手机号 ——
// 正是用户原话「个人中心绑定了手机,就可以开启 MFA」:
// 未绑手机的账号**开不了**(EnableMfa 直接拒),也就不存在"开了却收不到码"。
// 唯一会撞上 fail-closed 的情形是"开了之后手机号被清掉",那是管理员清的,
// 恢复也归管理员(POST /system/users/:id/mfa/disable)。
//
// # 为什么开关也要验证码
//
// 关 MFA 的验证码不是仪式感:**偷到会话的人第一件事就是把 MFA 关掉**,
// 之后再用偷来的密码就能无阻登录。开关动作本身验一次"持机",
// 让"改安全设置"和"用安全设置"付出同样的代价。
//
// # 为什么码由 EnableMfa/DisableMfa 直接消费,不走 VerifyAccountSms
//
// 验证码是**一次性**的(校验通过即置 used_at)。VerifyAccountSms 会先把码烧掉,
// 后面那个真正干活的调用拿到的一定是"短信验证码已使用"。
// 这就是 SmsCodeGuard 文档里 mode="delegate" 存在的原因。

// SendMfaLoginCode 登录 MFA 步骤的重发(公开端点 /public/auth/mfa/send 的后端)
//
// 密码必须**先验对**才发码 —— 与 Login 的自动发码同一个权限门槛:
// 不知道密码的人连触发发码的资格都没有。loginlock 照常计数,
// 这个端点不能变成绕过登录锁定的撞密码通道。
func (s *authService) SendMfaLoginCode(ctx context.Context, username, password, loginIP string) error {
	if err := MustNoEmpty("username", username); err != nil {
		return err
	}
	if err := MustNoEmpty("password", password); err != nil {
		return err
	}
	// 登录锁定预检:与 Login 同一口径(Redis 不可用时降级放行)
	if err := loginlock.PreCheck(ctx, username, loginIP, 10); err != nil {
		return err
	}
	var u model.SysUser
	if err := s.db.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 与 Login 一致的口径:不区分"用户不存在"和"密码错",防账号枚举
			loginlock.AfterFail(ctx, username, loginIP, s.lock)
			return errcode.New(errcode.LoginFailed, "账号或密码错误")
		}
		return errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	if !u.IsActive || u.IsLeave || (u.LockedUntil != nil && !u.LockedUntil.IsZero() && u.LockedUntil.Time.After(time.Now())) {
		loginlock.AfterFail(ctx, username, loginIP, s.lock)
		return errcode.New(errcode.LoginFailed, "账号或密码错误")
	}
	if !auth.CheckPassword(u.PasswordHash, password) {
		loginlock.AfterFail(ctx, username, loginIP, s.lock)
		return errcode.New(errcode.LoginFailed, "账号或密码错误")
	}
	// 密码对,但这个账号压根没开 MFA —— 重发无意义(也不用防枚举:
	// 能走到这里的都验过了密码,不是外人)
	if !u.MfaEnabled {
		return errcode.New(errcode.StateInvalid, "该账号未开启二次验证")
	}
	phone := decryptPII(u.Phone)
	if phone == "" {
		return errcode.New(errcode.MfaRequired,
			"该账号已开启二次验证但未绑定手机号,请联系管理员关闭二次验证")
	}
	return s.SendSms(ctx, phone, model.SmsSceneMFA)
}

// setMfaEnabled 开/关 MFA 的公共落点(两处只差一个目标值和方向校验)
func (s *authService) setMfaEnabled(ctx context.Context, caller *Caller, smsCode string, enable bool) (*CurrentUserView, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, caller.UserID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "用户不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	if enable == u.MfaEnabled {
		// 幂等请求当成状态错误报:连点两次"开启"不该静默成功,
		// 否则前端一遍遍消费验证码,用户以为每次都重新开了一遍
		if enable {
			return nil, errcode.New(errcode.StateInvalid, "该账号已开启二次验证")
		}
		return nil, errcode.New(errcode.StateInvalid, "该账号未开启二次验证")
	}
	// 验证码发到**登记手机号**上 —— 未绑手机的账号在这里被拦下,
	// 这正是"绑定了手机才可以开启 MFA"那条前提的执行点
	phone, err := s.accountPhone(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.consumeSmsCode(ctx, phone, model.SmsSceneMFA, smsCode); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", caller.UserID).
		Update("mfa_enabled", enable).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "更新二次验证状态失败").WithDetail(err.Error())
	}
	u.MfaEnabled = enable
	// 返回最新视图:前端整份替换 userInfo,界面上的 MFA 状态立即可见
	// (与 UpdateProfile / BindPhone 同一口径 —— 少返一个字段 = 把那个字段洗空)
	var identities []model.SysUserIdentity
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", caller.UserID, true).
		Order("is_primary DESC, id ASC").
		Find(&identities).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询身份失败").WithDetail(err.Error())
	}
	return NewCurrentUserView(&u, identities, caller.RoleCode, caller.IdentityID), nil
}

// EnableMfa 开启登录二次验证
func (s *authService) EnableMfa(ctx context.Context, caller *Caller, smsCode string) (*CurrentUserView, error) {
	return s.setMfaEnabled(ctx, caller, smsCode, true)
}

// DisableMfa 关闭登录二次验证
func (s *authService) DisableMfa(ctx context.Context, caller *Caller, smsCode string) (*CurrentUserView, error) {
	return s.setMfaEnabled(ctx, caller, smsCode, false)
}

// isHighRiskRole 切换到这些角色时必须 SMS 二次验证
// (与 web/src/components/layout/IdentitySwitcher.tsx highRisk 保持一致)
var highRiskRolesForSwitch = map[string]struct{}{
	"role_super_admin":       {},
	"role_platform_ops_lead": {},
	"role_platform_ops":      {},
	"role_finance":           {}, // 预留(当前 seed 暂未启用)
}

func isHighRiskRole(roleCode string) bool {
	_, ok := highRiskRolesForSwitch[roleCode]
	return ok
}

// UpdateProfile 更新个人资料
//   - 只允许更新 nickname / email / realName / avatar
//   - phone 变更需 SMS 二次验证(本期范围外)
//   - 邮箱格式校验
//   - 昵称长度校验
//
// ⚠️ **M113(2026-10-29):返回的是 *CurrentUserView(不是裸 model.SysUser)**
//
//	理由见 service/auth.go 的接口注释 —— 返裸 SysUser 会让前端
//	`setUserInfo(updated)` 把身份列表 / 超管标记 / 当前身份 / 手机号全洗空。
func (s *authService) UpdateProfile(ctx context.Context, caller *Caller, dto UpdateProfileDTO) (*CurrentUserView, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}

	// 构造 updates(只更新非 nil 字段)
	updates := map[string]any{}
	if dto.Nickname != nil {
		if *dto.Nickname == "" {
			return nil, errcode.New(errcode.InvalidParam, "昵称不能为空")
		}
		if len(*dto.Nickname) > 64 {
			return nil, errcode.New(errcode.InvalidParam, "昵称不能超过 64 字符")
		}
		updates["nickname"] = *dto.Nickname
	}
	if dto.Email != nil && *dto.Email != "" {
		if !isValidEmail(*dto.Email) {
			return nil, errcode.New(errcode.InvalidParam, "邮箱格式错误")
		}
		// Email 字段在 schema 中是 AES 加密;本期不直接更新加密值
		// TODO(后续 phase):接入 crypto.Field().Encrypt + crypto.Blind().Email
		updates["email"] = *dto.Email
	}
	if dto.RealName != nil {
		if len(*dto.RealName) > 64 {
			return nil, errcode.New(errcode.InvalidParam, "真实姓名不能超过 64 字符")
		}
		updates["real_name"] = *dto.RealName
	}
	// ⚠️ **没有 avatar 分支** —— M124 把 `UpdateProfileDTO.Avatar` 删了。
	// 头像只能**上传**(UserService.SetAvatar),不能在这里贴任意 URL。
	// 理由见 auth.go 里 UpdateProfileDTO 的注释(跟踪像素)。
	if len(updates) > 0 {
		// 写 DB
		if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
			Where("id = ?", caller.UserID).
			Updates(updates).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "更新个人资料失败").WithDetail(err.Error())
		}
	}

	// 返回**最新**的视图(而不是只回 DTO 里改过的那几个字段)——
	// 前端是整份替换 store 里的 userInfo,少返字段等于把它洗空。
	return s.currentUserView(ctx, caller)
}

// platformFlags 从**当前身份**的角色码判定平台超管 / 平台运维标记
//
// ⚠️ **M113(2026-10-29)抽出来的**:以前这段判定只写在 GetCurrentUser 里,
// 现在 currentUserView(UpdateProfile 走的那条)也要用同一套口径 ——
// 两处各写一遍,改了一处忘了另一处,就会出现
// "登录时是超管、改完资料标签没了"这种鬼故事。
//
// ⚠️ **M149(2026-11-01)口径又收窄了一次**:从「身份**列表**」改成「当前身份」。
//
//	M113 把"到处各写一遍的判定"收成一处,但那一处收的是**列表** ——
//	而"列表"恰恰是错的来源:多身份用户(超管 + 普通身份)切到普通身份后,
//	标志仍是超管 ⇒ 界面显示的身份与实际权限对不上。
//
//	现在只认**当前身份**的角色码 —— 与执行层(RBAC 中间件的 `caller.RoleCode`)
//	完全同一口径。
//
//	⚠️ 别再把"身份列表"传进来 —— 那正是这个洞的形状。
func platformFlags(roleCode string) (isSuper, isOps bool) {
	switch roleCode {
	case model.RoleSuperAdmin:
		isSuper = true
	case model.RolePlatformOps:
		isOps = true
	}
	return isSuper, isOps
}

// currentUserView 取当前用户的完整视图(用户 + 身份 + 平台标记)
//
// ⚠️ 为什么不用 GetCurrentUser:那个方法还会把**整棵资源树**查出来
// (超管是"全部 active 资源"),而个人资料更新根本不需要资源 ——
// 为了返一个 user 而拉全量资源,代价和风险都不成比例。
func (s *authService) currentUserView(ctx context.Context, caller *Caller) (*CurrentUserView, error) {
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, caller.UserID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "用户不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}

	var identities []model.SysUserIdentity
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", caller.UserID, true).
		Order("is_primary DESC, id ASC").
		Find(&identities).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询身份失败").WithDetail(err.Error())
	}

	return NewCurrentUserView(&u, identities, caller.RoleCode, caller.IdentityID), nil
}

// isValidEmail 简单邮箱校验
func isValidEmail(s string) bool {
	if len(s) < 5 || len(s) > 128 {
		return false
	}
	at := -1
	for i, c := range s {
		if c == '@' {
			if at != -1 {
				return false // 多个 @
			}
			at = i
		}
	}
	if at <= 0 || at >= len(s)-1 {
		return false
	}
	if !strings.Contains(s[at+1:], ".") {
		return false
	}
	return true
}

// GetCurrentUser 当前用户信息(供前端 /auth/me 调)
//  1. 加载 sys_user
//  2. 加载 sys_user_identity(active)—— **全部**活跃身份(前端身份切换器要这张列表)
//  3. 加载资源列表:**按当前身份**的 role_code → sys_role_resource → sys_resource
//     super_admin 走 special path:返回所有 active resource(本地 dev 没 casbin policy 时也可用)
//
// ⚠️ **M149(2026-11-01):参数从 userID 改成 caller**
//
//	以前只传 userID ⇒ 服务层只知道"这个用户是谁",不知道"他现在以哪个身份行事",
//	于是资源树和平台标记只能按"**所有身份**的并集"算 ——
//	多身份用户(超管 + 普通身份)切到普通身份后,菜单/按钮**完全不变**
//	(超管那一套全还在),点下去后端才 403 ⇒ 界面显示的和实际能做的对不上。
//
//	改成传 caller 之后,"当前身份"是**服务端权威值**(token claims 里解出来的),
//	不用再靠猜 —— 与执行层(RBAC 中间件用 `caller.RoleCode`)完全同一口径。
func (s *authService) GetCurrentUser(ctx context.Context, caller *Caller) (*model.SysUser, []model.SysUserIdentity, []model.SysResource, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, nil, nil, errcode.New(errcode.Unauthorized, "未登录")
	}

	// 1. 用户
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, caller.UserID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, nil, errcode.New(errcode.NotFound, "用户不存在")
		}
		return nil, nil, nil, errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	if !u.IsActive || u.IsLeave {
		return nil, nil, nil, errcode.New(errcode.NoActiveIdentity, "账号已停用")
	}

	// 2. 身份(全部活跃身份 —— 这张列表是**身份切换器**的输入,不能按当前身份裁)
	var identities []model.SysUserIdentity
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", caller.UserID, true).
		Order("is_primary DESC, id ASC").
		Find(&identities).Error; err != nil {
		return nil, nil, nil, errcode.New(errcode.Internal, "查询身份失败").WithDetail(err.Error())
	}

	// 3. 资源:**按当前身份**算(M149)
	resources, err := s.resourcesForRole(ctx, caller.RoleCode, "")
	if err != nil {
		return nil, nil, nil, err
	}

	// 4. 平台标记(isSuper / isOps)**不在这里返** —— 它们由
	//    NewCurrentUserView 按 caller.RoleCode 一次性派生(唯一口径 platformFlags)。
	//    以前这里返两个 bool、handler 再传给 view,等于同一个判定做了两遍。
	return &u, identities, resources, nil
}

// AccessibleResources 当前 caller 可访问的资源(供前端动态菜单/按钮控制)
//   - **按当前身份**算(M149):caller.RoleCode 就是 token 里当前身份的角色
//   - super_admin:全部 active
//   - 其他:sys_role_resource 聚合
//   - typeFilter: "" 全部, "menu" 仅菜单, "api" 仅 api, "button" 仅按钮
//
// ⚠️ **M149(2026-11-01):以前按"这个用户所有身份"的并集算**
//
//	多身份用户(超管 + 普通身份)从超管切到普通身份后,菜单/按钮**完全不变**
//	(超管那一套全还在),点下去后端才 403 —— 界面显示的和实际能做的对不上。
//	⇒ 现在与执行层(RBAC 中间件的 caller.RoleCode)同一口径:
//	**界面显示的 = 实际能做的**。
func (s *authService) AccessibleResources(ctx context.Context, caller *Caller, typeFilter string) ([]model.SysResource, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}
	return s.resourcesForRole(ctx, caller.RoleCode, typeFilter)
}

// resourcesForRole 按**单个角色**算它能访问的资源
//
// ⚠️ **M149(2026-11-01)**:这段逻辑以前在 AccessibleResources 和 GetCurrentUser
//
//	里各写一遍,而且两处都是按"**这个用户所有身份**的 role_code 并集"算 ——
//	多身份用户(超管 + 普通身份)切到普通身份后,菜单/按钮完全不变。
//	现在只按**一个**角色算,调用方传"当前身份"的 role_code。
//
//	口径与执行层(RBAC 中间件的 caller.RoleCode)一致 —— 界面显示的 = 实际能做的。
//
//	⚠️ 这里**不过滤 sys_role.is_active**:执行层(casbin 策略行)在角色停用时
//	并不会被清掉(停用只级联停用 identity),所以过滤了就会出现
//	"菜单没了但接口还能调"的反向不一致。要改得连执行层一起改。
func (s *authService) resourcesForRole(ctx context.Context, roleCode, typeFilter string) ([]model.SysResource, error) {
	// 构造 type 过滤(空 = 不加条件)
	typeClause := ""
	typeArgs := []any{true}
	if typeFilter != "" {
		typeClause = " AND sys_resource.type = ?"
		typeArgs = append(typeArgs, typeFilter)
	}
	if roleCode == model.RoleSuperAdmin {
		// 超管:全部启用中的资源
		q := s.db.WithContext(ctx).
			Where("is_active = ?", true)
		if typeFilter != "" {
			q = q.Where("type = ?", typeFilter)
		}
		var resources []model.SysResource
		if err := q.Order("sort ASC, id ASC").Find(&resources).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
		}
		return resources, nil
	}
	if roleCode == "" {
		// 没有角色 = 没有任何资源。
		// 与执行层一致:role: 空角色匹配不到任何策略行 ⇒ 一样是拒绝。
		return []model.SysResource{}, nil
	}
	var roles []model.SysRole
	if err := s.db.WithContext(ctx).
		Where("code = ?", roleCode).
		Find(&roles).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	roleIDs := make([]uint64, 0, len(roles))
	for _, r := range roles {
		roleIDs = append(roleIDs, r.ID)
	}
	if len(roleIDs) == 0 {
		return []model.SysResource{}, nil
	}
	// raw SQL 拼 type(因为 gorm 的 Joins+Where 拼字符串太啰嗦)
	sql := "rr.role_id IN ? AND sys_resource.is_active = ?" + typeClause
	var resources []model.SysResource
	if err := s.db.WithContext(ctx).
		Table("sys_resource").
		Joins("INNER JOIN sys_role_resource rr ON rr.resource_id = sys_resource.id").
		Where(sql, append([]any{roleIDs}, typeArgs...)...).
		Order("sys_resource.sort ASC, sys_resource.id ASC").
		Find(&resources).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
	}
	return resources, nil
}

// ===== LoginBySms / ResetPasswordBySms =====

// LoginBySms 短信验证码登录
// 流程: 校验短信码(scene=login) → 按 phone_hash 查用户 → 复用 Login() 的身份加载/token 签发逻辑
//
// 前置依赖: VerifySms 需先实现(Redis/DB 存验证码)
//
// ⚠️ M150:mfa_enabled 的账号**不再加验第二遍** ——
// 短信登录的验证码本来就发到登记手机号上(scene=login 且按 phone_hash 查人),
// "持机"这半个因子已经天然满足。要防的"只偷了密码"场景对短信登录不存在
// —— 它压根不收密码。scene=mfa 的码在这里消费不了( scene 不同),
// MFA 发的码也变不成短信登录的入场券。
func (s *authService) LoginBySms(ctx context.Context, phone, smsCode, loginIP, userAgent string) (*AuthTokens, error) {
	// 1. 校验短信码
	ok, err := s.VerifySms(ctx, phone, "login", smsCode)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errcode.New(errcode.SmsCodeInvalid, "短信验证码错误或已过期")
	}

	// 2. 按 phone_hash 查用户
	var u model.SysUser
	phoneHash := desensitize.PhoneHash(phone)
	err = s.db.WithContext(ctx).
		Where("phone_hash = ?", phoneHash).
		First(&u).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.LoginFailed, "账号未注册或手机号不匹配")
		}
		return nil, errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}

	// 3. 复用 Login() 流程: 状态检查 → 身份加载 → token 签发
	if u.IsLeave {
		return nil, errcode.New(errcode.NoActiveIdentity, "账号已离职,无法登录")
	}
	if !u.IsActive {
		return nil, errcode.New(errcode.NoActiveIdentity, "账号已停用")
	}

	var identities []model.SysUserIdentity
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", u.ID, true).
		Order("is_primary DESC, id ASC").
		Find(&identities).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询身份失败").WithDetail(err.Error())
	}
	if len(identities) == 0 {
		return nil, errcode.New(errcode.NoActiveIdentity, "账号未配置可用身份")
	}
	primary := identities[0]
	projectIDs := collectProjectIDs(identities)

	// 4. 更新登录态
	now := time.Now()
	_ = s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", u.ID).Updates(map[string]any{
		"last_login_at": model.Time{Time: now},
		"last_login_ip": loginIP,
		"failed_count":  0,
		"locked_until":  nil,
	}).Error

	// 5. 签发 token
	//    两次签发各用一份 Fork():sign() 会覆写整个 RegisteredClaims(含 jti),
	//    复用同一个结构签两次,内存里那份 base 会停留在**第二张**(refresh)上 ——
	//    此后 base.JTI() 读到的是新 token 的值,不是刚消费掉的那张旧 token。
	//    见 auth.Claims.Fork(含实测后果)。
	base := &auth.Claims{
		UserID:     u.ID,
		IdentityID: primary.ID,
		Username:   u.Username,
		RoleCode:   primary.RoleCode,
		ProjectIDs: projectIDs,
		SessionID:  auth.NewSessionID(),
	}
	accessToken, err := s.jwt.SignByAudience(auth.AudienceAdmin, base.Fork())
	if err != nil {
		return nil, errcode.New(errcode.Internal, "签发 access token 失败").WithDetail(err.Error())
	}
	refreshToken, err := s.jwt.RefreshByAudience(auth.AudienceAdmin, base.Fork())
	if err != nil {
		return nil, errcode.New(errcode.Internal, "签发 refresh token 失败").WithDetail(err.Error())
	}
	ttl := s.jwt.MustGet(auth.AudienceAdmin).AccessTTL()
	rttl := s.jwt.MustGet(auth.AudienceAdmin).RefreshTTL()

	// 6. 写登录成功日志
	s.recordLoginLog(ctx, u.ID, u.Username, LoginRequest{
		Username: u.Username, LoginIP: loginIP, UserAgent: userAgent,
	}, 1, "login-by-sms", base.SessionID)

	// 7. 异常登录检测(M128)
	//
	//   ⚠️ **短信登录这条路也必须检测** —— 它同样会签发 token、同样会
	//   在 sys_login_log 里留下一条"活跃会话"。
	//   只在密码登录那条路上接检测的话,一条**绕过密码的入口**就成了一条
	//   免检通道:被冒用的账号改用短信登录,本人永远收不到提醒。
	//   ⇒ 凡是"签发 token + 建会话"的入口,检测都得跟着走一遍。
	//
	//   口径与失败处理与 Login() 完全一致(检测失败只记日志,不影响登录)。
	s.detectLoginAnomaly(ctx, u.ID, base.SessionID, LoginRequest{
		Username: u.Username, LoginIP: loginIP, UserAgent: userAgent,
	}, now)

	return &AuthTokens{
		Token:            accessToken,
		RefreshToken:     refreshToken,
		ExpiresIn:        int(ttl.Seconds()),
		RefreshExpiresIn: int(rttl.Seconds()),
		User:             NewCurrentUserView(&u, identities, primary.RoleCode, primary.ID),
	}, nil
}

// ResetPasswordBySms 短信验证码自助重置密码
// 流程: 校验短信码(scene=reset_password) → 按 phone_hash 查用户 → 更新密码 hash
// → 推进该用户的作废线(该账号此前**一个令牌都不吊销**,是最严重的一处空白)
func (s *authService) ResetPasswordBySms(ctx context.Context, phone, smsCode, newPassword string) error {
	// 1. 校验短信码
	ok, err := s.VerifySms(ctx, phone, "reset_password", smsCode)
	if err != nil {
		return err
	}
	if !ok {
		return errcode.New(errcode.SmsCodeInvalid, "短信验证码错误或已过期")
	}

	// 2. 按 phone_hash 查用户
	var u model.SysUser
	phoneHash := desensitize.PhoneHash(phone)
	err = s.db.WithContext(ctx).
		Where("phone_hash = ?", phoneHash).
		First(&u).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.LoginFailed, "账号未注册或手机号不匹配")
		}
		return errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}

	// 3. 更新密码(明文 = 前端 SHA-256(明文), 库 = bcrypt(SHA-256))
	newHash, err := auth.HashPassword(newPassword)
	if err != nil {
		return errcode.New(errcode.Internal, "密码哈希失败").WithDetail(err.Error())
	}
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", u.ID).Updates(map[string]any{
		"password_hash":   newHash,
		"must_change_pwd": false,
		"failed_count":    0,
		"locked_until":    nil,
	}).Error; err != nil {
		return errcode.New(errcode.Internal, "更新密码失败").WithDetail(err.Error())
	}

	// 4. 推进作废线 —— 短信重置是**盗号后的自救路径**,恰恰是最需要强制下线的场景。
	//
	//    这个方法此前一个 token 都不吊销,是整条链路上最严重的一处空白:
	//    攻击者拿着用户的手机号收验证码重置了密码、掌握了账号,而原用户设备上
	//    那个仍然有效的会话照旧畅通 —— 攻击者可以在被踢出门的同时把原用户
	//    的东西改个遍(改收款信息、导出数据),两者在同一时间窗里并存。
	//
	//    代价:真正持有手机号的人做完这一步也会被踢下线,需要重新登录。这是
	//    正确行为 —— 他此刻并不掌握这个账号的密码,让他留在旧会话里没有道理。
	invalidateUserSessions(ctx, u.ID, "reset-password-by-sms")
	return nil
}
