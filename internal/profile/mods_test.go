package profile

import (
	"os"
	"path/filepath"
	"testing"

	"nexus/internal/netease"
)

func TestScanModsAndCompare(t *testing.T) {
	dir := t.TempDir()
	// 两个组件 jar + 一个无关 jar。
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("10086@3@0.jar", []byte("core jar"))
	write("20001@2@8.jar", []byte("asset jar"))
	write("optifine.jar", []byte("not a component"))

	mods, err := ScanMods(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 {
		t.Fatalf("应识别 2 个组件: %+v", mods)
	}
	for _, m := range mods {
		if len(m.MD5) != 32 {
			t.Fatalf("md5 未计算: %+v", m)
		}
	}

	// 期望一致 → 无差异。
	got := CompareMods(mods, mods)
	if len(got.Added)+len(got.Removed)+len(got.Changed) != 0 {
		t.Fatalf("相同集合不应有差异: %+v", got)
	}

	// 服务器要求全部，本地缺 10086 → added=[10086]。
	var actual []netease.Mod
	for _, m := range mods {
		if m.IID != "10086" {
			actual = append(actual, m)
		}
	}
	res := CompareMods(mods, actual)
	if len(res.Added) != 1 || res.Added[0] != "10086" {
		t.Fatalf("added = %+v", res)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("removed 应为空: %+v", res)
	}

	// 本地多出 30001 → removed=[30001]。
	extra := append(append([]netease.Mod{}, mods...), netease.Mod{IID: "30001", MD5: "AAAA"})
	res = CompareMods(mods, extra)
	if len(res.Removed) != 1 || res.Removed[0] != "30001" {
		t.Fatalf("removed = %+v", res)
	}

	// md5 变化 → changed。
	changed := []netease.Mod{{IID: "10086", MD5: "FFFF"}}
	res = CompareMods(changed, mods)
	if len(res.Changed) != 1 || res.Changed[0] != "10086" {
		t.Fatalf("changed = %+v", res)
	}
}

func TestScanModsMissingDir(t *testing.T) {
	if _, err := ScanMods(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("缺失目录应报错")
	}
}
