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
	// Cards and passages share the reconciler-derived search_document_id.
	byDocumentID := make(map[string]*rankedDocument, len(cards)+len(passages))
	// Passages first and their order wins: a document the engine ranked is better
	// evidenced than one only a card mentions, so cards start after the last passage.
	for rank, passage := range passages {
		doc := ensureRankedDocumentFromCandidate(byDocumentID, passage, names)
		doc.order = min(doc.order, rank)
		doc.ordinal = min(doc.ordinal, passage.Ordinal)
		if passage.FusedScore != nil && *passage.FusedScore > doc.document.Score {
			doc.document.Score = *passage.FusedScore
		}
		mergePassage(doc, passage, rank+1)
	}
	for rank, card := range cards {
		doc := ensureRankedDocumentFromCard(byDocumentID, card)
		doc.order = min(doc.order, len(passages)+rank)
		doc.document.Evidence = appendEvidence(doc.document.Evidence, RetrievalEvidence{
			Leg: "card", Rank: rank + 1, Score: new(card.Rank),
		})
	}

	ranked := make([]*rankedDocument, 0, len(byDocumentID))
	for _, doc := range byDocumentID {
		doc.document.RequiresOpen = forceOpen || len(doc.passages) == 0
		doc.document.Passages = sortedPassages(doc.passages, topPassages)
		ranked = append(ranked, doc)
	}
	sort.Slice(ranked, func(i, j int) bool {
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
func ensureRankedDocumentFromCard(byDocumentID map[string]*rankedDocument, card RetrievalCard) *rankedDocument {
	doc := byDocumentID[card.DocumentID]
	if doc == nil {
		doc = newRankedDocument(card.DocumentID)
		byDocumentID[card.DocumentID] = doc
	}
	doc.document.Title = card.Title
	doc.document.Card = card.Card
	if doc.document.SourceKey == "" {
		doc.document.SourceKind, doc.document.SourceKey = card.SourceKind, card.SourceKey
	}
	if doc.document.OriginalSHA256 == "" {
		doc.document.OriginalSHA256 = card.OriginalSHA256
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
	byDocumentID map[string]*rankedDocument,
	candidate arcadedb.PassageCandidate,
	names map[string]string,
) *rankedDocument {
	doc := byDocumentID[candidate.SearchDocumentID]
	if doc == nil {
		doc = newRankedDocument(candidate.SearchDocumentID)
		byDocumentID[candidate.SearchDocumentID] = doc
	}
	doc.document.SourceKind, doc.document.SourceKey = candidate.SourceKind, candidate.SourceKey
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
