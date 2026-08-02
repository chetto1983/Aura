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
	"strings"
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

// multiValuedPredicates are the ones a person can hold several of at once. Everything
// else supersedes: you have one role, one timezone, one preferred language, and a second
// submission means the first is no longer true — which is what the bitemporal model is
// for. Getting this backwards either buries the current answer under stale ones or
// silently deletes a colleague every time another is added.
var multiValuedPredicates = map[string]struct{}{
	"knows":          {},
	"also_known_as":  {},
	"prefers_system": {},
}

func (m *memoryProfileStore) writeProfile(
	ctx context.Context,
	cli mcp.Transport,
	identityID string,
	profile onboarding.ProfileMemory,
	completionPredicate string,
) error {
	now := m.now()
	runID := "onboarding-" + now.Format(time.RFC3339)
	for _, fact := range profileFacts(profile, identityID, completionPredicate, now) {
		if err := m.upsert(ctx, cli, identityID, runID, fact); err != nil {
			return err
		}
	}
	return nil
}

// profileFact is one bitemporal edge. `statement` is deliberately a sentence and not a
// restated triple: it is the text that gets embedded and searched, and "name role
// engineer" retrieves far worse than "Davide works as an engineer".
type profileFact struct{ subject, predicate, object, statement string }

// profileFacts flattens the deterministic profile projection into the ONE write the
// memory server has. Entities become facts too — in a fact graph the subject and the
// object ARE the entities, so an entity is `X is_a PERSON` and nothing is lost. The
// completion sentinel is the last fact so a reader that sees it knows the rest landed.
func profileFacts(
	profile onboarding.ProfileMemory,
	identityID, completionPredicate string,
	now time.Time,
) []profileFact {
	facts := make([]profileFact, 0,
		len(profile.Entities)+len(profile.Facts)+len(profile.Preferences)+1)

	for _, entity := range profile.Entities {
		statement := entity.Name + " is a " + strings.ToLower(entity.EntityType)
		if entity.Description != "" {
			statement += " (" + entity.Description + ")"
		}
		facts = append(facts, profileFact{entity.Name, "is_a", entity.EntityType, statement})
		for _, alias := range entity.Aliases {
			if alias == "" || alias == entity.Name {
				continue
			}
			facts = append(facts, profileFact{
				entity.Name, "also_known_as", alias, entity.Name + " is also known as " + alias,
			})
		}
	}

	for _, fact := range profile.Facts {
		facts = append(facts, profileFact{
			fact.Subject, fact.Predicate, fact.ObjectValue,
			fact.Subject + " " + strings.ReplaceAll(fact.Predicate, "_", " ") + " " + fact.ObjectValue,
		})
	}

	for _, preference := range profile.Preferences {
		statement := preference.Preference
		if preference.Context != "" {
			statement += " (" + preference.Context + ")"
		}
		facts = append(facts, profileFact{
			identityID, "prefers_" + preference.Category, preference.Preference, statement,
		})
	}

	// The sentinel is subject-scoped to the identity because that is what Status reads
	// back: memory_facts_about walks one entity's edges, so the marker has to hang off
	// the identity itself rather than off the operator's name.
	stamp := now.Format(time.RFC3339)
	return append(facts, profileFact{
		identityID, completionPredicate, stamp, "onboarding " +
			strings.TrimPrefix(completionPredicate, "onboarding_") + " at " + stamp,
	})
}

func (m *memoryProfileStore) upsert(
	ctx context.Context,
	cli mcp.Transport,
	identityID, runID string,
	fact profileFact,
) error {
	_, multi := multiValuedPredicates[fact.predicate]
	_, err := cli.CallTool(ctx, "memory_upsert_fact", map[string]any{
		"user_identifier": identityID,
		"subject":         fact.subject,
		"predicate":       fact.predicate,
		"object":          fact.object,
		"statement":       fact.statement,
		"supersedes":      !multi,
		"source_run_id":   runID,
	})
	if err != nil {
		return fmt.Errorf("onboarding memory fact %s/%s: %w", fact.subject, fact.predicate, err)
	}
	return nil
}

// Status reads the onboarding sentinel facts scoped to identityID. memory_facts_about
// walks one entity's edges by exact name, which is why writeProfile hangs the sentinel
// off the identity rather than off the operator's name. The predicate filter stays
// client-side: the tool takes one predicate and we look for either of two.
func (m *memoryProfileStore) Status(ctx context.Context, identityID string) (onboarding.OnboardingState, error) {
	if identityID == "" {
		return onboarding.OnboardingState{}, nil
	}
	var text string
	err := m.withSession(ctx, memoryOneCallTimeout, func(ctx context.Context, cli mcp.Transport) error {
		out, err := cli.CallTool(ctx, "memory_facts_about", map[string]any{
			"entity": identityID, "user_identifier": identityID,
		})
		text = out
		return err
	})
	if err != nil {
		return onboarding.OnboardingState{}, err
	}
	// No `error` field: the memory server reports failure as an MCP error, which
	// CallTool already returns above. A struct field for an error nobody sends is a
	// branch that can never run.
	var res struct {
		Facts []struct {
			Predicate string `json:"predicate"`
		} `json:"facts"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return onboarding.OnboardingState{}, fmt.Errorf("onboarding status decode: %w", err)
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
