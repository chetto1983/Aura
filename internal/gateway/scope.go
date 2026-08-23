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

// scopeOptionLabels is the label list routeApprove hands the model to relay verbatim into
// ask_user's options. ask_user accepts 2-4 distinct labels; these are three and distinct by
// construction (each carries a different verb prefix).
func scopeOptionLabels(s grantSubject) []string {
	entries := scopeLabels(s)
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Label
	}
	return out
}

// scopeForAnswer maps the operator's answer back to a scope using the subject's OWN label
// table. An empty, unknown, or reworded answer is ScopeOnce: an accept still stands (the
// operator did accept), but it authorizes only the call in front of them.
func scopeForAnswer(s grantSubject, answer string) ApprovalScope {
	for _, e := range scopeLabels(s) {
		if answer == e.Label {
			return e.Scope
		}
	}
	return ScopeOnce
}
