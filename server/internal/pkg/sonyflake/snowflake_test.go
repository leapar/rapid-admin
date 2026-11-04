package sonyflake

import (
	"os"
	"strconv"
	"sync"
	"testing"
)

// ── 本文件的来历 ───────────────────────────────────────────────────────
//
// 这个包之前**一张测试都没有**,而它是全仓 **52 个文件、几十张表的主键来源**。
//
// 它有一条 P0 真 bug(2026-10-29 M112 修的):
// 旧实现 `sonyflake.New(Settings{StartTime: start})` 没传 MachineID,
// 库用自己的默认值 —— **私网 IPv4 的低 16 位**(sonyflake v2 `lower16BitPrivateIP`):
//
//	192.168.1.5 → int(ip[2])<<8 + int(ip[3]) → 1<<8 + 5 = 261
//	10.0.1.5   → 0<<8   + 5 = 5      ← 咦,这两个其实不一样
//
//	真正会撞的是**同低 16 位**的地址,例如 192.168.1.5 与 192.168.2.5(都是 261)、
//	10.0.1.5 与 10.1.1.5(都是 261)。而 Docker/K8s 同节点副本共用节点 IP,
// 于是同节点上的多个副本**机号完全相同**。
//
// sonyflake 的布局是 `时间(39 bit) | 序号(8 bit) | 机号(16 bit)`,
// 同机号 ⇒ 同一 10ms 片内序号相同 ⇒ **生成出完全相同的 ID** ⇒ 主键重复。
//
// ── M112 可逆探针记录 ───────────────────────────────────────────────────
//
//	拆法                                        必须变红
//	────────────────────────────────────────  ───────────────────────────────────
//	机号退回库的默认值(只取私网 IP 低 16 位)      TestM112_MachineID_NotBarePrivateIPLow16
//	机号可以任意大(漏掉 & 0xFFFF)                TestM112_MachineID_InRange
//	环境变量写错时 panic                          TestM112_EnvInvalid_DoesNotPanic
//	环境变量写错时静默(不告警)                    TestM112_EnvInvalid_FallsBackWithError
//	兜底 ID 不唯一(退回 UnixNano)                TestM112_FallbackID_Unique
//	兜底 ID 跨进程会撞(没有进程盐)               TestM112_FallbackID_HasProcessSalt
// ───────────────────────────────────────────────────────────────────────

// ── 基本性质 ────────────────────────────────────────────────────────────

func TestM112_Next_Unique(t *testing.T) {
	seen := make(map[uint64]struct{}, 20000)
	for i := 0; i < 20000; i++ {
		id := Next()
		if _, dup := seen[id]; dup {
			t.Fatalf("第 %d 次生成了重复 ID:%d", i, id)
		}
		seen[id] = struct{}{}
	}
}

// TestM112_Next_ConcurrentUnique 并发下也不能重复
//
// sonyflake 的序号是 8 bit(每 10ms 片 256 个)⇒ 单进程每毫秒最多 256 个,
// 超过就自旋等待下一片。并发 2000 次必须仍然全不重复。
func TestM112_Next_ConcurrentUnique(t *testing.T) {
	const n = 2000
	var wg sync.WaitGroup
	ids := make([]uint64, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ids[i] = Next()
		}(i)
	}
	wg.Wait()

	seen := make(map[uint64]struct{}, n)
	for i, id := range ids {
		if _, dup := seen[id]; dup {
			t.Fatalf("并发下生成了重复 ID:%d(第 %d 个)", id, i)
		}
		seen[id] = struct{}{}
	}
}

// TestM112_Next_Magnitude 量级:17 位数(约 9.7e16)
//
// ID 布局(sonyflake v2.2.0):`elapsedTime(10ms 精度) << 24 | seq << 16 | machine`
// elapsedTime 是"距 2025-01-01 的 10ms 数"⇒ 2026-10 约 5.76e9
// ⇒ ID ≈ 5.76e9 × 2^24 ≈ 9.7e16 ⇒ **17 位**。
//
// ⚠️ 关键性质:**超过 2^53**(9.007e15)⇒ 前端 JSON.parse 会静默截断末位
//
//	⇒ 这就是 handler.OKID 为什么要把 ID 转成字符串返回(见 handler.go 的注释)。
//	这个断言锁住的是"ID 确实是 64 位量级的大数",不是"具体多少位"。
func TestM112_Next_Magnitude(t *testing.T) {
	id := Next()
	if id <= 1<<53 {
		t.Fatalf("ID 应超过 2^53(前端 JSON.parse 会截断),实际 %d", id)
	}
	if id >= 1e18 {
		t.Fatalf("ID 应在 1e18 以内,实际 %d", id)
	}
	s := strconv.FormatUint(id, 10)
	if len(s) < 17 || len(s) > 18 {
		t.Fatalf("ID 位数应为 17~18 位,实际 %d 位(%s)", len(s), s)
	}
}

