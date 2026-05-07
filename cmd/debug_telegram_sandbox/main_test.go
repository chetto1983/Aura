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
		PromptHash:         "abc123",
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
		HiddenToolRejected: true,
		LoopSteps:          3,
		TerminalTool:       "execute_code",
		ReadSkills:         []string{"test-driven-development"},
		ActiveCapabilities: []string{"sandbox_compute"},
		ToolsExposed:       []string{"read_skill", "run_aurabot_swarm", "execute_code"},
	}

	err := validateDebugExpectations(result, debugExpectations{
		Profile:            "sandbox_compute",
		Tools:              []string{"read_skill", "execute_code"},
		SkillRead:          true,
		SkillReadNames:     []string{"test-driven-development", "sandbox_compute"},
		SwarmUsed:          true,
		TerminalSwarm:      true,
		TokenMetrics:       true,
		SandboxUsed:        true,
		HiddenToolRejected: true,
		TerminalTool:       "execute_code",
		MaxLoopSteps:       3,
		TraceFields:        []string{"PromptHash", "ToolProfile", "ReadSkills", "ActiveCapabilities"},
		NoStaleSkillRef:    true,
		MaxElapsedMS:       1000,
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

func TestValidateDebugExpectationsRejectsMissingNamedSkillRead(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		SkillsRead: true,
		ReadSkills: []string{"document-pdf"},
	}

	err := validateDebugExpectations(result, debugExpectations{SkillReadNames: []string{"docx"}})
	if err == nil || !strings.Contains(err.Error(), `expected read_skill for "docx"`) {
		t.Fatalf("validateDebugExpectations() error = %v, want named skill failure", err)
	}
}

func TestValidateDebugExpectationsAcceptsCapabilitySkillRead(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		SkillsRead:         true,
		ReadSkills:         []string{"document-pdf"},
		ActiveCapabilities: []string{"document_generation"},
	}

	err := validateDebugExpectations(result, debugExpectations{SkillReadNames: []string{"document_generation"}})
	if err != nil {
		t.Fatalf("validateDebugExpectations() error = %v", err)
	}
}

func TestOptionalCSVFlagPreservesBooleanAndNamedSkillModes(t *testing.T) {
	var f optionalCSVFlag
	if err := f.Set("true"); err != nil {
		t.Fatal(err)
	}
	if !f.Any || len(f.Values) != 0 {
		t.Fatalf("bool flag = any:%v values:%v", f.Any, f.Values)
	}
	if err := f.Set("docx, sandbox_compute"); err != nil {
		t.Fatal(err)
	}
	if !f.Any || !hasAll(f.Values, "docx", "sandbox_compute") {
		t.Fatalf("named flag = any:%v values:%v", f.Any, f.Values)
	}
}

func TestValidateDebugExpectationsRejectsSlowRun(t *testing.T) {
	result := telegram.DebugTextSmokeResult{ElapsedMS: 2000}

	err := validateDebugExpectations(result, debugExpectations{MaxElapsedMS: 1000})
	if err == nil || !strings.Contains(err.Error(), "exceeds budget") {
		t.Fatalf("validateDebugExpectations() error = %v, want elapsed failure", err)
	}
}

func TestValidateDebugExpectationsRejectsMissingHiddenToolRejection(t *testing.T) {
	err := validateDebugExpectations(telegram.DebugTextSmokeResult{}, debugExpectations{HiddenToolRejected: true})
	if err == nil || !strings.Contains(err.Error(), "hidden tool rejection") {
		t.Fatalf("validateDebugExpectations() error = %v, want hidden tool rejection failure", err)
	}
}

func TestValidateDebugExpectationsRejectsWrongTerminalTool(t *testing.T) {
	result := telegram.DebugTextSmokeResult{TerminalTool: "run_aurabot_swarm"}

	err := validateDebugExpectations(result, debugExpectations{TerminalTool: "execute_code"})
	if err == nil || !strings.Contains(err.Error(), `expected terminal tool "execute_code"`) {
		t.Fatalf("validateDebugExpectations() error = %v, want terminal tool failure", err)
	}
}

func TestValidateDebugExpectationsRejectsLoopStepOverBudget(t *testing.T) {
	result := telegram.DebugTextSmokeResult{LoopSteps: 7}

	err := validateDebugExpectations(result, debugExpectations{MaxLoopSteps: 6})
	if err == nil || !strings.Contains(err.Error(), "loop_steps 7 exceeds budget 6") {
		t.Fatalf("validateDebugExpectations() error = %v, want loop step budget failure", err)
	}
}

func TestValidateDebugExpectationsRejectsMissingTraceField(t *testing.T) {
	result := telegram.DebugTextSmokeResult{ToolProfile: "sandbox_compute"}

	err := validateDebugExpectations(result, debugExpectations{TraceFields: []string{"ToolProfile", "PromptHash"}})
	if err == nil || !strings.Contains(err.Error(), `expected trace/result field "PromptHash"`) {
		t.Fatalf("validateDebugExpectations() error = %v, want trace field failure", err)
	}
}

func TestValidateDebugExpectationsRejectsUnknownTraceField(t *testing.T) {
	err := validateDebugExpectations(telegram.DebugTextSmokeResult{}, debugExpectations{TraceFields: []string{"NoSuchField"}})
	if err == nil || !strings.Contains(err.Error(), `unknown trace/result field "NoSuchField"`) {
		t.Fatalf("validateDebugExpectations() error = %v, want unknown trace field failure", err)
	}
}

func TestValidateDebugExpectationsRejectsStaleSkillReferences(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		ReadSkills: []string{"golem.yaml"},
		ToolCalls:  []string{"run_aurabot_worker"},
	}

	err := validateDebugExpectations(result, debugExpectations{NoStaleSkillRef: true})
	if err == nil || !strings.Contains(err.Error(), "stale skill/reference") {
		t.Fatalf("validateDebugExpectations() error = %v, want stale reference failure", err)
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

func TestRedactForReportRemovesKnownSecretValues(t *testing.T) {
	input := `LLM_API_KEY=sk-live TELEGRAM_BOT_TOKEN=123:abc Authorization: Bearer dashboard-token final Bearer inline-token raw sk-rawsecret`

	got := redactForReport(input)
	for _, secret := range []string{"sk-live", "123:abc", "dashboard-token", "inline-token", "sk-rawsecret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redactForReport() leaked %q in %q", secret, got)
		}
	}
	if strings.Count(got, "[REDACTED]")+strings.Count(got, "[redacted]") != 5 {
		t.Fatalf("redactForReport() = %q, want five redactions", got)
	}
}
