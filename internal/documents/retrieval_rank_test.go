package documents

import (
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func rankedIDs(documents []RetrievalDocument) []string {
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.DocumentID)
	}
	return ids
}

func fusedPassage(documentID string, score float64) arcadedb.PassageCandidate {
	return arcadedb.PassageCandidate{
		PassageID: documentID + ":0", SearchDocumentID: documentID, SourceKind: "s3",
		SourceKey: documentID, RawSHA256: documentID + "-raw", NormalizedSHA256: documentID + "-text",
		Text: "passage of " + documentID, Leg: arcadedb.RetrievalLegFused, FusedScore: &score,
	}
}

// The reserved place is for a file that has no passage at all, the spreadsheet it was written
// for, and not for any document this query happened to retrieve no passage of. Measured
// 2026-10-16 over Aura's own sources: keyed on "no passage retrieved", it promoted files found
// by their name alone -- a .service unit, a skill reference at 0.344 -- above every better hit.
func TestTheReservedPlaceGoesToAFileWithNoPassageAtAll(t *testing.T) {
	cards := []RetrievalCard{
		{DocumentID: "named.service", Title: "named.service", OriginalSHA256: "named-raw", Rank: 0.30, PassageCount: 5},
		{DocumentID: "sheet.xlsx", Title: "sheet.xlsx", OriginalSHA256: "sheet-raw", Rank: 0.20, PassageCount: 0},
	}
	passages := []arcadedb.PassageCandidate{fusedPassage("a.go", 0.6), fusedPassage("b.go", 0.5), fusedPassage("c.md", 0.4)}

	got := rankedIDs(rankDocuments(cards, passages, exactLane{}, nil, 8, 3))

	want := []string{"a.go", "sheet.xlsx", "b.go", "c.md", "named.service"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// With no passage leg every document is card-only, and the place still goes to the file that
// has no passage at all rather than to whichever card came second.
func TestTheReservedPlaceHoldsWhenOnlyCardsAnswer(t *testing.T) {
	cards := []RetrievalCard{
		{DocumentID: "notes.md", OriginalSHA256: "notes-raw", Rank: 0.50, PassageCount: 4},
		{DocumentID: "report.docx", OriginalSHA256: "report-raw", Rank: 0.45, PassageCount: 9},
		{DocumentID: "sheet.xlsx", OriginalSHA256: "sheet-raw", Rank: 0.30, PassageCount: 0},
	}

	got := rankedIDs(rankCardsOnly(cards, nil, 8, 3))

	want := []string{"notes.md", "sheet.xlsx", "report.docx"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func passageWithText(documentID string, score float64, text string) arcadedb.PassageCandidate {
	passage := fusedPassage(documentID, score)
	passage.Text = text
	return passage
}

// A document holding the question's identifier ranks ahead of one that only resembles the
// question. Measured 2026-10-16 on the complex corpus: "che difetto riguardava il reclamo
// RC-2026-040" put note_pagamenti.txt first by cosine, and the quality report, the only file
// holding that number, third.
func TestADocumentCarryingTheIdentifiersRanksFirst(t *testing.T) {
	passages := []arcadedb.PassageCandidate{
		passageWithText("note_pagamenti.txt", 0.48, "solleciti e pagamenti in sospeso"),
		passageWithText("fattura.xlsx", 0.44, "difetto segnalato dal cliente"),
		passageWithText("report_qualita.pdf", 0.41, "RC-2026-040 etichetta illeggibile Linea 3"),
	}

	got := rankedIDs(rankDocuments(nil, passages, exactLaneFor("che difetto riguardava il reclamo RC-2026-040"), nil, 8, 3))

	want := []string{"report_qualita.pdf", "note_pagamenti.txt", "fattura.xlsx"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// The rule runs before the limit, so a document holding the identifier is not cut off by the
// documents that outscore it, and a card can carry the identifier as well as a passage.
func TestTheIdentifierRuleRunsBeforeTheLimit(t *testing.T) {
	passages := []arcadedb.PassageCandidate{fusedPassage("a.md", 0.6), fusedPassage("b.md", 0.5)}
	cards := []RetrievalCard{
		{DocumentID: "audit.pdf", Title: "audit.pdf", Card: "Verbale con la non conformità NC-03",
			OriginalSHA256: "audit-raw", Rank: 0.30, PassageCount: 4},
	}

	got := rankedIDs(rankDocuments(cards, passages, exactLaneFor("requisito della NC-03"), nil, 2, 3))

	want := []string{"audit.pdf", "a.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// With the passage index down the cards answer alone, and the one carrying the identifier still
// ranks first.
func TestACardOnlyAnswerRanksTheCarrierFirst(t *testing.T) {
	cards := []RetrievalCard{
		{DocumentID: "note.md", OriginalSHA256: "note-raw", Rank: 0.5, PassageCount: 2},
		{DocumentID: "audit.pdf", Card: "Verbale con la non conformità NC-03", OriginalSHA256: "audit-raw", Rank: 0.3, PassageCount: 4},
	}

	got := rankedIDs(rankCardsOnly(cards, arcadedb.QueryExactTerms("requisito della NC-03"), 8, 3))

	if want := []string{"audit.pdf", "note.md"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// A question with no exact term is ranked by score alone, exactly as before the rule.
func TestAQuestionWithoutExactTermsKeepsTheScoreOrder(t *testing.T) {
	passages := []arcadedb.PassageCandidate{
		passageWithText("a.md", 0.6, "turni di notte"), passageWithText("b.md", 0.5, "ore di lavoro"),
	}

	got := rankedIDs(rankDocuments(nil, passages, exactLaneFor("chi lavora di notte"), nil, 8, 3))

	if want := []string{"a.md", "b.md"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
