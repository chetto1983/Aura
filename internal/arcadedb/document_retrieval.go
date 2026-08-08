package arcadedb

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RetrievalLeg identifies the evidence carried by one candidate score.
type RetrievalLeg string

// The leg decides which of the two mutually exclusive score fields a candidate carries,
// and in which direction that field orders: a lexical score is a relevance score where
// higher wins, a dense score is a cosine distance where lower wins.
const (
	RetrievalLegLexical RetrievalLeg = "lexical"
	RetrievalLegDense   RetrievalLeg = "dense"
)

// CandidateFilter is the bounded tenant and document subset shared by both legs.
type CandidateFilter struct {
	IdentityID  string
	Limit       int
	DocumentIDs []string
}

// LexicalCandidateQuery searches the multilingual StandardAnalyzer index.
type LexicalCandidateQuery struct {
	CandidateFilter
	Query string
}

// DenseCandidateQuery searches the exact-width NONE-quantized vector index.
type DenseCandidateQuery struct {
	CandidateFilter
	Embedding []float64
}

// PassageCandidate is an immutable passage plus generation and locator evidence.
type PassageCandidate struct {
	PassageID          string
	DocumentID         string
	SearchDocumentID   string
	VersionID          string
	VersionNumber      int64
	RawSHA256          string
	PipelineGeneration string
	SchemaVersion      string
	Ordinal            int64
	Text               string
	NormalizedSHA256   string
	SelfRef            string
	HeadingPath        []string
	Captions           []string
	PageNumber         *int64
	BoundingBox        *BoundingBox
	CharacterSpan      *CharacterSpan
	SheetName          string
	TableName          string
	RowNumber          *int64
	ColumnNumber       *int64
	CellReference      string
	Leg                RetrievalLeg
	LexicalScore       *float64
	DenseDistance      *float64
}

const passageCandidateFields = "passage_key, passage_id, projection_key, document_id, " +
	"search_document_id, version_id, version_number, raw_sha256, pipeline_generation, " +
	"schema_version, ordinal, text, normalized_text_sha256, self_ref, heading_path, captions, " +
	"page_number, bbox_left, bbox_top, bbox_right, bbox_bottom, char_start, char_end, " +
	"sheet_name, table_name, row_number, column_number, cell_reference, active"

// LexicalCandidates returns a deterministic, bounded lexical ranking.
func (d *DocumentIndex) LexicalCandidates(
	ctx context.Context,
	request LexicalCandidateQuery,
) ([]PassageCandidate, error) {
	filter, err := d.normalizeCandidateFilter(request.CandidateFilter)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return nil, fmt.Errorf("arcadedb: document lexical query must be non-empty")
	}
	if utf8.RuneCountInString(query) > d.config.MaxQueryRunes {
		return nil, fmt.Errorf("arcadedb: document query exceeds %d characters", d.config.MaxQueryRunes)
	}
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return nil, err
	}
	where, params := candidateWhere(filter.DocumentIDs)
	params["query"] = escapeLucene(query)
	statement := "SELECT " + passageCandidateFields + ", $score AS lexical_score FROM " +
		documentPassageType + " WHERE SEARCH_INDEX('" + documentPassageType +
		"[text]', :query) = true AND " + where +
		" ORDER BY lexical_score DESC, document_id ASC, ordinal ASC, passage_id ASC LIMIT " +
		strconv.Itoa(filter.Limit)
	rows, err := client.Query(ctx, statement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: lexical document candidates: %w", err)
	}
	return d.decodeCandidates(rows, RetrievalLegLexical, filter.Limit)
}

