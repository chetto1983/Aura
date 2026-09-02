package arcadedb

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Part A exercises desiredMentionEdges/mentionStats/sortedMentionEdges as pure
// functions of raw query rows -- no server involved. Part B exercises
// LinkMentions end to end against recordingClient/routedClient.

// ---- Part A helpers --------------------------------------------------------

func factRow(key, statement, subject, object string) map[string]any {
	return map[string]any{"fact_key": key, "statement": statement, "subject": subject, "object": object}
}

func mkEdge(source, target, factKey string) mentionEdge {
	return mentionEdge{Source: source, Target: target, FactKey: factKey}
}

func hasEdge(edges map[mentionEdge]struct{}, source, target, factKey string) bool {
	_, ok := edges[mkEdge(source, target, factKey)]
	return ok
}

func noEdgeToTarget(edges map[mentionEdge]struct{}, target string) bool {
	for e := range edges {
		if e.Target == target {
			return false
		}
	}
	return true
}

// A1: a fact whose statement names a known, name-shaped entity links BOTH of
// its own endpoints to that entity -- one edge per endpoint, not one per fact.
func TestDesiredMentionEdgesLinksBothEndpointsToAMentionedEntity(t *testing.T) {
	facts := []map[string]any{factRow("f1", "Davide built Aura using ArcadeDB.", "Davide", "Aura")}
	edges, _ := desiredMentionEdges([]string{"ArcadeDB"}, facts, 1.0)
	if len(edges) != 2 {
		t.Fatalf("edges = %v, want exactly 2 (one per endpoint)", edges)
	}
	if !hasEdge(edges, "Davide", "ArcadeDB", "f1") || !hasEdge(edges, "Aura", "ArcadeDB", "f1") {
		t.Fatalf("edges = %v, want Davide->ArcadeDB and Aura->ArcadeDB, both carrying f1", edges)
	}
}

// A2: a fact naming its OWN subject or object in prose creates no edge to
// that entity, but a different mentioned entity still links -- subject/object
// are passed as the "owned" set to namesIn so a fact cannot point at itself.
func TestDesiredMentionEdgesExcludesAFactsOwnEndpointsButKeepsOthers(t *testing.T) {
	entities := []string{"Davide", "Aura", "ArcadeDB"}
	facts := []map[string]any{factRow("f1", "Davide talked to Aura about ArcadeDB.", "Davide", "Aura")}
	edges, _ := desiredMentionEdges(entities, facts, 1.0)
	if !noEdgeToTarget(edges, "Davide") || !noEdgeToTarget(edges, "Aura") {
		t.Fatalf("edges = %v, want no edge targeting the fact's own subject or object", edges)
	}
	if len(edges) != 2 || !hasEdge(edges, "Davide", "ArcadeDB", "f1") || !hasEdge(edges, "Aura", "ArcadeDB", "f1") {
		t.Fatalf("edges = %v, want both endpoints linked to the other mentioned entity ArcadeDB", edges)
	}
}

// A3: a name in the statement that is not in the entity catalog is never
// looked for -- the scanner only knows the catalog's names -- so it creates
// no entity and no edge.
func TestDesiredMentionEdgesIgnoresAnUnknownEntityName(t *testing.T) {
	entities := []string{"Davide", "Aura"} // Neo4j deliberately absent
	facts := []map[string]any{factRow("f1", "Davide told Aura about Neo4j.", "Davide", "Aura")}
	edges, _ := desiredMentionEdges(entities, facts, 1.0)
	if !noEdgeToTarget(edges, "Neo4j") {
		t.Fatalf("edges = %v, want no edge targeting the unknown name Neo4j", edges)
	}
	if len(edges) != 0 {
		t.Fatalf("edges = %v, want none: the only mentioned name is unknown, and both endpoints own themselves", edges)
	}
}

