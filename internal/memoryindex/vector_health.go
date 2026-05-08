package memoryindex

import (
	"strings"
	"sync"
	"time"
)

type VectorHealth struct {
	Enabled         bool       `json:"enabled"`
	Running         bool       `json:"running"`
	Collection      string     `json:"collection,omitempty"`
	VectorSize      int        `json:"vector_size,omitempty"`
	LastStartedAt   *time.Time `json:"last_started_at,omitempty"`
	LastFinishedAt  *time.Time `json:"last_finished_at,omitempty"`
	LastDocsIndexed int        `json:"last_docs_indexed,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

type VectorHealthTracker struct {
	mu    sync.RWMutex
	state VectorHealth
	now   func() time.Time
}

func NewVectorHealthTracker(enabled bool, collection string) *VectorHealthTracker {
	return &VectorHealthTracker{
		state: VectorHealth{
			Enabled:    enabled,
			Collection: strings.TrimSpace(collection),
		},
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (t *VectorHealthTracker) SetEnabled(enabled bool, collection string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.Enabled = enabled
	if value := strings.TrimSpace(collection); value != "" {
		t.state.Collection = value
	}
}

func (t *VectorHealthTracker) Started() {
	if t == nil {
		return
	}
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.Running = true
	t.state.LastStartedAt = &now
	t.state.LastError = ""
}

func (t *VectorHealthTracker) Succeeded(report VectorReport) {
	if t == nil {
		return
	}
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.Enabled = true
	t.state.Running = false
	t.state.LastFinishedAt = &now
	t.state.LastError = ""
	if strings.TrimSpace(report.Collection) != "" {
		t.state.Collection = report.Collection
	}
	t.state.LastDocsIndexed = report.DocsIndexed
	t.state.VectorSize = report.VectorSize
}

func (t *VectorHealthTracker) Failed(err error) {
	if t == nil {
		return
	}
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.Running = false
	t.state.LastFinishedAt = &now
	if err != nil {
		t.state.LastError = err.Error()
	}
}

func (t *VectorHealthTracker) Snapshot() VectorHealth {
	if t == nil {
		return VectorHealth{}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state
}
