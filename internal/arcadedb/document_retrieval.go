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
	// RetrievalLegFused is the only leg: ArcadeDB fuses the full-text and the vector
	// index itself and returns one ranking. There is no lexical-only or dense-only path,
	// because reconciling two rankings in Go is exactly what measured 0.300 recall@1
	// against the engine's 0.850.
	RetrievalLegFused RetrievalLeg = "fused"
)

// FusionStrategy is `vector.fuse`'s combination rule -- the engine's three, no fourth.
type FusionStrategy string

// RRF fuses by rank, DBSF and LINEAR by normalised score. RRF is the default because
// this pair of sources has incomparable scales -- a cosine distance and a Lucene
// relevance score -- and rank fusion is indifferent to that by construction.
const (
	FusionRRF    FusionStrategy = "RRF"
	FusionDBSF   FusionStrategy = "DBSF"
	FusionLINEAR FusionStrategy = "LINEAR"
)

// The values the 0.850/0.900 measurement used. Constants, not knobs: a passage's fused
// rank depends on both, and the first attempt shipped groupSize 3 with 800 neighbours
// against a measurement taken at 1 and 200, and scored 0.000. Change either only with a
// fresh `go test -tags retrieval_bench ./internal/documents/`.
const (
	fusedDenseNeighbours = 200
	fusedGroupSize       = 1
)

// CandidateFilter is the bounded tenant and document subset shared by both legs.
type CandidateFilter struct {
	IdentityID  string
	Limit       int
	DocumentIDs []string
}

// FusedCandidateQuery searches both indexes and fuses them server-side, in one query.
type FusedCandidateQuery struct {
	CandidateFilter
	Query     string
	Embedding []float64
	Strategy  FusionStrategy
}

// PassageCandidate is an immutable passage plus its provenance and locator evidence.
type PassageCandidate struct {
	PassageID        string
	SearchDocumentID string
	SourceKind       string
	SourceKey        string
	RawSHA256        string
	SchemaVersion    string
	Ordinal          int64
	Text             string
	NormalizedSHA256 string
	HeadingPath      []string
	CharacterSpan    *CharacterSpan
	Leg              RetrievalLeg
	// FusedScore is the engine's combined score, higher-is-better. Under RRF it reads
	// back as a sum of 1/(60+rank) over the sources that matched, which makes a bad
	// ranking diagnosable by arithmetic instead of by guessing.
	FusedScore *float64
}

const passageCandidateFields = "passage_key, search_document_id, source_kind, source_key, " +
	"raw_sha256, schema_version, ordinal, text, normalized_text_sha256, heading_path, " +
	"char_start, char_end"

// FusedCandidates returns ONE ranking over the full-text and vector indexes, combined by
// ArcadeDB's own `vector.fuse` (manual: "Hybrid Search with vector.fuse", >= 26.5.1) and
// diversified to one passage per document by the engine's groupBy, inside the index
// traversal rather than over-fetched and partitioned afterwards.
func (d *DocumentIndex) FusedCandidates(
	ctx context.Context,
	request FusedCandidateQuery,
) ([]PassageCandidate, error) {
	filter, err := d.normalizeCandidateFilter(request.CandidateFilter)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return nil, fmt.Errorf("arcadedb: document fused query must be non-empty")
	}
	if utf8.RuneCountInString(query) > d.config.MaxQueryRunes {
		return nil, fmt.Errorf("arcadedb: document query exceeds %d characters", d.config.MaxQueryRunes)
	}
	if err := validateDenseVector(request.Embedding, d.config.Dimensions); err != nil {
		return nil, err
	}
	strategy := request.Strategy
	if strategy == "" {
		strategy = FusionRRF
	}
	switch strategy {
	case FusionRRF, FusionDBSF, FusionLINEAR:
	default:
		return nil, fmt.Errorf("arcadedb: unknown fusion strategy %q", strategy)
	}
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return nil, err
	}
	where, params := candidateWhere(filter.DocumentIDs)
	params["embedding"] = append([]float64(nil), request.Embedding...)
	params["query"] = escapeLucene(query)
	params["fetch"] = fusedDenseNeighbours
	rows, err := client.Query(ctx, fusedStatement(where, strategy, filter.Limit), params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: fused document candidates: %w", err)
	}
	return d.decodeCandidates(rows, RetrievalLegFused, filter.Limit)
}

