package agent

import (
	"testing"

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
