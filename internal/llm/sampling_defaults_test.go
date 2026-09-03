package llm

import "testing"

func floatAt(t *testing.T, got *float64, want float64, name string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s not read", name)
	}
	if *got != want {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

// The exact block a live Ollama 0.33.2 returned for qwen3:0.6b on 2026-09-03. It is
// Modelfile text, the columns are space-padded, `stop` appears twice and quotes a token
// that is not a number -- all three are why this needs a parser and not json.Unmarshal.
func TestParseOllamaSamplingParametersReadsTheModelfileBlock(t *testing.T) {
	got := parseOllamaSamplingParameters(
		"repeat_penalty                 1\n" +
			"stop                           \"<|im_start|>\"\n" +
			"stop                           \"<|im_end|>\"\n" +
			"temperature                    0.6\n" +
			"top_k                          20\n" +
			"top_p                          0.95")

	floatAt(t, got.Temperature, 0.6, "temperature")
	floatAt(t, got.Sampling.TopP, 0.95, "top_p")
	floatAt(t, got.Sampling.RepetitionPenalty, 1, "repeat_penalty")
	if got.Sampling.TopK == nil || *got.Sampling.TopK != 20 {
		t.Errorf("top_k = %v, want 20", got.Sampling.TopK)
	}
	// The quoted stop tokens must not become sampling knobs, and nothing the block did
	// not mention may be invented.
	if got.Sampling.MinP != nil || got.Sampling.PresencePenalty != nil {
		t.Errorf("invented a knob the block never stated: %+v", got.Sampling)
	}
}

// llama.cpp and Ollama say repeat_penalty; OpenRouter says repetition_penalty. Both must
// land on the one field Aura has, or the knob silently disappears on one backend.
func TestSamplingDefaultsNormaliseBothPenaltySpellings(t *testing.T) {
	for name, values := range map[string]map[string]float64{
		"llama.cpp/ollama": {"repeat_penalty": 1.1},
		"openrouter":       {"repetition_penalty": 1.1},
	} {
		t.Run(name, func(t *testing.T) {
			floatAt(t, samplingDefaultsFromNumbers(values).Sampling.RepetitionPenalty, 1.1, "penalty")
		})
	}
}

// A published default fills what the operator left alone and never overrules what they
// pinned. Both halves matter: the first is the whole point, the second is what stops a
// model card from quietly undoing a deliberate deployment choice.
func TestApplyDiscoveredSamplingYieldsToTheOperator(t *testing.T) {
	pinnedTopP := 0.5
	cfg := Config{
		Temperature:           0.7,
		TemperatureConfigured: true,
		Sampling:              Sampling{TopP: &pinnedTopP},
	}
	applyDiscoveredSampling(&cfg, samplingDefaultsFromNumbers(map[string]float64{
		"temperature": 1.0, "top_p": 0.95, "top_k": 64,
	}))

	if cfg.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want the operator's 0.7 to survive", cfg.Temperature)
	}
	floatAt(t, cfg.Sampling.TopP, 0.5, "top_p")
	if cfg.Sampling.TopK == nil || *cfg.Sampling.TopK != 64 {
		t.Errorf("top_k = %v, want the published 64 to fill an unset knob", cfg.Sampling.TopK)
	}
}

// The case that started this: Gemma's GGUF asks for 1.0 and the built-in 0.7 was
// overriding it on every request without saying so.
func TestApplyDiscoveredSamplingReplacesTheBuiltInTemperature(t *testing.T) {
	cfg := Config{Temperature: defaultTemperature}
	applyDiscoveredSampling(&cfg, samplingDefaultsFromNumbers(map[string]float64{"temperature": 1.0}))
	if cfg.Temperature != 1.0 {
		t.Errorf("Temperature = %v, want the model's published 1.0", cfg.Temperature)
	}
}

// A backend that publishes nothing -- most OpenRouter models -- must leave the config
// exactly as it was rather than blanking it.
func TestApplyDiscoveredSamplingIsANoOpWhenNothingIsPublished(t *testing.T) {
	cfg := Config{Temperature: 0.42}
	applyDiscoveredSampling(&cfg, samplingDefaultsFromNumbers(nil))
	if cfg.Temperature != 0.42 || cfg.Sampling != (Sampling{}) {
		t.Errorf("an empty publication changed the config: %+v", cfg.Sampling)
	}
}
