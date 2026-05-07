package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/telegram"
)

func TestMainRunsMigrationsBeforeSharedStoreConstruction(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	openIdx := strings.Index(source, "auradb.Open(cfg.DBPath)")
	migrateIdx := strings.Index(source, "migrations.Run(context.Background(), pool)")
	settingsIdx := strings.Index(source, "settings.NewStoreWithDB(pool)")
	telegramIdx := strings.Index(source, "telegram.New(")

	if openIdx < 0 || migrateIdx < 0 || settingsIdx < 0 || telegramIdx < 0 {
		t.Fatalf("startup markers missing: open=%d migrate=%d settings=%d telegram=%d", openIdx, migrateIdx, settingsIdx, telegramIdx)
	}
	if !(openIdx < migrateIdx && migrateIdx < settingsIdx && settingsIdx < telegramIdx) {
		t.Fatalf("startup order invalid: open=%d migrate=%d settings=%d telegram=%d", openIdx, migrateIdx, settingsIdx, telegramIdx)
	}
}

func TestResolveDebugDBPathUsesEnvFileDirectory(t *testing.T) {
	envPath := filepath.Join("data", ".env")
	got := resolveDebugDBPath(envPath, filepath.Join(".", "aura.db"))
	want := filepath.Join("data", "aura.db")
	if got != want {
		t.Fatalf("resolveDebugDBPath() = %q, want %q", got, want)
	}
}

func TestResolveDebugDBPathLeavesAbsolutePathUnchanged(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "aura.db")
	got := resolveDebugDBPath(filepath.Join("data", ".env"), dbPath)
	if got != filepath.Clean(dbPath) {
		t.Fatalf("resolveDebugDBPath() = %q, want %q", got, filepath.Clean(dbPath))
	}
}

func TestPrepareDebugDBCopyCopiesDatabaseAndSidecars(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "aura.db")
	if err := os.WriteFile(source, []byte("main"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(source+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatalf("write wal: %v", err)
	}
	if err := os.WriteFile(source+"-shm", []byte("shm"), 0o600); err != nil {
		t.Fatalf("write shm: %v", err)
	}

	got, cleanup, err := prepareDebugDBCopy(source)
	if err != nil {
		t.Fatalf("prepareDebugDBCopy() error = %v", err)
	}
	defer cleanup()
	if got == source {
		t.Fatalf("prepareDebugDBCopy() returned live path")
	}
	assertFileContent(t, got, "main")
	assertFileContent(t, got+"-wal", "wal")
	assertFileContent(t, got+"-shm", "shm")
}

func TestResolveRuntimeDBPathKeepsTempCopyAfterSettingsApply(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "aura.db")
	liveDB := filepath.Join(t.TempDir(), "live.db")

	got := resolveRuntimeDBPath(tempDB, liveDB, false)
	if got != filepath.Clean(tempDB) {
		t.Fatalf("resolveRuntimeDBPath() = %q, want temp DB %q", got, filepath.Clean(tempDB))
	}
}

func TestResolveRuntimeDBPathAllowsLiveDBWhenExplicit(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "aura.db")
	liveDB := filepath.Join(t.TempDir(), "live.db")

	got := resolveRuntimeDBPath(tempDB, liveDB, true)
	if got != filepath.Clean(liveDB) {
		t.Fatalf("resolveRuntimeDBPath() = %q, want live DB %q", got, filepath.Clean(liveDB))
	}
}

func TestTelegramSandboxSmokeReportPassesArtifactSmoke(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		CalledExecuteCode:        true,
		ContainsArtifactMetadata: true,
		ArtifactFilenames:        []string{"aura_sales_summary.csv", "aura_sales_plot.png"},
		ArtifactSourceIDs:        []string{"src_0123456789abcdef", "src_fedcba9876543210"},
		DocumentSends: []telegram.DebugDocumentSend{
			{
				Filename:  "aura_sales_summary.csv",
				Caption:   "Aura sandbox artifact: aura_sales_summary.csv",
				SizeBytes: 42,
			},
			{
				Filename:  "aura_sales_plot.png",
				Caption:   "Aura sandbox artifact: aura_sales_plot.png",
				SizeBytes: 2048,
			},
		},
	}

	if err := validateTelegramSandboxSmoke(result, true); err != nil {
		t.Fatalf("validateTelegramSandboxSmoke() error = %v", err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("content of %s = %q, want %q", path, got, want)
	}
}

