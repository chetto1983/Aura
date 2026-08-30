package agenttest

import (
	"context"
	"sync"

	"github.com/chetto1983/aura/internal/llm"
)

// FakeClient is a deterministic llm.Client for driving LlmAgent.Run tests without
// the network (D-31). Each call to Stream pops the next scripted turn: either a
// canned []llm.Chunk (delivered on a fresh channel that closes after the last
// chunk) or a synchronous Stream error (a REAL infra failure — D-15, the iter.Seq2
// error slot). It records every llm.Request it received so tests can assert
// message-history threading and immutability.
//
// It implements llm.Client; the channel it returns is fully buffered and pre-closed
// so a consumer that ranges to completion never blocks and no goroutine is spawned
// (goleak-clean by construction).
type FakeClient struct {
	mu sync.Mutex

	// Turns is the script: one entry consumed per Stream call, in order. A turn's
	// Err (when non-nil) makes Stream return (nil, Err); otherwise Stream returns a
	// channel carrying Chunks then closing.
	Turns []FakeTurn

	// Requests captures each req passed to Stream, in call order (deep enough for
	// immutability + history-threading assertions).
	Requests []llm.Request

	next int
}

// FakeTurn is one scripted Stream response.
type FakeTurn struct {
	Chunks []llm.Chunk
	Err    error
}

var _ llm.Client = (*FakeClient)(nil)

// NewFakeClient builds a FakeClient from an ordered script of turns.
func NewFakeClient(turns ...FakeTurn) *FakeClient {
	return &FakeClient{Turns: turns}
}

// Stream pops the next scripted turn. When the script is exhausted it returns a
// closed empty channel (a no-tool-call, no-content turn) so a misbehaving loop
// terminates instead of blocking. The captured request is a copy of req with a
// cloned Messages slice so a later in-place mutation by the agent cannot
// retroactively corrupt the recorded snapshot.
func (f *FakeClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	snap := req
	snap.Messages = append([]llm.Message(nil), req.Messages...)
	f.Requests = append(f.Requests, snap)

	var turn FakeTurn
	if f.next < len(f.Turns) {
		turn = f.Turns[f.next]
	}
	f.next++

	if turn.Err != nil {
		return nil, turn.Err
	}

	ch := make(chan llm.Chunk, len(turn.Chunks)+1)
	for _, c := range turn.Chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// CallCount reports how many times Stream was invoked.
func (f *FakeClient) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.next
}

// RecordedRequests returns a snapshot of every request the fake has seen, taken under the
// same lock Stream writes with. A caller that ranged over the exported Requests field
// directly would race any background worker still using this client -- the auto-title
// worker is exactly such a caller.
func (f *FakeClient) RecordedRequests() []llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llm.Request(nil), f.Requests...)
}

// LastRequest returns the most recent recorded request, or the zero request when
// Stream has not been called yet.
func (f *FakeClient) LastRequest() llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Requests) == 0 {
		return llm.Request{}
	}
	return f.Requests[len(f.Requests)-1]
}

// TextChunks is a helper that builds a turn streaming raw assistant content
// followed by a finish_reason — NOT a text_response tool call. Used for the
// content-stop fallback path (D-13/D-16) and length-truncation (D-21).
func TextChunks(finish string, parts ...string) FakeTurn {
	chunks := make([]llm.Chunk, 0, len(parts)+1)
	for _, p := range parts {
		chunks = append(chunks, llm.Chunk{Text: p})
	}
	chunks = append(chunks, llm.Chunk{FinishReason: finish})
	return FakeTurn{Chunks: chunks}
}

// ReasoningChunks builds a turn that streams ONLY reasoning deltas and then a
// finish_reason — the shape of a thinking model whose output budget ran out before
// any answer (amendment #189: finish "length", no text, no tool calls).
func ReasoningChunks(finish string, parts ...string) FakeTurn {
	chunks := make([]llm.Chunk, 0, len(parts)+1)
	for _, p := range parts {
		chunks = append(chunks, llm.Chunk{Reasoning: p})
	}
	chunks = append(chunks, llm.Chunk{FinishReason: finish})
	return FakeTurn{Chunks: chunks}
}

// TextThenErr builds a turn that streams partial assistant content and then a
// terminal stream Err chunk. Consumers must not treat the partial text as a
// complete answer.
func TextThenErr(err error, parts ...string) FakeTurn {
	chunks := make([]llm.Chunk, 0, len(parts)+1)
	for _, p := range parts {
		chunks = append(chunks, llm.Chunk{Text: p})
	}
	chunks = append(chunks, llm.Chunk{Err: err})
	return FakeTurn{Chunks: chunks}
}

// ToolCallTurn builds a turn whose chunks emit the given finalized tool calls then
// a finish_reason "tool_calls". Each call must already carry its ID + Function.
func ToolCallTurn(calls ...llm.ToolCall) FakeTurn {
	chunks := make([]llm.Chunk, 0, len(calls)+1)
	for i := range calls {
		c := calls[i]
		chunks = append(chunks, llm.Chunk{ToolCall: &c})
	}
	chunks = append(chunks, llm.Chunk{FinishReason: "tool_calls"})
	return FakeTurn{Chunks: chunks}
}

// WithUsage appends a trailing Usage chunk to a turn (mirrors the real client's
// final usage chunk) so span-attr assertions see non-zero token counts.
func WithUsage(turn FakeTurn, u llm.Usage) FakeTurn {
	uu := u
	turn.Chunks = append(turn.Chunks, llm.Chunk{Usage: &uu})
	return turn
}

// MakeToolCall is a small constructor for a finalized llm.ToolCall.
func MakeToolCall(id, name, args string) llm.ToolCall {
	var tc llm.ToolCall
	tc.ID = id
	tc.Type = "function"
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}
