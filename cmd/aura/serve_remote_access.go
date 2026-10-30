package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/remotetunnel"
	"golang.org/x/sync/singleflight"
)

type remoteSecrets interface {
	Secret(context.Context, string) (string, error)
	Upsert(context.Context, string, string, string) (sqlc.AuraSettings, error)
}
type remoteDesiredCommit func(context.Context, int64, remotetunnel.Desired, string, []remotetunnel.AbandonedResource, string) error

type remoteAccessController struct {
	mu         sync.Mutex
	lifecycle  sync.Mutex
	store      remotetunnel.StateStore
	secrets    remoteSecrets
	projection remotetunnel.Projection
	members    remotetunnel.Members
	commit     remoteDesiredCommit
	client     func(string) *cloudflareapi.Client
	reconciler *remotetunnel.Reconciler
	credential cloudflareapi.Secret
	group      singleflight.Group
	wake       chan struct{}
	done       chan struct{}
	cancel     context.CancelFunc
	events     []agui.RemoteAccessEvent
	// accountNames is keyed by account ID and forgotten when the API token changes. It has its own
	// lock: mu is held for a whole reconciliation pass, and a status read must not wait for one.
	namesMu      sync.Mutex
	accountNames map[string]string
}

func newRemoteAccessController(store remotetunnel.StateStore, secrets remoteSecrets, projection remotetunnel.Projection, members remotetunnel.Members, commit remoteDesiredCommit) *remoteAccessController {
	return &remoteAccessController{store: store, secrets: secrets, projection: projection, members: members, commit: commit, client: func(token string) *cloudflareapi.Client { return cloudflareapi.New("", token, nil) }, wake: make(chan struct{}, 1), events: []agui.RemoteAccessEvent{}, accountNames: map[string]string{}}
}

func (c *remoteAccessController) engine(ctx context.Context) (*remotetunnel.Reconciler, error) {
	token, err := c.secrets.Secret(ctx, "CLOUDFLARE_API_TOKEN")
	if err != nil {
		return nil, err
	}
	if c.reconciler == nil || c.credential.Reveal() != token {
		c.credential = cloudflareapi.Secret(token)
		c.reconciler = remotetunnel.New(c.store, c.client(token), c.members, c.projection, nil, remotetunnel.WithCredentials(remoteTunnelCredentials{secrets: c.secrets}), remotetunnel.WithNameServers(net.DefaultResolver))
	}
	return c.reconciler, nil
}

var accountLookupTimeout = 3 * time.Second

// Verify checks a candidate token and lists the accounts it sees. The token is not stored; the
// names of its accounts are remembered, because they are facts about Cloudflare, not about the token.
func (c *remoteAccessController) Verify(ctx context.Context, token string) ([]cloudflareapi.Account, error) {
	if strings.TrimSpace(token) == "" {
		return nil, remotetunnel.ErrConfiguration
	}
	client := c.client(token)
	if _, err := client.VerifyToken(ctx); err != nil {
		return nil, err
	}
	accounts, err := client.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, remotetunnel.ErrNoAccounts
	}
	c.rememberAccounts(accounts, false)
	return accounts, nil
}

// rememberAccounts keeps the names Cloudflare gave; replace drops the ones the previous token saw.
func (c *remoteAccessController) rememberAccounts(accounts []cloudflareapi.Account, replace bool) {
	c.namesMu.Lock()
	defer c.namesMu.Unlock()
	if replace {
		c.accountNames = map[string]string{}
	}
	for _, a := range accounts {
		c.accountNames[a.ID] = a.Name
	}
}

func (c *remoteAccessController) accountName(id string) (string, bool) {
	c.namesMu.Lock()
	defer c.namesMu.Unlock()
	name, ok := c.accountNames[id]
	return name, ok
}

// Configure saves the token and the address. A disabled save with no domain is the wizard's first
// step: it proves the token and the account, and asks Cloudflare nothing about zones.
func (c *remoteAccessController) Configure(ctx context.Context, in agui.RemoteAccessConfiguration, actor string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	desired := remotetunnel.Desired{Enabled: in.Enabled, AccountID: strings.TrimSpace(in.AccountID), ZoneName: strings.ToLower(strings.TrimSpace(in.ZoneName)), PublicLabel: in.PublicLabel}
	if desired.PublicLabel == "" && desired.ZoneName != "" {
		desired.PublicLabel = "aura"
	}
	if err := remotetunnel.ValidateDesired(desired); err != nil {
		return err
	}
	state, err := c.store.Load(ctx)
	if err != nil {
		return err
	}
	if desired.ZoneName == "" && state.Desired.ZoneName != "" {
		// A save with no domain is the first setup. A row that has a domain keeps it: Disable turns access
		// off, and Delete leaves the address in place for the operator to rebuild.
		if in.Generation != state.Generation {
			return remotetunnel.ErrStaleGeneration
		}
		return remotetunnel.ErrDomainInvalid
	}
	token := in.APIToken
	if token == "" {
		token, err = c.secrets.Secret(ctx, "CLOUDFLARE_API_TOKEN")
		if err != nil {
			return err
		}
	}
	accounts, err := c.Verify(ctx, token)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(accounts, func(a cloudflareapi.Account) bool { return a.ID == desired.AccountID }) {
		return remotetunnel.ErrAccountNotVisible
	}
	if desired.ZoneName != "" {
		if _, err = c.client(token).ListZones(ctx, desired.AccountID, desired.ZoneName); err != nil {
			return err
		}
	}
	if c.commit == nil {
		return remotetunnel.ErrConfiguration
	}
	if err = c.commit(ctx, in.Generation, desired, token, abandonedBy(state, accounts), actor); err != nil {
		return err
	}
	c.rememberAccounts(accounts, true)
	c.reconciler = nil
	c.Wake()
	return nil
}

