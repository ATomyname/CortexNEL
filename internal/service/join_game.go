// 进服一条龙：起本地代理 → 等镜像接入 → 代答 → LIVE。
package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"time"

	"nexus/internal/account"
	"nexus/internal/authsock"
	"nexus/internal/clientcontrol"
	"nexus/internal/netease"
	"nexus/internal/profile"
	"nexus/internal/relay"
	"nexus/internal/relay/packet/heypixel"
	"nexus/internal/relay/packet/login"
	"nexus/internal/relay/shadow"
	"nexus/internal/session"
	"nexus/internal/telemetry"
)

// AssetRequest 是一次进服前置资产的补齐请求。
type AssetRequest struct {
	MCVersion string
	ServerID  string
	IsRental  bool
	AccountID string
}

// AssetResult 是补齐结果：客户端根目录与 nexus 组件缓存目录。
type AssetResult struct {
	// ClientDir 是应使用的客户端根目录（已确保该版本客户端本体可用）。
	// 为空表示沿用调用方探测到的目录。
	ClientDir string
	// ComponentCacheDir 是 nexus 组件缓存（该服组件已落到此目录）；
	// 由 launcher 作为沙盒播种的最后一路叠加来源。为空表示本次没有下载组件。
	ComponentCacheDir string
}

// AssetProvider 在启动镜像客户端前补齐"必须具备的本地资产"：
// 客户端本体与该服的服务器组件；本地没有就下载，而不是退回别的客户端。
//
// 由消费方（service）定义接口、由 app 层实现 —— 装配所需的 paths / 设置 / 账号凭据 /
// 遥测总线都在 app 手里，service 不应反向依赖它们（规范 D2）。
// 为 nil 时表示不做补齐（沿用旧的"本地没有就退回默认目录"行为）。
type AssetProvider interface {
	EnsureJoinAssets(ctx context.Context, req AssetRequest) (AssetResult, error)
}

// ErrNoSession 表示当前无会话。
var ErrNoSession = errors.New("当前无会话")

// JoinService 编排并发脱盒会话（多代理：任意多个仅代理 + 至多一个白端）。
type JoinService struct {
	accounts *account.Manager
	logins   *LoginService
	profiles *profile.Manager
	launcher *MirrorLauncher
	registry *relay.Registry
	events   *telemetry.Bus
	log      *telemetry.Logger
	// assets 补齐进服前置资产（客户端本体与该服组件）；nil 表示不做补齐。
	assets AssetProvider

	mu   sync.Mutex
	runs map[string]*joinRun
	// sessionSeq 是会话 ID 自增序号（sess-1、sess-2…）。不复用服务器 ID：
	// 同一服务器允许起多个代理（见 join_gate.go）。
	sessionSeq int
	// mirrorSession 是白端会话的 ID（空 = 无）。一键进服同时只允许一个。
	mirrorSession string
	allowLAN      bool
	port          int
	// shadowEnabled 是影子端总开关（系统设置，随会话装配生效）：关闭时会话不
	// 装配录制/闸门/屏蔽器组件，Seam-1/Seam-2 不挂接 —— 不用影子端就不录制。
	shadowEnabled bool
}

// NewJoinService 构造（注册表由 main.go 装配后传入）。
func NewJoinService(accounts *account.Manager, profiles *profile.Manager, launcher *MirrorLauncher, registry *relay.Registry, events *telemetry.Bus, log *telemetry.Logger) *JoinService {
	return &JoinService{
		accounts: accounts,
		profiles: profiles,
		launcher: launcher,
		registry: registry,
		events:   events,
		log:      log,
		runs:     map[string]*joinRun{},
	}
}

// SetAllowLAN 设置是否监听 0.0.0.0 允许局域网共享连接。
func (s *JoinService) SetAllowLAN(allow bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowLAN = allow
}

// SetPort 设置期望代理监听的端口（0 表示默认 25565，若被占用自动回退到系统可用端口）。
func (s *JoinService) SetPort(port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.port = port
}

