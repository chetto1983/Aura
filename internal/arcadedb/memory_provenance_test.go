package arcadedb

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFactIdentityIsTrimmedAndProvenanceIndependent(t *testing.T) {
	base := validFact()
	want := factIdentity(base)
	const golden = "2eb33bff495f33ee2b6b3b5ed1f3fd9b60b14e4999e1056da04536e29747f83c"
	if want != golden {
		t.Fatalf("fact identity = %s, want stable golden %s", want, golden)
	}
	variant := base
	variant.Subject = "  " + base.Subject + "\t"
	variant.Statement = "\n" + base.Statement + " "
	variant.Source = FactSource{RunID: "another", MemoryIDs: []string{"z"}}
	variant.ValidFrom = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := factIdentity(variant); got != want {
		t.Fatalf("replay identity changed with whitespace/provenance/time: %s != %s", got, want)
	}
	variant.Object = "Torino"
	if got := factIdentity(variant); got == want {
		t.Fatal("different tuple reused the same fact identity")
	}
	if len(want) != 64 {
		t.Fatalf("identity = %q, want lowercase SHA-256", want)
	}
}

func TestActiveFactKeyAndRowsKeepOnlyCurrentIdentity(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if got := activeFactKey("key", time.Time{}, now); got != "key" {
		t.Fatalf("open key = %v", got)
	}
	if got := activeFactKey("key", now.Add(time.Hour), now); got != "key" {
		t.Fatalf("future valid_to key = %v", got)
	}
	if got := activeFactKey("key", now, now); got != nil {
		t.Fatalf("expired key = %v, want nil", got)
	}
	for name, row := range map[string]map[string]any{
		"open":   {"rid": "#1:0", "valid_to": nil, "expired_at": nil},
		"future": {"rid": "#1:1", "valid_to": now.Add(time.Hour).Format(time.RFC3339Nano), "expired_at": nil},
		"closed": {"rid": "#1:2", "valid_to": now.Format(time.RFC3339Nano), "expired_at": now.Format(time.RFC3339Nano)},
	} {
		active, err := activeFactRow(row, now)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if active != (name != "closed") {
			t.Fatalf("%s active = %v", name, active)
		}
	}
	if _, err := activeFactRow(map[string]any{"rid": "#1:9", "valid_to": "not-a-time"}, now); err == nil {
		t.Fatal("invalid valid_to accepted during migration")
	}
}

func TestMergeFactSourcesKeepsOneCanonicalEntryPerRun(t *testing.T) {
	got := mergeFactSources(
		[]FactSource{{RunID: " run-b ", MemoryIDs: []string{"2", "1", "1"}}},
		FactSource{RunID: "run-a", MemoryIDs: []string{"z"}},
		FactSource{RunID: "run-b", MemoryIDs: []string{"3", ""}},
	)
	want := []FactSource{
		{RunID: "run-a", MemoryIDs: []string{"z"}},
		{RunID: "run-b", MemoryIDs: []string{"1", "2", "3"}},
	}
	if !slices.EqualFunc(got, want, func(a, b FactSource) bool {
		return a.RunID == b.RunID && slices.Equal(a.MemoryIDs, b.MemoryIDs)
	}) {
		t.Fatalf("sources = %+v, want %+v", got, want)
	}
}

