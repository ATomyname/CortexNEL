// 停滞看护的判据：停滞超限必须主动断开，读到字节必须续命，且幂等关闭。
package archive

import (
	"io"
	"testing"
	"time"
)

// TestStallBodyClosesOnStall 停滞超过 limit 必须把阻塞中的 Read 解开：
// 看护协程关闭底层连接，Read 以错误返回 —— 重试逻辑由此接管。
func TestStallBodyClosesOnStall(t *testing.T) {
	// io.Pipe 无写入端活动：Read 永远阻塞，等价于一条死连接。
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	b := newStallBody(pr, 30*time.Millisecond)
	defer b.stopWatch()

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 16)
		_, err := b.Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("停滞超限后 Read 应以错误返回")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("停滞超限后 Read 仍未被解开（看护未生效）")
	}
}

// TestStallBodyKeepsAliveWhileReading 持续有字节就不得误杀：
// 每次读都刷新活动时间，超过 limit 的总时长后连接仍在。
// 写在独立协程 —— io.Pipe 是同步管道，同协程先写后读会自等待成死连接。
func TestStallBodyKeepsAliveWhileReading(t *testing.T) {
	pr, pw := io.Pipe()
	b := newStallBody(pr, 20*time.Millisecond)
	defer func() {
		b.stopWatch()
		_ = b.Close()
		_ = pw.Close()
	}()

	go func() {
		defer pw.Close()
		for i := 0; i < 20; i++ {
			if _, err := pw.Write([]byte("ok")); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond) // 间隔远小于 limit
		}
	}()

	buf := make([]byte, 4)
	for i := 0; i < 20; i++ {
		if _, err := b.Read(buf); err != nil {
			t.Fatalf("持续有字节时连接被误杀: %v", err)
		}
	}
}

// TestStallBodyCloseIsIdempotent 看护触发与调用方收尾可能都执行 Close，
// 底层只允许关一次（double close 会把错误喂给收尾路径）。
type countingBody struct {
	closes int
}

func (c *countingBody) Read([]byte) (int, error) { return 0, io.EOF }
func (c *countingBody) Close() error             { c.closes++; return nil }

func TestStallBodyCloseIsIdempotent(t *testing.T) {
	inner := &countingBody{}
	b := newStallBody(inner, time.Hour)
	defer b.stopWatch()
	_ = b.Close()
	_ = b.Close()
	if inner.closes != 1 {
		t.Fatalf("底层连接被关闭 %d 次，期望恰好 1 次", inner.closes)
	}
}
