package service

import (
	"context"
	"errors"
	"testing"

	"nexus/internal/account"
	"nexus/internal/store"
	"nexus/internal/telemetry"
)

func TestRefreshAuthenticationTokensUpdatesAndContinues(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	accounts := account.NewManager(paths, account.NewTokenStore(paths))
	if err := accounts.Upsert(&account.Account{ID: "bad", Label: "bad", UserID: 1}, "bad-token", false); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Upsert(&account.Account{ID: "good", Label: "good", UserID: 2}, "old-token", true); err != nil {
		t.Fatal(err)
	}
	logger, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	svc := NewLoginService(accounts, telemetry.NewBus(), logger)
	var calls int
	svc.refreshToken = func(_ context.Context, userID int64, token string) (string, error) {
		calls++
		if userID == 1 {
			if token != "bad-token" {
				t.Fatalf("bad account token = %q", token)
			}
			return "", errors.New("fixture failure")
		}
		if userID != 2 || token != "old-token" {
			t.Fatalf("refresh args = (%d, %q)", userID, token)
		}
		return "fresh-token", nil
	}

	svc.RefreshAuthenticationTokens(context.Background())
	if calls != 2 {
		t.Fatalf("refresh calls = %d, want 2", calls)
	}
	if got, err := accounts.Token("good"); err != nil || got != "fresh-token" {
		t.Fatalf("good token = %q, %v", got, err)
	}
	if got, err := accounts.Token("bad"); err != nil || got != "bad-token" {
		t.Fatalf("failed account token = %q, %v", got, err)
	}
	good, err := accounts.Get("good")
	if err != nil || !good.TokenPersisted {
		t.Fatalf("persistence changed: %+v, %v", good, err)
	}
}

func TestRefreshAuthenticationTokensDoesNotRestoreDeletedAccount(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	accounts := account.NewManager(paths, account.NewTokenStore(paths))
	if err := accounts.Upsert(&account.Account{ID: "gone", Label: "gone", UserID: 3}, "old-token", false); err != nil {
		t.Fatal(err)
	}
	logger, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	svc := NewLoginService(accounts, telemetry.NewBus(), logger)
	svc.refreshToken = func(context.Context, int64, string) (string, error) {
		if err := accounts.Remove("gone"); err != nil {
			t.Fatal(err)
		}
		return "fresh-token", nil
	}
	svc.RefreshAuthenticationTokens(context.Background())
	if _, err := accounts.Get("gone"); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("deleted account was restored: %v", err)
	}
}

func TestRefreshAuthenticationTokensDoesNotOverwriteConcurrentLogin(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	accounts := account.NewManager(paths, account.NewTokenStore(paths))
	if err := accounts.Upsert(&account.Account{ID: "account", Label: "account", UserID: 4}, "old-token", false); err != nil {
		t.Fatal(err)
	}
	logger, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	svc := NewLoginService(accounts, telemetry.NewBus(), logger)
	svc.refreshToken = func(context.Context, int64, string) (string, error) {
		if err := accounts.Upsert(&account.Account{ID: "account", Label: "account", UserID: 4}, "new-login-token", false); err != nil {
			t.Fatal(err)
		}
		return "stale-refresh-token", nil
	}
	svc.RefreshAuthenticationTokens(context.Background())
	if got, err := accounts.Token("account"); err != nil || got != "new-login-token" {
		t.Fatalf("concurrent login token = %q, %v", got, err)
	}
}
