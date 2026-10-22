// 下载：把远端分发的字节拉到本地磁盘，带进度、ctx 取消、重试与停滞看护。
//
// 只负责"取回"，不负责"取回的东西对不对"（见 checksum.go）。
//
// 落盘与健壮性规则：
//   - 原子落位：先写 dst 的临时文件，成功才改名 —— 半份文件绝不顶着最终
//     文件名留下（与解压侧 placement.go 同一条铁律）。失败时临时文件被清掉，
//     dst 要么完整要么不存在。
//   - 重试：传输层错误（连接断、读失败）按上限重试，每次从头下载（无法假设
//     服务器支持 Range 续传）；HTTP 状态码错误与 ctx 取消是确定性失败，
//     不重试。
//   - 停滞看护：连续 downloadStallLimit 无字节的连接按已死处理，主动断开并
//     计入重试（见 stallwatch.go）。
package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// copyBufferSize 是下载缓冲（256KB：大包下载时的系统调用与内存折中）。
	copyBufferSize = 256 << 10
	// downloadEmitInterval 是下载进度的最小上报间隔。
	downloadEmitInterval = 300 * time.Millisecond
	// downloadStallLimit 是「连续这么多时长一个字节都没有」判定连接已死。
	downloadStallLimit = 60 * time.Second
	// downloadMaxAttempts 是总尝试次数（首次 + 重试）。重试从头下载，只救
	// 瞬时故障；更大的重试次数对 404 这类确定性失败毫无意义，对 3.4GB 级
	// 大包也只是把失败时间拖长。
	downloadMaxAttempts = 3
)

// httpStatusError 表示服务器返回了非 200 —— 确定性失败，重试没有意义。
type httpStatusError struct {
	url  string
	code int
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("archive: 下载 %s: HTTP %d", e.url, e.code)
}

// Download 把 url 下载到 dst，期间按块回调进度。ctx 取消可中断。
// 重试期间进度会从头再报 —— 调用方的进度条据此归零重来，语义如实。
func Download(ctx context.Context, client *http.Client, url, dst string, progress ProgressFunc) error {
	tmp := dst + partSuffix
	if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= downloadMaxAttempts; attempt++ {
		err := downloadOnce(ctx, client, url, tmp, progress)
		if err == nil {
			return placeFile(tmp, dst)
		}
		_ = os.Remove(tmp)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var statusErr httpStatusError
		if errors.As(err, &statusErr) {
			return err
		}
		lastErr = err
		if attempt == downloadMaxAttempts {
			break
		}
		// 退避后重试；退避期间取消立即生效。
		select {
		case <-time.After(time.Duration(attempt) * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("archive: 下载 %s 重试 %d 次仍失败: %w", url, downloadMaxAttempts, lastErr)
}

// downloadOnce 执行一次完整下载到 dst（临时文件），不做重试。
func downloadOnce(ctx context.Context, client *http.Client, url, dst string, progress ProgressFunc) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	// 停滞看护接管响应体的关闭（幂等），正常收尾停掉看护即可。
	body := newStallBody(resp.Body, downloadStallLimit)
	defer body.stopWatch()
	defer body.Close()
	if resp.StatusCode != http.StatusOK {
		return httpStatusError{url: url, code: resp.StatusCode}
	}

	total := resp.ContentLength
	if progress != nil {
		progress(0, total)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	var done int64
	buf := make([]byte, copyBufferSize)
	lastEmit := time.Now()
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return werr
			}
			done += int64(n)
			if progress != nil && time.Since(lastEmit) >= downloadEmitInterval {
				progress(done, total)
				lastEmit = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return rerr
		}
	}
	if cerr := out.Close(); cerr != nil {
		return cerr
	}
	// 服务器声明了长度但流提前干净结束（代理/断点类故障的常见形态）：
	// 只有与 ContentLength 对账才能发现，EOF 本身不带错误。
	if total >= 0 && done != total {
		return fmt.Errorf("archive: 下载 %s 收到 %d 字节，Content-Length 声明 %d 字节", url, done, total)
	}
	if progress != nil {
		progress(done, total)
	}
	_ = out.Sync()
	// 下载中取消：把 ctx 错误浮上来，调用方据此走取消路径。
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
