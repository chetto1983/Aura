package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Release-surface contracts: what an operator downloads and runs (installer, systemd unit,
// goreleaser, backup docs, .env template) — as opposed to what is baked INTO the image,
// which container_artifacts_test.go owns. Split out when the combined file crossed the
// 600-LOC cap; the two halves fail for different reasons and are read by different people.

func TestDistributionSurfaceArtifactsMatchReleaseContract(t *testing.T) {
	root := repoRootForTest(t)
	installer := readProjectFile(t, root, "scripts/install.sh")
	releaser := readProjectFile(t, root, ".goreleaser.yaml")
	unit := readProjectFile(t, root, "deploy/aura.service")

	for _, want := range []string{
		"set -euo pipefail",
		"AURA_INSTALL_SKIP_HW",
		"Aura requires at least 4 CPU cores",
		// 14, not 15 or 16: MemTotal excludes firmware/kernel reservations, so a 16 GB box
		// never reports 16 GiB, and the reference appliance (GEEKOM Mini Air12) reports
		// 15610852 KiB = 14.8877 GiB -- under 15 too. Both earlier floors refused the very
		// box the footprint was measured on. Reasoning lives in scripts/install.sh; this
		// line exists so lowering the floor again stays a deliberate two-file edit.
		"Aura requires at least 14 GiB usable RAM",
		"Aura requires at least 20 GiB free disk",
		"50 GiB free disk is recommended",
		"https://get.docker.com",
		"brew install --cask docker",
		"openssl rand -hex 32",
		"ensure_internal_env_secrets",
		"ensure_objectstore_env_secrets",
		"ensure_objectstore_public_endpoint",
		"AURA_OBJECTSTORE_PUBLIC_ENDPOINT \"https://$(host_for_summary)\"",
		"AURA_AUTHULA_SECRET=${authula_secret}",
		"SEARXNG_SECRET=${searxng_secret}",
		"AURA_OBJECTSTORE_ACCESS_KEY=${objectstore_access_key}",
		"AURA_OBJECTSTORE_SECRET_KEY=${objectstore_secret_key}",
		"GARAGE_RPC_SECRET=${garage_rpc_secret}",
		"chmod 600 .env",
		"download_file searxng/settings.yml searxng/settings.yml",
		"download_file searxng/limiter.toml searxng/limiter.toml",
		"download_file scripts/garage_bootstrap.sh scripts/garage_bootstrap.sh",
		"AURA_ACCESS_TOKEN",
		"docker compose up -d",
		"https://${host}/setup/?token=${token}",
	} {
		if !strings.Contains(installer, want) {
			t.Fatalf("scripts/install.sh missing %q:\n%s", want, installer)
		}
	}
	for _, want := range []string{
		"dockers_v2:",
		"dockerfile: docker/aura/Dockerfile",
		"ghcr.io/chetto1983/aura",
		"{{ .Tag }}",
		"linux/amd64",
		"linux/arm64",
		"extra_files:",
		"go.mod",
		"go.sum",
		"cmd",
		"internal",
	} {
		if !strings.Contains(releaser, want) {
			t.Fatalf(".goreleaser.yaml missing %q:\n%s", want, releaser)
		}
	}
	if strings.Contains(releaser, "latest") {
		t.Fatalf(".goreleaser.yaml should not emit a latest image tag:\n%s", releaser)
	}
	for _, want := range []string{
		"After=network-online.target docker.service",
		"WorkingDirectory=/opt/aura",
		"ExecStart=/usr/bin/docker compose up -d",
		"ExecStop=/usr/bin/docker compose down",
		"WantedBy=multi-user.target",
		"runsc install",
		"dpkg --print-architecture",
		"native Linux only; never Docker Desktop",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("deploy/aura.service missing %q:\n%s", want, unit)
		}
	}
}

