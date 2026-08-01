//go:build arcadedb_integration

// Mem0's retrieval algorithm, with our leg instead of theirs.
//
// Read off mem0 at 29fa4155 (2026-07-31), mem0/memory/main.py _search_vector_store
// and mem0/utils/scoring.py. Their nine steps are:
//
//	lemmatise query -> extract query entities (spaCy) -> embed query ->
//	semantic search over-fetching max(limit*4,60) -> BM25 on the lemmatised query ->
//	entity boost from a VECTOR entity store (cosine >= 0.5, weight 0.5, hub penalty
//	1/(1+0.001*(n-1)^2)) -> candidates taken FROM THE SEMANTIC RESULTS ONLY ->
//	additive fusion (semantic + bm25 + entity) / adaptive max -> format.
//
// We have no semantic leg, so the faithful transcription is impossible: their
// threshold gates on the semantic score, which means for them lexical never
// retrieves, it only re-ranks. What we do have is the thing they approximate --
// exact entity linkage. So the entity store here is a real graph edge,
// (:LocomoEntity)-[:MENTIONED_IN]->(:LocomoTurn), traversed by name rather than
// matched by cosine, and everything else (over-fetch, sigmoid normalisation,
// hub penalty, additive fusion with an adaptive divisor) is theirs.
//
// Two variants, because they answer different questions:
//
//	A  candidates from lexical only, entities re-rank      -> can entity linkage REORDER?
//	B  candidates from lexical UNION entity traversal      -> can it RECOVER what lexical missed?
//
// Baseline to beat: lexical alone, R@1 20.8 / R@5 36.0 / R@10 43.8.
//
// Run: go test -tags arcadedb_integration -run LocomoMem0 -v ./internal/arcadedb/
package arcadedb

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/bm25"
)

const (
	entityType = "LocomoEntity"
	// entityBoostWeight and hubPenaltyK are Mem0's, verbatim: ENTITY_BOOST_WEIGHT
	// = 0.5 and boost = similarity * weight * 1/(1+0.001*(n-1)^2).
	entityBoostWeight = 0.5
	hubPenaltyK       = 0.001
	// overFetch is Mem0's max(limit*4, 60) with limit 10.
	overFetch = 60
)

// spacyOrSkip reaches the interpreter and models that live in the MCP image, so
// the benchmark extracts entities with exactly what production will use rather
// than an approximation of it. AURA_SPACY_CMD overrides the command.
func spacyOrSkip(t *testing.T) *SpacyExtractor {
	t.Helper()
	argv := strings.Fields(envOr("AURA_SPACY_CMD",
		"docker exec -i aura-arcadedb-mcp python /opt/arcadedb-mcp/spacy_worker.py"))
	extractor, err := NewSpacyExtractor(argv, "en")
	if err != nil {
		t.Skipf("spaCy worker unavailable: %v", err)
	}
	// One probe: a worker that starts and then cannot load its model would
	// otherwise degrade silently to the regex fallback and quietly measure that
	// instead, which is the whole thing this run exists to compare against.
	// "sunrise" is the tell -- lowercase, so no capital-letter rule can reach it,
	// and stripped of its determiner, which is what the worker is meant to do.
	probe := extractor.Entities("When did Melanie paint a sunrise?")
	if !slices.Contains(probe, "sunrise") {
		_ = extractor.Close()
		t.Skipf("spaCy worker answered without noun chunks (%v); refusing to "+
			"report regex numbers as spaCy numbers", probe)
	}
	t.Cleanup(func() { _ = extractor.Close() })
	return extractor
}

// bm25Params is get_bm25_params from mem0/utils/scoring.py, with the midpoints
// rescaled. Theirs assume raw BM25 in the 0-20 band; ArcadeDB's $score for these
// turns sits between 1 and 4, so their midpoint of 5 would push every real hit
// to the flat bottom of the sigmoid and normalise the whole signal to zero. The
// SHAPE -- longer query, higher midpoint, gentler slope -- is kept.
func bm25Params(query string) (midpoint, steepness float64) {
	terms := len(strings.Fields(query))
	switch {
	case terms <= 3:
		return 1.0, 3.5
	case terms <= 6:
		return 1.4, 3.0
	case terms <= 9:
		return 1.8, 2.5
	case terms <= 15:
		return 2.0, 2.5
	default:
		return 2.4, 2.5
	}
}

