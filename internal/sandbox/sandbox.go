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

	"github.com/aura/aura/internal/source"
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
	RuntimeKindProcess     RuntimeKind = "process"
	RuntimeKindUnavailable RuntimeKind = "unavailable"
)

// Runtime is the adapter boundary for sandbox execution backends.
type Runtime interface {
	Kind() RuntimeKind
	Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error)
	CheckAvailability() Availability
	ValidateCode(code string) error
}

// Executor is the narrow execute_code runtime surface.
type Executor interface {
	Execute(ctx context.Context, code string, allowNetwork bool) (*Result, error)
}

// CommandExecutor is the narrow execute_shell runtime surface. Only runtimes
// that intentionally expose a process shell implement it.
type CommandExecutor interface {
	ExecuteCommand(ctx context.Context, command string, allowNetwork bool) (*Result, error)
}

// CodeValidator validates generated Python before execution or persistence.
type CodeValidator interface {
	ValidateCode(code string) error
}

// AvailabilityReader reports runtime health for dashboard/status surfaces.
type AvailabilityReader interface {
	RuntimeKind() RuntimeKind
	CheckAvailability() Availability
	IsAvailable() bool
}

// ExecutionRuntime is the Telegram auto-improve runtime surface.
type ExecutionRuntime interface {
	Executor
	CodeValidator
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

func (m *Manager) ExtractXLSX(ctx context.Context, body []byte) (source.ExtractResult, error) {
	if m == nil || m.runtime == nil {
		return source.ExtractResult{}, errors.New("sandbox: runtime is required")
	}
	extractor, ok := m.runtime.(interface {
		ExtractXLSX(context.Context, []byte) (source.ExtractResult, error)
	})
	if !ok {
		return source.ExtractResult{}, errors.New("sandbox: runtime does not support xlsx extraction")
	}
	return extractor.ExtractXLSX(ctx, body)
}

func (m *Manager) ExtractDOCX(ctx context.Context, body []byte) (source.ExtractResult, error) {
	if m == nil || m.runtime == nil {
		return source.ExtractResult{}, errors.New("sandbox: runtime is required")
	}
	extractor, ok := m.runtime.(interface {
		ExtractDOCX(context.Context, []byte) (source.ExtractResult, error)
	})
	if !ok {
		return source.ExtractResult{}, errors.New("sandbox: runtime does not support docx extraction")
	}
	return extractor.ExtractDOCX(ctx, body)
}

// ExecuteCommand runs a shell command in runtimes that explicitly support it.
func (m *Manager) ExecuteCommand(ctx context.Context, command string, allowNetwork bool) (*Result, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("sandbox: command must not be empty")
	}
	if len(command) > 20_000 {
		return nil, errors.New("sandbox: command exceeds 20KB limit")
	}
	if m == nil || m.runtime == nil {
		return nil, errors.New("sandbox: runtime is required")
	}
	executor, ok := m.runtime.(CommandExecutor)
	if !ok {
		return nil, errors.New("sandbox: runtime does not support shell commands")
	}
	return executor.ExecuteCommand(ctx, command, allowNetwork)
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
