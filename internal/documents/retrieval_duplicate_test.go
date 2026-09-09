package documents

import (
	"testing"
	"time"

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

// Copies that are NOT byte-identical must not collapse -- and then the reader needs some way
// to tell them apart. Measured 2026-09-09 on the live corpus: two
// meteo_caraglio_settimanale_verificato.docx, 9028 and 7712 bytes, different raw_sha256,
// disagreeing on the forecast they contain, arriving with the SAME title and scores 0.014
// apart. Retrieval ranks topical similarity and cannot know which one is true; what it must
// not do is hide the object facts that let the caller decide, which the reconciler already
// records and the response used to drop on the floor.
func TestRankDocumentsCarriesTheObjectFactsThatSeparateSameNamedCopies(t *testing.T) {
	indexed := time.Date(2026, 9, 9, 17, 41, 12, 0, time.UTC)
	older := indexed.Add(-4 * time.Second)
	cards := []RetrievalCard{
		{
			DocumentID: "doc_big", Title: "meteo_caraglio_settimanale_verificato.docx",
			SourceKind: "s3", SourceKey: "Documenti/meteo_a.docx", OriginalSHA256: "b174d89c",
			Rank: 0.816, SizeBytes: 9028, PassageCount: 1, IndexedAt: indexed,
		},
		{
			DocumentID: "doc_small", Title: "meteo_caraglio_settimanale_verificato.docx",
			SourceKind: "s3", SourceKey: "Documenti/meteo_b.docx", OriginalSHA256: "f4f2913a",
			Rank: 0.814, SizeBytes: 7712, PassageCount: 1, IndexedAt: older,
		},
	}

	documents := rankDocuments(cards, nil, nil, 8, 3, false)

	if len(documents) != 2 {
		t.Fatalf("documents = %d, want both copies", len(documents))
	}
	for _, doc := range documents {
		if doc.SizeBytes == nil || doc.PassageCount == nil || doc.IndexedAt == nil {
			t.Fatalf("%s reached the caller without its object facts: %+v", doc.DocumentID, doc)
		}
	}
	if *documents[0].SizeBytes == *documents[1].SizeBytes ||
		documents[0].IndexedAt.Equal(*documents[1].IndexedAt) {
		t.Fatal("two same-named copies are indistinguishable in the response")
	}
}

// A document the card leg did not rank has no record behind it, so its object facts are
// ABSENT rather than zero: reporting passage_count 0 for a document that plainly carries
// passages would be a stated wrong answer, which is worse than a missing one.
func TestRankDocumentsOmitsObjectFactsWithoutACard(t *testing.T) {
	score := 0.61
	passages := []arcadedb.PassageCandidate{{
		PassageID: "doc_only:3", SearchDocumentID: "doc_only", SourceKind: "s3",
		SourceKey: "Documenti/senza_card.md", RawSHA256: "dddd", NormalizedSHA256: "eeee",
		Text: "un passaggio senza card", Leg: arcadedb.RetrievalLegFused, FusedScore: &score,
	}}

	documents := rankDocuments(nil, passages, nil, 8, 3, false)

	if len(documents) != 1 {
		t.Fatalf("documents = %d, want one", len(documents))
	}
	if documents[0].SizeBytes != nil || documents[0].PassageCount != nil ||
		documents[0].IndexedAt != nil {
		t.Fatalf("object facts invented for a document with no card: %+v", documents[0])
	}
}

// Same text, different bytes. Measured 2026-09-09 on the live corpus: three
// artifact-workspace-check.html of 6092, 6020 and 6037 bytes carried ONE identical
// normalized_text_sha256 and came back at the same score, 0.59846956, spending three of the
// caller's result slots on one text. No two documents in that corpus shared a raw_sha256 at
// all, so the fusion's own groupBy had nothing to collapse.
func TestRankDocumentsCollapsesCopiesWithIdenticalText(t *testing.T) {
	const sameText = "6667dde664ad5c7d9f0f4a2b1e8c3d5a7b9e0f1c2d3e4f50617283940a5b6c7d"
	score := 0.59846956
	passages := make([]arcadedb.PassageCandidate, 0, 3)
	for index, raw := range []string{"9187dfa3caa1", "f2c8d876af35", "b7b2ada4bf38"} {
		passages = append(passages, arcadedb.PassageCandidate{
			PassageID: raw + ":0", SearchDocumentID: "doc_" + raw, SourceKind: "s3",
			SourceKey: "Documenti/artifact-workspace-check.html", RawSHA256: raw,
			NormalizedSHA256: sameText, Ordinal: int64(index),
			Text: "identical extracted text", Leg: arcadedb.RetrievalLegFused, FusedScore: &score,
		})
	}
	// The card leg carries no normalized hash, so a card for a collapsed copy must resolve
	// through the same alias or it opens a second, passage-less entry beside the first.
	cards := []RetrievalCard{{
		DocumentID: "doc_f2c8d876af35", Title: "artifact-workspace-check.html",
		SourceKind: "s3", SourceKey: "Documenti/artifact-workspace-check.html",
		OriginalSHA256: "f2c8d876af35", Rank: 0.59846956,
	}}

	documents := rankDocuments(cards, passages, nil, 8, 3, false)

	if len(documents) != 1 {
		keys := make([]string, 0, len(documents))
		for _, doc := range documents {
			keys = append(keys, doc.DocumentID)
		}
		t.Fatalf("one text was returned as %d documents: %v", len(documents), keys)
	}
	if documents[0].DocumentID != "doc_9187dfa3caa1" {
		t.Fatalf("representative = %q, want the best-ranked copy", documents[0].DocumentID)
	}
	if documents[0].Title != "artifact-workspace-check.html" {
		t.Fatalf("title = %q, want the card's name folded onto the survivor", documents[0].Title)
	}
}

// Different text must never collapse, whatever the bytes do.
func TestRankDocumentsKeepsDocumentsWithDifferentText(t *testing.T) {
	score := 0.5
	passages := []arcadedb.PassageCandidate{
		{
			PassageID: "a:0", SearchDocumentID: "doc_a", SourceKind: "s3", SourceKey: "a.md",
			RawSHA256: "aaaa", NormalizedSHA256: "1111", Text: "primo",
			Leg: arcadedb.RetrievalLegFused, FusedScore: &score,
		},
		{
			PassageID: "b:0", SearchDocumentID: "doc_b", SourceKind: "s3", SourceKey: "b.md",
			RawSHA256: "bbbb", NormalizedSHA256: "2222", Text: "secondo",
			Leg: arcadedb.RetrievalLegFused, FusedScore: &score,
		},
	}
	if documents := rankDocuments(nil, passages, nil, 8, 3, false); len(documents) != 2 {
		t.Fatalf("documents = %d, want both", len(documents))
	}
}
