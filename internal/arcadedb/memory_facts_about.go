package arcadedb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

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
// factsAboutProjection is shared with the second hop so both depths return the
// same columns; a caller must not be able to tell them apart by shape.
const factsAboutProjection = "SELECT statement, predicate, valid_from, valid_to, " +
	"sources, fact_key, outV().name AS subject, outV().kind AS subject_kind, " +
	"inV().name AS object, inV().kind AS object_kind"

const factsAboutStatement = factsAboutProjection +
	" FROM " + factEdgeType + " WHERE (outV().name = :entity OR inV().name = :entity)"

const factsAboutPredicateFilter = " AND predicate = :predicate"

// FactsAbout returns the facts whose subject is entity, valid at asOf.
// predicate narrows to one relation when given. asOf defaults to now. depth 1 is
// the entity's own facts; depth 2 also reaches the ones sharing a mentioned
// entity with them (memory_mentions_read.go).
func (c *Client) FactsAbout(
	ctx context.Context,
	entity string,
	predicate string,
	limit int,
	asOf time.Time,
	depth int,
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
	statement, err := factsAboutStatementForDepth(depth)
	if err != nil {
		return nil, err
	}
	statement += asOfFilter
	params := map[string]any{
		"entity": entity,
		"as_of":  asOf.UTC().Format(time.RFC3339),
	}
	if predicate = strings.TrimSpace(predicate); predicate != "" {
		statement += factsAboutPredicateFilter
		params["predicate"] = predicate
	}
	statement += factsAboutOrdering(depth)
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

// The second hop.
//
// depth 1 is the statement that shipped, unchanged. That is a constraint, not an
// accident: memory_facts_about is the exact path taken whenever a question names
// an entity, and a regression there would be worse than the absent second hop it
// is being traded for. The depth-2 statement is a separate constant for the same
// reason -- nothing composes them, so nothing can perturb the first by editing
// the second.
//
// The neighbourhood is TWO MENTIONS hops, not one. A fact reaches a mentioned
// entity through its own endpoints, so a fact that mentions `ArcadeDB` links its
// subject and object to `ArcadeDB`; another fact mentioning `ArcadeDB` links ITS
// endpoints to the same vertex. Reaching from the first fact's endpoints to the
// second's therefore costs two hops: out to the shared entity, and back down.

// Exported because FactsAbout's depth parameter is exported: callers outside this
// package (internal/runner, cmd/arcadedb-mcp) otherwise have to write a bare 1 or 2,
// and the validation error already spells the vocabulary out ("must be %d or %d")
// while refusing to share the names for it.
const (
	FactsAboutDirect        = 1
	FactsAboutNeighbourhood = 2
)

// mentionNeighbourhood is the vertex set within two MENTIONS hops of :entity,
// the entity itself included. ArcadeDB traverses through index-free adjacency, so
// each hop is O(1) in the graph size and the cost is the neighbourhood's own
// size -- which is what the hub cap exists to bound.
//
// Naming the traversal twice, once per endpoint, rather than binding it once with
// LET: verified on 26.9.1 that this shape returns each fact EXACTLY ONCE without
// DISTINCT, because the WHERE runs per FACT record however many reach-set vertices
// it matches. The MATCH and Cypher forms of the same question both duplicate --
// nine rows where four were due -- and would need a DISTINCT whose absence nothing
// would have caught.
const mentionNeighbourhood = "(SELECT FROM (TRAVERSE both('" + mentionsEdgeType +
	"') FROM (SELECT FROM Entity WHERE name = :entity) WHILE $depth <= 2))"

const factsNearStatement = factsAboutProjection + " FROM " + factEdgeType +
	" WHERE (outV() IN " + mentionNeighbourhood + " OR inV() IN " + mentionNeighbourhood + ")"

// factsNearOrdering is on the second hop only. The first hop has never ordered
// its rows and adding one there would change what ships today; the second hop has
// no such history, and a neighbourhood that reshuffles between two identical
// calls is not something a caller can reason about.
const factsNearOrdering = " ORDER BY created_at DESC, fact_key ASC"

func factsAboutStatementForDepth(depth int) (string, error) {
	switch depth {
	case FactsAboutDirect:
		return factsAboutStatement, nil
	case FactsAboutNeighbourhood:
		return factsNearStatement, nil
	default:
		return "", fmt.Errorf(
			"arcadedb: facts depth must be %d or %d, got %d",
			FactsAboutDirect, FactsAboutNeighbourhood, depth)
	}
}

func factsAboutOrdering(depth int) string {
	if depth == FactsAboutNeighbourhood {
		return factsNearOrdering
	}
	return ""
}
