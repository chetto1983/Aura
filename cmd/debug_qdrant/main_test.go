package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvForDebugQdrant(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("QDRANT_URL=http://qdrant:6333\nQDRANT_COLLECTION='aura_memory_v1'\n"), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	t.Setenv("QDRANT_URL", "")
	t.Setenv("QDRANT_COLLECTION", "")
	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if os.Getenv("QDRANT_URL") != "http://qdrant:6333" {
		t.Fatalf("QDRANT_URL = %q", os.Getenv("QDRANT_URL"))
	}
	if os.Getenv("QDRANT_COLLECTION") != "aura_memory_v1" {
		t.Fatalf("QDRANT_COLLECTION = %q", os.Getenv("QDRANT_COLLECTION"))
	}
}
