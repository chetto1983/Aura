package arcadedb

import (
	"context"
	"strings"
	"testing"
)

func vectorsFor(n int) ([]string, [][]float64) {
	rids := make([]string, n)
	vectors := make([][]float64, n)
	for i := range rids {
		rids[i] = "#5:" + string(rune('0'+i))
		vectors[i] = make([]float64, vectorDimensions)
	}
	return rids, vectors
}

func countLanguage(rec *recorder, language string) int {
	n := 0
	for _, l := range rec.languages {
		if l == language {
			n++
		}
	}
	return n
}

// One round trip for the whole batch, and each statement bound to its OWN pair.
//
// Both halves are load-bearing and neither shows in the return value. Measured live
// 2026-08-03: a vector UPDATE by @rid costs 55-78ms against a 53-63ms bare round
// trip, so the write is ~10ms and the rest is HTTP — 32 facts written singly spent
// ~1.8s in handshakes to do ~0.3s of work. The per-statement binding is the part
// the manual could not prove and that was verified against a live 26.7.3 (three
// @rids in one script, all three landed); were the statements to share one
// :vector/:rid pair, every row would silently take the last vector.
func TestWriteVectorsSendsOneBoundScriptForTheWholeBatch(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	rids, vectors := vectorsFor(3)

	written, err := client.writeVectors(context.Background(), rids, vectors)
	if err != nil {
		t.Fatalf("writeVectors: %v", err)
	}
	if written != 3 {
		t.Errorf("written = %d, want 3", written)
	}
	if got := countLanguage(rec, "sqlscript"); got != 1 {
		t.Errorf("sqlscript requests = %d, want exactly 1", got)
	}
	if got := countLanguage(rec, "sql"); got != 0 {
		t.Errorf("single-statement requests = %d, want 0 — a successful batch must not fall back", got)
	}
	if got := strings.Count(rec.statements[0], "UPDATE"); got != 3 {
		t.Errorf("UPDATE statements in the script = %d, want 3", got)
	}
	for _, key := range []string{"v0", "r0", "v1", "r1", "v2", "r2"} {
		if _, ok := rec.params[0][key]; !ok {
			t.Errorf("param %q missing — the statements would share a binding", key)
		}
	}
	if rec.params[0]["r0"] == rec.params[0]["r1"] {
		t.Error("r0 and r1 bound to the same rid")
	}
}

// The fallback is the poison-row cure, not defensive padding. A sqlscript is atomic,
// so one malformed row would roll back its healthy companions and do so again on
// every later sweep — the batch would never land and the tenant would stall behind
// it. When the script fails, every fact must still get through singly.
func TestWriteVectorsFallsBackToSingleStatementsWhenTheScriptFails(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	rec.failLanguage = "sqlscript"
	rids, vectors := vectorsFor(5)

	written, err := client.writeVectors(context.Background(), rids, vectors)
	if err != nil {
		t.Fatalf("writeVectors after script failure: %v", err)
	}
	if written != 5 {
		t.Errorf("written = %d, want 5 — every fact must still land via the fallback", written)
	}
	if got := countLanguage(rec, "sqlscript"); got != 1 {
		t.Errorf("sqlscript attempts = %d, want 1", got)
	}
	if got := countLanguage(rec, "sql"); got != 5 {
		t.Errorf("single-statement requests = %d, want 5", got)
	}
}

// A vector of the wrong width is skipped, never written: the index is declared at
// 768 and refuses anything else, so storing one would trade a missing vector for a
// rejected one. Skipping is not an error — the sweep sees the fact again next round.
// With nothing usable at all, no transaction is opened just to close it.
func TestWriteVectorsSkipsUnusableVectors(t *testing.T) {
	for name, tc := range map[string]struct {
		spoil       func([][]float64)
		wantWritten int
		wantRequest bool
	}{
		"one wrong width": {
			spoil:       func(v [][]float64) { v[1] = make([]float64, 7) },
			wantWritten: 2, wantRequest: true,
		},
		"all unusable": {
			spoil:       func(v [][]float64) { v[0], v[1], v[2] = nil, make([]float64, 3), nil },
			wantWritten: 0, wantRequest: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, rec := recordingClient(t, `{"result":[]}`)
			rids, vectors := vectorsFor(3)
			tc.spoil(vectors)

			written, err := client.writeVectors(context.Background(), rids, vectors)
			if err != nil {
				t.Fatalf("writeVectors: %v", err)
			}
			if written != tc.wantWritten {
				t.Errorf("written = %d, want %d", written, tc.wantWritten)
			}
			if sent := len(rec.statements) > 0; sent != tc.wantRequest {
				t.Errorf("request sent = %v, want %v", sent, tc.wantRequest)
			}
		})
	}
}
