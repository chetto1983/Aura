package orchestration

import "strings"

// SkillPreflightMode controls how strongly Aura requires skill reads before
// capability-specific tools. The default is required.
type SkillPreflightMode string

const (
	SkillPreflightRequired SkillPreflightMode = "required"
	SkillPreflightAdvisory SkillPreflightMode = "advisory"
	SkillPreflightOff      SkillPreflightMode = "off"

	SkillFreshnessTurn = "turn"
)

// SkillRequirement describes the skill guidance Aura expects before a
// capability is used in the current turn.
type SkillRequirement struct {
	Capability        Capability
	Required          bool
	AllowedSkillNames []string
	SkillHints        []string
	Reason            string
	FreshnessScope    string
}

// SkillPreflightState is the pure per-turn state needed to evaluate preflight.
type SkillPreflightState struct {
	ReadSkillNames []string
}

// SkillPreflightDecision is a pure policy result; live-loop enforcement can
// decide later whether a missing required result becomes a tool error.
type SkillPreflightDecision struct {
	Applies      bool
	Required     bool
	Satisfied    bool
	Mode         SkillPreflightMode
	Profile      Profile
	Capability   Capability
	CalledTool   string
	Requirement  SkillRequirement
	MatchedSkill string
	Reason       string
}

var hardSkillPreflightCapabilities = map[Capability]bool{
	CapabilitySourceExtraction:   true,
	CapabilityDocumentGeneration: true,
	CapabilitySandboxCompute:     true,
	CapabilityBrowserE2E:         true,
	CapabilityDockerRuntime:      true,
	CapabilitySecurityReview:     true,
	CapabilityReleaseGit:         true,
	CapabilityMCPPlugin:          true,
}

// SkillRequirementForCapability returns the skill preflight requirement for a
// capability. Off mode disables requirements entirely.
func SkillRequirementForCapability(capability Capability, mode SkillPreflightMode) (SkillRequirement, bool) {
	mode = normalizeSkillPreflightMode(mode)
	if mode == SkillPreflightOff {
		return SkillRequirement{}, false
	}

	def, ok := CapabilityDefinitionFor(capability)
	if !ok {
		return SkillRequirement{}, false
	}

	required := hardSkillPreflightCapabilities[capability]
	if mode == SkillPreflightAdvisory {
		required = false
	}

	hints := append([]string(nil), def.SkillHints...)
	return SkillRequirement{
		Capability:        capability,
		Required:          required,
		AllowedSkillNames: skillNamesFromHints(hints),
		SkillHints:        hints,
		Reason:            skillRequirementReason(capability, required),
		FreshnessScope:    SkillFreshnessTurn,
	}, true
}

// NeedsSkillPreflight evaluates whether the current turn has satisfied skill
// preflight for the capability/tool pair.
func NeedsSkillPreflight(profile Profile, capability Capability, calledTool string, state SkillPreflightState, mode SkillPreflightMode) SkillPreflightDecision {
	mode = normalizeSkillPreflightMode(mode)
	if strings.TrimSpace(calledTool) != "" {
		if inferred, ok := inferCapabilityForToolProfile(calledTool, profile); ok {
			capability = inferred
		}
	}

	req, ok := SkillRequirementForCapability(capability, mode)
	if !ok {
		return SkillPreflightDecision{
			Applies:    false,
			Required:   false,
			Satisfied:  true,
			Mode:       mode,
			Profile:    profile,
			Capability: capability,
			CalledTool: calledTool,
			Reason:     "skill preflight disabled or no requirement for capability",
		}
	}

	matched := matchingReadSkill(state.ReadSkillNames, req)
	satisfied := matched != ""
	return SkillPreflightDecision{
		Applies:      true,
		Required:     req.Required,
		Satisfied:    satisfied,
		Mode:         mode,
		Profile:      normalizeProfile(string(profile)),
		Capability:   req.Capability,
		CalledTool:   calledTool,
		Requirement:  req,
		MatchedSkill: matched,
		Reason:       req.Reason,
	}
}

func inferCapabilityForToolProfile(tool string, profile Profile) (Capability, bool) {
	capabilities := CapabilitiesForTool(tool)
	if len(capabilities) == 0 {
		return "", false
	}

	profile = normalizeProfile(string(profile))
	profileMatches := make([]Capability, 0, len(capabilities))
	for _, capability := range capabilities {
		if capabilitySupportsProfile(capability, profile) {
			profileMatches = append(profileMatches, capability)
		}
	}
	if len(profileMatches) == 0 {
		return capabilities[0], true
	}

	if profile == ProfileDocument || profile == ProfileSandboxCompute {
		if tool != "list_sources" && containsCapability(profileMatches, CapabilitySourceExtraction) {
			return CapabilitySourceExtraction, true
		}
	}

	// Admin review propose_* tools are both memory mutations and security-sensitive.
	// Treat the review-gated write as the primary preflight; security remains a
	// secondary review capability that callers can request explicitly.
	if profile == ProfileAdminReview {
		if containsCapability(profileMatches, CapabilityMemoryWriteReviewed) {
			return CapabilityMemoryWriteReviewed, true
		}
	}

	return profileMatches[0], true
}

func capabilitySupportsProfile(capability Capability, profile Profile) bool {
	def, ok := CapabilityDefinitionFor(capability)
	if !ok {
		return false
	}
	for _, candidate := range def.Profiles {
		if candidate == profile {
			return true
		}
	}
	return false
}

func containsCapability(capabilities []Capability, capability Capability) bool {
	for _, candidate := range capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

func normalizeSkillPreflightMode(mode SkillPreflightMode) SkillPreflightMode {
	switch SkillPreflightMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case SkillPreflightAdvisory:
		return SkillPreflightAdvisory
	case SkillPreflightOff:
		return SkillPreflightOff
	default:
		return SkillPreflightRequired
	}
}

func skillNamesFromHints(hints []string) []string {
	names := make([]string, 0, len(hints))
	for _, hint := range hints {
		hint = strings.TrimSpace(hint)
		if hint == "" || strings.Contains(hint, " ") {
			continue
		}
		names = append(names, hint)
	}
	return names
}

func matchingReadSkill(readSkillNames []string, req SkillRequirement) string {
	allowed := append([]string(nil), req.AllowedSkillNames...)
	allowed = append(allowed, req.SkillHints...)
	for _, read := range readSkillNames {
		readNorm := normalizeSkillMatchValue(read)
		if readNorm == "" {
			continue
		}
		for _, candidate := range allowed {
			candidateNorm := normalizeSkillMatchValue(candidate)
			if candidateNorm != "" && readNorm == candidateNorm {
				return strings.TrimSpace(read)
			}
		}
	}
	return ""
}

func normalizeSkillMatchValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func skillRequirementReason(capability Capability, required bool) string {
	if required {
		return "read an applicable skill before using " + string(capability) + " tools"
	}
	return "read an applicable skill when useful before using " + string(capability) + " tools"
}
