package agui

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// runsession.go carries the per-run resumable event buffer of the detached
// /agent/run path (fix-plan 1.3 Tier B, docs/audit/sse-resume-design-1.3b-2026-07-23.md
// §2.1). RunSession is a SIBLING of Fanout, not a replacement (§2.2): Fanout is the
// in-process subscribe-BEFORE-Run seam (Telegram, client.go) with no history — its
// Subscribe-after-Run panics (WR-06) because the producer snapshots subscribers once.
// Resume is definitionally subscribe-AFTER-run-started plus replay-from-history, the
// exact contract Fanout forbids, so it lives here instead. The two share the
// lifecycle classification (isLifecycleFrame) and the drop policy (pumpDeliver,
// server_sse.go) so the disciplines cannot drift.

// defaultRunBufferEvents mirrors the AURA_AGUI_RUN_BUFFER_EVENTS config default
// (design §2.4: 2048 × ~512 B ≈ 1 MiB per run worst-case). Named here — the
// fanoutBuffer convention — so a non-positive resolved knob falls back rather than
// allocating a zero-cap ring.
const defaultRunBufferEvents = 2048

// seqEvent is one translated, ALREADY-REDACTED AG-UI event with its wire id.
// Redaction happens at append-time on the detached path (design §6.3): the session
// stores what it is given and never widens what the live stream showed.
type seqEvent struct {
	Seq int64        // 1-based, strictly monotonic per run — the SSE `id:` value
	Ev  events.Event // post-Translate, post-redactEvent frame
}

// runSubscriber pairs a subscriber's delivery channel with its abandon signal.
// gone unblocks a lifecycle send stuck on THIS subscriber's full channel when the
// viewer unsubscribes: the detached producer ctx does not cancel on client
// disconnect, so without gone an abandoned full channel would wedge append's
// blocking lifecycle send until the wallclock cap (the request-ctx escape pumpSend
// relies on does not exist here).
type runSubscriber struct {
	ch   chan seqEvent
	gone chan struct{}
}

// RunSession is one detached run's identity, replay ring, and live-subscriber set
// (design §2.1). The producer goroutine is the sole appender and the sole caller of
// finish; subscribers attach at any time via subscribeFrom. All mutable state is
// guarded by mu — append and subscribeFrom serialize on it, which is what makes
// replay-then-live gapless and duplicate-free by construction.
type RunSession struct {
	RunID      string
	ThreadID   string
	IdentityID string // owner captured at start (scopedIdentityID) — resume gate, §3.2

	mu         sync.Mutex
	ring       []seqEvent // fixed-cap ring; the oldest entry is overwritten when full
	head       int        // ring write index
	nextSeq    int64      // next seq to assign (1-based)
	firstSeq   int64      // oldest seq still in the ring (replay-gap detection)
	subs       map[int]runSubscriber
	nextSubID  int
	terminal   bool
	finishedAt time.Time

	// subBuffer is each subscriber channel's LIVE headroom beyond its replay
	// snapshot (the ServerConfig.BufferCap resolution — same drop-on-full cap as the
	// per-connection SSE pump, T-12-09 preserved for resumers).
	subBuffer int
	// cleanup is the run-end callback (§1.2): thread-lock release + registry
	// byThread removal. finish invokes it exactly once, after releasing mu.
	cleanup func()
	// cancel is the wallclock/explicit-cancel handle over the detached run ctx
	// (§1.1): fired by the cancel endpoint (RS-05) and by RunRegistry.Close at
	// daemon shutdown. Set by RunRegistry.Start before the session is published;
	// immutable afterwards.
	cancel context.CancelFunc
	now    func() time.Time
}

// newRunSession builds a session with a fixed-cap ring. Non-positive caps fall back
// to the design defaults (ring: defaultRunBufferEvents; subscriber headroom:
// fanoutBuffer) mirroring bufferCap's convention.
func newRunSession(runID, threadID, identityID string, ringCap, subBuffer int, cleanup func()) *RunSession {
	if ringCap <= 0 {
		ringCap = defaultRunBufferEvents
	}
	if subBuffer <= 0 {
		subBuffer = fanoutBuffer
	}
	return &RunSession{
		RunID:      runID,
		ThreadID:   threadID,
		IdentityID: identityID,
		ring:       make([]seqEvent, ringCap),
		nextSeq:    1,
		firstSeq:   1,
		subs:       make(map[int]runSubscriber),
		subBuffer:  subBuffer,
		cleanup:    cleanup,
		now:        time.Now,
	}
}

