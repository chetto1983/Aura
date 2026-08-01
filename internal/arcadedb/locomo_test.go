//go:build arcadedb_integration

// LOCOMO, scored the half of it that can be scored for free.
//
// The published leaderboard (Backboard 90.0, Zep 75.1, Mem0 66.9) is ANSWER
// accuracy judged by GPT-4.1: it needs a paid key and it measures the whole
// pipeline, extraction and generation included. Neither of those is built here.
//
// What is built is retrieval, and LOCOMO ships the ground truth to score it: every
// question carries `evidence`, the dialog ids that contain its answer. So this
// measures recall@k -- did the search return the turn the answer lives in -- which
// is not the same number as the leaderboard's and is not comparable to it, but is
// the CEILING on it. Nothing downstream can answer from a turn it never saw.
//
// The 446 category-5 questions are adversarial (deliberately unanswerable) and
// carry no usable evidence, so the scored set is the other 1540.
//
// Run:
//
//	ARCADEDB_URL=http://127.0.0.1:2480 ARCADEDB_DATABASE=aura_memory \
//	ARCADEDB_PASSWORD=… AURA_LOCOMO_DIR=/mnt/d/tmp/Backboard-Locomo-Benchmark \
//	go test -tags arcadedb_integration -run Locomo -v -timeout 60m ./internal/arcadedb/
package arcadedb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	locomoDocPrefix  = "locomo-"
	locomoBatchSize  = 400
	adversarialClass = 5
)

// locomoCategories are the labels the leaderboard reports. Read off the data
// rather than assumed: see TestLocomoCategoriesAreWhatWeThink.
var locomoCategories = map[int]string{
	1: "multi-hop",
	2: "temporal",
	3: "open-domain",
	4: "single-hop",
	5: "adversarial",
}

type locomoTurn struct {
	Speaker string `json:"speaker"`
	DiaID   string `json:"dia_id"`
	Text    string `json:"text"`
}

type locomoQA struct {
	Question string   `json:"question"`
	Answer   any      `json:"answer"`
	Evidence []string `json:"evidence"`
	Category int      `json:"category"`
}

type locomoSample struct {
	QA           []locomoQA     `json:"qa"`
	Conversation map[string]any `json:"conversation"`
	SampleID     string         `json:"sample_id"`
}

func loadLocomo(t *testing.T) []locomoSample {
	t.Helper()
	dir := envOr("AURA_LOCOMO_DIR", "/mnt/d/tmp/Backboard-Locomo-Benchmark")
	raw, err := os.ReadFile(filepath.Join(dir, "locomo_dataset.json"))
	if err != nil {
		t.Skipf("LOCOMO dataset unavailable: %v", err)
	}
	var samples []locomoSample
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatalf("decode LOCOMO: %v", err)
	}
	return samples
}

// turnsOf flattens a conversation's sessions in order. The sessions are keys
// `session_1`, `session_2`, … alongside their `_date_time` twins, so they are
// selected by shape rather than by iterating the map, whose order is undefined.
func turnsOf(sample locomoSample) []locomoTurn {
	turns := []locomoTurn{}
	for index := 1; ; index++ {
		value, ok := sample.Conversation[fmt.Sprintf("session_%d", index)]
		if !ok {
			// Sessions are not guaranteed contiguous; stop only after a gap of two.
			if _, next := sample.Conversation[fmt.Sprintf("session_%d", index+1)]; !next {
				break
			}
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}
		var session []locomoTurn
		if json.Unmarshal(encoded, &session) != nil {
			continue
		}
		turns = append(turns, session...)
	}
	return turns
}

// locomoIndex maps a stored passage back to the dialog id LOCOMO scores against.
type locomoIndex map[string]string // "docID#ordinal" -> dia_id

func indexKey(documentID string, ordinal int) string {
	return fmt.Sprintf("%s#%d", documentID, ordinal)
}

// writeTurns stores one conversation, a passage per turn, in UNWIND batches.
// One INSERT per turn would be four thousand round trips.
const locomoWriteBatch = `
UNWIND $chunks AS chunk
CREATE (c:Chunk)
SET c.id = chunk.id, c.document_id = chunk.document_id,
    c.chunk_index = chunk.chunk_index, c.text = chunk.text,
    c.file_name = chunk.file_name, c.active = true
RETURN count(c) AS written
`

