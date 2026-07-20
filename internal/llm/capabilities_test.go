package llm

import "testing"

func TestFallbackUpperBound(t *testing.T) {
	if got := ConservativeTokenUpperBound(100); got != 371 {
		t.Fatalf("got %d", got)
	}
}
