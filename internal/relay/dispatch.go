// 派发（架构文档 §6.3）：无命中 → 原始字节直接转发（P4 快路径）；
// 有命中 → 按 Read → Handle → Write 重新序列化，cancel 优先，改写按冻结顺序叠加。
package relay

import "nexus/internal/relay/codec"

// dispatch 处理 from 腿读出的一个包；返回 false 终止连接。
func (c *Conn) dispatch(from *leg, id int32, payload []byte) bool {
	forwardState := c.State()
	other := c.otherLeg(from)
	if other == nil {
		c.Close()
		return false
	}

	// Seam-2 上行闸门（影子接管期冻结白端上行）：引擎只执行回调，不解释策略。
	// 放在 registry 匹配之前 —— 吞帧走的是零解析路径（P4）。
	if from == c.client && c.C2SGate != nil && !c.C2SGate(id) {
		return true
	}

	hits := c.registry.Match(c.State(), from.dir, id, c.GameID, c.ProtocolVersion)
	if len(hits) == 0 {
		if err := other.writePacket(id, payload); err != nil {
			c.Close()
			return false
		}
		c.afterForward(from, forwardState, id, payload)
		if from == c.server && forwardState == StateConfiguration && id == 3 {
			c.MarkConfigurationFinishSent()
		}
		return true
	}

	// 处理器只拿到包体：记下当前包 id，供通配注册的处理器/诊断日志使用。
	c.mu.Lock()
	c.curPacketID = id
	c.mu.Unlock()

	cur := payload
	for _, h := range hits {
		p := h.Factory()
		if err := p.Read(c, codec.NewReader(cur)); err != nil {
			c.Logger().Error("relay", "包体反序列化失败", "connID", c.ID, "state", stateName(c.State()), "packetID", id, "dir", dirName(from.dir), "err", err.Error())
			c.PublishError("protocol.frame.error", map[string]any{
				"connID": c.ID, "state": stateName(c.State()), "packetID": id,
				"dir": dirName(from.dir), "err": err.Error(),
			})
			c.Close()
			return false
		}
		cancel, err := p.Handle(c)
		if err != nil {
			c.Logger().Error("relay", "处理器失败", "packetID", id, "state", stateName(c.State()), "err", err.Error())
			c.Close()
			return false
		}
		if cancel {
			return true
		}
		w := codec.NewWriter()
		if err := p.Write(w); err != nil {
			c.Logger().Error("relay", "包序列化错误", "packetID", id, "state", stateName(c.State()), "err", err.Error())
			c.Close()
			return false
		}
		// 后者基于前者的输出叠加（P3：冻结顺序确定性）。
		cur = w.Bytes()
	}

	if err := other.writePacket(id, cur); err != nil {
		c.Logger().Warn("relay", "转发包失败", "target", other.name, "packetID", id, "state", stateName(c.State()), "err", err.Error())
		c.Close()
		return false
	}
	c.afterForward(from, forwardState, id, cur)
	if from == c.server && forwardState == StateConfiguration && id == 3 {
		c.MarkConfigurationFinishSent()
	}
	// SetCompression 已成功转发给客户端：此后客户端理解压缩帧格式（facts §7.2 步骤 4）。
	if from == c.server {
		c.applyPendingClientCompression()
		// 注入包必须晚于当前包发出（例如资源包推送要等 JoinGame 先到，客户端才在 play 态）。
		c.drainClientInjections()
	}
	return true
}

// afterForward 在帧成功转发后触发客户端方向录制 tap（Seam-1）并分流给影子腿。
//
// 只在 server→client 方向触发，且给出的是最终写出字节（含处理器改写）——
// 录制必须与客户端实际所见一致，否则重放给影子端的世界与白端不同。
// 被 cancel 的包不会到这里（它没有转发给客户端）。
func (c *Conn) afterForward(from *leg, state State, id int32, payload []byte) {
	if from != c.server {
		return
	}
	if c.OnClientBoundFrame != nil {
		c.OnClientBoundFrame(c, id, payload)
	}
	if c.OnClientBoundFrameState != nil {
		// finish_configuration is the wire boundary: the server may emit PLAY
		// frames before the client's acknowledgement is observed locally.
		if state == StateConfiguration && c.ConfigurationFinishSent() {
			state = StatePlay
		}
		c.OnClientBoundFrameState(c, state, id, payload)
	}
	c.fanoutViewers(id, payload)
}

