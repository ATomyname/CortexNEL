// 上游断线重连：指数退避 1s→8s、3 次停止（架构文档 §7.2）。
// 时序测试必须确定性（规范 T1）：休眠经 Sleeper 注入。
package session

import (
	"context"
	"fmt"
	"time"
)

// MaxReconnectAttempts 是重连次数上限。
const MaxReconnectAttempts = 3

// Sleeper 抽象等待（测试注入 fake clock，T1）。
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// RealSleeper 是真实时间实现。
type RealSleeper struct{}

// Sleep 等待 d 或 ctx 取消。
func (RealSleeper) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ReconnectWithBackoff 以指数退避反复调用 attempt，直到成功、次数用尽或 ctx 取消。
// 退避序列 1s→2s→4s→8s（封顶 8s）。
func ReconnectWithBackoff(ctx context.Context, sleeper Sleeper, attempt func(ctx context.Context) error) error {
	delay := time.Second
	var lastErr error
	for i := 0; i < MaxReconnectAttempts; i++ {
		if err := attempt(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i == MaxReconnectAttempts-1 {
			break
		}
		if err := sleeper.Sleep(ctx, delay); err != nil {
			return fmt.Errorf("session: 重连中止: %w", err)
		}
		delay *= 2
		if delay > 8*time.Second {
			delay = 8 * time.Second
		}
	}
	return fmt.Errorf("session: 重连 %d 次仍失败: %w", MaxReconnectAttempts, lastErr)
}
