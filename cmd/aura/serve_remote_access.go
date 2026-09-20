package main

import (
	"context"
	"log/slog"
	"net/http"
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
type remoteDesiredCommit func(context.Context, int64, remotetunnel.Desired, string, string) error

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
}

func newRemoteAccessController(store remotetunnel.StateStore, secrets remoteSecrets, projection remotetunnel.Projection, members remotetunnel.Members, commit remoteDesiredCommit) *remoteAccessController {
	return &remoteAccessController{store: store, secrets: secrets, projection: projection, members: members, commit: commit, client: func(token string) *cloudflareapi.Client { return cloudflareapi.New("", token, nil) }, wake: make(chan struct{}, 1), events: []agui.RemoteAccessEvent{}}
}

func (c *remoteAccessController) engine(ctx context.Context) (*remotetunnel.Reconciler, error) {
	token, err := c.secrets.Secret(ctx, "CLOUDFLARE_API_TOKEN")
	if err != nil {
		return nil, err
	}
	if c.reconciler == nil || c.credential.Reveal() != token {
		c.credential = cloudflareapi.Secret(token)
		c.reconciler = remotetunnel.New(c.store, c.client(token), c.members, c.projection, nil, remotetunnel.WithCredentials(remoteTunnelCredentials{secrets: c.secrets}), remotetunnel.WithAcceptance(remotetunnel.PersistedAcceptance{Store: c.store}))
	}
	return c.reconciler, nil
}

func (c *remoteAccessController) Verify(ctx context.Context, token string) ([]cloudflareapi.Account, error) {
	if strings.TrimSpace(token) == "" {
		return nil, remotetunnel.ErrConfiguration
	}
	client := c.client(token)
	if _, err := client.VerifyToken(ctx); err != nil {
		return nil, err
	}
	return client.ListAccounts(ctx)
}

func (c *remoteAccessController) Configure(ctx context.Context, in agui.RemoteAccessConfiguration, actor string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	desired := remotetunnel.Desired{Enabled: in.Enabled, AccountID: strings.TrimSpace(in.AccountID), ZoneName: strings.ToLower(strings.TrimSpace(in.ZoneName)), PublicLabel: in.PublicLabel, WARPLabel: in.WARPLabel}
	if desired.PublicLabel == "" {
		desired.PublicLabel = "aura"
	}
	if desired.WARPLabel == "" {
		desired.WARPLabel = "aura-warp"
	}
	if err := remotetunnel.ValidateDesired(desired); err != nil {
		return err
	}
	token := in.APIToken
	if token == "" {
		var err error
		token, err = c.secrets.Secret(ctx, "CLOUDFLARE_API_TOKEN")
		if err != nil {
			return err
		}
	}
	accounts, err := c.Verify(ctx, token)
	if err != nil {
		return err
	}
	found := false
	for _, account := range accounts {
		found = found || account.ID == desired.AccountID
	}
	if !found {
		return remotetunnel.ErrConfiguration
	}
	if _, err = c.client(token).ListZones(ctx, desired.AccountID, desired.ZoneName); err != nil {
		return err
	}
	if c.commit == nil {
		return remotetunnel.ErrConfiguration
	}
	if err = c.commit(ctx, in.Generation, desired, token, actor); err != nil {
		return err
	}
	c.reconciler = nil
	c.Wake()
	return nil
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
	tunnel, err := c.secrets.Secret(ctx, "CLOUDFLARE_TUNNEL_TOKEN")
	if err != nil {
		return agui.RemoteAccessStatus{}, err
	}
	out := agui.RemoteAccessStatus{Enabled: state.Desired.Enabled, Phase: string(state.Phase), Generation: state.Generation, APITokenSet: api != "", TunnelTokenSet: tunnel != "", AccountID: state.Desired.AccountID, ZoneName: state.Desired.ZoneName, LastError: state.LastError, LastReconciledAt: state.LastReconciledAt, Connector: "disconnected", AcceptanceRequired: state.Desired.Enabled && !state.ObservedHealthy}
	if state.Desired.ZoneName != "" {
		out.PublicHostname = state.Desired.PublicLabel + "." + state.Desired.ZoneName
		out.WARPHostname = state.Desired.WARPLabel + "." + state.Desired.ZoneName
	}
	if state.ObservedHealthy {
		out.Connector = "healthy"
	} else if state.Phase == remotetunnel.PhaseConnecting {
		out.Connector = "awaiting_external_acceptance"
	}
	if state.Phase == remotetunnel.PhaseWaitingNameservers && state.Resources.ZoneID != "" {
		zone, e := c.client(api).GetZone(ctx, state.Resources.ZoneID)
		if e == nil && zone.ID == state.Resources.ZoneID && zone.Account.ID == state.Desired.AccountID && zone.Name == state.Desired.ZoneName {
			out.Nameservers = zone.NameServers
		}
	}
	return out, nil
}

func (c *remoteAccessController) Action(ctx context.Context, action, hostname string, generation int64, actor string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
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
		if state.Desired.ZoneName == "" || hostname != state.Desired.PublicLabel+"."+state.Desired.ZoneName {
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
		projection.Enabled = token != "" && state.Resources.TunnelID != ""
		projection.Token = cloudflareapi.Secret(token)
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
			delay = remoteAccessDelay(state.Phase)
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

func remoteAccessDelay(phase remotetunnel.Phase) time.Duration {
	switch phase {
	case remotetunnel.PhaseHealthy:
		return 5 * time.Minute
	case remotetunnel.PhaseWaitingNameservers:
		return 30 * time.Second
	case remotetunnel.PhaseConnecting:
		return 10 * time.Second
	case remotetunnel.PhaseDisabled, remotetunnel.PhaseError:
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
