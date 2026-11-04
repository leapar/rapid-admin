#!/usr/bin/env bash
# 可逆探针 —— 逐个拆掉实现,确认对应测试**真的会红**。
#
# 纪律(见 memory:mutation-probe-discipline):
#   1. 每个探针跑完必须无条件还原(git checkout),SKIP / 没跑起来 = 无效探针。
#   2. 探针要 git show 照抄原版,不能手搓"差不多的错版" —— 手搓的错版可能
#      编译不过,那是编译失败不是断言失败,证明不了什么。
#   3. "测试绿"不等于"实现对":反过来"测试红"才说明这条测试真的在守这条边界。
#
# 用法:bash internal/pkg/cronx/probe.sh
set -u

PASS=0
FAIL=0
S=internal/pkg/cronx/scheduler.go
BACKUP="internal/pkg/cronx/.probe.bak"

# ⚠️ restore **绝不能删备份**。早期版本这里 `rm -f "$BACKUP"`,于是只有第一次
# restore 生效,P1 的改动一路漏到 P6 —— 后面几个探针测的其实是被污染的代码,
# 结论全是假的。而且 cronx/ 是未跟踪文件,git 救不回来,只能手改。
# 每个探针都从同一份原版重新打补丁,所以一份备份全程够用。
restore() {
  if [ -f "$BACKUP" ]; then
    cp "$BACKUP" "$S"
  else
    echo "!!! 备份丢失,无法还原 —— 中止" >&2
    exit 2
  fi
}
trap 'restore; rm -f "$BACKUP"' EXIT

# run <探针名> <测试正则>
run() {
  local name="$1" re="$2"
  echo "───────── $name ─────────"
  go test ./internal/pkg/cronx/ -count=1 -timeout 60s -run "$re" 2>&1 \
    | grep -E "^(--- FAIL|--- PASS|ok|FAIL|PASS|.*build failed)" | head -5
}

# ── 每个探针跑之前先确认工作区是干净的 ──
# 上一次探针漏改的话,这里立刻炸,而不是让结论悄悄失真。
assert_clean() {
  if ! diff -q "$BACKUP" "$S" >/dev/null 2>&1; then
    echo "!!! 还原后 $S 仍与原版不一致 —— 探针没还原干净,后面全部不可信" >&2
    diff "$BACKUP" "$S" | head -20 >&2
    exit 3
  fi
}

expect_red() {
  local name="$1" re="$2"
  local out
  out=$(go test ./internal/pkg/cronx/ -count=1 -timeout 60s -run "$re" 2>&1)
  restore
  assert_clean   # 还原后必须干净,否则下一个探针测的是被污染的代码
  if echo "$out" | grep -qE "^(FAIL|--- FAIL)"; then
    echo "✅ $name → 测试变红(守得住)"
    PASS=$((PASS+1))
  elif echo "$out" | grep -q "build failed"; then
    echo "❌ $name → 只编译失败,不是断言失败。探针无效,说明它改的是签名不是行为。"
    FAIL=$((FAIL+1))
  else
    echo "❌ $name → **测试仍然绿**。这条测试没在守这条边界,白写。"
    FAIL=$((FAIL+1))
  fi
}

expect_green() {
  local name="$1" re="$2"
  local out
  out=$(go test ./internal/pkg/cronx/ -count=1 -timeout 60s -run "$re" 2>&1)
  restore
  assert_clean
  if echo "$out" | grep -q "^ok"; then
    echo "✅ $name → 测试仍绿(还原正确)"
    PASS=$((PASS+1))
  else
    echo "❌ $name → 还原后仍然红。探针没还原干净,后面全部不可信。"
    FAIL=$((FAIL+1))
  fi
}

S=internal/pkg/cronx/scheduler.go
cp "$S" "$BACKUP"

echo "=== cronx 可逆探针 ==="
echo

# ═══ P0 基线:不拆任何东西,全部应绿 ═══
expect_green "P0 基线(未拆实现)" "."

# ═══ P1 去掉热更新的 cron.Remove ═══
# 拆法:Reload 里 unregister 不再真正移除 entry。
# 预期:TestT6_Reload_* 变红 —— 这是"改频率要等重启才生效"那条边界的守卫。
python - "$S" <<'PY'
import sys, io
p = sys.argv[1]
s = io.open(p, encoding="utf-8", newline="").read()
old = """	s.unregister(key)
	if !s.started || !s.cfg.Enabled {
		return nil
	}"""
new = """	// PROBE P1: 不 unregister
	if !s.started || !s.cfg.Enabled {
		return nil
	}"""