// DenseCandidates returns a deterministic, bounded nearest-neighbor ranking.
func (d *DocumentIndex) DenseCandidates(
	ctx context.Context,
	request DenseCandidateQuery,
) ([]PassageCandidate, error) {
	filter, err := d.normalizeCandidateFilter(request.CandidateFilter)
	if err != nil {
		return nil, err
	}
	if err := validateDenseVector(request.Embedding, d.config.Dimensions); err != nil {
		return nil, err
	}
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return nil, err
	}
	where, params := candidateWhere(filter.DocumentIDs)
	params["embedding"] = append([]float64(nil), request.Embedding...)
	fetch := min(max(filter.Limit*4, 20), d.config.MaxRetrievalCandidates)
	params["fetch"] = fetch
	statement := "SELECT " + passageCandidateFields + ", distance AS dense_distance FROM (" +
		"SELECT expand(`vector.neighbors`('" + documentPassageType +
		"[embedding]', :embedding, :fetch, { filter: (SELECT @rid FROM " +
		documentPassageType + " WHERE " + where + ").@rid }))" +
		") ORDER BY dense_distance ASC, document_id ASC, ordinal ASC, passage_id ASC LIMIT " +
		strconv.Itoa(filter.Limit)
	rows, err := client.Query(ctx, statement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: dense document candidates: %w", err)
	}
	return d.decodeCandidates(rows, RetrievalLegDense, filter.Limit)
}

func (d *DocumentIndex) normalizeCandidateFilter(filter CandidateFilter) (CandidateFilter, error) {
	filter.IdentityID = strings.TrimSpace(filter.IdentityID)
	if err := validateIdentifier("document identity", filter.IdentityID); err != nil {
		return CandidateFilter{}, err
	}
	if filter.Limit <= 0 {
		filter.Limit = min(20, d.config.MaxRetrievalCandidates)
	}
	if filter.Limit > d.config.MaxRetrievalCandidates {
		return CandidateFilter{}, fmt.Errorf(
			"arcadedb: document candidate limit %d exceeds maximum %d",
			filter.Limit, d.config.MaxRetrievalCandidates,
		)
	}
	if len(filter.DocumentIDs) > d.config.MaxDocumentFilters {
		return CandidateFilter{}, fmt.Errorf(
			"arcadedb: document filter count %d exceeds maximum %d",
			len(filter.DocumentIDs), d.config.MaxDocumentFilters,
		)
	}
	seen := make(map[string]struct{}, len(filter.DocumentIDs))
	documentIDs := make([]string, 0, len(filter.DocumentIDs))
	for _, documentID := range filter.DocumentIDs {
		documentID = strings.TrimSpace(documentID)
		if err := validateIdentifier("document filter id", documentID); err != nil {
			return CandidateFilter{}, err
		}
		if _, duplicate := seen[documentID]; duplicate {
			continue
		}
		seen[documentID] = struct{}{}
		documentIDs = append(documentIDs, documentID)
	}
	sort.Strings(documentIDs)
	filter.DocumentIDs = documentIDs
	return filter, nil
}

func candidateWhere(documentIDs []string) (string, map[string]any) {
	params := make(map[string]any)
	where := "active = true"
	if len(documentIDs) > 0 {
		where += " AND document_id IN :document_ids"
		params["document_ids"] = documentIDs
	}
	return where, params
}

func validateDenseVector(vector []float64, dimensions int) error {
	if len(vector) != dimensions {
		return fmt.Errorf("arcadedb: document query embedding has dimension %d, want %d", len(vector), dimensions)
	}
	for _, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("arcadedb: document query embedding contains a non-finite component")
		}
	}
	return nil
}

func (d *DocumentIndex) decodeCandidates(
	rows []map[string]any,
	leg RetrievalLeg,
	limit int,
) ([]PassageCandidate, error) {
	if len(rows) > limit {
		return nil, fmt.Errorf("arcadedb: document candidate response has %d rows, limit is %d", len(rows), limit)
	}
	candidates := make([]PassageCandidate, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		candidate, passageKey, err := d.decodeCandidate(row, leg)
		if err != nil {
			return nil, fmt.Errorf("arcadedb: document candidate %d: %w", index, err)
		}
		if _, duplicate := seen[passageKey]; duplicate {
			return nil, fmt.Errorf("arcadedb: document candidate response duplicated passage %s", passageKey)
		}
		seen[passageKey] = struct{}{}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidateLess(candidates[i], candidates[j], leg)
	})
	return candidates, nil
}

