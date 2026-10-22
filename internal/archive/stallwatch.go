// 停滞看护：识别「连接还在、字节不来」的死连接。
//
// httpx 客户端刻意不设总超时（750MB 级下载会被掐断），Transport 超时只护住
// 拨号与握手 —— 建连之后服务器再也不吐字节的连接会永远挂着，进度条停在
// 半路。这里按「连续 limit 无一个字节」判定连接已死，主动关闭底层连接，
// 让阻塞中的 Read 以错误返回，交给调用方的重试逻辑。
//
// 只判停滞不判慢：CDN 限速再狠也会周期性吐出缓冲，零字节才是死的。
package archive

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// stallBody 包装响应体：每次读到字节刷新活动时间；停滞超限时关闭底层连接。
type stallBody struct {
	io.ReadCloser
	limit    time.Duration
	lastRead atomic.Int64 // 最近一次读到字节的 UnixNano
	closed   sync.Once
	stop     chan struct{} // 关闭即令看护协程退出（只由属主关闭一次）
}

// newStallBody 包装 rc 并启动看护协程；用完必须 stopWatch（或先触发再退出）。
func newStallBody(rc io.ReadCloser, limit time.Duration) *stallBody {
	b := &stallBody{ReadCloser: rc, limit: limit, stop: make(chan struct{})}
	b.lastRead.Store(time.Now().UnixNano())
	go b.watch()
	return b
}

func (b *stallBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.lastRead.Store(time.Now().UnixNano())
	}
	return n, err
}

// Close 幂等：看护协程触发与调用方收尾都会走到这里，底层只关一次。
func (b *stallBody) Close() error {
	var err error
	b.closed.Do(func() { err = b.ReadCloser.Close() })
	return err
}

// stopWatch 让看护协程退出（正常完成或失败收尾时调用）。
func (b *stallBody) stopWatch() {
	close(b.stop)
}

// watch 每秒巡检一次活动时间；停滞超限就关闭底层连接并退出。
func (b *stallBody) watch() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			last := time.Unix(0, b.lastRead.Load())
			if time.Since(last) > b.limit {
				_ = b.Close()
				return
			}
		}
	}
}
