// Windows 防火墙入站规则：保证「本应用代理端口的入站允许」存在。
//
// 只做规则管理，不做网络逻辑 —— 为什么要这条规则（LAN 共享需要入站）属于
// settings/relay 域的事，这里只负责把「端口 p 允许入站 TCP」这件事落成系统
// 状态，且重复调用不产生重复状态。
package app

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// firewallRulePrefix 是本应用防火墙规则的命名前缀。端口是规则名的一部分：
// 不同端口的规则互不覆盖，幂等判断也只看自己的名字。
const firewallRulePrefix = "CortexNEL-Minecraft-Proxy"

// firewallRuleName 是端口 port 的规则名。
func firewallRuleName(port int) string {
	return fmt.Sprintf("%s-%d", firewallRulePrefix, port)
}

// commandRunner 执行一次 netsh 调用，返回合并输出与错误。
type commandRunner func(args ...string) (string, error)

// runNetsh 是生产用的 netsh 执行器；HideWindow 避免每次配置弹出控制台窗口。
func runNetsh(args ...string) (string, error) {
	cmd := exec.Command("netsh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ensureFirewallRule 幂等地保证「端口 port 的入站允许规则」存在：
// 已存在直接返回，不存在才添加。添加成功后顺带清理旧版固定名规则 ——
// 历史实现每次点击都堆一条同名不同端口的规则，那些规则的端口早已过期。
// 清理失败不构成错误（规则本来就可能不存在）。
func ensureFirewallRule(port int, run commandRunner) error {
	name := firewallRuleName(port)
	// show 对不存在的规则返回非零退出码，正好当存在性判断用。
	if _, err := run("advfirewall", "firewall", "show", "rule", "name="+name); err == nil {
		return nil
	}
	out, err := run("advfirewall", "firewall", "add", "rule",
		"name="+name,
		"dir=in",
		"action=allow",
		"protocol=TCP",
		fmt.Sprintf("localport=%d", port),
	)
	if err != nil {
		return fmt.Errorf("添加防火墙规则 %s 失败: %s (%w)（需要以管理员身份运行）",
			name, strings.TrimSpace(out), err)
	}
	_, _ = run("advfirewall", "firewall", "delete", "rule", "name="+firewallRulePrefix)
	return nil
}
