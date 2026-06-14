# Richer Personal Onboarding + Standard Agent.md — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Aura's skeleton Telegram onboarding (name + lumped prefs, substring-parsed) with a ~5-prompt interview whose free-text answers are LLM-extracted into a standardized 8-section `Agent.md`.

**Architecture:** Phase A only (passive conversation-mining is deferred to Phase B). Three layers change: (1) `internal/profile` renders 8 sections; (2) `internal/onboarding` holds the richer `Answers`, the 5-step session machine, the draft renderer, and a new `AnswerExtractor` seam (fake for tests, an `llm.Client`-backed impl for production mirroring `reasoningOracle`); (3) `internal/channels/telegram` + `cmd/aura` wire the extractor and the Italian per-step prompts. The session stays a pure state machine; LLM extraction happens in the channel layer before queueing answers.

**Tech Stack:** Go 1.26, `gopkg.in/telebot.v4`, the project's `internal/llm` streaming client, `internal/agent/workflow` loop. Tests are table-driven with goleak + `-race`; the LLM is faked in unit tests.

**Spec:** [docs/superpowers/specs/2026-06-14-personal-onboarding-agentmd-design.md](../specs/2026-06-14-personal-onboarding-agentmd-design.md)

**Project conventions (apply to every commit):**
- After each Go edit: `go vet ./...`, `go build ./...`, `go test ./internal/<pkg>/`, `go test -race ./internal/<pkg>/`.
- Files ≤600 LOC; refactor-on-touch.
- Commit messages end with: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Run Go test commands in WSL or via the project toolchain; on Windows prefix `-race` with `BASH_ENV=~/.aura-toolchain.sh`.

---

## Canonical types (defined once, referenced by every task)

These names are fixed; later tasks must match them exactly.

```go
// internal/profile/render.go
type AgentContent struct {
	Identity           []string
	ExpertiseTools     []string
	ProjectsGoals      []string
	Interests          []string
	People             []string
	Style              []string
	Vetoes             []string
	CustomInstructions []string
}

// internal/profile/store.go — Preferences gains one field
//   Location string `json:"location,omitempty"`

// internal/onboarding/session.go
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

	// style/preferences (existing)
	Lang                string
	Timezone            string
	TonePreference      string
	ResponseLength      string
	VoiceMode           *bool
	CanProactiveMessage *bool
	CustomInstructions  string
}

const (
	StepIdentity Step = "identity" // name, role, company, location, timezone, languages
	StepWork     Step = "work"     // expertise, stack/tools
	StepProjects Step = "projects" // current projects, goals
	StepSocial   Step = "social"   // interests, key people
	StepStyle    Step = "style"    // tone, length, language, voice (buttons)
	StepDraft    Step = "draft"    // confirm / edit / skip
)

// internal/onboarding/extractor_llm.go
type AnswerExtractor interface {
	Extract(ctx context.Context, step Step, raw string) (Answers, error)
}
```

Interview order: `Identity → Work → Projects → Social → Style → Draft`.

---

## Task 1: Render 8-section Agent.md (`internal/profile`)

**Files:**
- Modify: `internal/profile/render.go` (`AgentContent`, `RenderAgentMD`)
- Modify: `internal/profile/store.go` (`Preferences.Location`)
- Test: `internal/profile/render_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/profile/render_test.go`:

