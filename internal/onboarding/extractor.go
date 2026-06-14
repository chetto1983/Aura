package onboarding

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/profile"
)

const maxAnswerFieldBytes = 2048

// Draft is the rendered Agent.md plus structured preferences from onboarding answers.
type Draft struct {
	AgentMD         string
	Preferences     profile.Preferences
	PreferencesJSON string
}

// ExtractDraft renders a bounded Agent.md draft and preferences from answers.
func ExtractDraft(answers Answers) (Draft, error) {
	answers = cleanAnswers(answers)
	prefs := profile.Preferences{
		Lang:                answers.Lang,
		Timezone:            answers.Timezone,
		Location:            answers.Location,
		VoiceMode:           boolValue(answers.VoiceMode),
		CanProactiveMessage: boolValue(answers.CanProactiveMessage),
		TonePreference:      answers.TonePreference,
		ResponseLength:      answers.ResponseLength,
	}

	md := profile.RenderAgentMD(profile.AgentContent{
		Identity:           identityLines(answers),
		ExpertiseTools:     expertiseLines(answers),
		ProjectsGoals:      projectGoalLines(answers),
		Interests:          bulletLines(answers.Interests),
		People:             bulletLines(answers.People),
		Style:              styleLines(answers),
		Vetoes:             bulletLines(answers.Vetoes),
		CustomInstructions: customInstructionLines(answers),
	})
	if len([]byte(md)) > profile.MaxAgentMDBytes {
		md = truncateBytes(md, profile.MaxAgentMDBytes)
	}
	prefsJSON, err := json.Marshal(prefs)
	if err != nil {
		return Draft{}, fmt.Errorf("marshal onboarding preferences: %w", err)
	}
	return Draft{AgentMD: md, Preferences: prefs, PreferencesJSON: string(prefsJSON)}, nil
}

func cleanAnswers(a Answers) Answers {
	a.Name = cleanField(a.Name)
	a.Role = cleanField(a.Role)
	a.Company = cleanField(a.Company)
	a.Location = cleanField(a.Location)
	a.Lang = cleanField(a.Lang)
	a.Timezone = cleanField(a.Timezone)
	a.TonePreference = cleanField(a.TonePreference)
	a.ResponseLength = cleanField(a.ResponseLength)
	a.CustomInstructions = cleanField(a.CustomInstructions)
	return a
}

func cleanField(s string) string {
	s = strings.TrimSpace(s)
	if len([]byte(s)) <= maxAnswerFieldBytes {
		return s
	}
	return truncateBytes(s, maxAnswerFieldBytes)
}

func identityLines(a Answers) []string {
	var out []string
	if a.Name != "" {
		out = append(out, "Name: "+a.Name)
	}
	if role := joinRoleCompany(a.Role, a.Company); role != "" {
		out = append(out, "Role: "+role)
	}
	if a.Location != "" {
		out = append(out, "Location: "+a.Location)
	}
	if a.Timezone != "" {
		out = append(out, "Timezone: "+a.Timezone)
	}
	if lang := languagePreference(a.Lang); lang != "" {
		out = append(out, lang)
	}
	return out
}

func joinRoleCompany(role, company string) string {
	switch {
	case role != "" && company != "":
		return role + " @ " + company
	case role != "":
		return role
	case company != "":
		return company
	default:
		return ""
	}
}

func expertiseLines(a Answers) []string {
	var out []string
	if len(a.Expertise) > 0 {
		out = append(out, "Domains: "+strings.Join(a.Expertise, ", "))
	}
	if len(a.Stack) > 0 {
		out = append(out, "Stack: "+strings.Join(a.Stack, ", "))
	}
	return out
}

func projectGoalLines(a Answers) []string {
	out := bulletLines(a.Projects)
	for _, g := range a.Goals {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, "Goal: "+g)
		}
	}
	return out
}

func styleLines(a Answers) []string {
	var out []string
	if a.TonePreference != "" {
		out = append(out, "Tone: "+a.TonePreference)
	}
	if a.ResponseLength != "" {
		out = append(out, "Response length: "+a.ResponseLength)
	}
	if a.Lang != "" {
		out = append(out, "Reply language: "+a.Lang)
	}
	if a.VoiceMode != nil {
		out = append(out, fmt.Sprintf("Voice mode: %t", boolValue(a.VoiceMode)))
	}
	if a.CanProactiveMessage != nil {
		out = append(out, fmt.Sprintf("Can proactive message: %t", boolValue(a.CanProactiveMessage)))
	}
	return out
}

func bulletLines(items []string) []string {
	var out []string
	for _, it := range items {
		if it = strings.TrimSpace(it); it != "" {
			out = append(out, it)
		}
	}
	return out
}

func customInstructionLines(a Answers) []string {
	if a.CustomInstructions == "" {
		return nil
	}
	return []string{a.CustomInstructions}
}

func languagePreference(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "":
		return ""
	case "it", "ita", "italian", "italiano", "italiana":
		return "Prefer Italian responses"
	case "en", "eng", "english", "inglese":
		return "Prefer English responses"
	default:
		return "Language: " + lang
	}
}

func truncateBytes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len([]byte(s)) <= limit {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if b.Len()+len(string(r)) > limit {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
