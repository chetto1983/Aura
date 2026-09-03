package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxPropsResponseBytes caps the local /props read. /props is small (chat template +
// a handful of capability flags), so 1 MiB is generous while still bounding a
// misbehaving local server (Threat T-37E-05 — /props is best-effort narrowing input).
const maxPropsResponseBytes = 1 << 20

// thinkingCapCandidates are the candidate chat_template_caps flag names that signal
// thinking/reasoning support. The EXACT field llama.cpp emits is undocumented and
// unconfirmed against a live spike-095 server (37E-RESEARCH.md P2.2 B, Wave-0 note),
// so the probe scans this small alias set and narrows ONLY on an explicit false — an
// unknown or absent flag must never narrow (the provider+ops-contract set is trusted).
var thinkingCapCandidates = []string{"supports_thinking", "thinking", "supports_reasoning", "reasoning"}

// llamaCppPropsResponse is the local llama-server GET /props DTO (official server
// README shape). Only ChatTemplateCaps is consulted today (best-effort thinking
// narrowing); ChatTemplate and Modalities are retained to pin the documented shape
// and for forward-compat narrowing without a contract change.
type llamaCppPropsResponse struct {
	ChatTemplate     string          `json:"chat_template"`
	ChatTemplateCaps map[string]any  `json:"chat_template_caps"`
	Modalities       map[string]bool `json:"modalities"`
	// The sampler the server will actually use: GGUF metadata (general.sampling.*),
	// command-line flags and built-ins already merged by llama-server. Measured
	// 2026-09-03 -- it reports Gemma's own temperature 1.0 / top_k 64, which are NOT
	// llama.cpp's built-in 0.8 / 40, so this is the model's card and not the server's.
	DefaultGenerationSettings struct {
		Params map[string]float64 `json:"params"`
	} `json:"default_generation_settings"`
}

// llamaCppReasoningCaps is the local-backend ReasoningCapabilitySource. It derives its
// set from EXPLICIT config (Provider=="llamacpp") plus the spike-095 ops contract
// (--jinja on, --reasoning-budget off → the full graduated set), which is authoritative
// (OQ-4 widen: the operator who sets provider=llamacpp is asserting that launch config).
// A best-effort GET /props can NARROW to off-only when the chat template advertises
// thinking disabled; /props is probed at most once (result cached — the local launch
// config is boot-stable, like the active model) so there is no per-turn network call.
type llamaCppReasoningCaps struct {
	cfg        Config
	httpClient *http.Client
	mu         sync.Mutex
	probed     bool
	narrowed   bool
}

var _ ReasoningCapabilitySource = (*llamaCppReasoningCaps)(nil)

// newLlamaCppReasoningCaps builds the source with a short-timeout HTTP client — /props
// is a localhost best-effort probe, so a slow/absent server must degrade fast to the
// provider+ops-contract set, never block a turn.
func newLlamaCppReasoningCaps(cfg Config) *llamaCppReasoningCaps {
	connect := time.Duration(cfg.ConnectTimeoutSec) * time.Second
	if connect <= 0 {
		connect = 2 * time.Second
	}
	return &llamaCppReasoningCaps{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				DialContext:       (&net.Dialer{Timeout: connect}).DialContext,
				DisableKeepAlives: true,
			},
		},
	}
}

// llamaCppGraduatedEfforts is the full spike-095 graduated set in off→max order (internal
// tokens). The `auto` symbol is NOT here — the endpoint/UI layer prepends it.
func llamaCppGraduatedEfforts() []ReasoningEffort {
	return []ReasoningEffort{
		ReasoningEffortNone, ReasoningEffortLow, ReasoningEffortMedium,
		ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax,
	}
}

// AllowedEfforts returns the local backend's set. An explicit llama.cpp provider is
// always detected=true (the operator asserts the spike-095 config); a best-effort
// /props probe (run once, then cached) narrows to {none} only when the chat template
// explicitly disables thinking. Anything unrecognized returns detected=false.
func (s *llamaCppReasoningCaps) AllowedEfforts(ctx context.Context) ([]ReasoningEffort, ReasoningEffort, bool) {
	if ReasoningTarget(s.cfg.Provider, s.cfg.BaseURL) != ReasoningTargetLlamaCpp {
		return nil, "", false
	}
	s.mu.Lock()
	if !s.probed {
		s.narrowed = s.probeThinkingDisabled(ctx)
		s.probed = true
	}
	narrowed := s.narrowed
	s.mu.Unlock()
	if narrowed {
		return []ReasoningEffort{ReasoningEffortNone}, "", true
	}
	return llamaCppGraduatedEfforts(), "", true
}

// probeThinkingDisabled makes a best-effort GET /props. On ANY failure (unreachable,
// non-2xx, malformed body) it returns false → trust the provider+ops-contract full
// set. It returns true ONLY when a known thinking flag is present AND explicitly false.
func (s *llamaCppReasoningCaps) probeThinkingDisabled(ctx context.Context) bool {
	props, err := s.fetchProps(ctx)
	if err != nil {
		return false
	}
	return thinkingExplicitlyDisabled(props.ChatTemplateCaps)
}

// thinkingExplicitlyDisabled reports whether chat_template_caps carries a known
// thinking/reasoning flag set to a literal false. Absent/unknown/non-bool → false
// (do not narrow), so a hostile or unfamiliar /props can never over-restrict.
func thinkingExplicitlyDisabled(caps map[string]any) bool {
	for _, name := range thinkingCapCandidates {
		if v, ok := caps[name]; ok {
			if b, isBool := v.(bool); isBool && !b {
				return true
			}
		}
	}
	return false
}

// fetchProps performs the GET /props round-trip and defensively parses it (body-size
// capped, json.Decoder). No auth header — /props on a local llama-server is unauthenticated.
func (s *llamaCppReasoningCaps) fetchProps(ctx context.Context) (llamaCppPropsResponse, error) {
	return fetchLlamaCppProps(ctx, s.cfg, s.httpClient)
}

func fetchLlamaCppProps(ctx context.Context, cfg Config, httpClient *http.Client) (llamaCppPropsResponse, error) {
	var out llamaCppPropsResponse
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/props", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return out, fmt.Errorf("llm: GET /props: unexpected status %d", resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxPropsResponseBytes))
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("llm: decode /props: %w", err)
	}
	return out, nil
}

// NewReasoningCapabilitySource selects the capability source for the active backend by
// llm.ReasoningTarget(cfg.Provider, cfg.BaseURL): OpenRouter → a TTL-cached /models
// source; llama.cpp → the provider+ops-contract source (best-effort /props narrowing);
// Ollama → the effort set narrowed by that model's own /api/show capabilities; anything else (e.g. a local
// vLLM/DGX endpoint) → nil, so the caller shows the safe
// floor {auto,off}. This is the boot seam the daemon composition root wires into the
// agui Server via SetReasoningCapabilitySource (plan 06).
func NewReasoningCapabilitySource(cfg Config, ttl time.Duration) ReasoningCapabilitySource {
	switch ReasoningTarget(cfg.Provider, cfg.BaseURL) {
	case ReasoningTargetOpenRouter:
		return &openRouterReasoningCaps{
			client: NewModelCapabilityClient(cfg, ttl),
			model:  cfg.Model,
		}
	case ReasoningTargetLlamaCpp:
		return newLlamaCppReasoningCaps(cfg)
	case ReasoningTargetOllama:
		return newOllamaReasoningCaps(cfg)
	default:
		return nil
	}
}
