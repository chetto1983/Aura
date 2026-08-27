package arcadedb

import (
	"context"
	"strings"
	"testing"
)

// TestMaySupersedeRefusesWorkerAllowsParent pins D-11's whole authority
// question: a worker may never close a fact, the parent turn/operator
// always may. The reason is model-readable prose naming the alternative
// (add a fact), not a Go error -- a worker asking to correct something is a
// request the model can act on, not a wiring bug.
func TestMaySupersedeRefusesWorkerAllowsParent(t *testing.T) {
	if allowed, reason := maySupersede(Actor{RunID: "run-w1", Role: WriterWorker}); allowed || reason == "" {
		t.Fatalf("worker: allowed=%v reason=%q, want refused with a non-empty reason", allowed, reason)
	}
	if allowed, reason := maySupersede(Actor{RunID: "run-p1", Role: WriterParent}); !allowed || reason != "" {
		t.Fatalf("parent: allowed=%v reason=%q, want allowed with no reason", allowed, reason)
	}
}

// TestUpsertFactRefusesWorkerSupersedeWithoutTouchingTheFact is D-11's live
// wiring proof: UpsertFact itself refuses BEFORE closeSuperseded ever runs,
// so zero Command calls are issued against the fact a worker asked to close
// -- not merely a refusal closeSuperseded decides to make on its own. The
// refused correction also creates nothing, matching closeSuperseded's own
// ambiguity-refusal shape (a refused correction never silently becomes a
// different write than the one asked for).
func TestUpsertFactRefusesWorkerSupersedeWithoutTouchingTheFact(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	fact := validFact()
	fact.Source.WriterRole = WriterWorker
	fact.Supersedes = true

	written, err := client.UpsertFact(context.Background(), fact, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if !written.Refused || strings.TrimSpace(written.Reason) == "" {
		t.Fatalf("written = %+v, want Refused=true with a reason", written)
	}
	if written.Superseded != 0 {
		t.Fatalf("Superseded = %d, want 0", written.Superseded)
	}
	for _, statement := range rec.statements {
		upper := strings.ToUpper(statement)
		if strings.Contains(upper, "SET VALID_TO") {
			t.Fatalf("a worker's refused supersede issued a close statement: %s", statement)
		}
		if strings.HasPrefix(upper, "CREATE EDGE") {
			t.Fatalf("a refused correction must create no fact: %s", statement)
		}
	}
}

// TestUpsertFactAllowsParentSupersede is the control case: the SAME refusal
// wiring must not fire for the parent/operator, or D-11 would have broken the
// legitimate correction path entirely rather than scoping it to workers. It
// mirrors TestUpsertFactSupersedesClosesOnExactlyOneCandidate's routing, but
// pins the WriterRole explicitly rather than relying on validFact's default.
func TestUpsertFactAllowsParentSupersede(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "outV().name = :entity OR inV().name = :entity"):
			return testResponse{Body: oneFactRowWithKey}
		case strings.HasPrefix(statement, "UPDATE "+factEdgeType+" SET valid_to"):
			return testResponse{Body: `{"result":[{"count":1}]}`}
		default:
			return testResponse{Body: `{"result":[]}`}
		}
	})
	fact := validFact()
	fact.Source.WriterRole = WriterParent
	fact.Supersedes = true

	written, err := client.UpsertFact(context.Background(), fact, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if written.Refused {
		t.Fatalf("written = %+v, want a parent's supersede allowed", written)
	}
	if written.Superseded != 1 {
		t.Fatalf("superseded = %d, want 1", written.Superseded)
	}
	found := false
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.HasPrefix(statement, "UPDATE "+factEdgeType+" SET valid_to") {
			found = true
		}
	}
	if !found {
		t.Fatal("no close statement issued for an allowed parent supersede")
	}
}

