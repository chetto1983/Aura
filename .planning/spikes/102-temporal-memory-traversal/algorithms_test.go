package temporal_test

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func algorithmFixture(t *testing.T) (*arcadedb.Client, map[string]string) {
	t.Helper()
	client := disposable(t)
	command(t, client, "CREATE VERTEX TYPE Entity", nil)
	command(t, client, "CREATE EDGE TYPE FACT", nil)
	for i, name := range []string{"A", "B", "C", "D", "E", "F", "Tail", "Isolated"} {
		group := 1
		if i >= 3 {
			group = 2
		}
		if i == 7 {
			group = 3
		}
		command(t, client, "CREATE VERTEX Entity SET name=:name, community=:group_id", map[string]any{"name": name, "group_id": group})
	}
	for _, edge := range [][2]string{{"A", "B"}, {"B", "C"}, {"C", "A"}, {"C", "D"}, {"D", "E"}, {"E", "F"}, {"F", "D"}, {"F", "Tail"}} {
		command(t, client, "CREATE EDGE FACT FROM (SELECT FROM Entity WHERE name=:source) TO (SELECT FROM Entity WHERE name=:target) SET weight=1.0", map[string]any{"source": edge[0], "target": edge[1]})
	}
	rows, err := client.Query(t.Context(), "SELECT @rid AS rid, name FROM Entity", nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, row := range rows {
		names[row["rid"].(string)] = row["name"].(string)
	}
	return client, names
}

type algorithmCase struct {
	name, call, fields, prefix string
	minRows, maxRows           int
}

func algorithmCases() []algorithmCase {
	const one = "MATCH (a:Entity {name:'A'}) "
	const pair = "MATCH (a:Entity {name:'A'}), (z:Entity {name:'Tail'}) "
	return []algorithmCase{
		{"graphSummary", "algo.graphSummary('FACT','Entity')", "nodeCount,edgeCount,isolatedNodes", "", 1, 1},
		{"degree", "algo.degree('FACT','BOTH')", "node,degree,inDegree,outDegree", "", 8, 8},
		{"wcc", "algo.wcc('FACT')", "node,componentId", "", 8, 8},
		{"scc", "algo.scc('FACT')", "node,componentId", "", 8, 8},
		{"kcore", "algo.kcore('FACT')", "node,coreNumber", "", 8, 8},
		{"triangleCount", "algo.triangleCount('FACT')", "node,triangles,clusteringCoefficient", "", 8, 8},
		{"articulationPoints", "algo.articulationPoints('FACT')", "node", "", 3, 3},
		{"bridges", "algo.bridges('FACT')", "source,target", "", 2, 2},
		{"biconnectedComponents", "algo.biconnectedComponents('FACT')", "node,componentId", "", 8, 16},
		{"kTruss", "algo.kTruss('FACT',3)", "nodeId,trussNumber", "", 8, 8},
		{"localClusteringCoefficient", "algo.localClusteringCoefficient('FACT')", "node,localClusteringCoefficient", "", 8, 8},
		{"densestSubgraph", "algo.densestSubgraph('FACT')", "node,inDenseSubgraph,density", "", 8, 8},
		{"clique", "algo.clique('FACT',3)", "clique,size", "", 2, 2},
		{"pagerank", "algo.pagerank({maxIterations:100,tolerance:0.000001})", "node,score", "", 8, 8},
		{"personalizedPageRank", "algo.personalizedPageRank(a,'FACT',0.85,100,0.000001)", "nodeId,score", one, 8, 8},
		{"betweenness", "algo.betweenness({normalized:true})", "node,score", "", 8, 8},
		{"harmonic", "algo.harmonic('FACT','BOTH',true)", "node,score", "", 8, 8},
		{"closeness", "algo.closeness('FACT','BOTH',true)", "node,score", "", 8, 8},
		{"hits", "algo.hits('FACT',100,0.000001)", "node,hubScore,authorityScore", "", 8, 8},
		{"voteRank", "algo.voteRank('FACT',3)", "nodeId,rank", "", 3, 3},
		{"louvain", "algo.louvain({maxIterations:20})", "node,communityId,modularity", "", 8, 8},
		{"leiden", "algo.leiden('FACT',20,1.0)", "nodeId,community", "", 8, 8},
		{"labelpropagation", "algo.labelpropagation({maxIterations:20,direction:'BOTH'})", "node,communityId", "", 8, 8},
		{"slpa", "algo.slpa({iterations:20,threshold:0.1,seed:42})", "node,communities", "", 8, 8},
		{"jaccard", "algo.jaccard(a,'FACT','BOTH',0.01)", "node1,node2,similarity", one, 1, 7},
		{"adamicAdar", "algo.adamicAdar(a,'FACT','BOTH',0.01)", "node1,node2,score", one, 1, 7},
		{"commonNeighbors", "algo.commonNeighbors(a,'FACT','BOTH',1)", "node1,node2,commonNeighbors", one, 1, 7},
		{"resourceAllocation", "algo.resourceAllocation(a,'FACT','BOTH',0.01)", "node1,node2,score", one, 1, 7},
		{"knn", "algo.knn(2,'FACT','BOTH')", "node1,node2,similarity", "", 1, 16},
		{"bfs", "algo.bfs(a,'FACT','BOTH',2)", "node,depth", one, 3, 3},
		{"dijkstra", "algo.dijkstra(a,z,'FACT','weight','BOTH')", "path,weight", pair, 1, 1},
		{"kShortestPaths", "algo.kShortestPaths(a,z,3,'FACT','weight')", "path,weight,rank", pair, 1, 3},
		{"steinerTree", "algo.steinerTree([a,b,z],'FACT','weight')", "source,target,weight,totalWeight", "MATCH (a:Entity {name:'A'}), (b:Entity {name:'E'}), (z:Entity {name:'Tail'}) ", 4, 6},
		{"allsimplepaths", "algo.allsimplepaths(a,z,'FACT',4)", "path", pair, 1, 8},
		{"randomWalk", "algo.randomWalk(a,4,'FACT','BOTH',42)", "path,steps", one, 1, 1},
		{"fastrp", "algo.fastrp({dimensions:16,iterations:3,relTypes:'FACT',direction:'BOTH',seed:42})", "node,embedding", "", 8, 8},
		{"graphsage", "algo.graphsage({embeddingDimension:16,layers:2,relTypes:'FACT',direction:'BOTH',seed:42})", "node,embedding", "", 8, 8},
		{"modularityScore", "algo.modularityScore('community','FACT')", "modularity,communities,edgeCount", "", 1, 1},
		{"conductance", "algo.conductance('community','FACT')", "community,conductance,internalEdges,boundaryEdges,nodeCount", "", 3, 3},
	}
}

func TestRelevantAlgorithmCatalog(t *testing.T) {
	client, names := algorithmFixture(t)
	for _, tc := range algorithmCases() {
		t.Run(tc.name, func(t *testing.T) {
			query := tc.prefix + "CALL " + tc.call + " YIELD " + tc.fields + " RETURN " + tc.fields
			start := time.Now()
			rows, err := client.Read(t.Context(), query, nil)
			if err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(start)
			normalized := normalizeResult(rows, names)
			encoded, err := json.Marshal(normalized)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("query=%s duration_ms=%.3f rows=%s", query, float64(elapsed.Microseconds())/1000, encoded)
			if len(rows) < tc.minRows || len(rows) > tc.maxRows {
				t.Fatalf("rows=%d expected %d..%d", len(rows), tc.minRows, tc.maxRows)
			}
			checkAlgorithm(t, tc.name, normalized.([]any))
		})
	}
}

func normalizeResult(value any, names map[string]string) any {
	switch value := value.(type) {
	case []map[string]any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = normalizeResult(item, names)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = normalizeResult(item, names)
		}
		return out
	case map[string]any:
		if rid, ok := value["@rid"].(string); ok && names[rid] != "" {
			return names[rid]
		}
		out := map[string]any{}
		for key, item := range value {
			out[key] = normalizeResult(item, names)
		}
		return out
	case string:
		if name := names[value]; name != "" {
			return name
		}
		return value
	default:
		return value
	}
}

