package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nexus/internal/account"
	"nexus/internal/store"
)

// SmallMachineImport 是小号机复制文本的结构化结果。
type SmallMachineImport struct {
	Channel  string
	Username string
	Password string
	SAuth    string
}

// ParseSmallMachineImport 解析小号机导出的多行文本。
// 支持中文字段名和字段之间的任意空白，sauth_json 可以是最后一行 JSON
// 或者混在其他文本中的 JSON 行；返回的 SAuth 保留原始包装，便于后续重登。
func ParseSmallMachineImport(raw string) (SmallMachineImport, error) {
	var out SmallMachineImport
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "{") {
			var payload struct {
				SAuth string `json:"sauth_json"`
			}
			if err := json.Unmarshal([]byte(line), &payload); err == nil && strings.TrimSpace(payload.SAuth) != "" {
				out.SAuth = strings.TrimSpace(payload.SAuth)
				continue
			}
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			key, value, ok = strings.Cut(line, "：")
		}
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimSuffix(key, "："))
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "channel", "渠道":
			out.Channel = value
		case "账号", "帐号", "account", "username", "user":
			out.Username = value
		case "密码", "password", "pass":
			out.Password = value
		}
	}
	if out.Channel == "" {
		return SmallMachineImport{}, errors.New("小号机导入缺少 Channel")
	}
	if out.Username == "" {
		return SmallMachineImport{}, errors.New("小号机导入缺少账号")
	}
	if out.Password == "" {
		return SmallMachineImport{}, errors.New("小号机导入缺少密码")
	}
	return out, nil
}

// ImportSmallMachine 优先使用小号机 sauth 快速登录；没有 sauth 时走账号密码登录。
func (s *LoginService) ImportSmallMachine(ctx context.Context, raw string, persistCred bool) (account.Account, error) {
	parsed, err := ParseSmallMachineImport(raw)
	if err != nil {
		return account.Account{}, err
	}
	base := account.Account{
		Channel:     parsed.Channel,
		Username:    parsed.Username,
		LoginMethod: "4399mixed",
		Label:       parsed.Username,
	}
	var acct account.Account
	if parsed.SAuth != "" {
		acct, err = s.LoginWithAccount(ctx, base, parsed.SAuth, persistCred)
	} else {
		acct, err = s.Login4399PasswordWithAccount(ctx, base, parsed.Password, persistCred)
	}
	if err != nil {
		return account.Account{}, err
	}
	if persistCred && parsed.SAuth != "" {
		encPass, encErr := store.ProtectString(parsed.Password)
		if encErr != nil {
			return account.Account{}, fmt.Errorf("保存小号机密码失败: %w", encErr)
		}
		acct.EncryptedPassword = encPass
		token, tokenErr := s.accounts.Token(acct.ID)
		if tokenErr != nil {
			return account.Account{}, fmt.Errorf("读取导入账号令牌失败: %w", tokenErr)
		}
		if err := s.accounts.Upsert(&acct, token, true); err != nil {
			return account.Account{}, fmt.Errorf("保存小号机凭据失败: %w", err)
		}
	}
	return acct, nil
}
