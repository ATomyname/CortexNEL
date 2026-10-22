// Package telemetry 是事件总线与结构化日志（架构文档 §11）。
// 事件总线是跨包可见状态迁移的唯一记录面（规范 I2）：{ts, subject, result, fields}。
package telemetry

import (
	"sync"
	"time"
)

// Event 是总线上的一次状态迁移留痕。红线（规范 S3）：Fields 禁止携带 token、
// 密码、共享密钥等敏感值；账号标识一律脱敏后输出。
type Event struct {
	TS     time.Time
	Name   string // <主体>.<动作>，如 session.stage（清单见 docs/design/EVENTS.md）
	Result string // ok / fail / 分类码
	Fields map[string]any
}

// RingHistory 容量：与 UI 回放上限一致（架构文档 §11）。
const ringCapacity = 5000

// CancelFunc 取消一个订阅。
type CancelFunc func()

// Bus 是进程内事件总线：订阅按名称前缀匹配，历史进环形缓冲。
type Bus struct {
	mu      sync.Mutex
	ring    []Event
	subs    []subscription
	nextSub int
}

type subscription struct {
	id     int
	prefix string
	fn     func(Event)
}

// NewBus 构造空总线。
func NewBus() *Bus {
	return &Bus{ring: make([]Event, 0, ringCapacity)}
}

// Subscribe 注册前缀匹配的订阅，返回取消函数。fn 在发布方 goroutine 内同步调用，
// 订阅方必须快速返回（Wails EventsEmit 足够快），不得回灌事件。
func (b *Bus) Subscribe(prefix string, fn func(Event)) CancelFunc {
	b.mu.Lock()
	b.nextSub++
	sub := subscription{id: b.nextSub, prefix: prefix, fn: fn}
	b.subs = append(b.subs, sub)
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.subs {
			if s.id == sub.id {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				return
			}
		}
	}
}

// Publish 发布事件：先落环形历史，再同步通知所有前缀匹配的订阅者。
func (b *Bus) Publish(name, result string, fields map[string]any) {
	e := Event{TS: time.Now(), Name: name, Result: result, Fields: fields}
	b.mu.Lock()
	if len(b.ring) >= ringCapacity {
		b.ring = b.ring[1:]
	}
	b.ring = append(b.ring, e)
	subs := make([]subscription, len(b.subs))
	copy(subs, b.subs)
	b.mu.Unlock()

	for _, s := range subs {
		if matches(s.prefix, name) {
			s.fn(e)
		}
	}
}

// History 返回环形历史的拷贝（UI 回放用）。
func (b *Bus) History() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Event, len(b.ring))
	copy(out, b.ring)
	return out
}

func matches(prefix, name string) bool {
	return prefix == "" || prefix == name ||
		(len(prefix) > 0 && prefix[len(prefix)-1] == '.' && len(name) >= len(prefix) && name[:len(prefix)] == prefix)
}
