package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent/panicobs"
)

// Background shell support (parity P4) mirrors Claude Code's
// Bash(run_in_background) / BashOutput / KillBash trio: shell_exec with
// "background": true starts a detached process and returns a shell_id immediately;
// shell_poll returns only the NEW output since the last poll plus the run status;
// shell_kill terminates it. For long jobs (builds, downloads, dev servers) that
// must not block a turn or die at the 120s synchronous timeout.

// BackgroundShells is the process-scoped registry of running/finished background
// shells. ONE instance is shared by shell_exec, shell_poll, and shell_kill at
// registration so a job started in one turn stays pollable/killable in a later
// turn (the tool instances outlive any single LlmAgent — the registry is built
// once at boot). Concurrency-safe.
type BackgroundShells struct {
	mu     sync.Mutex
	seq    int
	bufCap int
	max    int
	shells map[string]*bgShell
}

// NewBackgroundShells builds an empty registry to share across the shell tools.
func NewBackgroundShells() *BackgroundShells {
	return &BackgroundShells{bufCap: shellBackgroundBufCap(), max: shellBackgroundMax(), shells: map[string]*bgShell{}}
}

// bgShell is one detached process. Its combined stdout+stderr accumulate in buf;
// readOff marks how far shell_poll has already returned so each poll yields only
// the new bytes. cancel kills the process (shell_kill / shutdown).
type bgShell struct {
	id        string
	startedAt time.Time
	cancel    context.CancelFunc

	mu       sync.Mutex
	buf      []byte
	readOff  int
	bufCap   int
	dropped  int64
	reported int64
	done     bool
	killed   bool
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
	if s.killed {
		s.exitCode = nil
	} else if ec, ok := exitCode(waitErr); ok {
		s.exitCode = &ec
	} else if waitErr == nil {
		zero := 0
		s.exitCode = &zero
	}
	s.mu.Unlock()
}

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
// the current status line: running | exited:<code> | killed.
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

// start launches command DETACHED from the per-call ctx (which dies the moment
// Execute returns) so the job outlives the turn; kill is via the stored cancel.
func (b *BackgroundShells) start(command, dir string, env []string) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	name, args := shellInvocation(command)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = 5 * time.Second
	sh := &bgShell{startedAt: time.Now(), cancel: cancel, bufCap: b.bufCap}
	cmd.Stdout = sh
	cmd.Stderr = sh
	b.mu.Lock()
	if b.shells == nil {
		b.shells = map[string]*bgShell{}
	}
	b.pruneFinishedLocked()
	if b.max > 0 && b.runningCountLocked() >= b.max {
		b.mu.Unlock()
		cancel()
		return "", fmt.Errorf("background shell cap reached (%d); poll or kill an existing shell", b.max)
	}
	b.seq++
	id := fmt.Sprintf("sh_%d", b.seq)
	sh.id = id
	b.shells[id] = sh
	b.mu.Unlock()
	if err := cmd.Start(); err != nil {
		cancel()
		sh.mu.Lock()
		sh.done = true
		sh.killed = true
		sh.exitCode = nil
		sh.mu.Unlock()
		b.mu.Lock()
		delete(b.shells, id)
		b.mu.Unlock()
		return "", fmt.Errorf("background start: %w", err)
	}
	go runBackgroundShellReaper(sh, cmd.Wait, cancel)
	return id, nil
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

func (b *BackgroundShells) Shutdown(ctx context.Context) error {
	if b == nil {
		return nil
	}
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
// success (idempotent).
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

// ShellPoll reads new output from a background shell_exec job — Claude Code's
// BashOutput. Non-mutating: it only reads accumulated output + status.
type ShellPoll struct {
	Shells *BackgroundShells
}

type shellPollArgs struct {
	ShellID string `json:"shell_id"`
	Filter  string `json:"filter"`
}

func (p *ShellPoll) Spec() Spec {
	return Spec{
		Name:    "shell_poll",
		Summary: "Read new output from a background shell_exec job.",
		Description: "Retrieve output from a running or finished background shell started by shell_exec with \"background\": true. " +
			"Returns ONLY the output produced since your last poll, plus a final [aura_shell_bg {...}] footer with the shell_id and status (running | exited:<code> | killed). " +
			"Pass an optional regex 'filter' to keep only matching lines. Poll again to follow a long job; stop it with shell_kill.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "shell_id": {"type": "string", "description": "The id returned by the background shell_exec call."},
    "filter": {"type": "string", "description": "Optional regular expression; only output lines matching it are returned."}
  },
  "required": ["shell_id"]
}`),
		Deferred: true,
	}
}

func (p *ShellPoll) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if p.Shells == nil {
		return ToolResult{}, fmt.Errorf("shell_poll: background shells are not available in this context")
	}
	var a shellPollArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("shell_poll args: %w", err)
	}
	if strings.TrimSpace(a.ShellID) == "" {
		return ToolResult{}, fmt.Errorf("shell_poll: shell_id is required")
	}
	sh, ok := p.Shells.get(a.ShellID)
	if !ok {
		return ToolResult{}, fmt.Errorf("shell_poll: unknown shell_id %q", a.ShellID)
	}
	var filter *regexp.Regexp
	if strings.TrimSpace(a.Filter) != "" {
		re, err := regexp.Compile(a.Filter)
		if err != nil {
			return ToolResult{}, fmt.Errorf("shell_poll: invalid filter regex: %w", err)
		}
		filter = re
	}
	chunk, status := sh.snapshot(filter)
	body := chunk
	if strings.TrimSpace(body) == "" {
		body = "[no new output]"
	}
	body = redactModelPreview(body)
	footer, _ := json.Marshal(map[string]string{"shell_id": a.ShellID, "status": status})
	rendered := body + "\n[aura_shell_bg " + string(footer) + "]"
	res, err := NewResult(ctx, rendered)
	if err != nil {
		return ToolResult{}, err
	}
	res.Meta = &ToolResultMeta{"shell_id": a.ShellID, "status": status}
	return res, nil
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

// ShellKill terminates a background shell_exec job — Claude Code's KillBash.
type ShellKill struct {
	Shells *BackgroundShells
}

type shellKillArgs struct {
	ShellID string `json:"shell_id"`
}

func (k *ShellKill) Spec() Spec {
	return Spec{
		Name:    "shell_kill",
		Summary: "Terminate a background shell_exec job.",
		Description: "Kill a running background shell started by shell_exec (Claude Code's KillBash). " +
			"Takes the shell_id; returns success even if the job already finished (idempotent).",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "shell_id": {"type": "string", "description": "The id of the background shell to terminate."}
  },
  "required": ["shell_id"]
}`),
		Deferred: true,
		Mutating: true,
	}
}

func (k *ShellKill) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if k.Shells == nil {
		return ToolResult{}, fmt.Errorf("shell_kill: background shells are not available in this context")
	}
	var a shellKillArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("shell_kill args: %w", err)
	}
	if strings.TrimSpace(a.ShellID) == "" {
		return ToolResult{}, fmt.Errorf("shell_kill: shell_id is required")
	}
	if err := k.Shells.kill(a.ShellID); err != nil {
		return ToolResult{}, err
	}
	return NewResult(ctx, fmt.Sprintf("shell %s terminated", a.ShellID))
}
