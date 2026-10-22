// Package heypixel 是布吉岛/HeyPixel 服务器适配包（架构文档 §6.5）。
// 本目录是仓库内唯一允许出现 GameID、brand 字面量的地方（P1/P2）。
// GameID 本身是 facts §3.4 已验证的协议字段，由服务器档案在运行时提供。
package heypixel

// GameID 是布吉岛网络服的 GameID（facts §3.4：HeypixelProtocol.GameId）。
const GameID = "4661334467366178884"

// 1.20.1（protocol 763）相关包 ID（真源已核对，facts §7.6）。
// 真源：vanilla 763 协议表（PrismarineJS minecraft-data data/pc/1.20/protocol.json）；
// 交叉验证：Nirvana 参考系适配 ID 全部声明在 V1206=766（1.20.6）且与原版 766 表
// 逐项吻合（teams=0x60、config C2S custom_payload=0x02 等），team 槽位 763→766
// 相差 +6。763 无 configuration 态，brand 走 play C2S（参考系 config 态拦截不适用）。
// 集中一处定义，L3 真机抓包若仍有个别出入只改本表。
const (
	idPlayPluginMessageServerbound   = 13 // play C2S custom payload = 0x0D（brand 1.20.1）
	idConfigPluginMessageServerbound = 2  // config C2S custom payload = 0x02（brand 1.20.6 / 1.21+）
	idSetPlayerTeamClientbound1201   = 92 // play S2C SetPlayerTeam in 1.20.1 (0x5C = 92)
	idSetPlayerTeamClientbound1206   = 96 // play S2C SetPlayerTeam in 1.20.6 (0x60 = 96)
	// play S2C custom payload 逐版本不同（763 表 "0x17": custom_payload；766+ 表 0x18）：
	// 763 的 0x18 是 damage_event —— 旧单值 24 使 s2cSyncHandler 在 763 上静默失效
	// （真 s2cevent 帧在 0x17 未被匹配而直通），并会把 damage_event 误当插件消息解析。
	// 与 team 槽位同纪律：按版本分段注册。
	idPlayPluginMessageClientbound763 = 23 // play S2C custom payload = 0x17（763 真源核对）
	idPlayPluginMessageClientbound766 = 24 // play S2C custom payload = 0x18（766+）
	// 766 的 play C2S custom payload = 0x12 = 18（真源勘误 2026-09-17：
	// PrismarineJS 1.20.5 play.toServer "0x12": custom_payload。旧值 2 是
	// configuration 态的 C2S custom_payload，属跨态外推错误 —— 用它注册的
	// 处理器在 play 态永不命中，屏蔽 likewise 静默失效）。
	idPlayPluginMessageServerbound766 = 18
	idHandshakeServerbound            = 0 // handshake C2S Intention = 0x00

	// ---- 影子端所需（walkthrough/12 §12.5）----
	// 核对记录（2026-09-17，三源交叉，详见 facts §7.6 附录）：
	//   真源 = vanilla 763 协议表（PrismarineJS minecraft-data data/pc/1.20/protocol.json，
	//   play.toClient: "0x23": keep_alive / "0x28": login(JoinGame)；play.toServer:
	//   "0x12": keep_alive / "0x0d": custom_payload）；
	//   交叉源 2 = 老项目 mc-relay VersionTable.java 763 行（join=0x28 / ka_s=0x23，
	//   注"老项目已验证"）；
	//   交叉源 3 = 本仓库 brand 链路实测（custom_payload C2S=0x0D 一直在真实流量上生效）。
	// 旧值勘误：KeepAlive S2C 0x1F / C2S 0x0F 为错误外推（0x1F 实为 763 C2S
	// enchant_item、0x0F 实为 763 C2S query_entity_nbt），已按真源改 0x23 / 0x12。
	// 消费方容错纪律不变：锚点/兜底取错不致命，但此三值已过真源核对。
	idPlayJoinGameClientbound1201 = 0x28 // play S2C JoinGame（世界重建锚点）763；✅三源一致
	// 766（1.20.5 / 1.20.6 同协议号）的 JoinGame = 0x2b。真源核对（2026-09-17）：
	// PrismarineJS minecraft-data data/pc/1.20.5/protocol.json 的 play.toClient
	// 映射中 "0x2b": "login"（该表内 JoinGame 即为 login）；版本映射经同仓
	// common/protocolVersions.json 确认 766 → 1.20.5/1.20.6 双版本共用。
	// 缺失该值时 RecordAnchorPacketID(766) 返回 -1 → 录制永不识别世界锚点 →
	// recorderReady=False → 影子端回灌被拒（2026-09-17 实测故障）。
	idPlayJoinGameClientbound1206 = 0x2b // play S2C JoinGame（世界重建锚点）766；✅真源核对
	// 766 的 KeepAlive 与 763 不同号（旧查表只覆盖 763~765，766 会拿到 (-1,-1)
	// 使闸门 KeepAlive 兜底静默失效）。真源：1.20.5 play.toClient "0x26":
	// keep_alive、play.toServer "0x18": keep_alive。
	idPlayKeepAliveClientbound766 = 0x26 // play S2C KeepAlive 766；✅真源核对
	idPlayKeepAliveServerbound766 = 0x18 // play C2S KeepAlive 766；✅真源核对
	idPlayKeepAliveClientbound    = 0x23 // play S2C KeepAlive 763；✅真源+老项目一致（旧 0x1F 错）
	idPlayKeepAliveServerbound    = 0x12 // play C2S KeepAlive 763；✅真源（旧 0x0F 错）
)

