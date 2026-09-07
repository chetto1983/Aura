package arcadedb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTemporalEvidenceWindowsAndCompleteness(t *testing.T) {
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	base := FactHit{FactKey: "k", Subject: "A", Object: "B", Predicate: "uses", Statement: "A uses B", ValidFrom: "2026-06-01T00:00", Sources: []FactSource{{RunID: "observed"}}}
	for _, tc := range []struct {
		name   string
		change func(*FactHit)
		valid  bool
	}{
		{"open", func(*FactHit) {}, true},
		{"inclusive_start", func(f *FactHit) { f.ValidFrom = "2026-07-01T00:00:00Z" }, true},
		{"future", func(f *FactHit) { f.ValidFrom = "2026-08-01T00:00" }, false},
		{"exclusive_end", func(f *FactHit) { f.ValidTo = "2026-07-01 00:00:00" }, false},
		{"later_end", func(f *FactHit) { f.ValidTo = "2026-07-01T00:00:00.001" }, true},
		{"bad_start", func(f *FactHit) { f.ValidFrom = "unknown" }, false},
		{"unknown_start", func(f *FactHit) { f.ValidFrom = "" }, false},
		{"bad_end", func(f *FactHit) { f.ValidTo = "unknown" }, false},
		{"no_source", func(f *FactHit) { f.Sources = nil }, false},
		{"no_predicate", func(f *FactHit) { f.Predicate = "" }, false},
		{"no_subject", func(f *FactHit) { f.Subject = "" }, false},
		{"no_object", func(f *FactHit) { f.Object = "" }, false},
		{"no_statement", func(f *FactHit) { f.Statement = "" }, false},
		{"no_identity", func(f *FactHit) { f.FactKey = "" }, false},
		{"historical_record", func(f *FactHit) { f.FactKey = ""; f.RID = "#5:1" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fact := base
			tc.change(&fact)
			hadEnd := fact.ValidTo != ""
			err := validateTemporalFact(&fact, at)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
			if tc.valid {
				if _, parseErr := time.Parse(time.RFC3339Nano, fact.ValidFrom); parseErr != nil || !strings.HasSuffix(fact.ValidFrom, "Z") {
					t.Fatalf("noncanonical valid_from=%q", fact.ValidFrom)
				}
				if hadEnd {
					if _, parseErr := time.Parse(time.RFC3339Nano, fact.ValidTo); parseErr != nil || !strings.HasSuffix(fact.ValidTo, "Z") {
						t.Fatalf("noncanonical valid_to=%q", fact.ValidTo)
					}
				} else if fact.ValidTo != "" {
					t.Fatalf("open fact gained an end: %q", fact.ValidTo)
				}
			}
		})
	}
}

func TestTemporalPathSupportRefusesMalformedOrDisagreeingEvidence(t *testing.T) {
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	record := map[string]any{"fact_key": "k", "subject": "A", "object": "B", "predicate": "uses", "statement": "A uses B", "valid_from": "2026-06-01T00:00", "sources": []any{map[string]any{"run_id": "observed"}}}
	for _, tc := range []struct {
		name  string
		row   map[string]any
		edges []MemoryGraphPathEdge
		valid bool
	}{
		{"mention", map[string]any{"supports": []any{record}}, []MemoryGraphPathEdge{{Type: mentionsEdgeType, FactKey: "k"}}, true},
		{"missing", map[string]any{}, nil, false},
		{"malformed", map[string]any{"supports": []any{true}}, nil, false},
		{"duplicate", map[string]any{"supports": []any{record, record}}, nil, false},
		{"unsupported", map[string]any{"supports": []any{record}}, []MemoryGraphPathEdge{{Type: mentionsEdgeType, FactKey: "missing"}}, false},
		{"no_fact", map[string]any{"supports": []any{record}}, []MemoryGraphPathEdge{{Type: factEdgeType, FactKey: "k"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := temporalPathSupport(tc.row, tc.edges, at)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}
	for _, disagree := range []bool{false, true} {
		fact := factHitFromRow(record)
		if disagree {
			fact.Statement = "different evidence"
		}
		edges := []MemoryGraphPathEdge{{Type: factEdgeType, FactKey: "k", From: "A", To: "B", Fact: &fact}}
		err := temporalPathSupport(map[string]any{"supports": []any{record}}, edges, at)
		if (err != nil) != disagree {
			t.Fatalf("disagreement=%t err=%v", disagree, err)
		}
	}
}

func TestReadRepeatableUsesNativeSessionAndAlwaysEnds(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "query_error"}[fails], func(t *testing.T) {
			ended := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/begin/") {
					var payload map[string]string
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["isolationLevel"] != "REPEATABLE_READ" {
						t.Errorf("invalid begin body: %v %v", payload, err)
					}
					w.Header().Set(sessionHeader, "read-session")
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Header.Get(sessionHeader) != "read-session" {
					t.Error("query or cleanup lost its session")
				}
				if strings.Contains(r.URL.Path, "/rollback/") {
					ended++
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if fails {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"rejected"}`))
					return
				}
				_, _ = w.Write([]byte(`{"result":[{"value":1}]}`))
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL, Database: "test", User: "test", Password: "test"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.readRepeatable(context.Background(), "RETURN 1 AS value", nil)
			if (err != nil) != fails || ended != 1 {
				t.Fatalf("err=%v ended=%d", err, ended)
			}
		})
	}
}
