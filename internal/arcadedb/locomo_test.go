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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
