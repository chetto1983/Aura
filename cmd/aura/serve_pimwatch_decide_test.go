package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/decide"
	"github.com/chetto1983/aura/internal/settings"
)

const decideTestSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestTheWatcherGetsADeciderOnlyWithAPool(t *testing.T) {
	if decider := pimWatchDecider(&chatEnv{cfg: &config.Config{}}); decider != nil {
		t.Fatalf("no pool: decider = %#v, want an untyped nil", decider)
	}
	pool := unreachablePool(t)
	t.Cleanup(pool.Close)
	buf := captureLog(t)
	if decider := pimWatchDecider(&chatEnv{pool: pool, cfg: &config.Config{AuthulaSecret: "not hex"}}); decider != nil {
		t.Fatalf("an unusable secret: decider = %#v, want an untyped nil", decider)
	}
	if got := buf.String(); !strings.Contains(got, "aura.settings cannot be opened") || !strings.Contains(got, "error=") {
		t.Fatalf("an unusable secret logged %q, want its error", got)
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

// A call the budget cut or the run cancelled is the tick's business, not a model failure, so only a
// model that answered wrongly is logged.
func TestOnlyAModelFailureIsLogged(t *testing.T) {
	release := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(stalled.Close)
	t.Cleanup(func() { close(release) })
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(broken.Close)
	d := pimDecider{client: decide.Client{HTTP: &http.Client{}}}
	question := decide.Question{Options: []decide.Option{{Name: "Breakdown", Description: "A machine has stopped."}}}

	cut, cancelCut := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelCut()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, ctx := range map[string]context.Context{"a budget cut": cut, "a cancelled run": cancelled} {
		buf := captureLog(t)
		if _, err := d.Choose(ctx, decide.Route{BaseURL: stalled.URL}, "Subject: Line stopped", question); err == nil || buf.Len() != 0 {
			t.Errorf("%s (%v) logged %q", name, err, buf)
		}
	}

	buf := captureLog(t)
	_, err := d.Choose(context.Background(), decide.Route{BaseURL: broken.URL}, "Subject: Line stopped", question)
	if got := buf.String(); err == nil || !strings.Contains(got, "did not answer") || !strings.Contains(got, "class=upstream") {
		t.Fatalf("a model failure (%v) logged %q", err, got)
	}
}
