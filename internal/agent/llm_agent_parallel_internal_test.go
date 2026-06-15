package agent

import "testing"

// TestMaxParallelTools_EnvGuard pins maxParallelTools' env parsing: unset/blank,
// non-numeric, zero, and negative all fall back to the safe default, while a valid
// positive value is honored. A bug that returns 0 here would size the executeBatch
// worker pool to zero and deadlock the dispatch, so the <=0 / parse-error guards are
// load-bearing, not cosmetic.
func TestMaxParallelTools_EnvGuard(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want int
	}{
		{"blank_returns_default", "", defaultMaxParallelTools},
		{"whitespace_returns_default", "   ", defaultMaxParallelTools},
		{"valid_positive_is_honored", "8", 8},
		{"one_is_honored", "1", 1},
		{"zero_falls_back", "0", defaultMaxParallelTools},
		{"negative_falls_back", "-3", defaultMaxParallelTools},
		{"non_numeric_falls_back", "abc", defaultMaxParallelTools},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(envMaxParallelTools, c.env)
			if got := maxParallelTools(); got != c.want {
				t.Fatalf("maxParallelTools() with %q = %d, want %d", c.env, got, c.want)
			}
		})
	}
}
