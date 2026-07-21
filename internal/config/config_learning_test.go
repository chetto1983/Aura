package config

import (
	"testing"
	"time"
)

func TestLearningConfigDefaults(t *testing.T) {
	t.Setenv("AURA_LEARNING_SEEN_MAX_ENTRIES", "")
	t.Setenv("AURA_LEARNING_SEEN_TTL_DAYS", "")
	t.Setenv("AURA_LEARNING_EXAMPLE_TTL_DAYS", "")
	t.Setenv("AURA_LEARNING_BUCKET_CAP", "")
	t.Setenv("AURA_LEARNING_STORE_CAP", "")

	got := loadLearningConfig()
	if got.SeenMaxEntries != 100_000 || got.SeenTTL != 30*24*time.Hour {
		t.Fatalf("seen defaults = (%d, %s)", got.SeenMaxEntries, got.SeenTTL)
	}
	if got.ExampleTTL != 90*24*time.Hour || got.BucketCap != 512 || got.StoreCap != 10_000 {
		t.Fatalf("store defaults = (%s, %d, %d)", got.ExampleTTL, got.BucketCap, got.StoreCap)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLearningConfigRejectsNonPositiveLimits(t *testing.T) {
	base := loadLearningConfig()
	tests := []struct {
		name string
		edit func(*LearningConfig)
	}{
		{name: "seen cap", edit: func(c *LearningConfig) { c.SeenMaxEntries = 0 }},
		{name: "seen ttl", edit: func(c *LearningConfig) { c.SeenTTL = 0 }},
		{name: "example ttl", edit: func(c *LearningConfig) { c.ExampleTTL = 0 }},
		{name: "bucket cap", edit: func(c *LearningConfig) { c.BucketCap = 0 }},
		{name: "store cap", edit: func(c *LearningConfig) { c.StoreCap = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.edit(&cfg)
			if cfg.Validate() == nil {
				t.Fatal("Validate accepted a non-positive learning limit")
			}
		})
	}
}
