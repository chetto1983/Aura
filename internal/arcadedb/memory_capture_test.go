package arcadedb

import (
	"context"
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
	second.SourceRefs = []string{"conversation:conversation-a", "tool_call:tool-call-distinct"}

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
		"conversation:conversation-a",
		"tool_call:" + first.ToolCallID,
		"tool_call:" + second.ToolCallID,
	}
	allRefs := []string{}
	for _, source := range fact.Sources {
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

func countStrings(values []string, target string) int {
	return len(slices.DeleteFunc(slices.Clone(values), func(value string) bool { return value != target }))
}
