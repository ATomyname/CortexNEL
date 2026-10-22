package netease

import "testing"

// TestRoleUUIDGoldenVectors 守卫角色 UUID 派生的字节级稳定性。
//
// 期望值取自本仓 internal/launcher 上一版重复实现（同算法、同密钥）的实测输出，
// 在把派生收敛到本包时逐值抄录，用来证明收敛没有改变行为。
// 派生规则与白名单参考实现 Nirvana.Cipher 的 GenerateRoleUuid 一致；
// 任何改动若使本测试失败即为回归，不得改期望值绕过。
func TestRoleUUIDGoldenVectors(t *testing.T) {
	cases := []struct {
		role string
		uid  uint32
		want string
	}{
		{"Steve", 0, "81b8a1b77068406e9c819082cbcd5448"},
		{"Player1", 123456, "a77da55027d24a0d93b7aa0b1e17710d"},
		{"SaintSteve", 10001, "18bf666f4ca3456681f6f66feb0f61e7"},
		{"张三", 933874987, "615db57aa314429aaa0fbe95b594d458"},
	}
	for _, tc := range cases {
		got, err := RoleUUID(tc.role, tc.uid)
		if err != nil {
			t.Fatalf("RoleUUID(%q, %d): %v", tc.role, tc.uid, err)
		}
		if got != tc.want {
			t.Fatalf("RoleUUID(%q, %d) = %s, want %s", tc.role, tc.uid, got, tc.want)
		}
	}
}

// TestRoleUUIDShape 守卫 UUID 版本位与 variant 位。
func TestRoleUUIDShape(t *testing.T) {
	got, err := RoleUUID("Player1", 123456)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 {
		t.Fatalf("长度 = %d，期望 32 位 hex：%s", len(got), got)
	}
	// 第 6 字节高半字节 = 4 → hex 串下标 12。
	if got[12] != '4' {
		t.Fatalf("UUID version 位 = %c，期望 4：%s", got[12], got)
	}
	// 第 8 字节高两位 = 10 → hex 串下标 16 落在 8/9/a/b。
	switch got[16] {
	case '8', '9', 'a', 'b':
	default:
		t.Fatalf("UUID variant 位 = %c，期望 8/9/a/b：%s", got[16], got)
	}
}

// TestRoleUUIDDistinctInputs 守卫不同角色名派生不同 UUID。
func TestRoleUUIDDistinctInputs(t *testing.T) {
	a, err := RoleUUID("Player1", 123456)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RoleUUID("Player2", 123456)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("不同角色名派生出相同 UUID：%s", a)
	}
}
