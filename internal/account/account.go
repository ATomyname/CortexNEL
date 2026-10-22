// 账号实体与 token 生命周期（架构文档 §4 account 域）。
// 凭据落盘只经 TokenStore（由 internal/store DPAPI 实现，P5/DR-1）；
// 未开启落盘时 token 只存进程内存，重启即失（DR-1 默认关闭）。
package account

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"nexus/internal/store"
)

// Channel 是登录渠道（facts §6：当前主用 4399com）。
const Channel4399com = "4399com"

// Channel4399Mixed 是小号机导出的 4399mixed 会话渠道。
const Channel4399Mixed = "4399mixed"

// ErrNotFound 表示账号不存在。
var ErrNotFound = errors.New("account: 账号不存在")

// Account 是账号元数据（不含 token）。
type Account struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Channel  string `json:"channel"`
	UserID   int64  `json:"userId"`
	SDKUID   string `json:"sdkUid"`
	RoleName string `json:"roleName"`
	// TokenPersisted 表示 token 已按 DR-1 显式开启落盘（DPAPI）。
	TokenPersisted    bool   `json:"tokenPersisted"`
	Username          string `json:"username,omitempty"`
	LoginMethod       string `json:"loginMethod,omitempty"`
	EncryptedPassword string `json:"encryptedPassword,omitempty"`
	// EncryptedCookie 是 cookie/sauth 登录源的凭据（DPAPI 密文）：
	// cookie 过期时 ReLogin 用它走与登录同一条链重换 token（密码链不可用时）。
	EncryptedCookie string `json:"encryptedCookie,omitempty"`
}

// TokenStore 是凭据持久化契约（消费方定义，规范 D2）。
type TokenStore interface {
	SaveToken(accountID, token string) error
	LoadToken(accountID string) (string, error)
	DeleteToken(accountID string) error
}

// dpapiTokenStore 把 store 的 DPAPI 能力适配为 TokenStore。
// token 文件与账号元数据同目录，后缀 .token（ListIDs 只认 .json，不会泄漏进列表）。
type dpapiTokenStore struct {
	paths *store.Paths
}

// NewTokenStore 返回 DPAPI 实现。
func NewTokenStore(paths *store.Paths) TokenStore { return dpapiTokenStore{paths: paths} }

func (s dpapiTokenStore) tokenPath(id string) string {
	return filepath.Join(s.paths.Root, store.SubdirAccounts, id+".token")
}

func (s dpapiTokenStore) SaveToken(id, token string) error {
	cipher, err := store.ProtectString(token)
	if err != nil {
		return err
	}
	return store.Save(s.tokenPath(id), cipher)
}

func (s dpapiTokenStore) LoadToken(id string) (string, error) {
	cipher, _, err := store.Load[string](s.tokenPath(id))
	if err != nil {
		return "", err
	}
	return store.UnprotectString(cipher)
}

func (s dpapiTokenStore) DeleteToken(id string) error {
	err := os.Remove(s.tokenPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Manager 管理账号元数据与 token 生命周期。
type Manager struct {
	paths  *store.Paths
	tokens TokenStore
	mu     sync.RWMutex
	// memTokens 是未开启落盘时的会话内存 token（重启即失）。
	memTokens map[string]string
}

// NewManager 构造账号管理器。
func NewManager(paths *store.Paths, tokens TokenStore) *Manager {
	return &Manager{paths: paths, tokens: tokens, memTokens: make(map[string]string)}
}

// Upsert 写入账号元数据并按 opt-in 决定 token 落盘。
// a.ID 为空时生成新 ID 并回写 a（Channel/TokenPersisted 同步规范化），
// 调用方持有的实体与落库一致——值拷贝吞掉生成 ID 会让调用方拿空 ID 取 token（ErrNotFound）。
func (m *Manager) Upsert(a *Account, token string, persist bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		id := make([]byte, 8)
		if _, err := rand.Read(id); err != nil {
			return err
		}
		a.ID = hex.EncodeToString(id)
	}
	if a.Channel == "" {
		a.Channel = Channel4399com
	}
	a.TokenPersisted = persist
	if persist {
		if m.tokens == nil {
			return errors.New("account: 无凭据存储可用")
		}
		if err := m.tokens.SaveToken(a.ID, token); err != nil {
			return err
		}
		delete(m.memTokens, a.ID)
	} else {
		m.memTokens[a.ID] = token
	}
	return store.Save(m.paths.AccountFile(a.ID), a)
}

// List 返回全部账号（按 Label 排序，确定性）。
func (m *Manager) List() ([]Account, error) {
	ids, err := m.paths.ListIDs(store.SubdirAccounts)
	if err != nil {
		return nil, err
	}
	var out []Account
	for _, id := range ids {
		a, _, err := store.Load[Account](m.paths.AccountFile(id))
		if err != nil {
			return nil, fmt.Errorf("account: 读取 %s: %w", id, err)
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// Get 读取单个账号。
func (m *Manager) Get(id string) (Account, error) {
	a, _, err := store.Load[Account](m.paths.AccountFile(id))
	if err != nil {
		return Account{}, ErrNotFound
	}
	return a, nil
}

// Token 取回账号 token（来源与存储方式一致）。
func (m *Manager) Token(id string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, err := m.Get(id)
	if err != nil {
		return "", err
	}
	if a.TokenPersisted {
		if m.tokens == nil {
			return "", errors.New("account: 无凭据存储可用")
		}
		return m.tokens.LoadToken(id)
	}
	if token, ok := m.memTokens[id]; ok {
		return token, nil
	}
	return "", errors.New("account: token 未在会话内存中（应用重启后需重新登录）")
}

// ReplaceTokenIfCurrent 更新现存账号的 token，并保留其当前落盘策略。
// expected 不再是当前 token 时不写入，避免慢刷新的响应覆盖更新的登录凭据。
// 与 Remove/Upsert 串行，确保后台刷新不会复活已删除账号。
func (m *Manager) ReplaceTokenIfCurrent(id, expected, token string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.Get(id)
	if err != nil {
		return false, err
	}
	current := m.memTokens[id]
	if a.TokenPersisted {
		if m.tokens == nil {
			return false, errors.New("account: 无凭据存储可用")
		}
		current, err = m.tokens.LoadToken(id)
		if err != nil {
			return false, err
		}
	}
	if current != expected {
		return false, nil
	}
	if a.TokenPersisted {
		if err := m.tokens.SaveToken(id, token); err != nil {
			return false, err
		}
	} else {
		m.memTokens[id] = token
	}
	return true, nil
}

// UpdateLabel 更新账号备注（仅元数据落盘，不动凭据）。
func (m *Manager) UpdateLabel(id, label string) error {
	a, err := m.Get(id)
	if err != nil {
		return err
	}
	a.Label = label
	return store.Save(m.paths.AccountFile(id), a)
}

// UpdateRoleName 更新账号在服务器上的角色名。
func (m *Manager) UpdateRoleName(id, roleName string) error {
	a, err := m.Get(id)
	if err != nil {
		return err
	}
	a.RoleName = roleName
	return store.Save(m.paths.AccountFile(id), a)
}

// Remove 删除账号元数据与凭据。
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.Get(id); err != nil {
		return err
	}
	delete(m.memTokens, id)
	if m.tokens != nil {
		_ = m.tokens.DeleteToken(id)
	}
	return os.Remove(m.paths.AccountFile(id))
}
