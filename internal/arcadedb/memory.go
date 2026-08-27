package arcadedb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The memory model: the fact lives on the EDGE and is bitemporal.
//
// Retrieval walks an entity's edges when the question names it. Otherwise it
// fuses Lucene and EmbeddingGemma rankings inside ArcadeDB; the lexical leg is
// the bounded fallback whenever embedding is unavailable.
//
// Two independent time axes, because they answer different questions:
//   - valid_from / valid_to  -- when the fact was true in the world
//   - created_at / expired_at -- when we learned it, and when we stopped believing it
//
// A contradiction never deletes: it closes the previous fact's window. That is
// what lets "where did Davide live in 2024" and "where does he live now" both
// have answers. On LOCOMO the systems without this score 21-23% on temporal
// questions against Zep's 79.8%.
//
// Provenance is mandatory and multi-source. Exact replay reuses the edge, so a
// fact can be detached from one source without destroying support from another.
//
// Property names avoid `end`: ArcadeDB reserves it both as a bare identifier
// and as a bind-parameter name.

const factEdgeType = "FACT"

// memorySchemaStatements bootstrap the memory model. Every statement is
// idempotent so this runs on every boot.
func memorySchemaStatements() []string {
	return []string{
		"CREATE VERTEX TYPE Entity IF NOT EXISTS",
		"CREATE PROPERTY Entity.name IF NOT EXISTS STRING",
		"CREATE PROPERTY Entity.kind IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON Entity (name) UNIQUE",

		"CREATE EDGE TYPE " + factEdgeType + " IF NOT EXISTS",
		"CREATE PROPERTY " + factEdgeType + ".statement IF NOT EXISTS STRING",
		"CREATE PROPERTY " + factEdgeType + ".predicate IF NOT EXISTS STRING",
		"CREATE PROPERTY " + factEdgeType + ".valid_from IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + factEdgeType + ".valid_to IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + factEdgeType + ".created_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + factEdgeType + ".expired_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + factEdgeType + ".fact_key IF NOT EXISTS STRING",
		"CREATE PROPERTY " + factEdgeType + ".sources IF NOT EXISTS LIST OF MAP",

		// EnglishAnalyzer, not the StandardAnalyzer default: Standard does no
		// stemming, so a fact reading "works for" is invisible to a question
		// asking "who does X work for". Measured: `work` matched 0 rows under
		// Standard and 1 under English, with `lives`/`living` likewise.
		"CREATE INDEX IF NOT EXISTS ON " + factEdgeType + " (statement) FULL_TEXT " +
			"METADATA {analyzer:'org.apache.lucene.analysis.en.EnglishAnalyzer'}",
	}
}

// EnsureMemorySchema creates the memory model if it is not already there.
func (c *Client) EnsureMemorySchema(ctx context.Context) error {
	for _, statement := range memorySchemaStatements() {
		if _, err := c.Command(ctx, statement, nil); err != nil {
			return fmt.Errorf("arcadedb: ensure memory schema: %w", err)
		}
	}
	if err := c.migrateMemoryProvenance(ctx); err != nil {
		return err
	}
	for _, statement := range vectorSchemaStatements() {
		if _, err := c.Command(ctx, statement, nil); err != nil {
			return fmt.Errorf("arcadedb: ensure memory schema: %w", err)
		}
	}
	return nil
}

// WriterRole names who wrote a FactSource: the operator's own turn, or a
// swarm worker acting on its behalf. Host-derived at the cmd/arcadedb-mcp
// boundary (D-10) -- never a value the model supplies -- so "no fact
// attributed to the parent" (SWARM-07) is checkable rather than assumed, and
// D-11's supersede refusal (fact_authority.go) has something real to key on.
type WriterRole string

const (
	// WriterParent is the operator's own foreground turn (or a host-driven /
	// CLI write with no worker context at all -- see actorFromContext in
	// internal/agent/mcptools, which defaults to this role absent a
	// delegated-dispatch marker).
	WriterParent WriterRole = "parent"
	// WriterWorker is a swarm worker's own dispatch (internal/swarm's
	// runChild). A worker may ADD facts; it may not supersede one (D-11).
	WriterWorker WriterRole = "worker"
)

// FactSource identifies one extraction run supporting a fact.
type FactSource struct {
	RunID      string     `json:"run_id"`
	MemoryIDs  []string   `json:"memory_ids"`
	WriterRole WriterRole `json:"writer_role,omitempty"`
}

