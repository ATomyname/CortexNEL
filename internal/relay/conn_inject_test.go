package relay

import (
	"bytes"
	"testing"
	"time"
)

// TestQueueClientInjectionDeferredUntilDrain 注入包必须排队、不得抢先写出。
//
// 契约来源：资源包推送要等 JoinGame 先转发到客户端（客户端才在 play 态），
// 所以处理器只能在 Handle 里排队，由 dispatch 在转发成功后统一 drain。
// 这条顺序一旦破坏，注入会在客户端尚未进入 play 态时发出而被丢弃。
func TestQueueClientInjectionDeferredUntilDrain(t *testing.T) {
	c, clientSide := newPipedConn(t)
	payload := []byte("http://127.0.0.1:39217/ecpack.zip")
	c.QueueClientInjection(idResourcePackPushForTest, payload)

	// ① drain 之前：客户端一个字节都不该收到（net.Pipe 无缓冲，写入会阻塞到有人读）。
	_ = clientSide.SetReadDeadline(time.Now().Add(80 * time.Millisecond))
	buf := make([]byte, 64)
	if n, err := clientSide.Read(buf); err == nil {
		t.Fatalf("drain 之前不应写出注入包，实得 %d 字节", n)
	}
	_ = clientSide.SetReadDeadline(time.Time{})

	// ② drain 之后：客户端必须收到该载荷。
	done := make(chan []byte, 1)
	go func() {
		got := make([]byte, 0, 256)
		tmp := make([]byte, 64)
		for i := 0; i < 8; i++ {
			n, err := clientSide.Read(tmp)
			got = append(got, tmp[:n]...)
			if err != nil || bytes.Contains(got, payload) {
				break
			}
		}
		done <- got
	}()
	c.drainClientInjections()

	select {
	case got := <-done:
		if !bytes.Contains(got, payload) {
			t.Fatalf("客户端未收到注入载荷：% X", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时：客户端未收到注入包")
	}
}

// TestDrainWithoutQueueIsNoop 没有排队内容时 drain 不得写出任何字节（非 EC 会话零影响）。
func TestDrainWithoutQueueIsNoop(t *testing.T) {
	c, clientSide := newPipedConn(t)
	c.drainClientInjections()
	_ = clientSide.SetReadDeadline(time.Now().Add(60 * time.Millisecond))
	if n, err := clientSide.Read(make([]byte, 32)); err == nil {
		t.Fatalf("空队列 drain 不应写出字节，实得 %d", n)
	}
}

// idResourcePackPushForTest 用任意包 id 验证队列语义（本测试只关心「有没有按队列写出」）。
const idResourcePackPushForTest int32 = 73