// abandonedBy lists what the row holds in an account the token cannot see. Such a token can never
// delete it, so the save forgets it instead of blocking. It is nil when the token sees that account
// or the row holds nothing; an empty list still means "forget".
func abandonedBy(state remotetunnel.State, accounts []cloudflareapi.Account) []remotetunnel.AbandonedResource {
	held := state.Resources.AccountID
	if !remotetunnel.HasResources(state.Resources) || slices.ContainsFunc(accounts, func(a cloudflareapi.Account) bool { return a.ID == held }) {
		return nil
	}
	return remotetunnel.AbandonedOf(state)
}

func (c *remoteAccessController) Status(ctx context.Context) (agui.RemoteAccessStatus, error) {
	state, err := c.store.Load(ctx)
	if err != nil {
		return agui.RemoteAccessStatus{}, err
	}
	api, err := c.secrets.Secret(ctx, "CLOUDFLARE_API_TOKEN")
	if err != nil {
		return agui.RemoteAccessStatus{}, err
	}
	d := state.Desired
	out := agui.RemoteAccessStatus{
		Enabled: d.Enabled, Phase: string(state.Phase), APITokenSet: api != "", AccountID: d.AccountID, AccountName: c.accountNameFor(ctx, api, d.AccountID),
		ZoneName: d.ZoneName, PublicLabel: d.PublicLabel, Reason: string(state.Reason), Accepted: state.Accepted(),
		Reachable: state.ObservedHealthy, LastCheckedAt: state.LastReconciledAt, Abandoned: state.Abandoned,
		Generation: state.Generation,
	}
	if d.ZoneName != "" {
		out.PublicHostname = remotetunnel.PublicHostname(d)
	}
	if state.Phase == remotetunnel.PhaseWaitingNameservers && state.Resources.ZoneID != "" {
		zone, e := c.client(api).GetZone(ctx, state.Resources.ZoneID)
		if e == nil && zone.ID == state.Resources.ZoneID && zone.Account.ID == state.Resources.AccountID && zone.Name == d.ZoneName {
			out.Nameservers = zone.NameServers
		}
	}
	return out, nil
}

// accountNameFor looks the name up once per token. A failed lookup leaves it empty: the name is a
// courtesy, and a status that fails or stalls because of it would hide the step the operator is on.
func (c *remoteAccessController) accountNameFor(ctx context.Context, token, id string) string {
	if token == "" || id == "" {
		return ""
	}
	if name, ok := c.accountName(id); ok {
		return name
	}
	ctx, cancel := context.WithTimeout(ctx, accountLookupTimeout)
	defer cancel()
	accounts, err := c.client(token).ListAccounts(ctx)
	if err != nil {
		slog.Warn("Remote access account name unavailable", "error", err)
		return ""
	}
	c.rememberAccounts(accounts, false)
	name, _ := c.accountName(id)
	return name
}

func (c *remoteAccessController) Action(ctx context.Context, action, hostname string, generation int64, actor string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if action == "dismiss-abandoned" {
		return c.store.DismissAbandoned(ctx)
	}
	engine, err := c.engine(ctx)
	if err != nil {
		return err
	}
	switch action {
	case "reconcile":
		state, e := c.store.Load(ctx)
		if e != nil {
			return e
		}
		if state.Phase == remotetunnel.PhaseError {
			if _, e = engine.SaveDesired(ctx, state.Generation, state.Desired, actor); e != nil {
				return e
			}
		}
	case "token/refresh":
		state, e := c.store.Load(ctx)
		if e != nil {
			return e
		}
		if !state.Desired.Enabled || state.Resources.TunnelID == "" {
			return remotetunnel.ErrConfiguration
		}
		if _, e = engine.SaveDesired(ctx, state.Generation, state.Desired, actor); e != nil {
			return e
		}
	case "disable":
		err = engine.Disable(ctx, actor)
	case "delete":
		state, e := c.store.Load(ctx)
		if e != nil {
			return e
		}
		if state.Desired.ZoneName == "" || hostname != remotetunnel.PublicHostname(state.Desired) {
			return remotetunnel.ErrConfiguration
		}
		err = engine.Delete(ctx, actor)
	case "accept-external":
		err = engine.AcceptExternal(ctx, generation)
	default:
		return remotetunnel.ErrConfiguration
	}
	c.Wake()
	return err
}

