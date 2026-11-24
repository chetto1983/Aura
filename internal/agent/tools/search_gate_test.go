package tools

import (
	"context"
	"encoding/json"
	"maps"
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
		Deferred    bool            `json:"deferred"`
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
		if !d.Deferred {
			t.Fatalf("manifest fixture contains active tool %q", d.Name)
		}
		searchText := d.Summary + d.Description + string(d.Parameters)
		for _, retired := range []string{"user_identifier", "untrusted MCP server"} {
			if strings.Contains(searchText, retired) {
				t.Fatalf("manifest fixture tool %q contains retired contract %q", d.Name, retired)
			}
		}
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

// The fixture is a checked-in production-manifest snapshot. Pin its source
// inventory so a curation change cannot silently leave retrieval coverage on an
// older mounted surface: fixture, expectations, and ranking cases move together.
func TestDeferredManifestFixture_SourceInventory(t *testing.T) {
	counts := make(map[string]int)
	for _, spec := range loadManifestFixture(t) {
		source := "builtins"
		if namespace, _, namespaced := strings.Cut(spec.Name, "__"); namespaced {
			source = namespace
		}
		counts[source]++
	}

	want := map[string]int{
		"builtins": 13,
		"calendar": 29,
		"linear":   55,
		"memory":   4,
		"whatsapp": 15,
	}
	if !maps.Equal(counts, want) {
		t.Fatalf("manifest source inventory = %v, want %v", counts, want)
	}
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
	{"what time is it", []string{"current_time"}},
	{"schedule a recurring task", []string{"task"}},
	// Amendment #194: the six phrasings that found nothing on 2026-08-30.
	{"scheduling", []string{"task"}},
	{"scheduler", []string{"task"}},
	{"wake me up later", []string{"task"}},
	{"periodic check", []string{"task"}},
	{"timer", []string{"task"}},
	{"run this again tomorrow morning", []string{"task"}},
	{"keep a todo list of the current work", []string{"todo_write"}},
	{"run subtasks in parallel with worker agents", []string{"swarm_spawn"}},
	{"read a knowledge-work plugin pack and its connectors", []string{"plugin_pack"}},
	{"install or author a new skill", []string{"skill_manage"}},
	{"recall a deeper or historical fact from memory", []string{"memory__memory_recall"}},
	{"remember a fact about the user", []string{"memory__memory_upsert_fact"}},
	{"create a calendar event", []string{"calendar__create_event"}},
	{"create or update a Linear issue", []string{"linear__save_issue"}},
	{"list issues in the user's Linear workspace including active Triage Intelligence suggestions for issues in triage", []string{"linear__list_issues"}},
	{"create or update a comment on a Linear issue", []string{"linear__save_comment"}},
	{"projects in the user's Linear workspace", []string{"linear__list_projects"}},
	{"send a whatsapp message", []string{"whatsapp__send_message"}},
	{"get WhatsApp messages with optional surrounding context", []string{"whatsapp__list_messages"}},
}

func isGold(name string, gold []string) bool { return slices.Contains(gold, name) }

// evalSet is a written query set: testdata/blind_eval_2026-10-06.json, whose header in
// search_keywords_measure_test.go says how it was written and what spends it, and
// testdata/pim_heldout_2026-11-24.json, written before the PIM's retrieval keywords.
type evalSet struct {
	Cases []struct {
		Query string   `json:"query"`
		Lang  string   `json:"lang"`
		Gold  []string `json:"gold"`
	} `json:"cases"`
	OutOfScope []struct {
		Query string `json:"query"`
	} `json:"out_of_scope"`
}

func loadEvalSet(t *testing.T, file string) evalSet {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatalf("read eval set: %v", err)
	}
	var set evalSet
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatalf("decode eval set %s: %v", file, err)
	}
	return set
}

func loadBlindEval(t *testing.T) evalSet { return loadEvalSet(t, "blind_eval_2026-10-06.json") }

func (s evalSet) gateCases() []gateCase {
	out := make([]gateCase, 0, len(s.Cases))
	for _, c := range s.Cases {
		out = append(out, gateCase{c.Query, c.Gold})
	}
	return out
}

