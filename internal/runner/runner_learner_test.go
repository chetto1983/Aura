package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolselectstore"
)

// fakeEmbedder is a deterministic in-memory semindex.Embedder for the learner-wiring
// tests: it returns a fixed-dimension unit-ish vector keyed off the text length so two
// distinct texts get distinct vectors, and supports an injectable error for the
// sidecar-down path. No network.
type fakeEmbedder struct {
	mu  sync.Mutex
	err error
	n   int // number of Embed calls
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float64, len(texts))
	for i, t := range texts {
		// A 3-dim vector that varies with the text so distinct queries are distinct.
		seed := float64(len(t)%7) + 1
		out[i] = []float64{seed, seed / 2, 1}
	}
	return out, nil
}

func (f *fakeEmbedder) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

// fakeReasoningSaver is an in-memory reasoninglearn.Saver capturing saved examples.
type fakeReasoningSaver struct {
	mu    sync.Mutex
	saved []prompt.ReasoningTier
}

func (f *fakeReasoningSaver) Save(_ context.Context, _ string, _ []float64, tier prompt.ReasoningTier) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, tier)
	return nil
}

// fakeToolSelectSaver is an in-memory toolselectlearn.Saver that ALSO implements
// toolselectlearn.ExampleLoader so buildToolSelectLearner's Refresh closure exercises
// the type-assert + LoadExamples + FoldLearned re-fold path.
type fakeToolSelectSaver struct {
	mu       sync.Mutex
	saved    [][2]string // (query, tool)
	examples []toolselectstore.LabeledVec
	loadErr  error
	loaded   int
}

func (f *fakeToolSelectSaver) Save(_ context.Context, query, tool string, vec []float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, [2]string{query, tool})
	f.examples = append(f.examples, toolselectstore.LabeledVec{Tool: tool, Vec: vec, Query: query})
	return nil
}

func (f *fakeToolSelectSaver) LoadExamples(_ context.Context) ([]toolselectstore.LabeledVec, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loaded++
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	out := make([]toolselectstore.LabeledVec, len(f.examples))
	copy(out, f.examples)
	return out, nil
}

func (f *fakeToolSelectSaver) saveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saved)
}

// deferredTool is a minimal registry tool whose Spec is Deferred so deferredToolNames
// and the tool_search bank pick it up.
type deferredTool struct {
	name string
}

func (d deferredTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        d.name,
		Summary:     d.name + " summary",
		Description: d.name + " does a thing for testing the tool-selection catalog.",
		Deferred:    true,
	}
}

func (d deferredTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "ok")
}

// --- reasoningOracle.Label ---

func TestReasoningOracle_Label(t *testing.T) {
	t.Run("parses a valid tier from the stream", func(t *testing.T) {
		client := agenttest.NewFakeClient(agenttest.TextChunks("stop", `{"tier":"high"}`))
		o := &reasoningOracle{client: client, model: "test-model"}
		tier, ok := o.Label(context.Background(), "debug this stack trace")
		if !ok || tier != prompt.ReasoningTierHigh {
			t.Fatalf("Label = (%q, %v); want (high, true)", tier, ok)
		}
	})
	t.Run("stream error yields no label", func(t *testing.T) {
		client := agenttest.NewFakeClient(agenttest.FakeTurn{Err: errFake})
		o := &reasoningOracle{client: client, model: "test-model"}
		if tier, ok := o.Label(context.Background(), "x"); ok || tier != "" {
			t.Fatalf("Label on Stream error = (%q, %v); want ('', false)", tier, ok)
		}
	})
	t.Run("chunk error mid-stream yields no label", func(t *testing.T) {
		client := agenttest.NewFakeClient(agenttest.TextThenErr(errFake, "{\"tier\":"))
		o := &reasoningOracle{client: client, model: "test-model"}
		if tier, ok := o.Label(context.Background(), "x"); ok || tier != "" {
			t.Fatalf("Label on chunk error = (%q, %v); want ('', false)", tier, ok)
		}
	})
	t.Run("unparseable reply is an invalid tier", func(t *testing.T) {
		client := agenttest.NewFakeClient(agenttest.TextChunks("stop", "I cannot decide"))
		o := &reasoningOracle{client: client, model: "test-model"}
		if _, ok := o.Label(context.Background(), "x"); ok {
			t.Fatal("Label on unparseable reply must report ok=false")
		}
	})
}

// --- buildReasoningLearner ---

