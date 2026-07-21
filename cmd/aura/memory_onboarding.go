// memory_onboarding.go: the Amendment #87 replacement for the on-disk profile.Store.
// It implements onboarding.ProfileMemoryStore over the managed memory MCP — confirmed
// interview answers become memory_add_* writes, the raw draft is stored as a message
// safety net, and completion is a sentinel fact — and reads that sentinel back for
// /api/onboarding/status.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/onboarding"
)

// memoryProfileStore drives the memory MCP. Every call carries user_identifier =
// identityID: callMemoryToolText does NOT inject it (only the agent bridge does), so a
// bare call would land in the memory server's fail-open GLOBAL scope and leak across
// tenants in MUSR mode (mirrors serve_recall.go). The call + now seams make it
// daemon-free unit-testable.
type memoryProfileStore struct {
	call func(ctx context.Context, tool string, args map[string]any) (string, error)
	now  func() time.Time
}

func newMemoryProfileStore() *memoryProfileStore {
	return &memoryProfileStore{
		call: callMemoryToolText,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (m *memoryProfileStore) StoreConfirmed(ctx context.Context, identityID string, a onboarding.Answers, rawDraft string) error {
	if identityID == "" {
		return fmt.Errorf("onboarding memory store: empty identity")
	}
	pm := onboarding.MapProfile(a)
	for _, e := range pm.Entities {
		args := map[string]any{"name": e.Name, "entity_type": e.EntityType}
		if e.Description != "" {
			args["description"] = e.Description
		}
		if len(e.Aliases) > 0 {
			args["aliases"] = e.Aliases
		}
		if err := m.write(ctx, identityID, "memory_add_entity", args); err != nil {
			return err
		}
	}
	for _, f := range pm.Facts {
		if err := m.write(ctx, identityID, "memory_add_fact", map[string]any{
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
		if err := m.write(ctx, identityID, "memory_add_preference", args); err != nil {
			return err
		}
	}
	if rawDraft != "" {
		// Raw answers safety net (extraction is lossy — verbatim keeps the details the
		// deterministic mapper generalized away).
		if err := m.write(ctx, identityID, "memory_store_message", map[string]any{
			"content": rawDraft, "role": "user",
		}); err != nil {
			return err
		}
	}
	return m.writeSentinel(ctx, identityID, onboarding.PredicateOnboardingCompleted)
}

func (m *memoryProfileStore) StoreSkipped(ctx context.Context, identityID string) error {
	if identityID == "" {
		return fmt.Errorf("onboarding memory store: empty identity")
	}
	return m.writeSentinel(ctx, identityID, onboarding.PredicateOnboardingSkipped)
}

// writeSentinel records the onboarding-complete/skip marker as a fact whose subject is
// the identity UUID (never the operator name), so Status reads it back cleanly.
func (m *memoryProfileStore) writeSentinel(ctx context.Context, identityID, predicate string) error {
	return m.write(ctx, identityID, "memory_add_fact", map[string]any{
		"subject": identityID, "predicate": predicate, "object_value": m.now().Format(time.RFC3339),
	})
}

// write sets the mandatory user_identifier scope on EVERY memory call (fail-open guard).
func (m *memoryProfileStore) write(ctx context.Context, identityID, tool string, args map[string]any) error {
	args["user_identifier"] = identityID
	if _, err := m.call(ctx, tool, args); err != nil {
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
	text, err := m.call(ctx, "memory_get_facts", map[string]any{
		"subject": identityID, "user_identifier": identityID,
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
