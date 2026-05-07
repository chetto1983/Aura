package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/orchestration"
)

type reportInput struct {
	Prompt        string
	Mode          string
	Swarm         bool
	Sandbox       bool
	Proposals     bool
	PromptVersion string
	Overlay       string
	SkillsBlock   string
}

type report struct {
	Prompt                 string                   `json:"prompt"`
	PromptVersion          string                   `json:"prompt_version"`
	PromptHash             string                   `json:"prompt_hash"`
	PromptModules          []string                 `json:"prompt_modules"`
	Profile                string                   `json:"profile"`
	ProfileSelectReason    string                   `json:"profile_select_reason"`
	Capabilities           []string                 `json:"capabilities"`
	ToolsExposed           []string                 `json:"tools_exposed"`
	SkillPreflightGuidance []skillPreflightGuidance `json:"skill_preflight_guidance"`
	HiddenTools            []string                 `json:"hidden_tools"`
	LoopPolicy             loopPolicyReport         `json:"loop_policy"`
	TerminalTools          []string                 `json:"terminal_tools"`
}

type skillPreflightGuidance struct {
	Capability        string   `json:"capability"`
	Required          bool     `json:"required"`
	AllowedSkillNames []string `json:"allowed_skill_names,omitempty"`
	SkillHints        []string `json:"skill_hints,omitempty"`
	Reason            string   `json:"reason"`
	FreshnessScope    string   `json:"freshness_scope"`
}

type loopPolicyReport struct {
	MaxSteps                int      `json:"max_steps"`
	TerminalTools           []string `json:"terminal_tools"`
	AllowNoToolFinalization bool     `json:"allow_no_tool_finalization"`
	DuplicateToolPolicy     string   `json:"duplicate_tool_policy"`
	MaxElapsed              string   `json:"max_elapsed"`
}

func main() {
	prompt := flag.String("prompt", "", "user prompt to route")
	mode := flag.String("mode", orchestration.ProfileModeAuto, "tool profile mode")
	swarm := flag.Bool("swarm", true, "simulate swarm availability")
	sandbox := flag.Bool("sandbox", true, "simulate sandbox availability")
	proposals := flag.Bool("proposals", true, "simulate proposal tool availability")
	version := flag.String("prompt-version", orchestration.VersionAuraAgentV1, "prompt version")
	jsonOut := flag.Bool("json", false, "print JSON report")
	flag.Parse()

	if strings.TrimSpace(*prompt) == "" {
		fmt.Fprintln(os.Stderr, "FAIL: -prompt is required")
		os.Exit(1)
	}

	report := buildReport(reportInput{
		Prompt:        *prompt,
		Mode:          *mode,
		Swarm:         *swarm,
		Sandbox:       *sandbox,
		Proposals:     *proposals,
		PromptVersion: *version,
	})
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: encode report: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("prompt=%q\n", report.Prompt)
	fmt.Printf("prompt_version=%s\n", report.PromptVersion)
	fmt.Printf("prompt_hash=%s\n", report.PromptHash)
	fmt.Printf("prompt_modules=%s\n", strings.Join(report.PromptModules, ","))
	fmt.Printf("tool_profile=%s\n", report.Profile)
	fmt.Printf("profile_select_reason=%s\n", report.ProfileSelectReason)
	fmt.Printf("capabilities=%s\n", strings.Join(report.Capabilities, ","))
	fmt.Printf("tools_exposed=%s\n", strings.Join(report.ToolsExposed, ","))
	fmt.Printf("skill_preflight_guidance=%s\n", formatSkillPreflight(report.SkillPreflightGuidance))
	fmt.Printf("hidden_tools=%s\n", strings.Join(report.HiddenTools, ","))
	fmt.Printf("loop_policy=max_steps:%d,max_elapsed:%s,allow_no_tool_finalization:%t,duplicate_tool_policy:%q\n",
		report.LoopPolicy.MaxSteps,
		report.LoopPolicy.MaxElapsed,
		report.LoopPolicy.AllowNoToolFinalization,
		report.LoopPolicy.DuplicateToolPolicy,
	)
	fmt.Printf("terminal_tools=%s\n", strings.Join(report.TerminalTools, ","))
}

func buildReport(in reportInput) report {
	available := orchestration.Availability{
		Swarm:     in.Swarm,
		Sandbox:   in.Sandbox,
		Proposals: in.Proposals,
	}
	decision := orchestration.SelectProfileDecision(in.Prompt, in.Mode, available)
	tools, err := orchestration.ToolsForProfile(decision.Profile, available)
	if err != nil {
		decision = orchestration.ProfileDecision{
			Profile: orchestration.ProfileDefault,
			Reason:  "profile allowlist failed; fell back to default",
		}
		tools, _ = orchestration.ToolsForProfile(decision.Profile, available)
	}
	card, _ := orchestration.ProfileCardFor(decision.Profile)
	policy, _ := orchestration.LoopPolicyForProfile(decision.Profile)
	capabilities := orchestration.CapabilitiesForProfile(decision.Profile)
	capabilityNames := make([]string, 0, len(capabilities))
	guidance := make([]skillPreflightGuidance, 0, len(capabilities))
	for _, capability := range capabilities {
		capabilityNames = append(capabilityNames, string(capability))
		req, ok := orchestration.SkillRequirementForCapability(capability, orchestration.SkillPreflightRequired)
		if !ok {
			continue
		}
		guidance = append(guidance, skillPreflightGuidance{
			Capability:        string(req.Capability),
			Required:          req.Required,
			AllowedSkillNames: req.AllowedSkillNames,
			SkillHints:        req.SkillHints,
			Reason:            req.Reason,
			FreshnessScope:    req.FreshnessScope,
		})
	}
	plan := orchestration.ComposePrompt(orchestration.PromptInput{
		Version:           in.PromptVersion,
		Now:               time.Now(),
		Location:          time.Local,
		Overlay:           in.Overlay,
		SkillsBlock:       in.SkillsBlock,
		SwarmAvailable:    available.Swarm,
		SandboxAvailable:  available.Sandbox,
		ProposalAvailable: available.Proposals,
		Profile:           decision.Profile,
	})
	return report{
		Prompt:                 in.Prompt,
		PromptVersion:          plan.Version,
		PromptHash:             plan.Hash,
		PromptModules:          plan.Modules,
		Profile:                string(decision.Profile),
		ProfileSelectReason:    decision.Reason,
		Capabilities:           capabilityNames,
		ToolsExposed:           tools,
		SkillPreflightGuidance: guidance,
		HiddenTools:            card.DeniedTools,
		LoopPolicy: loopPolicyReport{
			MaxSteps:                policy.MaxSteps,
			TerminalTools:           policy.TerminalTools,
			AllowNoToolFinalization: policy.AllowNoToolFinalization,
			DuplicateToolPolicy:     policy.DuplicateToolPolicy,
			MaxElapsed:              policy.MaxElapsed.String(),
		},
		TerminalTools: policy.TerminalTools,
	}
}

func formatSkillPreflight(guidance []skillPreflightGuidance) string {
	if len(guidance) == 0 {
		return ""
	}
	parts := make([]string, 0, len(guidance))
	for _, item := range guidance {
		required := "advisory"
		if item.Required {
			required = "required"
		}
		skills := item.AllowedSkillNames
		if len(skills) == 0 {
			skills = item.SkillHints
		}
		parts = append(parts, fmt.Sprintf("%s:%s:%s", item.Capability, required, strings.Join(skills, "|")))
	}
	return strings.Join(parts, ";")
}
