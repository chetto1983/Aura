package orchestration

import (
	"slices"
	"testing"
)

func TestSkillRequirementForCapabilityAdvisoryMode(t *testing.T) {
	tests := []struct {
		name       string
		capability Capability
		hints      []string
	}{
		{
			name:       "document generation suggests document skills",
			capability: CapabilityDocumentGeneration,
			hints:      []string{"documents:documents", "docx", "document-pdf", "xlsx"},
		},
		{
			name:       "source extraction suggests source extraction guidance",
			capability: CapabilitySourceExtraction,
			hints:      []string{"aura-source-extraction", "source extraction", "OCR"},
		},
		{
			name:       "sandbox compute suggests sandbox guidance",
			capability: CapabilitySandboxCompute,
			hints:      []string{"systematic-debugging", "test-driven-development", "sandbox compute"},
		},
		{
			name:       "memory read suggests memory audit guidance",
			capability: CapabilityMemoryRead,
			hints:      []string{"aura-memory-audit", "memory read evidence"},
		},
		{
			name:       "swarm research suggests bounded delegation guidance",
			capability: CapabilitySwarmResearch,
			hints:      []string{"subagent-driven-development", "swarm research", "bounded delegation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, ok := SkillRequirementForCapability(tt.capability, SkillPreflightAdvisory)
			if !ok {
				t.Fatalf("SkillRequirementForCapability(%q) ok = false", tt.capability)
			}
			if req.Capability != tt.capability {
				t.Fatalf("Capability = %q, want %q", req.Capability, tt.capability)
			}
			if req.Required {
				t.Fatalf("Required = true, want advisory-only requirement")
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
	if ok {
		t.Fatalf("blank/default mode should disable preflight guidance, got %+v", req)
	}

	req, ok = SkillRequirementForCapability(CapabilityDocumentGeneration, SkillPreflightRequired)
	if !ok {
		t.Fatal("legacy required mode should degrade to advisory guidance")
	}
	if req.Required {
		t.Fatal("legacy required mode must not hard-require preflight")
	}

	req, ok = SkillRequirementForCapability(CapabilityDocumentGeneration, SkillPreflightOff)
	if ok {
		t.Fatalf("off mode ok = true, requirement = %+v", req)
	}
}

func TestNeedsSkillPreflightIsAdvisoryOnly(t *testing.T) {
	missing := NeedsSkillPreflight(ProfileDocument, CapabilityDocumentGeneration, "create_pdf", SkillPreflightState{}, SkillPreflightRequired)
	if !missing.Applies {
		t.Fatalf("Applies = false for document generation")
	}
	if missing.Required {
		t.Fatalf("Required = true, want advisory-only decision")
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
	if satisfied.Required {
		t.Fatalf("Required = true after skill read, want advisory-only decision")
	}
	if !satisfied.Satisfied {
		t.Fatalf("Satisfied = false with document-pdf skill read: %+v", satisfied)
	}
	if satisfied.MatchedSkill != "document-pdf" {
		t.Fatalf("MatchedSkill = %q, want document-pdf", satisfied.MatchedSkill)
	}
}

func TestNeedsSkillPreflightInfersProfileAwareCapabilityWithoutHardGate(t *testing.T) {
	decision := NeedsSkillPreflight(ProfileDocument, "", "read_source", SkillPreflightState{}, SkillPreflightRequired)
	if !decision.Applies {
		t.Fatalf("Applies = false for inferred source extraction: %+v", decision)
	}
	if decision.Capability != CapabilitySourceExtraction {
		t.Fatalf("Capability = %q, want %q", decision.Capability, CapabilitySourceExtraction)
	}
	if decision.Required {
		t.Fatalf("Required = true for source extraction")
	}
	if decision.Satisfied {
		t.Fatalf("Satisfied = true without aura-source-extraction read: %+v", decision)
	}

	list := NeedsSkillPreflight(ProfileDocument, "", "list_sources", SkillPreflightState{}, SkillPreflightRequired)
	if list.Capability != CapabilityMemoryRead {
		t.Fatalf("Capability = %q, want memory_read for read-only source listing", list.Capability)
	}
	if list.Required {
		t.Fatalf("Required = true, want advisory/read-only list_sources preflight: %+v", list)
	}
}

func TestNeedsSkillPreflightOffMode(t *testing.T) {
	off := NeedsSkillPreflight(ProfileDocument, CapabilityDocumentGeneration, "create_pdf", SkillPreflightState{}, SkillPreflightOff)
	if off.Applies {
		t.Fatalf("off mode should disable preflight: %+v", off)
	}
	if !off.Satisfied {
		t.Fatalf("off mode should be satisfied by policy: %+v", off)
	}
}