// normalizeBM25 is normalize_bm25 verbatim: a logistic sigmoid.
func normalizeBM25(raw, midpoint, steepness float64) float64 {
	return 1.0 / (1.0 + math.Exp(-steepness*(raw-midpoint)))
}

func ensureEntitySchema(t *testing.T, client *Client) {
	t.Helper()
	for _, ddl := range []string{
		"CREATE VERTEX TYPE " + entityType + " IF NOT EXISTS",
		"CREATE PROPERTY " + entityType + ".name IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON " + entityType + " (name) UNIQUE",
		"CREATE EDGE TYPE MENTIONED_IN IF NOT EXISTS",
	} {
		if _, err := client.Command(context.Background(), ddl, nil); err != nil {
			t.Fatalf("ddl %q: %v", ddl, err)
		}
	}
}

// linkEntities builds the entity layer as graph edges. This is the substitution:
// where Mem0 embeds every entity and matches by cosine, an exact name walks
// straight to its mentions.
const linkEntitiesBatch = `
UNWIND $links AS link
MERGE (e:LocomoEntity {name: link.entity})
WITH e, link
MATCH (t:LocomoTurn {id: link.turn})
MERGE (e)-[:MENTIONED_IN]->(t)
RETURN count(*) AS linked
`

func TestLocomoMem0EntityLayerLoads(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	samples := loadLocomo(t)
	ensureEnglishLocomoSchema(t, client)
	ensureEntitySchema(t, client)

	for _, statement := range []string{
		"DELETE FROM MENTIONED_IN", "DELETE FROM " + entityType,
	} {
		if _, err := client.Command(ctx, statement, nil); err != nil {
			t.Fatalf("clear %q: %v", statement, err)
		}
	}

	extractor := spacyOrSkip(t)

	started := time.Now()
	turnIDs, texts := []string{}, []string{}
	for index, sample := range samples {
		conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
		for _, turn := range turnsOf(sample) {
			turnIDs = append(turnIDs, conversation+"#"+turn.DiaID)
			texts = append(texts, turn.Speaker+": "+turn.Text)
		}
	}
	// Batched: spaCy's own pipe() is what makes 5882 documents affordable, and a
	// per-turn round trip over the pipe would throw that away.
	links := []any{}
	for start := 0; start < len(texts); start += locomoBatchSize {
		end := min(start+locomoBatchSize, len(texts))
		for offset, entities := range extractor.EntitiesBatch(texts[start:end]) {
			for _, entity := range entities {
				links = append(links, map[string]any{
					"entity": entity, "turn": turnIDs[start+offset],
				})
			}
		}
	}
	t.Logf("spaCy extracted from %d turns in %s", len(texts), time.Since(started).Round(time.Millisecond))
	for start := 0; start < len(links); start += locomoBatchSize {
		end := min(start+locomoBatchSize, len(links))
		if _, err := client.Write(ctx, linkEntitiesBatch,
			map[string]any{"links": links[start:end]}); err != nil {
			t.Fatalf("link %d-%d: %v", start, end, err)
		}
	}
	rows, err := client.Query(ctx, "SELECT count(*) AS n FROM "+entityType, nil)
	if err != nil {
		t.Fatalf("count entities: %v", err)
	}
	t.Logf("%d mentions over %d distinct entities in %s",
		len(links), rowInt(rows[0], "n"), time.Since(started).Round(time.Millisecond))
}

type candidate struct {
	diaID   string
	lexical float64
	entity  float64
	fused   float64
}

const entityMentionsQuery = `
MATCH (e:LocomoEntity {name: $entity})-[:MENTIONED_IN]->(t:LocomoTurn)
WHERE t.conversation = $conversation
RETURN t.dia_id AS dia_id
`

// entityBoosts is _compute_entity_boosts with the cosine replaced by an exact
// walk. Their similarity term is always 1.0 here -- the name either names the
// entity or it does not -- so what survives is the hub penalty, which is the
// part that actually shapes the ranking.
func entityBoosts(
	ctx context.Context,
	client *Client,
	entities []string,
	conversation string,
) map[string]float64 {
	boosts := map[string]float64{}
	for _, entity := range entities {
		rows, err := client.Read(ctx, entityMentionsQuery,
			map[string]any{"entity": entity, "conversation": conversation})
		if err != nil || len(rows) == 0 {
			continue
		}
		linked := max(len(rows), 1)
		boost := entityBoostWeight / (1.0 + hubPenaltyK*math.Pow(float64(linked-1), 2))
		for _, row := range rows {
			diaID := rowString(row, "dia_id")
			if boost > boosts[diaID] {
				boosts[diaID] = boost
			}
		}
	}
	return boosts
}

