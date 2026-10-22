// 盘根候选顺序的判据：动态枚举 + 稳定优先级，不依赖白名单。
package launcher

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestNeteaseInstallRootsDynamicAndStable 候选盘根必须来自真实固定盘枚举
// 而非白名单：每个根都落在固定盘上、去重、带分隔符，且 D/C 优先的次序稳定。
// 白名单 D:/C:/E:/F:/G: 会让装在其余固定盘上的已有客户端探测不到。
func TestNeteaseInstallRootsDynamicAndStable(t *testing.T) {
	roots := neteaseInstallRoots()
	if len(roots) == 0 {
		t.Skip("本机没有可枚举的固定盘（非 Windows 或枚举为空）")
	}
	seen := map[string]bool{}
	for _, root := range roots {
		if seen[root] {
			t.Fatalf("候选盘根重复: %v", roots)
		}
		seen[root] = true
		if !strings.HasSuffix(root, string(filepath.Separator)) {
			t.Fatalf("盘根应带分隔符: %q", root)
		}
		drive := strings.ToUpper(root[:1])
		found := false
		for _, d := range fixedDrives() {
			if string(d) == drive {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("候选 %q 不是固定盘（fixedDrives=%v）", root, fixedDrives())
		}
	}
	first := strings.ToUpper(roots[0][:1])
	if first != "D" && first != "C" {
		t.Fatalf("首候选应是 D 或 C（网易默认装 D 盘，其次系统盘），实得 %q in %v", roots[0], roots)
	}
	// 其余候选按盘符序：剔除首候选后不得乱序。
	rest := roots[1:]
	for i := 1; i < len(rest); i++ {
		if rest[i-1] >= rest[i] {
			t.Fatalf("除首候选外应按盘符升序: %v", roots)
		}
	}
}
