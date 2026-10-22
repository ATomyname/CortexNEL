// --userProperties / --userPropertiesEx 的取值装配。
//
// 这两个参数是网易客户端读取启动器信息的入口。其中 launcherport 写的是
// internal/clientcontrol 控制通道的端口，但它**不是**自动进服的必要条件：
// 客户端实际读的是 JVM 系统属性 -DlauncherControlPort（见 arguments.go 的
// buildJVMArgs 与 docs/facts/NETEASE-PROTOCOL.md §10.1）。这里一并写入只是为了
// 与官方启动器保持同样的参数形状。
//
// 真源：Reference/Fantnel-master/Fantnel-master/Nirvana.Game.Launcher/
// Services/Java/CommandService.cs 的 GetUserProperties / GetUserPropertiesEx。
// 参考实现是在拼命令行字符串，所以值里带 \" 转义；Go 直接以 argv 传参，
// 因此这里写入的是转义前的逻辑值（纯 JSON），由 os/exec 负责平台层转义。
package launcher

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"

	"nexus/internal/crypto"
	"nexus/internal/netease"
)

// netGameType 是 --userPropertiesEx 的 GameType 取值（真源 EnumGType.NetGame = 2）。
const netGameType = 2

// filterKeyLength 是 filterkey 的长度（真源取 32 位小写字母随机串）。
const filterKeyLength = 32

// buildUserProperties 装配 --userProperties 的值。
//
// gameid 固定为 0：参考实现同样传字面量 0 而不是真实 GameID，
// 服务器知识不经此参数下发（隔离门禁 P1/P2 也不允许启动器内联 GameID）。
func buildUserProperties(userID int64, controlPort int) string {
	props := map[string]any{
		"uid":           []any{userID, 0},
		"gameid":        []any{0, 0},
		"filterkey":     []any{randomFilterKey(), "0"},
		"filterpath":    []any{"", "0"},
		"timedelta":     []any{0, 0},
		"launchversion": []any{netease.LauncherVersion, "0"},
	}
	if controlPort > 0 {
		props["launcherport"] = []any{controlPort, 0}
	}

	raw, err := json.Marshal(props)
	if err != nil {
		return ""
	}
	return string(raw)
}

// buildUserPropertiesEx 装配 --userPropertiesEx 的值。
func buildUserPropertiesEx() string {
	// 字段名大小写按客户端约定，不可按 Go 习惯改写（真源 EntityUserPropertiesEx 的 JsonPropertyName）。
	raw, err := json.Marshal(map[string]any{
		"GameType":        netGameType,
		"isFilter":        true,
		"channel":         "netease",
		"timedelta":       0,
		"launcherVersion": netease.LauncherVersion,
	})
	if err != nil {
		return ""
	}
	return string(raw)
}

// randomFilterKey 生成 filterkey：32 位小写字母。
// 随机源不可用时退回定长占位串——该字段只用于客户端侧的过滤缓存键，
// 不参与鉴权，取值可预测不构成安全问题，但长度必须正确。
func randomFilterKey() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	buf := make([]byte, filterKeyLength)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("a", filterKeyLength)
	}
	out := make([]byte, filterKeyLength)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out)
}

// tokenArgument 返回 -DToken 参数；token 为空时返回空串表示不写该参数。
func tokenArgument(token string) string {
	if token == "" {
		return ""
	}
	return fmt.Sprintf("-DToken=%s", crypto.EncryptLauncherToken(token))
}
