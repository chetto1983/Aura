package documents

import (
	"math"
	"path"
	"sort"

	"github.com/chetto1983/aura/internal/arcadedb"
)

type rankedDocument struct {
	document RetrievalDocument
	passages map[string]*RetrievalPassage
	// order is the best position this document reached in the ranking the ENGINE
	// returned. Nothing here computes a score: ArcadeDB fused both indexes and ordered
	// them, and any order re-derived in Go could only disagree with it.
	order   int
	ordinal int64
}

// rankCardsOnly is the degraded answer: cards with no passage leg behind them. Names are
// not looked up because a card already carries the one it was ranked by.
func rankCardsOnly(cards []RetrievalCard, limit, topPassages int) []RetrievalDocument {
	return rankDocuments(cards, nil, nil, limit, topPassages, true)
}

func rankDocuments(
	cards []RetrievalCard,
	passages []arcadedb.PassageCandidate,
	names map[string]string,
	limit int,
	topPassages int,
	forceOpen bool,
) []RetrievalDocument {
	// Keyed by CONTENT, not by document id: the fusion already groups its own candidates by
	// raw_sha256, but the card leg is a separate Postgres query that cannot, so without this
	// the twin of a deduplicated file walks back in as a second document. The first copy seen
	// represents the file, and passages come first, so that is the best-evidenced one.
	byContent := make(map[string]*rankedDocument, len(cards)+len(passages))
	// Different bytes, same text. The fusion groups by raw_sha256, so it collapses a file
	// stored twice byte for byte and nothing else -- and measured 2026-09-09 on the live
	// corpus, no two documents shared a raw_sha256 at all while THREE
	// artifact-workspace-check.html of 6092, 6020 and 6037 bytes carried one identical
	// normalized_text_sha256 and came back at the same score, 0.59846956, spending three
	// result slots on one text.
	//
	// The engine returns each file's BEST passage (groupSize 1), so two files whose best
	// passage is textually identical offer identical evidence for this query and one of
	// them can represent both. What that trades: a file whose only overlap with another is
	// the passage that happened to match is hidden behind it, and is then reachable by a
	// query that matches its own content instead.
	sameText := textAliases(cards, passages)
	// Passages first and their order wins: a document the engine ranked is better
	// evidenced than one only a card mentions, so cards start after the last passage.
	for rank, passage := range passages {
		doc := ensureRankedDocumentFromCandidate(byContent, sameText, passage, names)
		doc.order = min(doc.order, rank)
		doc.ordinal = min(doc.ordinal, passage.Ordinal)
		if passage.FusedScore != nil && *passage.FusedScore > doc.document.Score {
			doc.document.Score = *passage.FusedScore
		}
		mergePassage(doc, passage, rank+1)
	}
	for rank, card := range cards {
		doc := ensureRankedDocumentFromCard(byContent, sameText, card)
		// Not len(passages)+rank any more. That offset was a precedence rule standing in for
		// a comparison the two legs could not make: BM25 here, a reranked cosine there. It
		// meant no card could outrank any passage however well it matched, and measured
		// 2026-09-09 that made gi_comuni_cap.xlsx — whose card names all seventeen of its
		// columns — absent from a search for its own filename, because a spreadsheet has no
		// passages. Both legs now score the same reranked cosine, so the score decides and
		// this is only the tie-break.
		doc.order = min(doc.order, rank)
		if card.Rank > doc.document.Score {
			doc.document.Score = card.Rank
		}
		doc.document.Evidence = appendEvidence(doc.document.Evidence, RetrievalEvidence{
			Leg: "card", Rank: rank + 1, Score: new(card.Rank),
		})
	}

	ranked := make([]*rankedDocument, 0, len(byContent))
	for _, doc := range byContent {
		doc.document.RequiresOpen = forceOpen || len(doc.passages) == 0
		doc.document.Passages = sortedPassages(doc.passages, topPassages)
		ranked = append(ranked, doc)
	}
	sort.Slice(ranked, func(i, j int) bool {
		// The score first, because both legs finally speak it. order stays as the tie-break:
		// the engine already put each leg in its own order and a tie re-derived in Go could
		// only disagree with it.
		if ranked[i].document.Score != ranked[j].document.Score {
			return ranked[i].document.Score > ranked[j].document.Score
		}
		if ranked[i].order != ranked[j].order {
			return ranked[i].order < ranked[j].order
		}
		if ranked[i].document.DocumentID != ranked[j].document.DocumentID {
			return ranked[i].document.DocumentID < ranked[j].document.DocumentID
		}
		return ranked[i].ordinal < ranked[j].ordinal
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]RetrievalDocument, len(ranked))
	for index := range ranked {
		out[index] = ranked[index].document
	}
	return out
}

