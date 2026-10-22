// 白端客户端目录与运行版本探测。
//
// 网易客户端的落盘结构（实测）：
//   - 公共版本与库：<盘>:\MCLDownload\Game\.minecraft（versions / libraries / assets）
//   - 各服独立运行目录：<盘>:\MCLDownload\Game\<gameId>\.minecraft
//   - 各服组件缓存：<盘>:\MCLDownload\cache\game\<gameId>\.minecraft\mods
//   - 整条产品线可装在自己的根：<盘>:\netease_minecraft_neoforge（EC · NeoForge）
//
// 根的枚举与选择见 clientroots.go；本文件负责候选顺序下的目录与版本解析。
package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// neteaseInstallRoots 返回候选盘根（非 Windows 平台为空）。
//
// 盘符动态枚举（固定盘，见 drives_windows.go），不再白名单 —— 写死
// D:/C:/E:/F:/G: 会让装在 H:/I: 等其余固定盘上的已有客户端探测不到，
// 白白走整包重下。优先级：网易启动器默认装 D 盘，其次系统盘 C，
// 其余按盘符序，保证探测顺序稳定可复现。
func neteaseInstallRoots() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	drives := fixedDrives()
	ordered := make([]rune, 0, len(drives))
	for _, want := range []rune{'D', 'C'} {
		for _, d := range drives {
			if d == want {
				ordered = append(ordered, d)
			}
		}
	}
	for _, d := range drives {
		if d != 'D' && d != 'C' {
			ordered = append(ordered, d)
		}
	}
	roots := make([]string, 0, len(ordered))
	for _, d := range ordered {
		roots = append(roots, string(d)+":"+string(filepath.Separator))
	}
	return roots
}

// neteaseGameRoot 返回首个存在的 MCLDownload\Game 目录，未找到时返回空串。
func neteaseGameRoot() string {
	for _, root := range neteaseInstallRoots() {
		cand := filepath.Join(root, "MCLDownload", "Game")
		if isExistingDir(cand) {
			return cand
		}
	}
	return ""
}

// DetectClientDir 探测公共 .minecraft 目录与可用版本。
func DetectClientDir(preferredVersion string) (clientDir, version string) {
	version = preferredVersion
	if version == "" {
		version = defaultMCVersion
	}

	candidates := append(clientRootCandidates(), ".")
	for _, dir := range candidates {
		if !isExistingDir(dir) {
			continue
		}
		if resolved, ok := resolveVersionInDir(dir, version); ok {
			return dir, resolved
		}
	}
	return ".", version
}

// DetectServerClientDir 探测某个服务器专属的 .minecraft、mods 目录与运行版本。
// gameID 为空或非 Windows 时退化为公共目录。
func DetectServerClientDir(gameID, preferredVersion string) (clientDir, modsDir, version string) {
	clientDir, version = DetectClientDir(preferredVersion)
	modsDir = filepath.Join(clientDir, "mods")

	if gameID == "" || runtime.GOOS != "windows" {
		return clientDir, modsDir, version
	}

	// 各服组件来源（缓存目录优先，其次按服独立运行目录）。
	// 盘根列表本身即优先级（neteaseInstallDrives 按实测命中概率排序），取首个命中。
	// 旧实现在盘根循环里逐个赋值，最后一个盘会覆盖前面的 —— 实测 C 盘与 D 盘都有
	// 同一 gameID 的组件缓存，结果 modsDir 落到 C 盘而 clientDir 在 D 盘，
	// 也与 findServerModsSource 的「首个优先」互相矛盾。
	if cached := findServerModsSource(gameID); cached != "" {
		modsDir = cached
	}

	// 各服独立运行目录若自带 versions，则整体切换到它。
	for _, root := range neteaseInstallRoots() {
		serverDir := filepath.Join(root, "MCLDownload", "Game", gameID, ".minecraft")
		if !isExistingDir(serverDir) || !isExistingDir(filepath.Join(serverDir, "versions")) {
			continue
		}
		if resolved, ok := resolveVersionInDir(serverDir, preferredVersion); ok {
			clientDir, version = serverDir, resolved
		}
		break
	}

	// 按服独立目录未命中时，按组件重合度在各产品线客户端根里选（多根安装）。
	// 选出的根若不含目标版本则保留默认根 —— 宁可根旧，也不要"根对了但版本缺失"。
	if root := selectClientRootByMods(clientRootCandidates(), modsDir); root != "" && root != clientDir {
		if resolved, ok := resolveVersionInDir(root, preferredVersion); ok {
			clientDir, version = root, resolved
		}
	}

	return clientDir, modsDir, version
}

// resolveVersionInDir 在 dir/versions 下挑一个可用版本：
// 完全匹配 preferred → 以 preferred 为前缀 → 1.20 系列 → 任意首个。
func resolveVersionInDir(dir, preferred string) (string, bool) {
	versionsDir := filepath.Join(dir, "versions")
	if preferred != "" && isExistingDir(filepath.Join(versionsDir, preferred)) {
		return preferred, true
	}
	entries, err := os.ReadDir(versionsDir)
	if err != nil || len(entries) == 0 {
		return "", false
	}
	if preferred != "" {
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), preferred) {
				return e.Name(), true
			}
		}
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), defaultMCVersion) {
			return e.Name(), true
		}
	}
	for _, e := range entries {
		if e.IsDir() {
			return e.Name(), true
		}
	}
	return "", false
}

// isExistingDir 判断路径存在且是目录。
func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
