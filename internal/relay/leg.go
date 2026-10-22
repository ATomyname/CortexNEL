// leg：单腿读取循环 + 出站写链（compress → framer → cipher，架构文档 §6.2）。
// 每条腿每方向唯一读取者（facts §7.4）。
package relay

import (
	"errors"
	"net"
	"sync"

	"nexus/internal/relay/codec"
	"nexus/internal/relay/stage"
)

type leg struct {
	name string // "client" / "server"
	conn net.Conn
	dir  Direction // 本腿读取方向
	c    *Conn

	writeMu sync.Mutex
	cipher  *stage.CipherPair // server leg 专用；client leg 恒为 nil（DR-3）
	comp    *stage.Compressor // nil = 压缩未启用

	framer stage.Framer
}

// readLoop 是本腿唯一读取者：cipher → framer → decompress → 派发。
func (l *leg) readLoop() {
	buf := make([]byte, 16*1024)
	for {
		n, err := l.conn.Read(buf)
		if n > 0 {
			data := buf[:n]
			if l.cipher != nil {
				l.cipher.TransformRead(data)
			}
			l.framer.Push(data)
			if !l.drainFrames() {
				return
			}
		}
		if err != nil {
			l.c.Logger().Info("relay", "腿读取断开", "leg", l.name, "state", stateName(l.c.State()), "err", err.Error())
			// 读取终止是唯一可靠的断开来源事实：携带腿名与错误，供编排层
			// 区分 session.upstream.lost / session.client.lost（EVENTS.md §1）。
			l.c.closeFromLeg(l.name, err)
			return
		}
		select {
		case <-l.c.Done():
			return
		default:
		}
	}
}

// drainFrames 处理缓冲中的完整帧；返回 false 表示连接应终止。
func (l *leg) drainFrames() bool {
	for {
		body, err := l.framer.Next()
		if errors.Is(err, stage.ErrNeedMore) {
			return true
		}
		if err != nil {
			l.c.Logger().Error("relay", "帧分包错误", "connID", l.c.ID, "dir", dirName(l.dir), "state", stateName(l.c.State()), "err", err.Error())
			l.c.PublishError("protocol.frame.error", map[string]any{
				"connID": l.c.ID, "dir": dirName(l.dir), "err": err.Error(),
			})
			l.c.Close()
			return false
		}
		if !l.handleFrame(body) {
			return false
		}
	}
}

// handleFrame 解压 + 提取包 ID + 派发；返回 false 终止连接。
func (l *leg) handleFrame(body []byte) bool {
	payload := body
	if l.comp != nil {
		p, err := l.comp.Decode(body)
		if err != nil {
			l.c.Logger().Error("relay", "解压缩错误", "connID", l.c.ID, "dir", dirName(l.dir), "state", stateName(l.c.State()), "err", err.Error())
			l.c.PublishError("protocol.compress.error", map[string]any{
				"connID": l.c.ID, "dir": dirName(l.dir), "err": err.Error(),
			})
			l.c.Close()
			return false
		}
		payload = p
	}

	r := codec.NewReader(payload)
	id, err := r.VarInt()
	if err != nil {
		l.c.Logger().Error("relay", "包ID解析错误", "connID", l.c.ID, "dir", dirName(l.dir), "state", stateName(l.c.State()), "err", err.Error())
		l.c.PublishError("protocol.frame.error", map[string]any{
			"connID": l.c.ID, "dir": dirName(l.dir), "err": err.Error(),
		})
		l.c.Close()
		return false
	}

	return l.c.dispatch(l, id, r.Rest())
}

// writePacket 完整出站链：id+载荷 → 压缩帧体 → 长度前缀 → 加密 → 落网。
func (l *leg) writePacket(id int32, payload []byte) error {
	w := codec.NewWriter()
	w.VarInt(id)
	w.Fixed(payload)
	return l.writeFrameBody(w.Bytes())
}

// writeFrameBody 对已含包 ID 的载荷做压缩与加密。
func (l *leg) writeFrameBody(withID []byte) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	body := withID
	if l.comp != nil {
		encoded, err := l.comp.Encode(withID)
		if err != nil {
			return err
		}
		body = encoded
	}
	frame := stage.WriteFrame(body)
	if l.cipher != nil {
		l.cipher.TransformWrite(frame)
	}
	_, err := l.conn.Write(frame)
	return err
}

// enableCompression 启用本腿压缩（发收两方向同规格）。
func (l *leg) enableCompression(threshold int32) {
	l.writeMu.Lock()
	l.comp = &stage.Compressor{Threshold: threshold}
	l.writeMu.Unlock()
}

// enableEncryption 启用本腿加密（server leg 专用）。
func (l *leg) enableEncryption(secret []byte) error {
	pair, err := stage.NewCipherPair(secret)
	if err != nil {
		return err
	}
	l.writeMu.Lock()
	l.cipher = pair
	l.writeMu.Unlock()
	return nil
}

func dirName(d Direction) string {
	if d == ServerBound {
		return "c2s"
	}
	return "s2c"
}
