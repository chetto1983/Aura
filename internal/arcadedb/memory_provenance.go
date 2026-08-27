package arcadedb

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	factKeyIndexName       = factEdgeType + "[fact_key]"
	legacySourceRunID      = "source_run_id"
	legacySourceMemoryIDs  = "source_memory_ids"
	memorySchemaStateQuery = "SELECT properties, indexes FROM schema:types WHERE name = :type"
)

type memorySchemaState struct {
	properties map[string]bool
	indexes    map[string]bool
}

func (s memorySchemaState) hasProperty(name string) bool { return s.properties[name] }
func (s memorySchemaState) hasIndex(name string) bool    { return s.indexes[name] }

func (c *Client) migrateMemoryProvenance(ctx context.Context) error {
	state, err := c.memorySchemaState(ctx)
	if err != nil {
		return fmt.Errorf("arcadedb: inspect memory schema: %w", err)
	}
	legacy := state.hasProperty(legacySourceRunID) || state.hasProperty(legacySourceMemoryIDs)
	if legacy || !state.hasIndex(factKeyIndexName) {
		if err := c.reindexFacts(ctx, state); err != nil {
			return fmt.Errorf("arcadedb: migrate fact provenance: %w", err)
		}
	}
	if !state.hasIndex(factKeyIndexName) {
		if _, err := c.Command(ctx,
			"CREATE INDEX IF NOT EXISTS ON "+factEdgeType+" (fact_key) UNIQUE", nil); err != nil {
			return fmt.Errorf("arcadedb: create fact identity index: %w", err)
		}
	}
	if legacy {
		for _, property := range []string{legacySourceRunID, legacySourceMemoryIDs} {
			if _, err := c.Command(ctx,
				"DROP PROPERTY "+factEdgeType+"."+property+" IF EXISTS", nil); err != nil {
				return fmt.Errorf("arcadedb: drop retired property %s: %w", property, err)
			}
		}
	}
	return nil
}

func (c *Client) memorySchemaState(ctx context.Context) (memorySchemaState, error) {
	rows, err := c.Query(ctx, memorySchemaStateQuery, map[string]any{"type": factEdgeType})
	if err != nil {
		return memorySchemaState{}, err
	}
	state := memorySchemaState{properties: map[string]bool{}, indexes: map[string]bool{}}
	if len(rows) == 0 {
		return state, nil
	}
	for _, name := range propertyNames(rows[0]["properties"]) {
		state.properties[name] = true
	}
	for _, name := range stringList(rows[0]["indexes"]) {
		state.indexes[name] = true
	}
	return state, nil
}

func (c *Client) reindexFacts(ctx context.Context, state memorySchemaState) error {
	columns := "@rid AS rid, statement, predicate, valid_from, valid_to, created_at, expired_at, " +
		"sources, outV().name AS subject, inV().name AS object"
	if state.hasProperty(legacySourceRunID) {
		columns += ", " + legacySourceRunID
	}
	if state.hasProperty(legacySourceMemoryIDs) {
		columns += ", " + legacySourceMemoryIDs
	}
	rows, err := c.Query(ctx, "SELECT "+columns+" FROM "+factEdgeType, nil)
	if err != nil {
		return err
	}
	groups := make(map[string][]map[string]any, len(rows))
	keys := make([]string, 0, len(rows))
	now := time.Now().UTC()
	for _, row := range rows {
		active, err := activeFactRow(row, now)
		if err != nil {
			return err
		}
		if !active {
			if err := c.convergeHistoricalFact(ctx, row); err != nil {
				return err
			}
			continue
		}
		key := factIdentity(Fact{
			Subject: rowString(row, "subject"), Predicate: rowString(row, "predicate"),
			Object: rowString(row, "object"), Statement: rowString(row, "statement"),
		})
		if _, exists := groups[key]; !exists {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], row)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := c.convergeFactGroup(ctx, key, groups[key]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) convergeHistoricalFact(ctx context.Context, row map[string]any) error {
	rid := rowString(row, "rid")
	if rid == "" {
		return fmt.Errorf("historical fact migration row has no RID")
	}
	sources := factSources(row["sources"])
	if legacyRun := rowString(row, legacySourceRunID); legacyRun != "" {
		sources = mergeFactSources(sources, FactSource{
			RunID: legacyRun, MemoryIDs: rowStrings(row, legacySourceMemoryIDs),
		})
	}
	_, err := c.Command(ctx,
		"UPDATE "+factEdgeType+" SET fact_key = NULL, sources = :sources WHERE @rid = :rid",
		map[string]any{"sources": sourcesParam(sources), "rid": rid})
	if err != nil {
		return fmt.Errorf("update historical %s: %w", rid, err)
	}
	return nil
}

func (c *Client) convergeFactGroup(ctx context.Context, key string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		return rowString(rows[i], "rid") < rowString(rows[j], "rid")
	})
	sources := []FactSource{}
	for _, row := range rows {
		sources = mergeFactSources(sources, factSources(row["sources"])...)
		legacyRun := rowString(row, legacySourceRunID)
		if legacyRun != "" {
			sources = mergeFactSources(sources, FactSource{
				RunID: legacyRun, MemoryIDs: rowStrings(row, legacySourceMemoryIDs),
			})
		}
	}
	canonicalRID := rowString(rows[0], "rid")
	if canonicalRID == "" {
		return fmt.Errorf("fact migration row has no RID")
	}
	for _, duplicate := range rows[1:] {
		rid := rowString(duplicate, "rid")
		if rid == "" {
			return fmt.Errorf("duplicate fact migration row has no RID")
		}
		if _, err := c.Command(ctx, "DELETE FROM "+factEdgeType+" WHERE @rid = :rid",
			map[string]any{"rid": rid}); err != nil {
			return fmt.Errorf("delete duplicate %s: %w", rid, err)
		}
	}
	_, err := c.Command(ctx,
		"UPDATE "+factEdgeType+" SET fact_key = :fact_key, sources = :sources WHERE @rid = :rid",
		map[string]any{"fact_key": key, "sources": sourcesParam(sources), "rid": canonicalRID})
	if err != nil {
		return fmt.Errorf("update canonical %s: %w", canonicalRID, err)
	}
	return nil
}

