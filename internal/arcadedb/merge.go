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
// A merge rewrites each fact onto the survivor and deletes the original. Getting
// there took two dead ends, and both are recorded because each looked correct.
//
// It used to copy in Cypher. One statement per direction did create-then-delete with
// `SET n = properties(f)`, on the stated premise that "neither language can change
// an edge's endpoints". Both halves of that were wrong, and the first half was
// fatal: `properties(f)` carries `sources`, which EnsureMemorySchema declares
// `LIST OF MAP`, and ArcadeDB's openCypher refuses a map-valued property
// assignment. Measured 2026-09-03 on 26.9.1:
//
//	http 400: TypeError: InvalidPropertyType - Property values can not contain map values
//
// The manual states the rule outright (arcadedb-docs, reference/cypher/cypher-clauses.adoc):
// "A property value must be a scalar or a list of scalars. A map value ... is rejected with
// a TypeError and no part of the clause is written." One page would have prevented this.
//
// The refusal is about ASSIGNING a map, not about touching an edge that holds one:
// probed on the same server, `SET f.statement = replace(...)` on an edge carrying
// `sources` succeeds and leaves `sources` untouched. Only the copy was impossible.
// Every fact written through the MCP surface carries provenance, and an entity with
// no facts has nothing to merge, so this failed for every merge that could matter.
// It shipped because MergeEntities had unit tests only, and those assert the
// statement this package emits against a fake server that answers `{"moved":1}` to
// anything -- see merge_live_integration_test.go, which now runs it for real.
//
// The second half was wrong too, but re-pointing is not the way out. SQL DOES
// change an edge's endpoint in place -- `UPDATE FACT SET @out = (SELECT FROM Entity
// WHERE name = :target)[0]` moves it and both vertices' adjacency follows, probed on
// 26.9.1 -- and it would have been strictly better, copying nothing. It only works
// on an UNINDEXED edge type. Probed on the same server, with everything else equal,
// a single index on FACT is enough to make the same statement fail:
//
//	bare FACT                     -> {"count":1}
//	+ FULL_TEXT index (statement) -> IllegalStateException: Cannot read original
//	+ UNIQUE index (fact_key)     -> buffer for record #5:1
//
// The engine cannot reindex an edge whose endpoint moved, and FACT carries three
// indexes. So the merge copies after all, and the copy is SQL, which stores a
// `LIST OF MAP` without complaint -- it is the language the fact was written in.
//
// Each fact moves as ONE sqlscript: create the replacement, delete the original,
// BEGIN/COMMIT around both. Create-then-delete over two round trips would leave a
// duplicated fact behind whenever the second one did not happen.
//
// Two properties are deliberately NOT carried over. `fact_key` is left null and
// rebuilt by reindexFacts once the originals are gone, so the unique key never
// collides mid-merge -- and reindexFacts additionally folds a group that the merge
// has just made identical, which is exactly what merging two names can produce.
// `embedding` is left null because the statement is REWRITTEN: the old vector
// describes the old wording, and carrying it would leave every merged fact answering
// dense retrieval under a name the memory no longer knows. The memory_embed_backfill
// sweep selects on `embedding IS NULL` and recomputes it within minutes, which is the
// mechanism this codebase already built for exactly this gap.

// mergeScanOutgoing reads the facts the source asserts, with everything needed to
// rewrite them onto the survivor.
//
// The WHERE excludes facts pointing at the target itself: after the merge those
// would be an entity asserting something about itself, which is what the duplicate
// WAS, not a fact worth keeping. They go with the source vertex.
const mergeScanOutgoing = mergeScanProjection + " inV().name AS other FROM " + factEdgeType +
	" WHERE outV().name = :source AND inV().name <> :target"

// mergeScanIncoming reads the facts asserted ABOUT the source.
const mergeScanIncoming = mergeScanProjection + " outV().name AS other FROM " + factEdgeType +
	" WHERE inV().name = :source AND outV().name <> :target"

// mergeScanProjection is every property the replacement edge has to carry, plus the
// @rid that identifies the original to delete. It omits `embedding` and `fact_key`
// on purpose; see the note above.
const mergeScanProjection = "SELECT @rid AS rid, statement, predicate, valid_from, " +
	"valid_to, created_at, expired_at, sources,"

// mergeMoveScript moves one fact. It reuses createFactStatement rather than restating
// its SET clause, so a property added to a fact is carried by the merge without
// anyone remembering to add it here twice.
const mergeMoveScript = createFactStatement +
	"; DELETE FROM " + factEdgeType + " WHERE @rid = :rid;"

