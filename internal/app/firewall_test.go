// 防火墙规则幂等性的判据。
//
// 真实 netsh 需要管理员权限且改系统状态，测试注入桩 runner 只验证调用序列
// 与端口格式 —— 「确实改到了系统防火墙」由人工在有管理员的真机上验证。
package app

import (
	"errors"
	"strings"
	"testing"
)

// netshStub 记录命令并按两类失败模式回放：
// showErr 非空模拟「规则不存在」（netsh 对查不到的规则返回非零退出码），
// addErr 非空模拟添加失败（如未提权）。
type netshStub struct {
	calls   []string
	showErr error
	addErr  error
}

func (s *netshStub) run(args ...string) (string, error) {
	joined := strings.Join(args, " ")
	s.calls = append(s.calls, joined)
	switch {
	case strings.HasPrefix(joined, "advfirewall firewall show"):
		return "", s.showErr
	case strings.HasPrefix(joined, "advfirewall firewall add"):
		return "", s.addErr
	}
	return "", nil
}

// firstCall 返回第一条以 prefix 开头的命令（没有则空串）。
func (s *netshStub) firstCall(prefix string) string {
	for _, c := range s.calls {
		if strings.HasPrefix(c, prefix) {
			return c
		}
	}
	return ""
}

// TestFirewallRuleNameIncludesPort 规则名必须带端口：不同端口的规则互不覆盖。
func TestFirewallRuleNameIncludesPort(t *testing.T) {
	if got := firewallRuleName(25565); got != "CortexNEL-Minecraft-Proxy-25565" {
		t.Fatalf("规则名 = %q", got)
	}
}

// TestEnsureFirewallRuleSkipsWhenExists 规则已存在时不得重复添加。
func TestEnsureFirewallRuleSkipsWhenExists(t *testing.T) {
	s := &netshStub{}
	if err := ensureFirewallRule(25565, s.run); err != nil {
		t.Fatal(err)
	}
	if s.firstCall("advfirewall firewall add") != "" {
		t.Fatalf("规则已存在仍执行了添加: %v", s.calls)
	}
}

// TestEnsureFirewallRuleAddsWhenMissing 规则缺失时按正确端口添加，
// 并清理旧版固定名规则（历史上每次点击堆一条，端口早已过期）。
func TestEnsureFirewallRuleAddsWhenMissing(t *testing.T) {
	s := &netshStub{showErr: errors.New("No rules match the specified criteria.")}
	if err := ensureFirewallRule(25566, s.run); err != nil {
		t.Fatal(err)
	}
	add := s.firstCall("advfirewall firewall add")
	if add == "" {
		t.Fatalf("规则缺失却未添加: %v", s.calls)
	}
	for _, want := range []string{"localport=25566", "dir=in", "action=allow", "protocol=TCP"} {
		if !strings.Contains(add, want) {
			t.Fatalf("添加参数缺 %s: %s", want, add)
		}
	}
	clean := s.firstCall("advfirewall firewall delete")
	if !strings.Contains(clean, "name="+firewallRulePrefix) {
		t.Fatalf("未清理旧版固定名规则: %v", s.calls)
	}
}

// TestEnsureFirewallRuleSurfacesAddFailure 添加失败必须如实上浮 ——
// 前端靠它提示「需要管理员」，吞掉会让用户以为配置成功了。
func TestEnsureFirewallRuleSurfacesAddFailure(t *testing.T) {
	s := &netshStub{
		showErr: errors.New("No rules match the specified criteria."),
		addErr:  errors.New("The requested operation requires elevation."),
	}
	err := ensureFirewallRule(25565, s.run)
	if err == nil {
		t.Fatal("添加失败必须报错")
	}
	if !strings.Contains(err.Error(), "管理员") {
		t.Fatalf("错误应提示需要管理员: %v", err)
	}
}
