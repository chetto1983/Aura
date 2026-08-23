// scope.go holds the approval-scope vocabulary (amendment #127): the three durations an
// operator can attach to an accept, the subject a duration is keyed on, and the
// server-generated option labels that carry the choice out to the operator and back.
//
// The labels are generated HERE, next to the question, for the same reason the question is
// (CR-01): the model relays them, it does not author them. An answer that matches no label
// resolves to ScopeOnce — the narrowest grant, never the widest.
package gateway

import (
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/approvalgrants"
)

// ApprovalScope is how long an operator's accept lasts.
type ApprovalScope string

const (
	// ScopeOnce authorizes exactly this call: the ledger entry is consumed by the
	// re-drive and the next call is withheld again. It is the pre-#127 behaviour and
	// the fallback for any answer the label table does not recognise.
	ScopeOnce ApprovalScope = "once"
	// ScopeSession authorizes the subject for the rest of the conversation. It lives in
	// memory beside the one-shot ledger, so it dies with EvictSession and with the process.
	ScopeSession ApprovalScope = "session"
	// ScopeAlways authorizes the subject for the identity until an explicit revoke. It is
	// the only scope that outlives the process, and the only one that needs a durable row.
	ScopeAlways ApprovalScope = "always"
)

// grantSubject is what a session/always grant is keyed on. Action is the verb of an
// action-multiplexed tool and is empty for every other tool, so a grant on
// "calendar delete_event" never covers "calendar send_email" (amendment #127).
type grantSubject struct {
	Tool   string
	Action string
}

// String renders the subject the way the operator reads it in an option label. It delegates
// to approvalgrants.Subject, the renderer the cockpit's revoke list also uses, so the label
// an operator approved and the label they later revoke cannot drift apart.
func (s grantSubject) String() string { return approvalgrants.Subject(s.Tool, s.Action) }

// subjectFor derives the grant subject from the spec and the model's own arguments. The
// action is read ONLY for a tool registered in multiplexedClassifiers — the same map
// classify dispatches through, so the set of tools whose verb matters cannot drift between
// classification and granting. An unparseable or action-less payload yields a tool-only
// subject, which is narrower than nothing and never wider than the call itself.
func subjectFor(spec tools.Spec, rawArgs json.RawMessage) grantSubject {
	if _, multiplexed := multiplexedClassifiers[spec.Name]; !multiplexed {
		return grantSubject{Tool: spec.Name}
	}
	var a struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(rawArgs, &a); err != nil {
		return grantSubject{Tool: spec.Name}
	}
	return grantSubject{Tool: spec.Name, Action: a.Action}
}

// scopeLabel pairs an operator-visible label with the scope it grants.
type scopeLabel struct {
	Label string
	Scope ApprovalScope
}

// scopeLabels is the server-generated label→scope table for one subject, narrowest first.
// It is the SINGLE source for both the options the operator is shown and the mapping the
// resume hook reads back, so a label can never be offered under one meaning and honoured
// under another.
func scopeLabels(s grantSubject) []scopeLabel {
	return []scopeLabel{
		{Label: "Approve once", Scope: ScopeOnce},
		{Label: fmt.Sprintf("Approve %s for this conversation", s), Scope: ScopeSession},
		{Label: fmt.Sprintf("Always approve %s", s), Scope: ScopeAlways},
	}
}

// scopeOptionPrefix marks an option VALUE as a gateway scope choice. The value is a
// semantic code, never display text — the same split internal/runner's ResolveOutcome
// already uses ("the runner is not locale-aware, so each surface renders its own copy").
// Two things fall out of it: the cockpit and Telegram can render the choice in the
// operator's language, and the string that travels the wire is stable, so a resume matches
// on a code rather than on prose a model might reword.
const scopeOptionPrefix = "gateway_scope:"

// ScopeOption is one relayed choice: the operator-visible fallback label, and the stable
// value the answer comes back as.
type ScopeOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// scopeOptionValue encodes a scope and its subject into the wire value.
// "gateway_scope:<scope>:<subject>" — a surface splits on the first two colons and
// localizes the rest. Neither a tool name nor a multiplexed action contains a colon, so the
// subject is the whole remainder and needs no escaping.
func scopeOptionValue(scope ApprovalScope, s grantSubject) string {
	return scopeOptionPrefix + string(scope) + ":" + s.String()
}

// scopeOptions is the option list routeApprove hands the model to relay verbatim into
// ask_user's options. ask_user accepts 2-4 distinct entries; these are three, distinct by
// construction. The English label is the fallback for a surface that does not localize —
// it is never the thing matched on resume.
func scopeOptions(s grantSubject) []ScopeOption {
	entries := scopeLabels(s)
	out := make([]ScopeOption, len(entries))
	for i, e := range entries {
		out[i] = ScopeOption{Label: e.Label, Value: scopeOptionValue(e.Scope, s)}
	}
	return out
}

// scopeForAnswer maps the operator's answer back to a scope. It matches the subject's OWN
// option values first — the stable, locale-free codes — and falls back to the English label
// table for a surface that submitted the fallback text. An empty, unknown, or reworded
// answer is ScopeOnce: an accept still stands (the operator did accept), but it authorizes
// only the call in front of them.
//
// Matching the value against THIS subject's own encoding is what stops a replay: a code
// minted for "calendar delete_event" does not equal the one for "skill_manage delete", so
// an answer lifted from another approval grants nothing here.
func scopeForAnswer(s grantSubject, answer string) ApprovalScope {
	for _, e := range scopeLabels(s) {
		if answer == scopeOptionValue(e.Scope, s) || answer == e.Label {
			return e.Scope
		}
	}
	return ScopeOnce
}
