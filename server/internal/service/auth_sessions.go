// 会话与设备管理 —— SRS §2.2.1 #6「查看活跃设备 / 强制下线其他设备」。
//
// # 数据源:sys_login_log 就是会话登记簿
//
// 一次登录 = 一行 = 一条会话,靠 session_id 关联到 token 里那个 sid claim
// (见 internal/pkg/auth/session.go、model.SysLoginLog 的说明)。
//
// 为什么不再单建一张 sys_session 表:
//   - 需求要的字段(设备 / IP / 浏览器 / 系统 / 登录时间)这张表全都有,
//     新建一张等于把这些字段抄一遍
//   - 登记簿必须**跨实例一致**。吊销存储的内存实现是进程内的,多实例部署时
//     A 实例看不见 B 实例上的会话;放 DB 就没这个问题
//
// # 与"推进作废线"形状相反,不能互相替代
//
//   - 作废线(pkg/auth/invalidate.go):按**用户**整体作废 —— 改密码时用,
//     一推就是该用户所有设备一起下线
//   - 本模块:按**会话**点名吊销 —— 保留本设备,只踢别的
//
// # 强制下线必须让 access 一起死
//
// 吊销 sid 之后,那张 refresh 换不动新 token(RefreshToken 查会话吊销态)。
// 但**已经发出去的那张 access token 还活着**,最长 8h —— 于是"强制下线"在
// 那台设备上 8 小时内毫无效果,用户只会认为功能没实现。
//
// 补法:中间件多查一道会话吊销态(middleware/auth.go 的第三道闸)。
// 代价是每个请求多一次吊销存储查询 —— 那里已经有两道(jti / 作废线),
// 第三道与它们同量级,不是新的一类开销。
package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/sonyflake"
)

// SessionView 一条会话 / 登录记录
//
// 同一套结构服务两个列表(活跃设备 / 历史登录记录),字段用不到的留零值 ——
// 分成两个结构体的话,前端要维护两套渲染,加字段时又得记得改两处。
type SessionView struct {
	// SessionID 会话 id。登录失败的历史记录没有会话,这里是空串。
	SessionID string `json:"session_id"`
	Device    string `json:"device"`
	Browser   string `json:"browser"`
	OS        string `json:"os"`
	IP        string `json:"ip"`
	Location  string `json:"location"`
	// Status 1 成功 / 0 失败
	Status  int    `json:"status"`
	Message string `json:"message"`
	// LoginAt 这一次登录的时刻
	LoginAt model.Time `json:"login_at"`
	// LastActiveAt 最后一次确认这条会话还活着的时刻
	//   (有请求打在这条会话上就记,登录本身也算一次,节流 ≤5min 一次;续期也写);
	//   nil = 登录失败的记录,此时用 LoginAt 兜底
	LastActiveAt *model.Time `json:"last_active_at"`
	// LogoutAt 被登出 / 强制下线的时刻;nil = 还没下线
	LogoutAt *model.Time `json:"logout_at"`
	// IsCurrent 这条是不是**我当前这条**会话(前端用它标"本机")
	IsCurrent bool `json:"is_current"`
	// Expired 这条会话是不是已经自然过期(还能看,但已经不算活跃设备)
	Expired bool `json:"expired"`
}

