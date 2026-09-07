package temporal_test

import "testing"

func TestNativeInlineTemporalPredicates(t *testing.T) {
	client := fixture(t)
	for _, tc := range []struct {
		name, predicate, instant string
		edges                    int
	}{
		{"fact_july", "r.valid_from <= localdatetime($at) AND (r.valid_to IS NULL OR r.valid_to > localdatetime($at))", "2026-07-01T00:00:00", 2},
		{"fact_february", "r.valid_from <= localdatetime($at) AND (r.valid_to IS NULL OR r.valid_to > localdatetime($at))", "2026-02-01T00:00:00", 1},
		{"support_july", "EXISTS { MATCH ()-[f:FACT]->() WHERE f.fact_key = r.fact_key AND f.valid_from <= localdatetime($at) AND (f.valid_to IS NULL OR f.valid_to > localdatetime($at)) }", "2026-07-01T00:00:00", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := "MATCH (s:Entity {name:'Start'}), (t:Entity {name:'Target'}), p=shortestPath((s)-[r:FACT|MENTIONS*..4 WHERE " + tc.predicate + "]->(t)) RETURN nodes(p) AS nodes, relationships(p) AS edges"
			rows, err := client.Read(t.Context(), query, map[string]any{"at": tc.instant})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || len(rows[0]["edges"].([]any)) != tc.edges {
				t.Fatalf("unexpected path: %v", rows)
			}
			t.Logf("native single-query predicate returns %d edges", tc.edges)
		})
	}
}

