// replayer.go — 职责: 把 A 的录制段回灌给影子端 B，并无缝切到 LIVE 镜像。
//
// 时序（walkthrough/12 §12.5）：
//
//	B 连影子端口 → fakeserver 完成伪造登录（B 进 play 入口）
//	  → replayer 以"写满 Viewer 出站队列"的方式回灌录制段
//	  → 回灌完成，此时 dispatch 的 fanout 已在向同一条队列供帧 → LIVE 镜像
//
// "无缝切换"的实现口径：**不需要显式切换**。录制快照是某一时刻的一致性切片，
// fanout 是它之后的增量；两者都进同一条 Viewer 出站队列（FIFO），
// 只要在回灌开始前 fanout 已启动、且回灌期间 fanout 的帧排在快照之后，
// B 看到的就是"世界基准 + 增量"的连续流。本类型只负责把快照按序入队；
// 顺序保证由调用方（影子接入流程）在挂 Viewer 之后才调 Replay 来达成。
//
// 首帧必须是锚点（JoinGame/Respawn）：世界由它建立。若快照里找不到锚点
// （录制预算不足把它裁掉了），拒绝回灌 —— 半截世界序列重放出来是
// "有实体没地形"的废状态，宁可让 B 重连。锚点判定以 Recorder 为唯一事实源，
// 不在此重复实现锚点匹配逻辑。
package shadow

import (
	"fmt"
	"sort"
	"time"

	"nexus/internal/relay"
)

// replayTailKeep 是失败日志保留的末帧数量。
const replayTailKeep = 32

// idConfigFinishMarker 是 configuration 态 S2C 的 finish_configuration 包号
// （764+；真源：1.20.5 configuration.toClient "0x03": finish_configuration）。
// 回灌流以此帧为界：灌完它必须等 B 的 acknowledge 再继续 play 段。
const idConfigFinishMarker int32 = 0x03

// Replayer 把录制快照回灌给一条影子腿。
type Replayer struct {
	v *relay.Viewer
	// log 可选：非 nil 时记录回灌诊断（末帧 ID 序列），用于定位
	// 「B 侧解码失败」这类只能从包序列反推的故障。
	log Logger
	// last 是最近发出的包 ID（诊断用，见 replayTailKeep）。
	last []int32
	// block 可选：回灌期的 S2C 屏蔽判定（与 LIVE 阶段的 Viewer 拦截器同源）。
	//
	// 为什么回灌也必须过滤：Viewer.SetS2CInterceptor 只作用于 LIVE 的 fanout，
	// 而 Replay 直接调 WritePacket 绕过了 deliver —— 于是"B 看不懂的包"会在
	// 回灌期原样发出，B 按原版协议解析时报 DecoderException 并断开
	// （2026-09-17 实测：裸 Forge 的 B 端收到白端网易 mod 的私有包即崩）。
	block func(id int32) bool
	// skipped 记录被屏蔽的包 ID 计数（诊断用）。
	skipped map[int32]int
	// lastSkipped 是 emit 内递增的单帧屏蔽计数（结果聚合用）。
	lastSkipped int
	// sent 记录已发送的包 ID 计数（诊断用）：B 崩溃时据此判断
	// "哪些包它收到了"，与 lastIDs（末帧序列）互相印证。
	sent map[int32]int
	// pace 是每帧之间的最小间隔（流控，见 ReplayFrameInterval）。0 = 不限速。
	pace time.Duration
	// onFinishConfig 在灌完 config 段的 finish_configuration 帧时回调：
	// 调用方在此等待 B 的 acknowledge（并消费之），然后才继续灌 play 段。
	// nil = 不等待（测试/纯诊断路径）。
	onFinishConfig func()
}

// SetFinishConfigHandler 设置 finish_configuration 后的等待回调。
func (r *Replayer) SetFinishConfigHandler(f func()) { r.onFinishConfig = f }

// NewReplayer 构造（无日志、不过滤；回灌失败只返回错误）。
func NewReplayer(v *relay.Viewer) *Replayer {
	return &Replayer{v: v, sent: make(map[int32]int), pace: ReplayFrameInterval}
}

// NewReplayerWithLog 构造带诊断日志的回灌器。
func NewReplayerWithLog(v *relay.Viewer, log Logger) *Replayer {
	return &Replayer{v: v, log: log, sent: make(map[int32]int), pace: ReplayFrameInterval}
}

// SetPace 覆盖每帧间隔（0 = 不限速）。测试用它避免等待。
func (r *Replayer) SetPace(d time.Duration) { r.pace = d }

// SetFrameFilter 设置回灌期的 S2C 屏蔽判定（返回 false 的帧不发给影子腿）。
// 应与 LIVE 阶段挂在 Viewer 上的拦截器同源，保证 B 在两个阶段看到同样的包集合。
func (r *Replayer) SetFrameFilter(f func(id int32) bool) {
	r.block = f
	r.skipped = make(map[int32]int)
}

