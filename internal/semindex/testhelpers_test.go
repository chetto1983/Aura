package semindex

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// fakeEmbedder maps each text to a 3-dim one-hot by keyword so groups land on
// orthogonal axes (a=[1,0,0], b=[0,1,0], c=[0,0,1]). Copied verbatim from
// reasoning_classifier_test.go:15-37 — the calls counter is how downstream
// waves assert "exactly N new embeds" (Req-3) and the error surface (Req-6).
type fakeEmbedder struct {
	mu        sync.Mutex
	calls     int
	failUntil int  // return an error for the first failUntil calls
	failQuery bool // error specifically on a single-text (query) call
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failUntil {
		return nil, errors.New("embed sidecar down")
	}
	if f.failQuery && len(texts) == 1 {
		return nil, errors.New("query embed failed")
	}
	out := make([][]float64, len(texts))
	for i, t := range texts {
		out[i] = vecForGroup(t)
	}
	return out, nil
}

// vecForGroup is the synthetic one-hot mapping (mirror of vecFor): keyword →
// orthogonal axis so per-group centroids land on known points and margins are
// exact and golden.
func vecForGroup(text string) []float64 {
	l := strings.ToLower(text)
	hasAny := func(ks ...string) bool {
		for _, k := range ks {
			if strings.Contains(l, k) {
				return true
			}
		}
		return false
	}
	switch {
	case hasAny("weather", "forecast", "rain", "temp"):
		return []float64{0, 1, 0} // group "weather"
	case hasAny("code", "debug", "script", "refactor"):
		return []float64{0, 0, 1} // group "code"
	default:
		return []float64{1, 0, 0} // group "chat"
	}
}
