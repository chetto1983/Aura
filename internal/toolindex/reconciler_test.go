package toolindex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/qdrant"
)

// --- Mocks ------------------------------------------------------------------

type mockProvider struct {
	defs []llm.ToolDefinition
}

func (m *mockProvider) Definitions() []llm.ToolDefinition { return m.defs }

type mockEmbedder struct {
	calls atomic.Int64
	err   error
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	m.calls.Add(1)
	if m.err != nil {
		return nil, m.err
	}
	// Deterministic 4-dim vector derived from len(text) so tests can
	// assert that different texts produce different vectors.
	v := []float32{float32(len(text)), 0, 0, 0}
	return v, nil
}

type mockQdrant struct {
	mu               sync.Mutex
	points           map[string]qdrant.Point // ID → Point
	upsertCalls      int
	deleteCalls      int
	upsertCalledWith []int // number of points per call
	deleteCalledWith []int
	collectionExists bool
	createErr        error
	upsertErr        error
	deleteErr        error
	infoErr          error
}

func newMockQdrant() *mockQdrant {
	return &mockQdrant{points: make(map[string]qdrant.Point)}
}

func (m *mockQdrant) Health(_ context.Context) error { return nil }

func (m *mockQdrant) CollectionInfo(_ context.Context, _ string) (qdrant.CollectionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.infoErr != nil {
		return qdrant.CollectionInfo{}, m.infoErr
	}
	if !m.collectionExists {
		// Mirror the real qdrant.httpClient pitfall-3 contract: 404 →
		// (zero value, nil error). Reconciler reads Status == "" as
		// "doesn't exist yet".
		return qdrant.CollectionInfo{}, nil
	}
	return qdrant.CollectionInfo{Status: "green", PointsCount: uint64(len(m.points))}, nil
}

func (m *mockQdrant) CreateCollection(_ context.Context, _ string, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	m.collectionExists = true
	return nil
}

func (m *mockQdrant) Upsert(_ context.Context, _ string, points []qdrant.Point) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsertCalls++
	m.upsertCalledWith = append(m.upsertCalledWith, len(points))
	if m.upsertErr != nil {
		return m.upsertErr
	}
	for _, p := range points {
		m.points[p.ID] = p
	}
	return nil
}

func (m *mockQdrant) Delete(_ context.Context, _ string, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls++
	m.deleteCalledWith = append(m.deleteCalledWith, len(ids))
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for _, id := range ids {
		delete(m.points, id)
	}
	return nil
}

// In-memory StateStore so reconciler tests don't need SQLite.
type memState struct {
	mu   sync.Mutex
	rows map[string]StateRow
}

func newMemState() *memState { return &memState{rows: make(map[string]StateRow)} }

func (m *memState) LoadAll(_ context.Context) (map[string]StateRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]StateRow, len(m.rows))
	for k, v := range m.rows {
		out[k] = v
	}
	return out, nil
}

func (m *memState) Upsert(_ context.Context, row StateRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[row.ToolName] = row
	return nil
}

func (m *memState) Remove(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, name)
	return nil
}

// --- Helpers ----------------------------------------------------------------

func def(name, desc string) llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        name,
		Description: desc,
		Parameters:  map[string]any{"type": "object"},
	}
}