// A4 is the single most important boundary in the file: the hub-cap
// comparison is INCLUSIVE (counts[name] > stats.cap excludes, so
// counts[name] == cap still links) and evaluated over the WHOLE corpus.
//
// share=0.5 keeps int(facts*share) exact in float64 (no rounding surprise):
// hubCap(4, 0.5) = int(2.0) = 2 and hubCap(5, 0.5) = int(2.5) = 2 -- the cap
// itself does not move when a fifth fact is added, only the mention count
// does, from exactly the cap to one past it.
func TestDesiredMentionEdgesHubCapIsInclusiveThenExcludesOnTheNextMention(t *testing.T) {
	const share = 0.5
	entities := []string{"Hub"}
	base := []map[string]any{
		factRow("f1", "Davide praised Hub today.", "Davide", "Marta"),
		factRow("f2", "Alice mentioned Hub too.", "Alice", "Bob"),
		factRow("f3", "Nothing special happened.", "Carol", "Dave"),
		factRow("f4", "Just another day.", "Eve", "Frank"),
	}
	wantCap := hubCap(len(base), share)
	if wantCap != 2 {
		t.Fatalf("test setup: hubCap(%d, %v) = %d, want 2 so Hub is mentioned by exactly the cap (f1, f2)", len(base), share, wantCap)
	}
	edges, stats := desiredMentionEdges(entities, base, share)
	if stats.cap != wantCap {
		t.Fatalf("cap = %d, want %d", stats.cap, wantCap)
	}
	if len(edges) != 4 ||
		!hasEdge(edges, "Davide", "Hub", "f1") || !hasEdge(edges, "Marta", "Hub", "f1") ||
		!hasEdge(edges, "Alice", "Hub", "f2") || !hasEdge(edges, "Bob", "Hub", "f2") {
		t.Fatalf("edges = %v, want Hub linked from both endpoints of f1 and f2 (mentioned by exactly the cap)", edges)
	}

	withOneMore := append(append([]map[string]any{}, base...), factRow("f5", "Grace also likes Hub.", "Grace", "Heidi"))
	newCap := hubCap(len(withOneMore), share)
	if newCap != wantCap {
		t.Fatalf("test setup: hubCap(%d, %v) = %d, want it still %d so only the mention count moved", len(withOneMore), share, newCap, wantCap)
	}
	edgesAfter, statsAfter := desiredMentionEdges(entities, withOneMore, share)
	if statsAfter.cap != newCap {
		t.Fatalf("cap after = %d, want %d", statsAfter.cap, newCap)
	}
	if len(edgesAfter) != 0 {
		t.Fatalf("edges after one more mention = %v, want NONE: mentions(Hub)=3 exceeds cap=%d, and the "+
			"exclusion is corpus-wide -- it also drops f1 and f2's previously-valid edges", edgesAfter, newCap)
	}
}

// A5: degenerate inputs yield an empty edge set and must not panic.
func TestDesiredMentionEdgesEmptyInputsYieldNoEdgesAndDoNotPanic(t *testing.T) {
	facts := []map[string]any{factRow("f1", "Something about Hub.", "A", "B")}
	t.Run("cap of zero excludes every mention", func(t *testing.T) {
		edges, stats := desiredMentionEdges([]string{"Hub"}, facts, 0)
		if stats.cap != 0 || len(edges) != 0 {
			t.Fatalf("edges = %v, stats = %+v, want cap 0 and no edges", edges, stats)
		}
	})
	t.Run("empty fact slice", func(t *testing.T) {
		edges, stats := desiredMentionEdges([]string{"Hub"}, nil, 0.5)
		if stats.cap != 0 || len(edges) != 0 {
			t.Fatalf("edges = %v, stats = %+v, want cap 0 (facts<=0) and no edges", edges, stats)
		}
	})
	t.Run("no name-shaped entities", func(t *testing.T) {
		edges, stats := desiredMentionEdges([]string{"il container", "un gate"}, facts, 0.9)
		if stats.candidates != 0 || len(edges) != 0 {
			t.Fatalf("edges = %v, stats = %+v, want zero candidates and no edges", edges, stats)
		}
	})
}