// conversationIndex is a real BM25 index over one conversation's turns, built to
// RE-SCORE the candidates ArcadeDB returned rather than to find them.
//
// It exists because ArcadeDB's $score is not BM25: measured on locomo-1, the
// single turn containing "sunrise" scores 1.000 while three turns matching the
// filler words "good great really" score 3.000. It counts matching terms, with no
// idf and no length normalisation, so a turn full of common words outranks the
// one turn that answers the question. Retrieval is left exactly as it was -- same
// top-60 -- so the pool ceiling is unchanged and any gain here is ranking alone.
type conversationIndex struct {
	index  *bm25.Index
	rowOf  map[string]int
	corpus []string
}

func buildConversationIndexes(t *testing.T, client *Client, conversations int) map[string]*conversationIndex {
	t.Helper()
	out := map[string]*conversationIndex{}
	for i := 1; i <= conversations; i++ {
		conversation := fmt.Sprintf("%s%d", locomoDocPrefix, i)
		rows, err := client.Query(context.Background(),
			"SELECT dia_id, text FROM "+locomoEnglishType+
				" WHERE conversation = :c LIMIT 5000", map[string]any{"c": conversation})
		if err != nil {
			t.Fatalf("read %s turns: %v", conversation, err)
		}
		entry := &conversationIndex{rowOf: map[string]int{}}
		for _, row := range rows {
			entry.rowOf[rowString(row, "dia_id")] = len(entry.corpus)
			entry.corpus = append(entry.corpus, rowString(row, "text"))
		}
		entry.index = bm25.New(entry.corpus)
		out[conversation] = entry
	}
	return out
}

// rescore replaces each candidate's ArcadeDB score with a real BM25 score,
// normalised through Mem0's sigmoid. The sigmoid parameters are Mem0's own here,
// not the rescaled ones: their midpoints assume genuine BM25 magnitudes, which is
// what this produces.
func (c *conversationIndex) rescore(query string, candidates map[string]float64) map[string]float64 {
	terms := bm25.Tokenize(query)
	midpoint, steepness := mem0BM25Params(query)
	out := make(map[string]float64, len(candidates))
	for diaID := range candidates {
		doc, ok := c.rowOf[diaID]
		if !ok {
			continue
		}
		out[diaID] = normalizeBM25(c.index.ScoreTerms(doc, terms), midpoint, steepness)
	}
	return out
}

// mem0BM25Params is mem0/utils/scoring.py verbatim, midpoints included, because
// this scorer produces the BM25 magnitudes those numbers were chosen for.
func mem0BM25Params(query string) (midpoint, steepness float64) {
	switch terms := len(strings.Fields(query)); {
	case terms <= 3:
		return 5.0, 0.7
	case terms <= 6:
		return 7.0, 0.6
	case terms <= 9:
		return 9.0, 0.5
	case terms <= 15:
		return 10.0, 0.5
	default:
		return 12.0, 0.5
	}
}

func lexicalCandidates(
	ctx context.Context,
	client *Client,
	query, conversation string,
) map[string]float64 {
	rows, err := client.Query(ctx,
		"SELECT dia_id, $score AS relevance FROM "+locomoEnglishType+
			" WHERE SEARCH_INDEX('"+locomoEnglishType+"[text]', :q)"+
			" AND conversation = :c LIMIT "+fmt.Sprint(overFetch),
		map[string]any{"q": escapeLucene(query), "c": conversation})
	if err != nil {
		return nil
	}
	midpoint, steepness := bm25Params(query)
	scores := map[string]float64{}
	for _, row := range rows {
		raw, _ := row["relevance"].(float64)
		scores[rowString(row, "dia_id")] = normalizeBM25(raw, midpoint, steepness)
	}
	return scores
}