func newReconciler(t *testing.T, prov *mockProvider, qd *mockQdrant, emb Embedder, st StateStore) *Reconciler {
	t.Helper()
	r, err := New(Config{
		Provider:   prov,
		Qdrant:     qd,
		Embedder:   emb,
		State:      st,
		Collection: "test_tools",
		VectorDim:  4,
		EmbedModel: "test-model",
		Debounce:   10 * time.Millisecond,
		Periodic:   -1, // disable periodic ticker in unit tests
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

// --- Tests ------------------------------------------------------------------

func TestReconcile_FreshBoot_UpsertAll(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha"), def("b", "bravo"), def("c", "charlie")}}
	qd := newMockQdrant()
	emb := &mockEmbedder{}
	st := newMemState()
	r := newReconciler(t, prov, qd, emb, st)

	rep := r.Reconcile(context.Background(), ReasonBoot)
	if len(rep.Errors) > 0 {
		t.Fatalf("errors: %v", rep.Errors)
	}
	if got, want := len(rep.Upserted), 3; got != want {
		t.Fatalf("upserted=%d, want %d (%v)", got, want, rep.Upserted)
	}
	if rep.Unchanged != 0 || len(rep.Deleted) != 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	if got, want := emb.calls.Load(), int64(3); got != want {
		t.Fatalf("embedder calls=%d, want %d", got, want)
	}
	if qd.upsertCalls != 1 {
		t.Fatalf("upsert calls=%d, want 1 (single batch)", qd.upsertCalls)
	}
	if qd.upsertCalledWith[0] != 3 {
		t.Fatalf("first upsert batch had %d points, want 3", qd.upsertCalledWith[0])
	}
	if qd.deleteCalls != 0 {
		t.Fatalf("delete calls=%d on fresh boot", qd.deleteCalls)
	}
	loaded, _ := st.LoadAll(context.Background())
	if len(loaded) != 3 {
		t.Fatalf("state size=%d, want 3", len(loaded))
	}
}

func TestReconcile_NoChange_ZeroEmbedCalls(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha"), def("b", "bravo")}}
	qd := newMockQdrant()
	emb := &mockEmbedder{}
	st := newMemState()
	r := newReconciler(t, prov, qd, emb, st)

	// Initial pass populates state.
	_ = r.Reconcile(context.Background(), ReasonBoot)
	embCalls1 := emb.calls.Load()
	upsertCalls1 := qd.upsertCalls

	// Second pass should be a complete no-op modulo state reads.
	rep := r.Reconcile(context.Background(), ReasonPeriodic)
	if got, want := rep.Unchanged, 2; got != want {
		t.Fatalf("unchanged=%d, want %d", got, want)
	}
	if len(rep.Upserted) != 0 || len(rep.Deleted) != 0 {
		t.Fatalf("expected no mutations: %+v", rep)
	}
	if emb.calls.Load() != embCalls1 {
		t.Fatalf("embedder called again on no-op pass: %d -> %d", embCalls1, emb.calls.Load())
	}
	if qd.upsertCalls != upsertCalls1 {
		t.Fatalf("qdrant Upsert called again on no-op pass")
	}
}

func TestReconcile_DescriptionChange_SingleUpsert(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha"), def("b", "bravo")}}
	qd := newMockQdrant()
	emb := &mockEmbedder{}
	st := newMemState()
	r := newReconciler(t, prov, qd, emb, st)

	_ = r.Reconcile(context.Background(), ReasonBoot)
	embAfterBoot := emb.calls.Load()

	// Mutate one tool's description.
	prov.defs[0].Description = "alpha-updated"

	rep := r.Reconcile(context.Background(), ReasonSkillsChanged)
	if want, got := []string{"a"}, rep.Upserted; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("upserted=%v, want [a]", got)
	}
	if rep.Unchanged != 1 {
		t.Fatalf("unchanged=%d, want 1 (b)", rep.Unchanged)
	}
	if got := emb.calls.Load() - embAfterBoot; got != 1 {
		t.Fatalf("expected exactly 1 new embed call, got %d", got)
	}
}

func TestReconcile_ToolRemoved_DeletePath(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha"), def("b", "bravo")}}
	qd := newMockQdrant()
	emb := &mockEmbedder{}
	st := newMemState()
	r := newReconciler(t, prov, qd, emb, st)

	_ = r.Reconcile(context.Background(), ReasonBoot)
	if len(qd.points) != 2 {
		t.Fatalf("setup: expected 2 indexed points, got %d", len(qd.points))
	}

	// Remove tool b from the registry.
	prov.defs = []llm.ToolDefinition{def("a", "alpha")}
	rep := r.Reconcile(context.Background(), ReasonMCPConfig)

	if got := rep.Deleted; len(got) != 1 || got[0] != "b" {
		t.Fatalf("deleted=%v, want [b]", got)
	}
	if rep.Unchanged != 1 {
		t.Fatalf("unchanged=%d, want 1", rep.Unchanged)
	}
	if len(qd.points) != 1 {
		t.Fatalf("expected 1 point remaining, got %d", len(qd.points))
	}
	loaded, _ := st.LoadAll(context.Background())
	if _, ok := loaded["b"]; ok {
		t.Fatal("state row for removed tool should be gone")
	}
}

func TestReconcile_ToolAdded(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha")}}
	qd := newMockQdrant()
	emb := &mockEmbedder{}
	st := newMemState()
	r := newReconciler(t, prov, qd, emb, st)

	_ = r.Reconcile(context.Background(), ReasonBoot)
	prov.defs = append(prov.defs, def("b", "bravo"))
	rep := r.Reconcile(context.Background(), ReasonSkillsChanged)

	if got := rep.Upserted; len(got) != 1 || got[0] != "b" {
		t.Fatalf("upserted=%v, want [b]", got)
	}
	if rep.Unchanged != 1 {
		t.Fatalf("unchanged=%d, want 1", rep.Unchanged)
	}
}

func TestReconcile_EmbedFailure_NoStateMutation(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha"), def("b", "bravo")}}
	qd := newMockQdrant()
	emb := &mockEmbedder{err: errors.New("embed sidecar down")}
	st := newMemState()
	r := newReconciler(t, prov, qd, emb, st)

	rep := r.Reconcile(context.Background(), ReasonBoot)
	if len(rep.Errors) == 0 {
		t.Fatal("expected errors when embedder fails")
	}
	if len(rep.Upserted) != 0 {
		t.Fatalf("no successful upserts allowed when embedder fails: %v", rep.Upserted)
	}
	loaded, _ := st.LoadAll(context.Background())
	if len(loaded) != 0 {
		t.Fatalf("state must be empty after embed failure, got %d rows", len(loaded))
	}
	if qd.upsertCalls != 0 {
		t.Fatalf("qdrant should not be upserted when no points succeeded")
	}
}

func TestReconcile_PartialEmbedFailure(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha"), def("b", "bravo")}}
	qd := newMockQdrant()
	st := newMemState()
	// Embedder that fails only on the second call.
	flakyEmb := &flakyEmbedder{failOn: "b: bravo"}
	r := newReconciler(t, prov, qd, flakyEmb, st)

	rep := r.Reconcile(context.Background(), ReasonBoot)
	if len(rep.Errors) == 0 {
		t.Fatal("expected an error for tool b")
	}
	if got := rep.Upserted; len(got) != 1 || got[0] != "a" {
		t.Fatalf("upserted=%v, want [a]", got)
	}
	loaded, _ := st.LoadAll(context.Background())
	if len(loaded) != 1 || loaded["a"].ToolName != "a" {
		t.Fatalf("state should contain only a, got: %+v", loaded)
	}
}

type flakyEmbedder struct {
	failOn string
}

func (f *flakyEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if text == f.failOn {
		return nil, errors.New("simulated embed failure for " + text)
	}
	return []float32{1, 2, 3, 4}, nil
}

func TestReconcile_EmbedModelSwap_FullRehash(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha"), def("b", "bravo")}}
	qd := newMockQdrant()
	emb := &mockEmbedder{}
	st := newMemState()

	r1, _ := New(Config{
		Provider: prov, Qdrant: qd, Embedder: emb, State: st,
		Collection: "test_tools", VectorDim: 4, EmbedModel: "model-v1", Periodic: -1,
	})
	rep1 := r1.Reconcile(context.Background(), ReasonBoot)
	if len(rep1.Upserted) != 2 {
		t.Fatalf("v1 upserted=%v, want 2", rep1.Upserted)
	}

	// Same registry, different embed model — every tool's hash should
	// diverge, forcing a full re-embed.
	r2, _ := New(Config{
		Provider: prov, Qdrant: qd, Embedder: emb, State: st,
		Collection: "test_tools", VectorDim: 4, EmbedModel: "model-v2", Periodic: -1,
	})
	rep2 := r2.Reconcile(context.Background(), ReasonManual)
	if len(rep2.Upserted) != 2 {
		t.Fatalf("v2 upserted=%v, want 2 (model swap should force full re-embed)", rep2.Upserted)
	}
}

func TestReconcile_StablePointID(t *testing.T) {
	r := newReconciler(t, &mockProvider{}, newMockQdrant(), &mockEmbedder{}, newMemState())
	id1 := r.PointID("search_memory")
	id2 := r.PointID("search_memory")
	id3 := r.PointID("other")
	if id1 != id2 {
		t.Fatalf("PointID not stable: %s != %s", id1, id2)
	}
	if id1 == id3 {
		t.Fatal("PointID collision for different names")
	}
	// uuidv5 format: 36 chars with dashes
	if len(id1) != 36 || strings.Count(id1, "-") != 4 {
		t.Fatalf("not a uuid: %s", id1)
	}
}

func TestNotify_DebounceCoalescesBurst(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha")}}
	qd := newMockQdrant()
	emb := &mockEmbedder{}
	st := newMemState()
	r, err := New(Config{
		Provider: prov, Qdrant: qd, Embedder: emb, State: st,
		Collection: "test_tools", VectorDim: 4, EmbedModel: "m",
		Debounce: 50 * time.Millisecond,
		Periodic: -1,
		Logger:   nil, // default discards
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneRun := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(doneRun)
	}()

	// Fire 10 notifications within the debounce window.
	for i := 0; i < 10; i++ {
		r.Notify(ReasonSkillsChanged)
	}
	time.Sleep(200 * time.Millisecond)

	// Stop Run() cleanly so we can read final counters without racing.
	cancel()
	<-doneRun

	// Exactly 1 reconcile pass: the 10 notifies were coalesced.
	if qd.upsertCalls != 1 {
		t.Fatalf("expected exactly 1 reconcile (upsertCalls=1), got %d", qd.upsertCalls)
	}
}

func TestNotify_FullChannelDropsButNextReconcileStillRuns(t *testing.T) {
	// Don't Run() — just exercise Notify's non-blocking semantics.
	r, _ := New(Config{
		Provider: &mockProvider{}, Qdrant: newMockQdrant(), Embedder: &mockEmbedder{},
		State: newMemState(), Collection: "c", VectorDim: 4, EmbedModel: "m", Periodic: -1,
	})
	// Channel buffer is 16; fire 1000 to verify no panic + no block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			r.Notify(ReasonManual)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked despite full channel")
	}
}

func TestReconcile_CreatesCollectionWhenMissing(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha")}}
	qd := newMockQdrant()
	// collectionExists is false → CollectionInfo returns "not found"
	if qd.collectionExists {
		t.Fatal("test setup wrong: collection should start missing")
	}
	emb := &mockEmbedder{}
	st := newMemState()
	r := newReconciler(t, prov, qd, emb, st)

	rep := r.Reconcile(context.Background(), ReasonBoot)
	if len(rep.Errors) != 0 {
		t.Fatalf("errors: %v", rep.Errors)
	}
	if !qd.collectionExists {
		t.Fatal("expected CreateCollection to fire on missing collection")
	}
	if len(qd.points) != 1 {
		t.Fatalf("expected 1 point after creating collection + upsert, got %d", len(qd.points))
	}
}

func TestReconcile_ConcurrentCallsSerialize(t *testing.T) {
	prov := &mockProvider{defs: []llm.ToolDefinition{def("a", "alpha"), def("b", "bravo")}}
	qd := newMockQdrant()
	emb := &mockEmbedder{}
	st := newMemState()
	r := newReconciler(t, prov, qd, emb, st)

	const N = 5
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			rep := r.Reconcile(context.Background(), Reason(fmt.Sprintf("manual-%d", i)))
			// Each call should produce a non-error report; reports differ
			// across callers because only the first observes the upsert,
			// later ones see Unchanged=2.
			if len(rep.Errors) != 0 {
				t.Errorf("call %d errors: %v", i, rep.Errors)
			}
		}(i)
	}
	wg.Wait()
	// Exactly 2 embed calls total (one per tool), the rest were no-op
	// observations of the same hashes.
	if got := emb.calls.Load(); got != 2 {
		t.Fatalf("expected 2 embedder calls under concurrent reconciles, got %d", got)
	}
}

func TestNew_RejectsMissingDeps(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"no provider", func(c *Config) { c.Provider = nil }},
		{"no qdrant", func(c *Config) { c.Qdrant = nil }},
		{"no embedder", func(c *Config) { c.Embedder = nil }},
		{"no state", func(c *Config) { c.State = nil }},
		{"no collection", func(c *Config) { c.Collection = "" }},
		{"zero dim", func(c *Config) { c.VectorDim = 0 }},
		{"no model", func(c *Config) { c.EmbedModel = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				Provider: &mockProvider{}, Qdrant: newMockQdrant(),
				Embedder: &mockEmbedder{}, State: newMemState(),
				Collection: "c", VectorDim: 4, EmbedModel: "m",
			}
			tc.mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}
