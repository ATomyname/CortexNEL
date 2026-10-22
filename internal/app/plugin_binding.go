package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"nexus/internal/launcher"
	"nexus/internal/profile"
)

func (a *App) ListPlugins() ([]PluginDTO, error) {
	env, err := a.mainEnv()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(env.ModsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []PluginDTO{}, nil
		}
		return nil, err
	}
	mods, _ := profile.ScanMods(env.ModsDir)
	out := make([]PluginDTO, 0)
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".jar") && !strings.HasSuffix(e.Name(), ".jar.disabled")) {
			continue
		}
		name, enabled := e.Name(), true
		if strings.HasSuffix(name, ".disabled") {
			name = strings.TrimSuffix(name, ".disabled")
			enabled = false
		}
		var iid, md5 string
		for _, item := range mods {
			if item.ModPath == name {
				iid, md5 = item.IID, item.MD5
				break
			}
		}
		out = append(out, PluginDTO{FileName: e.Name(), Name: name, IID: iid, MD5: md5, Enabled: enabled})
	}
	return out, nil
}

func (a *App) TogglePlugin(fileName string) error {
	if filepath.Base(fileName) != fileName || fileName == "" {
		return fmt.Errorf("插件文件名无效")
	}
	env, err := a.mainEnv()
	if err != nil {
		return err
	}
	from := filepath.Join(env.ModsDir, fileName)
	to := from + ".disabled"
	if strings.HasSuffix(fileName, ".disabled") {
		to = filepath.Join(env.ModsDir, strings.TrimSuffix(fileName, ".disabled"))
	}
	if _, err := os.Stat(from); err != nil {
		return err
	}
	return os.Rename(from, to)
}

// OpenModsDir 在系统资源管理器中打开当前客户端 mods 目录。
func (a *App) OpenModsDir() error {
	env, err := a.mainEnv()
	if err != nil {
		return err
	}
	_ = os.MkdirAll(env.ModsDir, 0755)
	return exec.Command("explorer", env.ModsDir).Start()
}

// GetClientEnvInfo 获取当前白端环境路径与自动探测信息。
func (a *App) GetClientEnvInfo() (EnvDTO, error) {
	env, err := a.mainEnv()
	if err != nil {
		return EnvDTO{}, err
	}
	var preferredVersion string
	if a.settings.MainServerID != "" {
		if s, err := a.profiles.Server(a.settings.MainServerID); err == nil {
			preferredVersion = s.McVersion
		}
	}
	javaPath := env.JavaPath
	if javaPath == "" {
		if j, err := launcher.DetectJavaForVersion(preferredVersion); err == nil && j != "" {
			javaPath = j
		}
	}
	return EnvDTO{
		ID:        env.ID,
		Name:      env.Name,
		ClientDir: env.ClientDir,
		ModsDir:   env.ModsDir,
		JavaPath:  javaPath,
		JvmArgs:   env.JvmArgs,
	}, nil
}

func (a *App) mainEnv() (profile.Env, error) {
	if a.settings.MainEnvID != "" {
		if e, err := a.profiles.Env(a.settings.MainEnvID); err == nil {
			return e, nil
		}
	}
	// 动态感知当前主服务器（架构规范：支持不同服务器切换专属组件与 Mods 环境）
	var gameID string
	var preferredVersion string
	serverName := "默认环境 (自动匹配)"
	if a.settings.MainServerID != "" {
		if s, err := a.profiles.Server(a.settings.MainServerID); err == nil {
			gameID = s.GameID
			if gameID == "" {
				gameID = s.ID
			}
			preferredVersion = s.McVersion
			if s.Name != "" {
				serverName = s.Name + " (专属环境)"
			}
		}
	}

	clientDir, modsDir, _ := launcher.DetectServerClientDir(gameID, preferredVersion)
	_ = os.MkdirAll(modsDir, 0755)
	return profile.Env{
		Name:      serverName,
		ClientDir: clientDir,
		ModsDir:   modsDir,
	}, nil
}
