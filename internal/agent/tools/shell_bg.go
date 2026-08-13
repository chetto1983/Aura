package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent/panicobs"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// Background shell support (parity P4) mirrors Claude Code's
// Bash(run_in_background) / BashOutput / KillBash trio: shell_exec with
// "background": true starts a detached process and returns a shell_id immediately;
// shell_poll returns only the NEW output since the last poll plus the run status;
// shell_kill terminates it. For long jobs (builds, downloads, dev servers) that
// must not block a turn or die at the 120s synchronous timeout.
//
// Job identity + authority live in shell_bg_owner.go (crypto-random IDs +
// (identity,session) owner + poll/kill authority, MUSR-03); the default-1h TTL
// reaper lives in shell_bg_ttl.go (MUSR-04).

// BackgroundShells is the process-scoped registry of running/finished background
// shells. ONE instance is shared by shell_exec, shell_poll, and shell_kill at
// registration so a job started in one turn stays pollable/killable in a later
// turn (the tool instances outlive any single LlmAgent — the registry is built
// once at boot). Concurrency-safe.
type BackgroundShells struct {
	mu     sync.Mutex
	bufCap int
	max    int
	shells map[string]*bgShell

	// Router is the per-identity box routing seam (SBX-01, plan 37-09) and the only way a
	// background job starts: shell_exec routes, then startBox runs the job INSIDE the box via
	// Router.ExecStream. There is no host *exec.Cmd path left.
	Router *usersandbox.SandboxRouter

	// TTL reaper lifecycle (shell_bg_ttl.go). ttl is the default per-job budget; the
	// reaper goroutine (StartReaper) sweeps expired jobs and is joined by Shutdown.
	ttl            time.Duration
	reaperInterval time.Duration
	reaperStop     chan struct{}
	reaperOnce     sync.Once
	reaperWG       sync.WaitGroup
}

// NewBackgroundShells builds an empty registry to share across the shell tools, wired with the
// per-identity box router every job runs through. The TTL reaper is NOT started here (goleak
// parity — the constructor spawns no goroutine); the composition root calls StartReaper on the
// daemon work ctx.
func NewBackgroundShells(router *usersandbox.SandboxRouter) *BackgroundShells {
	ttl := shellBackgroundTTL()
	return &BackgroundShells{
		Router:         router,
		bufCap:         shellBackgroundBufCap(),
		max:            shellBackgroundMax(),
		shells:         map[string]*bgShell{},
		ttl:            ttl,
		reaperInterval: reaperIntervalFor(ttl),
		reaperStop:     make(chan struct{}),
	}
}

// bgShell is one detached process. Its combined stdout+stderr accumulate in buf;
// readOff marks how far shell_poll has already returned so each poll yields only
// the new bytes. cancel kills the process (shell_kill / shutdown).
type bgShell struct {
	id        string
	ownerID   string // identityctx.IdentityID at start (or localOwnerID for the no-principal CLI)
	sessionID string // conversation/session key at start (D-23), "" for bare-ctx callers
	startedAt time.Time
	ttl       time.Duration // per-job budget; 0 disables TTL expiry (MUSR-04)
	cancel    context.CancelFunc

	mu sync.Mutex
	// box is the streamed box-exec handle (37-09), nil only between register and a successful
	// ExecStream. Guarded by mu because startBox sets it after the shell is already registered.
	// This is the "registry holds a box-exec handle, never a host process" invariant made concrete.
	box      *usersandbox.ExecStreamHandle
	buf      []byte
	readOff  int
	bufCap   int
	dropped  int64
	reported int64
	done     bool
	killed   bool
	expired  bool // TTL reaper terminated it (status "expired", MUSR-04)
	exitCode *int
}

// Write is the io.Writer wired to both cmd.Stdout and cmd.Stderr; the mutex makes
// the interleaved combined stream race-free.
func (s *bgShell) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	s.trimLocked()
	s.mu.Unlock()
	return len(p), nil
}

func (s *bgShell) trimLocked() {
	capBytes := s.bufCap
	if capBytes <= 0 {
		capBytes = defaultShellOutputCap
	}
	if len(s.buf) <= capBytes {
		return
	}
	drop := len(s.buf) - capBytes
	s.buf = append(s.buf[:0], s.buf[drop:]...)
	s.dropped += int64(drop)
	if s.readOff >= drop {
		s.readOff -= drop
	} else {
		s.readOff = 0
	}
}

func (s *bgShell) finish(waitErr error) {
	s.mu.Lock()
	s.done = true
	switch {
	case s.expired, s.killed:
		// TTL-expired / explicitly-killed jobs carry no exit code — we terminated the
		// process, it did not exit on its own. expired keeps its "expired" status.
		s.exitCode = nil
	default:
		// A box streamed exec reports its exit via ExecInspect (boxWait wraps it), so a
		// *bgBoxExit is the normal termination; anything else is an infra failure with no
		// exit code to report.
		var boxExit *bgBoxExit
		if errors.As(waitErr, &boxExit) {
			s.exitCode = &boxExit.code
		}
	}
	s.mu.Unlock()
}

