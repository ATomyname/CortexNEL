package login

import (
	"net"
	"strconv"
	"strings"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// 1.20.1（protocol 763）登录阶段包 ID（facts §7.2）。
const (
	idHandshake          = 0 // serverbound
	idLoginStart         = 0 // serverbound
	idEncryptionRequest  = 1 // clientbound
	idEncryptionResponse = 1 // serverbound
	idLoginSuccess       = 2 // clientbound
	idEnableCompression  = 3 // clientbound
	idLoginDisconnect    = 0 // clientbound
)

// handshake 是 C→S 握手包：记录协议版本与目标状态（对齐 Fantnel CHandshake 转发规范）。
type handshake struct {
	protocolVersion int32
	host            string
	port            uint16
	nextState       int32
}

func (p *handshake) Read(c *relay.Conn, r *codec.Reader) error {
	v, err := r.VarInt()
	if err != nil {
		return err
	}
	p.protocolVersion = v
	host, err := r.String()
	if err != nil {
		return err
	}
	p.host = host
	port, err := r.UShort()
	if err != nil {
		return err
	}
	p.port = port
	next, err := r.VarInt()
	if err != nil {
		return err
	}
	p.nextState = next
	return nil
}

func (p *handshake) Handle(c *relay.Conn) (bool, error) {
	c.Logger().Info("relay", "握手协议版本", "version", p.protocolVersion, "host", p.host, "nextState", p.nextState)
	c.SetProtocolVersion(p.protocolVersion)
	to := relay.StateLogin
	if p.nextState == 1 {
		to = relay.StateStatus
	}
	if p.nextState == 3 {
		// 转服回连（vanilla 1.20.5+ ClientIntent.TRANSFER）：客户端跟随上游的
		// Transfer 包回连本代理。对内按登录处理；对上游归一为普通登录
		// （nextState=2），避免不支持转入（accepts-transfers=false）的服务器
		// 以 transfers_disabled 拒连 —— 转服 cookie 应答等包照常透传。
		p.nextState = 2
	}
	c.SwitchState(to)
	if c.OnHandshake != nil {
		c.OnHandshake(c, to)
	}

	// 对齐 Fantnel CHandshake: 将 host 和 port 重写为上游真实地址与端口
	if c.UpstreamAddr != "" {
		if h, portStr, err := net.SplitHostPort(c.UpstreamAddr); err == nil {
			p.host = h
			if portNum, err := strconv.Atoi(portStr); err == nil && portNum > 0 {
				p.port = uint16(portNum)
			}
		} else {
			p.host = c.UpstreamAddr
		}
	}

	return false, nil
}

func (p *handshake) Write(w *codec.Writer) error {
	w.VarInt(p.protocolVersion)
	host := p.host
	if idx := strings.IndexByte(host, 0); idx >= 0 {
		host = host[:idx]
	}
	// 对齐 Fantnel CHandshake: 目标为游戏服务器 (nextState == 2) 时按版本追加 FML 标记
	if p.nextState == 2 {
		switch {
		case p.protocolVersion > 758 && p.protocolVersion <= 766: // 1.18.2 ~ 1.20.6
			host += "\x00FML3\x00"
		case p.protocolVersion > 758: // > 1.20.6
			host += "\x00FORGE"
		case p.protocolVersion <= 340: // <= 1.12.2
			host += "\x00FML\x00"
		default:
			host += "\x00FML2\x00"
		}
	}
	w.String(host)
	w.UShort(p.port)
	w.VarInt(p.nextState)
	return nil
}