// fuse is score_and_rank: additive, over an adaptive divisor. Without a semantic
// leg the divisor is 1.0 for lexical alone and 1.5 once entities contribute.
func fuse(lexical, boosts map[string]float64, union bool) []candidate {
	maxPossible := 1.0
	if len(boosts) > 0 {
		maxPossible += entityBoostWeight
	}
	pool := map[string]bool{}
	for diaID := range lexical {
		pool[diaID] = true
	}
	if union {
		for diaID := range boosts {
			pool[diaID] = true
		}
	}
	out := make([]candidate, 0, len(pool))
	for diaID := range pool {
		entry := candidate{diaID: diaID, lexical: lexical[diaID], entity: boosts[diaID]}
		entry.fused = math.Min((entry.lexical+entry.entity)/maxPossible, 1.0)
		out = append(out, entry)
	}
	// Ties broken by dia_id so a run is reproducible; map order is not.
	sort.Slice(out, func(i, j int) bool {
		if out[i].fused != out[j].fused {
			return out[i].fused > out[j].fused
		}
		return out[i].diaID < out[j].diaID
	})
	return out
}

func TestLocomoMem0Rerank(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	samples := loadLocomo(t)

	if rows, err := client.Query(ctx, "SELECT count(*) AS n FROM "+entityType, nil); err != nil ||
		len(rows) == 0 || rowInt(rows[0], "n") == 0 {
		t.Skip("entity layer not built; run TestLocomoMem0EntityLayerLoads first")
	}

	extractor := spacyOrSkip(t)

	// The ceiling first: how often is the evidence anywhere in the over-fetched
	// pool at all? Everything a re-ranker can do is bounded by this, and the gap
	// between it and R@10 is the only headroom re-ranking has.
	t.Run("pool ceiling", func(t *testing.T) {
		inPool, asked := 0, 0
		for index, sample := range samples {
			conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
			for _, qa := range sample.QA {
				if qa.Category == adversarialClass || len(qa.Evidence) == 0 {
					continue
				}
				asked++
				for diaID := range lexicalCandidates(ctx, client, qa.Question, conversation) {
					if slices.Contains(qa.Evidence, diaID) {
						inPool++
						break
					}
				}
			}
		}
		t.Logf("evidence present in the lexical top-%d pool: %.1f%% (%d/%d)",
			overFetch, pct(inPool, asked), inPool, asked)
	})

	indexes := buildConversationIndexes(t, client, len(samples))
	variants := []struct {
		name    string
		union   bool
		rescore bool
	}{
		{"A rerank-only (Mem0-faithful)", false, false},
		{"B union (entities may recover)", true, false},
		{"C real BM25 rescoring of the same pool", false, true},
		{"D real BM25 + union", true, true},
	}
	for _, variant := range variants {
		overall := &recallScore{}
		byCategory := map[int]*recallScore{}
		started := time.Now()
		searched := 0
		entityHits := 0

		for index, sample := range samples {
			conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
			for _, qa := range sample.QA {
				if qa.Category == adversarialClass || len(qa.Evidence) == 0 {
					continue
				}
				lexical := lexicalCandidates(ctx, client, qa.Question, conversation)
				if variant.rescore {
					lexical = indexes[conversation].rescore(qa.Question, lexical)
				}
				boosts := entityBoosts(ctx, client, extractor.Entities(qa.Question), conversation)
				if len(boosts) > 0 {
					entityHits++
				}
				ranked := fuse(lexical, boosts, variant.union)
				searched++

				rank := -1
				for position, entry := range ranked {
					if position >= 10 {
						break
					}
					if slices.Contains(qa.Evidence, entry.diaID) {
						rank = position
						break
					}
				}
				overall.add(rank)
				if byCategory[qa.Category] == nil {
					byCategory[qa.Category] = &recallScore{}
				}
				byCategory[qa.Category].add(rank)
			}
		}

		t.Logf("── %s", variant.name)
		t.Logf("   %d questions, %d with a matched entity, %s (%.1f ms each)",
			searched, entityHits, time.Since(started).Round(time.Second),
			float64(time.Since(started).Milliseconds())/float64(max(searched, 1)))
		categories := []int{}
		for category := range byCategory {
			categories = append(categories, category)
		}
		sort.Ints(categories)
		for _, category := range categories {
			score := byCategory[category]
			t.Logf("   %-14s %6d %7.1f%% %7.1f%% %7.1f%%", locomoCategories[category],
				score.asked, pct(score.hit1, score.asked),
				pct(score.hit5, score.asked), pct(score.hit10, score.asked))
		}
		t.Logf("   %-14s %6d %7.1f%% %7.1f%% %7.1f%%   (baseline 20.8 / 36.0 / 43.8)",
			"OVERALL", overall.asked, pct(overall.hit1, overall.asked),
			pct(overall.hit5, overall.asked), pct(overall.hit10, overall.asked))
	}
}
