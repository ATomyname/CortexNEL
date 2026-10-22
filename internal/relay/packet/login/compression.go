// 压缩协商：SetCompression 更新两腿阈值并转发客户端（facts §7.2 步骤 4）。
package login

import (
	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

type enableCompression struct {
	threshold int32
}

func (p *enableCompression) Read(_ *relay.Conn, r *codec.Reader) error {
	v, err := r.VarInt()
	if err != nil {
		return err
	}
	p.threshold = v
	return nil
}

func (p *enableCompression) Handle(c *relay.Conn) (bool, error) {
	// server 腿立即启用（上游随后的帧已带压缩格式）；
	// client 腿在本包转发完成后启用（客户端处理前不理解压缩帧格式）。
	c.EnableServerCompression(p.threshold)
	c.MarkClientCompressionPending(p.threshold)
	// 通知编排层：影子端需要用同一阈值编码回灌帧（否则 B 帧边界错乱）。
	if c.OnCompression != nil {
		c.OnCompression(c, p.threshold)
	}
	return false, nil
}

func (p *enableCompression) Write(w *codec.Writer) error {
	w.VarInt(p.threshold)
	return nil
}
