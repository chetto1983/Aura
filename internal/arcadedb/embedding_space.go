package arcadedb

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// The embedding space every memory vector is stamped with (spec §2).
//
// Vectors from two models do not share a space, and nothing errors when they mix:
// vector.neighbors still returns its k nearest and the answers quietly get worse. So every
// stored vector carries the space that produced it (embeddings.Space.ID), and dense
// retrieval runs only over a corpus entirely in the reader's space (spec §3).

// spaceStampStatements declare the stamp beside a type's vector.
//
// NULL_STRATEGY INDEX is load-bearing. ArcadeDB's default, SKIP, keeps nulls out of the
// index, and "queries against null values that use an index return no entries"
// (arcadedb-docs reference/sql/sql-indexes.adoc): every unstamped row -- after an upgrade,
// all of them -- would vanish from the gate's count and the pass's selection, and the gate
// would open over vectors it never checked.
func spaceStampStatements(typeName string) []string {
	return []string{
		"CREATE PROPERTY " + typeName + ".embed_space IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON " + typeName + " (embed_space) NOTUNIQUE NULL_STRATEGY INDEX",
	}
}

// storedVector is what embedding one text contributes to the row that stores it: a vector
// and the space that produced it; the space alone, when that space refused the text; or
// neither, when no route answered and the row is left for the pass (facts, traces) or the
// conversation reconciler (turns) to fill.
type storedVector struct {
	vector any // []float64 from the embedder, or a stored value carried over unchanged
	space  string
}

// createClause extends a CREATE with what v stores, and with nothing when it stores
// nothing: a new row without a vector is already unstamped.
//
// The vector used to have a clause of its own for the same reason, and leaving it off was a
// real defect: the vector was computed, bound as a parameter, and dropped because no SET
// named it. ArcadeDB accepts unused parameters silently, so every fact was stored without
// its vector and nothing said so.
func (v storedVector) createClause(params map[string]any) string {
	switch {
	case v.vector != nil:
		params["embedding"], params["embed_space"] = v.vector, nullableString(v.space)
		return ", embedding = :embedding, embed_space = :embed_space"
	case v.space != "":
		params["embed_space"] = v.space
		return ", embed_space = :embed_space"
	}
	return ""
}

// replaceClause sets both columns on an upsert or an update, to NULL where v has nothing:
// the row may still hold a vector computed for older content, or in another space, and
// leaving it would keep a vector that no longer describes the row.
func (v storedVector) replaceClause(params map[string]any) string {
	params["embedding"], params["embed_space"] = v.vector, nullableString(v.space)
	return ", embedding = :embedding, embed_space = :embed_space"
}

// memorySpaceType is one memory type whose vectors must share a space. live narrows the
// rows retrieval can reach: rows outside it neither close the gate nor cost the pass a
// request.
type memorySpaceType struct {
	name string
	live string
}

var (
	factSpace  = memorySpaceType{name: factEdgeType}
	turnSpace  = memorySpaceType{name: conversationTurnType, live: " AND deleted_at IS NULL"}
	traceSpace = memorySpaceType{name: reasoningTraceType}

	// memorySpaceTypes is the memory family (spec §3); the documents family is gated apart,
	// so a document that will not re-index never turns dense memory off.
	memorySpaceTypes = []memorySpaceType{factSpace, turnSpace, traceSpace}
)

// otherSpace matches a row whose stamp is not :space, a missing stamp included. ArcadeDB
// happened to answer `NULL <> 'x'` as true (lab VM, 26.9.1, 2026-09-23), but its docs do not
// define it, and the explicit form does not depend on it.
const otherSpace = "(embed_space IS NULL OR embed_space <> :space)"

// mismatchCount counts t's vectors in another space. Rows without a vector are not counted:
// they cannot be ranked, so they cannot be ranked wrongly.
func (t memorySpaceType) mismatchCount() string {
	return "SELECT count(*) AS n FROM " + t.name + " WHERE embedding IS NOT NULL AND " + otherSpace + t.live
}

// spaceGateTTL bounds how stale one tenant's gate answer can be: a pass that finishes
// opens the gate, and a stale writer closes it, within this.
const spaceGateTTL = 30 * time.Second

// spaceGate caches one tenant's answer, keyed by the space it was asked for.
type spaceGate struct {
	mu      sync.Mutex
	space   string
	open    bool
	checked time.Time
}

// memoryDenseOpen reports whether every memory vector this tenant holds is in space. Dense
// retrieval ranks a query against the whole corpus, so one vector from another model makes
// every distance suspect: until the pass has moved all of them, memory is served lexically
// (spec §3, operator decision 2).
func (c *Client) memoryDenseOpen(ctx context.Context, space string) (bool, error) {
	gate := &c.memoryGate
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.space == space && time.Since(gate.checked) < spaceGateTTL {
		return gate.open, nil
	}
	open := true
	for _, t := range memorySpaceTypes {
		rows, err := c.Query(ctx, t.mismatchCount(), map[string]any{"space": space})
		if err != nil {
			return false, fmt.Errorf("arcadedb: count %s vectors outside the space: %w", t.name, err)
		}
		if len(rows) > 0 && rowInt(rows[0], "n") > 0 {
			open = false
			break
		}
	}
	gate.space, gate.open, gate.checked = space, open, time.Now()
	return open, nil
}

// denseQueryVector is the dense leg's entry for every memory read: the query's vector, or
// nil and the reason the read must be lexical. The gate is asked before the query is
// embedded, so a closed gate costs no embedding request.
func (c *Client) denseQueryVector(ctx context.Context, query string) ([]float64, string) {
	if c == nil || c.embedder == nil {
		return nil, reasonEmbedderNotConfigured
	}
	space, err := c.embedder.Space(ctx)
	if err != nil {
		return nil, reasonEmbeddingFailed
	}
	open, err := c.memoryDenseOpen(ctx, space.ID)
	if err != nil {
		return nil, reasonSpaceCheckFailed
	}
	if !open {
		return nil, reasonEmbeddingSpaceMismatch
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskQueryPrefix, []string{query}))
	if err != nil {
		return nil, reasonEmbeddingFailed
	}
	if len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		return nil, reasonEmbeddingInvalid
	}
	return vectors[0], ""
}