func (d *DocumentIndex) decodeCandidate(
	row map[string]any,
	leg RetrievalLeg,
) (PassageCandidate, string, error) {
	passageKey, err := requiredString(row, "passage_key")
	if err != nil {
		return PassageCandidate{}, "", err
	}
	// projection_key, version_id and version_number are NOT required any more.
	//
	// They were the generation model: a projection grouped the passages of one document
	// version so a later generation could tombstone the earlier one wholesale. The writer
	// that produced them was deleted with the in-process pipeline, and the reconciler that
	// replaced it removes a document's rows when its object leaves the bucket -- so there
	// is no second generation to distinguish, and no tombstone to read. services/ingest
	// writes all three as NULL, which under the old rule made every candidate it produced
	// unreadable: rows landed, and retrieval returned nothing, with no error anywhere.
	//
	// What still has to hold is what a citation depends on: the passage's identity, the
	// document it came from, its text, and the two digests that stop a citation outliving
	// the bytes it quotes.
	required := []struct {
		key    string
		target *string
	}{
		{"passage_id", new(string)},
		{"document_id", new(string)}, {"search_document_id", new(string)},
		{"raw_sha256", new(string)},
		{"pipeline_generation", new(string)}, {"schema_version", new(string)},
		{"text", new(string)}, {"normalized_text_sha256", new(string)},
	}
	for index := range required {
		*required[index].target, err = requiredString(row, required[index].key)
		if err != nil {
			return PassageCandidate{}, "", err
		}
	}
	candidate := PassageCandidate{
		PassageID:  *required[0].target,
		DocumentID: *required[1].target, SearchDocumentID: *required[2].target,
		RawSHA256:          *required[3].target,
		PipelineGeneration: *required[4].target, SchemaVersion: *required[5].target,
		Text: *required[6].target, NormalizedSHA256: *required[7].target, Leg: leg,
	}
	if !validSHA256(candidate.RawSHA256) || !validSHA256(candidate.NormalizedSHA256) {
		return PassageCandidate{}, "", fmt.Errorf("candidate carries an invalid SHA-256")
	}
	if candidate.SchemaVersion != d.schemaVersion() {
		return PassageCandidate{}, "", fmt.Errorf(
			"candidate schema %q does not match %q", candidate.SchemaVersion, d.schemaVersion(),
		)
	}
	// The two projection-key equalities and the version_number check went with the
	// generation model above. passageKey is still read and returned -- it is the row's
	// unique key, and dedup across the lexical and dense legs needs it -- but it is no
	// longer required to hash to anything.
	candidate.Ordinal, err = requiredInt64(row, "ordinal", false)
	if err != nil {
		return PassageCandidate{}, "", err
	}
	active, err := requiredBool(row, "active")
	if err != nil || !active {
		if err == nil {
			err = fmt.Errorf("active is false")
		}
		return PassageCandidate{}, "", err
	}
	if err := decodeCandidateLocator(row, &candidate); err != nil {
		return PassageCandidate{}, "", err
	}
	switch leg {
	case RetrievalLegLexical:
		score, err := requiredNonNegativeFloat(row, "lexical_score")
		if err != nil {
			return PassageCandidate{}, "", err
		}
		candidate.LexicalScore = &score
	case RetrievalLegDense:
		distance, err := requiredNonNegativeFloat(row, "dense_distance")
		if err != nil {
			return PassageCandidate{}, "", err
		}
		candidate.DenseDistance = &distance
	default:
		return PassageCandidate{}, "", fmt.Errorf("unknown retrieval leg %q", leg)
	}
	return candidate, passageKey, nil
}

func decodeCandidateLocator(row map[string]any, candidate *PassageCandidate) error {
	var err error
	if candidate.SelfRef, err = optionalString(row, "self_ref"); err != nil {
		return err
	}
	if candidate.SheetName, err = optionalString(row, "sheet_name"); err != nil {
		return err
	}
	if candidate.TableName, err = optionalString(row, "table_name"); err != nil {
		return err
	}
	if candidate.CellReference, err = optionalString(row, "cell_reference"); err != nil {
		return err
	}
	if candidate.HeadingPath, err = optionalStrings(row, "heading_path"); err != nil {
		return err
	}
	if candidate.Captions, err = optionalStrings(row, "captions"); err != nil {
		return err
	}
	if candidate.PageNumber, err = optionalInt64(row, "page_number", true); err != nil {
		return err
	}
	if candidate.RowNumber, err = optionalInt64(row, "row_number", false); err != nil {
		return err
	}
	if candidate.ColumnNumber, err = optionalInt64(row, "column_number", false); err != nil {
		return err
	}
	candidate.BoundingBox, err = optionalBoundingBox(row)
	if err != nil {
		return err
	}
	candidate.CharacterSpan, err = optionalCharacterSpan(row)
	return err
}