// bgBoxExit carries a box streamed-exec exit code (from ExecInspect) so the shared reaper's
// finish() records it the same way it records a host *exec.ExitError code.
type bgBoxExit struct{ code int }

func (e *bgBoxExit) Error() string { return fmt.Sprintf("box exec exited with code %d", e.code) }

func (s *bgShell) finishPanic(recovered any) {
	msg := fmt.Sprintf("panic: %v", recovered)
	_, _ = s.Write([]byte(msg + "\n"))
	s.mu.Lock()
	s.done = true
	one := 1
	s.exitCode = &one
	s.mu.Unlock()
}

// snapshot returns the output produced since the last poll (advancing readOff) and
// the current status line: running | exited:<code> | killed | expired.
func (s *bgShell) snapshot(filter *regexp.Regexp) (chunk, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chunk = string(s.buf[s.readOff:])
	s.readOff = len(s.buf)
	if s.dropped > s.reported {
		chunk = fmt.Sprintf("[background output truncated: dropped %d byte(s)]\n%s", s.dropped-s.reported, chunk)
		s.reported = s.dropped
	}
	if filter != nil {
		chunk = filterLines(chunk, filter)
	}
	if s.readOff > 0 {
		s.buf = s.buf[s.readOff:]
		s.readOff = 0
	}
	switch {
	case s.expired:
		status = "expired"
	case s.killed:
		status = "killed"
	case !s.done:
		status = "running"
	case s.exitCode != nil:
		status = fmt.Sprintf("exited:%d", *s.exitCode)
	default:
		status = "killed"
	}
	return chunk, status
}

// startBox launches command DETACHED from the per-call ctx (which dies the moment Execute
// returns) so the job outlives the turn, INSIDE the per-identity box via a streamed box exec
// (Router.ExecStream). callerCtx carries the owner principal (identityctx) + session key bound
// onto the job for the poll/kill authority check (MUSR-03); it is NOT the job's lifetime ctx.
// A box start failure returns an error the caller maps to the fail-CLOSED deny (D-09/GATE-01) —
// no host process is spawnable from here.
func (b *BackgroundShells) startBox(callerCtx context.Context, h usersandbox.BoxHandle, command, dir string, env []string) (string, error) {
	id, err := newBackgroundShellID()
	if err != nil {
		return "", err
	}
	sh := b.newShell(callerCtx, id)
	// Until the box handle exists, cancel forwards to the (yet-nil) handle under sh.mu, so a
	// concurrent kill/sweep/Shutdown that fires during ExecStream cannot nil-panic and is honored
	// once the handle is set below.
	sh.cancel = func() {
		sh.mu.Lock()
		hd := sh.box
		sh.mu.Unlock()
		if hd != nil {
			hd.Kill()
		}
	}
	if err := b.register(id, sh); err != nil {
		return "", err
	}
	hnd, err := b.Router.ExecStream(callerCtx, h, usersandbox.ExecRequest{
		Command: command,
		Dir:     dir,
		Env:     env,
	}, sh)
	if err != nil {
		b.remove(id)
		return "", fmt.Errorf("background box start: %w", err)
	}
	sh.mu.Lock()
	sh.box = hnd
	terminated := sh.killed || sh.expired
	sh.mu.Unlock()
	if terminated {
		// A kill/sweep raced the box start (its cancel saw a nil handle) — honor it now (no leak).
		hnd.Kill()
	}
	go runBackgroundShellReaper(sh, boxWait(hnd), hnd.Kill)
	return id, nil
}

// newShell builds a bgShell with the owner/authority binding (identity + session captured at
// start, MUSR-03), the per-job TTL, and the output buffer cap. It is shared by the host start and
// the routed startBox so the authority model is IDENTICAL on both paths; only cancel and the
// process handle differ.
func (b *BackgroundShells) newShell(callerCtx context.Context, id string) *bgShell {
	return &bgShell{
		id:        id,
		ownerID:   ownerFromContext(callerCtx),
		sessionID: shellSessionKey(callerCtx),
		startedAt: time.Now(),
		ttl:       b.ttl,
		bufCap:    b.bufCap,
	}
}

// register prunes finished jobs, opportunistically sweeps expired ones (MUSR-04 defense-in-depth
// even when StartReaper was never wired), enforces the concurrency cap, and inserts sh — all under
// one lock so the cap is atomic. On cap it registers nothing and returns the cap error.
func (b *BackgroundShells) register(id string, sh *bgShell) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shells == nil {
		b.shells = map[string]*bgShell{}
	}
	b.pruneFinishedLocked()
	b.sweepExpiredLocked(time.Now())
	if b.max > 0 && b.runningCountLocked() >= b.max {
		return fmt.Errorf("background shell cap reached (%d); poll or kill an existing shell", b.max)
	}
	b.shells[id] = sh
	return nil
}

