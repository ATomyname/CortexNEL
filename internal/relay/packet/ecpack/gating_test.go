package ecpack

import (
	"testing"

	"nexus/internal/relay"
)

// realServer 是本机真实存档的一个服（%AppData%\CortexNEL\servers\*.json 摘录）。
type realServer struct {
	name   string
	gameID string
	ver    int32 // 该服 MC 版本对应的协议号；772 = 1.21.8
	isEC   bool
}

// realServers 覆盖本机全部 14 个存档服。
// ver 一律取该服真实版本，其中枫之屿同为 1.21.8→772，是版本门控放行、
// 唯一靠 GameID 挡的对照组 —— 它必须零命中。
var realServers = []realServer{
	{name: "EC小游戏·Java版1.21.8", gameID: "4688093584228529760", ver: 772, isEC: true},
	{name: "枫之屿(同为1.21.8)", gameID: "4678723331628683412", ver: 772},
	{name: "梦世界", gameID: "77140593557373952", ver: 767},
	{name: "布吉岛", gameID: "4661334467366178884", ver: 763},
	{name: "Clay粘土小游戏", gameID: "4619585369740458545", ver: 763},
	{name: "梦想世界", gameID: "4622278046164476475", ver: 340},
	{name: "第九大陆", gameID: "4627800895398370636", ver: 340},
	{name: "楼楼世界", gameID: "100540920033707008", ver: 340},
	{name: "MC部落", gameID: "4619328008690456675", ver: 340},
	{name: "东方大陆", gameID: "4619737516439909914", ver: 340},
	{name: "橙子生存", gameID: "4646072985456603598", ver: 340},
	{name: "DoMCer", gameID: "4634308743419409922", ver: 47},
	{name: "虎牙宇宙", gameID: "4642361503507129183", ver: 47},
	{name: "职业起床", gameID: "4624156820643253920", ver: 47},
}

// ecpackRegisteredKeys 是 ecpack 注册的全部 4 类键的探测点。
// 全部按 clientbound、协议 772 探测（版本门控最宽松的那一侧）。
var ecpackRegisteredKeys = []struct {
	label string
	st    relay.State
	dir   relay.Direction
	id    int32
}{
	{"资源包推送 play=74", relay.StatePlay, relay.ClientBound, idResourcePackPushPlay},
	{"资源包推送 play=9(实测)", relay.StatePlay, relay.ClientBound, 9},
	{"资源包推送 config=9", relay.StateConfiguration, relay.ClientBound, idResourcePackPushConfig},
	{"JoinGame=43", relay.StatePlay, relay.ClientBound, idJoinGamePlay},
}

// TestGatingOnlyECHasHandlers 真实服务器清单下，只有 EC 一个服能命中 ecpack 处理器。
//
// 这条是「不该出现就不许出现」的硬断言：枫之屿同为 1.21.8/772，版本门控对它
// 完全放行，若 GameID 门控失效，它会被误改写资源包。
func TestGatingOnlyECHasHandlers(t *testing.T) {
	rg := &relay.Registry{}
	rg.Register(Packets(GameIDs, stubProvider{ok: true})...)
	if len(rg.Snapshot()) == 0 {
		t.Fatal("ecpack 未注册任何处理器，断言无意义")
	}

	for _, s := range realServers {
		for _, k := range ecpackRegisteredKeys {
			hits := rg.Match(k.st, k.dir, k.id, s.gameID, s.ver)
			if s.isEC {
				if len(hits) == 0 {
					t.Errorf("%s(%s) 应命中 %s，实得 0", s.name, s.gameID, k.label)
				}
				continue
			}
			if len(hits) != 0 {
				t.Errorf("门控失效：非 EC 服 %s(%s) 命中了 %s，hits=%d",
					s.name, s.gameID, k.label, len(hits))
			}
		}
	}
}

// TestGatingWorstCaseNonECOn772 最坏情况：非 EC 服即便以 772 上报也必须零命中。
//
// 存档里枫之屿就是 1.21.8，这条把「版本恰好对上」的风险单独钉死。
func TestGatingWorstCaseNonECOn772(t *testing.T) {
	rg := &relay.Registry{}
	rg.Register(Packets(GameIDs, stubProvider{ok: true})...)

	for _, s := range realServers {
		if s.isEC {
			continue
		}
		for _, k := range ecpackRegisteredKeys {
			if hits := rg.Match(k.st, k.dir, k.id, s.gameID, 772); len(hits) != 0 {
				t.Errorf("最坏情况门控失效：非 EC 服 %s 以 772 上报命中 %s，hits=%d",
					s.name, k.label, len(hits))
			}
		}
	}
}

// TestGatingEmptyGameIDNeverMatches 连接未带 GameID（server.GameID 为空）时零命中。
func TestGatingEmptyGameIDNeverMatches(t *testing.T) {
	rg := &relay.Registry{}
	rg.Register(Packets(GameIDs, stubProvider{ok: true})...)

	for _, k := range ecpackRegisteredKeys {
		if hits := rg.Match(k.st, k.dir, k.id, "", 772); len(hits) != 0 {
			t.Errorf("空 GameID 误命中 %s，hits=%d", k.label, len(hits))
		}
	}
}

// TestGatingNoGameIDsMeansZeroHandlers GameIDs 为空 = 功能关闭，一条都不注册。
func TestGatingNoGameIDsMeansZeroHandlers(t *testing.T) {
	rg := &relay.Registry{}
	rg.Register(Packets(nil, stubProvider{ok: true})...)
	if n := len(rg.Snapshot()); n != 0 {
		t.Fatalf("空 GameIDs 应注册 0 条，实得 %d", n)
	}
	rg.Register(Packets([]string{""}, stubProvider{ok: true})...)
	if n := len(rg.Snapshot()); n != 0 {
		t.Fatalf("空串 GameID 应注册 0 条，实得 %d", n)
	}
}
