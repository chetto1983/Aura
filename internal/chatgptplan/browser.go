package chatgptplan

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"maps"
	"net/url"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

// BrowserLauncher starts an owner sandbox callback listener before navigation.
type BrowserLauncher interface {
	Open(context.Context, string) (BrowserSession, error)
}

// BrowserSession keeps Chromium and its loopback callback in the same sandbox.
type BrowserSession interface {
	RedirectURI() string
	Navigate(context.Context, string) error
	Callback(context.Context) (url.Values, error)
	Close() error
}

var errBrowserStartupTimeout = errors.New("chatgpt plan: browser startup timed out; try signing in again")

type browserPending struct {
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelCauseFunc
	flow            Flow
	attempt         string
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

// BrowserLogin keeps the callback and Chromium in the same owner sandbox.
type BrowserLogin struct {
	mu             sync.Mutex
	root           context.Context
	service        *Service
	launcher       BrowserLauncher
	logger         *slog.Logger
	pending        map[string]*browserPending
	closed         chan struct{}
	watchDone      chan struct{}
	workers        sync.WaitGroup
	cleanupErr     error
	startupTimeout time.Duration
	timeToLive     time.Duration
}

// NewBrowserLogin cancels and joins pending sessions when the root lifetime ends.
func NewBrowserLogin(root context.Context, service *Service, launcher BrowserLauncher) *BrowserLogin {
	b := &BrowserLogin{root: root, service: service, launcher: launcher, logger: slog.Default(), pending: make(map[string]*browserPending), closed: make(chan struct{}), watchDone: make(chan struct{}), startupTimeout: 45 * time.Second, timeToLive: flowTTL}
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
func (b *BrowserLogin) Start(ctx context.Context, owner, attempt string) (Flow, error) {
	if err := checkOwner(ctx, owner); err != nil {
		return Flow{}, err
	}
	if attempt == "" {
		return Flow{}, errors.New("chatgpt plan: login attempt id is required")
	}
	for {
		b.mu.Lock()
		select {
		case <-b.closed:
			b.mu.Unlock()
			return Flow{}, errors.New("chatgpt plan: browser login is shutting down")
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
			session := "chatgpt-" + hex.EncodeToString(random[:])
			lifetime, expire := context.WithTimeout(identityctx.WithIdentityID(b.root, owner), b.timeToLive)
			flowCtx, cancel := context.WithCancelCause(lifetime)
			p = &browserPending{ctx: flowCtx, cancel: cancel, flow: Flow{AuthURL: "/browser/" + session, Status: "authorization_required"}, attempt: attempt, ready: make(chan struct{}), done: make(chan struct{}), cancelRequested: make(chan struct{}), finished: make(chan struct{}), invalidated: make(chan struct{})}
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
func (b *BrowserLogin) Owns(ctx context.Context, owner, session string) bool {
	if identityctx.IdentityID(ctx) != owner || checkOwner(ctx, owner) != nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.pending[owner]
	return p != nil && p.ctx.Err() == nil && p.flow.AuthURL == "/browser/"+session
}

// Cancel compares routes so a delayed popup close cannot cancel its replacement.
// The empty route is reserved for the server's disconnect operation.
func (b *BrowserLogin) Cancel(ctx context.Context, owner, authURL string) error {
	if err := checkOwner(ctx, owner); err != nil {
		return err
	}
	b.mu.Lock()
	p := b.pending[owner]
	if p == nil || (authURL != "" && authURL != p.flow.AuthURL) {
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
func (b *BrowserLogin) Close() error {
	b.stop()
	b.workers.Wait()
	<-b.watchDone
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cleanupErr
}

func (b *BrowserLogin) stop() {
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

func (b *BrowserLogin) cancelPending(p *browserPending) {
	p.cancel(context.Canceled)
	p.cancelOnce.Do(func() { close(p.cancelRequested) })
}

func (b *BrowserLogin) invalidate(owner string, p *browserPending) {
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
	b.service.cancelBrowser(owner, authorizationURL)
}

func (b *BrowserLogin) run(request context.Context, owner, session string, p *browserPending, expire context.CancelFunc) {
	stopRequest := context.AfterFunc(request, func() { p.cancel(context.Cause(request)) })
	startup := time.AfterFunc(b.startupTimeout, func() { p.cancel(errBrowserStartupTimeout) })
	var browser BrowserSession
	var authorizationURL string
	ready := false
	defer func() {
		stopRequest()
		startup.Stop()
		p.cancel(context.Canceled)
		expire()
		if browser != nil && browser.Close() != nil {
			p.closeErr = errors.New("chatgpt plan: could not close the login browser; try again")
			b.logger.Warn("ChatGPT login browser cleanup failed", "identity_id", owner, "error", p.closeErr)
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
	browser, err = b.launcher.Open(p.ctx, session)
	if err == nil && browser != nil {
		var authorization Flow
		p.mu.Lock()
		authorization, err = b.service.Start(p.ctx, owner, browser.RedirectURI())
		authorizationURL = authorization.AuthURL
		p.authURL = authorizationURL
		p.mu.Unlock()
		if err == nil {
			err = browser.Navigate(p.ctx, authorizationURL)
		}
	}
	if err != nil || browser == nil || p.ctx.Err() != nil {
		p.err = b.failed(owner, authorizationURL, p.ctx, "chatgpt plan: could not open the login browser; try signing in again")
		return
	}
	stopRequest()
	startup.Stop()
	ready = true
	close(p.ready)
	query, err := browser.Callback(p.ctx)
	if err != nil || p.ctx.Err() != nil {
		_ = b.failed(owner, authorizationURL, p.ctx, "chatgpt plan: the login browser stopped; try signing in again")
		return
	}
	if err = b.service.Callback(p.ctx, query); err != nil {
		_ = b.failed(owner, authorizationURL, p.ctx, "chatgpt plan: the sign-in callback was rejected; try signing in again")
	} else {
		p.mu.Lock()
		p.approved = true
		p.mu.Unlock()
	}
}

func (b *BrowserLogin) failed(owner, authorizationURL string, ctx context.Context, message string) error {
	if ctx.Err() != nil {
		switch {
		case errors.Is(context.Cause(ctx), errBrowserStartupTimeout):
			message = errBrowserStartupTimeout.Error()
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			message = "chatgpt plan: sign-in expired; try signing in again"
		default:
			b.service.cancelBrowser(owner, authorizationURL)
			return ctx.Err()
		}
	}
	b.service.failBrowser(owner, authorizationURL, message)
	return errors.New(message)
}
