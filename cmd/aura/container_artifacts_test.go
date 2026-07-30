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
	minipc := readProjectFile(t, root, "compose.minipc.yaml")
	installer := readProjectFile(t, root, "scripts/install.sh")
	caddyfile := readProjectFile(t, root, "caddy/Caddyfile")
	dockerignore := readProjectFile(t, root, ".dockerignore")
	garageBootstrap := readProjectFile(t, root, "scripts/garage_bootstrap.sh")

	for _, want := range []string{
		"FROM golang:",
		"FROM debian:bookworm-slim",
		"FROM dxflrs/garage:v2.3.0 AS garagebin",
		"postgresql-client-18",
		"ghcr.io/astral-sh/uv:0.11.32",
		"mcp-neo4j-cypher==0.6.0",
		// uv/uvx stay in the image — the agent reaches them from shell_exec — but nothing
		// warms a package for a recipe here any more (asserted below).
		"COPY --from=ghcr.io/astral-sh/uv:",
		"ENV AURA_IN_CONTAINER=1",
		"COPY --from=garagebin /garage /usr/local/bin/garage",
		"aura-garage-bootstrap.sh",
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
		"AURA_CONTEXT_PREVIEW_CAP_BYTES: ${AURA_CONTEXT_PREVIEW_CAP_BYTES:-30000}",
		"AURA_SHOW_REASONING: ${AURA_SHOW_REASONING:-true}",
		"AURA_COMPLETION_GATE: ${AURA_COMPLETION_GATE:-true}",
		"SEARXNG_URL: http://searxng:8080/search",
		"AURA_WEB_FETCH_MAX_BODY_BYTES: ${AURA_WEB_FETCH_MAX_BODY_BYTES:-5000000}",
		"AURA_WEB_FETCH_TIMEOUT_SEC: ${AURA_WEB_FETCH_TIMEOUT_SEC:-10}",
		"AURA_MCP_CONFIG: ${AURA_MCP_CONFIG:-/var/lib/aura/mcp/servers.json}",
		"AURA_MCP_SERVERS_JSON: ${AURA_MCP_SERVERS_JSON:-}",
		"AURA_SCHEDULER_TZ: ${AURA_SCHEDULER_TZ:-Europe/Rome}",
		"AURA_SCHEDULER_NOTIFY_DEFAULT: ${AURA_SCHEDULER_NOTIFY_DEFAULT:-stdout}",
		"AURA_SKILL_BODY_CAP_BYTES: ${AURA_SKILL_BODY_CAP_BYTES:-32768}",
		"AURA_OBJECTSTORE_BACKEND: ${AURA_OBJECTSTORE_BACKEND:-garage}",
		"AURA_OBJECTSTORE_ENDPOINT: ${AURA_OBJECTSTORE_ENDPOINT:-http://garage:3900}",
		"AURA_OBJECTSTORE_PUBLIC_ENDPOINT: ${AURA_OBJECTSTORE_PUBLIC_ENDPOINT:-http://127.0.0.1:3900}",
		"AURA_OBJECTSTORE_REGION: ${AURA_OBJECTSTORE_REGION:-garage}",
		"AURA_OBJECTSTORE_BUCKET: ${AURA_OBJECTSTORE_BUCKET:-aura-assets}",
		"AURA_OBJECTSTORE_ACCESS_KEY: ${AURA_OBJECTSTORE_ACCESS_KEY:?AURA_OBJECTSTORE_ACCESS_KEY required in .env}",
		"AURA_OBJECTSTORE_SECRET_KEY: ${AURA_OBJECTSTORE_SECRET_KEY:?AURA_OBJECTSTORE_SECRET_KEY required in .env}",
		"AURA_OBJECTSTORE_PATH_STYLE: ${AURA_OBJECTSTORE_PATH_STYLE:-true}",
		"garage-bootstrap:",
		"service_completed_successfully",
		"entrypoint: [\"sh\", \"-lc\", \"aura-garage-bootstrap.sh && aura objectstore bootstrap\"]",
		"AURA_GARAGE_KEY_NAME: ${AURA_GARAGE_KEY_NAME:-aura-assets-local}",
		"AURA_GARAGE_ZONE: ${AURA_GARAGE_ZONE:-dc1}",
		"AURA_GARAGE_CAPACITY: ${AURA_GARAGE_CAPACITY:-1G}",
		"GARAGE_RPC_SECRET: ${GARAGE_RPC_SECRET:?GARAGE_RPC_SECRET required in .env}",
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
		"AURA_WEB_AUTH_PROVIDER: ${AURA_WEB_AUTH_PROVIDER:-authula}",
		"AURA_AUTHULA_DATABASE_URL: ${AURA_AUTHULA_DATABASE_URL:-}",
		"AURA_AUTHULA_SECRET: ${AURA_AUTHULA_SECRET:?AURA_AUTHULA_SECRET required in .env}",
		"AURA_AUTHULA_RATE_LIMIT_MAX: ${AURA_AUTHULA_RATE_LIMIT_MAX:-30}",
		"AURA_AUTHULA_OPERATOR_IDENTITY: ${AURA_AUTHULA_OPERATOR_IDENTITY:-}",
		"AURA_EMBED_IMAGE:-ghcr.io/ggml-org/llama.cpp:server-cuda",
		"AURA_EMBED_NGL:-99",
		"LLAMA_ARG_HOST: 0.0.0.0",
		"aura-migrate:",
		"service_completed_successfully",
		"entrypoint: [\"sh\", \"-lc\", \"aura db migrate && aura neo4j migrate\"]",
		"command: []",
		"POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in .env}",
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
		"curl -fsS --max-time 3 http://127.0.0.1:9080/readyz >/dev/null",
		"garage:",
		"image: ${AURA_GARAGE_IMAGE:-dxflrs/garage:v2.3.0}",
		"GARAGE_RPC_SECRET: ${GARAGE_RPC_SECRET:?GARAGE_RPC_SECRET required in .env}",
		"AURA_PIM_MCP_ADMIN_TOKEN: ${AURA_PIM_MCP_ADMIN_TOKEN:?AURA_PIM_MCP_ADMIN_TOKEN required in .env}",
		"SEARXNG_SECRET: ${SEARXNG_SECRET:?SEARXNG_SECRET required in .env}",
		"/etc/searxng/settings.template.yml",
		"/etc/searxng/limiter.toml",
		"__AURA_SEARXNG_SECRET__",
		"NEO4J_server_memory_heap_initial__size: 512m",
		"NEO4J_server_jvm_additional: \"--add-modules=jdk.incubator.vector\"",
		"127.0.0.1:${AURA_GARAGE_PORT:-3900}:3900",
		"./docker/garage/garage.toml:/etc/garage.toml:ro",
		"garage-data:",
		"driver: nvidia",
		"capabilities: [gpu]",
		"start_period: 90s",
		"start_period: 300s",
		"--profile extended",
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
	// The embedding sidecar's repo/file follow the same rule, for the same reason: which
	// model embeds is a deployment decision (it changed the day the graph stopped
	// discriminating), and pinning the tag here only guarantees this contract goes red
	// every time someone makes that decision. What the image contract owes is that a
	// default EXISTS and the operator can override it from .env.
	for _, knob := range []string{"AURA_EMBED_HF_REPO", "AURA_EMBED_HF_FILE"} {
		if !regexp.MustCompile(`\$\{` + knob + `:-[^}]+\}`).MatchString(compose) {
			t.Fatalf("compose.yaml missing env-overridable %s default pattern (${%s:-...})", knob, knob)
		}
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
		"AURA_LLM_REASONING_LEARNING",
	} {
		if strings.Contains(compose, retired) {
			t.Fatalf("compose.yaml still contains retired hardening knob %q", retired)
		}
	}
	for _, want := range []string{
		// AC11: a `:443` catch-all + on-demand internal cert so LAN clients reach the
		// cockpit by IP/hostname (a `localhost:443` site only served the localhost SNI
		// and failed the handshake for any other name).
		":443 {",
		"tls internal",
		"on_demand",
		// Auth moved INTO the aura binary (RequireAuth/Authula, d5eae07f): Caddy fronts the
		// full multi-user cockpit on 9080 with NO proxy-level token gate — the catch-all
		// reverse-proxies everything else straight to 9080 so an end user reaches /login.
		"reverse_proxy aura:9080",
		// Google OAuth redirect callback routed to the PIM sidecar, ahead of the catch-all.
		"handle /admin/auth/google/callback {",
		"reverse_proxy aura-pim-mcp:8080",
		// Presigned object-store asset downloads → garage.
		"handle /aura-assets/*",
		"reverse_proxy garage:3900",
		// First-run operator setup wizard (the 9081 server self-gates with AURA_SETUP_TOKEN).
		"handle /setup",
		"reverse_proxy aura:9081",
	} {
		if !strings.Contains(caddyfile, want) {
			t.Fatalf("caddy/Caddyfile missing %q:\n%s", want, caddyfile)
		}
	}
	// The proxy-level token gate was retired when auth moved into the binary (d5eae07f).
	// None of the old token-matcher primitives may reappear — that would re-expose the
	// pre-Authula posture where the cockpit SPA and login page were never reachable.
	for _, retired := range []string{"forward_auth", "@authed", "X-Aura-Token", "respond 401", "AURA_ACCESS_TOKEN"} {
		if strings.Contains(caddyfile, retired) {
			t.Fatalf("caddy/Caddyfile should not contain retired proxy-token primitive %q:\n%s", retired, caddyfile)
		}
	}
	// gVisor is a knob now, not a file: compose.gvisor.yaml was three lines plus a systemd
	// drop-in that rewrote ExecStart just to add a -f. The tier still exists and is still
	// opt-in — it just travels in .env with everything else.
	for _, want := range []string{
		"runtime: ${AURA_RUNTIME:-runc}",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml missing %q:\n%s", want, compose)
		}
	}
	for _, retired := range []string{"cap_drop", "read_only"} {
		if strings.Contains(compose, retired) {
			t.Fatalf("compose.yaml should not contain %q:\n%s", retired, compose)
		}
	}
	// The isolation tier must not silently disappear: runc is the default, so a typo in the
	// knob name would degrade to a plain container without failing anything.
	if !strings.Contains(installer, "set_env_value AURA_RUNTIME runsc") {
		t.Fatalf("scripts/install.sh must set AURA_RUNTIME=runsc for --gvisor:\n%s", installer)
	}

	// Nothing in the image may warm a uv cache for a recipe to consume at runtime. compose
	// mounts a named volume over /root/.cache/uv for the host-direct shell_exec path, and a
	// named volume is seeded ONCE at creation and never refreshed — so a warm-up here is
	// invisible to every image built after that volume existed. The calculator recipe was
	// built on exactly that assumption and is retired; this keeps the shape out.
	// Only the image side is checkable as text here; the recipe side is asserted against the
	// real catalog data in manager.TestNoRecipeDependsOnABuildTimeUVCache, which a comment
	// mentioning UV_OFFLINE cannot trip.
	if strings.Contains(dockerfile, "RUN uvx ") {
		t.Fatalf("docker/aura/Dockerfile warms a uvx package: the /root/.cache/uv volume shadows it at runtime")
	}

	// The mini PC file is the only other compose the installer ships; it must keep the
	// GPU-less machine GPU-less. A default inherited from the base is how the CUDA image
	// ended up on a box with no GPU, where llama.cpp only WARNS and falls back to CPU.
	for _, want := range []string{
		"AURA_EMBED_IMAGE:-ghcr.io/ggml-org/llama.cpp:server}",
		"deploy: !reset null",
		"COMPOSE_FILE=compose.yaml:compose.minipc.yaml",
	} {
		if !strings.Contains(minipc, want) {
			t.Fatalf("compose.minipc.yaml missing %q:\n%s", want, minipc)
		}
	}
	for _, want := range []string{".git", ".worktrees", "output", ".env"} {
		if !strings.Contains(dockerignore, want) {
			t.Fatalf(".dockerignore missing %q:\n%s", want, dockerignore)
		}
	}
	for _, want := range []string{
		"garage bucket allow --read --write --owner",
		"required_env_value AURA_OBJECTSTORE_ACCESS_KEY",
		"required_env_value AURA_OBJECTSTORE_SECRET_KEY",
		"AURA_OBJECTSTORE_ENDPOINT=http://garage:3900",
		"aura objectstore bootstrap",
	} {
		if !strings.Contains(garageBootstrap, want) {
			t.Fatalf("scripts/garage_bootstrap.sh missing %q:\n%s", want, garageBootstrap)
		}
	}
	for _, retired := range []string{
		"GK000000000000000000000000",
		"0000000000000000000000000000000000000000000000000000000000000000",
		"aura-searxng-internal-only-change-via-SEARXNG_SECRET",
	} {
		if strings.Contains(compose+garageBootstrap, retired) {
			t.Fatalf("container artifacts still contain retired dummy secret %q", retired)
		}
	}
}

