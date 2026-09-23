package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

type fakeRouteStore struct {
	rows      []sqlc.AuraSettings
	listErr   error
	secret    string
	secretErr error
}

func (f fakeRouteStore) List(context.Context) ([]sqlc.AuraSettings, error) { return f.rows, f.listErr }
func (f fakeRouteStore) Secret(context.Context, string) (string, error)    { return f.secret, f.secretErr }

func env(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestEmbedRouteRowsWinOverTheEnvironment(t *testing.T) {
	store := fakeRouteStore{rows: []sqlc.AuraSettings{
		{Key: "AURA_EMBED_BASE_URL", Value: "http://settings-embed:8081"},
		{Key: "AURA_EMBED_MODEL", Value: "vendor/embed-v2"},
	}, secret: "stored-key"}
	embed, key, err := EmbedRoute(context.Background(), store, env(map[string]string{
		"AURA_EMBED_BASE_URL": "http://stale-env:8081", "AURA_EMBED_MODEL": "stale/model",
	}), "http://default:8081")
	if err != nil {
		t.Fatalf("EmbedRoute: %v", err)
	}
	if embed.BaseURL != "http://settings-embed:8081" || embed.CloudModel != "vendor/embed-v2" || key != "stored-key" {
		t.Fatalf("route = %+v key %q, want the rows and the sealed key", embed, key)
	}
}

// OverlayEnv only ever calls Setenv, so a row deleted after boot stayed in the process
// environment and a re-read could not see the deletion. This helper never touches the
// environment: an absent row falls back to it, an EMPTY row overrides it.
func TestEmbedRouteDistinguishesAnAbsentRowFromAnEmptyOne(t *testing.T) {
	processEnv := env(map[string]string{"AURA_EMBED_MODEL": "stale/model", "AURA_EMBED_BASE_URL": "http://compose:8081"})

	absent, _, err := EmbedRoute(context.Background(), fakeRouteStore{}, processEnv, "http://default:8081")
	if err != nil {
		t.Fatalf("EmbedRoute: %v", err)
	}
	if absent.CloudModel != "stale/model" || absent.BaseURL != "http://compose:8081" {
		t.Fatalf("absent rows: route = %+v, want the environment", absent)
	}

	empty, _, err := EmbedRoute(context.Background(), fakeRouteStore{rows: []sqlc.AuraSettings{
		{Key: "AURA_EMBED_MODEL", Value: ""}, {Key: "AURA_EMBED_BASE_URL", Value: ""},
	}}, processEnv, "http://default:8081")
	if err != nil {
		t.Fatalf("EmbedRoute: %v", err)
	}
	if empty.CloudModel != "" || empty.BaseURL != "" {
		t.Fatalf("empty rows: route = %+v, want empty (local route, dense retrieval off)", empty)
	}
}

func TestEmbedRouteDefaultsTheLocalBaseOnlyWhenNothingNamesIt(t *testing.T) {
	embed, _, err := EmbedRoute(context.Background(), fakeRouteStore{}, env(nil), "http://aura-llama-embed:8081")
	if err != nil {
		t.Fatalf("EmbedRoute: %v", err)
	}
	if embed.BaseURL != "http://aura-llama-embed:8081" || embed.CloudModel != "" || embed.CloudBaseURL != "" {
		t.Fatalf("route = %+v, want the product default local base", embed)
	}
}

func TestEmbedRouteFailsClosed(t *testing.T) {
	for name, store := range map[string]fakeRouteStore{
		"rows unreadable":   {listErr: errors.New("postgres unavailable")},
		"secret unreadable": {secretErr: errors.New("sealed row unreadable")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := EmbedRoute(context.Background(), store, env(nil), "http://default:8081"); err == nil {
				t.Fatal("EmbedRoute succeeded; a route must never be guessed from stale state")
			}
		})
	}
}
