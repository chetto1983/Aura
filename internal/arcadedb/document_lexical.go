package arcadedb

import (
	"context"
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Documents in lexical mode (spec §8): the full-text indexes alone, when the dense legs cannot
// run -- the documents gate is closed, it could not be read, or the query could not be
// embedded.
//
// BM25 cannot abstain on raw scores. Measured 2026-09-24 on the lab VM's library (12
// documents, 50 passages): "orari dei traghetti per la Sardegna", which it cannot answer,
// scored 7.107 on "dei", "la" and "per" alone, above the correct top match for "PidTemp
// MultiZone" (5.361). SEARCH_INDEX takes the index and the query and nothing else
// (arcadedb-docs how-to/data-modeling/full-text-index.adoc; SQLFunctionSearchIndex requires 2
// parameters), and the analyzer that could drop stopwords is fixed when the index is created.
// So they are dropped here, before the query is sent, from the lists Lucene itself ships.

// italianStopwords and englishStopwords are Lucene's Snowball stop lists, byte for byte from
// lucene-analysis-common-10.5.1.jar (org/apache/lucene/analysis/snowball/), the jar ArcadeDB
// 26.9.1 runs. BSD-licensed; the notice is inside each file.
//
//go:embed stopwords/italian_stop.txt
var italianStopwords string

//go:embed stopwords/english_stop.txt
var englishStopwords string

var documentStopwords = stopwordSet(italianStopwords, englishStopwords)

// stopwordSet reads Snowball stop lists: words separated by white space, "|" starting a
// comment (Lucene WordlistLoader.getSnowballWordSet).
func stopwordSet(lists ...string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, list := range lists {
		for line := range strings.SplitSeq(list, "\n") {
			line, _, _ = strings.Cut(line, "|")
			for word := range strings.FieldsSeq(line) {
				set[word] = struct{}{}
			}
		}
	}
	return set
}

// documentLexicalMinScore is the documents twin of memory's LexicalMinScore, applied the same
// way (lexicalScoreFloor: a query of one remaining word needs no floor). Measured 2026-09-24
// with the stopwords dropped: all six out-of-corpus questions, three Italian and three English,
// matched nothing at all; the lowest correct result at the head of a leg, for a query of two
// or more words, was a card at 2.68, and the highest wrong result anywhere 1.02. Card and
// passage scores come from different indexes, so this is one number measured against both,
// not a shared scale.
const documentLexicalMinScore = 2

// lexicalQuery is what the lexical legs search for -- the query's words less its stopwords --
// and the floor those words earn. Empty terms mean there is nothing left to search for.
func lexicalQuery(query string) (string, float64) {
	kept := make([]string, 0, 8)
	for field := range strings.FieldsSeq(query) {
		word := strings.ToLower(strings.TrimFunc(field, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		}))
		if word == "" {
			continue
		}
		if _, stop := documentStopwords[word]; stop {
			continue
		}
		kept = append(kept, field)
	}
	terms := strings.Join(kept, " ")
	return terms, lexicalScoreFloor(terms, documentLexicalMinScore)
}

// lexicalRequest validates a lexical read as the dense reads are validated, and binds its
// terms and floor. Empty terms return no parameters: the caller sends nothing.
func (d *DocumentIndex) lexicalRequest(filter CandidateFilter, query string) (CandidateFilter, string, map[string]any, error) {
	filter, err := d.normalizeCandidateFilter(filter)
	if err != nil {
		return CandidateFilter{}, "", nil, err
	}
	query, err = d.validQuery(query)
	if err != nil {
		return CandidateFilter{}, "", nil, err
	}
	terms, floor := lexicalQuery(query)
	if terms == "" {
		return filter, "", nil, nil
	}
	where, params := candidateWhere(filter)
	params["query"], params["min_lexical_score"] = escapeLucene(terms), floor
	return filter, where, params, nil
}

