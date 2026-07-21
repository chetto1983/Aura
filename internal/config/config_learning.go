package config

import (
	"fmt"
	"time"
)

// LearningConfig is the typed capacity and expiry policy for learned examples.
type LearningConfig struct {
	SeenMaxEntries int
	SeenTTL        time.Duration
	ExampleTTL     time.Duration
	BucketCap      int
	StoreCap       int
}

func loadLearningConfig() LearningConfig { return LearningConfig{} }

// Validate rejects non-positive learning limits and invalid cap relationships.
func (c LearningConfig) Validate() error {
	return fmt.Errorf("learning limits are not implemented")
}
