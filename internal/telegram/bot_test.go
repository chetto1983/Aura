package telegram

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/config"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/storage/memoryindex"
	tele "gopkg.in/telebot.v4"
)

func TestToolActivityMessageDoesNotExposeArgs(t *testing.T) {
	got := toolActivityMessage("write_file")
	if got != "Sto lavorando alla richiesta..." {
		t.Fatalf("toolActivityMessage() = %q, want generic activity text", got)
	}
	if strings.Contains(got, "write_file") {
		t.Fatalf("toolActivityMessage leaked tool name: %q", got)
	}
}

func TestToolActivityMessageFallback(t *testing.T) {
	got := toolActivityMessage(" ")
	if got != "Sto lavorando alla richiesta..." {
		t.Fatalf("toolActivityMessage() = %q, want generic activity text", got)
	}
}

func TestNewRequiresDBPool(t *testing.T) {
	_, err := New(Deps{Cfg: &config.Config{}})
	if err == nil || !strings.Contains(err.Error(), "db pool required") {
		t.Fatalf("New() error = %v, want db pool required", err)
	}
}

func TestIsAllowlisted_UnionsConfiguredAndPersistedAllowlist(t *testing.T) {
	store := newTelegramTestAuthStore(t)
	if claimed, err := store.BootstrapUser(context.Background(), "persisted"); err != nil || !claimed {
		t.Fatalf("bootstrap claimed=%v err=%v, want true nil", claimed, err)
	}
	b := &Bot{
		cfg: &config.Config{
			Allowlist:           []string{"configured"},
			AllowlistConfigured: true,
		},
		rt: &botRuntime{authDB: store},
	}
	if !b.isAllowlisted("configured") {
		t.Fatal("configured user should be allowed")
	}
	if !b.isAllowlisted("persisted") {
		t.Fatal("persisted bootstrap/approved user should be allowed even when env allowlist is configured")
	}
	if b.isAllowlisted("other") {
		t.Fatal("unknown user should not be allowed")
	}
}

func TestIsAllowlisted_UsesBootstrapStoreWhenAllowlistBlank(t *testing.T) {
	store := newTelegramTestAuthStore(t)
	if claimed, err := store.BootstrapUser(context.Background(), "bootstrap"); err != nil || !claimed {
		t.Fatalf("bootstrap claimed=%v err=%v, want true nil", claimed, err)
	}
	b := &Bot{
		cfg: &config.Config{},
		rt:  &botRuntime{authDB: store},
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
	if claimed, err := store.BootstrapE2EUser(ctx, "e2e-id"); err != nil || claimed {
		t.Fatalf("e2e claimed=%v err=%v, want ignored because real owner exists", claimed, err)
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
		rt: &botRuntime{authDB: store},
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

func TestOnLoginBootstrapsFirstRunAndSendsToken(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	store := newTelegramTestAuthStore(t)
	b := &Bot{
		bot:    tb,
		cfg:    &config.Config{},
		rt:     &botRuntime{authDB: store},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123, Username: "owner"},
		Chat:   &tele.Chat{ID: 123},
		Text:   "/login",
	}})

	if err := b.onLogin(ctx); err != nil {
		t.Fatalf("onLogin() error = %v, want nil", err)
	}
	ok, err := store.IsUserAllowed(context.Background(), "123")
	if err != nil {
		t.Fatalf("allowed lookup: %v", err)
	}
	if !ok {
		t.Fatal("first /login user should be bootstrapped into allowed_users")
	}
	if got := countTelegramMethods(calls, "sendMessage"); got != 1 {
		t.Fatalf("sendMessage calls = %d, want 1 (calls=%+v)", got, calls)
	}
	text, _ := calls[0].Body["text"].(string)
	if !strings.Contains(text, "Dashboard token") {
		t.Fatalf("login reply = %q, want dashboard token", text)
	}
	if strings.Contains(text, "pending approval") || strings.Contains(text, "Aura is private") {
		t.Fatalf("login reply = %q, want direct first-run token", text)
	}
	token := extractDashboardToken(t, text)
	if got, err := store.Lookup(context.Background(), token); err != nil || got != "123" {
		t.Fatalf("issued token lookup = %q, %v; want user 123", got, err)
	}
	assertTelegramAuthz(t, store, "123", identity.CapabilitySkillsInstall, identity.DecisionAllow)
}

func TestOnLoginEnvAllowlistEnsuresIdentityBeforeToken(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	store := newTelegramTestAuthStore(t)
	b := &Bot{
		bot: tb,
		cfg: &config.Config{
			Allowlist:           []string{"123"},
			AllowlistConfigured: true,
		},
		rt:     &botRuntime{authDB: store},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123, Username: "envowner"},
		Chat:   &tele.Chat{ID: 123},
		Text:   "/login",
	}})

	if err := b.onLogin(ctx); err != nil {
		t.Fatalf("onLogin() error = %v, want nil", err)
	}
	if got := countTelegramMethods(calls, "sendMessage"); got != 1 {
		t.Fatalf("sendMessage calls = %d, want 1 (calls=%+v)", got, calls)
	}
	text, _ := calls[0].Body["text"].(string)
	token := extractDashboardToken(t, text)
	if got, err := store.Lookup(context.Background(), token); err != nil || got != "123" {
		t.Fatalf("issued token lookup = %q, %v; want user 123", got, err)
	}
	if ok, err := store.IsUserAllowed(context.Background(), "123"); err != nil || ok {
		t.Fatalf("IsUserAllowed(env user) = %v, %v; want false nil", ok, err)
	}
	assertTelegramAuthz(t, store, "123", identity.CapabilitySkillsInstall, identity.DecisionAllow)
}

