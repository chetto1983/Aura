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
// Both depths decode into FactHit; the direct SQL path retains its behavior.
const factsAboutProjection = "SELECT statement, predicate, valid_from, valid_to, " +
	"sources, fact_key, outV().name AS subject, outV().kind AS subject_kind, " +
	"inV().name AS object, inV().kind AS object_kind, @rid AS rid"

const factsAboutStatement = factsAboutProjection +
	" FROM " + factEdgeType + " WHERE (outV().name = :entity OR inV().name = :entity)"

const factsAboutPredicateFilter = " AND predicate = :predicate"

// FactsAbout returns facts touching entity, valid at asOf.
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
	predicate = strings.TrimSpace(predicate)
	if depth == FactsAboutNeighbourhood {
		return c.factsAboutNeighborhood(ctx, entity, predicate, limit, asOf)
	}
	if depth != FactsAboutDirect {
		return nil, fmt.Errorf("arcadedb: facts depth must be %d or %d, got %d", FactsAboutDirect, FactsAboutNeighbourhood, depth)
	}
	statement := factsAboutStatement + asOfFilter
	params := map[string]any{
		"entity": entity,
		"as_of":  asOf.UTC().Format(time.RFC3339),
	}
	if predicate != "" {
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

// Exported because FactsAbout's depth parameter is exported: callers outside this
// package (internal/runner, cmd/arcadedb-mcp) otherwise have to write a bare 1 or 2,
// and the validation error already spells the vocabulary out ("must be %d or %d")
// while refusing to share the names for it.
const (
	FactsAboutDirect        = 1
	FactsAboutNeighbourhood = 2
)