func TestMCPNeo4jRuntimeAndCoverageAreReproducible(t *testing.T) {
	root := repoRootForTest(t)
	const pinnedInstall = "mcp-neo4j-cypher==0.6.0 fastmcp==2.13.3"
	for _, rel := range []string{
		"docker/aura/Dockerfile",
		"docker/aura-sandbox/Dockerfile",
		"docker/mcp-neo4j-cypher/Dockerfile",
	} {
		contents := readProjectFile(t, root, rel)
		if !strings.Contains(contents, pinnedInstall) {
			t.Errorf("%s must pin the compatible FastMCP runtime with %q", rel, pinnedInstall)
		}
	}

	coverageGate := readProjectFile(t, root, "scripts/coverage_gate.sh")
	goTest := regexp.MustCompile(`(?s)go test .*?-count=1 .*?-covermode=atomic`)
	if !goTest.MatchString(coverageGate) {
		t.Error("scripts/coverage_gate.sh must force -count=1 before accepting integration coverage")
	}

	ci := readProjectFile(t, root, ".github/workflows/ci.yml")
	const pinnedCIInject = "pipx inject --force mcp-neo4j-cypher fastmcp==2.13.3"
	if got := strings.Count(ci, pinnedCIInject); got != 2 {
		t.Errorf("CI must pin FastMCP in both Neo4j jobs: found %d occurrences of %q, want 2", got, pinnedCIInject)
	}
}

