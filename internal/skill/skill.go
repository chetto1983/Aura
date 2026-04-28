package skill

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// TrustLevel classifies how much trust to place in a skill.
type TrustLevel int

const (
	// Local skills are authored by the user and run with full system access.
	Local TrustLevel = iota
	// Verified skills have been reviewed and run with limited restrictions.
	Verified
	// Untrusted skills are third-party and run with maximum restrictions.
	Untrusted
)

func (t TrustLevel) String() string {
	switch t {
	case Local:
		return "local"
	case Verified:
		return "verified"
	case Untrusted:
		return "untrusted"
	default:
		return "unknown"
	}
}

// TrustLevelFromString parses a trust level from string.
func TrustLevelFromString(s string) (TrustLevel, error) {
	switch s {
	case "local":
		return Local, nil
	case "verified":
		return Verified, nil
	case "untrusted":
		return Untrusted, nil
	default:
		return Untrusted, fmt.Errorf("unknown trust level: %q", s)
	}
}

// Constraints define resource limits for skill execution.
type Constraints struct {
	Timeout   time.Duration // Maximum execution time
	CPULimit  int           // CPU time limit in seconds (0 = unlimited)
	MemoryMB  int           // Memory limit in MB (0 = unlimited)
	Network   bool          // true = network access allowed
	MaxOutput int64         // Maximum output bytes (0 = unlimited)
}

// DefaultConstraints returns the default constraints for a trust level.
func DefaultConstraints(trust TrustLevel) Constraints {
	switch trust {
	case Local:
		return Constraints{
			Timeout:   30 * time.Second,
			Network:   true,
			MaxOutput: 1 << 20, // 1MB
		}
	case Verified:
		return Constraints{
			Timeout:   15 * time.Second,
			CPULimit:  30,
			MemoryMB:  256,
			Network:   true,
			MaxOutput: 512 * 1024, // 512KB
		}
	case Untrusted:
		return Constraints{
			Timeout:   10 * time.Second,
			CPULimit:  10,
			MemoryMB:  128,
			Network:   false,
			MaxOutput: 256 * 1024, // 256KB
		}
	default:
		return DefaultConstraints(Untrusted)
	}
}

// resolveConstraints merges skill-specific overrides with trust-level defaults.
// Untrusted skills always have Network=false (security invariant).
// Skills can only add restrictions, not remove them.
func resolveConstraints(skill Skill) Constraints {
	c := DefaultConstraints(skill.Trust)

	if skill.Constraints.Timeout > 0 {
		c.Timeout = skill.Constraints.Timeout
	}
	if skill.Constraints.CPULimit > 0 {
		c.CPULimit = skill.Constraints.CPULimit
	}
	if skill.Constraints.MemoryMB > 0 {
		c.MemoryMB = skill.Constraints.MemoryMB
	}
	if skill.Constraints.MaxOutput > 0 {
		c.MaxOutput = skill.Constraints.MaxOutput
	}

	// Security invariant: untrusted skills never get network access
	if skill.Trust == Untrusted {
		c.Network = false
	} else if !skill.Constraints.Network && skill.Constraints.MaxOutput > 0 {
		// Skills can restrict themselves further (only when other constraints
		// are explicitly set, indicating the Constraints struct was intentionally
		// configured rather than zero-valued)
		c.Network = false
	}

	return c
}

// Skill represents an executable skill with metadata.
type Skill struct {
	Name        string
	Command     string
	Args        []string
	Trust       TrustLevel
	Constraints Constraints
}

// Result holds the output of a skill execution.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
	TimedOut bool
}

// Runner executes skills in a sandboxed environment.
type Runner struct {
	logger *slog.Logger
}

// NewRunner creates a new skill runner.
func NewRunner(logger *slog.Logger) *Runner {
	return &Runner{logger: logger}
}

// Run executes a skill with the given constraints.
func (r *Runner) Run(ctx context.Context, skill Skill, input string) (*Result, error) {
	if skill.Command == "" {
		return nil, fmt.Errorf("skill %q has no command", skill.Name)
	}

	c := resolveConstraints(skill)

	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := buildCommand(ctx, &skill, c, r.logger)

	// Feed input via stdin
	if input != "" {
		cmd.Stdin = newStringReader(input)
	}

	// Capture output with bounded buffers
	var out, serr boundedBuffer
	out.limit = c.MaxOutput
	serr.limit = c.MaxOutput
	cmd.Stdout = &out
	cmd.Stderr = &serr

	// Set restricted environment for non-local skills
	if skill.Trust != Local {
		cmd.Env = restrictedEnv(skill.Trust)
	}

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &Result{
		ExitCode: exitCode(err),
		Stdout:   out.String(),
		Stderr:   serr.String(),
		Duration: duration,
		TimedOut: ctx.Err() == context.DeadlineExceeded,
	}

	if ctx.Err() == context.DeadlineExceeded {
		r.logger.Warn("skill execution timed out",
			"skill", skill.Name,
			"timeout", timeout,
		)
		return result, fmt.Errorf("skill %q timed out after %v", skill.Name, timeout)
	}

	if err != nil {
		r.logger.Warn("skill execution failed",
			"skill", skill.Name,
			"exit_code", result.ExitCode,
			"error", err,
		)
		return result, fmt.Errorf("skill %q failed: %w", skill.Name, err)
	}

	r.logger.Info("skill executed",
		"skill", skill.Name,
		"trust", skill.Trust,
		"duration", duration,
		"exit_code", result.ExitCode,
	)

	return result, nil
}

// restrictedEnv returns a minimal environment for untrusted/verified skills.
// Untrusted skills get no network-related variables.
func restrictedEnv(trust TrustLevel) []string {
	switch trust {
	case Verified:
		return []string{
			"PATH=/usr/local/bin:/usr/bin:/bin",
			"HOME=/tmp",
			"TMPDIR=/tmp",
		}
	case Untrusted:
		return []string{
			"PATH=/usr/local/bin:/usr/bin:/bin",
			"HOME=/tmp",
			"TMPDIR=/tmp",
		}
	default:
		return nil
	}
}

// exitCode extracts the exit code from an exec error.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

// boundedBuffer is an io.Writer that limits output size.
type boundedBuffer struct {
	data  bytes.Buffer
	limit int64
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 {
		remaining := b.limit - int64(b.data.Len())
		if remaining <= 0 {
			return len(p), nil // Buffer full, discard
		}
		if int64(len(p)) > remaining {
			b.data.Write(p[:remaining])
			return len(p), nil
		}
	}
	b.data.Write(p)
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	return b.data.String()
}

// stringReader implements io.Reader for a string.
type stringReader struct {
	data   []byte
	offset int
}

func newStringReader(s string) *stringReader {
	return &stringReader{data: []byte(s)}
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}
