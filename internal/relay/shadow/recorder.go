// recorder.go — 职责: client-bound 明文帧的环形录制与锚点裁剪。
// 包职责与依赖方向见 shadow.go。

package shadow

import "sync"

// FrameState 是录制帧所属的协议态（用于回灌分段：config 段与 play 段
// 的包 ID 表不同，B 只能按当前态解码——混态回灌必然错位）。
type FrameState int

const (
	// FrameStateUnknown 表示录制时未标注态（兼容旧调用方/测试）。
	FrameStateUnknown FrameState = iota
	// FrameStateConfiguration 是 configuration 态帧（registry/tags/finish 等）。
	FrameStateConfiguration
	// FrameStatePlay 是 play 态帧（JoinGame 起的世界数据）。
	FrameStatePlay
)

// Frame 是一条已录制的 client-bound 明文帧（解压后的包 id + 载荷）。
type Frame struct {
	ID      int32
	Payload []byte
	// State 是录制时的协议态。B 只能按其当前态的包 ID 表解码：
	// 混态回灌会让它报 unknown packet id（实测 21:55：config 段中
	// 混入 play 帧 47，B 立即断开）。
	State FrameState
}

// HardCapFactor 是硬上限相对软预算的倍数（见 Recorder 的两段式预算语义）。
const HardCapFactor = 4

// Recorder 是 client-bound 帧的环形录制器。
//
// 两段式预算语义：
//  1. **软预算 budget**：超出即裁剪到最近锚点 —— 丢弃锚点之前的部分。
//  2. **硬上限 budget*HardCapFactor**：锚点自身的世界重建序列若超过此值，
//     才退化为 FIFO（丢最旧帧）并置 truncated。
//
// 为什么这样分：重放必须**从锚点开始**才有意义（世界由 JoinGame/Respawn 建立）；
// 在半截序列上做重放会得到「实体在但脚下没地形」的状态。因此宁可容忍有限超出
// （锚点序列完整），也不为了严格守住预算而把锚点自己丢掉。
//
// anchorPacketID < 0 表示调用方未提供锚点（退化为纯 FIFO 并置 truncated）。
//
// 并发：Record 由腿的读取循环调用，Snapshot 由影子端接入流程调用 → 内部加锁。
type Recorder struct {
	mu     sync.Mutex
	frames []Frame
	bytes  int
	budget int
	hard   int
	anchor int32
	// anchorIdx 是 frames 中最近一次锚点的下标；-1 表示当前缓冲里没有锚点。
	anchorIdx int
	truncated bool
	// compression 是录制来源会话协商出的压缩阈值（<0 表示未启用）。
	//
	// 为什么录制器要知道它：录制内容是**已被解压**的帧（Seam-1 挂在转发字节上，
	// 而 client 腿在 EnableCompression 之后按帧解压）—— 回灌给影子腿时必须用
	// **同一个阈值重新编码**，否则 B 会按未压缩格式解析压缩帧，帧边界错乱并报
	// DecoderException（2026-09-17 实测：B 报 game_profile 解码失败、replayed=563 中断）。
	compression int32
	// loginBoundaryIdx 是「config 段起点」帧的下标；-1 = 未知/已被裁掉。
	//
	// 764+ 的 B 在 play 世界数据之前必须先收 config 段（registry_data 等）并
	// 完成 finish/acknowledge 交互——真机证据：B 解码 LoginSuccess 成功后报
	// "Received unknown packet id 43"（43=JoinGame，config 态包表里没有它）。
	// 回灌因此必须从 config 段起点开始，锚点只是 config 段之后的世界重建点。
	loginBoundaryIdx int
	// loginBoundaryPending 是「前一帧为 LoginSuccess 时落位边界」的待定标志。
	loginBoundaryPending bool
	// configFrames 是 config 态帧的独立小缓存（对齐 packetctl：play 缓存与
	// config 交互分离——config 帧数量少且固定，不参与 play 预算裁剪）。
	configFrames []Frame
	// generation 是世界代际（对齐 packetctl 的 generation）：白端每次进 play
	// 即 +1 并清空 play 缓存——旧代际的画面已失真，已接入的 B 必须重连。
	generation uint64
}

// ReplayCompressionUnset 是「来源会话未协商压缩」的哨兵值。
//
// 不能用 0：0 在 MC 协议里是合法阈值（阈值 0 = 全部压缩），
// 与「没有压缩」是相反的含义。
const ReplayCompressionUnset int32 = -1

