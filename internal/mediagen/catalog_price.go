package mediagen

import (
	"math"
	"strconv"
	"strings"
)

// ImagePrice returns the per-image USD range across the output_image lines priced per
// image. Token- and megapixel-priced lines depend on the prompt and output size, so a
// model billed only that way reports ok=false: no label beats a wrong one.
func ImagePrice(lines []PriceLine) (low, high float64, ok bool) {
	for _, line := range lines {
		if line.Billable == "output_image" && line.Unit == "image" {
			low, high, ok = widenPrice(low, high, ok, line.CostUSD)
		}
	}
	return low, high, ok
}

// ImageTokenPricePerMillion returns the USD range per million output image tokens, the unit
// every image model in the live catalog was billed in on 2026-09-16. It is a rate, not a
// per-image price: the token count depends on the size and quality of each image.
func ImageTokenPricePerMillion(lines []PriceLine) (low, high float64, ok bool) {
	for _, line := range lines {
		if line.Billable == "output_image" && line.Unit == "token" {
			low, high, ok = widenPrice(low, high, ok, perMillion(line.CostUSD))
		}
	}
	return low, high, ok
}

// perMillion scales a per-token rate, rounding off the binary noise of the product so
// 0.000038 reads as 38 rather than 38.00000000000001.
func perMillion(perToken float64) float64 {
	return math.Round(perToken*1e15) / 1e9
}

// VideoPrice returns the USD-per-second range across the duration_seconds* (dollars)
// and cents_per_second* (cents) SKUs. Every other SKU is priced per token, megapixel,
// image or generation and cannot be expressed per second, so it is left out.
func VideoPrice(skus map[string]string) (low, high float64, ok bool) {
	for key, raw := range skus {
		divisor, perSecond := videoSKUDivisor(key)
		if !perSecond {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			continue
		}
		low, high, ok = widenPrice(low, high, ok, value/divisor)
	}
	return low, high, ok
}

func videoSKUDivisor(key string) (float64, bool) {
	switch {
	case key == "duration_seconds" || strings.HasPrefix(key, "duration_seconds_"):
		return 1, true
	case key == "cents_per_second" || strings.HasPrefix(key, "cents_per_second_"):
		return 100, true
	}
	return 0, false
}

// VideoSecondPrice returns the USD per second of a clip at this resolution and audio choice,
// from the most specific SKU the model declares: resolution and audio, then audio, then
// resolution, then the plain rate; dollars before cents. The naming was checked against the
// costs measured for google/veo-3.1-lite on 2026-09-17, so a model that names its SKUs
// otherwise reports ok=false rather than a guess.
func VideoSecondPrice(skus map[string]string, resolution string, audio bool) (float64, bool) {
	sound := "_without_audio"
	if audio {
		sound = "_with_audio"
	}
	suffixes := []string{sound, ""}
	if res := strings.ToLower(strings.TrimSpace(resolution)); res != "" {
		suffixes = []string{sound + "_" + res, sound, "_" + res, ""}
	}
	for _, unit := range []struct {
		prefix  string
		divisor float64
	}{{"duration_seconds", 1}, {"cents_per_second", 100}} {
		for _, suffix := range suffixes {
			raw, declared := skus[unit.prefix+suffix]
			if !declared {
				continue
			}
			value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
			if err != nil || !(value >= 0) || math.IsInf(value, 1) {
				continue
			}
			return value / unit.divisor, true
		}
	}
	return 0, false
}

// widenPrice folds a finite nonnegative price into the range; anything else is ignored.
func widenPrice(low, high float64, known bool, price float64) (float64, float64, bool) {
	if !(price >= 0) || math.IsInf(price, 1) {
		return low, high, known
	}
	if !known {
		return price, price, true
	}
	return min(low, price), max(high, price), true
}
