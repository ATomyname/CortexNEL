// 内置处理器的注册入口：全部 GameID 通配（引擎内置 = 服务器无关，P1/P2）。
package login

import "nexus/internal/relay"

// Packets 返回引擎内置处理器（main.go 注册进引擎注册表）。
func Packets() []relay.Registered {
	return append(transferRegistrations(), []relay.Registered{
		{Key: relay.PacketKey{State: relay.StateHandshake, Dir: relay.ServerBound, PacketID: idHandshake}, Factory: func() relay.Packet { return &handshake{} }},
		// 状态查询（server list ping）：必须在本机应答，不能转发上游 ——
		// 转发会让上游按 host 判定后回 EOF，从而打断正在进行的会话（见 status.go）。
		{Key: relay.PacketKey{State: relay.StateStatus, Dir: relay.ServerBound, PacketID: idStatusRequest}, Factory: func() relay.Packet { return &statusRequest{} }},
		{Key: relay.PacketKey{State: relay.StateStatus, Dir: relay.ServerBound, PacketID: idStatusPing}, Factory: func() relay.Packet { return &statusPing{} }},
		{Key: relay.PacketKey{State: relay.StateLogin, Dir: relay.ServerBound, PacketID: idLoginStart}, Factory: func() relay.Packet { return &loginStart{} }},
		{Key: relay.PacketKey{State: relay.StateLogin, Dir: relay.ClientBound, PacketID: idEncryptionRequest}, Factory: func() relay.Packet { return &encryptionRequest{} }},
		{Key: relay.PacketKey{State: relay.StateLogin, Dir: relay.ServerBound, PacketID: idEncryptionResponse}, Factory: func() relay.Packet { return &clientEncryptionResponse{} }},
		{Key: relay.PacketKey{State: relay.StateLogin, Dir: relay.ClientBound, PacketID: idEnableCompression}, Factory: func() relay.Packet { return &enableCompression{} }},
		{Key: relay.PacketKey{State: relay.StateLogin, Dir: relay.ClientBound, PacketID: idLoginSuccess}, Factory: func() relay.Packet { return &loginSuccess{} }},
		{Key: relay.PacketKey{State: relay.StateLogin, Dir: relay.ClientBound, PacketID: idLoginDisconnect}, Factory: func() relay.Packet { return &loginDisconnect{} }},
		// 状态机切换：这两个包决定 state 是否推进到 CONFIGURATION / PLAY。
		// 版本列表漏掉某版本会让该版本下的会话"看起来连上了"却始终停在旧状态，
		// 从而让所有 play 态处理器失效（1.21.8 = 协议 772 就是这样踩到的）。
		// id 依据原版 1.21.8 源码逐条数出：login_acknowledged(SB)=3、
		// finish_configuration(SB, configuration)=3，与 764~768 一致。
		{Key: relay.PacketKey{State: relay.StateLogin, Dir: relay.ServerBound, PacketID: 3, Versions: []int32{764, 765, 766, 767, 768, 769, 770, 771, 772}}, Factory: func() relay.Packet { return &loginAcknowledged{} }},
		{Key: relay.PacketKey{State: relay.StateConfiguration, Dir: relay.ServerBound, PacketID: 3, Versions: []int32{766, 767, 768, 769, 770, 771, 772}}, Factory: func() relay.Packet { return &acknowledgeFinishConfiguration{} }},
		// start_configuration（play → configuration 重入）：clientbound ID 随版本
		// 漂移，取值单源在 StartConfigurationPacketID（含 772 的 0x69 实为
		// set_title_subtitle 的修正说明）。
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ClientBound, PacketID: StartConfigurationPacketID(766), Versions: []int32{766, 767}}, Factory: func() relay.Packet { return &startConfiguration{} }},
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ClientBound, PacketID: StartConfigurationPacketID(768), Versions: []int32{768, 769}}, Factory: func() relay.Packet { return &startConfiguration{} }},
		{Key: relay.PacketKey{State: relay.StatePlay, Dir: relay.ClientBound, PacketID: StartConfigurationPacketID(770), Versions: []int32{770, 771, 772}}, Factory: func() relay.Packet { return &startConfiguration{} }},
	}...)
}
