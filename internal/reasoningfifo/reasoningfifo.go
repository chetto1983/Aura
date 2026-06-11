// Package reasoningfifo is a rune-capped rolling buffer for streamed chain-of-thought
// reasoning text. It keeps the most-recent `max` runes (tail-keep, front-evict) so a
// live CoT display stays bounded without ever splitting a UTF-8 rune — the buffer
// operates on []rune, so eviction always lands on a rune boundary (no mojibake at the
// cap, the failure mode the byte-granular log ring deliberately accepts but a *display*
// cannot). It is NOT safe for concurrent use: the sole caller (the Telegram status
// pane) folds events on one goroutine, so locking would be dead weight.
package reasoningfifo

// FIFO is a fixed-capacity rune window. The zero value is unusable — call New. A nil
// *FIFO is a valid no-op (Push/Reset do nothing, String returns "") so a caller that
// leaves it unset never branches.
type FIFO struct {
	max   int
	runes []rune
}

// New returns a FIFO that retains at most max runes (most-recent kept). A max <= 0
// yields a buffer that never retains anything — Push is a no-op, String is "" — which
// is exactly the "live CoT disabled" behavior, so callers need no separate guard.
func New(max int) *FIFO {
	if max < 0 {
		max = 0
	}
	return &FIFO{max: max}
}

// Push appends delta's runes and, if the window now exceeds max, drops from the FRONT
// back to max so the newest reasoning stays visible. Eviction copies the live tail into
// a fresh slice so the dropped prefix is reclaimed (the backing array never grows past
// the window). A nil receiver, a zero cap, or an empty delta is a no-op.
func (f *FIFO) Push(delta string) {
	if f == nil || f.max == 0 || delta == "" {
		return
	}
	f.runes = append(f.runes, []rune(delta)...)
	if len(f.runes) > f.max {
		f.runes = append([]rune(nil), f.runes[len(f.runes)-f.max:]...)
	}
}

// String returns the current window (oldest-retained → newest). Always valid UTF-8.
func (f *FIFO) String() string {
	if f == nil {
		return ""
	}
	return string(f.runes)
}

// Len returns the window's current rune count.
func (f *FIFO) Len() int {
	if f == nil {
		return 0
	}
	return len(f.runes)
}

// Reset clears the window (turn-start and final-answer boundaries) while keeping the
// capacity, so the next turn reuses the same buffer.
func (f *FIFO) Reset() {
	if f == nil {
		return
	}
	f.runes = f.runes[:0]
}
