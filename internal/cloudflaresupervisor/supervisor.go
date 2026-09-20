package cloudflaresupervisor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Child separates process lifecycle from the generation handoff state machine.
type Child interface {
	Ready(context.Context) bool
	Exited() bool
	Stop()
}

// Launcher consumes only credential file paths, never token-bearing arguments.
type Launcher interface {
	Start(tokenPath string) (Child, error)
}

// Options bounds polling, candidate readiness and retry pressure.
type Options struct{ PollInterval, ReadyTimeout, RetryInterval time.Duration }

// Status intentionally contains no process errors, credential paths or token hashes.
type Status struct {
	State            string `json:"state"`
	Generation       int64  `json:"generation"`
	ActiveGeneration int64  `json:"active_generation"`
	Ready            bool   `json:"ready"`
	ErrorCode        string `json:"error_code,omitempty"`
}

type desiredState struct {
	Enabled    bool  `json:"enabled"`
	Generation int64 `json:"generation"`
}

// Supervisor retains the old connector until a candidate proves readiness.
type Supervisor struct {
	root                                  string
	launcher                              Launcher
	opts                                  Options
	mu                                    sync.RWMutex
	status                                Status
	active, candidate                     Child
	activeGeneration, candidateGeneration int64
	candidateDeadline, retryAt            time.Time
	lastGeneration                        int64
	candidateToken                        os.FileInfo
}

// NewSupervisor starts idle; Run is the sole owner of child lifecycle changes.
func NewSupervisor(root string, launcher Launcher, opts Options) *Supervisor {
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.ReadyTimeout <= 0 {
		opts.ReadyTimeout = time.Minute
	}
	if opts.RetryInterval <= 0 {
		opts.RetryInterval = 30 * time.Second
	}
	return &Supervisor{root: root, launcher: launcher, opts: opts, status: Status{State: "idle"}}
}

// Status returns an observation safe for concurrent HTTP handlers.
func (s *Supervisor) Status() Status { s.mu.RLock(); defer s.mu.RUnlock(); return s.status }

// Run reaps both children before returning on cancellation.
func (s *Supervisor) Run(ctx context.Context) {
	defer s.stop()
	ticker := time.NewTicker(s.opts.PollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.step(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) stopCandidate() {
	if s.candidate != nil {
		s.candidate.Stop()
		s.candidate = nil
		s.candidateToken = nil
	}
}

func (s *Supervisor) stop() {
	s.stopCandidate()
	if s.active != nil {
		s.active.Stop()
		s.active = nil
	}
	s.activeGeneration = 0
}

func (s *Supervisor) publish(ctx context.Context, state string, generation int64, code string) {
	ready := s.active != nil && s.active.Ready(ctx)
	s.mu.Lock()
	s.status = Status{State: state, Generation: generation, ActiveGeneration: s.activeGeneration, Ready: ready, ErrorCode: code}
	s.mu.Unlock()
}

func (s *Supervisor) step(ctx context.Context, now time.Time) {
	desired, err := readDesired(s.root)
	if err != nil {
		s.stopCandidate()
		s.publish(ctx, "degraded", s.lastGeneration, "projection_invalid")
		return
	}
	if !desired.Enabled {
		s.stop()
		s.lastGeneration = desired.Generation
		s.retryAt = time.Time{}
		s.publish(ctx, "idle", desired.Generation, "")
		return
	}
	if desired.Generation != s.lastGeneration {
		s.retryAt = time.Time{}
		s.lastGeneration = desired.Generation
	}
	if s.active != nil && s.active.Exited() {
		s.active.Stop()
		s.active = nil
		s.activeGeneration = 0
	}
	if s.candidate != nil {
		info, statErr := os.Stat(filepath.Join(s.root, "token"))
		if s.candidateGeneration != desired.Generation || statErr != nil || !sameToken(s.candidateToken, info) {
			s.stopCandidate()
		}
	}
	if s.candidate != nil {
		if s.candidate.Exited() || !now.Before(s.candidateDeadline) {
			s.stopCandidate()
			s.retryAt = now.Add(s.opts.RetryInterval)
			s.publish(ctx, "degraded", desired.Generation, "candidate_not_ready")
			return
		}
		if s.candidate.Ready(ctx) {
			if s.active != nil {
				s.active.Stop()
			}
			s.active, s.activeGeneration = s.candidate, s.candidateGeneration
			s.candidate, s.candidateToken = nil, nil
		}
	}
	if s.active != nil && s.activeGeneration == desired.Generation {
		if s.active.Ready(ctx) {
			s.publish(ctx, "healthy", desired.Generation, "")
		} else {
			s.publish(ctx, "degraded", desired.Generation, "connector_not_ready")
		}
		return
	}
	if s.candidate == nil && !now.Before(s.retryAt) {
		path := filepath.Join(s.root, "token")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 64*1024 {
			s.publish(ctx, "degraded", desired.Generation, "token_unavailable")
			return
		}
		child, err := s.launcher.Start(path)
		if err != nil {
			s.retryAt = now.Add(s.opts.RetryInterval)
			s.publish(ctx, "degraded", desired.Generation, "candidate_start_failed")
			return
		}
		s.candidate, s.candidateGeneration, s.candidateToken = child, desired.Generation, info
		s.candidateDeadline = now.Add(s.opts.ReadyTimeout)
	}
	state, code := "connecting", ""
	if s.candidate == nil {
		state, code = "degraded", "candidate_not_ready"
	}
	s.publish(ctx, state, desired.Generation, code)
}

func sameToken(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func readDesired(root string) (desiredState, error) {
	var state desiredState
	f, err := os.Open(filepath.Join(root, "desired.json")) // #nosec G304 -- root is fixed by the daemon, not request input.
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	defer func() { _ = f.Close() }()
	d := json.NewDecoder(io.LimitReader(f, 1024))
	d.DisallowUnknownFields()
	if err := d.Decode(&state); err != nil {
		return state, err
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return state, errors.New("invalid projection")
	}
	if state.Generation < 0 {
		return state, errors.New("invalid generation")
	}
	return state, nil
}
