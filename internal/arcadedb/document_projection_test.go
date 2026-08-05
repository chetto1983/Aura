package arcadedb

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func projectionFixture() ProjectionGeneration {
	page, row, column := int64(2), int64(4), int64(7)
	return ProjectionGeneration{
		IdentityID: documentTestIdentity, DocumentID: "doc-1", SearchDocumentID: "search-1",
		VersionID: "version-1", VersionNumber: 2, RawSHA256: strings.Repeat("a", 64),
		PipelineGeneration: "generation-7",
		Passages: []ProjectedPassage{
			{
				PassageID: "passage-2", Ordinal: 2, Text: "second passage",
				NormalizedSHA256: strings.Repeat("b", 64), Embedding: []float64{0, 1, 0},
			},
			{
				PassageID: "passage-1", Ordinal: 1, Text: "first passage",
				NormalizedSHA256: strings.Repeat("c", 64), SelfRef: "#/texts/1",
				HeadingPath: []string{" Section ", ""}, Captions: []string{"Table one"},
				PageNumber: &page, BoundingBox: &BoundingBox{Left: 1, Top: 2, Right: 3, Bottom: 4},
				CharacterSpan: &CharacterSpan{Start: 5, End: 17}, SheetName: " Sheet A ",
				TableName: "Table 1", RowNumber: &row, ColumnNumber: &column,
				CellReference: " G4 ", Embedding: []float64{1, 0, 0},
			},
		},
	}
}

func resultBody(rows any) string {
	encoded, _ := json.Marshal(map[string]any{"result": rows})
	return string(encoded)
}

func TestProjectGenerationWritesOneAtomicSortedGraph(t *testing.T) {
	var written map[string]any
	index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "SELECT projection_fingerprint"):
			return testResponse{Body: resultBody([]any{})}
		case strings.Contains(statement, "count(*)"):
			return testResponse{Body: resultBody([]any{map[string]any{"count": 0}})}
		case request.Payload["language"] == "cypher":
			written, _ = request.Payload["params"].(map[string]any)
			return testResponse{Body: resultBody([]any{map[string]any{"passage_count": 2}})}
		default:
			t.Fatalf("unexpected statement: %s", statement)
			return testResponse{}
		}
	}, true)

	result, err := index.ProjectGeneration(t.Context(), projectionFixture())
	if err != nil {
		t.Fatalf("ProjectGeneration: %v", err)
	}
	if !result.Created || !result.Active || result.PassageCount != 2 || !validSHA256(result.Fingerprint) {
		t.Fatalf("result = %+v", result)
	}
	if len(*requests) != 3 || written == nil {
		t.Fatalf("requests = %d, write params = %#v", len(*requests), written)
	}
	passages, ok := written["passages"].([]any)
	if !ok || len(passages) != 2 {
		t.Fatalf("passages params = %#v", written["passages"])
	}
	first, _ := passages[0].(map[string]any)
	if first["passage_id"] != "passage-1" || first["sheet_name"] != "Sheet A" ||
		first["cell_reference"] != "G4" || first["page_number"] != float64(2) {
		t.Fatalf("first passage = %#v", first)
	}
	headings, _ := first["heading_path"].([]any)
	if len(headings) != 1 || headings[0] != "Section" {
		t.Fatalf("heading path = %#v", first["heading_path"])
	}
}

func TestProjectGenerationAcceptsExactReplayAndRejectsConflict(t *testing.T) {
	generation := projectionFixture()
	probe, _ := NewDocumentIndex(tenantResolverFunc(func(_ context.Context, _ string) (*Client, error) {
		return nil, nil
	}), testDocumentConfig())
	normalized, err := probe.normalizeGeneration(generation)
	if err != nil {
		t.Fatalf("normalizeGeneration: %v", err)
	}
	fingerprint, err := probe.projectionFingerprint(normalized)
	if err != nil {
		t.Fatalf("projectionFingerprint: %v", err)
	}
	for name, storedFingerprint := range map[string]string{
		"replay":   fingerprint,
		"conflict": strings.Repeat("f", 64),
	} {
		t.Run(name, func(t *testing.T) {
			index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
				statement, _ := request.Payload["command"].(string)
				if strings.Contains(statement, "SELECT projection_fingerprint") {
					return testResponse{Body: resultBody([]any{map[string]any{
						"projection_fingerprint": storedFingerprint, "passage_count": 2, "active": true,
					}})}
				}
				if strings.Contains(statement, "count(*)") {
					return testResponse{Body: resultBody([]any{map[string]any{"count": 2}})}
				}
				t.Fatalf("unexpected statement: %s", statement)
				return testResponse{}
			}, true)
			result, err := index.ProjectGeneration(t.Context(), generation)
			if name == "conflict" {
				if !errors.Is(err, ErrDocumentProjectionConflict) {
					t.Fatalf("conflict error = %v", err)
				}
				if len(*requests) != 1 {
					t.Fatalf("conflict requests = %d", len(*requests))
				}
				return
			}
			if err != nil || result.Created || !result.Active || result.PassageCount != 2 {
				t.Fatalf("replay = %+v, %v", result, err)
			}
			if len(*requests) != 2 {
				t.Fatalf("replay requests = %d", len(*requests))
			}
		})
	}
}