func TestRetiredMiniPCComposeStaysOutOfDistribution(t *testing.T) {
	root := repoRootForTest(t)
	if _, err := os.Stat(filepath.Join(root, "compose.minipc.yaml")); !os.IsNotExist(err) {
		t.Fatalf("compose.minipc.yaml must stay retired, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "deployment", "mini-pc-cloud-appliance.md")); !os.IsNotExist(err) {
		t.Fatalf("retired Mini-PC guide must stay absent, stat err=%v", err)
	}

	for _, rel := range []string{
		".env.example",
		".github/workflows/ci.yml",
		".planning/codebase/STRUCTURE.md",
		"compose.yaml",
		"deploy/aura.service",
		"scripts/install.sh",
	} {
		contents := readProjectFile(t, root, rel)
		if strings.Contains(contents, "compose.minipc.yaml") {
			t.Errorf("%s still references the retired Mini-PC compose file", rel)
		}
	}

	workflow := readProjectFile(t, root, ".github/workflows/ci.yml")
	cacheOverlay := readProjectFile(t, root, ".github/compose.ci-cache.yaml")
	if !strings.Contains(workflow, "COMPOSE_FILE: compose.yaml:.github/compose.ci-cache.yaml") {
		t.Fatal("CPU CI jobs do not select the surviving CI cache overlay")
	}
	for _, want := range []string{
		"AURA_EMBED_IMAGE:-ghcr.io/ggml-org/llama.cpp:server}",
		"deploy: !reset null",
	} {
		if !strings.Contains(cacheOverlay, want) {
			t.Fatalf(".github/compose.ci-cache.yaml missing %q:\n%s", want, cacheOverlay)
		}
	}
}

func TestBackupLifecycleDocsMatchApplianceContract(t *testing.T) {
	root := repoRootForTest(t)
	restoreDrill := readProjectFile(t, root, "scripts/restore_drill.sh")
	readme := readProjectFile(t, root, "README.md")

	// Three planes: Postgres, the sidecar home volume and the object store. ArcadeDB
	// has no restore drill — memory lives in one database per identity and nothing
	// dumps them — so this contract deliberately claims no graph plane rather than
	// claiming one that does not run.
	for _, want := range []string{
		"pg_restore",
		"dr_compose_volume_name aura-home",
		"SIDECAR_SOURCE_VOLUME_CREATED",
		"scripts/objectstore_drill.go",
		`"checksum_ok": True`,
		`"cleanup_ok": True`,
	} {
		if !strings.Contains(restoreDrill, want) {
			t.Fatalf("scripts/restore_drill.sh missing %q:\n%s", want, restoreDrill)
		}
	}
	for _, want := range []string{
		"install.sh",
		"Docker Desktop",
		"PowerShell",
		"AURA_ACCESS_TOKEN",
		"docker compose pull",
		"docker compose up -d",
		"aura-migrate",
		"pg_restore",
		"WhatsApp Terms of Service",
		"Scan the QR code",
		"tls internal",
		"no Docker socket",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing %q", want)
		}
	}
	if strings.Index(readme, "## Quick Start") > strings.Index(readme, "## Development") {
		t.Fatalf("README.md should lead with the end-user quick start before development")
	}
}

func TestProductionReadinessProvidesDaemonBootContract(t *testing.T) {
	root := repoRootForTest(t)
	workflow := readProjectFile(t, root, ".github/workflows/production-readiness.yml")

	for _, want := range []string{
		"OPENROUTER_API_KEY: readiness-degraded-no-network",
		`AURA_WEB_TRUST_PROXY: "true"`,
		"Candidate to previous to candidate rollback rehearsal",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf(".github/workflows/production-readiness.yml missing %q", want)
		}
	}
}

