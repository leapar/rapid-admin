// 异常登录检测的**接线**测试(M128)
//
// # 这组用例守的是什么
//
// `pkg/anomalylogin` 从 M45(2026-10-19)就在仓里:172 行单测、字段注释齐全、
// 清单 §1.1 #11 标着 "P2 ✅"。但它**从来没有被任何一行生产代码调用过** ——
// `grep -rn 'internal/pkg/anomalylogin'` 全仓零命中(除了它自己)。
//
// ⇒ 清单里那句痛点「用户被盗号时无法感知」**一次都没有被真正解决过**:
// 检测器写好了,但没人叫它。
//
// 这一组用例把"接线"钉死。**它们守的不是检测算法**(算法在 pkg 层有 17 条用例),
// 而是三件接线特有的事:
//
//  1. **登录链路真的会调检测** —— 拆掉那行调用,这些用例会红(可逆探针)。
//  2. **「同时在线」的口径是活动窗口,不是 refresh_ttl(30 天)** ——
//     这是本模块最锋利的一条:口径错了,一个 30 天里登录过 4 次的正常人
//     **每次登录都报异常**,阈值形同虚设,功能上线第一天就会被关掉。
//  3. **检测永远不阻断登录** —— 查库失败 / 站内信投递失败都只记日志。
//
// # 为什么单独一个文件
//
// 检测是**静默**的:不报错、不弹窗,只在真的发现异常时才产生一行 WARN。
// ⇒ "检测没跑"和"跑了但没发现异常"在日志里长得一模一样。
// 所以这里断言的是**可观测的副作用**(WARN 日志 + 站内信),而不是"调用过"。
package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/desensitize"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/sonyflake"
)

// ===== 测试替身 =====

// fakeInbox 记录投递过的站内信。
//
// 不用真的 inbox.Manager:那需要 sys_inbox_message 表,而本组用例要盯的是
// "**有没有投递**"与"**投了什么内容**",不是"站内信落库正不正常"
// (那是 pkg/inbox 自己的事)。
type fakeInbox struct {
	mu   sync.Mutex
	msgs []inbox.Message
	err  error
}

func (f *fakeInbox) Notify(_ context.Context, msg inbox.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.msgs = append(f.msgs, msg)
	return nil
}

func (f *fakeInbox) NotifyBatch(_ context.Context, msgs []inbox.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.msgs = append(f.msgs, msgs...)
	return nil
}

func (f *fakeInbox) snapshot() []inbox.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]inbox.Message, len(f.msgs))
	copy(out, f.msgs)
	return out
}

// captureAnomalyLogs 把全局 logger 换成可观测的,返回日志收集器。
//
// ⚠️ 用 observer 而不是 mock:断言的是"**有一行 WARN 说检测到异常登录**",
// 而不是"某个函数被调用过 n 次"。后者在实现换写法时假绿,前者不会。
func captureAnomalyLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.WarnLevel)
	old := logger.Default
	logger.Default = zap.New(core)
	t.Cleanup(func() { logger.Default = old })
	return logs
}

// anomalyWarnCount 数"检测到异常登录"那行 WARN 的条数
func anomalyWarnCount(logs *observer.ObservedLogs) int {
	n := 0
	for _, e := range logs.All() {
		if e.Message == "anomalylogin: 检测到异常登录" {
			n++
		}
	}
	return n
}

// countLogs 数指定 message 的日志条数。
//
// ⚠️ 不能用 logs.Len() > 0 代替 —— 同一条链路上别的模块也会记 WARN
// (比如登录日志写库失败),笼统断言会被那些日志蒙混过去(假绿)。
func countLogs(logs *observer.ObservedLogs, msg string) int {
	n := 0
	for _, e := range logs.All() {
		if e.Message == msg {
			n++
		}
	}
	return n
}

// ===== 夹具 =====

// defaultAnomalyCfg 异常登录检测的生产口径配置。
//
// ⚠️ 跟生产默认值一致(3 端 / 30 分钟窗口)—— 测试里用另一套数字的话,
// "测出来的行为"和"线上跑的行为"就不是一回事了。
func defaultAnomalyCfg() config.AnomalyConfig {
	return config.AnomalyConfig{
		Enabled:       true,
		MaxConcurrent: 3,
		ActiveWindow:  30 * time.Minute,
		// ImpossibleTravelWindow 保持 0(关)—— 生产默认也是 0,
		// 理由见 config.AnomalyConfig:本仓没有 IP 地理库,开着必然误报。
	}
}

