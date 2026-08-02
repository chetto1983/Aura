package documents

import (
	"sort"
	"time"
)

// A digest answers one question — WHICH file is this — and deliberately not
// "what does it say". That second question used to be the index's job, and it
// cost 118 chunks and 118 vectors for one spreadsheet while still scoring 0% on
// every aggregate ("how many customers in Torino"), because the answer is a
// property of the whole set and lives in no passage. Since document_open now
// hands the agent the real file, the index only has to route.
//
// Nothing computes a digest at upload time any more: ingestion does not read the
// file. A digest is written by document_describe, after the agent has actually
// opened the document — her own note about a file she has read, in the same
// spirit as a memory fact.

// DigestHit is one document matched by digest search, with the id document_open
// takes. It carries no passage: the caller opens the file.
//
// Tags travel with it because they are the one part of a document's description
// a PERSON wrote. The cockpit makes them editable in the details drawer and
// shows them on every file row, so an operator who tagged three spreadsheets
// "fatturato 2026" has already told Aura how they think of those files — a
// signal no extractor produces. They are weighted B in the ranking vector,
// between the title and the generated digest.
type DigestHit struct {
	DocumentID string   `json:"document_id"`
	Title      string   `json:"title"`
	Tags       []string `json:"tags,omitempty"`
	Digest     string   `json:"digest"`
	Rank       float64  `json:"rank"`
	// UpdatedAt breaks a tie. It is not decoration: a BLANK query produces the
	// empty tsquery, so every row ranks 0.0 and the whole page is a tie. Ordering
	// those by title made "the file I just uploaded" return the alphabetically
	// first of the newest eight — the one question a library must get right.
	UpdatedAt time.Time `json:"updated_at"`
}

// SortDigestHits orders by rank descending, then NEWEST FIRST, then title. It
// mirrors the SQL's own `ORDER BY rank DESC, updated_at DESC` — this function
// exists only so a caller assembling hits from elsewhere gets the same order, and
// it must not contradict the statement it mirrors.
func SortDigestHits(hits []DigestHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Rank != hits[j].Rank {
			return hits[i].Rank > hits[j].Rank
		}
		if !hits[i].UpdatedAt.Equal(hits[j].UpdatedAt) {
			return hits[i].UpdatedAt.After(hits[j].UpdatedAt)
		}
		return hits[i].Title < hits[j].Title
	})
}
