// 异常登录检测配置块(M128)的特征测试
//
// 存在的理由与 notify_test.go / file_test.go 同源,但这一段更危险:
//
// 这一段的默认值是"**漏配 = 照常跑**",于是"键名写错"与"忘了配"
// 产生**一模一样**的症状 —— 检测静默不跑。而"检测静默不跑"与
// "检测跑了但没发现异常"在日志里也长得一模一样(都没有 WARN 行)。
//
// 三层防线:
//
//  1. 键必须在**两份模板**里显式出现(InConfig 只认配置文件,不看 SetDefault)
//     —— 抓"键名拼错"。
//  2. 值必须解析成期望值 —— 抓"键在文件里但 mapstructure tag 拼错"。
//  3. **漏掉整个块**时默认值必须仍然生效 —— 抓 SetDefault → Unmarshal
//     这条链(尤其是 "30m" → time.Duration 的解码)。
//     这一层最容易被忽略:如果 viper 的解码钩子没生效,
//     ActiveWindow 会是 0 ⇒ 检测**永远不会报并发异常**,而症状是零。
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// anomalyTemplates auth.anomaly 块必须在里面显式出现的**模板**。
//
// ⚠️ 只盯模板,不盯 configs/config.dev.yaml —— 那个文件是 gitignore 的
// 本机开发配置,新克隆的仓库里根本没有它,盯它等于给 CI 埋一颗雷。
var anomalyTemplates = []string{"config.dev.yaml.example", "config.example.yaml"}

// anomalyFileKeys auth.anomaly 块里**必须显式出现在 yaml 里**的键。
var anomalyFileKeys = []string{
	"auth.anomaly.enabled",
	"auth.anomaly.max_concurrent",
	"auth.anomaly.active_window",
	"auth.anomaly.impossible_travel_window",
}

// TestLoad_AnomalyKeysAreDeclaredInYaml 键必须在模板里显式出现。
//
// 写错键名 → 落到 SetDefault → 零症状。InConfig 只认配置文件本身。
func TestLoad_AnomalyKeysAreDeclaredInYaml(t *testing.T) {
	for _, name := range anomalyTemplates {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join("..", "..", "configs", name)
			if err := Load(p); err != nil {
				t.Fatalf("load %s: %v", p, err)
			}
			for _, k := range anomalyFileKeys {
				if !AppViper.InConfig(k) {
					t.Errorf("configs/%s 里没有 %s —— 键名写错会静默落回默认值,"+
						"而症状是「检测静默不跑」", name, k)
				}
			}
		})
	}
}

// TestLoad_AnomalyBlockIsFullyParsed 值必须真的解析出来。
//
// 这一遍抓的是**另一个方向**:键在文件里,但 mapstructure tag 拼错了
// (比如 yaml 写 max_concurrent、tag 写 maxConcurrent)—— 键检查会过,值却是零值。
//
// ⚠️ max_concurrent=0 的后果不是"不检测",是"只允许 1 个会话在线"
// (maxConcurrent() 把 ≤0 兜底成 1)⇒ 正常多端登录的用户每次登录都收到提醒。
func TestLoad_AnomalyBlockIsFullyParsed(t *testing.T) {
	for _, name := range anomalyTemplates {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join("..", "..", "configs", name)
			if err := Load(p); err != nil {
				t.Fatalf("load %s: %v", p, err)
			}
			a := AppConfig.Auth.Anomaly

			if !a.Enabled {
				t.Error("enabled = false —— 检测整个不跑,「用户被盗号时无法感知」这条痛点又回来了")
			}
			if a.MaxConcurrent != 3 {
				t.Errorf("max_concurrent = %d, want 3(≤1 会被兜底成「只允许单端在线」)", a.MaxConcurrent)
			}
			if a.ActiveWindow != 30*time.Minute {
				t.Errorf("active_window = %v, want 30m0s(0 = 不判「同时在线」,并发异常永不触发)", a.ActiveWindow)
			}
			// impossible_travel_window 默认**就是关**的,这不是遗漏:
			// 本仓没有 IP 地理库,开着必然误报(见 config.AnomalyConfig 的注释)。
			if a.ImpossibleTravelWindow != 0 {
				t.Errorf("impossible_travel_window = %v, want 0(没有 IP 地理库,默认关)", a.ImpossibleTravelWindow)
			}
		})
	}
}

// TestLoad_AnomalyDefaultsWhenBlockMissing **漏掉整个块**时默认值必须仍然生效。
//
// 这一条盯的是 SetDefault → Unmarshal 这条链,尤其是 "30m" → time.Duration
// 的解码钩子。如果钩子没生效(或者哪次重构把默认值写成了整数),
// ActiveWindow 会是 0 ⇒ 检测**永远不会报并发异常** ——
// 而症状与"检测跑了但没有异常"**完全无法区分**,是最难查的一类静默失效。
func TestLoad_AnomalyDefaultsWhenBlockMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "no-anomaly.yaml")
	// 最小可用配置:只要能被 Load 读进来就行。
	body := "system:\n  addr: 8080\nauth:\n  revocation_mode: auto\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	if err := Load(p); err != nil {
		t.Fatalf("load %s: %v", p, err)
	}
	a := AppConfig.Auth.Anomaly

	if !a.Enabled {
		t.Error("漏配 auth.anomaly 块时 enabled 必须是 true —— 「漏配 = 照常跑」," +
			"否则漏配的后果是检测静默不跑,而症状与「跑了但没异常」无法区分")
	}
	if a.MaxConcurrent != 3 {
		t.Errorf("漏配时 max_concurrent = %d, want 3", a.MaxConcurrent)
	}
	if a.ActiveWindow != 30*time.Minute {
		t.Errorf("漏配时 active_window = %v, want 30m0s —— 解码钩子没生效的话这里是 0,"+
			"而 0 的后果是并发异常**永远不会触发**", a.ActiveWindow)
	}
}
