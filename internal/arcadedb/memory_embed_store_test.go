package arcadedb

import (
	"context"
	"strings"
	"testing"
)

func vectorsFor(n int) ([]string, []storedVector) {
	rids := make([]string, n)
	vectors := make([]storedVector, n)
	for i := range rids {
		rids[i] = "#5:" + string(rune('0'+i))
		vectors[i] = storedVector{vector: make([]float64, vectorDimensions), space: "es1-a"}
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

// One round trip for the whole batch, and each statement bound to its OWN triple.
//
// Both halves are load-bearing and neither shows in the return value. Measured live
// 2026-08-03: a vector UPDATE by @rid costs 55-78ms against a 53-63ms bare round
// trip, so the write is ~10ms and the rest is HTTP — 32 facts written singly spent
// ~1.8s in handshakes to do ~0.3s of work. The per-statement binding is the part
// the manual could not prove and that was verified against a live 26.7.3 (three
// @rids in one script, all three landed); were the statements to share one
// :vector/:rid pair, every row would silently take the last vector.
func TestStoreVectorsSendsOneBoundScriptForTheWholeBatch(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	rids, vectors := vectorsFor(3)

	tally := client.storeVectors(context.Background(), factEdgeType, rids, vectors)
	if tally.embedded != 3 {
		t.Errorf("embedded = %d, want 3", tally.embedded)
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
	for _, key := range []string{"v0", "s0", "r0", "v1", "s1", "r1", "v2", "s2", "r2"} {
		if _, ok := rec.params[0][key]; !ok {
			t.Errorf("param %q missing — the statements would share a binding", key)
		}
	}
	if rec.params[0]["r0"] == rec.params[0]["r1"] {
		t.Error("r0 and r1 bound to the same rid")
	}
	if rec.params[0]["s0"] != "es1-a" {
		t.Errorf("s0 = %v, want the vector's space", rec.params[0]["s0"])
	}
}

// The fallback is the poison-row cure, not defensive padding. A sqlscript is atomic,
// so one malformed row would roll back its healthy companions and do so again on
// every later sweep — the batch would never land and the tenant would stall behind
// it. When the script fails, every row must still get through singly.
func TestStoreVectorsFallsBackToSingleStatementsWhenTheScriptFails(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	rec.failLanguage = "sqlscript"
	rids, vectors := vectorsFor(5)

	tally := client.storeVectors(context.Background(), factEdgeType, rids, vectors)
	if tally.embedded != 5 || tally.failed != 0 {
		t.Errorf("embedded = %d, want 5 — every row must still land via the fallback", tally.embedded)
	}
	if got := countLanguage(rec, "sqlscript"); got != 1 {
		t.Errorf("sqlscript attempts = %d, want 1", got)
	}
	if got := countLanguage(rec, "sql"); got != 5 {
		t.Errorf("single-statement requests = %d, want 5", got)
	}
}

// A row with neither vector nor space (a wrong-width answer) is skipped, never written:
// the index is declared at 768 and refuses anything else, and the cursor moves past it.
// A refusal is written -- no vector, the refusing space -- and counted apart. With nothing
// to write at all, no request is sent.
func TestStoreVectorsSkipsUnusableRowsAndWritesRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		spoil       func([]storedVector)
		wantTally   passTally
		wantRequest bool
	}{
		"one unusable": {
			spoil:     func(v []storedVector) { v[1] = storedVector{} },
			wantTally: passTally{embedded: 2}, wantRequest: true,
		},
		"one refused": {
			spoil:     func(v []storedVector) { v[1] = storedVector{space: "es1-a"} },
			wantTally: passTally{embedded: 2, refused: 1}, wantRequest: true,
		},
		"all unusable": {
			spoil:     func(v []storedVector) { v[0], v[1], v[2] = storedVector{}, storedVector{}, storedVector{} },
			wantTally: passTally{}, wantRequest: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, rec := recordingClient(t, `{"result":[]}`)
			rids, vectors := vectorsFor(3)
			tc.spoil(vectors)

			tally := client.storeVectors(context.Background(), factEdgeType, rids, vectors)
			if tally != tc.wantTally {
				t.Errorf("tally = %+v, want %+v", tally, tc.wantTally)
			}
			if sent := len(rec.statements) > 0; sent != tc.wantRequest {
				t.Errorf("request sent = %v, want %v", sent, tc.wantRequest)
			}
		})
	}
}
