package main

import (
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
		"Aura requires at least 16 GiB RAM",
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
		"AURA_PIM_MCP_ADMIN_TOKEN=${pim_admin_token}",
		"SEARXNG_SECRET=${searxng_secret}",
		"AURA_OBJECTSTORE_ACCESS_KEY=${objectstore_access_key}",
		"AURA_OBJECTSTORE_SECRET_KEY=${objectstore_secret_key}",
		"GARAGE_RPC_SECRET=${garage_rpc_secret}",
		"chmod 600 .env",
		"download_file searxng/settings.yml searxng/settings.yml",
		"download_file searxng/limiter.toml searxng/limiter.toml",
		"download_file scripts/garage_bootstrap.sh scripts/garage_bootstrap.sh",
		"AURA_ACCESS_TOKEN",
		// No -f: the file set comes from COMPOSE_FILE in .env, and an explicit -f here
		// would WIN over it — silently ignoring compose.minipc.yaml on every appliance.
		"docker compose up -d",
		"set_env_value COMPOSE_FILE compose.yaml:compose.minipc.yaml",
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
		// No -f, deliberately: an explicit -f WINS over COMPOSE_FILE, so hardcoding one
		// here would make every appliance silently ignore compose.minipc.yaml. The unit
		// stays identical on both machines and .env decides the file set.
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

func TestBackupLifecycleDocsMatchApplianceContract(t *testing.T) {
	root := repoRootForTest(t)
	restoreDrill := readProjectFile(t, root, "scripts/restore_drill.sh")
	readme := readProjectFile(t, root, "README.md")

	for _, want := range []string{
		"pg_restore",
		"cypher-shell",
		"bolt://neo4j:7687",
		"NEO4J_DUMPFILE",
		"neo4j-*.cypher",
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
		"cypher-shell",
		"mcp-neo4j-cypher",
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

func TestDotEnvTemplateHygiene(t *testing.T) {
	root := repoRootForTest(t)
	envExample := readProjectFile(t, root, ".env.example")
	gitignore := readProjectFile(t, root, ".gitignore")

	for _, want := range []string{"/.env", "/.env.*", "!/.env.example"} {
		if !strings.Contains(gitignore, want) {
			t.Fatalf(".gitignore missing %q:\n%s", want, gitignore)
		}
	}
	for _, want := range []string{
		"AURA_IMAGE=",
		"AURA_ACCESS_TOKEN=",
		"AURA_AGENT_MEMORY_MCP_PORT=",
		"OPENROUTER_API_KEY=",
		"AURA_EMBED_HF_REPO=",
		"AURA_EMBED_HF_FILE=",
		"AURA_LLM_STREAM_IDLE_TIMEOUT_SEC=",
		"AURA_MODEL_CONTEXT_WINDOW=",
		"AURA_COMPLETION_GATE=",
		"AURA_AGENT_JOB_MAX_DURATION_SEC=",
		"AURA_SWARM_MAX_GOALS=",
		"AURA_SWARM_CHILD_TIMEOUT_SEC=",
		"AURA_SWARM_MAX_CONCURRENT=",
		"AURA_SWARM_MAX_DEPTH=",
		"AURA_LOOP_MAX_PARALLEL_TOOLS=",
		"AURA_FS_MAX_READ_BYTES=",
		"AURA_FS_WALK_NODE_CAP=",
		"AURA_FS_WALK_TIMEOUT_MS=",
		"AURA_SHELL_MAX_TIMEOUT_MS=",
		"AURA_SHELL_OUTPUT_BUF_CAP=",
		"SEARXNG_URL=",
		"TELEGRAM_BOT_TOKEN=",
		"AURA_TELEGRAM_STATUS_THROTTLE_MS=",
		"AURA_VISION_CLOUD=",
		"MULTIMODAL_BASE_URL=",
		"STT_BASE_URL=",
		"TTS_BASE_URL=",
		"DOCUMENTS_BASE_URL=",
		"AURA_MCP_CONFIG=",
		"AURA_SKILLS_DIR=",
		"AURA_OBJECTSTORE_BACKEND=",
		"AURA_OBJECTSTORE_ENDPOINT=",
		"AURA_OBJECTSTORE_PUBLIC_ENDPOINT=",
		"AURA_OBJECTSTORE_REGION=",
		"AURA_OBJECTSTORE_BUCKET=",
		"AURA_OBJECTSTORE_ACCESS_KEY=",
		"AURA_OBJECTSTORE_SECRET_KEY=",
		"AURA_OBJECTSTORE_PATH_STYLE=",
		"AURA_ASSET_MAX_DOCUMENT_BYTES=",
		"AURA_ASSET_MAX_IMAGE_BYTES=",
		"AURA_ASSET_MAX_AUDIO_BYTES=",
		"AURA_ASSET_PRESIGN_TTL_SEC=",
		"AURA_ASSET_PROCESSING_CONCURRENCY=",
		"TELEGRAM_API_BASE_URL=",
		"TELEGRAM_FILE_BASE_URL=",
		"AURA_TELEGRAM_LOCAL_BOT_API=",
		"AURA_PROFILE_DIR=",
		"AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL=",
	} {
		if !hasActiveEnvAssignment(envExample, strings.TrimSuffix(want, "=")) {
			t.Fatalf(".env.example missing active assignment for %q", want)
		}
	}
	for _, want := range []string{
		"AURA_LLM_MODEL=deepseek/deepseek-v4-flash:nitro",
		"AURA_SHOW_REASONING=true",
		"AURA_OBJECTSTORE_PUBLIC_ENDPOINT=http://127.0.0.1:3900",
		"AURA_WHATSAPP_BRIDGE_PORT=8094",
	} {
		if !hasActiveEnvLine(envExample, want) {
			t.Fatalf(".env.example missing coherent default line %q", want)
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
