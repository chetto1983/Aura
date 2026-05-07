package main

import (
	"os"
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
		ToolCalls:    []string{"read_skill", "run_aurabot_swarm", "execute_code"},
		ToolProfile:  "sandbox_compute",
		SkillsRead:   true,
		SwarmUsed:    true,
		SandboxUsed:  true,
		ToolsExposed: []string{"read_skill", "run_aurabot_swarm", "execute_code"},
	}

	err := validateDebugExpectations(result, debugExpectations{
		Profile:     "sandbox_compute",
		Tools:       []string{"read_skill", "execute_code"},
		SkillRead:   true,
		SwarmUsed:   true,
		SandboxUsed: true,
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

func TestValidateDebugExpectationsRejectsConflictingToolExpectations(t *testing.T) {
	err := validateDebugExpectations(telegram.DebugTextSmokeResult{}, debugExpectations{
		Tools:   []string{"execute_code"},
		NoTools: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot combine") {
		t.Fatalf("validateDebugExpectations() error = %v, want conflict failure", err)
	}
}
