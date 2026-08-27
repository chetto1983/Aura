//go:build arcadedb_integration

// SWARM-07/SC#5's real hazard: N workers writing into ONE identity's graph at
// the same time. A sequential `for` loop calling UpsertFact N times in a row
// passes green while leaving the actual race -- two Command calls landing on
// the same content key at once -- completely unexercised (51-RESEARCH.md
// Sampling Rate). This file drives real goroutines against the live
// aura-arcadedb sidecar, per internal/swarm/swarm_test.go's own
// goleak.VerifyNone(t) convention.
//
// Run: go test -race -tags arcadedb_integration ./internal/arcadedb/ -run TestConcurrentWorkerFactWrite
package arcadedb

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// D-09 regression, under real concurrency rather than assumption: N workers
// each independently learning the SAME thing must still produce exactly one
// fact, carrying all N sources -- factIdentity's content key plus
// attachFactSource's short-circuit (memory.go ~line 265) is what already
// delivers this; nothing here is new production code, only the proof.
//
// The object suffix is deliberately short ("_k", not the full subject or a
// descriptive phrase): looksLikeProse's 80-rune bound applies to Object, and
// isolate(t, ...) derives subject from t.Name(), so a long test name plus a
// long suffix can silently cross that bound and fail for a reason unrelated
// to concurrency.
func TestConcurrentWorkerFactWriteSameContentMergesIntoOneFact(t *testing.T) {
	defer goleak.VerifyNone(t)
	client := integrationClient(t)
	// goleak sees net/http's idle keep-alive connections (persistConn's
	// readLoop/writeLoop) as "unexpected goroutines" even though they are
	// not a leak -- closing them before goleak's check is the standard
	// fix for this well-documented interaction, not a suppression of a
	// real one: every assertion below still runs at full strictness.
	defer client.http.CloseIdleConnections()
	subject, _ := isolate(t, client)
	object := subject + "_k"

	const n = 8
	runIDs := make([]string, n)
	for i := range n {
		runIDs[i] = fmt.Sprintf("%s-w%d", subject, i)
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := client.UpsertFact(context.Background(), Fact{
				Subject: subject, Predicate: "learned_lesson", Object: object,
				Statement: subject + " learned: the same shared lesson.",
				Source:    FactSource{RunID: runIDs[i], WriterRole: WriterWorker},
			}, time.Now())
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: UpsertFact: %v", i, err)
		}
	}

	hits, err := client.FactsAbout(context.Background(), subject, "learned_lesson", 20, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("facts = %d, want exactly 1 (N workers learning the same thing must merge): %+v", len(hits), hits)
	}
	hit := hits[0]
	if len(hit.Sources) != n {
		t.Fatalf("sources = %d, want exactly %d (one per worker, no lost writes, no duplicate edges): %+v",
			len(hit.Sources), n, hit.Sources)
	}
	// Complete regardless of arrival order (SWARM-07 "ordering" backstop) --
	// the SET of run ids is asserted, never a specific position or sequence.
	seen := make(map[string]bool, n)
	for _, source := range hit.Sources {
		if source.WriterRole != WriterWorker {
			t.Fatalf("source %+v carries WriterRole %q, want %q -- no fact may be attributed to the parent",
				source, source.WriterRole, WriterWorker)
		}
		seen[source.RunID] = true
	}
	for _, runID := range runIDs {
		if !seen[runID] {
			t.Fatalf("worker run id %q is missing from the merged fact's sources: %+v", runID, hit.Sources)
		}
	}
}

// Cross-attribution: N goroutines with N DIFFERENT worker actors, each
// learning something DIFFERENT, must produce N facts -- one per actor, each
// carrying its OWN writer's run id. None may carry another worker's id and
// none may carry the parent's, proving concurrent fan-out attributes writes
// correctly rather than merely not losing them.
func TestConcurrentWorkerFactWriteDistinctActorsProduceDistinctFacts(t *testing.T) {
	defer goleak.VerifyNone(t)
	client := integrationClient(t)
	defer client.http.CloseIdleConnections()
	subject, parentRunID := isolate(t, client)

	const n = 6
	runIDs := make([]string, n)
	for i := range n {
		runIDs[i] = fmt.Sprintf("%s-w%d", subject, i)
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			object := fmt.Sprintf("%s_t%d", subject, i)
			_, err := client.UpsertFact(context.Background(), Fact{
				Subject: subject, Predicate: "learned_lesson", Object: object,
				Statement: fmt.Sprintf("%s learned distinct topic %d.", subject, i),
				Source:    FactSource{RunID: runIDs[i], WriterRole: WriterWorker},
			}, time.Now())
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: UpsertFact: %v", i, err)
		}
	}

	hits, err := client.FactsAbout(context.Background(), subject, "learned_lesson", 20, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(hits) != n {
		t.Fatalf("facts = %d, want exactly %d (one per distinct worker, no cross-merge): %+v", len(hits), n, hits)
	}

	byRunID := make(map[string]FactHit, n)
	for _, hit := range hits {
		if len(hit.Sources) != 1 {
			t.Fatalf("fact %+v carries %d sources, want exactly 1 (distinct content must not merge)", hit, len(hit.Sources))
		}
		source := hit.Sources[0]
		if source.RunID == parentRunID {
			t.Fatalf("fact %+v is attributed to the parent run id %q; a worker's write must never be", hit, parentRunID)
		}
		if source.WriterRole != WriterWorker {
			t.Fatalf("fact %+v carries WriterRole %q, want %q", hit, source.WriterRole, WriterWorker)
		}
		byRunID[source.RunID] = hit
	}
	for _, runID := range runIDs {
		if _, ok := byRunID[runID]; !ok {
			t.Fatalf("worker run id %q produced no attributed fact; facts = %+v", runID, hits)
		}
	}
}