// Fact is one assertion about the world, as stored on an edge.
type Fact struct {
	Subject     string
	SubjectKind string
	Predicate   string
	Object      string
	ObjectKind  string
	Statement   string
	ValidFrom   time.Time
	ValidTo     time.Time
	Source      FactSource
	// Supersedes closes any still-valid fact sharing this subject and predicate.
	// It is explicit rather than inferred because some predicates are
	// single-valued ("lives in") and others are not ("likes").
	Supersedes bool
	// TargetFactKey, when set alongside Supersedes, closes exactly the fact
	// identified by this key instead of resolving a subject+predicate
	// candidate set. Empty uses the legacy ambiguity-resolved path (D-16).
	TargetFactKey string
}

// FactWrite reports what an upsert did.
type FactWrite struct {
	Statement  string
	Superseded int
	// Refused is true when Supersedes could not identify exactly one fact to
	// close: an explicit TargetFactKey matching no still-valid fact (always
	// 0-or-1, since fact_key is UNIQUE-indexed), or a subject+predicate match
	// of 0 or more than 1 distinct fact. Nothing was written in either case --
	// no fact closed, and the new fact itself was not created, so a refused
	// correction never adds to the ambiguity it could not resolve. Reason
	// explains why, and Candidates carries the preview set (empty for a
	// fact_key miss) so the caller can retry. This is the shape plan 45-07
	// renders as MemoryUpsertFactOutput.refused/reason/candidates (D-17).
	Refused    bool
	Reason     string
	Candidates []FactHit
}

// proseObjectRuneBound is the ceiling MEM-05's prose guard enforces on the
// object endpoint -- strictly tighter than EntityRunes (512). See
// looksLikeProse for the measurement behind the value.
const proseObjectRuneBound = 80

// looksLikeProse reports whether object reads as a sentence rather than an
// entity name (MEM-05). UpsertFact mints an Entity vertex for the object
// unconditionally under a UNIQUE index on Entity.name, so a sentence-shaped
// object becomes a junk node nothing can address by name again; the detail
// belongs in Statement, the field that gets embedded and searched.
//
// Pure function of its input: no shared state, no lock, so concurrent
// writes are still arbitrated exactly as before, by the pre-existing UNIQUE
// index on Entity.name -- this adds no new synchronization surface.
func looksLikeProse(object string) bool {
	if utf8.RuneCountInString(object) > proseObjectRuneBound {
		return true
	}
	if strings.ContainsAny(object, "\n\r") {
		return true
	}
	trimmed := strings.TrimSpace(object)
	if trimmed == "" {
		return false
	}
	switch trimmed[len(trimmed)-1] {
	case '.', '!', '?':
		return true
	}
	return false
}

// Validate rejects a fact that could not be answered for later.
func (f Fact) Validate() error {
	return f.validate(defaultMemoryLimits)
}

func (f Fact) validate(limits MemoryLimits) error {
	limits = limits.normalized()
	switch {
	case strings.TrimSpace(f.Subject) == "":
		return fmt.Errorf("arcadedb: fact subject must be non-empty")
	case strings.TrimSpace(f.Predicate) == "":
		return fmt.Errorf("arcadedb: fact predicate must be non-empty")
	case strings.TrimSpace(f.Object) == "":
		return fmt.Errorf("arcadedb: fact object must be non-empty")
	case strings.TrimSpace(f.Statement) == "":
		return fmt.Errorf("arcadedb: fact statement must be non-empty")
	case strings.TrimSpace(f.Source.RunID) == "":
		return fmt.Errorf("arcadedb: fact source run_id must be non-empty")
	// D-10: WriterRole is host-derived (cmd/arcadedb-mcp's hostDerivedActor,
	// or any other in-process caller building a Fact directly) and MUST be
	// one of the two known values -- never empty, never a third string. A
	// missing/unknown role is a wiring bug the caller must fix, not a fact
	// this package can guess an attribution for and write anyway.
	case f.Source.WriterRole != WriterParent && f.Source.WriterRole != WriterWorker:
		return fmt.Errorf("arcadedb: fact source writer_role must be %q or %q, got %q",
			WriterParent, WriterWorker, f.Source.WriterRole)
	// MEM-05 (D-18): the object names an entity, not prose -- the detail
	// belongs in statement, which is the field that gets embedded and
	// searched. Subject is deliberately NOT checked here; MEM-04's subject
	// work is plan 45-07's.
	case looksLikeProse(f.Object):
		return fmt.Errorf("arcadedb: fact object reads as prose, not an entity name; " +
			"put the detail in statement")
	case !f.ValidFrom.IsZero() && !f.ValidTo.IsZero() && !f.ValidTo.After(f.ValidFrom):
		return fmt.Errorf("arcadedb: fact valid_to must be after valid_from")
	}
	fields := []struct {
		name  string
		value string
		limit int
	}{
		{"subject", f.Subject, limits.EntityRunes},
		{"subject_kind", f.SubjectKind, limits.PredicateRunes},
		{"predicate", f.Predicate, limits.PredicateRunes},
		{"object", f.Object, limits.EntityRunes},
		{"object_kind", f.ObjectKind, limits.PredicateRunes},
		{"statement", f.Statement, limits.StatementRunes},
		{"source run_id", f.Source.RunID, limits.SourceRunIDRunes},
	}
	for _, field := range fields {
		if err := validateRuneLimit(field.name, field.value, field.limit); err != nil {
			return err
		}
	}
	if len(f.Source.MemoryIDs) > limits.SourceMemoryIDs {
		return fmt.Errorf("arcadedb: fact source memory_ids exceeds %d items", limits.SourceMemoryIDs)
	}
	for _, sourceID := range f.Source.MemoryIDs {
		if err := validateRuneLimit("source_memory_id", sourceID, limits.SourceMemoryIDRunes); err != nil {
			return err
		}
	}
	return nil
}

