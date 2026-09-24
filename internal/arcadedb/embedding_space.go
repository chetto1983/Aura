package arcadedb

import (
	"context"
	"fmt"
	"strings"
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
	// source is the text the type's vector embeds.
	source string
	// fillsUnanswered: rows with neither vector nor stamp are the pass's to embed. Turns are
	// not: the conversation reconciler fills them on its next replay (memory_conversation.go).
	fillsUnanswered bool
}

var (
	factSpace  = memorySpaceType{name: factEdgeType, source: "statement", fillsUnanswered: true}
	turnSpace  = memorySpaceType{name: conversationTurnType, source: "content", live: " AND deleted_at IS NULL"}
	traceSpace = memorySpaceType{name: reasoningTraceType, source: "provider_summary", live: activeReasoningTraceFilter,
		fillsUnanswered: true}

	// memorySpaceTypes is the memory family (spec §3); the documents family is gated apart,
	// so a document that will not re-index never turns dense memory off.
	memorySpaceTypes = []memorySpaceType{factSpace, turnSpace, traceSpace}
)

// otherSpace matches a row whose stamp is not :space, a missing stamp included. ArcadeDB
// happened to answer `NULL <> 'x'` as true (lab VM, 26.9.1, 2026-09-23), but its docs do not
// define it, and the explicit form does not depend on it.
const otherSpace = "(embed_space IS NULL OR embed_space <> :space)"

// spaceCount is one type's share of a family's gate.
type spaceCount struct {
	typeName  string
	statement string
	// ingested: services/ingest declares the type on its first run (arcade.py), so a tenant
	// with no document yet has none -- an empty library, with no vector in any space.
	ingested bool
}

// vectorsOutside counts typeName's vectors in another space. Rows without a vector are not
// counted: they cannot be ranked, so they cannot be ranked wrongly.
func vectorsOutside(typeName, live string) string {
	return "SELECT count(*) AS n FROM " + typeName + " WHERE embedding IS NOT NULL AND " + otherSpace + live
}

func gateCountsOf(types []memorySpaceType) []spaceCount {
	counts := make([]spaceCount, 0, len(types))
	for _, t := range types {
		counts = append(counts, spaceCount{typeName: t.name, statement: vectorsOutside(t.name, t.live)})
	}
	return counts
}

var (
	// memoryGateCounts is built from memorySpaceTypes, the list the pass re-embeds, so a type
	// the pass moves always closes the gate too.
	memoryGateCounts = gateCountsOf(memorySpaceTypes)
	// documentGateCounts is the documents family (spec §3), gated apart from memory.
	documentGateCounts = []spaceCount{
		{typeName: documentPassageType, statement: vectorsOutside(documentPassageType, ""), ingested: true},
		{typeName: IndexedDocumentType, statement: vectorsOutside(IndexedDocumentType, ""), ingested: true},
	}
)

// spaceGateTTL bounds how stale one tenant's gate answer can be: a pass that finishes
// opens the gate, and a stale writer closes it, within this.
const spaceGateTTL = 30 * time.Second

// spaceGate caches one tenant's answer for one family, keyed by the space it was asked for.
type spaceGate struct {
	mu      sync.Mutex
	space   string
	open    bool
	checked time.Time
}

