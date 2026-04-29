package skill

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestTrustLevelString(t *testing.T) {
	tests := []struct {
		level TrustLevel
		want  string
	}{
		{Local, "local"},
		{Verified, "verified"},
		{Untrusted, "untrusted"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("TrustLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestTrustLevelFromString(t *testing.T) {
	tests := []struct {
		input string
		want  TrustLevel
		err   bool
	}{
		{"local", Local, false},
		{"verified", Verified, false},
		{"untrusted", Untrusted, false},
		{"unknown", Untrusted, true},
	}
	for _, tt := range tests {
		got, err := TrustLevelFromString(tt.input)
		if tt.err && err == nil {
			t.Errorf("TrustLevelFromString(%q): expected error, got nil", tt.input)
		}
		if !tt.err && got != tt.want {
			t.Errorf("TrustLevelFromString(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDefaultConstraints(t *testing.T) {
	local := DefaultConstraints(Local)
	if !netAllowed(local.Network) {
		t.Error("Local skills should have network access")
	}
	if local.Timeout == 0 {
		t.Error("Local skills should have a timeout")
	}

	verified := DefaultConstraints(Verified)
	if !netAllowed(verified.Network) {
		t.Error("Verified skills should have network access")
	}
	if verified.CPULimit == 0 {
		t.Error("Verified skills should have a CPU limit")
	}
	if verified.MemoryMB == 0 {
		t.Error("Verified skills should have a memory limit")
	}

	untrusted := DefaultConstraints(Untrusted)
	if netAllowed(untrusted.Network) {
		t.Error("Untrusted skills should NOT have network access")
	}
	if untrusted.CPULimit == 0 {
		t.Error("Untrusted skills should have a CPU limit")
	}
	if untrusted.MemoryMB == 0 {
		t.Error("Untrusted skills should have a memory limit")
	}
}

func TestResolveConstraintsDefaults(t *testing.T) {
	// Skill with no overrides uses trust-level defaults
	skill := Skill{
		Name:    "test",
		Command: "echo",
		Trust:   Local,
	}
	c := resolveConstraints(skill)

	if !netAllowed(c.Network) {
		t.Error("Local skill should have network access by default")
	}
	if c.Timeout != 30*time.Second {
		t.Errorf("Local skill timeout = %v, want 30s", c.Timeout)
	}
}

func TestResolveConstraintsOverrides(t *testing.T) {
	// Skill with explicit overrides
	skill := Skill{
		Name:    "test",
		Command: "echo",
		Trust:   Local,
		Constraints: Constraints{
			Timeout:   60 * time.Second,
			CPULimit:  20,
			MaxOutput: 4096,
		},
	}
	c := resolveConstraints(skill)

	if c.Timeout != 60*time.Second {
		t.Errorf("override timeout = %v, want 60s", c.Timeout)
	}
	if c.CPULimit != 20 {
		t.Errorf("override CPULimit = %d, want 20", c.CPULimit)
	}
	if c.MaxOutput != 4096 {
		t.Errorf("override MaxOutput = %d, want 4096", c.MaxOutput)
	}
}

func TestResolveConstraintsUntrustedNetworkInvariant(t *testing.T) {
	// Untrusted skills must NEVER have network access, even if explicitly set to true
	skill := Skill{
		Name:    "malicious",
		Command: "curl",
		Trust:   Untrusted,
		Constraints: Constraints{
			Network: boolPtr(true), // Should be ignored
		},
	}
	c := resolveConstraints(skill)

	if netAllowed(c.Network) {
		t.Error("untrusted skill should never have network access, even if explicitly set to true")
	}
}

func TestResolveConstraintsExplicitNoNetwork(t *testing.T) {
	// Verified skill explicitly opts out of network
	skill := Skill{
		Name:    "offline-tool",
		Command: "tool",
		Trust:   Verified,
		Constraints: Constraints{
			Network: boolPtr(false),
		},
	}
	c := resolveConstraints(skill)

	if netAllowed(c.Network) {
		t.Error("skill with explicit Network=false should not have network access")
	}
}

func TestResolveConstraintsVerifiedDefaults(t *testing.T) {
	skill := Skill{
		Name:    "verified-tool",
		Command: "tool",
		Trust:   Verified,
	}
	c := resolveConstraints(skill)

	if !netAllowed(c.Network) {
		t.Error("Verified skill should have network access by default")
	}
	if c.CPULimit != 30 {
		t.Errorf("Verified CPULimit = %d, want 30", c.CPULimit)
	}
	if c.MemoryMB != 256 {
		t.Errorf("Verified MemoryMB = %d, want 256", c.MemoryMB)
	}
}

func TestRunLocalCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}

	runner := NewRunner(slog.Default())
	skill := Skill{
		Name:    "echo-hello",
		Command: "echo",
		Args:    []string{"hello"},
		Trust:   Local,
		Constraints: Constraints{
			Timeout:   5 * time.Second,
			MaxOutput: 1024,
		},
	}

	result, err := runner.Run(context.Background(), skill, "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Stdout == "" {
		t.Error("Stdout should not be empty")
	}
}

func TestRunWithInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}

	runner := NewRunner(slog.Default())
	skill := Skill{
		Name:    "cat-stdin",
		Command: "cat",
		Trust:   Local,
		Constraints: Constraints{
			Timeout:   5 * time.Second,
			MaxOutput: 1024,
		},
	}

	result, err := runner.Run(context.Background(), skill, "test input data")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestRunTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows (sleep command)")
	}

	runner := NewRunner(slog.Default())
	skill := Skill{
		Name:    "slow-command",
		Command: "sleep",
		Args:    []string{"10"},
		Trust:   Untrusted,
		Constraints: Constraints{
			Timeout:   100 * time.Millisecond,
			MaxOutput: 1024,
		},
	}

	result, err := runner.Run(context.Background(), skill, "")
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if result != nil && !result.TimedOut {
		t.Error("expected TimedOut to be true")
	}
}

func TestRunNoCommand(t *testing.T) {
	runner := NewRunner(slog.Default())
	skill := Skill{
		Name:    "empty",
		Command: "",
		Trust:   Local,
	}

	_, err := runner.Run(context.Background(), skill, "")
	if err == nil {
		t.Error("expected error for empty command, got nil")
	}
}

func TestRunUntrustedNetwork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}

	runner := NewRunner(slog.Default())
	skill := Skill{
		Name:    "env-check",
		Command: "env",
		Trust:   Untrusted,
		Constraints: Constraints{
			Timeout:   5 * time.Second,
			MaxOutput: 4096,
		},
	}

	result, err := runner.Run(context.Background(), skill, "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Untrusted should have restricted env — no HTTP_PROXY etc
	if containsEnvVar(result.Stdout, "HTTP_PROXY") {
		t.Error("untrusted skill should not have HTTP_PROXY in env")
	}
}

