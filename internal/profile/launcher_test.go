package profile

import (
	"strings"
	"testing"
)

func TestBuildLaunchUsesEnvTemplate(t *testing.T) {
	env := Env{
		ID:         "e1",
		ClientDir:  t.TempDir(),
		JavaPath:   `C:\Java\bin\java.exe`,
		JvmArgs:    []string{"-Xmx4G", "-DuserId={{userId}}"},
		LaunchArgs: []string{"--port", "{{port}}"},
	}
	spec, err := BuildLaunch(env, LaunchRequest{UserID: 933874987, LocalPort: 25565, MCVersion: "1.20.1"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Exe != env.JavaPath {
		t.Fatalf("Exe = %q，期望 %q", spec.Exe, env.JavaPath)
	}
	want := []string{"-Xmx4G", "-DuserId=933874987", "--port", "25565"}
	if len(spec.Args) != len(want) {
		t.Fatalf("参数数量 = %d，期望 %d：%q", len(spec.Args), len(want), spec.Args)
	}
	for i, a := range want {
		if spec.Args[i] != a {
			t.Fatalf("arg[%d] = %q，期望 %q", i, spec.Args[i], a)
		}
	}
}

// TestBuildLaunchWithEmptyEnv 空环境档案（无 JavaPath / JvmArgs / LaunchArgs）仍能装配出命令。
//
// ClientDir 与 MCVersion 必须显式给出。原因是 BuildMinecraftLaunch 带文件系统副作用
// （准备运行沙盒、恢复被误停用的核心组件、写 options.txt），而 ClientDir 为空时
// 会走 DetectServerClientDir 探测到**本机真实安装**，测试就会改名用户游戏目录里的组件。
// 自动探测本身由 launcher 包的 TestDetectServerClientDir 覆盖（只读）。
func TestBuildLaunchWithEmptyEnv(t *testing.T) {
	spec, err := BuildLaunch(Env{ClientDir: t.TempDir()}, LaunchRequest{
		UserID: 1, LocalPort: 25565, MCVersion: "1.20.1",
	})
	if err != nil {
		t.Fatalf("空环境档案应能自动补齐：%v", err)
	}
	if spec.Exe == "" {
		t.Fatal("期望非空 Exe")
	}
	if spec.WorkDir == "" {
		t.Fatal("期望非空 WorkDir")
	}
	if len(spec.Args) == 0 {
		t.Fatal("期望非空参数序列")
	}
}

func TestBuildLaunchSynthesizesWhenNoTemplate(t *testing.T) {
	env := Env{
		ID:        "e2",
		JavaPath:  `C:\Java\bin\java.exe`,
		ClientDir: t.TempDir(),
	}
	spec, err := BuildLaunch(env, LaunchRequest{RoleName: "PlayerA", UserID: 12345, LocalPort: 25565, MCVersion: "1.20.1"})
	if err != nil {
		t.Fatalf("自动合成失败：%v", err)
	}
	if spec.Exe != env.JavaPath {
		t.Fatalf("Exe = %q，期望 %q", spec.Exe, env.JavaPath)
	}
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "--username PlayerA") {
		t.Fatalf("参数应含角色名：%s", joined)
	}
	if !strings.Contains(joined, "--port 25565") {
		t.Fatalf("参数应含本机 relay 端口：%s", joined)
	}
}