// assertPIMQueriesInTopFive checks every case whose gold is a PIM tool, and fails when
// there is none: an empty set would pass without measuring anything.
func assertPIMQueriesInTopFive(t *testing.T, cases []gateCase) {
	t.Helper()
	cases = slices.DeleteFunc(slices.Clone(cases), func(c gateCase) bool { return !strings.HasPrefix(c.gold[0], "calendar__") })
	if len(cases) == 0 {
		t.Fatal("the eval holds no PIM query")
	}
	ts, ctx := newGateSearch(t)
	for _, c := range cases {
		got := gateSearch(t, ts, ctx, c.query)
		if !slices.ContainsFunc(got[:min(5, len(got))], func(n string) bool { return isGold(n, c.gold) }) {
			t.Errorf("%q -> %v, want one of %v in the top 5", c.query, got, c.gold)
		}
	}
}

// With one multiplexed PIM tool, any calendar hit was the right one. Split into 29, the
// model has to find the one it needs, so "does not regress" would compare unlike
// things: every PIM query of the eval must bring its own tool into tool_search's top 5.
func TestEveryPIMQueryFindsItsToolInTopFive(t *testing.T) {
	assertPIMQueriesInTopFive(t, slices.Concat(gateCases, loadBlindEval(t).gateCases()))
}

// heldOutPIMFloor is how many held-out cases, per language, bring a gold tool into the top 5.
// It is a ratchet: raise it when retrieval improves, never lower it. Before the split the
// same set scored 9/52, with calendar__calendar as the gold. Never tune keywords against
// this set: it was committed before the PIM's keywords existed, and read only after they
// were frozen, so it is the one evidence that they generalise.
var heldOutPIMFloor = map[string]int{"it": 21, "en": 11}

func TestHeldOutPIMQueriesKeepTheirFloor(t *testing.T) {
	set := loadEvalSet(t, "pim_heldout_2026-11-24.json")
	ts, ctx := newGateSearch(t)
	hits := map[string]int{}
	for _, c := range set.Cases {
		got := gateSearch(t, ts, ctx, c.Query)
		if slices.ContainsFunc(got[:min(5, len(got))], func(n string) bool { return isGold(n, c.Gold) }) {
			hits[c.Lang]++
		} else {
			t.Logf("miss (%s) %q -> %v, want one of %v", c.Lang, c.Query, got, c.Gold)
		}
	}
	total, floor := 0, 0
	for lang, want := range heldOutPIMFloor {
		total, floor = total+hits[lang], floor+want
		if hits[lang] < want {
			t.Errorf("%s: %d held-out cases in the top 5, floor %d", lang, hits[lang], want)
		}
	}
	t.Logf("held-out PIM cases in the top 5: %d/%d (floor %d)", total, len(set.Cases), floor)
	if total < floor {
		t.Errorf("held-out total %d, floor %d", total, floor)
	}
}

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

// The system prompt's family labels read as names: on 2026-10-05, 4 of the 11 select:
// calls on the lab VM carried "scheduling", and three of them cost a second search
// before `task` loaded. A select: entry that shares no token with any registered name
// is a capability word, so its best match loads in its place.
func TestToolSearchGate_SelectCapabilityWordLoadsBestMatch(t *testing.T) {
	ts, ctx := newGateSearch(t)
	res, err := ts.Execute(ctx, []byte(`{"query":"select:whatsapp__send_message,scheduling"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got []string
	if res.Meta != nil {
		got, _ = (*res.Meta)[MetaActivatedTools].([]string)
	}
	if want := []string{"whatsapp__send_message", "task"}; !slices.Equal(got, want) {
		t.Errorf("loaded %v, want %v", got, want)
	}
	if !strings.Contains(res.Preview, `"scheduling" is not a registered tool`) {
		t.Errorf("reply does not say the label is not a tool: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "## task") {
		t.Errorf("reply does not render the stand-in's schema: %q", res.Preview)
	}
}

// A stand-in already named in the same select: loads once.
func TestToolSearchGate_SelectCapabilityWordDoesNotDuplicate(t *testing.T) {
	ts, ctx := newGateSearch(t)
	if got := gateSearch(t, ts, ctx, "select:task,scheduling,reminder"); !slices.Equal(got, []string{"task"}) {
		t.Errorf("loaded %v, want [task] once", got)
	}
}

// A misspelled name keeps the naming answer: it shares tokens with the tool it meant,
// and loading a ranked guess in its place would hide the spelling error.
func TestToolSearchGate_MisspelledSelectLoadsNothing(t *testing.T) {
	ts, ctx := newGateSearch(t)
	if got := gateSearch(t, ts, ctx, "select:memory__memory_upsert_fcat"); len(got) != 0 {
		t.Errorf("misspelled name loaded %v, want nothing", got)
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
