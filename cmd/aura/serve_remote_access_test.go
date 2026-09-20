package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/remotetunnel"
)

type remoteMemoryStore struct {
	mu    sync.Mutex
	state remotetunnel.State
}

type remoteTestIdentities struct {
	identities []identity.Identity
	err        error
}

func (s remoteTestIdentities) ListIdentities(context.Context) ([]identity.Identity, error) {
	return s.identities, s.err
}
func (s remoteTestIdentities) HasCapability(_ context.Context, id, _ string) (bool, error) {
	return id == "admin", s.err
}

func TestRemoteAccessCloseCancelsInFlightCloudflare(t *testing.T) {
	reached := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(reached); <-r.Context().Done() }))
	defer server.Close()
	state := &remoteMemoryStore{state: remotetunnel.State{Generation: 1, Phase: remotetunnel.PhaseValidating, Desired: remotetunnel.Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura", WARPLabel: "aura-warp"}}}
	secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "fixture"}}
	members := remoteIdentityMembers{store: remoteTestIdentities{identities: []identity.Identity{{ID: "admin", Name: "admin@example.com", Kind: "user"}}}}
	c := newRemoteAccessController(state, secrets, &remoteRecordingProjection{}, members, nil)
	c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(server.URL, token, server.Client()) }
	c.Start(t.Context())
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("reconciliation did not start")
	}
	closed := make(chan struct{})
	go func() { _ = c.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Error("shutdown did not cancel in-flight request")
	}
}

func TestRemoteAccessMembersExcludeInactiveAndNonhuman(t *testing.T) {
	members := remoteIdentityMembers{store: remoteTestIdentities{identities: []identity.Identity{
		{ID: "admin", Name: "admin@example.com", Kind: "user"},
		{ID: "member", Name: "member@example.com", Kind: "user"},
		{Name: "inactive@example.com", Kind: "user", Deactivated: true},
		{Name: "system@example.com", Kind: "system"},
		{Name: "service@example.com", Kind: "service"},
		{Name: "not-an-email", Kind: "user"},
	}}}
	emails, err := members.ActiveEmails(t.Context())
	if err != nil || len(emails) != 2 {
		t.Fatalf("emails=%v err=%v", emails, err)
	}
	admins, err := members.ActiveAdminEmails(t.Context())
	if err != nil || len(admins) != 1 || admins[0] != "admin@example.com" {
		t.Fatalf("admins=%v err=%v", admins, err)
	}
}

func (s *remoteMemoryStore) Load(context.Context) (remotetunnel.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, nil
}
func (s *remoteMemoryStore) SaveDesired(_ context.Context, g int64, d remotetunnel.Desired, by string) (remotetunnel.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g != s.state.Generation {
		return remotetunnel.State{}, remotetunnel.ErrStaleGeneration
	}
	s.state.Desired = d
	s.state.Generation++
	s.state.ObservedHealthy = false
	s.state.Phase = remotetunnel.PhaseValidating
	return s.state, nil
}
func (s *remoteMemoryStore) Advance(_ context.Context, next remotetunnel.State) (remotetunnel.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = next
	return next, nil
}

type remoteMemorySecrets struct {
	mu     sync.Mutex
	values map[string]string
	err    error
}

func (s *remoteMemorySecrets) Secret(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key], s.err
}
func (s *remoteMemorySecrets) Upsert(_ context.Context, key, value, by string) (sqlc.AuraSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return sqlc.AuraSettings{}, s.err
	}
	s.values[key] = value
	return sqlc.AuraSettings{}, nil
}

type remoteRecordingProjection struct {
	mu     sync.Mutex
	states []remotetunnel.ProjectionState
}

func (p *remoteRecordingProjection) Apply(_ context.Context, state remotetunnel.ProjectionState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states = append(p.states, state)
	return nil
}

func TestRemoteAccessBootRebuildsProjectionAndCloses(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			state := &remoteMemoryStore{state: remotetunnel.State{Generation: 7, Desired: remotetunnel.Desired{Enabled: enabled}, Resources: remotetunnel.Resources{TunnelID: "owned-tunnel"}}}
			secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_TUNNEL_TOKEN": "stored-tunnel-token"}}
			projection := &remoteRecordingProjection{}
			c := newRemoteAccessController(state, secrets, projection, nil, nil)
			if err := c.rebuild(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(projection.states) != 1 || projection.states[0].Enabled != enabled || projection.states[0].Generation != 7 {
				t.Fatal("projection not rebuilt from database")
			}
			if enabled && projection.states[0].Token.Reveal() != "stored-tunnel-token" {
				t.Fatal("database token not projected")
			}
			c.Start(t.Context())
			for range 100 {
				c.Wake()
			}
			c.Close()
			c.Close()
		})
	}
}

func TestRemoteAccessCandidateVerificationNeverStores(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer candidate" {
			t.Error("candidate credential not used")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/user/tokens/verify" {
			_, _ = w.Write([]byte(`{"success":true,"result":{"status":"active"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"account","name":"Account"}],"result_info":{"page":1,"total_pages":1}}`))
	}))
	defer server.Close()
	secrets := &remoteMemorySecrets{values: map[string]string{}}
	c := newRemoteAccessController(&remoteMemoryStore{}, secrets, &remoteRecordingProjection{}, nil, nil)
	c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(server.URL, token, server.Client()) }
	accounts, err := c.Verify(t.Context(), "candidate")
	if err != nil || len(accounts) != 1 || len(secrets.values) != 0 {
		t.Fatalf("accounts=%v err=%v stored=%d", accounts, err, len(secrets.values))
	}
}

func TestRemoteAccessCredentialsSaveBeforeProjection(t *testing.T) {
	secrets := &remoteMemorySecrets{values: map[string]string{}}
	adapter := remoteTunnelCredentials{secrets: secrets}
	if err := adapter.Save(t.Context(), cloudflareapi.Secret("connector")); err != nil {
		t.Fatal(err)
	}
	if secrets.values["CLOUDFLARE_TUNNEL_TOKEN"] != "connector" {
		t.Fatal("missing durable token")
	}
	secrets.err = errors.New("database unavailable")
	if adapter.Save(t.Context(), cloudflareapi.Secret("replacement")) == nil {
		t.Fatal("failed persistence accepted")
	}
}

func TestRemoteAccessRetrySchedule(t *testing.T) {
	for _, tc := range []struct {
		phase remotetunnel.Phase
		want  time.Duration
	}{{remotetunnel.PhaseHealthy, 5 * time.Minute}, {remotetunnel.PhaseWaitingNameservers, 30 * time.Second}, {remotetunnel.PhaseConnecting, 10 * time.Second}, {remotetunnel.PhaseDegraded, time.Second}, {remotetunnel.PhaseDisabled, 0}, {remotetunnel.PhaseError, 0}} {
		if got := remoteAccessDelay(tc.phase); got != tc.want {
			t.Fatalf("phase=%s delay=%v", tc.phase, got)
		}
	}
}

func TestRemoteAccessConfigurationValidationPrecedesWrites(t *testing.T) {
	c := newRemoteAccessController(&remoteMemoryStore{}, &remoteMemorySecrets{values: map[string]string{}}, &remoteRecordingProjection{}, nil, nil)
	if err := c.Configure(t.Context(), agui.RemoteAccessConfiguration{ZoneName: "evil/path", AccountID: "account"}, "admin"); !errors.Is(err, remotetunnel.ErrConfiguration) {
		t.Fatalf("error=%v", err)
	}
}
