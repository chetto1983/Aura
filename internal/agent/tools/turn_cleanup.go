package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// TurnCleanup collects what a turn leaves in the box and must remove when the turn
// ends: today, the directory MCP files are materialized into (MCPFileSink).
// LlmAgent.Run installs one per run and drains it from its outermost defer, so a
// normal end, an ask_user pause, an error and a panic all reach it. Every swarm
// worker is its own Run, so each gets its own.
type TurnCleanup struct {
	mu    sync.Mutex
	keys  map[string]struct{}
	steps []cleanupStep
}

// cleanupStep pairs a registered step with the key it was added under, so a panic
// caught in Run can name which step it came from.
type cleanupStep struct {
	key  string
	step func(context.Context) error
}

// Add registers step under key. A key already registered is not registered twice,
// so every call of a turn can ask for the same directory to be removed. A nil
// TurnCleanup ignores it.
func (c *TurnCleanup) Add(key string, step func(context.Context) error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.keys[key]; ok {
		return
	}
	if c.keys == nil {
		c.keys = map[string]struct{}{}
	}
	c.keys[key] = struct{}{}
	c.steps = append(c.steps, cleanupStep{key: key, step: step})
}

// Run runs every registered step once, newest first, and forgets them. A failing
// step — including one that panics — does not stop the rest; the failures come back
// joined.
func (c *TurnCleanup) Run(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	steps := c.steps
	c.steps, c.keys = nil, nil
	c.mu.Unlock()
	var errs []error
	for _, s := range slices.Backward(steps) {
		if err := runStep(ctx, s.key, s.step); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// runStep runs one step and converts a panic into an error naming key, mirroring
// the recoverHook (hooks.go) / runToolRecovering (llm_agent_parallel.go) idiom: one
// bad step (say, a nil sandbox handle) must not sink the rest of the drain, and must
// not escape Run into the caller's outermost defer unlogged.
func runStep(ctx context.Context, key string, step func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("turn cleanup %q panicked: %v", key, r)
		}
	}()
	return step(ctx)
}

type turnCleanupCtxKey struct{}

// WithTurnCleanup returns ctx carrying a fresh TurnCleanup. A nested run gets its
// own, which shadows the outer one for everything that run calls.
func WithTurnCleanup(ctx context.Context) (context.Context, *TurnCleanup) {
	cleanup := &TurnCleanup{}
	return context.WithValue(ctx, turnCleanupCtxKey{}, cleanup), cleanup
}

// TurnCleanupFromContext returns the TurnCleanup of the run ctx belongs to, or nil
// outside one: toolpipe, the docs MCP and a readiness probe have no turn.
func TurnCleanupFromContext(ctx context.Context) *TurnCleanup {
	cleanup, _ := ctx.Value(turnCleanupCtxKey{}).(*TurnCleanup)
	return cleanup
}
