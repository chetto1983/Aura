package llm

// Where a model's own sampling defaults come from.
//
// A model card asks for a specific sampling set — Gemma wants temperature 1.0 / top_k 64
// / top_p 0.95, Qwen3 wants 0.6 / 20 / 0.95 — and getting it wrong degrades output in a
// way no error reports. Aura used to answer this with one built-in temperature applied to
// every model, which is a pin dressed as a default: it silently overrode whatever the
// serving model asked for.
//
// It does not have to be guessed. All three backends publish what they will use, and all
// three publish it on a call the model profile already makes:
//
//	llama.cpp   GET /props   -> default_generation_settings.params (JSON numbers)
//	Ollama      POST /api/show -> parameters (Modelfile TEXT, one "name value" per line)
//	OpenRouter  GET /models  -> data[].default_parameters (JSON numbers)
//
// Measured 2026-09-03 on the live stack. llama-server's /props reports the RESOLVED set —
// GGUF metadata (general.sampling.*), command-line flags and built-ins already merged —
// so it is the answer, not one input to it. Ollama's field is a text block in Modelfile
// syntax, not JSON, and repeats keys (two `stop` lines), so it needs a line parser.
// OpenRouter populates default_parameters for 264 of 424 models; the rest publish none
// and must keep whatever the operator or the built-ins supply.
//
// Every parser here returns ONLY what the backend actually stated. An absent knob stays
// nil, because "the provider did not say" and "the provider said zero" are different
// facts and min_p=0.0 is a real instruction on some cards.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

// samplingDefaults is one backend's published set, in canonical Aura names. The
// backends disagree on spelling (repeat_penalty vs repetition_penalty), so each parser
// normalises on the way in and nothing downstream has to know which server answered.
type samplingDefaults struct {
	Temperature *float64
	Sampling    Sampling
}

// empty reports whether the backend published nothing usable, which is the common case
// for an OpenRouter model with no default_parameters and for any probe that failed.
func (d samplingDefaults) empty() bool {
	return d.Temperature == nil && d.Sampling.TopP == nil && d.Sampling.TopK == nil &&
		d.Sampling.MinP == nil && d.Sampling.PresencePenalty == nil &&
		d.Sampling.RepetitionPenalty == nil
}

// samplingDefaultsFromNumbers builds the set from a name→value map. Both JSON sources
// reduce to this; only Ollama needs its own tokenizer first.
//
// The two penalty spellings map to the same field: llama.cpp and Ollama say
// repeat_penalty, OpenRouter says repetition_penalty, and they mean the knob Aura calls
// RepetitionPenalty.
func samplingDefaultsFromNumbers(values map[string]float64) samplingDefaults {
	var out samplingDefaults
	assign := func(name string, set func(float64)) {
		if v, ok := values[name]; ok {
			set(v)
		}
	}
	assign("temperature", func(v float64) { out.Temperature = &v })
	assign("top_p", func(v float64) { out.Sampling.TopP = &v })
	assign("min_p", func(v float64) { out.Sampling.MinP = &v })
	assign("presence_penalty", func(v float64) { out.Sampling.PresencePenalty = &v })
	assign("top_k", func(v float64) { k := int(v); out.Sampling.TopK = &k })
	for _, name := range []string{"repeat_penalty", "repetition_penalty"} {
		assign(name, func(v float64) { out.Sampling.RepetitionPenalty = &v })
	}
	return out
}

// parseOllamaSamplingParameters reads the /api/show `parameters` block, which is
// Modelfile text rather than JSON:
//
//	repeat_penalty                 1
//	temperature                    0.6
//	top_k                          20
//
// Values that are not numbers (`stop "<|im_end|>"`, which appears twice) are skipped
// rather than rejected: the block legitimately carries non-sampling directives, and one
// unparseable line must not discard the whole set.
func parseOllamaSamplingParameters(block string) samplingDefaults {
	values := make(map[string]float64, 8)
	for line := range strings.SplitSeq(block, "\n") {
		name, raw, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			continue
		}
		values[name] = value
	}
	return samplingDefaultsFromNumbers(values)
}

// applyDiscoveredSampling fills the knobs the operator left unset with what the backend
// published, and touches nothing else.
//
// An explicitly configured knob always wins: the operator who typed a number is making a
// statement about THIS deployment, while the published default describes the model in
// general. The direction matters at a model change — the previous model's discovered
// default must not survive into the new one, which is why the caller re-resolves rather
// than merging into whatever the last profile left behind.
func applyDiscoveredSampling(cfg *Config, discovered samplingDefaults) {
	if cfg == nil || discovered.empty() {
		return
	}
	if !cfg.TemperatureConfigured && discovered.Temperature != nil {
		cfg.Temperature = *discovered.Temperature
	}
	if cfg.Sampling.TopP == nil {
		cfg.Sampling.TopP = discovered.Sampling.TopP
	}
	if cfg.Sampling.TopK == nil {
		cfg.Sampling.TopK = discovered.Sampling.TopK
	}
	if cfg.Sampling.MinP == nil {
		cfg.Sampling.MinP = discovered.Sampling.MinP
	}
	if cfg.Sampling.PresencePenalty == nil {
		cfg.Sampling.PresencePenalty = discovered.Sampling.PresencePenalty
	}
	if cfg.Sampling.RepetitionPenalty == nil {
		cfg.Sampling.RepetitionPenalty = discovered.Sampling.RepetitionPenalty
	}
}

// llamaCppPublishedSampling reads the local server's resolved sampling set. It is a
// SECOND call — /models carries the context window, /props carries the sampler — and it
// is best-effort for the same reason the reasoning probe is: a local server that is slow,
// old or absent must degrade to "the backend stated nothing", never fail the profile.
func llamaCppPublishedSampling(ctx context.Context, client *http.Client, baseURL string) samplingDefaults {
	props, err := fetchLlamaCppProps(ctx, Config{BaseURL: baseURL}, client)
	if err != nil {
		return samplingDefaults{}
	}
	return samplingDefaultsFromNumbers(props.DefaultGenerationSettings.Params)
}
