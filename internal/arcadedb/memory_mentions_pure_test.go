package arcadedb

import (
	"slices"
	"strings"
	"testing"
)

func TestWordBoundaryMatchingRefusesASubstringInsideALongerWord(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"inside a longer word", "mitigate", false},
		{"plural suffix", "gates", false},
		{"underscore compound", "gate_keeper", false},
		{"surrounded by spaces", "un gate qui", true},
		{"followed by a period", "the gate.", true},
		{"inside parentheses", "(gate)", true},
		{"at the very start of the text", "gate is here", true},
		{"at the very end of the text", "this is the gate", true},
		{"exact match, whole text", "gate", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := occursOnWordBoundary(tt.text, "gate"); got != tt.want {
				t.Fatalf("occursOnWordBoundary(%q, %q) = %v, want %v", tt.text, "gate", got, tt.want)
			}
		})
	}
}

func TestWordByteAtTreatsMultiByteUTF8AsWordCharacters(t *testing.T) {
	accented := "è" // 2-byte UTF-8 (0xC3 0xA8): both bytes must read as word bytes.
	tests := []struct {
		name  string
		text  string
		index int
		want  bool
	}{
		{"letter", "abc", 0, true},
		{"digit", "a5b", 1, true},
		{"underscore", "a_b", 1, true},
		{"space is not a word byte", "a b", 1, false},
		{"period is not a word byte", "a.b", 1, false},
		{"negative index is out of range", "abc", -1, false},
		{"index at length is out of range", "abc", 3, false},
		{"UTF-8 lead byte counts as a word byte", accented, 0, true},
		{"UTF-8 continuation byte counts as a word byte", accented, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wordByteAt(tt.text, tt.index); got != tt.want {
				t.Fatalf("wordByteAt(%q, %d) = %v, want %v", tt.text, tt.index, got, tt.want)
			}
		})
	}
}

// The accented neighbour must be read as part of a word, not as a boundary --
// see wordByteAt's comment. "mitigatè" doesn't even contain "gate" as a byte
// substring (the trailing letter is replaced, not appended); it is kept here
// because the acceptance criteria name it explicitly, and a substring miss is
// still a legitimate way to not match.
func TestOccursOnWordBoundaryTreatsAnAccentedNeighbourAsPartOfAWord(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"accent immediately before the match", "ègate"},
		{"accent immediately after the match", "gateè"},
		{"trailing letter swapped for an accented one", "mitigatè"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if occursOnWordBoundary(tt.text, "gate") {
				t.Fatalf("occursOnWordBoundary(%q, %q) = true, want false", tt.text, "gate")
			}
		})
	}
}

func TestScannerMatchingIsCaseInsensitive(t *testing.T) {
	scanner := newMentionScanner([]string{"Neo4j"})
	for _, text := range []string{"neo4j is fast", "NEO4J IS FAST", "Neo4j is fast"} {
		got := scanner.namesIn(text)
		if len(got) != 1 || got[0] != "Neo4j" {
			t.Fatalf("namesIn(%q) = %v, want [Neo4j]", text, got)
		}
	}
}

func TestNestedNamesAreMatchedIndependently(t *testing.T) {
	scanner := newMentionScanner([]string{"Claude", "Claude Code"})
	got := scanner.namesIn("Claude Code ran here")
	if len(got) != 2 || !slices.Contains(got, "Claude") || !slices.Contains(got, "Claude Code") {
		t.Fatalf("namesIn(%q) = %v, want both Claude and Claude Code", "Claude Code ran here", got)
	}
	got = scanner.namesIn("Claude runs")
	if len(got) != 1 || got[0] != "Claude" {
		t.Fatalf("namesIn(%q) = %v, want [Claude]", "Claude runs", got)
	}
}

func TestNamesInOmitsEntitiesTheCallerAlreadyOwns(t *testing.T) {
	scanner := newMentionScanner([]string{"Aura", "Davide"})
	text := "Aura talked to Davide today"
	if got := scanner.namesIn(text, "  AURA  ", "davide"); len(got) != 0 {
		t.Fatalf("namesIn(%q, owned both) = %v, want none", text, got)
	}
	got := scanner.namesIn(text, "Aura")
	if len(got) != 1 || got[0] != "Davide" {
		t.Fatalf("namesIn(%q, owned Aura) = %v, want [Davide]", text, got)
	}
}

