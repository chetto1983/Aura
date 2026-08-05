package documents

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const doclingDocumentSchema = "DoclingDocument"

// BuildPassagesRequest supplies the immutable control-plane coordinates shared
// by every passage created from one Docling result.
type BuildPassagesRequest struct {
	IdentityID         string
	DocumentID         string
	SearchDocumentID   string
	VersionID          string
	VersionNumber      int
	OriginalSHA256     string
	PipelineGeneration int64
	MaxPassages        int
	Result             DoclingResult
}

type doclingItemProvenance struct {
	pageNumber  int
	boundingBox []float64
	charStart   int
	charEnd     int
	hasPage     bool
	hasBox      bool
	hasSpan     bool
}

// BuildPassages turns the supported Docling hybrid-chunk response into stable,
// provenance-bearing passages. It fails closed on malformed or duplicate
// chunks and never selects an ambiguous page, bounding box, or character span.
func BuildPassages(req BuildPassagesRequest) ([]Passage, error) {
	if err := validatePassageBuildRequest(req); err != nil {
		return nil, err
	}
	provenance, err := indexDoclingProvenance(req.Result.DocumentJSON)
	if err != nil {
		return nil, err
	}
	chunks := append([]DoclingChunk(nil), req.Result.Chunks...)
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ChunkIndex < chunks[j].ChunkIndex })

	passages := make([]Passage, 0, len(chunks))
	seenOrdinals := make(map[int]struct{}, len(chunks))
	anchorOccurrences := make(map[string]int, len(chunks))
	for _, chunk := range chunks {
		if _, duplicate := seenOrdinals[chunk.ChunkIndex]; duplicate {
			return nil, fmt.Errorf("docling passages: duplicate chunk index %d", chunk.ChunkIndex)
		}
		seenOrdinals[chunk.ChunkIndex] = struct{}{}
		text := strings.TrimSpace(chunk.Text)
		if text == "" {
			return nil, fmt.Errorf("docling passages: chunk %d has no contextualized text", chunk.ChunkIndex)
		}
		rawText := text
		if chunk.RawText != nil && strings.TrimSpace(*chunk.RawText) != "" {
			rawText = strings.TrimSpace(*chunk.RawText)
		}
		normalizedHash := NormalizedTextSHA256(rawText)
		anchor := structuralAnchor(chunk)
		occurrenceKey := anchor + "\x00" + normalizedHash
		occurrence := anchorOccurrences[occurrenceKey]
		anchorOccurrences[occurrenceKey] = occurrence + 1
		passageID, idErr := DeterministicPassageID(req.VersionID, anchor, occurrence, normalizedHash)
		if idErr != nil {
			return nil, fmt.Errorf("docling passages: chunk %d id: %w", chunk.ChunkIndex, idErr)
		}
		passages = append(passages, Passage{
			ID: passageID, IdentityID: strings.TrimSpace(req.IdentityID),
			DocumentID:       strings.TrimSpace(req.DocumentID),
			SearchDocumentID: strings.TrimSpace(req.SearchDocumentID),
			VersionID:        strings.TrimSpace(req.VersionID), VersionNumber: req.VersionNumber,
			OriginalSHA256:     strings.ToLower(strings.TrimSpace(req.OriginalSHA256)),
			PipelineGeneration: req.PipelineGeneration, Ordinal: chunk.ChunkIndex,
			Text: rawText, ContextualizedText: text, NormalizedTextSHA256: normalizedHash,
			Locator: locatorForChunk(chunk, provenance),
		})
	}
	return passages, nil
}

func validatePassageBuildRequest(req BuildPassagesRequest) error {
	for name, value := range map[string]string{
		"identity_id": req.IdentityID, "document_id": req.DocumentID,
		"search_document_id": req.SearchDocumentID, "version_id": req.VersionID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("docling passages: %s is required", name)
		}
	}
	if req.VersionNumber <= 0 || req.PipelineGeneration <= 0 {
		return fmt.Errorf("docling passages: version number and pipeline generation must be positive")
	}
	if _, err := canonicalSHA256(req.OriginalSHA256); err != nil {
		return fmt.Errorf("docling passages: original sha256: %w", err)
	}
	if len(req.Result.Chunks) == 0 {
		return fmt.Errorf("docling passages: result contains no chunks")
	}
	if req.MaxPassages <= 0 {
		return fmt.Errorf("docling passages: maximum passage count must be positive")
	}
	if len(req.Result.Chunks) > req.MaxPassages {
		return fmt.Errorf("docling passages: %d chunks exceed maximum %d", len(req.Result.Chunks), req.MaxPassages)
	}
	if req.Result.Partial || req.Result.DocumentStatus != "success" {
		return fmt.Errorf("docling passages: only a complete conversion can produce passages")
	}
	return nil
}

