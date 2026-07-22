package config

import (
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/envutil"
)

// LearningConfig is the typed capacity and expiry policy for learned examples.
type LearningConfig struct {
	SeenMaxEntries int
	SeenTTL        time.Duration
	ExampleTTL     time.Duration
	BucketCap      int
	StoreCap       int
}

func loadLearningConfig() LearningConfig {
	return LearningConfig{
		SeenMaxEntries: envutil.IntDefault("AURA_LEARNING_SEEN_MAX_ENTRIES", 100_000),
		SeenTTL:        days("AURA_LEARNING_SEEN_TTL_DAYS", 30),
		ExampleTTL:     days("AURA_LEARNING_EXAMPLE_TTL_DAYS", 90),
		BucketCap:      envutil.IntDefault("AURA_LEARNING_BUCKET_CAP", 512),
		StoreCap:       envutil.IntDefault("AURA_LEARNING_STORE_CAP", 10_000),
	}
}

// Validate rejects non-positive learning limits and invalid cap relationships.
func (c LearningConfig) Validate() error {
	switch {
	case c.SeenMaxEntries <= 0:
		return fmt.Errorf("seen max entries must be positive")
	case c.SeenTTL <= 0:
		return fmt.Errorf("seen TTL must be positive")
	case c.ExampleTTL <= 0:
		return fmt.Errorf("example TTL must be positive")
	case c.BucketCap <= 0:
		return fmt.Errorf("bucket cap must be positive")
	case c.StoreCap <= 0:
		return fmt.Errorf("store cap must be positive")
	case c.BucketCap > c.StoreCap:
		return fmt.Errorf("bucket cap cannot exceed store cap")
	default:
		return nil
	}
}
