package main

import (
	"strings"
	"testing"
)

func TestCloudflaredPackagingBoundary(t *testing.T) {
	root := repoRootForTest(t)
	compose := readProjectFile(t, root, "compose.yaml")
	service := composeServiceBlock(t, compose, "aura-cloudflared")
	for _, required := range []string{"user: \"65532:65532\"", "read_only: true", "cap_drop: [ALL]", "no-new-privileges:true", "aura-cloudflared-state:/state:ro", "- aura-tunnel", "healthcheck", "mem_limit:", "pids_limit:", "cpus:"} {
		if !strings.Contains(service, required) {
			t.Errorf("sidecar lacks %s", required)
		}
	}
	for _, forbidden := range []string{"ports:", "environment:", "docker.sock", "- default", "network_mode:", "privileged:", "POSTGRES", "AURA_DB_"} {
		if strings.Contains(service, forbidden) {
			t.Errorf("sidecar contains %s", forbidden)
		}
	}
	if !strings.Contains(composeServiceBlock(t, compose, "aura"), "aura-cloudflared-state:/var/lib/aura/cloudflared") {
		t.Fatal("Aura cannot write projection")
	}
	for _, name := range []string{"aura", "postgres"} {
		if strings.Contains(composeServiceBlock(t, compose, name), "aura-tunnel") {
			t.Fatalf("%s joins tunnel network", name)
		}
	}
	if !strings.Contains(composeServiceBlock(t, compose, "caddy"), "- aura-tunnel") {
		t.Fatal("Caddy cannot receive tunnel traffic")
	}
	dockerfile := readProjectFile(t, root, "docker/cloudflared/Dockerfile")
	for _, required := range []string{"cloudflare/cloudflared:2026.8.3@sha256:51c9cefcb4569df44e1ad403ab1d3d8065aa8e84339bcfc6aee75502e1140339", "USER 65532:65532", "CGO_ENABLED=0", "GOARCH=$TARGETARCH", "healthcheck"} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("image lacks %s", required)
		}
	}
	for _, rel := range []string{"caddy/Caddyfile", "caddy/Caddyfile.domain"} {
		text := readProjectFile(t, root, rel)
		if strings.Count(text, "(aura_frontdoor) {") != 1 || !strings.Contains(text, "http://:8080 {\n\timport aura_frontdoor\n}") {
			t.Fatalf("%s does not share routing", rel)
		}
		if strings.Count(text, "reverse_proxy garage:3900") != 1 {
			t.Fatalf("%s duplicates object-store route", rel)
		}
	}
}
