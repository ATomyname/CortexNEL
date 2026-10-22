// Package singleinstance 保证应用全局只跑一个进程，并在第二个实例被拦下时
// 把已在运行的窗口唤起。
//
// 不拦的后果：两个 CortexNEL.exe 会各起一个 relay 抢 25565、各抢控制通道
// 11413、并发读写同一份 %AppData%\CortexNEL\config.json（后写覆盖先写），
// 用户也分不清哪个窗口才是真正的会话持有者。
//
// 实现是 Wails 的 SingleInstanceLock：它在同一 Windows 登录会话内建
// Local\ 命名互斥体，第二个实例发消息后自行 os.Exit(0)。不同 Windows 用户
// 各在自己的登录会话里，互不影响 —— 他们有独立的 %AppData% 数据目录，
// 理应能同时使用。
package singleinstance

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// LockID 是单实例互斥体标识（Wails 会拼成 wails-app-<id>-sim）。
// 固定值才能让"第二个实例"找到"第一个"；改了它旧实例就拦不住新实例。
const LockID = "cortexnel-main-v1"

// Activator 记住首个实例的运行时上下文，供唤起窗口用。
// 第二个实例的消息到达时 Wails 在消息循环线程回调，那时 OnStartup 早已
// 跑完，ctx 可直接取用；取不到（窗口尚未起）就静默跳过，不阻断退出。
type Activator struct {
	mu  sync.Mutex
	ctx context.Context
}

// Remember 记录首个实例的运行时上下文，由 OnStartup 调用。
func (a *Activator) Remember(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

// BringToFront 把已在运行的窗口恢复到前台（取消最小化并显示）。
func (a *Activator) BringToFront() {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
}

// Lock 返回供 options.App.SingleInstanceLock 使用的配置：
// 第二个实例被拦下时唤起首个实例的窗口。
func (a *Activator) Lock() *options.SingleInstanceLock {
	return &options.SingleInstanceLock{
		UniqueId:               LockID,
		OnSecondInstanceLaunch: func(options.SecondInstanceData) { a.BringToFront() },
	}
}
