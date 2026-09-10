package skills

import (
	"slices"
	"strings"
	"testing"
)

func TestExecCommandEnvCarriesNoCredential(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-must-not-leak")
	t.Setenv("AURA_OPENROUTER_MANAGEMENT_KEY", "sk-or-v1-management-must-not-leak")
	env := execCommandEnv()
	for _, kv := range env {
		if strings.Contains(kv, "must-not-leak") {
			t.Fatalf("npx environment carries a credential under %s", strings.SplitN(kv, "=", 2)[0])
		}
	}
	if !slices.Contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Fatal("npx environment lost GIT_TERMINAL_PROMPT=0")
	}
	if !slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "PATH=") }) {
		t.Fatal("npx environment lost PATH, so npx itself cannot be found")
	}
}
