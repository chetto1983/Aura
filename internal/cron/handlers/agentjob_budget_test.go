package handlers

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// D-24 precedence with the hot loop profile inserted (amendment #188): the row's
// step_budget still wins, the profile beats the env/builtin default, and a silent
// profile leaves the default alone.
func TestNewJobBudgetPrecedence(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "")
	t.Setenv("AURA_LOOP_MAX_WALLCLOCK_SEC", "")

	fromRow, err := newJobBudget(4, llm.Config{LoopMaxSteps: 9})
	if err != nil {
		t.Fatal(err)
	}
	if got := fromRow.Remaining(); got != 4 {
		t.Fatalf("row step_budget remaining = %d, want 4 over profile 9", got)
	}
	fromProfile, err := newJobBudget(0, llm.Config{LoopMaxSteps: 9})
	if err != nil {
		t.Fatal(err)
	}
	if got := fromProfile.Remaining(); got != 9 {
		t.Fatalf("profile remaining = %d, want 9", got)
	}
	fromDefault, err := newJobBudget(0, llm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fromDefault.Remaining(); got != 25 {
		t.Fatalf("default remaining = %d, want builtin 25", got)
	}
}