func TestBuildReasoningLearner(t *testing.T) {
	classifier := prompt.NewReasoningClassifier(&fakeEmbedder{}, nil)
	t.Run("nil when learning disabled", func(t *testing.T) {
		l := buildReasoningLearner(Deps{LLM: llm.Config{ReasoningLearning: false}, ReasoningSaver: &fakeReasoningSaver{}}, classifier)
		if l != nil {
			t.Fatal("learning disabled must yield a nil learner")
		}
	})
	t.Run("nil when no saver", func(t *testing.T) {
		l := buildReasoningLearner(Deps{LLM: llm.Config{ReasoningLearning: true}}, classifier)
		if l != nil {
			t.Fatal("absent saver must yield a nil learner")
		}
	})
	t.Run("nil when no classifier", func(t *testing.T) {
		l := buildReasoningLearner(Deps{LLM: llm.Config{ReasoningLearning: true}, ReasoningSaver: &fakeReasoningSaver{}}, nil)
		if l != nil {
			t.Fatal("absent classifier must yield a nil learner")
		}
	})
	t.Run("builds and attaches when fully wired", func(t *testing.T) {
		client := agenttest.NewFakeClient()
		l := buildReasoningLearner(Deps{
			Client:         client,
			LLM:            llm.Config{Model: "test-model", ReasoningLearning: true},
			ReasoningSaver: &fakeReasoningSaver{},
		}, classifier)
		if l == nil {
			t.Fatal("fully wired learner must be non-nil")
		}
		l.Close() // goleak-clean: join the worker
	})
}

// --- deferredToolNames + toolSelectRouterPrompt ---

func TestDeferredToolNames(t *testing.T) {
	if got := deferredToolNames(nil); got != nil {
		t.Fatalf("deferredToolNames(nil) = %v; want nil", got)
	}
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{}) // non-deferred → excluded
	reg.Register(deferredTool{name: "zebra_tool"})
	reg.Register(deferredTool{name: "alpha_tool"})
	got := deferredToolNames(reg)
	if len(got) != 2 || got[0] != "alpha_tool" || got[1] != "zebra_tool" {
		t.Fatalf("deferredToolNames = %v; want sorted [alpha_tool zebra_tool] (text_response excluded)", got)
	}
}

func TestToolSelectRouterPrompt(t *testing.T) {
	p := toolSelectRouterPrompt([]string{"web_search", "shell_exec"})
	for _, want := range []string{"tool-routing classifier", "EXACTLY one tool name", "- web_search", "- shell_exec", "none"} {
		if !strings.Contains(p, want) {
			t.Fatalf("router prompt missing %q\nprompt: %s", want, p)
		}
	}
}

// --- toolSelectTeacher.Label ---

func TestToolSelectTeacher_Label(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(deferredTool{name: "web_search"})
	reg.Register(deferredTool{name: "shell_exec"})

	t.Run("confirms a catalog tool case-insensitively", func(t *testing.T) {
		client := agenttest.NewFakeClient(agenttest.TextChunks("stop", "Web_Search"))
		o := &toolSelectTeacher{client: client, model: "test-model", registry: reg}
		name, ok := o.Label(context.Background(), "latest news")
		if !ok || name != "web_search" {
			t.Fatalf("Label = (%q, %v); want (web_search, true)", name, ok)
		}
	})
	t.Run("rejects a hallucinated name", func(t *testing.T) {
		client := agenttest.NewFakeClient(agenttest.TextChunks("stop", "yahoo_finance"))
		o := &toolSelectTeacher{client: client, model: "test-model", registry: reg}
		if name, ok := o.Label(context.Background(), "btc price"); ok || name != "" {
			t.Fatalf("Label on hallucinated name = (%q, %v); want ('', false)", name, ok)
		}
	})
	t.Run("empty catalog short-circuits", func(t *testing.T) {
		o := &toolSelectTeacher{client: agenttest.NewFakeClient(), model: "m", registry: tools.NewRegistry()}
		if _, ok := o.Label(context.Background(), "x"); ok {
			t.Fatal("empty catalog must report ok=false without calling the client")
		}
	})
	t.Run("stream error yields no label", func(t *testing.T) {
		client := agenttest.NewFakeClient(agenttest.FakeTurn{Err: errFake})
		o := &toolSelectTeacher{client: client, model: "m", registry: reg}
		if _, ok := o.Label(context.Background(), "x"); ok {
			t.Fatal("Stream error must report ok=false")
		}
	})
	t.Run("chunk error yields no label", func(t *testing.T) {
		client := agenttest.NewFakeClient(agenttest.TextThenErr(errFake, "web_"))
		o := &toolSelectTeacher{client: client, model: "m", registry: reg}
		if _, ok := o.Label(context.Background(), "x"); ok {
			t.Fatal("chunk error must report ok=false")
		}
	})
}

// --- lookupToolSearch + toolSearchRanker.Rank ---

func TestLookupToolSearch(t *testing.T) {
	if lookupToolSearch(nil) != nil {
		t.Fatal("lookupToolSearch(nil) must be nil")
	}
	reg := tools.NewRegistry()
	if lookupToolSearch(reg) != nil {
		t.Fatal("registry without tool_search must yield nil")
	}
	ts := &tools.ToolSearch{Registry: reg}
	reg.Register(ts)
	if got := lookupToolSearch(reg); got != ts {
		t.Fatalf("lookupToolSearch returned %p; want the registered tool_search %p", got, ts)
	}
}

