// s2cevent.go — HeyPixel `heypixel:s2cevent` 通道：全通道审计 + 捕获 + 原版客户端伪装重放。
// 协议常量来源：运行期实证（heypixel-v3 交付 case/evidence/forge_registry.txt）：
//   - C2S 帧 = uvarint(packetId) + payload；>30000 分片 uvarint(0x7FFFFFFF),ver=1,session,total,idx,count,len
//   - payload(c0133 id=9)=UTF8 JSON{sessionId,actionType,slotIndex,key}；c0224/c0340=Map→JSON
//
// wire ID 按协商版本：763→13，766→2（参考系 Play/ServerBound 2）；S2C=24（766 同位）。
// 本文件为唯一允许出现 heypixel:s2cevent 字面量与封包 ID 表的位置（PROJECT-SPEC）。
package heypixel

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

const (
	s2ceventChannel = "heypixel:s2cevent"
	fragMagic       = 0x7FFFFFFF
	fragVersion     = 1
	fragThreshold   = 30000
	fragChunk       = 30000
	maxPacketSize   = 16777216
	auditPreviewLen = 96
)

// C2S 封包 ID 表（c0864 注册代码运行期提取）。
const (
	PktFileUpload   = 1  // c0531
	PktAttack       = 5  // c0418
	PktAttack2      = 6  // c0348
	PktHexClick     = 9  // c0133
	PktMapReport    = 10 // c0224
	PktMapReportAlt = 11 // c0340
)

// Config 伪造/捕获/重放参数（会话层或前端可改写）。
type Config struct {
	Enabled      bool
	OnSync       bool  // play 态首个 s2cevent S2C 帧后注入内置模板
	SessionID    int64 // c0133 sessionId
	ActionType   string
	SlotIndex    int
	Key          string
	ReportFields map[string]any
	CaptureFile  string // 非空：把全部 custom payload 帧（双向）落盘为 JSONL 模板
	ReplayFile   string // 非空：play 态触发时按模板重放真实客户端的上报序列
}

var Forge = Config{
	Enabled:      true,
	OnSync:       true,
	SessionID:    0x1122334455667788,
	ActionType:   "sync",
	SlotIndex:    0,
	Key:          "nexus",
	ReportFields: map[string]any{"status": "ok", "mods": []string{}, "native": "clean"},
}

var (
	syncOnce sync.Map // connID → 已注入标记
	capMu    sync.Mutex
	fragSeq  atomic.Int32
)

// ---------- varint / 帧 ----------

func AppendUvarint(b []byte, n uint32) []byte {
	for {
		x := byte(n & 0x7F)
		n >>= 7
		if n != 0 {
			x |= 0x80
		}
		b = append(b, x)
		if n == 0 {
			return b
		}
	}
}

func ParseUvarint(b []byte, off int) (uint32, int, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		if off >= len(b) {
			return 0, off, codec.ErrTruncated
		}
		c := b[off]
		off++
		v |= uint32(c&0x7F) << (7 * i)
		if c&0x80 == 0 {
			return v, off, nil
		}
	}
	return 0, off, codec.ErrVarIntTooBig
}

func ParseFrame(payload []byte) (int32, []byte, error) {
	id, off, err := ParseUvarint(payload, 0)
	if err != nil {
		return 0, nil, err
	}
	return int32(id), payload[off:], nil
}

// BuildFrames 组帧；>30000 自动分片。
func BuildFrames(packetID int32, payload []byte, session int32) [][]byte {
	frame := AppendUvarint(nil, uint32(packetID))
	frame = append(frame, payload...)
	if len(frame) <= fragThreshold {
		return [][]byte{frame}
	}
	count := (len(frame) + fragChunk - 1) / fragChunk
	out := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		off := i * fragChunk
		ln := len(frame) - off
		if ln > fragChunk {
			ln = fragChunk
		}
		head := AppendUvarint(nil, fragMagic)
		head = AppendUvarint(head, fragVersion)
		head = AppendUvarint(head, uint32(session))
		head = AppendUvarint(head, uint32(len(frame)))
		head = AppendUvarint(head, uint32(i))
		head = AppendUvarint(head, uint32(count))
		head = AppendUvarint(head, uint32(ln))
		out = append(out, append(head, frame[off:off+ln]...))
	}
	return out
}

// ---------- payload 构造 ----------

func PayloadMapReport(fields map[string]any) []byte {
	raw, err := json.Marshal(fields)
	if err != nil {
		raw = []byte("{}")
	}
	return raw
}

