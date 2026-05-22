package cron

import (
	"context"
	"fmt"
	"strings"
	"time"

	toolsets "github.com/aura/aura/internal/agent/tools/sets"
	"github.com/aura/aura/internal/conversation"
)

// ---- prompt builders --------------------------------------------------------

func agentJobNotificationMessage(task *Task, payload AgentJobPayload, content string) string {
	name := ""
	if task != nil {
		name = task.Name
	}
	if payload.Language == "it" {
		return fmt.Sprintf("Job agente %q completato.\n\n%s", name, truncate(content, 3200))
	}
	return fmt.Sprintf("Agent job %q completed.\n\n%s", name, truncate(content, 3200))
}

func AgentJobSystemPrompt(payload AgentJobPayload, now time.Time, loc *time.Location) string {
	prompt := "You are Aura running a scheduled agent job. Complete the saved routine with concise, evidence-oriented work. Write policy: " + payload.WritePolicy + ". Do not mutate wiki pages, sources, skills, settings, tasks, files, or external state directly. If durable memory or reusable procedural knowledge is useful, report the exact file path and patch you recommend instead of applying it. Return a short report with what you checked, any recommended edit, and unresolved issues."
	if lang := agentJobLanguageInstruction(payload.Language); lang != "" {
		prompt += " " + lang
	} else {
		prompt += " Output language: infer the language from the saved goal and context anchors; if unclear, mirror the language most recently used by the user in that saved context."
	}
	if len(payload.Skills) > 0 {
		prompt += " This job is skill-backed: inspect attached SKILL.md files with workspace file tools before applying their procedures."
	}
	if len(payload.WakeIfChanged) > 0 {
		prompt += " Respect wake_if_changed as a no-op guard: check those signals first and finish quickly with no proposal if there is no material change."
	}
	prompt += conversation.RenderRuntimeContext(now, loc)
	return prompt
}

// AgentJobUserPrompt builds a minimal user-turn prompt for Hub-routed agent jobs.
// It omits schedule context and prior outputs (not available from InboundMessage alone).
func AgentJobUserPrompt(payload AgentJobPayload) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Goal: %s", payload.Goal)
	if len(payload.EnabledToolsets) > 0 {
		fmt.Fprintf(&sb, "\n\nEnabled toolsets: %s", strings.Join(payload.EnabledToolsets, ", "))
	}
	if len(payload.Skills) > 0 {
		fmt.Fprintf(&sb, "\n\nAttached skills: %s\nFind and read the matching SKILL.md files before relying on their procedures. Do not install, delete, or edit skills directly.", strings.Join(payload.Skills, ", "))
	}
	if len(payload.ContextFrom) > 0 {
		fmt.Fprintf(&sb, "\n\nContext anchors: %s\nUse these anchors as the first retrieval targets.", strings.Join(payload.ContextFrom, ", "))
	}
	if len(payload.WakeIfChanged) > 0 {
		fmt.Fprintf(&sb, "\n\nWake-if-changed signals: %s\nBefore doing the full routine, check whether these signals changed materially. If not, return a concise no-change report and stop.", strings.Join(payload.WakeIfChanged, ", "))
	}
	return sb.String()
}

func agentJobLanguageInstruction(language string) string {
	switch NormalizeAgentJobLanguage(language) {
	case "it":
		return "Output language: Italian. Write the final report and all user-facing prose in Italian, regardless of tool or web result language."
	case "en":
		return "Output language: English. Write the final report and all user-facing prose in English, regardless of tool or web result language."
	default:
		return ""
	}
}

