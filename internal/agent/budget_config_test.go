package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// The hot loop budget (amendment #188): a profile that carries LoopMaxSteps /
// LoopMaxWallclockSec becomes an explicit override; a zero field leaves the env →
// default fallthrough untouched.
func TestBudgetOptionsFromConfig(t *testing.T) {
	t.Setenv(envMaxSteps, "")
	t.Setenv(envMaxWallclockSec, "")

	if opts := BudgetOptionsFromConfig(llm.Config{}); opts.MaxSteps != nil || opts.MaxWallclockSec != nil {
		t.Fatalf("zero profile must not override: %+v", opts)
	}

	opts := BudgetOptionsFromConfig(llm.Config{LoopMaxSteps: 7, LoopMaxWallclockSec: 11})
	if opts.MaxSteps == nil || *opts.MaxSteps != 7 || opts.MaxWallclockSec == nil || *opts.MaxWallclockSec != 11 {
		t.Fatalf("profile overrides not lifted: %+v", opts)
	}
	b, err := NewBudget(opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Remaining(); got != 7 {
		t.Fatalf("remaining = %d, want the profile's 7 (not the builtin %d)", got, defaultBudgetMaxSteps)
	}

	// The profile beats env; env still beats the builtin default when the profile is silent.
	t.Setenv(envMaxSteps, "40")
	b, err = NewBudget(BudgetOptionsFromConfig(llm.Config{LoopMaxSteps: 3}))
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Remaining(); got != 3 {
		t.Fatalf("remaining = %d, want profile 3 over env 40", got)
	}
	b, err = NewBudget(BudgetOptionsFromConfig(llm.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Remaining(); got != 40 {
		t.Fatalf("remaining = %d, want env 40 when the profile is silent", got)
	}
}

// The background window and ceiling (prd.md §15) follow the same precedence, and a child
// branch keeps both.
func TestBudgetResolvesTheBackgroundWindowAndCeiling(t *testing.T) {
	t.Setenv(envBackgroundAfterSec, "")
	t.Setenv(envBackgroundMaxSec, "")

	b, err := NewBudget(BudgetOptionsFromConfig(llm.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	if b.BackgroundWindow() != time.Minute || b.BackgroundCeiling() != 30*time.Minute {
		t.Fatalf("defaults = %v / %v, want 1m / 30m", b.BackgroundWindow(), b.BackgroundCeiling())
	}

	t.Setenv(envBackgroundAfterSec, "45")
	t.Setenv(envBackgroundMaxSec, "900")
	b, err = NewBudget(BudgetOptionsFromConfig(llm.Config{LoopBackgroundMaxSec: 600}))
	if err != nil {
		t.Fatal(err)
	}
	if b.BackgroundWindow() != 45*time.Second || b.BackgroundCeiling() != 10*time.Minute {
		t.Fatalf("got %v / %v, want env 45s and profile 10m over env 15m", b.BackgroundWindow(), b.BackgroundCeiling())
	}
	child := b.Child(2)
	if child.BackgroundWindow() != b.BackgroundWindow() || child.BackgroundCeiling() != b.BackgroundCeiling() {
		t.Fatalf("child = %v / %v, want the parent's", child.BackgroundWindow(), child.BackgroundCeiling())
	}
}

func TestBudgetRefusesABackgroundCeilingThatEndsEveryMovedCall(t *testing.T) {
	for name, c := range map[string]struct {
		cfg          llm.Config
		after, limit string
		want         string
	}{
		"ceiling at the window":    {cfg: llm.Config{LoopBackgroundAfterSec: 60, LoopBackgroundMaxSec: 60}, want: "must be greater than"},
		"ceiling under the window": {cfg: llm.Config{LoopBackgroundAfterSec: 120}, limit: "90", want: "must be greater than"},
		"zero window from env":     {after: "0", want: envBackgroundAfterSec + `="0": must be >= 1`},
		"malformed env window":     {after: "soon", want: envBackgroundAfterSec},
		"malformed env ceiling":    {limit: "later", want: envBackgroundMaxSec},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(envBackgroundAfterSec, c.after)
			t.Setenv(envBackgroundMaxSec, c.limit)
			if _, err := NewBudget(BudgetOptionsFromConfig(c.cfg)); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("NewBudget err = %v, want %q", err, c.want)
			}
		})
	}
}