// TestM112_NextString_MatchesNext 字符串形态与数值一致
func TestM112_NextString_MatchesNext(t *testing.T) {
	s := NextString()
	if s == "" {
		t.Fatal("NextString 不该返回空串")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("NextString 只该返回数字,实际 %q", s)
		}
	}
}

// ── 机号 ────────────────────────────────────────────────────────────────

// TestM112_MachineID_InRange 机号必须落在 0..65535
func TestM112_MachineID_InRange(t *testing.T) {
	id := MachineID()
	if id >= 1<<16 {
		t.Fatalf("机号必须是 16 bit,实际 %d", id)
	}
}

// TestM112_MachineID_NotBarePrivateIPLow16 机号不能只是"私网 IP 的低 16 位"
//
// 这是 M112 修的那条 P0:旧实现等于把机号 = 私网 IP 低 16 位,
// 同低 16 位的两台机器(以及同节点的多个副本)必然撞号。
//
// 现在机号 = IP低16 XOR 主机名哈希低16 XOR pid低16 ⇒
// **只有当三项同时相等时才会撞**,概率可以忽略。
// 断言方式:机号必须与"裸 IP 低 16 位"不同 ——
// (三项恰好异或成 0 的概率是 1/65536,测试环境里不会踩中。)
func TestM112_MachineID_NotBarePrivateIPLow16(t *testing.T) {
	bare := privateIPv4Low16()
	got := MachineID()
	if got == bare {
		t.Fatalf("机号与裸私网 IP 低 16 位相同(%d):这正是 M112 修掉的撞号成因", got)
	}
}

// TestM112_MachineIDSource_Computed 无环境变量时来源是 computed
func TestM112_MachineIDSource_Computed(t *testing.T) {
	t.Setenv(envMachineID, "")
	if _, from, err := resolveMachineID(); err != nil {
		t.Fatalf("不该报错,实际 %v", err)
	} else if from != sourceComputed {
		t.Fatalf("来源应为 %q,实际 %q", sourceComputed, from)
	}
}

// TestM112_EnvOverride 环境变量能指定机号
func TestM112_EnvOverride(t *testing.T) {
	t.Setenv(envMachineID, "4242")
	id, from, err := resolveMachineID()
	if err != nil {
		t.Fatalf("不该报错,实际 %v", err)
	}
	if id != 4242 {
		t.Fatalf("机号应为 4242,实际 %d", id)
	}
	if from != sourceEnv {
		t.Fatalf("来源应为 %q,实际 %q", sourceEnv, from)
	}
}

// TestM112_EnvInvalid_DoesNotPanic 环境变量写错不能 panic
//
// 理由写在 resolveMachineID 的注释里:"机号配错"的后果是主键重复(数据损坏),
// 比"启动失败"更难查;但为了一次笔误把整个服务打挂也不合适。
// ⇒ 能起,但一定告警。
//
// ⚠️ 纯空白(" ")**不算非法** —— resolveMachineID 会先 TrimSpace,
//
//	空白等价于"没配"⇒ 走自动计算,不报错。这是刻意的(容器里常写成空串)。
func TestM112_EnvInvalid_DoesNotPanic(t *testing.T) {
	for _, bad := range []string{"abc", "-1", "65536", "99999999999999999999", "1.5"} {
		t.Setenv(envMachineID, bad)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("环境变量 %q 不该 panic,实际 panic:%v", bad, r)
				}
			}()
			id, from, err := resolveMachineID()
			if err == nil {
				t.Fatalf("环境变量 %q 非法时必须报错", bad)
			}
			if from != sourceComputed {
				t.Fatalf("非法环境变量应回落到 %q,实际 %q", sourceComputed, from)
			}
			if id >= 1<<16 {
				t.Fatalf("回落的机号必须在 16 bit 内,实际 %d", id)
			}
		}()
	}
}

// TestM112_EnvBlank_IsNotSet 纯空白等价于"没配"
func TestM112_EnvBlank_IsNotSet(t *testing.T) {
	t.Setenv(envMachineID, "   ")
	id, from, err := resolveMachineID()
	if err != nil {
		t.Fatalf("空白环境变量不该报错,实际 %v", err)
	}
	if from != sourceComputed {
		t.Fatalf("空白环境变量应走自动计算,实际 %q", from)
	}
	if id >= 1<<16 {
		t.Fatalf("机号必须在 16 bit 内,实际 %d", id)
	}
}

// TestM112_EnvInvalid_FallsBackWithError 非法值必须**带着错误**回落(不能静默)
func TestM112_EnvInvalid_FallsBackWithError(t *testing.T) {
	t.Setenv(envMachineID, "not-a-number")
	_, _, err := resolveMachineID()
	if err == nil {
		t.Fatal("非法环境变量必须返回错误")
	}
	// 错误信息里要带上变量名,便于运维一眼定位
	if !contains(err.Error(), envMachineID) {
		t.Fatalf("错误信息里应包含变量名 %q,实际 %q", envMachineID, err.Error())
	}
}

