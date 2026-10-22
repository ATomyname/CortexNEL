package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// TestSettingsDTORoleNameDigitsAlwaysPresent 钉住 DTO 侧不能丢 roleNameDigits=0。
//
// 与 store 侧的 TestRoleNameDigitsZeroRoundTrip（internal/store/settings_test.go）
// 配对，覆盖同一条链路的两个掉落点：
//  1. 存盘：Settings 编进 config.json（store 包）；
//  2. 下发：Settings 编成 SettingsDTO 给前端（本包）。
//
// 两处都曾带 omitempty，0 都会被省略；前端 Object.assign 不拷贝缺失键，
// 于是界面退回默认值 —— 用户看到"调了不保存"。
func TestSettingsDTORoleNameDigitsAlwaysPresent(t *testing.T) {
	zero, err := json.Marshal(SettingsDTO{RoleNameDigits: 0})
	if err != nil {
		t.Fatalf("Marshal(0): %v", err)
	}
	if !strings.Contains(string(zero), `"roleNameDigits":0`) {
		t.Fatalf("DTO 丢了 roleNameDigits=0，前端会退回默认值：\n%s", zero)
	}

	// 非零值同样要在场，顺带钉住 1~4 全量程。
	for _, want := range []int{1, 2, 3, 4} {
		raw, err := json.Marshal(SettingsDTO{RoleNameDigits: want})
		if err != nil {
			t.Fatalf("Marshal(%d): %v", want, err)
		}
		needle := `"roleNameDigits":` + strconv.Itoa(want)
		if !strings.Contains(string(raw), needle) {
			t.Fatalf("DTO 缺 roleNameDigits=%d：\n%s", want, raw)
		}
	}
}

// TestSettingsDTOShadowEnabledAlwaysPresent 钉住 DTO 侧不能丢 shadowEnabled=false。
// 与 roleNameDigits 同一掉落点（omitempty 陷阱）：false 是用户的真实选择
// （影子端默认关闭），键一旦被省略，前端 Object.assign 不拷贝缺失键、退回
// 自建初值 —— 表现为"关了影子端还录制"。
func TestSettingsDTOShadowEnabledAlwaysPresent(t *testing.T) {
	off, err := json.Marshal(SettingsDTO{ShadowEnabled: false})
	if err != nil {
		t.Fatalf("Marshal(false): %v", err)
	}
	if !strings.Contains(string(off), `"shadowEnabled":false`) {
		t.Fatalf("DTO 丢了 shadowEnabled=false，前端会退回初值：\n%s", off)
	}
	on, err := json.Marshal(SettingsDTO{ShadowEnabled: true})
	if err != nil {
		t.Fatalf("Marshal(true): %v", err)
	}
	if !strings.Contains(string(on), `"shadowEnabled":true`) {
		t.Fatalf("DTO 缺 shadowEnabled=true：\n%s", on)
	}
}
