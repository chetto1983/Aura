package arcadedb

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Forgetting is the operation the memory model was missing.
//
// UpsertFact deliberately never deletes: a contradiction closes a validity
// window so the past stays answerable. That is right for a fact that STOPPED
// being true, and wrong for one that was never true at all -- a probe, a bad
// extraction, a run that wrote under the wrong identity. Superseding those
// leaves them in the graph, still returned by an as_of query, forever.
//
// So provenance is what removal is keyed on. source_run_id is documented as
// "required so everything a run produced can be found and removed", and until
// now nothing could remove it; this closes that. Aura has already paid for the
// gap once, when test entities written under the operator's real identity had
// to be cleaned out by hand.
//
// Deleting is guarded by construction: a filter that names nothing is refused
// rather than treated as "everything".

// ForgetFilter names what to remove. At least one of SourceRunID, Entity or
// Subject must be set -- an empty filter is an error, never a wildcard.
type ForgetFilter struct {
	// SourceRunID removes everything one run wrote. This is the undo.
	SourceRunID string
	// Subject, with the optional Predicate and Object, removes specific facts.
	Subject   string
	Predicate string
	Object    string
	// Entity removes every fact touching it, in either direction, and then the
	// entity itself.
	Entity string
	// DryRun counts what would go without removing any of it.
	DryRun bool
	// KeepOrphans leaves behind entities that this removal stripped of their
	// last fact. They are pruned by default because an entity with no facts is
	// a name with nothing attached to it.
	KeepOrphans bool
}

// ForgetResult reports what went, or what would have.
type ForgetResult struct {
	Facts    int  `json:"facts"`
	Entities int  `json:"entities"`
	DryRun   bool `json:"dry_run"`
}

// Forget removes facts matching filter, and the entities they leave stranded.
func (c *Client) Forget(ctx context.Context, filter ForgetFilter) (ForgetResult, error) {
	clause, params, err := filter.where()
	if err != nil {
		return ForgetResult{}, err
	}

	// The endpoints are read BEFORE the delete: afterwards there is no edge left
	// to ask which entities it joined.
	endpoints, facts, err := c.factEndpoints(ctx, clause, params)
	if err != nil {
		return ForgetResult{}, err
	}
	// An entity named outright is a prune candidate even when no fact matched:
	// otherwise "remove this entity and its facts" silently leaves the entity
	// behind whenever it had none, which is exactly when it is pure noise.
	if entity := strings.TrimSpace(filter.Entity); entity != "" {
		endpoints = appendUnique(endpoints, entity)
	}
	if filter.DryRun {
		orphans := 0
		if !filter.KeepOrphans {
			orphans, err = c.countStrandable(ctx, endpoints, clause, params)
			if err != nil {
				return ForgetResult{}, err
			}
		}
		return ForgetResult{Facts: facts, Entities: orphans, DryRun: true}, nil
	}
	if facts > 0 {
		if _, err = c.Command(ctx,
			"DELETE FROM "+factEdgeType+" WHERE "+clause, params); err != nil {
			return ForgetResult{}, fmt.Errorf("arcadedb: forget facts: %w", err)
		}
	}

	removed := 0
	if !filter.KeepOrphans {
		if removed, err = c.pruneOrphans(ctx, endpoints); err != nil {
			return ForgetResult{Facts: facts}, err
		}
	}
	return ForgetResult{Facts: facts, Entities: removed}, nil
}

