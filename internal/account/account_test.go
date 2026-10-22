package account

import (
	"errors"
	"testing"

	"nexus/internal/store"
)

func TestUpsertListTokenFlow(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(paths, NewTokenStore(paths))

	// 显式开启落盘（DR-1 opt-in）。
	a := Account{Label: "主号", UserID: 933874987}
	if err := m.Upsert(&a, "token-secret", true); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	list, err := m.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
	if !list[0].TokenPersisted || list[0].Channel != Channel4399com {
		t.Fatalf("账号元数据 = %+v", list[0])
	}
	got, err := m.Token(list[0].ID)
	if err != nil || got != "token-secret" {
		t.Fatalf("Token = %q, %v", got, err)
	}

	// Remove 后 token 一并删除。
	if err := m.Remove(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Token(list[0].ID); err == nil {
		t.Fatal("删除后 token 应不可取")
	}
}

func TestMemoryOnlyTokenLostOnRestart(t *testing.T) {
	// DR-1 默认关闭：token 只在会话内存。
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(paths, NewTokenStore(paths))
	a := Account{Label: "临时号", UserID: 1}
	if err := m.Upsert(&a, "mem-token", false); err != nil {
		t.Fatal(err)
	}
	list, _ := m.List()
	if got, err := m.Token(list[0].ID); err != nil || got != "mem-token" {
		t.Fatalf("会话内取 token = %q, %v", got, err)
	}
	// 新 Manager（模拟重启）：元数据仍在，内存 token 丢失。
	m2 := NewManager(paths, NewTokenStore(paths))
	if _, err := m2.Token(list[0].ID); err == nil {
		t.Fatal("重启后内存 token 应不可取")
	}
	if _, err := m2.Get(list[0].ID); err != nil {
		t.Fatalf("元数据应仍在: %v", err)
	}
}

func TestGetMissing(t *testing.T) {
	paths, _ := store.NewPaths(t.TempDir())
	m := NewManager(paths, NewTokenStore(paths))
	if _, err := m.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestReplaceTokenPreservesPersistenceMode(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(paths, NewTokenStore(paths))
	for _, persist := range []bool{false, true} {
		a := Account{Label: "replace", TokenPersisted: persist}
		if err := m.Upsert(&a, "old", persist); err != nil {
			t.Fatal(err)
		}
		list, err := m.List()
		if err != nil || len(list) != 1 {
			t.Fatalf("List = %v, %v", list, err)
		}
		updated, err := m.ReplaceTokenIfCurrent(list[0].ID, "old", "new")
		if err != nil || !updated {
			t.Fatalf("ReplaceTokenIfCurrent(persist=%v) = %v, %v", persist, updated, err)
		}
		got, err := m.Token(list[0].ID)
		if err != nil || got != "new" {
			t.Fatalf("Token(persist=%v) = %q, %v", persist, got, err)
		}
		if list[0].TokenPersisted != persist {
			t.Fatalf("persistence mode changed: %+v", list[0])
		}
		if err := m.Remove(list[0].ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReplaceTokenDoesNotRecreateRemovedAccount(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(paths, NewTokenStore(paths))
	if err := m.Upsert(&Account{Label: "remove race"}, "old", false); err != nil {
		t.Fatal(err)
	}
	list, _ := m.List()
	if err := m.Remove(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReplaceTokenIfCurrent(list[0].ID, "old", "new"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReplaceTokenIfCurrent after Remove = %v", err)
	}
	if _, err := m.Get(list[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed account was recreated: %v", err)
	}
}

func TestReplaceTokenIfCurrentDoesNotOverwriteNewLogin(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(paths, NewTokenStore(paths))
	if err := m.Upsert(&Account{ID: "account", Label: "account"}, "new-login", false); err != nil {
		t.Fatal(err)
	}
	updated, err := m.ReplaceTokenIfCurrent("account", "stale-request", "stale-response")
	if err != nil || updated {
		t.Fatalf("stale replacement = %v, %v", updated, err)
	}
	if got, err := m.Token("account"); err != nil || got != "new-login" {
		t.Fatalf("token = %q, %v", got, err)
	}
}

// 回归：Upsert 生成的 ID 必须回写调用方——否则调用方后续 Token(acct.ID) 拿空 ID
// 取令牌必报「account: 账号不存在」（小号机一键导入『读取导入账号令牌失败』根因）。
func TestUpsertWritesGeneratedIDBack(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(paths, NewTokenStore(paths))
	a := Account{Label: "小号机", UserID: 933874987} // 不预设 ID，复现导入链路
	if err := m.Upsert(&a, "token-secret", true); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if a.ID == "" {
		t.Fatal("Upsert 未把生成的 ID 回写调用方")
	}
	got, err := m.Token(a.ID)
	if err != nil || got != "token-secret" {
		t.Fatalf("Token(a.ID) = %q, %v", got, err)
	}
}
