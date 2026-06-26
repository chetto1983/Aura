package agent

import (
	"strings"
	"testing"
)

// TestCommandHookFailPolicy pins F-006: an UNSET/empty fail_policy must default
// to FailClosed so a configured security hook that crashes or times out DENIES
// the command instead of allowing it. fail_open is an explicit opt-in only.
func TestCommandHookFailPolicy(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    FailPolicy
		wantErr bool
	}{
		{"empty_defaults_closed", "", FailClosed, false},
		{"whitespace_defaults_closed", "   ", FailClosed, false},
		{"explicit_fail_open", "fail_open", FailOpen, false},
		{"explicit_fail_open_mixed_case", "Fail_Open", FailOpen, false},
		{"explicit_fail_open_padded", "  fail_open  ", FailOpen, false},
		{"explicit_fail_closed", "fail_closed", FailClosed, false},
		{"unknown_errors", "fail_sideways", FailClosed, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := commandHookFailPolicy(c.raw)
			if c.wantErr {
				if err == nil {
					t.Fatalf("commandHookFailPolicy(%q) err = nil, want error", c.raw)
				}
				if !strings.Contains(err.Error(), "unknown command hook fail_policy") {
					t.Fatalf("commandHookFailPolicy(%q) err = %v, want unknown-policy error", c.raw, err)
				}
			} else if err != nil {
				t.Fatalf("commandHookFailPolicy(%q) err = %v, want nil", c.raw, err)
			}
			if got != c.want {
				t.Fatalf("commandHookFailPolicy(%q) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}
