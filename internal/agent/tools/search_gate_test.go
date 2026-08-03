package tools

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Retrieval gate. It replays model-written capability queries against the REAL
// deferred corpus (testdata/deferred_manifest.json, dumped from the deployed
// registry with `aura toolpipe --manifest-json`) and scores the ranking that the
// production path actually returns.
//
// It replaces the old concept-axis corpus gate, which scored a fixed-map embedder
// over ten fixture tools Aura does not have, where each tool and its query carried
// the same hand-written concept label — a tautology that stayed green while live
// discovery ranked `document_search` first for "web search for <part> specs".
//
// The gate runs free in CI: the ranker is lexical, so there is no sidecar to reach.

const (
	gateTop1Floor    = 0.90
	gateRecall3Floor = 1.00
)

// manifestTool replays one dumped production spec. The gate needs the spec text
// only — Execute is never reached through a ranking.
type manifestTool struct{ spec Spec }

func (m manifestTool) Spec() Spec { return m.spec }
func (manifestTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

func loadManifestFixture(t *testing.T) []Spec {
	t.Helper()
	raw, err := os.ReadFile("testdata/deferred_manifest.json")
	if err != nil {
		t.Fatalf("read manifest fixture: %v", err)
	}
	var dumped []struct {
		Name        string          `json:"name"`
		Summary     string          `json:"summary"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &dumped); err != nil {
		t.Fatalf("decode manifest fixture: %v", err)
	}
	if len(dumped) < 40 {
		t.Fatalf("manifest fixture has %d specs, want the full deferred corpus", len(dumped))
	}
	out := make([]Spec, 0, len(dumped))
	for _, d := range dumped {
		out = append(out, Spec{
			Name:        d.Name,
			Summary:     d.Summary,
			Description: d.Description,
			Parameters:  d.Parameters,
			Deferred:    true,
		})
	}
	return out
}

func newGateSearch(t *testing.T) (*ToolSearch, context.Context) {
	t.Helper()
	reg := NewRegistry()
	for _, s := range loadManifestFixture(t) {
		reg.Register(manifestTool{spec: s})
	}
	ts := &ToolSearch{Registry: reg}
	reg.Register(ts)
	return ts, ctxWith(t, "sess-gate", "call-gate")
}

// gateSearch runs one query and returns the loaded tool names, in rank order.
// It reads the promotion Meta, not the preview: a multi-tool result exceeds the
// preview cap and spills to the sidecar, so the rendered text is a truncation
// artefact while the Meta is the authoritative list the runner promotes.
func gateSearch(t *testing.T, ts *ToolSearch, ctx context.Context, query string) []string {
	t.Helper()
	args, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		t.Fatalf("marshal query: %v", err)
	}
	res, err := ts.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute(%q): %v", query, err)
	}
	if res.Meta == nil {
		return nil
	}
	names, _ := (*res.Meta)[MetaActivatedTools].([]string)
	return names
}

// gateCase is a query plus the tool names that answer it. More than one gold is
// listed only where the corpus genuinely offers interchangeable answers.
type gateCase struct {
	query string
	gold  []string
}

// gateCases are English capability phrases in the shape the MODEL writes them —
// the query distribution measured in production, not the end user's Italian.
var gateCases = []gateCase{
	{"search the current web for prices or news", []string{"web_search"}},
	{"download the contents of a web page", []string{"web_fetch"}},
	{"web search for B&R 8LSA35.DB030S300-3 specifications", []string{"web_search"}},
	{"send a file to the user", []string{"send_file"}},
	{"read a file from disk", []string{"fs_read"}},
	{"edit a file on disk", []string{"fs_edit"}},
	{"write a new file", []string{"fs_write"}},
	{"list files matching a glob pattern", []string{"fs_glob"}},
	{"find text inside files in the repository", []string{"fs_grep"}},
	{"run a shell command on the host", []string{"shell_exec"}},
	{"kill a background process", []string{"shell_kill"}},
	{"poll a still-running background command", []string{"shell_poll"}},
	{"what time is it", []string{"current_time"}},
	{"schedule a recurring task", []string{"task"}},
	{"keep a todo list of the current work", []string{"todo_write"}},
	{"run subtasks in parallel with worker agents", []string{"swarm_spawn"}},
	{"install a new skill", []string{"skill"}},
	{"search the user uploaded documents", []string{"document_search"}},
	{"index a document for retrieval", []string{"document_index"}},
	{"recall a deeper or historical fact from memory", []string{"memory__memory_recall"}},
	{"remember a fact about the user", []string{"memory__memory_upsert_fact"}},
	{"create a calendar event", []string{"calendar__create_event"}},
	{"send an email", []string{"calendar__send_email"}},
	{"send a whatsapp message", []string{"whatsapp__send_message"}},
	{
		"search my whatsapp chat history",
		[]string{"whatsapp__list_messages", "whatsapp__get_chat"},
	},
}

func isGold(name string, gold []string) bool { return slices.Contains(gold, name) }

func TestToolSearchGate_RealCorpus(t *testing.T) {
	ts, ctx := newGateSearch(t)
	var top1, recall3 int
	for _, c := range gateCases {
		got := gateSearch(t, ts, ctx, c.query)
		if len(got) > 0 && isGold(got[0], c.gold) {
			top1++
		} else {
			t.Logf("top-1 MISS %-46q -> %v (want %v)", c.query, got, c.gold)
		}
		hit3 := false
		for _, n := range got[:min(3, len(got))] {
			if isGold(n, c.gold) {
				hit3 = true
			}
		}
		if hit3 {
			recall3++
		} else {
			t.Logf("recall@3 MISS %-43q -> %v (want %v)", c.query, got, c.gold)
		}
	}
	n := float64(len(gateCases))
	gotTop1, gotRecall3 := float64(top1)/n, float64(recall3)/n
	t.Logf("top-1 %.0f%% (%d/%d), recall@3 %.0f%% (%d/%d)",
		gotTop1*100, top1, len(gateCases), gotRecall3*100, recall3, len(gateCases))
	if gotTop1 < gateTop1Floor {
		t.Errorf("top-1 = %.2f, floor %.2f", gotTop1, gateTop1Floor)
	}
	if gotRecall3 < gateRecall3Floor {
		t.Errorf("recall@3 = %.2f, floor %.2f", gotRecall3, gateRecall3Floor)
	}
}

// A query whose terms appear nowhere in the corpus is a capability gap: the model
// must be routed to the skills system rather than handed a tool it did not ask for.
//
// The bar is vocabulary, not intent. "play a video game" is NOT in this set: the
// corpus really does contain "video" (whatsapp__send_file sends one), so returning
// that tool is the retriever working. A lexical ranker answers what the words say,
// and the honest gate asserts the case it can actually decide — nothing shared at
// all. Function words cannot create a match here because they are not indexed.
func TestToolSearchGate_CapabilityGapReturnsOrientation(t *testing.T) {
	ts, ctx := newGateSearch(t)
	for _, q := range []string{
		"book a table at a restaurant",
		"tune the harpsichord before the recital",
	} {
		got := gateSearch(t, ts, ctx, q)
		if len(got) != 0 {
			t.Errorf("%q loaded %v, want the capability-gap orientation", q, got)
		}
	}
}

// Ranking is a pure function of the corpus: the same query twice returns the same
// order, so a repeated discovery never reshuffles the model's context.
func TestToolSearchGate_RankingIsDeterministic(t *testing.T) {
	ts, ctx := newGateSearch(t)
	const q = "search the current web for prices or news"
	first := gateSearch(t, ts, ctx, q)
	for i := range 3 {
		if got := gateSearch(t, ts, ctx, q); !slices.Equal(got, first) {
			t.Fatalf("run %d returned %v, first run returned %v", i+2, got, first)
		}
	}
}

// An unregistered name in a select: is a naming error, not a capability gap. It
// must say so and name the closest registered tools — the skills-install
// orientation is actively wrong advice for a tool the model nearly spelled right.
func TestToolSearchGate_UnknownSelectNamesTheError(t *testing.T) {
	ts, ctx := newGateSearch(t)
	res, err := ts.Execute(ctx, []byte(`{"query":"select:memory__memory_upsert_fcat"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "memory__memory_upsert_fcat") {
		t.Errorf("reply does not name the unregistered tool: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "memory__memory_upsert_fact") {
		t.Errorf("reply does not suggest the closest registered name: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "find-skills") {
		t.Errorf("unregistered name got the capability-gap orientation: %q", res.Preview)
	}
}

// A select: that mixes a good and a bad name loads the good one AND reports the
// bad one. Silently dropping it leaves the model believing it loaded both.
func TestToolSearchGate_PartialSelectLoadsAndReports(t *testing.T) {
	ts, ctx := newGateSearch(t)
	res, err := ts.Execute(ctx, []byte(`{"query":"select:web_search,does_not_exist"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "## web_search") {
		t.Errorf("registered tool was not loaded: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "does_not_exist") {
		t.Errorf("unregistered name was dropped silently: %q", res.Preview)
	}
}

// The model routinely writes the names it already read off the tool_search
// description as a bare phrase. Those are names, not a ranking query: resolve them
// all, uncapped, instead of returning the top-5 of a similarity ranking.
func TestToolSearchGate_FreeTextOfRegisteredNamesResolvesAsSelect(t *testing.T) {
	ts, ctx := newGateSearch(t)
	const q = "memory__memory_upsert_fact memory__memory_merge_entities " +
		"memory__memory_recall memory__memory_forget"
	got := gateSearch(t, ts, ctx, q)
	want := strings.Fields(q)
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("free-text name list resolved to %v, want exactly %v", got, want)
	}
}
