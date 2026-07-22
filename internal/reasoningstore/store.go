// Package reasoningstore persists bounded oracle-labeled reasoning examples.
package reasoningstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/neostore"
)

const (
	defaultBucketCap       = 512
	defaultStoreCap        = 10_000
	defaultExampleTTL      = 90 * 24 * time.Hour
	defaultPinnedLoadLimit = 10_000
	learningPolicyVersion  = "learning-v1"
)

var reasoningSpec = neostore.LearningNodeSpec{
	Label: "ReasoningExample", SeedLabel: "ReasoningSeed", BucketField: "tier",
	ContentField: "text", ValueField: "tier", Store: "reasoning",
}

var loadQuery = neostore.LearnedLoadQuery(reasoningSpec)
var saveQuery = neostore.LearnedSaveQuery(reasoningSpec)
var pinnedLoadQuery = neostore.PinnedLoadQuery(reasoningSpec)
var pinnedSaveQuery = neostore.PinnedSaveQuery(reasoningSpec)

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

// Store reads and writes bounded reasoning examples through the graph seam.
type Store struct {
	Client          neostore.GraphClient
	Now             func() time.Time
	BucketCap       int
	StoreCap        int
	ExampleTTL      time.Duration
	PinnedLoadLimit int
	mu              sync.Mutex
}

// LoadExamples returns learned examples using server-side TTL and cap bounds.
func (s *Store) LoadExamples(ctx context.Context) ([]prompt.LabeledVec, error) {
	rows, err := s.Client.Read(ctx, loadQuery, map[string]any{
		"cutoff":       s.now().Add(-s.exampleTTL()).Format(time.RFC3339Nano),
		"bucket_limit": s.bucketCap(), "store_limit": s.storeCap(),
	})
	if err != nil {
		return nil, fmt.Errorf("load learned reasoning examples: %w", err)
	}
	return parseRows(rows), nil
}

// Save preserves the historical Saver API while surfacing capacity as a typed error.
func (s *Store) Save(ctx context.Context, text string, vec []float64, tier prompt.ReasoningTier) error {
	result, err := s.SaveLearned(ctx, text, vec, tier, 0.5, 0.5)
	if err != nil {
		return err
	}
	if result == SaveAtCapacity {
		return nil
	}
	return nil
}

// SaveLearned persists a scored learned example through atomic cap admission.
func (s *Store) SaveLearned(ctx context.Context, text string, vec []float64, tier prompt.ReasoningTier, quality, novelty float64) (SaveResult, error) {
	if !tier.Valid() || len(vec) == 0 {
		return "", fmt.Errorf("reasoningstore: invalid tier or empty embedding")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().Format(time.RFC3339Nano)
	rows, err := s.Client.Write(ctx, saveQuery, map[string]any{"rows": []map[string]any{{
		"hash": neostore.HashText(text), "label": string(tier), "bucket": string(tier),
		"tier": string(tier), "embedding": vec, "content": text, "text": text,
		"source": "learned", "store": reasoningSpec.Store,
		"created_at": now, "updated_at": now, "quality": neostore.ClampLearningScore(quality),
		"novelty": neostore.ClampLearningScore(novelty), "bucket_cap": s.bucketCap(),
		"store_cap": s.storeCap(), "policy_version": learningPolicyVersion,
	}}})
	if err != nil {
		return "", fmt.Errorf("save learned reasoning example: %w", err)
	}
	return parseSaveResult(rows)
}

// SavePinned persists a manual evaluation seed outside learned capacity.
func (s *Store) SavePinned(ctx context.Context, text string, vec []float64, tier prompt.ReasoningTier) error {
	if !tier.Valid() || len(vec) == 0 {
		return fmt.Errorf("reasoningstore: invalid pinned seed")
	}
	now := s.now().Format(time.RFC3339Nano)
	_, err := s.Client.Write(ctx, pinnedSaveQuery, map[string]any{"rows": []map[string]any{{
		"hash": neostore.HashText(text), "label": string(tier), "embedding": vec,
		"content": text, "created_at": now, "updated_at": now,
	}}})
	if err != nil {
		return fmt.Errorf("save pinned reasoning seed: %w", err)
	}
	return nil
}

// LoadPinnedExamples loads a separately bounded manual seed set.
func (s *Store) LoadPinnedExamples(ctx context.Context) ([]prompt.LabeledVec, error) {
	rows, err := s.Client.Read(ctx, pinnedLoadQuery, map[string]any{"limit": s.pinnedLimit()})
	if err != nil {
		return nil, fmt.Errorf("load pinned reasoning seeds: %w", err)
	}
	return parseRows(rows), nil
}

func parseRows(rows []map[string]any) []prompt.LabeledVec {
	out := make([]prompt.LabeledVec, 0, len(rows))
	for _, row := range rows {
		label := row["label"]
		if label == nil {
			label = row["tier"]
		}
		tier := prompt.ReasoningTier(neostore.AsString(label))
		vec := neostore.AsFloats(row["embedding"])
		if tier.Valid() && len(vec) > 0 {
			out = append(out, prompt.LabeledVec{Tier: tier, Vec: vec})
		}
	}
	return out
}

func parseSaveResult(rows []map[string]any) (SaveResult, error) {
	if len(rows) != 1 {
		return "", fmt.Errorf("reasoningstore: missing save status")
	}
	result := SaveResult(neostore.AsString(rows[0]["status"]))
	if result != SaveCreated && result != SaveUpdated && result != SaveAtCapacity {
		return "", fmt.Errorf("reasoningstore: invalid save status")
	}
	return result, nil
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
