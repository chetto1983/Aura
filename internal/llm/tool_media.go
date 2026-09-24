package llm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// MaxToolMediaPerTurn caps the images tools hand the model in one turn: every later
// request of the turn re-sends all of them, so each one is paid for again on every round.
const MaxToolMediaPerTurn = 8

// ErrToolMediaFull is returned by ToolMedia.Add once the turn holds MaxToolMediaPerTurn parts.
var ErrToolMediaFull = errors.New("tool media: turn image limit reached")

// ToolMedia carries the media tools produce during one turn to the provider client, keyed
// by the tool call that produced it, so the client can show it to the model right after
// that call's result. It lives on the turn's context and nowhere else: the transcript, the
// ledger and conversation storage only ever see the tool's text result.
type ToolMedia struct {
	mu    sync.Mutex
	parts map[string][]ProjectedRequestPart
	count int
}

// Add registers part under the tool call that produced it. Tool batches run in parallel,
// so Add is safe for concurrent use.
func (m *ToolMedia) Add(toolCallID string, part ProjectedRequestPart) error {
	if toolCallID == "" {
		return errors.New("tool media: the producing tool call id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.count >= MaxToolMediaPerTurn {
		return fmt.Errorf("%w (%d images per turn)", ErrToolMediaFull, MaxToolMediaPerTurn)
	}
	if m.parts == nil {
		m.parts = map[string][]ProjectedRequestPart{}
	}
	part.Bytes = slices.Clone(part.Bytes)
	m.parts[toolCallID] = append(m.parts[toolCallID], part)
	m.count++
	return nil
}

// Snapshot returns the media added so far by tool call id, each call's parts in the order
// they were added, or nil when there is none. The byte slices are shared: nothing mutates
// them after Add.
func (m *ToolMedia) Snapshot() map[string][]ProjectedRequestPart {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.parts) == 0 {
		return nil
	}
	out := make(map[string][]ProjectedRequestPart, len(m.parts))
	for id, parts := range m.parts {
		out[id] = slices.Clone(parts)
	}
	return out
}

type toolMediaContextKey struct{}

// WithToolMedia returns ctx carrying a fresh ToolMedia. A nested run gets its own, which
// shadows the outer one for everything that run calls.
func WithToolMedia(ctx context.Context) (context.Context, *ToolMedia) {
	media := &ToolMedia{}
	return context.WithValue(ctx, toolMediaContextKey{}, media), media
}

// ToolMediaFromContext returns the ToolMedia of the turn ctx belongs to, or nil outside
// one: toolpipe and the CLI have no model to show media to.
func ToolMediaFromContext(ctx context.Context) *ToolMedia {
	media, _ := ctx.Value(toolMediaContextKey{}).(*ToolMedia)
	return media
}