```go
func TestRenderAgentMD_EightSections(t *testing.T) {
	got := RenderAgentMD(AgentContent{
		Identity:           []string{"Preferred name: Davide", "Role: dev @ Aura"},
		ExpertiseTools:     []string{"Stack: Go, Neo4j"},
		ProjectsGoals:      []string{"Aura personal assistant"},
		Interests:          []string{"AI agents"},
		People:             []string{"Andrea — business partner"},
		Style:              []string{"Reply language: Italian"},
		Vetoes:             []string{"No em-dashes"},
		CustomInstructions: []string{"Be concise"},
	})
	for _, want := range []string{
		"## Identity\n- Preferred name: Davide",
		"## Expertise & Tools\n- Stack: Go, Neo4j",
		"## Projects & Goals\n- Aura personal assistant",
		"## Interests\n- AI agents",
		"## People\n- Andrea — business partner",
		"## Style\n- Reply language: Italian",
		"## Vetoes\n- No em-dashes",
		"## Custom Instructions\n- Be concise",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderAgentMD missing %q in:\n%s", want, got)
		}
	}
	// Stable section order.
	if i, j := strings.Index(got, "## Identity"), strings.Index(got, "## Expertise & Tools"); i == -1 || j == -1 || i > j {
		t.Errorf("section order wrong: Identity at %d, Expertise at %d", i, j)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/profile/ -run TestRenderAgentMD_EightSections`
Expected: FAIL — `AgentContent` has no field `Identity` / build error.

- [ ] **Step 3: Implement the 8-section content + renderer**

In `internal/profile/render.go`, replace `AgentContent` and `RenderAgentMD`:

```go
// AgentContent is the structured form rendered into Agent.md.
type AgentContent struct {
	Identity           []string
	ExpertiseTools     []string
	ProjectsGoals      []string
	Interests          []string
	People             []string
	Style              []string
	Vetoes             []string
	CustomInstructions []string
}

// RenderAgentMD renders Agent.md with a stable section order.
func RenderAgentMD(c AgentContent) string {
	var b strings.Builder
	b.WriteString("# Agent.md\n")
	writeSection(&b, "Identity", c.Identity)
	writeSection(&b, "Expertise & Tools", c.ExpertiseTools)
	writeSection(&b, "Projects & Goals", c.ProjectsGoals)
	writeSection(&b, "Interests", c.Interests)
	writeSection(&b, "People", c.People)
	writeSection(&b, "Style", c.Style)
	writeSection(&b, "Vetoes", c.Vetoes)
	writeSection(&b, "Custom Instructions", c.CustomInstructions)
	return b.String()
}
```

`writeSection`, `AddFact`, the byte-cap helpers, and `RenderContextBlock` are unchanged. `AddFact` still targets `## Facts`; update its fallback render call to seed `## Identity` instead:

```go
// in AddFact, the len(lines)==0 branch:
out := RenderAgentMD(AgentContent{Identity: []string{fact}})
```

And change the heading `AddFact` defaults to when no section exists from `"Facts"` to `"Identity"` (the `factsAt == -1` branch inserts `"## Identity"`). Phase B will pass an explicit section; for now Identity is the safe default.

- [ ] **Step 4: Add `Location` to `Preferences`**

In `internal/profile/store.go`, add to the `Preferences` struct (after `Timezone`):