// fanoutViewers 把 S2C 帧按各影子腿的策略分流（非阻塞，见 viewer.go）。
func (c *Conn) fanoutViewers(id int32, payload []byte) {
	c.mu.Lock()
	viewers := c.viewers
	c.mu.Unlock()
	if len(viewers) == 0 {
		return
	}
	for _, v := range viewers {
		if v.Closed() {
			continue
		}
		v.deliver(id, payload)
	}
}

// DetachViewer 摘掉一条影子腿（幂等；用于 B 主动断开或会话收尾）。
func (c *Conn) DetachViewer(v *Viewer) {
	if v == nil {
		return
	}
	c.mu.Lock()
	kept := c.viewers[:0]
	for _, x := range c.viewers {
		if x != v {
			kept = append(kept, x)
		}
	}
	c.viewers = kept
	c.mu.Unlock()
	v.Close()
}

// Viewers 返回当前影子腿快照（诊断用）。
func (c *Conn) Viewers() []*Viewer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*Viewer(nil), c.viewers...)
}

// applyPendingClientCompression 在 SetCompression 转发完成后启用 client 腿压缩。
func (c *Conn) applyPendingClientCompression() {
	c.mu.Lock()
	if !c.hasPendingClientComp {
		c.mu.Unlock()
		return
	}
	threshold := c.pendingClientCompression
	c.hasPendingClientComp = false
	c.mu.Unlock()
	c.client.enableCompression(threshold)
}

// SendServerPacket 由处理器注入一个包到上游（如代答 EncryptionResponse）。
func (c *Conn) SendServerPacket(id int32, p PacketWriter) error {
	w := codec.NewWriter()
	if err := p.Write(w); err != nil {
		return err
	}
	return c.server.writePacket(id, w.Bytes())
}

// SetProtocolVersion 由握手处理器写入（加锁）。
func (c *Conn) SetProtocolVersion(v int32) {
	c.mu.Lock()
	c.ProtocolVersion = v
	c.mu.Unlock()
}

// SwitchState 迁移状态并发布 session.stage（供引擎内置处理器调用）。
func (c *Conn) SwitchState(to State) { c.setState(to) }

// AcknowledgeConfigurationFinish records the client ack. Play starts only
// after the server finish packet has been forwarded.
func (c *Conn) AcknowledgeConfigurationFinish() {
	c.mu.Lock()
	if !c.configFinishSent {
		c.configAckPending = true
		c.mu.Unlock()
		return
	}
	c.configAckPending = false
	c.mu.Unlock()
	c.setState(StatePlay)
}

// BeginConfiguration starts a new configuration cycle. It may repeat while a
// PLAY connection is alive during a server transfer/reconfiguration.
func (c *Conn) BeginConfiguration() {
	c.mu.Lock()
	c.configFinishSent = false
	c.configAckPending = false
	c.mu.Unlock()
	c.setState(StateConfiguration)
}

// ConfigurationFinishSent reports whether the current cycle's finish packet
// has crossed the server leg.
func (c *Conn) ConfigurationFinishSent() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.configFinishSent
}

// MarkConfigurationFinishSent releases an ack that arrived before the finish
// packet's server read loop had processed it.
func (c *Conn) MarkConfigurationFinishSent() {
	c.mu.Lock()
	c.configFinishSent = true
	ackPending := c.configAckPending
	c.configAckPending = false
	c.mu.Unlock()
	if ackPending {
		c.setState(StatePlay)
	}
}

// EnableServerEncryption 启用上游腿 AES-CFB8（EncryptionRequest 代答后调用）。
func (c *Conn) EnableServerEncryption(secret []byte) error {
	return c.server.enableEncryption(secret)
}

// EnableServerCompression 立即启用上游腿压缩（上游发 SetCompression 后其帧已带压缩格式）。
func (c *Conn) EnableServerCompression(threshold int32) {
	c.server.enableCompression(threshold)
}

// MarkClientCompressionPending 记录待启用的 client 腿阈值，
// 由 dispatch 在该包成功转发后调用 applyPendingClientCompression 生效。
func (c *Conn) MarkClientCompressionPending(threshold int32) {
	c.mu.Lock()
	c.pendingClientCompression = threshold
	c.hasPendingClientComp = true
	c.mu.Unlock()
}

func (c *Conn) otherLeg(from *leg) *leg {
	if from == c.client {
		return c.server
	}
	return c.client
}
