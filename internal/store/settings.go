// 全局设置（config.json，架构文档 §9）：DR-1 凭据策略 + 主档案选择。
package store

import "path/filepath"

// Settings 是全局设置。
type Settings struct {
	MainAccountID string `json:"mainAccountId"`
	MainEnvID     string `json:"mainEnvId"`
	MainServerID  string `json:"mainServerId"`
	// CredentialPersist 是 DR-1 的显式 opt-in：默认 false，凭据只在会话内存。
	CredentialPersist bool `json:"credentialPersist"`

	// 服务器列表展示过滤偏好
	HideZeroPlayers bool `json:"hideZeroPlayers"`
	HideMaintaining bool `json:"hideMaintaining"`
	HideBedrockOnly bool `json:"hideBedrockOnly"`
	// 局域网共享监听（绑定 0.0.0.0）
	AllowLAN bool `json:"allowLan"`
	LanPort  int  `json:"lanPort,omitempty"`

	// 角色随机命名参数偏好
	RoleNamePrefix   string `json:"roleNamePrefix,omitempty"`
	RoleNameSuffix   string `json:"roleNameSuffix,omitempty"`
	RoleNameStyle    string `json:"roleNameStyle,omitempty"`
	RoleNameBaseWord string `json:"roleNameBaseWord,omitempty"`
	// RoleNameDigits 不能加 omitempty：0 是用户的真实选择（"不添加数字"），
	// 不是"未设置"。加了 omitempty 时 0 会被整个省略，反序列化后虽也是 0，
	// 但前端 Object.assign 不拷贝缺失键、退回自建的初值 2，表现为"调了不保存"。
	RoleNameDigits    int  `json:"roleNameDigits"`
	RoleNameHomophone bool `json:"roleNameHomophone,omitempty"`
	RoleNameExactTone bool `json:"roleNameExactTone,omitempty"`
	RoleNameAllowSelf bool `json:"roleNameAllowSelf,omitempty"`

	// ClientDataDir 是 nexus 自有的版本数据目录（下载回来的客户端本体存放处）。
	// 首次由 internal/clientdata 选址（可用空间最大的固定盘根下 CortexNEL\client）
	// 并写回此处：不持久化的话每次启动都会重新选址，已下载的 750MB 级数据将不可见。
	ClientDataDir string `json:"clientDataDir,omitempty"`

	// BypassSystemProxy 为 true 时绕过系统代理直连（网关与下载走直连 Transport）。
	// 代理抖动会表现为 TLS 握手超时，本地代理不可达时用它兜底。
	BypassSystemProxy bool `json:"bypassSystemProxy,omitempty"`

	// ShadowEnabled 是影子端总开关（录制 + 回灌 + 影子监听）：默认关闭（零值），
	// 不用影子端时会话完全不装配录制组件、不产生录制开销；开启仅对新会话生效
	// （已在跑的会话不回溯变更，见 service.SetShadowEnabled）。
	ShadowEnabled bool `json:"shadowEnabled"`
}

// LoadSettings 读全局设置（无文件时返回默认值）。
func LoadSettings(paths *Paths) (Settings, error) {
	if !Exists(paths.ConfigFile()) {
		// 新用户默认开启凭据落盘（DR-1）：cookie 会话过期后依赖保存的凭据自动重登，
		// 前端文案承诺"会话过期后可自动重连"。DPAPI 为当前 Windows 用户系统级加密。
		return Settings{CredentialPersist: true}, nil
	}
	s, _, err := Load[Settings](paths.ConfigFile())
	return s, err
}

// SaveSettings 原子写全局设置。
func SaveSettings(paths *Paths, s Settings) error {
	return Save(filepath.Clean(paths.ConfigFile()), s)
}
