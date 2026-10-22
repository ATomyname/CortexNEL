package netease

import "testing"

func TestComponentMods(t *testing.T) {
	mods := componentMods([]ComponentDownloadInfo{{
		ItemID:      "123",
		MTypeID:     3,
		SubEntities: []ComponentDownloadSub{{JarMD5: "ABCD", ResName: "core", ResVersion: 7}, {JarMD5: ""}},
	}})
	if len(mods) != 1 || mods[0].IID != "123" || mods[0].MD5 != "ABCD" || mods[0].ModPath != "123@3@0.jar" {
		t.Fatalf("unexpected component mapping: %#v", mods)
	}
}