// A6: an edge with no fact key could never be diffed away by LinkMentions
// (create/delete both bind :fact_key), so a row missing one is skipped.
func TestDesiredMentionEdgesSkipsAFactRowWithNoFactKey(t *testing.T) {
	facts := []map[string]any{factRow("", "Something about Hub.", "A", "B")}
	edges, stats := desiredMentionEdges([]string{"Hub"}, facts, 1.0)
	if len(edges) != 0 {
		t.Fatalf("edges = %v, want none: the only fact row has an empty fact_key", edges)
	}
	if stats.bridges != 0 {
		t.Fatalf("bridges = %d, want 0: a skipped row bridges nothing", stats.bridges)
	}
}

// A7 is the held-out backstop for A4: the cap sums mentions over the WHOLE
// corpus in a first pass, so fact order must not change the result.
func TestDesiredMentionEdgesIsOrderIndependent(t *testing.T) {
	f1 := factRow("f1", "Davide praised Hub today.", "Davide", "Marta")
	f2 := factRow("f2", "Alice mentioned Hub too.", "Alice", "Bob")
	f3 := factRow("f3", "Nothing special happened.", "Carol", "Dave")
	f4 := factRow("f4", "Just another day.", "Eve", "Frank")

	edgesA, statsA := desiredMentionEdges([]string{"Hub"}, []map[string]any{f1, f2, f3, f4}, 0.5)
	edgesB, statsB := desiredMentionEdges([]string{"Hub"}, []map[string]any{f4, f2, f1, f3}, 0.5)
	if statsA != statsB {
		t.Fatalf("stats = %+v, want identical stats regardless of order: %+v", statsA, statsB)
	}
	sortedA, sortedB := sortedMentionEdges(edgesA), sortedMentionEdges(edgesB)
	if !reflect.DeepEqual(sortedA, sortedB) {
		t.Fatalf("sorted edges differ by input order:\n forward:  %v\n shuffled: %v", sortedA, sortedB)
	}
	if len(sortedA) == 0 {
		t.Fatal("test setup: expected a non-empty edge set to make the comparison meaningful")
	}
}

// A8: mentionStats' three numbers, pinned on a fixture with known counts.
func TestMentionStatsCountsCandidatesBridgesAndCap(t *testing.T) {
	entities := []string{"Hub1", "Hub2", "Hub3", "il container"} // last one is not name-shaped
	facts := []map[string]any{
		factRow("f1", "Something about Hub1 happened.", "A", "B"),
		factRow("f2", "Something about Hub2 happened.", "C", "D"),
		factRow("f3", "Nothing interesting here.", "E", "F"), // mentions neither Hub
	}
	_, stats := desiredMentionEdges(entities, facts, 1.0)
	wantCap := hubCap(len(facts), 1.0)
	if stats.candidates != 3 {
		t.Fatalf("candidates = %d, want 3 (Hub1, Hub2, Hub3; \"il container\" is not name-shaped)", stats.candidates)
	}
	if stats.bridges != 2 {
		t.Fatalf("bridges = %d, want 2: Hub1 and Hub2 each contribute an edge, Hub3 is never mentioned", stats.bridges)
	}
	if stats.cap != wantCap {
		t.Fatalf("cap = %d, want hubCap(%d, 1.0) = %d", stats.cap, len(facts), wantCap)
	}
}

// A9: sortedMentionEdges is a total order over (Source, Target, FactKey),
// independent of how the underlying map was built.
func TestSortedMentionEdgesIsDeterministicAndTotallyOrdered(t *testing.T) {
	e1, e2, e3, e4 := mkEdge("B", "X", "f1"), mkEdge("A", "Y", "f1"), mkEdge("A", "X", "f2"), mkEdge("A", "X", "f1")
	want := []mentionEdge{e4, e3, e2, e1} // Source asc, then Target asc, then FactKey asc

	forward, reverse := map[mentionEdge]struct{}{}, map[mentionEdge]struct{}{}
	for _, e := range []mentionEdge{e1, e2, e3, e4} {
		forward[e] = struct{}{}
	}
	for _, e := range []mentionEdge{e4, e3, e2, e1} {
		reverse[e] = struct{}{}
	}
	if got := sortedMentionEdges(forward); !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedMentionEdges(forward-built map) = %v, want %v", got, want)
	}
	if got := sortedMentionEdges(reverse); !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedMentionEdges(reverse-built map) = %v, want %v", got, want)
	}
}