func TestProjectGenerationEnforcesCapacityBeforeWriting(t *testing.T) {
	index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "SELECT projection_fingerprint") {
			return testResponse{Body: resultBody([]any{})}
		}
		if strings.Contains(statement, "count(*)") {
			return testResponse{Body: resultBody([]any{map[string]any{"count": 7}})}
		}
		t.Fatalf("write reached after capacity refusal: %s", statement)
		return testResponse{}
	}, true)
	_, err := index.ProjectGeneration(t.Context(), projectionFixture())
	if !errors.Is(err, ErrDocumentCapacityExceeded) {
		t.Fatalf("capacity error = %v", err)
	}
	if len(*requests) != 2 {
		t.Fatalf("requests = %d", len(*requests))
	}
}

func TestProjectionValidationRejectsInvalidImmutableContent(t *testing.T) {
	tests := map[string]func(*ProjectionGeneration){
		"empty identity":    func(g *ProjectionGeneration) { g.IdentityID = "" },
		"invalid raw hash":  func(g *ProjectionGeneration) { g.RawSHA256 = "ABC" },
		"zero version":      func(g *ProjectionGeneration) { g.VersionNumber = 0 },
		"no passages":       func(g *ProjectionGeneration) { g.Passages = nil },
		"duplicate id":      func(g *ProjectionGeneration) { g.Passages[1].PassageID = g.Passages[0].PassageID },
		"duplicate ordinal": func(g *ProjectionGeneration) { g.Passages[1].Ordinal = g.Passages[0].Ordinal },
		"wrong dimension":   func(g *ProjectionGeneration) { g.Passages[0].Embedding = []float64{1} },
		"non-finite vector": func(g *ProjectionGeneration) { g.Passages[0].Embedding[0] = math.NaN() },
		"invalid page":      func(g *ProjectionGeneration) { value := int64(0); g.Passages[0].PageNumber = &value },
		"invalid span":      func(g *ProjectionGeneration) { g.Passages[0].CharacterSpan = &CharacterSpan{Start: 2, End: 1} },
		"invalid bbox":      func(g *ProjectionGeneration) { g.Passages[0].BoundingBox = &BoundingBox{Left: math.Inf(1)} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
				t.Fatal("invalid projection reached ArcadeDB")
				return testResponse{}
			}, true)
			generation := projectionFixture()
			mutate(&generation)
			if _, err := index.ProjectGeneration(t.Context(), generation); err == nil {
				t.Fatal("invalid projection accepted")
			}
			if len(*requests) != 0 {
				t.Fatalf("requests = %d", len(*requests))
			}
		})
	}
}

func TestTombstoneGenerationIsVerifiedAndIdempotent(t *testing.T) {
	reads := 0
	index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "SELECT projection_fingerprint"):
			reads++
			return testResponse{Body: resultBody([]any{map[string]any{
				"projection_fingerprint": strings.Repeat("d", 64), "passage_count": 2,
				"active": reads == 1,
			}})}
		case request.Payload["language"] == "sqlscript":
			return testResponse{Body: resultBody([]any{})}
		case strings.Contains(statement, "count(*)"):
			return testResponse{Body: resultBody([]any{map[string]any{"count": 0}})}
		default:
			t.Fatalf("unexpected statement: %s", statement)
			return testResponse{}
		}
	}, true)
	result, err := index.TombstoneGeneration(t.Context(), documentTestIdentity, "doc-1", "generation-7")
	if err != nil {
		t.Fatalf("TombstoneGeneration: %v", err)
	}
	if !result.Found || result.AlreadyTombstoned || result.PassageCount != 2 {
		t.Fatalf("result = %+v", result)
	}
	if len(*requests) != 4 || (*requests)[1].Payload["language"] != "sqlscript" {
		t.Fatalf("requests = %#v", *requests)
	}
}

