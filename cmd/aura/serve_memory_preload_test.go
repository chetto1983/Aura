package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The regression this file exists for. Measured on the live graph 2026-09-04: the
// question "cosa ti arriva dal contesto" recalled five records, every one of them a
// conversation, with fact_count 0 and entity_count 0 -- and the renderer, which read
// only facts and entities, produced nothing. The retrieval had succeeded; the turn
// arrived with no <memory_recall> block and no warning anywhere to say why.
func TestMemoryPreloadCarriesTheConversationLeg(t *testing.T) {
	client := &memoryReadinessClient{text: `{"evidence":[{"kind":"conversation","rank":1,` +
		`"conversation":{"conversation_id":"c1","anchor_seq":2,"turns":[` +
		`{"seq":1,"role":"user","content":"io mio cane si chiama Olaf","occurred_at":"2026-09-01 06:28:11.917"},` +
		`{"seq":2,"role":"assistant","content":"Aggiornato: il tuo cane si chiama Olaf.","occurred_at":"2026-09-01 06:28:14.100"}]}}],` +
		`"facts":[],"retrieval":{"abstained":false,"fact_count":0,"entity_count":0}}`}

	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).
		Search(context.Background(), "identity-a", "come si chiama il cane")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got == "" {
		t.Fatal("a recall that returned only conversations rendered an empty block")
	}
	// The anchor turn, not the whole window: seq 1 is context the tools return in full.
	if !strings.Contains(got, "Aggiornato: il tuo cane si chiama Olaf.") {
		t.Fatalf("anchor turn missing: %q", got)
	}
	if strings.Contains(got, "io mio cane si chiama Olaf") {
		t.Fatalf("whole window inlined instead of the anchor: %q", got)
	}
	// Said, not known. A turn that reads like a fact invites the model to quote a
	// question back as though it were an answer.
	if !strings.Contains(got, "said [2026-09-01 assistant]") {
		t.Fatalf("turn is not marked as something said: %q", got)
	}
	if strings.Contains(got, "06:28") {
		t.Fatalf("clock time survived into the block: %q", got)
	}
}

// Rank order is the ranking's whole output; grouping the block by kind would throw it
// away and put a 0.40 fact above a 0.73 conversation.
func TestMemoryPreloadKeepsRankOrder(t *testing.T) {
	client := &memoryReadinessClient{text: `{"evidence":[` +
		`{"kind":"conversation","rank":1,"conversation":{"anchor_seq":1,"turns":[` +
		`{"seq":1,"role":"user","content":"prima","occurred_at":"2026-09-01 06:00:00"}]}},` +
		`{"kind":"fact","rank":2,"fact":{"statement":"dopo"}}],"retrieval":{"abstained":false}}`}

	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).
		Search(context.Background(), "identity-a", "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if first, second := strings.Index(got, "prima"), strings.Index(got, "dopo"); first < 0 || second < 0 || first > second {
		t.Fatalf("rank order lost: %q", got)
	}
}

// The block rides in EVERY turn, so both the number of turns and the length of each
// one have to be bounded or the preload becomes the context budget.
func TestMemoryPreloadBoundsTheConversationLeg(t *testing.T) {
	long := strings.Repeat("parola ", 200)
	windows := make([]string, 0, preloadConversationLines+2)
	for index := range preloadConversationLines + 2 {
		windows = append(windows, `{"kind":"conversation","rank":`+string(rune('1'+index))+
			`,"conversation":{"anchor_seq":1,"turns":[{"seq":1,"role":"user","content":"`+
			long+`","occurred_at":"2026-09-01 06:00:00"}]}}`)
	}
	client := &memoryReadinessClient{text: `{"evidence":[` + strings.Join(windows, ",") +
		`],"retrieval":{"abstained":false}}`}

	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).
		Search(context.Background(), "identity-a", "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if lines := strings.Count(got, "- said ["); lines != preloadConversationLines {
		t.Fatalf("turns = %d, want %d: %q", lines, preloadConversationLines, got)
	}
	for line := range strings.SplitSeq(got, "\n") {
		if runes := []rune(line); len(runes) > preloadTurnRunes+64 {
			t.Fatalf("turn line is %d runes, unbounded: %q", len(runes), line)
		}
		if !strings.HasSuffix(line, "…") {
			t.Fatalf("long turn was not cut: %q", line)
		}
	}
}

// A window whose anchor_seq names no turn it carries still has to render something:
// dropping it silently is the failure this whole file was written for.
func TestMemoryPreloadFallsBackWhenTheAnchorIsMissing(t *testing.T) {
	client := &memoryReadinessClient{text: `{"evidence":[{"kind":"conversation",` +
		`"conversation":{"anchor_seq":99,"turns":[` +
		`{"seq":1,"role":"user","content":"primo","occurred_at":"2026-09-01 06:00:00"},` +
		`{"seq":2,"role":"assistant","content":"ultimo","occurred_at":"2026-09-01 06:00:01"}]}}],` +
		`"retrieval":{"abstained":false}}`}

	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).
		Search(context.Background(), "identity-a", "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(got, "ultimo") {
		t.Fatalf("missing anchor dropped the window: %q", got)
	}
}

// An evidence record carrying neither leg is skipped without spending one of the
// bounded conversation slots on a blank line.
func TestMemoryPreloadSkipsEmptyEvidence(t *testing.T) {
	client := &memoryReadinessClient{text: `{"evidence":[` +
		`{"kind":"conversation"},{"kind":"conversation","conversation":{"anchor_seq":1,"turns":[]}},` +
		`{"kind":"fact","fact":{"statement":"   "}},` +
		`{"kind":"conversation","conversation":{"anchor_seq":1,"turns":[` +
		`{"seq":1,"role":"user","content":"vero","occurred_at":"2026-09-01 06:00:00"}]}}],` +
		`"retrieval":{"abstained":false}}`}

	got, err := newMemoryContextProvider(client.mount(t, "identity-a"), 5, time.Second).
		Search(context.Background(), "identity-a", "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != "- said [2026-09-01 user] vero" {
		t.Fatalf("preload = %q", got)
	}
}
