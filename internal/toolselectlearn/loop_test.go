package toolselectlearn

import (
	"context"
	"sync"
	"testing"
	"time"
)

// --- Req-5a: detection recall on the spike-057 12-trace fixture ---

// trace is one observed agent turn from the spike-057 fixture: the user request, the
// tool the model ACTUALLY used, and the gold tool that SHOULD have handled it.
// gold==used => an efficient turn the detector must NOT flag; gold!=used => a real
// mis-route the detector must catch.
type trace struct {
	q, used, gold string
}

// spike057Traces is the spike-057 labeled trace set (7 mis-routes + 5 efficient).
// Copied verbatim from .planning/spikes/057-toolselection-oracle-signal/main.go.
var spike057Traces = []trace{
	// documented + plausible mis-routes (gold != used)
	{"quanto costa il bitcoin adesso?", "shell_exec", "web_search"},
	{"che tempo fa a Caraglio domani?", "shell_exec", "web_search"},
	{"ultime notizie su Cuneo di oggi", "text_response", "web_search"},
	{"ricorda che preferisco il caffe senza zucchero", "text_response", "memory_add_preference"},
	{"cerca nei miei documenti il manuale della caldaia", "shell_exec", "document_search"},
	{"scarica e riassumi questo articolo dal link", "fs_read", "web_fetch"},
	{"manda una mail a Marco con il riepilogo", "text_response", "send_email"},
	// efficient turns (gold == used) — the detector must NOT flag these
	{"esegui questo script python per processare il csv", "shell_exec", "shell_exec"},
	{"manda questo file excel all'utente come allegato", "send_file", "send_file"},
	{"scrivimi su whatsapp un messaggio quando il job e finito", "send_message", "send_message"},
	{"fai una web search per le previsioni meteo", "web_search", "web_search"},
	{"trova un ristorante aperto stasera vicino a me", "web_search", "web_search"},
}

// goldRanker is a deterministic fake Ranker that returns each request's GOLD tool as
// its top-1 — the spike-057 premise that the free embedding ranker routes the request
// correctly even when the model improvised. It lets the detection-recall test run free
// in CI (no granite sidecar): the disagreement signal fires exactly when used != gold.
type goldRanker struct {
	gold   map[string]string
	margin float64
}

func newGoldRanker(margin float64) *goldRanker {
	m := make(map[string]string, len(spike057Traces))
	for _, tr := range spike057Traces {
		m[tr.q] = tr.gold
	}
	return &goldRanker{gold: m, margin: margin}
}

func (g *goldRanker) Rank(_ context.Context, query string) (string, float64, bool) {
	t, ok := g.gold[query]
	return t, g.margin, ok
}

func TestDetection_RecallOnSpike057Traces(t *testing.T) {
	ranker := newGoldRanker(0.2) // confident, so the disagreement signal is trusted
	var tp, fn, fp, tn int
	for _, tr := range spike057Traces {
		flagged := isMisroute(context.Background(), tr.q, tr.used, ranker)
		misroute := !sameTool(tr.used, tr.gold)
		switch {
		case flagged && misroute:
			tp++
		case !flagged && misroute:
			fn++
		case flagged && !misroute:
			fp++
		default:
			tn++
		}
	}
	recall := float64(tp) / float64(tp+fn)
	if recall < 0.85 {
		t.Errorf("detection recall = %.2f (TP%d FN%d FP%d TN%d); want >= 0.85 (Req-5a, spike-057 R=1.00)", recall, tp, fn, fp, tn)
	}
	// Precision sanity: spike-057's union signal measured P=0.88 — the single FP is the
	// "esegui script python" turn where shell_exec is LEGITIMATELY the gold tool, so the
	// shell-fallback heuristic flags it. That FP is harmless (it confirms an idempotent
	// shell_exec->shell_exec label) and is the documented spike tradeoff for R=1.00. We
	// gate precision at the spike floor rather than demanding FP=0 (which would require
	// dropping the embedding-free shell heuristic that catches the bitcoin class).
	precision := float64(tp) / float64(tp+fp)
	if precision < 0.85 {
		t.Errorf("detection precision = %.2f (TP%d FP%d); want >= 0.85 (spike-057 P=0.88)", precision, tp, fp)
	}
}

// --- RESEARCH risk #3: sameTool is symmetric, splits on "__", NOT HasSuffix ---

