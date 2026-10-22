// 转服跟随：上游 Transfer 指示的记录与消费（会话编排层的策略状态）。
//
// 记录侧挂在 Acceptance.OnTransferRequest —— 上游发来 Transfer 包时记下真实目标；
// 消费侧在 Accept hook 顶部 —— 客户端断开重连本监听口时命中即改指新上游。
// 引擎只递事实（relay.Conn.OnTransferRequest），是否跟随转服是会话策略，故状态
// 归编排层而非引擎（规范 D2：跨域通信走显式小接口）。
package service

import (
	"sync"
	"time"
)

// transferFollowTTL 是转服指示的有效期：客户端收到 Transfer 包后应立即回连
// （实测毫秒级），留 30s 余量覆盖客户端重载世界；过期记录自动作废，
// 避免陈旧目标把后来者连错服务器。
const transferFollowTTL = 30 * time.Second

// transferFollow 承载一次转服指示：真实目标、来源连接与记录时刻。
// 零值即可用；同一会话只保留最新目标。
type transferFollow struct {
	mu     sync.Mutex
	from   string
	target string
	at     time.Time
}

// record 记录一次转服指示（connID 必须与发出指示的连接一致）。
func (f *transferFollow) record(connID, addr string) {
	f.mu.Lock()
	f.from, f.target, f.at = connID, addr, time.Now()
	f.mu.Unlock()
}

// take 取出转服目标：仅当来源连接一致且未超 TTL 时有效，取即清除。
func (f *transferFollow) take(connID string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.target == "" || f.from != connID {
		return "", false
	}
	addr := f.target
	f.target = ""
	if time.Since(f.at) > transferFollowTTL {
		return "", false
	}
	return addr, true
}
