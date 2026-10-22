// 镜像客户端的启动编排：开控制通道 → 装配参数 → 起进程 → 看住退出。
//
// 职责边界：本文件只决定「按什么顺序把客户端拉起来、失败了怎么退」，
// 控制通道的协议在 internal/clientcontrol，参数装配在 internal/launcher，
// 进程生命周期在 service/launch.go 的 MirrorLauncher。
//
// 为什么控制通道必须先开：端口要写进启动参数，客户端启动后靠它回连并索要进服目标，
// 缺了这一步客户端只会停在主菜单。真契约是 JVM 系统属性 -DlauncherControlPort，
// --userProperties 的 launcherport 一并写入但不是必要条件
// （依据见 docs/facts/NETEASE-PROTOCOL.md §10）。
package service

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strconv"

	"nexus/internal/authsock"
	"nexus/internal/clientcontrol"
	"nexus/internal/launcher"
	"nexus/internal/netease"
	"nexus/internal/profile"
	"nexus/internal/session"
)

// ensureMirrorJava 确认最终用于启动的 Java 大版本与 mcVer 匹配。
//
// 顺序（刻意如此）：
//  1. 盘上已部署的匹配 JRE —— 直接用；
//  2. 档案显式配置的 / 探测到的 Java —— 大版本匹配就用它；
//  3. 都不匹配 -> 从官方 CDN 部署对应 JRE；
//  4. 部署也失败 -> 返回可读错误。
//
// 为什么只在镜像启动前调用：仅代理模式不启动客户端，relay 只是监听端口
// 转发字节，与 Java 大版本无关。曾经无条件检查，导致 1.8.9 这类需要 Java 8
// 的老版本在只装了 Java 21 的机器上连「仅代理」都建不起来。
func (s *JoinService) ensureMirrorJava(ctx context.Context, env *profile.Env, mcVer string) error {
	majors := launcher.RequiredJavaMajors(mcVer)
	jreDir := ""
	if env.ClientDir != "" {
		jreDir = filepath.Join(filepath.Dir(filepath.Dir(env.ClientDir)), "jre")
	}

	// ① 已部署的匹配 JRE 优先：静默命中，不探测、不打告警。
	// 省掉一次 java -version 子进程，也避免每次启动都刷"大版本不匹配"。
	if jreDir != "" {
		if path := launcher.FindDeployedJavaw(majors, jreDir); path != "" {
			env.JavaPath = path
			return nil
		}
	}

	// ② 档案显式配置的 / 探测到的 Java。
	if env.JavaPath == "" {
		if detected, err := launcher.DetectJavaForVersion(mcVer); err == nil && detected != "" {
			env.JavaPath = detected
		}
	}
	if env.JavaPath != "" {
		if err := launcher.CheckJavaMajor(env.JavaPath, mcVer); err == nil {
			return nil
		} else {
			s.log.Info("service", "Java 大版本不匹配，准备部署匹配的 JRE",
				"mcVersion", mcVer, "currentJava", env.JavaPath, "reason", err.Error())
		}
	} else {
		s.log.Info("service", "未探测到可用 Java，准备部署匹配的 JRE", "mcVersion", mcVer)
	}

	// ③ 从官方 CDN 部署匹配大版本的 JRE 分发包（jdk8/jdk17/jdk22 均已实证）。
	if jreDir != "" {
		path, jerr := launcher.EnsureJre(ctx, majors, jreDir,
			func(stage, message string, done, total int64) {
				// 进度接 install.progress：JRE 下载与其他资产同一条进度链路。
				s.events.Publish("install.progress", "ok", map[string]any{
					"stage": stage, "subject": "jre", "done": done, "total": total, "message": message,
				})
				s.log.Info("service", "JRE 部署进度", "stage", stage,
					"done", done, "total", total, "message", message)
			})
		if jerr != nil {
			s.log.Warn("service", "JRE 部署失败", "err", jerr.Error())
			return fmt.Errorf("进服前置检查失败: JRE 部署失败: %w", jerr)
		}
		if verr := launcher.CheckJavaMajor(path, mcVer); verr != nil {
			s.log.Warn("service", "部署出的 JRE 大版本仍不匹配", "path", path, "err", verr.Error())
			return fmt.Errorf("进服前置检查失败: %w", verr)
		}
		s.log.Info("service", "JRE 部署成功，改用部署的 Java", "javaPath", path)
		env.JavaPath = path
		return nil
	}

	// ④ 没有客户端目录（定不了 JRE 落点）且没有匹配的 Java。
	return fmt.Errorf("进服前置检查失败: 未找到匹配 MC %s 的 Java（需要 Java %v），且无法确定 JRE 部署目录",
		mcVer, majors)
}

// loopbackHost 是镜像客户端的连接目标主机。
// relay 可能按用户设置绑到 0.0.0.0 以便局域网共享，但本机镜像客户端始终走环回。
const loopbackHost = "127.0.0.1"

