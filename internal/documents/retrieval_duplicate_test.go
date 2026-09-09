package documents

import (
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// The same bytes reach the index under two source keys whenever an operator both uploads a
// file and attaches it to a chat: measured 2026-09-09 on the live corpus, ArcadeDB-Manual.pdf
// sits at Documenti/ArcadeDB-Manual.pdf and chat/6df85477-....pdf with one raw_sha256,
// 42d8390351fb, and both surfaced at ranks 1 and 2 of every manual search. Two S3 objects
// really do exist, so the ingest is right to keep a row for each -- CocoIndex reconciles
// passages and their document row together, and collapsing them there would break that.
// Retrieval is where an operator's `limit` must not be spent twice on one file.
func TestRankDocumentsCollapsesByteIdenticalCopies(t *testing.T) {
	const sharedHash = "42d8390351fbeeee7fde53490ae06eef87a127706e17d561ea079e93a0f2bbd7"
	score := 0.75
	passages := []arcadedb.PassageCandidate{{
		PassageID: "doc_first:189", SearchDocumentID: "doc_first", SourceKind: "s3",
		SourceKey: "Documenti/ArcadeDB-Manual.pdf", RawSHA256: sharedHash,
		NormalizedSHA256: "5bb7ae6a", Text: "BM25 maintains per-type corpus counters",
		Ordinal: 189, Leg: arcadedb.RetrievalLegFused, FusedScore: &score,
	}}
	// The twin arrives on the card leg, which the fusion's groupBy cannot reach.
	cards := []RetrievalCard{{
		DocumentID: "doc_twin", Title: "ArcadeDB-Manual.pdf", SourceKind: "s3",
		SourceKey:      "chat/6df85477-cb22-44d2-b4a1-948dc31cad9b.pdf",
		OriginalSHA256: sharedHash, Rank: 4.66,
	}}

	documents := rankDocuments(cards, passages, nil, 8, 3, false)

	if len(documents) != 1 {
		ids := make([]string, 0, len(documents))
		for _, d := range documents {
			ids = append(ids, d.DocumentID+"/"+d.SourceKey)
		}
		t.Fatalf("byte-identical copies were returned as %d documents: %v", len(documents), ids)
	}
	if documents[0].DocumentID != "doc_first" {
		t.Fatalf("the evidenced copy must represent the file, got %q", documents[0].DocumentID)
	}
	if len(documents[0].Passages) != 1 {
		t.Fatalf("passages = %d, want the one the fusion returned", len(documents[0].Passages))
	}
}

// The card is the only leg that knows the file's real name: a chat attachment's key is
// chat/<uuid>.pdf on purpose, so the passage leg's fallback can only produce the uuid.
// Collapsing copies must not cost the name -- measured 2026-09-09, the Caraglio results
// came back titled "04e93eb7-...-.docx" instead of meteo_caraglio_settimanale.docx.
func TestRankDocumentsPrefersTheCardTitleOverTheKeyFallback(t *testing.T) {
	const hash = "97f484db28d94281934adcf15afd2242ea5b48df57a10b4c181c553cfd560c60"
	score := 0.75
	passages := []arcadedb.PassageCandidate{{
		PassageID: "doc_meteo:0", SearchDocumentID: "doc_meteo", SourceKind: "s3",
		SourceKey: "chat/04e93eb7-981d-4f68-b58b-b90f5c5aaba6.docx", RawSHA256: hash,
		NormalizedSHA256: "6f0e3071", Text: "Previsioni Meteo Settimanali",
		Leg: arcadedb.RetrievalLegFused, FusedScore: &score,
	}}
	cards := []RetrievalCard{{
		DocumentID: "doc_meteo", Title: "meteo_caraglio_settimanale.docx", SourceKind: "s3",
		SourceKey:      "chat/04e93eb7-981d-4f68-b58b-b90f5c5aaba6.docx",
		OriginalSHA256: hash, Rank: 6.05,
	}}

	documents := rankDocuments(cards, passages, nil, 8, 3, false)

	if len(documents) != 1 || documents[0].Title != "meteo_caraglio_settimanale.docx" {
		t.Fatalf("title = %q, want the card's name", documents[0].Title)
	}
}

// Ordering by leg made a document without passages unreachable: rankDocuments gave a
// passage hit order=rank and a card hit order=len(passages)+rank, so no card could ever
// outrank any passage however well it matched. Measured 2026-09-09 on the live corpus,
// gi_comuni_cap.xlsx was absent from its own filename search. Both legs now score a
// reranked cosine, so the score decides.
func TestABetterCardOutranksAWeakerPassage(t *testing.T) {
	weak := 0.34
	passages := []arcadedb.PassageCandidate{{
		PassageID: "doc_prose:1", SearchDocumentID: "doc_prose", SourceKind: "s3",
		SourceKey: "chat/prosa.md", RawSHA256: "aaaa", NormalizedSHA256: "bbbb",
		Text: "una prosa vagamente attinente", Leg: arcadedb.RetrievalLegFused, FusedScore: &weak,
	}}
	cards := []RetrievalCard{{
		DocumentID: "doc_table", Title: "gi_comuni_cap.xlsx", SourceKind: "s3",
		SourceKey: "Documenti/gi_comuni_cap.xlsx", OriginalSHA256: "cccc", Rank: 0.71,
	}}

	documents := rankDocuments(cards, passages, nil, 8, 3, false)

	if len(documents) != 2 {
		t.Fatalf("documents = %d, want both", len(documents))
	}
	if documents[0].DocumentID != "doc_table" {
		t.Fatalf("order = %s then %s, want the better-scoring card first",
			documents[0].DocumentID, documents[1].DocumentID)
	}
}