func TestNameShapedAcceptsIdentifiersAndRejectsProse(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"ArcadeDB", true},
		{"Claude Code", true},
		{"Neo4j", true},
		{"Codex", true},
		{"ralph.sh", true},
		{"golangci-lint", true},
		{"k8s", true},
		{"il container", false},
		{"un gate", false},
		{"inglese", false},
		{"", false},
		{"   ", false},
		// Every token must qualify -- one shaped token cannot carry the phrase,
		// regardless of which position the unshaped token sits in.
		{"il Container", false},
		{"Container il", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nameShaped(tt.name); got != tt.want {
				t.Fatalf("nameShaped(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestTokenNameShapedRequiresACapitalDigitOrIdentifierPunctuation(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{"ArcadeDB", true},
		{"Neo4j", true},
		{"k8s", true},
		{"ralph.sh", true},
		{"golangci-lint", true},
		{"il", false},
		{"container", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			if got := tokenNameShaped(tt.token); got != tt.want {
				t.Fatalf("tokenNameShaped(%q) = %v, want %v", tt.token, got, tt.want)
			}
		})
	}
}

// The order is asserted explicitly: newMentionScanner's own comment says it
// makes the emitted edge set reproducible, so a regression here is silent
// everywhere else.
func TestNewMentionScannerFiltersAndOrdersNamesLongestFirst(t *testing.T) {
	entities := []string{
		"Claude Code", "ArcadeDB", "Claude", "Neo4j", "Codex", "K8s",
		"il container", "un gate", "", "   ",
	}
	scanner := newMentionScanner(entities)
	want := []string{"Claude Code", "ArcadeDB", "Claude", "Codex", "Neo4j", "K8s"}
	if len(scanner.names) != len(want) {
		t.Fatalf("names = %v, want %v", scanner.names, want)
	}
	for i, name := range want {
		if scanner.names[i] != name {
			t.Fatalf("names = %v, want %v", scanner.names, want)
		}
	}
}

func TestHubCapTruncatesTowardZeroAndAvoidsDivisionByZero(t *testing.T) {
	tests := []struct {
		name  string
		facts int
		share float64
		want  int
	}{
		{"measured corpus at the default share", 107, 0.20, 21},
		{"zero facts never divides by zero", 0, 0.20, 0},
		{"zero share caps everything", 107, 0, 0},
		{"negative share caps everything", 107, -0.5, 0},
		{"truncates toward zero, not rounds", 10, 0.25, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hubCap(tt.facts, tt.share); got != tt.want {
				t.Fatalf("hubCap(%d, %v) = %d, want %d", tt.facts, tt.share, got, tt.want)
			}
		})
	}
}

func TestNamesInReturnsNilForEmptyWhitespaceAndNilScanner(t *testing.T) {
	scanner := newMentionScanner([]string{"Aura"})
	if got := scanner.namesIn(""); got != nil {
		t.Fatalf("namesIn(\"\") = %v, want nil", got)
	}
	if got := scanner.namesIn("   \t\n  "); got != nil {
		t.Fatalf("namesIn(whitespace) = %v, want nil", got)
	}
	var nilScanner *mentionScanner
	if got := nilScanner.namesIn("Aura runs"); got != nil {
		t.Fatalf("namesIn on a nil scanner = %v, want nil", got)
	}
}

func TestMentionSchemaStatementsAreIdempotentAndCreateTheEdgeType(t *testing.T) {
	statements := mentionSchemaStatements()
	if len(statements) == 0 {
		t.Fatal("mentionSchemaStatements() returned empty")
	}
	foundEdgeType := false
	for _, stmt := range statements {
		if !strings.Contains(stmt, "IF NOT EXISTS") {
			t.Fatalf("statement not idempotent: %q", stmt)
		}
		if strings.Contains(stmt, "CREATE EDGE TYPE "+mentionsEdgeType) {
			foundEdgeType = true
		}
	}
	if !foundEdgeType {
		t.Fatalf("mentionSchemaStatements() = %v, want a CREATE EDGE TYPE %s statement", statements, mentionsEdgeType)
	}
}