// fusedStatement is the measured query, parameter for parameter. No outer ORDER BY:
// `vector.fuse` returns descending by fused score, asserted by the engine's own
// SQLFunctionVectorFuseTest.
// The scope predicate reaches BOTH sub-pipelines and, when the caller named documents,
// restricts both to them. Measured 2026-08-08 to leave the
// ranking bit-identical, so it costs nothing and its absence would have silently ignored
// a caller's document filter.
func fusedStatement(where string, strategy FusionStrategy, limit int) string {
	return "SELECT " + passageCandidateFields + ", score AS fused_score FROM (" +
		"SELECT expand(`vector.fuse`(" +
		"`vector.neighbors`('" + documentPassageType + "[embedding]', :embedding, :fetch, " +
		"{ filter: (SELECT @rid FROM " + documentPassageType + " WHERE " + where + ").@rid })," +
		"(SELECT @rid, $score FROM " + documentPassageType +
		" WHERE SEARCH_INDEX('" + documentPassageType + "[text]', :query) = true AND " + where + ")," +
		"{ fusion: '" + string(strategy) + "', groupBy: 'search_document_id', groupSize: " +
		strconv.Itoa(fusedGroupSize) + " }" +
		"))) LIMIT " + strconv.Itoa(limit)
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
	where := "1 = 1"
	if len(documentIDs) > 0 {
		where += " AND search_document_id IN :document_ids"
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
	// NO re-sort. The engine returns the fused ranking in order and re-imposing one here
	// throws it away: the comparator this replaces had a branch per leg, so the fused leg
	// matched none of them and fell through to document_id ASC, ordinal ASC -- the
	// ranking arrived correct and left grouped by document and ascending, measured at
	// 0.000 recall@1 while the same query scored 0.850 run directly.
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
	required := []struct {
		key    string
		target *string
	}{
		{"search_document_id", new(string)}, {"source_kind", new(string)},
		{"source_key", new(string)},
		{"raw_sha256", new(string)},
		{"schema_version", new(string)},
		{"text", new(string)}, {"normalized_text_sha256", new(string)},
	}
	for index := range required {
		*required[index].target, err = requiredString(row, required[index].key)
		if err != nil {
			return PassageCandidate{}, "", err
		}
	}
	candidate := PassageCandidate{
		PassageID: passageKey, SearchDocumentID: *required[0].target,
		SourceKind: *required[1].target, SourceKey: *required[2].target,
		RawSHA256: *required[3].target, SchemaVersion: *required[4].target,
		Text: *required[5].target, NormalizedSHA256: *required[6].target, Leg: leg,
	}
	if !validSHA256(candidate.RawSHA256) || !validSHA256(candidate.NormalizedSHA256) {
		return PassageCandidate{}, "", fmt.Errorf("candidate carries an invalid SHA-256")
	}
	if candidate.SchemaVersion != d.schemaVersion() {
		return PassageCandidate{}, "", fmt.Errorf(
			"candidate schema %q does not match %q", candidate.SchemaVersion, d.schemaVersion(),
		)
	}
	candidate.Ordinal, err = requiredInt64(row, "ordinal", false)
	if err != nil {
		return PassageCandidate{}, "", err
	}
	if err := decodeCandidateLocator(row, &candidate); err != nil {
		return PassageCandidate{}, "", err
	}
	if leg != RetrievalLegFused {
		return PassageCandidate{}, "", fmt.Errorf("unknown retrieval leg %q", leg)
	}
	score, err := requiredNonNegativeFloat(row, "fused_score")
	if err != nil {
		return PassageCandidate{}, "", err
	}
	candidate.FusedScore = &score
	return candidate, passageKey, nil
}

func decodeCandidateLocator(row map[string]any, candidate *PassageCandidate) error {
	var err error
	if candidate.HeadingPath, err = optionalStrings(row, "heading_path"); err != nil {
		return err
	}
	candidate.CharacterSpan, err = optionalCharacterSpan(row)
	return err
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
