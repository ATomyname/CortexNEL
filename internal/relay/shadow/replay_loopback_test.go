// replay_loopback_test.go — M-S2 登录+重放链路的回环契约测试。
//
// 用最小客户端（与 probe 测试同源的做法）扮 B：
// 假登录（offline 形态）→ 收 LoginSuccess → 断言回灌序列与录制序列逐帧一致
// （重放字节序 == 录制字节序，walkthrough/12 验收第 1 条）→ 增量帧排在录制段之后
// （"回灌与 fanout 同队列即无缝衔接"的口径验证）。
package shadow

import (
	"net"
	"testing"
	"time"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
	"nexus/internal/relay/stage"
	"nexus/internal/telemetry"
)

// newRigConn 造一个双腿接 net.Pipe 的 Conn（生产构造器 + relay 包导出的测试装配口）。
// AttachViewer 只依赖 Conn 的 viewers 列表与 logger，不依赖双腿是否由
// Interceptor 装配；测试装配走 SetLegsForTest（relay 包导出，生产不调用）。
func newRigConn(t *testing.T) (*relay.Conn, net.Conn, net.Conn) {
	t.Helper()
	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	clientPeer, clientLeg := net.Pipe()
	serverPeer, serverLeg := net.Pipe()
	t.Cleanup(func() {
		_ = clientPeer.Close()
		_ = serverPeer.Close()
	})

	c := relay.NewConn("shadow-rig", &relay.Registry{}, nil, log)
	c.SetLegsForTest(clientLeg, serverLeg)
	return c, clientPeer, serverPeer
}

// attachShadowLeg 挂一条影子腿，返回 (viewer, B 一侧的连接)。
func attachShadowLeg(t *testing.T, c *relay.Conn) (*relay.Viewer, net.Conn) {
	t.Helper()
	peer, legEnd := net.Pipe()
	t.Cleanup(func() { _ = peer.Close(); _ = legEnd.Close() })
	v := c.AttachViewer(legEnd, "shadow-test")
	t.Cleanup(v.Close)
	return v, peer
}

// readShadowFrame B 一侧读一帧明文帧，返回 (包 id, 载荷)。
func readShadowFrame(t *testing.T, conn net.Conn, timeout time.Duration) (int32, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	var framer stage.Framer
	buf := make([]byte, 4096)
	for {
		if body, err := framer.Next(); err == nil {
			id, payload, serr := splitShadowPacket(body)
			if serr != nil {
				t.Fatalf("拆包: %v", serr)
			}
			return id, payload
		}
		n, rerr := conn.Read(buf)
		if n > 0 {
			framer.Push(buf[:n])
		}
		if rerr != nil {
			t.Fatalf("影子腿读帧: %v", rerr)
		}
	}
}

// writeShadowFrame B 一侧发一帧明文。
func writeShadowFrame(t *testing.T, conn net.Conn, id int32, payload []byte) {
	t.Helper()
	w := codec.NewWriter()
	w.VarInt(id)
	w.Fixed(payload)
	if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("写超时设置: %v", err)
	}
	if _, err := conn.Write(stage.WriteFrame(w.Bytes())); err != nil {
		t.Fatalf("写帧: %v", err)
	}
}

// seedFrames 录入一段以锚点开头的合成序列，返回期望序列。
func seedFrames(rec *Recorder, anchor int32) []Frame {
	want := []Frame{
		{ID: anchor, Payload: []byte("joint-world")},
		{ID: 0x0F, Payload: []byte("pos-1")},
		{ID: 0x18, Payload: []byte("chunk-1")},
		{ID: 0x0F, Payload: []byte("pos-2")},
	}
	for _, f := range want {
		rec.RecordState(FrameStatePlay, f.ID, f.Payload)
	}
	return want
}

