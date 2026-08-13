package arcadedb

// Unit tier for the supersede concern (memory_supersede.go): the exact-match
// close, and the legacy ambiguity contract (D-16) -- resolve the candidate
// set first, then refuse on 0 or >1 distinct matches, close on exactly 1.
// Split out of memory_test.go, which the file-size gate caps at 600 LOC.

import (
	"context"
	"strings"
	"testing"
)

func TestUpsertFactSupersedesByClosingTheWindow(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.HasPrefix(statement, "UPDATE FACT SET valid_to"):
			return testResponse{Body: `{"result":[{"count":2}]}`}
		case strings.Contains(statement, "outV().name = :entity OR inV().name = :entity"):
			// D-16: the legacy path resolves candidates before closing.
			// Exactly one still-valid match lets the close proceed --
			// the mocked UPDATE above is what actually reports the count
			// this test asserts on, unchanged from before D-16 landed.
			return testResponse{Body: oneFactRowWithKey}
		default:
			return testResponse{Body: `{"result":[]}`}
		}
	})
	fact := validFact()
	fact.Supersedes = true
	written, err := client.UpsertFact(context.Background(), fact, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if written.Refused {
		t.Fatalf("written = %+v, want a clean close: exactly one candidate resolved", written)
	}
	if written.Superseded != 2 {
		t.Fatalf("superseded = %d, want 2", written.Superseded)
	}
	statements := make([]string, 0, len(*requests))
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		statements = append(statements, statement)
	}
	all := strings.Join(statements, "\n")
	if strings.Contains(strings.ToUpper(all), "DELETE") {
		t.Fatalf("supersession must never delete:\n%s", all)
	}
	if !strings.Contains(all, "valid_to IS NULL OR valid_to > :valid_to") ||
		!strings.Contains(all, "expired_at IS NULL") {
		t.Fatalf("only facts active at the replacement instant may be closed:\n%s", all)
	}
	if !strings.Contains(all, "fact_key = NULL") {
		t.Fatalf("supersession did not release the active identity key:\n%s", all)
	}
	// outV(), not the dotted form: on an edge `out.name` yields NULL rather
	// than erroring, so the statement would match nothing, silently.
	if !strings.Contains(all, "outV().name = :subject_name") {
		t.Fatalf("supersession must match the subject via outV():\n%s", all)
	}
	// The object is the thing that changed; requiring it means this never fires.
	if strings.Contains(all, "inV().name = :object_name") {
		t.Fatalf("supersession must not filter on the object:\n%s", all)
	}
}

// D-15: an explicit fact_key closes exactly the one edge it names, skipping
// candidate resolution entirely -- it never falls back to the broad
// subject+predicate match.
func TestUpsertFactClosesExactlyOneFactByTargetFactKey(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "fact_key = :target_fact_key") {
			return testResponse{Body: `{"result":[{"count":1}]}`}
		}
		return testResponse{Body: `{"result":[]}`}
	})
	fact := validFact()
	fact.Supersedes = true
	fact.TargetFactKey = "target-key-1"
	written, err := client.UpsertFact(context.Background(), fact, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if written.Refused {
		t.Fatalf("written = %+v, want a clean close, not a refusal", written)
	}
	if written.Superseded != 1 {
		t.Fatalf("superseded = %d, want 1", written.Superseded)
	}
	statements := make([]string, 0, len(*requests))
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		statements = append(statements, statement)
	}
	all := strings.Join(statements, "\n")
	if !strings.Contains(all, "fact_key = :target_fact_key") {
		t.Fatalf("exact-match close statement never issued:\n%s", all)
	}
	// The broad statement's own self-exclusion clause -- must not fire
	// alongside the exact-match close.
	if strings.Contains(all, "fact_key <> :fact_key") {
		t.Fatalf("an explicit target must skip the broad-match statement entirely:\n%s", all)
	}
	if !strings.Contains(all, "expired_at IS NULL") {
		t.Fatalf("exact-match close must only touch a still-valid fact:\n%s", all)
	}
}

// An explicit fact_key naming nothing still-valid closes 0, refuses, and
// never falls back to the broad match -- the fallback is what F-2 needs
// this plan to remove, not relocate.
func TestUpsertFactWithUnknownTargetFactKeyRefusesAndWritesNothing(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "fact_key = :target_fact_key") {
			return testResponse{Body: `{"result":[{"count":0}]}`}
		}
		return testResponse{Body: `{"result":[]}`}
	})
	fact := validFact()
	fact.Supersedes = true
	fact.TargetFactKey = "does-not-exist"
	written, err := client.UpsertFact(context.Background(), fact, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if !written.Refused {
		t.Fatalf("written = %+v, want a refusal for an unknown fact_key", written)
	}
	if written.Superseded != 0 {
		t.Fatalf("superseded = %d, want 0", written.Superseded)
	}
	if written.Reason == "" {
		t.Fatal("a refusal must explain why")
	}
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.HasPrefix(statement, "CREATE EDGE") {
			t.Fatalf("a refused correction must write nothing, but the new fact was created: %s", statement)
		}
		if strings.Contains(statement, "outV().name = :subject_name") {
			t.Fatalf("an explicit target must never fall back to the broad-match statement: %s", statement)
		}
	}
}

