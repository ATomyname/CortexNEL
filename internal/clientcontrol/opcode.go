// Package clientcontrol 实现启动器与本机 Minecraft 客户端之间的控制通道。
//
// 职责边界：
//   - 只负责这条环回控制通道的协议收发与应答。
//   - 不启动进程（进程在 internal/service）、不装配启动参数（在 internal/launcher）、
//     不做 Yggdrasil 认证（在 internal/relay + internal/account 的链路内完成）。
//
// 通道由谁发起：带网易 protocol mod 的客户端启动后主动回连本机环回端口，
// 端口号经启动参数 --userProperties 的 launcherport 字段告知（装配在 internal/launcher）。
// 客户端在通道上索要「进哪个服、用哪个角色」，并周期性发心跳与校验请求。
//
// 协议真源分两层，判定应答行为时以第二层为准：
//
//  1. 形状参考：Reference/Fantnel-master/Fantnel-master/Nirvana.Game.Launcher/
//     Services/Java/RPC/ 下的 GameRpcService.cs、SimplePack.cs、SimpleUnpack.cs。
//     它给出了帧格式与应答载荷的构造方式，但它的 opcode 18 / 520 回调在发包前
//     必然抛异常（SimpleUnpack 对 null 实例取属性抛 TargetException；SimplePack
//     对皮肤枚举抛 NotSupportedException），因此**不能**据它判断「客户端是否需要应答」。
//  2. 行为真源：官方客户端的 NetworkSocket 与官方启动器的控制通道实测对话
//     （帧格式、操作码取值与应答矩阵见 docs/facts/NETEASE-PROTOCOL.md §10）。
//
// 帧格式、操作码与标识字符串由客户端决定，本包不得改写取值。
//
// nexus 的适配点：客户端连的是本机 relay 而不是真实服务器，
// 因此推送的进服目标是环回地址与 relay 端口，由调用方通过 JoinTarget 给出。
package clientcontrol

// 客户端 → 启动器的操作码（真源 GameRpcService.Connect 的回调注册表）。
const (
	opCheckPlayerMessage = 18   // 玩家消息校验（官方启动器逐包原样回吐）
	opEntityCheck        = 19   // 周期性实体校验，仅记录
	opAuthReadyModern    = 261  // 认证就绪（高版本）
	opHeartbeat          = 512  // 心跳（参考实现注册的回调）
	opAuthReadyLegacy    = 517  // 认证就绪（低版本）
	opPlayerSkin         = 520  // 皮肤请求
	opKeepAlive          = 530  // 周期保活：实测每整 600s 一发、仅 2 字节载荷、官方启动器不应答
	opMessageFilterCheck = 1298 // 消息过滤校验
	opLoginGame          = 4612 // 登录游戏通知，仅记录
)

// 启动器 → 客户端的操作码。
const (
	opJoinServerLegacy = 1031 // 进服指令（低版本，末尾多一个 bool）
	opJoinServerModern = 1799 // 进服指令（高版本）
)

// heartbeatIdentity 是心跳应答携带的启动器标识。
// 客户端按字面量比对，必须与参考实现逐字节一致（包含其中的拼写）。
const heartbeatIdentity = "i'am wpflauncher"

// modernJoinFloor 是改用 opJoinServerModern 的版本下限。
// 真源判定为「严格大于 1.18」（EnumGameVersion 是 major*1e6+minor*1e3+patch 的数值序），
// 落到版本号序即 >= 1.18.1。
const modernJoinFloor = "1.18.1"
