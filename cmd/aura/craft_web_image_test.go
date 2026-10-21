package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCraftWebImagePinMatchesItsInputs fails when docker/craft-web/ or its Dockerfile changes
// and the aura image still copies the Craft apps built from the old inputs. The tag is the
// inputs' hash (scripts/craft_web_inputs.sh); publish-craft-web.yml pushes that tag and prints
// the digest to pin beside it.
func TestCraftWebImagePinMatchesItsInputs(t *testing.T) {
	root := repoRootForTest(t)
	out, err := exec.Command("bash", filepath.Join(root, "scripts", "craft_web_inputs.sh")).Output()
	if err != nil {
		t.Fatalf("scripts/craft_web_inputs.sh: %v", err)
	}
	ref := "COPY --from=ghcr.io/chetto1983/aura-craft-web:" + strings.TrimSpace(string(out)) + "@sha256:"
	if !strings.Contains(readProjectFile(t, root, "docker/aura/Dockerfile"), ref) {
		t.Fatalf("docker/aura/Dockerfile does not copy the Craft apps built from the current inputs (%q…): "+
			"run publish-craft-web.yml and pin the reference it prints", ref)
	}
}