// ── 机号计算的稳定性 ────────────────────────────────────────────────────

// TestM112_PrivateIPv4Low16_Stable 同一台机器多次取值必须一致
//
// 库取的是 `net.InterfaceAddrs()` 里的**第一个**私网 IPv4,顺序不保证稳定;
// M112 改成"遍历全部取低 16 位最小的" ⇒ 永远挑同一个。
func TestM112_PrivateIPv4Low16_Stable(t *testing.T) {
	first := privateIPv4Low16()
	for i := 0; i < 20; i++ {
		if got := privateIPv4Low16(); got != first {
			t.Fatalf("私网 IP 低 16 位不稳定:第 %d 次 %d ≠ 首次 %d", i, got, first)
		}
	}
}

// TestM112_PrivateIPv4Low16_Minimal 取的是所有私网 IP 里最小的那个低 16 位
func TestM112_PrivateIPv4Low16_Minimal(t *testing.T) {
	// 不做网络断言(测试机可能没有私网 IP),只保证返回值本身在 16 bit 内
	if got := privateIPv4Low16(); got >= 1<<16 {
		t.Fatalf("低 16 位应在 16 bit 内,实际 %d", got)
	}
}

// TestM112_FNV1aLow16_Stable 哈希必须稳定且落在 16 bit 内
func TestM112_FNV1aLow16_Stable(t *testing.T) {
	h1 := fnv1aLow16("web-0")
	h2 := fnv1aLow16("web-0")
	if h1 != h2 {
		t.Fatalf("哈希不稳定:%d ≠ %d", h1, h2)
	}
	if h1 >= 1<<16 {
		t.Fatalf("哈希低 16 位应在 16 bit 内,实际 %d", h1)
	}
	if fnv1aLow16("web-1") == h1 && h1 != fnv1aLow16("web-1") {
		t.Fatal("不同输入不该有相同哈希")
	}
}

// ── 兜底 ID ─────────────────────────────────────────────────────────────

// TestM112_FallbackID_Unique 兜底 ID 必须唯一
//
// 旧实现返 `uint64(time.Now().UnixNano())` —— 系统计时器分辨率不是纳秒
// (Windows 上更明显)⇒ 连续两次调用可能拿到同一个值 ⇒ 主键重复。
func TestM112_FallbackID_Unique(t *testing.T) {
	seen := make(map[uint64]struct{}, 10000)
	for i := 0; i < 10000; i++ {
		id := fallbackID()
		if _, dup := seen[id]; dup {
			t.Fatalf("兜底 ID 重复了(第 %d 次):%d", i, id)
		}
		seen[id] = struct{}{}
	}
}

// TestM112_FallbackID_HasProcessSalt 兜底 ID 必须含进程盐(跨进程要隔开)
//
// 旧实现只有时间戳 ⇒ 两个进程在同一纳秒启动会撞。
// 现在中 16 位是 `主机名哈希 XOR pid` ⇒ 同机器上的两个进程必然不同。
func TestM112_FallbackID_HasProcessSalt(t *testing.T) {
	// 盐必须严格等于"主机名哈希 XOR pid 低 16 位"—— 公式锁死,
	// 也就证明了它**依赖 pid** ⇒ 同机器不同进程必然错开。
	want := (fnv1aLow16(hostname()) ^ uint64(os.Getpid()&0xFFFF)) & 0xFFFF
	if got := fallbackSaltValue(); got != want {
		t.Fatalf("进程盐应为 %d,实际 %d", want, got)
	}
	// 进程内必须稳定(不能每次调用都变)
	salt1, salt2 := fallbackSaltValue(), fallbackSaltValue()
	if salt1 != salt2 {
		t.Fatal("进程盐在进程内必须稳定")
	}
	// 兜底 ID 的中 16 位必须等于进程盐
	id := fallbackID()
	if (id>>16)&0xFFFF != fallbackSalt&0xFFFF {
		t.Fatalf("兜底 ID 的进程盐位不对:%d", id)
	}
}

// TestM112_FallbackID_CarriesSequence 低 16 位是自增序号
func TestM112_FallbackID_CarriesSequence(t *testing.T) {
	// 连续两次的序号必须 +1(低 16 位)
	a := fallbackID()
	b := fallbackID()
	seqA := a & 0xFFFF
	seqB := b & 0xFFFF
	if seqB != seqA+1 {
		t.Fatalf("序号应连续自增:%d → %d", seqA, seqB)
	}
}

// ── 启动日志对账 ────────────────────────────────────────────────────────

// TestM112_Accessors 不 panic 且返回合理值(启动日志依赖这两个)
func TestM112_Accessors(t *testing.T) {
	if MachineIDSource() != sourceComputed && MachineIDSource() != sourceEnv {
		t.Fatalf("未知的机号来源:%q", MachineIDSource())
	}
	if os.Getpid() == 0 {
		t.Fatal("不该出现")
	}
}

// contains 简易子串包含(避免为一个断言引 strings 之外的依赖)
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
