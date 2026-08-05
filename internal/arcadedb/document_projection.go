package arcadedb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrDocumentProjectionConflict means the generation key already exists carrying
	// different immutable content. Retrying cannot resolve it: a generation is write-once,
	// so the caller must mint a new pipeline generation instead.
	ErrDocumentProjectionConflict = errors.New("arcadedb: document projection conflict")
	// ErrDocumentCapacityExceeded means the tenant's candidate or active passage bound
	// would be crossed. It is raised before any row is written, so the database is
	// untouched when a caller sees it.
	ErrDocumentCapacityExceeded = errors.New("arcadedb: document passage capacity exceeded")
	// ErrDocumentProjectionActive means a physical delete was attempted on a generation
	// still eligible for retrieval. Tombstoning first is what makes the delete safe.
	ErrDocumentProjectionActive = errors.New("arcadedb: document projection is still active")
)

type storedProjectionState struct {
	fingerprint  string
	passageCount int
	active       bool
}

const (
	readProjectionStateStatement = "SELECT projection_fingerprint, passage_count, active FROM " +
		documentProjectionType + " WHERE projection_key = :projection_key LIMIT 2"
	countProjectionPassagesStatement = "SELECT count(*) AS count FROM " + documentPassageType +
		" WHERE projection_key = :projection_key"
	countActiveProjectionPassagesStatement = countProjectionPassagesStatement + " AND active = true"
	countActivePassagesStatement           = "SELECT count(*) AS count FROM " + documentPassageType +
		" WHERE active = true"
	createProjectionStatement = `
CREATE (d:DocumentProjection)
SET d = $projection
WITH d
UNWIND $passages AS fields
CREATE (p:Passage)
SET p = fields
CREATE (d)-[:HAS_PASSAGE]->(p)
RETURN count(p) AS passage_count
`
	tombstoneProjectionScript = "UPDATE " + documentPassageType +
		" SET active = false, tombstoned_at = :tombstoned_at" +
		" WHERE projection_key = :projection_key AND active = true;\n" +
		"UPDATE " + documentProjectionType +
		" SET active = false, tombstoned_at = :tombstoned_at" +
		" WHERE projection_key = :projection_key AND active = true"
	deleteProjectionScript = "DELETE VERTEX FROM " + documentPassageType +
		" WHERE projection_key = :projection_key;\n" +
		"DELETE VERTEX FROM " + documentProjectionType +
		" WHERE projection_key = :projection_key"
)

