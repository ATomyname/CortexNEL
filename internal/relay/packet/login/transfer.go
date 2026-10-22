// 原版转服拦截（vanilla Transfer，1.20.5 / protocol 766 引入）：
// 上游在 play / configuration 态发 ClientboundTransferPacket(host, port)，要求客户端
// 断开并直连新地址 —— 不拦截客户端就会绕开代理裸连真实服务器，认证代答与帧层适配
// 全部失效（2026-10-05 实测：转服回连目标 host 回显了带 FML 标记的握手 host，
// 客户端 DNS 解析 "<host>\0FORGE" 失败，整个转服流程死在半路）。
//
// 引擎职责：把包改写为本代理的回连地址（Conn.RelayAddr），并把真实目标经
// Conn.OnTransferRequest 递给编排层记录；客户端回连后由 Accept hook 改指新上游，
// 代理全程保持在链路内（DR-3 代答架构对转服后的新连接照常成立）。
//
// 协议事实单源（原版 1.21.8 反编译源码 —— Vesper workspace mc-1.21.8-src —— 与
// PrismarineJS minecraft-data 交叉验证，2026-10-05 核对）：
//   - 线格式：host = String、port = VarInt（ClientboundTransferPacket 用
//     readUtf / readVarInt；不是 wiki 旧页面写的 unsigned short）
//   - 包 ID（clientbound，按 GameProtocols/ConfigurationProtocols 的 addPacket
//     链序数出，与 PrismarineJS 一致）：play 态 766/767 = 0x73、768-772 = 0x7a；
//     configuration 态 766-772 恒 0x0b；765 及以下无此包
//   - 客户端收到后断开原连接（disconnect.transfer，"已转移至另一个服务器"），
//     以 intent = 3（ClientIntent.TRANSFER）回连 packet.host():port
//   - 客户端不会为新连接追加/剥离任何 FML 主机标记（NeoForge 1.20.5+ 已弃用
//     主机名标记机制）—— 上游转服 host 里看到的 "\0FORGE" 是对握手 host 的原样回显
package login

import (
	"net"
	"strconv"
	"strings"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// transferPacket 是 clientbound 转服指示：改写 host/port 指回本代理后原样转发。
// 未装配 RelayAddr（如部分测试路径）时保持原包不动，只记录真实目标。
type transferPacket struct {
	host string
	port int32

	outHost    string
	outPort    int32
	didRewrite bool
}

func (p *transferPacket) Read(_ *relay.Conn, r *codec.Reader) error {
	host, err := r.String()
	if err != nil {
		return err
	}
	port, err := r.VarInt()
	if err != nil {
		return err
	}
	p.host, p.port = host, port
	return nil
}

func (p *transferPacket) Handle(c *relay.Conn) (bool, error) {
	// 上游可能回显带 FML 标记的 host（握手 host 被原样送回，如
	// "ec.example.net\x00FORGE"）：记录目标时剥离标记，改写后的握手由
	// handshake 处理器按目标版本重新追加标记。
	target := p.host
	if idx := strings.IndexByte(target, 0); idx >= 0 {
		target = target[:idx]
	}
	addr := net.JoinHostPort(target, strconv.FormatInt(int64(p.port), 10))
	if c.OnTransferRequest != nil {
		c.OnTransferRequest(c, addr)
	}
	if c.RelayAddr != "" {
		if h, portStr, err := net.SplitHostPort(c.RelayAddr); err == nil {
			if port, err := strconv.Atoi(portStr); err == nil {
				p.outHost, p.outPort, p.didRewrite = h, int32(port), true
			}
		}
	}
	if p.didRewrite {
		c.Logger().Info("relay", "拦截转服：改指本代理", "target", addr, "advertise", c.RelayAddr)
	} else {
		c.Logger().Info("relay", "发现转服（未装配回连地址，原样转发）", "target", addr)
	}
	return false, nil
}

func (p *transferPacket) Write(w *codec.Writer) error {
	if p.didRewrite {
		w.String(p.outHost)
		w.VarInt(p.outPort)
		return nil
	}
	w.String(p.host)
	w.VarInt(p.port)
	return nil
}

// transferRegistrations 返回转服拦截处理器（GameID 通配；包 ID 随版本漂移，
// 按 ID 分组注册，版本清单见文件头事实单源）。
func transferRegistrations() []relay.Registered {
	var out []relay.Registered
	for _, e := range []struct {
		id       int32
		versions []int32
	}{
		{0x73, []int32{766, 767}},
		{0x7a, []int32{768, 769, 770, 771, 772}},
	} {
		out = append(out, relay.Registered{
			Key: relay.PacketKey{
				State: relay.StatePlay, Dir: relay.ClientBound,
				PacketID: e.id, Versions: e.versions,
			},
			Factory: func() relay.Packet { return &transferPacket{} },
		})
	}
	// common 包在 configuration 态同样可达（转服可发生在重配置周期内）。
	out = append(out, relay.Registered{
		Key: relay.PacketKey{
			State: relay.StateConfiguration, Dir: relay.ClientBound,
			PacketID: 0x0b, Versions: []int32{766, 767, 768, 769, 770, 771, 772},
		},
		Factory: func() relay.Packet { return &transferPacket{} },
	})
	return out
}
