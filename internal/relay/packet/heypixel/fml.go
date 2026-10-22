package heypixel

import (
	"net"
	"strconv"
	"strings"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

type fmlHandshake struct {
	protocolVersion int32
	host            string
	port            uint16
	nextState       int32
}

func (p *fmlHandshake) Read(_ *relay.Conn, r *codec.Reader) error {
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

func (p *fmlHandshake) Handle(c *relay.Conn) (bool, error) {
	// 仅在登录握手下兜底；状态查询（nextState=1）不动。
	if p.nextState == 1 {
		return false, nil
	}

	forwardHost := p.host
	forwardPort := p.port
	if c != nil && c.UpstreamAddr != "" {
		if h, portStr, err := net.SplitHostPort(c.UpstreamAddr); err == nil {
			forwardHost = h
			if portNum, err := strconv.Atoi(portStr); err == nil && portNum > 0 {
				forwardPort = uint16(portNum)
			}
		} else {
			forwardHost = c.UpstreamAddr
		}
	}

	// 移除可能已存在的标记
	if idx := strings.IndexByte(forwardHost, 0); idx >= 0 {
		forwardHost = forwardHost[:idx]
	}

	// 对齐 Fantnel CHandshake.cs:
	var suffix string
	switch {
	case p.protocolVersion > 758 && p.protocolVersion <= 766:
		suffix = "\x00FML3\x00"
	case p.protocolVersion > 758:
		suffix = "\x00FORGE"
	case p.protocolVersion <= 340:
		suffix = "\x00FML\x00"
	default:
		suffix = "\x00FML2\x00"
	}

	p.host = forwardHost + suffix
	p.port = forwardPort
	return false, nil
}

func (p *fmlHandshake) Write(w *codec.Writer) error {
	w.VarInt(p.protocolVersion)
	w.String(p.host)
	w.UShort(p.port)
	w.VarInt(p.nextState)
	return nil
}
