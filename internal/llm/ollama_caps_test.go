package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func ollamaShowServer(t *testing.T, capabilities []string, status int, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": capabilities})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ollamaCapsFor(t *testing.T, srv *httptest.Server) *ollamaReasoningCaps {
	t.Helper()
	return newOllamaReasoningCaps(Config{Provider: "ollama", BaseURL: srv.URL + "/v1", Model: "gemma4:31b-cloud"})
}

// The effort set must come from the MODEL, not from a constant. Ollama publishes a
// per-model capability list at /api/show — the same endpoint and field Aura already reads
// for vision — and it was the one backend answering from a hardcoded list while OpenRouter
// resolved its set from /models and llama.cpp narrowed its own with /props.
//
// Measured on Ollama 0.33.2, 2026-08-31: gemma4:31b-cloud publishes
// ["completion","thinking","tools","vision"], qwen3:0.6b publishes
// ["completion","tools","thinking"].
func TestOllamaReasoningCapsReadTheModelsOwnCapabilities(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		capabilities []string
		want         []ReasoningEffort
	}{
		{
			"a thinking model keeps the graduated set",
			[]string{"completion", "thinking", "tools", "vision"},
			[]ReasoningEffort{ReasoningEffortNone, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh},
		},
		{
			// The model answered and did not claim thinking. Offering three graduated
			// levels would be offering something it cannot do.
			"a model without thinking is offered none only",
			[]string{"completion", "tools"},
			[]ReasoningEffort{ReasoningEffortNone},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			caps := ollamaCapsFor(t, ollamaShowServer(t, test.capabilities, http.StatusOK, nil))
			efforts, deflt, detected := caps.AllowedEfforts(context.Background())
			if !detected || deflt != "" {
				t.Fatalf("AllowedEfforts = (_, %q, %v), want (_, empty, true)", deflt, detected)
			}
			if !slices.Equal(efforts, test.want) {
				t.Fatalf("efforts = %v, want %v", efforts, test.want)
			}
		})
	}
}

// A probe failure is not evidence that a model cannot think. Reporting one as non-thinking
// would silently disable reasoning on an unreachable daemon or an unpulled model, which is
// a worse error than the one the probe exists to fix — so it falls back to the full set,
// which is exactly the behaviour that shipped before the probe existed.
func TestOllamaReasoningCapsFailOpen(t *testing.T) {
	t.Parallel()
	caps := ollamaCapsFor(t, ollamaShowServer(t, nil, http.StatusInternalServerError, nil))
	efforts, _, detected := caps.AllowedEfforts(context.Background())
	want := []ReasoningEffort{ReasoningEffortNone, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}
	if !detected || !slices.Equal(efforts, want) {
		t.Fatalf("efforts = (%v,%v), want (%v,true) on a failed probe", efforts, detected, want)
	}
}

// The probe rides the request path, so it is cached: a second read inside the TTL must not
// dial the daemon again.
func TestOllamaReasoningCapsCacheTheProbe(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	caps := ollamaCapsFor(t, ollamaShowServer(t, []string{"completion", "thinking"}, http.StatusOK, &calls))
	for range 3 {
		caps.AllowedEfforts(context.Background())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("probed %d times, want 1 inside the TTL", got)
	}
	// Past the TTL it probes again — a model can be swapped under a running daemon.
	caps.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	caps.AllowedEfforts(context.Background())
	if got := calls.Load(); got != 2 {
		t.Fatalf("probed %d times, want 2 after the TTL elapsed", got)
	}
}
