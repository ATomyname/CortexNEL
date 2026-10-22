package namegen

import (
	"testing"
	"unicode/utf8"
)

func TestGenerate_NetEaseDefault(t *testing.T) {
	for i := 0; i < 50; i++ {
		name := Generate(Options{})
		runes := utf8.RuneCountInString(name)
		if runes < 3 || runes > 16 {
			t.Fatalf("生成的网易名称字符长度不合规: %q (len=%d)", name, runes)
		}
	}
}

func TestGenerate_NetEaseWithAffixesAndDigits(t *testing.T) {
	opts := Options{
		Prefix: "NX_",
		Suffix: "_Pro",
		Digits: 2,
	}
	for i := 0; i < 50; i++ {
		name := Generate(opts)
		runes := utf8.RuneCountInString(name)
		if runes < 3 || runes > 16 {
			t.Fatalf("带前后缀的网易名称长度超限: %d for %q", runes, name)
		}
	}
}

func TestGenerate_NetEaseStyles(t *testing.T) {
	styles := []string{"netease", "standard", "short", "homophone"}
	for _, style := range styles {
		for i := 0; i < 20; i++ {
			name := Generate(Options{Style: style})
			runes := utf8.RuneCountInString(name)
			if runes < 3 || runes > 16 {
				t.Fatalf("风格 %s 的网易名称长度不合规: %q (runes=%d)", style, name, runes)
			}
		}
	}
}

func TestBatchGenerate(t *testing.T) {
	list := BatchGenerate(Options{Prefix: "[战]"}, 5)
	if len(list) != 5 {
		t.Fatalf("预期生成 5 个，实际得到 %d", len(list))
	}
	for _, name := range list {
		runes := utf8.RuneCountInString(name)
		if runes < 3 || runes > 16 {
			t.Fatalf("批量生成的网易名称不合规: %q", name)
		}
	}
}

func TestGenerate_WithHomophone(t *testing.T) {
	for i := 0; i < 30; i++ {
		name := Generate(Options{UseHomophone: true})
		runes := utf8.RuneCountInString(name)
		if runes < 3 || runes > 16 {
			t.Fatalf("开启谐音生成的名称长度不合规: %q (runes=%d)", name, runes)
		}
		t.Logf("随机谐音名称 %d: %s", i+1, name)
	}
}

func TestGenerate_CustomBaseWordHomophone(t *testing.T) {
	opts := Options{
		Style:    "homophone",
		BaseWord: "海棠花阔地",
	}
	for i := 0; i < 20; i++ {
		name := Generate(opts)
		if utf8.RuneCountInString(name) != 5 {
			t.Fatalf("基础词5字应生成5字，得到: %s", name)
		}
		t.Logf("自定义基础词谐音 %d: %s -> %s", i+1, opts.BaseWord, name)
	}
}
