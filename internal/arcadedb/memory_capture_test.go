package arcadedb

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

var acceptedCaptureTime = time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)

func acceptedCaptureKey(fill string) string {
	return strings.Repeat(fill, 64)
}

func acceptedExplicitCapture(key, runID, role, object string) AcceptedCapture {
	conversationID := "conversation-a"
	toolCallID := "tool-call-" + runID
	return AcceptedCapture{
		IdempotencyKey: key,
		IdentityID:     "identity-a",
		ActorRunID:     runID,
		ActorRole:      role,
		SourceKind:     CaptureSourceExplicitFact,
		ConversationID: conversationID,
		ToolCallID:     toolCallID,
		SourceRefs: []string{
			"conversation:" + conversationID,
			"tool_call:" + toolCallID,
			"user_turn:" + runID,
		},
		Subject:    "Davide",
		Predicate:  "lives_in",
		Object:     object,
		Statement:  "Davide lives in " + object + ".",
		Confidence: 1,
		ObservedAt: acceptedCaptureTime,
	}
}

func applyCaptureForTest(t *testing.T, backend memoryBatchBackend, capture AcceptedCapture) error {
	t.Helper()
	return applyAcceptedCapture(
		context.Background(), capture, acceptedCaptureTime, defaultMemoryLimits, backend,
	)
}

func activeCaptureFacts(state memoryBatchState) []memoryBatchFact {
	active := []memoryBatchFact{}
	for _, fact := range state.Facts {
		if memoryBatchFactActive(fact, acceptedCaptureTime) {
			active = append(active, fact)
		}
	}
	return active
}

func captureFactByObject(t *testing.T, state memoryBatchState, object string) memoryBatchFact {
	t.Helper()
	for _, fact := range state.Facts {
		if fact.Fact.Object == object {
			return fact
		}
	}
	t.Fatalf("fact with object %q not found in %+v", object, state.Facts)
	return memoryBatchFact{}
}

func TestAcceptedCapture_Tracer(t *testing.T) {
	t.Run("one accepted explicit fact is durable", func(t *testing.T) {
		backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState())
		capture := acceptedExplicitCapture(acceptedCaptureKey("a"), "run-parent", "parent", "Caraglio")
		if err := applyCaptureForTest(t, backend, capture); err != nil {
			t.Fatalf("ApplyAcceptedCapture: %v", err)
		}
		final := backend.snapshot(capture.IdentityID)
		facts := activeCaptureFacts(final)
		if len(facts) != 1 || facts[0].Fact.Statement != capture.Statement {
			t.Fatalf("active facts = %+v, want the accepted statement", facts)
		}
		if backend.mutatingCommits != 1 {
			t.Fatalf("mutating commits = %d, want one", backend.mutatingCommits)
		}
	})

	t.Run("principal contradiction preserves temporal history", func(t *testing.T) {
		old := memoryBatchTestStoredFact("Davide", "lives_in", "Torino", "run-old")
		backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState(old))
		capture := acceptedExplicitCapture(acceptedCaptureKey("b"), "run-parent", "parent", "Caraglio")
		capture.Supersedes = true
		if err := applyCaptureForTest(t, backend, capture); err != nil {
			t.Fatalf("ApplyAcceptedCapture: %v", err)
		}
		final := backend.snapshot(capture.IdentityID)
		if len(final.Facts) != 2 || len(activeCaptureFacts(final)) != 1 {
			t.Fatalf("facts = %+v, want one historical and one active", final.Facts)
		}
		closed := captureFactByObject(t, final, "Torino")
		if !closed.ValidTo.Equal(acceptedCaptureTime) || closed.ExpiredAt.IsZero() {
			t.Fatalf("closed interval = valid_to %v expired_at %v", closed.ValidTo, closed.ExpiredAt)
		}
		if got := captureFactByObject(t, final, "Caraglio"); !memoryBatchFactActive(got, acceptedCaptureTime) {
			t.Fatalf("replacement is not active: %+v", got)
		}
	})

	t.Run("worker cannot supersede principal state", func(t *testing.T) {
		old := memoryBatchTestStoredFact("Davide", "lives_in", "Torino", "run-old")
		backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState(old))
		capture := acceptedExplicitCapture(acceptedCaptureKey("c"), "run-worker", "worker", "Caraglio")
		capture.Supersedes = true
		err := applyCaptureForTest(t, backend, capture)
		var batchErr *MemoryBatchError
		if !errors.As(err, &batchErr) || batchErr.Code != "unauthorized_actor" {
			t.Fatalf("error = %v, want unauthorized_actor", err)
		}
		if backend.commitAttempts != 0 {
			t.Fatalf("commit attempts = %d, want rejection before graph mutation", backend.commitAttempts)
		}
		final := backend.snapshot(capture.IdentityID)
		if len(final.Facts) != 1 || len(activeCaptureFacts(final)) != 1 {
			t.Fatalf("worker supersede changed state: %+v", final.Facts)
		}
	})
}

