// 时间工具 —— 统一东八区
package timeutil

import "time"

// AsiaShanghai 东八区
var AsiaShanghai = time.FixedZone("CST", 8*3600)

// Now 当前时间(东八区)
func Now() time.Time {
	return time.Now().In(AsiaShanghai)
}

// Today 今日(yyyy-mm-dd)
func Today() string {
	return Now().Format("2006-01-02")
}

// ParseShanghai 东八区解析
func ParseShanghai(layout, value string) (time.Time, error) {
	return time.ParseInLocation(layout, value, AsiaShanghai)
}
