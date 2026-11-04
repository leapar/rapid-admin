// M117-3 —— 登录日志的 browser / os 两列
//
// # 这两列此前**恒空**
//
//	model.SysLoginLog 里 **Browser / OS 两列一直都在**,
//	但全仓**没有写入方** —— recordLoginLog 只填 user_id / username / ip /
//	device / status / message / login_at / session_id。
//	⇒ 审计中心那两列永远是空的。
//
// # 本文件锁的是"真的写进去了",不是"解析函数对不对"
//
//	解析规则本身在 internal/pkg/uainfo 里测(那边测的是"认成什么"),
//	这边测的是"**落库的那一行**里 browser / os 到底有没有值" ——
//	两件事会分开坏:解析对了但没调用、调用了但没落库。
package service

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

const chromeWin10UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// loginLogRow 取刚写进去的那一行
func loginLogRow(t *testing.T, db *gorm.DB) model.SysLoginLog {
	t.Helper()
	var row model.SysLoginLog
	require.NoError(t, db.Order("id DESC").First(&row).Error)
	return row
}

func TestRecordLoginLog_WritesBrowserAndOS(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}

	svc.recordLoginLog(context.Background(), 7, "alice", LoginRequest{
		LoginIP:   "10.0.0.1",
		UserAgent: chromeWin10UA,
	}, 1, "ok", "sid-1")

	row := loginLogRow(t, db)
	require.Equal(t, "Chrome 120", row.Browser, "browser 列必须有值")
	require.Equal(t, "Windows 10/11", row.OS, "os 列必须有值")
	// device 装的是 UA 原文(不是设备型号)—— 这是既有契约,别改
	require.Equal(t, chromeWin10UA, row.Device)
	require.Equal(t, "alice", row.Username)
}

// TestRecordLoginLog_LongUAIsClipped —— 超长 UA 必须被截到列宽
//
// ⚠️ device 是 `varchar(128)`,而微信那种 UA 能到 200+ 字符。
// MySQL 严格模式下超长直接报 "Data too long for column" ⇒
// **整条登录日志写不进去** ⇒ 审计链断一条。
// SQLite 不校验长度,所以本地测不出来 —— 只能靠断言"落库的那行没超长"。
func TestRecordLoginLog_LongUAIsClipped(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}

	// 微信 iOS 的真实 UA 长度 ≈ 200+
	longUA := "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.30(0x18001e2f) NetType/WIFI Language/zh_CN"
	require.Greater(t, len([]rune(longUA)), 128, "这个用例的前提是 UA 超过 128 字符")

	svc.recordLoginLog(context.Background(), 7, "alice", LoginRequest{
		LoginIP:   "10.0.0.1",
		UserAgent: longUA,
	}, 1, "ok", "sid-1")

	row := loginLogRow(t, db)
	require.LessOrEqual(t, len([]rune(row.Device)), 128, "device 超了列宽")
	// 截断不能劈开 UTF-8 多字节字符(按字节切会把汉字劈成乱码)
	require.True(t, utf8.ValidString(row.Device), "device 被劈开了 UTF-8 字符")
	require.NotEmpty(t, row.Browser, "截断 device 不该影响 browser")
	require.NotEmpty(t, row.OS, "截断 device 不该影响 os")
}

// TestRecordLoginLog_NoUA —— 没有 UA 时 browser / os 是空串(不是猜一个)
func TestRecordLoginLog_NoUA(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}

	svc.recordLoginLog(context.Background(), 7, "alice", LoginRequest{LoginIP: "10.0.0.1"}, 1, "ok", "sid-1")

	row := loginLogRow(t, db)
	require.Empty(t, row.Browser)
	require.Empty(t, row.OS)
	require.Empty(t, row.Device)
}
