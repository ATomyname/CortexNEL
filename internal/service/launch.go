// 镜像客户端进程启动与等待（架构文档 §4 service/launch.go）。
package service

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"

	"nexus/internal/profile"
	"nexus/internal/telemetry"
)

// MirrorLauncher 负责镜像客户端进程生命周期。
type MirrorLauncher struct {
	log *telemetry.Logger
	mu  sync.Mutex
	cmd *exec.Cmd
}

// NewMirrorLauncher 构造。
func NewMirrorLauncher(log *telemetry.Logger) *MirrorLauncher {
	return &MirrorLauncher{log: log}
}

// sanitizeArgvForLog 是 argv 落日志前的脱敏视图：-DToken 取值替换为 ***，
// 其余项原样保留（userProperties/filterkey 均非凭据，见 launcher/userproperties.go）。
func sanitizeArgvForLog(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.HasPrefix(a, "-DToken=") {
			out[i] = "-DToken=***"
			continue
		}
		out[i] = a
	}
	return out
}

// Start 启动镜像客户端进程（不改文件、不注入进程，DR-5）。
// 返回等待结束的通道：进程退出（正常或崩溃）时关闭。
//
// 启动前先终止上一个尚未退出的镜像客户端：l.cmd 是单值字段，直接覆盖会让
// 旧进程失去引用而永远无法回收。旧客户端残留的后果不是"多开一个窗口"——
// 它会持续连本机 relay 端口做 server list ping（nextState=1），把当前会话
// 反复打断成 FAULTED，录制因此永远累积不到世界锚点，影子端接入时会话已死
// （2026-09-17 实测：日志出现 92 次 state=STATUS 与「当前无活动会话连接」）。
func (l *MirrorLauncher) Start(spec profile.LaunchSpec) (<-chan struct{}, error) {
	if prev := l.takeCommand(); prev != nil && prev.Process != nil {
		if l.log != nil {
			l.log.Warn("service", "发现未退出的上一个镜像客户端，先终止它",
				"pid", prev.Process.Pid)
		}
		_ = prev.Process.Kill()
	}
	if l.log != nil {
		// 全量 argv 落日志：崩溃后的命令行二分（如 --server 形状对照）依赖它重放。
		l.log.Info("service", "启动镜像客户端进程",
			"exe", spec.Exe, "workDir", spec.WorkDir,
			"argv", strings.Join(sanitizeArgvForLog(spec.Args), "\u241f"))
	}
	cmd := exec.Command(spec.Exe, spec.Args...)
	cmd.Dir = spec.WorkDir
	// 原生库目录加进 PATH：Windows 解析被加载 DLL 的依赖时只认应用程序目录、
	// 系统目录与 PATH，不认 java.library.path。官方启动器把这些 DLL 放在 JRE bin
	// （即应用目录）里；nexus 的 JRE 由 CDN 部署、不含它们，故改用 PATH
	// （见 launcher.LaunchSpec.NativeLibDirs 的实测说明）。
	cmd.Env = childEnvWithNativeLibDirs(spec.NativeLibDirs)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.cmd = cmd
	l.mu.Unlock()
	done := make(chan struct{})
	go func() {
		err := cmd.Wait()
		l.mu.Lock()
		if l.cmd == cmd {
			l.cmd = nil
		}
		l.mu.Unlock()
		if l.log != nil {
			errOutput := strings.TrimSpace(stderr.String())
			if err != nil {
				l.log.Warn("service", "镜像客户端进程退出", "err", err.Error(), "stderr", errOutput)
			} else {
				l.log.Info("service", "镜像客户端进程正常退出")
			}
		}
		close(done)
	}()
	return done, nil
}

// childEnvWithNativeLibDirs 构造子进程环境：把原生库目录按顺序加到 PATH 最前面。
//
// 返回 nil 表示无需改动（调用方把 nil 交给 cmd.Env 即继承父进程环境）。
func childEnvWithNativeLibDirs(dirs []string) []string {
	if len(dirs) == 0 {
		return nil
	}
	pathValue := os.Getenv("PATH")
	// 逆序拼接，使最终顺序与 dirs 一致（每次都在当前值前面插入）。
	for i := len(dirs) - 1; i >= 0; i-- {
		pathValue = dirs[i] + string(os.PathListSeparator) + pathValue
	}
	return append(os.Environ(), "PATH="+pathValue)
}

// takeCommand 取出并清空当前记录的进程（加锁）。
func (l *MirrorLauncher) takeCommand() *exec.Cmd {
	l.mu.Lock()
	defer l.mu.Unlock()
	cmd := l.cmd
	l.cmd = nil
	return cmd
}

// CurrentPID 返回当前镜像客户端进程的 PID；无活动进程返回 0。
// 供日志与诊断确认「当前会话持有的是哪一个客户端」—— 残留旧客户端
// 曾因无法从日志分辨而拖了很久才定位（2026-09-17）。
func (l *MirrorLauncher) CurrentPID() int {
	l.mu.Lock()
	cmd := l.cmd
	l.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return 0
	}
	return cmd.Process.Pid
}

// Stop terminates the auto-launched mirror client, if any.
func (l *MirrorLauncher) Stop() error {
	cmd := l.takeCommand()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
