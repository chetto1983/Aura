package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// serve builds the de-provisioning saga before the Authula provider exists and attaches the
// two Authula legs afterwards (serve.go, SetAuthulaTeardown). That only reaches the cockpit's
// removal route AND the cron grace-window sweep if both are handed the same instance, so the
// builder must memoize rather than assemble a second copy of the same wiring.
func TestBuildDeprovisionerIsOneSharedInstance(t *testing.T) {
	chat := &chatEnv{}

	first, second := buildDeprovisioner(chat), buildDeprovisioner(chat)

	if first == nil {
		t.Fatal("buildDeprovisioner returned nil; the route's wiring has no typed-nil guard")
	}
	if first != second {
		t.Fatalf("buildDeprovisioner returned %p then %p; want one shared instance", first, second)
	}
}

// Jobs was nil on every path, which is how a deactivated identity's tasks went on firing. The
// pool stays lazy: no test here reaches Postgres.
func TestDeprovisionDepsReachTheScheduledTasks(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://aura@127.0.0.1:1/unused")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	deps := deprovisionDeps(&chatEnv{pool: pool, cfg: &config.Config{}})

	if _, ok := deps.Jobs.(scheduledTaskAdapter); !ok {
		t.Fatalf("Jobs = %T", deps.Jobs)
	}
	if _, ok := deps.ScheduledTasks.(scheduledTaskAdapter); !ok {
		t.Fatalf("ScheduledTasks = %T", deps.ScheduledTasks)
	}
}

// A nil environment still answers with a usable saga: PurgeExpired is a safe no-op on it.
func TestBuildDeprovisionerToleratesNoEnvironment(t *testing.T) {
	if buildDeprovisioner(nil) == nil {
		t.Fatal("buildDeprovisioner(nil) = nil, want an empty saga")
	}
}
