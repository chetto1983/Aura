package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/config"
	"github.com/google/uuid"
)

const adaptiveBenchmarkFixtureCleanupTimeout = 60 * time.Second

type adaptiveBenchmarkFixtureMemory interface {
	CallTool(context.Context, string, map[string]any) (string, error)
	Close() error
}

type adaptiveBenchmarkFixtureDeps struct {
	openMemory func(context.Context) (adaptiveBenchmarkFixtureMemory, error)
}

type adaptiveBenchmarkFixtureAliasEntry struct {
	domain adaptive.Domain
	id     uuid.UUID
	alias  string
}

type adaptiveBenchmarkFixtureAliasManifest struct {
	aliases map[adaptive.Domain]map[uuid.UUID]string
}

func newAdaptiveBenchmarkFixtureAliasManifest(
	entries []adaptiveBenchmarkFixtureAliasEntry,
) (*adaptiveBenchmarkFixtureAliasManifest, error) {
	if len(entries) == 0 {
		return nil, errors.New("adaptive benchmark fixture aliases are empty")
	}
	aliases := make(
		map[adaptive.Domain]map[uuid.UUID]string,
		2,
	)
	seenAliases := make(map[adaptive.Domain]map[string]struct{}, 2)
	for _, entry := range entries {
		if entry.domain != adaptive.DomainMemoryRecall ||
			entry.id == uuid.Nil ||
			!validAdaptiveBenchmarkFixtureAlias(entry.alias) {
			return nil, errors.New(
				"adaptive benchmark fixture alias is invalid",
			)
		}
		if aliases[entry.domain] == nil {
			aliases[entry.domain] = make(map[uuid.UUID]string)
			seenAliases[entry.domain] = make(map[string]struct{})
		}
		if _, duplicate := aliases[entry.domain][entry.id]; duplicate {
			return nil, errors.New(
				"adaptive benchmark fixture ID is duplicated",
			)
		}
		if _, duplicate := seenAliases[entry.domain][entry.alias]; duplicate {
			return nil, errors.New(
				"adaptive benchmark fixture alias is duplicated",
			)
		}
		aliases[entry.domain][entry.id] = entry.alias
		seenAliases[entry.domain][entry.alias] = struct{}{}
	}
	return &adaptiveBenchmarkFixtureAliasManifest{aliases: aliases}, nil
}

func (manifest *adaptiveBenchmarkFixtureAliasManifest) ResolveFixtureAlias(
	domain adaptive.Domain,
	id uuid.UUID,
) (string, bool) {
	if manifest == nil || id == uuid.Nil {
		return "", false
	}
	alias, ok := manifest.aliases[domain][id]
	return alias, ok
}

type adaptiveBenchmarkFixtureMemoryNode struct {
	nodeType string
	id       uuid.UUID
}

type adaptiveBenchmarkFixtureSet struct {
	*adaptiveBenchmarkFixtureAliasManifest
	memory      adaptiveBenchmarkFixtureMemory
	ownerID     uuid.UUID
	memoryNodes []adaptiveBenchmarkFixtureMemoryNode

	cleanupOnce sync.Once
	cleanupErr  error
}

func productionAdaptiveBenchmarkFixtureDeps() adaptiveBenchmarkFixtureDeps {
	return adaptiveBenchmarkFixtureDeps{
		openMemory: func(
			ctx context.Context,
		) (adaptiveBenchmarkFixtureMemory, error) {
			return openMemoryMCP(ctx)
		},
	}
}

func installAdaptiveBenchmarkFixtures(
	ctx context.Context,
	cfg *config.Config,
	ownerID uuid.UUID,
	runID uuid.UUID,
	deps adaptiveBenchmarkFixtureDeps,
) (_ *adaptiveBenchmarkFixtureSet, returnedErr error) {
	if cfg == nil ||
		ownerID == uuid.Nil ||
		runID == uuid.Nil ||
		deps.openMemory == nil {
		return nil, errors.New(
			"adaptive benchmark fixture dependencies are invalid",
		)
	}
	fixtures := &adaptiveBenchmarkFixtureSet{ownerID: ownerID}
	defer func() {
		if returnedErr == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			adaptiveBenchmarkFixtureCleanupTimeout,
		)
		returnedErr = errors.Join(returnedErr, fixtures.Cleanup(cleanupCtx))
		cancel()
	}()
	memory, err := deps.openMemory(ctx)
	if err != nil {
		return nil, fmt.Errorf("open adaptive benchmark fixture memory: %w", err)
	}
	if memory == nil {
		return nil, errors.New(
			"open adaptive benchmark fixture memory: unavailable",
		)
	}
	fixtures.memory = memory
	entries, err := fixtures.seedMemory(ctx, ownerID, runID)
	if err != nil {
		return nil, err
	}
	manifest, err := newAdaptiveBenchmarkFixtureAliasManifest(entries)
	if err != nil {
		return nil, err
	}
	fixtures.adaptiveBenchmarkFixtureAliasManifest = manifest
	return fixtures, nil
}

func (fixtures *adaptiveBenchmarkFixtureSet) Cleanup(
	ctx context.Context,
) error {
	if fixtures == nil {
		return nil
	}
	fixtures.cleanupOnce.Do(func() {
		var cleanupErr error
		if fixtures.memory != nil {
			cleanupErr = errors.Join(
				cleanupErr,
				fixtures.cleanupMemory(ctx),
				fixtures.memory.Close(),
			)
			fixtures.memory = nil
		}
		fixtures.cleanupErr = cleanupErr
	})
	return fixtures.cleanupErr
}

func decodeAdaptiveBenchmarkFixtureJSON(
	payload string,
) (map[string]json.RawMessage, error) {
	var decoded map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New(
			"adaptive benchmark fixture response has trailing data",
		)
	}
	if rawError := decoded["error"]; len(rawError) != 0 &&
		string(rawError) != `""` &&
		string(rawError) != "null" {
		return nil, errors.New(
			"adaptive benchmark fixture service rejected request",
		)
	}
	return decoded, nil
}
