package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ActionRouter dispatches a single tool's `action` enum to a per-action handler
// (D-09). It is the first consumer of this pattern — the `task` tool routes
// schedule|list|cancel|run_now|approve through one Execute — and is kept generic
// (no cron-specific types) so Slice 7 (skill_*) reuses it verbatim. The point of
// the router is to keep ONE manifest entry and ONE OpenAI-wire-safe schema per
// multi-action tool instead of N near-duplicate tool files (the pre-rewrite
// 587-LOC scheduler.go god-tool is the anti-pattern this replaces).
type ActionRouter struct {
	handlers map[string]ActionFunc
}

// ActionFunc is one action handler: it receives the still-raw tool arguments
// (the whole object, including the `action` field) and returns a ToolResult or
// an error. Handlers re-unmarshal the fields they need from raw — the router does
// not pre-parse beyond the `action` discriminator.
type ActionFunc func(ctx context.Context, args json.RawMessage) (ToolResult, error)

// NewActionRouter builds a router from an action→handler map. The map is copied
// so a later mutation of the caller's map cannot change dispatch behavior.
func NewActionRouter(handlers map[string]ActionFunc) *ActionRouter {
	cp := make(map[string]ActionFunc, len(handlers))
	for name, fn := range handlers {
		cp[name] = fn
	}
	return &ActionRouter{handlers: cp}
}

// Actions returns the registered action names, sorted — for building the schema's
// `action` enum and for the unknown-action error message (a stable, testable order).
func (r *ActionRouter) Actions() []string {
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Dispatch routes action to its handler. An unknown action returns a structured
// error (never a panic) naming the valid actions, so a model that hallucinates an
// action gets an actionable correction rather than a crash.
func (r *ActionRouter) Dispatch(ctx context.Context, action string, args json.RawMessage) (ToolResult, error) {
	fn, ok := r.handlers[action]
	if !ok {
		return ToolResult{}, fmt.Errorf("unknown action %q: valid actions are %s", action, strings.Join(r.Actions(), ", "))
	}
	return fn(ctx, args)
}
