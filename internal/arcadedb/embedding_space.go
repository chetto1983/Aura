package arcadedb

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
