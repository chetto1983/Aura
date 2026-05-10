package agentruntime

import (
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation"
)

func TestSessionStoreBeginCreatesContextAndMarksActive(t *testing.T) {
	store := NewSessionStore()

	session, loaded := store.Begin("user-1", conversation.Config{MaxMessages: 4})
	if loaded {
		t.Fatal("Begin loaded = true, want false for first session")
	}
	if session.Conversation() == nil {
		t.Fatal("Conversation() = nil, want context")
	}
	if !store.IsActive("user-1") {
		t.Fatal("user should be active after Begin")
	}
	if got, ok := store.Load("user-1"); !ok || got != session.Conversation() {
		t.Fatalf("Load() = (%p, %v), want active context", got, ok)
	}
}

func TestSessionStoreBeginReusesExistingContext(t *testing.T) {
	store := NewSessionStore()
	first, _ := store.Begin("user-1", conversation.Config{MaxMessages: 4})
	first.Conversation().AddUserMessage("first")
	first.Finish()

	second, loaded := store.Begin("user-1", conversation.Config{MaxMessages: 4})
	if !loaded {
		t.Fatal("Begin loaded = false, want true for existing context")
	}
	if second.Conversation() != first.Conversation() {
		t.Fatal("Begin created a new context instead of reusing existing one")
	}
	if got := second.Conversation().Messages(); len(got) != 1 || got[0].Content != "first" {
		t.Fatalf("messages = %+v, want preserved first user message", got)
	}
}

func TestSessionFinishAndAbortClearActiveMarker(t *testing.T) {
	store := NewSessionStore()
	session, _ := store.Begin("user-1", conversation.Config{})
	session.Finish()
	if store.IsActive("user-1") {
		t.Fatal("user should not be active after Finish")
	}

	session, _ = store.Begin("user-1", conversation.Config{})
	session.Abort()
	if store.IsActive("user-1") {
		t.Fatal("user should not be active after Abort")
	}
}

func TestSessionStoreClearDeletesConversationActiveAndSnapshot(t *testing.T) {
	store := NewSessionStore()
	session, loaded := store.Begin("123", conversation.Config{})
	if loaded {
		t.Fatal("loaded = true, want false")
	}
	session.Conversation().AddUserMessage("ciao")
	store.StoreSnapshot("123", Snapshot{PromptVersion: "v-test"})
	if !store.IsActive("123") {
		t.Fatal("active = false, want true")
	}

	store.Clear("123")

	if _, ok := store.Load("123"); ok {
		t.Fatal("Load after Clear ok = true, want false")
	}
	if store.IsActive("123") {
		t.Fatal("IsActive after Clear = true, want false")
	}
	if _, ok := store.Snapshot("123"); ok {
		t.Fatal("Snapshot after Clear ok = true, want false")
	}
}

func TestSessionStoreStoresAndPrunesSnapshots(t *testing.T) {
	store := NewSessionStore()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	store.StoreSnapshot("old", Snapshot{StoredAt: now.Add(-8 * 24 * time.Hour), Toolset: "default"})
	store.StoreSnapshot("fresh", Snapshot{
		StoredAt:            now.Add(-6 * 24 * time.Hour),
		PromptVersion:       "aura-agent-v1",
		PromptHash:          "abc123",
		Toolset:             "document",
		ToolsetSelectReason: "document cues",
		ToolsExposed:        []string{"create_docx"},
		ToolsCalled:         []string{"create_docx"},
		ReadSkills:          []string{"docx"},
		LoopSteps:           1,
		LLMCalls:            1,
		ToolCalls:           1,
		TerminalTool:        "create_docx",
		TokensTotal:         42,
	})

	got, ok := store.Snapshot("fresh")
	if !ok {
		t.Fatal("Snapshot() missing fresh snapshot")
	}
	if got.Toolset != "document" || got.TerminalTool != "create_docx" || got.LLMCalls != 1 || got.ToolCalls != 1 {
		t.Fatalf("Snapshot() = %+v, want stored route/tool counters", got)
	}
	if len(got.ToolsExposed) != 1 || got.ToolsExposed[0] != "create_docx" {
		t.Fatalf("ToolsExposed = %+v", got.ToolsExposed)
	}

	store.PruneSnapshots(now, 7)
	if _, ok := store.Snapshot("old"); ok {
		t.Fatal("old snapshot survived retention pruning")
	}
	if _, ok := store.Snapshot("fresh"); !ok {
		t.Fatal("fresh snapshot was pruned")
	}
}
