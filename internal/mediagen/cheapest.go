package mediagen

import "slices"

// CheapestVideoInput fills the options a request left out with the least expensive ones the
// model declares: the shortest duration, the lowest resolution, and audio explicitly off.
//
// An omitted option is not free. The provider picks its own default for a field the body does
// not carry, and its default is whatever it sells best — on a per-second SKU that is the long,
// high, audible clip. The Studio names its model per request and its form leaves every option
// optional, so a request that says nothing must still cost the minimum rather than the
// provider's preference. Anything the caller did state is left exactly as it stated it; the
// clamp still has the last word on whether the model offers it.
//
// The seed is not defaulted: no seed IS the cheapest and is already what an absent one means.
func CheapestVideoInput(in VideoInput, m *Model) VideoInput {
	if m == nil {
		return in
	}
	if in.Duration == 0 && len(m.Durations) > 0 {
		in.Duration = slices.Min(m.Durations)
	}
	if in.Resolution == "" {
		in.Resolution = lowestResolution(m.Resolutions)
	}
	// Only when the model can generate audio: asking a silent model for silence earns a
	// "not supported" note about a choice the caller never made.
	if in.Audio == nil && m.GenerateAudio {
		off := false
		in.Audio = &off
	}
	return in
}

// lowestResolution is the shortest declared height, or empty when none can be measured — an
// unmeasurable label is not silently preferred to the caller's silence.
func lowestResolution(declared []string) string {
	lowest, lowestHeight := "", 0.0
	for _, candidate := range declared {
		height, ok := resolutionHeight(candidate)
		if !ok {
			continue
		}
		if lowest == "" || height < lowestHeight {
			lowest, lowestHeight = candidate, height
		}
	}
	return lowest
}