// TestFakeLoginOfflineLoopback 离线形态登录全流程：握手→LoginStart→
// LoginSuccess（UUID version3/IETF、用户名正确），且不发 EncryptionRequest。
func TestFakeLoginOfflineLoopback(t *testing.T) {
	bSide, fsSide := net.Pipe()
	defer bSide.Close()
	done := make(chan *FakeLoginResult, 1)
	errCh := make(chan error, 1)

	fs := NewFakeServer(FakeServerConfig{Shape: ShapeOfflineB, Username: "ShadowB"})
	go func() {
		res, err := fs.ServeLogin(fsSide)
		if err != nil {
			errCh <- err
			return
		}
		done <- res
	}()

	// 握手 + LoginStart 背靠背写进独立 goroutine：
	// net.Pipe 是同步管道，写阻塞到对端读；而 ServeLogin 在"写 LoginSuccess"时
	// 也会阻塞到本测试读。因此写与读必须并发，否则两边互等（此前"登录超时"
	// 的失败信息即源于此 —— 栈转储显示卡在 writeFrame:218 而非读侧）。
	go func() {
		hw := codec.NewWriter()
		hw.VarInt(763)
		hw.String("127.0.0.1")
		hw.UShort(25565)
		hw.VarInt(2)
		writeShadowFrame(t, bSide, 0, hw.Bytes())

		lw := codec.NewWriter()
		lw.String("BClient")
		writeShadowFrame(t, bSide, 0, lw.Bytes())
	}()

	// 离线形态不压缩不加密：S2C 只有 LoginSuccess(id=2)。先读再等结果，
	// 解除 writeFrame 的管道阻塞。
	id, payload := readShadowFrame(t, bSide, 5*time.Second)
	if id != 2 {
		t.Fatalf("期望 LoginSuccess(id=2)，实得 id=%d", id)
	}
	if len(payload) < 16 {
		t.Fatalf("LoginSuccess 载荷过短: %d", len(payload))
	}
	if v := payload[6] >> 4; v != 3 {
		t.Errorf("UUID version = %d，期望 3", v)
	}
	if variant := payload[8] >> 6; variant != 2 {
		t.Errorf("UUID variant = %d，期望 2", variant)
	}
	name, err := codec.NewReader(payload[16:]).String()
	if err != nil || name != "ShadowB" {
		t.Errorf("LoginSuccess 用户名 = %q, err=%v", name, err)
	}

	// LoginSuccess 已消费 → ServeLogin 的写已解除阻塞，现在等结果。
	select {
	case res := <-done:
		if res.LoginName != "BClient" {
			t.Errorf("LoginName = %q", res.LoginName)
		}
		if res.Effective != "ShadowB" {
			t.Errorf("Effective = %q，期望 ShadowB", res.Effective)
		}
		if res.Handshake.ProtocolVersion != 763 {
			t.Errorf("ProtocolVersion = %d", res.Handshake.ProtocolVersion)
		}
		if res.Handshake.NextState != 2 {
			t.Errorf("NextState = %d，期望 2", res.Handshake.NextState)
		}
	case err := <-errCh:
		t.Fatalf("登录失败: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("登录超时")
	}
}

// TestReplayByteOrderPreserved 重放字节序 == 录制字节序（walkthrough/12 验收第 1 条）。
func TestReplayByteOrderPreserved(t *testing.T) {
	anchor := int32(0x28)
	rec := NewRecorder(1<<20, anchor)
	// config 首帧自动落位边界；want 首元素与其对应。
	rec.RecordState(FrameStateConfiguration, 7, []byte("registry-data"))

	// play 段：锚点起的世界帧（seedFrames 保持原语义）。
	playFrames := seedFrames(rec, anchor)
	want := append([]Frame{
		{ID: 7, Payload: []byte("registry-data")},
	}, playFrames...)

	c, _, _ := newRigConn(t)
	v, bPeer := attachShadowLeg(t, c)

	rep := NewReplayer(v)
	n, err := rep.Replay(rec.ConfigFrames(), rec.Snapshot(), rec)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if n != len(want) {
		t.Fatalf("入队 %d 帧，期望 %d", n, len(want))
	}
	for i, f := range want {
		id, payload := readShadowFrame(t, bPeer, 2*time.Second)
		if id != f.ID || string(payload) != string(f.Payload) {
			t.Fatalf("第 %d 帧不匹配: 实得 (%d, %q) 期望 (%d, %q)",
				i, id, payload, f.ID, f.Payload)
		}
	}
}

// TestReplayRejectsNonAnchorSnapshot 快照不以锚点开头时拒绝回灌
// （半截世界重放 = "有实体没地形"，宁可 B 重连）。
func TestReplayRejectsNonAnchorSnapshot(t *testing.T) {
	anchor := int32(0x28)
	rec := NewRecorder(64, anchor) // 小预算强制裁剪掉锚点
	for i := 0; i < 20; i++ {
		rec.RecordState(FrameStatePlay, 0x0F, []byte("position-frame-without-anchor-000"))
	}
	if rec.StartsAtAnchor() {
		t.Fatal("前置失效：此场景应已丢失锚点")
	}

	c, _, _ := newRigConn(t)
	v, _ := attachShadowLeg(t, c)
	rep := NewReplayer(v)
	if _, err := rep.Replay(rec.ConfigFrames(), rec.Snapshot(), rec); err == nil {
		t.Fatal("半截世界应被拒绝回灌")
	}
}

// TestReplayEmptySnapshotRejected 空 play 快照（白端还没进世界）必须拒绝。
// 生产路径 source 非 nil：锚点未知或 play 缓存空都在拒绝之列。
func TestReplayEmptySnapshotRejected(t *testing.T) {
	c, _, _ := newRigConn(t)
	v, _ := attachShadowLeg(t, c)
	rep := NewReplayer(v)
	if _, err := rep.Replay(nil, nil, NewRecorder(1<<20, 0x2b)); err == nil {
		t.Fatal("空 play 快照应被拒绝")
	}
}

// TestReplayFollowedByFanout 无缝衔接：回灌完成后 fanout 的增量帧必须排在
// 录制段之后到达（同一条 FIFO 出站队列 —— "无需显式切换"的口径验证）。
func TestReplayFollowedByFanout(t *testing.T) {
	anchor := int32(0x28)
	rec := NewRecorder(1<<20, anchor)
	rec.RecordState(FrameStateConfiguration, 7, []byte("registry-data")) // config 首帧自动落位边界

	want := append([]Frame{{ID: 7, Payload: []byte("registry-data")}}, seedFrames(rec, anchor)...)

	c, _, _ := newRigConn(t)
	v, bPeer := attachShadowLeg(t, c)

	// 先回灌。
	rep := NewReplayer(v)
	if _, err := rep.Replay(rec.ConfigFrames(), rec.Snapshot(), rec); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	// 再 fanout 一帧增量（模拟 dispatch 的后续分流）。
	injID := int32(0x19)
	injPayload := []byte("live-increment")
	if err := v.WritePacket(injID, injPayload); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}

	// B 一侧按序收：录制段全部 → 增量。
	for i, f := range want {
		id, payload := readShadowFrame(t, bPeer, 2*time.Second)
		if id != f.ID || string(payload) != string(f.Payload) {
			t.Fatalf("录制段第 %d 帧不匹配: (%d, %q) vs (%d, %q)", i, id, payload, f.ID, f.Payload)
		}
	}
	id, payload := readShadowFrame(t, bPeer, 2*time.Second)
	if id != injID || string(payload) != string(injPayload) {
		t.Fatalf("增量帧不匹配: (%d, %q) vs (%d, %q)", id, payload, injID, injPayload)
	}
}

