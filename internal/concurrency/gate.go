package concurrency

import (
	"context"
	"sync"
	"time"
)

// UserGate serializes per-user message processing using the actor pattern.
// Each active user gets one long-lived goroutine with a private buffered channel
// inbox. Messages are processed one at a time per user (CONC-01), while different
// users process concurrently.
//
// TryAcquire provides a non-blocking path for notification delivery (CONC-02).
// InactivityTracker evicts idle actors automatically (CONC-03).
type UserGate struct {
	mu      sync.Mutex
	actors  map[string]*userActor
	config  Config
	tracker *InactivityTracker
	wg      sync.WaitGroup // tracks all actor goroutines for graceful shutdown
}

// userActor holds the state for a single user's actor goroutine.
type userActor struct {
	userID string
	inbox  chan Entry        // buffered, capacity from Config.InboxSize
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{} // closed when runActor exits
}

// New creates a UserGate with the given config. Missing/zero config fields are
// filled with defaults. The InactivityTracker is started immediately.
func New(cfg Config) *UserGate {
	// Apply defaults per D-16
	if cfg.InboxSize <= 0 {
		cfg.InboxSize = 8
	}
	if cfg.EvictionThreshold <= 0 {
		cfg.EvictionThreshold = 30 * time.Minute
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = 60 * time.Second
	}

	g := &UserGate{
		actors: make(map[string]*userActor),
		config: cfg,
	}

	// Create InactivityTracker internally per D-11. The tracker calls g.Evict
	// when a user exceeds the inactivity threshold.
	g.tracker = newInactivityTracker(cfg.EvictionThreshold, cfg.SweepInterval, g.Evict)
	g.tracker.Start()

	return g
}