// remove drops a shell from the registry (a failed start's cleanup). Concurrency-safe.
func (b *BackgroundShells) remove(id string) {
	b.mu.Lock()
	delete(b.shells, id)
	b.mu.Unlock()
}

// boxWait adapts a box exec-stream handle to the reaper's wait func: it blocks for the box exit
// code and returns it as a *bgBoxExit (finish extracts the code); an infra failure surfaces as-is.
func boxWait(h *usersandbox.ExecStreamHandle) func() error {
	return func() error {
		code, err := h.Wait()
		if err != nil {
			return err
		}
		return &bgBoxExit{code: code}
	}
}

func runBackgroundShellReaper(sh *bgShell, wait func() error, cancel context.CancelFunc) {
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			panicobs.Record(panicobs.SiteShellBGReaper)
			sh.finishPanic(r)
		}
	}()
	sh.finish(wait())
}

func (b *BackgroundShells) pruneFinishedLocked() {
	for id, sh := range b.shells {
		sh.mu.Lock()
		done := sh.done
		sh.mu.Unlock()
		if done {
			delete(b.shells, id)
		}
	}
}

// Evict reclaims finished background shells (SessionEvictor, AG-015). Background
// shells are process-scoped (not session-keyed), so any FINISHED shell is
// reclaimable regardless of the evicted session — a long-lived daemon must not
// accumulate finished-but-unpolled buffers (≤1 MiB each) until the next start.
// Running shells are left untouched. The sessionID arg satisfies the interface;
// pruning is by completion, not session.
func (b *BackgroundShells) Evict(string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneFinishedLocked()
}

// pruneFinishedExcept removes every FINISHED shell except keep (the one being
// polled, kept for a possible final re-poll). Running shells are left in place.
// Idempotent and concurrency-safe.
func (b *BackgroundShells) pruneFinishedExcept(keep string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, sh := range b.shells {
		if id == keep {
			continue
		}
		sh.mu.Lock()
		done := sh.done
		sh.mu.Unlock()
		if done {
			delete(b.shells, id)
		}
	}
}

func (b *BackgroundShells) runningCountLocked() int {
	n := 0
	for _, sh := range b.shells {
		sh.mu.Lock()
		running := !sh.done && !sh.killed
		sh.mu.Unlock()
		if running {
			n++
		}
	}
	return n
}

// Shutdown stops the TTL reaper and terminates every live background shell. The
// reaper is stopped first so it cannot race the group-terminate below.
func (b *BackgroundShells) Shutdown(ctx context.Context) error {
	if b == nil {
		return nil
	}
	// Stop the TTL reaper first so it cannot race the group-terminate below.
	b.stopReaper()
	b.mu.Lock()
	shells := make([]*bgShell, 0, len(b.shells))
	for _, sh := range b.shells {
		shells = append(shells, sh)
	}
	b.mu.Unlock()

	for _, sh := range shells {
		var cancel context.CancelFunc
		sh.mu.Lock()
		if !sh.done {
			sh.killed = true
			sh.exitCode = nil
			cancel = sh.cancel
		}
		sh.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		allDone := true
		for _, sh := range shells {
			sh.mu.Lock()
			done := sh.done
			sh.mu.Unlock()
			if !done {
				allDone = false
				break
			}
		}
		if allDone {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

func (b *BackgroundShells) get(id string) (*bgShell, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sh, ok := b.shells[id]
	return sh, ok
}

// kill terminates a running shell; killing an already-finished shell is a no-op
// success (idempotent). Caller authority is enforced upstream in ShellKill.Execute
// (shell_bg_owner.go) — this low-level primitive assumes the caller is entitled.
func (b *BackgroundShells) kill(id string) error {
	sh, ok := b.get(id)
	if !ok {
		return fmt.Errorf("unknown shell_id %q", id)
	}
	sh.mu.Lock()
	sh.killed = true
	sh.exitCode = nil
	sh.mu.Unlock()
	sh.cancel()
	return nil
}

func filterLines(s string, re *regexp.Regexp) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, ln := range lines {
		if re.MatchString(ln) {
			kept = append(kept, ln)
		}
	}
	return strings.Join(kept, "\n")
}

const (
	envShellBackgroundBufCap = "AURA_SHELL_BG_BUF_CAP"
	envShellBackgroundMax    = "AURA_SHELL_BG_MAX"
)

func shellBackgroundBufCap() int {
	v := strings.TrimSpace(os.Getenv(envShellBackgroundBufCap))
	if v == "" {
		return defaultShellOutputCap
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultShellOutputCap
	}
	return n
}

func shellBackgroundMax() int {
	v := strings.TrimSpace(os.Getenv(envShellBackgroundMax))
	if v == "" {
		return 8
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 8
	}
	return n
}
