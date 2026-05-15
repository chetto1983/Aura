package cron

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/wiki"
)

// ---- fake Notifier ----------------------------------------------------------

type fakeNotifier struct {
	reminders   []string
	completions []string
	owners      []string
	sendErr     error
}

type recordingJobRunner struct {
	calls      int
	actorID    string
	authorizer bool
	req        JobRequest
}

func (r *recordingJobRunner) RunJob(ctx context.Context, req JobRequest) (JobResult, error) {
	r.calls++
	r.actorID = identity.ActorIDFromContext(ctx)
	_, r.authorizer = identity.AuthorizerFromContext(ctx)
	r.req = req
	return JobResult{Content: "ok"}, nil
}

func openCronIdentityFixture(t *testing.T) (*Store, *identity.Store, *sql.DB) {
	t.Helper()
	db, err := auradb.Open(filepath.Join(t.TempDir(), "cron-identity.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cronStore, err := NewStoreWithDB(db)
	if err != nil {
		t.Fatalf("NewStoreWithDB: %v", err)
	}
	identityStore, err := identity.NewStore(db)
	if err != nil {
		t.Fatalf("identity.NewStore: %v", err)
	}
	return cronStore, identityStore, db
}

func seedCronParentActor(t *testing.T, store *identity.Store, userID string, capabilities ...identity.Capability) identity.Actor {
	t.Helper()
	ctx := context.Background()
	if _, err := store.BackfillTelegramAllowlistedUser(ctx, identity.TelegramAllowlistUserParams{
		UserID:        userID,
		Source:        "test",
		PrincipalKind: identity.PrincipalKindOwner,
		Capabilities:  capabilities,
	}); err != nil {
		t.Fatalf("BackfillTelegramAllowlistedUser: %v", err)
	}
	return identity.Actor{ID: identity.TelegramSessionActorID(userID), PrincipalID: identity.TelegramPrincipalID(userID), ActorType: identity.ActorTypeSession}
}

func upsertAgentJob(t *testing.T, store *Store, name, userID string) {
	t.Helper()
	if _, err := store.Upsert(context.Background(), &Task{
		Name:         name,
		Kind:         KindAgentJob,
		Payload:      `{"goal":"check context","tool_allowlist":["file"]}`,
		RecipientID:  userID,
		ScheduleKind: ScheduleAt,
		ScheduleAt:   time.Now().UTC().Add(time.Hour),
		NextRunAt:    time.Now().UTC().Add(time.Hour),
		Status:       StatusActive,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func assertCronScalar[T comparable](t *testing.T, db *sql.DB, query string, want T, args ...any) {
	t.Helper()
	var got T
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %v, want %v", query, got, want)
	}
}

func (f *fakeNotifier) SendReminder(_, body string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.reminders = append(f.reminders, body)
	return nil
}

func (f *fakeNotifier) SendCompletion(_, body string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.completions = append(f.completions, body)
	return nil
}

func (f *fakeNotifier) NotifyOwners(_ context.Context, msg string) {
	f.owners = append(f.owners, msg)
}

// ---- fake wiki.Repository ---------------------------------------------------

type fakeWiki struct {
	lintErr  error
	rebuildN int
	logCalls []string
}

func (f *fakeWiki) ReadPage(_ string) (*wiki.Page, error) { return nil, nil }
func (f *fakeWiki) ListPages() ([]string, error)          { return nil, nil }
func (f *fakeWiki) ResolveSlug(input string) (string, []string, error) {
	return input, nil, nil
}
func (f *fakeWiki) WritePage(_ context.Context, _ *wiki.Page, _ ...string) error { return nil }
func (f *fakeWiki) DeletePage(_ context.Context, _ string) error                 { return nil }
func (f *fakeWiki) Dir() string                                                  { return "" }
func (f *fakeWiki) Lint(_ context.Context) ([]wiki.LintIssue, error) {
	return nil, f.lintErr
}
func (f *fakeWiki) CleanMemory(_ context.Context, _ wiki.MemoryHygieneOptions) (*wiki.MemoryHygieneReport, error) {
	return &wiki.MemoryHygieneReport{}, nil
}
func (f *fakeWiki) RepairLink(_ context.Context, _, _ string) error { return nil }
func (f *fakeWiki) RebuildIndex(_ context.Context)                  { f.rebuildN++ }
func (f *fakeWiki) AppendLog(_ context.Context, action, _ string) {
	f.logCalls = append(f.logCalls, action)
}

// ---- dispatch routing -------------------------------------------------------

func TestDispatch_Routing(t *testing.T) {
	notifier := &fakeNotifier{}
	h := NewHandler(HandlerConfig{
		Notifier: notifier,
		Wiki:     &fakeWiki{},
	})
	ctx := context.Background()

	t.Run("reminder routes to SendReminder", func(t *testing.T) {
		task := &Task{
			Kind:        KindReminder,
			Name:        "morning-check",
			Payload:     "Check email",
			RecipientID: "42",
		}
		if err := h.Dispatch(ctx, task); err != nil {
			t.Fatalf("Dispatch reminder: %v", err)
		}
		if len(notifier.reminders) == 0 {
			t.Fatal("expected SendReminder to be called")
		}
	})

	t.Run("wiki_maintenance routes to wiki", func(t *testing.T) {
		fw := &fakeWiki{}
		h2 := NewHandler(HandlerConfig{Wiki: fw})
		task := &Task{Kind: KindWikiMaintenance, Name: "nightly"}
		if err := h2.Dispatch(ctx, task); err != nil {
			t.Fatalf("Dispatch wiki_maintenance: %v", err)
		}
		if fw.rebuildN == 0 {
			t.Error("expected RebuildIndex to be called")
		}
	})

	t.Run("agent_job returns unknown-kind error (now handled by Hub)", func(t *testing.T) {
		task := &Task{
			Kind:    KindAgentJob,
			Name:    "market-check",
			Payload: `{"goal":"check markets"}`,
		}
		err := h.Dispatch(ctx, task)
		if err == nil {
			t.Fatal("expected error: agent_job is no longer handled by cron.Handler")
		}
	})

	t.Run("unknown kind returns error", func(t *testing.T) {
		task := &Task{Kind: TaskKind("bogus"), Name: "x"}
		err := h.Dispatch(ctx, task)
		if err == nil {
			t.Fatal("expected error for unknown kind")
		}
	})
}

func TestRunNowDelegatesCronActor(t *testing.T) {
	cronStore, identityStore, db := openCronIdentityFixture(t)
	parent := seedCronParentActor(t, identityStore, "12345", identity.CapabilityCronRun, identity.CapabilityToolExecute)
	upsertAgentJob(t, cronStore, "delegated-job", "12345")
	runner := &recordingJobRunner{}
	h := NewHandler(HandlerConfig{AgentRunner: runner, SchedDB: cronStore, Identity: identityStore})

	ctx := identity.WithActorID(identity.WithAuthority(context.Background(), identityStore), parent.ID)
	result, err := h.RunNow(ctx, "delegated-job")
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !result.OK || runner.calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, runner.calls)
	}
	if runner.actorID == "" || runner.actorID == parent.ID {
		t.Fatalf("runner actor id = %q, parent = %q", runner.actorID, parent.ID)
	}
	if !runner.authorizer {
		t.Fatal("runner context missing authorizer")
	}
	assertCronScalar(t, db, `SELECT actor_type FROM actors WHERE id = ?`, "cron", runner.actorID)
	assertCronScalar(t, db, `SELECT parent_actor_id FROM actors WHERE id = ?`, parent.ID, runner.actorID)
	assertCronScalar(t, db, `SELECT COUNT(*) FROM capability_grants WHERE subject_type = 'actor' AND subject_id = ? AND capability = 'tool.execute'`, int64(1), runner.actorID)
	assertCronScalar(t, db, `SELECT COUNT(*) FROM authz_decisions WHERE actor_id = ? AND decision = 'allow'`, int64(2), parent.ID)
}

func TestRunNowDelegationRejectsMissingParentGrant(t *testing.T) {
	cronStore, identityStore, db := openCronIdentityFixture(t)
	parent := seedCronParentActor(t, identityStore, "67890", identity.CapabilityToolExecute)
	assertCronScalar(t, db, `SELECT COUNT(*) FROM capability_grants WHERE subject_id = ? AND capability = 'cron.run'`, int64(0), parent.PrincipalID)
	upsertAgentJob(t, cronStore, "denied-job", "67890")
	runner := &recordingJobRunner{}
	h := NewHandler(HandlerConfig{AgentRunner: runner, SchedDB: cronStore, Identity: identityStore})

	ctx := identity.WithActorID(identity.WithAuthority(context.Background(), identityStore), parent.ID)
	result, err := h.RunNow(ctx, "denied-job")
	if err != nil {
		t.Fatalf("RunNow returned transport error: %v", err)
	}
	if result.OK || !strings.Contains(result.LastError, identity.ErrUnauthorized.Error()) {
		t.Fatalf("RunNow result = %+v, want failed unauthorized result", result)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
	assertCronScalar(t, db, `SELECT COUNT(*) FROM actors WHERE parent_actor_id = ? AND actor_type = 'cron'`, int64(0), parent.ID)
	assertCronScalar(t, db, `SELECT COUNT(*) FROM authz_decisions WHERE actor_id = ? AND capability = 'cron.run' AND decision = 'deny'`, int64(1), parent.ID)
}

func TestRunNowRequiresIdentityDelegation(t *testing.T) {
	cronStore, _, _ := openCronIdentityFixture(t)
	upsertAgentJob(t, cronStore, "missing-identity", "12345")
	runner := &recordingJobRunner{}
	h := NewHandler(HandlerConfig{AgentRunner: runner, SchedDB: cronStore})

	result, err := h.RunNow(context.Background(), "missing-identity")
	if err != nil {
		t.Fatalf("RunNow returned transport error: %v", err)
	}
	if result.OK || !strings.Contains(result.LastError, identity.ErrUnauthorized.Error()) {
		t.Fatalf("RunNow result = %+v, want failed unauthorized result", result)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

// ---- dispatchReminder -------------------------------------------------------

func TestDispatchReminder_SendsBody(t *testing.T) {
	notifier := &fakeNotifier{}
	h := NewHandler(HandlerConfig{Notifier: notifier})
	task := &Task{
		Kind:        KindReminder,
		Name:        "buy-milk",
		Payload:     "Buy milk at the store",
		RecipientID: "99",
	}
	if err := h.dispatchReminder(task); err != nil {
		t.Fatalf("dispatchReminder: %v", err)
	}
	if len(notifier.reminders) != 1 {
		t.Fatalf("got %d reminders, want 1", len(notifier.reminders))
	}
	if notifier.reminders[0] != "⏰ Buy milk at the store" {
		t.Errorf("reminder body = %q", notifier.reminders[0])
	}
}

func TestDispatchReminder_DefaultBodyWhenPayloadEmpty(t *testing.T) {
	notifier := &fakeNotifier{}
	h := NewHandler(HandlerConfig{Notifier: notifier})
	task := &Task{
		Kind:        KindReminder,
		Name:        "stand-up",
		RecipientID: "99",
	}
	if err := h.dispatchReminder(task); err != nil {
		t.Fatalf("dispatchReminder: %v", err)
	}
	if len(notifier.reminders) != 1 {
		t.Fatalf("got %d reminders, want 1", len(notifier.reminders))
	}
	if notifier.reminders[0] != "Reminder: stand-up" {
		t.Errorf("default body = %q", notifier.reminders[0])
	}
}

func TestDispatchReminder_ErrorOnMissingRecipient(t *testing.T) {
	h := NewHandler(HandlerConfig{Notifier: &fakeNotifier{}})
	err := h.dispatchReminder(&Task{Kind: KindReminder, Name: "x"})
	if err == nil {
		t.Fatal("expected error: no recipient")
	}
}

func TestDispatchReminder_ErrorOnMissingNotifier(t *testing.T) {
	h := NewHandler(HandlerConfig{})
	err := h.dispatchReminder(&Task{Kind: KindReminder, Name: "x", RecipientID: "1"})
	if err == nil {
		t.Fatal("expected error: no notifier")
	}
}

func TestDispatchReminder_PropagatesNotifierError(t *testing.T) {
	notifier := &fakeNotifier{sendErr: errors.New("telegram down")}
	h := NewHandler(HandlerConfig{Notifier: notifier})
	task := &Task{
		Kind:        KindReminder,
		Name:        "x",
		Payload:     "body",
		RecipientID: "1",
	}
	if err := h.dispatchReminder(task); err == nil {
		t.Fatal("expected notifier error to propagate")
	}
}

// ---- dispatchWikiMaintenance ------------------------------------------------

func TestDispatchWikiMaintenance_CallsRebuildAndLog(t *testing.T) {
	fw := &fakeWiki{}
	h := NewHandler(HandlerConfig{Wiki: fw})
	if err := h.dispatchWikiMaintenance(context.Background()); err != nil {
		t.Fatalf("dispatchWikiMaintenance: %v", err)
	}
	if fw.rebuildN == 0 {
		t.Error("RebuildIndex not called")
	}
	if len(fw.logCalls) == 0 {
		t.Error("AppendLog not called")
	}
	if fw.logCalls[0] != "nightly-maintenance" {
		t.Errorf("log action = %q", fw.logCalls[0])
	}
}

func TestDispatchWikiMaintenance_ErrorOnMissingWiki(t *testing.T) {
	h := NewHandler(HandlerConfig{})
	err := h.dispatchWikiMaintenance(context.Background())
	if err == nil {
		t.Fatal("expected error: no wiki")
	}
}

func TestDispatchWikiMaintenance_LintErrorBubblesUp(t *testing.T) {
	fw := &fakeWiki{lintErr: errors.New("lint failed")}
	h := NewHandler(HandlerConfig{Wiki: fw})
	err := h.dispatchWikiMaintenance(context.Background())
	if err == nil {
		t.Fatal("expected lint error to propagate")
	}
}

func TestDispatchWikiMaintenance_NotifiesOwnersWhenWired(t *testing.T) {
	notifier := &fakeNotifier{}
	h := NewHandler(HandlerConfig{Wiki: &fakeWiki{}, Notifier: notifier})
	if err := h.dispatchWikiMaintenance(context.Background()); err != nil {
		t.Fatalf("dispatchWikiMaintenance: %v", err)
	}
	// No issues means no notification; just verify no panic and log is written.
	_ = notifier.owners
}

// ---- compile-time interface checks ------------------------------------------

var (
	_ Notifier        = (*fakeNotifier)(nil)
	_ wiki.Repository = (*fakeWiki)(nil)
)
