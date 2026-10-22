// 完整性校验的判据。
package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchesIsCaseInsensitive 文件 MD5 比对不区分大小写（网关返回小写、modInfo 用大写）。
func TestMatchesIsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := FileMD5(path)
	if err != nil {
		t.Fatal(err)
	}
	if !Matches(path, strings.ToUpper(sum)) {
		t.Fatal("大写 MD5 应视为匹配")
	}
	if Matches(path, "deadbeef") {
		t.Fatal("错误 MD5 不应匹配")
	}
	if Matches(filepath.Join(dir, "missing.bin"), sum) {
		t.Fatal("文件不存在不应匹配")
	}
}