func TestAcceptedCapture_Idempotent(t *testing.T) {
	backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState())
	first := acceptedExplicitCapture(acceptedCaptureKey("d"), "run-a", "parent", "Caraglio")
	second := acceptedExplicitCapture(acceptedCaptureKey("e"), "run-b", "parent", "Caraglio")
	second.ToolCallID = "tool-call-distinct"
	second.SourceRefs = []string{
		"conversation:conversation-a", "tool_call:tool-call-distinct", "user_turn:run-b",
	}

	for _, capture := range []AcceptedCapture{first, first, second} {
		if err := applyCaptureForTest(t, backend, capture); err != nil {
			t.Fatalf("ApplyAcceptedCapture(%s): %v", capture.ActorRunID, err)
		}
	}
	final := backend.snapshot(first.IdentityID)
	if len(final.Facts) != 1 {
		t.Fatalf("facts = %+v, want one content-deduplicated fact", final.Facts)
	}
	fact := activeCaptureFacts(final)[0]
	if len(fact.Sources) != 2 {
		t.Fatalf("sources = %+v, want one per direct run", fact.Sources)
	}
	wantRefs := []string{
		"capture:" + first.IdempotencyKey,
		"capture:" + second.IdempotencyKey,
		"tool_call:" + first.ToolCallID,
		"tool_call:" + second.ToolCallID,
	}
	allRefs := []string{}
	for _, source := range fact.Sources {
		if count := countStrings(source.MemoryIDs, "conversation:conversation-a"); count != 1 {
			t.Fatalf("source %+v carries the shared conversation ref %d times, want once", source, count)
		}
		allRefs = append(allRefs, source.MemoryIDs...)
	}
	for _, ref := range wantRefs {
		if count := countStrings(allRefs, ref); count != 1 {
			t.Fatalf("direct ref %q occurs %d times in %+v, want once", ref, count, allRefs)
		}
	}
	if backend.mutatingCommits != 2 {
		t.Fatalf("mutating commits = %d, want replay no-op plus distinct-source enrichment", backend.mutatingCommits)
	}
}

func TestAcceptedCapture_Retry(t *testing.T) {
	backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState())
	backend.commitConflicts = 1
	backend.conflictStateMutation = func(memoryBatchState) memoryBatchState {
		peer := memoryBatchTestStoredFact("Davide", "lives_in", "Caraglio", "peer-run")
		peer.Fact.Statement = "Davide lives in Caraglio."
		peer.FactKey = factIdentity(peer.Fact)
		return memoryBatchTestState(peer)
	}
	capture := acceptedExplicitCapture(acceptedCaptureKey("f"), "run-parent", "parent", "Caraglio")
	if err := applyCaptureForTest(t, backend, capture); err != nil {
		t.Fatalf("ApplyAcceptedCapture: %v", err)
	}
	final := backend.snapshot(capture.IdentityID)
	if backend.commitAttempts != 2 || backend.mutatingCommits != 1 {
		t.Fatalf("commit attempts=%d mutations=%d, want one conflict plus one committed decision",
			backend.commitAttempts, backend.mutatingCommits)
	}
	if len(final.Facts) != 1 || len(activeCaptureFacts(final)[0].Sources) != 2 {
		t.Fatalf("retry did not recompute from peer state: %+v", final.Facts)
	}
}