func normalizeFact(fact Fact) Fact {
	fact.Subject = strings.TrimSpace(fact.Subject)
	fact.SubjectKind = strings.TrimSpace(fact.SubjectKind)
	fact.Predicate = strings.TrimSpace(fact.Predicate)
	fact.Object = strings.TrimSpace(fact.Object)
	fact.ObjectKind = strings.TrimSpace(fact.ObjectKind)
	fact.Statement = strings.TrimSpace(fact.Statement)
	fact.Source = normalizeFactSource(fact.Source)
	return fact
}

func normalizeFactSource(source FactSource) FactSource {
	source.RunID = strings.TrimSpace(source.RunID)
	ids := make([]string, 0, len(source.MemoryIDs))
	for _, id := range source.MemoryIDs {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	source.MemoryIDs = ids
	return source
}

func factIdentity(fact Fact) string {
	hash := sha256.New()
	var size [8]byte
	for _, field := range []string{fact.Subject, fact.Predicate, fact.Object, fact.Statement} {
		value := []byte(strings.TrimSpace(field))
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(value)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func activeFactKey(key string, validTo, now time.Time) any {
	if !validTo.IsZero() && !validTo.After(now) {
		return nil
	}
	return key
}

func activeFactRow(row map[string]any, now time.Time) (bool, error) {
	if row["expired_at"] != nil && rowString(row, "expired_at") != "" {
		return false, nil
	}
	validTo := rowString(row, "valid_to")
	if validTo == "" {
		return true, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, validTo)
	if err != nil {
		return false, fmt.Errorf("fact %s has invalid valid_to %q: %w", rowString(row, "rid"), validTo, err)
	}
	return parsed.After(now), nil
}

func factSources(value any) []FactSource {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	sources := make([]FactSource, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		source := normalizeFactSource(FactSource{
			RunID: rowString(entry, "run_id"), MemoryIDs: rowStrings(entry, "memory_ids"),
			// A fact written before D-10 has no "writer_role" property at all;
			// rowString returns "" for it, which reads back as an unknown role
			// rather than a guessed one -- Fact.validate's role check only
			// gates NEW writes through UpsertFact, never a read-back.
			WriterRole: WriterRole(rowString(entry, "writer_role")),
		})
		if source.RunID != "" {
			sources = mergeFactSources(sources, source)
		}
	}
	return sources
}

// factSourceBucket accumulates one run's memory ids and its writer role while
// mergeFactSources groups by RunID. Role is set once, from whichever
// occurrence supplies it first (a run's role does not change mid-merge in
// practice: it is host-derived per call, not per source, so two additions
// sharing a RunID always carry the same role).
type factSourceBucket struct {
	role WriterRole
	ids  []string
}

func mergeFactSources(existing []FactSource, additions ...FactSource) []FactSource {
	byRun := make(map[string]*factSourceBucket, len(existing)+len(additions))
	for _, source := range append(slices.Clone(existing), additions...) {
		source = normalizeFactSource(source)
		if source.RunID == "" {
			continue
		}
		bucket, exists := byRun[source.RunID]
		if !exists {
			bucket = &factSourceBucket{}
			byRun[source.RunID] = bucket
		}
		if bucket.role == "" && source.WriterRole != "" {
			bucket.role = source.WriterRole
		}
		for _, id := range source.MemoryIDs {
			if !slices.Contains(bucket.ids, id) {
				bucket.ids = append(bucket.ids, id)
			}
		}
	}
	runs := make([]string, 0, len(byRun))
	for run := range byRun {
		runs = append(runs, run)
	}
	sort.Strings(runs)
	merged := make([]FactSource, 0, len(runs))
	for _, run := range runs {
		sort.Strings(byRun[run].ids)
		merged = append(merged, FactSource{RunID: run, MemoryIDs: byRun[run].ids, WriterRole: byRun[run].role})
	}
	return merged
}

func sourcesParam(sources []FactSource) []map[string]any {
	out := make([]map[string]any, 0, len(sources))
	for _, source := range mergeFactSources(nil, sources...) {
		out = append(out, map[string]any{
			"run_id": source.RunID, "memory_ids": source.MemoryIDs,
			"writer_role": string(source.WriterRole),
		})
	}
	return out
}

// attachFactSource attaches source to the fact identified by factKey,
// merging it into that fact's existing sources rather than creating a
// duplicate edge (D-09). It retries a bounded number of times on a genuine
// concurrent-write conflict -- see attachFactSourceOnce's doc comment for
// what that conflict looks like and why a blind single write cannot detect
// it.
func (c *Client) attachFactSource(
	ctx context.Context,
	factKey string,
	source FactSource,
	now time.Time,
) (bool, error) {
	for attempt := 0; attempt <= maxWriteConflictRetries; attempt++ {
		attached, conflict, err := c.attachFactSourceOnce(ctx, factKey, source, now)
		if err != nil {
			return false, err
		}
		if !conflict {
			return attached, nil
		}
		time.Sleep(writeConflictBackoff(attempt + 1))
	}
	return false, fmt.Errorf("arcadedb: attach fact source: too many concurrent writers for fact_key %q", factKey)
}

func sourceEqual(a, b FactSource) bool {
	return a.RunID == b.RunID && slices.Equal(a.MemoryIDs, b.MemoryIDs)
}

// attachFactSourceOnce is ONE read-merge-write attempt, guarded by a
// compare-and-swap on the `sources` property itself.
//
// A blind "UPDATE ... SET sources = :sources WHERE @rid = :rid" overwrites
// the whole property, not a per-element append, so two goroutines both
// attaching a DIFFERENT source to the SAME fact at the same moment can both
// read the same "existing" list, both compute a merge that includes their
// own addition, and both write -- the second write silently discards the
// first goroutine's contribution (a lost update). Measured live under this
// phase's concurrent fan-out test (51-04,
// TestConcurrentWorkerFactWriteSameContentMergesIntoOneFact): 8 concurrent
// attaches to one fact produced 4-5 surviving sources, not 8, with the
// blind write.
//
// ArcadeDB exposes no `@version` column to condition on (verified live:
// `SELECT @version FROM FACT` is a parse error on this server, unlike
// OrientDB's SQL dialect it otherwise follows), but a WHERE clause
// comparing a LIST OF MAP property for structural equality DOES work
// (verified live against the same server: a matching value updates and
// returns count 1, a stale one updates nothing and returns count 0). So the
// write is conditioned on `sources` still equalling exactly what this call
// just read -- `rawExisting` is passed back to the WHERE clause verbatim,
// not rebuilt from FactSource structs, so the comparison is byte-for-bit
// against the actual wire value and immune to any drift in how this
// package would re-serialize it (e.g. a legacy fact with no writer_role
// key at all, vs. one written with the key present and empty).
func (c *Client) attachFactSourceOnce(
	ctx context.Context,
	factKey string,
	source FactSource,
	now time.Time,
) (attached bool, conflict bool, err error) {
	params := map[string]any{"fact_key": factKey, "now": now.UTC().Format(time.RFC3339Nano)}
	if _, err := c.Command(ctx,
		"UPDATE "+factEdgeType+" SET fact_key = NULL WHERE fact_key = :fact_key AND "+
			"(expired_at IS NOT NULL OR (valid_to IS NOT NULL AND valid_to <= :now))",
		params); err != nil {
		if isTransientWriteConflict(err) {
			return false, true, nil
		}
		return false, false, fmt.Errorf("arcadedb: release expired fact identity: %w", err)
	}
	rows, err := c.Query(ctx,
		"SELECT @rid AS rid, sources FROM "+factEdgeType+
			" WHERE fact_key = :fact_key AND expired_at IS NULL AND "+
			"(valid_to IS NULL OR valid_to > :now) LIMIT 1",
		params)
	if err != nil {
		return false, false, fmt.Errorf("arcadedb: find exact fact: %w", err)
	}
	if len(rows) == 0 {
		return false, false, nil
	}
	rid := rowString(rows[0], "rid")
	if rid == "" {
		return false, false, fmt.Errorf("arcadedb: exact fact result has no RID")
	}
	rawExisting := rows[0]["sources"]
	existing := factSources(rawExisting)
	merged := mergeFactSources(existing, source)
	if slices.EqualFunc(existing, merged, sourceEqual) {
		return true, false, nil
	}
	updated, err := c.Command(ctx,
		"UPDATE "+factEdgeType+" SET sources = :sources WHERE @rid = :rid AND sources = :expected",
		map[string]any{"sources": sourcesParam(merged), "rid": rid, "expected": rawExisting})
	if err != nil {
		// A page-level conflict here (the same "Slot rebase ... please
		// retry" class createFactWithRetry handles) is just as retryable
		// as a CAS that ran but matched zero rows below -- both mean
		// "someone else's write is contending for this exact record right
		// now," not "this write is invalid."
		if isTransientWriteConflict(err) {
			return false, true, nil
		}
		return false, false, fmt.Errorf("arcadedb: attach fact source: %w", err)
	}
	if countUpdated(updated) == 0 {
		return false, true, nil
	}
	return true, false, nil
}
