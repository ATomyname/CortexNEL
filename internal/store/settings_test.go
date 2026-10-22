package store

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestRoleNameDigitsZeroRoundTrip 复现用户实测故障（2026-09-21）：
// 把「附加随机数字位数」调成 0（不添加数字）后，重新打开设置页又显示 2。
//
// 根因是 RoleNameDigits 带了 omitempty：0 被当作"未设置"整个省略，
// 前端 Object.assign 不拷贝缺失键、退回自建初值 2。
// 本测试同时钉住两件事：
//  1. 存盘 JSON 里 roleNameDigits 必须在场（哪怕值是 0）；
//  2. 存→读往返后仍是 0，且经 GetSettings 同一条 DTO 链路到前端也在场。
func TestRoleNameDigitsZeroRoundTrip(t *testing.T) {
	paths, err := NewPaths(t.TempDir())
	if err != nil {
		t.Fatalf("NewPaths: %v", err)
	}
	want := Settings{RoleNameDigits: 0, RoleNameStyle: "netease"}
	if err := SaveSettings(paths, want); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	raw, err := os.ReadFile(paths.ConfigFile())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), `"roleNameDigits"`) {
		t.Fatalf("roleNameDigits 被 omitempty 省略了，前端拿不到该键：\n%s", raw)
	}

	got, err := LoadSettings(paths)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got.RoleNameDigits != 0 {
		t.Fatalf("RoleNameDigits = %d，期望 0", got.RoleNameDigits)
	}

	// 前端实际收到的是 Settings 编成的 DTO：同样不能在 0 时消失。
	dto, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded struct {
		Digits *int `json:"roleNameDigits"`
	}
	if err := json.Unmarshal(dto, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Digits == nil {
		t.Fatalf("DTO 里 roleNameDigits 缺失，前端会退回默认值：\n%s", dto)
	}
	if *decoded.Digits != 0 {
		t.Fatalf("DTO roleNameDigits = %d，期望 0", *decoded.Digits)
	}
}

// TestRoleNameDigitsNonZeroRoundTrip 钉住正常值不受影响。
func TestRoleNameDigitsNonZeroRoundTrip(t *testing.T) {
	paths, err := NewPaths(t.TempDir())
	if err != nil {
		t.Fatalf("NewPaths: %v", err)
	}
	for _, want := range []int{1, 2, 3, 4} {
		if err := SaveSettings(paths, Settings{RoleNameDigits: want}); err != nil {
			t.Fatalf("SaveSettings(%d): %v", want, err)
		}
		got, err := LoadSettings(paths)
		if err != nil {
			t.Fatalf("LoadSettings: %v", err)
		}
		if got.RoleNameDigits != want {
			t.Fatalf("RoleNameDigits = %d，期望 %d", got.RoleNameDigits, want)
		}
	}
}

// TestSettingsOtherFieldsStillOmitEmpty 其余 omitempty 字段行为不变：
// 空串/false 仍被省略，不因本次修改放大配置文件。
func TestSettingsOtherFieldsStillOmitEmpty(t *testing.T) {
	paths, err := NewPaths(t.TempDir())
	if err != nil {
		t.Fatalf("NewPaths: %v", err)
	}
	if err := SaveSettings(paths, Settings{}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	raw, err := os.ReadFile(paths.ConfigFile())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	for _, absent := range []string{`"roleNamePrefix"`, `"roleNameHomophone"`, `"roleNameExactTone"`} {
		if strings.Contains(text, absent) {
			t.Fatalf("%s 不应出现在空值配置里：\n%s", absent, text)
		}
	}
	if !strings.Contains(text, `"roleNameDigits"`) {
		t.Fatalf("roleNameDigits 应始终在场：\n%s", text)
	}
}
