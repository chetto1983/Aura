package main

import (
	"os"
	"path/filepath"
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
	whatsappDockerfile := readProjectFile(t, root, "docker/whatsapp/Dockerfile")
	dockerignore := readProjectFile(t, root, ".dockerignore")
	if _, err := os.Stat(filepath.Join(root, "docker/whatsapp/whatsapp-mcp-src.tar.gz")); err != nil {
		t.Fatalf("docker/whatsapp/whatsapp-mcp-src.tar.gz missing: %v", err)
	}

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
		"AURA_AGUI_BIND: 0.0.0.0:9080",
		"AURA_SETUP_BIND: 0.0.0.0:9081",
		"aura-migrate:",
		"service_completed_successfully",
		"command: \"aura db migrate && aura neo4j migrate\"",
		"AURA_CONFIG_DIR: /var/lib/aura",
		"aura-home:/var/lib/aura",
		"127.0.0.1:${AURA_SETUP_PORT:-9081}:9081",
		"whatsapp:",
		"127.0.0.1:${AURA_WHATSAPP_MCP_PORT:-8092}:8080",
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
		"tls internal",
		"@authed",
		"X-Aura-Token",
		"query({'token': '{$AURA_ACCESS_TOKEN}'})",
		"reverse_proxy aura:9080",
		"reverse_proxy aura:9081",
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
	for _, want := range []string{
		"FROM golang:bookworm AS bridge-build",
		"FROM python:3.11-slim",
		"whatsapp-mcp-src.tar.gz",
		"CGO_ENABLED=1 go build",
		"WHATSAPP_BRIDGE_PORT=8081",
	} {
		if !strings.Contains(whatsappDockerfile, want) {
			t.Fatalf("docker/whatsapp/Dockerfile missing %q:\n%s", want, whatsappDockerfile)
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
