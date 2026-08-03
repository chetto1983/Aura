package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// The defect these pin, read off a live turn (thread 019fb9ef): the model called
// shell_exec — the most-used tool in the system — without having loaded it, and
// the refusal sent it away to fetch a schema the process was already holding:
//
//	· shell_exec → "I need to load shell_exec first" → · tool_search → · shell_exec
//
// Three model turns to run one command, every conversation. The refusal itself is
// correct and must stay: arguments written without ever seeing the parameters are
// invented. What was wrong was answering with an errand instead of the answer.
func TestDeferredBounceReturnsTheSchemaInsteadOfAnErrand(t *testing.T) {
	t.Parallel()
	a := &LlmAgent{registry: decayRegistry()}

	run := a.runTool(context.Background(), nil,
		toolCall("c1", "alpha", `{"invented":"argument"}`), time.Now())

	if !strings.Contains(run.Preview, "tool_not_loaded") {
		t.Fatalf("the guard stopped firing: %q", run.Preview)
	}
	// The schema itself, in the same shape tool_search renders — one format for one
	// thing, so the model does not have to learn two.
	spec := tools.Spec{}
	if tool, ok := a.registry.Get("alpha"); ok {
		spec = tool.Spec()
	}
	if !strings.Contains(run.Preview, tools.RenderSpec(spec)) {
		t.Errorf("the refusal carries no schema, so the model must still go and fetch it:\n%s", run.Preview)
	}
	// And it must NOT send the model to tool_search: that instruction was the whole
	// wasted round trip.
	if strings.Contains(run.Preview, "select:alpha") {
		t.Errorf("the refusal still sends the model on the tool_search errand:\n%s", run.Preview)
	}
}

// The retry has to work, or the bounce is an infinite loop rather than a saving.
func TestDeferredBounceLoadsTheToolForTheRetry(t *testing.T) {
	t.Parallel()
	a := &LlmAgent{registry: decayRegistry()}
	call := toolCall("c1", "alpha", `{}`)

	run := a.runTool(context.Background(), testBudget(t), call, time.Now())

	// The promotion rides the SERIAL result loop, exactly as a tool_search hit does —
	// dispatch runs concurrently and must not write the sets itself.
	if run.Result.Meta == nil {
		t.Fatal("no result Meta: nothing will promote the tool and the retry bounces again")
	}
	names, ok := (*run.Result.Meta)[tools.MetaActivatedTools].([]string)
	if !ok || len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("MetaActivatedTools = %v, want [alpha]", (*run.Result.Meta)[tools.MetaActivatedTools])
	}

	a.promoteFromMeta(run.Result.Meta)
	if a.isDeferredUnloaded("alpha") {
		t.Fatal("alpha still bounces after its schema was handed over — an infinite loop")
	}
	if retry := a.runTool(context.Background(), testBudget(t), call, time.Now()); strings.Contains(
		retry.Preview, "tool_not_loaded") {
		t.Fatalf("the retry was refused a second time: %q", retry.Preview)
	}
}

// A non-deferred tool never went through this path and must not start now.
func TestDispatchGateLeavesPlainToolsAlone(t *testing.T) {
	t.Parallel()
	a := &LlmAgent{registry: decayRegistry()}
	run := a.runTool(context.Background(), testBudget(t), toolCall("c1", "plain", `{}`), time.Now())
	if strings.Contains(run.Preview, "tool_not_loaded") {
		t.Fatalf("a non-deferred tool was bounced: %q", run.Preview)
	}
}

// The refusal must carry its own recovery instruction. It used to be duplicated in
// the system prompt as well; that copy is gone, and its absence is not a weakening
// — the prompt sat thousands of tokens from the failure, while this text arrives AT
// it, with the schema attached. Telling the model to tool_search here would spend a
// whole round trip fetching what the process already handed over.
func TestRefusalTellsTheModelToRetryNotToSearch(t *testing.T) {
	t.Parallel()
	agent := &LlmAgent{registry: decayRegistry()}
	run := agent.runTool(context.Background(), testBudget(t), toolCall("c1", "alpha", `{}`), time.Now())

	if !strings.Contains(run.Preview, "tool_not_loaded") {
		t.Fatalf("the deferred tool was not bounced: %q", run.Preview)
	}
	for _, phrase := range []string{
		"Its schema follows",
		"Do not call tool_search",
		"call alpha again using exactly these parameters",
	} {
		if !strings.Contains(run.Preview, phrase) {
			t.Errorf("the refusal no longer tells the model to retry directly: missing %q\ngot: %s", phrase, run.Preview)
		}
	}
}

// testBudget is the live budget the EXECUTING path needs: runTool reads
// NodeTimeout off it, so only the refusal path tolerates a nil one.
func testBudget(t *testing.T) *Budget {
	t.Helper()
	budget, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("budget: %v", err)
	}
	return budget
}
