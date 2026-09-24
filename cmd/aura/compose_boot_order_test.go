package main

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// REGRESSION GUARD, measured on the lab VM on 2026-09-24. Before it listens, aura serve lists
// the identities in Postgres and provisions each one's ArcadeDB tenant, and it exits when
// either store refuses the connection. ArcadeDB is a JVM that took 33 s to answer at boot,
// so an aura started without waiting for it exited, `docker compose up -d` gave up while
// waiting for aura to become healthy, and caddy and the observability stack were left
// Created on every boot — with aura.service failed and nothing to retry it.
func TestAuraWaitsForTheStoresItReachesBeforeListening(t *testing.T) {
	root := repoRootForTest(t)
	aura := composeServiceBlock(t, readProjectFile(t, root, "compose.yaml"), "aura")
	for _, want := range []string{
		"postgres:\n        condition: service_healthy",
		"arcadedb:\n        condition: service_healthy",
	} {
		if !strings.Contains(aura, want) {
			t.Errorf("aura must not start before %q:\n%s", want, aura)
		}
	}
}

// The updater writes INSTALL_DIR/update and Aura reads AURA_UPDATE_STATE_DIR: if the mount and
// the default drift apart, every appliance silently loses its update prompt.
func TestAuraMountsTheUpdaterChannelWhereItsConfigLooks(t *testing.T) {
	root := repoRootForTest(t)
	aura := composeServiceBlock(t, readProjectFile(t, root, "compose.yaml"), "aura")
	if want := "      - ./update:" + config.DefaultUpdateStateDir + "\n"; !strings.Contains(aura, want) {
		t.Fatalf("aura must mount %q:\n%s", strings.TrimSpace(want), aura)
	}
}
