package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/ctxmetrics"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/sandbox"
	runstore "github.com/aura/aura/internal/storage/runs"
)

func TestAuthStoreImplementsDelegationInterfaces(t *testing.T) {
	var _ identity.Delegator = (*auth.Store)(nil)
}

func TestChatHubLifecycleStoreWiring(t *testing.T) {
	var _ chat.LifecycleStore = (*runstore.Store)(nil)
	var _ identity.AuthorizationDenialRecorder = (*runstore.Store)(nil)
}

func TestSkillSearchRootsIncludeSkillsPathCatalogLayouts(t *testing.T) {
	cfg := &config.Config{
		SkillsPath:              filepath.Join("data", "skills"),
		SkillsInstallProjectDir: "",
	}

	roots := skillSearchRoots(cfg)
	for _, want := range []string{
		filepath.Join("data", "skills"),
		".agents/skills",
		".claude/skills",
		filepath.Join("data", "skills", ".agents", "skills"),
		filepath.Join("data", "skills", ".claude", "skills"),
		filepath.Join(".", ".agents", "skills"),
		filepath.Join(".", ".claude", "skills"),
	} {
		if !helpersContains(roots, want) {
			t.Fatalf("roots missing %q: %+v", want, roots)
		}
	}
}

func TestPopulateMaxConversationTokensUpdatesCTXMetricsGauges(t *testing.T) {
	oldGauges := ctxmetrics.GlobalGauges
	ctxmetrics.GlobalGauges = &ctxmetrics.Gauges{}
	t.Cleanup(func() { ctxmetrics.GlobalGauges = oldGauges })

	cfg := &config.Config{
		MaxHistoryMessages: 50,
		ModelContextWindow: 200000,
		CTXCompactPercent:  0.5,
		CTXCompactScope:    "total",
	}
	populateMaxConversationTokens(cfg, slog.Default())

	if cfg.MaxConversationTokens != 100000 {
		t.Fatalf("MaxConversationTokens = %d, want 100000", cfg.MaxConversationTokens)
	}
	if ctxmetrics.GlobalGauges.ModelContextWindow != 200000 {
		t.Fatalf("ModelContextWindow gauge = %d, want 200000", ctxmetrics.GlobalGauges.ModelContextWindow)
	}
	if ctxmetrics.GlobalGauges.CompactionThresholdTokens != 100000 {
		t.Fatalf("CompactionThresholdTokens gauge = %d, want 100000", ctxmetrics.GlobalGauges.CompactionThresholdTokens)
	}
}

func TestPopulateMaxConversationTokensLegacyPathUpdatesCTXMetricsGauges(t *testing.T) {
	oldGauges := ctxmetrics.GlobalGauges
	ctxmetrics.GlobalGauges = &ctxmetrics.Gauges{}
	t.Cleanup(func() { ctxmetrics.GlobalGauges = oldGauges })

	cfg := &config.Config{
		MaxHistoryMessages: 8,
		ModelContextWindow: 123456,
		CTXCompactPercent:  0,
	}
	populateMaxConversationTokens(cfg, slog.Default())

	if cfg.MaxConversationTokens != 4000 {
		t.Fatalf("MaxConversationTokens = %d, want 4000", cfg.MaxConversationTokens)
	}
	if ctxmetrics.GlobalGauges.ModelContextWindow != 123456 {
		t.Fatalf("ModelContextWindow gauge = %d, want 123456", ctxmetrics.GlobalGauges.ModelContextWindow)
	}
	if ctxmetrics.GlobalGauges.CompactionThresholdTokens != 4000 {
		t.Fatalf("CompactionThresholdTokens gauge = %d, want 4000", ctxmetrics.GlobalGauges.CompactionThresholdTokens)
	}
}

func helpersContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

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
	if tools.NewExecuteCodeToolWithStoreAndRegistry(mgr, nil, nil, nil) == nil {
		t.Fatal("execute_code not registered with process runtime")
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

func TestCheckEmbedSidecarNCtx_PassesWhenAboveMin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"default_generation_settings":{"n_ctx":2048}}`)
	}))
	defer srv.Close()

	if err := checkEmbedSidecarNCtx(context.Background(), srv.URL+"/v1", 2048, slog.Default()); err != nil {
		t.Fatalf("expected nil for n_ctx=2048 >= 2048, got %v", err)
	}
}

func TestCheckEmbedSidecarNCtx_FailsWhenBelowMin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"default_generation_settings":{"n_ctx":512}}`)
	}))
	defer srv.Close()

	if err := checkEmbedSidecarNCtx(context.Background(), srv.URL+"/v1", 2048, slog.Default()); err == nil {
		t.Fatal("expected error for n_ctx=512 < 2048, got nil")
	}
}

func TestCheckEmbedSidecarNCtx_SkipsWhenUnreachable(t *testing.T) {
	// Use a closed server to simulate an unreachable endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	srv.Close()

	if err := checkEmbedSidecarNCtx(context.Background(), srv.URL+"/v1", 2048, slog.Default()); err != nil {
		t.Fatalf("expected nil for unreachable endpoint, got %v", err)
	}
}

func TestCheckEmbedSidecarNCtx_SkipsWhenNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if err := checkEmbedSidecarNCtx(context.Background(), srv.URL+"/v1", 2048, slog.Default()); err != nil {
		t.Fatalf("expected nil for 404 response, got %v", err)
	}
}

func TestCheckEmbedSidecarNCtx_SkipsWhenEmptyURL(t *testing.T) {
	if err := checkEmbedSidecarNCtx(context.Background(), "", 2048, slog.Default()); err != nil {
		t.Fatalf("expected nil for empty URL, got %v", err)
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
