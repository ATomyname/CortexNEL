// 进服目标：控制通道要推送给客户端的「连哪里、用哪个角色」。
package clientcontrol

import "nexus/internal/mcversion"

// JoinTarget 是一次会话要推送给客户端的进服目标。
//
// nexus 与 Fantnel 的关键差异落在这里：Fantnel 填真实服务器地址，
// 让客户端直连服务器、由启动器旁路完成认证；nexus 填本机 relay 的地址，
// 让客户端连进代理、由 relay 在链路内代答认证。
type JoinTarget struct {
	// Host 是客户端要连接的主机，nexus 下为环回地址。
	Host string
	// Port 是客户端要连接的端口，nexus 下为 relay 实际监听端口。
	Port int
	// RoleName 是进服使用的角色名。控制通道是唯一能把它交给客户端的通道，
	// 启动命令行无法表达「用哪个网易角色进服」。
	RoleName string
	// MCVersion 决定客户端走高版本还是低版本的认证就绪流程。
	MCVersion string
}

// usesModernJoin 判断客户端是否走高版本进服流程。
func (t JoinTarget) usesModernJoin() bool {
	return mcversion.AtLeast(t.MCVersion, modernJoinFloor)
}

// frameFor 编码指定操作码的进服指令。
// 低版本（1031）在角色名之后多一个 bool，真源固定传 false。
func (t JoinTarget) frameFor(opcode uint16) []byte {
	p := newFrame(opcode)
	p.putString(t.Host)
	p.putInt32(int32(t.Port))
	p.putString(t.RoleName)
	if opcode == opJoinServerLegacy {
		p.putBool(false)
	}
	return p.payload()
}
