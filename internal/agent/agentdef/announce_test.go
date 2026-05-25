package agentdef

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestAnnounce_RendersDisplayNameWhenSet(t *testing.T) {
	got := RenderAnnounce(AnnounceData{Archetype: "summarizer", DisplayName: "Payload Summarizer", ModelOverride: "fast"})
	if !strings.Contains(got, "**Payload Summarizer**") {
		t.Fatalf("announce = %q, want display name", got)
	}
	if !strings.Contains(got, "_(model: fast)_") {
		t.Fatalf("announce = %q, want model override", got)
	}
}

func TestAnnounce_FallsBackToArchetypeID(t *testing.T) {
	got := RenderAnnounce(AnnounceData{Archetype: "summarizer"})
	if !strings.Contains(got, "**summarizer**") {
		t.Fatalf("announce = %q, want archetype fallback", got)
	}
}

func TestScrubber_DropsChildToolCalls(t *testing.T) {
	runner := &recordingDelegateRunner{}
	delegates := WithArchetypeDelegates(DefaultChatArchetype, testDelegateRegistry(nil), runner, []string{DefaultChatArchetype}, nil, nil)
	calls := []llm.ToolCall{
		{Name: "delegate_summarizer", Arguments: map[string]any{"prompt": "summarize"}},
		{Name: "search", Arguments: map[string]any{"query": "hidden child trace"}},
	}
	got := AnnouncementsForCalls(delegates, calls)
	if len(got) != 1 {
		t.Fatalf("announcements = %+v, want only delegate call", got)
	}
	if strings.Contains(got[0], "hidden child trace") || strings.Contains(got[0], "search") {
		t.Fatalf("announce leaked non-delegate/tool-call details: %q", got[0])
	}
}
