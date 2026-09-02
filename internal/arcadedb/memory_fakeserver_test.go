package arcadedb

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The fake ArcadeDB every unit test in this package talks to. Split out of
// memory_test.go, which the file-size gate caps at 600 LOC and which crossed it
// when recordingClient grew a per-statement response list.
//
// It records what was sent rather than asserting on it, so each test states its
// own expectation about the statements, the bind parameters and the language.

type recorder struct {
	statements []string
	params     []map[string]any
	// languages parallels statements. Only a caller that batches cares — the write
	// path sends "sqlscript" for a whole batch and "sql" for the per-fact fallback,
	// and that distinction is invisible in the statements themselves.
	languages []string
	// failLanguage makes the server refuse requests in that language, which is how
	// a test reaches the fallback branch without a real poisoned row.
	failLanguage string
	body         string
	// bodies answers successive statements when a single call issues more than
	// one — ListEntities pages the listing and then counts the total. The LAST
	// entry repeats, so a caller that passes one body keeps the old behaviour.
	bodies []string
}

func recordingClient(t *testing.T, bodies ...string) (*Client, *recorder) {
	t.Helper()
	if len(bodies) == 0 {
		t.Fatal("recordingClient needs at least one response body")
	}
	rec := &recorder{body: bodies[0], bodies: bodies}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleTransactionEndpoints(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Command  string         `json:"command"`
			Params   map[string]any `json:"params"`
			Language string         `json:"language"`
		}
		_ = json.Unmarshal(raw, &payload)
		rec.statements = append(rec.statements, payload.Command)
		rec.params = append(rec.params, payload.Params)
		rec.languages = append(rec.languages, payload.Language)
		if rec.failLanguage != "" && payload.Language == rec.failLanguage {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"refused"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := rec.body
		if n := len(rec.bodies); n > 0 {
			if i := len(rec.statements) - 1; i < n {
				body = rec.bodies[i]
			} else {
				body = rec.bodies[n-1]
			}
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	client, err := New(Config{BaseURL: srv.URL, Database: "aura", User: "root"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, rec
}

func (r *recorder) joined() string { return strings.Join(r.statements, "\n") }
