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
	// them, and any order re-derived in Go could only disagree with it -- which is what
	// the tier ladder this replaces did, at 0.300 recall@1 against 0.850.
	order   int
	ordinal int64
}

func rankDocuments(
	cards []RetrievalCard,
	passages []arcadedb.PassageCandidate,
	limit int,
	topPassages int,
	forceOpen bool,
) []RetrievalDocument {
	// Keyed by search_document_id, which BOTH sides already carry: a card's DocumentID is
	// aura.documents.search_document_id, and a candidate's is the same string the sidecar
	// derived from (identity, "s3", source_key).
	//
	// It used to be keyed by the card's catalog uuid, and that made the CARD the spine: a
	// document existed in the answer only because a card created it, and passages attached
	// to it afterwards. A document reconciled from the bucket has no catalog row at all, so
	// its passages matched the query and were then dropped on the floor with nothing to
	// attach to. The passage is the spine now and the card is enrichment -- the shape
	// cognee uses, where the chunk carries document_id/document_name for reference
	// rendering rather than joining to find them.
	byDocumentID := make(map[string]*rankedDocument, len(cards)+len(passages))
	// Passages first and their order wins: a document the engine ranked is better
	// evidenced than one only a card mentions, so cards start after the last passage.
	for rank, passage := range passages {
		doc := ensureRankedDocumentFromCandidate(byDocumentID, passage)
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

// ensureRankedDocumentFromCard adds the catalog's human-facing fields -- title, tags, the
// digest and the card body. They are enrichment: a document is perfectly answerable
// without them, it just answers with a filename instead of a title.
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
// caller. SourceKey is both the title fallback and the route back to the bytes.
func ensureRankedDocumentFromCandidate(
	byDocumentID map[string]*rankedDocument, candidate arcadedb.PassageCandidate,
) *rankedDocument {
	doc := byDocumentID[candidate.SearchDocumentID]
	if doc == nil {
		doc = newRankedDocument(candidate.SearchDocumentID)
		byDocumentID[candidate.SearchDocumentID] = doc
	}
	doc.document.SourceKind, doc.document.SourceKey = candidate.SourceKind, candidate.SourceKey
	if doc.document.Title == "" {
		doc.document.Title = path.Base(candidate.SourceKey)
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
			// document:<search_document_id>@<sha12>#<locator>. The middle field was the
			// version number, which is always absent now; the digest of the object replaces
			// it and is strictly better at the job a citation has -- a version number says
			// which row was current, while the digest says whether these are still the bytes
			// that were quoted. Twelve hex characters, like every other short digest here.
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
		SelfRef: candidate.SelfRef, HeadingPath: append([]string(nil), candidate.HeadingPath...),
		Captions:  append([]string(nil), candidate.Captions...),
		SheetName: candidate.SheetName, TableName: candidate.TableName,
		CellRef: candidate.CellReference,
	}
	locator.PageNumber = intFromInt64(candidate.PageNumber)
	locator.RowNumber = intFromInt64(candidate.RowNumber)
	locator.ColumnNumber = intFromInt64(candidate.ColumnNumber)
	if candidate.BoundingBox != nil {
		locator.BoundingBox = []float64{
			candidate.BoundingBox.Left, candidate.BoundingBox.Top,
			candidate.BoundingBox.Right, candidate.BoundingBox.Bottom,
		}
	}
	if candidate.CharacterSpan != nil {
		start, end := int(candidate.CharacterSpan.Start), int(candidate.CharacterSpan.End)
		locator.CharStart, locator.CharEnd = &start, &end
	}
	return locator
}

func intFromInt64(value *int64) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
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
