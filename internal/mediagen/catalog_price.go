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