// ensureMirrorSdkRuntime 补齐 SDK 运行库包目录（缺件从 CDN 下载）。
//
// 补不齐不阻断启动：本地可能已有齐全副本，落位阶段会自愈归集（见
// launcher.ResolveSdkRuntimeSource）。但缺项必须留痕（规范 I2）—— 1.8.9 那次就是
// 静默缺了 48 项，日志上只看得见「启动后崩了」。
func (s *JoinService) ensureMirrorSdkRuntime(ctx context.Context, clientDir string) {
	if clientDir == "" {
		return
	}
	report := launcher.FetchSdkRuntimePack(ctx, clientDir)
	if len(report.Failed) == 0 {
		return
	}
	s.log.Warn("service", "SDK 运行库补件有缺", "packDir", report.PackDir,
		"downloaded", report.Downloaded, "failed", report.Failed)
}

// mirrorRequest 是一次镜像启动的会话侧输入（与环境档案正交）。
type mirrorRequest struct {
	env       profile.Env
	accountID string
	roleName  string
	userID    int64
	mcVersion string
	gameID    string
	// relayAddr 是 relay 的实际监听地址，客户端与控制通道都以它的端口为进服目标。
	relayAddr string
	// componentCacheDir 是 nexus 组件缓存（该服按网关清单下载的组件），
	// 作为沙盒播种的最后一路叠加来源；为空表示本次没有下载组件。
	componentCacheDir string
	manager           *session.Manager
	// serverHost/serverPort 指向真实服务器时为「直连官方服」模式：
	// 客户端 --server 与控制通道都推真实地址，AuthLib join 由本地
	// authsock 代答（见 openAuthChannel）。serverHost 为空保持 relay 模式。
	serverHost string
	serverPort int
	// creds 提供 Yggdrasil join 凭据（含静默重登）；nil 表示不开鉴权代答通道。
	creds func() (netease.Credentials, error)
	// modInfo 提供 join 时的组件清单 JSON；可为 nil（此时 join 不带组件清单）。
	modInfo func() string
}

// launchMirror 拉起镜像客户端。
//
// 任一前置步骤失败都不拆掉会话：relay 仍在监听，用户可以手动接入客户端，
// 因此这里只记录原因后返回。只有「进程起失败」才把会话标记为故障。
func (s *JoinService) launchMirror(ctx context.Context, run *joinRun, req mirrorRequest) {
	port, err := relayPort(req.relayAddr)
	if err != nil {
		s.log.Warn("service", "监听地址解析失败，跳过自动启动", "addr", req.relayAddr, "err", err.Error())
		return
	}

	// 直连官方服模式下先开鉴权代答通道：-DlauncherControlPort 必须指向它，
	// 客户端 native authlib 才会把 join 请求交给 nexus 代答
	// （与 Fantnel AuthLibProtocol 同构）。relay 模式下不开，端口回落控制通道。
	authPort := s.openAuthChannel(run, req)

	// 控制通道开不起来时仍然启动客户端：客户端会停在主菜单等手动进服，
	// 比连一个没有监听的端口或干脆不启动更可用。
	controlPort := s.openControlChannel(run, req, port)

	// SDK 运行库包不随本产品分发（下载面见 launcher.FetchSdkRuntimePack），缺件必须
	// 在装配启动参数之前补齐：落位发生在 BuildLaunch 内，之后补等于本次仍是缺件状态，
	// 客户端会在 Library 的 native 调用处抛 UnsatisfiedLinkError。
	s.ensureMirrorSdkRuntime(ctx, req.env.ClientDir)

	spec, err := profile.BuildLaunch(req.env, profile.LaunchRequest{
		RoleName:          req.roleName,
		UserID:            req.userID,
		LocalPort:         port,
		MCVersion:         req.mcVersion,
		GameID:            req.gameID,
		ControlPort:       controlPort,
		ServerHost:        req.serverHost,
		ServerPort:        req.serverPort,
		AuthPort:          authPort,
		ComponentCacheDir: req.componentCacheDir,
		AuthToken:         s.launchToken(req.accountID),
	})
	if err != nil {
		s.log.Info("service", "未能合成客户端启动参数，等待手动接入镜像客户端", "reason", err.Error())
		// 没有客户端会来回连，通道留着只是占端口。
		s.closeAuthChannel(run)
		s.closeControlChannel(run)
		return
	}

	// 运行目录的准备结果必须留痕：沙盒曾静默丢掉基础核心组件，导致客户端连不上
	// 控制通道却看不出原因（基础组件为 0 即为该故障特征）。SDK 运行库缺项同样必须
	// 留痕：缺件会让客户端在 Library 的 native 调用处抛 UnsatisfiedLinkError。
	s.log.Info("service", "客户端运行目录已准备",
		"workDir", spec.WorkDir,
		"baseMods", spec.Runtime.BaseMods,
		"cachedMods", spec.Runtime.CachedMods,
		"restoredCoreMods", spec.Runtime.RestoredCoreMods,
		"sdkSource", spec.SdkRuntime.SourceDir,
		"sdkSeeded", spec.SdkRuntime.Seeded,
		"sdkMissing", spec.SdkRuntime.Missing)

	done, err := s.launcher.Start(spec)
	if err != nil {
		s.log.Warn("service", "镜像客户端启动失败", "err", err.Error())
		_ = req.manager.Transition(session.StageFaulted)
		_ = s.Stop(run.connID)
		return
	}

	go s.watchMirrorExit(ctx, done, run, req.manager)
}

