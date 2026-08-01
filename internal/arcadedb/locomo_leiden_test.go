//go:build arcadedb_integration

// Are there communities in the entity graph, and are they worth anything?
//
// Leiden is what Microsoft GraphRAG uses to build the hierarchical summaries it
// answers GLOBAL questions with -- the ones no single passage covers. In this
// benchmark those are the open-domain category, where every configuration
// measured today sits between 9% and 33%: the worst category and the smallest,
// 92 questions of 1536.
//
// So this does the cheap thing first and looks at the communities BEFORE wiring
// anything to them. A community that groups "Melanie, pottery, clay, bowl" is a
// topic and can be summarised; one that groups a speaker with three hundred
// unrelated nouns is the co-occurrence graph of a two-person chat and means
// nothing. The printout is the evaluation.
//
// go-leiden is pure Go, no cgo, MIT, ported from graspologic-native -- the same
// Rust implementation GraphRAG uses -- which is what makes it interesting: it
// removes the Python-over-Bolt sidecar that was planned when Leiden meant GDS.
// It is also v0.1.0 and self-described as agent-written, so it is read and
// measured here, not trusted.
//
// Run: go test -tags arcadedb_integration -run LocomoLeiden -v ./internal/arcadedb/
package arcadedb

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	leiden "github.com/k8nstantin/go-leiden"
)

// coOccurrenceQuery builds the edges: two entities are connected when they are
// mentioned in the same turn. The weight is how often that happens, which is
// what makes a topic denser than a coincidence.
const coOccurrenceQuery = `
MATCH (a:LocomoEntity)-[:MENTIONED_IN]->(t:LocomoTurn)<-[:MENTIONED_IN]-(b:LocomoEntity)
WHERE a.name < b.name
RETURN a.name AS a, b.name AS b, count(t) AS weight

`

func TestLocomoLeidenCommunities(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()

	if rows, err := client.Query(ctx, "SELECT count(*) AS n FROM LocomoEntity", nil); err != nil ||
		len(rows) == 0 || rowInt(rows[0], "n") == 0 {
		t.Skip("entity layer not built; run TestLocomoMem0EntityLayerLoads first")
	}

	started := time.Now()
	rows, err := client.Read(ctx, coOccurrenceQuery, nil)
	if err != nil {
		t.Fatalf("co-occurrence: %v", err)
	}
	// The LIMIT is explicit because ArcadeDB caps a result set at 20000 by default
	// and says nothing: the first run of this test computed its communities from
	// 20000 of the 65149 real pairs -- 31% of the graph -- and reported a clean
	// number for it. Any query here that returns exactly 20000 rows is truncated,
	// not complete.
	t.Logf("read %d co-occurrence pairs in %s", len(rows), time.Since(started).Round(time.Millisecond))
	if len(rows) == 20000 {
		t.Fatal("exactly 20000 rows: the default result cap is still truncating the graph")
	}
	if len(rows) == 0 {
		t.Skip("no co-occurrence edges")
	}

	// Node ids are dense integers; go-leiden takes a count and an edge list.
	id := map[string]int{}
	name := []string{}
	edges := make([]leiden.Edge, 0, len(rows))
	for _, row := range rows {
		a, b := rowString(row, "a"), rowString(row, "b")
		weight := float64(rowInt(row, "weight"))
		if a == "" || b == "" || weight <= 0 {
			continue
		}
		for _, n := range []string{a, b} {
			if _, seen := id[n]; !seen {
				id[n] = len(name)
				name = append(name, n)
			}
		}
		edges = append(edges, leiden.Edge{From: id[a], To: id[b], Weight: weight})
	}

	options := leiden.DefaultOptions()
	options.Seed = 42
	started = time.Now()
	result, err := leiden.Leiden(ctx, len(name), edges, options)
	if err != nil {
		t.Fatalf("leiden: %v", err)
	}
	t.Logf("%d nodes, %d edges -> %d communities in %s (quality %.4f, %d iterations)",
		len(name), len(edges), result.NumClusters,
		time.Since(started).Round(time.Millisecond), result.Quality, result.Iterations)

	grouped := leiden.GroupBy(result.Partition)
	sizes := make([]int, 0, len(grouped))
	for cluster := range grouped {
		sizes = append(sizes, cluster)
	}
	sort.Slice(sizes, func(a, b int) bool {
		return len(grouped[sizes[a]]) > len(grouped[sizes[b]])
	})

	// The distribution first: one giant community and a tail of singletons is the
	// signature of a graph with no real structure, and no summary can fix that.
	histogram := map[string]int{}
	for _, cluster := range sizes {
		size := len(grouped[cluster])
		switch {
		case size == 1:
			histogram["1"]++
		case size <= 5:
			histogram["2-5"]++
		case size <= 25:
			histogram["6-25"]++
		case size <= 100:
			histogram["26-100"]++
		default:
			histogram["100+"]++
		}
	}
	t.Logf("community sizes: %d singletons, %d of 2-5, %d of 6-25, %d of 26-100, %d over 100",
		histogram["1"], histogram["2-5"], histogram["6-25"], histogram["26-100"], histogram["100+"])

	t.Log("largest communities, by member:")
	for i, cluster := range sizes {
		if i >= 6 {
			break
		}
		members := grouped[cluster]
		shown := make([]string, 0, 14)
		for _, node := range members {
			if len(shown) >= 14 {
				break
			}
			shown = append(shown, name[node])
		}
		t.Logf("  [%3d members] %s", len(members), truncateRunes(fmt.Sprint(shown), 210))
	}
}
