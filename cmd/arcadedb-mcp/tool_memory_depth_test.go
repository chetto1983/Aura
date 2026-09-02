package main

// The depth contract on memory_facts_about. Separate from
// tool_memory_retrieval_test.go because the property under test is not what the
// tool returns but which STATEMENT it emits: depth 1 is the path taken whenever a
// question names an entity, and it must survive the arrival of a second hop
// untouched. Shares newRecordingDB/oneFactRow with the rest of the package.

import (
	"strings"
	"testing"
)

// The absent field and an explicit 1 must reach exactly the same SQL, and it must
// be the SQL that shipped: one hop, matching either endpoint by name, with no
// ordering clause. A regression here is worse than having no second hop at all.
func TestFactsAboutDepthOneEmitsTheStatementThatShipped(t *testing.T) {
	client, absent := newRecordingDB(t, oneFactRow)
	if _, err := factsAbout(t, client, MemoryFactsAboutInput{Entity: "Davide"}); err != nil {
		t.Fatalf("facts_about: %v", err)
	}
	client, explicit := newRecordingDB(t, oneFactRow)
	if _, err := factsAbout(t, client, MemoryFactsAboutInput{
		Entity: "Davide", Depth: new(1),
	}); err != nil {
		t.Fatalf("facts_about depth 1: %v", err)
	}
	if len(absent.statements) != 1 || len(explicit.statements) != 1 {
		t.Fatalf("statements = %d and %d, want one each",
			len(absent.statements), len(explicit.statements))
	}
	if absent.statements[0] != explicit.statements[0] {
		t.Fatalf("an absent depth and an explicit 1 differ:\n absent: %s\nexplicit: %s",
			absent.statements[0], explicit.statements[0])
	}
	statement := absent.statements[0]
	if !strings.Contains(statement, "outV().name = :entity") ||
		!strings.Contains(statement, "inV().name = :entity") {
		t.Fatalf("depth 1 no longer walks both endpoints by name: %s", statement)
	}
	if strings.Contains(statement, "MENTIONS") || strings.Contains(statement, "TRAVERSE") {
		t.Fatalf("depth 1 reached into the second hop: %s", statement)
	}
	if strings.Contains(statement, "ORDER BY") {
		t.Fatalf("depth 1 gained an ordering it never had: %s", statement)
	}
}

func TestFactsAboutDepthTwoTraversesMentionsAndOrdersItsRows(t *testing.T) {
	client, rec := newRecordingDB(t, oneFactRow)
	out, err := factsAbout(t, client, MemoryFactsAboutInput{
		Entity: "Davide", Depth: new(2),
	})
	if err != nil {
		t.Fatalf("facts_about depth 2: %v", err)
	}
	statement := rec.statements[0]
	for _, want := range []string{
		"TRAVERSE both('MENTIONS')", "WHILE $depth <= 2", "ORDER BY created_at DESC",
	} {
		if !strings.Contains(statement, want) {
			t.Fatalf("depth 2 statement is missing %q: %s", want, statement)
		}
	}
	// The valid-time condition must survive the hop, or a superseded fact becomes
	// reachable through a neighbour when it is unreachable directly.
	if !strings.Contains(statement, "valid_to") || rec.params[0]["as_of"] == nil {
		t.Fatalf("depth 2 dropped valid-time: %s params=%v", statement, rec.params[0])
	}
	if !strings.Contains(statement, "LIMIT") {
		t.Fatalf("depth 2 is unbounded: %s", statement)
	}
	// The caller must be able to tell the two hops apart in the reply, or a wider
	// answer is indistinguishable from a lucky one.
	if out.Retrieval.Path != "mentions" {
		t.Fatalf("retrieval path = %q, want %q", out.Retrieval.Path, "mentions")
	}
}

// Refused, not clamped. A silently clamped 3 answers a question the caller did
// not ask and gives it nothing to notice.
func TestFactsAboutRefusesADepthItDoesNotHave(t *testing.T) {
	for _, depth := range []int{0, 3, -1, 99} {
		client, rec := newRecordingDB(t, oneFactRow)
		_, err := factsAbout(t, client, MemoryFactsAboutInput{
			Entity: "Davide", Depth: new(depth),
		})
		if err == nil {
			t.Fatalf("depth %d was accepted", depth)
		}
		if !strings.Contains(err.Error(), "depth must be 1") {
			t.Fatalf("depth %d error is not model-readable: %v", depth, err)
		}
		if len(rec.statements) != 0 {
			t.Fatalf("depth %d reached the database before being refused: %v",
				depth, rec.statements)
		}
	}
}

// The two constants and the tool's own validation must agree. They are declared
// in different packages, so nothing but a test couples them.
func TestFactsAboutDepthDefaultsToOne(t *testing.T) {
	got, err := factsAboutDepth(nil)
	if err != nil || got != 1 {
		t.Fatalf("factsAboutDepth(nil) = %d, %v; want 1, nil", got, err)
	}
	if path := factsAboutPath(1); path != "graph" {
		t.Fatalf("depth 1 path = %q, want the unchanged %q", path, "graph")
	}
}