// TestReplayStartsAtLoginBoundary 回灌起点是**登录边界（config 段起点）**，
// 边界之前的登录段帧（LoginStart 应答等）被丢弃；config 段与其后的 play 段
// 全部保留。
//
// 真机定案（2026-09-17）：B 解码 LoginSuccess 成功后报
// "Received unknown packet id 43"——43=JoinGame 是 play 态包，B 还在
// configuration 态不认识它。764+ 必须先收 config 段（registry_data 等，
// 服务器按协议下发、与客户端无关）并完成 finish/acknowledge 才能进 play。
func TestReplayStartsAtAnchorDroppingPrefix(t *testing.T) {
	anchor := int32(0x2b) // 766 的 JoinGame
	rec := NewRecorder(1<<20, anchor)

	// 登录段帧（Record 不分桶，回灌不用——LoginSuccess 由 fakeserver 伪造）。
	rec.Record(0x03, []byte("pre-login-tail"))
	// config 段（真实录制数据，发给 B）：ConfigFrames 只含 config 态帧。
	rec.RecordState(FrameStateConfiguration, 0x07, []byte("registry-data"))
	rec.RecordState(FrameStateConfiguration, 0x03, []byte("finish-config"))
	// 世界锚点与其后的增量（play 缓存）。
	rec.RecordState(FrameStatePlay, anchor, []byte("joingame"))
	rec.RecordState(FrameStatePlay, 0x3f, []byte("chunk-data"))

	// config 帧不进 play 缓存；play 缓存首帧必是锚点。
	if got := len(rec.ConfigFrames()); got != 2 {
		t.Fatalf("ConfigFrames 数 = %d，期望 2（registry + finish）", got)
	}
	if snap := rec.Snapshot(); len(snap) != 2 || snap[0].ID != anchor {
		t.Fatalf("play 快照应只含锚点起的 2 帧，实得 %+v", snap)
	}

	c, _, _ := newRigConn(t)
	v, bPeer := attachShadowLeg(t, c)

	rep := NewReplayer(v)
	n, err := rep.Replay(rec.ConfigFrames(), rec.Snapshot(), rec)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	// config 2 帧 + play 2 帧 = 4 帧；登录段帧不出现。
	if n != 4 {
		t.Fatalf("应回灌 config+play 共 4 帧，实得 %d", n)
	}

	// B 收到的首帧必须是 config 段（registry_data），绝不是 play 锚点。
	id, payload := readShadowFrame(t, bPeer, 2*time.Second)
	if id != 0x07 {
		t.Fatalf("B 收到的首帧 id = %d，期望 config 段 0x07（play 锚点在 config 完成前不可见）", id)
	}
	if string(payload) != "registry-data" {
		t.Fatalf("首帧载荷 = %q", payload)
	}
	// 顺序：config → 锚点 → 增量。
	id, payload = readShadowFrame(t, bPeer, 2*time.Second)
	if id != 0x03 || string(payload) != "finish-config" {
		t.Fatalf("第二帧应为 finish: (%d, %q)", id, payload)
	}
	id, payload = readShadowFrame(t, bPeer, 2*time.Second)
	if id != anchor || string(payload) != "joingame" {
		t.Fatalf("第三帧应为世界锚点: (%d, %q)", id, payload)
	}
}

