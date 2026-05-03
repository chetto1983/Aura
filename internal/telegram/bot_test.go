package telegram

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/auth"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/swarm"
	"github.com/aura/aura/internal/tools"
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

func TestCollectOwnerIDs_UnionsEnvAndDB(t *testing.T) {
	store := newTelegramTestAuthStore(t)
	ctx := context.Background()
	if claimed, err := store.BootstrapUser(ctx, "bootstrap-id"); err != nil || !claimed {
		t.Fatalf("bootstrap claimed=%v err=%v", claimed, err)
	}
	if _, err := store.RequestAccess(ctx, "approved-id", "guest"); err != nil {
		t.Fatal(err)
	}
	if err := store.Approve(ctx, "approved-id"); err != nil {
		t.Fatal(err)
	}
	b := &Bot{
		cfg: &config.Config{
			Allowlist: []string{"env-id", "bootstrap-id"}, // intentional overlap
		},
		authDB: store,
	}
	got := b.collectOwnerIDs()
	want := map[string]struct{}{"env-id": {}, "bootstrap-id": {}, "approved-id": {}}
	if len(got) != len(want) {
		t.Fatalf("got %d ids, want %d (got=%v)", len(got), len(want), got)
	}
	for _, id := range got {
		if _, ok := want[id]; !ok {
			t.Errorf("unexpected id %q in result", id)
		}
		delete(want, id) // catch duplicates by emptying as we go
	}
}

func TestCollectOwnerIDs_EmptyWhenNothingConfigured(t *testing.T) {
	b := &Bot{cfg: &config.Config{}}
	if got := b.collectOwnerIDs(); len(got) != 0 {
		t.Errorf("got %v, want empty slice", got)
	}
}

func TestSwarmToolsAvailableRequiresManagerAndRegisteredTeamTool(t *testing.T) {
	reg := tools.NewRegistry(nil)
	b := &Bot{tools: reg}
	if b.swarmToolsAvailable() {
		t.Fatal("swarm tools should be unavailable without manager")
	}

	b.swarmMgr = &swarm.Manager{}
	if b.swarmToolsAvailable() {
		t.Fatal("swarm tools should be unavailable without run_aurabot_swarm")
	}

	reg.Register(fakeTelegramTool{name: "run_aurabot_swarm"})
	if !b.swarmToolsAvailable() {
		t.Fatal("swarm tools should be available with manager and registered team tool")
	}
}

func TestProposalToolsAvailableRequiresRegisteredTool(t *testing.T) {
	reg := tools.NewRegistry(nil)
	b := &Bot{tools: reg}
	if b.proposalToolsAvailable() {
		t.Fatal("proposal tools should be unavailable without propose_wiki_change")
	}

	reg.Register(fakeTelegramTool{name: "propose_wiki_change"})
	if !b.proposalToolsAvailable() {
		t.Fatal("proposal tools should be available with registered proposal tool")
	}
}

type fakeTelegramTool struct {
	name string
}

func (t fakeTelegramTool) Name() string { return t.name }

func (fakeTelegramTool) Description() string { return "fake tool" }

func (fakeTelegramTool) Parameters() map[string]any { return map[string]any{"type": "object"} }

func (fakeTelegramTool) Execute(context.Context, map[string]any) (string, error) { return "{}", nil }

func newTelegramTestAuthStore(t *testing.T) *auth.Store {
	t.Helper()
	store, err := auth.OpenStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open auth store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
