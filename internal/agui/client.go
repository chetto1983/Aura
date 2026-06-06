package agui

import (
	"context"
	"iter"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
)

// Event is the AG-UI event surface re-exported under the agui package so in-process
// consumers (e.g. the Phase-13 Telegram channel) reference agui.Event, not the external
// events.Event — keeping github.com/ag-ui-protocol/... out of every call site (PRD
// Slice 8: "Type aliases per evitare leak del package esterno nei call sites"). Only the
// subscriber surface is aliased; the SDK is not blanket re-exported.
type Event = events.Event

// EventType is the aliased AG-UI event discriminator (agui.Event.Type() returns it),
// re-exported alongside Event so a consumer can switch on event kind without importing
// the SDK.
type EventType = events.EventType

// Subscribe composes Plan 01's Translate with a single-subscriber Fanout and returns a
// receive-only channel of AG-UI events for ONE in-process consumer of an agent turn.
// Given the threadID/runID, an IDGenerator, and the agent's iter.Seq2[*agent.Event,
// error] turn stream, it translates the turn and pumps it through the cap-64,
// drop-on-full, ctx-cancel-clean Fanout — the same backpressure + leak discipline a
// future Telegram channel consumes (amendment #35 agui_subscriber.go seam). The channel
// closes when the turn ends or ctx is cancelled.
//
// This seam is transport-free: no HTTP, no SSE (that is server.go in a later plan). For
// N concurrent consumers, build a *Fanout directly and Subscribe() per consumer; this
// helper is the single-consumer convenience over that same pump.
//
// Reasoning forward-compat: the translated stream may later carry REASONING_* events
// (the dual-field reasoning/reasoning_content data-plane, amendment #57). This phase only
// surfaces the alias — no reasoning parsing is added here.
func Subscribe(ctx context.Context, threadID, runID string, idgen IDGenerator, turn iter.Seq2[*agent.Event, error]) <-chan Event {
	f := NewFanout(Translate(threadID, runID, idgen, turn))
	ch := f.Subscribe()
	f.Run(ctx)
	return ch
}