// ---- Part B: LinkMentions against the fake server --------------------------

// The one-fact fixture every B test starts from: a single fact naming Hub
// from both its endpoints, producing exactly 2 desired edges.
const (
	mentionOneFactEntityBody = `{"result":[{"name":"Hub"}]}`
	mentionOneFactFactBody   = `{"result":[{"fact_key":"f1","statement":"Davide praised Hub today.","subject":"Davide","object":"Marta"}]}`
)

// withUncappedShare sets a hub share generous enough that a 1-2 fact fixture
// is never excluded by the cap. The default 0.20 share floors to 0 on a
// corpus this small and would exclude everything.
func withUncappedShare(client *Client) {
	client.limits = MemoryLimits{MentionHubShare: 1.0}.normalized()
}

// B1: idempotence. When the existing-edge scan already equals what the
// current corpus and cap imply, LinkMentions issues no write at all.
func TestLinkMentionsIsIdempotentWhenExistingMatchesDesired(t *testing.T) {
	client, rec := recordingClient(t, mentionOneFactEntityBody, mentionOneFactFactBody,
		`{"result":[{"source":"Davide","target":"Hub","fact_key":"f1"},{"source":"Marta","target":"Hub","fact_key":"f1"}]}`)
	withUncappedShare(client)

	result, err := client.LinkMentions(context.Background())
	if err != nil {
		t.Fatalf("LinkMentions: %v", err)
	}
	if result.Created != 0 || result.Removed != 0 {
		t.Fatalf("result = %+v, want Created=0 Removed=0", result)
	}
	// Exactly the 3 reads (entity, fact, existing-edge scans) and nothing else.
	if len(rec.statements) != 3 {
		t.Fatalf("statements = %v, want exactly the 3 scans and no command", rec.statements)
	}
	for _, statement := range rec.statements {
		if strings.Contains(statement, "CREATE EDGE") || strings.Contains(statement, "DELETE FROM "+mentionsEdgeType) {
			t.Fatalf("idempotent run issued a write: %s", statement)
		}
	}
}

// B2: creation. An empty existing-edge scan means every desired edge is
// missing, so LinkMentions creates all of them with the right bind params.
func TestLinkMentionsCreatesMissingEdges(t *testing.T) {
	client, rec := recordingClient(t, mentionOneFactEntityBody, mentionOneFactFactBody, `{"result":[]}`)
	withUncappedShare(client)

	result, err := client.LinkMentions(context.Background())
	if err != nil {
		t.Fatalf("LinkMentions: %v", err)
	}
	if result.Created != 2 || result.Removed != 0 {
		t.Fatalf("result = %+v, want Created=2 Removed=0", result)
	}
	creates := rec.statements[3:]
	if len(creates) != 2 {
		t.Fatalf("create statements = %v, want 2", creates)
	}
	for _, statement := range creates {
		if !strings.Contains(statement, "CREATE EDGE "+mentionsEdgeType) {
			t.Fatalf("statement = %q, want a CREATE EDGE %s", statement, mentionsEdgeType)
		}
	}
	// sortedMentionEdges orders by Source, so "Davide" precedes "Marta".
	if got := rec.params[3]; got["source"] != "Davide" || got["target"] != "Hub" || got["fact_key"] != "f1" {
		t.Fatalf("params[3] = %v, want Davide->Hub,f1", got)
	}
	if got := rec.params[4]; got["source"] != "Marta" || got["target"] != "Hub" || got["fact_key"] != "f1" {
		t.Fatalf("params[4] = %v, want Marta->Hub,f1", got)
	}
}

