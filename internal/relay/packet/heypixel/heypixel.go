// 注册入口（架构文档 §6.5）：main.go 调 relay.Register(heypixel.Packets()...)。
package heypixel

import "nexus/internal/relay"

// Packets 返回全部服务器适配处理器，按 GameID 作用域注册。
func Packets() []relay.Registered {
	return []relay.Registered{
		{Key: relay.PacketKey{State: relay.StateHandshake, Dir: relay.ServerBound, PacketID: idHandshakeServerbound, GameID: GameID}, Factory: func() relay.Packet { return &fmlHandshake{} }},
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ServerBound, PacketID: idPlayPluginMessageServerbound, GameID: GameID, Versions: []int32{763, 764, 765}}, Factory: func() relay.Packet { return &brandPluginMessage{} }},
		{Key: relay.PacketKey{State: relay.StateConfiguration, Dir: relay.ServerBound, PacketID: idConfigPluginMessageServerbound, GameID: GameID, Versions: []int32{766, 767, 768}}, Factory: func() relay.Packet { return &brandPluginMessage{} }},
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ServerBound, PacketID: idPlayPluginMessageServerbound, GameID: GameID, Versions: []int32{763, 764, 765}}, Factory: func() relay.Packet { return &c2sAuditHandler{} }},
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ServerBound, PacketID: idPlayPluginMessageServerbound766, GameID: GameID, Versions: []int32{766}}, Factory: func() relay.Packet { return &c2sAuditHandler{} }},
		// S2C custom payload 逐版本分段（763=0x17 / 766+=0x18），与 team 槽位同纪律。
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ClientBound, PacketID: idPlayPluginMessageClientbound763, GameID: GameID, Versions: []int32{763, 764, 765}}, Factory: func() relay.Packet { return &s2cSyncHandler{} }},
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ClientBound, PacketID: idPlayPluginMessageClientbound766, GameID: GameID, Versions: []int32{766, 767, 768}}, Factory: func() relay.Packet { return &s2cSyncHandler{} }},
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ClientBound, PacketID: idSetPlayerTeamClientbound1201, GameID: GameID, Versions: []int32{763, 764, 765}}, Factory: func() relay.Packet { return &setPlayerTeam{} }},
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ClientBound, PacketID: idSetPlayerTeamClientbound1206, GameID: GameID, Versions: []int32{766, 767, 768}}, Factory: func() relay.Packet { return &setPlayerTeam{} }},
	}
}
