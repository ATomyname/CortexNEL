// DTO：绑定层对前端的稳定形状（规范 D1：绑定层只做映射与转发）。
package app

import "nexus/internal/account"

// AccountDTO 是账号元数据（永不包含 token，规范 S3）。
type AccountDTO struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Channel        string `json:"channel"`
	UserID         int64  `json:"userId"`
	RoleName       string `json:"roleName"`
	TokenPersisted bool   `json:"tokenPersisted"`
}

func toAccountDTO(a account.Account) AccountDTO {
	return AccountDTO{
		ID:             a.ID,
		Label:          a.Label,
		Channel:        a.Channel,
		UserID:         a.UserID,
		RoleName:       a.RoleName,
		TokenPersisted: a.TokenPersisted,
	}
}

// ServerDTO 是服务器档案。
type ServerDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GameID    string `json:"gameId"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	McVersion string `json:"mcVersion,omitempty"`
	IsRental  bool   `json:"isRental,omitempty"`
}

// EnvDTO 是环境档案。
type EnvDTO struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ClientDir  string   `json:"clientDir"`
	ModsDir    string   `json:"modsDir"`
	JavaPath   string   `json:"javaPath"`
	JvmArgs    []string `json:"jvmArgs"`
	LaunchArgs []string `json:"launchArgs"`
}

// SettingsDTO 是全局设置。
type SettingsDTO struct {
	MainAccountID     string `json:"mainAccountId"`
	MainEnvID         string `json:"mainEnvId"`
	MainServerID      string `json:"mainServerId"`
	CredentialPersist bool   `json:"credentialPersist"`
	HideZeroPlayers   bool   `json:"hideZeroPlayers"`
	HideMaintaining   bool   `json:"hideMaintaining"`
	HideBedrockOnly   bool   `json:"hideBedrockOnly"`
	AllowLAN          bool   `json:"allowLan"`
	LanPort           int    `json:"lanPort,omitempty"`
	RoleNamePrefix    string `json:"roleNamePrefix,omitempty"`
	RoleNameSuffix    string `json:"roleNameSuffix,omitempty"`
	RoleNameStyle     string `json:"roleNameStyle,omitempty"`
	RoleNameBaseWord  string `json:"roleNameBaseWord,omitempty"`
	// RoleNameDigits 不能加 omitempty：0 是用户的真实选择（"不添加数字"）。
	// store 侧已修（settings.go），DTO 侧同样要修——否则读出来是 0、编 DTO 时
	// 又被省略，前端 Object.assign 不拷贝缺失键，照样退回默认值。
	RoleNameDigits    int  `json:"roleNameDigits"`
	RoleNameHomophone bool `json:"roleNameHomophone,omitempty"`
	RoleNameExactTone bool `json:"roleNameExactTone,omitempty"`
	RoleNameAllowSelf bool `json:"roleNameAllowSelf,omitempty"`
	// ClientDataDir 是 nexus 自有的版本数据目录（下载的客户端本体存放处）。
	// 留空时由 internal/clientdata 选址并写回设置。
	ClientDataDir string `json:"clientDataDir,omitempty"`
	// BypassSystemProxy 为 true 时绕过系统代理直连（网关与下载）。
	BypassSystemProxy bool `json:"bypassSystemProxy,omitempty"`
	// ShadowEnabled 是影子端总开关（录制 + 回灌 + 影子监听）。
	// 不能加 omitempty：false 是用户的真实选择（默认关闭），省略会让前端
	// Object.assign 不拷贝缺失键、退回自建初值（同 RoleNameDigits 的教训）。
	ShadowEnabled bool `json:"shadowEnabled"`
}

// GenerateRoleNameOptions 是角色随机命名请求参数。
type GenerateRoleNameOptions struct {
	Prefix       string `json:"prefix,omitempty"`
	Suffix       string `json:"suffix,omitempty"`
	Style        string `json:"style,omitempty"`
	BaseWord     string `json:"baseWord,omitempty"`
	Digits       int    `json:"digits,omitempty"`
	MaxLength    int    `json:"maxLength,omitempty"`
	UseHomophone bool   `json:"useHomophone,omitempty"`
	ExactTone    bool   `json:"exactTone,omitempty"`
	AllowSelf    bool   `json:"allowSelf,omitempty"`
}

// StartSessionResult 是会话启动结果。
type StartSessionResult struct {
	// SessionID 是本次会话的 ID（sess-N），停止与事件路由都以它为准。
	SessionID  string   `json:"sessionId"`
	ListenAddr string   `json:"listenAddr"`
	LocalAddr  string   `json:"localAddr,omitempty"`
	LanAddrs   []string `json:"lanAddrs,omitempty"`
	PrimaryLan string   `json:"primaryLan,omitempty"`
	IsLAN      bool     `json:"isLan,omitempty"`
}

// SessionInfoDTO 是运行中代理会话的快照（代理管理页列表行）。
type SessionInfoDTO struct {
	SessionID    string `json:"sessionId"`
	ServerID     string `json:"serverId"`
	AccountID    string `json:"accountId"`
	RoleName     string `json:"roleName,omitempty"`
	ListenAddr   string `json:"listenAddr"`
	Stage        string `json:"stage"`
	LaunchMirror bool   `json:"launchMirror"`
	// StartedAt 是 RFC3339 时间戳，前端自行格式化为"已运行 N 分钟"。
	StartedAt string `json:"startedAt"`
}

// NetworkServerDTO 是网络服列表项（实时网易数据，facts §5.3）。
type NetworkServerDTO struct {
	EntityID      string `json:"entityId"`
	Name          string `json:"name"`
	OnlineCount   string `json:"onlineCount"`
	BriefSummary  string `json:"briefSummary"`
	ImageURL      string `json:"imageUrl"`
	McVersion     string `json:"mcVersion,omitempty"`
	IsBedrockOnly bool   `json:"isBedrockOnly"`
	IsMaintaining bool   `json:"isMaintaining"`
}

// NetworkServerDetailDTO 是网络服详情（封面/简介/版本）。
type NetworkServerDetailDTO struct {
	EntityID      string   `json:"entityId"`
	Name          string   `json:"name"`
	ImageURL      string   `json:"imageUrl"`
	Description   string   `json:"description"`
	McVersion     string   `json:"mcVersion,omitempty"`
	McVersionList []string `json:"mcVersionList,omitempty"`
	IsBedrockOnly bool     `json:"isBedrockOnly"`
	IsMaintaining bool     `json:"isMaintaining"`
}

// NetworkAddressDTO 是网络服上游地址（进服/存档案用）。
type NetworkAddressDTO struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type RoleDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SkinDTO struct {
	EntityID   string `json:"entityId"`
	Name       string `json:"name"`
	PreviewURL string `json:"previewUrl"`
}

type RentalServerDTO struct {
	EntityID     string `json:"entityId"`
	Name         string `json:"name"`
	ServerName   string `json:"serverName"`
	HasPassword  string `json:"hasPassword"`
	McVersion    string `json:"mcVersion"`
	PlayerCount  uint   `json:"playerCount"`
	Capacity     uint   `json:"capacity"`
	ImageURL     string `json:"imageUrl"`
	BriefSummary string `json:"briefSummary"`
}

type PluginDTO struct {
	FileName string `json:"fileName"`
	Name     string `json:"name"`
	IID      string `json:"iid"`
	MD5      string `json:"md5"`
	Enabled  bool   `json:"enabled"`
}
