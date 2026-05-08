package runtimebootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureLayoutCreatesRuntimeWorkspace(t *testing.T) {
	root := t.TempDir()
	cfg := testLayoutConfig(root)

	if err := EnsureLayout(cfg); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	for _, path := range []string{
		"runtime-workspace/AGENT.md",
		"runtime-workspace/HEARTBEAT.md",
		"runtime-workspace/mcp.json",
		"runtime-workspace/wiki",
		"runtime-workspace/wiki/raw",
		"runtime-workspace/wiki/index.md",
		"runtime-workspace/wiki/log.md",
		"runtime-workspace/wiki/graph",
		"runtime-workspace/skills",
		"runtime-workspace/inbox",
	} {
		assertExists(t, filepath.Join(root, filepath.FromSlash(path)))
	}
	assertFileContent(t, filepath.Join(cfg.RuntimeWorkspacePath, "mcp.json"), "{}\n")
}

func TestEnsureLayoutDoesNotOverwriteExistingFiles(t *testing.T) {
	root := t.TempDir()
	cfg := testLayoutConfig(root)

	keep := map[string]string{
		filepath.Join(cfg.RuntimeWorkspacePath, "AGENT.md"):         "custom agent\n",
		filepath.Join(cfg.RuntimeWorkspacePath, "wiki", "index.md"): "custom index\n",
		filepath.Join(cfg.RuntimeWorkspacePath, "wiki", "log.md"):   "custom log\n",
		filepath.Join(cfg.RuntimeWorkspacePath, "mcp.json"):         "{\"keep\":true}\n",
		cfg.MCPServersPath: "{\"configured\":true}\n",
	}
	for path, content := range keep {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	if err := EnsureLayout(cfg); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	for path, want := range keep {
		assertFileContent(t, path, want)
	}
}

func TestEnsureLayoutCreatesParentDirsForEnvDBLogsSkillsAndMCP(t *testing.T) {
	root := t.TempDir()
	cfg := LayoutConfig{
		RuntimeWorkspacePath: filepath.Join(root, "workspace"),
		EnvPath:              filepath.Join(root, "data", "nested", ".env"),
		DBPath:               filepath.Join(root, "data", "db", "aura.db"),
		LogDir:               filepath.Join(root, "data", "logs"),
		WikiPath:             filepath.Join(root, "wiki"),
		SkillsPath:           filepath.Join(root, "runtime", "skills"),
		MCPServersPath:       filepath.Join(root, "data", "mcp", "mcp.json"),
		PromptOverlayPath:    filepath.Join(root, "runtime", "prompt"),
	}

	if err := EnsureLayout(cfg); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	for _, path := range []string{
		filepath.Dir(cfg.EnvPath),
		filepath.Dir(cfg.DBPath),
		cfg.LogDir,
		cfg.WikiPath,
		filepath.Join(cfg.WikiPath, "raw"),
		cfg.SkillsPath,
		filepath.Dir(cfg.MCPServersPath),
		cfg.PromptOverlayPath,
	} {
		assertDir(t, path)
	}
	assertNotExists(t, cfg.EnvPath)
	assertNotExists(t, cfg.DBPath)
	assertFileContent(t, cfg.MCPServersPath, "{}\n")
}

func TestEnsureLayoutCopiesRootAgentWhenPresent(t *testing.T) {
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.WriteFile("AGENT.md", []byte("root runtime agent\n"), 0o644); err != nil {
		t.Fatalf("write root agent: %v", err)
	}

	cfg := testLayoutConfig(root)
	if err := EnsureLayout(cfg); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	assertFileContent(t, filepath.Join(cfg.RuntimeWorkspacePath, "AGENT.md"), "root runtime agent\n")
}

func testLayoutConfig(root string) LayoutConfig {
	return LayoutConfig{
		RuntimeWorkspacePath: filepath.Join(root, "runtime-workspace"),
		EnvPath:              filepath.Join(root, "data", ".env"),
		DBPath:               filepath.Join(root, "data", "aura.db"),
		LogDir:               filepath.Join(root, "data", "logs"),
		WikiPath:             filepath.Join(root, "wiki"),
		SkillsPath:           filepath.Join(root, "skills"),
		MCPServersPath:       filepath.Join(root, "data", "mcp.json"),
		PromptOverlayPath:    filepath.Join(root, "prompt"),
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected directory %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected %s to be absent", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, string(got), want)
	}
}
