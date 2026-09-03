//go:build arcadedb_integration

// A measurement, not a feature. It answers two questions in one read-only pass over a
// live memory, before any community code is written:
//
//  1. How much does the hub cap's PROPORTIONAL shape cost a small corpus? hubCap is
//     int(facts * share), so a 9-fact memory gets a cap of 1 and a 4-fact memory gets 0 —
//     at which point every shared name is treated as a hub and the second hop is
//     structurally dead. A floor would change that; this measures by how much.
//
//  2. Given the graph the linker actually produces, is there anything for Leiden to
//     cluster? Communities are a layer ON TOP of structure. On an edgeless graph the
//     answer is N singletons, and no resolution parameter rescues that.
//
// It WRITES NOTHING. desiredMentionEdges is pure, so the whole sweep is simulated in
// memory from one SELECT of entities and one of facts — which is what makes it safe to
// point at a real memory.
//
//	ARCADEDB_DATABASE=mem_<identity> go test -tags arcadedb_integration \
//	  -run TestMemoryCommunitiesSpike -v ./internal/arcadedb/
package arcadedb

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	leiden "github.com/k8nstantin/go-leiden"
)

// factGraph projects the mention edges down onto FACTS: two facts are adjacent when they
// hang off a shared mentioned entity. That projection is the graph a second hop actually
// walks, so it is the one worth clustering — clustering the entity graph would answer a
// different question.
func factGraph(edges map[mentionEdge]struct{}) (nodes []string, leidenEdges []leiden.Edge) {
	byEntity := map[string]map[string]struct{}{}
	for edge := range edges {
		if byEntity[edge.Target] == nil {
			byEntity[edge.Target] = map[string]struct{}{}
		}
		byEntity[edge.Target][edge.FactKey] = struct{}{}
	}
	index := map[string]int{}
	id := func(factKey string) int {
		if at, ok := index[factKey]; ok {
			return at
		}
		index[factKey] = len(nodes)
		nodes = append(nodes, factKey)
		return len(nodes) - 1
	}
	// Weight is the number of entities two facts share: co-mentioning three things is a
	// stronger tie than co-mentioning one, and CPM reads weights.
	weight := map[[2]int]float64{}
	for _, facts := range byEntity {
		keys := make([]string, 0, len(facts))
		for key := range facts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for i := range keys {
			for j := i + 1; j < len(keys); j++ {
				weight[[2]int{id(keys[i]), id(keys[j])}]++
			}
		}
	}
	for pair, w := range weight {
		leidenEdges = append(leidenEdges, leiden.Edge{From: pair[0], To: pair[1], Weight: w})
	}
	sort.Slice(leidenEdges, func(i, j int) bool {
		if leidenEdges[i].From != leidenEdges[j].From {
			return leidenEdges[i].From < leidenEdges[j].From
		}
		return leidenEdges[i].To < leidenEdges[j].To
	})
	return nodes, leidenEdges
}