func TestTombstoneGenerationHandlesMissingAndAlreadyInactive(t *testing.T) {
	for name, rows := range map[string][]any{
		"missing": {},
		"inactive": {map[string]any{
			"projection_fingerprint": strings.Repeat("e", 64), "passage_count": 1, "active": false,
		}},
	} {
		t.Run(name, func(t *testing.T) {
			index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
				statement, _ := request.Payload["command"].(string)
				if strings.Contains(statement, "count(*)") {
					return testResponse{Body: resultBody([]any{map[string]any{"count": 0}})}
				}
				return testResponse{Body: resultBody(rows)}
			}, true)
			result, err := index.TombstoneGeneration(t.Context(), documentTestIdentity, "doc-1", "generation-7")
			if err != nil {
				t.Fatalf("TombstoneGeneration: %v", err)
			}
			if name == "missing" && result.Found {
				t.Fatalf("missing result = %+v", result)
			}
			if name == "inactive" && (!result.Found || !result.AlreadyTombstoned) {
				t.Fatalf("inactive result = %+v", result)
			}
			wantRequests := 1
			if name == "inactive" {
				wantRequests = 2
			}
			if len(*requests) != wantRequests {
				t.Fatalf("requests = %d", len(*requests))
			}
		})
	}
}

func TestDeleteGenerationRequiresTombstoneAndVerifiesPhysicalRemoval(t *testing.T) {
	reads, counts := 0, 0
	index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "SELECT projection_fingerprint"):
			reads++
			if reads == 1 {
				return testResponse{Body: resultBody([]any{map[string]any{
					"projection_fingerprint": strings.Repeat("f", 64), "passage_count": 2, "active": false,
				}})}
			}
			return testResponse{Body: resultBody([]any{})}
		case strings.Contains(statement, "count(*)"):
			counts++
			remaining := 2
			if counts > 1 {
				remaining = 0
			}
			return testResponse{Body: resultBody([]any{map[string]any{"count": remaining}})}
		case request.Payload["language"] == "sqlscript":
			if !strings.Contains(statement, "DELETE VERTEX FROM Passage") ||
				!strings.Contains(statement, "DELETE VERTEX FROM DocumentProjection") {
				t.Fatalf("delete script = %s", statement)
			}
			return testResponse{Body: resultBody([]any{})}
		default:
			t.Fatalf("unexpected statement: %s", statement)
			return testResponse{}
		}
	}, true)
	result, err := index.DeleteGeneration(t.Context(), documentTestIdentity, "doc-1", "generation-7")
	if err != nil {
		t.Fatalf("DeleteGeneration: %v", err)
	}
	if !result.Found || result.PassageCount != 2 {
		t.Fatalf("result = %+v", result)
	}
	if len(*requests) != 5 {
		t.Fatalf("requests = %d", len(*requests))
	}
}

func TestDeleteGenerationRefusesActiveAndTreatsMissingAsSuccess(t *testing.T) {
	for name, state := range map[string][]any{
		"active": {map[string]any{
			"projection_fingerprint": strings.Repeat("a", 64), "passage_count": 1, "active": true,
		}},
		"missing": {},
	} {
		t.Run(name, func(t *testing.T) {
			index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
				statement, _ := request.Payload["command"].(string)
				if strings.Contains(statement, "SELECT projection_fingerprint") {
					return testResponse{Body: resultBody(state)}
				}
				if strings.Contains(statement, "count(*)") {
					count := 0
					if name == "active" {
						count = 1
					}
					return testResponse{Body: resultBody([]any{map[string]any{"count": count}})}
				}
				t.Fatalf("unexpected mutation: %s", statement)
				return testResponse{}
			}, true)
			result, err := index.DeleteGeneration(t.Context(), documentTestIdentity, "doc-1", "generation-7")
			if name == "active" {
				if !errors.Is(err, ErrDocumentProjectionActive) {
					t.Fatalf("active error = %v", err)
				}
			} else if err != nil || result.Found {
				t.Fatalf("missing result = %+v, %v", result, err)
			}
			if len(*requests) != 2 {
				t.Fatalf("requests = %d", len(*requests))
			}
		})
	}
}