func TestAcceptedCapture_SourceDefense(t *testing.T) {
	for _, sourceKind := range []CaptureSourceKind{
		"reasoning", "summary", "generated_text", "assistant_prose", "tool_result",
	} {
		t.Run(string(sourceKind), func(t *testing.T) {
			backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState())
			capture := acceptedExplicitCapture(acceptedCaptureKey("1"), "run-parent", "parent", "Caraglio")
			capture.SourceKind = sourceKind
			err := applyCaptureForTest(t, backend, capture)
			if err == nil || !strings.Contains(err.Error(), "source kind") {
				t.Fatalf("error = %v, want source-kind rejection", err)
			}
			if backend.commitAttempts != 0 || len(backend.snapshot(capture.IdentityID).Facts) != 0 {
				t.Fatal("ineligible source reached graph mutation")
			}
		})
	}
}

func TestAcceptedCapture_Contradiction(t *testing.T) {
	historical := memoryBatchTestStoredFact("Davide", "lives_in", "Cuneo", "run-history")
	historical.ValidTo = acceptedCaptureTime.Add(-24 * time.Hour)
	historical.ExpiredAt = historical.ValidTo
	historical.Fact.ValidTo = historical.ValidTo
	historical.FactKey = ""
	current := memoryBatchTestStoredFact("Davide", "lives_in", "Torino", "run-current")
	backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState(historical, current))
	capture := acceptedExplicitCapture(acceptedCaptureKey("2"), "run-parent", "parent", "Caraglio")
	capture.ValidFrom = acceptedCaptureTime.Add(-time.Hour)
	capture.Supersedes = true

	if err := applyCaptureForTest(t, backend, capture); err != nil {
		t.Fatalf("ApplyAcceptedCapture: %v", err)
	}
	final := backend.snapshot(capture.IdentityID)
	if len(final.Facts) != 3 || len(activeCaptureFacts(final)) != 1 {
		t.Fatalf("facts = %+v, want two historical intervals plus one active replacement", final.Facts)
	}
	if got := captureFactByObject(t, final, "Cuneo"); !got.ValidTo.Equal(historical.ValidTo) {
		t.Fatalf("older historical interval changed: %+v", got)
	}
	if got := captureFactByObject(t, final, "Torino"); !got.ValidTo.Equal(capture.ValidFrom) {
		t.Fatalf("current interval closed at %v, want %v", got.ValidTo, capture.ValidFrom)
	}
	replacement := captureFactByObject(t, final, "Caraglio")
	assertCaptureProvenance(t, replacement, capture)
}

func TestAcceptedCapture_WorkerAuthority(t *testing.T) {
	old := memoryBatchTestStoredFact("Davide", "lives_in", "Torino", "run-parent")
	backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState(old))
	workerEvidence := acceptedExplicitCapture(acceptedCaptureKey("3"), "run-worker", "worker", "Caraglio")
	if err := applyCaptureForTest(t, backend, workerEvidence); err != nil {
		t.Fatalf("worker evidence append: %v", err)
	}
	stateAfterEvidence := backend.snapshot(workerEvidence.IdentityID)
	if len(activeCaptureFacts(stateAfterEvidence)) != 2 {
		t.Fatalf("worker evidence replaced principal state: %+v", stateAfterEvidence.Facts)
	}
	assertCaptureProvenance(t, captureFactByObject(t, stateAfterEvidence, "Caraglio"), workerEvidence)

	workerSupersede := acceptedExplicitCapture(acceptedCaptureKey("4"), "run-worker", "worker", "Alba")
	workerSupersede.Supersedes = true
	if err := applyCaptureForTest(t, backend, workerSupersede); err == nil {
		t.Fatal("worker supersede succeeded")
	}
	if got := backend.snapshot(workerEvidence.IdentityID); len(activeCaptureFacts(got)) != 2 {
		t.Fatalf("refused worker supersede mutated state: %+v", got.Facts)
	}
}

func TestAcceptedCapture_PrincipalAuthority(t *testing.T) {
	torino := memoryBatchTestStoredFact("Davide", "lives_in", "Torino", "run-torino")
	bra := memoryBatchTestStoredFact("Davide", "lives_in", "Bra", "run-bra")
	backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState(torino, bra))
	capture := acceptedExplicitCapture(acceptedCaptureKey("5"), "run-parent", "parent", "Caraglio")
	capture.Supersedes = true
	capture.TargetFactKey = torino.FactKey

	if err := applyCaptureForTest(t, backend, capture); err != nil {
		t.Fatalf("principal supersede: %v", err)
	}
	final := backend.snapshot(capture.IdentityID)
	if got := captureFactByObject(t, final, "Torino"); memoryBatchFactActive(got, acceptedCaptureTime) {
		t.Fatalf("targeted Torino interval remained active: %+v", got)
	}
	if got := captureFactByObject(t, final, "Bra"); !memoryBatchFactActive(got, acceptedCaptureTime) {
		t.Fatalf("non-target Bra interval was closed: %+v", got)
	}
	replacement := captureFactByObject(t, final, "Caraglio")
	if !memoryBatchFactActive(replacement, acceptedCaptureTime) {
		t.Fatalf("replacement is not active: %+v", replacement)
	}
	assertCaptureProvenance(t, replacement, capture)
}

