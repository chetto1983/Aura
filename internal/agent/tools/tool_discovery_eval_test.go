//go:build tool_discovery_eval

// Ten-scenario probe for the adaptive tool-discovery arms (bm25 / semantic / static).
//
// It answers ONE question the frozen benchmark cannot: do the three ranking strategies
// actually rank differently, and does either beat the static default? A benchmark whose
// arms score identically teaches a bandit nothing, and the frozen dataset is exactly that
// — of its 12 tool_discovery scenarios, 10 name a tool that is not in the candidate set at
// all (`weather_now`, `send_email`, `finance_quote` do not exist; `memory_add_preference`
// and `calendar_create_event` drop the `__` namespace; `document_search` exists but is
// ACTIVE, and freezeDeferredTools only ever offers DEFERRED tools). The evaluator compares
// with exact string equality, so all ten score zero under every arm. The two survivors are
// both trivially lexical, which BM25 wins by construction.
//
// The scenarios below are stratified by what they are meant to SEPARATE, not by difficulty:
// a case all three arms win teaches as little as one they all lose. Every expected label is
// asserted to be in the live frozen catalog before a single query runs — the miniature of
// the guard the real dataset needs (`catalog_revision` recorded and enforced).
//
//	AURA_TOOL_MANIFEST=/path/aura-tools.json \
//	AURA_EMBED_BASE_URL=http://127.0.0.1:8081 \
//	go test -tags tool_discovery_eval -run TestToolDiscoveryArms -v ./internal/agent/tools/
//
// The manifest comes from the live registry — `aura tools --json` inside the container —
// so the candidate set and the ranking documents are byte-identical to production.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"
)

// discriminationClass records what a scenario is built to separate. A dataset without this
// cannot be audited for coverage: you cannot tell whether it probes the arms' differences
// or just repeats the cases they all agree on.
type discriminationClass string

const (
	// classLexicalTrap: the prompt shares surface tokens with a WRONG tool. BM25 is
	// expected to fall for it; a semantic ranker should not.
	classLexicalTrap discriminationClass = "lexical-trap"
	// classNoOverlap: correct intent, near-zero token overlap with the right tool's
	// document. Only a semantic ranker can bridge it.
	classNoOverlap discriminationClass = "no-overlap"
	// classCrossLingual: Italian prompt against English tool descriptions — Aura's real
	// usage, and the case BM25 is blind to by construction.
	classCrossLingual discriminationClass = "cross-lingual"
	// classNearNeighbour: two tools that plausibly both fit; picking the wrong one is a
	// realistic error, not a silly one.
	classNearNeighbour discriminationClass = "near-neighbour"
)

type discoveryScenario struct {
	id       string
	class    discriminationClass
	prompt   string
	expected string
	// trap is the wrong tool this scenario is built to attract. Empty when the scenario
	// has no specific decoy.
	trap string
}

// The ten. Every `expected` is a DEFERRED tool on the live appliance; the harness verifies
// that against the manifest before running, so a rename upstream fails loudly here instead
// of silently scoring zero forever.
var discoveryScenarios = []discoveryScenario{
	{
		id: "d01", class: classLexicalTrap,
		prompt:   "Manda a Marco il preventivo in PDF su WhatsApp",
		expected: "whatsapp__send_file",
		trap:     "send_file",
	},
	{
		id: "d02", class: classLexicalTrap,
		prompt:   "Dammi il file che hai appena generato",
		expected: "send_file",
		trap:     "whatsapp__send_file",
	},
	{
		id: "d03", class: classNearNeighbour,
		prompt:   "Qual e l'ultimo messaggio scambiato con Sara?",
		expected: "whatsapp__get_last_interaction",
		trap:     "whatsapp__list_messages",
	},
	{
		id: "d04", class: classNoOverlap,
		prompt:   "Avvisa Sara che arrivo tardi",
		expected: "whatsapp__send_message",
	},
	{
		id: "d05", class: classNoOverlap,
		prompt:   "Sono libero giovedi pomeriggio?",
		expected: "calendar__get_calendar_events",
	},
	{
		id: "d06", class: classNoOverlap,
		prompt:   "Scrivi a fornitore@acme.it che accettiamo l'offerta",
		expected: "calendar__send_email",
	},
	{
		id: "d07", class: classCrossLingual,
		prompt:   "Guarda in rete cosa si dice di Neo4j 2026",
		expected: "web_search",
	},
	{
		id: "d08", class: classCrossLingual,
		prompt:   "Prendi questa pagina e dammela in testo leggibile",
		expected: "web_fetch",
	},
	{
		id: "d09", class: classNearNeighbour,
		prompt:   "Trova i file .go che contengono la parola budget",
		expected: "fs_grep",
		trap:     "fs_glob",
	},
	{
		id: "d10", class: classNearNeighbour,
		prompt:   "Elenca i file che si chiamano *_test.go",
		expected: "fs_glob",
		trap:     "fs_grep",
	},
}

// manifestTool replays one live spec. Ranking only ever reads Spec(), so a stub carrying the
// real Name/Description/Parameters produces a byte-identical searchDocument.
type manifestTool struct{ spec Spec }

func (m manifestTool) Spec() Spec { return m.spec }