// TestFactValidateRejectsEmptyStatementOrSubjectBeforeAnyCommand is SWARM-07's
// "empty" edge (D-09): an upsert with an empty statement or empty subject is
// refused before any Command is issued -- zero network calls, not just a
// non-nil error.
func TestFactValidateRejectsEmptyStatementOrSubjectBeforeAnyCommand(t *testing.T) {
	cases := map[string]func(*Fact){
		"empty subject":   func(f *Fact) { f.Subject = "   " },
		"empty statement": func(f *Fact) { f.Statement = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			client, rec := recordingClient(t, `{"result":[]}`)
			fact := validFact()
			mutate(&fact)
			if _, err := client.UpsertFact(context.Background(), fact, now); err == nil {
				t.Fatal("expected a validation error")
			}
			if len(rec.statements) != 0 {
				t.Fatalf("a malformed fact must be rejected before anything is written; statements = %v", rec.statements)
			}
		})
	}
}

// TestFactValidateRejectsMissingOrUnknownWriterRole is D-10's own backstop at
// the internal/arcadedb layer: even a caller other than cmd/arcadedb-mcp that
// builds a Fact directly cannot write one with no actor, or a role that is
// neither of the two known values -- a wiring bug, never a silent write under
// an invented or blank role.
func TestFactValidateRejectsMissingOrUnknownWriterRole(t *testing.T) {
	for _, role := range []WriterRole{"", "operator", "WORKER"} {
		t.Run(string(role), func(t *testing.T) {
			client, rec := recordingClient(t, `{"result":[]}`)
			fact := validFact()
			fact.Source.WriterRole = role
			if _, err := client.UpsertFact(context.Background(), fact, now); err == nil {
				t.Fatalf("role %q was accepted, want a validation error", role)
			}
			if len(rec.statements) != 0 {
				t.Fatalf("statements = %v, want none written for an invalid role", rec.statements)
			}
		})
	}
}

// TestFactIdentityAdjacency is SWARM-07's "adjacency" edge (D-09): two facts
// whose content keys are exactly equal merge (same fact, two sources); two
// that merely share a subject remain two distinct facts. factIdentity is the
// content key UpsertFact/attachFactSource key on, so this is a pure-function
// pin of the boundary D-09's shipped dedup relies on -- not a rebuild of it.
func TestFactIdentityAdjacency(t *testing.T) {
	a := validFact()
	b := validFact()
	b.Source = FactSource{RunID: "run-2", WriterRole: WriterParent} // different source, same content
	if factIdentity(a) != factIdentity(b) {
		t.Fatalf("identical subject+predicate+object+statement produced different keys: %s != %s",
			factIdentity(a), factIdentity(b))
	}

	c := validFact()
	c.Predicate = "works_for"
	c.Object = "Aura"
	c.Statement = "Davide works for Aura."
	if factIdentity(a) == factIdentity(c) {
		t.Fatal("facts sharing only a subject collapsed onto the same key")
	}
}

// TestFactIdentityEncodingNormalization is SWARM-07's "encoding" backstop
// (D-09): factIdentity's own normalization is TrimSpace only, never case
// folding -- pinned explicitly so a future change to that normalization is
// caught here rather than assumed. If this test ever needs to change, the
// adjacency contract it protects changed with it.
func TestFactIdentityEncodingNormalization(t *testing.T) {
	trimmed := validFact()
	padded := validFact()
	padded.Subject = "  " + padded.Subject + "\t\n"
	padded.Statement = " " + padded.Statement + " "
	if factIdentity(trimmed) != factIdentity(padded) {
		t.Fatal("surrounding whitespace changed the fact identity; factIdentity must TrimSpace")
	}

	lower := validFact()
	lower.Subject = strings.ToLower(lower.Subject)
	if factIdentity(trimmed) == factIdentity(lower) {
		t.Fatal("factIdentity folded case; it must be case-sensitive (TrimSpace only) " +
			"-- if this is an intentional change, the adjacency tests above must be re-verified")
	}
}
