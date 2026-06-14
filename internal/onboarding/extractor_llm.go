package onboarding

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// AnswerExtractor turns a free-text interview answer into structured Answers.
type AnswerExtractor interface {
	Extract(ctx context.Context, step Step, raw string) (Answers, error)
}

// LLMAnswerExtractor extracts fields via a one-shot tool-free LLM completion,
// mirroring the reasoningOracle pattern. It never returns a hard error: on
// transport or parse failure it falls back to storing the raw answer in the
// step's primary field, so onboarding never blocks.
type LLMAnswerExtractor struct {
	client llm.Client
	model  string
}

// NewLLMAnswerExtractor creates an LLMAnswerExtractor backed by the given client and model.
func NewLLMAnswerExtractor(client llm.Client, model string) *LLMAnswerExtractor {
	return &LLMAnswerExtractor{client: client, model: model}
}

type extractDTO struct {
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	Company        string   `json:"company"`
	Location       string   `json:"location"`
	Timezone       string   `json:"timezone"`
	Lang           string   `json:"lang"`
	Expertise      []string `json:"expertise"`
	Stack          []string `json:"stack"`
	Projects       []string `json:"projects"`
	Goals          []string `json:"goals"`
	Interests      []string `json:"interests"`
	People         []string `json:"people"`
	TonePreference string   `json:"tone_preference"`
	ResponseLength string   `json:"response_length"`
	VoiceMode      *bool    `json:"voice_mode"`
}

// Extract runs a one-shot LLM call and returns structured Answers for the given step.
func (e *LLMAnswerExtractor) Extract(ctx context.Context, step Step, raw string) (Answers, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Answers{}, nil
	}
	disabled := false
	req := llm.Request{
		Model:       e.model,
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: extractSystemPrompt(step)}, {Role: llm.RoleUser, Content: raw}},
		Temperature: 0,
		MaxTokens:   256,
		Reasoning:   llm.ReasoningConfig{Enabled: &disabled},
		ToolChoice:  "none",
	}
	ch, err := e.client.Stream(ctx, req)
	if err != nil {
		return fallbackAnswers(step, raw), nil
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			return fallbackAnswers(step, raw), nil
		}
		b.WriteString(c.Text)
	}
	var dto extractDTO
	if err := json.Unmarshal([]byte(extractJSON(b.String())), &dto); err != nil {
		return fallbackAnswers(step, raw), nil
	}
	return Answers{
		Name: dto.Name,
		Role: dto.Role, Company: dto.Company, Location: dto.Location, Timezone: dto.Timezone, Lang: dto.Lang,
		Expertise: dto.Expertise, Stack: dto.Stack, Projects: dto.Projects, Goals: dto.Goals,
		Interests: dto.Interests, People: dto.People,
		TonePreference: dto.TonePreference, ResponseLength: dto.ResponseLength, VoiceMode: dto.VoiceMode,
	}, nil
}

// extractJSON trims to the outermost {...} if the model wrapped its output.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j >= i {
			return s[i : j+1]
		}
	}
	return s
}

// fallbackAnswers stores the raw answer in the step's primary slice/field so a
// failed extraction still records something.
func fallbackAnswers(step Step, raw string) Answers {
	switch step {
	case StepIdentity:
		return Answers{Name: raw}
	case StepWork:
		return Answers{Expertise: []string{raw}}
	case StepProjects:
		return Answers{Projects: []string{raw}}
	case StepSocial:
		return Answers{Interests: []string{raw}}
	default:
		return Answers{CustomInstructions: raw}
	}
}

// extractSystemPrompt is the per-step extraction instruction (English; the model
// returns JSON only).
func extractSystemPrompt(step Step) string {
	const base = "You extract structured profile facts from one onboarding answer. " +
		"Reply with a SINGLE JSON object and nothing else. Use empty string/array for unknown fields. " +
		"Correct obvious misspellings and normalize names, technologies, companies, and places to their standard spelling and capitalization. " +
		"Keep values short; preserve the user's language. "
	switch step {
	case StepIdentity:
		return base + `Fields: {"name":"","role":"","company":"","location":"","timezone":"","lang":""}. name is the user's first name or preferred name (not their full job title or sentence).`
	case StepWork:
		return base + `Fields: {"expertise":[],"stack":[]}.`
	case StepProjects:
		return base + `Fields: {"projects":[],"goals":[]}.`
	case StepSocial:
		return base + `Fields: {"interests":[],"people":[]}. For people use "Name — role".`
	case StepStyle:
		return base + `Fields: {"tone_preference":"","response_length":"","lang":"","voice_mode":null}. tone_preference is one of "direct","friendly","formal"; response_length is one of "short","normal","detailed"; voice_mode is true only if the user wants spoken/voice replies. Map synonyms (e.g. "amichevole"->friendly, "diretto"->direct, "formale"->formal, "breve"->short, "normale"->normal, "dettagliata"->detailed).`
	default:
		return base + `Fields: {"name":"","role":"","company":"","location":"","timezone":"","lang":"","expertise":[],"stack":[],"projects":[],"goals":[],"interests":[],"people":[],"tone_preference":"","response_length":"","voice_mode":null}. Extract every field the user mentions or corrects.`
	}
}
