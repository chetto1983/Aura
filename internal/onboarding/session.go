package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/profile"
)

type Intent string

const (
	IntentAnswer  Intent = "answer"
	IntentConfirm Intent = "confirm"
	IntentEdit    Intent = "edit"
	IntentSkip    Intent = "skip"
	IntentCancel  Intent = "cancel"
	IntentRestart Intent = "restart"
)

type Step string

const (
	StepName        Step = "name"
	StepPreferences Step = "preferences"
	StepDraft       Step = "draft"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusDraft     Status = "draft"
	StatusCompleted Status = "completed"
	StatusSkipped   Status = "skipped"
	StatusCanceled  Status = "canceled"
)

var (
	ErrDraftRequired = errors.New("onboarding draft required")
	ErrTerminal      = errors.New("onboarding session is terminal")
	ErrInvalidIntent = errors.New("invalid onboarding intent")
)

type Answers struct {
	Name                string
	Lang                string
	Timezone            string
	TonePreference      string
	ResponseLength      string
	VoiceMode           *bool
	CanProactiveMessage *bool
	CustomInstructions  string
}

type Input struct {
	Intent  Intent
	Text    string
	Answers Answers
}

type Transition struct {
	Content    string
	StateDelta map[string]any
	Terminal   bool
}

type Session struct {
	IdentityID   string
	IdentityName string
	Step         Step
	Status       Status

	Answers      Answers
	DraftAgentMD string
	Preferences  profile.Preferences

	pending  []Input
	prompted bool
}

func NewSession(identityID, identityName string) *Session {
	return &Session{
		IdentityID:   identityID,
		IdentityName: identityName,
		Step:         StepName,
		Status:       StatusActive,
	}
}

func (s *Session) Apply(in Input) (Transition, error) {
	if in.Intent == "" {
		in.Intent = IntentAnswer
	}
	if s.isTerminal() && in.Intent != IntentRestart {
		return Transition{}, ErrTerminal
	}
	switch in.Intent {
	case IntentAnswer:
		return s.applyAnswer(in)
	case IntentConfirm:
		return s.confirm()
	case IntentEdit:
		return s.edit(in)
	case IntentSkip:
		s.Status = StatusSkipped
		return s.terminal("Onboarding skipped. Normal chat resumes.", map[string]any{"skipped": true}), nil
	case IntentCancel:
		s.Status = StatusCanceled
		return s.terminal("Onboarding canceled. Normal chat resumes.", map[string]any{"canceled": true}), nil
	case IntentRestart:
		s.restart()
		return s.questionName(), nil
	default:
		return Transition{}, fmt.Errorf("%w: %s", ErrInvalidIntent, in.Intent)
	}
}

func (s *Session) Queue(inputs ...Input) {
	s.pending = append(s.pending, inputs...)
}

func (s *Session) nextTransition() (Transition, bool, error) {
	if s.isTerminal() {
		return Transition{}, false, nil
	}
	if len(s.pending) > 0 && shouldApplyBeforePrompt(s.pending[0]) {
		out, err := s.applyQueued()
		return out, true, err
	}
	if !s.prompted {
		s.prompted = true
		out, ok := s.currentPrompt()
		return out, ok, nil
	}
	if len(s.pending) == 0 {
		return Transition{}, false, nil
	}
	out, err := s.applyQueued()
	return out, true, err
}

func (s *Session) applyQueued() (Transition, error) {
	in := s.pending[0]
	s.pending = s.pending[1:]
	out, err := s.Apply(in)
	if err != nil {
		return Transition{}, err
	}
	s.prompted = !out.Terminal
	return out, nil
}

func (s *Session) applyAnswer(in Input) (Transition, error) {
	s.mergeAnswers(in)
	switch s.Step {
	case StepName:
		s.Step = StepPreferences
		return s.questionPreferences(), nil
	case StepPreferences, StepDraft:
		if err := s.refreshDraft(); err != nil {
			return Transition{}, err
		}
		s.Step = StepDraft
		s.Status = StatusDraft
		return s.draft("Draft ready. Confirm, edit, or skip."), nil
	default:
		return Transition{}, fmt.Errorf("unknown onboarding step %q", s.Step)
	}
}

func (s *Session) confirm() (Transition, error) {
	if strings.TrimSpace(s.DraftAgentMD) == "" {
		return Transition{}, ErrDraftRequired
	}
	s.Status = StatusCompleted
	return s.terminal("Profile confirmed. Saving Agent.md and resuming chat.", map[string]any{
		"onboarding_completed": true,
		"agent_md":             s.DraftAgentMD,
		"preferences_json":     s.preferencesJSON(),
	}), nil
}

func (s *Session) edit(in Input) (Transition, error) {
	if strings.TrimSpace(s.DraftAgentMD) == "" {
		return Transition{}, ErrDraftRequired
	}
	s.mergeAnswers(in)
	if err := s.refreshDraft(); err != nil {
		return Transition{}, err
	}
	s.Step = StepDraft
	s.Status = StatusDraft
	return s.draft("Draft updated. Confirm, edit, or skip."), nil
}

func (s *Session) restart() {
	id, name := s.IdentityID, s.IdentityName
	*s = *NewSession(id, name)
}

