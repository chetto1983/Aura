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

// TestCommandHookFailPolicy_OnlyExplicitFailOpenOpens pins the anti-silent-allow
// invariant at the policy-resolution layer (F-006): the ONLY inputs that resolve
// to FailOpen are an explicit, case-insensitive, whitespace-trimmed "fail_open".
// Every other string — empty, canonical fail_closed, unknown, or malformed —
// MUST resolve to FailClosed, and any non-canonical input MUST also surface an
// error so a caller that ignored the returned policy still cannot silently allow.
// This guards against a future edit that widened the fail-open set.
func TestCommandHookFailPolicy_OnlyExplicitFailOpenOpens(t *testing.T) {
	opensFailOpen := []string{"fail_open", "FAIL_OPEN", "Fail_Open", "  fail_open  ", "\tfail_open\n"}
	for _, raw := range opensFailOpen {
		got, err := commandHookFailPolicy(raw)
		if err != nil || got != FailOpen {
			t.Fatalf("commandHookFailPolicy(%q) = (%v, %v), want (FailOpen, nil)", raw, got, err)
		}
	}

	// Neither canonical closers nor adversarial/malformed strings may open.
	staysFailClosed := []string{
		"", "   ", "fail_closed", "FAIL_CLOSED", "  fail_closed  ",
		"failopen", "fail-open", "fail open", "open", "allow", "true", "1", "yes",
		"fail_sideways", "{}", "null", "fail_ope", "xfail_open", "fail_open_x",
	}
	for _, raw := range staysFailClosed {
		got, err := commandHookFailPolicy(raw)
		if got != FailClosed {
			t.Fatalf("commandHookFailPolicy(%q) = %v, want FailClosed (never silent-allow)", raw, got)
		}
		norm := strings.TrimSpace(strings.ToLower(raw))
		canonical := norm == "" || norm == "fail_closed"
		if !canonical && err == nil {
			t.Fatalf("commandHookFailPolicy(%q) err = nil, want unknown-policy error (no silent fallthrough)", raw)
		}
		if canonical && err != nil {
			t.Fatalf("commandHookFailPolicy(%q) err = %v, want nil for a canonical closer", raw, err)
		}
	}
}
