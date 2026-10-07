package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	frozenEvalSetPath   = "testdata/turn_recall_frozen_2026-10-07.json"
	frozenEvalProtocol  = "../../docs/verification/turn-recall-frozen-eval.md"
	frozenEvalHashLabel = "Dataset SHA-256: `"
)

type evalTurn struct {
	ID         string   `json:"id"`
	Family     string   `json:"family"`
	Text       string   `json:"text"`
	Accepted   []string `json:"accepted"`
	Attachment bool     `json:"attachment,omitempty"`
}

type evalConversation struct {
	ID    string     `json:"id"`
	Split string     `json:"split"`
	Lang  string     `json:"lang"`
	Turns []evalTurn `json:"turns"`
}

type evalSet struct {
	FrozenOn      string             `json:"frozen_on"`
	Rubric        map[string]string  `json:"rubric"`
	Conversations []evalConversation `json:"conversations"`
}

func (t evalTurn) hard() bool { return slices.Equal(t.Accepted, []string{"high"}) }

func loadFrozenEvalSet(t *testing.T) (evalSet, []byte) {
	t.Helper()
	raw, err := os.ReadFile(frozenEvalSetPath)
	if err != nil {
		t.Fatalf("read the frozen set: %v", err)
	}
	var set evalSet
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatalf("decode the frozen set: %v", err)
	}
	return set, raw
}

func TestFrozenEvalSetIsWellFormed(t *testing.T) {
	set, _ := loadFrozenEvalSet(t)
	ids := map[string]bool{}
	familySplit := map[string]string{}
	langs := map[string]map[string]bool{"calibration": {}, "final": {}}
	turns, hard := 0, 0
	for _, conv := range set.Conversations {
		if conv.Split != "calibration" && conv.Split != "final" {
			t.Errorf("%s: split %q", conv.ID, conv.Split)
		}
		langs[conv.Split][conv.Lang] = true
		for _, turn := range conv.Turns {
			turns++
			if ids[turn.ID] {
				t.Errorf("duplicate id %s", turn.ID)
			}
			ids[turn.ID] = true
			if split, seen := familySplit[turn.Family]; seen && split != conv.Split {
				t.Errorf("family %s is in both splits", turn.Family)
			}
			familySplit[turn.Family] = conv.Split
			if strings.TrimSpace(turn.Text) == "" || len(turn.Accepted) == 0 {
				t.Errorf("%s: empty text or accepted set", turn.ID)
			}
			for _, effort := range turn.Accepted {
				if _, ok := set.Rubric[effort]; !ok {
					t.Errorf("%s: accepted %q is not in the rubric", turn.ID, effort)
				}
			}
			if turn.hard() {
				hard++
			}
		}
	}
	for split, seen := range langs {
		if !seen["it"] || !seen["en"] {
			t.Errorf("split %s lacks a language: %v", split, seen)
		}
	}
	if turns != 51 || hard != 13 {
		t.Errorf("turns = %d, hard = %d; the frozen set has 51 and 13", turns, hard)
	}
}

// The set measures what the seed bank does NOT already contain: no turn may be a copy of a
// seed, a router prompt line or a gate case in the prompt package.
func TestFrozenEvalSetDoesNotCopyTheSeedBank(t *testing.T) {
	set, _ := loadFrozenEvalSet(t)
	sources, err := filepath.Glob("prompt/*.go")
	if err != nil || len(sources) == 0 {
		t.Fatalf("prompt sources: %v (%d files)", err, len(sources))
	}
	var corpus strings.Builder
	for _, path := range sources {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		corpus.Write(raw)
	}
	text := strings.ToLower(corpus.String())
	for _, conv := range set.Conversations {
		for _, turn := range conv.Turns {
			if len(turn.Text) > 3 && strings.Contains(text, strings.ToLower(turn.Text)) {
				t.Errorf("%s copies the prompt package: %q", turn.ID, turn.Text)
			}
		}
	}
}

// The protocol names the set's digest, so an edit to the set without a new protocol entry
// fails here, not in a report nobody re-reads.
func TestFrozenEvalSetMatchesItsProtocol(t *testing.T) {
	_, raw := loadFrozenEvalSet(t)
	protocol, err := os.ReadFile(frozenEvalProtocol)
	if err != nil {
		t.Fatalf("read the protocol: %v", err)
	}
	sum := sha256.Sum256(raw)
	if want := frozenEvalHashLabel + hex.EncodeToString(sum[:]) + "`"; !strings.Contains(string(protocol), want) {
		t.Fatalf("the protocol does not name the set's digest %s", hex.EncodeToString(sum[:]))
	}
}

// wilson is the 95% Wilson score interval for successes out of n.
func wilson(successes, n int) (float64, float64) {
	if n == 0 {
		return 0, 0
	}
	const z = 1.959963984540054
	p, total := float64(successes)/float64(n), float64(n)
	denominator := 1 + z*z/total
	center := (p + z*z/(2*total)) / denominator
	half := z * math.Sqrt(p*(1-p)/total+z*z/(4*total*total)) / denominator
	return center - half, center + half
}

// percentile is the nearest-rank percentile of values, q in (0, 1].
func percentile(values []time.Duration, q float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted[max(int(math.Ceil(q*float64(len(sorted))))-1, 0)]
}

func TestEvalStatistics(t *testing.T) {
	if lo, hi := wilson(8, 10); math.Abs(lo-0.4902) > 0.0005 || math.Abs(hi-0.9433) > 0.0005 {
		t.Errorf("wilson(8, 10) = (%.4f, %.4f), want (0.4902, 0.9433)", lo, hi)
	}
	if lo, hi := wilson(0, 0); lo != 0 || hi != 0 {
		t.Errorf("wilson(0, 0) = (%v, %v)", lo, hi)
	}
	var values []time.Duration
	for ms := 10; ms >= 1; ms-- {
		values = append(values, time.Duration(ms)*time.Millisecond)
	}
	if p50, p95 := percentile(values, 0.50), percentile(values, 0.95); p50 != 5*time.Millisecond || p95 != 10*time.Millisecond {
		t.Errorf("p50 = %v, p95 = %v; want 5ms and 10ms", p50, p95)
	}
	if percentile(nil, 0.5) != 0 {
		t.Error("percentile of nothing is not zero")
	}
}