func TestUpsertFactReplaysAndAttachesSourcesWithoutDuplicateEdges(t *testing.T) {
	var stored []FactSource
	creates := 0
	updates := 0
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		switch {
		case strings.Contains(statement, "WHERE fact_key = :fact_key"):
			if creates == 0 {
				return testResponse{Body: `{"result":[]}`}
			}
			body, _ := json.Marshal(map[string]any{"result": []any{
				map[string]any{"rid": "#1:0", "sources": sourcesParam(stored)},
			}})
			return testResponse{Body: string(body)}
		case strings.HasPrefix(statement, "CREATE EDGE FACT"):
			creates++
			stored = factSources(params["sources"])
			return testResponse{Body: `{"result":[{"count":1}]}`}
		case strings.HasPrefix(statement, "UPDATE FACT SET sources"):
			updates++
			stored = factSources(params["sources"])
			return testResponse{Body: `{"result":[{"count":1}]}`}
		default:
			return testResponse{Body: `{"result":[]}`}
		}
	})

	fact := validFact()
	fact.SubjectKind = " Person "
	fact.ObjectKind = " City "
	fact.Source.MemoryIDs = []string{"m2", "m1", "m1"}
	if _, err := client.UpsertFact(context.Background(), fact, now); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if _, err := client.UpsertFact(context.Background(), fact, now.Add(time.Second)); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	fact.Source = FactSource{RunID: "run-2", MemoryIDs: []string{"m9"}}
	if _, err := client.UpsertFact(context.Background(), fact, now.Add(2*time.Second)); err != nil {
		t.Fatalf("second source: %v", err)
	}
	if creates != 1 || updates != 1 || len(stored) != 2 {
		t.Fatalf("creates=%d updates=%d sources=%+v", creates, updates, stored)
	}
	all := make([]string, 0, len(*requests))
	for _, request := range *requests {
		all = append(all, request.Payload["command"].(string))
	}
	joined := strings.Join(all, "\n")
	if !strings.Contains(joined, "kind = :kind UPSERT") {
		t.Fatalf("entity kinds were not persisted:\n%s", joined)
	}
}

func TestUpsertFactRecoversAConcurrentExactCreate(t *testing.T) {
	createdByPeer := false
	var stored []FactSource
	client, _ := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		switch {
		case strings.HasPrefix(statement, "CREATE EDGE FACT"):
			createdByPeer = true
			stored = []FactSource{{RunID: "peer-run", MemoryIDs: []string{"peer-message"}}}
			return testResponse{Status: http.StatusConflict, Body: `{"detail":"duplicate fact_key"}`}
		case strings.HasPrefix(statement, "SELECT @rid AS rid"):
			if !createdByPeer {
				return testResponse{Body: `{"result":[]}`}
			}
			body, _ := json.Marshal(map[string]any{"result": []any{map[string]any{
				"rid": "#1:0", "sources": sourcesParam(stored),
			}}})
			return testResponse{Body: string(body)}
		case strings.HasPrefix(statement, "UPDATE FACT SET sources"):
			stored = factSources(params["sources"])
			return testResponse{Body: `{"result":[{"count":1}]}`}
		default:
			return testResponse{Body: `{"result":[]}`}
		}
	})
	if _, err := client.UpsertFact(context.Background(), validFact(), now); err != nil {
		t.Fatalf("concurrent exact upsert: %v", err)
	}
	if len(stored) != 2 || stored[0].RunID != "peer-run" || stored[1].RunID != "run-1" {
		t.Fatalf("concurrent sources = %+v", stored)
	}
}

