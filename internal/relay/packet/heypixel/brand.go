// brand 伪装：play 态 custom payload "minecraft:brand" → "forge"（facts 审计结论）。
package heypixel

import (
	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

type brandPluginMessage struct {
	relay.RawForward
	channel string
	payload []byte
}

func (p *brandPluginMessage) Read(_ *relay.Conn, r *codec.Reader) error {
	ch, err := r.String()
	if err != nil {
		return err
	}
	p.channel = ch
	p.payload = append([]byte(nil), r.Rest()...)
	return nil
}

func (p *brandPluginMessage) Handle(_ *relay.Conn) (bool, error) {
	if p.channel != "minecraft:brand" {
		return false, nil
	}
	p.payload = append([]byte(nil), brandForge...)
	return false, nil
}

func (p *brandPluginMessage) Write(w *codec.Writer) error {
	w.String(p.channel)
	w.Fixed(p.payload)
	return nil
}
