// timeutil —— 统一东八区的时间工具
//
// ## 这个包解决什么问题
//
// `time.Now()` 拿的是服务器本地时区。容器里通常是 UTC,
// 于是"今日"跨天点跟业务预期差 8 小时 ——
// 报表口径、每日重置、冷却期判断全都会偏。
//
// 本包把所有"业务时间"统一到东八区。
//
// ## 跑法
//
//	go run ./examples/timeutil
package main

import (
	"fmt"
	"time"

	"rapid-admin/internal/pkg/timeutil"
)

func main() {
	// 一、Now 返回的就是东八区时间(不是把 UTC 转一下,是时间点本身 + 固定时区)
	now := timeutil.Now()
	fmt.Println("当前时间(东八区) =", now.Format("2006-01-02 15:04:05"))
	fmt.Println("今日             =", timeutil.Today())

	// 二、Today 永远按东八区切天 —— 哪怕服务器在 UTC
	fmt.Println()
	fmt.Println("服务器本地时区   =", time.Now().Location())
	fmt.Println("业务时区         =", timeutil.AsiaShanghai)

	// 三、解析也按东八区 —— 存量数据里那些不带时区的时间串按这个口径解释
	fmt.Println()
	t, err := timeutil.ParseShanghai("2006-01-02 15:04:05", "2026-11-01 08:30:00")
	if err != nil {
		fmt.Println("解析失败:", err)
		return
	}
	fmt.Println("解析结果         =", t.Format(time.RFC3339))

	// 四、跨时区比较:同一时刻,UTC 和东八区表示不同,但 unix 时间戳相同
	fmt.Println()
	fmt.Println("同一时刻的两种表示:")
	fmt.Println("  东八区 =", t.Format("2006-01-02 15:04:05"))
	fmt.Println("  UTC    =", t.UTC().Format("2006-01-02 15:04:05"))
	fmt.Println("  unix   =", t.Unix())
}
