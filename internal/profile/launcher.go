// 镜像启动参数装配（架构文档 §4 profile/launcher.go）。
// 本文件只做一件事：把环境档案 + 会话参数翻译成 launcher 的输入。
package profile

import (
	"nexus/internal/launcher"
)

// LaunchSpec 是一次镜像客户端启动的全部参数。
type LaunchSpec = launcher.LaunchSpec

// LaunchRequest 是一次镜像启动的会话侧参数（与环境档案正交）。
// 留空字段交由 launcher 自动探测：RoleName 用默认角色，MCVersion 按客户端目录推断。
type LaunchRequest struct {
	RoleName   string
	UserID     int64
	LocalPort  int
	ServerHost string
	ServerPort int
	MCVersion  string
	GameID     string
	// ControlPort 是 internal/clientcontrol 控制通道的实际监听端口。
	// 为 0 表示本次会话没有开通道，启动参数里不写 launcherport。
	ControlPort int
	// AuthPort 是 internal/authsock 本地鉴权代答监听端口。
	AuthPort int
	// ComponentCacheDir 是 nexus 组件缓存目录（该服按网关清单下载的组件）。
	// 非空时由 launcher 作为沙盒组件播种的最后一路叠加来源。
	ComponentCacheDir string
	// AuthToken 是网易账号 token 明文，用于派生客户端的 -DToken。
	// 为空时不写该参数。
	AuthToken string
}

// BuildLaunch 装配启动参数：环境档案给路径与参数模板，LaunchRequest 给会话取值。
func BuildLaunch(env Env, req LaunchRequest) (LaunchSpec, error) {
	return launcher.BuildMinecraftLaunch(launcher.LaunchOptions{
		JavaPath:          env.JavaPath,
		ClientDir:         env.ClientDir,
		RoleName:          req.RoleName,
		UserID:            req.UserID,
		LocalPort:         req.LocalPort,
		ServerHost:        req.ServerHost,
		ServerPort:        req.ServerPort,
		JvmArgs:           env.JvmArgs,
		CustomLaunchArgs:  env.LaunchArgs,
		Version:           req.MCVersion,
		GameID:            req.GameID,
		ControlPort:       req.ControlPort,
		AuthPort:          req.AuthPort,
		ComponentCacheDir: req.ComponentCacheDir,
		AuthToken:         req.AuthToken,
	})
}
