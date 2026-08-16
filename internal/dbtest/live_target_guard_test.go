package dbtest

import "testing"

// The guard is the only thing standing between an exported deployment DSN and a migration
// applied to the running Aura, so its rule is pinned rather than trusted to a comment.
func TestMigrateURLRefusesTheLiveDatabaseOffCI(t *testing.T) {
	for name, dsn := range map[string]string{
		"plain":        "postgres://aura_migrate:pw@127.0.0.1:5432/aura",
		"with options": "postgres://aura_migrate:pw@127.0.0.1:5432/aura?sslmode=disable",
		"another host": "postgres://aura_migrate:pw@db.internal:5432/aura?sslmode=require",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GITHUB_ACTIONS", "")
			if !refused(t, dsn) {
				t.Fatalf("%q was allowed through", dsn)
			}
		})
	}
}

// In CI the database called "aura" is one the job created and throws away, so refusing it
// there would fail every integration job for the danger it does not carry.
func TestMigrateURLAllowsTheSameNameInCI(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	if refused(t, "postgres://aura_migrate:pw@127.0.0.1:5432/aura") {
		t.Fatal("CI's own throwaway database was refused")
	}
}

// Everything that is not the live database passes through byte-for-byte: a disposable
// database, a developer's own copy, and a DSN this cannot parse (which db.Migrate will
// reject on its own terms rather than being guessed at here).
func TestMigrateURLPassesEverythingElseThrough(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	for name, dsn := range map[string]string{
		"disposable": "postgres://aura_migrate:pw@127.0.0.1:5432/aura_pipeline_9f2c",
		"own copy":   "postgres://aura_migrate:pw@127.0.0.1:5432/aura_dev",
		"coverage":   "postgres://aura_migrate:pw@127.0.0.1:5432/aura_cov",
		"unparsable": "://not a dsn",
		"empty":      "",
	} {
		t.Run(name, func(t *testing.T) {
			if refused(t, dsn) {
				t.Fatalf("%q was refused", dsn)
			}
			if got := MigrateURL(t, dsn); got != dsn {
				t.Fatalf("MigrateURL returned %q, want the DSN unchanged", got)
			}
		})
	}
}

// refused reports whether MigrateURL failed the test it was given. It needs its own
// testing.TB because a Fatalf on the real one would end the case rather than be observed.
func refused(t *testing.T, dsn string) bool {
	t.Helper()
	probe := &recordingTB{TB: t}
	func() {
		defer func() { _ = recover() }()
		MigrateURL(probe, dsn)
	}()
	return probe.failed
}

type recordingTB struct {
	testing.TB
	failed bool
}

func (r *recordingTB) Fatalf(string, ...any) {
	r.failed = true
	// Fatalf must not return to its caller, exactly as the real one does not.
	panic("dbtest: guard refused")
}

func (r *recordingTB) Helper() {}