func TestAcceptedCapture_ProvenanceEnrichment(t *testing.T) {
	backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState())
	first := acceptedExplicitCapture(acceptedCaptureKey("6"), "run-parent", "parent", "Caraglio")
	second := acceptedExplicitCapture(acceptedCaptureKey("7"), "run-parent", "parent", "Caraglio")
	second.ToolCallID = "tool-call-second"
	second.SourceRefs = []string{
		"conversation:conversation-a", "tool_call:tool-call-second", "user_turn:run-parent", "memory:message-8",
	}
	for _, capture := range []AcceptedCapture{first, first, second} {
		if err := applyCaptureForTest(t, backend, capture); err != nil {
			t.Fatalf("ApplyAcceptedCapture(%s): %v", capture.IdempotencyKey, err)
		}
	}
	fact := activeCaptureFacts(backend.snapshot(first.IdentityID))[0]
	if len(fact.Sources) != 1 {
		t.Fatalf("sources = %+v, want one host run with enriched capture evidence", fact.Sources)
	}
	captures := fact.Sources[0].Captures
	if len(captures) != 2 {
		t.Fatalf("capture provenance = %+v, want two distinct entries and no replay duplicate", captures)
	}
	for _, capture := range []AcceptedCapture{first, second} {
		found := false
		for _, stored := range captures {
			if stored.IdempotencyKey == capture.IdempotencyKey {
				found = true
				if !slices.Equal(stored.SourceRefs, normalizeAcceptedCapture(capture).SourceRefs) ||
					stored.SourceKind != capture.SourceKind || stored.ConversationID != capture.ConversationID ||
					stored.ToolCallID != capture.ToolCallID || !stored.ObservedAt.Equal(capture.ObservedAt) ||
					stored.Confidence != capture.Confidence {
					t.Fatalf("stored provenance = %+v, want capture %+v", stored, capture)
				}
			}
		}
		if !found {
			t.Fatalf("capture %s absent from %+v", capture.IdempotencyKey, captures)
		}
	}
	encoded, err := json.Marshal(sourcesParam(fact.Sources))
	if err != nil {
		t.Fatalf("encode provenance: %v", err)
	}
	var wire any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode provenance: %v", err)
	}
	roundTripped := factSources(wire)
	if len(roundTripped) != 1 || len(roundTripped[0].Captures) != 2 {
		t.Fatalf("wire provenance = %+v, want both accepted captures", roundTripped)
	}
}

func TestAcceptedCapture_RecallTimestampMatchesArcadeDBDatetimePrecision(t *testing.T) {
	instant := time.Date(2026, 9, 1, 5, 36, 7, 123456789, time.UTC)
	if got, want := nullableMemoryBatchTime(instant), "2026-09-01T05:36:07Z"; got != want {
		t.Fatalf("batch DATETIME parameter = %v, want %s for immediate as-of recall", got, want)
	}
}

func assertCaptureProvenance(t *testing.T, fact memoryBatchFact, capture AcceptedCapture) {
	t.Helper()
	for _, source := range fact.Sources {
		if source.RunID != capture.ActorRunID || source.WriterRole != WriterRole(capture.ActorRole) {
			continue
		}
		for _, stored := range source.Captures {
			if stored.IdempotencyKey == capture.IdempotencyKey &&
				stored.SourceKind == capture.SourceKind &&
				stored.ConversationID == capture.ConversationID &&
				stored.ToolCallID == capture.ToolCallID &&
				stored.ObservedAt.Equal(capture.ObservedAt) && stored.Confidence == capture.Confidence {
				return
			}
		}
	}
	t.Fatalf("capture provenance for %s absent from %+v", capture.IdempotencyKey, fact.Sources)
}

func countStrings(values []string, target string) int {
	return len(slices.DeleteFunc(slices.Clone(values), func(value string) bool { return value != target }))
}
