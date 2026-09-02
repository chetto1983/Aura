//go:build arcadedb_integration

// The connectivity measurement, against a live ArcadeDB.
//
// Every test here runs on a DISPOSABLE database carrying a seeded corpus, and that
// is a correction. They used to run against whatever ARCADEDB_DATABASE named, which
// made them pass richly on a developer's real memory and, on CI's empty one, either
// skip or fail on a delta the corpus was too small to produce. A gate that only
// holds where the data happens to be right is not a gate.
//
// The phase's headline numbers -- 30 of 107 facts linked, six bridges -- remain in
// .planning/phases/49.1-memory-graph-connectivity/49.1-VALIDATION.md as a dated
// measurement against the real corpus, reproducible with the command recorded there.
// What lives HERE is the behaviour that must hold on any corpus.
package arcadedb

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"
)

// The seeded corpus. Sized so the hub cap BINDS in both directions: 32 facts put the
// default 20% cap at 6 and a tightened 5% cap at 1, while the two hubs are mentioned
// by 5 and 3 facts. Every hub therefore survives the default and neither survives the
// tightening -- exactly the property TestMemoryMentionsSweepFollowsTheCapDownAndBack
// asserts, and which no arbitrary memory can be relied on to exhibit.
const (
	seedFillerFacts = 22
	seedHubOneFacts = 5
	seedHubTwoFacts = 3
)

// mentionCorpusClient provisions a disposable database and fills it with that corpus.
func mentionCorpusClient(t *testing.T) *Client {
	t.Helper()
	client := disposableArcadeClient(t)
	ctx := context.Background()
	if err := client.EnsureMemorySchema(ctx); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	now := time.Now().UTC()
	write := func(subject, object, statement string) {
		t.Helper()
		_, err := client.UpsertFact(ctx, Fact{
			Subject: subject, Predicate: "seeded_for", Object: object, Statement: statement,
			Source: FactSource{RunID: "mention-corpus", WriterRole: WriterParent}, ValidFrom: now,
		}, now)
		if err != nil {
			t.Fatalf("seed %q: %v", subject, err)
		}
	}
	// The anchors exist only to MINT the hub entities. A hub named in a fact that owns
	// it is not a mention of it (namesIn excludes the fact's own endpoints), so an
	// anchor contributes zero to the hub's count and the arithmetic above stays exact.
	write("SeedAnchorOne", "SeedHubOne", "SeedAnchorOne introduces the first hub.")
	write("SeedAnchorTwo", "SeedHubTwo", "SeedAnchorTwo introduces the second hub.")
	for index := 1; index <= seedHubOneFacts; index++ {
		write(fmt.Sprintf("SeedSubject%02d", index), fmt.Sprintf("SeedObject%02d", index),
			fmt.Sprintf("SeedSubject%02d depends on SeedHubOne for scheduling.", index))
	}
	for index := seedHubOneFacts + 1; index <= seedHubOneFacts+seedHubTwoFacts; index++ {
		write(fmt.Sprintf("SeedSubject%02d", index), fmt.Sprintf("SeedObject%02d", index),
			fmt.Sprintf("SeedSubject%02d depends on SeedHubTwo for storage.", index))
	}
	last := seedHubOneFacts + seedHubTwoFacts + seedFillerFacts
	for index := seedHubOneFacts + seedHubTwoFacts + 1; index <= last; index++ {
		write(fmt.Sprintf("SeedSubject%02d", index), fmt.Sprintf("SeedObject%02d", index),
			fmt.Sprintf("SeedSubject%02d stands alone and shares nothing.", index))
	}
	return client
}

// connectivity is the shape the phase reports: how much of the corpus is
// reachable from anywhere else, and how big the neighbourhoods got.
type connectivity struct {
	Facts    int
	Linked   int
	MaxDeg   int
	MedianOf int // median degree over LINKED facts, not over all of them
	Bridges  int
}

// measureConnectivity projects the MENTIONS graph down onto facts: two facts are
// neighbours when they hang off a shared mentioned entity. The projection is done
// here rather than in SQL because it is the measurement's definition, and a
// measurement that shares code with the thing it measures proves nothing.
func measureConnectivity(t *testing.T, client *Client) connectivity {
	t.Helper()
	ctx := context.Background()
	factRows, err := client.Query(ctx, "SELECT fact_key FROM "+factEdgeType+" LIMIT 5000", nil)
	if err != nil {
		t.Fatalf("count facts: %v", err)
	}
	edgeRows, err := client.Query(ctx,
		"SELECT inV().name AS target, fact_key FROM "+mentionsEdgeType+" LIMIT 20000", nil)
	if err != nil {
		t.Fatalf("read mention edges: %v", err)
	}
	byEntity := map[string]map[string]struct{}{}
	for _, row := range edgeRows {
		target, factKey := rowString(row, "target"), rowString(row, "fact_key")
		if target == "" || factKey == "" {
			continue
		}
		if byEntity[target] == nil {
			byEntity[target] = map[string]struct{}{}
		}
		byEntity[target][factKey] = struct{}{}
	}
	neighbours := map[string]map[string]struct{}{}
	for _, facts := range byEntity {
		keys := make([]string, 0, len(facts))
		for key := range facts {
			keys = append(keys, key)
		}
		for _, left := range keys {
			for _, right := range keys {
				if left == right {
					continue
				}
				if neighbours[left] == nil {
					neighbours[left] = map[string]struct{}{}
				}
				neighbours[left][right] = struct{}{}
			}
		}
	}
	degrees := make([]int, 0, len(neighbours))
	result := connectivity{Facts: len(factRows), Bridges: len(byEntity)}
	for _, peers := range neighbours {
		degrees = append(degrees, len(peers))
		result.MaxDeg = max(result.MaxDeg, len(peers))
	}
	sort.Ints(degrees)
	result.Linked = len(degrees)
	if len(degrees) > 0 {
		// Lower of the two central values on an even set, stated so the number is
		// reproducible rather than implementation-defined.
		result.MedianOf = degrees[(len(degrees)-1)/2]
	}
	return result
}

