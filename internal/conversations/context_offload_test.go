package conversations

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// offloadTurns builds a head plus count user/assistant rounds, seq 1..n.
func offloadTurns(count int) []Turn {
	turns := []Turn{{Seq: 1, Role: llm.RoleSystem, Content: "system"}}
	seq := 2
	for range count {
		turns = append(turns,
			Turn{Seq: seq, Role: llm.RoleUser, Content: "ask " + strconv.Itoa(seq)},
			Turn{Seq: seq + 1, Role: llm.RoleAssistant, Content: "answer " + strconv.Itoa(seq+1)})
		seq += 2
	}
	return turns
}

// A drop the graph can serve leaves a pointer the model can act on. Without one, a turn
// that was dropped and a turn that never happened look identical from inside the context,
// and an agent does not go looking for what it cannot tell is missing.
func TestOffloadPointerNamesTheDroppedSpanAndHowToReadIt(t *testing.T) {
	before := offloadTurns(4)
	reduced := append([]Turn{before[0]}, before[5:]...) // drops seq 2..5

	got := withOffloadPointer("conv-1", before, reduced, 9)

	if len(got) != len(reduced)+1 {
		t.Fatalf("turns = %d, want one pointer added to %d", len(got), len(reduced))
	}
	// After the protected head, so it reads as the first thing about the body.
	pointer := got[1]
	if !isOffloadPointer(pointer) {
		t.Fatalf("turn after the head is not the pointer: %+v", pointer)
	}
	for _, want := range []string{"2-5", "memory_recall", "conv-1", "anchor_seq 2"} {
		if !strings.Contains(pointer.Content, want) {
			t.Errorf("pointer does not carry %q: %s", want, pointer.Content)
		}
	}
	if got[0].Role != llm.RoleSystem {
		t.Errorf("the system head moved: %+v", got[0])
	}
}

// The claim is bounded by the watermark. Pointing at a turn the graph never received
// would be worse than the silent drop it replaces: the model spends a recall and comes
// back empty, having been told the memory had it.
func TestOffloadPointerStaysSilentBeyondTheWatermark(t *testing.T) {
	before := offloadTurns(4)
	reduced := append([]Turn{before[0]}, before[5:]...) // drops seq 2..5

	for name, watermark := range map[string]int{
		"nothing projected":  0,
		"projected part way": 4,
		"watermark unknown":  -1,
	} {
		got := withOffloadPointer("conv-1", before, reduced, watermark)
		if len(got) != len(reduced) {
			t.Errorf("%s: a pointer was added for turns the graph may not hold", name)
		}
	}
	// Exactly at the watermark is a claim the graph can keep.
	if got := withOffloadPointer("conv-1", before, reduced, 5); len(got) != len(reduced)+1 {
		t.Error("a drop ending exactly at the watermark must still be pointed at")
	}
}

// Nothing dropped, nothing to say.
func TestOffloadPointerIsAbsentWhenNothingWasDropped(t *testing.T) {
	before := offloadTurns(3)
	if got := withOffloadPointer("conv-1", before, before, 99); len(got) != len(before) {
		t.Fatalf("turns = %d, want the input untouched", len(got))
	}
}

// The synthetic turns carry seqs that are markers, not positions. A pointer that named
// one would send the model to an anchor no turn ever had.
func TestDroppedSpanIgnoresSyntheticTurns(t *testing.T) {
	before := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "system"},
		{Seq: alwaysBlockSeq, Role: llm.RoleUser, ToolCallID: alwaysBlockMarker, Content: "block"},
		{Seq: 40, Role: llm.RoleUser, ToolCallID: compactionMarker, Content: "summary"},
		{Seq: 7, Role: llm.RoleUser, Content: "ask"},
		{Seq: 8, Role: llm.RoleAssistant, Content: "answer"},
	}
	after := []Turn{before[0], before[1]}

	from, through, ok := droppedSpan(before, after)
	if !ok || from != 7 || through != 8 {
		t.Fatalf("dropped span = %d..%d ok=%v, want the real turns 7..8", from, through, ok)
	}
}
