package mcp

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// oauth_flows.go makes the authorization flow survivable across an HTTP request.
//
// The problem it solves is structural, not cosmetic. The SDK's AuthorizationCodeFetcher
// BLOCKS: it is handed a consent URL and must return an authorization code. That is fine
// for a CLI, which owns a terminal and a loopback listener for as long as it takes. It is
// impossible for a cockpit request, which has to answer in milliseconds with a URL, and
// then receive the code minutes later on a completely different request.
//
// So the flow outlives both requests, and this is the registry that holds it. The shape
// is taken from the two implementations that already solved it:
//
//   - Hermes (apps/desktop/src/lib/mcp-dashboard-oauth.ts) drives its dashboard with a
//     start → poll loop over the statuses starting / authorization_required / approved /
//     error. That vocabulary is reused verbatim here rather than invented, because the
//     cockpit is going to poll it exactly the same way.
//   - LibreChat (packages/api/src/mcp/oauth/handler.ts) keys the callback by the OAuth
//     `state` — the only thing a redirect is guaranteed to carry back — and deletes the
//     mapping when a flow is replaced, with the comment "Prevents old authorization URLs
//     from resolving after a flow restart". Ours does the same, and it is not tidiness:
//     without it, a URL from an abandoned attempt still completes and silently overwrites
//     the grant the human just made with a newer one.

// Flow statuses. Hermes's vocabulary, kept identical so a reader can compare the two.
const (
	FlowStarting              = "starting"
	FlowAuthorizationRequired = "authorization_required"
	FlowApproved              = "approved"
	FlowError                 = "error"
)

// flowTTL bounds how long an unfinished authorization stays resumable. Long enough for
// somebody to find a password manager and a second-factor device, short enough that an
// abandoned attempt cannot be completed by a stale tab the next morning.
const flowTTL = 10 * time.Minute

// flowStartTimeout bounds the wait for the consent URL — discovery plus, on a server that
// registers dynamically, a client registration round trip. It is NOT the human's budget;
// that is flowTTL.
const flowStartTimeout = 45 * time.Second

var (
	// ErrNoSuchFlow covers both a flow id that never existed and one that has expired.
	// They are deliberately indistinguishable: telling a caller which is which only
	// helps somebody probing for live authorization states.
	ErrNoSuchFlow = errors.New("mcp oauth: no such authorization flow")

	// ErrFlowNotWaiting reports a callback for a flow that is no longer expecting one —
	// a replayed redirect, or a second tab finishing after the first already did.
	ErrFlowNotWaiting = errors.New("mcp oauth: this authorization was already completed or abandoned")
)

// SessionOpener opens a live MCP session. It exists as a seam so the registry can be
// driven in tests without a provider: everything interesting here — replay, state
// invalidation, expiry, the callback handoff — is decided before a byte reaches a server.
type SessionOpener func(ctx context.Context, name string, server ManagedServer, opts SessionOptions) (io.Closer, error)

// Flow is the snapshot a caller may see. It carries no code, no token and no state: those
// are the flow's secrets, and a status endpoint must not be a way to read them.
type Flow struct {
	ID               string
	ServerName       string
	Status           string
	AuthorizationURL string
	RedirectURI      string
	Error            string
	ExpiresAt        time.Time
}

type liveFlow struct {
	Flow
	owner string
	state string
	codes chan *auth.AuthorizationResult
	ready chan struct{}
	// cancel releases the goroutine parked in the fetcher. Forgetting a flow without
	// calling it leaves that goroutine waiting out the full TTL for a redirect that can
	// no longer be delivered — the map entry is gone, so nothing can ever wake it.
	cancel context.CancelFunc
	// released records that Start has been let go, NOT that the flow is over. The two
	// are different moments and conflating them refuses every redirect: Start is
	// released the instant the consent URL is published, which is precisely when the
	// flow BEGINS waiting for one.
	released bool
}

// Flows tracks in-progress authorizations for every identity on this process.
//
// In memory on purpose. A flow is worth less than the browser tab holding it open: if
// Aura restarts mid-authorization the human presses the button again, which costs one
// click, whereas persisting half-finished flows would mean storing a live PKCE verifier
// and a consent URL that an attacker who read them could complete. The GRANT is what
// deserves a database, and it has one.
type Flows struct {
	store  GrantStore
	open   SessionOpener
	logger *slog.Logger
	now    func() time.Time

	mu      sync.Mutex
	byID    map[string]*liveFlow
	byState map[string]*liveFlow
	byOwner map[string]*liveFlow
}

