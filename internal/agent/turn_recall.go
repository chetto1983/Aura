package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
)

// Effort sources a turn's decision is persisted with (migration 0137's CHECK). Only user and
// teacher decisions are reusable labels. The other four record how a turn was decided and are
// never copied, so the memory cannot reinforce its own guesses.
const (
	EffortSourceUser     = "user"
	EffortSourceTeacher  = "teacher"
	EffortSourceMemory   = "memory"
	EffortSourceSeeds    = "seeds"
	EffortSourceGreeting = "greeting"
	EffortSourceFallback = "fallback"
)

// TurnRecaller is the agent's port onto the identity's past turns. internal/agent does not
// import the memory store: the runner binds it per identity (arcadedb.Client.RecallTurns).
type TurnRecaller interface {
	RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error)
}

// TurnRecallRequest is one read of the identity's past turns for the turn being decided.
type TurnRecallRequest struct {
	Text          string
	ContextKey    string
	RouteKey      string
	PolicyVersion string
	SourceRef     string
	DeferredTools []string
	IncludeLabels bool
}

// RecalledTurn is one past user turn within the recall radius. Its fields are
// arcadedb.RecalledTurn's, in the same order: the runner converts one into the other.
type RecalledTurn struct {
	Distance        float64
	SourceRef       string
	Effort          string
	RequestedEffort string
	EffortSource    string
	ContextKey      string
	RouteKey        string
	PolicyVersion   string
	OriginRef       string
	Tools           []string
}

// TurnRecall holds each pool nearest first.
type TurnRecall struct {
	UserLabels    []RecalledTurn
	TeacherLabels []RecalledTurn
	ToolTurns     []RecalledTurn
}

// TurnReading is what the runner hands one dispatched user turn so the agent can read it
// against the identity's past turns. The zero value is what a resumed run, a branch re-run,
// a headless and a sub-agent run get: no memory, the seed bank's verdict whatever its
// margin, and never the teacher, since such a run has no row to label.
type TurnReading struct {
	Recaller TurnRecaller
	// Text is the message as typed, without the context blocks the model receives with it.
	Text string
	// ContextKey is TurnContextKey of what the model reads before this message; "" makes the
	// turn ineligible for recall (an unversioned input, or no dispatched message).
	ContextKey string
	// SourceRef is the dispatched user turn, which recall must not return to itself.
	SourceRef string
	// Standalone is true when nothing conversational precedes the message, the only case in
	// which a greeting takes the greeting fast path.
	Standalone bool
	// OnDecision receives the decision once, before the first request.
	OnDecision func(TurnDecision)
}

// TurnDecision is how the turn's effort was decided, as the runner persists it on the user
// turn (migration 0137). Effort is what is sent after the clamp, EffortRequested the
// decision before it. A composer's explicit effort is always requested, even on a route
// that takes no effort and so sends none; without one, such a route leaves both empty.
type TurnDecision struct {
	Effort          llm.ReasoningEffort
	EffortRequested llm.ReasoningEffort
	EffortSource    string
	RouteKey        string
	PolicyVersion   string
	OriginRef       string
	// AskTeacher asks the runner to have the teacher label this turn in the background, once
	// the decision is written. It is never persisted: the label it may become is.
	AskTeacher bool
}

// contextKeyFormat versions the encoding below. It is part of every key, so a change of
// format never lets an old key equal a new one.
const contextKeyFormat = "ctx1"

// TurnContextKey digests everything the model reads before the current user message: the
// system prompt, every prior message in order with its tool calls and results, and the
// context blocks the current message arrives with. The message's own text is left out so a
// paraphrase can match, and so are tool-call ids, which only pair a call with its result
// (spec 2026-10-06, "Compatibility and label provenance"). Equal context gives equal keys;
// any difference in what the model reads gives different ones.
func TurnContextKey(prior []llm.Message, currentBlocks string) string {
	type toolCall struct {
		Name      string
		Arguments string
	}
	type message struct {
		Role    string
		Content string
		Calls   []toolCall `json:",omitempty"`
	}
	encoded := struct {
		System  string
		Prior   []message
		Current string
	}{System: SystemPrompt, Prior: make([]message, 0, len(prior)), Current: currentBlocks}
	for _, m := range prior {
		msg := message{Role: m.Role, Content: m.Content}
		for _, tc := range m.ToolCalls {
			msg.Calls = append(msg.Calls, toolCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
		encoded.Prior = append(encoded.Prior, msg)
	}
	// Marshalling a struct of strings cannot fail.
	raw, _ := json.Marshal(encoded)
	sum := sha256.Sum256(raw)
	return contextKeyFormat + ":" + hex.EncodeToString(sum[:])
}

// routeKey names the route an effort is decided for: the backend and its endpoint, the
// model, and the set of efforts the model publishes, sorted so a catalogue refresh that
// reorders them does not retire every label. A label from another route is never reused.
// The API key, headers, sampling, and any userinfo or query in the base URL stay out.
func routeKey(cfg llm.Config) string {
	efforts := make([]string, len(cfg.SupportedReasoningEfforts))
	for index, effort := range cfg.SupportedReasoningEfforts {
		efforts[index] = string(effort)
	}
	slices.Sort(efforts)
	return "route1:" + digest(
		llm.ReasoningTarget(cfg.Provider, cfg.BaseURL).String(), cfg.Provider, endpointIdentity(cfg.BaseURL),
		cfg.Model, strings.Join(efforts, ","), strconv.FormatBool(cfg.ReasoningMandatory),
	)
}

func endpointIdentity(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + strings.TrimRight(parsed.Path, "/")
}

// turnPolicyVersion versions everything a label is decided under: the seed bank, its tier
// mapping, the greeting allowlist and the teacher prompt (prompt.ReasoningPolicyFingerprint),
// and the selection rules of readTurn. A label decided under another version is never reused.
var turnPolicyVersion = policyVersion(prompt.ReasoningPolicyFingerprint(), teacherMargin)

// selectionRules names readTurn's precedence; change it with the table. Since the amendment
// of 2026-10-07 the teacher decides no turn: below the margin it labels in the background.
const selectionRules = "composer>greeting>label(user>teacher)>seeds(margin)|fallback;teacher=background"

func policyVersion(fingerprint string, margin float64) string {
	return "policy1:" + digest(fingerprint, selectionRules, strconv.FormatFloat(margin, 'g', -1, 64))
}

func digest(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// LoggableSourceRef is a source ref as a log line carries it. The production log handler
// blanks every string beginning with postgres:// as a DSN, and a ref names a conversation
// and a turn with no credential, so the scheme is dropped rather than the redaction weakened.
func LoggableSourceRef(ref string) string { return strings.TrimPrefix(ref, "postgres://") }
