package login

import (
	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// loginStart preserves the version-specific tail while binding the username
// to the role used by the NetEase join session.
type loginStart struct {
	name string
	tail []byte
}

func (p *loginStart) Read(_ *relay.Conn, r *codec.Reader) error {
	name, err := r.String()
	if err != nil {
		return err
	}
	p.name = name
	p.tail = append([]byte(nil), r.Rest()...)
	return nil
}

func (p *loginStart) Handle(c *relay.Conn) (bool, error) {
	if c.LoginName != "" {
		p.name = c.LoginName
	}
	return false, nil
}

func (p *loginStart) Write(w *codec.Writer) error {
	w.String(p.name)
	w.Fixed(p.tail)
	return nil
}