func TestLocomoLoadsEveryConversation(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	samples := loadLocomo(t)

	started := time.Now()
	total := 0
	for index, sample := range samples {
		documentID := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
		if _, err := client.DeleteDocument(ctx, documentID); err != nil {
			t.Fatalf("clear %s: %v", documentID, err)
		}
		turns := turnsOf(sample)
		if len(turns) == 0 {
			t.Fatalf("%s yielded no turns", documentID)
		}
		if _, err := client.Command(ctx,
			"INSERT INTO Document SET id = :id, title = :title, file_name = :file,"+
				" source_kind = 'locomo', chunk_count = :n, status = 'searchable', active = true",
			map[string]any{
				"id": documentID, "title": "LOCOMO conversation " + sample.SampleID,
				"file": documentID + ".json", "n": len(turns),
			}); err != nil {
			t.Fatalf("insert %s: %v", documentID, err)
		}

		chunks := make([]any, 0, len(turns))
		for ordinal, turn := range turns {
			chunks = append(chunks, map[string]any{
				"id":          documentID + "#" + turn.DiaID,
				"document_id": documentID,
				"chunk_index": ordinal,
				"file_name":   documentID + ".json",
				"text":        turn.Speaker + ": " + turn.Text,
			})
		}
		for start := 0; start < len(chunks); start += locomoBatchSize {
			end := min(start+locomoBatchSize, len(chunks))
			if _, err := client.Write(ctx, locomoWriteBatch,
				map[string]any{"chunks": chunks[start:end]}); err != nil {
				t.Fatalf("write %s turns %d-%d: %v", documentID, start, end, err)
			}
		}
		total += len(turns)
		t.Logf("%-11s %4d turns, %3d questions", documentID, len(turns), len(sample.QA))
	}
	t.Logf("TOTAL: %d conversations, %d turns, %s",
		len(samples), total, time.Since(started).Round(time.Millisecond))
}

// buildIndex reads the stored passages back so a search hit, which reports
// document and ordinal, can be turned into the dia_id LOCOMO scores on.
func buildIndex(t *testing.T, client *Client, conversations int) locomoIndex {
	t.Helper()
	index := locomoIndex{}
	for i := 1; i <= conversations; i++ {
		documentID := fmt.Sprintf("%s%d", locomoDocPrefix, i)
		rows, err := client.Query(context.Background(),
			"SELECT id, chunk_index FROM Chunk WHERE document_id = :d LIMIT 5000",
			map[string]any{"d": documentID})
		if err != nil {
			t.Fatalf("index %s: %v", documentID, err)
		}
		for _, row := range rows {
			id := rowString(row, "id")
			if _, diaID, found := strings.Cut(id, "#"); found {
				index[indexKey(documentID, int(rowInt(row, "chunk_index")))] = diaID
			}
		}
	}
	if len(index) == 0 {
		t.Skip("no LOCOMO passages stored; run TestLocomoLoadsEveryConversation first")
	}
	return index
}

type recallScore struct{ hit1, hit5, hit10, asked int }

func (s *recallScore) add(rank int) {
	s.asked++
	if rank < 0 {
		return
	}
	if rank == 0 {
		s.hit1++
	}
	if rank < 5 {
		s.hit5++
	}
	s.hit10++
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return 100 * float64(n) / float64(d)
}

func TestLocomoRetrievalRecall(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	samples := loadLocomo(t)
	index := buildIndex(t, client, len(samples))

	overall := &recallScore{}
	byCategory := map[int]*recallScore{}
	started := time.Now()
	searches := 0

	for i, sample := range samples {
		documentID := fmt.Sprintf("%s%d", locomoDocPrefix, i+1)
		for _, qa := range sample.QA {
			if qa.Category == adversarialClass || len(qa.Evidence) == 0 {
				continue
			}
			passages, err := client.SearchPassages(ctx, qa.Question, 10, false)
			if err != nil {
				// A question that is all stopwords tokenises to nothing; that is a
				// miss, not a failure of the run.
				passages = nil
			}
			searches++
			rank := -1
			for position, passage := range passages {
				if passage.DocumentID != documentID {
					continue
				}
				diaID, ok := index[indexKey(passage.DocumentID, passage.Ordinal)]
				if ok && slices.Contains(qa.Evidence, diaID) {
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

	elapsed := time.Since(started)
	t.Logf("%d questions scored in %s (%.1f ms/search)",
		searches, elapsed.Round(time.Second), float64(elapsed.Milliseconds())/float64(max(searches, 1)))
	t.Logf("%-14s %7s %8s %8s %8s", "category", "asked", "R@1", "R@5", "R@10")
	categories := []int{}
	for category := range byCategory {
		categories = append(categories, category)
	}
	sort.Ints(categories)
	for _, category := range categories {
		score := byCategory[category]
		t.Logf("%-14s %7d %7.1f%% %7.1f%% %7.1f%%", locomoCategories[category],
			score.asked, pct(score.hit1, score.asked),
			pct(score.hit5, score.asked), pct(score.hit10, score.asked))
	}
	t.Logf("%-14s %7d %7.1f%% %7.1f%% %7.1f%%", "OVERALL", overall.asked,
		pct(overall.hit1, overall.asked), pct(overall.hit5, overall.asked),
		pct(overall.hit10, overall.asked))

	if overall.asked < 1000 {
		t.Fatalf("scored only %d questions, expected ~1540", overall.asked)
	}
}
