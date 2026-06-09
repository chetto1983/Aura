//go:build neo4j_integration || smoke

package knowledge

import (
	"os"
	"strconv"
	"testing"
)

func envIntOr(t *testing.T, key string, fallback int) int {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("%s must be an int, got %q", key, v)
	}
	return n
}