// contentKey collapses byte-identical copies onto one entry. It falls back to the document
// id when the hash is absent: keying every hashless row under "" would merge unrelated
// documents into one, which is a far worse answer than the duplicate it would prevent.
func contentKey(rawSHA256, documentID string) string {
	if rawSHA256 == "" {
		return documentID
	}
	return rawSHA256
}

// textAliases maps every content hash onto the one that represents its text. Both legs feed
// it: a copy whose passages did not rank is still named by its card, and the case that was
// actually measured -- three artifact-workspace-check.html at one score -- reached the
// caller through the card leg alone, with no passage to alias by.
//
// Passages are walked first and in the engine's own order, so the best-evidenced copy is the
// representative, which is the rule the byte-identical collapse already used.
//
// A passage hash covers one chunk and a card hash the whole extracted text. They share one
// map because identical text hashes identically whichever produced it, and a document whose
// single chunk IS its whole text is the same document either way.
func textAliases(cards []RetrievalCard, passages []arcadedb.PassageCandidate) map[string]string {
	alias := make(map[string]string, len(cards)+len(passages))
	representative := make(map[string]string, len(cards)+len(passages))
	link := func(normalized, raw string) {
		if normalized == "" || raw == "" {
			return
		}
		first, seen := representative[normalized]
		if !seen {
			representative[normalized] = raw
			return
		}
		if first != raw {
			alias[raw] = first
		}
	}
	for _, passage := range passages {
		link(passage.NormalizedSHA256, passage.RawSHA256)
	}
	for _, card := range cards {
		link(card.NormalizedSHA256, card.OriginalSHA256)
	}
	return alias
}

// resolveAlias is applied to BOTH legs or neither: the card leg carries no normalized hash
// of its own, so a card for a collapsed copy would open a second entry -- titled, scored and
// passage-less -- beside the document its passages had already been folded into.
func resolveAlias(sameText map[string]string, rawSHA256 string) string {
	if canonical, ok := sameText[rawSHA256]; ok {
		return canonical
	}
	return rawSHA256
}

func newRankedDocument(documentID string) *rankedDocument {
	return &rankedDocument{
		document: RetrievalDocument{
			DocumentID: documentID,
			Evidence:   []RetrievalEvidence{}, Passages: []RetrievalPassage{},
		},
		passages: make(map[string]*RetrievalPassage),
		order:    math.MaxInt, ordinal: math.MaxInt64,
	}
}

// ensureRankedDocumentFromCard adds the document's searchable description and object identity.
func ensureRankedDocumentFromCard(
	byContent map[string]*rankedDocument, sameText map[string]string, card RetrievalCard,
) *rankedDocument {
	key := contentKey(resolveAlias(sameText, card.OriginalSHA256), card.DocumentID)
	doc := byContent[key]
	if doc == nil {
		doc = newRankedDocument(card.DocumentID)
		byContent[key] = doc
	}
	// The card wins the title unconditionally: it is the only leg that carries the file's
	// real name, and the passage leg's fallback is a key's base name -- a uuid for every
	// chat attachment.
	doc.document.Title = card.Title
	doc.document.Card = card.Card
	if doc.document.SourceKey == "" {
		doc.document.SourceKind, doc.document.SourceKey = card.SourceKind, card.SourceKey
	}
	if doc.document.OriginalSHA256 == "" {
		doc.document.OriginalSHA256 = card.OriginalSHA256
	}
	doc.document.SizeBytes = &card.SizeBytes
	doc.document.PassageCount = &card.PassageCount
	if !card.IndexedAt.IsZero() {
		doc.document.IndexedAt = &card.IndexedAt
	}
	return doc
}

