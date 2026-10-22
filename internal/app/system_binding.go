// 系统域绑定（架构文档 §10.1）：SaveCredentialOptIn / OpenDataDir / GetSettings / SaveSettings。
package app

import (
	"fmt"
	"os/exec"

	"nexus/internal/httpx"
	"nexus/internal/namegen"
	"nexus/internal/service"
	"nexus/internal/store"
)

// GetSettings 读全局设置。
func (a *App) GetSettings() SettingsDTO {
	lanPort := a.settings.LanPort
	if lanPort <= 0 || lanPort > 65535 {
		lanPort = 25565
	}
	return SettingsDTO{
		MainAccountID:     a.settings.MainAccountID,
		MainEnvID:         a.settings.MainEnvID,
		MainServerID:      a.settings.MainServerID,
		CredentialPersist: a.settings.CredentialPersist,
		HideZeroPlayers:   a.settings.HideZeroPlayers,
		HideMaintaining:   a.settings.HideMaintaining,
		HideBedrockOnly:   a.settings.HideBedrockOnly,
		AllowLAN:          a.settings.AllowLAN,
		LanPort:           lanPort,
		RoleNamePrefix:    a.settings.RoleNamePrefix,
		RoleNameSuffix:    a.settings.RoleNameSuffix,
		RoleNameStyle:     a.settings.RoleNameStyle,
		RoleNameBaseWord:  a.settings.RoleNameBaseWord,
		RoleNameDigits:    a.settings.RoleNameDigits,
		RoleNameHomophone: a.settings.RoleNameHomophone,
		RoleNameExactTone: a.settings.RoleNameExactTone,
		RoleNameAllowSelf: a.settings.RoleNameAllowSelf,
		ClientDataDir:     a.settings.ClientDataDir,
		BypassSystemProxy: a.settings.BypassSystemProxy,
		ShadowEnabled:     a.settings.ShadowEnabled,
	}
}

// SaveSettings 写全局设置。
func (a *App) SaveSettings(dto SettingsDTO) error {
	mainAccountID := dto.MainAccountID
	if mainAccountID == "" {
		mainAccountID = a.settings.MainAccountID
	}
	mainEnvID := dto.MainEnvID
	if mainEnvID == "" {
		mainEnvID = a.settings.MainEnvID
	}
	mainServerID := dto.MainServerID
	if mainServerID == "" {
		mainServerID = a.settings.MainServerID
	}

	lanPort := dto.LanPort
	if lanPort <= 0 || lanPort > 65535 {
		lanPort = 25565
	}

	a.settings = store.Settings{
		MainAccountID:     mainAccountID,
		MainEnvID:         mainEnvID,
		MainServerID:      mainServerID,
		CredentialPersist: dto.CredentialPersist,
		HideZeroPlayers:   dto.HideZeroPlayers,
		HideMaintaining:   dto.HideMaintaining,
		HideBedrockOnly:   dto.HideBedrockOnly,
		AllowLAN:          dto.AllowLAN,
		LanPort:           lanPort,
		RoleNamePrefix:    dto.RoleNamePrefix,
		RoleNameSuffix:    dto.RoleNameSuffix,
		RoleNameStyle:     dto.RoleNameStyle,
		RoleNameBaseWord:  dto.RoleNameBaseWord,
		RoleNameDigits:    dto.RoleNameDigits,
		RoleNameHomophone: dto.RoleNameHomophone,
		RoleNameExactTone: dto.RoleNameExactTone,
		RoleNameAllowSelf: dto.RoleNameAllowSelf,
		// 留空沿用旧值：该目录由 internal/clientdata 选址写回，界面只是展示/可覆盖，
		// 不能因为前端没回传就把已下载的客户端本体路径抹掉。
		ClientDataDir:     a.settings.ClientDataDir,
		BypassSystemProxy: dto.BypassSystemProxy,
		ShadowEnabled:     dto.ShadowEnabled,
	}
	if dto.ClientDataDir != "" {
		a.settings.ClientDataDir = dto.ClientDataDir
	}

	a.joins.SetAllowLAN(dto.AllowLAN)
	a.joins.SetPort(lanPort)
	// 影子端开关随保存下发：仅对新会话生效（已在跑的会话与影子监听不变）。
	a.joins.SetShadowEnabled(dto.ShadowEnabled)

	// 代理策略一旦保存立即生效：新构造的网络客户端（网关/下载）读的是全局策略，
	// 不必等重启（internal/httpx.SetBypassProxy）。
	httpx.SetBypassProxy(a.settings.BypassSystemProxy)

	return a.saveSettings()
}

// GenerateRoleName 生成单个符合规范的随机角色名。
func (a *App) GenerateRoleName(opts GenerateRoleNameOptions) string {
	return namegen.Generate(namegen.Options{
		Prefix:       opts.Prefix,
		Suffix:       opts.Suffix,
		Style:        opts.Style,
		BaseWord:     opts.BaseWord,
		Digits:       opts.Digits,
		MaxLength:    opts.MaxLength,
		UseHomophone: opts.UseHomophone,
		ExactTone:    opts.ExactTone,
		AllowSelf:    opts.AllowSelf,
	})
}

// BatchGenerateRoleNames 批量生成候选角色名。
func (a *App) BatchGenerateRoleNames(opts GenerateRoleNameOptions, count int) []string {
	return namegen.BatchGenerate(namegen.Options{
		Prefix:       opts.Prefix,
		Suffix:       opts.Suffix,
		Style:        opts.Style,
		BaseWord:     opts.BaseWord,
		Digits:       opts.Digits,
		MaxLength:    opts.MaxLength,
		UseHomophone: opts.UseHomophone,
		ExactTone:    opts.ExactTone,
		AllowSelf:    opts.AllowSelf,
	}, count)
}

// ConvertHomophone 将指定中文文本转换为逻辑谐音串（如 "海棠花阔地" -> "海塘话扩第"）。
func (a *App) ConvertHomophone(text string, exactTone bool, allowSelf bool) string {
	return namegen.ConvertHomophone(text, namegen.HomophoneOptions{
		ExactTone: exactTone,
		AllowSelf: allowSelf,
	})
}

// BatchConvertHomophones 批量转换候选谐音串。
func (a *App) BatchConvertHomophones(text string, count int, exactTone bool, allowSelf bool) []string {
	return namegen.BatchConvertHomophone(text, count, namegen.HomophoneOptions{
		ExactTone: exactTone,
		AllowSelf: allowSelf,
	})
}

// SaveCredentialOptIn 单独切换 DR-1 凭据落盘（设置页开关，前端已二次确认）。
func (a *App) SaveCredentialOptIn(enabled bool) error {
	a.settings.CredentialPersist = enabled
	return a.saveSettings()
}

// OpenDataDir 打开数据目录（资源管理器）。
func (a *App) OpenDataDir() error {
	return exec.Command("explorer", a.paths.Root).Start()
}

func (a *App) saveSettings() error {
	if err := store.SaveSettings(a.paths, a.settings); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}
	return nil
}

// GetLocalIPv4s 获取本机可用局域网 IPv4 地址列表（用于设置页与概览页直观展示与排查）。
func (a *App) GetLocalIPv4s() []string {
	return service.GetLocalIPv4s()
}

// ConfigureFirewall 为指定端口保证 Windows 防火墙入站允许规则存在（幂等，
// 见 firewall.go；仅 Windows 平台）。
func (a *App) ConfigureFirewall(port int) error {
	if port <= 0 {
		port = 25565
	}
	return ensureFirewallRule(port, runNetsh)
}
