// viewer_blocker_test.go — B 腿 S2C 拦截的接线契约。
//
// 单独成文件的原因：这里测的是**拦截在分流管线里的位置语义**，
// 不是 Viewer 的基本分流（那在 viewer_test.go）也不是过滤器的判定（那在
// shadow 包）。关键不变量：拦截只影响 B 腿；主客户端与录制 tap 看到的
// 字节流必须与拦截前逐字节一致 —— 挂错位置会静默把 A 也废掉。
package relay

import (
	"bytes"
	"net"
	"testing"
	"time"

	"nexus/internal/relay/stage"
)

// TestViewerS2CInterceptorDropsOnlyViewerLeg 拦截只丢影子腿的帧：
// 主客户端照常收到，录制 tap 照常触发。
func TestViewerS2CInterceptorDropsOnlyViewerLeg(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	v, shadowPeer := attachViewer(t, s)

	// 拦截 id=24（模拟 763 play S2C CustomPayload），其余放行。
	v.SetS2CInterceptor(func(id int32) bool { return id != 24 })

	// 录制 tap 与主客户端一侧的观测。
	tapCh := make(chan int32, 8)
	s.c.OnClientBoundFrame = func(c *Conn, id int32, payload []byte) { tapCh <- id }

	go func() {
		_ = s.c.dispatch(s.c.server, 24, []byte("mod-handshake"))
		_ = s.c.dispatch(s.c.server, 7, []byte("normal"))
	}()

	// 主客户端：两帧都必须到。
	if got := readWithDeadline(t, s.toClient, time.Second); got == nil || !bytes.Contains(got, []byte("mod-handshake")) {
		t.Fatalf("主客户端必须收到 id=24（拦截不得影响 A 腿）：% X", got)
	}
	if got := readWithDeadline(t, s.toClient, time.Second); got == nil || !bytes.Contains(got, []byte("normal")) {
		t.Fatalf("主客户端未收到 id=7：% X", got)
	}

	// 录制 tap：两帧都必须记（录制内容必须与主客户端所见一致）。
	for want := 0; want < 2; want++ {
		select {
		case id := <-tapCh:
			_ = id
		case <-time.After(time.Second):
			t.Fatalf("录制 tap 只触发 %d 次，期望 2 次（拦截不得影响录制）", want)
		}
	}

	// 影子腿：只能收到 id=7；id=24 必须被拦截。
	if id, _ := readFramedPacket(t, shadowPeer); id != 7 {
		t.Fatalf("影子腿应只收到 id=7，实际先收到 id=%d（id=24 漏过去了）", id)
	}
	// 之后再无帧（id=24 被丢弃，没有第二帧可读）——用短超时读出失败即正确。
	if _, payload := mustNotReadFrame(t, shadowPeer); payload != nil {
		t.Fatalf("影子腿收到了被拦截之外的额外帧: %q", payload)
	}
}

// TestViewerS2CInterceptorNilIsTransparent 不挂拦截器时行为与接线前完全一致。
func TestViewerS2CInterceptorNilIsTransparent(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	_, shadowPeer := attachViewer(t, s)

	go func() { _ = s.c.dispatch(s.c.server, 24, []byte("payload")) }()

	if got := readWithDeadline(t, s.toClient, time.Second); got == nil || !bytes.Contains(got, []byte("payload")) {
		t.Fatalf("主客户端未收到帧：% X", got)
	}
	if id, payload := readFramedPacket(t, shadowPeer); id != 24 || !bytes.Contains(payload, []byte("payload")) {
		t.Fatalf("影子腿收到 (%d, %q)，期望 (24, payload)", id, payload)
	}
}

// TestViewerS2CInterceptorBeforePolicy 拦截先于策略执行：
// 被拦截的帧不得触发策略（否则重放期缓冲里会混进 mod 握手帧）。
// 注意：挂了策略后帧交给策略缓冲，不再写网络端 —— 这里只断言策略可见的帧集合。
func TestViewerS2CInterceptorBeforePolicy(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	v, _ := attachViewer(t, s)

	v.SetS2CInterceptor(func(id int32) bool { return id != 24 })

	var buffered []int32
	done := make(chan struct{})
	v.SetFramePolicy(func(id int32, payload []byte) {
		buffered = append(buffered, id)
	})

	go func() {
		defer close(done)
		_ = s.c.dispatch(s.c.server, 24, []byte("blocked"))
		_ = s.c.dispatch(s.c.server, 7, []byte("kept"))
	}()
	// net.Pipe 写阻塞直到被读：必须排空主客户端一侧，dispatch 才能走完。
	// 排空本身也是断言：拦截不得影响 A 腿，两帧都必须到达主客户端。
	if got := readWithDeadline(t, s.toClient, time.Second); got == nil || !bytes.Contains(got, []byte("blocked")) {
		t.Fatalf("主客户端必须收到 id=24（拦截只作用于影子腿）：% X", got)
	}
	if got := readWithDeadline(t, s.toClient, time.Second); got == nil || !bytes.Contains(got, []byte("kept")) {
		t.Fatalf("主客户端未收到 id=7：% X", got)
	}
	<-done

	if len(buffered) != 1 || buffered[0] != 7 {
		t.Fatalf("策略只应看到 id=7，实际 %v（被拦截的帧漏进了策略）", buffered)
	}
}

// mustNotReadFrame 期望在超时窗口内**读不到**任何帧；读到即失败。
// 返回 (读到的 id 或 0, 读到的载荷或 nil)，便于失败信息里给出实际内容。
func mustNotReadFrame(t *testing.T, conn net.Conn) (int32, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	var framer stage.Framer
	buf := make([]byte, 4096)
	for {
		if body, err := framer.Next(); err == nil {
			id, payload, serr := splitPacket(body)
			if serr != nil {
				t.Fatalf("拆包: %v", serr)
			}
			return id, payload
		} else if err != stage.ErrNeedMore {
			// 读超时/EOF = 符合预期（没有多余帧）。
			return 0, nil
		}
		n, rerr := conn.Read(buf)
		if n > 0 {
			framer.Push(buf[:n])
		}
		if rerr != nil {
			return 0, nil
		}
	}
}
