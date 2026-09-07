package temporal_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/joho/godotenv"
)

func disposable(t *testing.T) *arcadedb.Client {
	t.Helper()
	config := arcadedb.Config{BaseURL: os.Getenv("ARCADEDB_URL"), User: "root", Password: os.Getenv("ARCADEDB_PASSWORD"), Database: "unused", Timeout: 15 * time.Second}
	if config.BaseURL == "" || config.Password == "" {
		t.Fatal("ARCADEDB_URL and ARCADEDB_PASSWORD are required; this probe must run live")
	}
	admin, err := arcadedb.New(config)
	if err != nil {
		t.Fatal(err)
	}
	database := fmt.Sprintf("aura_spike102_%d", time.Now().UnixNano())
	if _, err := admin.CreateDatabase(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.DropDatabase(ctx, database); err != nil {
			t.Errorf("drop disposable database: %v", err)
		}
	})
	config.Database = database
	client, err := arcadedb.New(config)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func fixture(t *testing.T) *arcadedb.Client {
	t.Helper()
	client := disposable(t)
	for _, sql := range []string{
		"CREATE VERTEX TYPE Entity", "CREATE VERTEX TYPE Object EXTENDS Entity",
		"CREATE EDGE TYPE FACT", "CREATE EDGE TYPE MENTIONS",
		"CREATE PROPERTY FACT.valid_from DATETIME", "CREATE PROPERTY FACT.valid_to DATETIME",
	} {
		command(t, client, sql, nil)
	}
	for _, name := range []string{"Start", "Middle", "Future", "Gap", "Orphan", "Bridge", "Unknown"} {
		command(t, client, "CREATE VERTEX Entity SET name=:name", map[string]any{"name": name})
	}
	command(t, client, "CREATE VERTEX Object SET name='Target'", nil)
	for _, edge := range []struct{ key, from, to, start, end string }{
		{"expired", "Start", "Target", "2026-01-01 00:00:00", "2026-06-01 00:00:00"},
		{"live1", "Start", "Middle", "2026-06-01 00:00:00", ""},
		{"live2", "Middle", "Target", "2026-06-01 00:00:00", ""},
		{"future", "Start", "Future", "2026-10-01 00:00:00", ""},
		{"gap1", "Start", "Gap", "2026-03-01 00:00:00", "2026-05-01 00:00:00"},
		{"gap2", "Gap", "Target", "2026-06-01 00:00:00", ""},
		{"unknown", "Start", "Unknown", "", ""},
	} {
		var from, to any
		if edge.start != "" {
			from = edge.start
		}
		if edge.end != "" {
			to = edge.end
		}
		command(t, client, "CREATE EDGE FACT FROM (SELECT FROM Entity WHERE name=:source) TO (SELECT FROM Entity WHERE name=:target) SET fact_key=:key, valid_from=:vf, valid_to=:vt, statement=:statement, predicate='links', sources=:sources",
			map[string]any{"source": edge.from, "target": edge.to, "key": edge.key, "vf": from, "vt": to, "statement": edge.from + " links " + edge.to,
				"sources": []map[string]any{{"run_id": "spike102", "memory_ids": []string{"synthetic"}}}})
	}
	for _, edge := range [][3]string{{"Start", "Target", "expired"}, {"Start", "Bridge", "live1"}, {"Bridge", "Target", "live2"}, {"Start", "Orphan", "absent"}, {"Start", "Future", "future"}} {
		command(t, client, "CREATE EDGE MENTIONS FROM (SELECT FROM Entity WHERE name=:source) TO (SELECT FROM Entity WHERE name=:target) SET fact_key=:key", map[string]any{"source": edge[0], "target": edge[1], "key": edge[2]})
	}
	return client
}

func command(t *testing.T, client *arcadedb.Client, sql string, params map[string]any) {
	t.Helper()
	if _, err := client.Command(t.Context(), sql, params); err != nil {
		t.Fatal(err)
	}
}

func keysAt(t *testing.T, client *arcadedb.Client, instant string) []string {
	t.Helper()
	rows, err := client.Query(t.Context(), "SELECT fact_key FROM FACT WHERE valid_from <= :as_of AND (valid_to IS NULL OR valid_to > :as_of) ORDER BY fact_key LIMIT 10001", map[string]any{"as_of": instant})
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, row := range rows {
		keys = append(keys, row["fact_key"].(string))
	}
	return keys
}

