package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMainShellBootsRealREPL drives the actual root dispatch in a subprocess.
// With no database configured, the real chat/shell boot path fails fast on the infra it needs
// before DB open (the key may live in aura.settings, so it is checked after); the old
// placeholder returned 0 and printed a TODO, which is not an industrial shell.
func TestMainShellBootsRealREPL(t *testing.T) {
	if os.Getenv("AURA_TEST_MAIN_SHELL") == "1" {
		os.Args = []string{"aura", "shell"}
		main()
		return
	}

	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestMainShellBootsRealREPL$") //nolint:gosec // re-exec self
	cmd.Env = append(os.Environ(),
		"AURA_TEST_MAIN_SHELL=1",
		"HOME="+home,
		"USERPROFILE="+home,
		"OPENROUTER_API_KEY=",
		// The hosted provider is the case this asserts, and it stopped being implicit when
		// the empty-key gate started reading AURA_LLM_PROVIDER (amendment #219): the child
		// inherits os.Environ(), so a developer with a LOCAL provider exported would boot a
		// working shell here and see this fail for the wrong reason. Empty is the default,
		// which is hosted.
		"AURA_LLM_PROVIDER=",
		"AURA_LLM_MODEL=",
		"AURA_LLM_BASE_URL=",
		"AURA_LLM_TEMPERATURE=",
		"AURA_LLM_MAX_TOKENS=",
		"POSTGRES_PASSWORD=",
		"AURA_DB_URL=",
		"AURA_DB_MIGRATE_URL=",
	)

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("aura shell must boot the real REPL and fail without a database; got success:\n%s", out)
	}
	got := string(out)
	if !strings.Contains(got, "aura shell: config: POSTGRES_PASSWORD") {
		t.Fatalf("missing shell-prefixed config error:\n%s", got)
	}
	if strings.Contains(got, "TODO") {
		t.Fatalf("aura shell still hit the placeholder:\n%s", got)
	}
}