// B3: removal. An existing edge that the current corpus and cap no longer
// imply is deleted, and only that edge.
func TestLinkMentionsRemovesAnEdgeTheCapNoLongerImplies(t *testing.T) {
	client, rec := recordingClient(t, mentionOneFactEntityBody, mentionOneFactFactBody,
		`{"result":[{"source":"Davide","target":"Hub","fact_key":"f1"},{"source":"Marta","target":"Hub","fact_key":"f1"},`+
			`{"source":"Ghost","target":"Hub","fact_key":"stale"}]}`)
	withUncappedShare(client)

	result, err := client.LinkMentions(context.Background())
	if err != nil {
		t.Fatalf("LinkMentions: %v", err)
	}
	if result.Created != 0 || result.Removed != 1 {
		t.Fatalf("result = %+v, want Created=0 Removed=1", result)
	}
	if len(rec.statements) != 4 {
		t.Fatalf("statements = %v, want the 3 scans plus exactly 1 delete", rec.statements)
	}
	if !strings.Contains(rec.statements[3], "DELETE FROM "+mentionsEdgeType) {
		t.Fatalf("statement = %q, want a DELETE FROM %s", rec.statements[3], mentionsEdgeType)
	}
	if got := rec.params[3]; got["source"] != "Ghost" || got["target"] != "Hub" || got["fact_key"] != "stale" {
		t.Fatalf("delete params = %v, want the stale Ghost->Hub,stale edge", got)
	}
}

// B4: LinkMentions may never touch a fact's own content. The corpus below
// produces both creates and a delete in the same run, so every statement kind
// the sweep can emit is checked against the prohibition.
func TestLinkMentionsNeverMutatesFacts(t *testing.T) {
	client, rec := recordingClient(t, mentionOneFactEntityBody,
		`{"result":[{"fact_key":"f1","statement":"Davide praised Hub today.","subject":"Davide","object":"Marta"},`+
			`{"fact_key":"f2","statement":"Alice noted Hub too.","subject":"Alice","object":"Bob"}]}`,
		`{"result":[{"source":"Davide","target":"Hub","fact_key":"f1"},{"source":"Ghost","target":"Hub","fact_key":"stale"}]}`)
	withUncappedShare(client)

	result, err := client.LinkMentions(context.Background())
	if err != nil {
		t.Fatalf("LinkMentions: %v", err)
	}
	if result.Created != 3 || result.Removed != 1 {
		t.Fatalf("result = %+v, want Created=3 Removed=1 (test setup sanity)", result)
	}
	for _, statement := range rec.statements {
		upper := strings.ToUpper(statement)
		for _, forbidden := range []string{"UPDATE", "DELETE FROM " + factEdgeType, "DELETE EDGE " + factEdgeType, "SET STATEMENT"} {
			if strings.Contains(upper, forbidden) {
				t.Fatalf("statement touches FACT: %s", statement)
			}
		}
	}
}

// B5: truncation must be visible. When the fact scan returns more rows than
// the configured bound, Covered is false and Facts equals the bound.
func TestLinkMentionsTruncationIsVisible(t *testing.T) {
	t.Run("more rows than the bound", func(t *testing.T) {
		client, rec := recordingClient(t, mentionOneFactEntityBody,
			`{"result":[{"fact_key":"f1","statement":"x","subject":"A","object":"B"},`+
				`{"fact_key":"f2","statement":"x","subject":"C","object":"D"},`+
				`{"fact_key":"f3","statement":"x","subject":"E","object":"F"}]}`,
			`{"result":[]}`)
		client.limits = MemoryLimits{DigestScan: 2}.normalized()

		result, err := client.LinkMentions(context.Background())
		if err != nil {
			t.Fatalf("LinkMentions: %v", err)
		}
		if result.Covered {
			t.Fatalf("result = %+v, want Covered=false: the fact scan returned 3 rows for a bound of 2", result)
		}
		if result.Facts != 2 {
			t.Fatalf("Facts = %d, want the bound 2, not the oversized row count 3", result.Facts)
		}
		if !strings.Contains(rec.statements[0], "LIMIT 2") {
			t.Fatalf("entity scan = %q, want LIMIT 2", rec.statements[0])
		}
		if !strings.Contains(rec.statements[1], "LIMIT 3") {
			t.Fatalf("fact scan = %q, want LIMIT 3 (bound+1, so the extra row can be detected)", rec.statements[1])
		}
	})
	t.Run("at or under the bound is fully covered", func(t *testing.T) {
		client, _ := recordingClient(t, mentionOneFactEntityBody,
			`{"result":[{"fact_key":"f1","statement":"x","subject":"A","object":"B"}]}`, `{"result":[]}`)
		client.limits = MemoryLimits{DigestScan: 2}.normalized()

		result, err := client.LinkMentions(context.Background())
		if err != nil {
			t.Fatalf("LinkMentions: %v", err)
		}
		if !result.Covered || result.Facts != 1 {
			t.Fatalf("result = %+v, want Covered=true and Facts=1", result)
		}
	})
}