func PayloadHexClick(sessionID int64, actionType string, slotIndex int, key string) []byte {
	return PayloadMapReport(map[string]any{
		"sessionId": sessionID, "actionType": actionType,
		"slotIndex": slotIndex, "key": key,
	})
}

// ---------- 注入 ----------

type frameWriter struct{ body []byte }

func (f frameWriter) Write(w *codec.Writer) error { w.Fixed(f.body); return nil }

// playC2SPluginMessageID 按协商版本选 wire ID：763→13，766→2；未实证返回 -1。
func playC2SPluginMessageID(version int32) int32 {
	switch version {
	case 763:
		return 13
	case 766:
		return 2
	default:
		return -1
	}
}

// playS2CPluginMessageID 返回 play 态 S2C custom payload 的 wire ID（逐版本不同）。
// 与 ids.go 分段常量同源：763=0x17（旧值 24 是 damage_event，静默失效根因）；
// 764/765 沿 763 分支（763 表覆盖 1.20~1.20.1），766+=0x18。
func playS2CPluginMessageID(version int32) int32 {
	switch version {
	case 763, 764, 765:
		return idPlayPluginMessageClientbound763
	case 766, 767, 768:
		return idPlayPluginMessageClientbound766
	default:
		return -1
	}
}

// SendReport 经 c.SendServerPacket 注入伪造上报。
func SendReport(c *relay.Conn, packetID int32, payload []byte) error {
	if len(payload) > maxPacketSize {
		return fmt.Errorf("s2cevent payload too large: %d", len(payload))
	}
	wireID := playC2SPluginMessageID(c.ProtocolVersion)
	if wireID < 0 {
		return fmt.Errorf("s2cevent 注入不支持协议版本 %d", c.ProtocolVersion)
	}
	for _, f := range BuildFrames(packetID, payload, fragSeq.Add(1)) {
		if err := c.SendServerPacket(wireID, frameWriter{body: f}); err != nil {
			return err
		}
	}
	return nil
}

// ---------- 捕获 / 重放 ----------

// captureEntry 是模板文件的一行。
type captureEntry struct {
	TS      string `json:"ts"`
	Dir     string `json:"dir"`   // c2s / s2c
	State   string `json:"state"` // play / configuration
	Channel string `json:"channel"`
	WireID  int32  `json:"wireId"`
	Hex     string `json:"hex"` // 完整帧（含 uvarint packetId / 通道头之后仅 payload 时见 dir 语义）
}

