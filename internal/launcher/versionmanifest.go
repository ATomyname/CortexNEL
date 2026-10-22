// 版本清单（versions/<ver>/<ver>.json）解析。
//
// 网易基础包在清单里额外给出两个私有键 —— jvm_arguments / parameter_arguments ——
// 直接是成品参数串（参考实现 Fantnel CommandService.BuildCommand 以它们为模板）。
// 原版清单则给 minecraftArguments（旧格式）或 arguments.game/jvm（新格式）。
// 本文件只负责读取与归一，不装配参数（那属 arguments.go）。
package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// versionManifest 是版本清单中本包实际使用的字段子集。
//
// 有意不解析 downloads.classifiers 与 libraries[].natives：
// Windows 白端的 natives 由网易基础包预先解压到 versions/<ver>/natives，
// 无需运行期解压（参考实现同样只在非 Windows 平台才解压 natives）。
type versionManifest struct {
	ID         string             `json:"id"`
	MainClass  string             `json:"mainClass"`
	Assets     string             `json:"assets"`
	AssetIndex *manifestAssetRef  `json:"assetIndex"`
	Libraries  []manifestLibrary  `json:"libraries"`
	Arguments  *manifestArguments `json:"arguments"`

	// GameArgumentTemplate 是原版旧格式启动参数串。
	GameArgumentTemplate string `json:"minecraftArguments"`
	// NetEaseJVMArguments 是网易私有键 jvm_arguments（成品 JVM 参数串）。
	NetEaseJVMArguments string `json:"jvm_arguments"`
	// NetEaseGameArguments 是网易私有键 parameter_arguments（成品游戏参数串）。
	NetEaseGameArguments string `json:"parameter_arguments"`
}

type manifestAssetRef struct {
	ID string `json:"id"`
}

type manifestLibrary struct {
	Name      string             `json:"name"`
	Downloads *manifestDownloads `json:"downloads"`
	Rules     []manifestRule     `json:"rules"`
}

type manifestDownloads struct {
	Artifact *manifestArtifact `json:"artifact"`
}

type manifestArtifact struct {
	Path string `json:"path"`
}

type manifestRule struct {
	Action string          `json:"action"`
	OS     *manifestRuleOS `json:"os"`
}

type manifestRuleOS struct {
	Name string `json:"name"`
}

// manifestArguments 是原版新格式参数表。元素可能是字符串或带 rules 的对象，
// 本包只取无条件的字符串项（带 rules 的条件项属平台特化，Windows 白端不需要）。
type manifestArguments struct {
	Game []json.RawMessage `json:"game"`
	JVM  []json.RawMessage `json:"jvm"`
}

// readVersionManifest 读取并解析版本清单；文件缺失或格式非法时返回 ok=false。
func readVersionManifest(clientDir, version string) (versionManifest, bool) {
	path := filepath.Join(versionDir(clientDir, version), version+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return versionManifest{}, false
	}
	var m versionManifest
	// 先剥离尾逗号再解析：网易基础包的清单带尾逗号（实测 1.12.2.json 的
	// libraries 数组末尾一处），RFC 8259 不允许，encoding/json 会整体解析失败。
	// 这一处容错换来的是整份清单可用 —— 否则调用方拿不到 libraries 与 mainClass，
	// classpath 会退化成递归扫描全版本 libraries 目录（见 resolveClassPath）。
	if err := json.Unmarshal(stripTrailingCommas(data), &m); err != nil {
		return versionManifest{}, false
	}
	return m, true
}

// stripTrailingCommas 去掉 JSON 里 `]` / `}` 之前多余的逗号。
//
// 只动数组与对象结尾处的逗号，字符串字面量内的逗号不受影响
// （网易清单的 minecraftArguments 里就有逗号）。不校验 JSON 本身：
// 剥离后仍然非法的输入交给 json.Unmarshal 去报错。
func stripTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c != ',' {
			out = append(out, c)
			continue
		}
		// 逗号：向后跳过空白，若紧接着是 ] 或 } 则丢弃这个逗号。
		j := i + 1
		for j < len(data) && isJSONSpace(data[j]) {
			j++
		}
		if j < len(data) && (data[j] == ']' || data[j] == '}') {
			continue
		}
		out = append(out, c)
	}
	return out
}

// isJSONSpace 判断 JSON 空白字符。
func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// versionDir 是某个版本的目录（versions/<ver>）。
func versionDir(clientDir, version string) string {
	return filepath.Join(clientDir, "versions", version)
}

// mainClass 返回清单声明的入口类，缺失时回落到原版入口类。
func (m versionManifest) mainClass() string {
	if m.MainClass != "" {
		return m.MainClass
	}
	return defaultMainClass
}

// assetIndexID 返回资源索引 ID：优先 assetIndex.id，其次 assets，
// 最后按版本号推断（1.20.x 系列共用 "1.20" 索引）。
func (m versionManifest) assetIndexID(version string) string {
	if m.AssetIndex != nil && m.AssetIndex.ID != "" {
		return m.AssetIndex.ID
	}
	if m.Assets != "" {
		return m.Assets
	}
	if strings.HasPrefix(version, "1.20") {
		return "1.20"
	}
	return version
}

// gameArgumentTemplate 返回游戏参数模板：网易私有键优先，其次原版旧格式串，
// 最后把原版新格式数组拼成串。三者都为空时返回空串（调用方合成原版参数序列）。
func (m versionManifest) gameArgumentTemplate() string {
	if s := strings.TrimSpace(m.NetEaseGameArguments); s != "" {
		return s
	}
	if s := strings.TrimSpace(m.GameArgumentTemplate); s != "" {
		return s
	}
	if m.Arguments != nil {
		return strings.Join(plainStrings(m.Arguments.Game), " ")
	}
	return ""
}

// jvmArgumentTemplate 返回 JVM 参数模板：网易私有键优先，其次原版新格式数组。
func (m versionManifest) jvmArgumentTemplate() string {
	if s := strings.TrimSpace(m.NetEaseJVMArguments); s != "" {
		return s
	}
	if m.Arguments != nil {
		return strings.Join(plainStrings(m.Arguments.JVM), " ")
	}
	return ""
}

// plainStrings 只取参数数组里的无条件字符串项，跳过带 rules 的对象项。
func plainStrings(raw []json.RawMessage) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			out = append(out, s)
		}
	}
	return out
}

// isRuleAllowed 按 libraries[].rules 判定当前平台是否需要该库。
func isRuleAllowed(rules []manifestRule) bool {
	if len(rules) == 0 {
		return true
	}
	current := currentOSName()
	allowed := false
	for _, r := range rules {
		matches := r.OS == nil || r.OS.Name == current
		switch r.Action {
		case "allow":
			if matches {
				allowed = true
			}
		case "disallow":
			if matches {
				allowed = false
			}
		}
	}
	return allowed
}

// currentOSName 返回版本清单口径的平台名。
func currentOSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "osx"
	case "linux":
		return "linux"
	default:
		return "windows"
	}
}