func TestUpsertFactWithoutSupersedesLeavesPriorFactsAlone(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if _, err := client.UpsertFact(context.Background(), validFact(), now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	// The assertion is on the UPDATE, not on the string "expired_at". It used to
	// look for `expired_at = :expired_at`, which was a fair proxy while only the
	// supersede statement set that column -- until createFactStatement started
	// setting it too, so a merge could carry a closed fact's expiry across when it
	// re-points the edge. The proxy became ambiguous; the behaviour did not.
	for _, statement := range rec.statements {
		if strings.HasPrefix(strings.TrimSpace(statement), "UPDATE "+factEdgeType+" SET valid_to") {
			t.Fatalf("no supersede UPDATE should be issued by default:\n%s", statement)
		}
	}
}

// D-16: with no explicit fact_key, Supersedes:true first resolves the
// subject+predicate candidate set. Zero matches refuses -- never a silent
// no-op success, and never a blind close of nothing found.
func TestUpsertFactSupersedesRefusesOnZeroCandidates(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	fact := validFact()
	fact.Supersedes = true
	written, err := client.UpsertFact(context.Background(), fact, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if !written.Refused {
		t.Fatalf("written = %+v, want a refusal: no candidate matched", written)
	}
	if written.Superseded != 0 {
		t.Fatalf("superseded = %d, want 0", written.Superseded)
	}
	if written.Reason == "" {
		t.Fatal("a refusal must explain why")
	}
	if len(written.Candidates) != 0 {
		t.Fatalf("candidates = %+v, want none", written.Candidates)
	}
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.HasPrefix(statement, "UPDATE "+factEdgeType+" SET valid_to") {
			t.Fatalf("zero candidates must never reach the close statement:\n%s", statement)
		}
		if strings.HasPrefix(statement, "CREATE EDGE") {
			t.Fatalf("a refused correction must write nothing, but the new fact was created: %s", statement)
		}
	}
}

// Exactly one candidate closes it -- behaviour identical to today's single-
// valued case, now reached via resolution rather than a blind match. The
// resolution SELECT itself (factsAboutStatement's shape) must be issued --
// that is what distinguishes this from the pre-D-16 unconditional close.
func TestUpsertFactSupersedesClosesOnExactlyOneCandidate(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "outV().name = :entity OR inV().name = :entity"):
			return testResponse{Body: oneFactRowWithKey}
		case strings.HasPrefix(statement, "UPDATE FACT SET valid_to"):
			return testResponse{Body: `{"result":[{"count":1}]}`}
		default:
			return testResponse{Body: `{"result":[]}`}
		}
	})
	fact := validFact()
	fact.Supersedes = true
	written, err := client.UpsertFact(context.Background(), fact, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if written.Refused {
		t.Fatalf("written = %+v, want a clean close", written)
	}
	if written.Superseded != 1 {
		t.Fatalf("superseded = %d, want 1", written.Superseded)
	}
	resolved := false
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "outV().name = :entity OR inV().name = :entity") {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("the candidate set was never resolved before closing")
	}
}

// More than one distinct candidate refuses -- F-2's shape. Distinct means a
// different fact_key; this is never resolved by guessing (no recency
// tie-break, no similarity score, no cardinality registry -- Pitfall 6).
func TestUpsertFactSupersedesRefusesOnMultipleDistinctCandidates(t *testing.T) {
	twoCandidates := `{"result":[
		{"statement":"a","predicate":"learned_lesson","subject":"Davide","object":"o1","valid_from":"2026-01-01T00:00:00Z","fact_key":"key-1","sources":[]},
		{"statement":"b","predicate":"learned_lesson","subject":"Davide","object":"o2","valid_from":"2026-01-01T00:00:00Z","fact_key":"key-2","sources":[]}
	]}`
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "outV().name = :entity OR inV().name = :entity") {
			return testResponse{Body: twoCandidates}
		}
		return testResponse{Body: `{"result":[]}`}
	})
	fact := validFact()
	fact.Supersedes = true
	written, err := client.UpsertFact(context.Background(), fact, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if !written.Refused {
		t.Fatalf("written = %+v, want a refusal: two distinct candidates matched", written)
	}
	if written.Superseded != 0 {
		t.Fatalf("superseded = %d, want 0", written.Superseded)
	}
	if len(written.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want exactly 2 previews", written.Candidates)
	}
	if !strings.Contains(written.Reason, "supersedes_fact_key") {
		t.Fatalf("reason = %q, want it to name supersedes_fact_key as the disambiguation path", written.Reason)
	}
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.HasPrefix(statement, "UPDATE "+factEdgeType+" SET valid_to") {
			t.Fatalf("an ambiguous correction must never reach the close statement:\n%s", statement)
		}
		if strings.HasPrefix(statement, "CREATE EDGE") {
			t.Fatalf("a refused correction must write nothing, but the new fact was created: %s", statement)
		}
	}
}