// append assigns the next sequence number, writes the event into the ring
// (overwriting the oldest and advancing firstSeq when full), then fans it to every
// live subscriber under the session mutex with the shared pump discipline: a
// non-lifecycle delta drops on a full channel (WARN + metric), a lifecycle frame
// blocks until delivered, ctx-done, or the subscriber unsubscribes (pumpGone → the
// dead entry is pruned). Producer-only. Returns false on ctx-cancel (or a terminal
// session — a producer bug, tolerated defensively rather than panicking a detached
// goroutine) so the producer unwinds.
func (s *RunSession) append(ctx context.Context, ev events.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal {
		return false
	}
	sev := seqEvent{Seq: s.nextSeq, Ev: ev}
	s.nextSeq++
	s.ring[s.head] = sev
	s.head = (s.head + 1) % len(s.ring)
	if s.nextSeq-s.firstSeq > int64(len(s.ring)) {
		s.firstSeq++ // overwrote the oldest — the replay window slides forward
	}
	lifecycle := isLifecycleFrame(ev.Type())
	for id, sub := range s.subs {
		switch pumpDeliver(ctx, sub.gone, sub.ch, sev, lifecycle) {
		case pumpAbort:
			return false
		case pumpDropped:
			recordSSEDropped()
			slog.Warn("agui run session: subscriber slow, dropping event", "type", ev.Type(), "run_id", s.RunID)
		case pumpGone:
			delete(s.subs, id)
		}
	}
	return true
}

// subscribeFrom snapshots every ring entry with Seq > fromSeq into a fresh channel
// FIRST, then registers the channel — because append takes the same mutex,
// replay-then-live is gapless and duplicate-free by construction (§2.1: no window
// between snapshot and registration). The channel is sized replay + subBuffer so the
// snapshot can never block under the mutex while the live tail keeps the
// drop-on-full cap. ok=false signals a replay gap (fromSeq+1 < firstSeq → 410, §3.3).
// On a terminal session the tail is replayed and the channel returned already
// closed (terminal replay ends after RUN_FINISHED, §3.3). The returned cancel MUST
// be called when the caller stops draining a live subscription (idempotent; it is
// the gone signal that keeps an abandoned channel from wedging the producer).
func (s *RunSession) subscribeFrom(fromSeq int64) (<-chan seqEvent, func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fromSeq+1 < s.firstSeq {
		return nil, nil, false
	}
	start := fromSeq + 1
	replay := max(s.nextSeq-start, 0)
	ch := make(chan seqEvent, int(replay)+s.subBuffer)
	for seq := s.nextSeq - replay; seq < s.nextSeq; seq++ {
		ch <- s.ring[(s.head-int(s.nextSeq-seq)+len(s.ring))%len(s.ring)]
	}
	if s.terminal {
		close(ch)
		return ch, func() {}, true
	}
	id := s.nextSubID
	s.nextSubID++
	gone := make(chan struct{})
	s.subs[id] = runSubscriber{ch: ch, gone: gone}
	var once sync.Once
	cancel := func() {
		once.Do(func() { close(gone) })
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
	return ch, cancel, true
}

// terminalState reports whether the session is terminal and when it finished — the
// reaper's eviction predicate (`terminal && now-finishedAt > linger`, §1.3). Safe
// from any goroutine.
func (s *RunSession) terminalState() (bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminal, s.finishedAt
}

// finish marks the session terminal, stamps finishedAt (the reaper's linger clock,
// §1.3), closes every subscriber channel (sole sender closes — the Fanout.closeAll
// principle), and invokes the cleanup callback exactly once, after releasing the
// mutex so the callback may take the registry lock. Idempotent; producer-only.
func (s *RunSession) finish() {
	s.mu.Lock()
	if s.terminal {
		s.mu.Unlock()
		return
	}
	s.terminal = true
	s.finishedAt = s.now()
	for id, sub := range s.subs {
		close(sub.ch)
		delete(s.subs, id)
	}
	cleanup := s.cleanup
	s.cleanup = nil
	s.mu.Unlock()
	if cleanup != nil {
		cleanup()
	}
}
