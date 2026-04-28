package skill

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
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
	if !local.Network {
		t.Error("Local skills should have network access")
	}
	if local.Timeout == 0 {
		t.Error("Local skills should have a timeout")
	}

	verified := DefaultConstraints(Verified)
	if !verified.Network {
		t.Error("Verified skills should have network access")
	}
	if verified.CPULimit == 0 {
		t.Error("Verified skills should have a CPU limit")
	}

	untrusted := DefaultConstraints(Untrusted)
	if untrusted.Network {
		t.Error("Untrusted skills should NOT have network access")
	}
	if untrusted.CPULimit == 0 {
		t.Error("Untrusted skills should have a CPU limit")
	}
	if untrusted.MemoryMB == 0 {
		t.Error("Untrusted skills should have a memory limit")
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

	_, err := runner.Run(context.Background(), skill, "")
	if err == nil {
		t.Error("expected timeout error, got nil")
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
		// Should not contain proxy or network-related variables
		if len(e) > 10 && e[:10] == "HTTP_PROXY" {
			t.Errorf("untrusted env should not contain HTTP_PROXY: %s", e)
		}
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
}

func TestBoundedBufferNoLimit(t *testing.T) {
	buf := &boundedBuffer{limit: 0}
	buf.Write([]byte("hello world this is a longer string"))

	if buf.String() != "hello world this is a longer string" {
		t.Errorf("boundedBuffer without limit should not truncate")
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

func TestMain(m *testing.M) {
	// Skip tests if basic commands aren't available
	if _, err := exec.LookPath("echo"); err != nil {
		fmt := "echo not found, skipping skill tests"
		_ = fmt
	}
	os.Exit(m.Run())
}
