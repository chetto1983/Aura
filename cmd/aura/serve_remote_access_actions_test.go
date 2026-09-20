package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/remotetunnel"
)

func TestRemoteAccessConfigureCommitsOnlyAfterValidation(t *testing.T) {
	for _, mode := range []string{"success", "token-denied", "wrong-account", "zone-denied", "commit-error"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/user/tokens/verify":
					status := "active"
					if mode == "token-denied" {
						status = "disabled"
					}
					_, _ = w.Write([]byte(`{"success":true,"result":{"status":"` + status + `"}}`))
				case "/accounts":
					_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"account"}],"result_info":{"page":1,"total_pages":1}}`))
				case "/zones":
					if mode == "zone-denied" {
						w.WriteHeader(403)
						_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000}]}`))
						return
					}
					_, _ = w.Write([]byte(`{"success":true,"result":[],"result_info":{"page":1,"total_pages":1}}`))
				default:
					t.Errorf("unexpected verification request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			writes := 0
			commit := func(_ context.Context, g int64, d remotetunnel.Desired, token, actor string) error {
				writes++
				if g != 3 || d.ZoneName != "example.com" || d.PublicLabel != "aura" || d.WARPLabel != "aura-warp" || token != "candidate" || actor != "admin" {
					t.Fatal("incorrect commit")
				}
				if mode == "commit-error" {
					return remotetunnel.ErrAddressLocked
				}
				return nil
			}
			c := newRemoteAccessController(&remoteMemoryStore{}, &remoteMemorySecrets{values: map[string]string{}}, &remoteRecordingProjection{}, nil, commit)
			c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(server.URL, token, server.Client()) }
			in := agui.RemoteAccessConfiguration{Enabled: true, Generation: 3, AccountID: "account", ZoneName: " EXAMPLE.COM ", APIToken: "candidate"}
			if mode == "wrong-account" {
				in.AccountID = "other"
			}
			err := c.Configure(t.Context(), in, "admin")
			if mode == "success" {
				if err != nil || writes != 1 || len(c.wake) != 1 {
					t.Fatalf("err=%v writes=%d wakes=%d", err, writes, len(c.wake))
				}
			} else {
				if err == nil || len(c.wake) != 0 {
					t.Fatalf("invalid configuration committed: %v", err)
				}
				if mode != "commit-error" && writes != 0 {
					t.Fatal("stored unvalidated token")
				}
				if mode == "commit-error" && !errors.Is(err, remotetunnel.ErrAddressLocked) {
					t.Fatalf("lost address lock: %v", err)
				}
			}
		})
	}
}

func TestRemoteAccessMissingCredentialStopsBeforeNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("missing credential reached network") }))
	defer server.Close()
	state := &remoteMemoryStore{state: remotetunnel.State{Generation: 1, Phase: remotetunnel.PhaseValidating, Desired: remotetunnel.Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura", WARPLabel: "aura-warp"}}}
	secrets := &remoteMemorySecrets{values: map[string]string{}}
	members := remoteIdentityMembers{store: remoteTestIdentities{identities: []identity.Identity{{ID: "admin", Name: "admin@example.com", Kind: "user"}}}}
	c := newRemoteAccessController(state, secrets, &remoteRecordingProjection{}, members, nil)
	c.client = func(token string) *cloudflareapi.Client { return cloudflareapi.New(server.URL, token, server.Client()) }
	engine, err := c.engine(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Reconcile(t.Context()); err == nil || state.state.Phase != remotetunnel.PhaseError {
		t.Fatalf("error=%v phase=%s", err, state.state.Phase)
	}
	if err = engine.Reconcile(t.Context()); !errors.Is(err, remotetunnel.ErrTerminal) {
		t.Fatalf("missing credential retried: %v", err)
	}
}

func TestRemoteAccessActionsAndStatus(t *testing.T) {
	for _, action := range []string{"reconcile", "token/refresh", "disable", "delete", "unknown"} {
		t.Run(action, func(t *testing.T) {
			state := &remoteMemoryStore{state: remotetunnel.State{Generation: 3, Phase: remotetunnel.PhaseError, Desired: remotetunnel.Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura", WARPLabel: "aura-warp"}, Resources: remotetunnel.Resources{TunnelID: "owned"}}}
			secrets := &remoteMemorySecrets{values: map[string]string{"CLOUDFLARE_API_TOKEN": "api", "CLOUDFLARE_TUNNEL_TOKEN": "tunnel"}}
			projection := &remoteRecordingProjection{}
			c := newRemoteAccessController(state, secrets, projection, nil, nil)
			err := c.Action(t.Context(), action, "wrong.example.com", 3, "admin")
			if action == "delete" || action == "unknown" {
				if !errors.Is(err, remotetunnel.ErrConfiguration) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			status, err := c.Status(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if status.Generation != 4 || !status.APITokenSet || !status.TunnelTokenSet || status.PublicHostname != "aura.example.com" {
				t.Fatalf("status=%+v", status)
			}
			if action == "disable" && (status.Enabled || len(projection.states) != 1 || projection.states[0].Enabled) {
				t.Fatal("disable did not remove projection")
			}
		})
	}
}