const upsertEntityStatement = "UPDATE Entity SET name = :name UPSERT " +
	"RETURN AFTER WHERE name = :name"

const upsertTypedEntityStatement = "UPDATE Entity SET name = :name, kind = :kind UPSERT " +
	"RETURN AFTER WHERE name = :name"

const createFactStatement = "CREATE EDGE " + factEdgeType +
	" FROM (SELECT FROM Entity WHERE name = :subject_name)" +
	" TO (SELECT FROM Entity WHERE name = :object_name)" +
	" SET statement = :statement," +
	" predicate = :predicate, valid_from = :valid_from, valid_to = :valid_to," +
	" created_at = :created_at, expired_at = :expired_at," +
	" fact_key = :fact_key, sources = :sources"

// createFactEmbeddingClause is appended only when there IS a vector.
//
// It is a separate clause rather than a permanent column of createFactStatement
// because the two cases are genuinely different writes: with no embedder, or with
// the sidecar down, there is nothing to store, and asking the engine to SET an
// LSM_VECTOR-indexed property to null is a question worth not asking. The fact is
// still written, still found lexically, and the memory_embed_backfill sweep fills
// the vector in later.
//
// Its absence was the whole defect: the vector was computed on every write, passed
// as a bind parameter, and dropped on the floor because no SET named it. ArcadeDB
// accepts unused params silently, so every fact in every database was stored
// without its vector and nothing anywhere said so.
const createFactEmbeddingClause = ", embedding = :embedding"

// UpsertFact writes one fact, closing any fact it supersedes.
func (c *Client) UpsertFact(ctx context.Context, fact Fact, now time.Time) (FactWrite, error) {
	if err := fact.validate(c.memoryLimits()); err != nil {
		return FactWrite{}, err
	}
	fact = normalizeFact(fact)
	validFrom := fact.ValidFrom
	if validFrom.IsZero() {
		validFrom = now
	}
	for _, entity := range []struct{ name, kind string }{{fact.Subject, fact.SubjectKind}, {fact.Object, fact.ObjectKind}} {
		statement := upsertEntityStatement
		params := map[string]any{"name": entity.name}
		if entity.kind != "" {
			statement = upsertTypedEntityStatement
			params["kind"] = entity.kind
		}
		if _, err := c.upsertEntityWithRetry(ctx, statement, params); err != nil {
			return FactWrite{}, fmt.Errorf("arcadedb: upsert entity %q: %w", entity.name, err)
		}
	}
	factKey := factIdentity(fact)
	// Serializes this whole attach-or-create-or-supersede sequence against
	// any OTHER concurrent UpsertFact call for the SAME fact_key in this
	// process -- see fact_lock.go's doc comment for why this, not just the
	// transactional write inside attachFactSource, is what actually closes
	// D-09's concurrent-write race for the topology this package has.
	defer c.facts.lock(factKey)()
	written := FactWrite{Statement: fact.Statement}
	if attached, err := c.attachFactSource(ctx, factKey, fact.Source, now); err != nil {
		return FactWrite{}, err
	} else if attached {
		return written, nil
	}
	if fact.Supersedes {
		// D-11: the authority check runs BEFORE closeSuperseded is ever called,
		// so a worker's refusal issues zero Command calls against the fact it
		// asked to close -- not merely a refusal closeSuperseded itself decides
		// to make. Same early-return shape as closeSuperseded's own ambiguity
		// refusal: nothing closed, and the new fact itself is not created
		// either, so a refused correction never silently becomes a different
		// write than the one asked for.
		if allowed, reason := maySupersede(actorFromSource(fact.Source)); !allowed {
			return FactWrite{Statement: fact.Statement, Refused: true, Reason: reason}, nil
		}
		outcome, err := c.closeSuperseded(ctx, fact, factKey, validFrom, now)
		if err != nil {
			return FactWrite{}, err
		}
		if outcome.Refused {
			return FactWrite{
				Statement:  fact.Statement,
				Refused:    true,
				Reason:     outcome.Reason,
				Candidates: outcome.Candidates,
			}, nil
		}
		written.Superseded = outcome.Closed
	}
	params := map[string]any{
		"subject_name": fact.Subject,
		"object_name":  fact.Object,
		"statement":    fact.Statement,
		"predicate":    fact.Predicate,
		"valid_from":   validFrom.UTC().Format(time.RFC3339),
		"valid_to":     nullableTime(fact.ValidTo),
		"created_at":   now.UTC().Format(time.RFC3339),
		"expired_at":   nil,
		"fact_key":     activeFactKey(factKey, fact.ValidTo, now),
		"sources":      sourcesParam([]FactSource{fact.Source}),
	}
	statement := createFactStatement
	// nil when no embedder is configured or the sidecar is down, which stores the
	// fact without a vector rather than refusing the write. It stays reachable
	// lexically, and the memory_embed_backfill sweep embeds it within minutes.
	if vector := c.embedStatement(ctx, fact.Statement); vector != nil {
		params["embedding"] = vector
		statement += createFactEmbeddingClause
	}
	if err := c.createFactWithRetry(ctx, statement, params, factKey, fact.Source, now); err != nil {
		return FactWrite{}, err
	}
	return written, nil
}

