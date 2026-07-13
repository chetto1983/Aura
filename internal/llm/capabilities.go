package llm

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// ErrCompactionCapabilityUnavailable identifies a fail-closed preflight rejection.
var ErrCompactionCapabilityUnavailable = errors.New("llm: compaction capability unavailable")

// ToolOrdering describes the adapter's required tool-cycle ordering.
type ToolOrdering string

// ToolOrderingCallThenResult requires each result to follow a declared call.
const ToolOrderingCallThenResult ToolOrdering = "call_then_result"

// EstimatorCapability records the tokenizer or conservative estimator contract.
type EstimatorCapability struct {
	ID, Version            string
	ProviderTokenizer      bool
	DeclaredErrorTokens    int
	ConservativeMultiplier float64
	ConservativeAddend     int
}

// StructuredOutputCapability describes machine-validatable response support.
type StructuredOutputCapability struct{ JSON, Schema bool }

// InternalContextMapping describes the safe non-authoritative history envelope.
type InternalContextMapping struct {
	Role, EnvelopeVersion string
	Authoritative         bool
}

// ModalityCapability lists request parts accepted by the adapter.
type ModalityCapability struct{ Text, Image, Audio, File, ReferenceOnly bool }

// UsageCapability lists usage fields returned by the adapter.
type UsageCapability struct{ InputTokens, OutputTokens, CachedTokens, Cost bool }

// NativeCompactionCapability records native support and Aura metadata parity.
type NativeCompactionCapability struct{ Supported, AuraManifestMetadata bool }

// RetentionCapability records request storage and ZDR behavior.
type RetentionCapability struct {
	RequestStorage    bool
	ZeroDataRetention bool
	Policy            string
}

// ProviderCapability is the versioned activation contract for one transport adapter.
// Unknown and incomplete rows remain usable for ordinary requests but fail compaction preflight.
type ProviderCapability struct {
	ContractVersion                   string
	Provider, Model                   string
	WindowTokens, MaximumOutputTokens int
	Estimator                         EstimatorCapability
	StructuredOutput                  StructuredOutputCapability
	InternalContext                   InternalContextMapping
	Modalities                        ModalityCapability
	ToolOrdering                      ToolOrdering
	Usage                             UsageCapability
	NativeCompaction                  NativeCompactionCapability
	Retention                         RetentionCapability
}

// CapabilityFor resolves and validates the exhaustive current adapter row.
func CapabilityFor(cfg Config) (ProviderCapability, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "openai_compat"
	}
	if cfg.ContextWindow <= 0 || cfg.MaxOutputTokens < 0 {
		return ProviderCapability{}, fmt.Errorf("%w: invalid window/output", ErrCompactionCapabilityUnavailable)
	}
	base := ProviderCapability{ContractVersion: "aura.provider-capability.v1", Provider: provider, Model: cfg.Model, WindowTokens: cfg.ContextWindow, MaximumOutputTokens: cfg.MaxOutputTokens, StructuredOutput: StructuredOutputCapability{JSON: true, Schema: true}, InternalContext: InternalContextMapping{Role: RoleSystem, EnvelopeVersion: "aura.internal-context.v1"}, Modalities: ModalityCapability{Text: true, ReferenceOnly: true}, ToolOrdering: ToolOrderingCallThenResult, Usage: UsageCapability{InputTokens: true, OutputTokens: true, CachedTokens: true, Cost: true}, Retention: RetentionCapability{RequestStorage: false, ZeroDataRetention: true, Policy: "request_denied"}}
	switch provider {
	case "openrouter":
		base.Estimator = EstimatorCapability{ID: "cl100k_upper_bound", Version: "1", ConservativeMultiplier: 1.15, ConservativeAddend: 256}
		base.Modalities.Image = true
		base.Retention.Policy = "provider.data_collection=deny"
	case "llamacpp":
		base.Estimator = EstimatorCapability{ID: "llamacpp_declared_tokenizer", Version: "1", ProviderTokenizer: true}
		base.Retention = RetentionCapability{RequestStorage: false, ZeroDataRetention: true, Policy: "local_process"}
	case "vllm", "openai_compat", "openai-compatible", "generic":
		base.Estimator = EstimatorCapability{ID: "cl100k_upper_bound", Version: "1", ConservativeMultiplier: 1.15, ConservativeAddend: 256}
		base.Retention = RetentionCapability{RequestStorage: false, ZeroDataRetention: false, Policy: "operator_declared"}
	default:
		return ProviderCapability{}, fmt.Errorf("%w: provider %q", ErrCompactionCapabilityUnavailable, provider)
	}
	if err := base.ValidateForCompaction(); err != nil {
		return ProviderCapability{}, err
	}
	return base, nil
}

// ValidateForCompaction proves the row satisfies activation preflight.
func (c ProviderCapability) ValidateForCompaction() error {
	if c.ContractVersion == "" || c.Provider == "" || c.WindowTokens <= 0 || c.MaximumOutputTokens < 0 || c.Estimator.ID == "" || c.Estimator.Version == "" || !c.StructuredOutput.Schema || c.InternalContext.EnvelopeVersion == "" || c.InternalContext.Authoritative || !c.Modalities.Text || c.ToolOrdering != ToolOrderingCallThenResult || !c.Usage.InputTokens || !c.Usage.OutputTokens || c.Retention.Policy == "" {
		return fmt.Errorf("%w: incomplete capability row", ErrCompactionCapabilityUnavailable)
	}
	return nil
}

// ConservativeTokenUpperBound applies the mandated cold-start estimate bound.
func ConservativeTokenUpperBound(estimate int) int {
	if estimate <= 0 {
		return 256
	}
	return int(math.Ceil(float64(estimate)*1.15)) + 256
}
