package documents

import (
	"context"
	"slices"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/documents/filecard"
)

// Exact terms (spec 2026-10-25-document-exactness-and-judge, decision 1): a phrase the caller
// quoted, an identifier, a file name. A document that carries every one of them is admitted
// whatever its score and ranks ahead of every document that does not. No score may drop it, which
// is Paperless-ngx's rule: a second scorer may reorder an exact hit, never remove it.

// exactTermsUnavailable names a failed exact lane in the degradation log. It is not a degradation
// of the response: the legs' answer is intact and only the bypass is missing, as with
// passage_names_unavailable.
const exactTermsUnavailable = "exact_terms_unavailable"

// exactHits is what the full-text indexes found for one exact term, kept only for the documents
// that carry every exact term of the question.
type exactHits struct {
	passages []arcadedb.PassageCandidate
	cards    []RetrievalCard
}

// exactLane is the question's exact terms and, one entry per term, what the indexes found for it.
// Without a lane (no passage index, or a passage leg that failed) hits is empty, and the terms
// still rank first the documents the legs returned.
type exactLane struct {
	terms []arcadedb.ExactTerm
	hits  []exactHits
}

func (l exactLane) passages() []arcadedb.PassageCandidate {
	var all []arcadedb.PassageCandidate
	for _, found := range l.hits {
		all = append(all, found.passages...)
	}
	return all
}

func (l exactLane) cards() []RetrievalCard {
	var all []RetrievalCard
	for _, found := range l.hits {
		all = append(all, found.cards...)
	}
	return all
}

func (l exactLane) empty() bool {
	return len(l.passages()) == 0 && len(l.cards()) == 0
}

// exactTermLane asks the full-text indexes for each term inside filter's scope, one phrase query
// per term, because a document may carry one term in a passage and another in its name. It keeps
// what was found for the documents found for every term: carrying some of them is no bypass.
//
// A failure is logged and costs the lane, not the answer, as passageLegNames' does.
func (r *HostRetriever) exactTermLane(ctx context.Context, filter arcadedb.CandidateFilter, terms []arcadedb.ExactTerm) exactLane {
	lane := exactLane{terms: terms}
	found := make([]arcadedb.ExactTermHits, 0, len(terms))
	for _, term := range terms {
		hits, err := r.PassageIndex.ExactTermDocuments(ctx, filter, term)
		if err != nil {
			r.degradations.warn(exactTermsUnavailable, err.Error(), filter.IdentityID)
			return lane
		}
		found = append(found, hits)
	}
	carriers := carriersOfEvery(found)
	for _, hits := range found {
		passages := slices.DeleteFunc(slices.Clone(hits.Passages), func(passage arcadedb.PassageCandidate) bool {
			_, carrier := carriers[contentKey(passage.RawSHA256, passage.SearchDocumentID)]
			return !carrier
		})
		cards := slices.DeleteFunc(slices.Clone(hits.Cards), func(card arcadedb.DocumentCard) bool {
			_, carrier := carriers[contentKey(card.RawSHA256, card.SearchDocumentID)]
			return !carrier
		})
		lane.hits = append(lane.hits, exactHits{passages: passages, cards: retrievalCards(cards)})
	}
	return lane
}

// carriersOfEvery is the content keys found for every term. Keyed by content as rankDocuments is:
// the passage index returns one copy of a file stored twice, the card index both.
func carriersOfEvery(found []arcadedb.ExactTermHits) map[string]struct{} {
	counts := make(map[string]int)
	for _, hits := range found {
		keys := make(map[string]struct{}, len(hits.Passages)+len(hits.Cards))
		for _, passage := range hits.Passages {
			keys[contentKey(passage.RawSHA256, passage.SearchDocumentID)] = struct{}{}
		}
		for _, card := range hits.Cards {
			keys[contentKey(card.RawSHA256, card.SearchDocumentID)] = struct{}{}
		}
		for key := range keys {
			counts[key]++
		}
	}
	carriers := make(map[string]struct{}, len(counts))
	for key, count := range counts {
		if count == len(found) {
			carriers[key] = struct{}{}
		}
	}
	return carriers
}

// rankExactTermsFirst moves every document that carries all of the question's exact terms ahead of
// those that do not, keeping the score order within each group, and returns how many carry them.
// It runs before the limit, so a carrier is never cut off by the documents that outscore it.
//
// Similarity cannot establish an exact technical identity, which is memory recall's rule (arcadedb
// identifiers.go), and the final vector.rerank orders by cosine alone. Measured 2026-10-16 on the
// complex corpus with identifiers alone: "che difetto riguardava il reclamo RC-2026-040" ranked
// note_pagamenti.txt first and the quality report, the only file holding that number, third. With
// the rule r@1 went 0.735 -> 0.794 and MRR 0.848 -> 0.882 over 34 questions, two golds up and none
// down, and the office fixture did not move (prd.md, "Exact identifiers rank first").
func rankExactTermsFirst(ranked []*rankedDocument, terms []arcadedb.ExactTerm) int {
	if len(terms) == 0 {
		return 0
	}
	carriers := make([]*rankedDocument, 0, len(ranked))
	rest := make([]*rankedDocument, 0, len(ranked))
	for _, doc := range ranked {
		if carriesExactTerms(doc.document, terms) {
			carriers = append(carriers, doc)
		} else {
			rest = append(rest, doc)
		}
	}
	copy(ranked, carriers)
	copy(ranked[len(carriers):], rest)
	return len(carriers)
}

// carriesExactTerms reports whether the document's title, its card without the sentences Aura
// writes into every card, or the passages it returns carry every term. What the response returns
// is what is judged, so a carrier shows the agent why it ranks first; sortedPassages keeps the
// passages with exact evidence when it cuts.
func carriesExactTerms(document RetrievalDocument, terms []arcadedb.ExactTerm) bool {
	texts := make([]string, 0, 2+len(document.Passages))
	texts = append(texts, document.Title, filecard.WithoutAuraNotes(document.Card))
	for _, passage := range document.Passages {
		texts = append(texts, passage.Text)
	}
	return arcadedb.CarriesEvery(terms, texts...)
}

func hasExactEvidence(passage RetrievalPassage) bool {
	return slices.ContainsFunc(passage.Evidence, func(evidence RetrievalEvidence) bool {
		return evidence.Leg == string(arcadedb.RetrievalLegExact)
	})
}
