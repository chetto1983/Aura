package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestProductionContainerArtifactsMatchFatImageContract(t *testing.T) {
	root := repoRootForTest(t)
	if _, err := os.Stat(filepath.Join(root, "Dockerfile")); !os.IsNotExist(err) {
		t.Fatalf("repo-root Dockerfile should stay absent after the packaging box-model split, stat err=%v", err)
	}
	dockerfile := readProjectFile(t, root, "docker/aura/Dockerfile")
	compose := readProjectFile(t, root, "compose.yaml")
	gvisor := readProjectFile(t, root, "compose.gvisor.yaml")
	caddyfile := readProjectFile(t, root, "caddy/Caddyfile")
	dockerignore := readProjectFile(t, root, ".dockerignore")

	for _, want := range []string{
		"FROM golang:",
		"FROM debian:bookworm-slim",
		"postgresql-client-17",
		"ghcr.io/astral-sh/uv:0.11.21",
		"mcp-neo4j-cypher==0.6.0",
		"ENV AURA_IN_CONTAINER=1",
		"ENTRYPOINT [\"aura\"]",
		"CMD [\"serve\"]",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("docker/aura/Dockerfile missing %q:\n%s", want, dockerfile)
		}
	}
	for _, want := range []string{
		"aura:",
		"dockerfile: docker/aura/Dockerfile",
		"OPENROUTER_API_KEY: ${OPENROUTER_API_KEY:-}",
		"AURA_LLM_BASE_URL: ${AURA_LLM_BASE_URL:-https://openrouter.ai/api/v1}",
		"AURA_LLM_STREAM_IDLE_TIMEOUT_SEC: ${AURA_LLM_STREAM_IDLE_TIMEOUT_SEC:-60}",
		"AURA_SHOW_REASONING: ${AURA_SHOW_REASONING:-true}",
		"AURA_COMPLETION_GATE: ${AURA_COMPLETION_GATE:-true}",
		"AURA_LLM_REASONING_LEARNING: ${AURA_LLM_REASONING_LEARNING:-false}",
		"SEARXNG_URL: http://searxng:8080/search",
		"AURA_WEB_FETCH_MAX_BODY_BYTES: ${AURA_WEB_FETCH_MAX_BODY_BYTES:-5000000}",
		"AURA_MCP_CONFIG: ${AURA_MCP_CONFIG:-/var/lib/aura/mcp/servers.json}",
		"AURA_MCP_SERVERS_JSON: ${AURA_MCP_SERVERS_JSON:-}",
		"AURA_SCHEDULER_TZ: ${AURA_SCHEDULER_TZ:-Europe/Rome}",
		"AURA_SCHEDULER_NOTIFY_DEFAULT: ${AURA_SCHEDULER_NOTIFY_DEFAULT:-stdout}",
		"AURA_SKILL_BODY_CAP_BYTES: ${AURA_SKILL_BODY_CAP_BYTES:-32768}",
		"AURA_AGUI_CORS_PERMISSIVE: ${AURA_AGUI_CORS_PERMISSIVE:-false}",
		"TELEGRAM_BOT_TOKEN: ${TELEGRAM_BOT_TOKEN:-}",
		"AURA_TELEGRAM_STATUS_THROTTLE_MS: ${AURA_TELEGRAM_STATUS_THROTTLE_MS:-1500}",
		"AURA_TELEGRAM_CONTENT_THROTTLE_MS: ${AURA_TELEGRAM_CONTENT_THROTTLE_MS:-500}",
		"AURA_TELEGRAM_CHAT_RATE_LIMIT_MS: ${AURA_TELEGRAM_CHAT_RATE_LIMIT_MS:-1000}",
		"AURA_REASONING_FIFO_RUNES: ${AURA_REASONING_FIFO_RUNES:-4096}",
		"AURA_VISION_CLOUD: ${AURA_VISION_CLOUD:-false}",
		"MULTIMODAL_BASE_URL: http://aura-ocr-vl:8082/v1",
		"STT_BASE_URL: http://aura-stt:9000/v1",
		"TTS_BASE_URL: http://aura-tts:8880/v1",
		"DOCUMENTS_BASE_URL: http://markitdown:8080",
		"AURA_PROFILE_DIR: ${AURA_PROFILE_DIR:-/var/lib/aura/agents}",
		"AURA_AGUI_BIND: 0.0.0.0:9080",
		"AURA_SETUP_BIND: 0.0.0.0:9081",
		"AURA_SETUP_TOKEN: ${AURA_ACCESS_TOKEN:?AURA_ACCESS_TOKEN required in .env}",
		"AURA_WEB_AUTH_PROVIDER: ${AURA_WEB_AUTH_PROVIDER:-passphrase}",
		"AURA_AUTHULA_DATABASE_URL: ${AURA_AUTHULA_DATABASE_URL:-}",
		"AURA_AUTHULA_SECRET: ${AURA_AUTHULA_SECRET:-}",
		"AURA_AUTHULA_OPERATOR_IDENTITY: ${AURA_AUTHULA_OPERATOR_IDENTITY:-local}",
		"aura-migrate:",
		"service_completed_successfully",
		"entrypoint: [\"sh\", \"-lc\", \"aura db migrate && aura neo4j migrate\"]",
		"command: []",
		"AURA_CONFIG_DIR: /var/lib/aura",
		"aura-home:/var/lib/aura",
		"127.0.0.1:${AURA_SETUP_PORT:-9081}:9081",
		"whatsapp:",
		"ghcr.io/chetto1983/whatsapp-mcp:sidecar",
		"127.0.0.1:${AURA_WHATSAPP_MCP_PORT:-8092}:8080",
		"127.0.0.1:${AURA_WHATSAPP_BRIDGE_PORT:-8094}:8081",
		"aura-whatsapp-session:/app/whatsapp-bridge/store",
		"caddy:",
		"0.0.0.0:${AURA_HTTPS_PORT:-443}:443",
		"caddy-data:/data",
		"AURA_BACKUP_DIR: /backups",
		"${AURA_BACKUP_DIR:-./backups}:/backups",
		"mem_limit:",
		"cpus:",
		"healthcheck:",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml missing %q", want)
		}
	}
	// AURA_LLM_MODEL stays fully .env-configurable: assert the env-override pattern
	// with a non-empty built-in fallback, never a specific model tag. The operator
	// switches models via AURA_LLM_MODEL / .env without touching this contract.
	if !regexp.MustCompile(`AURA_LLM_MODEL: \$\{AURA_LLM_MODEL:-[^}]+\}`).MatchString(compose) {
		t.Fatalf("compose.yaml missing env-overridable AURA_LLM_MODEL default pattern (AURA_LLM_MODEL: ${AURA_LLM_MODEL:-...})")
	}
	if strings.Count(compose, "0.0.0.0:${AURA_HTTPS_PORT:-443}:443") != 1 {
		t.Fatalf("compose.yaml should publish only caddy on non-loopback 443")
	}
	for _, retired := range []string{
		"read_only: true",
		"cap_drop:",
		"aura-runs:",
		"aura-skills:",
		"aura-exported-skills:",
	} {
		if strings.Contains(compose, retired) {
			t.Fatalf("compose.yaml still contains retired hardening knob %q", retired)
		}
	}
	for _, want := range []string{
		// AC11: a `:443` catch-all + on-demand internal cert so LAN clients reach
		// the wizard by IP/hostname (a `localhost:443` site only served the
		// localhost SNI and failed the handshake for any other name).
		":443 {",
		"tls internal",
		"on_demand",
		"@authed",
		"X-Aura-Token",
		"query({'token': '{$AURA_ACCESS_TOKEN}'})",
		"@setup path /setup /setup/*",
		"reverse_proxy aura:9080",
		"reverse_proxy aura:9081",
		// Google OAuth redirect callback routed to the PIM sidecar, token-exempt.
		"handle /admin/auth/google/callback {",
		"reverse_proxy aura-pim-mcp:8080",
		"respond 401",
	} {
		if !strings.Contains(caddyfile, want) {
			t.Fatalf("caddy/Caddyfile missing %q:\n%s", want, caddyfile)
		}
	}
	if strings.Contains(caddyfile, "forward_auth") {
		t.Fatalf("caddy/Caddyfile should use a local token matcher, not forward_auth:\n%s", caddyfile)
	}
	for _, want := range []string{
		"docker compose -f compose.yaml -f compose.gvisor.yaml up -d",
		"runsc install",
		"runtime: runsc",
	} {
		if !strings.Contains(gvisor, want) {
			t.Fatalf("compose.gvisor.yaml missing %q:\n%s", want, gvisor)
		}
	}
	for _, retired := range []string{"cap_drop", "read_only"} {
		if strings.Contains(gvisor, retired) {
			t.Fatalf("compose.gvisor.yaml should not contain %q:\n%s", retired, gvisor)
		}
	}
	for _, want := range []string{".git", ".worktrees", "output", ".env"} {
		if !strings.Contains(dockerignore, want) {
			t.Fatalf(".dockerignore missing %q:\n%s", want, dockerignore)
		}
	}
}

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
		"chmod 600 .env",
		"existing .env is missing",
		"AURA_ACCESS_TOKEN",
		"docker compose -f compose.yaml up -d",
		"docker compose -f compose.yaml -f compose.gvisor.yaml up -d",
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
		"ExecStart=/usr/bin/docker compose -f /opt/aura/compose.yaml up -d",
		"ExecStop=/usr/bin/docker compose -f /opt/aura/compose.yaml down",
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
		"AURA_LLM_STREAM_IDLE_TIMEOUT_SEC=",
		"AURA_MODEL_CONTEXT_WINDOW=",
		"AURA_COMPLETION_GATE=",
		"AURA_LLM_REASONING_LEARNING=",
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
		"AURA_AGUI_CORS_PERMISSIVE=",
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
		"AURA_LLM_REASONING_LEARNING=false",
		"AURA_WHATSAPP_BRIDGE_PORT=8094",
	} {
		if !hasActiveEnvLine(envExample, want) {
			t.Fatalf(".env.example missing coherent default line %q", want)
		}
	}

	seen := map[string]bool{}
	for _, line := range strings.Split(envExample, "\n") {
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

func hasActiveEnvAssignment(contents, key string) bool {
	prefix := key + "="
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func hasActiveEnvLine(contents, want string) bool {
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if line == want {
			return true
		}
	}
	return false
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func readProjectFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}
