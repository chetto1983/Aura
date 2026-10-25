package arcadedb

import (
	"context"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/documents/filecard"
)

// ExactTermHits are the documents that carry one exact term: each file's best passage carrying it,
// and the documents whose card or file name carries it.
type ExactTermHits struct {
	Passages []PassageCandidate
	Cards    []DocumentCard
}

// ExactTermDocuments finds the documents that carry one exact term, inside the caller's scope and
// with no score floor: no score may drop an exact hit (spec 2026-10-25-document-exactness-and-judge,
// decision 1). It runs beside the fused or the lexical leg, so a carrier reaches the ranking even
// when the rerank's candidate cut or the relevance floor removed it there.
//
// The engine's phrase query finds a record holding every word of the phrase, in any order: read
// in FullTextQueryExecutor.collectPhraseMatches on 26.10.2, "We can't verify word order without
// position indexing, so we just require all terms", although the docs promise the order. So each
// text it returns is checked here, before a file's best passage is chosen, and a card without the
// sentences Aura writes into it: its trailer, indexed too, names Aura in every card.
func (d *DocumentIndex) ExactTermDocuments(ctx context.Context, filter CandidateFilter, term ExactTerm) (ExactTermHits, error) {
	filter, err := d.normalizeCandidateFilter(filter)
	if err != nil {
		return ExactTermHits{}, err
	}
	if strings.TrimSpace(term.Raw) == "" {
		return ExactTermHits{}, nil
	}
	where, params := candidateWhere(filter)
	params["query"], params["min_lexical_score"] = term.lucenePhrase(), 0
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return ExactTermHits{}, err
	}
	passages, err := d.lexicalPassages(ctx, client, where, params, filter.Limit, RetrievalLegExact,
		func(candidate PassageCandidate) bool { return term.CarriedBy(candidate.Text) })
	if err != nil {
		return ExactTermHits{}, err
	}
	cards, err := lexicalCards(ctx, client, where, params, filter.Limit)
	if err != nil {
		return ExactTermHits{}, err
	}
	cards = slices.DeleteFunc(cards, func(card DocumentCard) bool {
		return !term.CarriedBy(card.FileName, filecard.WithoutAuraNotes(card.Card))
	})
	return ExactTermHits{Passages: passages, Cards: cards}, nil
}
