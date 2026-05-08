package conversation

import (
	"strings"
	"testing"
)

func TestComposeRetrievalCapsuleIncludesRelevantPagesAndEvidence(t *testing.T) {
	got := ComposeRetrievalCapsule(RetrievalCapsuleInput{
		Route:    "retrieve",
		UserText: "come posso migliorare Aura?",
		SearchContext: "Relevant wiki knowledge:\n" +
			"- [[piano-di-miglioramento]] Piano\n  Migliorie possibili\n" +
			"- [[aura-operating-memory]] Aura Memory\n  Stato operativo\n",
		MaxBytes: 4000,
	})

	for _, want := range []string{
		"## Retrieval Capsule",
		"### Route",
		"retrieve",
		"### Relevant Pages",
		"- [[aura-operating-memory]]",
		"- [[piano-di-miglioramento]]",
		"### Evidence",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("retrieval capsule missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"Graph Context", "Recent Wiki Log", "update"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("retrieval capsule contains hot-path residue %q:\n%s", notWant, got)
		}
	}
}

func TestComposeRetrievalCapsuleHonorsBudget(t *testing.T) {
	got := ComposeRetrievalCapsule(RetrievalCapsuleInput{
		SearchContext: strings.Repeat("search ", 1000),
		MaxBytes:      900,
	})
	if len(got) > 900 {
		t.Fatalf("retrieval capsule length = %d, want <= 900", len(got))
	}
	if !strings.HasPrefix(got, "## Retrieval Capsule") {
		t.Fatalf("capsule missing heading: %q", got)
	}
}

func TestComposeRetrievalCapsuleEmptyInput(t *testing.T) {
	if got := ComposeRetrievalCapsule(RetrievalCapsuleInput{}); got != "" {
		t.Fatalf("empty capsule = %q, want empty", got)
	}
}
