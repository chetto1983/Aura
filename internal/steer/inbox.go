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
	"sync"
	"time"

	"github.com/google/uuid"
)

// TODO(RED): stub only — compiles for the pre-commit vet gate (go vet ./...
// runs over the whole tree) but implements nothing yet, matching phase 45-04's
// established RED-commit convention (see 52-02-PLAN.md <no_stale_inputs>).
// GREEN fills in Push/Drain/Close.

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
	MaxBytes int // per-message UTF-8 byte cap; <=0 resolves to a package default
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
	now    func() time.Time
}

// New builds an Inbox from the resolved caps.
func New(cfg Config) *Inbox {
	return &Inbox{
		byConv: make(map[string][]Message),
		cfg:    cfg,
		now:    time.Now,
	}
}

// Push enqueues text for conv, tagged with source.
func (i *Inbox) Push(conv, source, text string) error {
	_ = uuid.NewString // referenced in GREEN
	return nil
}

// Drain pops and returns everything queued for conv.
func (i *Inbox) Drain(conv string) []Message {
	return nil
}

// Close stops accepting new Push calls; queued messages are not discarded.
func (i *Inbox) Close() {
}