// Acquire enqueues entry into the per-user actor's buffered channel inbox.
// If the inbox is full, the oldest entry is dropped and OnOverflow is called
// in a separate goroutine (per D-03 and Pitfall 4). Acquire blocks only if
// the inbox is full after the drop (which should not happen with cap >= 2).
// Returns ctx.Err() if the context is cancelled while waiting.
//
// When QueueNoticeAfter > 0 and OnQueueNotice != nil, a per-entry timer goroutine
// is spawned after successful enqueue. The timer fires OnQueueNotice(userID) if
// the entry has not begun processing within QueueNoticeAfter (CONC-01 gap-closure).
//
// Callers MUST call Acquire from outside the per-user actor goroutine (i.e.,
// from onMessage's goroutine) to avoid deadlock (Pitfall 1).
func (g *UserGate) Acquire(ctx context.Context, userID string, entry Entry) error {
	actor := g.getOrCreateActor(userID)

	// Pre-create startedCh only if queue-notice is configured. This avoids
	// allocating a channel for every entry when the feature is disabled.
	var startedCh chan struct{}
	if g.config.QueueNoticeAfter > 0 && g.config.OnQueueNotice != nil {
		startedCh = make(chan struct{})
		entry.startedCh = startedCh
	}

	for {
		select {
		case actor.inbox <- entry:
			if startedCh != nil {
				// Spawn timer goroutine in a separate goroutine per Pitfall 4.
				// The gate's hot path must never block on external I/O or timers.
				// Register timer goroutine with g.wg so Close() waits for it
				// (CR-01: prevents leaked timers from firing after shutdown).
				g.wg.Add(1)
				go g.runQueueNoticeTimer(userID, startedCh, actor.ctx)
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Inbox full: drop oldest entry and notify, then retry enqueue.
			g.dropOldestAndNotify(actor, userID)
		}
	}
}

// runQueueNoticeTimer waits QueueNoticeAfter; if startedCh has not been closed
// (i.e., the entry has not begun processing) and the actor context is still
// active, it fires OnQueueNotice(userID).
// Called in a separate goroutine by Acquire after successful enqueue (Pitfall 4).
//
// CR-01: The actorCtx arm prevents the timer from firing for an entry whose
// actor was cancelled by Evict or Close. Without this, the timer would deliver
// a misleading "still working on your previous message" notice for a user that
// has been evicted (and possibly replaced with a fresh actor). The timer
// goroutine is tracked by g.wg so Close() waits for it to exit cleanly.
func (g *UserGate) runQueueNoticeTimer(userID string, startedCh <-chan struct{}, actorCtx context.Context) {
	defer g.wg.Done()
	timer := time.NewTimer(g.config.QueueNoticeAfter)
	defer timer.Stop()
	select {
	case <-timer.C:
		// Threshold elapsed -- fire notice. Caller-supplied callback should
		// hand off to its own goroutine for external I/O (Pitfall 4).
		if g.config.OnQueueNotice != nil {
			g.config.OnQueueNotice(userID)
		}
	case <-startedCh:
		// Entry began processing before the threshold -- no notice.
	case <-actorCtx.Done():
		// Actor was evicted or the gate is shutting down -- do NOT fire,
		// the entry will never be processed (CR-01).
	}
}

// TryAcquire attempts a non-blocking enqueue into the per-user actor's inbox.
// Returns true if the entry was enqueued, false if the inbox is full.
// Never blocks (per CONC-02). Used by notification paths (scheduler) to avoid
// deadlock when the user's actor goroutine is busy (Pitfall 1).
func (g *UserGate) TryAcquire(userID string, entry Entry) bool {
	actor := g.getOrCreateActor(userID)
	select {
	case actor.inbox <- entry:
		return true
	default:
		return false
	}
}

// Evict cancels the per-user goroutine context, removes it from the actors map,
// waits for the goroutine to exit, and then calls the OnEvict callback.
// It is safe to call Evict on a userID that has no actor (no-op).
// Called by InactivityTracker when a user exceeds the inactivity threshold.
func (g *UserGate) Evict(userID string) {
	g.mu.Lock()
	actor, exists := g.actors[userID]
	if !exists {
		g.mu.Unlock()
		return
	}
	delete(g.actors, userID)
	g.mu.Unlock()

	// Cancel actor context -- signals runActor to exit via ctx.Done().
	actor.cancel()

	// Wait for goroutine to fully exit before calling OnEvict.
	// This ensures OnEvict sees a consistent state (D-10).
	<-actor.done

	// Call OnEvict after actor is fully stopped (D-10, D-12).
	if g.config.OnEvict != nil {
		g.config.OnEvict(userID)
	}
}

// IsActive reports whether userID has an active actor goroutine in the gate.
// Used by SessionStore.IsActive to delegate active-user queries (D-11, CONC-01).
func (g *UserGate) IsActive(userID string) bool {
	g.mu.Lock()
	_, exists := g.actors[userID]
	g.mu.Unlock()
	return exists
}

// Close stops the InactivityTracker and cancels all active user actor goroutines.
// It waits for all actor goroutines to exit before returning (Pitfall 2 prevention).
// OnEvict is NOT called for each actor on Close -- shutdown is not eviction.
func (g *UserGate) Close() {
	// Stop the tracker first so no new evictions fire during shutdown.
	g.tracker.Stop()

	// Cancel all active actors.
	g.mu.Lock()
	for _, actor := range g.actors {
		actor.cancel()
	}
	g.mu.Unlock()

	// Wait for all actor goroutines to exit.
	g.wg.Wait()
}

// getOrCreateActor returns the existing actor for userID, or creates a new one.
func (g *UserGate) getOrCreateActor(userID string) *userActor {
	g.mu.Lock()
	defer g.mu.Unlock()

	actor, exists := g.actors[userID]
	if !exists {
		actor = g.spawnActorLocked(userID)
		g.actors[userID] = actor
	}
	return actor
}

// spawnActorLocked creates and starts a new userActor goroutine.
// Must be called with g.mu held.
func (g *UserGate) spawnActorLocked(userID string) *userActor {
	ctx, cancel := context.WithCancel(context.Background())
	actor := &userActor{
		userID: userID,
		inbox:  make(chan Entry, g.config.InboxSize),
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	g.wg.Add(1)
	go g.runActor(actor)
	return actor
}

// runActor is the per-user goroutine loop. It processes entries from the inbox
// one at a time (serialization) and updates the inactivity tracker after each.
// Exits when the actor context is cancelled (eviction or shutdown).
//
// CR-01: On exit, runActor drains any remaining entries from the inbox and
// closes their startedCh. This is belt-and-suspenders for the queue-notice
// timer goroutines: their primary cancellation path is the actorCtx.Done()
// arm in runQueueNoticeTimer, but draining and closing startedCh ensures the
// timers exit promptly (via the startedCh arm) even if the actor is
// re-entered before timer goroutines observe the cancellation.
func (g *UserGate) runActor(actor *userActor) {
	defer g.wg.Done()
	defer close(actor.done)
	defer func() {
		// Drain remaining entries from the inbox so their queue-notice
		// timer goroutines can exit via the startedCh arm. Without this,
		// queued entries' startedCh would never be closed and their
		// timers would rely solely on actorCtx.Done() to exit.
		for {
			select {
			case entry := <-actor.inbox:
				if entry.startedCh != nil {
					close(entry.startedCh)
				}
			default:
				return
			}
		}
	}()
	for {
		select {
		case <-actor.ctx.Done():
			return // eviction or shutdown
		case entry, ok := <-actor.inbox:
			if !ok {
				return // inbox channel closed
			}
			// Signal Acquire's queue-notice timer (if any) that processing
			// has begun, before invoking the user's Process function.
			if entry.startedCh != nil {
				close(entry.startedCh)
			}
			entry.Process(actor.ctx)
			g.tracker.Touch(actor.userID) // reset inactivity clock per D-09
		}
	}
}

// dropOldestAndNotify removes the oldest entry from the inbox and calls
// OnOverflow in a separate goroutine (Pitfall 4: gate goroutine must never
// block on external I/O such as a Telegram API call).
// If the dropped entry has a startedCh (queue-notice timer), it is closed so
// the timer goroutine exits without firing OnQueueNotice (the entry was dropped,
// not processed; no notice should be sent for it).
func (g *UserGate) dropOldestAndNotify(actor *userActor, userID string) {
	// Non-blocking receive to drop the oldest entry.
	select {
	case dropped := <-actor.inbox:
		// Cancel the dropped entry's queue-notice timer so it does not fire.
		if dropped.startedCh != nil {
			close(dropped.startedCh)
		}
	default:
		// Inbox was empty (should not happen if we just detected it was full).
	}
	// Call OnOverflow in a separate goroutine per Pitfall 4. Track it on
	// g.wg so Close() waits for the callback to return — otherwise a slow
	// overflow handler can outlive the Gate and leak on shutdown.
	if g.config.OnOverflow != nil {
		g.wg.Go(func() {
			g.config.OnOverflow(userID)
		})
	}
}