func (c *remoteAccessController) Events(context.Context) ([]agui.RemoteAccessEvent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]agui.RemoteAccessEvent{}, c.events...), nil
}

func (c *remoteAccessController) rebuild(ctx context.Context) error {
	state, err := c.store.Load(ctx)
	if err != nil {
		return err
	}
	projection := remotetunnel.ProjectionState{Generation: state.Generation}
	if state.Desired.Enabled {
		token, e := c.secrets.Secret(ctx, "CLOUDFLARE_TUNNEL_TOKEN")
		if e != nil {
			return e
		}
		if token != "" && state.Resources.TunnelID != "" {
			// A restart between a tunnel's creation and its credential's replacement leaves the
			// previous tunnel's credential stored; starting it would serve the old address.
			if tunnel, e := tunnelOfCredential(token); e == nil && tunnel == state.Resources.TunnelID {
				projection.Enabled, projection.Token = true, cloudflareapi.Secret(token)
			} else {
				slog.Warn("Remote access stored credential does not belong to the recorded tunnel; the connector stays off", "tunnel", state.Resources.TunnelID)
			}
		}
	}
	return c.projection.Apply(ctx, projection)
}

func (c *remoteAccessController) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *remoteAccessController) Start(ctx context.Context) {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	if c.done != nil {
		return
	}
	ctx, c.cancel = context.WithCancel(ctx)
	c.done = make(chan struct{})
	go c.run(ctx)
}

func (c *remoteAccessController) Close() error {
	c.lifecycle.Lock()
	cancel, done := c.cancel, c.done
	c.lifecycle.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return nil
}

func (c *remoteAccessController) run(ctx context.Context) {
	defer close(c.done)
	if err := c.rebuild(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("Remote access projection rebuild failed")
	}
	c.Wake()
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	var tick <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-tick:
		}
		if ctx.Err() != nil {
			return
		}
		_, reconcileErr, _ := c.group.Do("reconcile", func() (any, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			engine, err := c.engine(ctx)
			if err == nil {
				err = engine.Reconcile(ctx)
			} else if ctx.Err() == nil {
				slog.Warn("Remote access credentials unavailable; retrying")
			}
			state, e := c.store.Load(ctx)
			if e == nil {
				event := agui.RemoteAccessEvent{At: time.Now().UTC(), Phase: string(state.Phase), Generation: state.Generation}
				if len(c.events) == 0 || c.events[len(c.events)-1].Phase != event.Phase || c.events[len(c.events)-1].Generation != event.Generation {
					c.events = append(c.events, event)
					if len(c.events) > 100 {
						c.events = c.events[len(c.events)-100:]
					}
				}
			}
			return nil, err
		})
		if timer != nil {
			timer.Stop()
		}
		state, err := c.store.Load(ctx)
		delay := time.Second
		if err == nil {
			delay = remoteAccessDelay(state)
		} else if ctx.Err() == nil {
			slog.Warn("Remote access state unreadable; retrying", "error", err)
		}
		if reconcileErr != nil && state.Phase != remotetunnel.PhaseError {
			delay = time.Second
		}
		tick = nil
		if delay > 0 {
			timer = time.NewTimer(delay)
			tick = timer.C
		}
	}
}

func remoteAccessDelay(state remotetunnel.State) time.Duration {
	if state.Phase == remotetunnel.PhaseDegraded && state.Reason == remotetunnel.ReasonTunnelDown {
		// Each pass walks every Cloudflare step; an outage must not repeat that every second.
		return 10 * time.Second
	}
	switch state.Phase {
	case remotetunnel.PhaseHealthy:
		return 5 * time.Minute
	case remotetunnel.PhaseWaitingNameservers:
		return 30 * time.Second
	case remotetunnel.PhaseConnecting:
		return 10 * time.Second
	case remotetunnel.PhaseDisabled, remotetunnel.PhaseError:
		// Leftover WARP resources mean a retirement is still owed; nothing else wakes a parked row.
		if state.Resources.LegacyWARPHostname != "" {
			return 5 * time.Minute
		}
		return 0
	default:
		return time.Second
	}
}

type remoteTunnelCredentials struct{ secrets remoteSecrets }

func (a remoteTunnelCredentials) Save(ctx context.Context, token cloudflareapi.Secret) error {
	_, err := a.secrets.Upsert(ctx, "CLOUDFLARE_TUNNEL_TOKEN", token.Reveal(), "")
	return err
}

func mountRemoteAccessRoutes(mux *http.ServeMux, handler http.Handler, auth agui.AuthDeps) {
	for _, route := range agui.RemoteAccessRoutes() {
		mux.Handle(route, agui.RequireCapability(handler, auth, governanceWriteCapability))
	}
}