// newAnomalySvc 构造一个**异常检测开着**的 AuthService。
func newAnomalySvc(t *testing.T, db *gorm.DB, cfg config.AnomalyConfig, notifier inbox.Notifier) AuthService {
	t.Helper()
	return NewAuthService(db, buildTestJWTMulti(t), nil, defaultSMSCfg(), cfg, notifier)
}

// seedActiveSession 写一条"仍活跃"的会话登记簿行。
//
// lastActive 是这条会话最后一次活动的时刻 —— 活动窗口就是拿它跟 cutoff 比。
func seedActiveSession(t *testing.T, db *gorm.DB, userID uint64, sid, ip, device string, lastActive time.Time) {
	t.Helper()
	la := model.Time{Time: lastActive}
	require.NoError(t, db.Create(&model.SysLoginLog{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		UserID:       userID,
		Username:     "u",
		IP:           ip,
		Device:       device,
		Status:       1,
		Message:      "ok",
		LoginAt:      model.Time{Time: lastActive},
		SessionID:    sid,
		LastActiveAt: &la,
	}).Error)
}

// seedActiveSessions 铺 n 个"仍活跃"且**IP / 设备都跟本次登录不同**的会话。
//
// IP 与设备都不同 ⇒ 命中规则 1(High,IP 不同)与规则 2(Medium,设备不同)。
func seedActiveSessions(t *testing.T, db *gorm.DB, userID uint64, n int, lastActive time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		seedActiveSession(t, db, userID,
			fmt.Sprintf("sid-%d-%d", userID, i),
			fmt.Sprintf("10.0.0.%d", i+1),
			fmt.Sprintf("device-%d", i),
			lastActive)
	}
}

// loginWithIP 用指定 IP 登录一次
func loginWithIP(t *testing.T, svc AuthService, username, pwd, ip string) {
	t.Helper()
	_, err := svc.Login(context.Background(), LoginRequest{
		Username:  username,
		Password:  preHash(pwd),
		LoginIP:   ip,
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) TestAgent/1.0",
	})
	require.NoError(t, err, "登录失败 —— 检测绝不该影响登录结果")
}

// seedSmsUserWithIdentity 造一个"能用短信登录"的用户:有手机号哈希 + 一个活跃身份。
//
// ⚠️ 不复用 auth_invalidate_test.go 里的 seedSmsUser —— 那个**不建身份**,
// 而 LoginBySms 要求至少一个活跃身份(没有就直接拒)。
// 也不复用 seedAuthUser —— 那个**不建手机号哈希**,按手机号查不到人。
// 两个 helper 各缺一半,这里把两半凑齐。
func seedSmsUserWithIdentity(t *testing.T, db *gorm.DB, username, plain, phone string) uint64 {
	t.Helper()
	userID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel:     model.BaseModel{ID: userID},
		Username:      username,
		PasswordHash:  hashForSeed(t, plain),
		PhoneHash:     desensitize.PhoneHash(phone),
		Nickname:      username,
		IsActive:      true,
		MustChangePwd: false,
	}).Error)
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		RoleName:  "role_user_admin",
		IsActive:  true,
		IsPrimary: true,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)
	return userID
}

// ===== 1. 接线本身 =====

// TestLogin_Anomaly_ConcurrentDifferentIP_TriggersHigh 同一账号在多个 IP
// 同时活跃 ⇒ 报 High 级异常 + 记 WARN 日志 + 给用户发站内信。
//
// 这是整个模块的**主用例**:没有它,"接线"这件事没有任何证据。
func TestLogin_Anomaly_ConcurrentDifferentIP_TriggersHigh(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom1", "role_user_admin", "Pwd@1234567")
	// 3 个仍活跃的会话(IP / 设备都与本次不同)+ 本次 = 4 > 阈值 3
	seedActiveSessions(t, db, userID, 3, time.Now())

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	loginWithIP(t, svc, "anom1", "Pwd@1234567", "203.0.113.9")

	assert.Positive(t, anomalyWarnCount(logs),
		"登录链路没有跑异常检测(或者跑了但没记日志)—— "+
			"pkg/anomalylogin 从 M45 起就没有任何生产调用方,「用户被盗号时无法感知」"+
			"这条痛点一次都没被真正解决过")

	msgs := fb.snapshot()
	require.Len(t, msgs, 1, "High 级异常必须给用户发一条站内信")
	assert.Equal(t, userID, msgs[0].UserID, "站内信发给的是**账号本人**,不是操作者")
	assert.Equal(t, "system", msgs[0].MsgType, "msg_type 必须是站内信中心认识的类型")
	assert.Contains(t, msgs[0].Title, "安全提醒")
	assert.Contains(t, msgs[0].Content, "203.0.113.9", "正文里应当出现本次登录的 IP")
}

