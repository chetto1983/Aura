package debugcommon

import (
	"testing"
)

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
