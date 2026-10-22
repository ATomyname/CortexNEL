// 登录结果：服务器对 LoginStart 的答复 —— 成功（LoginSuccess，facts §7.2 步骤 6）
// 或失败（Disconnect）。两者同属一个主题，故同文件（对齐 encryption.go / status.go
// 的按主题分组），但 LoginSuccess 的 UUID 代际分支单独留注释。
//
// LoginSuccess 是两条协议代际的分界：UUID 字段的编码在 1.16 换过一次形态，
// 按单一形态读会让老协议版本在登录阶段直接断连。
package login

import (
	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// minBinaryUUIDProtocol 是 LoginSuccess 的 UUID 字段改为 16 字节二进制的最低协议版本。
// 1.16 起 UUID 由「VarInt 长度字符串」（1.7.10~1.15.x 形态，实测 1.8=47 为 36 字符）
// 改为两个 long；边界取 735。
const minBinaryUUIDProtocol int32 = 735

type loginSuccess struct {
	relay.RawForward
	// uuidField 是 uuid 字段的原始载荷（不含长度前缀）。编码随协议版本换代，
	// 这里按读入时的形态缓存、回写时按原形态输出 —— Write 拿不到 Conn，无法再判版本。
	uuidField []byte
	// uuidIsString 标记 uuidField 是字符串形态（协议 < minBinaryUUIDProtocol），
	// 回写需补 VarInt 长度前缀；否则是裸 16 字节。
	uuidIsString bool
	username     string
}

func (p *loginSuccess) Read(c *relay.Conn, r *codec.Reader) error {
	// 按二进制读字符串形态会把 "xxxxxxxx-xxxx-4…" 截成 16 字节，随后 username 的
	// VarInt 长度从字符串中间取到垃圾值 → ErrTruncated，连接被 dispatch 直接关闭。
	// 实测故障：protocol 47 / packetID=2 / dir=s2c / 「codec: 字节流提前结束」。
	if c.ProtocolVersion < minBinaryUUIDProtocol {
		id, err := r.String()
		if err != nil {
			return err
		}
		p.uuidField = []byte(id)
		p.uuidIsString = true
	} else {
		raw, err := r.Fixed(16)
		if err != nil {
			return err
		}
		p.uuidField = append([]byte(nil), raw...)
		p.uuidIsString = false
	}
	name, err := r.String()
	if err != nil {
		return err
	}
	p.username = name
	// 尾部（properties / strict 等）不解释，原样转发：本实现比参考实现
	// sdk-ref/Codexus.ExampleMod/ProtocolSupport.cs 更容错 —— 后者把这两项解析成
	// 强类型，遇到版本增删会直接失败。
	return p.ReadAll(r)
}

func (p *loginSuccess) Handle(c *relay.Conn) (bool, error) {
	c.Logger().Info("relay", "LoginSuccess", "username", p.username)
	// protocol >= 764 (1.20.2+) 登录成功后进入 Configuration 态，其余直接入 Play
	if c.ProtocolVersion >= 764 {
		c.SwitchState(relay.StateConfiguration)
	} else {
		c.SwitchState(relay.StatePlay)
	}
	if c.OnLoginSuccess != nil {
		c.OnLoginSuccess(c)
	}
	return false, nil
}

func (p *loginSuccess) Write(w *codec.Writer) error {
	if p.uuidIsString {
		w.String(string(p.uuidField))
	} else {
		w.Fixed(p.uuidField)
	}
	w.String(p.username)
	p.WriteRaw(w)
	return nil
}

type loginDisconnect struct {
	reason string
}

func (p *loginDisconnect) Read(_ *relay.Conn, r *codec.Reader) error {
	s, err := r.String()
	if err != nil {
		return err
	}
	p.reason = s
	return nil
}

func (p *loginDisconnect) Handle(c *relay.Conn) (bool, error) {
	c.Logger().Warn("relay", "上游在登录阶段断开", "reason", p.reason)
	return false, nil
}

func (p *loginDisconnect) Write(w *codec.Writer) error {
	w.String(p.reason)
	return nil
}
