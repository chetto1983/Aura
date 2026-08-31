package arcadedb

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// ForgetFilter names what to remove. At least one of SourceRunID, Entity or
// Subject must be set -- an empty filter is an error, never a wildcard.
type ForgetFilter struct {
	SourceRunID string
	Subject     string
	Predicate   string
	Object      string
	Entity      string
	DryRun      bool
	KeepOrphans bool
}

// ForgetResult reports what went, or what would have.
type ForgetResult struct {
	Facts    int  `json:"facts"`
	Entities int  `json:"entities"`
	DryRun   bool `json:"dry_run"`
}

func normalizeForgetFilter(filter ForgetFilter) ForgetFilter {
	filter.SourceRunID = strings.TrimSpace(filter.SourceRunID)
	filter.Subject = strings.TrimSpace(filter.Subject)
	filter.Predicate = strings.TrimSpace(filter.Predicate)
	filter.Object = strings.TrimSpace(filter.Object)
	filter.Entity = strings.TrimSpace(filter.Entity)
	return filter
}

func (f ForgetFilter) matchesMemoryBatchFact(fact memoryBatchFact) bool {
	if f.SourceRunID != "" {
		matched := false
		for _, source := range fact.Sources {
			matched = matched || source.RunID == f.SourceRunID
		}
		if !matched {
			return false
		}
	}
	if f.Entity != "" && fact.Fact.Subject != f.Entity && fact.Fact.Object != f.Entity {
		return false
	}
	if f.Subject != "" && fact.Fact.Subject != f.Subject {
		return false
	}
	if f.Predicate != "" && fact.Fact.Predicate != f.Predicate {
		return false
	}
	return f.Object == "" || fact.Fact.Object == f.Object
}

// Forget detaches source-scoped support and removes an edge only when no source
// remains. Entity/subject filters without a source are explicitly destructive.
func (c *Client) Forget(ctx context.Context, filter ForgetFilter) (ForgetResult, error) {
	filter = normalizeForgetFilter(filter)
	if err := filter.validate(c.memoryLimits()); err != nil {
		return ForgetResult{}, err
	}
	clause, params, err := filter.where()
	if err != nil {
		return ForgetResult{}, err
	}
	facts, err := c.factsToForget(ctx, clause, params)
	if err != nil {
		return ForgetResult{}, err
	}
	deleteRIDs := make([]string, 0, len(facts))
	updates := make(map[string][]FactSource)
	for _, fact := range facts {
		if runID := strings.TrimSpace(filter.SourceRunID); runID != "" {
			remaining := removeFactSource(fact.sources, runID)
			if len(remaining) > 0 {
				updates[fact.rid] = remaining
				continue
			}
		}
		deleteRIDs = append(deleteRIDs, fact.rid)
	}
	endpoints := endpointsOfDeletedFacts(facts, deleteRIDs)
	if entity := strings.TrimSpace(filter.Entity); entity != "" {
		endpoints = appendUnique(endpoints, entity)
	}
	if filter.DryRun {
		orphans := 0
		if !filter.KeepOrphans {
			orphans, err = c.countStrandableByRIDs(ctx, endpoints, deleteRIDs)
			if err != nil {
				return ForgetResult{}, err
			}
		}
		return ForgetResult{Facts: len(facts), Entities: orphans, DryRun: true}, nil
	}
	for rid, sources := range updates {
		if _, err = c.Command(ctx,
			"UPDATE "+factEdgeType+" SET sources = :sources WHERE @rid = :rid",
			map[string]any{"sources": sourcesParam(sources), "rid": rid}); err != nil {
			return ForgetResult{}, fmt.Errorf("arcadedb: detach fact source: %w", err)
		}
	}
	if len(deleteRIDs) > 0 {
		if _, err = c.Command(ctx,
			"DELETE FROM "+factEdgeType+" WHERE @rid IN :rids",
			map[string]any{"rids": deleteRIDs}); err != nil {
			return ForgetResult{}, fmt.Errorf("arcadedb: forget facts: %w", err)
		}
	}
	removed := 0
	if !filter.KeepOrphans {
		if removed, err = c.pruneOrphans(ctx, endpoints); err != nil {
			return ForgetResult{Facts: len(facts)}, err
		}
	}
	return ForgetResult{Facts: len(facts), Entities: removed}, nil
}