func candidateLess(left, right PassageCandidate, leg RetrievalLeg) bool {
	if leg == RetrievalLegLexical && *left.LexicalScore != *right.LexicalScore {
		return *left.LexicalScore > *right.LexicalScore
	}
	if leg == RetrievalLegDense && *left.DenseDistance != *right.DenseDistance {
		return *left.DenseDistance < *right.DenseDistance
	}
	if left.DocumentID != right.DocumentID {
		return left.DocumentID < right.DocumentID
	}
	if left.Ordinal != right.Ordinal {
		return left.Ordinal < right.Ordinal
	}
	return left.PassageID < right.PassageID
}

func requiredInt64(row map[string]any, key string, positive bool) (int64, error) {
	value, ok := exactInt64(row[key])
	if !ok || value < 0 || (positive && value == 0) {
		return 0, fmt.Errorf("%s is missing or not a valid integer", key)
	}
	return value, nil
}

func requiredNonNegativeFloat(row map[string]any, key string) (float64, error) {
	value, ok := finiteFloat(row[key])
	if !ok || value < 0 {
		return 0, fmt.Errorf("%s is missing or not a non-negative finite number", key)
	}
	return value, nil
}

func finiteFloat(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func optionalString(row map[string]any, key string) (string, error) {
	value, present := row[key]
	if !present || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s is not a string", key)
	}
	return text, nil
}

func optionalStrings(row map[string]any, key string) ([]string, error) {
	value, present := row[key]
	if !present || value == nil {
		return nil, nil
	}
	if stringsValue, ok := value.([]string); ok {
		return append([]string(nil), stringsValue...), nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s is not a string list", key)
	}
	out := make([]string, len(items))
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s item %d is not a string", key, index)
		}
		out[index] = text
	}
	return out, nil
}

func optionalInt64(row map[string]any, key string, positive bool) (*int64, error) {
	value, present := row[key]
	if !present || value == nil {
		return nil, nil
	}
	number, ok := exactInt64(value)
	if !ok || number < 0 || (positive && number == 0) {
		return nil, fmt.Errorf("%s is not a valid integer", key)
	}
	return &number, nil
}

func optionalBoundingBox(row map[string]any) (*BoundingBox, error) {
	keys := []string{"bbox_left", "bbox_top", "bbox_right", "bbox_bottom"}
	values := make([]float64, len(keys))
	present := 0
	for index, key := range keys {
		value, exists := row[key]
		if !exists || value == nil {
			continue
		}
		var ok bool
		values[index], ok = finiteFloat(value)
		if !ok {
			return nil, fmt.Errorf("%s is not a finite number", key)
		}
		present++
	}
	if present == 0 {
		return nil, nil
	}
	if present != len(keys) {
		return nil, fmt.Errorf("bounding box is incomplete")
	}
	return &BoundingBox{Left: values[0], Top: values[1], Right: values[2], Bottom: values[3]}, nil
}

func optionalCharacterSpan(row map[string]any) (*CharacterSpan, error) {
	start, startErr := optionalInt64(row, "char_start", false)
	end, endErr := optionalInt64(row, "char_end", false)
	if startErr != nil {
		return nil, startErr
	}
	if endErr != nil {
		return nil, endErr
	}
	if start == nil && end == nil {
		return nil, nil
	}
	if start == nil || end == nil || *end < *start {
		return nil, fmt.Errorf("character span is incomplete or invalid")
	}
	return &CharacterSpan{Start: *start, End: *end}, nil
}
