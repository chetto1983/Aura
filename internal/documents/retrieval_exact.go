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

// exactLane is the question's exact terms and what the full-text indexes found carrying any of
// them. Without a lane (no passage index, or a passage leg that failed) it holds the terms alone,
// which still rank first the documents the legs returned. failed says the lane was asked and could
// not answer.
type exactLane struct {
	terms    []arcadedb.ExactTerm
	passages []arcadedb.PassageCandidate
	cards    []RetrievalCard
	failed   bool
}

// exactTermLane asks the full-text indexes for the terms inside filter's scope. What it returns
// carries one term at least; rankDocuments keeps the documents that carry every one, judged on
// everything it holds for each, because a document may carry one term in a passage and another
// only in its name.
//
// A failure is logged and costs the lane, not the answer.
func (r *HostRetriever) exactTermLane(ctx context.Context, filter arcadedb.CandidateFilter, terms []arcadedb.ExactTerm) exactLane {
	lane := exactLane{terms: terms}
	if len(terms) == 0 {
		return lane
	}
	hits, err := r.PassageIndex.ExactTermDocuments(ctx, filter, terms)
	if err != nil {
		r.degradations.warn(DegradationExactTerms, err.Error(), filter.IdentityID)
		lane.failed = true
		return lane
	}
	lane.passages, lane.cards = hits.Passages, retrievalCards(hits.Cards)
	return lane
}

// rankExactTermsFirst moves every carrier of all the question's exact terms ahead of the documents
// that are not, keeping the score order within each group, and returns how many carry them. It
// runs before the limit, so a carrier is never cut off by the documents that outscore it.
//
// Similarity cannot establish an exact technical identity, which is memory recall's rule (arcadedb
// identifiers.go), and the final vector.rerank orders by cosine alone. Measured 2026-10-16 on the
// complex corpus with identifiers alone: "che difetto riguardava il reclamo RC-2026-040" ranked
// note_pagamenti.txt first and the quality report, the only file holding that number, third. With
// the rule r@1 went 0.735 -> 0.794 and MRR 0.848 -> 0.882 over 34 questions, two golds up and none
// down, and the office fixture did not move (prd.md, "Exact identifiers rank first").
func rankExactTermsFirst(ranked []*rankedDocument) int {
	carriers := make([]*rankedDocument, 0, len(ranked))
	rest := make([]*rankedDocument, 0, len(ranked))
	for _, doc := range ranked {
		if doc.carrier {
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