// NewRecorder 构造录制器。budget <= 0 时用 DefaultBudget。
func NewRecorder(budget int, anchorPacketID int32) *Recorder {
	if budget <= 0 {
		budget = DefaultBudget
	}
	return &Recorder{
		budget:               budget,
		hard:                 budget * HardCapFactor,
		anchor:               anchorPacketID,
		anchorIdx:            -1,
		compression:          ReplayCompressionUnset,
		loginBoundaryIdx:     -1,
		loginBoundaryPending: false,
	}
}

// Record 记录一帧（未标注态，视为 FrameStateUnknown）。
// 载荷会被拷贝：调用方（引擎）复用底层缓冲，不拷贝会让录制内容随后续帧被改写。
func (r *Recorder) Record(id int32, payload []byte) {
	r.RecordState(FrameStateUnknown, id, payload)
}

// RecordState 记录一帧并标注其协议态。可直接适配 relay.Acceptance 的
// OnClientBoundFrameState（Seam-1）：调用方使用转发前的协议态，避免状态
// 处理器已切换后误把 LoginSuccess 归入 configuration。
//
// 分桶语义（对齐 packetctl）：
//   - config 态帧 → configFrames 小缓存（B 的 config 交互数据，不参与裁剪）
//   - play 态帧   → play 主缓存（世界数据，参与锚点裁剪与预算）
//   - 未知态帧    → 丢弃（回灌用不到，录了也是混态风险）
func (r *Recorder) RecordState(state FrameState, id int32, payload []byte) {
	cp := append([]byte(nil), payload...)
	r.mu.Lock()
	defer r.mu.Unlock()
	switch state {
	case FrameStateConfiguration:
		r.configFrames = append(r.configFrames, Frame{ID: id, Payload: cp, State: state})
	case FrameStatePlay:
		r.frames = append(r.frames, Frame{ID: id, Payload: cp, State: state})
		r.bytes += len(cp)
		if r.anchor >= 0 && id == r.anchor {
			r.anchorIdx = len(r.frames) - 1
		}
		r.trimLocked()
	default:
		// 登录段/未知态帧：回灌不需要（LoginSuccess 由 fakeserver 伪造），
		// 不录——避免混态回灌风险。
	}
}

// ConfigFrames 返回 config 段帧的深拷贝（764+ 回灌：B 的 config 交互数据）。
// 数量少（几十帧），独立于 play 缓存维护，裁剪不影响它。
func (r *Recorder) ConfigFrames() []Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Frame, 0, len(r.configFrames))
	for _, f := range r.configFrames {
		out = append(out, Frame{ID: f.ID, Payload: append([]byte(nil), f.Payload...), State: f.State})
	}
	return out
}

// ResetConfiguration starts a fresh configuration cycle for future replays.
func (r *Recorder) ResetConfiguration() {
	r.mu.Lock()
	r.configFrames = r.configFrames[:0]
	r.mu.Unlock()
}

// ResetPlay 对齐 packetctl 的「进 play 即世界重置」：清空 play 缓存并递增
// 代际。调用方据此断开所有已接入的 B（旧画面失真，必须重连）。
// 返回递增后的代际值。
func (r *Recorder) ResetPlay() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = r.frames[:0]
	r.bytes = 0
	r.anchorIdx = -1
	r.truncated = false
	r.generation++
	return r.generation
}

// Generation 返回当前代际（每次世界重置 +1）。
func (r *Recorder) Generation() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.generation
}

// Snapshot 返回录制内容的深拷贝（调用方可安全持有与遍历）。
func (r *Recorder) Snapshot() []Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Frame, len(r.frames))
	for i, f := range r.frames {
		out[i] = Frame{ID: f.ID, Payload: append([]byte(nil), f.Payload...), State: f.State}
	}
	return out
}

// SnapshotFrom 返回 from 下标起的深拷贝（回灌按边界切片用）。
func (r *Recorder) SnapshotFrom(from int) []Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	if from < 0 || from >= len(r.frames) {
		return nil
	}
	out := make([]Frame, 0, len(r.frames)-from)
	for _, f := range r.frames[from:] {
		out = append(out, Frame{ID: f.ID, Payload: append([]byte(nil), f.Payload...), State: f.State})
	}
	return out
}

// Bytes 返回当前占用的载荷字节数（不含结构开销）。
func (r *Recorder) Bytes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bytes
}

// Len 返回当前帧数。
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames)
}

// Truncated 表示是否因预算不足丢掉了锚点**内部**的帧（重放可能不完整）。
func (r *Recorder) Truncated() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.truncated
}

