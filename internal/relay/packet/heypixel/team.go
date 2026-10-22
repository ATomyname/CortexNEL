// collideRule 队伍包丢弃：teamName 前缀 collideRule_ 的 S2C SetPlayerTeam 直接吞掉
// （facts 审计：heyhook collideRule team drop）。
package heypixel

import (
	"strings"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

type setPlayerTeam struct {
	relay.RawForward
	teamName string
}

func (p *setPlayerTeam) Read(_ *relay.Conn, r *codec.Reader) error {
	name, err := r.String()
	if err != nil {
		return err
	}
	p.teamName = name
	p.ReadAll(r)
	return nil
}

func (p *setPlayerTeam) Handle(_ *relay.Conn) (bool, error) {
	if strings.HasPrefix(p.teamName, "collideRule_") {
		return true, nil // 丢弃，不转发客户端
	}
	return false, nil
}

func (p *setPlayerTeam) Write(w *codec.Writer) error {
	w.String(p.teamName)
	p.WriteRaw(w)
	return nil
}
