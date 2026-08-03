package arcadedb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The memory model: the fact lives on the EDGE and is bitemporal.
//
// There is no embedding. Retrieval is the graph -- walk an entity's edges when
// the question names it, and the Lucene index over the statement text when it
// does not. Both are exact and cost a single round trip; a vector layer on top
// added an embedding call per read and per write, a fusion stage to tune, and
// three ranking defects, to answer questions a traversal answers outright.
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
// Provenance (source_run_id, source_memory_ids) is mandatory, taken from
// Cognee, whose graph interface spends a third of its methods on source refs.
// Aura has already paid for its absence once: test entities written under the
// operator's real identity had no discriminator and had to be cleaned by hand.
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
		"CREATE PROPERTY " + factEdgeType + ".source_run_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + factEdgeType + ".source_memory_ids IF NOT EXISTS LIST",

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
	for _, statement := range append(memorySchemaStatements(), vectorSchemaStatements()...) {
		if _, err := c.Command(ctx, statement, nil); err != nil {
			return fmt.Errorf("arcadedb: ensure memory schema: %w", err)
		}
	}
	return nil
}

// Fact is one assertion about the world, as stored on an edge.
type Fact struct {
	Subject         string
	Predicate       string
	Object          string
	Statement       string
	ValidFrom       time.Time
	ValidTo         time.Time
	SourceRunID     string
	SourceMemoryIDs []string
	// Supersedes closes any still-valid fact sharing this subject and predicate.
	// It is explicit rather than inferred because some predicates are
	// single-valued ("lives in") and others are not ("likes").
	Supersedes bool
}

// FactWrite reports what an upsert did.
type FactWrite struct {
	Statement  string
	Superseded int
}

// Validate rejects a fact that could not be answered for later.
func (f Fact) Validate() error {
	switch {
	case strings.TrimSpace(f.Subject) == "":
		return fmt.Errorf("arcadedb: fact subject must be non-empty")
	case strings.TrimSpace(f.Predicate) == "":
		return fmt.Errorf("arcadedb: fact predicate must be non-empty")
	case strings.TrimSpace(f.Object) == "":
		return fmt.Errorf("arcadedb: fact object must be non-empty")
	case strings.TrimSpace(f.Statement) == "":
		return fmt.Errorf("arcadedb: fact statement must be non-empty")
	case strings.TrimSpace(f.SourceRunID) == "":
		return fmt.Errorf("arcadedb: fact source_run_id must be non-empty")
	case !f.ValidFrom.IsZero() && !f.ValidTo.IsZero() && !f.ValidTo.After(f.ValidFrom):
		return fmt.Errorf("arcadedb: fact valid_to must be after valid_from")
	}
	return nil
}

const upsertEntityStatement = "UPDATE Entity SET name = :name UPSERT " +
	"RETURN AFTER WHERE name = :name"

// closeSupersededStatement ends the validity window of every still-valid fact
// with the same subject and predicate. It does not delete: the row stays
// queryable through `as_of`.
//
// The object is deliberately NOT in the WHERE clause -- the object is the thing
// that changed, so matching on it would mean the statement could never fire.
//
// Endpoints are read with outV()/inV(). The dotted `out.name` form returns NULL
// on an edge rather than failing, so getting this wrong is silent.
const closeSupersededStatement = "UPDATE " + factEdgeType + " SET valid_to = :valid_to, " +
	"expired_at = :expired_at WHERE predicate = :predicate AND valid_to IS NULL " +
	"AND outV().name = :subject_name"

