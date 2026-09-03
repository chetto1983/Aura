package arcadedb

// The class rules, tested on the kinds this memory actually held on 2026-09-03 rather than
// on invented ones. The eleven measured labels were Person, Project, Phase, Tool,
// Environment, System, Branch, Technique, Model, Dataset and Pattern.

import (
	"context"
	"strings"
	"testing"
)

func TestPoleClassForMapsEveryKindTheCorpusActuallyHeld(t *testing.T) {
	measured := map[string]string{
		"Person":      "Person",
		"Project":     "Object",
		"Phase":       "Event",
		"Tool":        "Object",
		"Environment": "Location",
		"System":      "Object",
		"Branch":      "Object",
		"Technique":   "Object",
		"Model":       "Object",
		"Dataset":     "Object",
		"Pattern":     "Object",
	}
	for kind, want := range measured {
		if got, refused := poleClassFor("", kind); got != want || refused {
			t.Errorf("poleClassFor(\"\", %q) = %q refused=%v, want %q", kind, got, refused, want)
		}
	}
}

// The point of a closed set is that it cannot be widened by a writer. A class nobody
// declared must land in Other AND be reported, or the caller learns nothing and the next
// write repeats it.
func TestPoleClassForRefusesAClassOutsideTheSet(t *testing.T) {
	got, refused := poleClassFor("Artefact", "Tool")
	if got != poleOther {
		t.Fatalf("class = %q, want %q", got, poleOther)
	}
	if !refused {
		t.Fatal("an unknown class was accepted silently, so the caller cannot correct it")
	}
}

// An explicit class beats the kind mapping: the writer knows more than the table does.
func TestPoleClassForPrefersTheExplicitClassOverTheKind(t *testing.T) {
	if got, _ := poleClassFor("Event", "Tool"); got != "Event" {
		t.Fatalf("class = %q, want the explicitly requested Event", got)
	}
}

func TestPoleClassForFoldsCase(t *testing.T) {
	if got, refused := poleClassFor("oRgAnIsAtIoN", ""); got != "Organisation" || refused {
		t.Fatalf("class = %q refused=%v, want canonical Organisation", got, refused)
	}
}

// An unmapped kind is not an error: Other is the honest answer, and the official POLE
// example ships the same escape hatch.
func TestPoleClassForFallsToOtherRatherThanGuessing(t *testing.T) {
	if got, refused := poleClassFor("", "Reticulated Splines"); got != poleOther || refused {
		t.Fatalf("class = %q refused=%v, want %q with no refusal", got, refused, poleOther)
	}
}

// Every class in the set must be declared as a subtype, or a write resolving to it hits a
// type that does not exist.
func TestPoleSchemaDeclaresEveryClassAsASubtypeOfEntity(t *testing.T) {
	statements := poleSchemaStatements()
	if len(statements) != len(poleClasses) {
		t.Fatalf("%d statements for %d classes", len(statements), len(poleClasses))
	}
	for i, class := range poleClasses {
		want := "CREATE VERTEX TYPE " + class + " IF NOT EXISTS EXTENDS Entity"
		if statements[i] != want {
			t.Fatalf("statement %d = %q, want %q", i, statements[i], want)
		}
	}
}

// The bare supertype must never be the write target. An entity minted as Entity carries no
// class at all, which is exactly the unclassified state the closed set exists to prevent.
func TestUpsertEntityInClassNamesTheSubtypeNotTheSupertype(t *testing.T) {
	for _, typed := range []bool{false, true} {
		statement := upsertEntityInClass("Location", typed)
		if !strings.HasPrefix(statement, "UPDATE Location ") {
			t.Fatalf("typed=%v: statement targets %q, want the subtype", typed, statement)
		}
		if !strings.Contains(statement, "UPSERT") {
			t.Fatalf("typed=%v: not an upsert: %s", typed, statement)
		}
	}
	if strings.Contains(upsertEntityInClass("Person", false), ":kind") {
		t.Fatal("an untyped upsert must not bind a kind it was not given")
	}
	if !strings.Contains(upsertEntityInClass("Person", true), "kind = :kind") {
		t.Fatal("a typed upsert must persist the kind alongside the class")
	}
}

func TestEntityClassScanReadsTheClassEachNameAlreadyHas(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[
		{"name":"Aura","pole":"Object"},
		{"name":"Operator","pole":"Person"}]}`)
	held, err := client.entityClassScan(context.Background(), "Operator", "Aura")
	if err != nil {
		t.Fatalf("entityClassScan: %v", err)
	}
	if held["Aura"] != "Object" || held["Operator"] != "Person" {
		t.Fatalf("held = %v", held)
	}
	if !strings.Contains(rec.statements[0], "@type AS pole") {
		t.Fatalf("the class is not projected: %s", rec.statements[0])
	}
}

// A fact whose subject and object are the same name must ask about it once. The duplicate
// would not be wrong, only wasteful, and the scan runs on every single write.
func TestEntityClassScanAsksOncePerDistinctName(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if _, err := client.entityClassScan(context.Background(), "Aura", "Aura", "  Aura  "); err != nil {
		t.Fatalf("entityClassScan: %v", err)
	}
	// The recorder round-trips params through JSON, so the bound list arrives as []any on
	// this path and as []string when the client is called directly. Count either.
	var bound int
	switch names := rec.params[0]["names"].(type) {
	case []string:
		bound = len(names)
	case []any:
		bound = len(names)
	default:
		t.Fatalf("names bound as %T, want a list", rec.params[0]["names"])
	}
	if bound != 1 {
		t.Fatalf("names = %v, want one Aura", rec.params[0]["names"])
	}
}

// Nothing to ask about must cost no query at all: an empty IN list is a round trip that
// can only return nothing.
func TestEntityClassScanSkipsTheQueryWhenThereIsNothingToAsk(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	held, err := client.entityClassScan(context.Background(), "", "   ")
	if err != nil {
		t.Fatalf("entityClassScan: %v", err)
	}
	if len(held) != 0 {
		t.Fatalf("held = %v, want empty", held)
	}
	if len(rec.statements) != 0 {
		t.Fatalf("issued %d statements for no names: %s", len(rec.statements), rec.joined())
	}
}

func TestPoleClassListNamesEveryAllowedClass(t *testing.T) {
	list := poleClassList()
	for _, class := range poleClasses {
		if !strings.Contains(list, class) {
			t.Fatalf("%q is allowed but absent from the caller-facing list %q", class, list)
		}
	}
}
