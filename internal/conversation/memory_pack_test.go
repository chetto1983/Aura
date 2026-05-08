package conversation

import (
	"strings"
	"testing"
)

func TestComposeMemoryPackIncludesRelevantPagesGraphAndLog(t *testing.T) {
	got := ComposeMemoryPack(MemoryPackInput{
		UserText: "come posso migliorare Aura?",
		SearchContext: "Relevant wiki knowledge:\n" +
			"- [[piano-di-miglioramento]] Piano\n  Migliorie possibili\n" +
			"- [[aura-operating-memory]] Aura Memory\n  Stato operativo\n",
		GraphContext: "# Wiki Graph Context\n\n- Pages: 22\n- Edges: 53\n\n## Hubs\n- [[davide]] degree=13\n",
		RecentLog:    "| timestamp | action | page |\n| 2026-05-08T10:00:00Z | update | [[aura-operating-memory]] |",
		MaxBytes:     4000,
	})

	for _, want := range []string{
		"## Memory Pack",
		"### Relevant Pages",
		"- [[aura-operating-memory]]",
		"- [[piano-di-miglioramento]]",
		"### Graph Context",
		"- Pages: 22",
		"### Recent Wiki Log",
		"update",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("memory pack missing %q:\n%s", want, got)
		}
	}
}

func TestComposeMemoryPackHonorsBudget(t *testing.T) {
	got := ComposeMemoryPack(MemoryPackInput{
		SearchContext: strings.Repeat("search ", 1000),
		GraphContext:  strings.Repeat("graph ", 1000),
		RecentLog:     strings.Repeat("log ", 1000),
		MaxBytes:      900,
	})
	if len(got) > 900 {
		t.Fatalf("memory pack length = %d, want <= 900", len(got))
	}
	if !strings.HasPrefix(got, "## Memory Pack") {
		t.Fatalf("pack missing heading: %q", got)
	}
}

func TestComposeMemoryPackEmptyInput(t *testing.T) {
	if got := ComposeMemoryPack(MemoryPackInput{}); got != "" {
		t.Fatalf("empty pack = %q, want empty", got)
	}
}