// NewFlows builds the registry.
//
// The egress policy is deliberately NOT a field: SessionOpener is a closure the
// composition root builds, and that is where the runtime policy for a server is already
// resolved. Threading it through here would mean two places deciding what a mount may
// reach, which is how the two drift apart.
func NewFlows(store GrantStore, open SessionOpener, logger *slog.Logger) (*Flows, error) {
	if store == nil {
		return nil, errors.New("mcp oauth: flows need a grant store")
	}
	if open == nil {
		return nil, errors.New("mcp oauth: flows need a session opener")
	}
	return &Flows{
		store:   store,
		open:    open,
		logger:  resolveLogger(logger),
		now:     time.Now,
		byID:    map[string]*liveFlow{},
		byState: map[string]*liveFlow{},
		byOwner: map[string]*liveFlow{},
	}, nil
}

// Start begins an authorization, or returns the one already in progress.
//
// owner scopes the flow to one identity: two people authorizing the same server at the
// same time must not see each other's consent URL, and the second must not silently
// replace the first.
//
// Returning the existing flow rather than starting a second is LibreChat's replay
// behaviour, and it matters more than it looks: a cockpit reload that started a fresh
// flow would leave the first one's state live, register a second dynamic client, and give
// the human two consent URLs of which only one can work.
func (f *Flows) Start(ctx context.Context, owner, name string, server ManagedServer, redirectURI string) (Flow, error) {
	settings, err := OAuthSettingsFromEnv(server.Env)
	if err != nil {
		return Flow{}, err
	}
	if !UsesOAuth(server, settings) {
		return Flow{}, fmt.Errorf("mcp oauth: %q takes no authorization flow", name)
	}
	if existing, ok := f.replayable(owner, name); ok {
		return existing, nil
	}
	flow := &liveFlow{
		Flow: Flow{
			ID:          newFlowID(),
			ServerName:  name,
			Status:      FlowStarting,
			RedirectURI: redirectURI,
			ExpiresAt:   f.now().Add(flowTTL),
		},
		owner: owner,
		codes: make(chan *auth.AuthorizationResult, 1),
		ready: make(chan struct{}),
	}
	// Detached from the request — this authorization outlives the request that asked
	// for it by design — but cancellable, so expiry and shutdown can end it.
	// detachedForRefresh keeps the identity the grant store scopes its rows by.
	flowCtx, cancel := context.WithCancel(detachedForRefresh(ctx))
	flow.cancel = cancel
	f.register(flow)
	go f.run(flowCtx, flow, name, server)

	select {
	case <-flow.ready:
		return f.snapshot(flow), nil
	case <-time.After(flowStartTimeout):
		f.fail(flow, errors.New("timed out waiting for the authorization server"))
		return f.snapshot(flow), nil
	}
}

// run opens a real session, which is what triggers the server's own 401 and therefore the
// authorization. Driving the SDK's flow in isolation would prove only that an
// authorization server said yes; this proves the token works against the server that
// issued it, and persists the grant through the handler's own hook on the way.
func (f *Flows) run(ctx context.Context, flow *liveFlow, name string, server ManagedServer) {
	opts := SessionOptions{
		Logger: f.logger,
		OAuth: OAuthOptions{
			Store:       f.store,
			Fetcher:     f.fetcher(ctx, flow),
			RedirectURL: flow.RedirectURI,
		},
	}
	session, err := f.open(ctx, name, server, opts)
	if err != nil {
		f.fail(flow, err)
		return
	}
	if session != nil {
		_ = session.Close()
	}
	f.approve(flow)
}

// fetcher publishes the consent URL and then waits for the redirect to arrive on another
// request entirely.
//
// The state is PARSED OUT of the URL rather than generated here, because the SDK owns it:
// it builds the URL, and it compares what comes back (authorization_code.go:594). Parsing
// it is how the callback — which carries nothing else we chose — finds this flow again.
func (f *Flows) fetcher(ctx context.Context, flow *liveFlow) auth.AuthorizationCodeFetcher {
	return func(fetchCtx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		state, err := stateFromAuthorizationURL(args.URL)
		if err != nil {
			return nil, err
		}
		f.publish(flow, args.URL, state)
		select {
		case result := <-flow.codes:
			return result, nil
		case <-fetchCtx.Done():
			return nil, fmt.Errorf("mcp oauth: authorization abandoned: %w", fetchCtx.Err())
		case <-ctx.Done():
			return nil, fmt.Errorf("mcp oauth: authorization abandoned: %w", ctx.Err())
		case <-time.After(flowTTL):
			return nil, errors.New("mcp oauth: nobody completed the authorization in time")
		}
	}
}

// Complete hands a redirect's parameters to the flow waiting on that state.
func (f *Flows) Complete(state, code, iss string) (Flow, error) {
	f.mu.Lock()
	flow, ok := f.byState[state]
	if ok && flow.Status != FlowAuthorizationRequired {
		ok = false
	}
	if ok {
		// Consumed here, under the same lock that found it, so two redirects racing
		// (a double-click, a browser prefetch) cannot both be delivered.
		delete(f.byState, state)
	}
	f.mu.Unlock()
	if !ok {
		return Flow{}, ErrFlowNotWaiting
	}
	flow.codes <- &auth.AuthorizationResult{Code: code, State: state, Iss: iss}
	return f.snapshot(flow), nil
}

