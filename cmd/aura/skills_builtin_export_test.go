package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

func TestNativeArtifactScriptsReachTheSandboxExport(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	cfg := &config.Config{SkillsDir: filepath.Join(base, "active"), SkillExportDir: filepath.Join(base, "export")}
	tool := newSkillTool(cfg, nil)
	if tool.Loader == nil {
		t.Fatal("native skill loader not initialized")
	}
	for _, resource := range []string{"SKILL.md", "scripts/init-artifact.sh", "scripts/bundle-artifact.sh", "scripts/verify-artifact.py", "scripts/shadcn-components.tar.gz"} {
		active, err := os.ReadFile(filepath.Join(cfg.SkillsDir, "web-artifacts-builder", resource))
		if err != nil {
			t.Fatal(err)
		}
		exported, err := os.ReadFile(filepath.Join(cfg.SkillExportDir, "web-artifacts-builder", resource))
		if err != nil {
			t.Fatalf("the agent can read instructions but cannot execute %s: %v", resource, err)
		}
		if !bytes.Equal(active, exported) {
			t.Fatalf("export is stale: %s", resource)
		}
	}
}
