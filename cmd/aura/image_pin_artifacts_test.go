package main

import (
	"regexp"
	"strings"
	"testing"
)

// An edge appliance takes compose.yaml and its overlays from its aura image on every update
// tick, so a version pinned there reaches the whole fleet -- but only when nothing on the host
// outranks the pin, and only when it is a version at all. Measured 2026-09-14 on the mini PC:
// the installer had written POSTGRES_IMAGE and AURA_EMBED_IMAGE into .env, where they
// outranked compose's defaults, and the llama.cpp/whisper/kokoro defaults were moving tags no
// update ever re-pulled -- the embedding sidecar ran llama.cpp b10884 while b10951 was out.
func TestThirdPartyImagesArePinnedWhereTheUpdaterDeliversThem(t *testing.T) {
	root := repoRootForTest(t)
	files := map[string]string{}
	for _, rel := range []string{"compose.yaml", "compose.vulkan.yaml", "compose.cpu.yaml", ".github/compose.ci-cache.yaml"} {
		files[rel] = readProjectFile(t, root, rel)
	}

	imageLine := regexp.MustCompile(`(?m)^\s+image:\s*(\S+)\s*$`)
	llamaBuild := regexp.MustCompile(`llama\.cpp:server(?:-cuda|-vulkan)?-b(\d+)`)
	builds := map[string][]string{}
	for rel, contents := range files {
		for _, match := range imageLine.FindAllStringSubmatch(contents, -1) {
			ref := composeDefault(match[1])
			if !isThirdPartyImage(ref) {
				continue
			}
			if !isPinnedImage(ref) {
				t.Errorf("%s: %s rides a moving tag, which no update re-pulls and no fleet can verify", rel, match[1])
			}
			if build := llamaBuild.FindStringSubmatch(ref); build != nil {
				builds[build[1]] = append(builds[build[1]], rel)
			} else if strings.Contains(ref, "llama.cpp:") {
				t.Errorf("%s: %s is not pinned to a llama.cpp build", rel, match[1])
			}
		}
	}
	if len(builds) != 1 {
		t.Errorf("the stack runs more than one llama.cpp build, so CUDA, Vulkan, CPU and CI hosts do not test the same server: %v", builds)
	}
}

// The update timer deletes RETIRED_ENV_KEYS from every appliance's .env. A compose file that
// still reads one would let the stale value outrank its pin again, and a template line or an
// installer write would put the key straight back on the next install.
func TestRetiredEnvKeysStayRetired(t *testing.T) {
	root := repoRootForTest(t)
	declared := regexp.MustCompile(`(?m)^RETIRED_ENV_KEYS=\(([^)]*)\)`).
		FindStringSubmatch(readProjectFile(t, root, "deploy/aura-image-update.sh"))
	if declared == nil || len(strings.Fields(declared[1])) == 0 {
		t.Fatal("deploy/aura-image-update.sh declares no RETIRED_ENV_KEYS")
	}
	envExample := readProjectFile(t, root, ".env.example")
	installer := readProjectFile(t, root, "scripts/install.sh")
	for key := range strings.FieldsSeq(declared[1]) {
		for _, rel := range []string{"compose.yaml", "compose.vulkan.yaml", "compose.cpu.yaml"} {
			if strings.Contains(readProjectFile(t, root, rel), "${"+key) {
				t.Errorf("%s reads ${%s}, which outranks the pin on every host that still has it", rel, key)
			}
		}
		if hasActiveEnvAssignment(envExample, key) {
			t.Errorf(".env.example sets %s, which the update timer removes from every appliance", key)
		}
		written := regexp.MustCompile(`(?m)(\b(set_env_value|ensure_env_default)\s+` + key + `\b|^` + key + `=)`)
		if written.MatchString(installer) {
			t.Errorf("scripts/install.sh writes %s, which the update timer removes from every appliance", key)
		}
	}
}

// composeDefault resolves ${VAR:-default} to the default a host without VAR runs.
func composeDefault(ref string) string {
	if strings.HasPrefix(ref, "${") && strings.HasSuffix(ref, "}") {
		if _, fallback, ok := strings.Cut(ref[2:len(ref)-1], ":-"); ok {
			return fallback
		}
	}
	return ref
}

// Repo-built images ride the edge channel's moving tags on purpose, published and pulled by
// the same pipeline; locally built ones are named aura*.
func isThirdPartyImage(ref string) bool {
	return !strings.HasPrefix(ref, "ghcr.io/chetto1983/") && !strings.HasPrefix(ref, "aura")
}

// A digest is a pin; so is a tag carrying a version number. latest, server-cuda and their kind
// carry none.
func isPinnedImage(ref string) bool {
	if strings.Contains(ref, "@sha256:") {
		return true
	}
	name := ref[strings.LastIndex(ref, "/")+1:]
	_, tag, ok := strings.Cut(name, ":")
	return ok && strings.ContainsAny(tag, "0123456789")
}