// TestLogin_Anomaly_Disabled_NoDetection 关掉开关 ⇒ 检测**整个不跑**。
//
// 断言的是"没有 WARN 日志 **且** 没有站内信" —— 两条都要,
// 因为"关掉"如果只关掉一半(比如只关站内信),用户仍会被骚扰,
// 而运维以为"我已经关掉了"。
func TestLogin_Anomaly_Disabled_NoDetection(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom2", "role_user_admin", "Pwd@1234567")
	seedActiveSessions(t, db, userID, 3, time.Now())

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	cfg := defaultAnomalyCfg()
	cfg.Enabled = false
	svc := newAnomalySvc(t, db, cfg, fb)

	loginWithIP(t, svc, "anom2", "Pwd@1234567", "203.0.113.9")

	assert.Equal(t, 0, anomalyWarnCount(logs), "enabled=false 时不该有任何检测日志")
	assert.Empty(t, fb.snapshot(), "enabled=false 时不该投递站内信")
}

// TestLogin_Anomaly_UnderThreshold_NoDetection 没超过阈值 ⇒ 不报。
//
// MaxConcurrent=3 ⇒ 本次 + 2 个别人 = 3,**恰好等于**阈值,不该报。
// 这一条钉的是"超过"而不是"达到":写成 >= 会让"第 3 端登录"就报警,
// 正常人多端使用会被骚扰。
func TestLogin_Anomaly_UnderThreshold_NoDetection(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom3", "role_user_admin", "Pwd@1234567")
	seedActiveSessions(t, db, userID, 2, time.Now()) // 2 + 本次 = 3 = 阈值

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	loginWithIP(t, svc, "anom3", "Pwd@1234567", "203.0.113.9")

	assert.Equal(t, 0, anomalyWarnCount(logs),
		"3 个活跃会话(= 阈值 3)不该报警 —— 阈值语义是「超过 N」,不是「达到 N」")
	assert.Empty(t, fb.snapshot())
}

// ===== 2. 「同时在线」的口径 —— 本模块最锋利的一条 =====

// TestLogin_Anomaly_StaleSessions_NotCounted 太久没活动的会话**不算**"同时在线"。
//
// 这一条是 M128 的核心设计约束。仓里有两个"会话还活着"的口径:
// - sessionExpired:COALESCE(last_active_at, login_at) + refresh_ttl(720h=30 天)
//   - 活动窗口:COALESCE(last_active_at, login_at) 落在 active_window(30m)内
//
// 用错了(拿 30 天当"同时在线")⇒ 一个 30 天里在 4 台设备上登录过的人,
// **每次登录都算 4 端在线** ⇒ 每次都报异常 ⇒ 阈值形同虚设,
// 功能上线第一天就会被关掉 —— 于是又回到"检测从未真正生效"。
//
// 所以这里铺 3 个 **2 小时前**的活动(窗外),本次登录必须**不报**。
func TestLogin_Anomaly_StaleSessions_NotCounted(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom4", "role_user_admin", "Pwd@1234567")
	seedActiveSessions(t, db, userID, 3, time.Now().Add(-2*time.Hour))

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	loginWithIP(t, svc, "anom4", "Pwd@1234567", "203.0.113.9")

	assert.Equal(t, 0, anomalyWarnCount(logs),
		"2 小时前就没活动的会话不该算「同时在线」—— "+
			"若这里报警,说明口径用成了 refresh_ttl(30 天),正常用户会被天天骚扰")
	assert.Empty(t, fb.snapshot())
}

// TestLogin_Anomaly_ActiveWindowBoundary 活动窗口的边界。
//
// 刚好落在窗口内(29 分钟前)⇒ 算活跃;落在窗口外(31 分钟前)⇒ 不算。
func TestLogin_Anomaly_ActiveWindowBoundary(t *testing.T) {
	for _, tc := range []struct {
		name       string
		age        time.Duration
		wantDetect bool
	}{
		{"窗口内_29分钟", 29 * time.Minute, true},
		{"窗口外_31分钟", 31 * time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newAuthTestDB(t)
			userID, _ := seedAuthUser(t, db, "anom5", "role_user_admin", "Pwd@1234567")
			seedActiveSessions(t, db, userID, 3, time.Now().Add(-tc.age))

			logs := captureAnomalyLogs(t)
			fb := &fakeInbox{}
			svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

			loginWithIP(t, svc, "anom5", "Pwd@1234567", "203.0.113.9")

			got := anomalyWarnCount(logs) > 0
			assert.Equal(t, tc.wantDetect, got,
				"活动窗口是 30m:%s 的会话 %s 算活跃", tc.name, tc.age)
		})
	}
}

