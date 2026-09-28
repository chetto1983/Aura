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

// REGRESSION GUARD, measured on CI 2026-09-27/28. The memory MCP verifies every bearer against
// aura's JWKS, so it can answer nothing until aura SERVES /oauth/jwks; a started container is
// not enough. In all three red Agent Memory MRS runs (36359705239, 36373835410, 36405162541)
// the latency tier's first `initialize` came 155-161 s after aura started, while aura was still
// booting, and got the 500 cmd/arcadedb-mcp/auth.go keySet() documents; the twelve green runs
// got there after 183 s or more. The sidecar's /health is liveness-only by design, so the gate
// probes the JWKS itself, from inside the sidecar, at the URL its verifier fetches.
func TestMemoryGateWaitsForTheTokenAuthorityBeforeCallingTheMCP(t *testing.T) {
	root := repoRootForTest(t)
	// A workflow job is a two-space YAML key, exactly like a compose service.
	job := composeServiceBlock(t, readProjectFile(t, root, ".github/workflows/ci.yml"), "arcadedb-integration-test")
	probe := strings.Index(job, `docker compose exec -T arcadedb-mcp sh -c 'wget -q -T 5 -O /dev/null "$MCP_OAUTH_JWKS_URL"'`)
	evaluator := strings.Index(job, "make agent-memory-eval")
	if probe < 0 || evaluator < 0 || probe > evaluator {
		t.Errorf("the MRS job must probe the sidecar's MCP_OAUTH_JWKS_URL before make agent-memory-eval (probe at %d, evaluator at %d):\n%s",
			probe, evaluator, job)
	}
	if dump := "if: failure()\n        run: docker compose logs --no-color --timestamps arcadedb-mcp aura"; !strings.Contains(job, dump) {
		t.Errorf("the MRS job must dump the token path's logs when it fails (%q):\n%s", dump, job)
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
