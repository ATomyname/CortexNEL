// 登录用例编排（架构文档 §4 service/login.go）：登录链收尾 + 账号落库 + 事件。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"nexus/internal/account"
	"nexus/internal/captchaocr"
	"nexus/internal/netease"
	"nexus/internal/store"
	"nexus/internal/telemetry"
)

// ErrLoginLimited 表示 4399 侧限流/受限（facts §6.2）。
var ErrLoginLimited = errors.New("4399 登录受限（aid login limit），请稍后再试")

// CookieSource 是 UI 注入 sauth cookie 的 SessionSource（M1 形态：粘贴 cookie 登录）。
type CookieSource struct {
	Cookie string
}

// SAuthCookie 实现 netease.SessionSource。
func (s CookieSource) SAuthCookie(context.Context) (string, error) {
	if strings.TrimSpace(s.Cookie) == "" {
		return "", errors.New("请先粘贴登录 Cookie")
	}
	return s.Cookie, nil
}

// LoginService 编排登录用例。
type LoginService struct {
	accounts     *account.Manager
	events       *telemetry.Bus
	log          *telemetry.Logger
	refreshToken func(context.Context, int64, string) (string, error)

	// 4399 验证码识别器（懒加载共享，见 login4399.go）。
	ocrOnce sync.Once
	ocr     *captchaocr.Recognizer
	ocrErr  error
}

// NewLoginService 构造。
func NewLoginService(accounts *account.Manager, events *telemetry.Bus, log *telemetry.Logger) *LoginService {
	return &LoginService{accounts: accounts, events: events, log: log, refreshToken: refreshAuthenticationToken}
}

func refreshAuthenticationToken(ctx context.Context, userID int64, token string) (string, error) {
	return netease.NewGateway(netease.Credentials{UserID: userID, Token: token}).RefreshAuthenticationToken(ctx)
}

// RefreshAuthenticationTokens 刷新所有当前可用账号的认证 token。
// 单个账号失败只记脱敏日志并继续，避免阻断其余账号。
func (s *LoginService) RefreshAuthenticationTokens(ctx context.Context) {
	accounts, err := s.accounts.List()
	if err != nil {
		s.log.Error("service", "定时刷新账号令牌失败", "error", err.Error())
		return
	}
	for _, a := range accounts {
		if err := ctx.Err(); err != nil {
			return
		}
		oldToken, err := s.accounts.Token(a.ID)
		if err != nil {
			s.log.Warn("service", "跳过不可读取令牌的账号", "accountRef", a.Label, "error", err.Error())
			continue
		}
		newToken, err := s.refreshToken(ctx, a.UserID, oldToken)
		if err != nil {
			s.log.Warn("service", "账号令牌定时刷新失败", "accountRef", a.Label, "error", err.Error())
			continue
		}
		if newToken == "" {
			s.log.Warn("service", "账号令牌定时刷新失败", "accountRef", a.Label, "error", "响应缺少 token")
			continue
		}
		updated, err := s.accounts.ReplaceTokenIfCurrent(a.ID, oldToken, newToken)
		if err != nil {
			s.log.Warn("service", "保存刷新后令牌失败", "accountRef", a.Label, "error", err.Error())
			continue
		}
		if !updated {
			s.log.Info("service", "跳过过期的令牌刷新响应", "accountRef", a.Label)
			continue
		}
		s.log.Info("service", "账号令牌已刷新", "accountRef", a.Label)
	}
}

// Login 完成 cookie → 会话 → 账号落库（persistCred 受 DR-1 opt-in 控制）。
func (s *LoginService) Login(ctx context.Context, label, cookie string, persistCred bool) (account.Account, error) {
	return s.LoginWithAccount(ctx, account.Account{Label: label}, cookie, persistCred)
}

// LoginWithAccount 完成会话并按传入账号元数据落库。
func (s *LoginService) LoginWithAccount(ctx context.Context, a account.Account, cookie string, persistCred bool) (account.Account, error) {
	session, err := netease.CompleteLogin(ctx, CookieSource{Cookie: cookie})
	if err != nil {
		s.events.Publish("account.login", "fail", map[string]any{"result": classifyLoginErr(err)})
		return account.Account{}, TranslateLoginError(err)
	}

	if a.Channel == "" {
		if session.Channel != "" {
			a.Channel = session.Channel
		} else {
			a.Channel = account.Channel4399com
		}
	}
	a.UserID = session.EntityID
	a.SDKUID = session.SDKUID
	if a.Label == "" {
		a.Label = maskedLabel(session.EntityID)
	}
	// cookie/sauth 登录源：persistCred 开启时把 cookie 一并 DPAPI 落盘，
	// 供 ReLogin 在 token 过期时走同一条 cookie 链重换 token（不再只依赖密码）。
	if persistCred && cookie != "" {
		if encCookie, err := store.ProtectString(cookie); err == nil {
			a.EncryptedCookie = encCookie
		}
	}
	// 登录成功后显式刷新 authentication/update；长期在线链路使用换发后的 token。
	refreshedToken, err := netease.NewGateway(netease.Credentials{
		UserID: session.EntityID,
		Token:  session.Token,
	}).RefreshAuthenticationToken(ctx)
	if err != nil {
		return account.Account{}, fmt.Errorf("刷新登录令牌失败: %w", err)
	}
	if refreshedToken == "" {
		return account.Account{}, fmt.Errorf("刷新登录令牌失败: 响应缺少 token")
	}
	if err := s.accounts.Upsert(&a, refreshedToken, persistCred); err != nil {
		return account.Account{}, fmt.Errorf("保存账号失败: %w", err)
	}

	// 规范 S3：账号标识一律脱敏后输出。
	s.events.Publish("account.login", "ok", map[string]any{
		"accountRef": a.Label, "channel": a.Channel,
	})
	s.log.Info("service", "登录完成", "accountRef", a.Label)
	return a, nil
}

// TranslateLoginError 把底层错误翻译为用户可读文案并保留错误链（规范 S1）。
func TranslateLoginError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "aid login limit"), strings.Contains(msg, "403"):
		return fmt.Errorf("%w（原始错误: %v）", ErrLoginLimited, err)
	case strings.Contains(msg, "uni_sauth"):
		return fmt.Errorf("登录会话校验失败，请重新获取 Cookie（原始错误: %v）", err)
	case strings.Contains(msg, "authentication-otp"):
		return fmt.Errorf("换取登录令牌失败，Cookie 可能已过期（原始错误: %v）", err)
	default:
		return fmt.Errorf("登录失败: %w", err)
	}
}

func classifyLoginErr(err error) string {
	if strings.Contains(err.Error(), "403") {
		return "limited"
	}
	return "fail"
}

// maskedLabel 生成脱敏显示名（规范 S3）。
func maskedLabel(userID int64) string {
	s := fmt.Sprintf("%d", userID)
	if len(s) > 4 {
		s = s[:2] + "****" + s[len(s)-2:]
	}
	return "账号 " + s
}