// TestMemoryMentionsConnectivityReport is R5: the before/after table.
func TestMemoryMentionsConnectivityReport(t *testing.T) {
	client := mentionCorpusClient(t)
	ctx := context.Background()
	if err := client.EnsureMemorySchema(ctx); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	entitiesBefore := countRows(t, client, "Entity")
	before := measureConnectivity(t, client)

	result, err := client.LinkMentions(ctx)
	if err != nil {
		t.Fatalf("LinkMentions: %v", err)
	}
	after := measureConnectivity(t, client)

	t.Logf("corpus: %d facts, %d entities, %d name-shaped candidates, cap %d",
		result.Facts, result.Entities, result.Candidates, result.Cap)
	t.Logf("sweep: created %d, removed %d, bridges %d, covered %v",
		result.Created, result.Removed, result.Bridges, result.Covered)
	t.Logf("connectivity | linked/facts | max degree | median over linked | bridges")
	t.Logf("      before | %8d/%3d | %10d | %18d | %7d",
		before.Linked, before.Facts, before.MaxDeg, before.MedianOf, before.Bridges)
	t.Logf("       after | %8d/%3d | %10d | %18d | %7d",
		after.Linked, after.Facts, after.MaxDeg, after.MedianOf, after.Bridges)

	// No skip on an empty corpus: the seed guarantees one, so zero facts here would
	// mean the seeding itself silently failed -- which a skip would have hidden.
	if after.Facts != seedHubOneFacts+seedHubTwoFacts+seedFillerFacts+2 {
		t.Fatalf("corpus = %d facts, want the seeded %d",
			after.Facts, seedHubOneFacts+seedHubTwoFacts+seedFillerFacts+2)
	}
	// The assertion is on the state after linking, not on the delta. The sweep is
	// idempotent, so on every run but the first the correct delta is ZERO -- a
	// strict-increase gate would fail precisely because the code works.
	if after.Linked == 0 {
		t.Fatal("linking produced no connectivity at all")
	}
	if after.Linked < before.Linked {
		t.Fatalf("linking LOST connectivity: %d linked before, %d after",
			before.Linked, after.Linked)
	}
	// The cap's whole job. A neighbourhood the size of the corpus is a clique, and
	// a traversal that returns everything is as useless as one that returns nothing.
	if after.MaxDeg > result.Cap {
		t.Fatalf("max degree %d exceeds the hub cap %d", after.MaxDeg, result.Cap)
	}
	if got := countRows(t, client, "Entity"); got != entitiesBefore {
		t.Fatalf("linking minted entities: %d before, %d after", entitiesBefore, got)
	}
}

// TestMemoryMentionsSweepIsIdempotent is R4: a second pass over an unchanged
// corpus must be a no-op, or the graph accumulates history instead of matching
// the configuration.
func TestMemoryMentionsSweepIsIdempotent(t *testing.T) {
	client := mentionCorpusClient(t)
	ctx := context.Background()
	if err := client.EnsureMemorySchema(ctx); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	if _, err := client.LinkMentions(ctx); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	edgesAfterFirst := countRows(t, client, mentionsEdgeType)
	second, err := client.LinkMentions(ctx)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if second.Created != 0 || second.Removed != 0 {
		t.Fatalf("second sweep changed the graph: created %d, removed %d",
			second.Created, second.Removed)
	}
	if got := countRows(t, client, mentionsEdgeType); got != edgesAfterFirst {
		t.Fatalf("edge count drifted: %d then %d", edgesAfterFirst, got)
	}
}