// TestLogin_Anomaly_LoggedOutSessions_Excluded 已登出的会话不算活跃。
func TestLogin_Anomaly_LogoutSessions_Excluded(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom6", "role_user_admin", "Pwd@1234567")
	for i := 0; i < 3; i++ {
		seedActiveSession(t, db, userID, "sid-out-"+string(rune('a'+i)),
			"10.0.0."+string(rune('1'+i)), "device-"+string(rune('a'+i)), time.Now())
	}
	// 把这 3 行都标成"已登出"
	logoutAt := model.Time{Time: time.Now()}
	require.NoError(t, db.Model(&model.SysLoginLog{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{"logout_at": logoutAt}).Error)

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	loginWithIP(t, svc, "anom6", "Pwd@1234567", "203.0.113.9")

	assert.Equal(t, 0, anomalyWarnCount(logs), "已登出的会话不算「同时在线」")
	assert.Empty(t, fb.snapshot())
}

// TestLogin_Anomaly_FailedLoginRows_Excluded 登录**失败**的记录不算活跃会话。
//
// 失败记录 status=0、没有 sid、没有会话 —— 把它算进去等于把
// "别人猜我的密码失败了几次"算成"我有几台设备在线"。
func TestLogin_Anomaly_FailedRows_Excluded(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom7", "role_user_admin", "Pwd@1234567")
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Create(&model.SysLoginLog{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			UserID:    userID,
			Username:  "u",
			IP:        "10.0.0." + string(rune('1'+i)),
			Device:    "device-" + string(rune('a'+i)),
			Status:    0, // 失败
			Message:   "密码错误",
			LoginAt:   model.Time{Time: time.Now()},
		}).Error)
	}

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	loginWithIP(t, svc, "anom7", "Pwd@1234567", "203.0.113.9")

	assert.Equal(t, 0, anomalyWarnCount(logs), "登录失败的记录不是活跃会话")
	assert.Empty(t, fb.snapshot())
}

// ===== 3. 站内信只发 High =====

// TestLogin_Anomaly_MediumOnly_NoInboxMessage 只有 Medium(设备不同、IP 相同)
// 时**不发站内信**,但日志照记。
//
// 理由:一个"手机 + 电脑"的正常人每次登录都会命中一次 Medium。
// 发站内信 ⇒ 用户被反复骚扰 ⇒ 关掉通知 ⇒ 真异常也没人知道。
//
// 所以 Medium 只进日志(给运维看),High 才进站内信(给用户看)。
func TestLogin_Anomaly_MediumOnly_NoInboxMessage(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom8", "role_user_admin", "Pwd@1234567")
	// 3 个会话:**IP 与本次相同**(所以不命中规则 1),设备不同(命中规则 2 = Medium)
	for i := 0; i < 3; i++ {
		seedActiveSession(t, db, userID, "sid-same-"+string(rune('a'+i)),
			"203.0.113.9", "device-"+string(rune('a'+i)), time.Now())
	}

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	loginWithIP(t, svc, "anom8", "Pwd@1234567", "203.0.113.9")

	assert.Positive(t, anomalyWarnCount(logs), "Medium 也必须进日志(运维要看得到)")
	assert.Empty(t, fb.snapshot(),
		"只有 Medium 时不该发站内信 —— 「手机 + 电脑」是常态,发站内信等于骚扰用户")
}

// ===== 4. 检测永远不阻断登录 =====

// TestLogin_Anomaly_QueryFailure_LoginStillSucceeds 查库失败**不能**影响登录。
//
// ⚠️ 这是"安全增强"最容易造成的生产事故:一个辅助查询挂了,
// 把所有人的登录搞挂。
//
// 做法:把 sys_login_log 表删掉 ⇒ 查询必然报错。
func TestLogin_Anomaly_QueryFailure_LoginStillSucceeds(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "anom9", "role_user_admin", "Pwd@1234567")
	require.NoError(t, db.Migrator().DropTable(&model.SysLoginLog{}))

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	// 注意:这里不能断言 err == nil 之前先确认 Login 真的返回了 token ——
	// loginWithIP 内部已经 require.NoError,失败会直接 FailNow。
	loginWithIP(t, svc, "anom9", "Pwd@1234567", "203.0.113.9")

	// ⚠️ 断言的是**那一条具体日志**,不是"日志里有什么" ——
	//   同一个用例里 recordLoginLog 也会因为表没了而记一条 WARN,
	//   断言 logs.Len() > 0 会被那条蒙混过去(假绿)。
	//   必须按 message 精确匹配检测那条。
	assert.Positive(t, countLogs(logs, "anomalylogin: 查询并发会话失败,跳过本次检测"),
		"查库失败必须留日志 —— 否则又回到「什么都没发生」的老路")
	assert.Empty(t, fb.snapshot(), "查库失败时不该有站内信")
}