// where builds the filter's SQL, refusing an empty one. The clauses are ANDed,
// so naming a run and a subject narrows to that subject's facts from that run
// rather than removing both.
func (f ForgetFilter) where() (string, map[string]any, error) {
	clauses := []string{}
	params := map[string]any{}
	if run := strings.TrimSpace(f.SourceRunID); run != "" {
		clauses = append(clauses, "source_run_id = :source_run_id")
		params["source_run_id"] = run
	}
	if entity := strings.TrimSpace(f.Entity); entity != "" {
		clauses = append(clauses, "(outV().name = :entity OR inV().name = :entity)")
		params["entity"] = entity
	}
	if subject := strings.TrimSpace(f.Subject); subject != "" {
		clauses = append(clauses, "outV().name = :subject")
		params["subject"] = subject
	}
	if predicate := strings.TrimSpace(f.Predicate); predicate != "" {
		clauses = append(clauses, "predicate = :predicate")
		params["predicate"] = predicate
	}
	if object := strings.TrimSpace(f.Object); object != "" {
		clauses = append(clauses, "inV().name = :object")
		params["object"] = object
	}
	if len(clauses) == 0 {
		return "", nil, fmt.Errorf(
			"arcadedb: forget needs source_run_id, entity or subject; " +
				"an empty filter would remove the whole memory")
	}
	// A predicate or object on its own is a filter with no anchor: it would
	// match every subject in the graph, which is not what anyone means by it.
	if params["source_run_id"] == nil && params["entity"] == nil && params["subject"] == nil {
		return "", nil, fmt.Errorf(
			"arcadedb: predicate and object narrow a removal, they cannot define one; " +
				"name a source_run_id, entity or subject as well")
	}
	return strings.Join(clauses, " AND "), params, nil
}

// factEndpoints returns the distinct entity names the matching facts join, and
// how many facts matched.
func (c *Client) factEndpoints(
	ctx context.Context,
	clause string,
	params map[string]any,
) ([]string, int, error) {
	rows, err := c.Query(ctx,
		"SELECT outV().name AS subject, inV().name AS object FROM "+
			factEdgeType+" WHERE "+clause, params)
	if err != nil {
		return nil, 0, fmt.Errorf("arcadedb: read facts to forget: %w", err)
	}
	names := []string{}
	for _, row := range rows {
		for _, key := range []string{"subject", "object"} {
			names = appendUnique(names, rowString(row, key))
		}
	}
	return names, len(rows), nil
}

func appendUnique(names []string, name string) []string {
	if name == "" || slices.Contains(names, name) {
		return names
	}
	return append(names, name)
}

// pruneOrphans removes those endpoints left with no edges at all. The degree is
// re-read per entity rather than inferred, because an entity may hold facts
// this removal did not match.
func (c *Client) pruneOrphans(ctx context.Context, names []string) (int, error) {
	removed := 0
	for _, name := range names {
		rows, err := c.Command(ctx,
			"DELETE FROM Entity WHERE name = :name AND bothE().size() = 0",
			map[string]any{"name": name})
		if err != nil {
			return removed, fmt.Errorf("arcadedb: prune entity %q: %w", name, err)
		}
		removed += countUpdated(rows)
	}
	return removed, nil
}

// countStrandable answers the dry run's second question -- how many entities
// WOULD be left with nothing -- without removing anything. An endpoint is
// stranded when its total degree equals the number of edges this filter takes.
//
// It is two plain queries rather than one clever projection: ArcadeDB's `[…]`
// operator indexes a collection, it does not filter one, so the tempting
// `bothE('FACT')[clause].size()` is not a narrower count but a syntax error.
func (c *Client) countStrandable(
	ctx context.Context,
	names []string,
	clause string,
	params map[string]any,
) (int, error) {
	stranded := 0
	for _, name := range names {
		total, err := c.Query(ctx,
			"SELECT bothE().size() AS degree FROM Entity WHERE name = :name",
			map[string]any{"name": name})
		if err != nil {
			return 0, fmt.Errorf("arcadedb: read degree of %q: %w", name, err)
		}
		scoped := maps.Clone(params)
		scoped["name"] = name
		going, err := c.Query(ctx,
			"SELECT count(*) AS n FROM "+factEdgeType+
				" WHERE (outV().name = :name OR inV().name = :name) AND "+clause, scoped)
		if err != nil {
			return 0, fmt.Errorf("arcadedb: count edges leaving %q: %w", name, err)
		}
		if len(total) == 1 && len(going) == 1 &&
			rowInt(total[0], "degree") == rowInt(going[0], "n") {
			stranded++
		}
	}
	return stranded, nil
}