assert old in s, "P1 anchor not found"
io.open(p, "w", encoding="utf-8", newline="").write(s.replace(old, new, 1))
PY
expect_red "P1 去掉 Reload 里的 unregister(热更新失效)" "TestT6_"

# ═══ P2 Layer 0 判定去掉 ═══
# 预期:TestT3_Layer0_* 变红
python - "$S" <<'PY'
import sys, io
p = sys.argv[1]
s = io.open(p, encoding="utf-8", newline="").read()
old = """	} else if running {
		recordLockSkipped(key)"""
new = """	} else if false && running {
		recordLockSkipped(key)"""
assert old in s, "P2 anchor not found"
io.open(p, "w", encoding="utf-8", newline="").write(s.replace(old, new, 1))
PY
expect_red "P2 去掉 Layer 0 重叠判定" "TestT3_Layer0"

# ═══ P3 拿掉 Degraded 的独立降级 ═══
# 把 lockUsable / layer0OK 合成一个开关 —— 也就是我最初写错的那版。
# 预期:TestT3_Layer0_ErrorStillTakesLock 变红(DB 挂了会连带丢掉 Redis 互斥)。
python - "$S" <<'PY'
import sys, io
p = sys.argv[1]
s = io.open(p, encoding="utf-8", newline="").read()
old = """	lockUsable = false
	if s.locker.Available() {"""
new = """	lockUsable = false
	if lockUsable && s.locker.Available() {"""
assert old in s, "P3 anchor not found"
io.open(p, "w", encoding="utf-8", newline="").write(s.replace(old, new, 1))
PY
expect_red "P3 两层降级合成一个开关(Redis 互斥被 DB 故障连累)" "TestT3_Layer0_ErrorStillTakesLock"

# ═══ P4 释放锁去掉 CAS 校验(裸 DEL 的等价物)═══════
# 预期:看门狗相关测试变红 / 或 Unlock 断言变红。
# 说明:fakeLocker 不校验 owner,所以这条探针**证明不了 CAS 的必要性** ——
# CAS 的正确性只能靠 Redis 集成测试兜。这是本模块单测的真实盲区,记录在案。
python - "$S" <<'PY'
import sys, io
p = sys.argv[1]
s = io.open(p, encoding="utf-8", newline="").read()
old = """	if locked {
		if uerr := s.locker.Unlock("""
new = """	if locked && rec.Status == model.JobStatusSuccess {
		if uerr := s.locker.Unlock("""
assert old in s, "P4 anchor not found"
io.open(p, "w", encoding="utf-8", newline="").write(s.replace(old, new, 1))
PY
expect_red "P4 失败时不释放锁(锁泄漏 → 任务永久卡死)" "TestT2_Trigger_HandlerError"

# ═══ P5 去掉 Handler 的 panic 兜底 ═══
# 预期:panic 会冒出 Trigger,TestT2_Trigger_Panic_* 直接 panic 而不是断言失败。
python - "$S" <<'PY'
import sys, io
p = sys.argv[1]
s = io.open(p, encoding="utf-8", newline="").read()
old = """	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\\n%s", r, debug.Stack())
		}
	}()"""
new = """	_ = debug.Stack // PROBE P5"""
assert old in s, "P5 anchor not found"
io.open(p, "w", encoding="utf-8", newline="").write(s.replace(old, new, 1))
PY
expect_red "P5 去掉 panic 兜底" "TestT2_Trigger_Panic"

# ═══ P6 inflight 换回按 jobKey 做键 ═══
# 预期:TestT9_Shutdown_BudgetExhaustedCancelsInflight 挂死/超时。
# 这条正是最初那个真实缺陷的复现 —— 两个同 key 并发运行会互相覆盖 cancel。
python - "$S" <<'PY'
import sys, io
p = sys.argv[1]
s = io.open(p, encoding="utf-8", newline="").read()
old = """	s.inflightSeq++
	id := s.inflightSeq
	s.inflight[id] = cancel
	return id"""
new = """	s.inflightSeq++
	// PROBE P6: 退回按 jobKey 做键
	s.inflight[s.inflightSeq%1+uint64(0)] = cancel
	return s.inflightSeq"""
assert old in s, "P6 anchor not found"
io.open(p, "w", encoding="utf-8", newline="").write(s.replace(old, new, 1))
PY
echo "───────── P6 inflight 键冲突(预期:测试红) ─────────"
expect_red "P6 inflight 按 jobKey 做键(并发同 key 会漏杀)" "TestT9_Shutdown"

restore
echo
echo "=== 探针结果:通过 $PASS,失败 $FAIL ==="
[ "$FAIL" -eq 0 ]
