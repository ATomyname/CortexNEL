// 4399 密码登录编排（架构文档 §4 service 层）：
// 自动识别验证码（captchaocr CNN + 置信度门控）→ 4399com 密码段 → 复用 Login 收尾。
// app 绑定层只做转发，重试/识别策略收敛在此（D1）。
package service

import (
	"context"
	"fmt"
	"strings"

	"nexus/internal/account"
	"nexus/internal/captchaocr"
	"nexus/internal/netease"
	"nexus/internal/store"
)

const (
	// login4399MaxAttempts 是实际登录提交上限。
	login4399MaxAttempts = 3
	// login4399MaxFetches 是验证码获取/识别总上限（含低置信度换图）。
	login4399MaxFetches = 10
	// ocrMinMargin 是 OCR top1-top2 最小边距门限，低于此值换图不提交
	//（2026-09 实测真实验证码边距分布 0.95~4.13，低边距与识别错误强相关）。
	ocrMinMargin = 1.5
)

// Login4399Password 完成 4399 账号密码登录（facts §6.1.1）。
// 低置信度验证码直接换图不提交；验证码类错误自动换码重试；
// 账号密码等其余错误原样返回（不盲试）。
func (s *LoginService) Login4399Password(ctx context.Context, accountName, password, label string, persistCred bool) (account.Account, error) {
	return s.Login4399PasswordWithAccount(ctx, account.Account{
		Label:    label,
		Username: accountName,
	}, password, persistCred)
}

// Login4399PasswordWithAccount 保留已有账号实体（包括已存在的 ID）执行登录与凭据刷新。
func (s *LoginService) Login4399PasswordWithAccount(ctx context.Context, baseAcct account.Account, password string, persistCred bool) (account.Account, error) {
	if baseAcct.Username == "" || password == "" {
		return account.Account{}, fmt.Errorf("请输入 4399 账号和密码")
	}
	client, err := netease.NewCom4399Client()
	if err != nil {
		return account.Account{}, err
	}
	rec, err := s.recognizer()
	if err != nil {
		return account.Account{}, fmt.Errorf("验证码识别器初始化失败: %w", err)
	}

	var lastErr error
	fetches, logins := 0, 0
	for logins < login4399MaxAttempts && fetches < login4399MaxFetches {
		captchaID, img, err := client.CaptchaChallenge(ctx)
		if err != nil {
			return account.Account{}, fmt.Errorf("验证码获取失败: %w", err)
		}
		fetches++
		code, margin, err := rec.RecognizeWithConfidence(img)
		if err != nil {
			lastErr = fmt.Errorf("验证码识别失败: %w", err)
			continue
		}
		if margin < ocrMinMargin {
			lastErr = fmt.Errorf("验证码置信度不足（margin=%.2f）", margin)
			continue
		}
		logins++
		sauth, err := client.Login(ctx, baseAcct.Username, password, captchaID, code)
		if err != nil {
			lastErr = err
			if isCaptchaErr(err) {
				continue
			}
			return account.Account{}, err
		}

		a := baseAcct
		if persistCred {
			if encPass, err := store.ProtectString(password); err == nil {
				a.EncryptedPassword = encPass
			}
		}
		return s.LoginWithAccount(ctx, a, sauth, persistCred)
	}
	return account.Account{}, fmt.Errorf("连续 %d 次登录未成功（最后一次: %w）", logins, lastErr)
}

// SAuthForPE 以账号凭据执行一次 4399 登录并返回 sauth（PE 线专用，facts §8：
// PE 鉴权需要 sauth 会话而非 x19 token；独立会话不与 Java 线互踢）。
func (s *LoginService) SAuthForPE(ctx context.Context, accountID string) (account.Account, string, error) {
	acct, err := s.accounts.Get(accountID)
	if err != nil {
		return account.Account{}, "", err
	}
	if acct.Username == "" || acct.EncryptedPassword == "" {
		return acct, "", fmt.Errorf("账号未保存登录凭据，无法获取 PE 会话（请在账号页重新登录）")
	}
	password, err := store.UnprotectString(acct.EncryptedPassword)
	if err != nil {
		return acct, "", fmt.Errorf("解密账号凭据失败: %w", err)
	}
	client, err := netease.NewCom4399Client()
	if err != nil {
		return acct, "", err
	}
	// PE 线前置：注册 PE 设备身份（facts §8；缺此步 /pe-authentication
	// 报 code=2000"客户端登录版本异常"）。
	if err := client.PrimePE(ctx); err != nil {
		s.log.Error("service", "PE 设备注册失败", "error", err.Error())
	}
	rec, err := s.recognizer()
	if err != nil {
		return acct, "", fmt.Errorf("验证码识别器初始化失败: %w", err)
	}
	var lastErr error
	fetches, logins := 0, 0
	for logins < login4399MaxAttempts && fetches < login4399MaxFetches {
		captchaID, img, err := client.CaptchaChallenge(ctx)
		if err != nil {
			return acct, "", fmt.Errorf("验证码获取失败: %w", err)
		}
		fetches++
		code, margin, err := rec.RecognizeWithConfidence(img)
		if err != nil {
			lastErr = fmt.Errorf("验证码识别失败: %w", err)
			continue
		}
		if margin < ocrMinMargin {
			lastErr = fmt.Errorf("验证码置信度不足（margin=%.2f）", margin)
			continue
		}
		logins++
		sauth, err := client.Login(ctx, acct.Username, password, captchaID, code)
		if err != nil {
			lastErr = err
			if isCaptchaErr(err) {
				continue
			}
			return acct, "", err
		}
		return acct, sauth, nil
	}
	return acct, "", fmt.Errorf("连续 %d 次登录未成功（最后一次: %w）", logins, lastErr)
}

