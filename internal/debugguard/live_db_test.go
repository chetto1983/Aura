package debugguard

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefuseLiveDockerDBWriteBlocksComposeDataDBWhenAuraRuns(t *testing.T) {
	dbPath := filepath.Join("data", "aura.db")

	err := RefuseLiveDockerDBWrite(context.Background(), dbPath, "debug_memory_closure -apply", func(context.Context) (bool, error) {
		return true, nil
	})

	if err == nil {
		t.Fatal("RefuseLiveDockerDBWrite returned nil, want guardrail error")
	}
	for _, want := range []string{"debug_memory_closure -apply", "data", "aura.db", "docker compose stop aura"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestRefuseLiveDockerDBWriteAllowsComposeDataDBWhenAuraStopped(t *testing.T) {
	dbPath := filepath.Join("data", "aura.db")

	if err := RefuseLiveDockerDBWrite(context.Background(), dbPath, "seed_e2e_env", func(context.Context) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("RefuseLiveDockerDBWrite returned %v, want nil", err)
	}
}

func TestRefuseLiveDockerDBWriteAllowsNonComposeDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "aura.db")

	if err := RefuseLiveDockerDBWrite(context.Background(), dbPath, "debug", func(context.Context) (bool, error) {
		return true, nil
	}); err != nil {
		t.Fatalf("RefuseLiveDockerDBWrite returned %v, want nil", err)
	}
}

func TestRefuseLiveDockerDBWriteDoesNotBlockContainerPath(t *testing.T) {
	if err := RefuseLiveDockerDBWrite(context.Background(), "/data/aura.db", "debug", func(context.Context) (bool, error) {
		return true, nil
	}); err != nil {
		t.Fatalf("RefuseLiveDockerDBWrite returned %v, want nil for in-container path", err)
	}
}
