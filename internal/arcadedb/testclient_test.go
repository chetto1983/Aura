//go:build arcadedb_integration

package arcadedb

import (
	"os"
	"strings"
	"testing"
)

// documentClient is the plain live client the LOCOMO benchmarks take. It kept
// its name through the migration that removed the document layer from ArcadeDB:
// those benchmarks measure RETRIEVAL over a stored corpus, which is still what
// they do, and renaming seven files to say `benchClient` would obscure that the
// numbers in their comments were produced by this exact constructor.
// liveGap reports a missing piece of the live stack. Under $CI it FAILS: a
// tagged tier that skips in the pipeline is a falsely-green job exercising
// nothing (CLAUDE.md no-skip-as-green). On a workstation with no stack up the
// same condition is a legitimate skip, which is why this is one helper and not
// two policies scattered across the tier's constructors.
//
// It covers env gaps and unreachable services only. A skip that reports missing
// DATA — the LOCOMO dataset, an empty fact corpus — stays a plain t.Skip: those
// tests are excluded from the CI job by name, not silently tolerated inside it.
func liveGap(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatalf("the arcadedb_integration tier cannot skip under CI: "+format, args...)
	}
	t.Skipf(format, args...)
}

func documentClient(t *testing.T) *Client {
	t.Helper()
	password := strings.TrimSpace(os.Getenv("ARCADEDB_PASSWORD"))
	if password == "" {
		liveGap(t, "ARCADEDB_PASSWORD not set")
	}
	base := strings.TrimSpace(os.Getenv("ARCADEDB_URL"))
	if base == "" {
		base = "http://127.0.0.1:2480"
	}
	database := strings.TrimSpace(os.Getenv("ARCADEDB_DATABASE"))
	if database == "" {
		database = "aura_memory"
	}
	client, err := New(Config{BaseURL: base, Database: database, User: "root", Password: password})
	if err != nil {
		liveGap(t, "arcadedb: %v", err)
	}
	return client
}
