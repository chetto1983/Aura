//go:build neo4j_integration || smoke

// Shared test helper for the live-container builds. Tagged `neo4j_integration ||
// smoke` so it is present whenever either consuming build runs (client_test.go /
// smoke_test.go) and absent from the plain unit build — keeping it out of the
// `unused` linter's view there.
package knowledge

import (
	"os"
	"testing"
)

// envOrSkipCI returns the env value, or skips locally / fails under CI when it is
// unset. A skipped integration/smoke test must never pass as green in the pipeline
// (mirrors internal/db's envOrSkip). GitHub Actions always sets CI=true.
func envOrSkipCI(t *testing.T, key, what string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s requires %s, but it is unset under CI — a skipped test must not "+
				"pass as green; wire it in ci.yml", what, key)
		}
		t.Skipf("%s unset — skipping %s", key, what)
	}
	return v
}