func (f ForgetFilter) validate(limits MemoryLimits) error {
	limits = limits.normalized()
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"source run_id", f.SourceRunID, limits.SourceRunIDRunes},
		{"entity", f.Entity, limits.EntityRunes},
		{"subject", f.Subject, limits.EntityRunes},
		{"predicate", f.Predicate, limits.PredicateRunes},
		{"object", f.Object, limits.EntityRunes},
	} {
		if err := validateRuneLimit(field.name, field.value, field.limit); err != nil {
			return err
		}
	}
	return nil
}

func (f ForgetFilter) where() (string, map[string]any, error) {
	clauses := []string{}
	params := map[string]any{}
	if run := strings.TrimSpace(f.SourceRunID); run != "" {
		clauses = append(clauses, "sources CONTAINS (run_id = :source_run_id)")
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
			"arcadedb: forget needs source run_id, entity or subject; an empty filter would remove the whole memory")
	}
	if params["source_run_id"] == nil && params["entity"] == nil && params["subject"] == nil {
		return "", nil, fmt.Errorf(
			"arcadedb: predicate and object narrow a removal, they cannot define one; name a source run_id, entity or subject as well")
	}
	return strings.Join(clauses, " AND "), params, nil
}

type forgetFact struct {
	rid     string
	subject string
	object  string
	sources []FactSource
}

func (c *Client) factsToForget(
	ctx context.Context,
	clause string,
	params map[string]any,
) ([]forgetFact, error) {
	rows, err := c.Query(ctx,
		"SELECT @rid AS rid, sources, outV().name AS subject, inV().name AS object FROM "+
			factEdgeType+" WHERE "+clause, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: read facts to forget: %w", err)
	}
	facts := make([]forgetFact, 0, len(rows))
	for _, row := range rows {
		rid := rowString(row, "rid")
		if rid == "" {
			return nil, fmt.Errorf("arcadedb: fact selected for removal has no RID")
		}
		facts = append(facts, forgetFact{
			rid: rid, subject: rowString(row, "subject"), object: rowString(row, "object"),
			sources: factSources(row["sources"]),
		})
	}
	return facts, nil
}

func removeFactSource(sources []FactSource, runID string) []FactSource {
	remaining := make([]FactSource, 0, len(sources))
	for _, source := range sources {
		if source.RunID != runID {
			remaining = append(remaining, source)
		}
	}
	return remaining
}

func endpointsOfDeletedFacts(facts []forgetFact, rids []string) []string {
	names := []string{}
	for _, fact := range facts {
		if slices.Contains(rids, fact.rid) {
			names = appendUnique(names, fact.subject)
			names = appendUnique(names, fact.object)
		}
	}
	return names
}

func appendUnique(names []string, name string) []string {
	if name == "" || slices.Contains(names, name) {
		return names
	}
	return append(names, name)
}

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

func (c *Client) countStrandableByRIDs(
	ctx context.Context,
	names []string,
	rids []string,
) (int, error) {
	stranded := 0
	for _, name := range names {
		total, err := c.Query(ctx,
			"SELECT bothE().size() AS degree FROM Entity WHERE name = :name",
			map[string]any{"name": name})
		if err != nil {
			return 0, fmt.Errorf("arcadedb: read degree of %q: %w", name, err)
		}
		going, err := c.Query(ctx,
			"SELECT count(*) AS n FROM "+factEdgeType+
				" WHERE @rid IN :rids AND (outV().name = :name OR inV().name = :name)",
			map[string]any{"name": name, "rids": rids})
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
