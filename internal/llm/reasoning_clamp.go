package llm

// Keeping a chosen reasoning effort inside what the model will accept.
//
// The adaptive tier classifies the CONVERSATION — a greeting gets "none", a proof gets
// "high" — which is the right question to ask and the wrong answer to send unchecked.
// 298 of OpenRouter's 424 models declare `reasoning.mandatory: true`, and they do not
// quietly ignore an effort they cannot honour: they refuse the whole request.
//
// Measured 2026-09-03 against z-ai/glm-5.3-flash, which advertises
// supported_efforts ["max","high","low"]:
//
//	{"effort":"none"}     -> HTTP 400 "Reasoning is mandatory for this endpoint and cannot be disabled."
//	{"enabled":false}     -> HTTP 400, same message
//	{"effort":"low"}      -> 200
//
// So every turn the classifier called easy died on that model, which is precisely the
// traffic that should have been cheapest. google/gemini-3.8-flash is the same shape with
// a different set (["high","medium","low"]).
//
// ApplyAdaptiveReasoning's own doc comment claimed a tier it sets "is still bounded" by
// the capability source. It was not: AllowedEfforts was consumed only by the cockpit —
// the effort dropdown and the validator for an EXPLICIT user choice — and nothing on the
// request path ever consulted it. This file is that missing bound, placed where both the
// adaptive and the fixed path already hold the config.
//
// The clamp only ever fires on a set the provider actually published. An unknown surface
// leaves the effort exactly as chosen: guessing a substitute for a model that told us
// nothing would trade a loud, legible 400 for a silent behaviour change.

import "slices"

// reasoningLadder orders the vocabulary from least to most reasoning. Distance along it
// is what "nearest supported effort" means; it is not a set of magnitudes, only an order.
var reasoningLadder = []ReasoningEffort{
	ReasoningEffortNone, ReasoningEffortLow, ReasoningEffortMedium,
	ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax,
}

// reasoningEffortTokens pulls the advertised effort strings out of the catalogue entry.
// The pointer is nil for a model that publishes no reasoning block at all.
func reasoningEffortTokens(reasoning *struct {
	SupportedEfforts []string `json:"supported_efforts"`
	Mandatory        bool     `json:"mandatory"`
}) []string {
	if reasoning == nil {
		return nil
	}
	return reasoning.SupportedEfforts
}

// clampAdvertisedEfforts maps advertised tokens onto the internal vocabulary through the
// same strict allowlist the capability source uses: a token Aura does not model is
// DROPPED rather than passed through, so a future or hostile upstream value can never
// become an effort that goes on the wire.
func clampAdvertisedEfforts(tokens []string) []ReasoningEffort {
	var out []ReasoningEffort
	for _, token := range tokens {
		if effort, ok := allowedReasoningEfforts[token]; ok && !slices.Contains(out, effort) {
			out = append(out, effort)
		}
	}
	return out
}

// ClampReasoningEffort returns the nearest effort the serving model accepts.
//
// It returns want unchanged when the model published no set, or when want is already in
// it. Otherwise it walks the ladder for the supported effort closest to want, preferring
// the LOWER one on a tie: the substitution exists because the turn was classified as
// cheap, so a tie should not resolve into more reasoning than the classifier asked for.
func (c Config) ClampReasoningEffort(want ReasoningEffort) ReasoningEffort {
	if want == "" || len(c.SupportedReasoningEfforts) == 0 ||
		slices.Contains(c.SupportedReasoningEfforts, want) {
		return want
	}
	target := slices.Index(reasoningLadder, want)
	if target < 0 {
		return want
	}
	best, bestDistance := want, len(reasoningLadder)+1
	for _, candidate := range c.SupportedReasoningEfforts {
		at := slices.Index(reasoningLadder, candidate)
		if at < 0 {
			continue
		}
		distance := at - target
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance || (distance == bestDistance && at < slices.Index(reasoningLadder, best)) {
			best, bestDistance = candidate, distance
		}
	}
	return best
}