func TestNativeTemporalPaths(t *testing.T) {
	client := fixture(t)
	const current = "2026-07-01T00:00:00Z"
	for _, tc := range []struct {
		name, source, target, relations, direction, instant string
		depth                                               int
		want                                                []string
	}{
		{"longer_valid_route", "Start", "Target", "FACT", "OUT", current, 4, []string{"Start", "Middle", "Target"}},
		{"historical_shortcut", "Start", "Target", "FACT", "OUT", "2026-02-01T00:00:00Z", 4, []string{"Start", "Target"}},
		{"exclusive_end_inclusive_start", "Start", "Target", "FACT", "OUT", "2026-06-01T00:00:00Z", 4, []string{"Start", "Middle", "Target"}},
		{"depth_prunes", "Start", "Target", "FACT", "OUT", current, 1, nil},
		{"future_excluded", "Start", "Future", "FACT|MENTIONS", "OUT", current, 4, nil},
		{"future_at_start", "Start", "Future", "FACT", "OUT", "2026-10-01T00:00:00Z", 4, []string{"Start", "Future"}},
		{"null_start_excluded", "Start", "Unknown", "FACT", "OUT", current, 4, nil},
		{"empty_eligible_set", "Start", "Target", "FACT|MENTIONS", "OUT", "2025-01-01T00:00:00Z", 4, nil},
		{"missing_support_excluded", "Start", "Orphan", "FACT|MENTIONS", "OUT", current, 4, nil},
		{"mention_support_filtered", "Start", "Target", "MENTIONS", "OUT", current, 4, []string{"Start", "Bridge", "Target"}},
		{"mixed_types", "Start", "Bridge", "FACT|MENTIONS", "OUT", current, 4, []string{"Start", "Bridge"}},
		{"reverse_traversal", "Target", "Start", "FACT", "IN", current, 4, []string{"Target", "Middle", "Start"}},
		{"wrong_direction", "Target", "Start", "FACT", "OUT", current, 4, nil},
		{"both_directions", "Target", "Start", "FACT", "BOTH", current, 4, []string{"Target", "Middle", "Start"}},
		{"missing_endpoint", "Absent", "Target", "FACT", "OUT", current, 4, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := keysAt(t, client, tc.instant)
			left, right := "-", "->"
			if tc.direction == "IN" {
				left, right = "<-", "-"
			}
			if tc.direction == "BOTH" {
				right = "-"
			}
			// Inline WHERE is enforced while expanding, upstream issue #5481.
			query := fmt.Sprintf("MATCH (s:Entity {name:$source}), (t:Entity {name:$target}), p=shortestPath((s)%s[r:%s*..%d WHERE r.fact_key IN $keys]%s(t)) RETURN [n IN nodes(p) | n.name] AS names, [r IN relationships(p) | r.fact_key] AS keys", left, tc.relations, tc.depth, right)
			start := time.Now()
			rows, err := client.Read(t.Context(), query, map[string]any{"source": tc.source, "target": tc.target, "keys": keys})
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			if len(rows) > 0 {
				for _, value := range rows[0]["names"].([]any) {
					names = append(names, value.(string))
				}
				for _, key := range rows[0]["keys"].([]any) {
					if !slices.Contains(keys, key.(string)) {
						t.Fatalf("ineligible path edge: %v", key)
					}
				}
			}
			t.Logf("eligible=%v path=%v duration=%s", keys, names, time.Since(start))
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("path=%v want=%v", names, tc.want)
			}
		})
	}
	keys := keysAt(t, client, current)
	query := "MATCH (s:Entity {name:'Start'}), (t:Entity {name:'Target'}), p=shortestPath((s)-[:FACT*..4]->(t)) WHERE all(r IN relationships(p) WHERE r.fact_key IN $keys) RETURN p"
	rows, err := client.Read(t.Context(), query, map[string]any{"keys": keys})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("post_filter_control rows=%d (valid two-hop route exists)", len(rows))
	if len(rows) != 0 {
		t.Fatalf("post-filter unexpectedly found route: %v", rows)
	}
}

func TestDisjointWindowsAndDeletedSupport(t *testing.T) {
	client := fixture(t)
	query := "MATCH (s:Entity {name:'Start'}), (t:Entity {name:'Target'}), p=shortestPath((s)-[r:FACT*..4 WHERE r.fact_key IN $keys]->(t)) RETURN p"
	for _, instant := range []string{"2026-04-01T00:00:00Z", "2026-07-01T00:00:00Z"} {
		keys := keysAt(t, client, instant)
		keys = slices.DeleteFunc(keys, func(key string) bool { return key != "gap1" && key != "gap2" })
		rows, err := client.Read(t.Context(), query, map[string]any{"keys": keys})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Fatalf("disjoint validity windows composed at %s: %v", instant, rows)
		}
		t.Logf("disjoint windows at %s: eligible=%v no path", instant, keys)
	}
	keys := keysAt(t, client, "2026-07-01T00:00:00Z")
	command(t, client, "DELETE FROM FACT WHERE fact_key='live1'", nil)
	query = "MATCH (s:Entity {name:'Start'}), (t:Entity {name:'Bridge'}), p=shortestPath((s)-[r:MENTIONS*..4 WHERE r.fact_key IN $keys]->(t)) RETURN p"
	stale, err := client.Read(t.Context(), query, map[string]any{"keys": keys})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := client.Read(t.Context(), query, map[string]any{"keys": keysAt(t, client, "2026-07-01T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("support deleted between reads: stale keyset paths=%d refreshed keyset paths=%d", len(stale), len(fresh))
	if len(stale) != 1 || len(fresh) != 0 {
		t.Fatal("support race behavior changed; reassess snapshot contract")
	}
}

func TestMain(m *testing.M) {
	// Reuse the project's dotenv parser; never print credentials or persist them.
	if err := godotenv.Load("../../../.env"); err != nil && !os.IsNotExist(err) {
		panic(err)
	}
	os.Exit(m.Run())
}