```go
	Location string `json:"location,omitempty"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/profile/ -run 'TestRenderAgentMD|TestAddFact'`
Expected: PASS. Then `go vet ./internal/profile/ && go build ./...`.

If pre-existing `render_test.go` / `store_test.go` cases referenced the old `Facts/Preferences/Context` fields, update them to the new field names (these are broken tests, not behavior changes — note it in the commit body).

- [ ] **Step 6: Commit**

```bash
git add internal/profile/render.go internal/profile/store.go internal/profile/render_test.go internal/profile/store_test.go
git commit -m "feat(profile): render 8-section Agent.md + Preferences.Location"
```

---

## Task 2: Expand `Answers` + 5-step session machine (`internal/onboarding`)

**Files:**
- Modify: `internal/onboarding/session.go` (`Answers`, `Step` consts, transitions, prompts, `mergeAnswers`)
- Test: `internal/onboarding/session_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/onboarding/session_test.go`:

```go
func TestSession_FiveStepFlow(t *testing.T) {
	s := NewSession("id1", "Davide")
	if s.Step != StepIdentity {
		t.Fatalf("first step = %q, want %q", s.Step, StepIdentity)
	}
	steps := []Step{StepIdentity, StepWork, StepProjects, StepSocial, StepStyle}
	for i, want := range steps {
		if s.Step != want {
			t.Fatalf("step %d = %q, want %q", i, s.Step, want)
		}
		out, err := s.Apply(Input{Intent: IntentAnswer, Answers: Answers{Name: "Davide"}})
		if err != nil {
			t.Fatalf("apply step %d: %v", i, err)
		}
		if i < len(steps)-1 && out.Terminal {
			t.Fatalf("step %d unexpectedly terminal", i)
		}
	}
	if s.Step != StepDraft || s.Status != StatusDraft {
		t.Fatalf("after style: step=%q status=%q, want draft/draft", s.Step, s.Status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/onboarding/ -run TestSession_FiveStepFlow`
Expected: FAIL — `StepIdentity`/`StepWork`/… undefined.

- [ ] **Step 3: Replace the Step constants**

In `internal/onboarding/session.go`, replace the `StepName`/`StepPreferences`/`StepDraft` block:

```go
const (
	StepIdentity Step = "identity"
	StepWork     Step = "work"
	StepProjects Step = "projects"
	StepSocial   Step = "social"
	StepStyle    Step = "style"
	StepDraft    Step = "draft"
)
```

- [ ] **Step 4: Expand the `Answers` struct**

Replace the `Answers` struct with the canonical definition (see "Canonical types" above).

- [ ] **Step 5: Rewrite transitions + prompts + merge**

`NewSession` starts at `StepIdentity`:

```go
func NewSession(identityID, identityName string) *Session {
	return &Session{IdentityID: identityID, IdentityName: identityName, Step: StepIdentity, Status: StatusActive}
}
```

Replace `applyAnswer` with a linear advance, refreshing the draft after the last collecting step:

```go
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
```

Replace `questionName`/`questionPreferences` with a single `question(step)` (English/neutral Content; the Telegram layer renders the Italian user-facing text per step):

```go
func (s *Session) question(step Step) Transition {
	return Transition{Content: string(step), StateDelta: s.state("onboarding_step", string(step))}
}
```

Update `currentPrompt` and `IntentRestart` to use the new first step:

```go
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
```

Replace the `IntentRestart` branch body in `Apply` and the `questionName()` call there with `s.question(StepIdentity)`.

Rewrite `mergeAnswers` to merge every field (scalars replace when non-empty; slices append-dedup; the Step-name special-case now targets `StepIdentity`):

```go
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
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/onboarding/ -run TestSession_FiveStepFlow`
Expected: PASS. Fix any pre-existing `session_*_test.go` cases that referenced `StepName`/`StepPreferences` (broken tests — update to new steps; note in commit body). Then `go vet ./internal/onboarding/`.

- [ ] **Step 7: Commit**

```bash
git add internal/onboarding/session.go internal/onboarding/session_test.go internal/onboarding/session_edge_test.go
git commit -m "feat(onboarding): 5-step interview machine + richer Answers"
```

---

## Task 3: Render the 8 sections from `Answers` (`ExtractDraft`)

**Files:**
- Modify: `internal/onboarding/extractor.go` (`ExtractDraft`, section builders)
- Test: `internal/onboarding/extractor_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/onboarding/extractor_test.go`:

```go
func TestExtractDraft_RichSections(t *testing.T) {
	d, err := ExtractDraft(Answers{
		Name: "Davide", Role: "dev", Company: "Aura", Timezone: "Europe/Rome", Lang: "it",
		Stack: []string{"Go", "Neo4j"}, Projects: []string{"Aura"}, Goals: []string{"ship Phase 14"},
		Interests: []string{"AI agents"}, People: []string{"Andrea — business partner"},
		Vetoes: []string{"No em-dashes"}, TonePreference: "direct", ResponseLength: "short",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Identity\n- Name: Davide",
		"Role: dev @ Aura",
		"## Expertise & Tools\n- Stack: Go, Neo4j",
		"## Projects & Goals",
		"- Aura",
		"## People\n- Andrea — business partner",
		"## Style",
		"## Vetoes\n- No em-dashes",
	} {
		if !strings.Contains(d.AgentMD, want) {
			t.Errorf("draft missing %q:\n%s", want, d.AgentMD)
		}
	}
	if d.Preferences.Lang != "it" || d.Preferences.Timezone != "Europe/Rome" {
		t.Errorf("prefs not carried: %+v", d.Preferences)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/onboarding/ -run TestExtractDraft_RichSections`
Expected: FAIL — draft only emits old Facts/Preferences/Context.

- [ ] **Step 3: Implement the section builders**

In `internal/onboarding/extractor.go`, rewrite `ExtractDraft`'s `RenderAgentMD` call and the line builders:

```go
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
```

Add `Location` to the `Preferences` literal in `ExtractDraft`:

```go
prefs := profile.Preferences{
	Lang:                answers.Lang,
	Timezone:            answers.Timezone,
	Location:            answers.Location,
	VoiceMode:           boolValue(answers.VoiceMode),
	CanProactiveMessage: boolValue(answers.CanProactiveMessage),
	TonePreference:      answers.TonePreference,
	ResponseLength:      answers.ResponseLength,
}
```

Add the builders (replace `factLines`/`preferenceLines`; keep `customInstructionLines`, `languagePreference`, `truncateBytes`, `cleanField`):

```go
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
```

Extend `cleanAnswers` to also clean `Role`, `Company`, `Location` (the existing per-field clean) — slices are bounded by the extractor.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/onboarding/ -run TestExtractDraft`
Expected: PASS. Then `go vet ./internal/onboarding/`.

- [ ] **Step 5: Commit**

```bash
git add internal/onboarding/extractor.go internal/onboarding/extractor_test.go internal/onboarding/extractor_edge_test.go
git commit -m "feat(onboarding): render 8 Agent.md sections from Answers"
```

---

## Task 4: `AnswerExtractor` seam + fake + LLM impl (`internal/onboarding`)

**Files:**
- Create: `internal/onboarding/extractor_llm.go`
- Test: `internal/onboarding/extractor_llm_test.go`

- [ ] **Step 1: Write the failing test (fake client + JSON parse)**

Create `internal/onboarding/extractor_llm_test.go`:

```go
package onboarding

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// fakeClient streams a fixed JSON body as one Text chunk.
type fakeClient struct{ body string }

func (f fakeClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Text: f.body, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func TestLLMAnswerExtractor_Work(t *testing.T) {
	ex := NewLLMAnswerExtractor(fakeClient{body: `{"expertise":["backend"],"stack":["Go","Neo4j"]}`}, "m")
	got, err := ex.Extract(context.Background(), StepWork, "sono un dev backend, uso Go e Neo4j")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Stack) != 2 || got.Stack[0] != "Go" {
		t.Errorf("stack = %v", got.Stack)
	}
	if len(got.Expertise) != 1 || got.Expertise[0] != "backend" {
		t.Errorf("expertise = %v", got.Expertise)
	}
}

func TestLLMAnswerExtractor_BadJSONFallsBack(t *testing.T) {
	ex := NewLLMAnswerExtractor(fakeClient{body: "not json"}, "m")
	got, err := ex.Extract(context.Background(), StepProjects, "lavoro su Aura")
	if err != nil {
		t.Fatalf("fallback should not error: %v", err)
	}
	// Fail-soft: raw text lands in the step's primary slice.
	if len(got.Projects) != 1 || got.Projects[0] != "lavoro su Aura" {
		t.Errorf("fallback projects = %v", got.Projects)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/onboarding/ -run TestLLMAnswerExtractor`
Expected: FAIL — `NewLLMAnswerExtractor` undefined.

- [ ] **Step 3: Implement the extractor**

Create `internal/onboarding/extractor_llm.go`:

```go
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
// mirroring the reasoningOracle pattern (ToolChoice="none", reasoning disabled,
// drain the stream, parse JSON). It never returns a hard error: on transport or
// parse failure it falls back to storing the raw answer in the step's primary
// field, so onboarding never blocks (the channel surfaces a WARN).
type LLMAnswerExtractor struct {
	client llm.Client
	model  string
}

func NewLLMAnswerExtractor(client llm.Client, model string) *LLMAnswerExtractor {
	return &LLMAnswerExtractor{client: client, model: model}
}

// extractDTO is the strict JSON the model returns; only step-relevant fields are
// expected to be populated. Slice fields tolerate the model emitting [] or null.
type extractDTO struct {
	Role      string   `json:"role"`
	Company   string   `json:"company"`
	Location  string   `json:"location"`
	Timezone  string   `json:"timezone"`
	Lang      string   `json:"lang"`
	Expertise []string `json:"expertise"`
	Stack     []string `json:"stack"`
	Projects  []string `json:"projects"`
	Goals     []string `json:"goals"`
	Interests []string `json:"interests"`
	People    []string `json:"people"`
}

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
		Role: dto.Role, Company: dto.Company, Location: dto.Location, Timezone: dto.Timezone, Lang: dto.Lang,
		Expertise: dto.Expertise, Stack: dto.Stack, Projects: dto.Projects, Goals: dto.Goals,
		Interests: dto.Interests, People: dto.People,
	}, nil
}

// extractJSON trims a fenced ```json block if the model wrapped its output.
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
// failed extraction still records something the user said.
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
// returns JSON only). Few-shot-free: the field list + "JSON only" is enough for a
// strong model and keeps the call cheap.
func extractSystemPrompt(step Step) string {
	const base = "You extract structured profile facts from one onboarding answer. " +
		"Reply with a SINGLE JSON object and nothing else. Use empty string/array for unknown fields. " +
		"Keep values short; preserve the user's language. "
	switch step {
	case StepIdentity:
		return base + `Fields: {"role":"","company":"","location":"","timezone":"","lang":""}.`
	case StepWork:
		return base + `Fields: {"expertise":[],"stack":[]}.`
	case StepProjects:
		return base + `Fields: {"projects":[],"goals":[]}.`
	case StepSocial:
		return base + `Fields: {"interests":[],"people":[]}. For people use "Name — role".`
	default:
		return base + `Fields: {}.`
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/onboarding/ -run TestLLMAnswerExtractor`
Expected: PASS. Then `go vet ./internal/onboarding/ && go build ./...`.

- [ ] **Step 5: Commit**

```bash
git add internal/onboarding/extractor_llm.go internal/onboarding/extractor_llm_test.go
git commit -m "feat(onboarding): LLM AnswerExtractor seam (fail-soft, tool-free one-shot)"
```

---

## Task 5: Wire extractor + Italian prompts into Telegram (`internal/channels/telegram`)

**Files:**
- Modify: `internal/channels/telegram/profile_onboarding.go` (extractor field, `handleText`, `replyFromEvent`, delete `answersFromText`/`answersForStep`)
- Modify: `internal/channels/telegram/bot.go` (`Deps.AnswerExtractor`)
- Test: `internal/channels/telegram/profile_onboarding_test.go` (existing) + new case

- [ ] **Step 1: Write the failing test**

Add to a telegram onboarding test file (offline, fake extractor + fake account resolver + temp profile store):

```go
func TestProfileOnboarding_RichInterviewWritesProfile(t *testing.T) {
	dir := t.TempDir()
	store := profile.NewStore(dir)
	accounts := fakeAccounts{acct: Account{IdentityID: "id1", Username: "davide"}}
	fakeEx := fakeExtractor{} // returns canned Answers per step (see Step 3)
	p := newProfileOnboarding(store, accounts)
	p.extractor = fakeEx

	chatID, uid := int64(1), int64(1)
	// maybeStart asks Identity; then 5 free-text answers drive to draft.
	if _, ok := p.maybeStart(context.Background(), chatID, uid); !ok {
		t.Fatal("maybeStart should start onboarding for a profile-less identity")
	}
	for _, ans := range []string{"Davide, dev @ Aura, Roma", "Go e Neo4j", "Aura", "AI; Andrea partner", "diretto, breve"} {
		p.handleText(context.Background(), chatID, ans)
	}
	// Confirm writes the profile.
	p.handleCallback(context.Background(), chatID, profileCallbackData(chatID, profileActionConfirm))

	got, err := store.ReadProfile("id1")
	if err != nil {
		t.Fatalf("profile not written: %v", err)
	}
	if !strings.Contains(got.AgentMD, "## Projects & Goals") || !strings.Contains(got.AgentMD, "## People") {
		t.Errorf("Agent.md missing rich sections:\n%s", got.AgentMD)
	}
}
```

(Reuse or add `fakeAccounts`/`fakeExtractor` doubles in the test file; `fakeExtractor.Extract` returns `Answers` keyed by `step`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/channels/telegram/ -run TestProfileOnboarding_RichInterview`
Expected: FAIL — `p.extractor` field undefined.

- [ ] **Step 3: Add the extractor field + use it in `handleText`**

In `profile_onboarding.go`, add the field and constructor wiring:

```go
type profileOnboarding struct {
	store     *profile.Store
	accounts  profileAccountResolver
	extractor profileflow.AnswerExtractor

	mu       sync.Mutex
	sessions map[int64]*profileSession
}

func newProfileOnboarding(store *profile.Store, accounts profileAccountResolver) *profileOnboarding {
	return &profileOnboarding{store: store, accounts: accounts, sessions: make(map[int64]*profileSession)}
}
```

In `handleText`, replace the substring parse with the extractor (fail-soft; style step stays text but the extractor returns empty for default → the raw lands in CustomInstructions only on the default branch, which Style is not — Style is button-driven via `handleCallback`, so a text answer at Style still extracts nothing structured and advances). Replace the `else` branch:

```go
	} else {
		ans := Answers{}
		if p.extractor != nil {
			if extracted, err := p.extractor.Extract(ctx, ps.session.Step, text); err == nil {
				ans = extracted
			} else {
				slog.Warn("telegram profile onboarding: extract", "step", ps.session.Step, "err", err)
			}
		}
		if ps.session.Step == profileflow.StepIdentity && ans.Name == "" {
			ans.Name = strings.TrimSpace(text)
		}
		ps.session.Queue(profileflow.Input{Intent: profileflow.IntentAnswer, Text: text, Answers: ans})
	}
```

Delete `answersForStep` and `answersFromText` entirely.

Update `replyFromEvent`'s step switch to the Italian per-step prompts:

```go
	switch profileflow.Step(step) {
	case profileflow.StepIdentity:
		return profileReply{text: "Come ti chiami, di cosa ti occupi (ruolo + azienda/team) e dove sei (fuso orario)?"}
	case profileflow.StepWork:
		return profileReply{text: "Quali sono le tue competenze principali e lo stack/strumenti che usi di solito?"}
	case profileflow.StepProjects:
		return profileReply{text: "A cosa stai lavorando in questo periodo e quali obiettivi hai?"}
	case profileflow.StepSocial:
		return profileReply{text: "Quali sono i tuoi interessi e le persone ricorrenti con cui collabori (nome + ruolo)?"}
	case profileflow.StepStyle:
		return profileReply{text: "Come preferisci che ti risponda? (tono: diretto/amichevole/formale; lunghezza: breve/normale/dettagliata; lingua; voce on/off)"}
	case profileflow.StepDraft:
		draft, _ := state["profile_draft"].(string)
		return profileReply{text: "Ecco la bozza del profilo. Puoi confermare, modificare o saltare.\n\n" + draft, markup: profileDraftMarkup(chatID)}
	}
```

- [ ] **Step 4: Add `AnswerExtractor` to `Deps` + use in `profileForDispatch`**

In `bot.go` `Deps`, add:

```go
	// AnswerExtractor parses free-text onboarding answers into structured fields.
	// Nil → onboarding records only the raw Identity name (degraded, never panics).
	AnswerExtractor profileflow.AnswerExtractor
```

(Add the `profileflow "github.com/chetto1983/aura/internal/onboarding"` import to bot.go if not present.)

In `bot_dispatch_auth.go` `profileForDispatch`, thread the extractor:

```go
func (t *Telegram) profileForDispatch() *profileOnboarding {
	if t.profile != nil {
		return t.profile
	}
	p := newProfileOnboarding(t.deps.Profile, t.accountsForDispatch())
	p.extractor = t.deps.AnswerExtractor
	return p
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/channels/telegram/ -run 'TestProfileOnboarding'`
Expected: PASS. Then `go vet ./internal/channels/telegram/ && go test -race ./internal/channels/telegram/`.

- [ ] **Step 6: Commit**

```bash
git add internal/channels/telegram/profile_onboarding.go internal/channels/telegram/bot.go internal/channels/telegram/bot_dispatch_auth.go internal/channels/telegram/profile_onboarding_test.go
git commit -m "feat(telegram): wire LLM AnswerExtractor + Italian 5-step onboarding prompts"
```

---

## Task 6: Construct the extractor at the composition root (`cmd/aura`)

**Files:**
- Modify: `cmd/aura/serve_channels.go` (`bootChannelsAndSetup` → `telegram.Deps`)
- Test: `cmd/aura/serve_test.go` (compile-level; the boot is integration-tested live)

- [ ] **Step 1: Add the extractor to the Telegram Deps literal**

In `serve_channels.go`, in the `telegram.NewChannel(telegram.Deps{...})` literal, add:

```go
		AnswerExtractor: onboarding.NewLLMAnswerExtractor(chat.client, chat.cfg.LLM.Model),
```

Add the import `onboarding "github.com/chetto1983/aura/internal/onboarding"` to `serve_channels.go`.

- [ ] **Step 2: Build + vet**

Run: `go build ./... && go vet ./...`
Expected: clean. (`chat.client` is `llm.Client`, `chat.cfg.LLM.Model` is the configured model — both already in scope at `bootChannelsAndSetup`.)

- [ ] **Step 3: Commit**

```bash
git add cmd/aura/serve_channels.go
git commit -m "feat(serve): construct onboarding LLM extractor from the shared LLM client"
```

---

## Task 7: Profile-usage behavioral rules in the system prompt

**Files:**
- Modify: the system-prompt source in `internal/agent/prompt` (locate the base/system prompt string; grep `RenderContextBlock` / `<profile:Agent.md>` consumers and the system-prompt builder)
- Test: the prompt package's existing prompt test (assert the new clauses are present)

- [ ] **Step 1: Locate the prompt**

Run: `rg -n "profile:Agent.md|system prompt|SystemPrompt|RenderContextBlock" internal/agent/prompt internal/agent`
Identify the constant/builder that emits the system message.

- [ ] **Step 2: Write the failing test**

Add to the prompt package test: assert the system prompt contains the relevance-gate + privacy clauses, e.g.:

```go
func TestSystemPrompt_ProfileUsageRules(t *testing.T) {
	p := <BuilderThatReturnsSystemPrompt>()
	for _, want := range []string{
		"only when it is relevant",
		"do not infer or surface sensitive",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}
```

- [ ] **Step 3: Add the clauses (English, per project rule)**

Append to the system prompt, near the Agent.md/profile usage guidance:

```
The user profile in <profile:Agent.md> is background context. Apply it silently and
only when it is relevant to the request; never announce or recite it. Do not infer or
surface sensitive attributes (health, religion, ethnicity, sexual orientation,
political affiliation, financial or legal status) unless the user raises them. An
explicit in-message language request overrides the profile's preferred language.
```

- [ ] **Step 4: Run tests + vet**

Run: `go test ./internal/agent/prompt/ -run TestSystemPrompt_ProfileUsageRules && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/prompt/<file>.go internal/agent/prompt/<file>_test.go
git commit -m "feat(prompt): relevance-gate + privacy rules for Agent.md profile usage"
```

---

## Task 8: Full-suite gate + live re-test

**Files:** none (verification)

- [ ] **Step 1: Vet, build, race, coverage across touched packages**

```bash
go vet ./...
go build ./...
go test -race ./internal/profile/ ./internal/onboarding/ ./internal/channels/telegram/
go test ./internal/profile/ ./internal/onboarding/ -cover   # expect ≥85%
```

- [ ] **Step 2: Rebuild the daemon + live `/onboard` re-test**

The daemon may already be running from the session. Rebuild + restart:

```bash
# Windows host:
go build -o D:/tmp/aura-tglive.exe ./cmd/aura
# kill the old daemon (PowerShell): Get-Process *aura-tglive* | Stop-Process -Force
set -a; source /d/tmp/aura-env.sh; set +a; /d/tmp/aura-tglive.exe serve > /d/tmp/aura-tg-serve.log 2>&1 &
```

In Telegram (@DavMar1983_Bot, already linked): send `/onboard`, answer the 5 Italian prompts naturally (e.g. *"Davide, sviluppatore @ Aura, Roma"* / *"Go, Neo4j, Postgres"* / *"sto costruendo Aura, obiettivo: chiudere Phase 14"* / *"AI agents; Andrea è il mio socio per le vendite"* / tap the style buttons), then tap **Conferma**.

- [ ] **Step 3: Assert ground truth on disk (not the reply text)**

```bash
cat ~/.aura/agents/00000000-0000-0000-0000-000000000001/Agent.md
```

Expected: populated `## Identity` (name + role @ company + location/tz), `## Expertise & Tools`, `## Projects & Goals`, `## Interests`, `## People` (Andrea), `## Style`. Confirm `metadata.json` has `"onboarding_completed": true` and `preferences.json` carries lang/tz/tone/length/location.

- [ ] **Step 4: Final commit (docs/state)**

Update the spec's status to "implemented (Phase A)"; flip ROADMAP/quality-snapshot per CLAUDE.md if treating this as the Phase 14 close. Commit.

```bash
git add docs/superpowers/specs/2026-06-14-personal-onboarding-agentmd-design.md
git commit -m "docs: mark Phase A onboarding enrichment implemented + live-verified"
```

---

## Self-review (run before execution)

- **Spec coverage:** §5 (8 sections)→T1/T3; §6 (data model)→T1/T2/T3; §7 (5-prompt interview + LLM extraction)→T2/T4/T5; §8 (profile-usage rules)→T7; §10 (testing + live)→T8; Phase B (§9) intentionally out of scope. ✓
- **Type consistency:** `AgentContent` fields, `Answers` fields, `Step` consts, `AnswerExtractor.Extract` signature, `NewLLMAnswerExtractor(client, model)` are used identically across T1–T6. ✓
- **No placeholders:** every code step shows real code; the only `<…>` are in Task 7 (the prompt file path + builder name) because the exact prompt-source file must be located by grep at execution — Step 1 of T7 does that location explicitly. ✓
- **Risks:** LLM extraction adds ≤4 small calls/onboarding (fail-soft); old `Facts/Preferences/Context` tests in `profile`/`onboarding` must be updated to the new fields (broken-test exception, noted per task).