// Fail marks a flow as refused by the provider. The redirect carries `error=access_denied`
// rather than a code, and without this the human would watch a spinner until the TTL.
func (f *Flows) Fail(state string, reason error) (Flow, error) {
	f.mu.Lock()
	flow, ok := f.byState[state]
	if ok {
		delete(f.byState, state)
	}
	f.mu.Unlock()
	if !ok {
		return Flow{}, ErrFlowNotWaiting
	}
	f.fail(flow, reason)
	return f.snapshot(flow), nil
}

// Status is what the cockpit polls.
func (f *Flows) Status(owner, id string) (Flow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepLocked()
	flow, ok := f.byID[id]
	// The owner check is an authorization decision, not a lookup detail: a flow id is
	// a bearer-ish handle, and one identity must not be able to watch another's.
	if !ok || flow.owner != owner {
		return Flow{}, ErrNoSuchFlow
	}
	return flow.Flow, nil
}

func (f *Flows) replayable(owner, name string) (Flow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepLocked()
	flow, ok := f.byOwner[ownerKey(owner, name)]
	if !ok || flow.Status != FlowAuthorizationRequired {
		return Flow{}, false
	}
	return flow.Flow, true
}

func (f *Flows) register(flow *liveFlow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepLocked()
	key := ownerKey(flow.owner, flow.ServerName)
	// Replacing an unfinished attempt drops its state mapping. LibreChat's
	// deleteStateMapping, and the reason is not hygiene: a live state from an abandoned
	// attempt is a URL that still completes, and completing it would overwrite the
	// grant the human is about to make.
	if previous, ok := f.byOwner[key]; ok {
		f.forgetLocked(previous)
	}
	f.byOwner[key] = flow
	f.byID[flow.ID] = flow
}

func (f *Flows) publish(flow *liveFlow, authURL, state string) {
	f.mu.Lock()
	flow.AuthorizationURL = authURL
	flow.Status = FlowAuthorizationRequired
	flow.state = state
	f.byState[state] = flow
	f.mu.Unlock()
	f.signalReady(flow)
}

func (f *Flows) approve(flow *liveFlow) {
	f.mu.Lock()
	flow.Status = FlowApproved
	flow.AuthorizationURL = ""
	if flow.state != "" {
		delete(f.byState, flow.state)
	}
	f.mu.Unlock()
	f.signalReady(flow)
}

func (f *Flows) fail(flow *liveFlow, err error) {
	f.mu.Lock()
	if flow.Status != FlowApproved {
		flow.Status = FlowError
		// RedactSecrets because a transport error can quote a URL that carries a token
		// or a client secret, and this string is rendered in a browser.
		flow.Error = RedactSecrets(err.Error())
		flow.AuthorizationURL = ""
	}
	if flow.state != "" {
		delete(f.byState, flow.state)
	}
	f.mu.Unlock()
	f.signalReady(flow)
}

// signalReady releases Start exactly once, whichever outcome arrives first.
func (f *Flows) signalReady(flow *liveFlow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !flow.released {
		flow.released = true
		close(flow.ready)
	}
}

func (f *Flows) snapshot(flow *liveFlow) Flow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return flow.Flow
}

func (f *Flows) sweepLocked() {
	now := f.now()
	for _, flow := range f.byID {
		if now.After(flow.ExpiresAt) {
			f.forgetLocked(flow)
		}
	}
}

func (f *Flows) forgetLocked(flow *liveFlow) {
	if flow.cancel != nil {
		flow.cancel()
	}
	delete(f.byID, flow.ID)
	if flow.state != "" {
		delete(f.byState, flow.state)
	}
	if current, ok := f.byOwner[ownerKey(flow.owner, flow.ServerName)]; ok && current == flow {
		delete(f.byOwner, ownerKey(flow.owner, flow.ServerName))
	}
}

// Close ends every flow still in progress. A parked fetcher is waiting for a redirect
// that will never arrive once the process is going down, and leaving it there makes a
// graceful shutdown wait out the TTL.
func (f *Flows) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, flow := range f.byID {
		f.forgetLocked(flow)
	}
}

func ownerKey(owner, server string) string { return owner + "\x00" + server }

// stateFromAuthorizationURL pulls the state the SDK put in the consent URL. A URL without
// one cannot be completed by a callback, so failing here is better than publishing a
// button that leads nowhere.
func stateFromAuthorizationURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("mcp oauth: unparseable authorization URL: %w", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		return "", errors.New("mcp oauth: the authorization URL carries no state, so no redirect could be matched to it")
	}
	return state, nil
}

func newFlowID() string { return rand.Text() }
