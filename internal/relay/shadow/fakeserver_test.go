// fakeserver_test.go — 职责: FakeServer 对 B 的登录段契约（握手分流、状态查询应答、登录成功）。
//
// 为什么单独成文件：FakeServer 是「relay 作为服务器对 B 说话」的唯一实现
// （方向与 internal/relay/packet/login 相反，见 fakeserver.go 文件头）。
// 这里测的是它的**协议应答契约**，不含回放链路（那在 replay_loopback_test.go）。
package shadow

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"nexus/internal/relay/codec"
)

// TestFakeLoginAnswersStatusQuery B 先做 server list ping 时必须**应答**而不是断开。
//
// 这是一次真实故障的护栏（2026-09-17）：官方客户端「添加服务器 → 连接」的流程里，
// 状态查询（nextState=1）先于登录。原先直接断开，B 侧表现为
// java.net.SocketException: Connection reset，永远走不到登录段 ——
// 影子端因此完全不可用。
func TestFakeLoginAnswersStatusQuery(t *testing.T) {
	bSide, fsSide := net.Pipe()
	defer bSide.Close()
	errCh := make(chan error, 1)

	fs := NewFakeServer(FakeServerConfig{Shape: ShapeOfflineB})
	go func() {
		_, err := fs.ServeLogin(fsSide)
		errCh <- err
	}()

	// 1. 握手：nextState=1（状态查询）。
	hw := codec.NewWriter()
	hw.VarInt(763)
	hw.String("127.0.0.1")
	hw.UShort(25580)
	hw.VarInt(1)
	writeShadowFrame(t, bSide, 0, hw.Bytes())

	// 2. StatusRequest(id=0，空载荷)。
	writeShadowFrame(t, bSide, 0, nil)

	// 3. 必须收到 StatusResponse(id=0, JSON string)。
	_ = bSide.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := readOneShadowFrame(bSide, nil)
	if err != nil {
		t.Fatalf("应收到 StatusResponse，实得读取错误: %v", err)
	}
	id, payload, err := splitShadowPacket(resp)
	if err != nil {
		t.Fatalf("StatusResponse 帧格式错误: %v", err)
	}
	if id != idStatusResponse {
		t.Fatalf("StatusResponse 包 id = %d，期望 %d", id, idStatusResponse)
	}
	body, err := codec.NewReader(payload).String()
	if err != nil {
		t.Fatalf("StatusResponse 载荷应为 JSON 字符串: %v", err)
	}
	if !strings.Contains(body, "CortexNEL") {
		t.Fatalf("StatusResponse JSON 应可辨识为影子端，实得 %q", body)
	}
	if !strings.Contains(body, "763") {
		t.Fatalf("StatusResponse 应回显握手协议版本，实得 %q", body)
	}

	// 4. 应答后连接结束，且以哨兵错误标记「这是状态查询、不是失败」。
	select {
	case err := <-errCh:
		if !errors.Is(err, errStatusQueryAnswered) {
			t.Fatalf("应以 errStatusQueryAnswered 结束（不是失败），实得 %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("应答后未结束连接")
	}
}

// TestFakeLoginAnswersPingPong 客户端发 Ping 时必须回同值 Pong。
func TestFakeLoginAnswersPingPong(t *testing.T) {
	bSide, fsSide := net.Pipe()
	defer bSide.Close()

	fs := NewFakeServer(FakeServerConfig{Shape: ShapeOfflineB})
	go func() { _, _ = fs.ServeLogin(fsSide) }()

	hw := codec.NewWriter()
	hw.VarInt(763)
	hw.String("127.0.0.1")
	hw.UShort(25580)
	hw.VarInt(1)
	writeShadowFrame(t, bSide, 0, hw.Bytes())
	writeShadowFrame(t, bSide, 0, nil) // StatusRequest

	_ = bSide.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := readOneShadowFrame(bSide, nil); err != nil {
		t.Fatalf("读 StatusResponse: %v", err)
	}

	// Ping(id=1) 带 int64 载荷，期望 Pong 回同值。
	const pingValue int64 = 0x1122334455667788
	pw := codec.NewWriter()
	pw.Fixed([]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88})
	writeShadowFrame(t, bSide, idPingPong, pw.Bytes())

	_ = bSide.SetReadDeadline(time.Now().Add(3 * time.Second))
	pong, err := readOneShadowFrame(bSide, nil)
	if err != nil {
		t.Fatalf("应收到 Pong: %v", err)
	}
	id, payload, err := splitShadowPacket(pong)
	if err != nil {
		t.Fatalf("Pong 帧格式错误: %v", err)
	}
	if id != idPingPong {
		t.Fatalf("Pong 包 id = %d，期望 %d", id, idPingPong)
	}
	if len(payload) != 8 {
		t.Fatalf("Pong 载荷长度 = %d，期望 8（原样回显 int64）", len(payload))
	}
	var got int64
	for _, b := range payload {
		got = got<<8 | int64(b)
	}
	if got != pingValue {
		t.Fatalf("Pong 值 = %#x，期望 %#x", got, pingValue)
	}
}

// TestFakeLoginSucceedsOnLoginHandshake nextState=2（登录）走正常离线登录段：
// LoginStart → LoginSuccess，且 UUID 为离线命名空间派生值。
func TestFakeLoginSucceedsOnLoginHandshake(t *testing.T) {
	bSide, fsSide := net.Pipe()
	defer bSide.Close()
	type result struct {
		res *FakeLoginResult
		err error
	}
	ch := make(chan result, 1)

	fs := NewFakeServer(FakeServerConfig{Shape: ShapeOfflineB})
	go func() {
		res, err := fs.ServeLogin(fsSide)
		ch <- result{res, err}
	}()

	hw := codec.NewWriter()
	hw.VarInt(763)
	hw.String("127.0.0.1")
	hw.UShort(25580)
	hw.VarInt(2) // 登录
	writeShadowFrame(t, bSide, 0, hw.Bytes())

	lw := codec.NewWriter()
	lw.String("ShadowTester")
	writeShadowFrame(t, bSide, idHSLoginStart, lw.Bytes())

	_ = bSide.SetReadDeadline(time.Now().Add(3 * time.Second))
	frame, err := readOneShadowFrame(bSide, nil)
	if err != nil {
		t.Fatalf("应收到 LoginSuccess: %v", err)
	}
	id, _, err := splitShadowPacket(frame)
	if err != nil {
		t.Fatalf("LoginSuccess 帧格式错误: %v", err)
	}
	if id != idHSLoginSuccess {
		t.Fatalf("包 id = %d，期望 LoginSuccess(%d)", id, idHSLoginSuccess)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("登录段应成功: %v", r.err)
		}
		if r.res.LoginName != "ShadowTester" {
			t.Fatalf("LoginName = %q", r.res.LoginName)
		}
		if r.res.Handshake.NextState != 2 {
			t.Fatalf("NextState = %d，期望 2", r.res.Handshake.NextState)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("登录段未结束")
	}
}

// TestFakeLoginSuccessCarriesEmptyProperties 守卫 LoginSuccess 的 1.19+ 完整格式：
// UUID ‖ name ‖ properties 数组（VarInt 计数）。
//
// 这是一次真实故障的护栏（2026-09-17）：登录成功包漏写 properties 数组，
// 客户端解码器把下一帧字节当计数字段读，报
// Failed to decode packet 'clientbound/minecraft:game_profile' 并断开。
// 之前的探针"真机验证"用的是我们自己写的最小客户端（按自己的格式读），
// 没能发现此缺陷 —— 只有官方客户端的解码器会因缺字段而崩。
func TestFakeLoginSuccessCarriesEmptyProperties(t *testing.T) {
	bSide, fsSide := net.Pipe()
	defer bSide.Close()
	done := make(chan error, 1)

	fs := NewFakeServer(FakeServerConfig{Shape: ShapeOfflineB})
	go func() {
		_, err := fs.ServeLogin(fsSide)
		done <- err
	}()

	// 走到 LoginSuccess：登录握手 + LoginStart。
	hw := codec.NewWriter()
	hw.VarInt(766)
	hw.String("127.0.0.1")
	hw.UShort(25580)
	hw.VarInt(2)
	writeShadowFrame(t, bSide, 0, hw.Bytes())
	lw := codec.NewWriter()
	lw.String("FormatProbe")
	writeShadowFrame(t, bSide, idHSLoginStart, lw.Bytes())

	_ = bSide.SetReadDeadline(time.Now().Add(3 * time.Second))
	frame, err := readOneShadowFrame(bSide, nil)
	if err != nil {
		t.Fatalf("读 LoginSuccess: %v", err)
	}
	id, payload, err := splitShadowPacket(frame)
	if err != nil {
		t.Fatalf("帧格式错误: %v", err)
	}
	if id != idHSLoginSuccess {
		t.Fatalf("包 id = %d，期望 LoginSuccess(%d)", id, idHSLoginSuccess)
	}

	// 逐字段按客户端解码口径断言：UUID(16) ‖ name ‖ VarInt(properties 计数)。
	r := codec.NewReader(payload)
	uuid, err := r.Fixed(16)
	if err != nil {
		t.Fatalf("UUID 段读取失败: %v", err)
	}
	if len(uuid) != 16 {
		t.Fatalf("UUID 段 = %d 字节，期望 16", len(uuid))
	}
	name, err := r.String()
	if err != nil {
		t.Fatalf("读 name: %v", err)
	}
	if name != "FormatProbe" {
		t.Fatalf("name = %q", name)
	}
	count, err := r.VarInt()
	if err != nil {
		t.Fatal("缺 properties 数组计数字段 —— 客户端会在此报 game_profile 解码失败")
	}
	if count != 0 {
		t.Fatalf("properties 计数 = %d，离线会话应为 0", count)
	}
	// 766~767 专有：strictErrorHandling 布尔（768 起移除）。漏写它会让
	// 客户端解码器在包尾再读一个字段——这正是 20:45 版仍失败的实测原因。
	b, err := r.Byte()
	if err != nil {
		t.Fatal("缺 strictErrorHandling 字段（766~767 专有）")
	}
	if b != 0 {
		t.Fatalf("strictErrorHandling = %d，期望 0", b)
	}
	if r.Rest() != nil && len(r.Rest()) != 0 {
		t.Fatalf("包尾有多余字节: % x", r.Rest())
	}
}
