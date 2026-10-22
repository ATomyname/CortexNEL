// status_test.go — 职责: 状态查询（server list ping）的本地应答契约。
//
// 为什么必须在本机应答：relay 端口对外暴露，任何客户端刷新服务器列表都会发
// status 查询。转发上游会走完整握手语义（上游按 host 判定）→ 上游回 EOF →
// 连接断开 → 编排层把**正在进行的会话**判为 FAULTED。2026-09-17 实测：
// B 客户端刷新列表即打断白端的 LIVE 会话（日志出现 state=STATUS 与 LIVE→TAP 告警）。
package login

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
	"nexus/internal/telemetry"
)

// testLogger 构造落盘到临时目录的日志器。
// 处理器普遍直接调用 c.Logger().Xxx（见 login/configuration.go 等），
// 传 nil 会在 nil 接收者上锁 mutex 而 panic —— 夹具必须给出真实日志器。
func testLogger(t *testing.T) *telemetry.Logger {
	t.Helper()
	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}

// statusRig 装配一条测试连接：客户腿接一端内存管道，上游腿接另一端。
//
// 客户端侧由**独立读取 goroutine** 消费并把帧送进 frames：net.Pipe 是无缓冲
// 同步管道，无人读取时处理器里的写回会阻塞在写调用上（生产由内核 socket
// 缓冲兜住；测试必须显式模拟读取方，否则用例会死锁）。
func statusRig(t *testing.T) (*relay.Conn, <-chan []byte) {
	t.Helper()
	clientPeer, clientLeg := net.Pipe()
	serverPeer, serverLeg := net.Pipe()
	t.Cleanup(func() {
		_ = clientPeer.Close()
		_ = serverPeer.Close()
	})

	c := relay.NewConn("status-test", &relay.Registry{}, telemetry.NewBus(), testLogger(t))
	c.SetLegsForTest(clientLeg, serverLeg)

	frames := make(chan []byte, 4)
	go func() {
		defer close(frames)
		for {
			body, err := readFrameBody(clientPeer)
			if err != nil {
				return
			}
			frames <- body
		}
	}()
	return c, frames
}

// readFrameBody 从管道读一帧，返回不含长度前缀的帧体。
// 测试自带实现：生产包不暴露读帧入口（leg 的方法是包私有的），
// 与其为测试开导出，不如在测试内复刻 varint 长度前缀的读法。
func readFrameBody(conn net.Conn) ([]byte, error) {
	var length int
	var shift uint
	for i := 0; i < 5; i++ {
		var b [1]byte
		if _, err := conn.Read(b[:]); err != nil {
			return nil, err
		}
		length |= int(b[0]&0x7F) << shift
		if b[0]&0x80 == 0 {
			break
		}
		shift += 7
	}
	if length <= 0 {
		return nil, fmt.Errorf("帧长度非法: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// nextFrame 等待下一帧（带超时），超时即失败。
func nextFrame(t *testing.T, frames <-chan []byte) []byte {
	t.Helper()
	select {
	case body, ok := <-frames:
		if !ok {
			t.Fatal("客户端腿已关闭，未收到预期帧")
		}
		return body
	case <-time.After(3 * time.Second):
		t.Fatal("等待应答帧超时")
		return nil
	}
}

// TestStatusRequestAnsweredLocally StatusRequest 必须在本机应答 StatusResponse，
// 且**不得**转发到上游。
func TestStatusRequestAnsweredLocally(t *testing.T) {
	c, frames := statusRig(t)
	c.SwitchState(relay.StateStatus)
	c.SetProtocolVersion(766)

	p := &statusRequest{}
	cancel, err := p.Handle(c)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !cancel {
		t.Fatal("状态查询必须被 cancel（不回上游），否则上游 EOF 会打断会话")
	}

	// 客户端应收到 StatusResponse（字符串 JSON）。
	frame := nextFrame(t, frames)
	r := codec.NewReader(frame)
	id, err := r.VarInt()
	if err != nil {
		t.Fatalf("读包 id: %v", err)
	}
	if id != idStatusResponse {
		t.Fatalf("包 id = %d，期望 %d", id, idStatusResponse)
	}
	body, err := r.String()
	if err != nil {
		t.Fatalf("StatusResponse 载荷应为 JSON 字符串: %v", err)
	}
	if !strings.Contains(body, "CortexNEL") {
		t.Fatalf("应答体应可辨识为 CortexNEL 代理，实得 %q", body)
	}
	if !strings.Contains(body, "766") {
		t.Fatalf("应答体应回显协议版本，实得 %q", body)
	}
}

// TestStatusPingEchoedAndClosed Ping 必须回同值 Pong 并结束该探测连接。
func TestStatusPingEchoedAndClosed(t *testing.T) {
	c, frames := statusRig(t)
	c.SwitchState(relay.StateStatus)
	c.SetProtocolVersion(766)

	const value int64 = 0x0102030405060708
	payload := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	p := &statusPing{}
	if err := p.Read(c, codec.NewReader(payload)); err != nil {
		t.Fatalf("Read: %v", err)
	}
	cancel, err := p.Handle(c)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !cancel {
		t.Fatal("Ping 必须被 cancel（不回上游）")
	}

	frame := nextFrame(t, frames)
	if len(frame) < 9 {
		t.Fatalf("Pong 帧过短：%d 字节", len(frame))
	}
	// VarInt(1) 占 1 字节，其后 8 字节应原样回显。
	var got int64
	for _, b := range frame[1:9] {
		got = got<<8 | int64(b)
	}
	if got != value {
		t.Fatalf("Pong 值 = %#x，期望 %#x", got, value)
	}
}

// TestStatusHandlersRegistered 两个状态处理器必须已注册进引擎注册表 ——
// 漏注册会让 status 帧直通上游，从而打断会话（本次故障形态）。
func TestStatusHandlersRegistered(t *testing.T) {
	rg := &relay.Registry{}
	rg.Register(Packets()...)

	for _, id := range []int32{idStatusRequest, idStatusPing} {
		hits := rg.Match(relay.StateStatus, relay.ServerBound, id, "any", 766)
		if len(hits) == 0 {
			t.Fatalf("状态查询包 %d 未注册：status 帧会直通上游并打断会话", id)
		}
	}
}