func TestTelegramSandboxSmokeReportRejectsArtifactSmokeWithoutRichArtifacts(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		CalledExecuteCode:        true,
		ContainsArtifactMetadata: true,
		ArtifactFilenames:        []string{"aura_artifact.txt"},
		ArtifactSourceIDs:        []string{"src_0123456789abcdef"},
		DocumentSends: []telegram.DebugDocumentSend{{
			Filename:  "aura_artifact.txt",
			Caption:   "Aura sandbox artifact: aura_artifact.txt",
			SizeBytes: 30,
		}},
	}

	err := validateTelegramSandboxSmoke(result, true)
	if err == nil || !strings.Contains(err.Error(), "rich artifact") {
		t.Fatalf("validateTelegramSandboxSmoke() error = %v, want rich artifact failure", err)
	}
}

func TestTelegramSandboxSmokeReportRejectsArtifactSmokeWithoutDocument(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		CalledExecuteCode:        true,
		ContainsArtifactMetadata: true,
		ArtifactFilenames:        []string{"aura_sales_summary.csv", "aura_sales_plot.png"},
		ArtifactSourceIDs:        []string{"src_0123456789abcdef", "src_fedcba9876543210"},
	}

	err := validateTelegramSandboxSmoke(result, true)
	if err == nil || !strings.Contains(err.Error(), "document") {
		t.Fatalf("validateTelegramSandboxSmoke() error = %v, want document failure", err)
	}
}

func TestTelegramSandboxSmokeReportRejectsArtifactSmokeWithoutSource(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		CalledExecuteCode:        true,
		ContainsArtifactMetadata: true,
		ArtifactFilenames:        []string{"aura_sales_summary.csv", "aura_sales_plot.png"},
		DocumentSends: []telegram.DebugDocumentSend{{
			Filename:  "aura_sales_summary.csv",
			Caption:   "Aura sandbox artifact: aura_sales_summary.csv",
			SizeBytes: 30,
		}, {
			Filename:  "aura_sales_plot.png",
			Caption:   "Aura sandbox artifact: aura_sales_plot.png",
			SizeBytes: 2048,
		}},
	}

	err := validateTelegramSandboxSmoke(result, true)
	if err == nil || !strings.Contains(err.Error(), "source persistence") {
		t.Fatalf("validateTelegramSandboxSmoke() error = %v, want source persistence failure", err)
	}
}

func TestValidateDebugExpectationsAcceptsMatchedOrchestrationSignals(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		ToolCalls:          []string{"read_skill", "run_aurabot_swarm", "execute_code"},
		ToolProfile:        "sandbox_compute",
		FinalText:          "Swarm research completed.",
		SkillsRead:         true,
		SwarmUsed:          true,
		TerminalSwarm:      true,
		SwarmFinalization:  "aggregate",
		WorkerCount:        2,
		WorkerFailures:     0,
		TokenUsageReported: true,
		TokensTotal:        10,
		ElapsedMS:          100,
		SandboxUsed:        true,
		ToolsExposed:       []string{"read_skill", "run_aurabot_swarm", "execute_code"},
	}

	err := validateDebugExpectations(result, debugExpectations{
		Profile:       "sandbox_compute",
		Tools:         []string{"read_skill", "execute_code"},
		SkillRead:     true,
		SwarmUsed:     true,
		TerminalSwarm: true,
		TokenMetrics:  true,
		SandboxUsed:   true,
		MaxElapsedMS:  1000,
	})
	if err != nil {
		t.Fatalf("validateDebugExpectations() error = %v", err)
	}
}

func TestValidateDebugExpectationsRejectsMismatchedProfile(t *testing.T) {
	result := telegram.DebugTextSmokeResult{ToolProfile: "default"}

	err := validateDebugExpectations(result, debugExpectations{Profile: "sandbox_compute"})
	if err == nil || !strings.Contains(err.Error(), `expected profile "sandbox_compute"`) {
		t.Fatalf("validateDebugExpectations() error = %v, want profile failure", err)
	}
}

