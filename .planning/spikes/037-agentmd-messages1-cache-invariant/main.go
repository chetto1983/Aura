package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

const (
	systemPrefix = "You are Aura. Static runtime policy, tool-use rules, and safety invariants live here."
	alwaysBlock  = "Active skill instructions (always-on):\n\n- Keep replies concise.\n- Prefer the user's requested language."
	profileV1    = "# Agent.md\n\n## Facts\n- Name: Davide\n\n## Preferences\n- Prefer Italian responses."
	profileV2    = "# Agent.md\n\n## Facts\n- Name: Davide\n\n## Preferences\n- Prefer Italian responses.\n- Prefer short technical answers."
	maxMsg1Bytes = 32 * 1024
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[FAIL]", err)
		os.Exit(1)
	}
	fmt.Println("[SUMMARY] VALIDATED phase14 Agent.md messages[1] cache invariant")
}

func run() error {
	builder := prompt.NewPromptBuilder()
	reg := tools.NewRegistry()
	cfg := llm.Config{Model: "spike-037", Temperature: 0, MaxTokens: 512}

	var baseH0, baseH1 string
	for turn := 1; turn <= 20; turn++ {
		history := composeHistory(profileV1, turn)
		req := builder.Build(history, reg, "openrouter", cfg, prompt.Budget{
			Used:      turn,
			Remaining: 20 - turn,
			Workspace: "D:/tmp/aura-spike-037",
		})
		if err := assertShape(req.Messages); err != nil {
			return fmt.Errorf("turn %02d: %w", turn, err)
		}
		h0, err := prompt.PrefixHash(req.Messages, []int{0})
		if err != nil {
			return err
		}
		h1, err := prompt.PrefixHash(req.Messages, []int{1})
		if err != nil {
			return err
		}
		if turn == 1 {
			baseH0, baseH1 = h0, h1
			fmt.Printf("[CHECK] turn %02d base messages[0]=%s messages[1]=%s\n", turn, h0, h1)
			continue
		}
		if h0 != baseH0 {
			return fmt.Errorf("messages[0] drifted at turn %02d: %s != %s", turn, h0, baseH0)
		}
		if h1 != baseH1 {
			return fmt.Errorf("messages[1] drifted at turn %02d: %s != %s", turn, h1, baseH1)
		}
		fmt.Printf("[CHECK] turn %02d stable messages[0] and messages[1]\n", turn)
	}

	updated := builder.Build(composeHistory(profileV2, 21), reg, "openrouter", cfg, prompt.Budget{})
	h0Updated, err := prompt.PrefixHash(updated.Messages, []int{0})
	if err != nil {
		return err
	}
	h1Updated, err := prompt.PrefixHash(updated.Messages, []int{1})
	if err != nil {
		return err
	}
	if h0Updated != baseH0 {
		return fmt.Errorf("profile update mutated messages[0]: %s != %s", h0Updated, baseH0)
	}
	if h1Updated == baseH1 {
		return fmt.Errorf("profile update did not change messages[1]")
	}
	fmt.Printf("[CHECK] profile update changes messages[1] only: messages[0]=%s messages[1]=%s\n", h0Updated, h1Updated)
	return nil
}

func composeHistory(profile string, turns int) []llm.Message {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrefix},
		{Role: llm.RoleUser, Content: renderMessages1(profile, alwaysBlock)},
	}
	for i := 1; i <= turns; i++ {
		msgs = append(msgs,
			llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("user turn %02d", i)},
			llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("assistant turn %02d", i)},
		)
	}
	return msgs
}

func renderMessages1(profile, always string) string {
	return strings.Join([]string{
		"<profile:Agent.md>",
		strings.TrimSpace(profile),
		"</profile:Agent.md>",
		"",
		"<always-on:skills>",
		strings.TrimSpace(always),
		"</always-on:skills>",
	}, "\n")
}

func assertShape(msgs []llm.Message) error {
	if len(msgs) < 3 {
		return fmt.Errorf("request has %d messages, want at least 3", len(msgs))
	}
	if msgs[0].Role != llm.RoleSystem {
		return fmt.Errorf("messages[0] role = %q, want %q", msgs[0].Role, llm.RoleSystem)
	}
	if strings.Contains(msgs[0].Content, "Agent.md") {
		return fmt.Errorf("messages[0] contains Agent.md")
	}
	if msgs[1].Role != llm.RoleUser {
		return fmt.Errorf("messages[1] role = %q, want %q", msgs[1].Role, llm.RoleUser)
	}
	if len([]byte(msgs[1].Content)) > maxMsg1Bytes {
		return fmt.Errorf("messages[1] is %d bytes, above %d-byte cap", len([]byte(msgs[1].Content)), maxMsg1Bytes)
	}
	profileAt := strings.Index(msgs[1].Content, "<profile:Agent.md>")
	alwaysAt := strings.Index(msgs[1].Content, "<always-on:skills>")
	if profileAt < 0 || alwaysAt < 0 {
		return fmt.Errorf("messages[1] missing profile or always-on markers")
	}
	if profileAt > alwaysAt {
		return fmt.Errorf("messages[1] must put profile before always-on skills")
	}
	if !strings.HasPrefix(msgs[len(msgs)-1].Content, "<budget>") {
		return fmt.Errorf("PromptBuilder budget tail missing from final message")
	}
	return nil
}