func TestSameTool_SymmetricNamespaceSplit(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"whatsapp__send_message", "send_message", true},
		{"send_message", "whatsapp__send_message", true}, // BOTH orders
		{"web_search", "web_search", true},
		{"telegram__send_message", "whatsapp__send_message", true}, // same bare name, two namespaces
		{"web_search", "search", false},                            // NOT a suffix match (the risk #3 spurious FP)
		{"search", "web_search", false},
		{"shell_exec", "web_search", false},
		{"a__b__send_file", "send_file", true}, // multi-segment namespace, last "__" wins
	}
	for _, c := range cases {
		if got := sameTool(c.a, c.b); got != c.want {
			t.Errorf("sameTool(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
		// Symmetry: the answer MUST be order-independent.
		if sameTool(c.a, c.b) != sameTool(c.b, c.a) {
			t.Errorf("sameTool not symmetric for (%q,%q)", c.a, c.b)
		}
	}
}

// --- Req-5a fallback heuristic: shell/fs improvisation flagged with no ranker ---

func TestDetection_ShellFallbackNeedsNoRanker(t *testing.T) {
	ctx := context.Background()
	// The bitcoin failure mode: shell_exec used, no ranker wired at all. The embedding-
	// free heuristic must still flag it (P0.88/R1.00 alone, spike-057).
	if !isMisroute(ctx, "quanto costa il bitcoin adesso?", "shell_exec", nil) {
		t.Error("shell_exec improvisation must be flagged even with a nil ranker")
	}
	// A non-fallback tool with no ranker cannot be judged a mis-route (no signal).
	if isMisroute(ctx, "manda questo file all'utente", "send_file", nil) {
		t.Error("a non-fallback tool with no ranker must not be flagged (no disagreement signal)")
	}
	// An empty request or empty used-tool is never a mis-route.
	if isMisroute(ctx, "", "shell_exec", nil) {
		t.Error("empty request must not be flagged")
	}
	if isMisroute(ctx, "some request", "", nil) {
		t.Error("empty used-tool must not be flagged")
	}
}

// --- T-08.2-16 / risk #7: the oracle kill-switch off makes no paid call ---

// countingTeacher records how many times the paid DeepSeek teacher was invoked.
type countingTeacher struct {
	mu    sync.Mutex
	calls int
	tool  string
}

func (c *countingTeacher) Label(_ context.Context, _ string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.tool, c.tool != ""
}

func (c *countingTeacher) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// recordingSaver records every saved (query, tool) pair.
type recordingSaver struct {
	mu    sync.Mutex
	saved [][2]string
}

func (r *recordingSaver) Save(_ context.Context, query, tool string, _ []float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saved = append(r.saved, [2]string{query, tool})
	return nil
}

func (r *recordingSaver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.saved)
}

func TestOracle_KillSwitchOff_NoPaidCall(t *testing.T) {
	t.Setenv(oracleEnv, "off")
	teacher := &countingTeacher{tool: "web_search"}
	saver := &recordingSaver{}
	// Low-margin ranker => the free tier declines => the loop would normally escalate.
	o := twoTierOracle{ranker: newGoldRanker(0.0), teacher: teacher, saver: saver}

	saved := o.LabelAndSave(context.Background(), "quanto costa il bitcoin adesso?", []float64{1, 0, 0})
	if saved {
		t.Error("with the kill-switch off and a low-margin ranker, no label should commit")
	}
	if teacher.count() != 0 {
		t.Errorf("the paid teacher was called %d times with the kill-switch off; want 0 (no-paid-runs guardrail)", teacher.count())
	}
	if saver.count() != 0 {
		t.Errorf("saved %d examples with no committed label; want 0", saver.count())
	}
}

func TestOracle_FreeTierConfident_NoPaidCall(t *testing.T) {
	t.Setenv(oracleEnv, "on")
	teacher := &countingTeacher{tool: "wrong_tool"}
	saver := &recordingSaver{}
	// A CONFIDENT ranker => the free tier labels => the paid teacher is never reached.
	o := twoTierOracle{ranker: newGoldRanker(0.3), teacher: teacher, saver: saver}

	saved := o.LabelAndSave(context.Background(), "quanto costa il bitcoin adesso?", []float64{1, 0, 0})
	if !saved {
		t.Fatal("a confident free-tier label should commit")
	}
	if teacher.count() != 0 {
		t.Errorf("the paid teacher was called %d times for a confident free case; want 0", teacher.count())
	}
	if saver.count() != 1 || saver.saved[0][1] != "web_search" {
		t.Errorf("free-tier label not saved as the ranker top-1: %+v", saver.saved)
	}
}

func TestOracle_LowMarginEscalatesWhenOn(t *testing.T) {
	t.Setenv(oracleEnv, "on")
	teacher := &countingTeacher{tool: "web_search"}
	saver := &recordingSaver{}
	// Low-margin ranker + escalation ON => the hard case rides the DeepSeek teacher.
	o := twoTierOracle{ranker: newGoldRanker(0.0), teacher: teacher, saver: saver}

	saved := o.LabelAndSave(context.Background(), "quanto costa il bitcoin adesso?", []float64{1, 0, 0})
	if !saved {
		t.Fatal("a low-margin case with escalation on should commit via the teacher")
	}
	if teacher.count() != 1 {
		t.Errorf("the teacher was called %d times; want exactly 1 (the low-margin escalation)", teacher.count())
	}
	if saver.count() != 1 || saver.saved[0][1] != "web_search" {
		t.Errorf("escalated label not saved: %+v", saver.saved)
	}
}

// --- D-05: the loop reuses internal/activelearn (Observe rides the shared core) ---

