package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGuardLiveDBWriteForSeedBlocksLiveComposeDB(t *testing.T) {
	err := guardLiveDBWriteForSeed(context.Background(), filepath.Join("data", "aura.db"), func(context.Context) (bool, error) {
		return true, nil
	})

	if err == nil {
		t.Fatal("guardLiveDBWriteForSeed returned nil, want guardrail error")
	}
}

func TestGuardLiveDBWriteForSeedAllowsTempDB(t *testing.T) {
	err := guardLiveDBWriteForSeed(context.Background(), filepath.Join(t.TempDir(), "aura.db"), func(context.Context) (bool, error) {
		return true, nil
	})

	if err != nil {
		t.Fatalf("guardLiveDBWriteForSeed returned %v, want nil", err)
	}
}
