// Package toolselectstore persists bounded oracle-confirmed tool examples.
package toolselectstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/neostore"
)

const (
	defaultBucketCap       = 512
	defaultStoreCap        = 10_000
	defaultExampleTTL      = 90 * 24 * time.Hour
	defaultPinnedLoadLimit = 10_000
	learningPolicyVersion  = "learning-v1"
)

var toolSpec = neostore.LearningNodeSpec{
	Label: "ToolSelectionExample", SeedLabel: "ToolSelectionSeed", BucketField: "tool",
	ContentField: "query", ValueField: "tool", Store: "tool_selection",
}

var loadQuery = neostore.LearnedLoadQuery(toolSpec)
var saveQuery = neostore.LearnedSaveQuery(toolSpec)
var pinnedLoadQuery = neostore.PinnedLoadQuery(toolSpec)
var pinnedSaveQuery = neostore.PinnedSaveQuery(toolSpec)

// LabeledVec is one bounded tool-selection example.
type LabeledVec struct {
	Tool  string
	Vec   []float64
	Query string
}

// SaveResult is the typed outcome of a learned-example write admission.
type SaveResult string

const (
	// SaveCreated reports a newly admitted example.
	SaveCreated SaveResult = "created"
	// SaveUpdated reports a same-hash idempotent update.
	SaveUpdated SaveResult = "updated"
	// SaveAtCapacity reports a hard-cap rejection.
	SaveAtCapacity SaveResult = "at_capacity"
)

// Store reads and writes bounded tool-selection examples through the graph seam.
type Store struct {
	Client          neostore.GraphClient
	Now             func() time.Time
	BucketCap       int
	StoreCap        int
	ExampleTTL      time.Duration
	PinnedLoadLimit int
	ValidTool       func(string) bool
	mu              sync.Mutex
}

// LoadExamples returns learned examples using server-side TTL and cap bounds.
func (s *Store) LoadExamples(ctx context.Context) ([]LabeledVec, error) {
	rows, err := s.Client.Read(ctx, loadQuery, map[string]any{
		"cutoff":       s.now().Add(-s.exampleTTL()).Format(time.RFC3339Nano),
		"bucket_limit": s.bucketCap(), "store_limit": s.storeCap(),
	})
	if err != nil {
		return nil, fmt.Errorf("load learned tool examples: %w", err)
	}
	return s.parseRows(rows), nil
}

// Save preserves the historical Saver API while surfacing capacity as an error.
func (s *Store) Save(ctx context.Context, query, tool string, vec []float64) error {
	result, err := s.SaveLearned(ctx, query, tool, vec, 0.5, 0.5)
	if err != nil {
		return err
	}
	if result == SaveAtCapacity {
		return nil
	}
	return nil
}

// SaveLearned persists a scored learned example through atomic cap admission.
func (s *Store) SaveLearned(ctx context.Context, query, tool string, vec []float64, quality, novelty float64) (SaveResult, error) {
	if !s.validTool(tool) || len(vec) == 0 {
		return "", fmt.Errorf("toolselectstore: invalid tool or empty embedding")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().Format(time.RFC3339Nano)
	rows, err := s.Client.Write(ctx, saveQuery, map[string]any{"rows": []map[string]any{{
		"hash": neostore.HashText(query), "label": tool, "bucket": tool,
		"tool": tool, "embedding": vec, "content": query, "query": query,
		"source": "learned", "store": toolSpec.Store,
		"created_at": now, "updated_at": now, "quality": neostore.ClampLearningScore(quality),
		"novelty": neostore.ClampLearningScore(novelty), "bucket_cap": s.bucketCap(),
		"store_cap": s.storeCap(), "policy_version": learningPolicyVersion,
	}}})
	if err != nil {
		return "", fmt.Errorf("save learned tool example: %w", err)
	}
	return parseSaveResult(rows)
}

// SavePinned persists a manual evaluation seed outside learned capacity.
func (s *Store) SavePinned(ctx context.Context, query, tool string, vec []float64) error {
	if !s.validTool(tool) || len(vec) == 0 {
		return fmt.Errorf("toolselectstore: invalid pinned seed")
	}
	now := s.now().Format(time.RFC3339Nano)
	_, err := s.Client.Write(ctx, pinnedSaveQuery, map[string]any{"rows": []map[string]any{{
		"hash": neostore.HashText(query), "label": tool, "embedding": vec,
		"content": query, "created_at": now, "updated_at": now,
	}}})
	if err != nil {
		return fmt.Errorf("save pinned tool seed: %w", err)
	}
	return nil
}

// LoadPinnedExamples loads a separately bounded manual seed set.
func (s *Store) LoadPinnedExamples(ctx context.Context) ([]LabeledVec, error) {
	rows, err := s.Client.Read(ctx, pinnedLoadQuery, map[string]any{"limit": s.pinnedLimit()})
	if err != nil {
		return nil, fmt.Errorf("load pinned tool seeds: %w", err)
	}
	return s.parseRows(rows), nil
}

func (s *Store) parseRows(rows []map[string]any) []LabeledVec {
	out := make([]LabeledVec, 0, len(rows))
	for _, row := range rows {
		label := row["label"]
		if label == nil {
			label = row["tool"]
		}
		tool := neostore.AsString(label)
		vec := neostore.AsFloats(row["embedding"])
		content := row["content"]
		if content == nil {
			content = row["query"]
		}
		if s.validTool(tool) && len(vec) > 0 {
			out = append(out, LabeledVec{Tool: tool, Vec: vec, Query: neostore.AsString(content)})
		}
	}
	return out
}

func parseSaveResult(rows []map[string]any) (SaveResult, error) {
	if len(rows) != 1 {
		return "", fmt.Errorf("toolselectstore: missing save status")
	}
	result := SaveResult(neostore.AsString(rows[0]["status"]))
	if result != SaveCreated && result != SaveUpdated && result != SaveAtCapacity {
		return "", fmt.Errorf("toolselectstore: invalid save status")
	}
	return result, nil
}

func (s *Store) validTool(tool string) bool {
	if tool == "" {
		return false
	}
	return s.ValidTool == nil || s.ValidTool(tool)
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Store) bucketCap() int {
	if s.BucketCap > 0 {
		return s.BucketCap
	}
	return defaultBucketCap
}

func (s *Store) storeCap() int {
	if s.StoreCap > 0 {
		return s.StoreCap
	}
	return defaultStoreCap
}

func (s *Store) exampleTTL() time.Duration {
	if s.ExampleTTL > 0 {
		return s.ExampleTTL
	}
	return defaultExampleTTL
}

func (s *Store) pinnedLimit() int {
	if s.PinnedLoadLimit > 0 {
		return s.PinnedLoadLimit
	}
	return defaultPinnedLoadLimit
}
