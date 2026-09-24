package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

const watchTick = 2 * time.Millisecond

// sequenceResolver answers each read with the next route (or error), repeating the last.
type sequenceResolver struct {
	mu     sync.Mutex
	routes []embeddingRoute
	errs   []error
	reads  int
}

func (s *sequenceResolver) resolve(context.Context) (embeddingRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := min(s.reads, len(s.routes)-1)
	s.reads++
	return s.routes[index], s.errs[index]
}

func (s *sequenceResolver) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

func localRoute() embeddingRoute {
	return embeddingRoute{embed: config.EmbedConfig{BaseURL: "http://aura-llama-embed:8081"}, baseURL: "http://aura-llama-embed:8081"}
}

func cloudRoute(key string) embeddingRoute {
	return embeddingRoute{
		embed:   config.EmbedConfig{BaseURL: "http://aura-llama-embed:8081", CloudModel: "vendor/embed"},
		baseURL: "https://openrouter.ai/api", apiKey: key,
	}
}

// watch runs the watcher until it stops the process or reads reads times, and reports whether
// it asked the process to stop.
func watch(t *testing.T, resolver *sequenceResolver, boot embeddingRoute, reads int) (stopped int32, logged string) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stops atomic.Int32
	var buffer bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, w: &buffer}, nil))
	done := make(chan struct{})
	go func() {
		watchEmbeddingRoute(ctx, func() { stops.Add(1) }, identityOf(boot), resolver.resolve, watchTick, logger)
		close(done)
	}()
	deadline := time.After(5 * time.Second)
	for resolver.count() < reads && stops.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("the watcher stopped reading")
		case <-time.After(watchTick):
		}
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	return stops.Load(), buffer.String()
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestWatchKeepsRunningWhileTheRouteIsUnchanged(t *testing.T) {
	resolver := &sequenceResolver{routes: []embeddingRoute{localRoute()}, errs: []error{nil}}
	if stopped, _ := watch(t, resolver, localRoute(), 5); stopped != 0 {
		t.Fatalf("stopped %d times on an unchanged route", stopped)
	}
}

// Review Focus 4: a settings read that fails keeps the process on the route it booted with.
func TestWatchKeepsRunningThroughAFailedRead(t *testing.T) {
	resolver := &sequenceResolver{routes: []embeddingRoute{{}}, errs: []error{errors.New("postgres down")}}
	stopped, logged := watch(t, resolver, localRoute(), 3)
	if stopped != 0 || !strings.Contains(logged, "postgres down") {
		t.Fatalf("stopped %d, log:\n%s", stopped, logged)
	}
}

func TestWatchStopsOnceWhenTheModelChanges(t *testing.T) {
	resolver := &sequenceResolver{
		routes: []embeddingRoute{localRoute(), localRoute(), cloudRoute("key-a")}, errs: []error{nil, nil, nil},
	}
	stopped, logged := watch(t, resolver, localRoute(), 10)
	if stopped != 1 || resolver.count() != 3 || !strings.Contains(logged, "embedding route changed") {
		t.Fatalf("stopped %d after %d reads, log:\n%s", stopped, resolver.count(), logged)
	}
}

func TestWatchStopsWhenTheCredentialChangesAndNeverLogsIt(t *testing.T) {
	resolver := &sequenceResolver{routes: []embeddingRoute{cloudRoute("sk-new-secret")}, errs: []error{nil}}
	stopped, logged := watch(t, resolver, cloudRoute("sk-old-secret"), 10)
	if stopped != 1 {
		t.Fatalf("stopped %d times on a rotated key", stopped)
	}
	if strings.Contains(logged, "sk-new-secret") || strings.Contains(logged, "sk-old-secret") {
		t.Fatalf("a credential reached the log:\n%s", logged)
	}
}

// Spec §6: a deleted row falls back to the environment as it was before the overlay, never to
// the value the overlay copied in; and every read opens and closes its own store.
func TestRouteResolverReadsRowsAgainstThePreOverlayEnvironment(t *testing.T) {
	t.Setenv("AURA_EMBED_MODEL", "from-env-before-overlay")
	before := environmentBefore()
	t.Setenv("AURA_EMBED_MODEL", "copied-in-by-the-overlay")
	opens, closes := 0, 0
	store := &fakeBootSettings{secret: "stored-key"}
	resolve := routeResolver("postgres://db/aura", "authula-secret",
		func(_ context.Context, dsn, secret string) (bootSettingsStore, func(), error) {
			opens++
			return store, func() { closes++ }, nil
		}, before)
	route, err := resolve(t.Context())
	if err != nil || route.embed.CloudModel != "from-env-before-overlay" || route.apiKey != "stored-key" {
		t.Fatalf("route %+v err %v, want the pre-overlay model and the sealed key", route, err)
	}
	store.rows = []sqlc.AuraSettings{{Key: "AURA_EMBED_MODEL", Value: ""}}
	if route, _ = resolve(t.Context()); route.embed.CloudModel != "" || opens != 2 || closes != 2 {
		t.Fatalf("route %+v opens %d closes %d: want the empty row to win and a store per read", route, opens, closes)
	}
	failing := routeResolver("dsn", "secret", func(context.Context, string, string) (bootSettingsStore, func(), error) {
		return nil, nil, errors.New("refused")
	}, before)
	if _, err := failing(t.Context()); err == nil {
		t.Fatal("a store that would not open resolved a route")
	}
}
