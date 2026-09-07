package arcadedb

import (
	"strings"
	"testing"
	"time"
)

func TestIncompleteMentionInventoriesCannotDelete(t *testing.T) {
	for _, tc := range []struct {
		name, entities, edges string
		wantError             bool
	}{
		{"entities", `{"result":[{"name":"A"},{"name":"B"},{"name":"C"}]}`, `{"result":[]}`, false},
		{"edges", `{"result":[{"name":"A"}]}`, `{"result":[{"source":"A","target":"B","fact_key":"1"},{"source":"A","target":"C","fact_key":"2"},{"source":"A","target":"D","fact_key":"3"}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, rec := recordingClient(t, tc.entities, `{"result":[]}`, tc.edges)
			client.limits.DigestScan = 2
			result, err := client.LinkMentions(t.Context())
			if (err != nil) != tc.wantError || result.Covered {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			for _, statement := range rec.statements {
				if !strings.HasPrefix(statement, "SELECT ") {
					t.Fatalf("incomplete scan wrote %q", statement)
				}
			}
		})
	}
}

func TestNeighborhoodPreflightAndEvidenceRefusal(t *testing.T) {
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	client, rec := recordingClient(t, `{"result":[{"name":"Entity","type":"vertex","records":10001}]}`)
	if _, err := client.FactsAbout(t.Context(), "A", "", 10, at, FactsAboutNeighbourhood); err == nil {
		t.Fatal("oversized neighborhood accepted")
	}
	if len(rec.statements) != 1 {
		t.Fatal("algorithm ran after failed preflight")
	}
	for _, body := range []string{`{"result":[{"fact":false}]}`, `{"result":[{"fact":{"fact_key":"bad"}}]}`} {
		client, _ := recordingClient(t, `{"result":[]}`, body)
		if _, err := client.FactsAbout(t.Context(), "A", "", 10, at, FactsAboutNeighbourhood); err == nil {
			t.Fatal("malformed evidence accepted")
		}
	}
}