// denseOpen reports whether no vector counted by counts is outside space. Dense retrieval
// ranks a query against the whole corpus, so one vector from another model makes every
// distance suspect: until all of them are re-embedded, the family is served lexically (spec
// §3, operator decision 2).
//
// The lock is held across the counts on purpose: readers of one tenant arriving together
// share one check instead of sending the counts each. The cost is that a waiter cannot give
// up before the holder's counts return, whatever its own deadline. Each count scans its type
// (the `<>` half of otherSpace uses no index): 25-34 ms at 5,000 rows, measured 2026-09-24
// (spec, "What this design does not prove").
func (c *Client) denseOpen(ctx context.Context, gate *spaceGate, counts []spaceCount, space string) (bool, error) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.space == space && time.Since(gate.checked) < spaceGateTTL {
		return gate.open, nil
	}
	open := true
	params := map[string]any{"space": space, "now": time.Now().UTC().Format(time.RFC3339Nano)}
	for _, count := range counts {
		rows, err := c.Query(ctx, count.statement, params)
		if err != nil {
			if count.ingested && missingIngestType(err, count.typeName) {
				continue
			}
			return false, fmt.Errorf("arcadedb: count %s vectors outside the space: %w", count.typeName, err)
		}
		if len(rows) > 0 && rowInt(rows[0], "n") > 0 {
			open = false
			break
		}
	}
	gate.space, gate.open, gate.checked = space, open, time.Now()
	return open, nil
}

// memoryDenseOpen reports whether every memory vector this tenant holds is in space.
func (c *Client) memoryDenseOpen(ctx context.Context, space string) (bool, error) {
	return c.denseOpen(ctx, &c.memoryGate, memoryGateCounts, space)
}

// documentsDenseOpen reports whether every document vector this tenant holds is in space.
func (c *Client) documentsDenseOpen(ctx context.Context, space string) (bool, error) {
	return c.denseOpen(ctx, &c.documentGate, documentGateCounts, space)
}

// DocumentsDenseOpen reports whether every Passage and IndexedDocument vector identityID holds
// is in space, so the dense legs may rank a query embedded there (spec §3). A tenant with
// nothing ingested is open.
func (d *DocumentIndex) DocumentsDenseOpen(ctx context.Context, identityID, space string) (bool, error) {
	if strings.TrimSpace(space) == "" {
		return false, fmt.Errorf("arcadedb: the document gate needs the reader's space")
	}
	client, err := d.tenantClient(ctx, identityID)
	if err != nil {
		return false, err
	}
	return client.documentsDenseOpen(ctx, space)
}

// denseSpaceFilter keeps a dense leg to the reader's space. The gate decides whether a read
// is dense at all; this makes a ranking over two models' vectors impossible even while the
// gate's cached answer is older than a stale write.
const denseSpaceFilter = " AND embed_space = :space"

// denseQuery is a query embedded for the dense leg, and the space its vector is in.
type denseQuery struct {
	vector []float64
	space  string
}

func (q denseQuery) bind(params map[string]any) {
	params["vector"], params["space"] = q.vector, q.space
}

// denseQueryVector is the dense leg's entry for every memory read: the embedded query, or
// none and the reason the read must be lexical. The gate is asked before the query is
// embedded, so a closed gate costs no embedding request.
//
// The space is read again once the query is embedded. For a write, reading it first is the
// safe order (embedStored); for a read it is not: a model swapped in between would rank a
// new model's vector against a corpus checked in the old space.
func (c *Client) denseQueryVector(ctx context.Context, query string) (denseQuery, string) {
	if c == nil || c.embedder == nil {
		return denseQuery{}, reasonEmbedderNotConfigured
	}
	space, err := c.embedder.Space(ctx)
	if err != nil {
		return denseQuery{}, reasonEmbeddingFailed
	}
	open, err := c.memoryDenseOpen(ctx, space.ID)
	if err != nil {
		return denseQuery{}, reasonSpaceCheckFailed
	}
	if !open {
		return denseQuery{}, reasonEmbeddingSpaceMismatch
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskQueryPrefix, []string{query}))
	if err != nil {
		return denseQuery{}, reasonEmbeddingFailed
	}
	if len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		return denseQuery{}, reasonEmbeddingInvalid
	}
	after, err := c.embedder.Space(ctx)
	if err != nil {
		return denseQuery{}, reasonEmbeddingFailed
	}
	if after.ID != space.ID {
		return denseQuery{}, reasonEmbeddingSpaceMismatch
	}
	return denseQuery{vector: vectors[0], space: space.ID}, ""
}
