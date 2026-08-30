package agui

import (
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
)

func discardEvent() *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{DiscardStreamed: true}}
}

// finalizeEvent mirrors (*LlmAgent).finalizeEvent: the synthesized answer and the
// trip reason ride ONE terminal Event.
func finalizeEvent(text string, delta map[string]any) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		LLMResponse: &agent.LLMResponse{Content: text, FinishReason: "stop"},
		Actions:     agent.Actions{StateDelta: delta}}
}

func discardedMessageID(t *testing.T, ev events.Event) string {
	t.Helper()
	custom, ok := ev.(*events.CustomEvent)
	if !ok || custom.Name != DiscardEventName {
		t.Fatalf("event = %T %v, want CUSTOM %s", ev, ev.Type(), DiscardEventName)
	}
	value, _ := custom.Value.(map[string]any)
	id, _ := value["message_id"].(string)
	return id
}

// The shape measured live on 2026-08-30 (amendment #191): two content-stop drafts
// vetoed by the completion gate, then the budget trip's synthesized answer on the
// terminal Event. Each veto must close its message, name it in an aura.discard
// CUSTOM, and leave the terminal answer free to stream on a fresh message — before
// the fix the drafts coalesced into one message and the answer never reached the wire.
func TestTranslatorDiscardRepudiatesStreamedProse(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		chunk("Il budget è esaurito, bozza uno."),
		discardEvent(),
		chunk("Ecco l'output del primo passo, bozza due."),
		discardEvent(),
		finalizeEvent("Ecco i risultati ottenuti dai passi richiesti.", map[string]any{"limit_hit": "max_steps"}),
	})
	want := []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "CUSTOM",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "CUSTOM",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
		"STATE_DELTA", "RUN_FINISHED",
	}
	got := typesOf(evs)
	if len(got) != len(want) {
		t.Fatalf("types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("types[%d] = %s, want %s (full: %v)", i, got[i], want[i], got)
		}
	}
	if id := discardedMessageID(t, evs[4]); id != "msg-1" {
		t.Fatalf("first discard names %q, want msg-1", id)
	}
	if id := discardedMessageID(t, evs[8]); id != "msg-2" {
		t.Fatalf("second discard names %q, want msg-2", id)
	}
	answer := evs[10].(*events.TextMessageContentEvent)
	if answer.MessageID != "msg-3" || answer.Delta != "Ecco i risultati ottenuti dai passi richiesti." {
		t.Fatalf("terminal answer = %+v, want the synthesized answer on a fresh msg-3", answer)
	}
}

// A repudiation with nothing streamed (an empty completion retried, a tool-call
// round) is silent: no CUSTOM, no dangling END, the next prose opens normally.
func TestTranslatorDiscardWithoutStreamedProseIsSilent(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		discardEvent(),
		chunk("ok"),
		finalChunk("ok", "stop"),
	})
	want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	got := typesOf(evs)
	if len(got) != len(want) {
		t.Fatalf("types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("types[%d] = %s, want %s", i, got[i], want[i])
		}
	}
	if countType(evs, "CUSTOM") != 0 {
		t.Fatal("a discard with no streamed prose emitted a CUSTOM")
	}
}

// The B-12 mid-stream retry: partial chunks, discard, the retry's fresh chunks. The
// repudiated partial is named and the retry streams on its own message.
func TestTranslatorDiscardOnStreamRetry(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		chunk("partial "),
		discardEvent(),
		chunk("fresh answer"),
		finalChunk("fresh answer", "stop"),
	})
	if countType(evs, "CUSTOM") != 1 || countType(evs, "TEXT_MESSAGE_START") != 2 {
		t.Fatalf("types = %v, want one discard and two text messages", typesOf(evs))
	}
	if id := discardedMessageID(t, evs[4]); id != "msg-1" {
		t.Fatalf("discard names %q, want msg-1", id)
	}
}
