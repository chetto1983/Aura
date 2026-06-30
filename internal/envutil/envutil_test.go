package envutil

import "testing"

// TestIntDefault_ParsesValidAndFallsBack covers the union of inputs the three old
// copies (config.envIntDefault, telegram.envIntDefault) handled: a valid int parses,
// while garbage / empty / unset fall back. "0" is a deliberate value passed through
// verbatim, never treated as empty.
func TestIntDefault_ParsesValidAndFallsBack(t *testing.T) {
	t.Setenv("AURA_TEST_INT_VALID", "42")
	if got := IntDefault("AURA_TEST_INT_VALID", 7); got != 42 {
		t.Errorf("valid int: want 42, got %d", got)
	}
	t.Setenv("AURA_TEST_INT_GARBAGE", "not-a-number")
	if got := IntDefault("AURA_TEST_INT_GARBAGE", 7); got != 7 {
		t.Errorf("garbage: want fallback 7, got %d", got)
	}
	t.Setenv("AURA_TEST_INT_EMPTY", "")
	if got := IntDefault("AURA_TEST_INT_EMPTY", 13); got != 13 {
		t.Errorf("empty: want fallback 13, got %d", got)
	}
	if got := IntDefault("AURA_TEST_INT_UNSET_XYZ", 5); got != 5 {
		t.Errorf("unset: want fallback 5, got %d", got)
	}
	t.Setenv("AURA_TEST_INT_ZERO", "0")
	if got := IntDefault("AURA_TEST_INT_ZERO", 9); got != 0 {
		t.Errorf("zero: want 0 passthrough, got %d", got)
	}
}

// TestBoolDefault_ParsesValidAndFallsBack covers the union the old copies
// (config.envBoolDefault, registry.envChannelEnabled's inline parse) handled: true/1
// parse, garbage / empty / unset fall back.
func TestBoolDefault_ParsesValidAndFallsBack(t *testing.T) {
	t.Setenv("AURA_TEST_BOOL_TRUE", "true")
	if got := BoolDefault("AURA_TEST_BOOL_TRUE", false); !got {
		t.Error("valid true: want true, got false")
	}
	t.Setenv("AURA_TEST_BOOL_ONE", "1")
	if got := BoolDefault("AURA_TEST_BOOL_ONE", false); !got {
		t.Error("'1': want true, got false")
	}
	t.Setenv("AURA_TEST_BOOL_GARBAGE", "not-a-bool")
	if got := BoolDefault("AURA_TEST_BOOL_GARBAGE", true); !got {
		t.Error("garbage: want fallback true, got false")
	}
	t.Setenv("AURA_TEST_BOOL_EMPTY", "")
	if got := BoolDefault("AURA_TEST_BOOL_EMPTY", true); !got {
		t.Error("empty: want fallback true, got false")
	}
	if got := BoolDefault("AURA_TEST_BOOL_UNSET_XYZ", false); got {
		t.Error("unset: want fallback false, got true")
	}
}
