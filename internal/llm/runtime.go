package llm

import "sync/atomic"

// RuntimeSnapshot is one immutable client/config pair used for a complete LLM run.
type RuntimeSnapshot struct {
	Client Client
	Config Config
}

// Runtime owns the primary LLM route selected by the operator.
type Runtime struct {
	current atomic.Pointer[RuntimeSnapshot]
}

// NewRuntime publishes the boot-time primary LLM route.
func NewRuntime(client Client, cfg Config) *Runtime {
	r := &Runtime{}
	r.current.Store(&RuntimeSnapshot{Client: client, Config: cfg})
	return r
}

// Snapshot returns the client and configuration a new run must retain until it ends.
func (r *Runtime) Snapshot() RuntimeSnapshot {
	if r == nil || r.current.Load() == nil {
		return RuntimeSnapshot{}
	}
	return *r.current.Load()
}

// Replace publishes a new primary route. The RED stub deliberately leaves the boot
// snapshot unchanged until the hot-reload implementation lands.
func (r *Runtime) Replace(Client, Config) {}
