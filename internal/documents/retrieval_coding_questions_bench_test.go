//go:build retrieval_eval

// What a coding agent gets back from document_search over Aura's own sources. Each question is
// one asked while working on this repository, and its gold is the files that answer it, chosen
// by reading the code before any run. The production arm is HostRetriever.Retrieve, the call
// document_search and `aura docs search` make, at the tool's default limit; the baseline is the
// same retriever with no embedder, so both legs read the full-text indexes alone (spec §8).
//
// The ranking-rule arms measure a change to rankDocuments before it is made. They re-order the
// production retriever's WHOLE ranking from the evidence every document carries, and the empty
// rule must reproduce production exactly, or the run stops: a re-derivation that drifted from
// the shipped order would be measuring itself.
//
// Every response is written whole to AURA_BENCH_OUT, so what the agent would have read can be
// judged, not only where the gold ranked.
//
//	go test -tags retrieval_eval ./internal/documents/ -run TestCodingAgentQuestions -v
package documents

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// documentSearchDefaultLimit is document_search's own default (internal/agent/tools).
const documentSearchDefaultLimit = 8

// wholeRanking is past anything the two legs can return together (2 x CandidateLimit).
const wholeRanking = 1000

type codingAgentAnswer struct {
	Arm         string            `json:"arm"`
	QID         string            `json:"qid"`
	GoldRank    int               `json:"gold_rank"`
	RetrievalMS int64             `json:"retrieval_ms"`
	Response    RetrievalResponse `json:"response"`
}

// rankingRule is one candidate change to rankDocuments; the zero value is the shipped rule.
type rankingRule struct {
	name string
	// cardsOnlyForFilesWithoutPassages lets a card score only a file that has no passages, so a
	// file with text ranks by its text and not by its name.
	cardsOnlyForFilesWithoutPassages bool
	// reservedPlaces is how many files with no passage at all are held right behind the best hit;
	// zero means reserveCardLane's one. Memory recall measured "at least two of each kind" as
	// where its plateau ends (memory_recall_quota.go).
	reservedPlaces int
	// cardSlots gives the card leg its own places, memory recall's quota (memory_recall_quota.go):
	// cards rank only against cards, passages only against passages, and no card score is ever
	// compared with a passage score.
	cardSlots []int
	// ignoreExactTerms ranks by score alone, without rankExactTermsFirst: the shipped rule
	// before 2026-10-16, kept so its counterfactual stays measured.
	ignoreExactTerms bool
}

type codingAgentArm struct {
	name    string
	ranking map[string][]string
	found   int
}

