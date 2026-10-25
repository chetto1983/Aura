package arcadedb

import (
	"context"
	"slices"

	"github.com/chetto1983/aura/internal/documents/filecard"
)

// ExactTermHits are the texts that carry one of the question's exact terms: each file's best passage
// for every term it carries, and the documents whose card or file name carries one.
type ExactTermHits struct {
	Passages []PassageCandidate
	Cards    []DocumentCard
}

// ExactTermDocuments finds the texts that carry the question's exact terms, inside the caller's
// scope and with no score floor: no score may drop an exact hit (spec
// 2026-10-25-document-exactness-and-judge, decision 1). It runs beside the fused or the lexical leg,
// so a carrier reaches the ranking even when the rerank's candidate cut or the relevance floor
// removed it there. Whether a document carries every term is the ranking's to decide, on everything
// it holds for that document: the engine and Go tokenise apart, so a document found here for one
// term may carry another only in its title (calibre_demo.docx is one token to the engine).
//
// One query per index asks for every term, the phrases OR-ed (lucenePhrases), so the cost does not
// grow with the terms. Its pool is the index's candidate cap, shared by the terms: what the cap cuts
// is not seen here.
//
// The engine's phrase query finds a record holding every word of the phrase, in any order: read
// in FullTextQueryExecutor.collectPhraseMatches on 26.10.2, "We can't verify word order without
// position indexing, so we just require all terms", although the docs promise the order. So each
// text it returns is checked here, before a file's best passage is chosen, and a card without the
// sentences Aura writes into it: its trailer, indexed too, names Aura in every card.
func (d *DocumentIndex) ExactTermDocuments(ctx context.Context, filter CandidateFilter, terms []ExactTerm) (ExactTermHits, error) {
	filter, err := d.normalizeCandidateFilter(filter)
	if err != nil || len(terms) == 0 {
		return ExactTermHits{}, err
	}
	where, params := candidateWhere(filter)
	params["query"], params["min_lexical_score"] = lucenePhrases(terms), 0
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return ExactTermHits{}, err
	}
	candidates, err := d.lexicalPassages(ctx, client, where, params, filter.Limit, RetrievalLegExact)
	if err != nil {
		return ExactTermHits{}, err
	}
	cards, err := lexicalCards(ctx, client, where, params, filter.Limit)
	if err != nil {
		return ExactTermHits{}, err
	}
	cards = slices.DeleteFunc(cards, func(card DocumentCard) bool {
		return !carriesAny(terms, card.FileName, filecard.WithoutAuraNotes(card.Card))
	})
	return ExactTermHits{Passages: bestPassagePerTerm(candidates, terms), Cards: cards}, nil
}

// bestPassagePerTerm keeps, in the engine's order, each file's first passage carrying each term: a
// file may carry one term in one passage and another in a second, and both are evidence.
func bestPassagePerTerm(candidates []PassageCandidate, terms []ExactTerm) []PassageCandidate {
	kept := make([]PassageCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		carries := false
		for _, term := range terms {
			key := candidate.RawSHA256 + "\x00" + term.Key
			if _, found := seen[key]; found || !term.CarriedBy(candidate.Text) {
				continue
			}
			seen[key], carries = struct{}{}, true
		}
		if carries {
			kept = append(kept, candidate)
		}
	}
	return kept
}

func carriesAny(terms []ExactTerm, texts ...string) bool {
	return slices.ContainsFunc(terms, func(term ExactTerm) bool { return term.CarriedBy(texts...) })
}
