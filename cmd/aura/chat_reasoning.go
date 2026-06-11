// chat_reasoning.go holds the REPL's bounded live chain-of-thought renderer, split
// out of chat_render.go (refactor-on-touch, ≤600 LOC). It replaces the original
// append-only dim stream (amendment #57e), which grew unbounded and never cleared,
// with a rune-capped in-place rolling line that mirrors the Telegram status pane's
// window for cross-surface parity (.planning/spikes/cot-reasoning-fifo.md).
package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/reasoningfifo"
)

// cliReasoningWidth bounds the VISIBLE rolling reasoning line to one physical line on
// a narrow terminal — the in-place carriage-return redraw assumes no soft-wrap. The
// 💭 prefix eats a few columns; the rest is the most-recent reasoning runes.
const cliReasoningWidth = 72

// cliReasoning renders a live chain-of-thought as a SINGLE in-place dim line: a
// rune-capped rolling tail (reusing internal/reasoningfifo, so eviction never splits a
// multibyte rune) redrawn per delta with carriage-return + clear-to-end-of-line, then
// erased when the reasoning phase ends so the answer prints clean. It only fires when
// reasoning deltas actually arrive — i.e. when AURA_SHOW_REASONING made the adaptive
// policy request the reasoning UNEXCLUDED — so the default (redacted) REPL is byte-for-
// byte unchanged: push is never called, clear is a no-op.
type cliReasoning struct {
	fifo   *reasoningfifo.FIFO
	active bool
}

func newCLIReasoning() *cliReasoning {
	return &cliReasoning{fifo: reasoningfifo.New(cliReasoningWidth)}
}

// push appends a reasoning delta and redraws the in-place rolling window (dim, 💭
// prefixed, whitespace-collapsed so a multi-line blob stays one rolling line).
func (r *cliReasoning) push(w io.Writer, delta string) {
	r.fifo.Push(delta)
	window := strings.Join(strings.Fields(r.fifo.String()), " ")
	_, _ = fmt.Fprintf(w, "\r\x1b[2m💭 %s\x1b[0m\x1b[K", window)
	r.active = true
}

// clear erases the in-place reasoning line and resets the window so the following
// answer or tool-activity output starts at column 0. A no-op when no reasoning line is
// currently shown, so callers can invoke it unconditionally before non-reasoning output.
func (r *cliReasoning) clear(w io.Writer) {
	if !r.active {
		return
	}
	_, _ = io.WriteString(w, "\r\x1b[K")
	r.fifo.Reset()
	r.active = false
}