func TestCodingAgentQuestions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	identity := benchEnv(t, "AURA_BENCH_IDENTITY")
	outPath := benchEnv(t, "AURA_BENCH_OUT")
	pilot, _ := loadBenchPilot(t)
	index, embedder := benchDocumentIndex(t)
	control := &ArcadeRetrievalControlPlane{Index: index}
	production := &HostRetriever{ControlPlane: control, PassageIndex: index, Embedder: embedder}
	lexical := &HostRetriever{ControlPlane: control, PassageIndex: index}
	whole := &HostRetriever{ControlPlane: control, PassageIndex: index, Embedder: embedder,
		Config: RetrievalConfig{MaxLimit: wholeRanking}}
	rules := []rankingRule{
		{name: "cards: no passages at all", cardsOnlyForFilesWithoutPassages: true},
		{name: "quota: cards at 2 and 4", cardSlots: []int{1, 3}},
		{name: "two reserved places", reservedPlaces: 2},
		{name: "three reserved places", reservedPlaces: 3},
		{name: "exact terms ignored", ignoreExactTerms: true},
	}

	arms := []*codingAgentArm{{name: "production"}, {name: "lexical only"}}
	for _, rule := range rules {
		arms = append(arms, &codingAgentArm{name: rule.name})
	}
	for _, arm := range arms {
		arm.ranking = map[string][]string{}
	}
	var answers []codingAgentAnswer
	record := func(arm *codingAgentArm, question benchQuestion, response RetrievalResponse, elapsed time.Duration) {
		keys := sourceKeys(response.Documents)
		arm.ranking[question.QID] = keys
		rank := slices.IndexFunc(keys, func(key string) bool { return slices.Contains(question.GoldDocs, key) }) + 1
		if rank > 0 {
			arm.found++
		}
		answers = append(answers, codingAgentAnswer{
			Arm: arm.name, QID: question.QID, GoldRank: rank, RetrievalMS: elapsed.Milliseconds(), Response: response,
		})
		t.Logf("%-26s %-20s gold %d of %d  %s%s  top: %s", arm.name, question.QID, rank, len(keys),
			response.Status, response.FloorsReason, strings.Join(keys[:min(len(keys), 3)], ", "))
	}

	for _, question := range pilot.Questions {
		request := RetrievalRequest{IdentityID: identity, Query: question.Query, Limit: documentSearchDefaultLimit}
		for i, retriever := range []*HostRetriever{production, lexical} {
			start := time.Now()
			response, err := retriever.Retrieve(ctx, request)
			if err != nil {
				t.Fatalf("%s %s: %v", arms[i].name, question.QID, err)
			}
			record(arms[i], question, response, time.Since(start))
		}

		request.Limit = wholeRanking
		full, err := whole.Retrieve(ctx, request)
		if err != nil {
			t.Fatalf("whole ranking %s: %v", question.QID, err)
		}
		shipped := applyRankingRule(full.Documents, rankingRule{}, question.Query)
		if got, want := sourceKeys(shipped), arms[0].ranking[question.QID]; !slices.Equal(got, want) {
			t.Fatalf("%s: the re-derived shipped order drifted from production:\n got %v\nwant %v",
				question.QID, got, want)
		}
		for i, rule := range rules {
			response := full
			response.Documents = applyRankingRule(full.Documents, rule, question.Query)
			record(arms[2+i], question, response, 0)
		}
	}

	for _, arm := range arms {
		metrics := scoreBenchmark(pilot.Questions, arm.ranking)
		t.Logf("%-26s recall@1=%.3f recall@3=%.3f recall@%d=%.3f mrr=%.3f over %d questions", arm.name,
			metrics.RecallAt1, metrics.RecallAt3, documentSearchDefaultLimit,
			float64(arm.found)/float64(len(pilot.Questions)), metrics.MRR, metrics.Queries)
	}
	encoded, err := json.MarshalIndent(answers, "", " ")
	if err != nil {
		t.Fatalf("encode answers: %v", err)
	}
	if err := os.WriteFile(outPath, encoded, 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
}

// applyRankingRule re-orders a whole ranking the way rankDocuments would under rule, from the
// evidence each document carries (retrieval_rank.go: the best score, then the best position
// either leg gave it, then the id, then the earliest passage, with the documents carrying the
// question's exact terms first and the reserved places behind them), and keeps the tool's limit.
func applyRankingRule(documents []RetrievalDocument, rule rankingRule, query string) []RetrievalDocument {
	var all []rankedEvidence
	for _, document := range documents {
		entry := evidenceOf(document)
		entry.score = max(entry.passageScore, entry.cardScore)
		if rule.cardsOnlyForFilesWithoutPassages && hasPassages(document) {
			if len(document.Passages) == 0 {
				continue // found by its name alone
			}
			entry.score = entry.passageScore
		}
		all = append(all, entry)
	}
	if len(rule.cardSlots) > 0 {
		return withCardSlots(all, rule.cardSlots)
	}
	sortRanked(all)
	carriers := 0
	if terms := arcadedb.QueryExactTerms(query); !rule.ignoreExactTerms && len(terms) > 0 {
		sort.SliceStable(all, func(i, j int) bool {
			return carriesExactTerms(all[i].document, terms) && !carriesExactTerms(all[j].document, terms)
		})
		for carriers < len(all) && carriesExactTerms(all[carriers].document, terms) {
			carriers++
		}
	}
	// The k-th file with no passage sits no lower than place k+2 behind the carriers, the best
	// hit among the rest staying first.
	rest := all[carriers:]
	for lane := 1; lane <= max(rule.reservedPlaces, 1); lane++ {
		seen, best := 0, -1
		for index, entry := range rest {
			if !hasPassages(entry.document) {
				if seen++; seen == lane {
					best = index
					break
				}
			}
		}
		if best > lane {
			promoted := rest[best]
			copy(rest[lane+1:best+1], rest[lane:best])
			rest[lane] = promoted
		}
	}
	out := make([]RetrievalDocument, 0, documentSearchDefaultLimit)
	for _, entry := range all[:min(len(all), documentSearchDefaultLimit)] {
		out = append(out, entry.document)
	}
	return out
}