// mentionScanRouter answers the entity and fact scans normally, routes the
// existing-edge scan to whatever the caller wants, and answers everything
// else (the create/delete commands) with an empty success.
func mentionScanRouter(edgeScan testResponse) func(recordedRequest) testResponse {
	return func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.HasPrefix(statement, mentionEntityScanStatement):
			return testResponse{Body: mentionOneFactEntityBody}
		case strings.HasPrefix(statement, mentionFactScanStatement):
			return testResponse{Body: mentionOneFactFactBody}
		case strings.HasPrefix(statement, mentionEdgeScanStatement):
			return edgeScan
		default:
			return testResponse{Body: `{"result":[]}`}
		}
	}
}

// B6: a memory whose schema predates MENTIONS reports the existing-edge scan
// as empty rather than failing (isMissingTypeError), but any OTHER failure on
// that same scan is still returned as an error, so the fallback cannot
// swallow a real failure.
func TestLinkMentionsTreatsAMissingMentionsTypeAsEmptyButPropagatesOtherErrors(t *testing.T) {
	t.Run("missing type falls back to empty", func(t *testing.T) {
		client, requests := routedClient(t, mentionScanRouter(testResponse{
			Status: http.StatusInternalServerError, Body: `{"detail":"class 'MENTIONS' was not found"}`,
		}))
		withUncappedShare(client)

		result, err := client.LinkMentions(context.Background())
		if err != nil {
			t.Fatalf("LinkMentions: %v, want the missing-type scan to fall back to an empty set", err)
		}
		if result.Created != 2 || result.Removed != 0 {
			t.Fatalf("result = %+v, want both desired edges created against a treated-empty existing set", result)
		}
		sawEdgeScan := false
		for _, request := range *requests {
			statement, _ := request.Payload["command"].(string)
			if strings.HasPrefix(statement, mentionEdgeScanStatement) {
				sawEdgeScan = true
			}
		}
		if !sawEdgeScan {
			t.Fatal("test setup: the existing-edge scan was never issued")
		}
	})
	t.Run("a different failure is returned, not swallowed", func(t *testing.T) {
		client, _ := routedClient(t, mentionScanRouter(testResponse{
			Status: http.StatusForbidden, Body: `{"detail":"tenant refused"}`,
		}))
		withUncappedShare(client)

		_, err := client.LinkMentions(context.Background())
		if err == nil || !strings.Contains(err.Error(), "tenant refused") {
			t.Fatalf("err = %v, want a propagated \"tenant refused\" failure", err)
		}
	})
}

// B7: the fact scan must observe valid time -- both the SQL condition and the
// as_of bind parameter.
func TestLinkMentionsFactScanCarriesValidTimeCondition(t *testing.T) {
	client, rec := recordingClient(t, mentionOneFactEntityBody, mentionOneFactFactBody, `{"result":[]}`)
	withUncappedShare(client)

	if _, err := client.LinkMentions(context.Background()); err != nil {
		t.Fatalf("LinkMentions: %v", err)
	}
	if !strings.Contains(rec.statements[1], asOfCondition) {
		t.Fatalf("fact scan = %q, want the validity condition %q", rec.statements[1], asOfCondition)
	}
	if asOf, _ := rec.params[1]["as_of"].(string); asOf == "" {
		t.Fatalf("fact scan params = %v, want a non-empty as_of bind parameter", rec.params[1])
	}
}
