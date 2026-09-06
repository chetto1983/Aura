package runner

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestCollapseJoinedStatesOneCauseOnce reproduces the shape measured on a live stack: one
// missing ArcadeDB admin credential, ten identities, the same sentence ten times with a
// different UUID in front, once a minute. The collapse has to say how wide the failure is,
// what it is, and keep one whole part so nothing a reader needs is thrown away.
func TestCollapseJoinedStatesOneCauseOnce(t *testing.T) {
	ids := []string{
		"00000000-0000-0000-0000-000000000001",
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	}
	parts := make([]error, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Errorf(
			"conversation projection identity %s: memory for %s: arcadedb: ensure memory schema: arcadedb: http 403: User/Password not valid",
			id, id))
	}
	got := collapseJoined(errors.Join(parts...))

	if !strings.HasPrefix(got, "3 failures share one cause: ") {
		t.Fatalf("the count must lead, got %q", got)
	}
	if !strings.Contains(got, "http 403: User/Password not valid") {
		t.Fatalf("the shared cause must survive, got %q", got)
	}
	if !strings.Contains(got, "example: conversation projection identity "+ids[0]) {
		t.Fatalf("one whole part must be kept as the example, got %q", got)
	}
	// The win is volume: the UUID of every OTHER identity is gone, so one cause reads as one
	// sentence instead of three.
	for _, id := range ids[1:] {
		if strings.Contains(got, id) {
			t.Fatalf("identity %s should not appear once the cause is shared: %q", id, got)
		}
	}
}

// TestCollapseJoinedKeepsUnrelatedFailuresWhole: collapsing is only safe when there IS a
// shared cause. Different failures must be reported in full, or the summary would hide the
// second problem behind the first.
func TestCollapseJoinedKeepsUnrelatedFailuresWhole(t *testing.T) {
	got := collapseJoined(errors.Join(
		errors.New("conversation projection identity a: arcadedb: http 403: User/Password not valid"),
		errors.New("conversation projection identity b: postgres: connection refused"),
	))
	if !strings.Contains(got, "connection refused") || !strings.Contains(got, "403") {
		t.Fatalf("unrelated failures must both survive, got %q", got)
	}
	if strings.Contains(got, "share one cause") {
		t.Fatalf("unrelated failures must not be presented as one cause, got %q", got)
	}
}

func TestCollapseJoinedPassesThroughSimpleErrors(t *testing.T) {
	if got := collapseJoined(nil); got != "" {
		t.Fatalf("nil must render empty, got %q", got)
	}
	single := errors.New("just the one problem")
	if got := collapseJoined(single); got != single.Error() {
		t.Fatalf("a plain error must pass through verbatim, got %q", got)
	}
	if got := collapseJoined(errors.Join(single)); got != single.Error() {
		t.Fatalf("a one-part join must pass through verbatim, got %q", got)
	}
}