// SetShadowEnabled 设置影子端总开关。仅对新会话生效：已在跑的会话不回溯变更
// （已在监听的影子端也不强制停止，由用户显式 StopShadow 或随会话结束）。
func (s *JoinService) SetShadowEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shadowEnabled = enabled
}

// SetLoginService 注入登录服务（用于会话过期时静默重登）。
func (s *JoinService) SetLoginService(logins *LoginService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logins = logins
}

// SetAssetProvider 注入进服前置资产补齐器（客户端本体与服务器组件）。
// 不注入时保持旧行为：本地没有就退回默认客户端目录。
func (s *JoinService) SetAssetProvider(p AssetProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets = p
}

// joinRun 是一次进行中的会话资源集。
type joinRun struct {
	cancel      context.CancelFunc
	interceptor *relay.Interceptor
	manager     *session.Manager
	// control 是镜像客户端的控制通道，由 mirror_launch.go 在启动客户端前挂上；
	// 未启动镜像或通道开启失败时为 nil。
	control *clientcontrol.Listener
	// auth 是镜像客户端直连官方服时的 AuthLib 本地鉴权代答监听。
	auth *authsock.Listener
	// startedAt 支撑 session.stop 的 durationMs（EVENTS.md §1）。
	startedAt time.Time
	// connID 与 interceptor 的会话键一致（影子监听用 ConnByID 找活动连接）。
	// 多代理下它就是会话 ID（sess-N），全局唯一。
	connID string
	// launchMirror 标记这是自动拉起白端的会话（至多一个）；仅代理为 false。
	launchMirror bool
	// serverID / accountID / roleName 供代理管理列表展示与按会话定位。
	serverID  string
	accountID string
	roleName  string
	// shadowComponents 是影子端语义组件（录制器/闸门/屏蔽器），由 StartShadow
	// 装配；影子监听 attach 回调按需读取。nil = 影子端未启用。
	shadow *shadowRuntime

	// transfer 承载转服跟随指示（记录/消费/TTL 见 transfer_follow.go）：
	// 上游 Transfer 包到达时记录，客户端回连本监听口时消费并改指新上游。
	transfer transferFollow
}

// SessionHandle 是一次会话启动后的句柄（供绑定层回给前端）。
type SessionHandle struct {
	SessionID  string
	ListenAddr string
}

// SessionInfo 是运行中会话的快照，供代理管理列表展示。
type SessionInfo struct {
	SessionID    string
	ServerID     string
	AccountID    string
	RoleName     string
	ListenAddr   string
	Stage        string
	LaunchMirror bool
	StartedAt    time.Time
}

// shadowRuntime 聚合一次会话的影子端组件（service 层持有，shadow 包消费）。
type shadowRuntime struct {
	listener *shadow.ShadowListener
	recorder *shadow.Recorder
	gate     *shadow.Gate
	filter   *shadow.CustomPayloadFilter
	anchorID int32
	kaIDs    [2]int32
}

// shadowFrameState classifies a successfully forwarded server-bound frame for
// shadow replay. LoginSuccess is observed after its handler has already moved
// Conn to Configuration, but it is the fake server's responsibility and must
// not be recorded as a configuration frame.
func shadowFrameState(state relay.State, id int32) shadow.FrameState {
	switch state {
	case relay.StateConfiguration:
		return shadow.FrameStateConfiguration
	case relay.StatePlay:
		return shadow.FrameStatePlay
	default:
		return shadow.FrameStateUnknown
	}
}

// Join 启动会话并自动拉起白端，返回会话句柄（镜像客户端以 ListenAddr 为 --server 目标）。
// 阶段：ATTACH（代理就绪）→ TAP（镜像接入）→ AUTH（登录）→ MIRROR（代答完成）→ LIVE（进服）。
// 白端同时只允许一个（见 join_gate.go）。
func (s *JoinService) Join(ctx context.Context, envID, serverID, accountID string) (SessionHandle, error) {
	return s.join(ctx, envID, serverID, accountID, true)
}