// SessionPage 历史登录记录分页
type SessionPage struct {
	List  []SessionView `json:"list"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}

// SessionOverview 会话总览:活跃设备 + 历史登录记录
type SessionOverview struct {
	Active  []SessionView `json:"active"`
	History SessionPage   `json:"history"`
}

// ListSessions 列出我自己的活跃会话与历史登录记录
//
// 只能看**自己**的(需求 §2.2.1 #6 写的是"本人历史登录记录与活跃设备"),
// 所以这里不接任何 userId 参数 —— 没有可越权的入口,比"接了再校验"更省事。
func (s *authService) ListSessions(ctx context.Context, caller *Caller, page, size int) (*SessionOverview, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	ttl := s.refreshTTL(caller)
	now := time.Now()

	// 1. 活跃会话:成功登录 + 没登出 + 有 sid
	//
	//    "还在有效期内"这一条在 Go 里判(见 sessionExpired),因为它要按
	//    COALESCE(last_active_at, login_at) 算,写进 SQL 不直观。
	var activeRows []model.SysLoginLog
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND status = 1 AND logout_at IS NULL AND session_id <> ''", caller.UserID).
		Order("login_at DESC").
		Find(&activeRows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询活跃会话失败").WithDetail(err.Error())
	}
	active := make([]SessionView, 0, len(activeRows))
	for i := range activeRows {
		r := &activeRows[i]
		if s.sessionExpired(r, ttl, now) {
			continue
		}
		active = append(active, s.toSessionView(r, caller.SessionID, false))
	}

	// 2. 历史登录记录(含失败)。分页走两次独立语句,不复用 Count 那个 tx ——
	//    复用时 GORM 的 Count 会改写 SELECT 子句,行为依赖版本,不如写死。
	var total int64
	if err := s.db.WithContext(ctx).Model(&model.SysLoginLog{}).
		Where("user_id = ?", caller.UserID).
		Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询登录记录失败").WithDetail(err.Error())
	}
	var histRows []model.SysLoginLog
	if err := s.db.WithContext(ctx).Model(&model.SysLoginLog{}).
		Where("user_id = ?", caller.UserID).
		Order("id DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&histRows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询登录记录失败").WithDetail(err.Error())
	}
	hist := make([]SessionView, 0, len(histRows))
	for i := range histRows {
		r := &histRows[i]
		hist = append(hist, s.toSessionView(r, caller.SessionID, s.sessionExpired(r, ttl, now)))
	}

	return &SessionOverview{
		Active: active,
		History: SessionPage{
			List:  hist,
			Total: total,
			Page:  page,
			Size:  size,
		},
	}, nil
}

// RevokeSessions 强制下线指定的会话(必须属于我自己)
//
// 返回真正被吊销的条数 —— 传进来的 sid 可能已经过期或已被并发吊掉,
// 那个数要如实反映,否则前端会说"已下线 3 个"而实际只下线了 1 个。
//
// # 归属校验是**硬要求**,不是防御性编程
//
// session_id 是能直接递给吊销存储的句柄。不校验归属的话,任何登录用户都能
// 构造别人的 sid 把它吊掉 —— 变成"任意用户可踢任意用户下线"的 DoS。
// 所以先把 sid 与 caller.UserID **一起**查,只处理查得到的那些。
//
// # 不能吊销自己当前这条
//
// 自己这条要走 /auth/logout(它连带把 access 的 jti 也吊了,语义完整)。
// 在这里吊掉自己这条,结果是"access 还活着、refresh 已死"—— 一种更难解释的
// 中间状态(用户眼前还好好地登着,却已经不能续期了)。
func (s *authService) RevokeSessions(ctx context.Context, caller *Caller, sessionIDs []string) (int, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}

	// 1. 去重 + 去空
	uniq := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sid := range sessionIDs {
		sid = strings.TrimSpace(sid)
		if sid == "" {
			continue
		}
		if _, ok := seen[sid]; ok {
			continue
		}
		seen[sid] = struct{}{}
		uniq = append(uniq, sid)
	}
	if len(uniq) == 0 {
		// 没有要下线的东西 —— 不是错误。"下线其他所有设备"在只有本机时
		// 就会走到这里(前端也会把按钮置灰,但接口不该因此报错)。
		return 0, nil
	}

	// 2. 归属校验:只取属于我自己的、且是成功登录的那些行
	var rows []model.SysLoginLog
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND session_id IN ? AND status = 1", caller.UserID, uniq).
		Find(&rows).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "查询会话失败").WithDetail(err.Error())
	}
	if len(rows) == 0 {
		// 一个都没查到 = 要么 sid 不存在,要么是**别人的**。
		// 两种都不该"部分成功"地放过,但也不该告诉调用方"那是别人的 sid"
		// (那是信息泄露)—— 一句话盖过去。
		return 0, errcode.New(errcode.BadRequest, "会话不存在或不属于当前用户")
	}

	// 3. 吊销
	//
	//    TTL 取 **refresh 的寿命**:会话里寿命最长的就是它。短了会出现
	//    "吊销记录先过期、token 还在" —— 那等于没吊。
	//
	//    取不到寿命(理论上不可能,main 里三个受众都装了 manager)时**报错**,
	//    不静默跳过:Revoke 在 ttl<=0 时是个 no-op,静默跳过等于告诉用户
	//    "已下线",而那条会话其实还活着。
	ttl, err := s.refreshTTLErr(caller)
	if err != nil {
		return 0, err
	}

	now := model.Time{Time: time.Now()}
	revoked := 0
	done := make([]string, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		if r.SessionID == caller.SessionID {
			// 自己当前这条:跳过(见函数注释)
			continue
		}
		if err := auth.RevokeSession(ctx, r.SessionID, ttl); err != nil {
			// 吊销写不进去 = 那条会话还活着。这是安全事件,记 ERROR。
			logger.L().Error("强制下线:吊销会话失败,该会话仍可续期",
				zap.String("sid", r.SessionID), zap.Uint64("user_id", caller.UserID), zap.Error(err))
			continue
		}
		// 登记簿里这一行标记成已下线。
		// 写失败不阻塞:会话已经吊掉了(安全目标已达成),只是列表上还显示着,
		// 那下次刷新列表时会因为会话已过期/已吊销而自然消失。
		if err := s.db.WithContext(ctx).Model(&model.SysLoginLog{}).
			Where("id = ?", r.ID).
			Update("logout_at", &now).Error; err != nil {
			logger.L().Warn("强制下线:写 logout_at 失败(会话已吊销,列表上可能还显示着)",
				zap.Uint64("id", r.ID), zap.Error(err))
		}
		revoked++
		done = append(done, r.SessionID)
	}

	// 4. 操作留痕(需求 §1397「强制下线其他设备;操作留痕」)
	s.recordSessionRevoke(ctx, caller, done)

	return revoked, nil
}

// recordSessionRevoke 写一条操作日志:谁、在什么时候、踢了哪几条会话。
//
// 留痕放在这里而不是只靠 RBAC 中间件那条通用记录:中间件记的是
// "某个用户调了某个接口",记不住"踢了哪几条会话"—— 而这正是事后要查的东西。
func (s *authService) recordSessionRevoke(ctx context.Context, caller *Caller, sessionIDs []string) {
	if len(sessionIDs) == 0 {
		return
	}
	opLog := &model.SysOperationLog{
		BaseModel:   model.BaseModel{ID: sonyflake.Next()},
		UserID:      caller.UserID,
		Username:    caller.Username,
		Module:      "auth",
		Action:      "revoke-sessions",
		Description: "count=" + strconv.Itoa(len(sessionIDs)) + " sids=[" + strings.Join(sessionIDs, ",") + "]",
		Status:      1,
	}
	if err := s.db.WithContext(ctx).Create(opLog).Error; err != nil {
		logger.L().Warn("写强制下线操作日志失败",
			zap.Uint64("user_id", caller.UserID), zap.Error(err))
	}
}

// sessionExpired 这条会话是否已经自然过期(没被吊销,只是活得够久了)
//
// 判据:`COALESCE(last_active_at, login_at) + refresh_ttl <= now`
//
// 为什么不是只看 login_at:refresh token 每次轮换都**重新起**一个 refresh_ttl
// 窗口。一条 40 天前登录、昨天刚续过期的会话**还活着**,只看 login_at 会把它
// 误判成死的,于是"活跃设备"列表里少了一台真在用的设备。
//
// ttl <= 0 表示"取不到寿命"(jwt 没装 manager),此时**不判定过期** ——
// 宁可多列一条陈旧会话,也不要因为一个配置问题把整张列表清空
// (清空 = 用户以为"只有一台设备",而实际上有五台)。
func (s *authService) sessionExpired(r *model.SysLoginLog, ttl time.Duration, now time.Time) bool {
	if ttl <= 0 {
		return false
	}
	base := r.LoginAt.Time
	if r.LastActiveAt != nil && !r.LastActiveAt.Time.IsZero() {
		base = r.LastActiveAt.Time
	}
	if base.IsZero() {
		return false
	}
	return base.Add(ttl).Before(now)
}

// toSessionView 把一行登记簿转成对外视图
func (s *authService) toSessionView(r *model.SysLoginLog, currentSID string, expired bool) SessionView {
	return SessionView{
		SessionID:    r.SessionID,
		Device:       r.Device,
		Browser:      r.Browser,
		OS:           r.OS,
		IP:           r.IP,
		Location:     r.Location,
		Status:       r.Status,
		Message:      r.Message,
		LoginAt:      r.LoginAt,
		LastActiveAt: r.LastActiveAt,
		LogoutAt:     r.LogoutAt,
		IsCurrent:    currentSID != "" && r.SessionID == currentSID,
		Expired:      expired,
	}
}

// refreshTTL 取该受众的 refresh 寿命;取不到时返回 0(调用方自行决定降级)
func (s *authService) refreshTTL(caller *Caller) time.Duration {
	ttl, err := s.refreshTTLErr(caller)
	if err != nil {
		return 0
	}
	return ttl
}

// refreshTTLErr 取该受众的 refresh 寿命
//
// 拿不到 manager 时**不 panic**:MustGet 遇到未知受众会 panic,而受众来自
// token claim,不该由它终结一次请求。退到 admin;再没有就报错。
func (s *authService) refreshTTLErr(caller *Caller) (time.Duration, error) {
	if s.jwt == nil {
		return 0, errcode.New(errcode.Internal, "会话寿命不可知,拒绝执行强制下线")
	}
	if m, ok := s.jwt.Get(auth.Audience(caller.Audience)); ok {
		return m.RefreshTTL(), nil
	}
	if m, ok := s.jwt.Get(auth.AudienceAdmin); ok {
		return m.RefreshTTL(), nil
	}
	return 0, errcode.New(errcode.Internal, "会话寿命不可知,拒绝执行强制下线")
}
