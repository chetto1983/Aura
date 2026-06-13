//go:build cot_eval || live_e2e

package eval

import (
	"encoding/json"
	"testing"
)

// TestAnyFloatJSONNumber covers the M-07 sibling: anyFloat must parse a
// json.Number cost_usd (the StateDelta is decoded with UseNumber) instead of
// silently dropping it; a non-numeric json.Number falls back to (_, false).
func TestAnyFloatJSONNumber(t *testing.T) {
	if got, ok := anyFloat(json.Number("4.2")); !ok || got != 4.2 {
		t.Errorf("anyFloat(json.Number(\"4.2\")) = %v,%v; want 4.2,true", got, ok)
	}
	if _, ok := anyFloat(json.Number("nope")); ok {
		t.Errorf("anyFloat(json.Number(\"nope\")) ok = true; want false fallback")
	}
}