// TestReplayRejectsSnapshotWithoutAnchor 快照里找不到锚点时必须拒绝，
// 而不是回灌半截世界（"有实体没地形"）。
func TestReplayRejectsSnapshotWithoutAnchor(t *testing.T) {
	anchor := int32(0x2b)
	rec := NewRecorder(1<<20, anchor)
	rec.RecordState(FrameStateConfiguration, 7, []byte("registry-data")) // config 首帧自动落位边界

	rec.RecordState(FrameStatePlay, 1, []byte("a"))
	rec.RecordState(FrameStatePlay, 2, []byte("b"))

	c, _, _ := newRigConn(t)
	v, _ := attachShadowLeg(t, c)

	rep := NewReplayer(v)
	if _, err := rep.Replay(rec.ConfigFrames(), rec.Snapshot(), rec); err == nil {
		t.Fatal("锚点缺失时必须拒绝回灌")
	}
}

// TestReplayRejectsUnknownAnchor 锚点未知（协议版本未识别，anchor=-1）时
// 必须拒绝 —— 这正是 2026-09-17 故障形态，不得静默退化成"随便灌"。
func TestReplayRejectsUnknownAnchor(t *testing.T) {
	rec := NewRecorder(1<<20, -1)
	rec.RecordState(FrameStatePlay, 0x2b, []byte("joingame"))

	c, _, _ := newRigConn(t)
	v, _ := attachShadowLeg(t, c)

	rep := NewReplayer(v)
	if _, err := rep.Replay(rec.ConfigFrames(), rec.Snapshot(), rec); err == nil {
		t.Fatal("锚点未知时必须拒绝回灌（否则会灌出无法重建世界的序列）")
	}
}

