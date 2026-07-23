package agui

import (
	"context"
	"errors"
	"sync"
	"time"
)

// runregistry.go carries the process-local registry of detached RunSessions
// (fix-plan 1.3 Tier B, docs/audit/sse-resume-design-1.3b-2026-07-23.md §2.5) plus
// the linger reaper (§1.3). Everything is in-memory by design: a daemon restart
// loses resumability and the client degrades to the MESSAGES_SNAPSHOT — exactly
// today's behavior (§7.3 non-goal). byThread indexes ONLY non-terminal sessions
// (live-run discovery, §4.2): the session's cleanup callback removes its entry at
// terminal, while the linger window applies to byRun alone so a late resume can
// still replay the tail + RUN_FINISHED before eviction.

// Registry-side fallbacks mirroring the AURA_AGUI_RUN_* config defaults (amendment
// #90 point 9), the fanoutBuffer/defaultRunBufferEvents convention: a non-positive
// resolved knob falls back rather than disabling a bound.
const (
	defaultRunLinger       = 180 * time.Second
	defaultRunMaxWallclock = 3600 * time.Second
	defaultRunMaxLive      = 16
)

// errRunRegistryFull refuses Start past the live-run cap BEFORE the thread lock is
// taken (§2.4 DoS bound); RS-04 maps it to 503 + Retry-After.
var errRunRegistryFull = errors.New("agui: run registry at max live runs")

// errRunRegistryClosed refuses Start after Close so a shutdown race can never leak
// a session no reaper will ever evict.
var errRunRegistryClosed = errors.New("agui: run registry closed")

// threadKey composites (identity, thread) so two identities never share a
// live-run discovery slot — the runner's composite-key rationale (D-23).
type threadKey struct{ identity, thread string }

// runRegistryConfig bundles the resolved caps + linger + wallclock (§2.5).
// maxWallclock is carried for RS-04's detachedRunContext bound; the registry
// itself never consumes it.
type runRegistryConfig struct {
	ringCap      int           // AURA_AGUI_RUN_BUFFER_EVENTS — per-run replay ring capacity
	subBuffer    int           // ServerConfig.BufferCap — per-subscriber live headroom
	maxLive      int           // AURA_AGUI_RUN_MAX_LIVE — non-terminal session cap
	linger       time.Duration // AURA_AGUI_RUN_LINGER_SEC — terminal replay window
	maxWallclock time.Duration // AURA_AGUI_RUN_MAX_WALLCLOCK_SEC — detached-ctx outer bound
}

// RunRegistry owns every detached RunSession between Start and reaper eviction.
// The reaper goroutine starts lazily on the first Start (ticker at linger/2) and is
// joined by Close (goleak-clean); non-terminal sessions are never reaped — the
// wallclock ctx makes them terminal, which starts their linger clock (§1.3).
type RunRegistry struct {
	mu       sync.Mutex
	byRun    map[string]*RunSession
	byThread map[threadKey]*RunSession
	cfg      runRegistryConfig
	// now is the injectable clock stamped into each session's finishedAt and read
	// by the eviction predicate; tests swap it before the first Start.
	now func() time.Time

	stop       chan struct{}
	reaperDone chan struct{}
	closed     bool
}

// newRunRegistry builds a registry over resolved knobs, falling back to the design
// defaults on any non-positive value (the bufferCap convention). The exported
// ServerConfig-shaped constructor arrives with the config knobs (RS-03).
func newRunRegistry(cfg runRegistryConfig) *RunRegistry {
	if cfg.ringCap <= 0 {
		cfg.ringCap = defaultRunBufferEvents
	}
	if cfg.subBuffer <= 0 {
		cfg.subBuffer = fanoutBuffer
	}
	if cfg.maxLive <= 0 {
		cfg.maxLive = defaultRunMaxLive
	}
	if cfg.linger <= 0 {
		cfg.linger = defaultRunLinger
	}
	if cfg.maxWallclock <= 0 {
		cfg.maxWallclock = defaultRunMaxWallclock
	}
	return &RunRegistry{
		byRun:    make(map[string]*RunSession),
		byThread: make(map[threadKey]*RunSession),
		cfg:      cfg,
		now:      time.Now,
	}
}

