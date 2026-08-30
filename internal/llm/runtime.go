package llm

import (
	"maps"
	"sync/atomic"
)

// RuntimeSnapshot is one immutable client/config pair used for a complete LLM run.
type RuntimeSnapshot struct {
	Client Client
	Config Config
}

// Runtime owns the primary LLM client and model profile selected by the operator.
type Runtime struct {
	current atomic.Pointer[RuntimeSnapshot]
}

// NewRuntime publishes the boot-time primary LLM client and model profile.
func NewRuntime(client Client, cfg Config) *Runtime {
	r := &Runtime{}
	r.Replace(client, cfg)
	return r
}

// Snapshot returns the client and configuration a new run must retain until it ends.
func (r *Runtime) Snapshot() RuntimeSnapshot {
	if r == nil {
		return RuntimeSnapshot{}
	}
	current := r.current.Load()
	if current == nil {
		return RuntimeSnapshot{}
	}
	return *current
}

// Replace atomically publishes the primary route used by runs that start afterward.
// Existing callers retain the immutable value returned by their earlier Snapshot.
func (r *Runtime) Replace(client Client, cfg Config) {
	if r == nil {
		return
	}
	cfg.Headers = maps.Clone(cfg.Headers)
	cfg.Prices = maps.Clone(cfg.Prices)
	r.current.Store(&RuntimeSnapshot{Client: client, Config: cfg})
}
