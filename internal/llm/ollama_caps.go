package llm

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ollamaThinkingCapability is the name Ollama publishes in POST /api/show `capabilities`
// for a model that can produce reasoning content. Measured on Ollama 0.33.2, 2026-08-31:
//
//	gemma4:31b-cloud -> ["completion","thinking","tools","vision"]
//	qwen3:0.6b       -> ["completion","tools","thinking"]
//
// The same endpoint and the same field already back the vision probe in
// model_content_caps.go (ollamaModalities, amendment #197) — the reasoning side simply
// never read it.
const ollamaThinkingCapability = "thinking"

// ollamaReasoningEffortSet is the effort vocabulary Ollama's OpenAI-compatible
// /v1/chat/completions accepts (openai_compat/request.go's ollamaReasoningEffort maps
// Aura's seven symbols onto it). The endpoint publishes no PER-MODEL effort metadata, so
// this stays a constant — but it applies only to a model that can think at all.
func ollamaReasoningEffortSet() []ReasoningEffort {
	return []ReasoningEffort{
		ReasoningEffortNone,
		ReasoningEffortLow,
		ReasoningEffortMedium,
		ReasoningEffortHigh,
	}
}

// ollamaReasoningCaps answers the allowed effort set for the ACTIVE Ollama model, read
// from that model rather than assumed.
//
// It used to be a bare struct returning {none,low,medium,high} unconditionally, for every
// model, which made the adaptive router provider-aware in the wrong direction: OpenRouter
// resolves its set from /models and llama.cpp narrows its own with /props, while Ollama —
// the one backend that publishes a per-model capability list Aura ALREADY fetches for
// vision — was answering from a constant. A model with no thinking capability was still
// advertised as supporting three graduated levels, so the composer offered them and the
// router could select one the model cannot honour.
//
// The probe is cached for ttl and is FAIL-OPEN: an unreachable daemon, an unpulled model
// or a malformed response falls back to the full set rather than reporting a model as
// non-thinking. Silently disabling reasoning on a transient probe failure would be a
// worse error than the one this fixes, and the fallback is exactly the previous behaviour.
type ollamaReasoningCaps struct {
	cfg        Config
	httpClient *http.Client
	ttl        time.Duration
	now        func() time.Time

	mu       sync.Mutex
	probed   bool
	probedAt time.Time
	efforts  []ReasoningEffort
}

var _ ReasoningCapabilitySource = (*ollamaReasoningCaps)(nil)

func newOllamaReasoningCaps(cfg Config) *ollamaReasoningCaps {
	connect := time.Duration(cfg.ConnectTimeoutSec) * time.Second
	if connect <= 0 {
		connect = 2 * time.Second
	}
	return &ollamaReasoningCaps{
		cfg: cfg,
		ttl: time.Minute,
		now: time.Now,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				DialContext:       (&net.Dialer{Timeout: connect}).DialContext,
				DisableKeepAlives: true,
			},
		},
	}
}

// AllowedEfforts reports the set this model can actually honour. detected is always true:
// either the probe answered, or the fail-open fallback reproduces the previous contract —
// in both cases the set is one the caller may gate a fixed level against.
func (s *ollamaReasoningCaps) AllowedEfforts(ctx context.Context) ([]ReasoningEffort, ReasoningEffort, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probed && s.now().Sub(s.probedAt) < s.ttl {
		return append([]ReasoningEffort(nil), s.efforts...), "", true
	}
	s.efforts = s.probeEfforts(ctx)
	s.probed = true
	s.probedAt = s.now()
	return append([]ReasoningEffort(nil), s.efforts...), "", true
}

func (s *ollamaReasoningCaps) probeEfforts(ctx context.Context) []ReasoningEffort {
	show, err := fetchOllamaShow(ctx, s.httpClient, s.cfg.BaseURL, s.cfg.Model)
	if err != nil {
		return ollamaReasoningEffortSet() // fail-open: a probe failure is not evidence of anything
	}
	for _, capability := range show.Capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), ollamaThinkingCapability) {
			return ollamaReasoningEffortSet()
		}
	}
	// The model answered and did NOT claim thinking. Offering it a graduated level would
	// be offering something it cannot do, so `none` is the whole truth.
	return []ReasoningEffort{ReasoningEffortNone}
}
