// Package dbtest holds the guards db_integration tests share. It is test-support: no
// production code imports it, and scripts/coverage_gate.sh excludes it from the floor for
// the same reason it excludes internal/agent/agenttest.
package dbtest

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

// liveDatabaseName is the deployment's database. In CI a database by this name is a
// throwaway the job just created; on a developer machine it is the running Aura.
const liveDatabaseName = "aura"

// MigrateURL returns raw after refusing to hand a test the LIVE database.
//
// db.Migrate applies every pending migration to whatever DSN it is given, and a test that
// takes its DSN from the environment will migrate the deployment the moment someone exports
// the deployment's DSN -- which .env hands out, and which every operator has in their shell
// for a reason that has nothing to do with tests. MEASURED 2026-08-13: a local
// db_integration run applied 0095 to the live database outside any deploy. The 2026-07-10
// coverage-gate incident was the same shape and cost the operator identity table.
//
// The rule is scripts/coverage_docker.sh's, so the two cannot disagree: a database named
// "aura" is refused unless GITHUB_ACTIONS is set, because in CI that name belongs to a
// container the job created and will throw away. Everything else passes through untouched --
// a disposable database, a per-test schema, a developer's own copy.
//
// It fails rather than skips. A skip here would read as "this tier did not apply", which is
// exactly the false green the no-skip-as-green rule exists to prevent; the test did not
// fail to find its environment, it found a dangerous one.
func MigrateURL(t testing.TB, raw string) string {
	t.Helper()
	if name := databaseName(raw); name == liveDatabaseName && os.Getenv("GITHUB_ACTIONS") == "" {
		t.Fatalf(
			"refusing to migrate the live database %q: point AURA_DB_MIGRATE_URL at a "+
				"throwaway database, or use a disposable-pool helper", name)
	}
	return raw
}

// databaseName is the path component of a Postgres DSN, empty when there is none to read.
// A DSN this cannot parse is not treated as dangerous: it will fail in db.Migrate on its own
// terms, and guessing here would refuse valid targets on a parser disagreement.
func databaseName(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.Trim(parsed.Path, "/")
}