// openControlChannel 开启控制通道并挂到指定会话上，返回实际端口；
// 失败或会话已结束时返回 0，调用方据此不写 launcherport。
func (s *JoinService) openControlChannel(run *joinRun, req mirrorRequest, relayListenPort int) int {
	// 直连官方服模式推真实服务器地址（Fantnel 旁路认证同构）；
	// relay 模式推本机 relay，由链路内代答认证。
	host, port := loopbackHost, relayListenPort
	if req.serverHost != "" && req.serverPort > 0 {
		host, port = req.serverHost, req.serverPort
	}
	listener, err := clientcontrol.Listen(clientcontrol.JoinTarget{
		Host:      host,
		Port:      port,
		RoleName:  req.roleName,
		MCVersion: req.mcVersion,
	}, s.log)
	if err != nil {
		s.log.Warn("service", "控制通道开启失败，客户端将不会自动进服", "err", err.Error())
		return 0
	}

	s.mu.Lock()
	if s.runs[run.connID] == run {
		run.control = listener
	} else {
		// 会话在开通道期间已被停止：立刻收掉，避免留下无人关闭的监听。
		s.mu.Unlock()
		_ = listener.Close()
		return 0
	}
	s.mu.Unlock()

	return listener.Port()
}

// closeControlChannel 关闭并摘掉指定会话的控制通道（幂等）。
func (s *JoinService) closeControlChannel(run *joinRun) {
	s.mu.Lock()
	var listener *clientcontrol.Listener
	if run != nil {
		listener = run.control
		run.control = nil
	}
	s.mu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
}

// openAuthChannel 在直连官方服模式下开启本地 AuthLib 鉴权代答通道并挂到会话上，
// 返回实际端口（写入 -DlauncherControlPort）。非直连模式（serverHost 为空）或
// 凭据缺失时返回 0，调用方据此让 -DlauncherControlPort 回落控制通道端口——
// relay 模式下 join 由链路内代答，native authlib 不活跃，行为不变。
func (s *JoinService) openAuthChannel(run *joinRun, req mirrorRequest) int {
	if req.serverHost == "" || req.serverPort <= 0 || req.creds == nil {
		return 0
	}

	join := func(ctx context.Context, gameID, userID, serverHash string) error {
		// 客户端发来的 gameID 是十进制字符串；Yggdrasil join 需要的是 int64。
		gid, err := strconv.ParseInt(gameID, 10, 64)
		if err != nil {
			return fmt.Errorf("authsock: gameID %q 不是合法十进制数: %w", gameID, err)
		}
		cred, err := req.creds()
		if err != nil {
			return err
		}
		var modInfoJSON string
		if req.modInfo != nil {
			modInfoJSON = req.modInfo()
		}
		return netease.Join(ctx, netease.JoinParams{
			Credentials: cred,
			GameID:      gid,
			ServerID:    serverHash,
			ModInfo:     modInfoJSON,
			MCVersion:   req.mcVersion,
		})
	}

	listener, err := authsock.Listen(join, s.log)
	if err != nil {
		s.log.Warn("service", "鉴权代答通道开启失败，直连模式将无法完成进服认证", "err", err.Error())
		return 0
	}

	s.mu.Lock()
	if s.runs[run.connID] == run {
		run.auth = listener
	} else {
		// 会话在开通道期间已被停止：立刻收掉，避免留下无人关闭的监听。
		s.mu.Unlock()
		_ = listener.Close()
		return 0
	}
	s.mu.Unlock()

	return listener.Port()
}

// closeAuthChannel 关闭并摘掉指定会话的鉴权代答通道（幂等）。
func (s *JoinService) closeAuthChannel(run *joinRun) {
	s.mu.Lock()
	var listener *authsock.Listener
	if run != nil {
		listener = run.auth
		run.auth = nil
	}
	s.mu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
}

// launchToken 取账号 token 供客户端 -DToken 使用；取不到时返回空串（不写该参数）。
//
// 这里不触发静默重登：重登属于进服认证链路（credsSupplier）的职责，
// 启动参数缺 token 只影响客户端自身的网易服务调用，不影响经 relay 的进服。
func (s *JoinService) launchToken(accountID string) string {
	token, err := s.accounts.Token(accountID)
	if err != nil || token == "" {
		s.log.Info("service", "账号 token 不可用，启动参数不写 -DToken")
		return ""
	}
	return token
}

// watchMirrorExit 客户端进程退出后收尾：未到 LIVE 即视为故障。
// 只收尾自己这个会话，其他代理继续跑。
func (s *JoinService) watchMirrorExit(ctx context.Context, done <-chan struct{}, run *joinRun, manager *session.Manager) {
	select {
	case <-done:
		if manager.Stage() != session.StageLive {
			_ = manager.Transition(session.StageFaulted)
			_ = s.Stop(run.connID)
		}
	case <-ctx.Done():
	}
}

// relayPort 从监听地址里取端口。
func relayPort(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}
