package temporal_test

import (
	"cmp"
	"slices"
	"testing"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func TestNativeRankingScope(t *testing.T) {
	client := fixture(t)
	for _, source := range []string{"Start", "Target"} {
		scores := ranking(t, client, source)
		t.Logf("source=%s scores=%v", source, scores)
		if source == "Start" && (scores["Future"] <= 0 || scores["Orphan"] <= 0) {
			t.Fatal("unfiltered PPR unexpectedly excluded future or unsupported topology")
		}
		if source == "Target" && (scores["Target"] != 1 || scores["Middle"] != 0 || scores["Start"] != 0) {
			t.Fatal("PPR sink behavior changed; reassess direction support")
		}
	}
}

func TestMentionHistoryIsNotRetained(t *testing.T) {
	client := fixture(t)
	before, err := client.Query(t.Context(), "SELECT count(*) AS n FROM MENTIONS WHERE fact_key='expired'", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.LinkMentions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	after, err := client.Query(t.Context(), "SELECT count(*) AS n FROM MENTIONS WHERE fact_key='expired'", nil)
	if err != nil {
		t.Fatal(err)
	}
	history := keysAt(t, client, "2026-02-01T00:00:00Z")
	t.Logf("sweep=%+v expired mention count before=%v after=%v historical fact keys=%v", result, before[0]["n"], after[0]["n"], history)
	if before[0]["n"] != float64(1) || after[0]["n"] != float64(0) || len(history) != 1 || history[0] != "expired" {
		t.Fatal("historical mention retention changed; reassess projection contract")
	}
}

func TestBoundedExpansionComparedWithNativeRank(t *testing.T) {
	client := fixture(t)
	// Two supporting facts answer the synthetic question: how does Start reach
	// Target using facts valid in July? Gap is a valid but irrelevant distractor.
	command(t, client, "UPDATE FACT SET statement='Start reaches Middle' WHERE fact_key='live1'", nil)
	command(t, client, "UPDATE FACT SET statement='Middle reaches Target' WHERE fact_key='live2'", nil)
	keys := keysAt(t, client, "2026-07-01T00:00:00Z")
	rows, err := client.Read(t.Context(), "MATCH (s:Entity {name:'Target'}), p=(s)-[r:FACT*1..2 WHERE r.fact_key IN $keys]-(n:Entity) RETURN [r IN relationships(p) | r.fact_key] AS keys", map[string]any{"keys": keys})
	if err != nil {
		t.Fatal(err)
	}
	expanded := map[string]int{}
	for _, row := range rows {
		for i, key := range row["keys"].([]any) {
			name := key.(string)
			if expanded[name] == 0 || i+1 < expanded[name] {
				expanded[name] = i + 1
			}
		}
	}
	if expanded["live1"] != 2 || expanded["live2"] != 1 || expanded["expired"] != 0 {
		t.Fatalf("bounded expansion keys=%v", expanded)
	}
	// Score candidate facts by the mean of endpoint PPR. This is an explicit
	// experimental rule, not an existing Aura retrieval contract.
	ranks := ranking(t, client, "Target")
	facts, err := client.Query(t.Context(), "SELECT fact_key, statement, outV().name AS subject, inV().name AS object FROM FACT WHERE fact_key IN :keys ORDER BY fact_key", map[string]any{"keys": keys})
	if err != nil {
		t.Fatal(err)
	}
	candidates := []candidate{}
	for _, fact := range facts {
		key := fact["fact_key"].(string)
		score := (ranks[fact["subject"].(string)] + ranks[fact["object"].(string)]) / 2
		if expanded[key] != 0 {
			candidates = append(candidates, candidate{key, fact["statement"].(string), expanded[key], score})
		}
		t.Logf("candidate=%s depth=%d endpoint_mean_ppr=%g statement=%s", key, expanded[key], score, fact["statement"])
	}
	for _, method := range []string{"bounded", "native_ppr"} {
		ordered := slices.Clone(candidates)
		slices.SortFunc(ordered, func(a, b candidate) int {
			order := cmp.Compare(a.depth, b.depth)
			if method == "native_ppr" {
				order = cmp.Compare(b.score, a.score)
			}
			if order == 0 {
				order = cmp.Compare(a.key, b.key)
			}
			return order
		})
		for _, budget := range []int{32, 64} {
			used, hits := 0, 0
			selected := []string{}
			for _, fact := range ordered {
				cost := utf8.RuneCountInString(fact.statement)
				if len(selected) > 0 {
					cost++
				}
				if used+cost > budget {
					break
				}
				used += cost
				selected = append(selected, fact.key)
				if fact.key == "live1" || fact.key == "live2" {
					hits++
				}
			}
			t.Logf("method=%s budget=%d used=%d selected=%v relevant=%d/2", method, budget, used, selected, hits)
			want := 2
			if budget == 32 {
				want = 0
			}
			if hits != want {
				t.Fatalf("retrieval changed: got %d want %d", hits, want)
			}
		}
	}
}

type candidate struct {
	key, statement string
	depth          int
	score          float64
}

func ranking(t *testing.T, client *arcadedb.Client, source string) map[string]float64 {
	t.Helper()
	rows, err := client.Query(t.Context(), "SELECT @rid AS rid, name FROM Entity", nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, row := range rows {
		names[row["rid"].(string)] = row["name"].(string)
	}
	rows, err = client.Read(t.Context(), "MATCH (s:Entity {name:$source}) CALL algo.personalizedPageRank(s, 'FACT,MENTIONS', 0.85, 100, 0.000001) YIELD nodeId, score RETURN nodeId, score ORDER BY score DESC", map[string]any{"source": source})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, row := range rows {
		out[names[row["nodeId"].(string)]] = row["score"].(float64)
	}
	if len(out) == 0 {
		t.Fatal("PPR entity join returned no rows")
	}
	return out
}
