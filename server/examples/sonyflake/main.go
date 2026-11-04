// sonyflake —— 分布式 ID 生成
//
// ## 这个包解决什么问题
//
// 自增主键在分库分表/多实例下会撞;UUID 36 字符占索引空间;
// snowflake 的时间戳依赖外部时钟服务。
//
// Sonyflake 把方案改成:`时间(39 bit) | 序号(8 bit) | 机号(16 bit)`,
// **不自增** ⇒ 插入顺序不敏感,不怕并发写热点;数字串 ⇒ 索引友好。
//
// ## 关键点:机号是自己算的
//
// 旧实现没传 MachineID,库自己去取私网 IPv4 的低 16 位 —— 于是
// `192.168.1.5` 和 `10.0.1.5` 同机号、Docker 同节点副本共用节点 IP 也同机号,
// 而多网卡机器重启后取到的 IP 还可能变。机号相同 ⇒ 同机号 ⇒
// 10ms 片内序号相同 ⇒ **生成出完全相同的 ID** ⇒ 主键重复 ⇒ 后写覆盖先写。
//
// 现在自己算机号,部署期也可以用环境变量显式指定。
//
// ## 跑法
//
//	go run ./examples/sonyflake
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/sonyflake"
)

func main() {
	fmt.Println("机号     =", sonyflake.MachineID())
	fmt.Println("机号来源 =", sonyflake.MachineIDSource(), "(env = 部署期显式指定;computed = 自己算)")

	fmt.Println()
	fmt.Println("连取 5 个 ID(注意单调递增):")
	for i := 0; i < 5; i++ {
		fmt.Printf("  %s\n", sonyflake.NextString())
	}

	fmt.Println()
	fmt.Println("ID 是十进制数字串 ⇒ 索引友好、JSON 里直接用字符串传(JS 的 2^53 装不下 19 位数)")
	fmt.Println("uint64 形态 =", sonyflake.Next())
}
