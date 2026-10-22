package relay

// seam_test.go — 职责: 影子端所需的两个引擎接缝的契约测试。
//   Seam-1 OnClientBoundFrame —— 成功转发后的录制 tap（walkthrough/12 §12.3）；
//   Seam-2 C2SGate           —— registry 匹配之前的上行闸门。
// 夹具与处理器桩见 seam_fixture_test.go；录制器接线测试在 relay/shadow 包内。

import (
	"bytes"
	"testing"
	"time"
)

// TestC2SGateDropsFramesWhenClosed 闸门关闭时，白端的 C2S 帧不得到达上游。
func TestC2SGateDropsFramesWhenClosed(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	gateCalls := 0
	s.c.C2SGate = func(int32) bool { gateCalls++; return false }

	if !s.c.dispatch(s.c.client, 13, []byte("brand")) {
		t.Fatal("吞帧不应终止连接")
	}
	if gateCalls != 1 {
		t.Fatalf("闸门被调用 %d 次，期望 1", gateCalls)
	}
	if got := readWithDeadline(t, s.toServer, 50*time.Millisecond); got != nil {
		t.Fatalf("上游不应收到被吞的帧，实得 % X", got)
	}
}

// TestC2SGateAllowsFramesWhenOpen 闸门打开时行为与现状一致（帧到达上游）。
func TestC2SGateAllowsFramesWhenOpen(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	s.c.C2SGate = func(int32) bool { return true }

	go func() { _ = s.c.dispatch(s.c.client, 13, []byte("brand")) }()
	got := readWithDeadline(t, s.toServer, time.Second)
	if got == nil {
		t.Fatal("闸门打开时帧必须到达上游")
	}
	if !bytes.Contains(got, []byte("brand")) {
		t.Fatalf("上游载荷不含原文：% X", got)
	}
}

// TestC2SGateSkippedForServerBound 闸门只作用于 client 腿：上游来的帧不受影响
// （否则影子端接管时会把世界流一起吞掉）。
func TestC2SGateSkippedForServerBound(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	s.c.C2SGate = func(int32) bool { return false }

	go func() { _ = s.c.dispatch(s.c.server, 5, []byte("world")) }()
	got := readWithDeadline(t, s.toClient, time.Second)
	if got == nil || !bytes.Contains(got, []byte("world")) {
		t.Fatalf("server→client 方向不应受闸门影响，实得 % X", got)
	}
}

// TestClientBoundTapSeesForwardedBytes tap 必须拿到**最终写出**的字节（含改写），
// 否则录制的世界与白端实际所见不一致，重放给影子端会错乱。
func TestClientBoundTapSeesForwardedBytes(t *testing.T) {
	s := newSeamConn(t, registryWith(&rewriteStub{}))
	type tapEvent struct {
		id      int32
		payload []byte
	}
	tapped := make(chan tapEvent, 1)
	s.c.OnClientBoundFrame = func(c *Conn, id int32, payload []byte) {
		tapped <- tapEvent{id: id, payload: append([]byte(nil), payload...)}
	}

	go func() { _ = s.c.dispatch(s.c.server, 5, []byte{0x01, 0x02}) }()
	got := readWithDeadline(t, s.toClient, time.Second)
	if got == nil || !bytes.Contains(got, []byte("REWRITTEN")) {
		t.Fatalf("客户端应收到改写后的字节，实得 % X", got)
	}

	// tap 在写完之后调用，必须等事件而不是读后立即断言（否则竞态）。
	select {
	case ev := <-tapped:
		if ev.id != 5 {
			t.Fatalf("tap 包 id = %d，期望 5", ev.id)
		}
		if !bytes.Contains(ev.payload, []byte("REWRITTEN")) {
			t.Fatalf("tap 应收到改写后的字节，实得 % X", ev.payload)
		}
	case <-time.After(time.Second):
		t.Fatal("tap 未被调用")
	}
}

// TestClientBoundTapSkippedOnCancel 被 cancel 的包没有转发给客户端，因此不得录制
// （录制它会让重放出现白端从未见过的帧）。
func TestClientBoundTapSkippedOnCancel(t *testing.T) {
	s := newSeamConn(t, registryWith(&cancelStub{}))
	tapped := 0
	s.c.OnClientBoundFrame = func(*Conn, int32, []byte) { tapped++ }

	if !s.c.dispatch(s.c.server, 5, []byte{0x01}) {
		t.Fatal("cancel 不应终止连接")
	}
	if tapped != 0 {
		t.Fatalf("被 cancel 的包不应触发 tap，实得 %d 次", tapped)
	}
	if got := readWithDeadline(t, s.toClient, 50*time.Millisecond); got != nil {
		t.Fatalf("被 cancel 的包不应到达客户端，实得 % X", got)
	}
}

// TestSeamsDefaultNilKeepsBehaviour 两个接缝均为 nil 时不得改变现状行为（零开销路径）。
func TestSeamsDefaultNilKeepsBehaviour(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	if s.c.OnClientBoundFrame != nil || s.c.C2SGate != nil {
		t.Fatal("接缝默认必须为 nil（未启用）")
	}
	go func() { _ = s.c.dispatch(s.c.client, 13, []byte("plain")) }()
	if got := readWithDeadline(t, s.toServer, time.Second); got == nil {
		t.Fatal("默认路径必须照常转发")
	}
}
