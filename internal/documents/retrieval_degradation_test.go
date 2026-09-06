package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A degradation that says nothing is the failure this file exists to end: measured live on
// 2026-09-06, document_search answered `arcadedb_unavailable` with an empty result on every
// turn and the server log carried not one line about it.
func TestDegradationNamesItsCauseInTheLog(t *testing.T) {
	var buf bytes.Buffer
	restore := swapDefaultLogger(t, &buf)
	defer restore()

	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	retriever := &HostRetriever{
		ControlPlane: control,
		PassageIndex: &fakePassageIndex{fusedErr: errors.New("engine refused")},
		Embedder:     &fakeRetrievalEmbedder{vector: []float64{1}},
	}
	if _, err := retriever.Retrieve(context.Background(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "codice cliente",
	}); err != nil {
		t.Fatal(err)
	}
	line := buf.String()
	if !strings.Contains(line, "engine refused") || !strings.Contains(line, DegradationArcade) {
		t.Fatalf("log = %q, want the reason and the cause", line)
	}
}

// An unwired passage index and an index that is wired and refusing need different operator
// responses, so they must not share a name on the wire.
func TestUnconfiguredPassageIndexIsNotAnOutage(t *testing.T) {
	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	response, err := (&HostRetriever{ControlPlane: control}).Retrieve(
		context.Background(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "codice cliente"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != RetrievalCardOnly || response.DegradationReason != DegradationUnconfigured {
		t.Fatalf("response = %#v", response)
	}
}

// The rate limit is what makes the log survivable on a per-turn tool: an unchanged cause is
// stated once, then again only after the restate interval.
func TestDegradationLogSuppressesAnUnchangedCause(t *testing.T) {
	now := time.Unix(0, 0)
	log := &degradationLog{now: func() time.Time { return now }}

	if !log.state(DegradationArcade, "engine refused") {
		t.Fatal("the first statement must always be made")
	}
	if log.state(DegradationArcade, "engine refused") {
		t.Fatal("an unchanged cause must stay quiet")
	}
	if !log.state(DegradationArcade, "a different fault") {
		t.Fatal("a CHANGED cause must be stated immediately")
	}
	now = now.Add(degradationRestateAfter)
	if !log.state(DegradationArcade, "a different fault") {
		t.Fatal("an outage that outlives the interval must be restated")
	}
	if !log.state(DegradationEmbedding, "a different fault") {
		t.Fatal("reasons are tracked apart: one quiet reason must not silence another")
	}
}

// A nil log never suppresses — a retriever built without one still says everything rather
// than silently swallowing, which is the whole point.
func TestNilDegradationLogNeverSuppresses(t *testing.T) {
	var log *degradationLog
	for range 3 {
		if !log.state(DegradationArcade, "same cause") {
			t.Fatal("a nil log must never suppress")
		}
	}
}

func swapDefaultLogger(t *testing.T, buf *bytes.Buffer) func() {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return func() { slog.SetDefault(previous) }
}

// The log line must stay machine-readable: an operator greps it, a dashboard parses it.
func TestDegradationLogLineIsStructured(t *testing.T) {
	var buf bytes.Buffer
	restore := swapDefaultLogger(t, &buf)
	defer restore()

	(&degradationLog{}).warn(DegradationArcade, "engine refused", retrievalIdentity)

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
	}
	for key, want := range map[string]string{
		"reason": DegradationArcade, "cause": "engine refused", "identity": retrievalIdentity,
	} {
		if record[key] != want {
			t.Fatalf("%s = %v, want %q", key, record[key], want)
		}
	}
}
