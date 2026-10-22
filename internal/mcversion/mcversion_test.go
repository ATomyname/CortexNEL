package mcversion

import "testing"

func TestAtLeast(t *testing.T) {
	cases := []struct {
		version string
		floor   string
		want    bool
	}{
		{"1.20.1", "1.17", true},
		{"1.16.5", "1.17", false},
		{"1.17", "1.17", true},
		{"1.20", "1.20.5", false},
		{"1.20.5", "1.20.5", true},
		{"1.20.6", "1.20.5", true},
		{"1.21", "1.20.5", true},
		// Fantnel 的进服操作码判定是「严格大于 1.18」，落到本函数即 floor=1.18.1。
		{"1.18", "1.18.1", false},
		{"1.18.2", "1.18.1", true},
		{"1.19.2", "1.18.1", true},
		{"1.20", "1.18.1", true},
		{"1.12.2", "1.18.1", false},
		// 带后缀的版本号取前导数字，仍落在正确档位。
		{"1.20-pre1", "1.20", true},
		{"1.20.1-forge", "1.17", true},
		{"1.16.5-OptiFine", "1.17", false},
		{"", "1.8", false},
	}
	for _, c := range cases {
		if got := AtLeast(c.version, c.floor); got != c.want {
			t.Errorf("AtLeast(%q, %q) = %v，期望 %v", c.version, c.floor, got, c.want)
		}
	}
}

func TestSegmentsTolerantOfJunk(t *testing.T) {
	cases := map[string][]int{
		"1.20.x":       {1, 20, 0},
		"1.20.1-forge": {1, 20, 1},
		"1.20-pre1":    {1, 20},
		" 1.19.2 ":     {1, 19, 2},
		"v1.21":        {1, 21},
	}
	for input, want := range cases {
		got := Segments(input)
		if len(got) != len(want) {
			t.Errorf("Segments(%q) 段数 = %d，期望 %d：%v", input, len(got), len(want), got)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("Segments(%q)[%d] = %d，期望 %d", input, i, got[i], want[i])
			}
		}
	}
}