// TestLogin_Anomaly_InboxFailure_LoginStillSucceeds 站内信投递失败不影响登录。
func TestLogin_Anomaly_InboxFailure_LoginStillSucceeds(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom10", "role_user_admin", "Pwd@1234567")
	seedActiveSessions(t, db, userID, 3, time.Now())

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{err: assert.AnError}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	loginWithIP(t, svc, "anom10", "Pwd@1234567", "203.0.113.9")

	assert.Positive(t, anomalyWarnCount(logs), "检测本身跑成功了(有 WARN)")
	assert.Empty(t, fb.snapshot(), "投递失败的站内信不该被记成已投递")
}

// TestLogin_Anomaly_NilInbox_NoPanic 没有站内信通道(精简部署 / 单测)
// 时只记日志,不 panic。
//
// `inbox` 在 NewAuthService 里是**必填参数**(可以传 nil),但传 nil 不该炸 ——
// 通知通道缺失不该让登录链路有意见。
func TestLogin_Anomaly_NilInbox_NoPanic(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom11", "role_user_admin", "Pwd@1234567")
	seedActiveSessions(t, db, userID, 3, time.Now())

	logs := captureAnomalyLogs(t)
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), nil)

	loginWithIP(t, svc, "anom11", "Pwd@1234567", "203.0.113.9")

	assert.Positive(t, anomalyWarnCount(logs), "没有站内信通道时日志仍要记")
}

// TestLogin_Anomaly_FailedLogin_NoDetection 登录**失败**时不跑检测。
//
// 检测挂在成功登录之后:密码还没校验对就去查"我有几台设备在线",
// 等于给撞库的人送情报(还能看出这个账号平时在哪些 IP 活动)。
func TestLogin_Anomaly_FailedLogin_NoDetection(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "anom12", "role_user_admin", "Pwd@1234567")
	seedActiveSessions(t, db, userID, 3, time.Now())

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	_, err := svc.Login(context.Background(), LoginRequest{
		Username: "anom12",
		Password: preHash("wrong-password"),
		LoginIP:  "203.0.113.9",
	})
	require.Error(t, err, "密码错必须登录失败")

	assert.Equal(t, 0, anomalyWarnCount(logs), "登录失败不该触发异常检测")
	assert.Empty(t, fb.snapshot())
}

// ===== 5. 短信登录这条入口也得检测 =====

// TestLoginBySms_Anomaly_ConcurrentDifferentIP_TriggersHigh
//
//	短信登录是**第二个**能签发 token、建会话的入口。
//	只在密码登录那条路上接检测的话,它就是一条**免检通道**:
//	被冒用的账号改走短信登录,本人永远收不到提醒 ——
//	而"账号被接管"恰恰最可能走的就是这条路(手机号也到手了)。
//
//	⚠️ 这一条是"接线"完整性的钉子:**凡是签发 token + 建会话的入口,
//	   检测都得跟着走一遍**。补这条之前,LoginBySms 完全不跑检测。
func TestLoginBySms_Anomaly_ConcurrentDifferentIP_TriggersHigh(t *testing.T) {
	db := newAuthTestDB(t)
	const phone = "13800001111"
	userID := seedSmsUserWithIdentity(t, db, "anomsms", "Pwd@1234567", phone)
	// 3 个仍活跃的会话(IP / 设备都与本次不同)+ 本次 = 4 > 阈值 3
	seedActiveSessions(t, db, userID, 3, time.Now())

	logs := captureAnomalyLogs(t)
	fb := &fakeInbox{}
	svc := newAnomalySvc(t, db, defaultAnomalyCfg(), fb)

	seedSmsCode(t, db, phone, model.SmsSceneLogin, "123456")
	_, err := svc.LoginBySms(context.Background(), phone, "123456",
		"203.0.113.77", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) TestAgent/1.0")
	require.NoError(t, err, "短信登录失败")

	assert.Positive(t, anomalyWarnCount(logs),
		"短信登录这条入口没有跑异常检测 —— 它同样签发 token、同样建会话,"+
			"只接密码登录那条路就等于留了一条免检通道")

	msgs := fb.snapshot()
	require.Len(t, msgs, 1, "High 级异常必须给用户发一条站内信")
	assert.Equal(t, userID, msgs[0].UserID, "站内信发给的是**账号本人**")
	assert.Contains(t, msgs[0].Content, "203.0.113.77", "正文里应当出现本次登录的 IP")
}