// JoinProxyOnly starts the relay without launching the mirror client. 可多开。
func (s *JoinService) JoinProxyOnly(ctx context.Context, envID, serverID, accountID string) (SessionHandle, error) {
	return s.join(ctx, envID, serverID, accountID, false)
}

func (s *JoinService) join(ctx context.Context, envID, serverID, accountID string, launchMirror bool) (SessionHandle, error) {
	// 准入：占会话席位并分配 ID，退出时必释放（失败路径不释放会让白端位永久占着）。
	sessionID, release, gateErr := s.acquireJoinSlot(launchMirror)
	if gateErr != nil {
		return SessionHandle{}, gateErr
	}
	var success bool
	defer func() {
		if !success {
			release()
		}
	}()

	s.mu.Lock()
	allowLAN := s.allowLAN
	shadowOn := s.shadowEnabled
	s.mu.Unlock()

	var env profile.Env
	var err error
	if envID != "" {
		if e, err := s.profiles.Env(envID); err == nil {
			env = e
		}
	}
	server, err := s.profiles.Server(serverID)
	if err != nil {
		return SessionHandle{}, fmt.Errorf("读取服务器档案失败: %w", err)
	}
	// 账号提前解析：重复角色检查要用 RoleName，且越早失败越省掉后面的
	// 资产对账与 Java 探测（这些耗时且可能触发下载）。
	acct, err := s.accounts.Get(accountID)
	if err != nil {
		return SessionHandle{}, fmt.Errorf("读取账号失败: %w", err)
	}
	// 同一服务器 + 同一角色不许多开。放在资产/Java 之前：这两步可能触发
	// 数百 MB 下载，为一个注定被拒的请求付代价不值得。
	if dup := s.findDuplicateRun(server.ID, acct.RoleName); dup != nil {
		return SessionHandle{}, fmt.Errorf("%w（%s）", ErrDuplicateRole, dup.connID)
	}

	mcVer := server.McVersion
	if mcVer == "" {
		mcVer = "1.20"
	}
	gameID := server.GameID
	if gameID == "" {
		gameID = server.ID
	}
	// 进服前置资产只服务镜像启动：客户端本体与该服组件按网关权威清单对账补齐到
	// nexus 自己的数据目录 —— 不复用本机网易安装（其版本与完整性无从对账）。
	// 仅代理不走这条路：用户自备客户端，只起代理不该替他下载（joinAssets 的职责
	// 就是"启动镜像客户端必须具备的本地资产"）。modInfo 不受影响：其首选路径是
	// 网关权威清单，本地 mods 扫描只是回退。
	var componentCacheDir string
	if launchMirror {
		if s.assets != nil {
			assets, aerr := s.assets.EnsureJoinAssets(ctx, AssetRequest{
				MCVersion: mcVer,
				ServerID:  gameID,
				IsRental:  server.IsRental,
				AccountID: accountID,
			})
			if aerr != nil {
				return SessionHandle{}, fmt.Errorf("进服前置资产未就绪: %w", aerr)
			}
			if assets.ClientDir != "" {
				env.ClientDir = assets.ClientDir
				env.ModsDir = filepath.Join(assets.ClientDir, "mods")
			}
			componentCacheDir = assets.ComponentCacheDir
		}
		if env.ClientDir == "" {
			// 不做"探测本机网易安装"的回退：复用来路不明的客户端正是
			// "点 A 服却起了 B 服客户端"与组件过期故障的根源。
			return SessionHandle{}, fmt.Errorf("进服前置资产未就绪: 资产补齐器未注入或未产出客户端根")
		}
	}
	// Java 大版本只对镜像启动有意义：仅代理不启动客户端，relay 只是监听端口
	// 转发字节（详见 mirror_launch.go 的 ensureMirrorJava）。
	if launchMirror {
		if err := s.ensureMirrorJava(ctx, &env, mcVer); err != nil {
			return SessionHandle{}, err
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	preferredPort := 25565
	if s.port > 0 {
		preferredPort = s.port
	}
	bindHost := "127.0.0.1"
	if allowLAN {
		bindHost = "0.0.0.0"
	}
	bindAddr := fmt.Sprintf("%s:%d", bindHost, preferredPort)
	interceptor := relay.NewInterceptor(bindAddr, s.registry, s.events, s.log, nil)
	addr, err := listenReady(interceptor, runCtx)
	if err != nil {
		s.log.Warn("service", "首选端口监听失败，回退至系统可用端口", "preferred", bindAddr, "err", err.Error())
		bindAddr = fmt.Sprintf("%s:0", bindHost)
		interceptor = relay.NewInterceptor(bindAddr, s.registry, s.events, s.log, nil)
		addr, err = listenReady(interceptor, runCtx)
		if err != nil {
			cancel()
			return SessionHandle{}, fmt.Errorf("启动代理失败: %w", err)
		}
	}

	// 会话 ID 即连接键：多代理下必须全局唯一（同一服务器可起多个）。
	connID := sessionID
	manager := session.NewManager(connID, s.events, s.log)
	recorder := &stateToStage{managed: manager}

	modInfo := s.hybridModInfoSupplier(accountID, acct, server.GameID, mcVer, server.IsRental, env)
	joiner := &mirrorJoiner{
		inner:  account.NewJoiner(s.credsSupplier(accountID, acct), modInfo, mcVer),
		onOK:   func() { _ = manager.Transition(session.StageMirror) },
		events: s.events,
		connID: connID,
		relogin: func() bool {
			// DR-1：仅当用户开启凭据落盘（账号持有加密密码）时才静默重登。
			if s.logins == nil {
				return false
			}
			_, newToken, err := s.logins.ReLogin(context.Background(), accountID)
			if err != nil {
				s.log.Warn("service", "0xff 静默重登失败", "err", err.Error())
				return false
			}
			return newToken != ""
		},
	}

	// 影子端组件：录制器挂 Seam-1（S2C tap）、闸门挂 Seam-2（C2S 判定）。
	// 仅系统设置开启影子端才装配：录制是"接入时就有世界可回灌"的前提，但不用
	// 影子端时录制纯属浪费内存与 CPU —— 总开关默认关闭，关闭时 Seam-1/Seam-2
	// 一并不挂（relay 对 nil 回调有守卫，见 dispatch.go，主链路行为不变）。
	var sRec *shadow.Recorder
	var sGate *shadow.Gate
	var srt *shadowRuntime
	if shadowOn {
		anchorID := heypixel.RecordAnchorPacketID(relay.VersionAny)
		kaS2C, kaC2S := heypixel.KeepAlivePacketIDs(relay.VersionAny)
		sRec = shadow.NewRecorder(shadow.DefaultBudget, anchorID)
		sFilter := shadow.NewCustomPayloadFilter(shadow.CustomPayloadIDs{
			ServerBound: []int32{heypixel.PlayC2SPluginMessageID()},
			ClientBound: []int32{heypixel.PlayS2CPluginMessageID()},
		})
		sGate = shadow.NewGate(sFilter.AllowServerBound, shadow.KeepAliveConfig{
			ClientBoundID: kaS2C, ServerBoundID: kaC2S,
		})
		srt = &shadowRuntime{
			recorder: sRec, gate: sGate, filter: sFilter,
			anchorID: anchorID, kaIDs: [2]int32{kaS2C, kaC2S},
		}
	}
	// run 在 Accept 闭包之前构造：闭包内的转服跟随回调需要引用它。
	// （监听在 listenReady 已就绪，Accept 最早可在本行之前被并发触发，
	// 但转服记录只会发生在客户端接入之后 —— run 先于闭包存在即可。）
	run := &joinRun{
		cancel: cancel, interceptor: interceptor, manager: manager,
		startedAt: time.Now(), connID: connID,
		launchMirror: launchMirror,
		serverID:     server.ID,
		accountID:    accountID,
		roleName:     acct.RoleName,
		shadow:       srt,
	}
	interceptor.Accept = func(net.Conn) (*relay.Acceptance, error) {
		upstream := fmt.Sprintf("%s:%d", server.Host, server.Port)
		if target, ok := run.transfer.take(connID); ok {
			// 转服回连：客户端收到 Transfer 包后断开重连（实测毫秒级）。
			// 旧连接让位（Close 同步触发其 OnDisconnect → FAULTED 迁移 +
			// conns 表清理，随后按新连接重新装配），上游改指 Transfer 目标。
			// 必须先于 LIVE 守卫判定：转服瞬间旧连接可能尚未走完清理，
			// stage 仍显示 LIVE。
			if old := interceptor.ConnByID(connID); old != nil {
				old.Close()
			}
			s.log.Info("service", "转服回连：上游改指 Transfer 目标", "connID", connID, "upstream", target)
			upstream = target
		} else {
			// 会话已进入 play（LIVE）后到达且非转服回连的新连接一律拒绝：网易白端进服后会
			// 自发额外的 Forge 登录连接（实测 12:07:57，host 带 FORGE 标记），
			// 若放行装配，它会以同一 connID 覆盖正在 LIVE 的会话连接——白端被
			// 自己的第二个连接顶掉（FAULTED），影子端随之失去可挂靠的会话。
			// status 查询已由引擎本机应答，能走到 Accept 的都是登录态连接。
			if s.mu.Lock(); manager.Stage() == session.StageLive {
				s.mu.Unlock()
				s.log.Warn("service", "拒绝会话进行中的新登录连接（疑似客户端自发二次连接）",
					"stage", manager.Stage())
				return nil, fmt.Errorf("session: 会话进行中，拒绝二次登录连接")
			}
			s.mu.Unlock()
		}
		if err := manager.Transition(session.StageTap); err != nil {
			s.log.Warn("service", "阶段迁移告警", "err", err.Error())
		}
		acc := &relay.Acceptance{
			ID:           connID,
			UpstreamAddr: upstream,
			GameID:       server.GameID,
			LoginName:    acct.RoleName,
			Joiner:       joiner,
			// 白端（自动拉起的镜像客户端）原生完成 s2cevent 上报，relay 的
			// 服务器伪装只对仅代理会话生效。
			MirrorClient: launchMirror,
			OnStateChange: func(c *relay.Conn, from, to relay.State) {
				recorder.onStateChange(c, from, to)
				if sRec != nil && from == relay.StatePlay && to == relay.StateConfiguration {
					sRec.ResetConfiguration()
				}
			},
			OnLoginSuccess: func(*relay.Conn) {
				_ = manager.Transition(session.StageLive)
			},
			// 转服跟随（记录侧）：上游 Transfer 包到达即记下目标；客户端回连时
			// Accept hook 顶部的 take 命中并改指新上游。
			OnTransferRequest: func(c *relay.Conn, addr string) {
				run.transfer.record(c.ID, addr)
			},
			OnDisconnect: func(c *relay.Conn, err error) {
				leg, legErr := c.LostLeg()
				// 来源腿只由读取终止携带（err 必非 nil），此处防御以保回调安全。
				errText := "连接断开"
				if legErr != nil {
					errText = legErr.Error()
				}
				switch leg {
				case "server":
					// 上游读取终止：留痕并转 FAULTED（保留本地监听，客户端可重试）。
					s.events.Publish("session.upstream.lost", "fail", map[string]any{
						"connID": c.ID, "err": errText,
					})
					_ = manager.Transition(session.StageFaulted)
				case "client":
					// 镜像客户端退出/断开：留痕并转 FAULTED。
					s.events.Publish("session.client.lost", "fail", map[string]any{
						"connID": c.ID, "err": errText,
					})
					_ = manager.Transition(session.StageFaulted)
				default:
					// 无来源（用户停止 / 协议错误路径）：保留原 FAULTED 迁移语义，
					// 不发 lost 事件——没有"哪条腿先断"的事实就不造数据（U4）。
					_ = manager.Transition(session.StageFaulted)
				}
			},
		}
		if sRec == nil {
			// 影子端关闭（系统设置默认）：Seam-1/Seam-2 一并不挂，不录制、不判闸。
			// relay 对 nil 回调有守卫（dispatch.go），主链路行为与开启时完全一致。
			return acc, nil
		}
		// Seam-1 录制 tap：挂在最终转发字节上（含处理器改写），并标注
		// 录制时的协议态 —— config 段与 play 段的包 ID 表不同，B 只能按
		// 当前态解码，混态回灌会让它报 unknown packet id 而断开
		// （2026-09-17 21:55 实测：config 段混入 play 帧 47）。
		acc.OnClientBoundFrameState = func(c *relay.Conn, state relay.State, id int32, payload []byte) {
			// start_configuration is a state boundary, not a world snapshot
			// frame. It is consumed by the state machine and must not enter the
			// PLAY replay stream.
			if state == relay.StatePlay && c.State() == relay.StateConfiguration &&
				id == login.StartConfigurationPacketID(c.ProtocolVersion) {
				return
			}
			if state == relay.StateConfiguration && c.ConfigurationFinishSent() {
				state = relay.StatePlay
			}
			st := shadowFrameState(state, id)
			if st == shadow.FrameStatePlay {
				// play 缓存重置（packetctl 语义）：锚点帧（JoinGame）到达
				// 即新世界代际——清空 play 缓存从锚点起重新录制；已接入的
				// B 由回灌后的代际校验发现失真并断开重连。
				if id == sRec.Anchor() {
					sRec.ResetPlay()
				}
			}
			sRec.RecordState(st, id, payload)
		}
		// Seam-2 上行闸门：这里挂的是**主客户端（白端）**的判定，必须用 AllowA。
		// AllowB 要求 sender == SenderB，而闸门默认 sender = SenderA 且未接入影子端时
		// 无人切换 → AllowB 恒 false，会把白端的全部上行帧（含握手包）吞掉，
		// 表现为所有服务器都进不去。影子端自身的 C2S 判定在 shadow_listener.go 内用 AllowB。
		acc.C2SGate = sGate.AllowA
		// 握手回调：此处才拿到客户端的真实协议版本，用它把录制锚点与
		// KeepAlive 包号从「通配版本的 -1」更正为真实值。
		// 构造期只能用 VersionAny(-1) 查表，必然拿到 -1 —— 那样录制永远
		// 识别不到世界锚点，影子端回灌必然被拒（2026-09-17 实测故障）。
		acc.OnHandshake = func(c *relay.Conn, _ relay.State) {
			ver := c.ProtocolVersion
			anchor := heypixel.RecordAnchorPacketID(ver)
			sRec.SetAnchor(anchor)
			kaC, kaS := heypixel.KeepAlivePacketIDs(ver)
			sGate.SetKeepAliveIDs(kaC, kaS)
			s.log.Info("shadow", "已按客户端协议版本更正录制锚点与 KeepAlive 包号",
				"version", ver, "anchor", anchor, "kaS2C", kaC, "kaC2S", kaS)
		}
		// 压缩协商结果必须传给录制器：影子端回灌时要用同一阈值编码，
		// 否则 B 会按未压缩格式解析压缩帧并报 DecoderException
		// （2026-09-17 实测：B 报 game_profile 解码失败、replayed=563 中断）。
		acc.OnCompression = func(_ *relay.Conn, threshold int32) {
			sRec.SetCompression(threshold)
			s.log.Info("shadow", "已记录会话压缩阈值（供影子端回灌编码）",
				"compression", threshold)
		}
		return acc, nil
	}

	s.mu.Lock()
	s.runs[connID] = run
	s.mu.Unlock()

	if err := manager.Transition(session.StageAttach); err != nil {
		s.log.Warn("service", "阶段迁移告警", "err", err.Error())
	}
	s.events.Publish("session.start", "ok", map[string]any{
		"sessionID": connID, "serverID": server.ID, "gameID": server.GameID,
	})
	s.log.Info("service", "会话已就绪", "sessionID", connID, "listen", addr, "version", mcVer)

	// 镜像启动编排：档案已配置启动模板则自动拉起，否则等待手动接入。
	if launchMirror {
		gameID := server.GameID
		if gameID == "" {
			gameID = server.ID
		}
		s.launchMirror(runCtx, run, mirrorRequest{
			env:               env,
			accountID:         accountID,
			roleName:          acct.RoleName,
			userID:            acct.UserID,
			mcVersion:         mcVer,
			gameID:            gameID,
			relayAddr:         addr,
			componentCacheDir: componentCacheDir,
			manager:           manager,
			serverHost:        server.Host,
			serverPort:        server.Port,
			creds:             s.credsSupplier(accountID, acct),
			modInfo:           modInfo,
		})
	}

	// 客户端接入看门狗：没有它，客户端起失败/被拦/控制通道降级时会话永远停在
	// ATTACH，用户只看到一个永远脉冲的"等待客户端"，分不清"还在启动"和"已经卡死"。
	go s.watchClientAttach(runCtx, manager, connID, launchMirror)

	success = true
	return SessionHandle{SessionID: connID, ListenAddr: addr}, nil
}

// 客户端接入提示阈值。镜像模式客户端由我们拉起，JVM+Forge 初始化通常一分钟内；
// proxy-only 模式由用户手动连自己的客户端，可能故意连得慢，阈值放宽。
const (
	clientAttachWarnMirror    = 3 * time.Minute
	clientAttachWarnProxyOnly = 10 * time.Minute
)

// watchClientAttach 在会话就绪后客户端长时间未连入时发布一次 session.attach.waiting。
//
// 只提示、不迁移状态：proxy-only 模式下用户可能故意连得慢，自动转 FAULTED 会误伤；
// 真正要中断会话，用户可点"停止"（Stop 会 cancel runCtx，本 goroutine 随之退出）。
func (s *JoinService) watchClientAttach(ctx context.Context, manager *session.Manager, connID string, launchMirror bool) {
	wait := clientAttachWarnProxyOnly
	if launchMirror {
		wait = clientAttachWarnMirror
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
		// 只有仍停在 ATTACH（代理已监听、尚无客户端连入）才提示；
		// 已推进到 TAP/AUTH/... 或已结束都无需提示。
		if manager.Stage() != session.StageAttach {
			return
		}
		s.events.Publish("session.attach.waiting", "warn", map[string]any{
			"connID":       connID,
			"waitedMs":     wait.Milliseconds(),
			"launchMirror": launchMirror,
		})
		s.log.Warn("service", "客户端长时间未连入", "connID", connID,
			"waitedMs", wait.Milliseconds(), "launchMirror", launchMirror)
	}
}

// stateToStage 把 relay 的 MC 态迁移映射为会话阶段。
type stateToStage struct {
	mu      sync.Mutex
	managed *session.Manager
}

func (r *stateToStage) onStateChange(_ *relay.Conn, from, to relay.State) {
	r.mu.Lock()
	m := r.managed
	r.mu.Unlock()
	if m == nil {
		return
	}
	switch {
	case to == relay.StateLogin && from == relay.StateHandshake:
		_ = m.Transition(session.StageAuth)
	case to == relay.StatePlay:
		_ = m.Transition(session.StageLive)
	}
}

// mirrorJoiner 包装 account.Joiner：join 成功即进入 MIRROR（代答完成）；
// init 0xff（会话过期）由编排层按 DR-1 静默重登后重试一次，并发布 session.rejoin 留痕。
type mirrorJoiner struct {
	inner relay.Joiner
	onOK  func()
	// relogin 由 join() 绑定编排层方法（DR-1：仅凭据已落盘时真重登）；
	// 返回 true 表示凭据已刷新、可重试。测试可注入替身。
	relogin func() bool
	events  *telemetry.Bus
	connID  string
}

func (j *mirrorJoiner) OnEncryptionRequest(c *relay.Conn, req relay.EncryptionRequestData) ([]byte, error) {
	secret, err := j.inner.OnEncryptionRequest(c, req)
	if err == nil {
		j.onOK()
		return secret, nil
	}
	if !errors.Is(err, netease.ErrSessionDead) {
		return nil, err
	}
	// 0xff：登录会话过期（facts §4）。是否真重登由 DR-1 决定。
	result := "fail"
	if j.relogin != nil && j.relogin() {
		result = "ok"
	}
	j.events.Publish("session.rejoin", result, map[string]any{
		"connID": j.connID, "reason": "0xff", "result": result,
	})
	if result != "ok" {
		return nil, err
	}
	// inner 的 Credentials 闭包每次调用都经 credsSupplier 重取 token，
	// 重试自然携带新凭据；只重试一次，避免坏凭据反复打 authserver。
	return j.inner.OnEncryptionRequest(c, req)
}

func (s *JoinService) credsSupplier(accountID string, acct account.Account) func() (netease.Credentials, error) {
	return func() (netease.Credentials, error) {
		token, err := s.accounts.Token(accountID)
		if err != nil || token == "" {
			if s.logins != nil && acct.Username != "" && acct.EncryptedPassword != "" {
				if newAcct, newToken, reErr := s.logins.ReLogin(context.Background(), accountID); reErr == nil && newToken != "" {
					acct = newAcct
					token = newToken
					err = nil
				}
			}
		}
		if err != nil || token == "" {
			return netease.Credentials{}, fmt.Errorf("读取凭据失败: %w", err)
		}
		return netease.Credentials{UserID: acct.UserID, Token: token}, nil
	}
}

func (s *JoinService) hybridModInfoSupplier(accountID string, acct account.Account, serverID, mcVer string, isRental bool, env profile.Env) func() string {
	return func() string {
		// 1. 优先从网关拉取最新权威模组清单 (包含目标 MC 版本核心模组 + 服务器组件模组)
		token, err := s.accounts.Token(accountID)
		if err != nil || token == "" {
			if s.logins != nil && acct.Username != "" && acct.EncryptedPassword != "" {
				if newAcct, newToken, reErr := s.logins.ReLogin(context.Background(), accountID); reErr == nil && newToken != "" {
					acct = newAcct
					token = newToken
					err = nil
				}
			}
		}
		if err == nil && token != "" {
			gw := netease.NewGateway(netease.Credentials{UserID: acct.UserID, Token: token})
			mods, err := gw.FetchAuthoritativeMods(context.Background(), mcVer, serverID, isRental)
			if err == nil && len(mods) > 0 {
				s.log.Info("service", "从网易网关成功获取最新权威组件", "version", mcVer, "isRental", isRental, "count", len(mods))
				return netease.BuildModInfoJSON(mods)
			}
			if err != nil {
				s.log.Warn("service", "从网易网关获取组件失败，尝试回退本地扫描", "version", mcVer, "err", err.Error())
			}
		}

		// 2. 回退本地组件扫描 (如 D:\MCLDownload\Game\.minecraft\mods)
		if env.ModsDir != "" {
			if mods, err := profile.ScanMods(env.ModsDir); err == nil && len(mods) > 0 {
				s.log.Info("service", "使用本地已安装组件构建 modInfo", "count", len(mods))
				return netease.BuildModInfoJSON(mods)
			}
		}

		return ""
	}
}

// listenReady 启动监听并等待就绪。
func listenReady(i *relay.Interceptor, ctx context.Context) (string, error) {
	errCh := make(chan error, 1)
	go func() { errCh <- i.ListenAndServe(ctx) }()
	for tick := 0; tick < 100; tick++ {
		if a := i.Addr(); a != nil {
			return a.String(), nil
		}
		select {
		case err := <-errCh:
			return "", err
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	select {
	case err := <-errCh:
		return "", err
	default:
		return "", errors.New("代理监听超时")
	}
}

// RegistryWithDefaults 装配引擎注册表（main.go 调用）。
func RegistryWithDefaults() *relay.Registry {
	rg := &relay.Registry{}
	rg.Register(login.Packets()...)
	rg.Register(heypixel.Packets()...)
	return rg
}