// ProjectGeneration atomically writes a candidate generation into one tenant database.
func (d *DocumentIndex) ProjectGeneration(
	ctx context.Context,
	generation ProjectionGeneration,
) (ProjectionResult, error) {
	normalized, err := d.normalizeGeneration(generation)
	if err != nil {
		return ProjectionResult{}, err
	}
	client, err := d.tenantClient(ctx, normalized.IdentityID)
	if err != nil {
		return ProjectionResult{}, err
	}
	projectionKey := documentProjectionKey(normalized.DocumentID, normalized.PipelineGeneration)
	fingerprint, err := d.projectionFingerprint(normalized)
	if err != nil {
		return ProjectionResult{}, err
	}

	lock := d.projectionLock(normalized.IdentityID)
	lock.Lock()
	defer lock.Unlock()
	existing, err := loadProjectionState(ctx, client, projectionKey)
	if err != nil {
		return ProjectionResult{}, err
	}
	if existing != nil {
		return verifyProjectionReplay(ctx, client, projectionKey, fingerprint, len(normalized.Passages), existing)
	}
	active, err := countRows(ctx, client, countActivePassagesStatement, nil)
	if err != nil {
		return ProjectionResult{}, fmt.Errorf("arcadedb: count active document passages: %w", err)
	}
	if active+len(normalized.Passages) > d.config.MaxActivePassages {
		return ProjectionResult{}, fmt.Errorf(
			"%w: active=%d candidate=%d maximum=%d",
			ErrDocumentCapacityExceeded, active, len(normalized.Passages), d.config.MaxActivePassages,
		)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	projection := projectionFields(normalized, projectionKey, fingerprint, d.schemaVersion(), now)
	passages := passageFields(normalized, projectionKey, d.schemaVersion(), now)
	rows, createErr := client.Write(ctx, createProjectionStatement, map[string]any{
		"projection": projection,
		"passages":   passages,
	})
	if createErr != nil {
		// A fenced retry can race after the read. The unique keys decide whether
		// that race was an exact replay or immutable-content corruption.
		raced, readErr := loadProjectionState(ctx, client, projectionKey)
		if readErr == nil && raced != nil {
			return verifyProjectionReplay(ctx, client, projectionKey, fingerprint, len(passages), raced)
		}
		return ProjectionResult{}, fmt.Errorf("arcadedb: create document projection: %w", createErr)
	}
	written, err := oneCount(rows, "passage_count")
	if err != nil || written != len(passages) {
		if err == nil {
			err = fmt.Errorf("wrote %d passages, want %d", written, len(passages))
		}
		return ProjectionResult{}, fmt.Errorf("arcadedb: invalid document projection response: %w", err)
	}
	return ProjectionResult{
		ProjectionKey: projectionKey, Fingerprint: fingerprint,
		PassageCount: written, Created: true, Active: true,
	}, nil
}

// TombstoneGeneration makes one exact generation ineligible for retrieval.
func (d *DocumentIndex) TombstoneGeneration(
	ctx context.Context,
	identityID string,
	documentID string,
	pipelineGeneration string,
) (TombstoneResult, error) {
	documentID = strings.TrimSpace(documentID)
	pipelineGeneration = strings.TrimSpace(pipelineGeneration)
	if err := validateIdentifier("document id", documentID); err != nil {
		return TombstoneResult{}, err
	}
	if err := validateIdentifier("pipeline generation", pipelineGeneration); err != nil {
		return TombstoneResult{}, err
	}
	client, err := d.tenantClient(ctx, identityID)
	if err != nil {
		return TombstoneResult{}, err
	}
	projectionKey := documentProjectionKey(documentID, pipelineGeneration)

	lock := d.projectionLock(strings.TrimSpace(identityID))
	lock.Lock()
	defer lock.Unlock()
	state, err := loadProjectionState(ctx, client, projectionKey)
	if err != nil {
		return TombstoneResult{}, err
	}
	if state == nil {
		return TombstoneResult{ProjectionKey: projectionKey}, nil
	}
	result := TombstoneResult{
		ProjectionKey: projectionKey, PassageCount: state.passageCount, Found: true,
		AlreadyTombstoned: !state.active,
	}
	if !state.active {
		active, err := countRows(ctx, client, countActiveProjectionPassagesStatement,
			map[string]any{"projection_key": projectionKey})
		if err != nil {
			return TombstoneResult{}, fmt.Errorf("arcadedb: verify tombstoned passages: %w", err)
		}
		if active == 0 {
			return result, nil
		}
	}
	if _, err := client.Script(ctx, tombstoneProjectionScript, map[string]any{
		"projection_key": projectionKey,
		"tombstoned_at":  time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return TombstoneResult{}, fmt.Errorf("arcadedb: tombstone document projection: %w", err)
	}
	after, err := loadProjectionState(ctx, client, projectionKey)
	if err != nil {
		return TombstoneResult{}, err
	}
	if after == nil || after.active {
		return TombstoneResult{}, fmt.Errorf("arcadedb: tombstone document projection postcondition failed")
	}
	active, err := countRows(ctx, client, countActiveProjectionPassagesStatement,
		map[string]any{"projection_key": projectionKey})
	if err != nil {
		return TombstoneResult{}, fmt.Errorf("arcadedb: verify tombstoned passages: %w", err)
	}
	if active != 0 {
		return TombstoneResult{}, fmt.Errorf(
			"arcadedb: tombstone document projection left %d active passages", active,
		)
	}
	return result, nil
}

// DeleteGeneration physically removes an already tombstoned projection and its passages.
func (d *DocumentIndex) DeleteGeneration(
	ctx context.Context,
	identityID string,
	documentID string,
	pipelineGeneration string,
) (DeleteProjectionResult, error) {
	documentID = strings.TrimSpace(documentID)
	pipelineGeneration = strings.TrimSpace(pipelineGeneration)
	if err := validateIdentifier("document id", documentID); err != nil {
		return DeleteProjectionResult{}, err
	}
	if err := validateIdentifier("pipeline generation", pipelineGeneration); err != nil {
		return DeleteProjectionResult{}, err
	}
	client, err := d.tenantClient(ctx, identityID)
	if err != nil {
		return DeleteProjectionResult{}, err
	}
	projectionKey := documentProjectionKey(documentID, pipelineGeneration)
	lock := d.projectionLock(strings.TrimSpace(identityID))
	lock.Lock()
	defer lock.Unlock()
	state, err := loadProjectionState(ctx, client, projectionKey)
	if err != nil {
		return DeleteProjectionResult{}, err
	}
	passages, err := countRows(ctx, client, countProjectionPassagesStatement,
		map[string]any{"projection_key": projectionKey})
	if err != nil {
		return DeleteProjectionResult{}, fmt.Errorf("arcadedb: count document passages for deletion: %w", err)
	}
	result := DeleteProjectionResult{
		ProjectionKey: projectionKey, PassageCount: passages, Found: state != nil || passages > 0,
	}
	if state != nil && state.active {
		return DeleteProjectionResult{}, fmt.Errorf("%w: %s", ErrDocumentProjectionActive, projectionKey)
	}
	if !result.Found {
		return result, nil
	}
	if _, err := client.Script(ctx, deleteProjectionScript,
		map[string]any{"projection_key": projectionKey}); err != nil {
		return DeleteProjectionResult{}, fmt.Errorf("arcadedb: delete document projection: %w", err)
	}
	after, err := loadProjectionState(ctx, client, projectionKey)
	if err != nil {
		return DeleteProjectionResult{}, err
	}
	remaining, err := countRows(ctx, client, countProjectionPassagesStatement,
		map[string]any{"projection_key": projectionKey})
	if err != nil {
		return DeleteProjectionResult{}, fmt.Errorf("arcadedb: verify deleted document passages: %w", err)
	}
	if after != nil || remaining != 0 {
		return DeleteProjectionResult{}, fmt.Errorf(
			"arcadedb: delete document projection postcondition failed: projection=%t passages=%d",
			after != nil, remaining,
		)
	}
	return result, nil
}

func (d *DocumentIndex) normalizeGeneration(generation ProjectionGeneration) (ProjectionGeneration, error) {
	generation.IdentityID = strings.TrimSpace(generation.IdentityID)
	generation.DocumentID = strings.TrimSpace(generation.DocumentID)
	generation.SearchDocumentID = strings.TrimSpace(generation.SearchDocumentID)
	generation.VersionID = strings.TrimSpace(generation.VersionID)
	generation.RawSHA256 = strings.TrimSpace(generation.RawSHA256)
	generation.PipelineGeneration = strings.TrimSpace(generation.PipelineGeneration)
	identifiers := []struct {
		name  string
		value string
	}{
		{"identity id", generation.IdentityID}, {"document id", generation.DocumentID},
		{"search document id", generation.SearchDocumentID}, {"version id", generation.VersionID},
		{"pipeline generation", generation.PipelineGeneration},
	}
	for _, identifier := range identifiers {
		if err := validateIdentifier(identifier.name, identifier.value); err != nil {
			return ProjectionGeneration{}, err
		}
	}
	if generation.VersionNumber <= 0 {
		return ProjectionGeneration{}, fmt.Errorf("arcadedb: document version number must be positive")
	}
	if !validSHA256(generation.RawSHA256) {
		return ProjectionGeneration{}, fmt.Errorf("arcadedb: raw SHA-256 must be 64 lowercase hexadecimal characters")
	}
	if len(generation.Passages) == 0 {
		return ProjectionGeneration{}, fmt.Errorf("arcadedb: document projection must contain passages")
	}
	if len(generation.Passages) > d.config.MaxCandidatePassages {
		return ProjectionGeneration{}, fmt.Errorf(
			"%w: candidate=%d maximum=%d",
			ErrDocumentCapacityExceeded, len(generation.Passages), d.config.MaxCandidatePassages,
		)
	}
	generation.Passages = append([]ProjectedPassage(nil), generation.Passages...)
	seenIDs := make(map[string]struct{}, len(generation.Passages))
	seenOrdinals := make(map[int64]struct{}, len(generation.Passages))
	for index := range generation.Passages {
		passage, err := normalizePassage(generation.Passages[index], d.config.Dimensions)
		if err != nil {
			return ProjectionGeneration{}, fmt.Errorf("arcadedb: passage %d: %w", index, err)
		}
		if _, duplicate := seenIDs[passage.PassageID]; duplicate {
			return ProjectionGeneration{}, fmt.Errorf("arcadedb: duplicate passage id %q", passage.PassageID)
		}
		if _, duplicate := seenOrdinals[passage.Ordinal]; duplicate {
			return ProjectionGeneration{}, fmt.Errorf("arcadedb: duplicate passage ordinal %d", passage.Ordinal)
		}
		seenIDs[passage.PassageID] = struct{}{}
		seenOrdinals[passage.Ordinal] = struct{}{}
		generation.Passages[index] = passage
	}
	sort.Slice(generation.Passages, func(i, j int) bool {
		if generation.Passages[i].Ordinal == generation.Passages[j].Ordinal {
			return generation.Passages[i].PassageID < generation.Passages[j].PassageID
		}
		return generation.Passages[i].Ordinal < generation.Passages[j].Ordinal
	})
	return generation, nil
}

func normalizePassage(passage ProjectedPassage, dimensions int) (ProjectedPassage, error) {
	passage.PassageID = strings.TrimSpace(passage.PassageID)
	passage.NormalizedSHA256 = strings.TrimSpace(passage.NormalizedSHA256)
	passage.SelfRef = strings.TrimSpace(passage.SelfRef)
	passage.SheetName = strings.TrimSpace(passage.SheetName)
	passage.TableName = strings.TrimSpace(passage.TableName)
	passage.CellReference = strings.TrimSpace(passage.CellReference)
	passage.HeadingPath = normalizeLabels(passage.HeadingPath)
	passage.Captions = normalizeLabels(passage.Captions)
	if err := validateIdentifier("passage id", passage.PassageID); err != nil {
		return ProjectedPassage{}, err
	}
	if passage.Ordinal < 0 {
		return ProjectedPassage{}, fmt.Errorf("ordinal must not be negative")
	}
	if strings.TrimSpace(passage.Text) == "" {
		return ProjectedPassage{}, fmt.Errorf("text must be non-empty")
	}
	if utf8.RuneCountInString(passage.Text) > maxProjectedPassageRunes {
		return ProjectedPassage{}, fmt.Errorf("text exceeds %d characters", maxProjectedPassageRunes)
	}
	if !validSHA256(passage.NormalizedSHA256) {
		return ProjectedPassage{}, fmt.Errorf("normalized text SHA-256 must be 64 lowercase hexadecimal characters")
	}
	if len(passage.Embedding) != dimensions {
		return ProjectedPassage{}, fmt.Errorf("embedding has dimension %d, want %d", len(passage.Embedding), dimensions)
	}
	for _, value := range passage.Embedding {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return ProjectedPassage{}, fmt.Errorf("embedding contains a non-finite component")
		}
	}
	if passage.PageNumber != nil && *passage.PageNumber <= 0 {
		return ProjectedPassage{}, fmt.Errorf("page number must be positive")
	}
	if passage.BoundingBox != nil && !finiteBoundingBox(*passage.BoundingBox) {
		return ProjectedPassage{}, fmt.Errorf("bounding box must contain finite coordinates")
	}
	if passage.CharacterSpan != nil &&
		(passage.CharacterSpan.Start < 0 || passage.CharacterSpan.End < passage.CharacterSpan.Start) {
		return ProjectedPassage{}, fmt.Errorf("character span is invalid")
	}
	for name, value := range map[string]*int64{
		"row number": passage.RowNumber, "column number": passage.ColumnNumber,
	} {
		if value != nil && *value < 0 {
			return ProjectedPassage{}, fmt.Errorf("%s must not be negative", name)
		}
	}
	return passage, nil
}

func projectionFields(
	generation ProjectionGeneration,
	projectionKey string,
	fingerprint string,
	schemaVersion string,
	createdAt string,
) map[string]any {
	return map[string]any{
		"projection_key": projectionKey, "document_id": generation.DocumentID,
		"search_document_id": generation.SearchDocumentID, "version_id": generation.VersionID,
		"version_number": generation.VersionNumber, "raw_sha256": generation.RawSHA256,
		"pipeline_generation": generation.PipelineGeneration, "schema_version": schemaVersion,
		"projection_fingerprint": fingerprint, "passage_count": len(generation.Passages),
		"active": true, "created_at": createdAt,
	}
}

func passageFields(
	generation ProjectionGeneration,
	projectionKey string,
	schemaVersion string,
	createdAt string,
) []map[string]any {
	fields := make([]map[string]any, 0, len(generation.Passages))
	for _, passage := range generation.Passages {
		row := map[string]any{
			"passage_key": passageProjectionKey(projectionKey, passage.PassageID),
			"passage_id":  passage.PassageID, "projection_key": projectionKey,
			"document_id": generation.DocumentID, "search_document_id": generation.SearchDocumentID,
			"version_id": generation.VersionID, "version_number": generation.VersionNumber,
			"raw_sha256": generation.RawSHA256, "pipeline_generation": generation.PipelineGeneration,
			"schema_version": schemaVersion, "ordinal": passage.Ordinal, "text": passage.Text,
			"normalized_text_sha256": passage.NormalizedSHA256, "embedding": passage.Embedding,
			"active": true, "created_at": createdAt,
		}
		addPassageLocator(row, passage)
		fields = append(fields, row)
	}
	return fields
}

func addPassageLocator(row map[string]any, passage ProjectedPassage) {
	optionalStrings := map[string]string{
		"self_ref": passage.SelfRef, "sheet_name": passage.SheetName,
		"table_name": passage.TableName, "cell_reference": passage.CellReference,
	}
	for key, value := range optionalStrings {
		if value != "" {
			row[key] = value
		}
	}
	if len(passage.HeadingPath) > 0 {
		row["heading_path"] = passage.HeadingPath
	}
	if len(passage.Captions) > 0 {
		row["captions"] = passage.Captions
	}
	if passage.PageNumber != nil {
		row["page_number"] = *passage.PageNumber
	}
	if passage.BoundingBox != nil {
		row["bbox_left"], row["bbox_top"] = passage.BoundingBox.Left, passage.BoundingBox.Top
		row["bbox_right"], row["bbox_bottom"] = passage.BoundingBox.Right, passage.BoundingBox.Bottom
	}
	if passage.CharacterSpan != nil {
		row["char_start"], row["char_end"] = passage.CharacterSpan.Start, passage.CharacterSpan.End
	}
	if passage.RowNumber != nil {
		row["row_number"] = *passage.RowNumber
	}
	if passage.ColumnNumber != nil {
		row["column_number"] = *passage.ColumnNumber
	}
}

func (d *DocumentIndex) projectionFingerprint(generation ProjectionGeneration) (string, error) {
	payload := struct {
		SchemaVersion string
		Generation    ProjectionGeneration
	}{d.schemaVersion(), generation}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("arcadedb: fingerprint document projection: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func documentProjectionKey(documentID, generation string) string {
	return hashKey("document", documentID, "generation", generation)
}

func passageProjectionKey(projectionKey, passageID string) string {
	return hashKey("projection", projectionKey, "passage", passageID)
}

func hashKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func loadProjectionState(
	ctx context.Context,
	client *Client,
	projectionKey string,
) (*storedProjectionState, error) {
	rows, err := client.Query(ctx, readProjectionStateStatement,
		map[string]any{"projection_key": projectionKey})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: read document projection: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("arcadedb: projection key %s returned %d rows", projectionKey, len(rows))
	}
	fingerprint, err := requiredString(rows[0], "projection_fingerprint")
	if err != nil {
		return nil, fmt.Errorf("arcadedb: invalid document projection response: %w", err)
	}
	passageCount, err := requiredNonNegativeInt(rows[0], "passage_count")
	if err != nil {
		return nil, fmt.Errorf("arcadedb: invalid document projection response: %w", err)
	}
	active, err := requiredBool(rows[0], "active")
	if err != nil {
		return nil, fmt.Errorf("arcadedb: invalid document projection response: %w", err)
	}
	return &storedProjectionState{fingerprint: fingerprint, passageCount: passageCount, active: active}, nil
}

func verifyProjectionReplay(
	ctx context.Context,
	client *Client,
	projectionKey string,
	fingerprint string,
	wantPassages int,
	state *storedProjectionState,
) (ProjectionResult, error) {
	if state.fingerprint != fingerprint || state.passageCount != wantPassages {
		return ProjectionResult{}, fmt.Errorf("%w: generation %s already has different immutable content",
			ErrDocumentProjectionConflict, projectionKey)
	}
	actual, err := countRows(ctx, client, countProjectionPassagesStatement,
		map[string]any{"projection_key": projectionKey})
	if err != nil {
		return ProjectionResult{}, fmt.Errorf("arcadedb: verify document projection replay: %w", err)
	}
	if actual != wantPassages {
		return ProjectionResult{}, fmt.Errorf(
			"arcadedb: projection %s records %d passages but contains %d",
			projectionKey, wantPassages, actual,
		)
	}
	return ProjectionResult{
		ProjectionKey: projectionKey, Fingerprint: fingerprint,
		PassageCount: wantPassages, Active: state.active,
	}, nil
}

func countRows(
	ctx context.Context,
	client *Client,
	statement string,
	params map[string]any,
) (int, error) {
	rows, err := client.Query(ctx, statement, params)
	if err != nil {
		return 0, err
	}
	return oneCount(rows, "count")
}

func oneCount(rows []map[string]any, key string) (int, error) {
	if len(rows) != 1 {
		return 0, fmt.Errorf("expected one row, got %d", len(rows))
	}
	return requiredNonNegativeInt(rows[0], key)
}