func (s *Session) isTerminal() bool {
	return s.Status == StatusCompleted || s.Status == StatusSkipped || s.Status == StatusCanceled
}

func (s *Session) mergeAnswers(in Input) {
	a := in.Answers
	if a.Name == "" && s.Step == StepName && strings.TrimSpace(in.Text) != "" {
		a.Name = strings.TrimSpace(in.Text)
	}
	if a.Name != "" {
		s.Answers.Name = strings.TrimSpace(a.Name)
	}
	if a.Lang != "" {
		s.Answers.Lang = strings.TrimSpace(a.Lang)
	}
	if a.Timezone != "" {
		s.Answers.Timezone = strings.TrimSpace(a.Timezone)
	}
	if a.TonePreference != "" {
		s.Answers.TonePreference = strings.TrimSpace(a.TonePreference)
	}
	if a.ResponseLength != "" {
		s.Answers.ResponseLength = strings.TrimSpace(a.ResponseLength)
	}
	if a.VoiceMode != nil {
		s.Answers.VoiceMode = a.VoiceMode
	}
	if a.CanProactiveMessage != nil {
		s.Answers.CanProactiveMessage = a.CanProactiveMessage
	}
	if a.CustomInstructions != "" {
		s.Answers.CustomInstructions = strings.TrimSpace(a.CustomInstructions)
	}
}

func (s *Session) refreshDraft() error {
	prefs := profile.Preferences{
		Lang:                s.Answers.Lang,
		Timezone:            s.Answers.Timezone,
		TonePreference:      s.Answers.TonePreference,
		ResponseLength:      s.Answers.ResponseLength,
		CanProactiveMessage: boolValue(s.Answers.CanProactiveMessage),
		VoiceMode:           boolValue(s.Answers.VoiceMode),
	}
	facts := valueList("Name: ", s.Answers.Name)
	prefsLines := []string{}
	if s.Answers.Lang != "" {
		prefsLines = append(prefsLines, "Language: "+s.Answers.Lang)
	}
	if s.Answers.Timezone != "" {
		prefsLines = append(prefsLines, "Timezone: "+s.Answers.Timezone)
	}
	if s.Answers.TonePreference != "" {
		prefsLines = append(prefsLines, "Tone: "+s.Answers.TonePreference)
	}
	if s.Answers.ResponseLength != "" {
		prefsLines = append(prefsLines, "Response length: "+s.Answers.ResponseLength)
	}
	if s.Answers.VoiceMode != nil {
		prefsLines = append(prefsLines, fmt.Sprintf("Voice mode: %t", boolValue(s.Answers.VoiceMode)))
	}
	if s.Answers.CanProactiveMessage != nil {
		prefsLines = append(prefsLines, fmt.Sprintf("Can proactive message: %t", boolValue(s.Answers.CanProactiveMessage)))
	}
	custom := valueList("", s.Answers.CustomInstructions)
	md := profile.RenderAgentMD(profile.AgentContent{
		Facts:              facts,
		Preferences:        prefsLines,
		CustomInstructions: custom,
	})
	if len([]byte(md)) > profile.MaxAgentMDBytes {
		return fmt.Errorf("onboarding draft exceeds %d bytes", profile.MaxAgentMDBytes)
	}
	s.DraftAgentMD = md
	s.Preferences = prefs
	return nil
}

func (s *Session) questionName() Transition {
	return Transition{
		Content:    "What should Aura call you?",
		StateDelta: s.state("onboarding_step", string(StepName)),
	}
}

func (s *Session) currentPrompt() (Transition, bool) {
	switch s.Step {
	case StepName:
		return s.questionName(), true
	case StepPreferences:
		return s.questionPreferences(), true
	case StepDraft:
		if strings.TrimSpace(s.DraftAgentMD) == "" {
			return Transition{}, false
		}
		return s.draft("Draft ready. Confirm, edit, or skip."), true
	default:
		return Transition{}, false
	}
}

func (s *Session) questionPreferences() Transition {
	return Transition{
		Content:    "Which language, timezone, tone, and response length should Aura use?",
		StateDelta: s.state("onboarding_step", string(StepPreferences)),
	}
}

func (s *Session) draft(content string) Transition {
	state := s.state("onboarding_step", string(StepDraft))
	state["profile_draft"] = s.DraftAgentMD
	state["preferences_json"] = s.preferencesJSON()
	return Transition{Content: content, StateDelta: state}
}

func (s *Session) terminal(content string, extra map[string]any) Transition {
	state := s.state("resume_chat", true)
	for k, v := range extra {
		state[k] = v
	}
	return Transition{Content: content, StateDelta: state, Terminal: true}
}

func (s *Session) state(key string, value any) map[string]any {
	state := map[string]any{
		"identity_id":   s.IdentityID,
		"identity_name": s.IdentityName,
		key:             value,
	}
	return state
}

func (s *Session) preferencesJSON() string {
	raw, err := json.Marshal(s.Preferences)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func valueList(prefix, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{prefix + value}
}

func boolValue(v *bool) bool {
	return v != nil && *v
}

func shouldApplyBeforePrompt(in Input) bool {
	return in.Intent == IntentSkip || in.Intent == IntentCancel || in.Intent == IntentRestart
}
