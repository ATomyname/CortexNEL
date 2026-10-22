// 档案域绑定（架构文档 §10.1）：服务器 / 环境 CRUD。
package app

import (
	"fmt"

	"nexus/internal/launcher"
	"nexus/internal/profile"
)

// DetectJava 自动探测本机 Java 可执行文件，不修改环境档案。
func (a *App) DetectJava() (string, error) {
	return launcher.DetectJava()
}

// ListServers 列出服务器档案。
func (a *App) ListServers() ([]ServerDTO, error) {
	list, err := a.profiles.ListServers()
	if err != nil {
		return nil, err
	}
	out := make([]ServerDTO, 0, len(list))
	for _, s := range list {
		out = append(out, ServerDTO{
			ID:        s.ID,
			Name:      s.Name,
			GameID:    s.GameID,
			Host:      s.Host,
			Port:      s.Port,
			McVersion: s.McVersion,
			IsRental:  s.IsRental,
		})
	}
	return out, nil
}

// SaveServer 写入服务器档案。
func (a *App) SaveServer(dto ServerDTO) error {
	if dto.ID == "" || dto.Name == "" || dto.Host == "" {
		return fmt.Errorf("ID / 名称 / 上游地址不能为空")
	}
	return a.profiles.SaveServer(profile.Server{
		ID:        dto.ID,
		Name:      dto.Name,
		GameID:    dto.GameID,
		Host:      dto.Host,
		Port:      dto.Port,
		McVersion: dto.McVersion,
		IsRental:  dto.IsRental,
	})
}

func (a *App) RemoveServer(id string) error {
	if id == "" {
		return fmt.Errorf("服务器 ID 不能为空")
	}
	if a.settings.MainServerID == id {
		a.settings.MainServerID = ""
		if err := a.saveSettings(); err != nil {
			return err
		}
	}
	return a.profiles.RemoveServer(id)
}

// ListEnvs 列出环境档案。
func (a *App) ListEnvs() ([]EnvDTO, error) {
	list, err := a.profiles.ListEnvs()
	if err != nil {
		return nil, err
	}
	out := make([]EnvDTO, 0, len(list))
	for _, e := range list {
		out = append(out, EnvDTO{
			ID: e.ID, Name: e.Name, ClientDir: e.ClientDir, ModsDir: e.ModsDir,
			JavaPath: e.JavaPath, JvmArgs: e.JvmArgs, LaunchArgs: e.LaunchArgs,
		})
	}
	return out, nil
}

// SaveEnv 写入环境档案。
func (a *App) SaveEnv(dto EnvDTO) error {
	if dto.ID == "" || dto.Name == "" {
		return fmt.Errorf("ID / 名称不能为空")
	}
	return a.profiles.SaveEnv(profile.Env{
		ID: dto.ID, Name: dto.Name, ClientDir: dto.ClientDir, ModsDir: dto.ModsDir,
		JavaPath: dto.JavaPath, JvmArgs: dto.JvmArgs, LaunchArgs: dto.LaunchArgs,
	})
}

func (a *App) RemoveEnv(id string) error {
	if id == "" {
		return fmt.Errorf("环境 ID 不能为空")
	}
	if a.settings.MainEnvID == id {
		a.settings.MainEnvID = ""
		if err := a.saveSettings(); err != nil {
			return err
		}
	}
	return a.profiles.RemoveEnv(id)
}
