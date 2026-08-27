//go:build arcadedb_integration

// Live-graph proof for the supersede concern (memory_supersede.go): an
// exact-match close touches only the fact it names (D-15), the legacy
// ambiguity contract refuses rather than guesses (D-16), and F-2's own
// eight-fact shape is replayed and survives. Split out of
// memory_integration_test.go, which the file-size gate caps at 600 LOC --
// the same reasoning memory_supersede.go itself documents.
//
// Run: go test -tags arcadedb_integration ./internal/arcadedb/
package arcadedb

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// D-15/SC#4: an explicit fact_key closes exactly the one edge it names.
// Siblings sharing the same subject and predicate -- the shape F-2 destroyed
// -- must survive untouched, and the closed edge stays queryable via as_of.
func TestFactKeyClosesOnlyTheNamedSibling(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	// Business time spread by minutes/hours, not real wall-clock gaps: valid_from
	// and valid_to travel the wire as RFC3339 with SECOND granularity, so two
	// timestamps captured milliseconds apart in a fast test round to the same
	// second and a strict `>`/`<=` boundary check goes the wrong way. The
	// existing live analog (TestSupersessionClosesTheWindowAndKeepsThePastQueryable)
	// avoids this the same way: years apart, never relying on sub-second order.
	now := time.Now()
	learned := now.Add(-time.Hour) // when the siblings were learned
	corrected := now               // when the correction happens
	wellInsideTheWindow := now.Add(-30 * time.Minute)

	var keys [3]string
	for i, lesson := range []string{"first lesson", "second lesson", "third lesson"} {
		object := fmt.Sprintf("%s_lesson_%d", subject, i)
		write(t, client, Fact{
			Subject: subject, Predicate: "learned_lesson", Object: object,
			Statement: subject + " learned: " + lesson,
			Source:    FactSource{RunID: runID, WriterRole: WriterParent}, ValidFrom: learned,
		}, now)
		hits, err := client.FactsAbout(context.Background(), subject, "learned_lesson", 10, time.Time{})
		if err != nil {
			t.Fatalf("FactsAbout: %v", err)
		}
		hit, ok := findByObject(hits, object)
		if !ok || hit.FactKey == "" {
			t.Fatalf("fact %d has no fact_key after write: %+v", i, hits)
		}
		keys[i] = hit.FactKey
	}

	// Close only the first sibling by its exact fact_key.
	written, err := client.UpsertFact(context.Background(), Fact{
		Subject: subject, Predicate: "learned_lesson", Object: subject + "_lesson_0_corrected",
		Statement: subject + " learned: first lesson, corrected",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent}, ValidFrom: corrected,
		Supersedes: true, TargetFactKey: keys[0],
	}, now)
	if err != nil {
		t.Fatalf("UpsertFact by fact_key: %v", err)
	}
	if written.Refused {
		t.Fatalf("written = %+v, want a clean close", written)
	}
	if written.Superseded != 1 {
		t.Fatalf("superseded = %d, want exactly 1", written.Superseded)
	}

	present, err := client.FactsAbout(context.Background(), subject, "learned_lesson", 10, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout after close: %v", err)
	}
	// N-1 siblings survive open: the second and third lessons, plus the
	// corrected replacement -- three still-open facts.
	openCount := 0
	for _, hit := range present {
		if hit.ValidTo == "" {
			openCount++
		}
	}
	if openCount != 3 {
		t.Fatalf("open facts = %d, want 3 (two untouched siblings + the correction): %+v", openCount, present)
	}
	if _, ok := findByObject(present, subject+"_lesson_1"); !ok {
		t.Fatalf("sibling 1 was closed by a targeted correction naming sibling 0: %+v", present)
	}
	if _, ok := findByObject(present, subject+"_lesson_2"); !ok {
		t.Fatalf("sibling 2 was closed by a targeted correction naming sibling 0: %+v", present)
	}

	// The closed fact stays queryable in the past -- closed, not deleted.
	past, err := client.FactsAbout(context.Background(), subject, "learned_lesson", 10, wellInsideTheWindow)
	if err != nil {
		t.Fatalf("FactsAbout(as_of): %v", err)
	}
	closedHit, ok := findByObject(past, subject+"_lesson_0")
	if !ok {
		t.Fatalf("the closed fact is no longer queryable as_of its own valid_from: %+v", past)
	}
	if closedHit.ValidTo == "" {
		t.Fatal("closed fact has no valid_to: the window was not closed")
	}
}

