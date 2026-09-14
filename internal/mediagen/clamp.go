package mediagen

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// ClampImage narrows in to what m declares and explains every change. A nil m is a
// model missing from the catalog: the input passes through and OpenRouter validates
// it. For a known model an absent descriptor means unsupported, so the ratio or the
// references are dropped rather than sent to a model that never declared them.
func ClampImage(in ImageInput, m *Model) (ImageInput, []string) {
	out := ImageInput{
		Prompt:            in.Prompt,
		AspectRatio:       in.AspectRatio,
		ReferenceAssetIDs: slices.Clone(in.ReferenceAssetIDs),
	}
	if m == nil {
		return out, nil
	}
	var notes adjustments
	out.AspectRatio = notes.choose("aspect ratio", in.AspectRatio, m.Parameters["aspect_ratio"].Values, aspectRatioValue)
	references, declared := m.Parameters["input_references"]
	switch {
	case !declared:
		out.ReferenceAssetIDs = notes.truncate(out.ReferenceAssetIDs, 0)
	case references.Max != nil:
		out.ReferenceAssetIDs = notes.truncate(out.ReferenceAssetIDs, max(*references.Max, 0))
	}
	return out, notes
}

// ClampVideo narrows in to what m declares and explains every change. Unlike images,
// the video catalog leaves most sets null for many models, so only a nonempty declared
// set is clamped; an empty one is left for provider validation. A first frame is the
// exception: animating an image the model cannot take is refused, never silently
// turned into text-to-video.
func ClampVideo(in VideoInput, m *Model) (VideoInput, []string, error) {
	out := in
	out.ReferenceAssetIDs = slices.Clone(in.ReferenceAssetIDs)
	if in.Audio != nil {
		audio := *in.Audio
		out.Audio = &audio
	}
	if m == nil {
		return out, nil, nil
	}
	if in.FirstFrameAssetID != "" && !slices.Contains(m.FrameImages, "first_frame") {
		return VideoInput{}, nil, &Error{Code: "unsupported", Message: "The selected video model cannot start from an image."}
	}
	var notes adjustments
	if in.Duration != 0 && len(m.Durations) > 0 && !slices.Contains(m.Durations, in.Duration) {
		out.Duration = nearestInt(in.Duration, m.Durations)
		notes.add("duration %ds is not offered; used %ds", in.Duration, out.Duration)
	}
	if len(m.Resolutions) > 0 {
		out.Resolution = notes.choose("resolution", in.Resolution, m.Resolutions, resolutionHeight)
	}
	if len(m.AspectRatios) > 0 {
		out.AspectRatio = notes.choose("aspect ratio", in.AspectRatio, m.AspectRatios, aspectRatioValue)
	}
	if references, declared := m.Parameters["input_references"]; declared && references.Max != nil {
		out.ReferenceAssetIDs = notes.truncate(out.ReferenceAssetIDs, max(*references.Max, 0))
	}
	if in.Audio != nil && !m.GenerateAudio {
		out.Audio = nil
		notes.add("audio %t is not supported by this model; omitted", *in.Audio)
	}
	return out, notes, nil
}

// adjustments collects the plain-language notes the tool result returns to the model.
type adjustments []string

func (a *adjustments) add(format string, args ...any) {
	*a = append(*a, fmt.Sprintf(format, args...))
}

// choose keeps want when supported lists it, else picks the nearest measurable
// candidate (the smaller one on a tie), else drops want.
func (a *adjustments) choose(label, want string, supported []string, measure func(string) (float64, bool)) string {
	if want == "" || slices.Contains(supported, want) {
		return want
	}
	best, bestValue, bestDistance := "", 0.0, math.Inf(1)
	if target, ok := measure(want); ok {
		for _, candidate := range supported {
			value, ok := measure(candidate)
			if !ok {
				continue
			}
			distance := math.Abs(value - target)
			if distance < bestDistance || (distance == bestDistance && value < bestValue) {
				best, bestValue, bestDistance = candidate, value, distance
			}
		}
	}
	if best == "" {
		a.add("%s %s is not supported; omitted", label, want)
		return ""
	}
	a.add("%s %s is not offered; used %s", label, want, best)
	return best
}

func (a *adjustments) truncate(ids []string, limit int) []string {
	if len(ids) <= limit {
		return ids
	}
	if limit == 0 {
		a.add("reference images are not supported by this model; %d omitted", len(ids))
		return nil
	}
	a.add("%d reference images requested; this model accepts at most %d, used the first %d", len(ids), limit, limit)
	return ids[:limit]
}

func nearestInt(want int, supported []int) int {
	best := supported[0]
	distance := func(v int) uint64 {
		if v >= want {
			return uint64(v) - uint64(want)
		}
		return uint64(want) - uint64(v)
	}
	for _, candidate := range supported[1:] {
		if distance(candidate) < distance(best) ||
			(distance(candidate) == distance(best) && candidate < best) {
			best = candidate
		}
	}
	return best
}

var resolutionKHeights = map[string]float64{"1k": 1024, "2k": 2048, "4k": 4096}

// resolutionHeight measures "<n>p" and the 1K/2K/4K labels the catalog uses by pixel height.
func resolutionHeight(label string) (float64, bool) {
	label = strings.ToLower(strings.TrimSpace(label))
	if height, ok := resolutionKHeights[label]; ok {
		return height, true
	}
	digits, found := strings.CutSuffix(label, "p")
	if !found {
		return 0, false
	}
	height, err := strconv.Atoi(digits)
	if err != nil || height <= 0 {
		return 0, false
	}
	return float64(height), true
}

// aspectRatioValue measures "W:H" as W/H; "auto" and malformed labels are not ratios.
func aspectRatioValue(label string) (float64, bool) {
	width, height, found := strings.Cut(label, ":")
	if !found {
		return 0, false
	}
	w, wErr := strconv.ParseFloat(width, 64)
	h, hErr := strconv.ParseFloat(height, 64)
	if wErr != nil || hErr != nil || !(w > 0) || !(h > 0) || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return 0, false
	}
	return w / h, true
}
