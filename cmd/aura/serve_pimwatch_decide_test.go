package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/settings"
)

const decideTestSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestTheWatcherGetsADeciderOnlyWithAPool(t *testing.T) {
	if decider := pimWatchDecider(&chatEnv{cfg: &config.Config{}}); decider != nil {
		t.Fatalf("no pool: decider = %#v, want an untyped nil", decider)
	}
	pool := unreachablePool(t)
	t.Cleanup(pool.Close)
	if decider := pimWatchDecider(&chatEnv{pool: pool, cfg: &config.Config{AuthulaSecret: "not hex"}}); decider != nil {
		t.Fatalf("an unusable secret: decider = %#v, want an untyped nil", decider)
	}
	if decider := pimWatchDecider(&chatEnv{pool: pool, cfg: &config.Config{AuthulaSecret: decideTestSecret}}); decider == nil {
		t.Fatal("a pool gave no decider")
	}
}

// noSettings is an aura.settings with no row.
type noSettings struct{}

func (noSettings) List(context.Context) ([]sqlc.AuraSettings, error) { return nil, nil }

func (noSettings) Secret(context.Context, string) (string, error) { return "", nil }

// The overlay copies AURA_DECIDE_MODEL's row into the environment. Once the person deletes the row,
// the tick must fall back to what the deployment itself named, so the boot read comes first.
func TestTheDecideBootIsReadBeforeTheOverlay(t *testing.T) {
	saved := bootDecide
	t.Cleanup(func() { bootDecide = saved })
	bootDecide = newDecideBoot()
	t.Setenv("AURA_DECIDE_MODEL", "vendor/from-the-environment")
	t.Setenv("OPENROUTER_API_KEY", "sk-from-the-environment")
	pool := unreachablePool(t)
	ops := bootSettingsOps{overlay: func(context.Context, *pgxpool.Pool) error {
		t.Setenv("AURA_DECIDE_MODEL", "vendor/from-the-row")
		return nil
	}}
	cfg := validBootConfig()
	load := func() (*config.Config, error) { return cfg, nil }
	open := func(context.Context, *db.Config) (*pgxpool.Pool, error) { return pool, nil }
	if _, _, err := resolveConfigAndPoolWithSettings(context.Background(), load, open, ops); err != nil {
		t.Fatal(err)
	}
	pool.Close()

	route, err := settings.DecideRoute(context.Background(), noSettings{}, bootDecideLookup)
	if err != nil {
		t.Fatal(err)
	}
	if route.Model != "vendor/from-the-environment" {
		t.Fatalf("with the row deleted the route is %q, want the environment's own model", route.Model)
	}
	if key, err := route.Key(context.Background()); err != nil || key != "sk-from-the-environment" {
		t.Fatalf("key = %q, %v: the other names read the environment", key, err)
	}
}