// MergeResult reports what the merge moved.
type MergeResult struct {
	Moved   int    `json:"moved" jsonschema:"facts re-pointed onto the surviving entity"`
	Dropped int    `json:"dropped" jsonschema:"facts discarded because they only linked the two names together"`
	Target  string `json:"target"`
}

func normalizeMergeEntities(source, target string) (string, string, error) {
	source = strings.TrimSpace(source)
	target = strings.TrimSpace(target)
	switch {
	case source == "":
		return "", "", fmt.Errorf("arcadedb: merge source must be non-empty")
	case target == "":
		return "", "", fmt.Errorf("arcadedb: merge target must be non-empty")
	case source == target:
		return "", "", fmt.Errorf("arcadedb: cannot merge %q into itself", source)
	}
	return source, target, nil
}

// MergeEntities folds source into target and removes source. When target does
// not exist yet this is a rename, which is the same operation with one name
// that happens to be unused.
func (c *Client) MergeEntities(ctx context.Context, source, target string) (MergeResult, error) {
	var err error
	source, target, err = normalizeMergeEntities(source, target)
	if err != nil {
		return MergeResult{}, err
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
	for _, scan := range []struct {
		statement       string
		sourceIsSubject bool
	}{
		{mergeScanOutgoing, true},
		{mergeScanIncoming, false},
	} {
		rows, err := c.Query(ctx, scan.statement, params)
		if err != nil {
			return MergeResult{}, fmt.Errorf("arcadedb: merge %q into %q: %w", source, target, err)
		}
		for _, row := range rows {
			move := mergeMoveParams(row, source, target, scan.sourceIsSubject)
			if _, err := c.Script(ctx, mergeMoveScript, move); err != nil {
				return MergeResult{}, fmt.Errorf("arcadedb: merge %q into %q: %w", source, target, err)
			}
			moved++
		}
	}

	// DETACH so the facts that only joined the two names go with it; they are
	// the assertion that these were the same thing, which the merge just acted on.
	if _, err = c.Write(ctx,
		"MATCH (s:Entity {name: $source}) DETACH DELETE s", params); err != nil {
		return MergeResult{}, fmt.Errorf("arcadedb: remove merged entity %q: %w", source, err)
	}
	state := memorySchemaState{properties: map[string]bool{"sources": true}}
	if err := c.reindexFacts(ctx, state); err != nil {
		return MergeResult{}, fmt.Errorf("arcadedb: rebuild fact identity after merge: %w", err)
	}
	return MergeResult{Moved: moved, Dropped: before - moved, Target: target}, nil
}

// mergeMoveParams turns one scanned fact into the replacement edge's bind
// parameters. sourceIsSubject says which endpoint the survivor takes over; the
// other endpoint is whatever the original pointed at, carried through as `other`.
//
// The statement is rewritten here, in Go. Not because the database cannot: `replace` is
// not a FUNCTION on this server (probed on 26.9.1: "Unknown function name 'replace'") but
// it IS a string method -- `<value>.replace(<to-find>, <to-replace>)`, arcadedb-docs,
// reference/sql/sql-methods.adoc -- so the earlier note here claiming SQL had no replace
// mistook the shape of the call for the absence of the capability. It stays in Go because
// the original statement is already in hand from the scan, so a server-side rewrite would
// buy nothing and be harder to test. Rewriting it at all was the un-obvious call -- never rewrite what a source said -- but leaving it
// makes the merge half-done: the fact would hang off "Marta Bellini" while still
// reading "M. Bellini specialises in…", so the full-text index keeps answering under
// the name that no longer exists, which is the exact problem the merge was called to
// fix. Provenance is carried untouched and still points at what was originally said.
func mergeMoveParams(row map[string]any, source, target string, sourceIsSubject bool) map[string]any {
	subject, object := target, rowString(row, "other")
	if !sourceIsSubject {
		subject, object = rowString(row, "other"), target
	}
	return map[string]any{
		"subject_name": subject,
		"object_name":  object,
		"statement":    strings.ReplaceAll(rowString(row, "statement"), source, target),
		"predicate":    rowString(row, "predicate"),
		"valid_from":   row["valid_from"],
		"valid_to":     row["valid_to"],
		"created_at":   row["created_at"],
		"expired_at":   row["expired_at"],
		"sources":      row["sources"],
		"fact_key":     nil,
		"rid":          rowString(row, "rid"),
	}
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