const createFactStatement = "CREATE EDGE " + factEdgeType +
	" FROM (SELECT FROM Entity WHERE name = :subject_name)" +
	" TO (SELECT FROM Entity WHERE name = :object_name)" +
	" SET statement = :statement," +
	" predicate = :predicate, valid_from = :valid_from, valid_to = :valid_to," +
	" created_at = :created_at, expired_at = :expired_at," +
	" source_run_id = :source_run_id, source_memory_ids = :source_memory_ids"

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
	if err := fact.Validate(); err != nil {
		return FactWrite{}, err
	}
	validFrom := fact.ValidFrom
	if validFrom.IsZero() {
		validFrom = now
	}
	for _, name := range []string{fact.Subject, fact.Object} {
		if _, err := c.Command(ctx, upsertEntityStatement, map[string]any{"name": name}); err != nil {
			return FactWrite{}, fmt.Errorf("arcadedb: upsert entity %q: %w", name, err)
		}
	}
	written := FactWrite{Statement: fact.Statement}
	if fact.Supersedes {
		rows, err := c.Command(ctx, closeSupersededStatement, map[string]any{
			"valid_to":     validFrom.UTC().Format(time.RFC3339),
			"expired_at":   now.UTC().Format(time.RFC3339),
			"predicate":    fact.Predicate,
			"subject_name": fact.Subject,
			"object_name":  fact.Object,
		})
		if err != nil {
			return FactWrite{}, fmt.Errorf("arcadedb: close superseded facts: %w", err)
		}
		written.Superseded = countUpdated(rows)
	}
	params := map[string]any{
		"subject_name":      fact.Subject,
		"object_name":       fact.Object,
		"statement":         fact.Statement,
		"predicate":         fact.Predicate,
		"valid_from":        validFrom.UTC().Format(time.RFC3339),
		"valid_to":          nullableTime(fact.ValidTo),
		"created_at":        now.UTC().Format(time.RFC3339),
		"expired_at":        nil,
		"source_run_id":     fact.SourceRunID,
		"source_memory_ids": fact.SourceMemoryIDs,
	}
	statement := createFactStatement
	// nil when no embedder is configured or the sidecar is down, which stores the
	// fact without a vector rather than refusing the write. It stays reachable
	// lexically, and the memory_embed_backfill sweep embeds it within minutes.
	if vector := c.embedStatement(ctx, fact.Statement); vector != nil {
		params["embedding"] = vector
		statement += createFactEmbeddingClause
	}
	if _, err := c.Command(ctx, statement, params); err != nil {
		return FactWrite{}, fmt.Errorf("arcadedb: create fact: %w", err)
	}
	return written, nil
}

// FactHit is one retrieved fact.
type FactHit struct {
	Statement       string
	Predicate       string
	Subject         string
	Object          string
	ValidFrom       string
	ValidTo         string
	SourceRunID     string
	SourceMemoryIDs []string
}

// searchFactsStatement is the entry point for when the question does not name
// an entity: the Lucene index over the statement text, stemmed. It reads the
// edges directly, so endpoints resolve -- outV()/inV(), never the dotted
// `out.name` form, which yields NULL on an edge instead of failing.
const searchFactsStatement = "SELECT statement, predicate, valid_from, valid_to, " +
	"source_run_id, source_memory_ids, outV().name AS subject, inV().name AS object " +
	"FROM " + factEdgeType + " WHERE SEARCH_INDEX('" + factEdgeType +
	"[statement]', :query)"

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
	if limit <= 0 {
		limit = 5
	}
	if asOf.IsZero() {
		asOf = time.Now()
	}
	rows, err := c.Query(ctx, searchFactsStatement+asOfFilter+" LIMIT "+strconv.Itoa(limit),
		map[string]any{
			"query": query,
			"as_of": asOf.UTC().Format(time.RFC3339),
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
	"source_run_id, source_memory_ids, outV().name AS subject, inV().name AS object " +
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
	if limit <= 0 {
		limit = 20
	}
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

func factHitFromRow(row map[string]any) FactHit {
	return FactHit{
		Statement:       rowString(row, "statement"),
		Predicate:       rowString(row, "predicate"),
		Subject:         rowString(row, "subject"),
		Object:          rowString(row, "object"),
		ValidFrom:       rowString(row, "valid_from"),
		ValidTo:         rowString(row, "valid_to"),
		SourceRunID:     rowString(row, "source_run_id"),
		SourceMemoryIDs: rowStrings(row, "source_memory_ids"),
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
