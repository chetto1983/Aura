package debugcommon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(`
# comment
AURA_DEBUGCOMMON_FOO = "bar baz"
AURA_DEBUGCOMMON_SINGLE='quoted'
ignored-line
`), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("AURA_DEBUGCOMMON_FOO", "")
	t.Setenv("AURA_DEBUGCOMMON_SINGLE", "")

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("AURA_DEBUGCOMMON_FOO"); got != "bar baz" {
		t.Fatalf("AURA_DEBUGCOMMON_FOO = %q, want %q", got, "bar baz")
	}
	if got := os.Getenv("AURA_DEBUGCOMMON_SINGLE"); got != "quoted" {
		t.Fatalf("AURA_DEBUGCOMMON_SINGLE = %q, want %q", got, "quoted")
	}
}

func TestEnvDefault(t *testing.T) {
	t.Setenv("AURA_DEBUGCOMMON_DEFAULT", "  configured  ")
	if got := EnvDefault("AURA_DEBUGCOMMON_DEFAULT", "fallback"); got != "configured" {
		t.Fatalf("EnvDefault configured = %q, want %q", got, "configured")
	}
	t.Setenv("AURA_DEBUGCOMMON_DEFAULT", " ")
	if got := EnvDefault("AURA_DEBUGCOMMON_DEFAULT", "fallback"); got != "fallback" {
		t.Fatalf("EnvDefault blank = %q, want %q", got, "fallback")
	}
}

func TestContainsHelpers(t *testing.T) {
	if !ContainsTools([]string{"read_file", "write_file"}, []string{"write_file"}) {
		t.Fatal("ContainsTools should find wanted tool")
	}
	if ContainsTools([]string{"read_file"}, []string{"write_file"}) {
		t.Fatal("ContainsTools should reject missing tool")
	}
	if !ContainsAllText("Marker Cerulean-731", []string{"cerulean"}) {
		t.Fatal("ContainsAllText should be case-insensitive")
	}
	if ContainsNoText("tool error: denied", []string{"tool error"}) {
		t.Fatal("ContainsNoText should reject present text")
	}
	if !ContainsNoText("ok", []string{" "}) {
		t.Fatal("ContainsNoText should ignore blank rejects")
	}
}

func TestSingleLine(t *testing.T) {
	if got := SingleLine("hello\n  debug\tworld", 100); got != "hello debug world" {
		t.Fatalf("SingleLine collapse = %q", got)
	}
	if got := SingleLine("abcdef", 4); got != "abcd..." {
		t.Fatalf("SingleLine truncate = %q", got)
	}
}
