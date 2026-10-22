package relay

import "testing"

func dummyFactory() Packet { return &NoForward{} }

func TestRegistryMatchRules(t *testing.T) {
	rg := &Registry{}
	rg.Register(
		// 引擎内置：登录加密请求，全服务器通配。
		Registered{Key: PacketKey{State: StateLogin, Dir: ClientBound, PacketID: 1}, Factory: dummyFactory},
		// 服务器适配：GameID 作用域。
		Registered{Key: PacketKey{State: StatePlay, Dir: ServerBound, PacketID: 9, GameID: "G1"}, Factory: dummyFactory},
		// 版本作用域。
		Registered{Key: PacketKey{State: StatePlay, Dir: ClientBound, PacketID: 86, GameID: "G1", Versions: []int32{763}}, Factory: dummyFactory},
	)

	if hits := rg.Match(StateLogin, ClientBound, 1, "ANY", 763); len(hits) != 1 {
		t.Fatalf("通配 GameID 未命中: %d", len(hits))
	}
	if hits := rg.Match(StatePlay, ServerBound, 9, "G1", 763); len(hits) != 1 {
		t.Fatalf("GameID 相等未命中: %d", len(hits))
	}
	if hits := rg.Match(StatePlay, ServerBound, 9, "G2", 763); len(hits) != 0 {
		t.Fatalf("GameID 不等不应命中: %d", len(hits))
	}
	if hits := rg.Match(StatePlay, ClientBound, 86, "G1", 763); len(hits) != 1 {
		t.Fatalf("版本命中失败: %d", len(hits))
	}
	if hits := rg.Match(StatePlay, ClientBound, 86, "G1", 758); len(hits) != 0 {
		t.Fatalf("版本不符不应命中: %d", len(hits))
	}
	// PacketID 通配。
	rg.Register(Registered{Key: PacketKey{State: StatePlay, Dir: ServerBound, PacketID: -1, GameID: "G1"}, Factory: dummyFactory})
	if hits := rg.Match(StatePlay, ServerBound, 123, "G1", 763); len(hits) != 1 {
		t.Fatalf("PacketID 通配未命中: %d", len(hits))
	}
}

func TestRegistrySnapshotIsFrozen(t *testing.T) {
	rg := &Registry{}
	rg.Register(Registered{Key: PacketKey{State: StateLogin, Dir: ClientBound, PacketID: 1}, Factory: dummyFactory})
	snap := rg.Snapshot()
	rg.Register(Registered{Key: PacketKey{State: StateLogin, Dir: ClientBound, PacketID: 2}, Factory: dummyFactory})
	if len(snap) != 1 {
		t.Fatalf("快照应冻结注册数量: %d", len(snap))
	}
}
