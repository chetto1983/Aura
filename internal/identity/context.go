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

type actorIDContextKey struct{}
type authorizerContextKey struct{}

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
