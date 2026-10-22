package netease

import (
	"testing"
)

func TestBuildModInfoJSONEmptyList(t *testing.T) {
	// 对齐 Fantnel GameProfile.GetModInfo：空列表输出 ""
	if got := BuildModInfoJSON(nil); got != "" {
		t.Fatalf("空列表 = %s", got)
	}
	if got := BuildModInfoJSON([]Mod{}); got != "" {
		t.Fatalf("空切片 = %s", got)
	}
}

func TestBuildModInfoJSONFieldOrder(t *testing.T) {
	// facts §3.7：字段序固定 modPath,name,id,iid,md5,version。
	got := BuildModInfoJSON([]Mod{{ModPath: "10000@3@0.jar", ID: "0", IID: "10000", MD5: "AB12"}})
	want := `{"mods":[{"modPath":"10000@3@0.jar","name":"","id":"0","iid":"10000","md5":"AB12","version":""}]}`
	if got != want {
		t.Fatalf("字段序不符:\n got %s\nwant %s", got, want)
	}
}

func TestParseJarNameCoreMod(t *testing.T) {
	// facts §3.7 / Fantnel：核心模组(MTypeId=3) modPath=id={ItemId}@{MTypeId}@0.jar。
	m, err := ParseJarName("10086@3@520.jar")
	if err != nil {
		t.Fatal(err)
	}
	if m.ModPath != "10086@3@0.jar" || m.IID != "10086" || m.ID != "10086@3@0.jar" {
		t.Fatalf("核心模组解析 = %+v", m)
	}
}

func TestParseJarNameAssetMod(t *testing.T) {
	// facts §3.7：资产模组(2) 可用实际文件名（@2@8 风格）。
	m, err := ParseJarName("20001@2@8.jar")
	if err != nil {
		t.Fatal(err)
	}
	if m.ModPath != "20001@2@8.jar" || m.IID != "20001" {
		t.Fatalf("资产模组解析 = %+v", m)
	}
}

func TestParseJarNameRejectsBadShape(t *testing.T) {
	for _, name := range []string{"plain.jar", "a@b.jar", "a@b@c@d.jar", "@3@0.jar"} {
		if _, err := ParseJarName(name); err == nil {
			t.Fatalf("%q 应被拒绝", name)
		}
	}
}
