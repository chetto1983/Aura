package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

// The route watcher (spec §7). MCP builds its embedder once, at boot, from the route in
// aura.settings; a route changed from the cockpit would leave it writing vectors in the old
// space until someone restarted it. So it re-reads the route every minute and, when the route
// or its credential moved, stops through its own graceful shutdown; `restart: unless-stopped`
// boots it on the new one. It cannot loop: it exits only when the settings name a route other
// than the one it runs.
//
// The route is compared, not an attested space: the local route's embedder already attests the
// sidecar on every call (embeddings.Route), so a GGUF swapped under unchanged settings needs no
// restart, and a boot attestation that failed must not restart a healthy process.

// routeWatchInterval is how often the route is re-read.
const routeWatchInterval = 60 * time.Second

// routeIdentity is what the running embedder was built from. The credential is kept only as a
// hash: it is compared, never logged.
type routeIdentity struct {
	embed          config.EmbedConfig
	credentialHash string
}

func identityOf(route embeddingRoute) routeIdentity {
	sum := sha256.Sum256([]byte(route.apiKey))
	return routeIdentity{embed: route.embed, credentialHash: hex.EncodeToString(sum[:])}
}

// watchEmbeddingRoute re-reads the route every interval until ctx ends, and calls stop once
// when it names another route or credential. A read that fails keeps the route this process
// booted on: the failure is Postgres's, not the route's.
func watchEmbeddingRoute(
	ctx context.Context, stop func(), boot routeIdentity,
	resolve func(context.Context) (embeddingRoute, error), interval time.Duration, logger *slog.Logger,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		readCtx, cancel := context.WithTimeout(ctx, interval)
		route, err := resolve(readCtx)
		cancel()
		if err != nil {
			logger.Warn("embedding route re-read failed; keeping the route this process booted on", "error", err)
			continue
		}
		if identityOf(route) != boot {
			logger.Info("embedding route changed; exiting to restart on it",
				"embed_url", route.baseURL, "model", route.embed.CloudModel)
			stop()
			return
		}
	}
}

// routeResolver reads the route from aura.settings with a store opened for that read alone:
// a minute apart, a pool held open between reads would buy nothing.
func routeResolver(
	dsn, authulaSecret string, open bootSettingsOpener, lookupEnv func(string) (string, bool),
) func(context.Context) (embeddingRoute, error) {
	return func(ctx context.Context) (embeddingRoute, error) {
		store, closeStore, err := open(ctx, dsn, authulaSecret)
		if err != nil {
			return embeddingRoute{}, err
		}
		defer closeStore()
		return resolveRoute(ctx, store, lookupEnv)
	}
}

// environmentBefore snapshots the environment before OverlayEnv copies rows into it. OverlayEnv
// never unsets, so a live lookup would bring a deleted row's value back as its fallback (spec
// §6, review of plan 1).
func environmentBefore() func(string) (string, bool) {
	environment := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		environment[key] = value
	}
	return func(key string) (string, bool) {
		value, ok := environment[key]
		return value, ok
	}
}
