package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/browserlogin"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

var (
	signInBrowserSessionPattern = regexp.MustCompile(`^signin-[a-f0-9]{24}$`)
	errSignInDiskLow            = errors.New("sign-in needs at least 8 GiB of free disk; use your own browser")
	errSignInImageMissing       = errors.New("download the sign-in browser image first, or use your own browser")
)

type sandboxSignInBrowser struct {
	router              *usersandbox.SandboxRouter
	image, sidecarImage string
	policy              usersandbox.EgressPolicy
	mu                  sync.RWMutex
	sessions            map[string]*signInBrowserSession
	owns                func(context.Context, string, string) bool
	cancel              func(context.Context, string, string) error
}

func (b *sandboxSignInBrowser) Open(ctx context.Context, session string, opts browserlogin.LaunchOptions) (browserlogin.Session, error) {
	if !signInBrowserSessionPattern.MatchString(session) {
		return nil, errors.New("invalid sign-in browser session")
	}
	owner := identityctx.IdentityID(ctx)
	if owner == "" {
		return nil, errors.New("sign-in needs an Aura identity")
	}
	if _, err := listenerAddressFromRedirect(opts.Redirect); err != nil {
		return nil, err
	}
	// This is another Chrome, even when the owner's agent browser is already open.
	if err := b.router.AdmitBrowser(identityctx.WithIdentityID(ctx, session)); err != nil {
		return nil, err
	}
	s := &signInBrowserSession{browser: b, session: session, owner: owner, opts: opts}
	b.mu.Lock()
	if b.sessions == nil {
		b.sessions = map[string]*signInBrowserSession{}
	}
	b.sessions[session] = s
	b.mu.Unlock()
	_, err := b.preflight(ctx)
	return s, err
}

func (b *sandboxSignInBrowser) preflight(ctx context.Context) (bool, error) {
	present, err := b.router.SignInImageState(ctx, b.image)
	if err != nil {
		return false, err
	}
	if present {
		return true, nil
	}
	h, err := b.router.Route(ctx)
	if err != nil {
		return false, err
	}
	disk, err := b.router.Exec(ctx, h, usersandbox.ExecRequest{Command: "df -B1 --output=avail /", Dir: "/"})
	if err != nil || disk.ExitCode != 0 {
		return false, errors.New("could not check space for the sign-in image")
	}
	available, err := freeBytesFromDf(disk.Stdout)
	if err != nil {
		return false, err
	}
	if available < signInMinFreeBytes {
		return false, errSignInDiskLow
	}
	return false, errSignInImageMissing
}

type signInBrowserSession struct {
	browser        *sandboxSignInBrowser
	session, owner string
	opts           browserlogin.LaunchOptions
	mu             sync.RWMutex
	box            usersandbox.SignInBox
	callback       *chatGPTBrowserSession
	once           sync.Once
	closeErr       error
}

func (s *signInBrowserSession) RedirectURI() string { return s.opts.Redirect }

func (s *signInBrowserSession) Navigate(ctx context.Context, authURL string) error {
	u, err := url.Parse(authURL)
	if err != nil || u.Query().Get("state") == "" || len(u.Query()["state"]) != 1 {
		return errors.New("invalid sign-in authorization state")
	}
	spec := usersandbox.SignInSpec{Session: s.session, IdentityID: s.owner, DesktopImage: s.browser.image, SidecarImage: s.browser.sidecarImage,
		AuthURL: authURL, CockpitOrigin: s.opts.CockpitOrigin, StreamOrigin: s.opts.StreamOrigin, Egress: s.browser.policy}
	_, err = s.browser.router.StartSignIn(ctx, spec, func(ctx context.Context, box usersandbox.SignInBox) error {
		s.mu.Lock()
		s.box = box
		s.mu.Unlock()
		return s.startCallback(ctx, box, u.Query().Get("state"))
	})
	return err
}

func (s *signInBrowserSession) startCallback(ctx context.Context, box usersandbox.SignInBox, state string) error {
	address, err := listenerAddressFromRedirect(s.opts.Redirect)
	if err != nil {
		return err
	}
	if err = s.browser.router.WriteFile(ctx, box.Sidecar, "/tmp/callback-listener.mjs", callbackListenerScript); err != nil {
		return err
	}
	if err = s.browser.router.WriteFile(ctx, box.Sidecar, "/tmp/signin-relay.mjs", signInRelayScript); err != nil {
		return err
	}
	in, commands := io.Pipe()
	out, output := io.Pipe()
	handle, err := s.browser.router.ExecStream(ctx, box.Sidecar, usersandbox.ExecRequest{Command: "node /tmp/callback-listener.mjs", Dir: "/workspace"}, in, output)
	if err != nil {
		_ = in.Close()
		_ = commands.Close()
		_ = out.Close()
		_ = output.Close()
		return err
	}
	callback := newChatGPTBrowserSession(handle, commands, out, output)
	s.mu.Lock()
	s.callback = callback
	s.mu.Unlock()
	if err = json.NewEncoder(commands).Encode(map[string]any{"type": "listen", "host": address.Host, "port": address.Port, "path": address.Path, "state": state}); err != nil {
		return err
	}
	_, err = callback.next(ctx, "listening")
	return err
}

func (s *signInBrowserSession) Callback(ctx context.Context) (url.Values, error) {
	s.mu.RLock()
	callback := s.callback
	s.mu.RUnlock()
	if callback == nil {
		return nil, errors.New("sign-in callback is not ready")
	}
	return callback.Callback(ctx)
}

func (s *signInBrowserSession) Close() error {
	s.once.Do(func() {
		s.mu.RLock()
		callback := s.callback
		s.mu.RUnlock()
		var callbackErr error
		if callback != nil {
			callbackErr = callback.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.closeErr = errors.Join(callbackErr, s.browser.router.StopSignIn(ctx, s.session))
		s.browser.mu.Lock()
		if s.browser.sessions[s.session] == s {
			delete(s.browser.sessions, s.session)
		}
		s.browser.mu.Unlock()
	})
	return s.closeErr
}
