package identity

import (
	"context"
	"strings"
)

// Authorizer is the identity read-side needed by outer layers that must gate
// a capability without depending on a concrete store.
type Authorizer interface {
	Authorize(context.Context, AuthorizeParams) (AuthorizationDecision, error)
}

// Delegator is the identity write-side needed by cron/swarm boundaries that
// need child actors whose grants are bounded by a parent actor.
type Delegator interface {
	Authorizer
	DelegateActor(context.Context, DelegateActorParams) (DelegateActorResult, error)
}

// AuthorizationDenialRecorder is implemented by the run/audit event store.
// The identity package owns the interface so it can emit metadata-only denial
// evidence without importing the concrete run store.
type AuthorizationDenialRecorder interface {
	RecordAuthorizationDenial(context.Context, AuthorizationDecision) error
}

type actorIDContextKey struct{}
type authorizerContextKey struct{}
type delegatorContextKey struct{}
type runIDContextKey struct{}
type eventIDContextKey struct{}
type authorizationDenialRecorderContextKey struct{}

func WithActorID(ctx context.Context, actorID string) context.Context {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return ctx
	}
	return context.WithValue(ctx, actorIDContextKey{}, actorID)
}

func ActorIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(actorIDContextKey{}).(string); ok {
		return v
	}
	return ""
}

func WithAuthorizer(ctx context.Context, authorizer Authorizer) context.Context {
	if authorizer == nil {
		return ctx
	}
	return context.WithValue(ctx, authorizerContextKey{}, authorizer)
}

func AuthorizerFromContext(ctx context.Context) (Authorizer, bool) {
	if v, ok := ctx.Value(authorizerContextKey{}).(Authorizer); ok && v != nil {
		return v, true
	}
	return nil, false
}

func WithDelegator(ctx context.Context, delegator Delegator) context.Context {
	if delegator == nil {
		return ctx
	}
	return context.WithValue(ctx, delegatorContextKey{}, delegator)
}

func DelegatorFromContext(ctx context.Context) (Delegator, bool) {
	if v, ok := ctx.Value(delegatorContextKey{}).(Delegator); ok && v != nil {
		return v, true
	}
	return nil, false
}

func WithAuthority(ctx context.Context, authority any) context.Context {
	if authorizer, ok := authority.(Authorizer); ok {
		ctx = WithAuthorizer(ctx, authorizer)
	}
	if delegator, ok := authority.(Delegator); ok {
		ctx = WithDelegator(ctx, delegator)
	}
	return ctx
}

func WithRunID(ctx context.Context, runID string) context.Context {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ctx
	}
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

func RunIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(runIDContextKey{}).(string); ok {
		return v
	}
	return ""
}

func EventIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(eventIDContextKey{}).(string); ok {
		return v
	}
	return ""
}

func WithAuthorizationDenialRecorder(ctx context.Context, recorder AuthorizationDenialRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, authorizationDenialRecorderContextKey{}, recorder)
}

func AuthorizationDenialRecorderFromContext(ctx context.Context) (AuthorizationDenialRecorder, bool) {
	if v, ok := ctx.Value(authorizationDenialRecorderContextKey{}).(AuthorizationDenialRecorder); ok && v != nil {
		return v, true
	}
	return nil, false
}
