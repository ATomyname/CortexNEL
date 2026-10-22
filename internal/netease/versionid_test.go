package netease

import "testing"

// TestMcVersionIDAnchors 钉死 /game-patch-info 实测锚点。
// 1.21.10 回归防护：旧 Contains 兜底曾把它错映射进 1.21（1021000），
// 导致装错版本包、verifyClientLayout 找 versions/1.21.10/ 失败。
func TestMcVersionIDAnchors(t *testing.T) {
	cases := map[string]int{
		"1.21.10":  1021010, // 实测 name=1.21.10-363051_…
		"1.21.8":   1021008,
		"1.21":     1021000,
		"1.20.6":   1020006,
		"1.20":     1020000,
		"1.19.2":   1019002,
		"1.14.3":   1014003,
		"1.12.2":   1012002,
		"1.9.4":    1009004,
		"1.8.9":    1008009,
		"1.7.10":   1007010,
		"1.7.2":    1007002,
		"1.21.10 ": 1021010, // 尾部噪声截断
	}
	for in, want := range cases {
		if got := McVersionID(in); got != want {
			t.Errorf("McVersionID(%q) = %d，期望 %d", in, got, want)
		}
	}
}
