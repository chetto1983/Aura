package runner

import (
	"context"
	"sync/atomic"
)

// The branch a turn continues, and the seq it started from, travel on the context like the
// thread lock and the turn's attachments. The AG-UI handler knows which branch the client has
// on screen, but it is the runner that appends the turn continuing it. Only the HTTP stream
// reports the seqs back. Widening Turn and its variants would reach every channel; the
// context reaches only the caller that sets it (spec 2026-10-27, D2 and D7).

type branchLeafKey struct{}

// WithBranchLeaf names the last turn of the branch the client has on screen: the user turn
// a run appends continues it. A seq <= 0 stores nothing, and the run continues the turn
// written last.
func WithBranchLeaf(ctx context.Context, seq int) context.Context {
	if seq <= 0 {
		return ctx
	}
	return context.WithValue(ctx, branchLeafKey{}, seq)
}

func branchLeaf(ctx context.Context) int {
	seq, _ := ctx.Value(branchLeafKey{}).(int)
	return seq
}

// TurnHead receives the seq a run started from: the user turn it appended, or the fork it
// re-ran. It stays 0 for a run that started from neither, such as a resume. A caller that
// wants the seq reports it after the run; the runner only writes it.
type TurnHead struct{ seq atomic.Int64 }

// Seq is the head the run recorded, or 0.
func (h *TurnHead) Seq() int { return int(h.seq.Load()) }

type turnHeadKey struct{}

// WithTurnHead asks the run on ctx to record its head in h.
func WithTurnHead(ctx context.Context, h *TurnHead) context.Context {
	return context.WithValue(ctx, turnHeadKey{}, h)
}

// recordTurnHead keeps the first head of the run: a leftover steer delivered after the
// first turn appends another user turn, and the client's message is the first.
func recordTurnHead(ctx context.Context, seq int) {
	if h, ok := ctx.Value(turnHeadKey{}).(*TurnHead); ok && seq > 0 {
		h.seq.CompareAndSwap(0, int64(seq))
	}
}
