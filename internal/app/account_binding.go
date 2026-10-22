// 账号域绑定（架构文档 §10.1）：Login / ListAccounts / SwitchAccount / Logout / 4399 密码登录。
// 4399 密码登录的识别/重试策略在 service 层（login4399.go），此处只做映射转发（D1）。
package app

import "fmt"

// Login 用 cookie 完成登录并落库（persistCred 由设置页 DR-1 opt-in 决定）。
func (a *App) Login(label, cookie string, persistCred bool) (AccountDTO, error) {
	if cookie == "" {
		return AccountDTO{}, fmt.Errorf("Cookie 不能为空")
	}
	acct, err := a.logins.Login(a.ctx, label, cookie, persistCred)
	if err != nil {
		return AccountDTO{}, err
	}
	return toAccountDTO(acct), nil
}

// ImportSmallMachine 导入小号机复制的 Channel/账号/密码/sauth_json 文本。
func (a *App) ImportSmallMachine(raw string, persistCred bool) (AccountDTO, error) {
	if raw == "" {
		return AccountDTO{}, fmt.Errorf("小号机导入内容不能为空")
	}
	acct, err := a.logins.ImportSmallMachine(a.ctx, raw, persistCred)
	if err != nil {
		return AccountDTO{}, err
	}
	return toAccountDTO(acct), nil
}

// ListAccounts 列出账号（不含 token）。
func (a *App) ListAccounts() ([]AccountDTO, error) {
	list, err := a.accounts.List()
	if err != nil {
		return nil, err
	}
	out := make([]AccountDTO, 0, len(list))
	for _, acct := range list {
		out = append(out, toAccountDTO(acct))
	}
	return out, nil
}

// SwitchAccount 切换主账号（写入全局设置）。
func (a *App) SwitchAccount(id string) error {
	if id == "" {
		return fmt.Errorf("账号 ID 不能为空")
	}
	if _, err := a.accounts.Get(id); err != nil {
		return fmt.Errorf("账号不存在")
	}
	a.settings.MainAccountID = id
	return a.saveSettings()
}

// Login4399 用 4399 账号密码完成登录（4399com OAuth 链 + 验证码自动识别，facts §6.1.1）。
// 失败返回可读错误；验证码类错误已在 service 层自动换码重试。
func (a *App) Login4399(accountName, password, label string, persist bool) (AccountDTO, error) {
	acct, err := a.logins.Login4399Password(a.ctx, accountName, password, label, persist)
	if err != nil {
		return AccountDTO{}, err
	}
	return toAccountDTO(acct), nil
}

// LoginX19 用网易邮箱账号密码完成登录（mPay 设备 + 邮箱链，facts §6.3）。
func (a *App) LoginX19(email, password, label string, persist bool) (AccountDTO, error) {
	acct, err := a.logins.LoginX19(a.ctx, email, password, label, persist)
	if err != nil {
		return AccountDTO{}, err
	}
	return toAccountDTO(acct), nil
}

// RequestX19SMS 发送 mPay 手机号登录短信验证码。
func (a *App) RequestX19SMS(phone string) error {
	return a.logins.RequestX19SMS(a.ctx, phone)
}

// LoginX19PhonePassword 用手机号密码完成 mPay 登录。
func (a *App) LoginX19PhonePassword(phone, password, label string, persist bool) (AccountDTO, error) {
	acct, err := a.logins.LoginX19PhonePassword(a.ctx, phone, password, label, persist)
	if err != nil {
		return AccountDTO{}, err
	}
	return toAccountDTO(acct), nil
}

// LoginX19PhoneSMS 用手机号短信验证码完成 mPay 登录。
func (a *App) LoginX19PhoneSMS(phone, code, label string, persist bool) (AccountDTO, error) {
	acct, err := a.logins.LoginX19PhoneSMS(a.ctx, phone, code, label, persist)
	if err != nil {
		return AccountDTO{}, err
	}
	return toAccountDTO(acct), nil
}

// UpdateAccountLabel 更新账号备注（元数据落盘，不动凭据）。
func (a *App) UpdateAccountLabel(id, label string) error {
	if id == "" {
		return fmt.Errorf("账号 ID 不能为空")
	}
	return a.accounts.UpdateLabel(id, label)
}

func (a *App) UpdateAccountRoleName(id, roleName string) error {
	if id == "" || roleName == "" {
		return fmt.Errorf("账号 ID / 角色名不能为空")
	}
	return a.accounts.UpdateRoleName(id, roleName)
}

// Logout 移除账号及其凭据。
func (a *App) Logout(id string) error {
	if id == "" {
		return fmt.Errorf("账号 ID 不能为空")
	}
	if err := a.joins.StopAccountSessions(id); err != nil {
		return fmt.Errorf("停止该账号的代理会话失败，账号未删除: %w", err)
	}
	if err := a.accounts.Remove(id); err != nil {
		return err
	}
	if a.settings.MainAccountID == id {
		a.settings.MainAccountID = ""
		return a.saveSettings()
	}
	return nil
}

// ReLogin 触发指定账号的静默重登（使用保存的凭据换取新的 session token）。
func (a *App) ReLogin(id string) (AccountDTO, error) {
	if id == "" {
		id = a.settings.MainAccountID
	}
	if id == "" {
		return AccountDTO{}, fmt.Errorf("未指定账号 ID")
	}
	acct, _, err := a.logins.ReLogin(a.ctx, id)
	if err != nil {
		return AccountDTO{}, err
	}
	return toAccountDTO(acct), nil
}