// runParams is what handleRun hands Start on the detached path (§1.2): identity is
// the scopedIdentityID snapshot (the resume/cancel 404 gate), cancel the detached
// ctx handle, unlock the thread-lock release whose ownership transfers from the
// handler defer to the session's terminal cleanup.
type runParams struct {
	runID      string
	threadID   string
	identityID string
	cancel     context.CancelFunc
	unlock     func()
}

// Start registers a new session, refusing past the live cap (errRunRegistryFull →
// 503, RS-04) or after Close. len(byThread) IS the live count — byThread holds
// exactly the non-terminal sessions, so a lingering terminal run never consumes a
// live slot. The session's cleanup chains the byThread removal with the thread
// unlock, both fired exactly once at terminal (finish).
func (r *RunRegistry) Start(p runParams) (*RunSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errRunRegistryClosed
	}
	if len(r.byThread) >= r.cfg.maxLive {
		return nil, errRunRegistryFull
	}
	key := threadKey{identity: p.identityID, thread: p.threadID}
	var sess *RunSession
	cleanup := func() {
		r.mu.Lock()
		if r.byThread[key] == sess {
			delete(r.byThread, key)
		}
		r.mu.Unlock()
		if p.unlock != nil {
			p.unlock()
		}
	}
	sess = newRunSession(p.runID, p.threadID, p.identityID, r.cfg.ringCap, r.cfg.subBuffer, cleanup)
	sess.cancel = p.cancel
	sess.now = r.now
	r.byRun[p.runID] = sess
	r.byThread[key] = sess
	r.startReaperLocked()
	return sess, nil
}

// Get resolves a session by run id — live or lingering-terminal (the resume route's
// lookup; a miss is the owner-scoped 404, §3.2).
func (r *RunRegistry) Get(runID string) (*RunSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.byRun[runID]
	return sess, ok
}

// LiveForThread resolves the (identity, thread)'s non-terminal session — the
// conversation DTO's live_run_id discovery read (§4.2).
func (r *RunRegistry) LiveForThread(identityID, threadID string) (*RunSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.byThread[threadKey{identity: identityID, thread: threadID}]
	return sess, ok
}

// startReaperLocked lazily launches the single reaper goroutine on the first Start
// (caller holds r.mu). The tick at linger/2 bounds how stale an expired session can
// get to half a window; Close joins via reaperDone.
func (r *RunRegistry) startReaperLocked() {
	if r.reaperDone != nil {
		return
	}
	interval := r.cfg.linger / 2
	if interval <= 0 {
		interval = time.Second
	}
	r.stop = make(chan struct{})
	r.reaperDone = make(chan struct{})
	go func(stop <-chan struct{}, done chan<- struct{}) {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.evictExpired()
			}
		}
	}(r.stop, r.reaperDone)
}

// evictExpired drops every session satisfying `terminal && now-finishedAt > linger`
// (§1.3) from byRun — ring included, the memory-bound release of §2.4. Non-terminal
// sessions are untouchable here by construction of the predicate.
func (r *RunRegistry) evictExpired() {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, sess := range r.byRun {
		if terminal, at := sess.terminalState(); terminal && now.Sub(at) > r.cfg.linger {
			delete(r.byRun, id)
		}
	}
}

// Close cancels every registered session (daemon shutdown walks all sessions,
// §1.1 bound 3c), rejects further Starts, and joins the reaper goroutine
// (goleak-clean). Idempotent; a second Close just re-joins.
func (r *RunRegistry) Close() {
	r.mu.Lock()
	var cancels []context.CancelFunc
	if !r.closed {
		r.closed = true
		for _, sess := range r.byRun {
			if sess.cancel != nil {
				cancels = append(cancels, sess.cancel)
			}
		}
		if r.stop != nil {
			close(r.stop)
		}
	}
	done := r.reaperDone
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	if done != nil {
		<-done
	}
}
