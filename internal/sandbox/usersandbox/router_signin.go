package usersandbox

import (
	"context"
	"errors"
)

type signInBackend interface {
	StartSignIn(context.Context, SignInSpec, SignInPrepare) (SignInBox, error)
	StopSignIn(context.Context, string) error
	SweepSignIns(context.Context) error
	SignInImageState(context.Context, string) (bool, error)
	PullSignInImage(context.Context, string, func(int64, int64)) error
}

var errSignInUnavailable = errors.New("sandbox backend does not support sign-in desktops")

// StartSignIn is reserved for trusted login adapters, outside agent tool routing.
func (r *SandboxRouter) StartSignIn(ctx context.Context, s SignInSpec, prepare SignInPrepare) (SignInBox, error) {
	b, ok := backendAs[signInBackend](r)
	if !ok {
		return SignInBox{}, errSignInUnavailable
	}
	return b.StartSignIn(ctx, s, prepare)
}

// StopSignIn retires the desktop and its callback session together.
func (r *SandboxRouter) StopSignIn(ctx context.Context, session string) error {
	b, ok := backendAs[signInBackend](r)
	if !ok {
		return errSignInUnavailable
	}
	return b.StopSignIn(ctx, session)
}

// SweepSignIns handles the lost in-memory states from a previous daemon lifetime.
func (r *SandboxRouter) SweepSignIns(ctx context.Context) error {
	b, ok := backendAs[signInBackend](r)
	if !ok {
		return errSignInUnavailable
	}
	return b.SweepSignIns(ctx)
}

// SignInImageState leaves a missing desktop image for the explicit download step.
func (r *SandboxRouter) SignInImageState(ctx context.Context, image string) (bool, error) {
	b, ok := backendAs[signInBackend](r)
	if !ok {
		return false, errSignInUnavailable
	}
	return b.SignInImageState(ctx, image)
}

// PullSignInImage keeps a large download outside the login startup deadline.
func (r *SandboxRouter) PullSignInImage(ctx context.Context, image string, progress func(int64, int64)) error {
	b, ok := backendAs[signInBackend](r)
	if !ok {
		return errSignInUnavailable
	}
	return b.PullSignInImage(ctx, image, progress)
}