// HasAnchor 表示当前缓冲里含有世界锚点（锚点及其后的序列可用于回灌）。
//
// 语义边界：**不要求锚点是第一帧**。录制自会话建立就开始，登录段与更早的帧
// 都排在锚点之前；回灌器（Replayer.Replay）从锚点处切片，只要锚点还在缓冲里、
// 且其后的序列未被丢弃，世界就能重建。要判断"锚点之后是否完整"看 Truncated()。
func (r *Recorder) HasAnchor() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.anchor >= 0 && r.anchorIdx >= 0
}

// StartsAtAnchor 表示缓冲的第一帧就是锚点（裁剪后恰好从世界重建点开始）。
// 仅供诊断与测试观察裁剪效果；回灌的准入判定请用 HasAnchor。
func (r *Recorder) StartsAtAnchor() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.anchor >= 0 && len(r.frames) > 0 && r.frames[0].ID == r.anchor
}

// SetAnchor 在录制进行中设置/更正锚点包号，并回溯已录帧查找锚点。
//
// 为什么需要运行期设置：录制器在会话建立时构造，而协议版本要等客户端握手
// 才知道（见 join_game.go 的 OnHandshake 挂接）。构造时只能用通配版本
// （VersionAny=-1）查表，必然拿到 -1 —— 那样 anchorIdx 永远为 -1，
// StartsAtAnchor 恒 false，影子端回灌必然被拒（2026-09-17 实测故障）。
//
// 已录帧里若含锚点，本方法据实更新 anchorIdx；若不含（锚点在录制开始前
// 就已过去），anchorIdx 保持 -1，由调用方按快照内容判断是否需要重新录制。
func (r *Recorder) SetAnchor(anchorPacketID int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.anchor = anchorPacketID
	r.anchorIdx = -1
	if anchorPacketID < 0 {
		return
	}
	for i, f := range r.frames {
		if f.ID == anchorPacketID {
			r.anchorIdx = i
			break
		}
	}
}

// Anchor 返回当前锚点包号（未设置/未知版本为 -1）。
func (r *Recorder) Anchor() int32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.anchor
}

// SetCompression 记录来源会话协商出的压缩阈值（<0 = 未启用）。
//
// 由会话编排层在 EnableCompression 协商完成后调用；影子端回灌前读它，
// 用同一阈值构造影子腿，否则 B 会按未压缩格式解析压缩帧而崩溃。
func (r *Recorder) SetCompression(threshold int32) {
	r.mu.Lock()
	r.compression = threshold
	r.mu.Unlock()
}

// Compression 返回来源会话的压缩阈值（<0 = 未启用）。
// 零值语义与 MC 协议一致：0 是合法阈值（全压缩），因此"未设置"用 ReplayCompressionUnset。
func (r *Recorder) Compression() int32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.compression
}

// trimLocked 执行两段式裁剪（语义见 Recorder 文档）。
//
// 裁剪基准是**登录边界**（config 段起点）：764+ 的 config 段是 B 重放的必经
// 数据（真机证据：跳过它则 B 报 unknown packet id 43），比 play 锚点更靠前、
// 更不可丢；保边界必然保住其后的锚点。边界缺失时退回锚点基准。
func (r *Recorder) trimLocked() {
	if r.bytes <= r.budget || len(r.frames) == 0 {
		return
	}
	base := r.loginBoundaryIdx
	if base < 0 {
		base = r.anchorIdx
	}
	// ① 软预算超限：丢基准之前的部分。
	if base > 0 {
		for _, f := range r.frames[:base] {
			r.bytes -= len(f.Payload)
		}
		r.frames = append([]Frame(nil), r.frames[base:]...)
		if r.loginBoundaryIdx >= 0 {
			r.loginBoundaryIdx = 0
		}
		if r.anchorIdx >= 0 {
			r.anchorIdx -= base
			if r.anchorIdx < 0 {
				r.anchorIdx = -1
			}
		}
	}
	// ② 仍在硬上限内：容忍超出（边界→锚点序列不可截断），只标注状态。
	if r.bytes <= r.hard {
		return
	}
	// ③ 超过硬上限：序列本身已不可全保，退化为 FIFO 并标注不完整。
	for r.bytes > r.hard && len(r.frames) > 1 {
		r.bytes -= len(r.frames[0].Payload)
		r.frames = r.frames[1:]
		if r.loginBoundaryIdx >= 0 {
			r.loginBoundaryIdx--
			if r.loginBoundaryIdx < 0 {
				r.loginBoundaryIdx = -1 // 边界被 FIFO 掉了
			}
		}
		if r.anchorIdx >= 0 {
			r.anchorIdx--
		}
		r.truncated = true
	}
	if r.anchorIdx < 0 {
		r.anchorIdx = -1
	}
}
