package orchestration

import (
	"slices"
	"testing"
)

func TestSkillRequirementForCapabilityInRequiredMode(t *testing.T) {
	tests := []struct {
		name       string
		capability Capability
		required   bool
		hints      []string
	}{
		{
			name:       "document generation requires document skills",
			capability: CapabilityDocumentGeneration,
			required:   true,
			hints:      []string{"documents:documents", "docx", "document-pdf", "xlsx"},
		},
		{
			name:       "source extraction requires source extraction guidance",
			capability: CapabilitySourceExtraction,
			required:   true,
			hints:      []string{"aura-source-extraction", "source extraction", "OCR"},
		},
		{
			name:       "sandbox compute requires sandbox guidance",
			capability: CapabilitySandboxCompute,
			required:   true,
			hints:      []string{"systematic-debugging", "test-driven-development", "sandbox compute"},
		},
		{
			name:       "browser e2e requires browser skill guidance",
			capability: CapabilityBrowserE2E,
			required:   true,
			hints:      []string{"browser-use", "aura-dashboard-e2e"},
		},
		{
			name:       "docker runtime requires docker skill guidance",
			capability: CapabilityDockerRuntime,
			required:   true,
			hints:      []string{"docker-compose-orchestration", "aura-release-docker"},
		},
		{
			name:       "security review requires security skill guidance",
			capability: CapabilitySecurityReview,
			required:   true,
			hints:      []string{"codex-security:security-scan", "security review", "MCP security"},
		},
		{
			name:       "release git requires release skill guidance",
			capability: CapabilityReleaseGit,
			required:   true,
			hints:      []string{"github:yeet", "aura-release-docker", "release git"},
		},
		{
			name:       "mcp plugin requires plugin review guidance",
			capability: CapabilityMCPPlugin,
			required:   true,
			hints:      []string{"aura-mcp-plugin-review", "MCP plugin review"},
		},
		{
			name:       "memory read is advisory",
			capability: CapabilityMemoryRead,
			required:   false,
			hints:      []string{"aura-memory-audit", "memory read evidence"},
		},
		{
			name:       "swarm research is advisory",
			capability: CapabilitySwarmResearch,
			required:   false,
			hints:      []string{"subagent-driven-development", "swarm research", "bounded delegation"},
		},
		{
			name:       "memory write reviewed requires memory review guidance",
			capability: CapabilityMemoryWriteReviewed,
			required:   true,
			hints:      []string{"aura-memory-audit", "review-gated memory write"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, ok := SkillRequirementForCapability(tt.capability, SkillPreflightRequired)
			if !ok {
				t.Fatalf("SkillRequirementForCapability(%q) ok = false", tt.capability)
			}
			if req.Capability != tt.capability {
				t.Fatalf("Capability = %q, want %q", req.Capability, tt.capability)
			}
			if req.Required != tt.required {
				t.Fatalf("Required = %v, want %v", req.Required, tt.required)
			}
			if req.FreshnessScope != SkillFreshnessTurn {
				t.Fatalf("FreshnessScope = %q, want %q", req.FreshnessScope, SkillFreshnessTurn)
			}
			if req.Reason == "" {
				t.Fatal("Reason is empty")
			}
			for _, hint := range tt.hints {
				if !slices.Contains(req.SkillHints, hint) {
					t.Fatalf("SkillHints missing %q: %+v", hint, req.SkillHints)
				}
			}
		})
	}
}

func TestSkillRequirementForCapabilityModes(t *testing.T) {
	req, ok := SkillRequirementForCapability(CapabilityDocumentGeneration, "")
	if !ok {
		t.Fatal("default mode should return document requirement")
	}
	if !req.Required {
		t.Fatal("default mode should be required")
	}

	req, ok = SkillRequirementForCapability(CapabilityDocumentGeneration, SkillPreflightAdvisory)
	if !ok {
		t.Fatal("advisory mode should return document requirement")
	}
	if req.Required {
		t.Fatal("advisory mode should make document preflight non-hard-required")
	}

	req, ok = SkillRequirementForCapability(CapabilityDocumentGeneration, SkillPreflightOff)
	if ok {
		t.Fatalf("off mode ok = true, requirement = %+v", req)
	}
}

func TestNeedsSkillPreflightReportsMissingAndSatisfiedTurnSkillReads(t *testing.T) {
	missing := NeedsSkillPreflight(ProfileDocument, CapabilityDocumentGeneration, "create_pdf", SkillPreflightState{}, SkillPreflightRequired)
	if !missing.Applies {
		t.Fatalf("Applies = false for document generation")
	}
	if !missing.Required {
		t.Fatalf("Required = false for missing document generation")
	}
	if missing.Satisfied {
		t.Fatalf("Satisfied = true without turn skill reads: %+v", missing)
	}
	if missing.MatchedSkill != "" {
		t.Fatalf("MatchedSkill = %q, want empty", missing.MatchedSkill)
	}

	satisfied := NeedsSkillPreflight(ProfileDocument, CapabilityDocumentGeneration, "create_pdf", SkillPreflightState{
		ReadSkillNames: []string{"document-pdf"},
	}, SkillPreflightRequired)
	if !satisfied.Applies {
		t.Fatalf("Applies = false for satisfied document generation")
	}
	if !satisfied.Required {
		t.Fatalf("Required = false for satisfied document generation")
	}
	if !satisfied.Satisfied {
		t.Fatalf("Satisfied = false with document-pdf skill read: %+v", satisfied)
	}
	if satisfied.MatchedSkill != "document-pdf" {
		t.Fatalf("MatchedSkill = %q, want document-pdf", satisfied.MatchedSkill)
	}
}

