package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

func TestProjectDocumentPassagePreservesProvenance(t *testing.T) {
	page, start, end, row, column := 3, 10, 24, 7, 2
	passage, err := projectDocumentPassage(documents.Passage{
		ID: "passage-1", Ordinal: 4, ContextualizedText: "Synthetic evidence",
		NormalizedTextSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Locator: documents.PassageLocator{
			SelfRef: "#/texts/4", HeadingPath: []string{"Safety"}, Captions: []string{"Table 1"},
			PageNumber: &page, BoundingBox: []float64{1, 2, 3, 4},
			CharStart: &start, CharEnd: &end, SheetName: "Limits", TableName: "Ratings",
			RowNumber: &row, ColumnNumber: &column, CellRef: "B8",
		},
	}, []float64{0.25, 0.75})
	if err != nil {
		t.Fatal(err)
	}
	if passage.PageNumber == nil || *passage.PageNumber != 3 || passage.BoundingBox == nil ||
		passage.CharacterSpan == nil || passage.CharacterSpan.Start != 10 ||
		passage.CellReference != "B8" || len(passage.Embedding) != 2 {
		t.Fatalf("projected passage lost provenance: %#v", passage)
	}
}

func TestProjectDocumentPassageRejectsInventedBoundingBoxShape(t *testing.T) {
	_, err := projectDocumentPassage(documents.Passage{
		ID: "passage-1", ContextualizedText: "Synthetic evidence",
		Locator: documents.PassageLocator{BoundingBox: []float64{1, 2, 3}},
	}, []float64{1})
	if err == nil {
		t.Fatal("invalid bounding box accepted")
	}
}