func TestValidateDebugExpectationsRejectsUnexpectedTools(t *testing.T) {
	result := telegram.DebugTextSmokeResult{ToolCalls: []string{"search_memory"}}

	err := validateDebugExpectations(result, debugExpectations{NoTools: true})
	if err == nil || !strings.Contains(err.Error(), "expected no tool calls") {
		t.Fatalf("validateDebugExpectations() error = %v, want no-tools failure", err)
	}
}

func TestValidateDebugExpectationsRejectsMissingUsageSignals(t *testing.T) {
	tests := []struct {
		name string
		want debugExpectations
		msg  string
	}{
		{name: "skill", want: debugExpectations{SkillRead: true}, msg: "expected read_skill"},
		{name: "swarm", want: debugExpectations{SwarmUsed: true}, msg: "expected swarm usage"},
		{name: "terminal swarm", want: debugExpectations{TerminalSwarm: true}, msg: "expected terminal swarm"},
		{name: "token metrics", want: debugExpectations{TokenMetrics: true}, msg: "expected token usage metrics"},
		{name: "sandbox", want: debugExpectations{SandboxUsed: true}, msg: "expected sandbox usage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDebugExpectations(telegram.DebugTextSmokeResult{}, tt.want)
			if err == nil || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("validateDebugExpectations() error = %v, want %q", err, tt.msg)
			}
		})
	}
}

func TestValidateDebugExpectationsRejectsSlowRun(t *testing.T) {
	result := telegram.DebugTextSmokeResult{ElapsedMS: 2000}

	err := validateDebugExpectations(result, debugExpectations{MaxElapsedMS: 1000})
	if err == nil || !strings.Contains(err.Error(), "exceeds budget") {
		t.Fatalf("validateDebugExpectations() error = %v, want elapsed failure", err)
	}
}

func TestValidateDebugExpectationsRejectsTerminalSwarmWorkerFailures(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		TerminalSwarm:     true,
		SwarmFinalization: "aggregate",
		FinalText:         "Swarm research completed.",
		WorkerCount:       2,
		WorkerFailures:    1,
	}

	err := validateDebugExpectations(result, debugExpectations{TerminalSwarm: true})
	if err == nil || !strings.Contains(err.Error(), "worker_failures=0") {
		t.Fatalf("validateDebugExpectations() error = %v, want worker failure", err)
	}
}

func TestValidateDebugExpectationsRejectsTerminalSwarmWithoutWorkers(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		TerminalSwarm:     true,
		SwarmFinalization: "aggregate",
		FinalText:         "Swarm research completed.",
	}

	err := validateDebugExpectations(result, debugExpectations{TerminalSwarm: true})
	if err == nil || !strings.Contains(err.Error(), "worker_count >= 1") {
		t.Fatalf("validateDebugExpectations() error = %v, want worker count failure", err)
	}
}

func TestValidateDebugExpectationsRejectsTerminalSwarmWithoutFinalText(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		TerminalSwarm:     true,
		SwarmFinalization: "aggregate",
		WorkerCount:       1,
	}

	err := validateDebugExpectations(result, debugExpectations{TerminalSwarm: true})
	if err == nil || !strings.Contains(err.Error(), "final text") {
		t.Fatalf("validateDebugExpectations() error = %v, want final text failure", err)
	}
}

func TestValidateDebugExpectationsRejectsTerminalSwarmWithoutKnownFinalization(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		TerminalSwarm: true,
		FinalText:     "Swarm research completed.",
		WorkerCount:   1,
	}

	err := validateDebugExpectations(result, debugExpectations{TerminalSwarm: true})
	if err == nil || !strings.Contains(err.Error(), "known swarm finalization") {
		t.Fatalf("validateDebugExpectations() error = %v, want finalization failure", err)
	}
}

func TestValidateDebugExpectationsRejectsConflictingToolExpectations(t *testing.T) {
	err := validateDebugExpectations(telegram.DebugTextSmokeResult{}, debugExpectations{
		Tools:   []string{"execute_code"},
		NoTools: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot combine") {
		t.Fatalf("validateDebugExpectations() error = %v, want conflict failure", err)
	}
}
