package mediagen

import (
	"math"
	"testing"
)

func TestImagePriceDoesNotCallTokenPricingPerImage(t *testing.T) {
	_, _, ok := ImagePrice([]PriceLine{{Billable: "output_image", Unit: "token", CostUSD: .000038}})
	if ok {
		t.Fatal("token pricing must not become a per-image label")
	}
}

// TestImageTokenPrice pins the rate the picker shows for a model billed per output token. Every
// image model in the live catalog on 2026-09-16 billed that way, so without it no image row
// carried a price and the cheapest model could not be chosen from the menu.
func TestImageTokenPrice(t *testing.T) {
	cases := []struct {
		name      string
		lines     []PriceLine
		low, high float64
		ok        bool
	}{
		{name: "no lines"},
		{name: "per-image lines are not token rates", lines: []PriceLine{{Billable: "output_image", Unit: "image", CostUSD: 0.04}}},
		{name: "input tokens are not the output rate", lines: []PriceLine{{Billable: "input_image", Unit: "token", CostUSD: 0.000008}}},
		{
			name: "observed MAI endpoint",
			lines: []PriceLine{
				{Billable: "input_text", Unit: "token", CostUSD: 0.000005},
				{Billable: "output_image", Unit: "token", CostUSD: 0.000038},
			},
			low: 38, high: 38, ok: true,
		},
		{
			name: "two providers widen the range",
			lines: []PriceLine{
				{Billable: "output_image", Unit: "token", CostUSD: 0.00003},
				{Billable: "output_image", Unit: "token", CostUSD: 0.000008},
			},
			low: 8, high: 30, ok: true,
		},
		{name: "a negative rate is ignored", lines: []PriceLine{{Billable: "output_image", Unit: "token", CostUSD: -1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			low, high, ok := ImageTokenPricePerMillion(tc.lines)
			if ok != tc.ok || low != tc.low || high != tc.high {
				t.Fatalf("ImageTokenPricePerMillion = %v, %v, %v; want %v, %v, %v", low, high, ok, tc.low, tc.high, tc.ok)
			}
		})
	}
}

func TestImagePrice(t *testing.T) {
	cases := []struct {
		name      string
		lines     []PriceLine
		low, high float64
		ok        bool
	}{
		{name: "no lines", lines: nil},
		{
			name: "observed MAI endpoint is token priced",
			lines: []PriceLine{
				{Billable: "input_text", Unit: "token", CostUSD: 0.000005},
				{Billable: "input_image", Unit: "token", CostUSD: 0.000008},
				{Billable: "output_image", Unit: "token", CostUSD: 0.000038},
			},
		},
		{
			name:  "one per-image line",
			lines: []PriceLine{{Billable: "output_image", Unit: "image", CostUSD: 0.05}},
			low:   0.05, high: 0.05, ok: true,
		},
		{
			name: "variants and endpoints widen the range",
			lines: []PriceLine{
				{Billable: "output_image", Unit: "image", Variant: "high", CostUSD: 0.19},
				{Billable: "input_image", Unit: "image", CostUSD: 0.01},
				{Billable: "output_image", Unit: "image", Variant: "low", CostUSD: 0.02},
				{Billable: "output_image", Unit: "megapixel", CostUSD: 0.001},
				{Billable: "output_image", Unit: "image", Variant: "medium", CostUSD: 0.07},
			},
			low: 0.02, high: 0.19, ok: true,
		},
		{
			name:  "zero is a real known price",
			lines: []PriceLine{{Billable: "output_image", Unit: "image", CostUSD: 0}},
			low:   0, high: 0, ok: true,
		},
		{
			name: "negative, NaN and infinite costs are ignored",
			lines: []PriceLine{
				{Billable: "output_image", Unit: "image", CostUSD: -0.04},
				{Billable: "output_image", Unit: "image", CostUSD: math.NaN()},
				{Billable: "output_image", Unit: "image", CostUSD: math.Inf(1)},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			low, high, ok := ImagePrice(tc.lines)
			if low != tc.low || high != tc.high || ok != tc.ok {
				t.Fatalf("ImagePrice = (%v, %v, %v), want (%v, %v, %v)", low, high, ok, tc.low, tc.high, tc.ok)
			}
		})
	}
}

func TestVideoPrice(t *testing.T) {
	cases := []struct {
		name      string
		skus      map[string]string
		low, high float64
		ok        bool
	}{
		{name: "nil map", skus: nil},
		{
			name: "observed Hailuo duration SKUs",
			skus: map[string]string{"duration_seconds": "0.08", "duration_seconds_480p": "0.05", "duration_seconds_768p": "0.08"},
			low:  0.05, high: 0.08, ok: true,
		},
		{
			name: "cents per second are converted to dollars",
			skus: map[string]string{"cents_per_second_output": "3", "cents_per_second": "12.5"},
			low:  0.03, high: 0.125, ok: true,
		},
		{
			name: "token, megapixel, image and flat SKUs carry no per-second price",
			skus: map[string]string{
				"video_tokens": "0.000007", "cents_per_megapixel_second_precise": "4",
				"cents_per_image_input": "2", "minimum_cents_per_generation": "10",
				"text_to_video_duration_seconds_480p": "0.05", "generate": "0.50",
				"duration_secondsx": "0.01",
			},
		},
		{
			name: "zero is a real known price",
			skus: map[string]string{"duration_seconds": "0"},
			low:  0, high: 0, ok: true,
		},
		{
			name: "malformed, negative, NaN and infinite values are ignored",
			skus: map[string]string{
				"duration_seconds": "free", "duration_seconds_720p": "-0.1",
				"duration_seconds_1080p": "NaN", "cents_per_second": "Inf",
				"duration_seconds_4k": " 0.4 ",
			},
			low: 0.4, high: 0.4, ok: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			low, high, ok := VideoPrice(tc.skus)
			if low != tc.low || high != tc.high || ok != tc.ok {
				t.Fatalf("VideoPrice = (%v, %v, %v), want (%v, %v, %v)", low, high, ok, tc.low, tc.high, tc.ok)
			}
		})
	}
}

