// Package app 是 Wails 绑定层（架构文档 §10.1）：只做参数校验 + DTO 映射 + 转发（P7/D1），
// 业务决策一律在 internal/service 与领域包。
package app

import (
	"context"
	"runtime"
	"sync"
	"time"

	"nexus/internal/account"
	"nexus/internal/httpx"
	"nexus/internal/profile"
	"nexus/internal/relay"
	relaypack "nexus/internal/relay/packet/ecpack"
	"nexus/internal/service"
	"nexus/internal/store"
	"nexus/internal/telemetry"
)

// Deps 是 main.go 装配好的依赖集。
type Deps struct {
	Registry *relay.Registry
}

// App 是暴露给前端的绑定对象；按域分文件（account_binding.go 等）。
type App struct {
	ctx context.Context

	paths    *store.Paths
	events   *telemetry.Bus
	logger   *telemetry.Logger
	accounts *account.Manager
	profiles *profile.Manager
	joins    *service.JoinService
	logins   *service.LoginService
	launcher *service.MirrorLauncher
	// ecPack 是 EC 资源包交付器（惰性准备；仅 EC GameID 会用到）。
	ecPack *ecPackDelivery

	emitOnce      sync.Once
	refreshCancel context.CancelFunc
	refreshWG     sync.WaitGroup
	settings      store.Settings

	serverImages      sync.Map
	serverVersions    sync.Map
	serverBedrock     sync.Map
	serverMaintaining sync.Map
	skinImages        sync.Map
}

// New 装配全部依赖（进程启动期，主 goroutine 串行）。
func New(deps Deps) (*App, error) {
	root, err := store.DataDir()
	if err != nil {
		return nil, err
	}
	paths, err := store.NewPaths(root)
	if err != nil {
		return nil, err
	}
	bus := telemetry.NewBus()
	logger, err := telemetry.New(bus, root)
	if err != nil {
		return nil, err
	}
	accounts := account.NewManager(paths, account.NewTokenStore(paths))
	profiles := profile.NewManager(paths)
	settings, err := store.LoadSettings(paths)
	if err != nil {
		return nil, err
	}
	// 代理策略随设置启动即生效（保存时由 SaveSettings 同步更新，见 system_binding.go）。
	httpx.SetBypassProxy(settings.BypassSystemProxy)

	logins := service.NewLoginService(accounts, bus, logger)
	joins := service.NewJoinService(accounts, profiles, service.NewMirrorLauncher(logger), deps.Registry, bus, logger)
	joins.SetLoginService(logins)
	joins.SetAllowLAN(settings.AllowLAN)
	joins.SetShadowEnabled(settings.ShadowEnabled)
	if settings.LanPort > 0 {
		joins.SetPort(settings.LanPort)
	}

	app := &App{
		paths:    paths,
		events:   bus,
		logger:   logger,
		accounts: accounts,
		profiles: profiles,
		launcher: service.NewMirrorLauncher(logger),
		joins:    joins,
		logins:   logins,
		settings: settings,
	}
	// 进服前置资产补齐：本地缺客户端本体或该服组件时下载，而不是退回别的客户端。
	// 闭包指向 app.settings，clientdata 选址后会写回它并落盘。
	joins.SetAssetProvider(newJoinAssets(paths, accounts, bus, logger, &app.settings))
	// EC 资源包交付：把服务端进服下发的资源包原样经 relay 下发给镜像客户端。
	// 惰性准备（首次进 EC 收到服务端推送才下载/起服务），失败只记日志、不介入 —— 非 EC 服务器
	// 根本不会命中这些处理器（按 EC GameID 作用域注册），因此零改动、零开销。
	if deps.Registry != nil {
		delivery := ecPackDeliveryFor(paths, &app.settings, logger)
		packets := relaypack.Packets(relaypack.GameIDs, delivery)
		deps.Registry.Register(packets...)
		app.ecPack = delivery
		// 留痕：实机验证时据此确认交付器确实注册上了（惰性准备要等进 EC 才触发）。
		logger.Info("ecpack", "EC 资源包交付已注册",
			"gameIDs", relaypack.GameIDs, "handlers", len(packets))
	}
	return app, nil
}

// Startup 在 Wails 启动时调用：保存运行时上下文并把遥测事件桥接到前端。
func (a *App) Startup(ctx context.Context) {
	if runtime.GOARCH != "amd64" || runtime.GOOS != "windows" {
		panic("CortexNEL 仅支持 Windows x64 (windows/amd64) 架构，禁止在非 x64 平台运行")
	}
	a.ctx = ctx
	refreshCtx, cancelRefresh := context.WithCancel(ctx)
	a.refreshCancel = cancelRefresh
	a.refreshWG.Add(1)
	go func() {
		defer a.refreshWG.Done()
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-refreshCtx.Done():
				return
			case <-ticker.C:
				a.logins.RefreshAuthenticationTokens(refreshCtx)
			}
		}
	}()
	a.emitOnce.Do(func() {
		a.events.Subscribe("", func(e telemetry.Event) {
			// 遥测事件统一推给前端（emit 补统一载荷 ts；前端 bus.ts 落 store）。
			emit(ctx, e)
		})
	})
}

// Shutdown 在应用退出时调用：停全部会话（含 hosts 回滚）并关日志。
func (a *App) Shutdown(ctx context.Context) {
	if a.refreshCancel != nil {
		a.refreshCancel()
		a.refreshWG.Wait()
	}
	_ = a.joins.StopAll()
	_ = a.logger.Close()
}