func TestMemoryCommunitiesSpike(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()

	// The same two reads LinkMentions performs, and nothing else: no CREATE, no DELETE.
	scan := client.memoryLimits().DigestScan
	entityRows, err := client.Query(ctx, mentionEntityScanStatement+strconv.Itoa(scan), nil)
	if err != nil {
		t.Fatalf("read entities: %v", err)
	}
	entities := make([]string, 0, len(entityRows))
	for _, row := range entityRows {
		if name := rowString(row, "name"); name != "" {
			entities = append(entities, name)
		}
	}
	factRows, err := client.Query(ctx, mentionFactScanStatement+strconv.Itoa(scan+1),
		map[string]any{"as_of": time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	t.Logf("corpus: %d facts, %d entities", len(factRows), len(entities))

	t.Log("share  cap  edges  linked  bridges  nodes  communities  largest  quality")
	for _, share := range []float64{0.05, 0.10, 0.20, 0.35, 0.50} {
		edges, stats := desiredMentionEdges(entities, factRows, share)
		nodes, graph := factGraph(edges)
		line := fmt.Sprintf("%5.2f  %3d  %5d  %6d  %7d  %5d",
			share, stats.cap, len(edges), stats.bridges, stats.bridges, len(nodes))
		if len(nodes) == 0 {
			t.Log(line + "            -        -        -")
			continue
		}
		result, err := leiden.Leiden(ctx, len(nodes), graph, leiden.DefaultOptions())
		if err != nil {
			t.Fatalf("leiden at share %.2f: %v", share, err)
		}
		sizes := map[int]int{}
		for _, cluster := range result.Partition {
			sizes[cluster]++
		}
		largest := 0
		for _, n := range sizes {
			largest = max(largest, n)
		}
		t.Logf("%s  %11d  %7d  %7.3f", line, result.NumClusters, largest, result.Quality)
	}

	// What the communities actually ARE. "Five clusters" says nothing about whether the
	// partition is meaningful; the subjects do.
	t.Log("--- membership at the default share ---")
	edges, _ := desiredMentionEdges(entities, factRows, 0.20)
	nodes, graph := factGraph(edges)
	if len(nodes) > 0 {
		subject := map[string]string{}
		for _, row := range factRows {
			subject[rowString(row, "fact_key")] = rowString(row, "subject")
		}
		result, err := leiden.Leiden(ctx, len(nodes), graph, leiden.DefaultOptions())
		if err != nil {
			t.Fatalf("leiden: %v", err)
		}
		members := map[int][]string{}
		for index, cluster := range result.Partition {
			members[cluster] = append(members[cluster], subject[nodes[index]])
		}
		clusters := make([]int, 0, len(members))
		for cluster := range members {
			clusters = append(clusters, cluster)
		}
		sort.Slice(clusters, func(i, j int) bool { return len(members[clusters[i]]) > len(members[clusters[j]]) })
		for _, cluster := range clusters {
			sort.Strings(members[cluster])
			t.Logf("  community %d (%d facts): %v", cluster, len(members[cluster]), members[cluster])
		}
	}

	// The floor question, stated as the comparison it is: what a small corpus would get if
	// the cap could not fall below a handful.
	// Resolution, before anything else. Neo4j GDS defaults gamma to 1.0 and states plainly
	// that higher resolutions give more communities; graphify passes 1.0 too. go-leiden's
	// DefaultOptions ships 0.05 — twenty times lower — so every number above was measured
	// under a setting that asks for FEW LARGE communities by construction.
	t.Log("--- resolution sweep at share 0.20 (go-leiden default is 0.05; GDS and graphify use 1.0) ---")
	t.Log("resolution  communities  largest  quality")
	resEdges, _ := desiredMentionEdges(entities, factRows, 0.20)
	resNodes, resGraph := factGraph(resEdges)
	for _, resolution := range []float64{0.05, 0.25, 0.5, 1.0, 2.0} {
		options := leiden.DefaultOptions()
		options.Resolution = resolution
		result, err := leiden.Leiden(ctx, len(resNodes), resGraph, options)
		if err != nil {
			t.Fatalf("leiden at resolution %.2f: %v", resolution, err)
		}
		sizes := map[int]int{}
		for _, cluster := range result.Partition {
			sizes[cluster]++
		}
		largest := 0
		for _, n := range sizes {
			largest = max(largest, n)
		}
		t.Logf("%10.2f  %11d  %7d  %7.3f", resolution, result.NumClusters, largest, result.Quality)
	}

	// Does graphify's hub exclusion recover the structure the default cap flattens?
	// Same graph, same algorithm; the only change is which nodes get to vote on the
	// partition. Percentile 0 is the control: it excludes nothing.
	t.Log("--- graphify hub exclusion at share 0.20 (nodes are facts; hubs are essay statements) ---")
	t.Log("percentile  hubs  communities  largest")
	hubEdges, _ := desiredMentionEdges(entities, factRows, 0.20)
	hubNodes, hubGraph := factGraph(hubEdges)
	for _, percentile := range []float64{0, 99, 95, 90, 80} {
		clusters, largest, hubs, err := excludeHubsAndPartition(ctx, hubNodes, hubGraph, percentile)
		if err != nil {
			t.Fatalf("leiden with %.0f%% hub exclusion: %v", percentile, err)
		}
		t.Logf("%10.0f  %4d  %11d  %7d", percentile, hubs, clusters, largest)
	}

	t.Log("--- with a floor of 3, i.e. cap = max(3, int(facts*share)) ---")
	for _, share := range []float64{0.05, 0.20} {
		floored := float64(max(3, hubCap(len(factRows), share))) / float64(max(len(factRows), 1))
		edges, stats := desiredMentionEdges(entities, factRows, floored)
		nodes, _ := factGraph(edges)
		t.Logf("share %.2f -> cap %d: %d edges over %d linked facts", share, stats.cap, len(edges), len(nodes))
	}
}

// Graphify's answer to the same failure, ported to measure whether it transfers.
//
// graphify/cluster.py excludes high-degree nodes from the PARTITION and reattaches them
// afterwards by majority vote over their neighbours' communities. Its comment names the
// exact symptom measured here: "exclude hub nodes from partitioning so they don't pull
// unrelated subsystems into the same community".
//
// The translation is not the obvious one. Their nodes are code entities, so their hubs are
// high-degree entities — which is what hubCap already drops. Here the nodes are FACTS and
// an edge is a shared entity, so the hub that flattens the partition is a fact that
// MENTIONS many entities: one 300-word statement naming twenty names joins twenty
// otherwise-unrelated facts into a clique. Excluding those from clustering and putting
// them back afterwards is the real analogue.
func excludeHubsAndPartition(
	ctx context.Context, nodes []string, edges []leiden.Edge, percentile float64,
) (clusters int, largest int, hubs int, err error) {
	degree := make([]int, len(nodes))
	neighbours := make([][]int, len(nodes))
	for _, e := range edges {
		degree[e.From]++
		degree[e.To]++
		neighbours[e.From] = append(neighbours[e.From], e.To)
		neighbours[e.To] = append(neighbours[e.To], e.From)
	}
	// Threshold read off the FULL graph's degrees, before anything is removed — the
	// ordering graphify is careful about, since a degree computed after removal is a
	// different statistic.
	sorted := append([]int(nil), degree...)
	sort.Ints(sorted)
	isHub := make([]bool, len(nodes))
	if percentile > 0 && len(sorted) > 0 {
		at := max(0, int(float64(len(sorted))*percentile/100)-1)
		threshold := sorted[at]
		for i, d := range degree {
			if d > threshold {
				isHub[i] = true
				hubs++
			}
		}
	}
	kept := make([]int, 0, len(nodes))
	remap := make(map[int]int, len(nodes))
	for i := range nodes {
		if !isHub[i] && degree[i] > 0 {
			remap[i] = len(kept)
			kept = append(kept, i)
		}
	}
	if len(kept) == 0 {
		return 0, 0, hubs, nil
	}
	sub := make([]leiden.Edge, 0, len(edges))
	for _, e := range edges {
		from, keptFrom := remap[e.From]
		to, keptTo := remap[e.To]
		if keptFrom && keptTo {
			sub = append(sub, leiden.Edge{From: from, To: to, Weight: e.Weight})
		}
	}
	result, err := leiden.Leiden(ctx, len(kept), sub, leiden.DefaultOptions())
	if err != nil {
		return 0, 0, hubs, err
	}
	community := make(map[int]int, len(nodes))
	sizes := map[int]int{}
	for at, cluster := range result.Partition {
		community[kept[at]] = cluster
		sizes[cluster]++
	}
	// Reattach every excluded hub to the community most of its neighbours are in, with
	// graphify's deterministic tie-break: most votes first, then the lowest community id.
	next := result.NumClusters
	for i := range nodes {
		if !isHub[i] {
			continue
		}
		votes := map[int]int{}
		for _, nb := range neighbours[i] {
			if cluster, ok := community[nb]; ok {
				votes[cluster]++
			}
		}
		best, bestVotes := next, 0
		for cluster, n := range votes {
			if n > bestVotes || (n == bestVotes && cluster < best) {
				best, bestVotes = cluster, n
			}
		}
		if bestVotes == 0 {
			next++
		}
		community[i] = best
		sizes[best]++
	}
	for _, n := range sizes {
		largest = max(largest, n)
	}
	return len(sizes), largest, hubs, nil
}
