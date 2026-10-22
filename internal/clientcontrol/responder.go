// 操作码 → 应答的映射（形状参考 GameRpcService 的各 Handle*/On* 回调，
// 应答与否以官方启动器的控制通道实测对话为准，见 opcode.go 的真源分层说明）。
// 拆成纯函数是为了能脱离套接字直接验证应答字节。
package clientcontrol

// replyKind 说明一次收帧的处理结果，用于日志与测试断言。
type replyKind string

const (
	replyJoinServer        replyKind = "进服指令"
	replyHeartbeat         replyKind = "心跳"
	replyFilterCheck       replyKind = "消息过滤校验"
	replyPlayerMessageEcho replyKind = "玩家消息校验回吐"
	replyObserveOnly       replyKind = "仅记录"
	replyUnknownOpcode     replyKind = "未知操作码"
)

// reply 计算一次收帧的应答载荷。返回值 nil 表示无需回写。
//
// payload 是完整帧载荷（含起头 2 字节操作码，由 readFrame 给出）。
//
// 应答矩阵（实测依据见 docs/facts/NETEASE-PROTOCOL.md §10）：
//   - 261 / 517：认证就绪 → 推 1799 / 1031 进服指令。
//   - 512：心跳 → 回启动器标识。
//   - 1298：消息过滤校验 → false + 两个 int64 零值。
//   - 18：玩家消息校验 → **原样回吐**。官方启动器对每一帧 18 都回一帧同长度 18，
//     实测 313/532/330/560 四种载荷各 3 次请求对 3 次应答，长度逐一相同；
//     字段语义与参考实现的 Pack(18, Length, Message) 一致。回吐是该实测范围内
//     最保守的形态：不解析字段，也就不存在编错未验证字段的可能。
//   - 530：周期保活 → 不应答。官方启动器实测从不回它（每整 600s 一发、仅 2 字节
//     载荷，连接连续健康存活 1 小时），因此这里也不回。
//   - 520：皮肤 → 不应答。官方启动器会回皮肤数据，但皮肤不参与进服，
//     本实现按 M1 范围只做记录（皮肤走 internal/app/skin_binding.go 的 HTTP 通道）。
//   - 19 / 4612：仅记录。
func reply(opcode uint16, payload []byte, target JoinTarget) ([]byte, replyKind) {
	switch opcode {
	case opAuthReadyModern:
		// 真源仅在高版本回应 261；低版本客户端会另发 517。
		if !target.usesModernJoin() {
			return nil, replyObserveOnly
		}
		return target.frameFor(opJoinServerModern), replyJoinServer

	case opAuthReadyLegacy:
		// 真源对 517 恒用 1031 应答，不按版本切换。
		return target.frameFor(opJoinServerLegacy), replyJoinServer

	case opHeartbeat:
		p := newFrame(opHeartbeat)
		p.putString(heartbeatIdentity)
		return p.payload(), replyHeartbeat

	case opMessageFilterCheck:
		p := newFrame(opMessageFilterCheck)
		p.putBool(false)
		p.putInt64(0)
		p.putInt64(0)
		return p.payload(), replyFilterCheck

	case opCheckPlayerMessage:
		if len(payload) < 2 {
			// 连操作码都装不下：没有可回吐的内容。readFrame 已保证不会走到这里，
			// 这里兜住直接调用本函数的路径（含测试）。
			return nil, replyObserveOnly
		}
		// 返回值与入参共用底层数组：调用方写入套接字后即丢弃该帧，无需复制。
		return payload, replyPlayerMessageEcho

	case opKeepAlive, opEntityCheck, opLoginGame, opPlayerSkin:
		return nil, replyObserveOnly

	default:
		return nil, replyUnknownOpcode
	}
}
