// transfer_follow_test.go — 职责: 转服跟随的记录/消费契约与装配（2026-10-05
// 实测故障：EC 高版本转服让客户端绕开代理裸连）。joinRun.transfer 的策略状态
// 与 Accept hook 的改指行为都在本文件守卫。
package service

import (
	"context"
	"testing"
	"time"
)

// TestTransferFollowReroutesUpstream 上游 Transfer 指示记录后，客户端回连时的
// Acceptance 必须改指 Transfer 目标；消费一次即清除，之后的连接回落档案默认上游。
func TestTransferFollowReroutesUpstream(t *testing.T) {
	svc := newEventJoinService(t)
	handle, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("JoinProxyOnly 失败: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(handle.SessionID) })

	svc.mu.Lock()
	run := svc.runs[handle.SessionID]
	svc.mu.Unlock()
	if run == nil || run.interceptor == nil || run.interceptor.Accept == nil {
		t.Fatal("会话启动后 Accept 应已装配")
	}

	const connID = "sess-1"
	run.transfer.record(connID, "game.example.net:25566")
	acc, err := run.interceptor.Accept(nil)
	if err != nil {
		t.Fatalf("转服回连的 Accept 不应被拒: %v", err)
	}
	if acc.UpstreamAddr != "game.example.net:25566" {
		t.Fatalf("转服回连应改指 Transfer 目标，实得 %s", acc.UpstreamAddr)
	}

	// 消费即清除：未记录指示的连接回落档案上游（srv-1 = 127.0.0.1:1）。
	acc, err = run.interceptor.Accept(nil)
	if err != nil {
		t.Fatalf("后续 Accept 不应被拒: %v", err)
	}
	if acc.UpstreamAddr != "127.0.0.1:1" {
		t.Fatalf("无转服指示应使用档案上游 127.0.0.1:1，实得 %s", acc.UpstreamAddr)
	}
}

// TestTransferFollowExpiredKeepsProfileUpstream 过期（>30s）的转服指示作废：
// 不得把陈旧目标灌给后来者。
func TestTransferFollowExpiredKeepsProfileUpstream(t *testing.T) {
	svc := newEventJoinService(t)
	handle, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("JoinProxyOnly 失败: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(handle.SessionID) })

	svc.mu.Lock()
	run := svc.runs[handle.SessionID]
	svc.mu.Unlock()
	run.transfer.record(run.connID, "game.example.net:25566")
	run.transfer.at = time.Now().Add(-transferFollowTTL - time.Second)

	acc, err := run.interceptor.Accept(nil)
	if err != nil {
		t.Fatalf("Accept 不应被拒: %v", err)
	}
	if acc.UpstreamAddr != "127.0.0.1:1" {
		t.Fatalf("过期指示应回落档案上游，实得 %s", acc.UpstreamAddr)
	}
}
