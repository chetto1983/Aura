package arcadedb

import (
	"context"
	"fmt"
	"strings"
)

// Merging exists because the entities are named by a language model, so
// "Marta Bellini" and "M. Bellini" will eventually both appear, and neither
// will hold the whole story. memory_forget does not help: it destroys one of
// them along with the facts only it knows.
//
// This is Cypher rather than SQL. Neither language can change an edge's
// endpoints, so a merge is always create-then-delete -- but Cypher does it in
// one statement per direction with `SET n = properties(f)`, where SQL would
// need the properties read into the client and written back one edge at a time.
// Verified on 26.7.3: relationshipsCreated 1, relationshipsDeleted 1.

// mergeOutgoing re-points the facts the source asserts.
//
// The WHERE excludes facts pointing at the target itself: after the merge those
// would be an entity asserting something about itself, which is what the
// duplicate WAS, not a fact worth keeping.
// The statement is rewritten along with the endpoint. Leaving it alone was the
// tempting choice -- never rewrite what a source said -- but it makes the merge
// half-done: the fact would hang off "Marta Bellini" while still reading
// "M. Bellini specialises in…", so the full-text index keeps answering under the
// name that no longer exists, which is the exact problem the merge was called to
// fix. Provenance is not lost by this: source_run_id and source_memory_ids are
// copied untouched and still point at what was originally said.
const mergeOutgoing = `
MATCH (s:Entity {name: $source})-[f:FACT]->(o:Entity)
WHERE o.name <> $target
MATCH (t:Entity {name: $target})
CREATE (t)-[n:FACT]->(o)
SET n = properties(f), n.statement = replace(f.statement, $source, $target)
DELETE f
RETURN count(n) AS moved
`

// mergeIncoming re-points the facts asserted ABOUT the source.
const mergeIncoming = `
MATCH (s:Entity)-[f:FACT]->(o:Entity {name: $source})
WHERE s.name <> $target
MATCH (t:Entity {name: $target})
CREATE (s)-[n:FACT]->(t)
SET n = properties(f), n.statement = replace(f.statement, $source, $target)
DELETE f
RETURN count(n) AS moved
`

// MergeResult reports what the merge moved.
type MergeResult struct {
	Moved   int    `json:"moved" jsonschema:"facts re-pointed onto the surviving entity"`
	Dropped int    `json:"dropped" jsonschema:"facts discarded because they only linked the two names together"`
	Target  string `json:"target"`
}

// MergeEntities folds source into target and removes source. When target does
// not exist yet this is a rename, which is the same operation with one name
// that happens to be unused.
func (c *Client) MergeEntities(ctx context.Context, source, target string) (MergeResult, error) {
	source = strings.TrimSpace(source)
	target = strings.TrimSpace(target)
	switch {
	case source == "":
		return MergeResult{}, fmt.Errorf("arcadedb: merge source must be non-empty")
	case target == "":
		return MergeResult{}, fmt.Errorf("arcadedb: merge target must be non-empty")
	case source == target:
		return MergeResult{}, fmt.Errorf("arcadedb: cannot merge %q into itself", source)
	}

	before, err := c.entityFactCount(ctx, source)
	if err != nil {
		return MergeResult{}, err
	}
	if before == 0 {
		if _, err = c.Query(ctx,
			"SELECT name FROM Entity WHERE name = :name LIMIT 1",
			map[string]any{"name": source}); err != nil {
			return MergeResult{}, fmt.Errorf("arcadedb: read %q: %w", source, err)
		}
	}
	if _, err = c.Command(ctx, upsertEntityStatement,
		map[string]any{"name": target}); err != nil {
		return MergeResult{}, fmt.Errorf("arcadedb: upsert merge target %q: %w", target, err)
	}

	params := map[string]any{"source": source, "target": target}
	moved := 0
	for _, statement := range []string{mergeOutgoing, mergeIncoming} {
		rows, err := c.Write(ctx, statement, params)
		if err != nil {
			return MergeResult{}, fmt.Errorf("arcadedb: merge %q into %q: %w", source, target, err)
		}
		for _, row := range rows {
			moved += int(rowInt(row, "moved"))
		}
	}

	// DETACH so the facts that only joined the two names go with it; they are
	// the assertion that these were the same thing, which the merge just acted on.
	if _, err = c.Write(ctx,
		"MATCH (s:Entity {name: $source}) DETACH DELETE s", params); err != nil {
		return MergeResult{}, fmt.Errorf("arcadedb: remove merged entity %q: %w", source, err)
	}
	return MergeResult{Moved: moved, Dropped: before - moved, Target: target}, nil
}

func (c *Client) entityFactCount(ctx context.Context, name string) (int, error) {
	rows, err := c.Query(ctx,
		"SELECT count(*) AS n FROM "+factEdgeType+
			" WHERE outV().name = :name OR inV().name = :name",
		map[string]any{"name": name})
	if err != nil {
		return 0, fmt.Errorf("arcadedb: count facts of %q: %w", name, err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return int(rowInt(rows[0], "n")), nil
}