// D-15: a fact_key naming no still-valid edge closes nothing and never falls
// back to the broad subject+predicate match -- that fallback is F-2's defect.
func TestUpsertFactWithUnknownFactKeyRefusesAgainstALiveGraph(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()

	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement: subject + " lives in Torino.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
	}, now)

	written, err := client.UpsertFact(context.Background(), Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Caraglio",
		Statement:  subject + " lives in Caraglio.",
		Source:     FactSource{RunID: runID, WriterRole: WriterParent},
		Supersedes: true, TargetFactKey: "fact-key-that-does-not-exist",
	}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if !written.Refused {
		t.Fatalf("written = %+v, want a refusal for an unknown fact_key", written)
	}
	if written.Superseded != 0 {
		t.Fatalf("superseded = %d, want 0", written.Superseded)
	}

	present, err := client.FactsAbout(context.Background(), subject, "lives_in", 10, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	// The original Torino fact must still be open, and the Caraglio
	// correction must never have been written -- a refusal writes nothing.
	if len(present) != 1 || present[0].ValidTo != "" || !strings.Contains(present[0].Statement, "Torino") {
		t.Fatalf("present facts = %+v, want only the untouched original", present)
	}
}

// D-16: the legacy blanket path resolves candidates before closing. Zero
// still-valid facts share this subject+predicate -- there is nothing to
// supersede -- so it refuses rather than silently creating an unlinked new
// fact next to nothing.
func TestSupersedeWithNoCandidatesRefusesAgainstALiveGraph(t *testing.T) {
	client := integrationClient(t)
	subject, _ := isolate(t, client)
	now := time.Now()

	written, err := client.UpsertFact(context.Background(), Fact{
		Subject: subject, Predicate: "learned_lesson", Object: subject + "_lesson",
		Statement:  subject + " learned a lesson.",
		Source:     FactSource{RunID: "it-" + subject, WriterRole: WriterParent},
		Supersedes: true,
	}, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if !written.Refused {
		t.Fatalf("written = %+v, want a refusal: no prior fact shares this subject+predicate", written)
	}
	if written.Superseded != 0 {
		t.Fatalf("superseded = %d, want 0", written.Superseded)
	}

	present, err := client.FactsAbout(context.Background(), subject, "learned_lesson", 10, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(present) != 0 {
		t.Fatalf("a refused correction must write nothing: %+v", present)
	}
}

// D-16/SC#4/F-2 replay: eight learned_lesson facts share subject and
// predicate -- F-2's exact shape, which the broad blind match closed all
// eight of to correct one. A blanket correction with no fact_key must
// refuse with eight previews, and EVERY fact -- all eight learned_lesson
// siblings AND the unrelated lives_in fact on the same subject -- must
// still be open afterwards. Asserted per-fact, not by a returned count: a
// count is exactly what hid the original damage.
func TestSupersedeReplaysF2EightFactsRefused(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()

	const lessonCount = 8
	for i := range lessonCount {
		write(t, client, Fact{
			Subject: subject, Predicate: "learned_lesson",
			Object:    fmt.Sprintf("%s_lesson_%d", subject, i),
			Statement: fmt.Sprintf("%s learned lesson number %d.", subject, i),
			Source:    FactSource{RunID: runID, WriterRole: WriterParent},
		}, now)
	}
	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement: subject + " lives in Torino.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
	}, now)

	written, err := client.UpsertFact(context.Background(), Fact{
		Subject: subject, Predicate: "learned_lesson", Object: subject + "_lesson_blanket_correction",
		Statement: subject + " learned a lesson, corrected.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent}, Supersedes: true,
	}, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if !written.Refused {
		t.Fatalf("written = %+v, want a refusal: %d distinct candidates matched", written, lessonCount)
	}
	if written.Superseded != 0 {
		t.Fatalf("superseded = %d, want 0", written.Superseded)
	}
	if len(written.Candidates) != lessonCount {
		t.Fatalf("candidates = %d, want %d previews", len(written.Candidates), lessonCount)
	}

	lessons, err := client.FactsAbout(context.Background(), subject, "learned_lesson", 20, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout(learned_lesson): %v", err)
	}
	if len(lessons) != lessonCount {
		t.Fatalf("learned_lesson facts = %d, want exactly %d (no blanket correction written)", len(lessons), lessonCount)
	}
	for i := range lessonCount {
		object := fmt.Sprintf("%s_lesson_%d", subject, i)
		hit, ok := findByObject(lessons, object)
		if !ok {
			t.Fatalf("lesson %d is missing entirely: %+v", i, lessons)
		}
		if hit.ValidTo != "" {
			t.Fatalf("lesson %d was closed by a refused correction: %+v", i, hit)
		}
	}

	livesIn, err := client.FactsAbout(context.Background(), subject, "lives_in", 10, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout(lives_in): %v", err)
	}
	if len(livesIn) != 1 || livesIn[0].ValidTo != "" {
		t.Fatalf("the unrelated lives_in fact was touched by a learned_lesson correction: %+v", livesIn)
	}
}

func findByObject(hits []FactHit, object string) (FactHit, bool) {
	for _, hit := range hits {
		if hit.Object == object {
			return hit, true
		}
	}
	return FactHit{}, false
}