func TestNativeTemporalPathSupportInSameQuery(t *testing.T) {
	client := fixture(t)
	query := `MATCH (s:Entity {name:'Start'}), (t:Entity {name:'Bridge'}),
	p=shortestPath((s)-[r:MENTIONS*..4 WHERE EXISTS { MATCH ()-[f:FACT]->() WHERE f.fact_key=r.fact_key AND f.valid_from<=localdatetime($at) AND (f.valid_to IS NULL OR f.valid_to>localdatetime($at)) }]->(t))
	WITH nodes(p) AS nodes, relationships(p) AS edges
	MATCH (a:Entity)-[f:FACT]->(b:Entity) WHERE f.fact_key IN [r IN edges | r.fact_key]
	RETURN nodes,edges,collect({fact_key:f.fact_key,statement:f.statement,predicate:f.predicate,sources:f.sources,valid_from:f.valid_from,valid_to:f.valid_to,subject:a.name,object:b.name}) AS supports`
	rows, err := client.Read(t.Context(), query, map[string]any{"at": "2026-07-01T00:00:00"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%v", rows)
	}
	supports := rows[0]["supports"].([]any)
	if len(supports) != 1 || supports[0].(map[string]any)["fact_key"] != "live1" {
		t.Fatalf("support=%v", supports)
	}
	t.Logf("one native query returns path and original supporting fact: %v", supports)
}

func TestNativeTemporalNeighborhoodPreservesDirectEvidence(t *testing.T) {
	client := fixture(t)
	query := `MATCH (s:Entity {name:$entity}), p=(s)-[m:MENTIONS*0..2 WHERE EXISTS { MATCH ()-[support:FACT]->() WHERE support.fact_key=m.fact_key AND support.valid_from<=localdatetime($at) AND (support.valid_to IS NULL OR support.valid_to>localdatetime($at)) }]-(near:Entity)
	WITH s,collect(DISTINCT near) AS nearby
	MATCH (a:Entity)-[f:FACT]->(b:Entity) WHERE (a IN nearby OR b IN nearby) AND f.valid_from<=localdatetime($at) AND (f.valid_to IS NULL OR f.valid_to>localdatetime($at))
	RETURN f.fact_key AS fact_key,f.statement AS statement
	ORDER BY CASE WHEN a=s OR b=s THEN 0 ELSE 1 END, f.created_at DESC,f.fact_key ASC LIMIT $cap`
	for _, tc := range []struct {
		entity, at string
		cap, want  int
		first      string
	}{
		{"Start", "2026-07-01T00:00:00", 1, 1, "live1"},
		{"Start", "2026-02-01T00:00:00", 20, 1, "expired"},
		{"Bridge", "2026-02-01T00:00:00", 20, 0, ""},
		{"Bridge", "2026-07-01T00:00:00", 20, 3, "gap2"},
	} {
		rows, err := client.Read(t.Context(), query, map[string]any{"entity": tc.entity, "at": tc.at, "cap": tc.cap})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != tc.want || (tc.want > 0 && rows[0]["fact_key"] != tc.first) {
			t.Fatalf("case=%+v rows=%v", tc, rows)
		}
		t.Logf("entity=%s at=%s cap=%d rows=%v", tc.entity, tc.at, tc.cap, rows)
	}
}

func TestNativeMentionIdentityIncludesSupportingFact(t *testing.T) {
	client := fixture(t)
	command(t, client, "CREATE PROPERTY MENTIONS.fact_key IF NOT EXISTS STRING", nil)
	command(t, client, "CREATE INDEX IF NOT EXISTS ON MENTIONS (`@out`,`@in`,fact_key) UNIQUE", nil)
	query := "CREATE EDGE MENTIONS FROM (SELECT FROM Entity WHERE name='Start') TO (SELECT FROM Entity WHERE name='Bridge') SET fact_key=:key"
	command(t, client, query, map[string]any{"key": "expired"})
	if _, err := client.Command(t.Context(), query, map[string]any{"key": "expired"}); err == nil {
		t.Fatal("duplicate triple bypassed the unique index")
	} else {
		t.Logf("native duplicate signal: %v", err)
	}
	rows, err := client.Query(t.Context(), "SELECT fact_key FROM MENTIONS WHERE outV().name='Start' AND inV().name='Bridge' ORDER BY fact_key", nil)
	if err != nil || len(rows) != 2 {
		t.Fatalf("different supporting facts did not coexist: %v %v", rows, err)
	}
	t.Logf("same endpoints preserve both supporting facts: %v; duplicate triple rejected", rows)
}

func TestNativeMentionRecordLinkRetainsClosedSupport(t *testing.T) {
	client := fixture(t)
	command(t, client, "CREATE PROPERTY MENTIONS.fact_rid LINK", nil)
	rows, err := client.Query(t.Context(), "SELECT @rid AS rid FROM FACT WHERE fact_key='expired'", nil)
	if err != nil {
		t.Fatal(err)
	}
	command(t, client, "UPDATE MENTIONS SET fact_rid=:rid WHERE fact_key='expired'", map[string]any{"rid": rows[0]["rid"]})
	command(t, client, "UPDATE FACT SET fact_key=NULL WHERE fact_key='expired'", nil)
	query := `MATCH (s:Entity {name:'Start'}),(t:Entity {name:'Target'}),p=shortestPath((s)-[m:MENTIONS*..2 WHERE m.fact_rid.valid_from<=localdatetime($at) AND (m.fact_rid.valid_to IS NULL OR m.fact_rid.valid_to>localdatetime($at))]->(t)) RETURN relationships(p) AS edges`
	rows, err = client.Read(t.Context(), query, map[string]any{"at": "2026-02-01T00:00:00"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("native link lookup rows=%v err=%v", rows, err)
	}
	t.Logf("historical support remains reachable by native LINK with no active key: %v", rows)
	query = `MATCH (s:Entity {name:'Start'}),(t:Entity {name:'Target'}),p=shortestPath((s)-[m:MENTIONS*..2 WHERE EXISTS { MATCH ()-[f:FACT]->() WHERE elementId(f)=toString(m.fact_rid) AND f.valid_from<=localdatetime($at) AND (f.valid_to IS NULL OR f.valid_to>localdatetime($at)) }]->(t)) WITH nodes(p) AS nodes,relationships(p) AS edges MATCH (a:Entity)-[f:FACT]->(b:Entity) WHERE elementId(f) IN [m IN edges | toString(m.fact_rid)] RETURN nodes,edges,collect({rid:elementId(f),fact_key:f.fact_key,statement:f.statement,subject:a.name,object:b.name}) AS supports`
	rows, err = client.Read(t.Context(), query, map[string]any{"at": "2026-02-01T00:00:00"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("LINK evidence join rows=%v err=%v", rows, err)
	}
	t.Logf("same-query historical evidence by record identity: %v", rows[0]["supports"])
	command(t, client, "DELETE FROM FACT WHERE statement='Start links Target'", nil)
	rows, err = client.Read(t.Context(), query, map[string]any{"at": "2026-02-01T00:00:00"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("dangling native LINK was not excluded: %v %v", rows, err)
	}
}
