package cronadapter_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aura/aura/internal/channels/cron"
	"github.com/aura/aura/internal/chat"
	ourcron "github.com/aura/aura/internal/cron"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
)

// --- fakes -------------------------------------------------------------------

type stubLoop struct {
	calls atomic.Int32
	err   error
}

type recordingCronJobRunner struct {
	calls      int
	actorID    string
	authorizer bool
	req        ourcron.JobRequest
}

func (r *recordingCronJobRunner) RunJob(ctx context.Context, req ourcron.JobRequest) (ourcron.JobResult, error) {
	r.calls++
	r.actorID = identity.ActorIDFromContext(ctx)
	_, r.authorizer = identity.AuthorizerFromContext(ctx)
	r.req = req
	return ourcron.JobResult{Content: "cron loop ok"}, nil
}

func (s *stubLoop) Run(_ context.Context, _ *chat.Run, _ chat.InboundMessage, _ chat.EmitFn) error {
	s.calls.Add(1)
	return s.err
}

// --- helpers -----------------------------------------------------------------

func newTestHub(t *testing.T, loop chat.AgentLoop) *chat.Hub {
	t.Helper()
	hub, err := chat.New(chat.Config{Loop: loop})
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	hub.RegisterInbound(cronadapter.New())
	return hub
}