func (h *Handler) agentJobPrompt(ctx context.Context, task *Task, payload AgentJobPayload, now time.Time, loc *time.Location) string {
	var sb strings.Builder
	if schedule := agentJobScheduleContext(task, now, loc); schedule != "" {
		sb.WriteString(schedule)
		sb.WriteString("\n\n")
	}
	fmt.Fprintf(&sb, "Goal: %s", payload.Goal)
	if len(payload.EnabledToolsets) > 0 {
		fmt.Fprintf(&sb, "\n\nEnabled toolsets: %s", strings.Join(payload.EnabledToolsets, ", "))
	}
	if len(payload.Skills) > 0 {
		fmt.Fprintf(&sb, "\n\nAttached skills: %s\nFind and read the matching SKILL.md files before relying on their procedures. Do not install, delete, or edit skills directly.", strings.Join(payload.Skills, ", "))
	}
	if len(payload.ContextFrom) > 0 {
		fmt.Fprintf(&sb, "\n\nContext anchors: %s\nUse these anchors as the first retrieval targets, preferably via search_memory or narrow read tools before broad web/tool use.", strings.Join(payload.ContextFrom, ", "))
		if prior := h.agentJobPriorOutputs(ctx, payload.ContextFrom); prior != "" {
			fmt.Fprintf(&sb, "\n\nPrior job outputs:\n%s", prior)
		}
	}
	if len(payload.WakeIfChanged) > 0 {
		fmt.Fprintf(&sb, "\n\nWake-if-changed signals: %s\nBefore doing the full routine, check whether these signals changed materially. If not, return a concise no-change report and stop.", strings.Join(payload.WakeIfChanged, ", "))
	}
	return sb.String()
}

func agentJobScheduleContext(task *Task, now time.Time, loc *time.Location) string {
	if task == nil || task.NextRunAt.IsZero() {
		return ""
	}
	if loc == nil {
		loc = time.Local
	}
	scheduledLocal := task.NextRunAt.In(loc)
	runningLocal := now.In(loc)
	var sb strings.Builder
	fmt.Fprintf(&sb, "Scheduled task: %s\n", task.Name)
	fmt.Fprintf(&sb, "Scheduled for: %s local (%s UTC)\n",
		scheduledLocal.Format("2006-01-02 15:04:05"),
		task.NextRunAt.UTC().Format(time.RFC3339),
	)
	fmt.Fprintf(&sb, "Running at: %s local (%s UTC)\n",
		runningLocal.Format("2006-01-02 15:04:05"),
		now.UTC().Format(time.RFC3339),
	)
	if task.ScheduleKind != "" {
		fmt.Fprintf(&sb, "Schedule kind: %s", task.ScheduleKind)
		switch task.ScheduleKind {
		case ScheduleDaily:
			fmt.Fprintf(&sb, " daily=%s", task.ScheduleDaily)
			if task.ScheduleWeekdays != "" {
				fmt.Fprintf(&sb, " weekdays=%s", task.ScheduleWeekdays)
			}
		case ScheduleEvery:
			fmt.Fprintf(&sb, " every_minutes=%d", task.ScheduleEveryMinutes)
		}
		sb.WriteString("\n")
	}
	if delay := now.Sub(task.NextRunAt); delay > time.Minute {
		fmt.Fprintf(&sb, "Run delay: %s. Treat current-date research as of Running at, not Scheduled for, unless the goal explicitly asks for historical state.\n", delay.Round(time.Minute))
	}
	return strings.TrimSpace(sb.String())
}

func (h *Handler) agentJobPriorOutputs(ctx context.Context, anchors []string) string {
	if h.schedDB == nil {
		return ""
	}
	var lines []string
	for _, anchor := range anchors {
		name, ok := AgentJobTaskAnchor(anchor)
		if !ok {
			continue
		}
		task, err := h.schedDB.GetByName(ctx, name)
		if err != nil || strings.TrimSpace(task.LastOutput) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", task.Name, truncate(task.LastOutput, 800)))
	}
	return strings.Join(lines, "\n")
}

// ---- helpers ----------------------------------------------------------------

func SafeAgentJobTools(requested []string) []string {
	out := toolsets.FilterAllowed(requested, AgentJobAllowedTools)
	if len(out) == 0 {
		return append([]string(nil), DefaultAgentJobTools...)
	}
	return out
}

func truncate(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