// TestReplayAppliesFrameFilter 回灌必须应用 S2C 屏蔽 —— 与 LIVE 阶段同源的过滤。
//
// 这是一次真实故障的护栏（2026-09-17）：Viewer.SetS2CInterceptor 只作用于 LIVE 的
// fanout，而 Replay 直接调 WritePacket 绕过了 deliver。于是"B 看不懂的包"在回灌期
// 原样发出，B（裸 Forge、无网易 mod）按原版协议解析时报
// DecoderException: Failed to decode packet 'clientbound/minecraft:game_profile' 并断开。
func TestReplayAppliesFrameFilter(t *testing.T) {
	anchor := int32(0x2b)
	rec := NewRecorder(1<<20, anchor)
	rec.RecordState(FrameStateConfiguration, 7, []byte("registry-data")) // config 首帧自动落位边界

	rec.RecordState(FrameStatePlay, anchor, []byte("joingame"))
	rec.RecordState(FrameStatePlay, 0x27, []byte("chunk-ok"))
	rec.RecordState(FrameStatePlay, 0x3e, []byte("blocked-by-filter"))
	rec.RecordState(FrameStatePlay, 0x27, []byte("chunk-ok-2"))

	c, _, _ := newRigConn(t)
	v, bPeer := attachShadowLeg(t, c)

	rep := NewReplayer(v)
	// 模拟"B 看不懂的包"：屏蔽 0x3e。
	rep.SetFrameFilter(func(id int32) bool { return id != 0x3e })

	n, err := rep.Replay(rec.ConfigFrames(), rec.Snapshot(), rec)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if n != 4 {
		t.Fatalf("应写入 4 帧（含 config 首帧，屏蔽 1 帧），实得 %d", n)
	}

	// B 侧收到的序列里不得出现被屏蔽的包。
	for i := 0; i < 4; i++ {
		id, _ := readShadowFrame(t, bPeer, 2*time.Second)
		if id == 0x3e {
			t.Fatal("被屏蔽的包不得发给影子腿：B 会因解码失败而断开")
		}
	}
}

// TestReplayWithoutFilterPassesEverything 未设过滤器时全量回灌（默认行为不变）。
func TestReplayWithoutFilterPassesEverything(t *testing.T) {
	anchor := int32(0x2b)
	rec := NewRecorder(1<<20, anchor)
	rec.RecordState(FrameStateConfiguration, 7, []byte("registry-data")) // config 首帧自动落位边界

	rec.RecordState(FrameStatePlay, anchor, []byte("joingame"))
	rec.RecordState(FrameStatePlay, 0x3e, []byte("kept"))

	c, _, _ := newRigConn(t)
	v, _ := attachShadowLeg(t, c)

	rep := NewReplayer(v)
	n, err := rep.Replay(rec.ConfigFrames(), rec.Snapshot(), rec)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if n != 3 {
		t.Fatalf("未设过滤器时应全量回灌（含 config 首帧），实得 %d", n)
	}
}
