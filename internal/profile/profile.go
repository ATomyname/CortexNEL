// 档案实体：账号 / 环境 / 服务器（架构文档 §4 profile 域），版本化落盘。
package profile

import (
	"errors"
	"fmt"
	"os"

	"nexus/internal/store"
)

// Server 是服务器档案（含 GameID 与上游地址；GameID 字面量由用户档案提供，P2）。
type Server struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GameID    string `json:"gameId"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	McVersion string `json:"mcVersion,omitempty"`
	IsRental  bool   `json:"isRental,omitempty"`
}

// Env 是环境档案：镜像客户端路径与启动参数模板。
// LaunchArgs 支持 {{userId}} / {{port}} 占位（launcher.go 替换）。
type Env struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ClientDir  string   `json:"clientDir"`
	ModsDir    string   `json:"modsDir"`
	JavaPath   string   `json:"javaPath"`
	JvmArgs    []string `json:"jvmArgs"`
	LaunchArgs []string `json:"launchArgs"`
}

// Manager 管理档案落盘。
type Manager struct {
	paths *store.Paths
}

// NewManager 构造档案管理器。
func NewManager(paths *store.Paths) *Manager {
	return &Manager{paths: paths}
}

// SaveServer 写入服务器档案。
func (m *Manager) SaveServer(s Server) error {
	if s.ID == "" {
		return fmt.Errorf("profile: 服务器档案缺少 ID")
	}
	return store.Save(m.paths.ServerFile(s.ID), s)
}

// Server 读取服务器档案。
func (m *Manager) Server(id string) (Server, error) {
	s, _, err := store.Load[Server](m.paths.ServerFile(id))
	return s, err
}

// ListServers 列出全部服务器档案。
func (m *Manager) ListServers() ([]Server, error) {
	ids, err := m.paths.ListIDs(store.SubdirServers)
	if err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(ids))
	for _, id := range ids {
		s, _, err := store.Load[Server](m.paths.ServerFile(id))
		if err != nil {
			return nil, fmt.Errorf("profile: 读取服务器 %s: %w", id, err)
		}
		out = append(out, s)
	}
	return out, nil
}

func (m *Manager) RemoveServer(id string) error {
	if id == "" {
		return errors.New("profile: 服务器 ID 不能为空")
	}
	if err := os.Remove(m.paths.ServerFile(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// SaveEnv 写入环境档案。
func (m *Manager) SaveEnv(e Env) error {
	if e.ID == "" {
		return fmt.Errorf("profile: 环境档案缺少 ID")
	}
	return store.Save(m.paths.ProfileFile(e.ID), e)
}

// Env 读取环境档案。
func (m *Manager) Env(id string) (Env, error) {
	e, _, err := store.Load[Env](m.paths.ProfileFile(id))
	return e, err
}

// ListEnvs 列出全部环境档案。
func (m *Manager) ListEnvs() ([]Env, error) {
	ids, err := m.paths.ListIDs(store.SubdirProfiles)
	if err != nil {
		return nil, err
	}
	out := make([]Env, 0, len(ids))
	for _, id := range ids {
		e, _, err := store.Load[Env](m.paths.ProfileFile(id))
		if err != nil {
			return nil, fmt.Errorf("profile: 读取环境 %s: %w", id, err)
		}
		out = append(out, e)
	}
	return out, nil
}

func (m *Manager) RemoveEnv(id string) error {
	if id == "" {
		return errors.New("profile: 环境 ID 不能为空")
	}
	if err := os.Remove(m.paths.ProfileFile(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