func TestMemoryProvenanceMigrationConvertsDeduplicatesAndDropsRetiredFields(t *testing.T) {
	var canonicalSources []FactSource
	deleted := 0
	updated := 0
	dropped := 0
	indexed := false
	client, _ := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		switch {
		case statement == memorySchemaStateQuery:
			return testResponse{Body: `{"result":[{"properties":[
				{"name":"fact_key"},{"name":"sources"},{"name":"source_run_id"},{"name":"source_memory_ids"}
			],"indexes":[]}]}`}
		case strings.HasPrefix(statement, "SELECT @rid AS rid"):
			return testResponse{Body: `{"result":[
				{"rid":"#1:0","subject":"Davide","predicate":"lives_in","object":"Caraglio",
				 "statement":"Davide lives in Caraglio.","source_run_id":"run-1","source_memory_ids":["m1"]},
				{"rid":"#1:1","subject":"Davide","predicate":"lives_in","object":"Caraglio",
				 "statement":"Davide lives in Caraglio.","source_run_id":"run-2","source_memory_ids":["m2"]},
				{"rid":"#1:2","subject":"Davide","predicate":"lives_in","object":"Caraglio",
				 "statement":"Davide lives in Caraglio.","valid_to":"2020-01-01T00:00:00Z",
				 "expired_at":"2020-01-01T00:00:00Z","source_run_id":"history-1","source_memory_ids":[]},
				{"rid":"#1:3","subject":"Davide","predicate":"lives_in","object":"Caraglio",
				 "statement":"Davide lives in Caraglio.","valid_to":"2021-01-01T00:00:00Z",
				 "expired_at":"2021-01-01T00:00:00Z","source_run_id":"history-2","source_memory_ids":[]}
			]}`}
		case strings.HasPrefix(statement, "DELETE FROM FACT"):
			deleted++
			return testResponse{Body: `{"result":[{"count":1}]}`}
		case strings.HasPrefix(statement, "UPDATE FACT SET fact_key"):
			updated++
			if _, current := params["fact_key"]; current {
				canonicalSources = factSources(params["sources"])
			}
			return testResponse{Body: `{"result":[{"count":1}]}`}
		case strings.HasPrefix(statement, "CREATE INDEX"):
			indexed = true
			return testResponse{Body: `{"result":[]}`}
		case strings.HasPrefix(statement, "DROP PROPERTY"):
			dropped++
			return testResponse{Body: `{"result":[]}`}
		default:
			return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected statement"}`}
		}
	})
	if err := client.migrateMemoryProvenance(context.Background()); err != nil {
		t.Fatalf("migrateMemoryProvenance: %v", err)
	}
	if deleted != 1 || updated != 3 || !indexed || dropped != 2 || len(canonicalSources) != 2 {
		t.Fatalf("deleted=%d updated=%d indexed=%v dropped=%d sources=%+v",
			deleted, updated, indexed, dropped, canonicalSources)
	}
}

func TestMemoryProvenanceCurrentSchemaNeedsNoDataRewrite(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if statement != memorySchemaStateQuery {
			return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected rewrite"}`}
		}
		return testResponse{Body: `{"result":[{"properties":[{"name":"fact_key"},{"name":"sources"}],
			"indexes":[{"name":"FACT[fact_key]"}]}]}`}
	})
	if err := client.migrateMemoryProvenance(context.Background()); err != nil {
		t.Fatalf("migrateMemoryProvenance: %v", err)
	}
	if len(*requests) != 1 {
		t.Fatalf("warm schema made %d requests", len(*requests))
	}
}

func TestMemoryProvenanceResumesAfterIndexBeforeLegacyDrop(t *testing.T) {
	indexedAgain := false
	dropped := 0
	client, _ := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case statement == memorySchemaStateQuery:
			return testResponse{Body: `{"result":[{"properties":[
				{"name":"fact_key"},{"name":"sources"},{"name":"source_run_id"}
			],"indexes":[{"name":"FACT[fact_key]"}]}]}`}
		case strings.HasPrefix(statement, "SELECT @rid AS rid"):
			return testResponse{Body: `{"result":[{
				"rid":"#1:0","subject":"Davide","predicate":"lives_in","object":"Caraglio",
				"statement":"Davide lives in Caraglio.","source_run_id":"run-1",
				"sources":[{"run_id":"run-1","memory_ids":[]}]
			}]}`}
		case strings.HasPrefix(statement, "UPDATE FACT SET fact_key"):
			return testResponse{Body: `{"result":[{"count":1}]}`}
		case strings.HasPrefix(statement, "CREATE INDEX"):
			indexedAgain = true
			return testResponse{Body: `{"result":[]}`}
		case strings.HasPrefix(statement, "DROP PROPERTY"):
			dropped++
			return testResponse{Body: `{"result":[]}`}
		default:
			return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected statement"}`}
		}
	})
	if err := client.migrateMemoryProvenance(context.Background()); err != nil {
		t.Fatalf("resume migration: %v", err)
	}
	if indexedAgain || dropped != 2 {
		t.Fatalf("indexed again=%v dropped=%d", indexedAgain, dropped)
	}
}
