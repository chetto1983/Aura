package main

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identityctx"
)

func (r *signInRuntime) ImageState(ctx context.Context) (agui.SignInImageState, error) {
	if identityctx.IdentityID(ctx) == "" {
		return agui.SignInImageState{}, errors.New("sign-in needs an Aura identity")
	}
	r.pullMu.Lock()
	state, closed := r.pullState, r.closed
	r.pullMu.Unlock()
	if closed {
		return agui.SignInImageState{}, errors.New("sign-in is shutting down")
	}
	if state.Pulling {
		return state, nil
	}
	present, err := r.browser.preflight(ctx)
	state.Present = present
	if err != nil {
		state.Reason = "Download the sign-in browser, or use your own browser"
		if !errors.Is(err, errSignInImageMissing) {
			state.Error = "Could not prepare the sign-in browser; use your own browser"
		}
		if errors.Is(err, errSignInDiskLow) {
			state.Error = errSignInDiskLow.Error()
		}
	}
	if present {
		state.Error, state.Reason = "", ""
	}
	return state, nil
}

func (r *signInRuntime) PullImage(ctx context.Context) (agui.SignInImageState, error) {
	if identityctx.IdentityID(ctx) == "" {
		return agui.SignInImageState{}, errors.New("sign-in needs an Aura identity")
	}
	if err := ctx.Err(); err != nil {
		return agui.SignInImageState{}, err
	}
	state, err := r.ImageState(ctx)
	if err != nil || state.Present || state.Pulling {
		return state, err
	}
	// A previous failed pull can be retried; disk/daemon preflight errors cannot.
	_, err = r.browser.preflight(ctx)
	if !errors.Is(err, errSignInImageMissing) {
		return state, nil
	}
	r.pullMu.Lock()
	defer r.pullMu.Unlock()
	if r.closed {
		return agui.SignInImageState{}, errors.New("sign-in is shutting down")
	}
	if r.pullState.Pulling {
		return r.pullState, nil
	}
	if r.pullState.Present {
		return r.pullState, nil
	}
	if err = r.root.Err(); err != nil {
		return agui.SignInImageState{}, err
	}
	r.pullState = agui.SignInImageState{Pulling: true}
	r.pullWorkers.Add(1)
	go r.pullImage(identityctx.WithIdentityID(r.root, identityctx.IdentityID(ctx)))
	return r.pullState, nil
}

func (r *signInRuntime) pullImage(ctx context.Context) {
	defer r.pullWorkers.Done()
	err := r.browser.router.PullSignInImage(ctx, r.browser.image, func(done, total int64) {
		r.pullMu.Lock()
		r.pullState.Done, r.pullState.Total = done, total
		r.pullMu.Unlock()
	})
	r.pullMu.Lock()
	defer r.pullMu.Unlock()
	r.pullState.Pulling = false
	r.pullState.Present = err == nil
	if err != nil {
		r.pullState.Error = "The sign-in browser download failed; retry or use your own browser"
	}
}

func (r *signInRuntime) Close() error {
	r.pullMu.Lock()
	r.closed = true
	if r.stop != nil {
		r.stop()
	}
	r.pullMu.Unlock()
	r.pullWorkers.Wait()
	if r.login != nil {
		return r.login.Close()
	}
	return nil
}
