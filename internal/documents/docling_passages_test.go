package documents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildPassagesPreservesUnambiguousDoclingProvenance(t *testing.T) {
	raw := "Ricavi 2026"
	result := DoclingResult{
		DocumentStatus: "success",
		DocumentJSON: json.RawMessage(`{
  "schema_name":"DoclingDocument",
  "texts":[{
    "self_ref":"#/texts/4",
    "prov":[{"page_no":7,"bbox":{"l":1.5,"t":8,"r":10,"b":2},"charspan":[11,22]}]
  }]
}`),
		Chunks: []DoclingChunk{{
			Filename: "report.pdf", ChunkIndex: 3, Text: "Sezione\nRicavi 2026",
			RawText: &raw, Headings: []string{" Sezione "}, Captions: []string{"Tabella 1"},
			DocItems: []string{"#/texts/4"}, PageNumbers: []int{7},
		}},
	}
	passages, err := BuildPassages(testPassageBuildRequest(result))
	if err != nil {
		t.Fatal(err)
	}
	if len(passages) != 1 {
		t.Fatalf("passages = %d, want 1", len(passages))
	}
	passage := passages[0]
	if passage.Text != raw || passage.ContextualizedText != "Sezione\nRicavi 2026" || passage.Ordinal != 3 {
		t.Fatalf("passage content = %#v", passage)
	}
	locator := passage.Locator
	if locator.SelfRef != "#/texts/4" || locator.PageNumber == nil || *locator.PageNumber != 7 ||
		locator.CharStart == nil || *locator.CharStart != 11 || locator.CharEnd == nil || *locator.CharEnd != 22 {
		t.Fatalf("locator = %#v", locator)
	}
	if len(locator.BoundingBox) != 4 || locator.BoundingBox[0] != 1.5 ||
		len(locator.HeadingPath) != 1 || locator.HeadingPath[0] != "Sezione" {
		t.Fatalf("locator metadata = %#v", locator)
	}
}

func TestBuildPassagesOmitsAmbiguousCoordinatesAndKeepsStableIDs(t *testing.T) {
	result := DoclingResult{
		DocumentStatus: "success",
		DocumentJSON: json.RawMessage(`{
  "schema_name":"DoclingDocument",
  "texts":[
    {"self_ref":"#/texts/1","prov":[{"page_no":1,"charspan":[0,4]}]},
    {"self_ref":"#/texts/2","prov":[{"page_no":2,"charspan":[0,4]}]}
  ]
}`),
		Chunks: []DoclingChunk{{
			Filename: "multi.pdf", ChunkIndex: 0, Text: "same",
			DocItems: []string{"#/texts/1", "#/texts/2"}, PageNumbers: []int{1, 2},
		}},
	}
	first, err := BuildPassages(testPassageBuildRequest(result))
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPassages(testPassageBuildRequest(result))
	if err != nil {
		t.Fatal(err)
	}
	if first[0].ID != second[0].ID {
		t.Fatalf("replay changed passage id: %q != %q", first[0].ID, second[0].ID)
	}
	locator := first[0].Locator
	if locator.SelfRef != "" || locator.PageNumber != nil || locator.CharStart != nil || len(locator.BoundingBox) != 0 {
		t.Fatalf("ambiguous coordinates were selected: %#v", locator)
	}
}

func TestBuildPassagesRejectsUnsafeConversionResults(t *testing.T) {
	goodChunk := DoclingChunk{Filename: "a.pdf", ChunkIndex: 0, Text: "fact"}
	cases := []struct {
		name   string
		result DoclingResult
		max    int
	}{
		{"partial", DoclingResult{DocumentStatus: "partial_success", Partial: true, Chunks: []DoclingChunk{goodChunk}, DocumentJSON: json.RawMessage(`{"schema_name":"DoclingDocument"}`)}, 10},
		{"wrong schema", DoclingResult{DocumentStatus: "success", Chunks: []DoclingChunk{goodChunk}, DocumentJSON: json.RawMessage(`{"schema_name":"Other"}`)}, 10},
		{"empty text", DoclingResult{DocumentStatus: "success", Chunks: []DoclingChunk{{ChunkIndex: 0}}, DocumentJSON: json.RawMessage(`{"schema_name":"DoclingDocument"}`)}, 10},
		{"capacity", DoclingResult{DocumentStatus: "success", Chunks: []DoclingChunk{goodChunk}, DocumentJSON: json.RawMessage(`{"schema_name":"DoclingDocument"}`)}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := testPassageBuildRequest(tc.result)
			req.MaxPassages = tc.max
			if _, err := BuildPassages(req); err == nil {
				t.Fatal("unsafe result accepted")
			}
		})
	}
}

func TestBuildPassagesDistinguishesRepeatedTextAtOneAnchor(t *testing.T) {
	result := DoclingResult{
		DocumentStatus: "success",
		DocumentJSON:   json.RawMessage(`{"schema_name":"DoclingDocument"}`),
		Chunks: []DoclingChunk{
			{Filename: "a.txt", ChunkIndex: 0, Text: " repeated ", DocItems: []string{"#/texts/1"}},
			{Filename: "a.txt", ChunkIndex: 1, Text: "repeated", DocItems: []string{"#/texts/1"}},
		},
	}
	passages, err := BuildPassages(testPassageBuildRequest(result))
	if err != nil {
		t.Fatal(err)
	}
	if passages[0].ID == passages[1].ID || passages[0].NormalizedTextSHA256 != passages[1].NormalizedTextSHA256 {
		t.Fatalf("repeated passage identity = %#v", passages)
	}
}

func testPassageBuildRequest(result DoclingResult) BuildPassagesRequest {
	return BuildPassagesRequest{
		IdentityID: "10000000-0000-0000-0000-000000000001",
		DocumentID: "20000000-0000-0000-0000-000000000002", SearchDocumentID: "doc-customer",
		VersionID: "30000000-0000-0000-0000-000000000003", VersionNumber: 1,
		OriginalSHA256: strings.Repeat("a", 64), PipelineGeneration: 1, MaxPassages: 10,
		Result: result,
	}
}
