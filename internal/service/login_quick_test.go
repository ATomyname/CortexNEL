// 快速登录（ReLogin 会话复用前置分支）单测：心跳验证并续期 / 失败回落全链 / CAS 不覆盖并发登录。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nexus/internal/account"
	"nexus/internal/store"
	"nexus/internal/telemetry"
)

func newQuickLoginFixture(t *testing.T, userID int64, token string) (*LoginService, *account.Manager) {
	t.Helper()
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	accounts := account.NewManager(paths, account.NewTokenStore(paths))
	if err := accounts.Upsert(&account.Account{ID: "acct", Label: "acct", UserID: userID}, token, true); err != nil {
		t.Fatal(err)
	}
	logger, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	return NewLoginService(accounts, telemetry.NewBus(), logger), accounts
}

// 会话仍存活：心跳换发 token 即完成登录，不触碰任何登录链（账号无 cookie/密码，链必失败）。
func TestReLoginQuickPathReusesLiveSession(t *testing.T) {
	svc, accounts := newQuickLoginFixture(t, 5, "old-token")
	var calls int
	svc.refreshToken = func(_ context.Context, userID int64, token string) (string, error) {
		calls++
		if userID != 5 || token != "old-token" {
			t.Fatalf("refresh args = (%d, %q)", userID, token)
		}
		return "rotated-token", nil
	}
	acct, token, err := svc.ReLogin(context.Background(), "acct")
	if err != nil {
		t.Fatalf("ReLogin quick path err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls)
	}
	if token != "rotated-token" {
		t.Fatalf("returned token = %q, want rotated-token", token)
	}
	if acct.ID != "acct" {
		t.Fatalf("account = %+v", acct)
	}
	if got, err := accounts.Token("acct"); err != nil || got != "rotated-token" {
		t.Fatalf("stored token = %q, %v", got, err)
	}
}

// 心跳失败：回落全链。账号无凭据时应报可读错误（证明确实走进了链）。
func TestReLoginQuickPathFallsBackToChain(t *testing.T) {
	svc, _ := newQuickLoginFixture(t, 5, "old-token")
	svc.refreshToken = func(context.Context, int64, string) (string, error) {
		return "", errors.New("heartbeat rejected")
	}
	if _, _, err := svc.ReLogin(context.Background(), "acct"); err == nil || !strings.Contains(err.Error(), "账号未保存可用的登录凭据") {
		t.Fatalf("ReLogin err = %v, want 无凭据可读错误", err)
	}
}

// CAS：心跳响应期间并发登录写入新 token 时，不被旧换发结果覆盖。
func TestReLoginQuickPathDoesNotOverwriteConcurrentLogin(t *testing.T) {
	svc, accounts := newQuickLoginFixture(t, 5, "old-token")
	svc.refreshToken = func(context.Context, int64, string) (string, error) {
		if err := accounts.Upsert(&account.Account{ID: "acct", Label: "acct", UserID: 5}, "new-login-token", true); err != nil {
			t.Fatal(err)
		}
		return "stale-rotated-token", nil
	}
	_, token, err := svc.ReLogin(context.Background(), "acct")
	if err != nil {
		t.Fatalf("ReLogin err = %v", err)
	}
	if token != "new-login-token" {
		t.Fatalf("returned token = %q, want new-login-token", token)
	}
	if got, err := accounts.Token("acct"); err != nil || got != "new-login-token" {
		t.Fatalf("stored token = %q, %v", got, err)
	}
}
