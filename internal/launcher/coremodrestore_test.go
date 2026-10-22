package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRestoreDisabledCoreComponents 旧规则误停用的核心组件必须被恢复。
// 盘上实态：Game\.minecraft\mods\4690372021023046883@3@0.jar.disabled
func TestRestoreDisabledCoreComponents(t *testing.T) {
	modsDir := t.TempDir()
	disabled := filepath.Join(modsDir, "4690372021023046883@3@0.jar"+disabledModSuffix)
	mustWrite(t, disabled, "netease-core-mod")

	if got := RestoreDisabledCoreComponents(modsDir); got != 1 {
		t.Fatalf("恢复数 = %d，期望 1", got)
	}
	if !isExistingFile(filepath.Join(modsDir, "4690372021023046883@3@0.jar")) {
		t.Fatal("核心组件应已恢复为启用状态")
	}
	if isExistingFile(disabled) {
		t.Fatal(".disabled 副本应已消失")
	}
}

// TestRestoreDisabledCoreComponentsKeepsOtherDisabled 非核心组件的停用属用户意图，不得改动。
func TestRestoreDisabledCoreComponentsKeepsOtherDisabled(t *testing.T) {
	modsDir := t.TempDir()
	thirdParty := filepath.Join(modsDir, "CustomSkinLoader.jar"+disabledModSuffix)
	mustWrite(t, thirdParty, "user-disabled")
	// 资产模组（MTypeId=2）同样不是本函数的目标。
	assetMod := filepath.Join(modsDir, "4687456828685462969@2@8.jar"+disabledModSuffix)
	mustWrite(t, assetMod, "asset-mod")

	if got := RestoreDisabledCoreComponents(modsDir); got != 0 {
		t.Fatalf("恢复数 = %d，期望 0", got)
	}
	if !isExistingFile(thirdParty) || !isExistingFile(assetMod) {
		t.Fatal("非核心组件的停用状态不应被改动")
	}
}

// TestRestoreDisabledCoreComponentsKeepsExistingEnabled 同名启用文件已存在时不覆盖。
func TestRestoreDisabledCoreComponentsKeepsExistingEnabled(t *testing.T) {
	modsDir := t.TempDir()
	mustWrite(t, filepath.Join(modsDir, "4690372021023046883@3@0.jar"), "enabled-copy")
	disabled := filepath.Join(modsDir, "4690372021023046883@3@0.jar"+disabledModSuffix)
	mustWrite(t, disabled, "disabled-copy")

	if got := RestoreDisabledCoreComponents(modsDir); got != 0 {
		t.Fatalf("恢复数 = %d，期望 0", got)
	}
	data, err := os.ReadFile(filepath.Join(modsDir, "4690372021023046883@3@0.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "enabled-copy" {
		t.Fatalf("已有启用文件被覆盖为 %q", data)
	}
}

// TestRestoreDisabledCoreComponentsIsIdempotent 幂等：第二遍无文件可改。
func TestRestoreDisabledCoreComponentsIsIdempotent(t *testing.T) {
	modsDir := t.TempDir()
	mustWrite(t, filepath.Join(modsDir, "4690372021023046883@3@0.jar"+disabledModSuffix), "core")

	if got := RestoreDisabledCoreComponents(modsDir); got != 1 {
		t.Fatalf("首次恢复数 = %d，期望 1", got)
	}
	if got := RestoreDisabledCoreComponents(modsDir); got != 0 {
		t.Fatalf("二次恢复数 = %d，期望 0", got)
	}
}
