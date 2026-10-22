// 1.20.2+ (Protocol >= 764) Configuration 状态机支持。
package login

import (
	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// startConfiguration is sent by the server while PLAY is active when it
// starts another configuration cycle (protocol 766+).
type startConfiguration struct{ payload []byte }

// StartConfigurationPacketID 返回该协议版本 play 态 clientbound
// start_configuration 的包 ID；无重配置周期的版本（< 766）返回 -1。
//
// ID 按版本漂移：766/767 = 0x69、768/769 = 0x70、770~772 = 0x6f —— 依据原版
// 1.21.8 GameProtocols 的 addPacket 链序数出，并与 PrismarineJS minecraft-data
// 交叉验证。注意 772 的 0x69 是 set_title_subtitle（字幕包）：按旧表把它当
// start_configuration 解析会让 conn.State 整体错位、play 态处理器全部失配。
// 本函数是 start_configuration 包 ID 的单源，注册表与影子端录制边界共用。
func StartConfigurationPacketID(version int32) int32 {
	switch {
	case version >= 770:
		return 0x6f
	case version >= 768:
		return 0x70
	case version >= 766:
		return 0x69
	default:
		return -1
	}
}

func (p *startConfiguration) Read(_ *relay.Conn, r *codec.Reader) error {
	p.payload = append([]byte(nil), r.Rest()...)
	return nil
}

func (p *startConfiguration) Handle(c *relay.Conn) (bool, error) {
	c.Logger().Info("relay", "StartConfiguration -> Configuration state")
	c.BeginConfiguration()
	return false, nil
}

func (p *startConfiguration) Write(w *codec.Writer) error { w.Fixed(p.payload); return nil }

// loginAcknowledged 是客户端在收到 LoginSuccess 后的确认包 (C2S Packet ID 3)，用于切入 Configuration 态。
type loginAcknowledged struct {
	payload []byte
}

func (p *loginAcknowledged) Read(_ *relay.Conn, r *codec.Reader) error {
	p.payload = append([]byte(nil), r.Rest()...)
	return nil
}

func (p *loginAcknowledged) Handle(c *relay.Conn) (bool, error) {
	c.Logger().Info("relay", "LoginAcknowledged -> Configuration state")
	c.SwitchState(relay.StateConfiguration)
	return false, nil
}

func (p *loginAcknowledged) Write(w *codec.Writer) error {
	w.Fixed(p.payload)
	return nil
}

// acknowledgeFinishConfiguration 是客户端在收到 FinishConfiguration 后的确认包 (C2S Packet ID 3)，用于切入 Play 态。
type acknowledgeFinishConfiguration struct {
	payload []byte
}

func (p *acknowledgeFinishConfiguration) Read(_ *relay.Conn, r *codec.Reader) error {
	p.payload = append([]byte(nil), r.Rest()...)
	return nil
}

func (p *acknowledgeFinishConfiguration) Handle(c *relay.Conn) (bool, error) {
	c.Logger().Info("relay", "AcknowledgeFinishConfiguration -> Play state")
	c.AcknowledgeConfigurationFinish()
	return false, nil
}

func (p *acknowledgeFinishConfiguration) Write(w *codec.Writer) error {
	w.Fixed(p.payload)
	return nil
}
