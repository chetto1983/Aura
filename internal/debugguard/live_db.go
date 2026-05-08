package debugguard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

func RefuseLiveDockerDBWriteWithCompose(ctx context.Context, dbPath, operation string) error {
	return RefuseLiveDockerDBWrite(ctx, dbPath, operation, IsComposeAuraRunning)
}

func IsComposeAuraRunning(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, "docker", "compose", "ps", "--status", "running", "--format", "{{.Service}}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(checkCtx.Err(), context.DeadlineExceeded) {
			return false, checkCtx.Err()
		}
		return false, fmt.Errorf("docker compose ps: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.TrimSpace(line) == "aura" {
			return true, nil
		}
	}
	return false, nil
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
