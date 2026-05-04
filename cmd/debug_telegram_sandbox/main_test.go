package main

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/telegram"
)

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
