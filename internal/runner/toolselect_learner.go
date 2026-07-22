package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/activelearn"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/learningretention"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/semindex"
	"github.com/chetto1983/aura/internal/toolselectlearn"
)

// toolSelectTeacher is the DeepSeek escalation oracle for the tool-selection loop: it
// asks the EXISTING router (no new sidecar, Req-8) "which single tool should have
// handled this request?" given the current deferred-tool catalog, and returns the
// confirmed tool name. It MIRRORS runner.reasoningOracle's shape exactly — MaxTokens:32,
// Temperature:0, ToolChoice:"none", Reasoning.Enabled=false — so it is a cheap,
// deterministic, non-tool-calling label, never a hot-path call (it rides the activelearn
// worker, gated by AURA_TOOLSELECT_ORACLE in the twoTierOracle).
type toolSelectTeacher struct {
	client   llm.Client
	model    string
	registry *tools.Registry
}

// Label runs the tool-selection router prompt and returns the confirmed tool name. It
// validates the reply against the live deferred-tool catalog so a hallucinated name is
// rejected (ok=false) rather than poisoning the bank.
func (o *toolSelectTeacher) Label(ctx context.Context, request string) (string, bool) {
	catalog := deferredToolNames(o.registry)
	if len(catalog) == 0 {
		return "", false
	}
	enabled := false
	req := llm.Request{
		Model: o.model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: toolSelectRouterPrompt(catalog)},
			{Role: llm.RoleUser, Content: request},
		},
		Temperature: 0,
		MaxTokens:   32,
		Reasoning:   llm.ReasoningConfig{Enabled: &enabled},
		ToolChoice:  "none",
	}
	ch, err := o.client.Stream(ctx, req)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			return "", false
		}
		b.WriteString(c.Text)
	}
	name := strings.TrimSpace(b.String())
	// Validate against the catalog (reject hallucinations — training-data integrity).
	for _, t := range catalog {
		if strings.EqualFold(name, t) {
			return t, true
		}
	}
	return "", false
}

// toolSelectRouterPrompt builds the teacher system prompt: a fixed instruction plus
// the sorted deferred-tool catalog. The model must reply with EXACTLY one tool name
// from the list (or "none" — which we reject as no-confirmation).
func toolSelectRouterPrompt(catalog []string) string {
	var b strings.Builder
	b.WriteString("You are a tool-routing classifier. Given a user request, reply with EXACTLY one tool name ")
	b.WriteString("from the list below that should handle it — no other words, no punctuation. If none fit, reply \"none\".\n\nTools:\n")
	for _, t := range catalog {
		fmt.Fprintf(&b, "- %s\n", t)
	}
	return b.String()
}

// deferredToolNames returns the sorted names of the registry's deferred tools (the
// catalog the teacher chooses from, matching what tool_search ranks).
func deferredToolNames(reg *tools.Registry) []string {
	if reg == nil {
		return nil
	}
	var names []string
	for _, t := range reg.All() {
		s := t.Spec()
		if s.Deferred {
			names = append(names, s.Name)
		}
	}
	sort.Strings(names)
	return names
}

// buildToolSelectLearner wires the tool-selection active-learning loop when a save-
// capable store is present; nil otherwise. It locates the registered tool_search hook
// (the stage-1 ranker), attaches a learnedBoost over the SAME granite embedder, and
// wires the detector (tool_search ranker) + the DeepSeek teacher + the toolselectstore
// Saver onto the activelearn core, with Refresh re-folding the per-tool centroids from
// Neo4j after each save. The embedder is reused — NOT re-wired (the ranker embedder is
// already set by wireToolSearchEmbedder).
func buildToolSelectLearner(d Deps) *toolselectlearn.Learner {
	if d.ToolSelectSaver == nil || d.Embedder == nil {
		return nil
	}
	ts := lookupToolSearch(d.Registry)
	if ts == nil {
		return nil
	}
	embed := semindex.Embedder(d.Embedder)
	ts.EnableLearnedBoost(embed)

	saver := d.ToolSelectSaver
	// Refresh re-folds the FULL :ToolSelectionExample set into the per-tool centroids
	// after every save, so the query-keyed UPSERT (latest label per query) is reflected
	// exactly. A loader failure leaves the previous bank in place (best-effort).
	refresh := func() {
		loader, ok := saver.(toolselectlearn.ExampleLoader)
		if !ok {
			return
		}
		examples, err := loader.LoadExamples(context.Background())
		if err != nil {
			return
		}
		ts.FoldLearned(examples)
	}

	return toolselectlearn.New(toolselectlearn.Config{
		Embedder:       embed,
		Ranker:         toolSearchRanker{ts: ts},
		Teacher:        &toolSelectTeacher{client: d.Client, model: d.LLM.Model, registry: d.Registry},
		Saver:          saver,
		Refresh:        refresh,
		SeenMaxEntries: d.Learning.SeenMaxEntries,
		SeenTTL:        d.Learning.SeenTTL,
		Metrics: func(metric activelearn.LearningMetric) {
			learningretention.RecordMetric(learningretention.Metric{
				Operation: metric.Operation, ToolClass: "other", Outcome: metric.Outcome,
				State: metric.State, ErrorClass: metric.ErrorClass, Size: metric.Size, OldestAge: metric.OldestAge,
			})
		},
	})
}

// toolSearchRanker adapts the tool_search hook into the toolselectlearn.Ranker seam
// (RankForLearner ranks against the STAGE-1 description bank, never the learned
// centroids — guard #4 oracle-in-loop: the ranker never self-confirms a label).
type toolSearchRanker struct {
	ts *tools.ToolSearch
}

func (r toolSearchRanker) Rank(ctx context.Context, query string) (string, float64, bool) {
	if r.ts == nil {
		return "", 0, false
	}
	return r.ts.RankForLearner(ctx, query)
}

// lookupToolSearch returns the registered tool_search hook, or nil.
func lookupToolSearch(reg *tools.Registry) *tools.ToolSearch {
	if reg == nil {
		return nil
	}
	t, ok := reg.Get("tool_search")
	if !ok {
		return nil
	}
	ts, ok := t.(*tools.ToolSearch)
	if !ok {
		return nil
	}
	return ts
}
