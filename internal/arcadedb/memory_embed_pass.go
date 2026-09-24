package arcadedb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The pass that keeps a tenant's memory in one embedding space (spec §5).
//
// It generalises the old fill of "facts without a vector". After a route change every row
// carries the old space, and "has no vector" selected none of them; "not stamped with the
// daemon's space" selects all of them, and shrinks as the work is done, which is what makes
// an interrupted run, or a second route change in mid-pass, need no protocol.

// passSelection is the rows of t the pass owns in :space, one cursor page at a time.
// ArcadeDB pages by RID natively: `@rid > :cursor` seeks, starting from #-1:-1
// (arcadedb-docs reference/sql/sql-pagination.adoc). A string parameter compares as a RID
// -- #44:9 is followed by #44:10, not #44:2048 (lab VM, 26.9.1, 2026-09-23). The cursor is
// what lets a run pass a row it could not fix instead of selecting it again every round.
//
// The page size binds as :page, not :batch. A named parameter is `COLON identifier`, and
// BATCH is a keyword the identifier rule does not admit, so `LIMIT :batch` is a syntax
// error (ArcadeDB SQLParser.g4 inputParameter/identifier; measured live 26.9.1).
func (t memorySpaceType) passSelection() string {
	answered := ""
	if !t.fillsUnanswered {
		answered = " AND (embedding IS NOT NULL OR embed_space IS NOT NULL)"
	}
	return "SELECT @rid AS rid, " + t.source + " AS text FROM " + t.name +
		" WHERE " + otherSpace + answered + t.live +
		" AND @rid > :cursor ORDER BY @rid LIMIT :page"
}

// selectedRow is a row as the pass selected it: its RID, and the source text it held then,
// nil for NULL.
type selectedRow struct {
	rid  string
	text any
}

// setVectorStatement writes one row's vector and stamp, bound to its own :vN, :sN, :rN and
// :tN; statements sharing one binding in a script would all take the last row's vector.
//
// The write holds only while the row still has the text the pass embedded. A writer can
// rewrite a turn or a trace between the pass's select and its write; its own vector then
// describes the new text, and the pass's would describe text the row no longer holds --
// stamped in the space, so nothing would ever look at it again.
func (t memorySpaceType) setVectorStatement(n string, nullText bool) string {
	unchanged := " AND " + t.source + " = :t" + n
	if nullText {
		unchanged = " AND " + t.source + " IS NULL"
	}
	return "UPDATE " + t.name + " SET embedding = :v" + n + ", embed_space = :s" + n +
		" WHERE @rid = :r" + n + unchanged
}

func bindStoredRow(params map[string]any, n string, row selectedRow, vector storedVector) {
	params["v"+n], params["s"+n], params["r"+n] = vector.vector, vector.space, row.rid
	if row.text != nil {
		params["t"+n] = row.text
	}
}

// passTally counts what a pass changed: vectors written, records the model refused, and
// rows the store would not take, the first of them named with its cause.
type passTally struct {
	embedded    int
	refused     int
	failed      int
	firstFailed string
	failCause   error
}

func (p *passTally) add(other passTally) {
	p.embedded += other.embedded
	p.refused += other.refused
	p.failed += other.failed
	if p.firstFailed == "" {
		p.firstFailed, p.failCause = other.firstFailed, other.failCause
	}
}

// writeError names the rows the store would not take, for a caller that reports what it did
// instead of logging it.
func (p passTally) writeError() error {
	if p.failed == 0 {
		return nil
	}
	return fmt.Errorf("arcadedb: %d row(s) could not be stored (first %s: %w)", p.failed, p.firstFailed, p.failCause)
}

// reembedMemory runs the pass over the whole memory family until nothing is left or ctx
// ends.
func (c *Client) reembedMemory(ctx context.Context) (passTally, error) {
	var tally passTally
	for _, t := range memorySpaceTypes {
		typed, err := c.reembedType(ctx, t, backfillBatch, 0)
		tally.add(typed)
		if err != nil {
			return tally, err
		}
	}
	return tally, nil
}

