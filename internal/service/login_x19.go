// x19 邮箱账号链编排（facts §6.3）：mPay 设备 + 邮箱密码 → sauth cookie
// → 复用 CompleteLogin 收尾链（netease 渠道跳过 uni_sauth）→ 账号落库。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nexus/internal/account"
	"nexus/internal/netease"
	"nexus/internal/store"
)

// ErrX19NeedVerify 表示网易侧要求风控验证（facts §6.3 code 1351）。
var ErrX19NeedVerify = errors.New("账号需网易安全验证，请在官方端完成验证后重试")

// errX19Empty 是入参为空的内部哨兵（登录前即可判定）。
var errX19Empty = errors.New("netease: 邮箱或密码不能为空")

// LoginX19 用网易邮箱账号密码完成登录（facts §6.3；RunNEL LoginX19 对应编排）。
// persistCred 时邮箱与 DPAPI 加密密码随账号落盘（DR-1 opt-in，供后续静默重登）。
func (s *LoginService) LoginX19(ctx context.Context, email, password, label string, persistCred bool) (account.Account, error) {
	return s.loginX19WithAccount(ctx, account.Account{Label: label, Username: email, Channel: "netease", LoginMethod: "email"}, password, persistCred)
}

// RequestX19SMS 发送手机号 mPay 登录验证码。
func (s *LoginService) RequestX19SMS(ctx context.Context, phone string) error {
	if strings.TrimSpace(phone) == "" {
		return fmt.Errorf("请输入手机号")
	}
	return netease.X19RequestSMS(ctx, phone)
}

// LoginX19PhoneSMS 使用手机号和短信验证码完成 mPay 登录。
func (s *LoginService) LoginX19PhoneSMS(ctx context.Context, phone, code, label string, persistCred bool) (account.Account, error) {
	if strings.TrimSpace(phone) == "" || strings.TrimSpace(code) == "" {
		return account.Account{}, fmt.Errorf("请输入手机号和短信验证码")
	}
	cookie, err := netease.X19PhoneSMSLogin(ctx, phone, code)
	if err != nil {
		return account.Account{}, translateX19LoginError(err)
	}
	return s.finishX19Login(ctx, account.Account{Label: label, Username: phone, Channel: "netease", LoginMethod: "sms"}, cookie, "", persistCred)
}

// LoginX19PhonePassword 使用手机号和密码完成 mPay 登录。
func (s *LoginService) LoginX19PhonePassword(ctx context.Context, phone, password, label string, persistCred bool) (account.Account, error) {
	if strings.TrimSpace(phone) == "" || password == "" {
		return account.Account{}, fmt.Errorf("请输入手机号和密码")
	}
	cookie, err := netease.X19PhonePasswordLogin(ctx, phone, password)
	if err != nil {
		return account.Account{}, translateX19LoginError(err)
	}
	return s.finishX19Login(ctx, account.Account{Label: label, Username: phone, Channel: "netease", LoginMethod: "phone_password"}, cookie, password, persistCred)
}

// loginX19WithAccount 保留已有账号实体（含已存在的 ID）执行 x19 邮箱登录与凭据刷新。
func (s *LoginService) loginX19WithAccount(ctx context.Context, base account.Account, password string, persistCred bool) (account.Account, error) {
	if base.Username == "" || password == "" {
		return account.Account{}, translateX19LoginError(errX19Empty)
	}
	var cookie string
	var err error
	if base.LoginMethod == "phone_password" {
		cookie, err = netease.X19PhonePasswordLogin(ctx, base.Username, password)
	} else {
		cookie, err = netease.X19EmailLogin(ctx, base.Username, password)
	}
	if err != nil {
		return account.Account{}, translateX19LoginError(err)
	}
	return s.finishX19Login(ctx, base, cookie, password, persistCred)
}

func (s *LoginService) finishX19Login(ctx context.Context, base account.Account, cookie, password string, persistCred bool) (account.Account, error) {
	acct, err := s.LoginWithAccount(ctx, base, cookie, persistCred)
	if err != nil {
		return account.Account{}, err
	}
	if persistCred && password != "" {
		// LoginWithAccount 落库时不携带密码；补写 DPAPI 加密密码供 0xff 静默重登（DR-1）。
		if encPass, encErr := store.ProtectString(password); encErr == nil {
			acct.EncryptedPassword = encPass
			// Upsert 总会重写 TokenPersisted；取回刚落库的 token，避免补写密码时
			// 把持久化会话降级成空的内存 token。
			token, tokenErr := s.accounts.Token(acct.ID)
			if tokenErr != nil {
				s.log.Warn("service", "x19 凭据落盘时读取 token 失败", "err", tokenErr.Error())
			} else if upErr := s.accounts.Upsert(&acct, token, true); upErr != nil {
				s.log.Warn("service", "x19 凭据落盘失败", "err", upErr.Error())
			}
		}
	}
	return acct, nil
}

// translateX19LoginError 把 x19 链错误翻译为用户可读文案并保留错误链（规范 S1）。
// 1351 风控走 netease.ErrMPayVerifyRequired（reason 与官方 verify_url 已解码带入）。
func translateX19LoginError(err error) error {
	msg := err.Error()
	switch {
	case errors.Is(err, errX19Empty):
		return errors.New("请输入网易邮箱账号和密码")
	case errors.Is(err, netease.ErrMPayVerifyRequired):
		return fmt.Errorf("%w（原始错误: %v）", ErrX19NeedVerify, err)
	case strings.Contains(msg, "HTTP 4"):
		return fmt.Errorf("邮箱或密码错误，或账号不存在（原始错误: %v）", err)
	default:
		return fmt.Errorf("网易账号登录失败: %w", err)
	}
}
