package main

import (
	"context"
	_ "embed"
	"net"

	"github.com/chetto1983/aura/internal/identityctx"
)

//go:embed signin-relay.mjs
var signInRelayScript []byte

func (b *sandboxSignInBrowser) Owns(ctx context.Context, owner, session string) bool {
	if owner == "" || identityctx.IdentityID(ctx) != owner || b.owns == nil || !b.owns(ctx, owner, session) {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	s := b.sessions[session]
	return s != nil && s.owner == owner
}

func (b *sandboxSignInBrowser) Origins(session string) (cockpit, stream string, ok bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s := b.sessions[session]
	if s == nil {
		return "", "", false
	}
	return s.opts.CockpitOrigin, s.opts.StreamOrigin, true
}

func (b *sandboxSignInBrowser) Dial(ctx context.Context, session string) (net.Conn, error) {
	if !b.Owns(ctx, identityctx.IdentityID(ctx), session) {
		return nil, net.ErrClosed
	}
	b.mu.RLock()
	s := b.sessions[session]
	b.mu.RUnlock()
	if s == nil {
		return nil, net.ErrClosed
	}
	s.mu.RLock()
	box := s.box
	s.mu.RUnlock()
	if box.Sidecar.ContainerID == "" {
		return nil, net.ErrClosed
	}
	return b.router.DialExec(ctx, box.Sidecar, "node /tmp/signin-relay.mjs")
}

func (b *sandboxSignInBrowser) Cancel(ctx context.Context, owner, session string) error {
	if !b.Owns(ctx, owner, session) || b.cancel == nil {
		return nil
	}
	return b.cancel(ctx, owner, "/signing-in/"+session)
}