func TestApproveAccessCreatesIdentityBeforeSendingToken(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	store := newTelegramTestAuthStore(t)
	ctx := context.Background()
	if claimed, err := store.BootstrapUser(ctx, "123"); err != nil || !claimed {
		t.Fatalf("bootstrap owner claimed=%v err=%v, want true nil", claimed, err)
	}
	if _, err := store.RequestAccess(ctx, "456", "guest"); err != nil {
		t.Fatalf("request access: %v", err)
	}
	b := &Bot{
		bot:    tb,
		cfg:    &config.Config{},
		rt:     &botRuntime{authDB: store},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := b.ApproveAccess(ctx, "456"); err != nil {
		t.Fatalf("ApproveAccess() error = %v, want nil", err)
	}
	if got := countTelegramMethods(calls, "sendMessage"); got != 1 {
		t.Fatalf("sendMessage calls = %d, want 1 (calls=%+v)", got, calls)
	}
	text, _ := calls[0].Body["text"].(string)
	token := extractDashboardToken(t, text)
	if got, err := store.Lookup(ctx, token); err != nil || got != "456" {
		t.Fatalf("issued token lookup = %q, %v; want user 456", got, err)
	}
	assertTelegramAuthz(t, store, "456", identity.CapabilityAPIChat, identity.DecisionAllow)
	assertTelegramAuthz(t, store, "456", identity.CapabilitySkillsInstall, identity.DecisionDeny)
}

func TestOnMessageIgnoresUnauthorizedText(t *testing.T) {
	b := &Bot{
		cfg: &config.Config{
			Allowlist:           []string{"owner"},
			AllowlistConfigured: true,
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx := tele.NewContext(nil, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 999, Username: "guest"},
		Chat:   &tele.Chat{ID: 999},
		Text:   "hello",
	}})

	if err := b.onMessage(ctx); err != nil {
		t.Fatalf("onMessage() error = %v, want nil", err)
	}
	if b.sessionStore().IsActive("999") {
		t.Fatal("unauthorized text should not mark the user active")
	}
	if _, ok := b.sessionStore().Load("999"); ok {
		t.Fatal("unauthorized text should not create conversation context")
	}
}

func TestCompactMemoryStatusSummaryReportsMirrorState(t *testing.T) {
	tracker := memoryindex.NewVectorHealthTracker(true, "aura_memory_v1_compact")
	tracker.Started()
	tracker.Succeeded(memoryindex.VectorReport{Collection: "aura_memory_v1_compact", DocsIndexed: 7, VectorSize: 1024})
	b := &Bot{rt: &botRuntime{compactMemoryHealth: tracker}}

	got := b.compactMemoryStatusSummary()

	for _, want := range []string{"Compact memory mirror", "aura_memory_v1_compact", "docs: 7", "vector: 1024"} {
		if !strings.Contains(got, want) {
			t.Fatalf("compactMemoryStatusSummary() = %q, missing %q", got, want)
		}
	}
}

func TestStopWithoutStartReturns(t *testing.T) {
	done := make(chan struct{})
	go func() {
		(&Bot{}).Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked on a bot that was never started")
	}
}

func newTelegramTestAuthStore(t *testing.T) *auth.Store {
	t.Helper()
	pool, err := auradb.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := migrations.Run(context.Background(), pool); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	store, err := auth.NewStoreWithDB(pool)
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	return store
}

func extractDashboardToken(t *testing.T, text string) string {
	t.Helper()
	parts := strings.Split(text, "\n\n")
	if len(parts) < 3 {
		t.Fatalf("dashboard token message has %d sections: %q", len(parts), text)
	}
	token := strings.TrimSpace(parts[2])
	if token == "" || strings.Contains(token, "\n") {
		t.Fatalf("could not extract dashboard token from %q", text)
	}
	return token
}

func assertTelegramAuthz(t *testing.T, store *auth.Store, userID string, capability identity.Capability, want identity.Decision) {
	t.Helper()
	decision, err := store.Authorize(context.Background(), identity.AuthorizeParams{
		ActorID:    identity.TelegramSessionActorID(userID),
		Capability: capability,
		Resource:   identity.ResourceRef{Type: "test", ID: string(capability)},
	})
	if err != nil {
		t.Fatalf("Authorize(%s, %s): %v", userID, capability, err)
	}
	if decision.Decision != want {
		t.Fatalf("Authorize(%s, %s) = %s (%s), want %s", userID, capability, decision.Decision, decision.Reason, want)
	}
}

func waitUntil(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