func TestRetiredAuraImagesFailClosed(t *testing.T) {
	root := repoRootForTest(t)
	readiness := readProjectFile(t, root, ".github/workflows/production-readiness.yml")
	retirement := readProjectFile(t, root, ".github/workflows/retire-aura-images.yml")

	if strings.Contains(readiness, "ghcr.io/chetto1983/aura:v1.0.1") {
		t.Fatal("production readiness must not default to the retired v1.0.1 image")
	}
	// previous_image is no longer `required: true`: the first release has no
	// approved image to roll back to, so bootstrap may omit it. The guard that
	// `required: true` used to provide -- the input cannot be skipped, so the
	// retired-digest check always runs -- now lives in the Validate step, and is
	// pinned here in the stronger form: omitting the digest is refused unless
	// bootstrap, and bootstrap itself is refused once any release exists.
	for _, want := range []string{
		"previous_image must be an immutable image@sha256 digest",
		"bootstrap takes no previous_image",
		"bootstrap is first-release only",
		`repos/$GITHUB_REPOSITORY/releases`,
		`^.+@sha256:[0-9a-f]{64}$`,
		"retired-aura-image-digests.txt",
	} {
		if !strings.Contains(readiness, want) {
			t.Fatalf("production readiness missing retired-image guard %q", want)
		}
	}
	for _, want := range []string{
		"packages: write",
		"DELETE_ALL_AURA_IMAGES",
		`packages/container/aura/versions`,
		"Delete failed for version",
		"remaining remote Aura versions",
		`jq -e 'length == 0'`,
	} {
		if !strings.Contains(retirement, want) {
			t.Fatalf("retirement workflow missing fail-closed contract %q", want)
		}
	}
}

func TestDotEnvTemplateHygiene(t *testing.T) {
	root := repoRootForTest(t)
	envExample := readProjectFile(t, root, ".env.example")
	gitignore := readProjectFile(t, root, ".gitignore")

	for _, want := range []string{"/.env", "/.env.*", "!/.env.example"} {
		if !strings.Contains(gitignore, want) {
			t.Fatalf(".gitignore missing %q:\n%s", want, gitignore)
		}
	}
	// REWRITTEN with the management-key design: the template used to have to set dozens of
	// knobs whose value compose already supplies. It now holds what compose requires and
	// what a fresh install sets differently (env_example_test.go keeps the rest out).
	//
	// Every variable compose fail-fasts on is in the template: compose interpolates the whole
	// file before selecting a service, so one missing name aborts every compose invocation an
	// operator makes, including ones that touch none of its containers.
	compose := readProjectFile(t, root, "compose.yaml")
	for _, m := range composeRequiredEnv.FindAllStringSubmatch(compose, -1) {
		if !hasActiveEnvAssignment(envExample, m[1]) {
			t.Errorf(".env.example missing active assignment for %q, which compose requires", m[1])
		}
	}
	// The posture a fresh install ships differs from compose's fallbacks, which keep an
	// in-place upgrade unchanged, so the template states it.
	for _, want := range []string{
		"AURA_PROFILE=single_user_hardened",
		"AURA_MUSR_ISOLATION=true",
		"AURA_SANDBOX_IMAGE=ghcr.io/chetto1983/aura-sandbox:edge",
		"COMPOSE_PROFILES=observability",
	} {
		if !hasActiveEnvLine(envExample, want) {
			t.Errorf(".env.example missing the shipped posture line %q", want)
		}
	}
	// Cockpit settings an admin sets in the first-run setup and Aura keeps in aura.settings:
	// a template line would let .env carry a second copy that diverges in silence.
	for _, name := range []string{
		"OPENROUTER_API_KEY", "AURA_OPENROUTER_MANAGEMENT_KEY", "AURA_LLM_PROVIDER",
		"AURA_LLM_MODEL", "AURA_LLM_BASE_URL", "TELEGRAM_BOT_TOKEN",
	} {
		if hasActiveEnvAssignment(envExample, name) {
			t.Errorf(".env.example still sets %s, a cockpit setting", name)
		}
	}
	if strings.Contains(envExample, "AURA_LLM_REASONING_LEARNING") {
		t.Fatal(".env.example still exposes the forbidden legacy learned-serving switch")
	}

	seen := map[string]bool{}
	for line := range strings.SplitSeq(envExample, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf(".env.example active line is not KEY=value: %q", line)
		}
		if strings.Contains(line, " #") {
			t.Fatalf(".env.example active assignment has an inline comment; keep comments on their own line: %q", line)
		}
		if seen[key] {
			t.Fatalf(".env.example has duplicate active assignment for %q", key)
		}
		seen[key] = true
	}
}