func structuralAnchor(chunk DoclingChunk) string {
	refs := make([]string, 0, len(chunk.DocItems))
	for _, ref := range chunk.DocItems {
		if ref = strings.TrimSpace(ref); ref != "" {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return fmt.Sprintf("chunk:%d", chunk.ChunkIndex)
	}
	return strings.Join(refs, "|")
}

func locatorForChunk(chunk DoclingChunk, indexed map[string]doclingItemProvenance) PassageLocator {
	locator := PassageLocator{
		HeadingPath: cleanLabels(chunk.Headings),
		Captions:    cleanLabels(chunk.Captions),
	}
	refs := cleanLabels(chunk.DocItems)
	if len(refs) != 1 {
		return locator
	}
	locator.SelfRef = refs[0]
	item, ok := indexed[refs[0]]
	if !ok {
		if page, unique := uniquePositiveInt(chunk.PageNumbers); unique {
			locator.PageNumber = &page
		}
		return locator
	}
	if item.hasPage {
		locator.PageNumber = new(item.pageNumber)
	} else if page, unique := uniquePositiveInt(chunk.PageNumbers); unique {
		locator.PageNumber = &page
	}
	if item.hasBox {
		locator.BoundingBox = append([]float64(nil), item.boundingBox...)
	}
	if item.hasSpan {
		locator.CharStart = new(item.charStart)
		locator.CharEnd = new(item.charEnd)
	}
	return locator
}

func indexDoclingProvenance(documentJSON json.RawMessage) (map[string]doclingItemProvenance, error) {
	var root map[string]any
	if len(documentJSON) == 0 || json.Unmarshal(documentJSON, &root) != nil {
		return nil, fmt.Errorf("docling passages: converted document is not valid JSON")
	}
	if schema, _ := root["schema_name"].(string); schema != doclingDocumentSchema {
		return nil, fmt.Errorf("docling passages: unsupported converted document schema")
	}
	indexed := make(map[string]doclingItemProvenance)
	walkDoclingValues(root, indexed)
	return indexed, nil
}

func walkDoclingValues(value any, indexed map[string]doclingItemProvenance) {
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["self_ref"].(string); ok && strings.TrimSpace(ref) != "" {
			if provenance, unique := uniqueDoclingProvenance(typed["prov"]); unique {
				indexed[strings.TrimSpace(ref)] = provenance
			}
		}
		for _, child := range typed {
			walkDoclingValues(child, indexed)
		}
	case []any:
		for _, child := range typed {
			walkDoclingValues(child, indexed)
		}
	}
}

func uniqueDoclingProvenance(value any) (doclingItemProvenance, bool) {
	values, ok := value.([]any)
	if !ok || len(values) != 1 {
		return doclingItemProvenance{}, false
	}
	entry, ok := values[0].(map[string]any)
	if !ok {
		return doclingItemProvenance{}, false
	}
	var out doclingItemProvenance
	if page, ok := exactJSONInt(entry["page_no"]); ok && page > 0 {
		out.pageNumber, out.hasPage = page, true
	}
	if box, ok := entry["bbox"].(map[string]any); ok {
		coords := make([]float64, 0, 4)
		for _, key := range []string{"l", "t", "r", "b"} {
			coordinate, valid := box[key].(float64)
			if !valid {
				coords = nil
				break
			}
			coords = append(coords, coordinate)
		}
		if len(coords) == 4 {
			out.boundingBox, out.hasBox = coords, true
		}
	}
	if span, ok := entry["charspan"].([]any); ok && len(span) == 2 {
		start, startOK := exactJSONInt(span[0])
		end, endOK := exactJSONInt(span[1])
		if startOK && endOK && start >= 0 && end >= start {
			out.charStart, out.charEnd, out.hasSpan = start, end, true
		}
	}
	return out, out.hasPage || out.hasBox || out.hasSpan
}

func exactJSONInt(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || number < 0 || number != float64(int(number)) {
		return 0, false
	}
	return int(number), true
}

func uniquePositiveInt(values []int) (int, bool) {
	seen := 0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if seen != 0 && seen != value {
			return 0, false
		}
		seen = value
	}
	return seen, seen > 0
}

func cleanLabels(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
