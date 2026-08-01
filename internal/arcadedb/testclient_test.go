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
func documentClient(t *testing.T) *Client {
	t.Helper()
	password := strings.TrimSpace(os.Getenv("ARCADEDB_PASSWORD"))
	if password == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_PASSWORD must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_PASSWORD not set")
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
		t.Skipf("arcadedb: %v", err)
	}
	return client
}
