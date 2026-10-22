package store

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers", "s1.json")

	type demo struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	if err := Save(path, demo{Name: "布吉岛", Port: 25565}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, version, err := Load[demo](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("version = %d", version)
	}
	if got.Name != "布吉岛" || got.Port != 25565 {
		t.Fatalf("roundtrip = %+v", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, _, err := Load[struct{}](filepath.Join(t.TempDir(), "none.json")); err == nil {
		t.Fatal("缺失文件应报错")
	}
}