// ensureRankedDocumentFromCandidate builds the document out of the passage itself, so a
// document reconciled from the bucket -- which has no card at all -- still reaches the
// caller. SourceKey is the route back to the bytes.
//
// The title comes from names, which the caller resolved for exactly these documents. The
// key's tail is the LAST resort and it is only ever right by luck: it is the real name for
// an object dropped straight into the bucket, and a uuid for a chat attachment, whose name
// is deliberately kept out of its key so it cannot leak through a presigned URL.
func ensureRankedDocumentFromCandidate(
	byContent map[string]*rankedDocument,
	sameText map[string]string,
	candidate arcadedb.PassageCandidate,
	names map[string]string,
) *rankedDocument {
	key := contentKey(resolveAlias(sameText, candidate.RawSHA256), candidate.SearchDocumentID)
	doc := byContent[key]
	if doc == nil {
		doc = newRankedDocument(candidate.SearchDocumentID)
		byContent[key] = doc
	}
	if doc.document.SourceKey == "" {
		doc.document.SourceKind, doc.document.SourceKey = candidate.SourceKind, candidate.SourceKey
	}
	if doc.document.Title == "" {
		if name := names[candidate.SearchDocumentID]; name != "" {
			doc.document.Title = name
		} else {
			doc.document.Title = path.Base(candidate.SourceKey)
		}
	}
	if doc.document.OriginalSHA256 == "" {
		doc.document.OriginalSHA256 = candidate.RawSHA256
	}
	return doc
}

func mergePassage(doc *rankedDocument, candidate arcadedb.PassageCandidate, rank int) {
	passage := doc.passages[candidate.PassageID]
	if passage == nil {
		locator := citationLocator(candidate)
		passage = &RetrievalPassage{
			PassageID: candidate.PassageID, Ordinal: candidate.Ordinal, Text: candidate.Text,
			// document:<search_document_id>@<sha12>#<locator> pins the citation to object bytes.
			CitationToken: "document:" + candidate.SearchDocumentID + "@" +
				shortHash(candidate.RawSHA256) + "#" + locator,
			CitationLocator: locator, Locator: passageLocator(candidate),
			OriginalSHA256: candidate.RawSHA256, NormalizedSHA256: candidate.NormalizedSHA256,
			Evidence: []RetrievalEvidence{},
		}
		doc.passages[candidate.PassageID] = passage
	}
	evidence := RetrievalEvidence{Leg: string(candidate.Leg), Rank: rank}
	if candidate.FusedScore != nil {
		evidence.Score = new(*candidate.FusedScore)
	}
	passage.Evidence = appendEvidence(passage.Evidence, evidence)
	doc.document.Evidence = appendEvidence(doc.document.Evidence, evidence)
}

func passageLocator(candidate arcadedb.PassageCandidate) PassageLocator {
	locator := PassageLocator{
		HeadingPath: append([]string(nil), candidate.HeadingPath...),
	}
	if candidate.CharacterSpan != nil {
		start, end := int(candidate.CharacterSpan.Start), int(candidate.CharacterSpan.End)
		locator.CharStart, locator.CharEnd = &start, &end
	}
	return locator
}

func sortedPassages(passages map[string]*RetrievalPassage, limit int) []RetrievalPassage {
	out := make([]RetrievalPassage, 0, len(passages))
	for _, passage := range passages {
		out = append(out, *passage)
	}
	// Engine order is already the ranking, so ties break on position in the document.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ordinal != out[j].Ordinal {
			return out[i].Ordinal < out[j].Ordinal
		}
		return out[i].PassageID < out[j].PassageID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func appendEvidence(existing []RetrievalEvidence, candidate RetrievalEvidence) []RetrievalEvidence {
	for _, evidence := range existing {
		if evidence.Leg == candidate.Leg && evidence.Rank == candidate.Rank {
			return existing
		}
	}
	return append(existing, candidate)
}