func checkAlgorithm(t *testing.T, name string, rows []any) {
	t.Helper()
	byNode := map[string]map[string]any{}
	for _, raw := range rows {
		row := raw.(map[string]any)
		for _, field := range []string{"node", "nodeId"} {
			if node, ok := row[field].(string); ok {
				byNode[node] = row
			}
		}
	}
	first := rows[0].(map[string]any)
	switch name {
	case "graphSummary":
		if first["nodeCount"] != float64(8) || first["edgeCount"] != float64(8) {
			t.Fatal("wrong fixture totals")
		}
	case "wcc", "scc":
		if byNode["A"]["componentId"] != byNode["C"]["componentId"] || byNode["A"]["componentId"] == byNode["Isolated"]["componentId"] {
			t.Fatal("component membership incorrect")
		}
		if (byNode["A"]["componentId"] == byNode["D"]["componentId"]) != (name == "wcc") {
			t.Fatal("directional connectivity incorrect")
		}
	case "kcore":
		if byNode["A"]["coreNumber"] != float64(2) || byNode["Tail"]["coreNumber"] != float64(1) || byNode["Isolated"]["coreNumber"] != float64(0) {
			t.Fatal("core decomposition incorrect")
		}
	case "articulationPoints":
		for _, node := range []string{"C", "D", "F"} {
			if byNode[node] == nil {
				t.Fatalf("missing connector %s", node)
			}
		}
	case "triangleCount":
		if byNode["A"]["triangles"] != float64(1) || byNode["Tail"]["triangles"] != float64(0) {
			t.Fatal("triangle count incorrect")
		}
	case "bfs":
		if byNode["A"] != nil || byNode["B"]["depth"] != float64(1) || byNode["C"]["depth"] != float64(1) || byNode["D"]["depth"] != float64(2) {
			t.Fatal("BFS radius incorrect")
		}
	case "dijkstra":
		if first["weight"] != float64(4) {
			t.Fatal("undirected shortest route should cost four")
		}
	case "kShortestPaths":
		if first["weight"] != float64(6) {
			t.Fatal("directed shortest route should cost six")
		}
	case "fastrp", "graphsage":
		for _, raw := range rows {
			vector := raw.(map[string]any)["embedding"].([]any)
			if len(vector) != 16 {
				t.Fatal("embedding dimension not honored")
			}
			for _, v := range vector {
				if math.IsNaN(v.(float64)) || math.IsInf(v.(float64), 0) {
					t.Fatal("nonfinite embedding")
				}
			}
		}
	case "louvain", "leiden", "labelpropagation":
		field := "communityId"
		if name == "leiden" {
			field = "community"
		}
		groups := map[string][]string{}
		for node, row := range byNode {
			key := fmt.Sprint(row[field])
			groups[key] = append(groups[key], node)
		}
		for _, group := range groups {
			slices.Sort(group)
		}
		t.Logf("communities=%v", groups)
	}
}
