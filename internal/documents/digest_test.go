package documents

import (
	"strings"
	"testing"
)

func rowsChunk(sheet string, from, to int, text string) Chunk {
	return Chunk{
		Kind:    "rows",
		Text:    text,
		Locator: Locator{Sheet: sheet, RowStart: from, RowEnd: to},
	}
}

const clientiTable = `## Clienti
| Area Comm | Filiale | Ragione sociale | Località | Venditore Esterno |
| --- | --- | --- | --- | --- |
| DIST.TORINO | F038 | 173 DIVISIONE ELETTRICA SRL | TORINO | PETRELLI ENRICO |`

func TestBuildDigestDescribesASpreadsheetByItsColumns(t *testing.T) {
	t.Parallel()
	digest := BuildDigest(ExtractedDocument{
		FileName: "Clienti.xlsx", Title: "Clienti.xlsx", SizeBytes: 331239,
		MIMEType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Chunks: []Chunk{
			rowsChunk("Clienti", 2, 51, clientiTable),
			rowsChunk("Clienti", 52, 101, clientiTable),
			rowsChunk("Clienti", 102, 5890, clientiTable),
		},
	})

	// The column headers are the words a person searches by — "ragione sociale",
	// "venditore" — and they are what makes one spreadsheet distinguishable from
	// the next in a library of them.
	for _, want := range []string{
		"Clienti.xlsx", "331239 bytes", "Tabular data.",
		"Ragione sociale", "Località", "Venditore Esterno",
	} {
		if !strings.Contains(digest, want) {
			t.Errorf("digest is missing %q:\n%s", want, digest)
		}
	}
	if !strings.Contains(digest, "(5889 rows)") {
		t.Errorf("digest does not state the row count:\n%s", digest)
	}
	// It is a label, not the document: the 5889 data rows must NOT be in here.
	if strings.Count(digest, "PETRELLI ENRICO") > 1 {
		t.Errorf("digest repeats row content:\n%s", digest)
	}
}

func TestBuildDigestListsEverySheet(t *testing.T) {
	t.Parallel()
	digest := BuildDigest(ExtractedDocument{
		FileName: "Fornitori.xlsx",
		Chunks: []Chunk{
			rowsChunk("Acquisitori", 2, 40, "| Nome | Zona |\n| --- | --- |\n| x | y |"),
			rowsChunk("Fornitori", 2, 90, "| Fornitore | Codice |\n| --- | --- |\n| a | b |"),
			rowsChunk("Acquisitori", 41, 80, "| Nome | Zona |\n| --- | --- |\n| z | w |"),
		},
	})
	for _, want := range []string{"Acquisitori (79 rows)", "Fornitori (89 rows)", "Nome, Zona", "Fornitore, Codice"} {
		if !strings.Contains(digest, want) {
			t.Errorf("missing %q:\n%s", want, digest)
		}
	}
}

func TestBuildDigestUsesTheHeadingOutlineForProse(t *testing.T) {
	t.Parallel()
	digest := BuildDigest(ExtractedDocument{
		FileName: "Costituzione.pdf", MIMEType: "application/pdf",
		Chunks: []Chunk{
			{Kind: "section", HeadingPath: []string{"Principi fondamentali"}},
			{Kind: "section", HeadingPath: []string{"Principi fondamentali"}},
			{Kind: "section", HeadingPath: []string{"Diritti e doveri dei cittadini", "Rapporti civili"}},
			{Kind: "markdown", Locator: Locator{Section: "Ordinamento della Repubblica"}},
		},
	})
	for _, want := range []string{
		"Principi fondamentali", "Rapporti civili", "Ordinamento della Repubblica",
	} {
		if !strings.Contains(digest, want) {
			t.Errorf("missing %q:\n%s", want, digest)
		}
	}
	// A heading seen in twenty chunks is still one heading.
	if strings.Count(digest, "Principi fondamentali") != 1 {
		t.Errorf("headings are not deduplicated:\n%s", digest)
	}
}

func TestBuildDigestIsBounded(t *testing.T) {
	t.Parallel()
	chunks := make([]Chunk, 0, 200)
	for i := range 200 {
		chunks = append(chunks, Chunk{
			Kind:        "section",
			HeadingPath: []string{strings.Repeat("heading", 30) + string(rune('a'+i%26))},
		})
	}
	digest := BuildDigest(ExtractedDocument{FileName: "big.pdf", Chunks: chunks})
	if len(digest) > maxDigestChars {
		t.Fatalf("digest is %d chars, cap is %d", len(digest), maxDigestChars)
	}
}

func TestHeaderLine(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ in, want string }{
		"markdown header":             {"| A | B |\n| --- | --- |", "A, B"},
		"empty cells dropped":         {"|  | B |\n| --- | --- |", "B"},
		"not a table":                 {"just prose", ""},
		"pipe row with nothing in it": {"|  |  |", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := headerLine(tc.in); got != tc.want {
				t.Fatalf("headerLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	t.Run("wide table is elided", func(t *testing.T) {
		cells := make([]string, 20)
		for i := range cells {
			cells[i] = "c"
		}
		got := headerLine("| " + strings.Join(cells, " | ") + " |\n| --- |")
		if !strings.HasSuffix(got, "…") {
			t.Fatalf("a %d-column header was not elided: %q", len(cells), got)
		}
	})
}

func TestBuildDigestOnAnEmptyDocument(t *testing.T) {
	t.Parallel()
	if digest := BuildDigest(ExtractedDocument{}); digest != "" {
		t.Fatalf("digest of nothing = %q, want empty", digest)
	}
	// A file with no extractable structure still gets a label from what the
	// catalog knows — otherwise it is unfindable by anything but its uuid.
	digest := BuildDigest(ExtractedDocument{FileName: "scan.pdf", MIMEType: "application/pdf", SizeBytes: 12})
	if !strings.Contains(digest, "scan.pdf") || !strings.Contains(digest, "application/pdf") {
		t.Fatalf("digest = %q", digest)
	}
}

func TestSortDigestHitsIsStableOnTies(t *testing.T) {
	t.Parallel()
	hits := []DigestHit{
		{Title: "zeta", Rank: 0.5},
		{Title: "alpha", Rank: 0.5},
		{Title: "best", Rank: 0.9},
	}
	SortDigestHits(hits)
	if hits[0].Title != "best" {
		t.Fatalf("rank did not win: %#v", hits)
	}
	// A tie must not depend on whatever order the database returned.
	if hits[1].Title != "alpha" || hits[2].Title != "zeta" {
		t.Fatalf("tie is not broken by title: %#v", hits)
	}
}
