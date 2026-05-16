package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/secrets"
)

// writeSecret persists key→value in the secrets store. Empty values are
// deleted (clearing a previously set secret). Returns an error if store is nil.
// Secret values are never logged.
func writeSecret(ctx context.Context, store secrets.Store, key, value string) error {
	if store == nil {
		return fmt.Errorf("setup: SecretsStore not configured")
	}
	if strings.TrimSpace(value) == "" {
		return store.Delete(ctx, key)
	}
	return store.Set(ctx, key, value)
}
