package main

import (
	"regexp"
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
	// The upstream binary must be a dated release pinned by digest. Which release is
	// Dependabot's to move, so the test asserts the pin's shape, not one version.
	if !regexp.MustCompile(`(?m)^FROM cloudflare/cloudflared:\d{4}\.\d+\.\d+@sha256:[0-9a-f]{64}$`).MatchString(dockerfile) {
		t.Error("image does not pin cloudflare/cloudflared to a release digest")
	}
	for _, required := range []string{"USER 65532:65532", "CGO_ENABLED=0", "GOARCH=$TARGETARCH", "healthcheck"} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("image lacks %s", required)
		}
	}
	for _, rel := range []string{"caddy/Caddyfile", "caddy/Caddyfile.domain"} {
		text := readProjectFile(t, root, rel)
		if strings.Count(text, "(aura_frontdoor) {") != 1 || !strings.Contains(text, "http://:8080 {\n\trequest_header X-Aura-Remote-Ingress tunnel\n\timport aura_frontdoor\n}") {
			t.Fatalf("%s does not share routing", rel)
		}
		if !strings.Contains(text, "request_header -X-Aura-Remote-Ingress") {
			t.Fatalf("%s permits spoofed direct-ingress acceptance", rel)
		}
		if strings.Count(text, "reverse_proxy garage:3900") != 1 {
			t.Fatalf("%s duplicates object-store route", rel)
		}
	}
}
