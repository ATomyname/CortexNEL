// Package main 是 Nexus 进程入口，只负责装配与生命周期（规范 §3）。
// 业务代码一律位于 internal/。
package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"nexus/internal/app"
	"nexus/internal/service"
	"nexus/internal/singleinstance"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 引擎注册表：内置登录处理器 + 服务器适配包（GameID 作用域，架构文档 §6.5）。
	registry := service.RegistryWithDefaults()

	binding, err := app.New(app.Deps{Registry: registry})
	if err != nil {
		panic(err)
	}

	// 单实例：拦下第二个进程并唤起本窗口（细节见 internal/singleinstance）。
	activator := &singleinstance.Activator{}

	err = wails.Run(&options.App{
		Title:     "CortexNEL",
		Width:     1024,
		Height:    700,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup: func(ctx context.Context) {
			activator.Remember(ctx)
			binding.Startup(ctx)
		},
		OnShutdown:         binding.Shutdown,
		SingleInstanceLock: activator.Lock(),
		Bind: []any{
			binding,
		},
	})
	if err != nil {
		panic(err)
	}
}