func TestAuraServiceCanResolveContainerInternalSearxng(t *testing.T) {
	root := repoRootForTest(t)
	compose := readProjectFile(t, root, "compose.yaml")
	aura := composeServiceBlock(t, compose, "aura")
	if !strings.Contains(aura, "SEARXNG_URL: http://searxng:8080/search") {
		t.Fatalf("aura service must keep the container-internal SearXNG URL:\n%s", aura)
	}
	if !regexp.MustCompile(`(?m)^    networks:\n      - default\n      - aura-web$`).MatchString(aura) {
		t.Fatalf("aura service must join default and aura-web networks so SEARXNG_URL resolves:\n%s", aura)
	}
}

func TestGarageBootstrapCanReachIdempotencyRegistry(t *testing.T) {
	root := repoRootForTest(t)
	compose := readProjectFile(t, root, "compose.yaml")
	bootstrap := composeServiceBlock(t, compose, "garage-bootstrap")
	for _, want := range []string{
		"aura-migrate:\n        condition: service_completed_successfully",
		"AURA_DB_URL: postgres://${AURA_DB_APP_ROLE:-aura_app}:${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in .env}@postgres:5432/${POSTGRES_DB:-aura}?sslmode=disable",
	} {
		if !strings.Contains(bootstrap, want) {
			t.Fatalf("garage-bootstrap service missing idempotency registry dependency %q:\n%s", want, bootstrap)
		}
	}
}

func hasActiveEnvAssignment(contents, key string) bool {
	prefix := key + "="
	for line := range strings.SplitSeq(contents, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func hasActiveEnvLine(contents, want string) bool {
	for line := range strings.SplitSeq(contents, "\n") {
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

func composeServiceBlock(t *testing.T, compose, name string) string {
	t.Helper()
	compose = strings.ReplaceAll(compose, "\r\n", "\n")
	startRe := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `:\n`)
	loc := startRe.FindStringIndex(compose)
	if loc == nil {
		t.Fatalf("compose service %q not found", name)
	}
	rest := compose[loc[1]:]
	endRe := regexp.MustCompile(`(?m)^  [A-Za-z0-9_-]+:\n`)
	end := endRe.FindStringIndex(rest)
	if end == nil {
		return compose[loc[0]:]
	}
	return compose[loc[0] : loc[1]+end[0]]
}