// reembedType pages t by RID and re-embeds each page in the space read for it. rounds
// bounds the pages; 0 means until nothing is left.
func (c *Client) reembedType(ctx context.Context, t memorySpaceType, batch, rounds int) (passTally, error) {
	var tally passTally
	cursor := "#-1:-1"
	for round := 0; rounds == 0 || round < rounds; round++ {
		space, err := c.embedder.Space(ctx)
		if err != nil {
			return tally, routeFailure(err)
		}
		rows, err := c.Query(ctx, t.passSelection(), map[string]any{
			"space": space.ID, "cursor": cursor, "page": batch, "now": time.Now().UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return tally, fmt.Errorf("arcadedb: select %s to re-embed: %w", t.name, err)
		}
		var embeddable, empty []selectedRow
		var texts []string
		for _, row := range rows {
			rid := rowString(row, "rid")
			if rid == "" {
				continue
			}
			cursor = rid
			selected := selectedRow{rid: rid, text: row["text"]}
			if text := rowString(row, "text"); strings.TrimSpace(text) != "" {
				embeddable, texts = append(embeddable, selected), append(texts, text)
			} else {
				empty = append(empty, selected)
			}
		}
		if len(texts) > 0 {
			vectors, err := c.embedStored(ctx, texts)
			if err != nil {
				return tally, err
			}
			tally.add(c.storeVectors(ctx, t, embeddable, vectors))
		}
		// A row with no text can never be embedded; left holding an old vector it would keep
		// the gate closed for good, and nothing would say why. It is set aside like a record
		// the model refused.
		if len(empty) > 0 {
			aside := make([]storedVector, len(empty))
			for i := range aside {
				aside[i] = storedVector{space: space.ID}
			}
			tally.add(c.storeVectors(ctx, t, empty, aside))
		}
		// A row the store will not take is behind the cursor and named in the tally, so one
		// poisoned row cannot end every run at the same place. A page's worth of them is a
		// store refusing writes, not a row: going on would embed, and on a cloud route pay
		// for, the whole type on every run.
		if len(rows) < batch || tally.failed >= batch {
			return tally, nil
		}
	}
	return tally, nil
}

// storeVectors writes a page in ONE round trip, and falls back to one statement per row
// only when that fails.
//
// The round trip is the entire cost. Measured on this host 2026-08-03: a vector UPDATE by
// @rid takes 55-78ms where `SELECT 1` takes 53-63ms, so a page of 32 written one at a time
// spent ~1.8s in handshakes to do ~0.3s of work. "A sqlscript can consist of one or
// multiple SQL statements, which is collectively treated as a transaction"
// (arcadedb-docs reference/http-api/http.adoc), which is also why the per-row fallback
// exists: one poisoned row would roll back its 31 healthy companions on every run.
//
// A vector is written with its space. A refused text is set aside: its vector removed, its
// row stamped with the space that refused it, so neither the pass nor the gate counts it
// until the space changes. A row with neither is left as it was. A row the store still
// refuses on its own is counted in the tally, not returned: it stays outside the space, and
// the next run tries it again. A row whose text changed meanwhile is left to its writer (see
// setVectorStatement) and still counted as written: a sqlscript reports its last statement
// only.
func (c *Client) storeVectors(
	ctx context.Context, t memorySpaceType, rows []selectedRow, vectors []storedVector,
) passTally {
	var tally passTally
	statements := make([]string, 0, len(rows))
	params := make(map[string]any, len(rows)*4)
	usable := make([]int, 0, len(rows))
	for i, row := range rows {
		if i >= len(vectors) || vectors[i].space == "" {
			continue
		}
		usable = append(usable, i)
		n := strconv.Itoa(len(usable) - 1)
		statements = append(statements, t.setVectorStatement(n, row.text == nil))
		bindStoredRow(params, n, row, vectors[i])
	}
	count := func(i int) {
		if vectors[i].vector != nil {
			tally.embedded++
		} else {
			tally.refused++
		}
	}
	if len(statements) == 0 {
		return tally
	}
	if _, err := c.Script(ctx, strings.Join(statements, ";\n"), params); err == nil {
		for _, i := range usable {
			count(i)
		}
		return tally
	}
	for _, i := range usable {
		single := make(map[string]any, 4)
		bindStoredRow(single, "0", rows[i], vectors[i])
		if _, err := c.Command(ctx, t.setVectorStatement("0", rows[i].text == nil), single); err != nil {
			tally.failed++
			if tally.firstFailed == "" {
				tally.firstFailed, tally.failCause = rows[i].rid, err
			}
			continue
		}
		count(i)
	}
	return tally
}

// EmbedMissingFacts is one bounded round of the pass over facts: the memory_reembed tool's
// repair, without `all`. It returns how many vectors it wrote.
func (c *Client) EmbedMissingFacts(ctx context.Context, batch int) (int, error) {
	if c == nil || c.embedder == nil {
		return 0, fmt.Errorf("arcadedb: no embedder configured")
	}
	tally, err := c.reembedType(ctx, factSpace, boundedLimit(batch, 100, c.memoryLimits().MaintenanceBatch), 1)
	if err == nil {
		err = tally.writeError()
	}
	return tally.embedded, err
}

// ReEmbedAllFacts recomputes EVERY fact's vector in the current space: a same-space repair
// (spec, Out of scope). A route change needs no call: the pass re-embeds on its own. The
// repair is not hypothetical: on 2026-08-02 the appliance was found running a GGUF missing
// EmbeddingGemma's two dense projections, and the space's name could not tell.
//
// It clears every vector and stamp, then drains. Selecting "facts with a vector" directly
// is what shipped first, and it could not finish: that set does not shrink as the work is
// done, so every call returned the same first `batch` rows (measured 2026-09-03 through the
// MCP surface: two consecutive `all` calls on a 55-fact memory both reported 30). Whatever
// this call's rounds do not reach, the scheduled pass picks up.
func (c *Client) ReEmbedAllFacts(ctx context.Context, batch int) (int, error) {
	if c == nil || c.embedder == nil {
		return 0, fmt.Errorf("arcadedb: no embedder configured")
	}
	if _, err := c.Command(ctx, clearFactEmbeddingsStatement, nil); err != nil {
		return 0, fmt.Errorf("arcadedb: clear fact vectors: %w", err)
	}
	tally, err := c.reembedType(ctx, factSpace, boundedLimit(batch, 100, c.memoryLimits().MaintenanceBatch),
		backfillRoundsPerTenant)
	if err == nil {
		err = tally.writeError()
	}
	return tally.embedded, err
}

// clearFactEmbeddingsStatement drops every fact's vector and stamp, together: a stamp
// without a vector means "this space refused the text".
const clearFactEmbeddingsStatement = "UPDATE " + factEdgeType +
	" SET embedding = NULL, embed_space = NULL WHERE embedding IS NOT NULL OR embed_space IS NOT NULL"
