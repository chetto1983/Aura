package browserlogin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

type browserPending struct {
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelCauseFunc
	flow            Flow
	attempt         string
	options         LaunchOptions
	authorizer      Authorizer
	authURL         string
	approved        bool
	cancelRequested chan struct{}
	finished        chan struct{}
	invalidated     chan struct{}
	cancelOnce      sync.Once
	ready           chan struct{}
	done            chan struct{}
	err             error
	closeErr        error
}

// Login keeps the callback and Chromium in the same owner sandbox.
type Login struct {
	mu                sync.Mutex
	root              context.Context
	options           Options
	startupTimeoutErr error
	launcher          Launcher
	logger            *slog.Logger
	pending           map[string]*browserPending
	closed            chan struct{}
	watchDone         chan struct{}
	workers           sync.WaitGroup
	cleanupErr        error
	startupTimeout    time.Duration
	timeToLive        time.Duration
}

// New cancels and joins pending sessions when the root lifetime ends.
func New(root context.Context, launcher Launcher, options Options) *Login {
	options = defaultOptions(options)
	b := &Login{root: root, options: options, launcher: launcher, logger: slog.Default(), pending: make(map[string]*browserPending), closed: make(chan struct{}), watchDone: make(chan struct{}), startupTimeout: options.Startup, timeToLive: options.TTL, startupTimeoutErr: errors.New(options.Messages.StartupTimeout)}
	go func() {
		defer close(b.watchDone)
		select {
		case <-root.Done():
			b.stop()
		case <-b.closed:
		}
		b.workers.Wait()
	}()
	return b
}

// Start returns a live route only after callback setup and browser navigation succeed.
func (b *Login) Start(ctx context.Context, owner, attempt string, opts LaunchOptions, a Authorizer) (Flow, error) {
	if err := b.options.CheckOwner(ctx, owner); err != nil {
		return Flow{}, err
	}
	if attempt == "" {
		return Flow{}, errors.New(b.options.Messages.AttemptRequired)
	}
	for {
		b.mu.Lock()
		select {
		case <-b.closed:
			b.mu.Unlock()
			return Flow{}, errors.New(b.options.Messages.ShuttingDown)
		default:
		}
		p := b.pending[owner]
		if p != nil && (p.attempt != attempt || p.ctx.Err() != nil) {
			b.mu.Unlock()
			b.cancelPending(p)
			select {
			case <-p.done:
				continue
			case <-ctx.Done():
				return Flow{}, ctx.Err()
			}
		}
		if p == nil {
			var random [12]byte
			_, _ = rand.Read(random[:])
			session := b.options.Prefix + hex.EncodeToString(random[:])
			lifetime, expire := context.WithTimeout(identityctx.WithIdentityID(b.root, owner), b.timeToLive)
			flowCtx, cancel := context.WithCancelCause(lifetime)
			p = &browserPending{ctx: flowCtx, cancel: cancel, flow: Flow{Route: b.options.Route + session, Status: "authorization_required"}, attempt: attempt, options: opts, authorizer: a, ready: make(chan struct{}), done: make(chan struct{}), cancelRequested: make(chan struct{}), finished: make(chan struct{}), invalidated: make(chan struct{})}
			b.pending[owner] = p
			b.workers.Add(1)
			go b.invalidate(owner, p)
			go b.run(ctx, owner, session, p, expire)
		}
		b.mu.Unlock()
		select {
		case <-p.ready:
			return p.startResult()
		case <-ctx.Done():
			return Flow{}, ctx.Err()
		}
	}
}

func (p *browserPending) startResult() (Flow, error) {
	if p.err != nil {
		return Flow{}, p.err
	}
	ctxErr := p.ctx.Err()
	p.mu.Lock()
	approved := p.approved
	p.mu.Unlock()
	if approved {
		return Flow{Status: "approved"}, nil
	}
	if ctxErr != nil {
		return Flow{}, ctxErr
	}
	return p.flow, nil
}

// Owns limits live browser access to the authenticated initiating identity.
func (b *Login) Owns(ctx context.Context, owner, session string) bool {
	if identityctx.IdentityID(ctx) != owner || b.options.CheckOwner(ctx, owner) != nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.pending[owner]
	return p != nil && p.ctx.Err() == nil && p.flow.Route == b.options.Route+session
}

