package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/chetto1983/aura/internal/canonicaljson"
)

// ApprovedCall is an operator's accept of one parked call, as the durable approval record
// holds it. The gateway honours it in place of asking again, and folds OperatorID and Scope
// into the call's single reservation.
//
// The fingerprint is a consistency check against a hook or a bug changing the arguments
// between the accept and the run. The forgery barrier is the server-held record the accept
// was read from, never this value.
type ApprovedCall struct {
	ConversationID, ToolCallID, ToolName string
	Arguments                            json.RawMessage
	Fingerprint, OperatorID, Scope       string
}

type approvedCallsCtxKey struct{}

// WithApprovedCalls carries the accepted calls to the dispatch that runs them. It belongs on
// that dispatch's context only: a later model round that re-emits the same call id must not
// inherit the authorization.
func WithApprovedCalls(ctx context.Context, calls []ApprovedCall) context.Context {
	return context.WithValue(ctx, approvedCallsCtxKey{}, slices.Clone(calls))
}

// ApprovedCallFor returns the accepted call of the dispatch on ctx, matched on the
// tool-call context's session and call id. A dispatch without both ids matches nothing.
func ApprovedCallFor(ctx context.Context) (ApprovedCall, bool) {
	sessionID, toolCallID := SessionIDFromContext(ctx), ToolCallIDFromContext(ctx)
	if sessionID == "" || toolCallID == "" {
		return ApprovedCall{}, false
	}
	calls, _ := ctx.Value(approvedCallsCtxKey{}).([]ApprovedCall)
	for _, c := range calls {
		if c.ConversationID == sessionID && c.ToolCallID == toolCallID {
			return c, true
		}
	}
	return ApprovedCall{}, false
}

// Authorizes reports whether the accept covers a call of toolName with raw arguments.
func (c ApprovedCall) Authorizes(toolName string, raw json.RawMessage) bool {
	return c.ToolName != "" && c.ToolName == toolName && c.Fingerprint != "" && c.Fingerprint == CallFingerprint(raw)
}

// CallFingerprint is the cosmetic-insensitive fingerprint of a call's JSON arguments: two
// encodings of the same object share it, and any changed value does not.
func CallFingerprint(raw json.RawMessage) string {
	sum := sha256.Sum256(canonicaljson.CanonicalArgs(string(raw)))
	return hex.EncodeToString(sum[:])
}
