package main

import (
	"context"
	"fmt"
	"iter"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
	"github.com/google/uuid"
)

type scriptedOnboarding struct {
	name   string
	mode   string
	cursor int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[FAIL]", err)
		os.Exit(1)
	}
	fmt.Println("[SUMMARY] VALIDATED phase14 Telegram onboarding LoopAgent prototype")
}

func run() error {
	if err := assertConfirmScenario(); err != nil {
		return err
	}
	fmt.Println("[CHECK] confirm path escalates with completed Agent.md")
	if err := assertSkipScenario(); err != nil {
		return err
	}
	fmt.Println("[CHECK] skip path escalates and resumes normal chat")
	if err := assertEditScenario(); err != nil {
		return err
	}
	fmt.Println("[CHECK] edit path revises draft before profile write")
	return nil
}

func assertConfirmScenario() error {
	events, err := runScenario("confirm")
	if err != nil {
		return err
	}
	if len(events) != 4 {
		return fmt.Errorf("confirm emitted %d events, want 4", len(events))
	}
	if !strings.Contains(events[0].LLMResponse.Content, "What should Aura call you") {
		return fmt.Errorf("first confirm prompt wrong: %q", events[0].LLMResponse.Content)
	}
	last := events[len(events)-1]
	if !last.Actions.Escalate {
		return fmt.Errorf("confirm final event did not escalate")
	}
	if !boolState(last, "onboarding_completed") {
		return fmt.Errorf("confirm final state missing onboarding_completed=true")
	}
	if !strings.Contains(stringState(last, "agent_md"), "Prefer Italian responses") {
		return fmt.Errorf("confirm final Agent.md missing extracted preference")
	}
	return nil
}

func assertSkipScenario() error {
	events, err := runScenario("skip")
	if err != nil {
		return err
	}
	if len(events) != 1 {
		return fmt.Errorf("skip emitted %d events, want 1", len(events))
	}
	last := events[0]
	if !last.Actions.Escalate || !boolState(last, "skipped") || !boolState(last, "resume_chat") {
		return fmt.Errorf("skip final state wrong: %+v", last.Actions.StateDelta)
	}
	return nil
}

func assertEditScenario() error {
	events, err := runScenario("edit")
	if err != nil {
		return err
	}
	if len(events) != 3 {
		return fmt.Errorf("edit emitted %d events, want 3", len(events))
	}
	last := events[len(events)-1]
	if !last.Actions.Escalate {
		return fmt.Errorf("edit final event did not escalate")
	}
	md := stringState(last, "agent_md")
	if !strings.Contains(md, "Prefer concise Italian technical answers") {
		return fmt.Errorf("edited Agent.md missing revised preference: %q", md)
	}
	return nil
}

func runScenario(mode string) ([]*agent.Event, error) {
	maxSteps := 8
	budget, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		return nil, err
	}
	step := &scriptedOnboarding{name: "InterviewStepAgent", mode: mode}
	loop := workflow.NewLoop("TelegramOnboardingLoop", 8, step)
	ic := agent.InvocationContext{
		Ctx:       context.Background(),
		Agent:     loop,
		RequestID: uuid.Must(uuid.NewV7()),
		Branch:    "telegram.onboarding",
		Budget:    budget,
	}
	var events []*agent.Event
	for ev, err := range loop.Run(ic) {
		if err != nil {
			return nil, err
		}
		if ev != nil {
			events = append(events, ev)
			fmt.Printf("[TRACE] %s event %02d author=%s escalate=%v state=%v\n", mode, len(events), ev.Author, ev.Actions.Escalate, ev.Actions.StateDelta)
		}
	}
	return events, nil
}

func (s *scriptedOnboarding) Name() string { return s.name }

func (s *scriptedOnboarding) Description() string {
	return "collects first-run Telegram profile facts and escalates on terminal user choice"
}

func (s *scriptedOnboarding) SubAgents() []agent.Agent { return nil }

func (s *scriptedOnboarding) FindAgent(name string) agent.Agent {
	if name == s.name {
		return s
	}
	return nil
}

func (s *scriptedOnboarding) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		ev := s.nextEvent(ic)
		if ev == nil {
			return
		}
		_ = yield(ev, nil)
	}
}

func (s *scriptedOnboarding) nextEvent(ic agent.InvocationContext) *agent.Event {
	step := s.cursor
	s.cursor++
	switch s.mode {
	case "confirm":
		return s.confirmEvent(ic, step)
	case "skip":
		return newEvent(ic, s.name, "Onboarding skipped. Normal chat resumes.", true, map[string]any{
			"skipped":     true,
			"resume_chat": true,
		})
	case "edit":
		return s.editEvent(ic, step)
	default:
		return newEvent(ic, s.name, "unknown scenario", true, map[string]any{"error": "unknown scenario"})
	}
}

func (s *scriptedOnboarding) confirmEvent(ic agent.InvocationContext, step int) *agent.Event {
	switch step {
	case 0:
		return newEvent(ic, s.name, "What should Aura call you?", false, map[string]any{"onboarding_step": "name"})
	case 1:
		return newEvent(ic, s.name, "Which language and tone should Aura use?", false, map[string]any{"onboarding_step": "preferences"})
	case 2:
		return newEvent(ic, s.name, "Draft ready. Tap Conferma to save it.", false, map[string]any{
			"profile_draft": baseAgentMD(),
		})
	default:
		return newEvent(ic, s.name, "Conferma received. Saving Agent.md and resuming chat.", true, map[string]any{
			"onboarding_completed": true,
			"agent_md":             baseAgentMD(),
			"preferences_json":     `{"lang":"it","timezone":"Europe/Rome","voice_mode":true}`,
			"resume_chat":          true,
		})
	}
}

func (s *scriptedOnboarding) editEvent(ic agent.InvocationContext, step int) *agent.Event {
	switch step {
	case 0:
		return newEvent(ic, s.name, "Draft ready. You can confirm, edit, or skip.", false, map[string]any{
			"profile_draft": baseAgentMD(),
		})
	case 1:
		return newEvent(ic, s.name, "Edit applied: concise Italian technical answers.", false, map[string]any{
			"profile_draft": editedAgentMD(),
		})
	default:
		return newEvent(ic, s.name, "Conferma received after edit. Saving profile.", true, map[string]any{
			"onboarding_completed": true,
			"agent_md":             editedAgentMD(),
			"resume_chat":          true,
		})
	}
}

func newEvent(ic agent.InvocationContext, author, content string, escalate bool, state map[string]any) *agent.Event {
	return &agent.Event{
		RequestID: ic.RequestID,
		SpanID:    ic.SpanID,
		Author:    author,
		Branch:    ic.Branch,
		LLMResponse: &agent.LLMResponse{
			Content: content,
		},
		Actions: agent.Actions{
			Escalate:   escalate,
			StateDelta: state,
		},
		Timestamp: time.Now().UTC(),
	}
}

func baseAgentMD() string {
	return "# Agent.md\n\n## Facts\n- Name: Davide\n\n## Preferences\n- Prefer Italian responses.\n- Timezone: Europe/Rome.\n"
}

func editedAgentMD() string {
	return "# Agent.md\n\n## Facts\n- Name: Davide\n\n## Preferences\n- Prefer concise Italian technical answers.\n- Timezone: Europe/Rome.\n"
}

func boolState(ev *agent.Event, key string) bool {
	v, _ := ev.Actions.StateDelta[key].(bool)
	return v
}

func stringState(ev *agent.Event, key string) string {
	v, _ := ev.Actions.StateDelta[key].(string)
	return v
}
