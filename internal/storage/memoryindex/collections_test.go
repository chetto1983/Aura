package memoryindex

import (
	"strings"
	"testing"
)

// TestCollectionConstants verifies that all 6 Collection constants resolve to
// Registry entries and that the 3 historical Kind strings match exactly.
func TestCollectionConstants(t *testing.T) {
	all := []Collection{
		CollectionWiki,
		CollectionSource,
		CollectionArchive,
		CollectionProposal,
		CollectionUserMemory,
		CollectionOperational,
	}

	if len(Registry) != len(all) {
		t.Fatalf("Registry has %d entries, want %d", len(Registry), len(all))
	}

	for _, c := range all {
		desc, ok := Registry[c]
		if !ok {
			t.Errorf("Registry missing entry for Collection %q", c)
			continue
		}
		if desc.Kind != c {
			t.Errorf("descriptor Kind = %q, want %q", desc.Kind, c)
		}
	}
}

// TestKindBackCompat asserts that the 3 historical untyped string constants
// exactly equal the corresponding typed Collection values.
func TestKindBackCompat(t *testing.T) {
	if KindSource != string(CollectionSource) {
		t.Errorf("KindSource %q != string(CollectionSource) %q", KindSource, string(CollectionSource))
	}
	if KindArchive != string(CollectionArchive) {
		t.Errorf("KindArchive %q != string(CollectionArchive) %q", KindArchive, string(CollectionArchive))
	}
	if KindProposal != string(CollectionProposal) {
		t.Errorf("KindProposal %q != string(CollectionProposal) %q", KindProposal, string(CollectionProposal))
	}
	if KindOperational != string(CollectionOperational) {
		t.Errorf("KindOperational %q != string(CollectionOperational) %q", KindOperational, string(CollectionOperational))
	}
}

// TestScaffoldedCollections verifies that both UserMemory (Phase-O US-O03) and
// Operational (Phase-N US-N03) are now wired with a StorageBackend, and that
// neither description still says "scaffolded".
func TestScaffoldedCollections(t *testing.T) {
	for _, c := range []Collection{CollectionUserMemory, CollectionOperational} {
		desc, ok := Registry[c]
		if !ok {
			t.Fatalf("Registry missing %q", c)
		}
		if desc.StorageBackend == "" {
			t.Errorf("%q StorageBackend is empty — should be wired", c)
		}
		lower := strings.ToLower(desc.Description)
		if strings.Contains(lower, "scaffolded") {
			t.Errorf("%q Description still says 'scaffolded' after wiring: %q", c, desc.Description)
		}
	}
}

// TestLookup verifies the Lookup helper for known and unknown collections.
func TestLookup(t *testing.T) {
	d, ok := Lookup(CollectionWiki)
	if !ok {
		t.Fatal("Lookup(CollectionWiki) returned ok=false")
	}
	if d.Kind != CollectionWiki {
		t.Errorf("Lookup returned wrong Kind: %q", d.Kind)
	}

	_, ok = Lookup("no_such_collection")
	if ok {
		t.Error("Lookup(unknown) should return ok=false")
	}
}