// fakeEmbedder embeds each text to a fixed one-hot-ish vector; deterministic, no
// sidecar. The Learner.Observe path embeds the flagged request before enqueueing.
type fakeEmbedder struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = []float64{1, 0, 0}
	}
	return out, nil
}

func (f *fakeEmbedder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestLearner_ObserveRidesActiveLearn(t *testing.T) {
	t.Setenv(oracleEnv, "on")
	emb := &fakeEmbedder{}
	saver := &recordingSaver{}
	done := make(chan struct{})
	l := New(Config{
		Embedder: emb,
		Ranker:   newGoldRanker(0.3), // confident free label
		Saver:    saver,
		Refresh:  func() { close(done) },
		Queue:    8,
	})
	if l == nil {
		t.Fatal("New returned nil with embedder+saver wired")
	}
	defer l.Close()

	// A flagged mis-route (shell_exec improvisation). Observe is a non-blocking handoff;
	// the intake worker embeds + enqueues, then the activelearn worker labels + saves +
	// fires Refresh — all asynchronously (CR-01).
	l.Observe("quanto costa il bitcoin adesso?", "shell_exec")
	<-done // the shared activelearn worker processed the observation

	if saver.count() != 1 {
		t.Errorf("expected the flagged turn to be labeled+saved once; got %d", saver.count())
	}
	if emb.count() == 0 {
		t.Error("the intake worker should have embedded the flagged request")
	}
}

func TestLearner_UnflaggedTurnIsNoOp(t *testing.T) {
	emb := &fakeEmbedder{}
	saver := &recordingSaver{}
	l := New(Config{Embedder: emb, Ranker: newGoldRanker(0.3), Saver: saver, Queue: 8})

	// An efficient turn (used == gold) must not embed or enqueue anything. Observe is an
	// async handoff (CR-01), so join the intake+activelearn workers via Close before
	// asserting — the unflagged signal is processed (or dropped) but never embeds/saves.
	l.Observe("fai una web search per le previsioni meteo", "web_search")
	l.Close()
	if emb.count() != 0 {
		t.Errorf("an unflagged turn embedded %d times; want 0 (intake worker short-circuits)", emb.count())
	}
	if saver.count() != 0 {
		t.Errorf("an unflagged turn saved %d examples; want 0", saver.count())
	}
}

func TestNew_NilWithoutRequiredDeps(t *testing.T) {
	if New(Config{Saver: &recordingSaver{}}) != nil {
		t.Error("New must return nil without an Embedder")
	}
	if New(Config{Embedder: &fakeEmbedder{}}) != nil {
		t.Error("New must return nil without a Saver")
	}
}

// blockingEmbedder blocks every Embed call until its ctx is cancelled (or the test
// releases it). It models a hung granite sidecar — the exact failure CR-01 must keep
// off the turn goroutine. It also proves the intake worker's embed is bounded by the
// learner's lifetime ctx (Close aborts it).
type blockingEmbedder struct {
	entered chan struct{} // closed-on-first-entry signal so the test knows the worker reached Embed
	once    sync.Once
}

func (b *blockingEmbedder) Embed(ctx context.Context, _ []string) ([][]float64, error) {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done() // block until the learner's lifetime ctx is cancelled (Close)
	return nil, ctx.Err()
}

// TestObserve_NonBlockingEvenWhenEmbedderHangs is the CR-01 regression guard: a hung
// embed sidecar must NOT delay the turn. Observe runs on the synchronous, lock-held
// turn path, so it must return promptly regardless of embed latency — the detect+embed
// work happens on the intake worker. Without the fix, Observe embedded inline and would
// block here for the full sidecar timeout (or forever).
func TestObserve_NonBlockingEvenWhenEmbedderHangs(t *testing.T) {
	emb := &blockingEmbedder{entered: make(chan struct{})}
	saver := &recordingSaver{}
	// A nil ranker => the shell-fallback heuristic alone flags shell_exec, so the intake
	// worker proceeds to the (blocking) exemplar embed without needing a ranker round-trip.
	l := New(Config{Embedder: emb, Saver: saver, Queue: 8})
	if l == nil {
		t.Fatal("New returned nil with embedder+saver wired")
	}

	// Observe must return effectively instantly even though the embedder will hang. We
	// bound it generously: a regression (inline embed) would block until Close, far past
	// this. The turn goroutine never touches the embed.
	returned := make(chan struct{})
	go func() {
		l.Observe("quanto costa il bitcoin adesso?", "shell_exec") // flagged (shell_exec fallback)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("Observe blocked on a hung embedder; CR-01 regression (must be a non-blocking handoff)")
	}

	// Prove the work really reached the worker's embed (so the test isn't vacuously green)
	// and that Close cancels the in-flight embed (goleak-clean, bounded).
	select {
	case <-emb.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("intake worker never reached the embed for a flagged turn")
	}
	l.Close() // cancels the lifetime ctx → unblocks the embedder → joins both workers
	if saver.count() != 0 {
		t.Errorf("a cancelled embed must not persist an exemplar; saved %d", saver.count())
	}
}
