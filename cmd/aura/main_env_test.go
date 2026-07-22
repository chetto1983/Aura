package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMainLoadsDotEnvForMCPDispatch(t *testing.T) {
	if runMainMCPDotEnvChild() {
		return
	}

	wd := t.TempDir()
	home := t.TempDir()
	want := filepath.Join(wd, "managed", "servers.json")
	writeDotEnvMCPConfig(t, wd, want)

	out, err := runMainMCPDotEnvSubprocess(t, wd, home, nil)
	if err != nil {
		t.Fatalf("aura mcp add with .env AURA_MCP_CONFIG failed: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(want); statErr != nil {
		t.Fatalf("main did not load .env before MCP dispatch; custom config missing: %v\n%s", statErr, out)
	}
	defaultPath := filepath.Join(home, ".aura", "mcp", "servers.json")
	if _, statErr := os.Stat(defaultPath); !os.IsNotExist(statErr) {
		t.Fatalf("MCP dispatch wrote default path %q despite .env override (stat err=%v)", defaultPath, statErr)
	}
}

func TestMainDotEnvDoesNotOverrideProcessEnv(t *testing.T) {
	if runMainMCPDotEnvChild() {
		return
	}

	wd := t.TempDir()
	home := t.TempDir()
	dotenvPath := filepath.Join(wd, "dotenv", "servers.json")
	processPath := filepath.Join(wd, "process", "servers.json")
	writeDotEnvMCPConfig(t, wd, dotenvPath)

	out, err := runMainMCPDotEnvSubprocess(t, wd, home, []string{"AURA_MCP_CONFIG=" + processPath})
	if err != nil {
		t.Fatalf("aura mcp add with process AURA_MCP_CONFIG failed: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(processPath); statErr != nil {
		t.Fatalf("process-set AURA_MCP_CONFIG should win over .env; process config missing: %v\n%s", statErr, out)
	}
	if _, statErr := os.Stat(dotenvPath); !os.IsNotExist(statErr) {
		t.Fatalf(".env path %q was written even though process env should win (stat err=%v)", dotenvPath, statErr)
	}
}

func runMainMCPDotEnvChild() bool {
	if os.Getenv("AURA_TEST_MAIN_MCP_DOTENV") != "1" {
		return false
	}
	name := os.Getenv("AURA_TEST_MCP_SERVER_NAME")
	if name == "" {
		name = "dotenv"
	}
	os.Args = []string{"aura", "mcp", "add", name, "--", "echo", "ok"}
	main()
	os.Exit(0)
	return true
}

func runMainMCPDotEnvSubprocess(t *testing.T, wd, home string, extraEnv []string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$") //nolint:gosec // re-exec self
	cmd.Dir = wd
	cmd.Env = append(filteredEnv("AURA_MCP_CONFIG", "AURA_TEST_MAIN_MCP_DOTENV", "AURA_TEST_MCP_SERVER_NAME"),
		"AURA_TEST_MAIN_MCP_DOTENV=1",
		// This test exercises dotenv-to-dispatch wiring inside the marked child;
		// durable parent acquisition has its own registry/executor coverage.
		cliIdempotencyChildEnv+"=1",
		"AURA_TEST_MCP_SERVER_NAME="+strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-")),
		"HOME="+home,
		"USERPROFILE="+home,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd.CombinedOutput()
}

func writeDotEnvMCPConfig(t *testing.T, dir, path string) {
	t.Helper()
	value := filepath.ToSlash(path)
	line := "AURA_MCP_CONFIG=" + strconv.Quote(value) + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(line), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
}

func filteredEnv(remove ...string) []string {
	drop := make(map[string]struct{}, len(remove))
	for _, k := range remove {
		drop[strings.ToUpper(k)] = struct{}{}
	}
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, shouldDrop := drop[strings.ToUpper(key)]; shouldDrop {
			continue
		}
		out = append(out, kv)
	}
	return out
}
