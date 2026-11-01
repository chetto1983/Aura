package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

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

// REGRESSION GUARD, the fourth outage of one kind. `docker compose up` gives up on a dependency
// once and aura.service does not retry, so a service gated on aura's health turned any slow boot
// into a dark cockpit, even with aura recovering behind it: a fresh install on 2026-09-10, the
// boots of 2026-09-24 and 2026-10-28, and on 2026-11-01 a hosted embedding route that went silent.
// Readiness reports; it must not hold the front door or the telemetry shut.
func TestNothingWaitsForAuraToBeHealthy(t *testing.T) {
	compose := strings.ReplaceAll(readProjectFile(t, repoRootForTest(t), "compose.yaml"), "\r\n", "\n")
	if strings.Contains(compose, "      aura:\n        condition: service_healthy") {
		t.Error("a service gates on aura's health; use service_started so a slow boot cannot strand it")
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

// docker stop sends SIGKILL once the service's stop_grace_period has passed, 10 s when
// compose sets none, while aura serve's drain may take AURA_SERVE_SHUTDOWN_GRACE_SEC. A
// shorter grace period cuts a turn mid-write, which is what the drain exists to prevent.
func TestAuraStopGracePeriodOutlastsTheDrain(t *testing.T) {
	aura := composeServiceBlock(t, readProjectFile(t, repoRootForTest(t), "compose.yaml"), "aura")
	drain := regexp.MustCompile(`AURA_SERVE_SHUTDOWN_GRACE_SEC: \$\{AURA_SERVE_SHUTDOWN_GRACE_SEC:-(\d+)\}`).FindStringSubmatch(aura)
	if drain == nil {
		t.Fatalf("aura sets no default for AURA_SERVE_SHUTDOWN_GRACE_SEC:\n%s", aura)
	}
	drainSec, err := strconv.Atoi(drain[1])
	if err != nil {
		t.Fatalf("drain default %q: %v", drain[1], err)
	}
	period := regexp.MustCompile(`(?m)^    stop_grace_period: (\S+)$`).FindStringSubmatch(aura)
	if period == nil {
		t.Fatalf("aura sets no stop_grace_period, so docker stop kills it 10 s into a drain of up to %d s", drainSec)
	}
	grace, err := time.ParseDuration(period[1])
	if err != nil {
		t.Fatalf("stop_grace_period %q: %v", period[1], err)
	}
	if grace <= time.Duration(drainSec)*time.Second {
		t.Fatalf("stop_grace_period %s does not outlast the %d s drain", grace, drainSec)
	}
}
