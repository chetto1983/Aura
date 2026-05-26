package db

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

type AuraRunningFunc func(context.Context) (bool, error)

func RefuseLiveDockerDBWrite(ctx context.Context, dbPath, operation string, auraRunning AuraRunningFunc) error {
	if !isComposeDataDB(dbPath) {
		return nil
	}
	running, err := auraRunning(ctx)
	if err != nil {
		return fmt.Errorf("check Docker Aura service before %s: %w", operation, err)
	}
	if !running {
		return nil
	}
	return fmt.Errorf("%s would write %s while the Docker Aura service is running; run inside Compose or stop it first with `docker compose stop aura`", operation, filepath.Join("data", "aura.db"))
}

func isComposeDataDB(dbPath string) bool {
	clean := filepath.Clean(strings.TrimSpace(dbPath))
	if clean == "" {
		return false
	}
	slash := filepath.ToSlash(clean)
	if slash == "/data/aura.db" {
		return false
	}
	return slash == "data/aura.db" || strings.HasSuffix(slash, "/data/aura.db")
}
