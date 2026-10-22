package app

import (
	"fmt"
	"net"
	"time"

	"nexus/internal/service"
)

// StartSession 启动会话（使用全局设置中的主档案组合），返回会话 ID 与本机代理地址。
// hosts 劫持属 S4 高风险操作：确认弹窗在前端发起调用前完成（架构文档 §10.6）。
func (a *App) StartSession() (StartSessionResult, error) {
	return a.startSession(true)
}

// StartProxyOnly starts the local relay and waits for a manually launched client.
// 可多开：每次调用都会新建一个独立代理会话。
func (a *App) StartProxyOnly() (StartSessionResult, error) {
	return a.startSession(false)
}

// StartSessionWithTarget 直接指定目标服务器与账号启动会话（对齐 OpenNEL/Fantnel 原子化进服）。
func (a *App) StartSessionWithTarget(serverID, accountID string, launchMirror bool) (StartSessionResult, error) {
	if serverID == "" || accountID == "" {
		return StartSessionResult{}, fmt.Errorf("请指定目标服务器与账号")
	}

	a.settings.MainServerID = serverID
	a.settings.MainAccountID = accountID
	_ = a.saveSettings()

	return a.startSession(launchMirror)
}

func (a *App) startSession(launchMirror bool) (StartSessionResult, error) {
	if a.settings.MainServerID == "" || a.settings.MainAccountID == "" {
		return StartSessionResult{}, fmt.Errorf("请先选择目标服务器与账号角色")
	}
	a.joins.SetAllowLAN(a.settings.AllowLAN)
	if a.settings.LanPort > 0 {
		a.joins.SetPort(a.settings.LanPort)
	}
	var handle service.SessionHandle
	var err error
	if launchMirror {
		handle, err = a.joins.Join(a.ctx, a.settings.MainEnvID, a.settings.MainServerID, a.settings.MainAccountID)
	} else {
		handle, err = a.joins.JoinProxyOnly(a.ctx, a.settings.MainEnvID, a.settings.MainServerID, a.settings.MainAccountID)
	}
	if err != nil {
		return StartSessionResult{}, err
	}

	_, portStr, _ := net.SplitHostPort(handle.ListenAddr)
	if portStr == "" {
		portStr = "25565"
	}
	localAddr := fmt.Sprintf("127.0.0.1:%s", portStr)

	res := StartSessionResult{
		SessionID:  handle.SessionID,
		ListenAddr: localAddr,
		LocalAddr:  localAddr,
		IsLAN:      a.settings.AllowLAN,
	}

	if a.settings.AllowLAN {
		ips := service.GetLocalIPv4s()
		var lanAddrs []string
		for _, ip := range ips {
			lanAddrs = append(lanAddrs, fmt.Sprintf("%s:%s", ip, portStr))
		}
		res.LanAddrs = lanAddrs
		if len(lanAddrs) > 0 {
			res.PrimaryLan = lanAddrs[0]
			res.ListenAddr = lanAddrs[0]
		} else {
			res.PrimaryLan = localAddr
		}
	}

	return res, nil
}

// StopSession 停止指定会话并回滚 hosts。多代理下每次只停一个。
func (a *App) StopSession(sessionID string) error {
	return a.joins.Stop(sessionID)
}

// StopAllSessions 停止全部会话（应用退出用）。
func (a *App) StopAllSessions() error {
	return a.joins.StopAll()
}

// ListSessions 返回全部运行中的代理会话（代理管理页数据源）。
func (a *App) ListSessions() ([]SessionInfoDTO, error) {
	infos := a.joins.ListSessions()
	out := make([]SessionInfoDTO, 0, len(infos))
	for _, info := range infos {
		out = append(out, SessionInfoDTO{
			SessionID:    info.SessionID,
			ServerID:     info.ServerID,
			AccountID:    info.AccountID,
			RoleName:     info.RoleName,
			ListenAddr:   info.ListenAddr,
			Stage:        info.Stage,
			LaunchMirror: info.LaunchMirror,
			StartedAt:    info.StartedAt.Format(time.RFC3339),
		})
	}
	return out, nil
}
