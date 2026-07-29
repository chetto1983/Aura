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
	// memorySubmissionTimeout covers embedding and one atomic graph transaction for the
	// complete profile rather than the shorter status-read budget.
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

// StoreConfirmed sends the deterministic profile through one host-only atomic memory
// operation. The completion sentinel commits in the same graph transaction.
func (m *memoryProfileStore) StoreConfirmed(ctx context.Context, identityID string, a onboarding.Answers) error {
	if identityID == "" {
		return fmt.Errorf("onboarding memory store: empty identity")
	}
	return m.withSession(ctx, memorySubmissionTimeout, func(ctx context.Context, cli mcp.Transport) error {
		return m.writeProfile(
			ctx,
			cli,
			identityID,
			onboarding.MapProfile(a),
			onboarding.PredicateOnboardingCompleted,
		)
	})
}

func (m *memoryProfileStore) StoreSkipped(ctx context.Context, identityID string) error {
	if identityID == "" {
		return fmt.Errorf("onboarding memory store: empty identity")
	}
	return m.withSession(ctx, memoryOneCallTimeout, func(ctx context.Context, cli mcp.Transport) error {
		return m.writeProfile(
			ctx,
			cli,
			identityID,
			onboarding.ProfileMemory{},
			onboarding.PredicateOnboardingSkipped,
		)
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

func (m *memoryProfileStore) writeProfile(
	ctx context.Context,
	cli mcp.Transport,
	identityID string,
	profile onboarding.ProfileMemory,
	completionPredicate string,
) error {
	entities := make([]map[string]any, 0, len(profile.Entities))
	for _, entity := range profile.Entities {
		entities = append(entities, map[string]any{
			"name": entity.Name, "entity_type": entity.EntityType,
			"description": entity.Description, "aliases": entity.Aliases,
		})
	}
	facts := make([]map[string]any, 0, len(profile.Facts))
	for _, fact := range profile.Facts {
		facts = append(facts, map[string]any{
			"subject": fact.Subject, "predicate": fact.Predicate, "object_value": fact.ObjectValue,
		})
	}
	preferences := make([]map[string]any, 0, len(profile.Preferences))
	for _, preference := range profile.Preferences {
		preferences = append(preferences, map[string]any{
			"category":   preference.Category,
			"preference": preference.Preference,
			"context":    preference.Context,
		})
	}
	_, err := cli.CallTool(ctx, "memory_store_profile", map[string]any{
		"entities":             entities,
		"facts":                facts,
		"preferences":          preferences,
		"completion_predicate": completionPredicate,
		"completion_value":     m.now().Format(time.RFC3339),
		"user_identifier":      identityID,
	})
	if err != nil {
		return fmt.Errorf("onboarding memory profile: %w", err)
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