// FactHit is one retrieved fact.
type FactHit struct {
	Statement   string       `json:"statement"`
	Predicate   string       `json:"predicate"`
	Subject     string       `json:"subject"`
	SubjectKind string       `json:"subject_kind,omitempty"`
	Object      string       `json:"object"`
	ObjectKind  string       `json:"object_kind,omitempty"`
	ValidFrom   string       `json:"valid_from"`
	ValidTo     string       `json:"valid_to,omitempty"`
	Sources     []FactSource `json:"sources"`
	// FactKey is this fact's content-derived identity (factIdentity), the
	// same value a correction names to close exactly this edge. Empty for a
	// fact that is already closed -- the property is NULLed on close.
	FactKey string `json:"fact_key,omitempty"`
}

// searchFactsStatement is the entry point for when the question does not name
// an entity: the Lucene index over the statement text, stemmed. It reads the
// edges directly, so endpoints resolve -- outV()/inV(), never the dotted
// `out.name` form, which yields NULL on an edge instead of failing.
const searchFactsStatement = "SELECT statement, predicate, valid_from, valid_to, " +
	"sources, fact_key, outV().name AS subject, outV().kind AS subject_kind, " +
	"inV().name AS object, inV().kind AS object_kind " +
	"FROM " + factEdgeType + " WHERE SEARCH_INDEX('" + factEdgeType +
	"[statement]', :query) = true AND $score >= :min_lexical_score"

// asOfFilter keeps only facts whose validity window contains the instant. A
// NULL valid_to means "still true", which is why it cannot be a plain
// comparison. It extends the hydration statement's existing WHERE.
const asOfCondition = "valid_from <= :as_of AND (valid_to IS NULL OR valid_to > :as_of)"

// asOfFilter extends an existing WHERE; asOfCondition is the same test for a
// statement that has none of its own to extend (see the digest).
const asOfFilter = " AND " + asOfCondition

// SearchFacts finds facts by their words, restricted to those valid at asOf.
// asOf defaults to now: a search with no instant means "what is true", and
// letting expired facts through is how a superseded answer wins.
func (c *Client) SearchFacts(
	ctx context.Context,
	query string,
	limit int,
	asOf time.Time,
) ([]FactHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("arcadedb: search query must be non-empty")
	}
	limits := c.memoryLimits()
	if err := validateRuneLimit("search query", query, limits.QueryRunes); err != nil {
		return nil, err
	}
	limit = boundedLimit(limit, 5, limits.Results)
	if asOf.IsZero() {
		asOf = time.Now()
	}
	rows, err := c.Query(ctx, searchFactsStatement+asOfFilter+" LIMIT "+strconv.Itoa(limit),
		map[string]any{
			"query":             escapeLucene(query),
			"min_lexical_score": lexicalScoreFloor(query, limits.LexicalMinScore),
			"as_of":             asOf.UTC().Format(time.RFC3339),
		})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: search facts: %w", err)
	}
	hits := make([]FactHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, factHitFromRow(row))
	}
	return hits, nil
}

