// Package steer is the conversation-id-keyed bounded FIFO steer inbox.
//
// It is an in-memory, single-replica-by-construction queue: a multi-replica
// Aura would need cross-replica signalling, not a bigger queue (amendment
// #133's stated boundary). It is keyed on conversation id, never run id,
// because the Telegram dispatch path calls runner.Turn directly with no run
// identity in scope at all (D-01) — a run-id-keyed inbox would be
// unreachable from that half of the callers by construction.
//
// It is NOT internal/agui's RunRegistry: no SSE fan-out, no subscriber set,
// no replay-from-seq. A steer is consumed by Drain, never replayed.
package steer

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// defaultMax and defaultMaxBytes are the package-level fallbacks a
// non-positive Config cap resolves to — the same <=0 → default convention
// agui.NewRunRegistry uses. Deliberately NOT the ratified amendment #132
// item 10 numbers: those live in config.AGUISteerConfig and arrive through
// Config, never as a literal in this file (a D-11-shaped drift guard).
const (
	defaultMax      = 32
	defaultMaxBytes = 32768
)

// Sentinel errors returned by Push, one per validation reason, so a caller
// (the HTTP route in 52-04, the Telegram reply in 52-06) can render each
// without string matching.
var (
	ErrEmpty     = errors.New("steer: message is empty")
	ErrTooLarge  = errors.New("steer: message exceeds max size")
	ErrQueueFull = errors.New("steer: conversation queue is full")
	ErrClosed    = errors.New("steer: inbox is closed")
)

// Config bundles the caps New is configured with — this package's mirror of
// config.AGUISteerConfig's Max/MaxBytes fields, kept separate so this
// package never imports internal/config; the composition root converts.
type Config struct {
	Max      int // queued-message cap per conversation; <=0 resolves to a package default
	MaxBytes int // per-message byte cap (Unicode-encoded byte length, not rune count); <=0 resolves to a package default
}

// Message is one operator steer, carrying the minimum provenance the
// aura.steer echo frame needs: an id, the source channel, and the arrival
// time. A small value type, not an interface.
type Message struct {
	ID      string
	Source  string
	Text    string
	Arrived time.Time
}

// Inbox holds one mutex and one map[string][]Message — the queue itself.
type Inbox struct {
	mu     sync.Mutex
	byConv map[string][]Message
	cfg    Config
	closed bool
	now    func() time.Time
}

// New builds an Inbox from the resolved caps. A non-positive cap falls back
// to the package default — a cap exists to bound memory, not to be silently
// disabled by an unset/zero env value.
func New(cfg Config) *Inbox {
	if cfg.Max <= 0 {
		cfg.Max = defaultMax
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = defaultMaxBytes
	}
	return &Inbox{
		byConv: make(map[string][]Message),
		cfg:    cfg,
		now:    time.Now,
	}
}

// Push enqueues text for conv, tagged with source (the channel that produced
// it — the cockpit HTTP route or the Telegram dispatch path). Validates in
// order — empty/whitespace, oversize, closed, queue-full — returning a
// distinct sentinel for each so a caller never has to string-match. A
// refused Push never touches the queue.
func (i *Inbox) Push(conv, source, text string) error {
	if strings.TrimSpace(text) == "" {
		return ErrEmpty
	}
	// Byte semantics, not runes: the cap bounds memory and wire size, and a
	// rune cap would let a multi-byte body exceed the memory bound it exists
	// to enforce. len(string) in Go already IS the encoded byte length.
	if size := len([]byte(text)); size > i.cfg.MaxBytes {
		return ErrTooLarge
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return ErrClosed
	}
	if len(i.byConv[conv]) >= i.cfg.Max {
		return ErrQueueFull
	}
	i.byConv[conv] = append(i.byConv[conv], Message{
		ID:      uuid.NewString(),
		Source:  source,
		Text:    text,
		Arrived: i.now(),
	})
	return nil
}

// Drain pops and returns everything queued for conv, atomically clearing the
// slot — a second Drain on the same conv returns empty. Deletes the map key
// so an idle conversation leaves no residue behind; a conv never pushed to
// returns a nil (len 0) slice without allocating an entry.
func (i *Inbox) Drain(conv string) []Message {
	i.mu.Lock()
	defer i.mu.Unlock()
	msgs := i.byConv[conv]
	delete(i.byConv, conv)
	return msgs
}

// Close stops accepting new Push calls (which return ErrClosed from then
// on); it does not discard whatever is already queued — Drain after Close
// still returns the pending messages, so shutdown never silently destroys
// operator input.
func (i *Inbox) Close() {
	i.mu.Lock()
	i.closed = true
	i.mu.Unlock()
}
