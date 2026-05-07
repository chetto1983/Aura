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
	Prompt              string   `json:"prompt"`
	PromptVersion       string   `json:"prompt_version"`
	PromptHash          string   `json:"prompt_hash"`
	PromptModules       []string `json:"prompt_modules"`
	Profile             string   `json:"profile"`
	ProfileSelectReason string   `json:"profile_select_reason"`
	ToolsExposed        []string `json:"tools_exposed"`
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
	fmt.Printf("tools_exposed=%s\n", strings.Join(report.ToolsExposed, ","))
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
		Prompt:              in.Prompt,
		PromptVersion:       plan.Version,
		PromptHash:          plan.Hash,
		PromptModules:       plan.Modules,
		Profile:             string(decision.Profile),
		ProfileSelectReason: decision.Reason,
		ToolsExposed:        tools,
	}
}