func TestRestrictedEnv(t *testing.T) {
	env := restrictedEnv(Untrusted)
	if len(env) == 0 {
		t.Error("untrusted env should not be empty")
	}
	for _, e := range env {
		if len(e) >= 10 && e[:10] == "HTTP_PROXY" {
			t.Errorf("untrusted env should not contain HTTP_PROXY: %s", e)
		}
	}

	// Verified should have more env vars than untrusted
	verifiedEnv := restrictedEnv(Verified)
	if len(verifiedEnv) <= len(env) {
		t.Error("verified env should be richer than untrusted env")
	}
}

func TestBoundedBuffer(t *testing.T) {
	buf := &boundedBuffer{limit: 10}
	buf.Write([]byte("hello"))
	buf.Write([]byte(" world")) // 11 bytes total, limit 10

	result := buf.String()
	if len(result) > 10 {
		t.Errorf("boundedBuffer should limit output: got %d bytes", len(result))
	}
	if result != "hello worl" {
		t.Errorf("boundedBuffer content = %q, want %q", result, "hello worl")
	}
	if !buf.truncated {
		t.Error("boundedBuffer should mark truncated when output exceeds limit")
	}
}

func TestBoundedBufferNoLimit(t *testing.T) {
	buf := &boundedBuffer{limit: 0}
	buf.Write([]byte("hello world this is a longer string"))

	if buf.String() != "hello world this is a longer string" {
		t.Errorf("boundedBuffer without limit should not truncate")
	}
	if buf.truncated {
		t.Error("boundedBuffer without limit should not mark truncated")
	}
}

func TestBoundedBufferExactLimit(t *testing.T) {
	buf := &boundedBuffer{limit: 5}
	buf.Write([]byte("12345"))

	if buf.String() != "12345" {
		t.Errorf("boundedBuffer at exact limit should not truncate: got %q", buf.String())
	}
	if buf.truncated {
		t.Error("boundedBuffer at exact limit should not mark truncated")
	}
}

func TestBoundedBufferOverLimit(t *testing.T) {
	buf := &boundedBuffer{limit: 5}
	buf.Write([]byte("1234567890"))

	// Should only contain first 5 bytes
	if len(buf.String()) > 5 {
		t.Errorf("boundedBuffer should truncate at limit: got %d bytes", len(buf.String()))
	}
	if !buf.truncated {
		t.Error("boundedBuffer should mark truncated when single write exceeds limit")
	}
}

func TestExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}

	// Successful command
	runner := NewRunner(slog.Default())
	skill := Skill{
		Name:    "true",
		Command: "true",
		Trust:   Local,
		Constraints: Constraints{
			Timeout:   5 * time.Second,
			MaxOutput: 1024,
		},
	}

	result, err := runner.Run(context.Background(), skill, "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code for 'true' = %d, want 0", result.ExitCode)
	}

	// Failing command
	skillFail := Skill{
		Name:    "false",
		Command: "false",
		Trust:   Local,
		Constraints: Constraints{
			Timeout:   5 * time.Second,
			MaxOutput: 1024,
		},
	}

	result, _ = runner.Run(context.Background(), skillFail, "")
	if result.ExitCode == 0 {
		t.Error("exit code for 'false' should be non-zero")
	}
}

func TestResolveConstraintsZeroValues(t *testing.T) {
	// Skill with zero-value Constraints should use defaults
	skill := Skill{
		Name:    "test",
		Command: "echo",
		Trust:   Untrusted,
	}
	c := resolveConstraints(skill)

	if netAllowed(c.Network) {
		t.Error("Untrusted skill should not have network access")
	}
	if c.Timeout != 10*time.Second {
		t.Errorf("Untrusted timeout = %v, want 10s", c.Timeout)
	}
	if c.CPULimit != 10 {
		t.Errorf("Untrusted CPULimit = %d, want 10", c.CPULimit)
	}
	if c.MemoryMB != 128 {
		t.Errorf("Untrusted MemoryMB = %d, want 128", c.MemoryMB)
	}
}

func TestStringReaderEOF(t *testing.T) {
	r := newStringReader("hi")
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	if n != 2 {
		t.Errorf("first read: n = %d, want 2", n)
	}
	if err != nil {
		t.Errorf("first read: unexpected error %v", err)
	}
	n, err = r.Read(buf)
	if n != 0 {
		t.Errorf("second read: n = %d, want 0", n)
	}
	if err != io.EOF {
		t.Errorf("second read: error = %v, want io.EOF", err)
	}
}

func containsEnvVar(output, varName string) bool {
	return len(output) >= len(varName) && output[:len(varName)] == varName ||
		containsSubstring(output, varName+"=")
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}