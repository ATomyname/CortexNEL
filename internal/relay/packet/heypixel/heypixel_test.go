package heypixel

import (
	"testing"

	"nexus/internal/relay"
)

// 注册键必须是 GameID 作用域（P2 断言：适配包必须绑定 GameID 才能注册），
// 且不得注册进登录态（登录态属引擎内置，facts §7.2）。
func TestPacketsAreGameIDScoped(t *testing.T) {
	for _, e := range Packets() {
		if e.Key.GameID != GameID {
			t.Fatalf("适配包注册键未绑定 GameID: %+v", e.Key)
		}
		if e.Key.State == relay.StateLogin {
			t.Fatalf("适配包不应注册在登录态: %+v", e.Key)
		}
	}
}