// rankedEvidence is one document and what each leg said about it.
type rankedEvidence struct {
	document     RetrievalDocument
	passageScore float64
	cardScore    float64
	score        float64
	order        int
	ordinal      int64
}

func evidenceOf(document RetrievalDocument) rankedEvidence {
	entry := rankedEvidence{document: document, order: math.MaxInt, ordinal: math.MaxInt64}
	for _, evidence := range document.Evidence {
		// The exact lane sets neither the score nor the order (rankDocuments).
		if evidence.Leg == string(arcadedb.RetrievalLegExact) {
			continue
		}
		entry.order = min(entry.order, evidence.Rank-1)
		if evidence.Score == nil {
			continue
		}
		if evidence.Leg == "card" {
			entry.cardScore = max(entry.cardScore, *evidence.Score)
		} else {
			entry.passageScore = max(entry.passageScore, *evidence.Score)
		}
	}
	if len(document.Passages) > 0 {
		entry.ordinal = document.Passages[0].Ordinal
	}
	return entry
}

// sortRanked is rankDocuments' order on entry.score.
func sortRanked(all []rankedEvidence) {
	sort.Slice(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		if all[i].order != all[j].order {
			return all[i].order < all[j].order
		}
		if all[i].document.DocumentID != all[j].document.DocumentID {
			return all[i].document.DocumentID < all[j].document.DocumentID
		}
		return all[i].ordinal < all[j].ordinal
	})
}

// withCardSlots fills the tool's limit from two rankings that never meet: the documents the
// passage leg found, by their passage score, and the documents the card leg found, by their card
// score, the latter at slots. A document in both takes the earlier place it earns; a ranking that
// runs out leaves its places to the other.
func withCardSlots(all []rankedEvidence, slots []int) []RetrievalDocument {
	var byPassage, byCard []rankedEvidence
	for _, entry := range all {
		if entry.passageScore > 0 {
			entry.score = entry.passageScore
			byPassage = append(byPassage, entry)
		}
		if entry.cardScore > 0 {
			entry.score = entry.cardScore
			byCard = append(byCard, entry)
		}
	}
	sortRanked(byPassage)
	sortRanked(byCard)
	placed := map[string]bool{}
	next := func(ranking []rankedEvidence) (RetrievalDocument, []rankedEvidence, bool) {
		for len(ranking) > 0 {
			entry := ranking[0]
			ranking = ranking[1:]
			if !placed[entry.document.DocumentID] {
				placed[entry.document.DocumentID] = true
				return entry.document, ranking, true
			}
		}
		return RetrievalDocument{}, ranking, false
	}
	out := make([]RetrievalDocument, 0, documentSearchDefaultLimit)
	for position := range documentSearchDefaultLimit {
		first, second := &byPassage, &byCard
		if slices.Contains(slots, position) {
			first, second = &byCard, &byPassage
		}
		document, rest, ok := next(*first)
		*first = rest
		if !ok {
			if document, rest, ok = next(*second); !ok {
				break
			}
			*second = rest
		}
		out = append(out, document)
	}
	return out
}

// hasPassages is whether the file has any passage at all, which only its card knows; a document
// the card leg did not rank came in through a passage, so it has one.
func hasPassages(document RetrievalDocument) bool {
	return document.PassageCount == nil || *document.PassageCount > 0
}

func sourceKeys(documents []RetrievalDocument) []string {
	keys := make([]string, 0, len(documents))
	for _, document := range documents {
		keys = append(keys, document.SourceKey)
	}
	return keys
}