// LexicalCandidates ranks each file's best passage by the full-text index alone, best first. A
// query that is only stopwords returns nothing without asking ArcadeDB.
func (d *DocumentIndex) LexicalCandidates(ctx context.Context, filter CandidateFilter, query string) ([]PassageCandidate, error) {
	filter, where, params, err := d.lexicalRequest(filter, query)
	if err != nil || params == nil {
		return nil, err
	}
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return nil, err
	}
	// Over-fetch, as the fused leg does: the grouping below runs after the LIMIT, and a file
	// with many matching passages would otherwise fill the pool on its own.
	fetch := min(max(filter.Limit*4, 20), d.config.MaxRetrievalCandidates)
	rows, err := client.Query(ctx, lexicalPassageStatement(where, fetch), params)
	if err != nil {
		if missingIngestType(err, documentPassageType) {
			return nil, nil // nothing ingested yet — an empty library, not a failure
		}
		return nil, fmt.Errorf("arcadedb: lexical document candidates: %w", err)
	}
	candidates, err := d.decodeCandidates(rows, RetrievalLegLexical, fetch)
	if err != nil {
		return nil, err
	}
	best := bestPassagePerFile(candidates)
	return best[:min(len(best), filter.Limit)], nil
}

// bestPassagePerFile keeps each file's first, so best, passage: what the fused statement's
// groupBy 'raw_sha256', groupSize 1 does inside the engine. rankDocuments reads one passage per
// file, and a passage two files share verbatim would otherwise merge them under one title.
func bestPassagePerFile(ranked []PassageCandidate) []PassageCandidate {
	kept := make([]PassageCandidate, 0, len(ranked))
	seen := make(map[string]struct{}, len(ranked))
	for _, candidate := range ranked {
		if _, duplicate := seen[candidate.RawSHA256]; duplicate {
			continue
		}
		seen[candidate.RawSHA256] = struct{}{}
		kept = append(kept, candidate)
	}
	return kept
}

// LexicalDocumentCards ranks documents by their card and by their split file name, one query
// per index, a document keeping the higher of its two scores. One OR query is not used: its
// $score depends on predicate order (measured 1.3798 against 1.2880 for one document, audit F8).
func (d *DocumentIndex) LexicalDocumentCards(ctx context.Context, filter CandidateFilter, query string) ([]DocumentCard, error) {
	filter, where, params, err := d.lexicalRequest(filter, query)
	if err != nil || params == nil {
		return nil, err
	}
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return nil, err
	}
	best := make(map[string]DocumentCard)
	for _, field := range []string{"card", "file_name_words"} {
		rows, err := client.Query(ctx, lexicalCardStatement(field, where, filter.Limit), params)
		if err != nil {
			if missingIndexedDocumentType(err) {
				return nil, nil // nothing ingested yet — an empty library, not a failure
			}
			return nil, fmt.Errorf("arcadedb: lexical document cards (%s): %w", field, err)
		}
		for index, row := range rows {
			card, err := decodeDocumentCard(row)
			if err != nil {
				return nil, fmt.Errorf("arcadedb: lexical document card %d: %w", index, err)
			}
			if kept, seen := best[card.SearchDocumentID]; !seen || card.Score > kept.Score {
				best[card.SearchDocumentID] = card
			}
		}
	}
	cards := make([]DocumentCard, 0, len(best))
	for _, card := range best {
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].Score != cards[j].Score {
			return cards[i].Score > cards[j].Score
		}
		return cards[i].SearchDocumentID < cards[j].SearchDocumentID
	})
	return cards[:min(len(cards), filter.Limit)], nil
}

// lexicalPassageStatement ranks passages by BM25. The floor sits in the WHERE, as memory's
// lexical leg has it (searchFactsStatement), and the scope follows the full-text match so the
// caller's document filter always applies.
func lexicalPassageStatement(where string, limit int) string {
	return "SELECT " + passageCandidateFields + ", $score AS lexical_score FROM " + documentPassageType +
		" WHERE SEARCH_INDEX('" + documentPassageType + "[text]', :query) = true AND " + where +
		" AND $score >= :min_lexical_score ORDER BY lexical_score DESC, passage_key ASC LIMIT " +
		strconv.Itoa(limit)
}

// lexicalCardStatement ranks cards by BM25 over one of their two full-text indexes.
func lexicalCardStatement(field, where string, limit int) string {
	return "SELECT " + documentCardFields + ", $score AS card_score FROM " + IndexedDocumentType +
		" WHERE SEARCH_INDEX('" + IndexedDocumentType + "[" + field + "]', :query) = true AND " + where +
		" AND $score >= :min_lexical_score ORDER BY card_score DESC, search_document_id ASC LIMIT " +
		strconv.Itoa(limit)
}