// factsAboutStatement reads an entity's facts by walking its edges. When the
// question names the entity this is the whole answer: exact, nothing to rank
// and nothing to tune. The full-text search above is for when it does not.
//
// BOTH directions, and that is the whole point. Matching only outV() was silent
// and severe: measured on 1002 facts, "PETRELLI ENRICO" -- a salesperson named
// as the object of eighteen facts -- returned NOTHING, while memory_entities
// listed them with eighteen and graph_schema saw them. An entity that is always
// spoken ABOUT rather than speaking is exactly the kind a memory is asked about,
// and the hubs of any real graph sit on that side. memory_forget already walked
// both directions, so the surface disagreed with itself as well.
const factsAboutStatement = "SELECT statement, predicate, valid_from, valid_to, " +
	"sources, fact_key, outV().name AS subject, outV().kind AS subject_kind, " +
	"inV().name AS object, inV().kind AS object_kind " +
	"FROM " + factEdgeType + " WHERE (outV().name = :entity OR inV().name = :entity)"

const factsAboutPredicateFilter = " AND predicate = :predicate"

// FactsAbout returns the facts whose subject is entity, valid at asOf.
// predicate narrows to one relation when given. asOf defaults to now.
func (c *Client) FactsAbout(
	ctx context.Context,
	entity string,
	predicate string,
	limit int,
	asOf time.Time,
) ([]FactHit, error) {
	if strings.TrimSpace(entity) == "" {
		return nil, fmt.Errorf("arcadedb: entity must be non-empty")
	}
	limits := c.memoryLimits()
	if err := validateRuneLimit("entity", entity, limits.EntityRunes); err != nil {
		return nil, err
	}
	if err := validateRuneLimit("predicate", predicate, limits.PredicateRunes); err != nil {
		return nil, err
	}
	limit = boundedLimit(limit, 20, limits.Results)
	if asOf.IsZero() {
		asOf = time.Now()
	}
	statement := factsAboutStatement + asOfFilter
	params := map[string]any{
		"entity": entity,
		"as_of":  asOf.UTC().Format(time.RFC3339),
	}
	if predicate = strings.TrimSpace(predicate); predicate != "" {
		statement += factsAboutPredicateFilter
		params["predicate"] = predicate
	}
	rows, err := c.Query(ctx, statement+" LIMIT "+strconv.Itoa(limit), params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: facts about %q: %w", entity, err)
	}
	hits := make([]FactHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, factHitFromRow(row))
	}
	return hits, nil
}

func validateRuneLimit(field, value string, limit int) error {
	if utf8.RuneCountInString(value) > limit {
		return fmt.Errorf("arcadedb: %s exceeds %d characters", field, limit)
	}
	return nil
}

func boundedLimit(value, fallback, maximum int) int {
	if value <= 0 {
		value = fallback
	}
	return min(value, maximum)
}

// SEARCH_INDEX already proves a one-token lookup matched; the configured floor
// is for rejecting longer claims whose only lexical evidence is one named entity.
func lexicalScoreFloor(query string, configured float64) float64 {
	if len(strings.Fields(query)) <= 1 {
		return 0
	}
	return configured
}

func factHitFromRow(row map[string]any) FactHit {
	return FactHit{
		Statement:   rowString(row, "statement"),
		Predicate:   rowString(row, "predicate"),
		Subject:     rowString(row, "subject"),
		SubjectKind: rowString(row, "subject_kind"),
		Object:      rowString(row, "object"),
		ObjectKind:  rowString(row, "object_kind"),
		ValidFrom:   rowString(row, "valid_from"),
		ValidTo:     rowString(row, "valid_to"),
		Sources:     factSources(row["sources"]),
		FactKey:     rowString(row, "fact_key"),
	}
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func countUpdated(rows []map[string]any) int {
	total := 0
	for _, row := range rows {
		switch typed := row["count"].(type) {
		case float64:
			total += int(typed)
		case int:
			total += typed
		default:
			total++
		}
	}
	return total
}

// rowInt tolerates JSON's float64 as well as the integer types a future decoder
// might hand over.
func rowInt(row map[string]any, key string) int64 {
	switch typed := row[key].(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	}
	return 0
}

func rowString(row map[string]any, key string) string {
	text, _ := row[key].(string)
	return text
}

func rowStrings(row map[string]any, key string) []string {
	items, ok := row[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