// Cancel compares routes so a delayed popup close cannot cancel its replacement.
// The empty route is reserved for the server's disconnect operation.
func (b *Login) Cancel(ctx context.Context, owner, authURL string) error {
	if err := b.options.CheckOwner(ctx, owner); err != nil {
		return err
	}
	b.mu.Lock()
	p := b.pending[owner]
	if p == nil || (authURL != "" && authURL != p.flow.Route) {
		p = nil
	}
	b.mu.Unlock()
	if p == nil {
		return nil
	}
	b.cancelPending(p)
	select {
	case <-p.done:
		return p.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close waits for session cleanup and the root lifetime watcher to finish.
func (b *Login) Close() error {
	b.stop()
	b.workers.Wait()
	<-b.watchDone
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cleanupErr
}

func (b *Login) stop() {
	b.mu.Lock()
	select {
	case <-b.closed:
		b.mu.Unlock()
		return
	default:
		close(b.closed)
	}
	pending := maps.Clone(b.pending)
	b.mu.Unlock()
	for _, p := range pending {
		b.cancelPending(p)
	}
}

func (b *Login) cancelPending(p *browserPending) {
	p.cancel(context.Canceled)
	p.cancelOnce.Do(func() { close(p.cancelRequested) })
}

func (b *Login) invalidate(owner string, p *browserPending) {
	defer close(p.invalidated)
	select {
	case <-p.cancelRequested:
	case <-p.finished:
		select {
		case <-p.cancelRequested:
		default:
			return
		}
	}
	p.mu.Lock()
	authorizationURL := p.authURL
	p.mu.Unlock()
	p.authorizer.Cancel(owner, authorizationURL)
}

func (b *Login) run(request context.Context, owner, session string, p *browserPending, expire context.CancelFunc) {
	stopRequest := context.AfterFunc(request, func() { p.cancel(context.Cause(request)) })
	startup := time.AfterFunc(b.startupTimeout, func() { p.cancel(b.startupTimeoutErr) })
	var browser Session
	var authorizationURL string
	ready := false
	defer func() {
		stopRequest()
		startup.Stop()
		p.cancel(context.Canceled)
		expire()
		if browser != nil && browser.Close() != nil {
			p.closeErr = errors.New(b.options.Messages.CleanupFailed)
			b.logger.Warn(b.options.Messages.CleanupLog, "identity_id", owner, "error", p.closeErr)
			b.mu.Lock()
			b.cleanupErr = p.closeErr
			b.mu.Unlock()
		}
		close(p.finished)
		<-p.invalidated
		if !ready {
			close(p.ready)
		}
		b.mu.Lock()
		if b.pending[owner] == p {
			delete(b.pending, owner)
		}
		b.mu.Unlock()
		close(p.done)
		b.workers.Done()
	}()
	var err error
	browser, err = b.launcher.Open(p.ctx, session, p.options)
	if err == nil && browser != nil {
		p.mu.Lock()
		authorizationURL, err = p.authorizer.Start(p.ctx, owner, browser.RedirectURI())
		p.authURL = authorizationURL
		p.mu.Unlock()
		if err == nil {
			err = browser.Navigate(p.ctx, authorizationURL)
		}
	}
	if err != nil || browser == nil || p.ctx.Err() != nil {
		p.err = b.failed(owner, authorizationURL, p, b.options.Messages.OpenFailed)
		return
	}
	stopRequest()
	startup.Stop()
	ready = true
	close(p.ready)
	query, err := browser.Callback(p.ctx)
	if err != nil || p.ctx.Err() != nil {
		_ = b.failed(owner, authorizationURL, p, b.options.Messages.BrowserStopped)
		return
	}
	if err = p.authorizer.Callback(p.ctx, query); err != nil {
		_ = b.failed(owner, authorizationURL, p, b.options.Messages.CallbackRejected)
	} else {
		p.mu.Lock()
		p.approved = true
		p.mu.Unlock()
	}
}

func (b *Login) failed(owner, authorizationURL string, p *browserPending, message string) error {
	ctx := p.ctx
	if ctx.Err() != nil {
		switch {
		case errors.Is(context.Cause(ctx), b.startupTimeoutErr):
			message = b.startupTimeoutErr.Error()
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			message = b.options.Messages.Expired
		default:
			p.authorizer.Cancel(owner, authorizationURL)
			return ctx.Err()
		}
	}
	p.authorizer.Fail(owner, authorizationURL, message)
	return errors.New(message)
}
