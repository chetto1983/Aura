package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/config"
	"github.com/google/uuid"
)

func TestInstallAdaptiveBenchmarkFixturesBuildsIndependentAliasesAndCleans(
	t *testing.T,
) {
	t.Parallel()
	memory := &adaptiveBenchmarkFixtureMemoryFake{
		nodes: make(map[string]string),
	}
	ownerID := uuid.Must(uuid.NewV7())
	runID := uuid.Must(uuid.NewV7())
	fixtures, err := installAdaptiveBenchmarkFixtures(
		t.Context(),
		&config.Config{},
		ownerID,
		runID,
		adaptiveBenchmarkFixtureDeps{
			openMemory: func(
				context.Context,
			) (adaptiveBenchmarkFixtureMemory, error) {
				return memory, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("installAdaptiveBenchmarkFixtures: %v", err)
	}
	if len(fixtures.memoryNodes) != len(adaptiveBenchmarkMemoryFixtures) {
		t.Fatalf("fixture counts drifted: %#v", fixtures)
	}
	for _, fixture := range adaptiveBenchmarkMemoryFixtures {
		id := memory.aliasIDs[fixture.alias]
		alias, ok := fixtures.ResolveFixtureAlias(
			adaptive.DomainMemoryRecall,
			id,
		)
		if !ok || alias != fixture.alias {
			t.Fatalf("memory alias %s=%q,%t", id, alias, ok)
		}
	}
	if _, ok := fixtures.ResolveFixtureAlias(
		adaptive.DomainMemoryRecall,
		uuid.Must(uuid.NewV7()),
	); ok {
		t.Fatal("fixture manifest resolved an unknown UUID")
	}
	if err := fixtures.Cleanup(t.Context()); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	callsAfterCleanup := memory.calls
	if err := fixtures.Cleanup(t.Context()); err != nil {
		t.Fatalf("idempotent Cleanup: %v", err)
	}
	if !memory.closed ||
		len(memory.nodes) != 0 ||
		memory.calls != callsAfterCleanup ||
		memory.forgetScopes != len(adaptiveBenchmarkMemoryFixtures)*2 {
		t.Fatalf("cleanup memory=%#v", memory)
	}
}

func TestInstallAdaptiveBenchmarkFixturesCleansPartialSeedFailure(
	t *testing.T,
) {
	t.Parallel()
	memory := &adaptiveBenchmarkFixtureMemoryFake{
		nodes:     make(map[string]string),
		failAddAt: 3,
	}
	fixtures, err := installAdaptiveBenchmarkFixtures(
		t.Context(),
		&config.Config{},
		uuid.Must(uuid.NewV7()),
		uuid.Must(uuid.NewV7()),
		adaptiveBenchmarkFixtureDeps{
			openMemory: func(
				context.Context,
			) (adaptiveBenchmarkFixtureMemory, error) {
				return memory, nil
			},
		},
	)
	if fixtures != nil || err == nil {
		t.Fatalf("fixtures=%#v error=%v", fixtures, err)
	}
	if !memory.closed || len(memory.nodes) != 0 {
		t.Fatalf("partial fixtures survived memory=%#v", memory)
	}
}

func TestAdaptiveBenchmarkFixtureManifestRejectsAmbiguity(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7())
	valid := adaptiveBenchmarkFixtureAliasEntry{
		domain: adaptive.DomainMemoryRecall,
		id:     id,
		alias:  "fact:fixture",
	}
	tests := []struct {
		name    string
		entries []adaptiveBenchmarkFixtureAliasEntry
	}{
		{name: "empty"},
		{
			name: "invalid domain",
			entries: []adaptiveBenchmarkFixtureAliasEntry{{
				domain: adaptive.DomainKnowledge,
				id:     id,
				alias:  "fixture",
			}},
		},
		{
			name:    "duplicate ID",
			entries: []adaptiveBenchmarkFixtureAliasEntry{valid, valid},
		},
		{
			name: "duplicate alias",
			entries: []adaptiveBenchmarkFixtureAliasEntry{
				valid,
				{
					domain: valid.domain,
					id:     uuid.Must(uuid.NewV7()),
					alias:  valid.alias,
				},
			},
		},
		{
			name: "unsafe alias",
			entries: []adaptiveBenchmarkFixtureAliasEntry{{
				domain: valid.domain,
				id:     id,
				alias:  " fixture",
			}},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if manifest, err := newAdaptiveBenchmarkFixtureAliasManifest(
				testCase.entries,
			); err == nil {
				t.Fatalf("accepted manifest %#v", manifest)
			}
		})
	}
}

func TestAdaptiveBenchmarkChatEnvironmentCleanupFailurePreventsSuccess(
	t *testing.T,
) {
	t.Parallel()
	cleanupErr := errors.New("fixture retention failed")
	cleanupCalls := 0
	environment := &adaptiveBenchmarkChatEnvironment{
		env: &chatEnv{},
		fixtureCleanup: func(context.Context) error {
			cleanupCalls++
			return cleanupErr
		},
	}
	if err := environment.Close(); !errors.Is(err, cleanupErr) {
		t.Fatalf("Close error=%v", err)
	}
	if err := environment.Close(); !errors.Is(err, cleanupErr) ||
		cleanupCalls != 1 {
		t.Fatalf(
			"idempotent Close error=%v calls=%d",
			err,
			cleanupCalls,
		)
	}
}

type adaptiveBenchmarkFixtureMemoryFake struct {
	nodes        map[string]string
	aliasIDs     map[string]uuid.UUID
	failAddAt    int
	addCalls     int
	calls        int
	forgetScopes int
	closed       bool
}

func (memory *adaptiveBenchmarkFixtureMemoryFake) CallTool(
	_ context.Context,
	tool string,
	args map[string]any,
) (string, error) {
	memory.calls++
	if tool == "memory_forget" {
		if scope, ok := args["user_identifier"].(string); ok && scope != "" {
			memory.forgetScopes++
		}
		id, _ := args["node_id"].(string)
		if _, exists := memory.nodes[id]; !exists {
			return `{"deleted":null,"reason":"not found"}`, nil
		}
		delete(memory.nodes, id)
		return fmt.Sprintf(`{"deleted":%q}`, id), nil
	}
	memory.addCalls++
	if memory.failAddAt == memory.addCalls {
		return "", errors.New("seed failed")
	}
	metadata, _ := args["metadata"].(map[string]any)
	alias, _ := metadata["benchmark_alias"].(string)
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(alias))
	if memory.aliasIDs == nil {
		memory.aliasIDs = make(map[string]uuid.UUID)
	}
	memory.aliasIDs[alias] = id
	memory.nodes[id.String()] = tool
	payload, _ := json.Marshal(map[string]any{
		"stored": true,
		"id":     id.String(),
		"type":   strings.TrimPrefix(tool, "memory_add_"),
	})
	return string(payload), nil
}

func (memory *adaptiveBenchmarkFixtureMemoryFake) Close() error {
	memory.closed = true
	return nil
}
