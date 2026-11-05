package main

import (
	"regexp"
	"strings"
	"testing"
)

const decideService = "aura-llama-decide"

var composeFilesWithLlamaPin = []string{
	"compose.yaml",
	"compose.cpu.yaml",
	"compose.vulkan.yaml",
	".github/compose.ci-cache.yaml",
}

func TestDecideSidecarIsOptionalAndPinnedToTheStacksLlamaBuild(t *testing.T) {
	root := repoRootForTest(t)
	compose := readProjectFile(t, root, "compose.yaml")
	block := composeServiceBlock(t, compose, decideService)

	if !regexp.MustCompile(`(?m)^    profiles: \[localdecide\]\s*$`).MatchString(block) {
		t.Fatalf("%s must sit behind the localdecide profile alone:\n%s", decideService, block)
	}
	if strings.Contains(composeServiceBlock(t, compose, "aura"), decideService+":") {
		t.Fatalf("aura must not depend on the optional %s", decideService)
	}
	for _, want := range []string{"--alias", "d1-3B-Q4_K_M", "/root/.cache/llama.cpp/d1-3B-Q4_K_M.gguf", `${AURA_EMBED_NGL:-99}`, "127.0.0.1:8085:8085"} {
		if !strings.Contains(block, want) {
			t.Fatalf("%s is missing %q:\n%s", decideService, want, block)
		}
	}

	for _, file := range composeFilesWithLlamaPin {
		pinned := composeServiceBlock(t, readProjectFile(t, root, file), decideService)
		if !regexp.MustCompile(`(?m)image: \S*llama\.cpp:server(-[a-z]+)?-b11960\s*$`).MatchString(pinned) {
			t.Fatalf("%s must pin %s to llama.cpp b11960:\n%s", file, decideService, pinned)
		}
	}
}

func TestAuraForwardsTheDecideAddressAndNoKey(t *testing.T) {
	root := repoRootForTest(t)
	aura := composeServiceBlock(t, readProjectFile(t, root, "compose.yaml"), "aura")

	if !strings.Contains(aura, "AURA_DECIDE_BASE_URL: ${AURA_DECIDE_BASE_URL:-}") {
		t.Fatalf("aura must forward AURA_DECIDE_BASE_URL, empty when the installer wrote none:\n%s", aura)
	}
	for line := range strings.SplitSeq(aura, "\n") {
		if strings.Contains(line, "AURA_DECIDE") && strings.Contains(strings.ToUpper(line), "KEY") {
			t.Fatalf("aura must forward no decision-model key, found %q", line)
		}
	}
}

func TestEmbeddingGGUFIsStillTheFirstModelCompose(t *testing.T) {
	root := repoRootForTest(t)
	compose := readProjectFile(t, root, "compose.yaml")
	first := regexp.MustCompile(`(?m)^\s+- -m\n\s+- (/\S+\.gguf)\s*$`).FindStringSubmatch(compose)
	if first == nil || !strings.HasSuffix(first[1], "embeddinggemma-300M-Q8_0.gguf") {
		t.Fatalf("the first literal -m in compose.yaml must be the embedding model, got %v", first)
	}
}