func TestVideoSecondPrice(t *testing.T) {
	veoLite := map[string]string{ // google/veo-3.1-lite, live catalog 2026-09-17
		"duration_seconds_with_audio":         "0.08",
		"duration_seconds_without_audio":      "0.05",
		"duration_seconds_with_audio_720p":    "0.05",
		"duration_seconds_without_audio_720p": "0.03",
	}
	cases := []struct {
		name       string
		skus       map[string]string
		resolution string
		audio      bool
		want       float64
		ok         bool
	}{
		{"resolution and audio SKU", veoLite, "720p", false, 0.03, true},
		{"resolution with audio", veoLite, "720p", true, 0.05, true},
		{"unpriced resolution falls back to the audio SKU", veoLite, "1080p", true, 0.08, true},
		{"no resolution given", veoLite, "", false, 0.05, true},
		{"plain duration SKU", map[string]string{"duration_seconds": "0.1"}, "720p", true, 0.1, true},
		{"resolution-only SKU", map[string]string{"duration_seconds_720p": "0.2", "duration_seconds": "0.4"}, "720P", false, 0.2, true},
		{"cents per second", map[string]string{"cents_per_second_with_audio": "7"}, "", true, 0.07, true},
		{"unparseable value is skipped", map[string]string{"duration_seconds_720p": "x", "duration_seconds": "0.4"}, "720p", false, 0.4, true},
		{"negative value is skipped", map[string]string{"duration_seconds": "-1"}, "", false, 0, false},
		{"no per-second SKU", map[string]string{"per_generation": "1"}, "720p", false, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := VideoSecondPrice(tc.skus, tc.resolution, tc.audio)
			if ok != tc.ok || math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("VideoSecondPrice = %v, %v; want %v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