func captureFrame(c *relay.Conn, dir, state, channel string, wireID int32, body []byte) {
	if Forge.CaptureFile == "" {
		return
	}
	e := captureEntry{TS: time.Now().Format(time.RFC3339Nano), Dir: dir, State: state,
		Channel: channel, WireID: wireID, Hex: hex.EncodeToString(body)}
	raw, err := json.Marshal(e)
	if err != nil {
		return
	}
	capMu.Lock()
	defer capMu.Unlock()
	f, err := os.OpenFile(Forge.CaptureFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}

// replayConnFrames 按模板重放捕获的 C2S 帧（当前实现：全部 c2s 帧按序爆发重放）。
func replayConnFrames(c *relay.Conn) {
	data, err := os.ReadFile(Forge.ReplayFile)
	if err != nil {
		c.Logger().Warn("heypixel", "重放模板不可读", "connID", c.ID, "err", err.Error())
		return
	}
	wireID := playC2SPluginMessageID(c.ProtocolVersion)
	if wireID < 0 {
		c.Logger().Warn("heypixel", "重放不支持协议版本", "connID", c.ID, "version", c.ProtocolVersion)
		return
	}
	n := 0
	for _, line := range splitLines(data) {
		var e captureEntry
		if err := json.Unmarshal(line, &e); err != nil || e.Dir != "c2s" {
			continue
		}
		body, err := hex.DecodeString(e.Hex)
		if err != nil {
			continue
		}
		if err := c.SendServerPacket(wireID, frameWriter{body: body}); err != nil {
			c.Logger().Warn("heypixel", "重放帧失败", "connID", c.ID, "err", err.Error())
			return
		}
		n++
	}
	c.Logger().Info("heypixel", "模板重放完成", "connID", c.ID, "frames", n)
}

func splitLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				out = append(out, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

// ---------- 处理器 ----------

// s2cSyncHandler 监听 custom payload（play S2C id=24 / config S2C id=1）：审计 + 捕获 + 触发。
type s2cSyncHandler struct {
	relay.RawForward
	channel string
	inner   []byte
}

func (h *s2cSyncHandler) Read(_ *relay.Conn, r *codec.Reader) error {
	ch, err := r.String()
	if err != nil {
		return err
	}
	h.channel = ch
	h.inner = append([]byte(nil), r.Rest()...)
	h.ReadAll(r)
	return nil
}

func (h *s2cSyncHandler) Handle(c *relay.Conn) (bool, error) {
	// 仅代理门控：白端（镜像客户端）原生完成 s2cevent 上报，审计/捕获/伪装
	// 注入都不适用 —— 原样透传（Write 走 RawForward 缓存，字节不变）。
	if c != nil && c.MirrorClient {
		return false, nil
	}
	if (h.channel != s2ceventChannel && isKnownNoise(h.channel)) || c == nil {
		return false, nil
	}
	state := "play"
	if c.State() != relay.StatePlay {
		state = "configuration"
	}
	logFrame(c, "s2c", state, h.channel, h.inner)
	captureFrame(c, "s2c", state, h.channel, playS2CPluginMessageID(c.ProtocolVersion), h.inner)
	if state != "play" {
		return false, nil // Configuration 态只审计/捕获，禁注入
	}
	if h.channel != s2ceventChannel {
		return false, nil
	}
	if Forge.Enabled && Forge.OnSync {
		if _, loaded := syncOnce.LoadOrStore(c.ID, true); !loaded {
			if Forge.ReplayFile != "" {
				replayConnFrames(c)
			} else {
				emitForgeReports(c)
			}
		}
	}
	return false, nil
}

func (h *s2cSyncHandler) Write(w *codec.Writer) error {
	w.String(h.channel)
	h.WriteRaw(w)
	return nil
}

// c2sAuditHandler 审计/捕获客户端 C2S custom payload（play id=13@763、id=2@766；config id=2），原样透传。
type c2sAuditHandler struct {
	relay.RawForward
	channel string
	inner   []byte
}

func (h *c2sAuditHandler) Read(_ *relay.Conn, r *codec.Reader) error {
	ch, err := r.String()
	if err != nil {
		return err
	}
	h.channel = ch
	h.inner = append([]byte(nil), r.Rest()...)
	h.ReadAll(r)
	return nil
}

func (h *c2sAuditHandler) Handle(c *relay.Conn) (bool, error) {
	if (h.channel != s2ceventChannel && isKnownNoise(h.channel)) || c == nil {
		return false, nil
	}
	state := "play"
	if c.State() != relay.StatePlay {
		state = "configuration"
	}
	logFrame(c, "c2s", state, h.channel, h.inner)
	captureFrame(c, "c2s", state, h.channel, playC2SPluginMessageID(c.ProtocolVersion), h.inner)
	return false, nil
}

func (h *c2sAuditHandler) Write(w *codec.Writer) error {
	w.String(h.channel)
	h.WriteRaw(w)
	return nil
}

// emitForgeReports 内置模板上报（无重放文件时的兜底）。
func emitForgeReports(c *relay.Conn) {
	if err := SendReport(c, PktMapReport, PayloadMapReport(Forge.ReportFields)); err != nil {
		c.Logger().Warn("heypixel", "伪造 c0224 上报失败", "connID", c.ID, "err", err.Error())
		return
	}
	if err := SendReport(c, PktHexClick, PayloadHexClick(Forge.SessionID, Forge.ActionType, Forge.SlotIndex, Forge.Key)); err != nil {
		c.Logger().Warn("heypixel", "伪造 c0133 上报失败", "connID", c.ID, "err", err.Error())
		return
	}
	c.Logger().Info("heypixel", "同步触发伪造上报已发送", "connID", c.ID, "packets", "10,9")
}

// isKnownNoise 过滤 vanilla/forge 固有通道（审计噪声），未知通道仍全量审计。
func isKnownNoise(ch string) bool {
	switch ch {
	case "minecraft:brand", "minecraft:register", "minecraft:unregister", "forge:handshake":
		return true
	}
	return false
}

// logFrame 审计日志（截断预览）。
func logFrame(c *relay.Conn, dir, state, channel string, inner []byte) {
	prev := inner
	if len(prev) > auditPreviewLen {
		prev = prev[:auditPreviewLen]
	}
	c.Logger().Info("heypixel", "custom payload 审计", "connID", c.ID, "dir", dir,
		"state", state, "channel", channel, "len", len(inner), "hex", fmt.Sprintf("%x", prev))
}
