package login

import (
	"fmt"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// 状态查询（nextState=1）阶段的包 ID。与登录段同号但语义不同，
// 由握手里的 nextState 区分，故分开命名避免误用。
const (
	idStatusRequest = 0 // C→S：StatusRequest（空载荷）
	idStatusPing    = 1 // 双向：Ping(int64) / Pong(同值)
)

// statusRequest 处理 server list ping 的 StatusRequest。
//
// 为什么必须在**本机应答**而不是转发上游：relay 端口是对外暴露的（白端客户端连它），
// 任何客户端在服务器列表里刷新条目都会发 status 查询。转发上游会走完整的握手
// 语义（上游按 host 判定），上游回 EOF → 该连接断开 → 会话被编排层判为 FAULTED，
// **正在进行的游戏会话因此被打断**（2026-09-17 实测：B 客户端刷新列表即打断白端）。
//
// 应答后本连接结束：status 是独立短连接，客户端拿到响应会另开连接做真正的登录。
type statusRequest struct{}

func (p *statusRequest) Read(_ *relay.Conn, _ *codec.Reader) error { return nil }

func (p *statusRequest) Handle(c *relay.Conn) (bool, error) {
	body := fmt.Sprintf(statusJSONTemplate, c.ProtocolVersion)
	w := codec.NewWriter()
	w.String(body)
	if err := c.WriteToClient(idStatusResponse, w.Bytes()); err != nil {
		c.Logger().Warn("relay", "状态查询应答写回失败", "connID", c.ID, "err", err.Error())
	}
	c.Logger().Info("relay", "已在本机应答状态查询", "connID", c.ID, "version", c.ProtocolVersion)
	// cancel=true：该帧不回上游。连接由 statusPing 处理器在 Pong 后关闭；
	// 客户端若不再发帧会自行断开。
	return true, nil
}

func (p *statusRequest) Write(w *codec.Writer) error { return nil }

// statusPing 处理 Ping 并回同值 Pong，随后结束该连接。
type statusPing struct {
	value []byte
}

func (p *statusPing) Read(_ *relay.Conn, r *codec.Reader) error {
	p.value = append([]byte(nil), r.Rest()...)
	return nil
}

func (p *statusPing) Handle(c *relay.Conn) (bool, error) {
	if err := c.WriteToClient(idStatusPing, p.value); err != nil {
		c.Logger().Warn("relay", "状态 Ping 应答写回失败", "connID", c.ID, "err", err.Error())
	} else {
		c.Logger().Info("relay", "已应答状态 Ping，关闭该探测连接", "connID", c.ID)
	}
	// 关掉客户端腿：status 是短连接，不必占用会话。
	// 注意这里只关这一条腿 —— 上游腿由 dispatch 的 cancel 路径随连接收尾。
	c.CloseClientLeg()
	return true, nil
}

func (p *statusPing) Write(w *codec.Writer) error { return nil }

// idStatusResponse 是 StatusResponse 的包 id（与 StatusRequest 同号，方向相反）。
const idStatusResponse = 0

// statusJSONTemplate 是状态查询的应答体（%d = 协议版本）。
//
// 描述里点明这是 CortexNEL 代理，便于用户在服务器列表分辨；
// 谎报在线人数没有意义，据实给 0。
const statusJSONTemplate = `{"version":{"name":"CortexNEL","protocol":%d},` +
	`"players":{"max":0,"online":0},"description":{"text":"CortexNEL 代理"}}`