// ReLogin 为指定账号恢复可用会话，更新 session token。
// 快速登录优先：会话仍存活时只打 /authentication/update 心跳复用登录状态，
// 不触碰 4399/mPay 登录链（零风控暴露）；失败才按账号渠道分发全链重登：
// 4399mixed / 4399com → 4399 密码链，netease → x19 链。
// 优先复用已保存的 sauth；失效时回落账号密码，cookie-only 账号则返回可读错误。
func (s *LoginService) ReLogin(ctx context.Context, accountID string) (account.Account, string, error) {
	acct, err := s.accounts.Get(accountID)
	if err != nil {
		return account.Account{}, "", err
	}
	if s.log != nil {
		s.log.Info("service", "开始执行静默自动重新登录", "accountRef", acct.Label)
	}

	// 快速登录：先复用仍存活的登录状态（对齐参考实现 WPLauncherAccountAPI.refresh()，
	// 一次心跳同时验证会话并换发 token），成功即完成，不再走任何登录链。
	if token, ok := s.quickLoginBySession(ctx, acct); ok {
		if s.log != nil {
			s.log.Info("service", "会话仍存活，快速登录完成（未触碰登录链）", "accountRef", acct.Label)
		}
		return acct, token, nil
	}

	// 分支 1：保存了 cookie（cookie/sauth 登录源）→ 走与登录同一条 cookie 链重换 token。
	if acct.EncryptedCookie != "" {
		cookie, err := store.UnprotectString(acct.EncryptedCookie)
		if err == nil && cookie != "" {
			newAcct, lerr := s.LoginWithAccount(ctx, acct, cookie, acct.TokenPersisted)
			if lerr == nil {
				return s.reloginFinish(acct, newAcct)
			}
			if s.log != nil {
				s.log.Warn("service", "cookie 自动重登失败，回落密码链", "accountRef", acct.Label, "err", lerr.Error())
			}
		} else if err != nil {
			if s.log != nil {
				s.log.Warn("service", "解密保存的 cookie 失败", "accountRef", acct.Label, "err", err.Error())
			}
		}
	}

	if acct.Username == "" || acct.EncryptedPassword == "" {
		err := fmt.Errorf("账号未保存可用的登录凭据（cookie 与密码均不可用），无法静默重登（请在账号页重新登录）")
		if s.log != nil {
			s.log.Warn("service", "静默自动重登无可用凭据", "accountRef", acct.Label)
		}
		return acct, "", err
	}
	password, err := store.UnprotectString(acct.EncryptedPassword)
	if err != nil {
		return acct, "", fmt.Errorf("解密账号凭据失败: %w", err)
	}
	var newAcct account.Account
	switch {
	case acct.LoginMethod == "4399mixed":
		newAcct, err = s.Login4399PasswordWithAccount(ctx, acct, password, acct.TokenPersisted)
	case acct.Channel == "netease":
		newAcct, err = s.loginX19WithAccount(ctx, acct, password, acct.TokenPersisted)
	default:
		newAcct, err = s.Login4399PasswordWithAccount(ctx, acct, password, acct.TokenPersisted)
	}
	if err != nil {
		if s.log != nil {
			s.log.Warn("service", "密码链静默重登失败", "accountRef", acct.Label, "err", err.Error())
		}
		return acct, "", fmt.Errorf("静默重登失败: %w", err)
	}
	return s.reloginFinish(acct, newAcct)
}

// quickLoginBySession 见 login_quick.go（会话复用快速登录，职责独立于登录链编排）。

// reloginFinish 重登成功后保持原角色名并取新 token。
func (s *LoginService) reloginFinish(acct, newAcct account.Account) (account.Account, string, error) {
	if acct.RoleName != "" && newAcct.RoleName == "" {
		_ = s.accounts.UpdateRoleName(newAcct.ID, acct.RoleName)
		newAcct.RoleName = acct.RoleName
	}
	token, err := s.accounts.Token(newAcct.ID)
	return newAcct, token, err
}

// isCaptchaErr 判定是否验证码类错误（可换码重试）。
func isCaptchaErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "验证码") || strings.Contains(msg, "captcha")
}

// recognizer 懒加载共享识别器（模型只解析一次）。
func (s *LoginService) recognizer() (*captchaocr.Recognizer, error) {
	s.ocrOnce.Do(func() {
		s.ocr, s.ocrErr = captchaocr.New()
	})
	return s.ocr, s.ocrErr
}
