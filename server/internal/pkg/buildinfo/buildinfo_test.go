// buildinfo 单测(§1.8 补 P2)
package buildinfo

import (
	"encoding/json"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestSnapshot_Defaults(t *testing.T) {
	// 保存原值再恢复
	origApp, origVer, origCom, origBT := AppName, Version, Commit, BuildTime
	defer func() { AppName, Version, Commit, BuildTime = origApp, origVer, origCom, origBT }()

	AppName = "test-app"
	Version = "1.2.3"
	Commit = "abc1234"
	BuildTime = "2026-10-19T00:00:00Z"

	got := Snapshot()
	if got.AppName != "test-app" {
		t.Errorf("AppName = %q, want test-app", got.AppName)
	}
	if got.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", got.Version)
	}
	if got.Commit != "abc1234" {
		t.Errorf("Commit = %q, want abc1234", got.Commit)
	}
	if got.BuildTime != "2026-10-19T00:00:00Z" {
		t.Errorf("BuildTime = %q", got.BuildTime)
	}
	if got.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", got.GoVersion, runtime.Version())
	}
	if got.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", got.OS, runtime.GOOS)
	}
}

func TestSnapshot_Defaults_WhenNotInjected(t *testing.T) {
	// 默认值(未 ldflags 注入)
	if Version != "dev" {
		t.Errorf("default Version = %q, want dev", Version)
	}
	if Commit != "unknown" {
		t.Errorf("default Commit = %q, want unknown", Commit)
	}
	if BuildTime != "unknown" {
		t.Errorf("default BuildTime = %q, want unknown", BuildTime)
	}
}

func TestSnapshot_JSON(t *testing.T) {
	origApp, origVer, origCom, origBT := AppName, Version, Commit, BuildTime
	defer func() { AppName, Version, Commit, BuildTime = origApp, origVer, origCom, origBT }()

	AppName = "hass"
	Version = "v9"
	Commit = "deadbeef"
	BuildTime = "2026-01-01T00:00:00Z"

	got := Snapshot()
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"app_name":"hass"`,
		`"version":"v9"`,
		`"commit":"deadbeef"`,
		`"build_time":"2026-01-01T00:00:00Z"`,
		`"go_version":` + strconv.Quote(runtime.Version()),
	} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON missing %s, got: %s", want, s)
		}
	}
}
