package llm

// How a model's context window is divided.
//
// Three numbers come out of one: what is held back for the answer, what is left as slack
// for whatever the token count cannot see, and what remains as prompt budget. They live
// together because they are one piece of arithmetic — reading any of them alone is how
// they came to disagree — and they left config.go when it crossed the 600-LOC cap.
//
// Everything here is expressed as an absolute AND a share, and takes the smaller. An
// absolute alone stops describing anything the moment the window changes size, which is
// the failure both constants below record having caused.

const (
	minOutputReservation = 20_000
	promptHeadroomTokens = 13_000

	// The two reserves above are absolute token counts sized for a large window, and on a
	// small one they are the whole window: a 32,768-token window (AURA_LLM_CTX's default on
	// the local llama.cpp lane, which is exactly what Aura is meant to run on) needs
	// 20,000 + 13,000 = 33,000 and is refused at boot by 232 tokens. MEASURED 2026-08-16
	// on the live deployment: `aura serve` crash-looped with "invalid prompt budget -232".
	//
	// So each reserve is now the SMALLER of its absolute size and a share of the window.
	// The shares are set so the absolute value still wins on every window big enough to
	// afford it: 30% of 100k is 30,000 and 20% is 20,000, both above the constants, so a
	// 100k window and the 1M deployment reserve exactly what they reserved before. They
	// bite only where the constants stop making sense -- at 32,768 they become 9,830 and
	// 6,553, leaving 16,385 of prompt budget where the old arithmetic left -232.
	outputReservePercent  = 30
	promptHeadroomPercent = 20
)

// DerivedMaxOutputTokens is the answer cap to use for a model whose provider publishes
// none: the default, capped at a share of THAT model's window.
//
// It exists because the alternative is inheritance, and an inherited cap is meaningless.
// Max output is derived from the model's published limit, so on a provider that does not
// publish one the previously derived number simply stays — a number describing a
// different model, now read against a different window. Measured 2026-09-03 switching
// OpenRouter to Ollama: 384,000 (the OpenRouter model's cap) survived into a 262,144
// window and the save failed on a negative prompt budget. Config.Validate refuses a
// non-positive cap, so the reset cannot be to zero; it is to what the new window affords.
func DerivedMaxOutputTokens(contextWindow int) int {
	return scaledReserve(contextWindow, defaultMaxOutputTokens, outputReservePercent)
}

// OutputReserve is the tokens held back for the model's answer: the configured cap, never
// below the floor.
//
// It is deliberately NOT clamped to a share of the window. A clamp here cannot tell an
// operator's explicit cap from one Aura derived, and silently halving an explicit one
// turns a configuration the operator should hear about — a 30,000-token answer asked for
// inside a 33,000-token window — into a surprise they never see. The impossible
// combination is Validate's to refuse, loudly. What must never be inherited is a DERIVED
// cap from another model, and that is recomputed at the model change itself; see
// DerivedMaxOutputTokens.
func OutputReserve(contextWindow, maxOutputTokens int) int {
	return max(maxOutputTokens, scaledReserve(contextWindow, minOutputReservation, outputReservePercent))
}

// PromptHeadroom is the slack left for what the token count cannot see (tool schemas, the
// provider's own framing), capped the same way.
func PromptHeadroom(contextWindow int) int {
	return scaledReserve(contextWindow, promptHeadroomTokens, promptHeadroomPercent)
}

// scaledReserve is min(absolute, window*percent), floored at 1 so a degenerate window
// still leaves a reserve rather than silently reserving nothing.
func scaledReserve(contextWindow, absolute, percent int) int {
	if contextWindow <= 0 {
		return absolute
	}
	scaled := max(contextWindow*percent/100, 1)
	return min(absolute, scaled)
}
