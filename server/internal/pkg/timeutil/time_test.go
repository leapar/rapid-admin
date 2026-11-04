package timeutil

import (
	"testing"
	"time"
)

// TestAsiaShanghaiOffset FixedZone 的偏移量恒为 UTC+8(固定偏移,无夏令时)
func TestAsiaShanghaiOffset(t *testing.T) {
	tm := time.Date(2026, 6, 1, 12, 0, 0, 0, AsiaShanghai)
	name, off := tm.Zone()
	if off != 8*3600 {
		t.Fatalf("偏移 = %d 秒,want %d", off, 8*3600)
	}
	if name != "CST" {
		t.Fatalf("时区名 = %q,want \"CST\"", name)
	}
}

// TestNowAndTodayEastEight Now/Today 落在东八区
//
// 期望值用 UTC 加 8 小时的算术构造,不依赖运行机器的本地时区
// (Windows 上 t.Setenv("TZ") 不可靠,统一走 UTC 换算)。
func TestNowAndTodayEastEight(t *testing.T) {
	if _, off := Now().Zone(); off != 8*3600 {
		t.Fatalf("Now() 偏移 = %d 秒,want %d", off, 8*3600)
	}
	// 期望与 Today() 在微秒级窗口内取的时钟,跨日边界竞态概率可忽略
	want := time.Now().UTC().Add(8 * time.Hour).Format("2006-01-02")
	if got := Today(); got != want {
		t.Fatalf("Today() = %q,want %q(UTC+8 日期)", got, want)
	}
}

// TestParseShanghaiNoTimezone 无时区字符串按东八区解析,而不是机器本地时区
func TestParseShanghaiNoTimezone(t *testing.T) {
	tm, err := ParseShanghai("2006-01-02 15:04:05", "2026-11-03 08:00:00")
	if err != nil {
		t.Fatalf("ParseShanghai 报错 = %v", err)
	}
	if _, off := tm.Zone(); off != 8*3600 {
		t.Fatalf("解析结果偏移 = %d 秒,want %d", off, 8*3600)
	}
	// 2026-11-03 08:00(+08:00)等于 2026-11-03 00:00 UTC —— 偏移语义的直接验证
	if u := tm.UTC(); u.Hour() != 0 || u.Minute() != 0 || u.Day() != 3 {
		t.Fatalf("08:00+08 应等于 2026-11-03 00:00 UTC,got %s", u)
	}
}
