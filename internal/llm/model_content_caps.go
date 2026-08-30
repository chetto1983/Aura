package llm

import (
	"context"
	"maps"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type openRouterContentCaps struct {
	client *ModelCapabilityClient
	model  string
}

var _ ContentCapabilitySource = (*openRouterContentCaps)(nil)

func (s *openRouterContentCaps) ContentCapabilities(ctx context.Context) (ProviderContentCapabilities, bool) {
	capability, ok, err := s.client.ReasoningCapabilityFor(ctx, s.model)
	if err != nil || !ok {
		return ProviderContentCapabilities{}, false
	}
	return capabilitiesFromModalities(capability.InputModalities), true
}

// modalityProbe asks one local backend which native input modalities the active model
// accepts, in Aura's normalized names (the wire name "vision" is already "image").
type modalityProbe func(ctx context.Context, cfg Config, httpClient *http.Client) ([]string, error)

// probedContentCaps caches one modality probe of a local backend for ttl and answers
// text-only (detected=false) for any other target or for a failed probe.
type probedContentCaps struct {
	target     ReasoningTargetKind
	probe      modalityProbe
	cfg        Config
	httpClient *http.Client
	ttl        time.Duration
	now        func() time.Time

	mu       sync.Mutex
	probed   bool
	probedAt time.Time
	caps     ProviderContentCapabilities
	detected bool
}

var _ ContentCapabilitySource = (*probedContentCaps)(nil)

func newProbedContentCaps(cfg Config, target ReasoningTargetKind, probe modalityProbe) *probedContentCaps {
	connect := time.Duration(cfg.ConnectTimeoutSec) * time.Second
	if connect <= 0 {
		connect = 2 * time.Second
	}
	return &probedContentCaps{
		target: target,
		probe:  probe,
		cfg:    cfg,
		ttl:    time.Minute,
		now:    time.Now,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				DialContext:       (&net.Dialer{Timeout: connect}).DialContext,
				DisableKeepAlives: true,
			},
		},
	}
}

func newLlamaCppContentCaps(cfg Config) *probedContentCaps {
	return newProbedContentCaps(cfg, ReasoningTargetLlamaCpp, llamaCppModalities)
}

func newOllamaContentCaps(cfg Config) *probedContentCaps {
	return newProbedContentCaps(cfg, ReasoningTargetOllama, ollamaModalities)
}

func (s *probedContentCaps) ContentCapabilities(ctx context.Context) (ProviderContentCapabilities, bool) {
	if ReasoningTarget(s.cfg.Provider, s.cfg.BaseURL) != s.target {
		return ProviderContentCapabilities{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probed && s.now().Sub(s.probedAt) < s.ttl {
		return cloneContentCapabilities(s.caps), s.detected
	}
	modalities, err := s.probe(ctx, s.cfg, s.httpClient)
	s.probed = true
	s.probedAt = s.now()
	if err != nil {
		s.caps = ProviderContentCapabilities{}
		s.detected = false
		return ProviderContentCapabilities{}, false
	}
	s.caps = capabilitiesFromModalities(clampInputModalities(modalities))
	s.detected = true
	return cloneContentCapabilities(s.caps), true
}

// llamaCppModalities reads GET /props `modalities` ({"vision":true,"audio":false,...}).
func llamaCppModalities(ctx context.Context, cfg Config, httpClient *http.Client) ([]string, error) {
	props, err := fetchLlamaCppProps(ctx, cfg, httpClient)
	if err != nil {
		return nil, err
	}
	modalities := make([]string, 0, len(props.Modalities))
	for name, enabled := range props.Modalities {
		if !enabled {
			continue
		}
		if name == "vision" {
			name = "image"
		}
		modalities = append(modalities, name)
	}
	return modalities, nil
}

// ollamaModalities reads POST /api/show `capabilities` (["completion","tools","vision",...]
// — measured on Ollama 0.33.2 for gemma4:31b-cloud, amendment #197). Only "vision" names an
// input modality; the rest describe generation features and are not modalities.
func ollamaModalities(ctx context.Context, cfg Config, httpClient *http.Client) ([]string, error) {
	show, err := fetchOllamaShow(ctx, httpClient, cfg.BaseURL, cfg.Model)
	if err != nil {
		return nil, err
	}
	modalities := make([]string, 0, 1)
	for _, capability := range show.Capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), "vision") {
			modalities = append(modalities, "image")
		}
	}
	return modalities, nil
}

func capabilitiesFromModalities(modalities []string) ProviderContentCapabilities {
	out := ProviderContentCapabilities{Modalities: make(map[string]bool, len(modalities))}
	for _, modality := range modalities {
		out.Modalities[modality] = true
	}
	return out
}

func cloneContentCapabilities(in ProviderContentCapabilities) ProviderContentCapabilities {
	out := ProviderContentCapabilities{
		Modalities: make(map[string]bool, len(in.Modalities)),
		MIMETypes:  make(map[string]bool, len(in.MIMETypes)),
	}
	maps.Copy(out.Modalities, in.Modalities)
	maps.Copy(out.MIMETypes, in.MIMETypes)
	return out
}

// NewContentCapabilitySource selects live model-capability discovery for the active
// OpenAI-compatible backend. Unknown backends return nil and therefore stay text-only.
func NewContentCapabilitySource(cfg Config, ttl time.Duration) ContentCapabilitySource {
	switch ReasoningTarget(cfg.Provider, cfg.BaseURL) {
	case ReasoningTargetOpenRouter:
		return &openRouterContentCaps{
			client: NewModelCapabilityClient(cfg, ttl),
			model:  cfg.Model,
		}
	case ReasoningTargetLlamaCpp:
		source := newLlamaCppContentCaps(cfg)
		if ttl > 0 {
			source.ttl = ttl
		}
		return source
	case ReasoningTargetOllama:
		source := newOllamaContentCaps(cfg)
		if ttl > 0 {
			source.ttl = ttl
		}
		return source
	default:
		return nil
	}
}