// TestMemoryMentionsSecondHopReachesWhatTheFirstCannot is R3, and the reason the
// whole phase exists: at depth 1 an entity answers only with what is stated about
// it directly.
func TestMemoryMentionsSecondHopReachesWhatTheFirstCannot(t *testing.T) {
	client := mentionCorpusClient(t)
	ctx := context.Background()
	if err := client.EnsureMemorySchema(ctx); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	if _, err := client.LinkMentions(ctx); err != nil {
		t.Fatalf("LinkMentions: %v", err)
	}
	rows, err := client.Query(ctx,
		"SELECT outV().name AS source, count(*) AS degree FROM "+mentionsEdgeType+
			" GROUP BY outV().name ORDER BY degree DESC LIMIT 1", nil)
	if err != nil {
		t.Fatalf("find a linked entity: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the seeded corpus produced no mention edges: the sweep linked nothing")
	}
	entity := rowString(rows[0], "source")

	direct, err := client.FactsAbout(ctx, entity, "", 100, time.Time{}, FactsAboutDirect)
	if err != nil {
		t.Fatalf("depth 1: %v", err)
	}
	wide, err := client.FactsAbout(ctx, entity, "", 100, time.Time{}, FactsAboutNeighbourhood)
	if err != nil {
		t.Fatalf("depth 2: %v", err)
	}
	t.Logf("%q: depth 1 returned %d facts, depth 2 returned %d", entity, len(direct), len(wide))
	if len(wide) <= len(direct) {
		t.Fatalf("depth 2 reached nothing new for %q: %d then %d",
			entity, len(direct), len(wide))
	}
	seen := map[string]struct{}{}
	for _, hit := range direct {
		seen[hit.FactKey] = struct{}{}
	}
	for _, hit := range wide {
		if _, ok := seen[hit.FactKey]; !ok {
			return
		}
	}
	t.Fatal("depth 2 returned more rows but no fact the first hop did not already have")
}

// TestMemoryMentionsSecondHopIsDeterministic pins the ordering: a neighbourhood
// that reshuffles between two identical calls is not something a caller can
// reason about, and it defeats prompt caching downstream.
func TestMemoryMentionsSecondHopIsDeterministic(t *testing.T) {
	client := mentionCorpusClient(t)
	ctx := context.Background()
	if _, err := client.LinkMentions(ctx); err != nil {
		t.Fatalf("LinkMentions: %v", err)
	}
	rows, err := client.Query(ctx,
		"SELECT outV().name AS source FROM "+mentionsEdgeType+" LIMIT 1", nil)
	if err != nil {
		t.Fatalf("find a linked entity: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the seeded corpus produced no mention edges: nothing to order")
	}
	entity := rowString(rows[0], "source")
	first, err := client.FactsAbout(ctx, entity, "", 50, time.Time{}, FactsAboutNeighbourhood)
	if err != nil {
		t.Fatalf("depth 2: %v", err)
	}
	second, err := client.FactsAbout(ctx, entity, "", 50, time.Time{}, FactsAboutNeighbourhood)
	if err != nil {
		t.Fatalf("depth 2 again: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("row count changed between identical calls: %d then %d", len(first), len(second))
	}
	for index := range first {
		if first[index].FactKey != second[index].FactKey {
			t.Fatalf("order changed at %d: %q then %q",
				index, first[index].FactKey, second[index].FactKey)
		}
	}
}

func countRows(t *testing.T, client *Client, typeName string) int {
	t.Helper()
	rows, err := client.Query(context.Background(), "SELECT count(*) AS total FROM "+typeName, nil)
	if err != nil {
		t.Fatalf("count %s: %v", typeName, err)
	}
	if len(rows) == 0 {
		return 0
	}
	return int(rowInt(rows[0], "total"))
}

// TestMemoryMentionsSweepFollowsTheCapDownAndBack is the other half of R4: a
// re-run after the cap changes must REMOVE the edges of entities that are now
// above it. Without that the graph accumulates every cap it has ever had, and the
// configured value stops describing what is actually stored.
//
// It restores the default cap before returning, so the live memory it runs
// against is left exactly as it was found.
func TestMemoryMentionsSweepFollowsTheCapDownAndBack(t *testing.T) {
	client := mentionCorpusClient(t)
	ctx := context.Background()
	if err := client.EnsureMemorySchema(ctx); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	withCap := func(share float64) MentionLinkResult {
		t.Helper()
		client.limits = MemoryLimits{MentionHubShare: share}.normalized()
		result, err := client.LinkMentions(ctx)
		if err != nil {
			t.Fatalf("LinkMentions at cap %.2f: %v", share, err)
		}
		return result
	}
	defer func() { withCap(0.20) }()

	atDefault := withCap(0.20)
	edgesAtDefault := countRows(t, client, mentionsEdgeType)

	tightened := withCap(0.05)
	edgesWhenTight := countRows(t, client, mentionsEdgeType)
	t.Logf("cap %d -> %d: %d edges became %d (removed %d)",
		atDefault.Cap, tightened.Cap, edgesAtDefault, edgesWhenTight, tightened.Removed)
	if tightened.Removed == 0 {
		t.Fatal("tightening the cap removed nothing")
	}
	if edgesWhenTight >= edgesAtDefault {
		t.Fatalf("tightening the cap did not shrink the graph: %d then %d",
			edgesAtDefault, edgesWhenTight)
	}

	restored := withCap(0.20)
	if got := countRows(t, client, mentionsEdgeType); got != edgesAtDefault {
		t.Fatalf("restoring the cap did not restore the graph: %d, want %d (created %d)",
			got, edgesAtDefault, restored.Created)
	}
}