func TestNeedsSkillPreflightAcceptsSkillHintReadsForXLSXAndPDF(t *testing.T) {
	for _, skill := range []string{"xlsx", "document-pdf"} {
		decision := NeedsSkillPreflight(ProfileDocument, CapabilityDocumentGeneration, "create_xlsx", SkillPreflightState{
			ReadSkillNames: []string{skill},
		}, SkillPreflightRequired)
		if !decision.Satisfied {
			t.Fatalf("document preflight not satisfied by %q: %+v", skill, decision)
		}
	}
}

func TestNeedsSkillPreflightInfersProfileAwareSourceExtractionForDocumentTools(t *testing.T) {
	decision := NeedsSkillPreflight(ProfileDocument, "", "read_source", SkillPreflightState{}, SkillPreflightRequired)
	if !decision.Applies {
		t.Fatalf("Applies = false for inferred source extraction: %+v", decision)
	}
	if decision.Capability != CapabilitySourceExtraction {
		t.Fatalf("Capability = %q, want %q", decision.Capability, CapabilitySourceExtraction)
	}
	if !decision.Required {
		t.Fatalf("Required = false for source extraction")
	}
	if decision.Satisfied {
		t.Fatalf("Satisfied = true without aura-source-extraction read: %+v", decision)
	}

	for _, profile := range []Profile{ProfileDocument, ProfileSandboxCompute} {
		for _, tool := range []string{"read_source", "list_sources"} {
			decision := NeedsSkillPreflight(profile, CapabilityMemoryRead, tool, SkillPreflightState{}, SkillPreflightRequired)
			if decision.Capability != CapabilitySourceExtraction {
				t.Fatalf("NeedsSkillPreflight(%q, memory_read, %q) Capability = %q, want source_extraction", profile, tool, decision.Capability)
			}
			if !decision.Required {
				t.Fatalf("NeedsSkillPreflight(%q, memory_read, %q) Required = false, want true", profile, tool)
			}
		}
	}
}

func TestNeedsSkillPreflightSourceExtractionSatisfiedByAuraSourceExtractionSkill(t *testing.T) {
	decision := NeedsSkillPreflight(ProfileDocument, "", "read_source", SkillPreflightState{
		ReadSkillNames: []string{"aura-source-extraction"},
	}, SkillPreflightRequired)
	if !decision.Applies {
		t.Fatalf("Applies = false for inferred source extraction: %+v", decision)
	}
	if decision.Capability != CapabilitySourceExtraction {
		t.Fatalf("Capability = %q, want %q", decision.Capability, CapabilitySourceExtraction)
	}
	if !decision.Required {
		t.Fatalf("Required = false for source extraction")
	}
	if !decision.Satisfied {
		t.Fatalf("Satisfied = false with aura-source-extraction read: %+v", decision)
	}
	if decision.MatchedSkill != "aura-source-extraction" {
		t.Fatalf("MatchedSkill = %q, want aura-source-extraction", decision.MatchedSkill)
	}
}

func TestNeedsSkillPreflightInfersAdminReviewWriteReviewBeforeSecurityReview(t *testing.T) {
	decision := NeedsSkillPreflight(ProfileAdminReview, "", "propose_wiki_change", SkillPreflightState{}, SkillPreflightRequired)
	if !decision.Applies {
		t.Fatalf("Applies = false for inferred admin review write: %+v", decision)
	}
	if decision.Capability != CapabilityMemoryWriteReviewed {
		t.Fatalf("Capability = %q, want %q", decision.Capability, CapabilityMemoryWriteReviewed)
	}
	if !decision.Required {
		t.Fatalf("Required = false for memory write review")
	}
}

func TestNeedsSkillPreflightAdvisoryAndOffMode(t *testing.T) {
	advisory := NeedsSkillPreflight(ProfileMemory, CapabilityMemoryRead, "read_wiki", SkillPreflightState{}, SkillPreflightRequired)
	if !advisory.Applies {
		t.Fatal("memory read advisory preflight should apply")
	}
	if advisory.Required {
		t.Fatal("memory read should be advisory in required mode")
	}
	if advisory.Satisfied {
		t.Fatal("memory read without skill reads should not be satisfied")
	}

	swarm := NeedsSkillPreflight(ProfileSwarmResearch, CapabilitySwarmResearch, "run_aurabot_swarm", SkillPreflightState{}, SkillPreflightRequired)
	if !swarm.Applies {
		t.Fatal("swarm research advisory preflight should apply")
	}
	if swarm.Required {
		t.Fatal("swarm research should be advisory in required mode")
	}

	off := NeedsSkillPreflight(ProfileDocument, CapabilityDocumentGeneration, "create_pdf", SkillPreflightState{}, SkillPreflightOff)
	if off.Applies {
		t.Fatalf("off mode should disable preflight: %+v", off)
	}
	if !off.Satisfied {
		t.Fatalf("off mode should be satisfied by policy: %+v", off)
	}
}
