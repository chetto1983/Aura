package documents

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDeterministicDocumentPipelineIDs(t *testing.T) {
	documentID := "10000000-0000-0000-0000-000000000001"
	rawHash := strings.Repeat("a", 64)
	version1, err := DeterministicVersionID(documentID, rawHash)
	if err != nil {
		t.Fatal(err)
	}
	version2, err := DeterministicVersionID(documentID, strings.ToUpper(rawHash))
	if err != nil {
		t.Fatal(err)
	}
	if version1 != version2 {
		t.Fatalf("version replay changed id: %s != %s", version1, version2)
	}
	parsed, err := uuid.Parse(version1)
	if err != nil || parsed.Version() != 8 {
		t.Fatalf("version id = %q, err = %v, want UUIDv8", version1, err)
	}

	textHash := NormalizedTextSHA256(" Café\n\tclienti ")
	passage1, err := DeterministicPassageID(version1, "#/texts/4", 0, textHash)
	if err != nil {
		t.Fatal(err)
	}
	passage2, err := DeterministicPassageID(version1, "#/texts/4", 1, textHash)
	if err != nil {
		t.Fatal(err)
	}
	if passage1 == passage2 {
		t.Fatal("duplicate text occurrences collided")
	}
	otherAnchor, err := DeterministicPassageID(version1, "#/texts/5", 0, textHash)
	if err != nil {
		t.Fatal(err)
	}
	if passage1 == otherAnchor {
		t.Fatal("distinct structural anchors collided")
	}
}

func TestDeterministicPipelineIDsRejectMalformedInputs(t *testing.T) {
	if _, err := DeterministicVersionID("not-a-uuid", strings.Repeat("a", 64)); err == nil {
		t.Fatal("invalid document id accepted")
	}
	if _, err := DeterministicVersionID("10000000-0000-0000-0000-000000000001", "abc"); err == nil {
		t.Fatal("short content hash accepted")
	}
	if _, err := DeterministicPassageID("10000000-0000-0000-0000-000000000001", "", 0, strings.Repeat("b", 64)); err == nil {
		t.Fatal("empty anchor accepted")
	}
	if _, err := DeterministicPassageID("10000000-0000-0000-0000-000000000001", "ref", -1, strings.Repeat("b", 64)); err == nil {
		t.Fatal("negative occurrence accepted")
	}
}

func TestNormalizedTextSHA256CanonicalizesUnicodeAndWhitespace(t *testing.T) {
	composed := NormalizedTextSHA256("Café   clienti")
	decomposed := NormalizedTextSHA256("Cafe\u0301\nclienti")
	if composed != decomposed {
		t.Fatalf("canonical hashes differ: %s != %s", composed, decomposed)
	}
}

func TestCitationTokenUsesMostPreciseProvenance(t *testing.T) {
	page := 7
	start, end := 11, 19
	cases := []struct {
		name    string
		locator PassageLocator
		want    string
	}{
		{"sheet cell", PassageLocator{SheetName: "Ricavi Q3", CellRef: "B12", PageNumber: &page}, "#sheet:Ricavi%20Q3!B12"},
		{"page", PassageLocator{PageNumber: &page, SelfRef: "#/texts/1"}, "#page:7"},
		{"self ref", PassageLocator{SelfRef: "#/texts/1", CharStart: &start}, "#ref:%23%2Ftexts%2F1"},
		{"character span", PassageLocator{CharStart: &start, CharEnd: &end}, "#char:11-19"},
		{"ordinal fallback", PassageLocator{}, "#passage:4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := CitationToken("doc_abc", 3, tc.locator, 4)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(token, tc.want) || !strings.HasPrefix(token, "document:doc_abc@3") {
				t.Fatalf("token = %q, want suffix %q", token, tc.want)
			}
		})
	}
	if _, err := CitationToken("", 1, PassageLocator{}, 0); err == nil {
		t.Fatal("empty search id accepted")
	}
	if _, err := CitationToken("doc", 0, PassageLocator{}, 0); err == nil {
		t.Fatal("non-positive version accepted")
	}
}
