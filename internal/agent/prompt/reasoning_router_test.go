package prompt

import (
	"strings"
	"testing"
)

// TestReasoningRouterSystemPrompt locks the oracle prompt contract: it must
// demand a JSON-only reply and enumerate exactly the three routable tiers, since
// both the LLM-router fallback and the self-improvement oracle parse its output
// with ParseReasoningRouterTier. A drift here (e.g. an added "medium" tier the
// parser does not normalize) would silently produce unparseable verdicts.
func TestReasoningRouterSystemPrompt(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"Reply only JSON",
		`{"tier":"none"|"low"|"high"}`,
		"none=",
		"low=",
		"high=",
	} {
		if !strings.Contains(ReasoningRouterSystemPrompt, want) {
			t.Errorf("ReasoningRouterSystemPrompt missing %q", want)
		}
	}
}

// TestParseReasoningRouterTier_CleanJSON covers the happy path: a bare JSON
// object is unmarshalled and its tier normalized to a valid ReasoningTier.
func TestParseReasoningRouterTier_CleanJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want ReasoningTier
	}{
		{"none", `{"tier":"none"}`, ReasoningTierNone},
		{"low", `{"tier":"low"}`, ReasoningTierLow},
		{"high", `{"tier":"high"}`, ReasoningTierHigh},
		{"extra fields ignored", `{"tier":"high","why":"coding task"}`, ReasoningTierHigh},
		{"whitespace around object", "  \n{\"tier\":\"low\"}\n  ", ReasoningTierLow},
		{"uppercase value normalized", `{"tier":"HIGH"}`, ReasoningTierHigh},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseReasoningRouterTier(tc.raw); got != tc.want {
				t.Fatalf("ParseReasoningRouterTier(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseReasoningRouterTier_FencedAndPrefixed proves the tolerance the doc
// comment promises: a JSON object wrapped in prose or markdown fences is sliced
// out via the first '{' / last '}' before unmarshalling.
func TestParseReasoningRouterTier_FencedAndPrefixed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want ReasoningTier
	}{
		{"markdown fence", "```json\n{\"tier\":\"high\"}\n```", ReasoningTierHigh},
		{"leading prose", "Sure, here is the classification: {\"tier\":\"low\"}", ReasoningTierLow},
		{"trailing prose", "{\"tier\":\"none\"} — that's a greeting.", ReasoningTierNone},
		{"prose both sides", "result => {\"tier\":\"high\"} done", ReasoningTierHigh},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseReasoningRouterTier(tc.raw); got != tc.want {
				t.Fatalf("ParseReasoningRouterTier(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseReasoningRouterTier_BareWordFallback exercises the non-JSON fallback:
// when there is no parseable object the raw string is normalized directly, so a
// model that ignores the JSON instruction and replies with a bare tier word
// still routes correctly.
func TestParseReasoningRouterTier_BareWordFallback(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want ReasoningTier
	}{
		{"bare none", "none", ReasoningTierNone},
		{"bare low", "low", ReasoningTierLow},
		{"bare high", "high", ReasoningTierHigh},
		{"padded bare word", "  HIGH  ", ReasoningTierHigh},
		{"quoted bare word", `"low"`, ReasoningTierLow},
		{"json-language-prefixed bare word", "json high", ReasoningTierHigh},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseReasoningRouterTier(tc.raw); got != tc.want {
				t.Fatalf("ParseReasoningRouterTier(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseReasoningRouterTier_Unparseable proves the invalid-sentinel contract:
// anything that does not resolve to one of the three tiers yields the empty
// ("" / invalid) tier so the caller falls back to the embedding classifier or a
// safe default instead of acting on a bogus verdict.
func TestParseReasoningRouterTier_Unparseable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
	}{
		{"empty string", ""},
		{"whitespace only", "   \n\t "},
		{"unknown tier value", `{"tier":"extreme"}`},
		{"unknown bare word", "banana"},
		{"empty object", `{}`},
		{"json with empty tier", `{"tier":""}`},
		{"only braces", "{}{}"},
		// First '{' .. last '}' slices a span that is not valid JSON (the leading
		// '{ignore}' merges in), so the unmarshal fails and the raw-string fallback
		// finds no bare tier word either.
		{"first-brace-to-last-brace span is invalid json", "note {ignore} {\"tier\":\"low\"}"},
		// Bareword JSON value: unmarshal fails, and normalizing the whole braces
		// string matches no synonym.
		{"malformed json bareword value", `{"tier": low,}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParseReasoningRouterTier(tc.raw)
			if got != "" {
				t.Fatalf("ParseReasoningRouterTier(%q) = %q, want empty (invalid) tier", tc.raw, got)
			}
			if got.Valid() {
				t.Fatalf("ParseReasoningRouterTier(%q) returned a Valid() tier %q for unparseable input", tc.raw, got)
			}
		})
	}
}

// TestParseReasoningRouterTier_SynonymsNormalize covers the synonym arms of
// normalizeReasoningTier reachable through the bare-word fallback: the parser
// maps off-vocabulary words a model might emit ("off"/"deep"/"small"...) onto the
// canonical tiers rather than discarding them.
func TestParseReasoningRouterTier_SynonymsNormalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want ReasoningTier
	}{
		{"no", ReasoningTierNone},
		{"off", ReasoningTierNone},
		{"false", ReasoningTierNone},
		{"small", ReasoningTierLow},
		{"light", ReasoningTierLow},
		{"deep", ReasoningTierHigh},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			if got := ParseReasoningRouterTier(tc.raw); got != tc.want {
				t.Fatalf("ParseReasoningRouterTier(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestReasoningTierValid_RejectsUnknown closes the Valid() default arm: only the
// three canonical tiers are valid, and the empty/garbage sentinels are not. This
// is the predicate ApplyAdaptiveReasoning and Classify gate on, so a false
// positive here would let a bogus tier reach the wire request.
func TestReasoningTierValid_RejectsUnknown(t *testing.T) {
	t.Parallel()
	valid := []ReasoningTier{ReasoningTierNone, ReasoningTierLow, ReasoningTierHigh}
	for _, tr := range valid {
		if !tr.Valid() {
			t.Errorf("%q.Valid() = false, want true", tr)
		}
	}
	invalid := []ReasoningTier{"", "medium", "NONE", "off", " low", "high "}
	for _, tr := range invalid {
		if tr.Valid() {
			t.Errorf("%q.Valid() = true, want false (only none/low/high are valid)", tr)
		}
	}
}
