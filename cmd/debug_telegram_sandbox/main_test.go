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
		ArtifactFilenames:        []string{"aura_artifact.txt"},
		DocumentSends: []telegram.DebugDocumentSend{{
			Filename:  "aura_artifact.txt",
			Caption:   "Aura sandbox artifact: aura_artifact.txt",
			SizeBytes: 30,
		}},
	}

	if err := validateTelegramSandboxSmoke(result, true); err != nil {
		t.Fatalf("validateTelegramSandboxSmoke() error = %v", err)
	}
}

func TestTelegramSandboxSmokeReportRejectsArtifactSmokeWithoutDocument(t *testing.T) {
	result := telegram.DebugTextSmokeResult{
		CalledExecuteCode:        true,
		ContainsArtifactMetadata: true,
		ArtifactFilenames:        []string{"aura_artifact.txt"},
	}

	err := validateTelegramSandboxSmoke(result, true)
	if err == nil || !strings.Contains(err.Error(), "document") {
		t.Fatalf("validateTelegramSandboxSmoke() error = %v, want document failure", err)
	}
}
