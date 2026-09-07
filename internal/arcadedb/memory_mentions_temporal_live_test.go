//go:build arcadedb_integration

package arcadedb

import (
	"sync"
	"testing"
	"time"
)

func TestHistoricalMentionSurvivesSweep(t *testing.T) {
	client := temporalGraphFixture(t)
	withUncappedShare(client)
	for _, sql := range []string{
		"UPDATE FACT SET statement='Origin used Bridge with Destination' WHERE fact_key='past'",
		"UPDATE FACT SET statement='Origin uses Bridge with Middle' WHERE fact_key='first'",
	} {
		if _, err := client.Command(t.Context(), sql, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Reuse the existing linker over all retained facts to reconstruct the links
	// the former current-only sweep discarded. This seeds a historical oracle.
	facts, err := client.Query(t.Context(), "SELECT @rid AS fact_rid,fact_key,statement,outV().name AS subject,inV().name AS object FROM FACT", nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := client.Query(t.Context(), "SELECT name FROM Entity", nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, node := range nodes {
		names = append(names, node["name"].(string))
	}
	desired, _ := desiredMentionEdges(names, facts, 1)
	for _, edge := range sortedMentionEdges(desired) {
		if err := client.commandMentionEdge(t.Context(), mentionCreateStatement, edge); err != nil {
			t.Fatal(err)
		}
	}
	request := MemoryGraphPathRequest{MemoryGraphRequest: MemoryGraphRequest{Relations: "mentions", AsOf: "2026-02-01T00:00:00Z"}, Source: "Origin", Target: "Bridge", MaxDepth: 1}
	before, err := client.MemoryGraphPath(t.Context(), request)
	if err != nil || !before.Found {
		t.Fatalf("reconstructed historical path=%+v err=%v", before, err)
	}
	result, err := client.LinkMentions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	after, err := client.MemoryGraphPath(t.Context(), request)
	if err != nil || !after.Found || after.Edges[0].FactKey != "past" {
		t.Fatalf("historical path lost after sweep %+v: path=%+v err=%v", result, after, err)
	}
	request.AsOf = "2026-07-01T00:00:00Z"
	current, err := client.MemoryGraphPath(t.Context(), request)
	if err != nil || !current.Found || current.Edges[0].FactKey != "first" {
		t.Fatalf("current path=%+v err=%v", current, err)
	}
	if replay, err := client.LinkMentions(t.Context()); err != nil || replay.Created != 0 || replay.Removed != 0 {
		t.Fatalf("sweep replay=%+v err=%v", replay, err)
	}
}

func TestConcurrentMentionSupportsRemainDistinct(t *testing.T) {
	client := temporalGraphFixture(t)
	if _, err := client.Command(t.Context(), "DELETE FROM MENTIONS WHERE outV().name='Origin' AND inV().name='Bridge'", nil); err != nil {
		t.Fatal(err)
	}
	facts, err := client.Query(t.Context(), "SELECT @rid AS rid,fact_key FROM FACT WHERE fact_key IN ['first','past']", nil)
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{}
	for _, fact := range facts {
		refs[rowString(fact, "fact_key")] = rowString(fact, "rid")
	}
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			key := "past"
			if i%2 == 0 {
				key = "first"
			}
			errors <- client.commandMentionEdge(t.Context(), mentionCreateStatement, mentionEdge{Source: "Origin", Target: "Bridge", FactRID: refs[key]})
		}(i)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, err := client.Query(t.Context(), "SELECT fact_rid FROM MENTIONS WHERE outV().name='Origin' AND inV().name='Bridge'", nil)
	if err != nil || len(rows) != 2 || rowString(rows[0], "fact_rid") == rowString(rows[1], "fact_rid") {
		t.Fatalf("concurrent supports=%v err=%v", rows, err)
	}
}

func TestMentionRecordSurvivesActualSupersession(t *testing.T) {
	client := disposableMemoryClient(t)
	withUncappedShare(client)
	if _, err := client.Command(t.Context(), "CREATE VERTEX Entity SET name='ChronologyBridge'", nil); err != nil {
		t.Fatal(err)
	}
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := Fact{Subject: "ChronologyOrigin", Predicate: "uses", Object: "OldTeam", Statement: "ChronologyOrigin uses ChronologyBridge with OldTeam", ValidFrom: jan, Source: FactSource{RunID: "chronology", WriterRole: "parent"}}
	if _, err := client.UpsertFact(t.Context(), first, jan); err != nil {
		t.Fatal(err)
	}
	if _, err := client.LinkMentions(t.Context()); err != nil {
		t.Fatal(err)
	}
	request := MemoryGraphPathRequest{MemoryGraphRequest: MemoryGraphRequest{Relations: "mentions", AsOf: "2026-03-01T00:00:00Z"}, Source: first.Subject, Target: "ChronologyBridge", MaxDepth: 1}
	before, err := client.MemoryGraphPath(t.Context(), request)
	if err != nil || !before.Found {
		t.Fatalf("before=%+v err=%v", before, err)
	}
	second := first
	second.Object = "NewTeam"
	second.Statement = "ChronologyOrigin uses ChronologyBridge with NewTeam"
	second.ValidFrom = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	second.Supersedes = true
	if _, err := client.UpsertFact(t.Context(), second, second.ValidFrom); err != nil {
		t.Fatal(err)
	}
	after, err := client.MemoryGraphPath(t.Context(), request)
	if err != nil || !after.Found || after.Edges[0].SupportingFact.FactKey != "" || after.Edges[0].SupportingFact.RID != before.Edges[0].SupportingFact.RID {
		t.Fatalf("closed support=%+v err=%v", after, err)
	}
	if _, err := client.LinkMentions(t.Context()); err != nil {
		t.Fatal(err)
	}
	request.AsOf = "2026-07-01T00:00:00Z"
	current, err := client.MemoryGraphPath(t.Context(), request)
	if err != nil || !current.Found || current.Edges[0].SupportingFact.Object != "NewTeam" {
		t.Fatalf("current support=%+v err=%v", current, err)
	}
}

func TestMemoryTemporalNeighborhoodRejectsMissingSupportAndKeepsDirectFirst(t *testing.T) {
	client := temporalGraphFixture(t)
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		entity      string
		limit, want int
		key         string
	}{
		{"Orphan", 20, 0, ""},
		{"Origin", 1, 1, "first"},
	} {
		t.Run(tc.entity, func(t *testing.T) {
			facts, err := client.FactsAbout(t.Context(), tc.entity, "", tc.limit, at, FactsAboutNeighbourhood)
			if err != nil {
				t.Fatal(err)
			}
			if len(facts) != tc.want || (tc.want > 0 && facts[0].FactKey != tc.key) {
				t.Fatalf("facts=%+v", facts)
			}
		})
	}
}

func TestIncompleteMentionSweepDoesNotMutate(t *testing.T) {
	client := temporalGraphFixture(t)
	client.limits.DigestScan = 2
	before, err := client.Query(t.Context(), "SELECT count(*) AS n FROM MENTIONS", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.LinkMentions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	after, err := client.Query(t.Context(), "SELECT count(*) AS n FROM MENTIONS", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("incomplete sweep=%+v edges before=%v after=%v", result, before[0]["n"], after[0]["n"])
	if result.Covered || result.Created != 0 || result.Removed != 0 || before[0]["n"] != after[0]["n"] {
		t.Fatal("partial inventory mutated the graph")
	}
}
