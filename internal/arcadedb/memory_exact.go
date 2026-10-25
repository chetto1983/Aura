package arcadedb

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// Exact terms in memory (spec 2026-10-25-document-exactness-and-judge, decision 1, "Memory"): a
// fact or a turn that carries every exact term of the question is admitted past the relevance
// floor and ranks first, as a document does. The identifier filter stays, so a candidate without a
// query identifier is still dropped. A quoted phrase or a file name never filters: a fact about
// "Turin" must still answer a question that quotes "Torino", and cross-language recall is why the
// dense leg exists.

// reasonExactTermsFailed names an exact lane that could not run: the legs' answer stands, without
// the carriers their floor kept out.
const reasonExactTermsFailed = "exact_terms_failed"

// exactMemoryRIDs asks each lexical statement for each term as one phrase with no floor, and
// returns every record found, once, in the order found. The caller hydrates them and keeps those
// that carry every term. The phrase's BM25 is dropped: it is not the reranked cosine a recall
// score reports.
func (c *Client) exactMemoryRIDs(
	ctx context.Context, statements []string, params map[string]any, excluded []string, terms []ExactTerm,
) ([]recallRankedRID, error) {
	var found []recallRankedRID
	seen := make(map[string]struct{})
	for _, statement := range statements {
		for _, term := range terms {
			bound := maps.Clone(params)
			bound["query"], bound["min_lexical_score"] = term.lucenePhrase(), 0
			rows, err := c.Query(ctx, applyRecallExclusions(statement, bound, excluded), bound)
			if err != nil {
				return nil, fmt.Errorf("arcadedb: exact term %q: %w", term.Raw, err)
			}
			for _, ranked := range decodeRecallRanking(rows) {
				if _, duplicate := seen[ranked.rid]; !duplicate {
					seen[ranked.rid] = struct{}{}
					found = append(found, recallRankedRID{rid: ranked.rid, exactOnly: true})
				}
			}
		}
	}
	return found, nil
}

// withExactOnly appends to ranked the exact lane's records it does not already hold.
func withExactOnly(ranked, exact []recallRankedRID) []recallRankedRID {
	held := make(map[string]struct{}, len(ranked))
	for _, item := range ranked {
		held[item.rid] = struct{}{}
	}
	out := slices.Clone(ranked)
	for _, item := range exact {
		if _, ok := held[item.rid]; !ok {
			out = append(out, item)
		}
	}
	return out
}

// exactRecallOrder puts first the records that carry every term, in ranked's order, and drops the
// exact lane's own records that do not: no floor admitted them.
func exactRecallOrder(
	ranked []recallRankedRID, facts map[string]FactHit, turns map[string]ConversationTurnHit, terms []ExactTerm,
) []recallRankedRID {
	if len(terms) == 0 {
		return ranked
	}
	carriers := make([]recallRankedRID, 0, len(ranked))
	rest := make([]recallRankedRID, 0, len(ranked))
	for _, item := range ranked {
		switch {
		case CarriesEvery(terms, recallTexts(item.rid, facts, turns)...):
			carriers = append(carriers, item)
		case !item.exactOnly:
			rest = append(rest, item)
		}
	}
	return append(carriers, rest...)
}

// recallTexts is what a hydrated record says: a fact's statement and the names it joins, or a
// turn's content. A record hydration did not return says nothing.
func recallTexts(rid string, facts map[string]FactHit, turns map[string]ConversationTurnHit) []string {
	if fact, ok := facts[rid]; ok {
		return []string{fact.Statement, fact.Subject, fact.Object}
	}
	if turn, ok := turns[rid]; ok {
		return []string{turn.Content}
	}
	return nil
}

// exactFactsFirst puts first the facts that carry every exact term of query: those hits already
// holds, in its order, then those only the exact lane found. The rest of hits follow, to limit.
func (c *Client) exactFactsFirst(ctx context.Context, query string, hits []FactHit, limit int, asOf time.Time) ([]FactHit, error) {
	terms := QueryExactTerms(query)
	if len(terms) == 0 {
		return hits, nil
	}
	exact, err := c.exactMemoryRIDs(ctx, []string{recallFactLexicalStatement}, map[string]any{
		"as_of":      asOf.UTC().Format(time.RFC3339),
		"candidates": min(max(limit*4, 20), c.memoryLimits().HybridCandidates),
	}, nil, terms)
	if err != nil {
		return hits, err
	}
	var carriers, rest []FactHit
	held := make(map[string]struct{}, len(hits))
	for _, hit := range hits {
		held[hit.RID] = struct{}{}
		if CarriesEvery(terms, hit.Statement, hit.Subject, hit.Object) {
			carriers = append(carriers, hit)
		} else {
			rest = append(rest, hit)
		}
	}
	missing := make([]string, 0, len(exact))
	for _, item := range exact {
		if _, ok := held[item.rid]; !ok {
			missing = append(missing, item.rid)
		}
	}
	if len(missing) > 0 {
		found, err := c.hydrateRecallFacts(ctx, missing, asOf)
		if err != nil {
			return hits, err
		}
		for _, rid := range missing {
			if fact, ok := found[rid]; ok && CarriesEvery(terms, fact.Statement, fact.Subject, fact.Object) {
				carriers = append(carriers, fact)
			}
		}
	}
	ordered := slices.Concat(carriers, rest)
	return ordered[:min(len(ordered), limit)], nil
}

// joinReasons names every reason a read is less than whole, in order, as one "+"-joined value.
func joinReasons(reasons ...string) string {
	kept := slices.DeleteFunc(slices.Clone(reasons), func(reason string) bool { return reason == "" })
	return strings.Join(kept, "+")
}
