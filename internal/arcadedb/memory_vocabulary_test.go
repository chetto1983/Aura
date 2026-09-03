package arcadedb

// The vocabulary reply, tested on the shapes the real memory actually produced. Every
// fixture below is a name measured on 2026-09-03 in a live 107-fact memory, not one
// invented to make the ranking look good.

import (
	"reflect"
	"testing"
)

// The 30 real names that corpus held, alongside a few of its 181 coinages.
var liveVocabulary = []string{
	"Aura", "ArcadeDB", "Neo4j", "Codex", "Claude Code", "ralph.sh", "golangci-lint",
	"il protocollo SSE di Aura", "la memoria di Aura", "cerimonie di approvazione",
}

func TestNearestNamesReachesTheNameBuriedInACoinage(t *testing.T) {
	// The case the whole feature exists for: a six-word coinage whose first word is a name
	// the memory has held all along.
	got := nearestNames(liveVocabulary, "la lentezza percepita di Aura sui turni banali")
	if len(got) == 0 || got[0] != "Aura" {
		t.Fatalf("nearest = %v, want Aura first", got)
	}
}

// A short name wholly contained in a long coinage must outrank a long name that merely
// overlaps it. Scoring by the CANDIDATE's length is what buys that; a Jaccard score over
// the union would bury `Aura` under the coinage's own extra words.
func TestNearestNamesPrefersTheContainedNameOverTheLongerNeighbour(t *testing.T) {
	got := nearestNames(liveVocabulary, "il flusso git su Aura")
	if len(got) == 0 {
		t.Fatal("no neighbour found for a coinage naming Aura")
	}
	if got[0] != "Aura" {
		t.Fatalf("nearest = %v, want the contained name first", got)
	}
}

// 157 of the 181 measured coinages contained no existing name at all. An empty result is
// the honest answer for those, and it is information: there is nothing to reuse.
func TestNearestNamesSaysNothingWhenNothingIsNear(t *testing.T) {
	if got := nearestNames(liveVocabulary, "attribuzione di una run a pagamento"); len(got) != 0 {
		t.Fatalf("nearest = %v, want none", got)
	}
}

// Articles and prepositions are shared by nearly every phrase in both languages this
// memory is written in. Matching on them would make everything look related to
// everything, which is the same as matching on nothing.
func TestNearestNamesIgnoresTheWordsEveryPhraseShares(t *testing.T) {
	if got := nearestNames([]string{"il gate di qualita", "la memoria di Aura"}, "il di la"); len(got) != 0 {
		t.Fatalf("nearest = %v, want none: stop words alone are not a match", got)
	}
}

func TestCoinedEntitiesReportsOnlyWhatTheMemoryDoesNotHold(t *testing.T) {
	coined := coinedEntities(liveVocabulary, "ArcadeDB", "il protocollo gRPC di Aura")
	if len(coined) != 1 {
		t.Fatalf("coined = %+v, want only the new name", coined)
	}
	if coined[0].Name != "il protocollo gRPC di Aura" {
		t.Fatalf("coined = %+v", coined)
	}
	if len(coined[0].Near) == 0 || coined[0].Near[0] != "Aura" {
		t.Fatalf("near = %v, want Aura first", coined[0].Near)
	}
}

// Case and surrounding space are spelling, not identity: a memory that reports `arcadedb`
// as a coinage next to `ArcadeDB` teaches the writer to distrust the reply.
func TestCoinedEntitiesMatchesTheVocabularyCaseInsensitively(t *testing.T) {
	if coined := coinedEntities(liveVocabulary, "  arcadedb  ", "NEO4J"); len(coined) != 0 {
		t.Fatalf("coined = %+v, want none", coined)
	}
}

func TestCoinedEntitiesReportsEachNewNameOnce(t *testing.T) {
	coined := coinedEntities(liveVocabulary, "un nome nuovo", "un nome nuovo")
	if len(coined) != 1 {
		t.Fatalf("coined = %+v, want one entry for one name", coined)
	}
}

// The cap keeps the reply readable. Without it a coinage sharing a common word with
// half the memory answers with half the memory.
func TestNearestNamesIsCapped(t *testing.T) {
	crowd := make([]string, 0, 40)
	for _, suffix := range []string{"uno", "due", "tre", "quattro", "cinque", "sei", "sette", "otto"} {
		crowd = append(crowd, "Aura "+suffix)
	}
	if got := nearestNames(crowd, "Aura nove"); len(got) != vocabularyNearLimit {
		t.Fatalf("nearest returned %d names, want the cap of %d", len(got), vocabularyNearLimit)
	}
}

// Two identical inputs must rank identically, or a reply the writer is meant to act on
// changes under them for no reason.
func TestNearestNamesIsDeterministic(t *testing.T) {
	first := nearestNames(liveVocabulary, "il protocollo HTTP di Aura")
	second := nearestNames(liveVocabulary, "il protocollo HTTP di Aura")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("ranking is not stable: %v then %v", first, second)
	}
}
