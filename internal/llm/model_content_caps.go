package llm

import (
	"context"
	"maps"
	"net"
	"net/http"
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

type llamaCppContentCaps struct {
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

var _ ContentCapabilitySource = (*llamaCppContentCaps)(nil)

func newLlamaCppContentCaps(cfg Config) *llamaCppContentCaps {
	connect := time.Duration(cfg.ConnectTimeoutSec) * time.Second
	if connect <= 0 {
		connect = 2 * time.Second
	}
	return &llamaCppContentCaps{
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

func (s *llamaCppContentCaps) ContentCapabilities(ctx context.Context) (ProviderContentCapabilities, bool) {
	if ReasoningTarget(s.cfg.Provider, s.cfg.BaseURL) != ReasoningTargetLlamaCpp {
		return ProviderContentCapabilities{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probed && s.now().Sub(s.probedAt) < s.ttl {
		return cloneContentCapabilities(s.caps), s.detected
	}
	props, err := fetchLlamaCppProps(ctx, s.cfg, s.httpClient)
	s.probed = true
	s.probedAt = s.now()
	if err != nil {
		s.caps = ProviderContentCapabilities{}
		s.detected = false
		return ProviderContentCapabilities{}, false
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
	s.caps = capabilitiesFromModalities(clampInputModalities(modalities))
	s.detected = true
	return cloneContentCapabilities(s.caps), true
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
	default:
		return nil
	}
}