// RecordAnchorPacketID 返回当前协议版本的「世界重建锚点」包号；未知版本返回 -1
// （Recorder 据此退化为纯 FIFO 并置 truncated，不会误裁剪）。
func RecordAnchorPacketID(version int32) int32 {
	switch version {
	case 763, 764, 765:
		return idPlayJoinGameClientbound1201
	case 766, 767, 768:
		return idPlayJoinGameClientbound1206
	default:
		return -1
	}
}

// KeepAlivePacketIDs 返回 (S2C, C2S) KeepAlive 包号；未知版本返回 (-1, -1)。
func KeepAlivePacketIDs(version int32) (clientbound, serverbound int32) {
	switch version {
	case 763, 764, 765:
		return idPlayKeepAliveClientbound, idPlayKeepAliveServerbound
	case 766, 767, 768:
		return idPlayKeepAliveClientbound766, idPlayKeepAliveServerbound766
	default:
		return -1, -1
	}
}

// PlayS2CPluginMessageID 返回 play 态 S2C custom payload 的包号（逐版本分段）。
// 供影子端 CustomPayload 屏蔽与 s2cevent 捕获使用。
//
// 调用方不传协议版本（历史签名），因此这里按当前主链路版本 766 返回：
// 白端实测为 763→0x17、766→0x18，取错会让屏蔽与审计静默失效。
// 若将来支持 763 主链路，需改为按 c.ProtocolVersion 分派。
func PlayS2CPluginMessageID() int32 { return idPlayPluginMessageClientbound766 }

// PlayC2SPluginMessageID 返回 play 态 C2S custom payload 的包号（逐版本分段）。
// 763 = 0x0D；766 = 0x12（真源：1.20.5 play.toServer "0x12": custom_payload）。
// 影子端 B 腿用它屏蔽 mod 私有通道——屏蔽值取错版本会让 s2cevent 等私有包
// 直通上游（2026-09-17 勘误：此前恒返回 763 的 0x0D，766 下屏蔽失效）。
func PlayC2SPluginMessageID() int32 { return idPlayPluginMessageServerbound766 }

// brandForge 是重写后的 brand 载荷：0x05 长度前缀 + "forge"。
var brandForge = []byte{0x05, 'f', 'o', 'r', 'g', 'e'}
