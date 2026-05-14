package telegram

import (
	"log/slog"
	"os/exec"
	"testing"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/agent/tools/registry"
)

func TestSetupSandboxRuntime_Disabled(t *testing.T) {
	mgr, health := setupSandboxRuntime(&config.Config{SandboxEnabled: false}, slog.Default())

	if mgr != nil {
		t.Fatal("manager = non-nil, want nil")
	}
	if health.Enabled {
		t.Fatal("health.Enabled = true, want false")
	}
	if health.RuntimeKind != string(sandbox.RuntimeKindUnavailable) {
		t.Fatalf("RuntimeKind = %q", health.RuntimeKind)
	}
	if health.Detail != "sandbox disabled" {
		t.Fatalf("Detail = %q", health.Detail)
	}
}

func TestSetupSandboxRuntime_ProcessRunnerEnablesCodeAndShell(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	workDir := t.TempDir()
	mgr, health := setupSandboxRuntime(&config.Config{
		SandboxEnabled:    true,
		SandboxTimeoutSec: 21,
		WorkspaceRoot:     workDir,
	}, slog.Default())

	if mgr == nil {
		t.Fatal("manager = nil, want configured manager")
	}
	if tools.NewExecuteCodeTool(mgr) == nil {
		t.Fatal("execute_code not registered with process runtime")
	}
	if tools.NewExecuteShellTool(mgr) == nil {
		t.Fatal("execute_shell not registered with process runtime")
	}
	if !health.Available {
		t.Fatalf("health.Available = false, detail=%q", health.Detail)
	}
	if health.RuntimeKind != string(sandbox.RuntimeKindProcess) {
		t.Fatalf("RuntimeKind = %q, want process", health.RuntimeKind)
	}
	if health.Runtime != workDir {
		t.Fatalf("Runtime = %q, want %q", health.Runtime, workDir)
	}
}

func TestShouldBootstrapPromptOverlayDefaults(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "container workspace",
			cfg:  &config.Config{PromptOverlayPath: "/workspace", WorkspaceRoot: "/workspace", RuntimeWorkspacePath: "/workspace"},
			want: true,
		},
		{
			name: "runtime workspace",
			cfg:  &config.Config{PromptOverlayPath: "./runtime-workspace", WorkspaceRoot: ".", RuntimeWorkspacePath: "./runtime-workspace"},
			want: true,
		},
		{
			name: "repo root default skipped",
			cfg:  &config.Config{PromptOverlayPath: ".", WorkspaceRoot: ".", RuntimeWorkspacePath: "./runtime-workspace"},
			want: false,
		},
		{
			name: "unrelated path skipped",
			cfg:  &config.Config{PromptOverlayPath: "D:/Aura", WorkspaceRoot: "./runtime-workspace", RuntimeWorkspacePath: "./runtime-workspace"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldBootstrapPromptOverlayDefaults(tt.cfg); got != tt.want {
				t.Fatalf("shouldBootstrapPromptOverlayDefaults() = %v, want %v", got, tt.want)
			}
		})
	}
}