func TestToolSearchRanker_Rank(t *testing.T) {
	t.Run("nil tool_search yields not-ok", func(t *testing.T) {
		r := toolSearchRanker{ts: nil}
		if _, _, ok := r.Rank(context.Background(), "q"); ok {
			t.Fatal("nil ts must report ok=false")
		}
	})
	t.Run("ranks the deferred catalog by embedding", func(t *testing.T) {
		reg := tools.NewRegistry()
		ts := &tools.ToolSearch{Registry: reg, Embed: &fakeEmbedder{}}
		reg.Register(ts)
		reg.Register(deferredTool{name: "web_search"})
		reg.Register(deferredTool{name: "shell_exec"})
		r := toolSearchRanker{ts: ts}
		name, _, ok := r.Rank(context.Background(), "find the latest news")
		if !ok || name == "" {
			t.Fatalf("Rank over a 2-tool deferred bank = (%q, %v); want a confident top-1", name, ok)
		}
	})
}

// --- buildToolSelectLearner: nil guards + full wiring driving the refresh closure ---

func TestBuildToolSelectLearner_NilGuards(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.ToolSearch{Registry: reg})
	t.Run("nil when no saver", func(t *testing.T) {
		if buildToolSelectLearner(Deps{Embedder: &fakeEmbedder{}, Registry: reg}) != nil {
			t.Fatal("absent ToolSelectSaver must yield nil")
		}
	})
	t.Run("nil when no embedder", func(t *testing.T) {
		if buildToolSelectLearner(Deps{ToolSelectSaver: &fakeToolSelectSaver{}, Registry: reg}) != nil {
			t.Fatal("absent Embedder must yield nil")
		}
	})
	t.Run("nil when tool_search absent", func(t *testing.T) {
		if buildToolSelectLearner(Deps{ToolSelectSaver: &fakeToolSelectSaver{}, Embedder: &fakeEmbedder{}, Registry: tools.NewRegistry()}) != nil {
			t.Fatal("registry without tool_search must yield nil")
		}
	})
}

// TestBuildToolSelectLearner_RefreshFoldsExamples drives the full assembled learner:
// a flagged mis-route (shell_exec used for a lookup) flows through Observe → the
// fakeToolSelectSaver confirms it → buildToolSelectLearner's Refresh closure re-folds
// the saved examples back into the ranker. It proves the assembled wiring (Teacher,
// Saver, Refresh, embedder reuse) is live, not just constructed.
func TestBuildToolSelectLearner_RefreshFoldsExamples(t *testing.T) {
	reg := tools.NewRegistry()
	ts := &tools.ToolSearch{Registry: reg}
	reg.Register(ts)
	reg.Register(deferredTool{name: "web_search"})
	reg.Register(deferredTool{name: "shell_exec"})

	embed := &fakeEmbedder{}
	saver := &fakeToolSelectSaver{}
	// A teacher that confirms web_search so the escalation tier commits a label.
	teacherClient := agenttest.NewFakeClient(agenttest.TextChunks("stop", "web_search"))

	l := buildToolSelectLearner(Deps{
		Client:          teacherClient,
		LLM:             llm.Config{Model: "test-model"},
		Registry:        reg,
		Embedder:        embed,
		ToolSelectSaver: saver,
	})
	if l == nil {
		t.Fatal("fully wired tool-select learner must be non-nil")
	}
	defer l.Close()

	// A lookup request answered by shell_exec is the canonical mis-route: it flags,
	// escalates to the teacher (confirms web_search), saves, and refreshes.
	l.Observe("what is the latest news about ai", "shell_exec")

	deadline := time.After(2 * time.Second)
	for saver.saveCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the async learner to confirm + save a mis-route exemplar")
		case <-time.After(5 * time.Millisecond):
		}
	}
	// Close joins the worker; the Refresh closure must have re-folded the saved example
	// at least once (LoadExamples was called).
	l.Close()
	saver.mu.Lock()
	loaded := saver.loaded
	saver.mu.Unlock()
	if loaded == 0 {
		t.Fatal("Refresh closure never re-folded examples (LoadExamples not called after a save)")
	}
	if embed.calls() == 0 {
		t.Fatal("the embedder was never used by the assembled learner")
	}
}

// TestBuildToolSelectLearner_RefreshLoaderError exercises the Refresh closure's
// best-effort LoadExamples-error branch (a loader failure leaves the previous bank).
func TestBuildToolSelectLearner_RefreshLoaderError(t *testing.T) {
	reg := tools.NewRegistry()
	ts := &tools.ToolSearch{Registry: reg}
	reg.Register(ts)
	reg.Register(deferredTool{name: "web_search"})
	reg.Register(deferredTool{name: "shell_exec"})

	saver := &fakeToolSelectSaver{loadErr: errors.New("neo4j down")}
	teacherClient := agenttest.NewFakeClient(agenttest.TextChunks("stop", "web_search"))
	l := buildToolSelectLearner(Deps{
		Client:          teacherClient,
		LLM:             llm.Config{Model: "test-model"},
		Registry:        reg,
		Embedder:        &fakeEmbedder{},
		ToolSelectSaver: saver,
	})
	if l == nil {
		t.Fatal("learner must be non-nil")
	}
	l.Observe("what is the latest news about ai", "shell_exec")
	deadline := time.After(2 * time.Second)
	for saver.saveCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for a save")
		case <-time.After(5 * time.Millisecond):
		}
	}
	l.Close() // a loader error must not panic; the bank simply stays as-is
}
