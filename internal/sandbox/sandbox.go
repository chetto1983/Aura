// Package sandbox executes LLM-generated Python code in an isolated runtime.
//
// Manager owns policy, health, and the stable execute_code boundary. Runtime
// adapters own the actual backend implementation; release builds are expected
// to provide the bundled Pyodide adapter.
package sandbox

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Result holds the output of a sandbox execution.
type Result struct {
	OK        bool       `json:"ok"`
	Stdout    string     `json:"stdout"`
	Stderr    string     `json:"stderr"`
	ExitCode  int        `json:"exit_code"`
	ElapsedMs int        `json:"elapsed_ms"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// Artifact is an explicit file emitted by sandbox code from the configured
// output directory. Runtimes must only populate this for allowlisted paths.
type Artifact struct {
	Name      string `json:"name"`
	MimeType  string `json:"mime_type"`
	Bytes     []byte `json:"bytes"`
	SizeBytes int64  `json:"size_bytes"`
}

// Config controls sandbox behaviour.
type Config struct {
	// Runtime is the execution adapter. Nil means no runtime is configured,
	// so execute_code must remain disabled.
	Runtime Runtime
	// Timeout is the per-execution wall-clock limit. Default 15s.
	Timeout time.Duration
}

type RuntimeKind string

const (
	RuntimeKindPyodide     RuntimeKind = "pyodide"
	RuntimeKindUnavailable RuntimeKind = "unavailable"
)

// Runtime is the adapter boundary for sandbox execution backends.
type Runtime interface {
	Kind() RuntimeKind
	Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error)
	CheckAvailability() Availability
	ValidateCode(code string) error
}

// Availability describes whether the configured runtime can execute code.
type Availability struct {
	Available bool
	Kind      RuntimeKind
	Detail    string
}

// Manager runs Python code through a configured sandbox runtime.
type Manager struct {
	cfg     Config
	runtime Runtime
}

// NewManager creates a sandbox manager. Nil Runtime fails closed because Aura
// no longer has a host-runtime fallback.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.Runtime == nil {
		return nil, errors.New("sandbox: runtime is required")
	}
	return &Manager{cfg: cfg, runtime: cfg.Runtime}, nil
}

// RuntimeKind reports the configured backend kind.
func (m *Manager) RuntimeKind() RuntimeKind {
	if m == nil || m.runtime == nil {
		return RuntimeKindUnavailable
	}
	return m.runtime.Kind()
}

// Execute runs the given Python code in the configured runtime.
func (m *Manager) Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error) {
	if err := m.ValidateCode(code); err != nil {
		return nil, err
	}
	return m.runtime.Execute(ctx, code, allowNetwork)
}

// IsAvailable reports whether the configured runtime can execute code.
func (m *Manager) IsAvailable() bool {
	return m.CheckAvailability().Available
}

// CheckAvailability runs the runtime probe used to decide whether
// execute_code should be registered.
func (m *Manager) CheckAvailability() Availability {
	if m == nil || m.runtime == nil {
		return Availability{
			Available: false,
			Kind:      RuntimeKindUnavailable,
			Detail:    "sandbox runtime unavailable",
		}
	}
	return normalizeAvailability(m.runtime.Kind(), m.runtime.CheckAvailability())
}

// ValidateCode performs defense-in-depth validation before sandbox execution.
// The configured runtime owns the concrete validation mechanism.
func (m *Manager) ValidateCode(code string) error {
	if strings.TrimSpace(code) == "" {
		return errors.New("sandbox: code must not be empty")
	}
	if len(code) > 100_000 {
		return errors.New("sandbox: code exceeds 100KB limit")
	}
	return m.runtime.ValidateCode(code)
}

func normalizeAvailability(kind RuntimeKind, availability Availability) Availability {
	if availability.Kind == "" {
		availability.Kind = kind
	}
	if availability.Kind == "" {
		availability.Kind = RuntimeKindUnavailable
	}
	return availability
}