func (m manifestTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, fmt.Errorf("eval stub %q is not executable", m.spec.Name)
}

// httpEmbedder is the OpenAI-compatible /v1/embeddings client, inlined so the probe pulls in
// no production package beyond the one under test.
type httpEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

func (e httpEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, e.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: http %d", resp.StatusCode)
	}
	var decoded struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	out := make([][]float64, 0, len(decoded.Data))
	for _, d := range decoded.Data {
		out = append(out, d.Embedding)
	}
	return out, nil
}

func loadLiveRegistry(t *testing.T) *Registry {
	t.Helper()
	path := os.Getenv("AURA_TOOL_MANIFEST")
	if path == "" {
		t.Skip("set AURA_TOOL_MANIFEST to an `aura tools --json` dump from the live container")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var entries []ManifestEntryJSON
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	reg := NewRegistry()
	for _, e := range entries {
		reg.Register(manifestTool{spec: Spec{
			Name:        e.Name,
			Summary:     e.Summary,
			Description: e.Description,
			Parameters:  e.Parameters,
			Deferred:    e.Deferred,
		}})
	}
	return reg
}

// TestToolDiscoveryArms ranks the ten scenarios under each registered strategy and reports
// top-1 accuracy, how often the arms actually disagree, and which trap caught which arm.
func TestToolDiscoveryArms(t *testing.T) {
	reg := loadLiveRegistry(t)
	base := os.Getenv("AURA_EMBED_BASE_URL")
	if base == "" {
		t.Skip("set AURA_EMBED_BASE_URL to the live embed sidecar")
	}
	// The package goleak gate counts this client's keep-alive goroutines, so the idle
	// pool has to be drained before the test returns.
	client := &http.Client{Timeout: 60 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	ts := &ToolSearch{
		Registry: reg,
		Embed: httpEmbedder{
			baseURL: base,
			model:   os.Getenv("AURA_EMBED_MODEL"),
			client:  client,
		},
	}
	frozen := ts.freezeDeferredTools()
	t.Logf("catalogo: %d candidati deferred, revisione %s", len(frozen.specs), frozen.catalogRevision)

	// Principle 2 in miniature: no scenario may name a label outside the frozen candidate
	// set. This is the check whose absence let the frozen dataset ship with 10 of 12
	// scenarios unsatisfiable.
	for _, s := range discoveryScenarios {
		if !slices.Contains(frozen.ids, s.expected) {
			t.Fatalf("%s: expected %q is NOT a deferred candidate — the label is unreachable, "+
				"exactly the defect this probe exists to prevent", s.id, s.expected)
		}
		if s.trap != "" && !slices.Contains(frozen.ids, s.trap) {
			t.Fatalf("%s: trap %q is not in the catalog, so the scenario cannot trap anything", s.id, s.trap)
		}
	}

	strategies := []string{ToolDiscoveryBM25, ToolDiscoverySemantic, ToolDiscoveryStatic}
	ctx := context.Background()
	top1 := map[string]int{}
	trapped := map[string][]string{}
	picks := map[string]map[string]string{} // scenario -> strategy -> top-1

	for _, s := range discoveryScenarios {
		picks[s.id] = map[string]string{}
		for _, strategy := range strategies {
			ranked, err := ts.rankFrozen(ctx, frozen, s.prompt, 5, strategy)
			if err != nil {
				t.Fatalf("%s/%s: rank: %v", s.id, strategy, err)
			}
			got := ""
			if len(ranked) > 0 {
				got = ranked[0].Spec().Name
			}
			picks[s.id][strategy] = got
			if got == s.expected {
				top1[strategy]++
			}
			if s.trap != "" && got == s.trap {
				trapped[strategy] = append(trapped[strategy], s.id)
			}
		}
	}

	t.Log("")
	t.Log("scenario  classe            atteso                          bm25 / semantic / static")
	for _, s := range discoveryScenarios {
		mark := func(strategy string) string {
			if picks[s.id][strategy] == s.expected {
				return "OK"
			}
			return picks[s.id][strategy]
		}
		t.Logf("%-9s %-17s %-31s %s | %s | %s",
			s.id, s.class, s.expected,
			mark(ToolDiscoveryBM25), mark(ToolDiscoverySemantic), mark(ToolDiscoveryStatic))
	}

	t.Log("")
	for _, strategy := range strategies {
		t.Logf("top-1 %-9s %d/%d   trappole prese: %v",
			strategy, top1[strategy], len(discoveryScenarios), trapped[strategy])
	}

	// The number that decides whether a bandit can learn anything here at all.
	disagreements := 0
	for _, s := range discoveryScenarios {
		p := picks[s.id]
		if p[ToolDiscoveryBM25] != p[ToolDiscoverySemantic] ||
			p[ToolDiscoverySemantic] != p[ToolDiscoveryStatic] {
			disagreements++
		}
	}
	t.Logf("")
	t.Logf("i bracci divergono su %d/%d scenari", disagreements, len(discoveryScenarios))
	if disagreements == 0 {
		t.Errorf("i tre bracci hanno scelto identicamente ovunque: con questi scenari " +
			"nessun bandit puo imparare niente — il dataset non discrimina")
	}
}
