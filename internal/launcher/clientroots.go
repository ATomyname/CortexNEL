// 网易客户端根的枚举。
//
// 网易启动器把不同产品线装在不同的根目录下，每个根都是一份完整客户端安装
// （versions / libraries / assets / mods）：
//
//	<盘根>\MCLDownload\Game\.minecraft    Java 线（实测：1.20 / HeyPixel）
//	<盘根>\netease_minecraft_neoforge     EC · NeoForge 线（实测：1.21.8，自带 ec-config / ecguard）
//
// 第二条线的根名不是稳定约定，因此这里按「是否具备客户端安装特征」识别，
// 不按目录名白名单匹配 —— 白名单会在网易改名时静默失效。
package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ClientRoots 返回候选客户端根（导出版，供「在客户端目录里找东西」的调用方复用）。
//
// 只读枚举、保持既有优先级；调用方不应假设顺序以外的语义。
func ClientRoots() []string { return clientRootCandidates() }

// clientRootCandidates 返回候选客户端根，按默认优先级排列。
//
// 顺序有意义：首项是历史默认根（MCLDownload\Game\.minecraft），
// 只在有更具体的服务器证据时才被后续根取代（见 selectClientRootByMods）。
func clientRootCandidates() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(dir string) {
		key := filepath.Clean(dir)
		if key == "" || seen[key] || !isExistingDir(dir) {
			return
		}
		seen[key] = true
		out = append(out, dir)
	}

	for _, root := range neteaseInstallRoots() {
		add(filepath.Join(root, "MCLDownload", "Game", ".minecraft"))

		// 盘根下的独立安装：只看一层子目录，逐目录按特征判定，
		// 不做递归扫描（盘根下递归的代价不可控）。
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			cand := filepath.Join(root, e.Name())
			if isClientInstall(cand) {
				add(cand)
			}
		}
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		add(filepath.Join(appdata, ".minecraft"))
	}
	return out
}

// isClientInstall 判断目录是否是一份客户端安装根（有版本清单目录与依赖库目录）。
func isClientInstall(dir string) bool {
	return isExistingDir(filepath.Join(dir, "versions")) &&
		isExistingDir(filepath.Join(dir, "libraries"))
}

// selectClientRootByMods 在候选根里选出与该服组件缓存最匹配的一个；无证据时返回空串。
//
// 依据：网易把某条产品线的组件同时放在两处 —— 服务器组件缓存
// `cache\game\<gameID>\.minecraft\mods` 与那条线客户端根自己的 `mods`。不同线的组件集
// 不同（实测 Java 线为 `@2@8` 资产组件，EC 线为 ecguard / bedrockloader 等具名组件），
// 因此「与该服缓存 jar 名重合最多的根」就是该服的安装根。
//
// 平局取候选顺序靠前者（默认根排在前面），因此没有新证据时行为与单根时代一致；
// 重合数为 0 视为无证据，返回空串让调用方保留默认根。
func selectClientRootByMods(candidates []string, serverModsDir string) string {
	wanted := jarNameSet(serverModsDir)
	if len(wanted) == 0 {
		return ""
	}
	best, bestHits := "", 0
	for _, cand := range candidates {
		hits := 0
		for name := range jarNameSet(filepath.Join(cand, "mods")) {
			if wanted[name] {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = cand, hits
		}
	}
	return best
}

// jarNameSet 返回目录下的 jar 文件名集合（目录不存在时为空集合）。
func jarNameSet(dir string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		out[e.Name()] = true
	}
	return out
}
