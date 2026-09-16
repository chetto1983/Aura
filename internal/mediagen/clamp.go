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
// it. For a known model an absent descriptor means unsupported: an undeclared ratio is
// dropped with a note, but references the model cannot take refuse the whole request
// (checkReferences), because dropping them still bills a generation that ignores them.
func ClampImage(in ImageInput, m *Model) (ImageInput, []string, error) {
	out := ImageInput{
		Prompt:            in.Prompt,
		AspectRatio:       in.AspectRatio,
		ReferenceAssetIDs: slices.Clone(in.ReferenceAssetIDs),
	}
	if m == nil {
		return out, nil, nil
	}
	references, declared := m.Parameters["input_references"]
	switch {
	case !declared:
		if err := checkReferences(KindImage, len(in.ReferenceAssetIDs), 0); err != nil {
			return ImageInput{}, nil, err
		}
	case references.Max != nil:
		if err := checkReferences(KindImage, len(in.ReferenceAssetIDs), *references.Max); err != nil {
			return ImageInput{}, nil, err
		}
	}
	var notes adjustments
	out.AspectRatio = notes.choose("aspect ratio", in.AspectRatio, m.Parameters["aspect_ratio"].Values, aspectRatioValue)
	return out, notes, nil
}

// ClampVideo narrows in to what m declares and explains every change. Unlike images,
// the video catalog leaves most sets null for many models, so only a nonempty declared
// set is clamped; an empty one is left for provider validation. Images are the
// exception: a first frame the model cannot start from, or more references than its
// declared maximum, refuse the request rather than bill a clip that ignores them.
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
		return VideoInput{}, nil, &Error{Code: "unsupported", Message: "The selected video model cannot start from an image. " +
			"Nothing was generated. Do not resubmit it as a text-only video: ask the operator to choose a video model with image-to-video."}
	}
	if references, declared := m.Parameters["input_references"]; declared && references.Max != nil {
		if err := checkReferences(KindVideo, len(in.ReferenceAssetIDs), *references.Max); err != nil {
			return VideoInput{}, nil, err
		}
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

// checkReferences refuses requested references beyond what the model takes. Dropping the
// excess used to leave a billed call that ignored them — an edit silently turned into a
// fresh image, found in review on 2026-09-16 — and which ones to keep is the model's choice,
// not the first N. The message says nothing was generated and what to do instead, so it
// works whether or not the media skill is loaded. A negative catalog maximum reads as zero.
func checkReferences(kind Kind, requested, limit int) error {
	limit = max(limit, 0)
	if requested <= limit {
		return nil
	}
	if limit == 0 {
		return &Error{Code: "unsupported", Message: fmt.Sprintf(
			"The selected %s model cannot use reference images. Nothing was generated. "+
				"Do not retry without them: ask the operator to choose a %s model that accepts reference images.", kind, kind)}
	}
	return &Error{Code: "unsupported", Message: fmt.Sprintf(
		"The selected %s model accepts at most %d reference %s and %d were given. Nothing was generated. "+
			"Call again with the %d that matter most, or ask the operator which to keep.",
		kind, limit, pluralImage(limit), requested, limit)}
}

func pluralImage(n int) string {
	if n == 1 {
		return "image"
	}
	return "images"
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
