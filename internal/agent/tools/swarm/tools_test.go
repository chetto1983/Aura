package swarmtools

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/swarm"
)

type fakeRunner struct {
	mu       sync.Mutex
	last     agent.Task
	tasks    []agent.Task
	actorIDs []string
}

func (r *fakeRunner) Run(ctx context.Context, task agent.Task) (agent.Result, error) {
	r.mu.Lock()
	r.last = task
	r.tasks = append(r.tasks, task)
	r.actorIDs = append(r.actorIDs, identity.ActorIDFromContext(ctx))
	r.mu.Unlock()
	return agent.Result{
		Content:   "worker result",
		LLMCalls:  2,
		ToolCalls: len(task.ToolAllowlist),
		Tokens:    llm.TokenUsage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
		Elapsed:   12 * time.Millisecond,
	}, nil
}

func (r *fakeRunner) snapshot() (agent.Task, []agent.Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tasks := append([]agent.Task(nil), r.tasks...)
	return r.last, tasks
}

func (r *fakeRunner) actors() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.actorIDs...)
}

func assertSwarmToolScalar[T comparable](t *testing.T, db *sql.DB, query string, want T, args ...any) {
	t.Helper()
	var got T
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %v, want %v", query, got, want)
	}
}

func newToolTest(t *testing.T) (*swarm.Store, *fakeRunner, *swarm.Manager) {
	t.Helper()
	store, err := swarm.OpenStore(filepath.Join(t.TempDir(), "swarm.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	runner := &fakeRunner{}
	manager, err := swarm.NewManager(swarm.ManagerConfig{Runner: runner, Store: store, MaxActive: 2, MaxDepth: 1})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return store, runner, manager
}

func authorizedSwarmToolContext(t *testing.T, store *swarm.Store, userID string) context.Context {
	t.Helper()
	identityStore, err := identity.NewStore(store.DB())
	if err != nil {
		t.Fatalf("identity.NewStore: %v", err)
	}
	if _, err := identityStore.BackfillTelegramAllowlistedUser(context.Background(), identity.TelegramAllowlistUserParams{
		UserID:        userID,
		Source:        "test",
		PrincipalKind: identity.PrincipalKindOwner,
		Capabilities:  []identity.Capability{identity.CapabilitySwarmSpawn, identity.CapabilityToolExecute},
	}); err != nil {
		t.Fatalf("BackfillTelegramAllowlistedUser: %v", err)
	}
	ctx := tools.WithUserID(context.Background(), userID)
	ctx = identity.WithAuthority(ctx, identityStore)
	return identity.WithActorID(ctx, identity.TelegramSessionActorID(userID))
}

func TestSpawnAuraBotTool(t *testing.T) {
	store, runner, manager := newToolTest(t)
	tool := NewSpawnAuraBotTool(manager)
	ctx := authorizedSwarmToolContext(t, store, "user-123")
	out, err := tool.Execute(ctx, map[string]any{
		"name":  "read context",
		"role":  "librarian",
		"task":  "read the wiki index",
		"tools": []any{"file"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp spawnResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK || resp.RunID == "" || resp.TaskID == "" {
		t.Fatalf("response = %+v", resp)
	}
	if resp.LLMCalls != 2 || resp.ToolCalls != 1 || resp.TokensTotal != 8 {
		t.Fatalf("metrics = %+v", resp)
	}
	last, _ := runner.snapshot()
	if last.UserID != "user-123" {
		t.Fatalf("runner user id = %q", last.UserID)
	}
	if len(last.ToolAllowlist) != 1 || last.ToolAllowlist[0] != "file" {
		t.Fatalf("allowlist = %+v", last.ToolAllowlist)
	}
	task, err := store.GetTask(context.Background(), resp.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != swarm.TaskCompleted || task.Result != "worker result" {
		t.Fatalf("task = %+v", task)
	}
}

func TestRunAuraBotSwarmTool(t *testing.T) {
	store, runner, manager := newToolTest(t)
	tool := NewRunAuraBotSwarmTool(manager)
	ctx := authorizedSwarmToolContext(t, store, "user-456")
	out, err := tool.Execute(ctx, map[string]any{
		"goal":  "audit wiki health",
		"roles": []any{"librarian", "critic", "synthesizer"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp runSwarmResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK || resp.RunID == "" || resp.Status != string(swarm.RunCompleted) {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Goal != "audit wiki health" || len(resp.Roles) != 1 || len(resp.Tasks) != 1 {
		t.Fatalf("plan response = %+v", resp)
	}
	if resp.Metrics.CompletedTasks != 1 || resp.Metrics.LLMCalls != 2 || resp.Metrics.TokensTotal != 8 {
		t.Fatalf("metrics = %+v", resp.Metrics)
	}
	if resp.Summary == "" {
		t.Fatal("missing synthesis summary")
	}
	_, tasks := runner.snapshot()
	if len(tasks) != 1 {
		t.Fatalf("runner tasks = %d", len(tasks))
	}
	for _, task := range tasks {
		if task.UserID != "user-456" {
			t.Fatalf("task user id = %q", task.UserID)
		}
		for _, toolName := range task.ToolAllowlist {
			if toolName == "write_file" || toolName == "append_log" || toolName == "task" {
				t.Fatalf("unsafe tool in allowlist: %+v", task.ToolAllowlist)
			}
		}
	}
}

func TestRunAuraBotSwarmToolRequiresSwarmSpawnCapabilityAndDelegatesWorkers(t *testing.T) {
	store, runner, manager := newToolTest(t)
	identityStore, err := identity.NewStore(store.DB())
	if err != nil {
		t.Fatalf("identity.NewStore: %v", err)
	}
	ctx := context.Background()
	if _, err := identityStore.BackfillTelegramAllowlistedUser(ctx, identity.TelegramAllowlistUserParams{
		UserID:        "999",
		Source:        "test",
		PrincipalKind: identity.PrincipalKindOwner,
		Capabilities:  []identity.Capability{identity.CapabilityToolExecute},
	}); err != nil {
		t.Fatalf("BackfillTelegramAllowlistedUser: %v", err)
	}
	parentActorID := identity.TelegramSessionActorID("999")
	runCtx := identity.WithActorID(identity.WithAuthority(ctx, identityStore), parentActorID)
	registry := tools.NewRegistry(nil)
	registry.Register(NewRunAuraBotSwarmTool(manager))

	_, err = registry.Execute(runCtx, "run_aurabot_swarm", map[string]any{"goal": "audit wiki health"})
	if err == nil {
		t.Fatal("expected missing swarm.spawn grant to fail")
	}
	assertSwarmToolScalar(t, store.DB(), `SELECT COUNT(*) FROM authz_decisions WHERE actor_id = ? AND capability = 'swarm.spawn' AND decision = 'deny'`, int64(1), parentActorID)
	_, tasks := runner.snapshot()
	if len(tasks) != 0 {
		t.Fatalf("runner tasks before swarm.spawn grant = %d, want 0", len(tasks))
	}

	if _, err := identityStore.CreateGrant(ctx, identity.GrantParams{
		ID:          "grant-parent-swarm",
		SubjectType: identity.SubjectTypePrincipal,
		SubjectID:   identity.TelegramPrincipalID("999"),
		Capability:  identity.CapabilitySwarmSpawn,
	}); err != nil {
		t.Fatalf("CreateGrant swarm.spawn: %v", err)
	}
	out, err := registry.Execute(runCtx, "run_aurabot_swarm", map[string]any{"goal": "audit wiki health"})
	if err != nil {
		t.Fatalf("Execute with swarm.spawn: %v\nout=%s", err, out)
	}
	actors := runner.actors()
	if len(actors) != 1 || actors[0] == "" || actors[0] == parentActorID {
		t.Fatalf("runner actors = %+v, parent = %q", actors, parentActorID)
	}
	assertSwarmToolScalar(t, store.DB(), `SELECT actor_type FROM actors WHERE id = ?`, "swarm", actors[0])
	assertSwarmToolScalar(t, store.DB(), `SELECT parent_actor_id FROM actors WHERE id = ?`, parentActorID, actors[0])
	assertSwarmToolScalar(t, store.DB(), `SELECT COUNT(*) FROM capability_grants WHERE subject_type = 'actor' AND subject_id = ? AND capability = 'tool.execute'`, int64(1), actors[0])
	assertSwarmToolScalar(t, store.DB(), `SELECT COUNT(*) FROM authz_decisions WHERE actor_id = ? AND capability = 'tool.execute' AND decision = 'allow'`, int64(1), parentActorID)
}

type fakeSwarmRunner struct {
	result swarm.RunResult
}

func (f fakeSwarmRunner) Run(context.Context, swarm.RunRequest) (swarm.RunResult, error) {
	return f.result, nil
}

func TestRunSwarmToolsAcceptRunnerInterface(t *testing.T) {
	completed := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	runner := fakeSwarmRunner{result: swarm.RunResult{
		Run: &swarm.Run{
			ID:          "swarm_1234567890abcdef",
			Goal:        "boundary",
			Status:      swarm.RunCompleted,
			CreatedAt:   completed.Add(-time.Minute),
			UpdatedAt:   completed,
			CompletedAt: &completed,
		},
		Tasks: []swarm.Task{{
			ID:          "task_1234567890abcdef",
			RunID:       "swarm_1234567890abcdef",
			Role:        "librarian",
			Status:      swarm.TaskCompleted,
			Result:      "done",
			CreatedAt:   completed.Add(-time.Minute),
			CompletedAt: &completed,
		}},
	}}
	if tool := NewRunAuraBotSwarmTool(runner); tool == nil {
		t.Fatal("expected run_aurabot_swarm tool")
	}
	if tool := NewSpawnAuraBotTool(runner); tool == nil {
		t.Fatal("expected spawn_aurabot tool")
	}
}

func TestRunAuraBotSwarmRejectsUnknownRole(t *testing.T) {
	_, _, manager := newToolTest(t)
	tool := NewRunAuraBotSwarmTool(manager)
	_, err := tool.Execute(context.Background(), map[string]any{
		"goal":  "write everything",
		"roles": []any{"librarian", "writer"},
	})
	if err == nil {
		t.Fatal("expected unknown role error")
	}
}

func TestSpawnAuraBotRejectsDisallowedTool(t *testing.T) {
	_, _, manager := newToolTest(t)
	tool := NewSpawnAuraBotTool(manager)
	_, err := tool.Execute(context.Background(), map[string]any{
		"role":  "librarian",
		"task":  "try a write",
		"tools": []any{"write_file"},
	})
	if err == nil {
		t.Fatal("expected disallowed tool error")
	}
}

func TestListAndReadSwarmTools(t *testing.T) {
	store, _, manager := newToolTest(t)
	spawn := NewSpawnAuraBotTool(manager)
	out, err := spawn.Execute(authorizedSwarmToolContext(t, store, "user-789"), map[string]any{
		"role": "critic",
		"task": "lint",
	})
	if err != nil {
		t.Fatalf("spawn Execute: %v", err)
	}
	var resp spawnResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal spawn: %v", err)
	}

	listOut, err := NewListSwarmTasksTool(store).Execute(context.Background(), map[string]any{"run_id": resp.RunID})
	if err != nil {
		t.Fatalf("list Execute: %v", err)
	}
	if listOut == "" || !json.Valid([]byte(listOut)) {
		t.Fatalf("list output not JSON: %q", listOut)
	}

	readOut, err := NewReadSwarmResultTool(store).Execute(context.Background(), map[string]any{"task_id": resp.TaskID})
	if err != nil {
		t.Fatalf("read Execute: %v", err)
	}
	var task taskSummary
	if err := json.Unmarshal([]byte(readOut), &task); err != nil {
		t.Fatalf("unmarshal read: %v", err)
	}
	if task.Result != "worker result" || task.Status != string(swarm.TaskCompleted) {
		t.Fatalf("task summary = %+v", task)
	}
}

type fakeSwarmTaskReader struct {
	tasks []swarm.Task
}

func (f fakeSwarmTaskReader) ListTasks(_ context.Context, runID string) ([]swarm.Task, error) {
	var out []swarm.Task
	for _, task := range f.tasks {
		if task.RunID == runID {
			out = append(out, task)
		}
	}
	return out, nil
}

func (f fakeSwarmTaskReader) GetTask(_ context.Context, id string) (*swarm.Task, error) {
	for _, task := range f.tasks {
		if task.ID == id {
			cp := task
			return &cp, nil
		}
	}
	return nil, context.Canceled
}

func TestListAndReadSwarmToolsAcceptTaskReaderInterfaces(t *testing.T) {
	completed := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	reader := fakeSwarmTaskReader{tasks: []swarm.Task{{
		ID:          "task_1234567890abcdef",
		RunID:       "swarm_1234567890abcdef",
		Role:        "critic",
		Subject:     "audit",
		Status:      swarm.TaskCompleted,
		Result:      "boundary works",
		CreatedAt:   completed.Add(-time.Minute),
		CompletedAt: &completed,
	}}}

	listOut, err := NewListSwarmTasksTool(reader).Execute(context.Background(), map[string]any{"run_id": "swarm_1234567890abcdef"})
	if err != nil {
		t.Fatalf("list Execute: %v", err)
	}
	if !json.Valid([]byte(listOut)) {
		t.Fatalf("list output not JSON: %q", listOut)
	}

	readOut, err := NewReadSwarmResultTool(reader).Execute(context.Background(), map[string]any{"task_id": "task_1234567890abcdef"})
	if err != nil {
		t.Fatalf("read Execute: %v", err)
	}
	if !json.Valid([]byte(readOut)) {
		t.Fatalf("read output not JSON: %q", readOut)
	}
}

// TestCatalogueSwarmToolMetadata verifies that every swarm tool has been
// explicitly catalogued with MCP hints + VisibilityTier. Swarm tools cannot
// be checked from the registry package scan test (import cycle), so they are
// covered here instead.
func TestCatalogueSwarmToolMetadata(t *testing.T) {
	isDefaultState := func(d tools.ToolDefinition) bool {
		return !d.ReadOnlyHint && !d.DestructiveHint && !d.IdempotentHint && !d.OpenWorldHint &&
			d.VisibilityTier == tools.VisibilityActiveTurn
	}

	// Use zero-value structs — Definition() methods are hardcoded and do not
	// read struct fields, so nil deps are safe for metadata validation.
	swarmTools := []interface{ Definition() tools.ToolDefinition }{
		&RunAuraBotSwarmTool{},
		&SpawnAuraBotTool{},
		&ListSwarmTasksTool{},
		&ReadSwarmResultTool{},
	}

	for _, tool := range swarmTools {
		def := tool.Definition()
		if isDefaultState(def) {
			t.Errorf("swarm tool %q has all-defaults metadata (not explicitly catalogued); "+
				"add explicit hints + VisibilityTier to its Definition() method", def.Name)
		}
	}
}
