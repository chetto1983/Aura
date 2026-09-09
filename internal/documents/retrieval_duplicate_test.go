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

// A card is a filename-and-description match, and the tool's own instructions say a filename
// match alone is not evidence. It earns its place when the passage legs found nothing -- that
// is what rankCardsOnly is for -- but once they have answered, a card must not add a document
// with no passage behind it. Measured 2026-09-09 through the MCP: asking how to back up an
// ArcadeDB database returned the manual at 0.5659 and the PRD at 0.3828, then a worker report
// about ask_user at score 0, carried in by the card leg alone.
func TestCardsDoNotAddPassagelessDocumentsToAnEvidencedAnswer(t *testing.T) {
	score := 0.57
	passages := []arcadedb.PassageCandidate{{
		PassageID: "doc_manual:233", SearchDocumentID: "doc_manual", SourceKind: "s3",
		SourceKey: "Documenti/ArcadeDB-Manual.pdf", RawSHA256: "42d8390351fb",
		NormalizedSHA256: "37e2bcd6", Text: "take a regular full ArcadeDB backup",
		Ordinal: 233, Leg: arcadedb.RetrievalLegFused, FusedScore: &score,
	}}
	cards := []RetrievalCard{
		{DocumentID: "doc_manual", Title: "ArcadeDB-Manual.pdf", OriginalSHA256: "42d8390351fb", Rank: 10.9},
		{DocumentID: "doc_worker", Title: "w1-f843d485.md", OriginalSHA256: "d560d529ba54", Rank: 8.3},
	}

	documents := rankDocuments(cards, passages, nil, 8, 3, false)

	if len(documents) != 1 || documents[0].DocumentID != "doc_manual" {
		ids := make([]string, 0, len(documents))
		for _, d := range documents {
			ids = append(ids, d.DocumentID)
		}
		t.Fatalf("evidenced answer carried card-only documents: %v", ids)
	}
	if documents[0].Title != "ArcadeDB-Manual.pdf" {
		t.Fatalf("the card must still name the document it ranked, got %q", documents[0].Title)
	}
}
