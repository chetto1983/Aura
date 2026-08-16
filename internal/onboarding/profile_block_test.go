package onboarding

import (
	"strings"
	"testing"
)

// The block goes into messages[1] on every turn, so what it does with a HALF-filled form
// matters more than what it does with a complete one: an operator who typed a name and
// nothing else must not have the model told that they have no company, no goals and no
// vetoes.

func TestRenderProfileBlockOmitsEmptyFields(t *testing.T) {
	t.Parallel()
	block := RenderProfileBlock(Answers{Name: "Davide", Timezone: "Europe/Rome"})

	if !strings.Contains(block, "Name: Davide") || !strings.Contains(block, "Timezone: Europe/Rome") {
		t.Fatalf("block dropped an answered field:\n%s", block)
	}
	for _, absent := range []string{"Company", "Role", "Goals", "Never do"} {
		if strings.Contains(block, absent) {
			t.Errorf("block states %q for an unanswered field:\n%s", absent, block)
		}
	}
	if !strings.HasPrefix(block, "<operator_profile>") || !strings.HasSuffix(block, "</operator_profile>") {
		t.Errorf("block is not delimited, so nothing can strip it deterministically:\n%s", block)
	}
}

// An empty form renders nothing at all — an empty tag pair would still cost the always-block
// its cache-stable bytes and tell the model there is a profile when there is none.
func TestRenderProfileBlockEmptyAnswersRenderNothing(t *testing.T) {
	t.Parallel()
	for name, a := range map[string]Answers{
		"zero":       {},
		"whitespace": {Name: "   ", Role: "\t", Expertise: []string{"", "  "}},
	} {
		if got := RenderProfileBlock(a); got != "" {
			t.Errorf("%s answers rendered %q, want empty", name, got)
		}
	}
}

// Vetoes are the only prohibitions in the block. They go last, under their own heading, so
// a hard rule cannot be read as one more preference in a list of interests.
func TestRenderProfileBlockPutsVetoesLastAndProhibitive(t *testing.T) {
	t.Parallel()
	block := RenderProfileBlock(Answers{
		Name:      "Davide",
		Interests: []string{"vela"},
		Vetoes:    []string{"non scrivere email al mio posto", "  "},
	})

	never := strings.Index(block, "Never do: non scrivere email al mio posto")
	if never < 0 {
		t.Fatalf("veto missing or not prohibitive:\n%s", block)
	}
	if never < strings.Index(block, "Interests:") {
		t.Errorf("vetoes must come after the descriptive fields:\n%s", block)
	}
	if strings.Contains(block, "Never do: non scrivere email al mio posto, ") {
		t.Errorf("a blank veto rendered a trailing separator the model will imitate:\n%s", block)
	}
}

// The list fields keep their own separator handling: a half-filled list must not leak the
// blanks the form left behind.
func TestRenderProfileBlockJoinsListsWithoutBlanks(t *testing.T) {
	t.Parallel()
	block := RenderProfileBlock(Answers{Stack: []string{"Go", "", "  ", "Postgres"}})
	if !strings.Contains(block, "Stack: Go, Postgres") {
		t.Fatalf("list join kept blanks:\n%s", block)
	}
}

// The nil store is a real deployment shape (no Postgres), and every method must degrade to
// "no profile" rather than panic on the turn path.
func TestNilProfileStoreDegradesQuietly(t *testing.T) {
	t.Parallel()
	var s *ProfileStore
	ctx := t.Context()

	if got := s.ProfileBlock(ctx, "id"); got != "" {
		t.Errorf("ProfileBlock on a nil store = %q, want empty", got)
	}
	if got := s.Timezone(ctx, "id"); got != "" {
		t.Errorf("Timezone on a nil store = %q, want empty", got)
	}
	if err := s.Save(ctx, "id", Answers{Name: "x"}); err != nil {
		t.Errorf("Save on a nil store = %v, want nil", err)
	}
	if err := s.StoreConfirmed(ctx, "id", Answers{Name: "x"}); err != nil {
		t.Errorf("StoreConfirmed on a nil store = %v, want nil", err)
	}
	if err := s.StoreSkipped(ctx, "id"); err != nil {
		t.Errorf("StoreSkipped on a nil store = %v, want nil", err)
	}
	if err := s.MarkNudged(ctx, "id"); err != nil {
		t.Errorf("MarkNudged on a nil store = %v, want nil", err)
	}
	st, err := s.Status(ctx, "id")
	if err != nil || st.Completed || st.Skipped || st.Nudged {
		t.Errorf("Status on a nil store = %+v, %v; want the zero gate and no error", st, err)
	}
	answers, ok, err := s.Load(ctx, "id")
	if ok || err != nil || answers.Name != "" {
		t.Errorf("Load on a nil store = %+v, %v, %v; want empty/false/nil", answers, ok, err)
	}
}
