package conversations

// What L2.5 leaves behind when it drops.
//
// The ladder was written when Postgres was the only record, so a hard drop was a loss and
// the honest terminal answer was "start a new chat". A second record exists now: the
// projection copies user and assistant prose into the memory graph, where `memory_recall`
// can page it by conversation and anchor. A turn the graph holds is therefore OFFLOADED
// rather than lost — but only the model can act on that, and only if something tells it.
//
// So a drop leaves a pointer, the same shape L1 already uses for a tool output it clears:
// the bytes go, a line saying how to get them back stays. Without it a dropped turn and a
// turn that never happened are indistinguishable from inside the context, which is the
// failure mode worth avoiding — an agent does not go looking for what it cannot tell is
// missing.
//
// The pointer is claimed ONLY up to the projection watermark
// (ContextConfig.ProjectedThroughSeq). A turn above it exists in Postgres alone, and
// pointing at a turn the graph does not hold would be worse than the silent drop it
// replaces: the model would spend a call and come back with nothing, having been told the
// memory had it. An unknown watermark is 0, which claims nothing.
//
// Tool traffic is never covered. The projection filters it out (role IN user/assistant,
// no tool_call_id, no tool_calls), so a dropped tool round has no memory path at all —
// its own path is the sidecar behind read_tool_output, which L1 already points at.

import (
	"fmt"

	"github.com/chetto1983/aura/internal/llm"
)

// offloadMarker tags the synthetic pointer turn in its ToolCallID field, exactly as
// compactionMarker and alwaysBlockMarker do: a field a real persisted user turn never
// populates, so the renderer can emit a clean user-role message.
const offloadMarker = "__aura_offloaded__"

// isOffloadPointer reports whether t is the injected pointer turn.
func isOffloadPointer(t Turn) bool {
	return t.ToolCallID == offloadMarker && t.Role == llm.RoleUser
}

// offloadPointerTurn frames the pointer. It names the conversation and the anchor rather
// than describing the situation, because the only useful form of this sentence is one the
// model can act on without a second thought about which call to make.
func offloadPointerTurn(conversationID string, fromSeq, throughSeq int) Turn {
	return Turn{
		Seq:        throughSeq,
		Role:       llm.RoleUser,
		ToolCallID: offloadMarker,
		Content: fmt.Sprintf(
			"[Turns %d-%d of this conversation are no longer in this context. They were not "+
				"lost: they are in long-term memory. Read them with memory_recall, mode "+
				"\"open\", conversation_id %q, anchor_seq %d, and page on with mode "+
				"\"scroll\" and the cursor it returns. Do this when the answer depends on what "+
				"was said earlier — and do not treat the gap as though nothing was said there.]",
			fromSeq, throughSeq, conversationID, fromSeq),
	}
}

// droppedSpan reports the range of real turn sequences present in before and absent from
// after. Synthetic turns are ignored: the always-block and a compaction summary carry
// seqs that are markers rather than positions in the conversation, and a pointer that
// named one would send the model to an anchor no turn ever had.
func droppedSpan(before, after []Turn) (from, through int, ok bool) {
	kept := make(map[int]struct{}, len(after))
	for _, turn := range after {
		kept[turn.Seq] = struct{}{}
	}
	for _, turn := range before {
		if isAlwaysBlock(turn) || isCompaction(turn) || isOffloadPointer(turn) {
			continue
		}
		if _, survives := kept[turn.Seq]; survives {
			continue
		}
		if !ok || turn.Seq < from {
			from = turn.Seq
		}
		if !ok || turn.Seq > through {
			through = turn.Seq
		}
		ok = true
	}
	return from, through, ok
}

// withOffloadPointer inserts the pointer after the protected head, so it reads as the
// first thing about the conversation body rather than as a turn in the middle of it.
//
// It returns reduced unchanged when there is nothing to claim: no drop, no watermark, or
// a drop that reaches past what the graph holds. Partial claims are deliberately not
// made — "turns 4-9 are in memory, 10-12 are gone" is a sentence that costs more to read
// than the recall it saves.
func withOffloadPointer(conversationID string, before, reduced []Turn, projectedThroughSeq int) []Turn {
	from, through, dropped := droppedSpan(before, reduced)
	if !dropped || projectedThroughSeq <= 0 || through > projectedThroughSeq {
		return reduced
	}
	head, history, active := splitHeadHistoryActive(reduced)
	out := make([]Turn, 0, len(reduced)+1)
	out = append(out, head...)
	out = append(out, offloadPointerTurn(conversationID, from, through))
	out = append(out, history...)
	return append(out, active...)
}
