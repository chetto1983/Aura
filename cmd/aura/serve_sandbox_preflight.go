package main

import (
	"context"

	"github.com/chetto1983/aura/internal/config"
)

// STUB — RED phase (TDD #3770): deliberately wrong bodies so the tests in
// serve_sandbox_preflight_test.go compile and fail on assertion, not on a missing symbol.
// GREEN implements these for real.

func sandboxImagePreflight(ctx context.Context, chat *chatEnv) error {
	return nil
}

func logMultiUserAvailability(cfg *config.Config) {
}
