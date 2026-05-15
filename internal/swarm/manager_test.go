package swarm

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/identity"
)

type fakeRunner struct {
	delay time.Duration
	err   error

	mu        sync.Mutex
	active    int
	maxActive int
	calls     int
	actorIDs  []string
}

var _ LimitController = (*Manager)(nil)

func (r *fakeRunner) Run(ctx context.Context, task agent.Task) (agent.Result, error) {
	r.mu.Lock()
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.calls++
	r.actorIDs = append(r.actorIDs, identity.ActorIDFromContext(ctx))
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.active--
		r.mu.Unlock()
	}()

	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return agent.Result{}, ctx.Err()
		}
	}
	if r.err != nil {
		return agent.Result{}, r.err
	}
	return agent.Result{
		Content:   "result: " + task.Prompt,
		LLMCalls:  1,
		ToolCalls: len(task.ToolAllowlist),
		Elapsed:   r.delay,
	}, nil
}

func (r *fakeRunner) stats() (calls, maxActive int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.maxActive
}

func (r *fakeRunner) actors() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.actorIDs...)
}

func assertSwarmScalar[T comparable](t *testing.T, db *sql.DB, query string, want T, args ...any) {
	t.Helper()
	var got T
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %v, want %v", query, got, want)
	}
}

func TestManagerRunExecutesAssignmentsAndPersistsResults(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	runner := &fakeRunner{delay: 5 * time.Millisecond}
	manager, err := NewManager(ManagerConfig{Runner: runner, Store: store, MaxActive: 3, MaxDepth: 1})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	res, err := manager.Run(ctx, RunRequest{
		Goal:      "parallel read",
		CreatedBy: "user",
		Assignments: []Assignment{
			{Role: "librarian", Subject: "a", Prompt: "a", ToolAllowlist: []string{"read_file"}},
			{Role: "critic", Subject: "b", Prompt: "b", ToolAllowlist: []string{"search_files"}},
			{Role: "researcher", Subject: "c", Prompt: "c"},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Run.Status != RunCompleted {
		t.Fatalf("run status = %s", res.Run.Status)
	}
	if len(res.Tasks) != 3 {
		t.Fatalf("tasks = %d", len(res.Tasks))
	}
	for _, task := range res.Tasks {
		if task.Status != TaskCompleted || task.Result == "" {
			t.Fatalf("task not completed: %+v", task)
		}
	}
	calls, _ := runner.stats()
	if calls != 3 {
		t.Fatalf("runner calls = %d", calls)
	}
}

func TestManagerRespectsMaxActive(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	runner := &fakeRunner{delay: 25 * time.Millisecond}
	manager, err := NewManager(ManagerConfig{Runner: runner, Store: store, MaxActive: 2, MaxDepth: 1})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	assignments := make([]Assignment, 6)
	for i := range assignments {
		assignments[i] = Assignment{Role: "librarian", Prompt: "work"}
	}
	if _, err := manager.Run(ctx, RunRequest{Goal: "limit", Assignments: assignments}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	_, maxActive := runner.stats()
	if maxActive > 2 {
		t.Fatalf("max active = %d", maxActive)
	}
}

func TestManagerUpdateLimitsAffectsNextRun(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	runner := &fakeRunner{delay: 25 * time.Millisecond}
	manager, err := NewManager(ManagerConfig{Runner: runner, Store: store, MaxActive: 1, MaxDepth: 1})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	manager.UpdateLimits(3, 2)
	maxActive, maxDepth := manager.Limits()
	if maxActive != 3 || maxDepth != 2 {
		t.Fatalf("limits = active:%d depth:%d", maxActive, maxDepth)
	}

	assignments := make([]Assignment, 6)
	for i := range assignments {
		assignments[i] = Assignment{Role: "librarian", Prompt: "work", Depth: 2}
	}
	if _, err := manager.Run(ctx, RunRequest{Goal: "updated limits", Assignments: assignments}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls, observedMaxActive := runner.stats()
	if calls != 6 {
		t.Fatalf("runner calls = %d, want 6", calls)
	}
	if observedMaxActive > 3 {
		t.Fatalf("observed max active = %d, want <= 3", observedMaxActive)
	}
}

func TestManagerDepthLimitFailsTaskWithoutRunning(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	runner := &fakeRunner{}
	manager, err := NewManager(ManagerConfig{Runner: runner, Store: store, MaxActive: 2, MaxDepth: 1})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	res, err := manager.Run(ctx, RunRequest{
		Goal: "depth",
		Assignments: []Assignment{
			{Role: "librarian", Prompt: "ok", Depth: 1},
			{Role: "librarian", Prompt: "too deep", Depth: 2},
		},
	})
	if err == nil {
		t.Fatal("expected run error")
	}
	if res.Run.Status != RunFailed {
		t.Fatalf("run status = %s", res.Run.Status)
	}
	calls, _ := runner.stats()
	if calls != 1 {
		t.Fatalf("runner calls = %d", calls)
	}
	var failed Task
	for _, task := range res.Tasks {
		if task.Status == TaskFailed {
			failed = task
		}
	}
	if failed.LastError == "" {
		t.Fatalf("failed task missing error: %+v", res.Tasks)
	}
}

func TestManagerDelegatesAssignmentActorContext(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	identityStore, err := identity.NewStore(store.DB())
	if err != nil {
		t.Fatalf("identity.NewStore: %v", err)
	}
	if _, err := identityStore.BackfillTelegramAllowlistedUser(ctx, identity.TelegramAllowlistUserParams{
		UserID:        "12345",
		Source:        "test",
		PrincipalKind: identity.PrincipalKindOwner,
		Capabilities:  []identity.Capability{identity.CapabilitySwarmSpawn, identity.CapabilityToolExecute},
	}); err != nil {
		t.Fatalf("BackfillTelegramAllowlistedUser: %v", err)
	}
	parentActorID := identity.TelegramSessionActorID("12345")
	runner := &fakeRunner{}
	manager, err := NewManager(ManagerConfig{Runner: runner, Store: store, MaxActive: 1, MaxDepth: 1})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	runCtx := identity.WithActorID(identity.WithAuthority(ctx, identityStore), parentActorID)
	res, err := manager.Run(runCtx, RunRequest{
		Goal:      "delegated",
		CreatedBy: "12345",
		Assignments: []Assignment{{
			Role:                  "librarian",
			Subject:               "read",
			Prompt:                "read",
			ToolAllowlist:         []string{"file"},
			DelegatedCapabilities: []identity.Capability{identity.CapabilityToolExecute},
		}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Run.Status != RunCompleted {
		t.Fatalf("run status = %s", res.Run.Status)
	}
	actors := runner.actors()
	if len(actors) != 1 || actors[0] == "" || actors[0] == parentActorID {
		t.Fatalf("runner actors = %+v, parent = %q", actors, parentActorID)
	}
	assertSwarmScalar(t, store.DB(), `SELECT actor_type FROM actors WHERE id = ?`, "swarm", actors[0])
	assertSwarmScalar(t, store.DB(), `SELECT parent_actor_id FROM actors WHERE id = ?`, parentActorID, actors[0])
	assertSwarmScalar(t, store.DB(), `SELECT COUNT(*) FROM capability_grants WHERE subject_type = 'actor' AND subject_id = ? AND capability = 'tool.execute'`, int64(1), actors[0])
	assertSwarmScalar(t, store.DB(), `SELECT COUNT(*) FROM authz_decisions WHERE actor_id = ? AND capability = 'tool.execute' AND decision = 'allow'`, int64(1), parentActorID)
}

func TestManagerDelegationRejectsMissingParentGrant(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	identityStore, err := identity.NewStore(store.DB())
	if err != nil {
		t.Fatalf("identity.NewStore: %v", err)
	}
	if _, err := identityStore.BackfillTelegramAllowlistedUser(ctx, identity.TelegramAllowlistUserParams{
		UserID:        "67890",
		Source:        "test",
		PrincipalKind: identity.PrincipalKindOwner,
		Capabilities:  []identity.Capability{identity.CapabilitySwarmSpawn},
	}); err != nil {
		t.Fatalf("BackfillTelegramAllowlistedUser: %v", err)
	}
	parentActorID := identity.TelegramSessionActorID("67890")
	runner := &fakeRunner{}
	manager, err := NewManager(ManagerConfig{Runner: runner, Store: store, MaxActive: 1, MaxDepth: 1})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	runCtx := identity.WithActorID(identity.WithAuthority(ctx, identityStore), parentActorID)
	res, err := manager.Run(runCtx, RunRequest{
		Goal:      "delegated",
		CreatedBy: "67890",
		Assignments: []Assignment{{
			Role:                  "librarian",
			Subject:               "read",
			Prompt:                "read",
			ToolAllowlist:         []string{"file"},
			DelegatedCapabilities: []identity.Capability{identity.CapabilityToolExecute},
		}},
	})
	if !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("Run error = %v, want ErrUnauthorized", err)
	}
	if res.Run.Status != RunFailed {
		t.Fatalf("run status = %s", res.Run.Status)
	}
	calls, _ := runner.stats()
	if calls != 0 {
		t.Fatalf("runner calls = %d, want 0", calls)
	}
	assertSwarmScalar(t, store.DB(), `SELECT COUNT(*) FROM actors WHERE parent_actor_id = ? AND actor_type = 'swarm'`, int64(0), parentActorID)
	assertSwarmScalar(t, store.DB(), `SELECT COUNT(*) FROM authz_decisions WHERE actor_id = ? AND capability = 'tool.execute' AND decision = 'deny'`, int64(1), parentActorID)
}

func TestManagerDelegationRequiresIdentity(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	runner := &fakeRunner{}
	manager, err := NewManager(ManagerConfig{Runner: runner, Store: store, MaxActive: 1, MaxDepth: 1})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	res, err := manager.Run(ctx, RunRequest{
		Goal:      "delegated",
		CreatedBy: "12345",
		Assignments: []Assignment{{
			Role:                  "librarian",
			Subject:               "read",
			Prompt:                "read",
			ToolAllowlist:         []string{"file"},
			DelegatedCapabilities: []identity.Capability{identity.CapabilityToolExecute},
		}},
	})
	if !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("Run error = %v, want ErrUnauthorized", err)
	}
	if res.Run.Status != RunFailed {
		t.Fatalf("run status = %s", res.Run.Status)
	}
	calls, _ := runner.stats()
	if calls != 0 {
		t.Fatalf("runner calls = %d, want 0", calls)
	}
}

func TestManagerRunnerErrorMarksTaskAndRunFailed(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	runner := &fakeRunner{err: errors.New("model unavailable")}
	manager, err := NewManager(ManagerConfig{Runner: runner, Store: store})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	res, err := manager.Run(ctx, RunRequest{
		Goal:        "fail",
		Assignments: []Assignment{{Role: "critic", Prompt: "lint"}},
	})
	if err == nil {
		t.Fatal("expected run error")
	}
	if res.Run.Status != RunFailed || res.Run.LastError == "" {
		t.Fatalf("run = %+v", res.Run)
	}
	if len(res.Tasks) != 1 || res.Tasks[0].Status != TaskFailed || res.Tasks[0].LastError == "" {
		t.Fatalf("tasks = %+v", res.Tasks)
	}
}

func TestNewManagerValidation(t *testing.T) {
	store := newTestStore(t)
	if _, err := NewManager(ManagerConfig{Store: store}); err == nil {
		t.Fatal("expected missing runner error")
	}
	if _, err := NewManager(ManagerConfig{Runner: &fakeRunner{}}); err == nil {
		t.Fatal("expected missing store error")
	}
	manager, err := NewManager(ManagerConfig{Runner: &fakeRunner{}, Store: store})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := manager.Run(context.Background(), RunRequest{}); err == nil {
		t.Fatal("expected empty assignments error")
	}
}
