package arcadedb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestReasoningDeletionRetriesMissingRecordFromFreshSelection(t *testing.T) {
	for _, tc := range []struct {
		name, exception string
		status          int
		wantRetry       bool
		persistent      bool
	}{
		{"write_conflict", "com.arcadedb.exception.ConcurrentModificationException", 503, true, false},
		{"key_conflict", "com.arcadedb.exception.DuplicatedKeyException", 409, true, false},
		{"concurrent_vertex_delete", "com.arcadedb.exception.VertexNotFoundException", 404, true, false},
		{"persistent_missing_vertex", "com.arcadedb.exception.VertexNotFoundException", 404, false, true},
		{"other_missing_record", "com.arcadedb.exception.RecordNotFoundException", 404, false, false},
		{"wrong_status", "com.arcadedb.exception.VertexNotFoundException", 500, false, false},
		{"missing_database", "com.arcadedb.exception.DatabaseOperationException", 404, false, false},
		{"missing_http_route", "", 404, false, false},
		{"permission_denied", "com.arcadedb.exception.SecurityException", 403, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var events []string
			begins := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				operation, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
				events = append(events, operation)
				switch operation {
				case "begin":
					begins++
					w.Header().Set(sessionHeader, fmt.Sprintf("attempt-%d", begins))
					w.WriteHeader(http.StatusNoContent)
				case "query":
					if begins == 1 || tc.persistent {
						_, _ = fmt.Fprint(w, `{"result":[{"trace_id":"trace-race"}]}`)
					} else {
						_, _ = fmt.Fprint(w, `{"result":[]}`)
					}
				case "command":
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprintf(w, `{"exception":%q,"detail":"peer removed the selected vertex"}`, tc.exception)
				case "commit", "rollback":
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected operation %s", operation)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL, Database: "memory", User: "root", Password: "test"})
			if err != nil {
				t.Fatal(err)
			}
			deleted, err := client.DeleteReasoningBySource(context.Background(), ReasoningDeleteSelector{
				IdentityID: "identity-a", TraceID: "trace-race",
			})
			if (err == nil) != tc.wantRetry || deleted != 0 {
				t.Fatalf("deleted=%d err=%v want successful retry=%v", deleted, err, tc.wantRetry)
			}
			want := []string{"begin", "query", "command", "rollback"}
			if tc.persistent {
				for range maxWriteConflictRetries {
					want = append(want, "begin", "query", "command", "rollback")
				}
			}
			if tc.wantRetry {
				want = append(want, "begin", "query", "commit", "rollback")
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("transaction sequence=%v want=%v", events, want)
			}
		})
	}
}
