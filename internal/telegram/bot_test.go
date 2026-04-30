package telegram

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/auth"
	"github.com/aura/aura/internal/config"
)

func TestToolActivityMessageDoesNotExposeArgs(t *testing.T) {
	got := toolActivityMessage("write_wiki")
	if got != "Running: write_wiki" {
		t.Fatalf("toolActivityMessage() = %q, want %q", got, "Running: write_wiki")
	}
}

func TestToolActivityMessageFallback(t *testing.T) {
	got := toolActivityMessage(" ")
	if got != "Running tool" {
		t.Fatalf("toolActivityMessage() = %q, want %q", got, "Running tool")
	}
}

func TestIsAllowlisted_UsesConfiguredAllowlistFirst(t *testing.T) {
	b := &Bot{
		cfg: &config.Config{
			Allowlist:           []string{"configured"},
			AllowlistConfigured: true,
		},
	}
	if !b.isAllowlisted("configured") {
		t.Fatal("configured user should be allowed")
	}
	if b.isAllowlisted("bootstrap") {
		t.Fatal("bootstrap store should be ignored when env allowlist is configured")
	}
}

func TestIsAllowlisted_UsesBootstrapStoreWhenAllowlistBlank(t *testing.T) {
	store := newTelegramTestAuthStore(t)
	if claimed, err := store.BootstrapUser(context.Background(), "bootstrap"); err != nil || !claimed {
		t.Fatalf("bootstrap claimed=%v err=%v, want true nil", claimed, err)
	}
	b := &Bot{
		cfg:    &config.Config{},
		authDB: store,
	}
	if !b.isAllowlisted("bootstrap") {
		t.Fatal("bootstrap user should be allowed")
	}
	if b.isAllowlisted("other") {
		t.Fatal("other user should not be allowed")
	}
}

func newTelegramTestAuthStore(t *testing.T) *auth.Store {
	t.Helper()
	store, err := auth.OpenStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open auth store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
