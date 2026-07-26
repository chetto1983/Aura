// memory_onboarding.go: the Amendment #87 replacement for the on-disk profile.Store.
// It implements onboarding.ProfileMemoryStore over the managed memory MCP — the seed
// form's mapped Answers become memory_add_* writes and completion is a sentinel fact —
// and reads that sentinel back for /api/onboarding/status. Amendment #95: the whole
// submission rides ONE MCP session, and no :Message node is written.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/onboarding"
)

const (
	// memorySubmissionTimeout budgets the WHOLE seed submission, not one write: a full seed
	// maps to nine sequential tool calls over a single connection, so the per-call 20s of
	// callMemoryToolText would be the wrong shape here (a slow sidecar would trip on write 2
	// while write 1 still counted against nothing).
	memorySubmissionTimeout = 60 * time.Second
	// memoryOneCallTimeout keeps the single-call paths (the skip sentinel, the status read
	// every authenticated page load makes) on callMemoryToolText's original per-call budget.
	// Sharing the submission budget here would silently triple how long a dead sidecar can
	// hang a read.
	memoryOneCallTimeout = 20 * time.Second
)

// memoryProfileStore drives the memory MCP. Every call carries user_identifier =
// identityID: the transport does NOT inject it (only the agent bridge does), so a bare
// call would land in the memory server's fail-open GLOBAL scope and leak across tenants
// in MUSR mode (mirrors serve_recall.go). The open + now seams make it daemon-free
// unit-testable.
type memoryProfileStore struct {
	open func(ctx context.Context) (mcp.Transport, error)
	now  func() time.Time
}

func newMemoryProfileStore() *memoryProfileStore {
	return &memoryProfileStore{
		open: openMemoryMCP,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

// StoreConfirmed maps the seed onto entity/fact/preference writes and stamps the
// completion sentinel LAST, all over ONE MCP session (Amendment #95's "one connection
// for the whole submission" — nine writes used to mean nine handshakes).
func (m *memoryProfileStore) StoreConfirmed(ctx context.Context, identityID string, a onboarding.Answers) error {
	if identityID == "" {
		return fmt.Errorf("onboarding memory store: empty identity")
	}
	return m.withSession(ctx, memorySubmissionTimeout, func(ctx context.Context, cli mcp.Transport) error {
		pm := onboarding.MapProfile(a)
		for _, e := range pm.Entities {
			args := map[string]any{"name": e.Name, "entity_type": e.EntityType}
			if e.Description != "" {
				args["description"] = e.Description
			}
			if len(e.Aliases) > 0 {
				args["aliases"] = e.Aliases
			}
			if err := m.write(ctx, cli, identityID, "memory_add_entity", args); err != nil {
				return err
			}
		}
		for _, f := range pm.Facts {
			if err := m.write(ctx, cli, identityID, "memory_add_fact", map[string]any{
				"subject": f.Subject, "predicate": f.Predicate, "object_value": f.ObjectValue,
			}); err != nil {
				return err
			}
		}
		for _, p := range pm.Preferences {
			args := map[string]any{"category": p.Category, "preference": p.Preference}
			if p.Context != "" {
				args["context"] = p.Context
			}
			if err := m.write(ctx, cli, identityID, "memory_add_preference", args); err != nil {
				return err
			}
		}
		return m.writeSentinel(ctx, cli, identityID, onboarding.PredicateOnboardingCompleted)
	})
}

func (m *memoryProfileStore) StoreSkipped(ctx context.Context, identityID string) error {
	if identityID == "" {
		return fmt.Errorf("onboarding memory store: empty identity")
	}
	return m.withSession(ctx, memoryOneCallTimeout, func(ctx context.Context, cli mcp.Transport) error {
		return m.writeSentinel(ctx, cli, identityID, onboarding.PredicateOnboardingSkipped)
	})
}

// withSession opens one memory MCP connection, runs fn over it, and closes it. budget
// covers the whole body, so every call fn makes shares one deadline.
func (m *memoryProfileStore) withSession(ctx context.Context, budget time.Duration, fn func(context.Context, mcp.Transport) error) error {
	sessCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	cli, err := m.open(sessCtx)
	if err != nil {
		return fmt.Errorf("onboarding memory session: %w", err)
	}
	defer func() { _ = cli.Close() }()
	return fn(sessCtx, cli)
}

// writeSentinel records the onboarding-complete/skip marker as a fact whose subject is
// the identity UUID (never the operator name), so Status reads it back cleanly.
func (m *memoryProfileStore) writeSentinel(ctx context.Context, cli mcp.Transport, identityID, predicate string) error {
	return m.write(ctx, cli, identityID, "memory_add_fact", map[string]any{
		"subject": identityID, "predicate": predicate, "object_value": m.now().Format(time.RFC3339),
	})
}

// write sets the mandatory user_identifier scope on EVERY memory call (fail-open guard).
// It calls the transport directly, bypassing scopeMemoryArgs — this stamp is the ONLY
// thing keeping these writes out of the memory server's GLOBAL scope, so every write on
// this path must go through here.
func (m *memoryProfileStore) write(ctx context.Context, cli mcp.Transport, identityID, tool string, args map[string]any) error {
	args["user_identifier"] = identityID
	if _, err := cli.CallTool(ctx, tool, args); err != nil {
		return fmt.Errorf("onboarding memory %s: %w", tool, err)
	}
	return nil
}

// Status reads the onboarding sentinel facts scoped to identityID. The predicate filter
// is client-side because memory_get_facts matches only on subject.
func (m *memoryProfileStore) Status(ctx context.Context, identityID string) (onboarding.OnboardingState, error) {
	if identityID == "" {
		return onboarding.OnboardingState{}, nil
	}
	var text string
	err := m.withSession(ctx, memoryOneCallTimeout, func(ctx context.Context, cli mcp.Transport) error {
		out, err := cli.CallTool(ctx, "memory_get_facts", map[string]any{
			"subject": identityID, "user_identifier": identityID,
		})
		text = out
		return err
	})
	if err != nil {
		return onboarding.OnboardingState{}, err
	}
	var res struct {
		Facts []struct {
			Predicate string `json:"predicate"`
		} `json:"facts"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return onboarding.OnboardingState{}, fmt.Errorf("onboarding status decode: %w", err)
	}
	if res.Error != "" {
		return onboarding.OnboardingState{}, fmt.Errorf("onboarding status: %s", res.Error)
	}
	var st onboarding.OnboardingState
	for _, f := range res.Facts {
		switch f.Predicate {
		case onboarding.PredicateOnboardingCompleted:
			st.Completed = true
		case onboarding.PredicateOnboardingSkipped:
			st.Skipped = true
		}
	}
	return st, nil
}
