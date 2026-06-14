package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/profile"
)

// Intent describes the next user action applied to an onboarding session.
type Intent string

const (
	// IntentAnswer records an answer for the current onboarding step.
	IntentAnswer Intent = "answer"
	// IntentConfirm accepts and completes the current Agent.md draft.
	IntentConfirm Intent = "confirm"
	// IntentEdit updates the current Agent.md draft with revised answers.
	IntentEdit Intent = "edit"
	// IntentSkip ends onboarding without saving a profile.
	IntentSkip Intent = "skip"
	// IntentCancel cancels onboarding and resumes normal chat.
	IntentCancel Intent = "cancel"
	// IntentRestart resets onboarding to the first step.
	IntentRestart Intent = "restart"
)

// Step is the current profile onboarding interview step.
type Step string

const (
	// StepIdentity asks name, role, company, and location.
	StepIdentity Step = "identity"
	// StepWork collects expertise and tech stack.
	StepWork Step = "work"
	// StepProjects collects active projects and goals.
	StepProjects Step = "projects"
	// StepSocial collects interests and key collaborators.
	StepSocial Step = "social"
	// StepStyle collects tone, language, response-length, and voice preferences.
	StepStyle Step = "style"
	// StepDraft presents the Agent.md draft for confirm, edit, or skip.
	StepDraft Step = "draft"
)

// Status is the lifecycle state for a profile onboarding session.
type Status string

const (
	// StatusActive means the interview is still collecting answers.
	StatusActive Status = "active"
	// StatusDraft means an Agent.md draft is ready for review.
	StatusDraft Status = "draft"
	// StatusCompleted means onboarding produced a confirmed profile.
	StatusCompleted Status = "completed"
	// StatusSkipped means onboarding ended without saving a profile.
	StatusSkipped Status = "skipped"
	// StatusCanceled means onboarding was canceled before completion.
	StatusCanceled Status = "canceled"
)

var (
	// ErrDraftRequired is returned when confirm or edit is requested before a draft exists.
	ErrDraftRequired = errors.New("onboarding draft required")
	// ErrTerminal is returned when input is applied to a terminal session.
	ErrTerminal = errors.New("onboarding session is terminal")
	// ErrInvalidIntent is returned for an unknown onboarding intent.
	ErrInvalidIntent = errors.New("invalid onboarding intent")
)

// Answers contains structured profile facts collected during onboarding.
type Answers struct {
	Name      string
	Role      string
	Company   string
	Location  string
	Expertise []string
	Stack     []string
	Projects  []string
	Goals     []string
	Interests []string
	People    []string // e.g. "Andrea — business partner"
	Vetoes    []string

	// style/preferences
	Lang                string
	Timezone            string
	TonePreference      string
	ResponseLength      string
	VoiceMode           *bool
	CanProactiveMessage *bool
	CustomInstructions  string
}

// Input is one queued user action for the session state machine.
type Input struct {
	Intent  Intent
	Text    string
	Answers Answers
}

// Transition is the prompt and state delta emitted after applying input.
type Transition struct {
	Content    string
	StateDelta map[string]any
	Terminal   bool
}

// Session tracks one identity's profile onboarding state.
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

// NewSession starts profile onboarding for an identity.
func NewSession(identityID, identityName string) *Session {
	return &Session{IdentityID: identityID, IdentityName: identityName, Step: StepIdentity, Status: StatusActive}
}

// Apply applies one input to the session and returns the next transition.
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
		return s.question(StepIdentity), nil
	default:
		return Transition{}, fmt.Errorf("%w: %s", ErrInvalidIntent, in.Intent)
	}
}

// Queue appends inputs that the loop agent will consume in order.
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
	case StepIdentity:
		s.Step = StepWork
		return s.question(StepWork), nil
	case StepWork:
		s.Step = StepProjects
		return s.question(StepProjects), nil
	case StepProjects:
		s.Step = StepSocial
		return s.question(StepSocial), nil
	case StepSocial:
		s.Step = StepStyle
		return s.question(StepStyle), nil
	case StepStyle, StepDraft:
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
	if a.Name == "" && s.Step == StepIdentity && strings.TrimSpace(in.Text) != "" {
		a.Name = strings.TrimSpace(in.Text)
	}
	mergeStr(&s.Answers.Name, a.Name)
	mergeStr(&s.Answers.Role, a.Role)
	mergeStr(&s.Answers.Company, a.Company)
	mergeStr(&s.Answers.Location, a.Location)
	mergeStr(&s.Answers.Lang, a.Lang)
	mergeStr(&s.Answers.Timezone, a.Timezone)
	mergeStr(&s.Answers.TonePreference, a.TonePreference)
	mergeStr(&s.Answers.ResponseLength, a.ResponseLength)
	mergeStr(&s.Answers.CustomInstructions, a.CustomInstructions)
	s.Answers.Expertise = mergeSlice(s.Answers.Expertise, a.Expertise)
	s.Answers.Stack = mergeSlice(s.Answers.Stack, a.Stack)
	s.Answers.Projects = mergeSlice(s.Answers.Projects, a.Projects)
	s.Answers.Goals = mergeSlice(s.Answers.Goals, a.Goals)
	s.Answers.Interests = mergeSlice(s.Answers.Interests, a.Interests)
	s.Answers.People = mergeSlice(s.Answers.People, a.People)
	s.Answers.Vetoes = mergeSlice(s.Answers.Vetoes, a.Vetoes)
	if a.VoiceMode != nil {
		s.Answers.VoiceMode = a.VoiceMode
	}
	if a.CanProactiveMessage != nil {
		s.Answers.CanProactiveMessage = a.CanProactiveMessage
	}
}

func mergeStr(dst *string, v string) {
	if strings.TrimSpace(v) != "" {
		*dst = strings.TrimSpace(v)
	}
}

func mergeSlice(dst, add []string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, d := range dst {
		seen[d] = struct{}{}
	}
	for _, v := range add {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		dst = append(dst, v)
	}
	return dst
}

func (s *Session) refreshDraft() error {
	draft, err := ExtractDraft(s.Answers)
	if err != nil {
		return err
	}
	s.DraftAgentMD = draft.AgentMD
	s.Preferences = draft.Preferences
	return nil
}

func (s *Session) question(step Step) Transition {
	return Transition{Content: string(step), StateDelta: s.state("onboarding_step", string(step))}
}

func (s *Session) currentPrompt() (Transition, bool) {
	switch s.Step {
	case StepIdentity, StepWork, StepProjects, StepSocial, StepStyle:
		return s.question(s.Step), true
	case StepDraft:
		if strings.TrimSpace(s.DraftAgentMD) == "" {
			return Transition{}, false
		}
		return s.draft("Draft ready. Confirm, edit, or skip."), true
	default:
		return Transition{}, false
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

func boolValue(v *bool) bool {
	return v != nil && *v
}

func shouldApplyBeforePrompt(in Input) bool {
	return in.Intent == IntentSkip || in.Intent == IntentCancel || in.Intent == IntentRestart
}