func openCronLoopIdentityStore(t *testing.T) (*identity.Store, *sql.DB) {
	t.Helper()
	db, err := auradb.Open(filepath.Join(t.TempDir(), "cron-loop.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := identity.NewStore(db)
	if err != nil {
		t.Fatalf("identity.NewStore: %v", err)
	}
	return store, db
}

func seedCronLoopParent(t *testing.T, store *identity.Store, userID string, capabilities ...identity.Capability) identity.Actor {
	t.Helper()
	if _, err := store.BackfillTelegramAllowlistedUser(context.Background(), identity.TelegramAllowlistUserParams{
		UserID:        userID,
		Source:        "test",
		PrincipalKind: identity.PrincipalKindOwner,
		Capabilities:  capabilities,
	}); err != nil {
		t.Fatalf("BackfillTelegramAllowlistedUser: %v", err)
	}
	return identity.Actor{ID: identity.TelegramSessionActorID(userID)}
}

func assertCronLoopScalar[T comparable](t *testing.T, db *sql.DB, query string, want T, args ...any) {
	t.Helper()
	var got T
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %v, want %v", query, got, want)
	}
}

// --- dispatcher routing ------------------------------------------------------

func TestHubDispatcher_AgentJob_RoutesViaHub(t *testing.T) {
	loop := &stubLoop{}
	hub := newTestHub(t, loop)

	fallbackCalled := false
	fallback := ourcron.Dispatcher(func(_ context.Context, _ *ourcron.Task) error {
		fallbackCalled = true
		return nil
	})

	d := cronadapter.NewHubDispatcher(hub, fallback)

	task := &ourcron.Task{
		ID:      1,
		Name:    "daily-check",
		Kind:    ourcron.KindAgentJob,
		Payload: `{"goal":"check memory"}`,
	}
	if err := d(context.Background(), task); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if loop.calls.Load() != 1 {
		t.Errorf("AgentLoop.Run called %d times, want 1", loop.calls.Load())
	}
	if fallbackCalled {
		t.Error("fallback should not be called for KindAgentJob")
	}
}

func TestHubDispatcher_Reminder_RoutesFallback(t *testing.T) {
	loop := &stubLoop{}
	hub := newTestHub(t, loop)

	fallbackCalled := false
	var fallbackTask *ourcron.Task
	fallback := ourcron.Dispatcher(func(_ context.Context, task *ourcron.Task) error {
		fallbackCalled = true
		fallbackTask = task
		return nil
	})

	d := cronadapter.NewHubDispatcher(hub, fallback)

	task := &ourcron.Task{
		ID:          2,
		Name:        "morning-reminder",
		Kind:        ourcron.KindReminder,
		RecipientID: "42",
		Payload:     "Time to stretch!",
	}
	if err := d(context.Background(), task); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if loop.calls.Load() != 0 {
		t.Errorf("AgentLoop.Run should not be called for KindReminder, got %d calls", loop.calls.Load())
	}
	if !fallbackCalled {
		t.Fatal("fallback should be called for KindReminder")
	}
	if fallbackTask != task {
		t.Error("fallback received wrong task pointer")
	}
}

func TestHubDispatcher_AgentJob_PropagatesLoopError(t *testing.T) {
	loop := &stubLoop{err: errors.New("llm down")}
	hub := newTestHub(t, loop)

	fallback := ourcron.Dispatcher(func(_ context.Context, _ *ourcron.Task) error { return nil })
	d := cronadapter.NewHubDispatcher(hub, fallback)

	task := &ourcron.Task{Kind: ourcron.KindAgentJob, Name: "x", Payload: `{"goal":"g"}`}
	err := d(context.Background(), task)
	if err == nil {
		t.Fatal("expected error from loop to propagate")
	}
}

func TestCronAgentLoopDelegatesActorContext(t *testing.T) {
	identityStore, db := openCronLoopIdentityStore(t)
	parent := seedCronLoopParent(t, identityStore, "12345", identity.CapabilityCronRun, identity.CapabilityToolExecute)
	runner := &recordingCronJobRunner{}
	loop := cronadapter.NewCronAgentLoop(runner, nil, cronadapter.WithIdentity(identityStore))
	run := &chat.Run{ID: "run-cron-loop"}
	msg := chat.InboundMessage{
		Channel:     chat.ChannelCron,
		PrincipalID: "12345",
		ThreadID:    "cron:nightly",
		Text:        `{"goal":"check context","tool_allowlist":["file"]}`,
		Mode:        chat.DeliveryModeSilent,
	}

	if err := loop.Run(context.Background(), run, msg, func(chat.OutboundEvent) error { return nil }); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if runner.actorID == "" || runner.actorID == parent.ID {
		t.Fatalf("runner actor id = %q, parent = %q", runner.actorID, parent.ID)
	}
	if !runner.authorizer {
		t.Fatal("runner context missing authorizer")
	}
	assertCronLoopScalar(t, db, `SELECT actor_type FROM actors WHERE id = ?`, "cron", runner.actorID)
	assertCronLoopScalar(t, db, `SELECT parent_actor_id FROM actors WHERE id = ?`, parent.ID, runner.actorID)
	assertCronLoopScalar(t, db, `SELECT COUNT(*) FROM capability_grants WHERE subject_type = 'actor' AND subject_id = ? AND capability = 'tool.execute'`, int64(1), runner.actorID)
	assertCronLoopScalar(t, db, `SELECT COUNT(*) FROM authz_decisions WHERE actor_id = ? AND decision = 'allow'`, int64(2), parent.ID)
}

func TestCronAgentLoopRequiresIdentityDelegation(t *testing.T) {
	runner := &recordingCronJobRunner{}
	loop := cronadapter.NewCronAgentLoop(runner, nil)
	err := loop.Run(context.Background(), &chat.Run{ID: "run-no-identity"}, chat.InboundMessage{
		Channel:     chat.ChannelCron,
		PrincipalID: "12345",
		ThreadID:    "cron:no-identity",
		Text:        `{"goal":"check context","tool_allowlist":["file"]}`,
		Mode:        chat.DeliveryModeSilent,
	}, func(chat.OutboundEvent) error { return nil })
	if !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("Run error = %v, want ErrUnauthorized", err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

// --- InboundAdapter ----------------------------------------------------------

func TestInboundAdapter_Normalize_MapsTaskFields(t *testing.T) {
	adapter := cronadapter.New()
	if adapter.Channel() != chat.ChannelCron {
		t.Fatalf("Channel() = %q, want %q", adapter.Channel(), chat.ChannelCron)
	}

	task := &ourcron.Task{
		ID:          99,
		Name:        "market-watch",
		Kind:        ourcron.KindAgentJob,
		Payload:     `{"goal":"check indices"}`,
		RecipientID: "777",
	}
	msg, err := adapter.Normalize(context.Background(), task)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if msg.Channel != chat.ChannelCron {
		t.Errorf("Channel = %q", msg.Channel)
	}
	if msg.PrincipalID != "777" {
		t.Errorf("PrincipalID = %q", msg.PrincipalID)
	}
	if msg.ThreadID != "cron:market-watch" {
		t.Errorf("ThreadID = %q", msg.ThreadID)
	}
	if msg.Text != task.Payload {
		t.Errorf("Text = %q", msg.Text)
	}
	if msg.Mode != chat.DeliveryModeSilent {
		t.Errorf("Mode = %q", msg.Mode)
	}
	if msg.ChannelData["task_id"] != int64(99) {
		t.Errorf("ChannelData[task_id] = %v", msg.ChannelData["task_id"])
	}
	if msg.ChannelData["kind"] != "agent_job" {
		t.Errorf("ChannelData[kind] = %v", msg.ChannelData["kind"])
	}
}

func TestInboundAdapter_Normalize_EmptyRecipientDefaultsToCron(t *testing.T) {
	adapter := cronadapter.New()
	task := &ourcron.Task{Name: "anon-job", Kind: ourcron.KindAgentJob, Payload: `{"goal":"g"}`}
	msg, err := adapter.Normalize(context.Background(), task)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if msg.PrincipalID != "cron" {
		t.Errorf("PrincipalID = %q, want %q", msg.PrincipalID, "cron")
	}
}

func TestInboundAdapter_Normalize_WrongTypeReturnsError(t *testing.T) {
	adapter := cronadapter.New()
	_, err := adapter.Normalize(context.Background(), "not-a-task")
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

// --- compile-time checks -----------------------------------------------------

var _ chat.InboundAdapter = (*cronadapter.InboundAdapter)(nil)