// Replay 按 packetctl handleMirrorClient 的时序回灌：
//
//	configFrames → finish → 等待 B 的 acknowledge → play 快照 → 校验代际
//
// configFrames 是 Recorder.ConfigFrames 的深拷贝（B 的 config 交互数据）；
// playSnap 是 play 缓存快照（JoinGame 起的世界数据）；source 提供锚点校验
// 与代际。返回回灌帧数；中途失败返回已入队数与错误。
func (r *Replayer) Replay(configFrames, playSnap []Frame, source *Recorder) (int, error) {
	// 1. config 段：registry/tags 等 + finish_configuration。
	n := 0
	skipped := 0
	var firstErr error
	for _, f := range configFrames {
		if err := r.emit(f, &n); err != nil {
			return n, err
		}
		skipped += r.lastSkipped
		if f.ID == idConfigFinishMarker && r.onFinishConfig != nil {
			// finish 后必须等 B 的 acknowledge 到达并消费，B 才切 play；
			// 否则下行世界帧与 ack 上行交错，B 按旧态解码即错位。
			r.onFinishConfig()
		}
	}
	// 2. play 段：定位锚点并切片后灌世界数据。
	if source != nil {
		if source.Anchor() < 0 {
			return n, fmt.Errorf("replay: 录制锚点未知（协议版本未识别），拒绝回灌")
		}
		if len(playSnap) == 0 {
			return n, fmt.Errorf("replay: play 快照为空（白端可能尚未进世界）")
		}
		// play 快照必须从锚点（JoinGame）起回灌——锚点之前的帧属于旧代际
		// 残留或非世界数据，灌给刚进 play 的 B 会使其解码错乱（实测报
		// disconnect 解码失败）。无论首帧是不是锚点，一律切片到锚点。
		idx := -1
		for i, f := range playSnap {
			if f.ID == source.Anchor() {
				idx = i
				break
			}
		}
		if idx < 0 {
			return n, fmt.Errorf("replay: play 快照中找不到世界锚点（包 %d），拒绝回灌", source.Anchor())
		}
		if idx > 0 {
			playSnap = playSnap[idx:]
		}
	}
	for _, f := range playSnap {
		if err := r.emit(f, &n); err != nil {
			firstErr = err
			break
		}
	}
	if r.log != nil && firstErr != nil {
		r.log.Warn("shadow", "回灌中断",
			"written", n, "skipped", skipped, "total", len(playSnap),
			"lastIDs", r.tailIDs(), "sentTop", r.topIDs(r.sent), "err", firstErr.Error())
	}
	return n, firstErr
}

// emit 发一帧并维护诊断计数（回灌主循环的公共尾）。
func (r *Replayer) emit(f Frame, n *int) error {
	if r.block != nil && !r.block(f.ID) {
		r.skipped[f.ID]++
		r.lastSkipped++
		return nil
	}
	// 在写入前记录本帧：B 的解码失败发生在收到某帧之后，而 relay 只能在
	// **再写下一帧**时才发现对端已断——"崩溃时最后入队的那一帧"是嫌疑帧。
	r.note(f.ID, len(f.Payload), *n)
	if r.sent != nil {
		r.sent[f.ID]++
	}
	if err := r.v.WritePacketBlocking(f.ID, f.Payload); err != nil {
		return err
	}
	*n++
	// 流控：给客户端留出消费时间，避免其读缓冲堆积导致帧被截断。
	if r.pace > 0 {
		time.Sleep(r.pace)
	}
	return nil
}

// topIDs 返回计数最高的若干包 ID（"id:次数,大小"），供日志诊断。
func (r *Replayer) topIDs(counts map[int32]int) []string {
	if len(counts) == 0 {
		return nil
	}
	type kv struct {
		id int32
		n  int
	}
	all := make([]kv, 0, len(counts))
	for id, n := range counts {
		all = append(all, kv{id, n})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].id < all[j].id
	})
	const top = 16
	if len(all) > top {
		all = all[:top]
	}
	out := make([]string, 0, len(all))
	for _, e := range all {
		out = append(out, fmt.Sprintf("%d:%d", e.id, e.n))
	}
	return out
}

// note 记录一帧的诊断信息（尾部环形保留，避免长序列刷爆内存）。
func (r *Replayer) note(id int32, size int, _ int) {
	if r.log == nil {
		return
	}
	r.last = append(r.last, id)
	if len(r.last) > replayTailKeep {
		r.last = r.last[len(r.last)-replayTailKeep:]
	}
}

// tailIDs 返回最近若干帧的包 ID（供失败日志使用）。
func (r *Replayer) tailIDs() []int32 {
	return append([]int32(nil), r.last...)
}

// ReplayFrameInterval 是回灌每帧之间的最小间隔（流控）。
//
// 为什么需要显式节流：TCP 写阻塞只在**内核发送缓冲满**时发生（通常数百 KB），
// 而上千帧世界数据可以轻易在缓冲填满前全部灌出。客户端 Netty 若来不及消费，
// 其读缓冲里会堆积大量字节；当某一帧在缓冲边界被截断时，解码器立刻报
// DecoderException（客户端表现为"无法连接至服务器"）。
// 实测（2026-09-17）：同一条 B 腿时而灌完 1176 帧、时而 460 帧即断 —— 这种
// "不固定中断点"正是缺流控的特征，而不是某个包解析不了。
//
// 取值取舍：1ms ≈ 1000 帧/秒，回灌 1500 帧约 1.5 秒，用户可接受；
// 同时给客户端留出消费时间。设为 0 可关闭（测试用）。
var ReplayFrameInterval = time.Millisecond
