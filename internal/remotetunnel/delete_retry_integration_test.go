//go:build db_integration

package remotetunnel

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/settings"
)

type retryDeletionCloud struct {
	Cloudflare
	tunnel          cloudflareapi.Tunnel
	denied, deleted bool
	attempts        int
	beforeDelete    func()
}

func (c *retryDeletionCloud) GetTunnel(context.Context, string, string) (cloudflareapi.Tunnel, error) {
	if c.deleted {
		return cloudflareapi.Tunnel{}, &cloudflareapi.APIError{Status: 404}
	}
	return c.tunnel, nil
}

func (c *retryDeletionCloud) DeleteTunnel(context.Context, string, string) error {
	c.attempts++
	if c.beforeDelete != nil {
		c.beforeDelete()
	}
	if c.denied {
		return &cloudflareapi.APIError{Status: 403}
	}
	c.deleted = true
	return nil
}

func TestFailedDeletionSurvivesDesiredWritesAndCredentialReplacement(t *testing.T) {
	pool := remoteAccessPool(t)
	for _, mode := range []string{"retry", "replace-disabled", "replace-enabled", "disable"} {
		t.Run(mode, func(t *testing.T) {
			store := NewStore(pool)
			initial, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			initial.Resources = Resources{AccountID: initial.Desired.AccountID}
			initial.Phase = PhaseDisabled
			if _, err = store.Advance(t.Context(), initial); err != nil {
				t.Fatal(err)
			}
			desired := Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura", WARPLabel: "aura-warp"}
			state, err := store.SaveDesired(t.Context(), initial.Generation, desired, "admin")
			if err != nil {
				t.Fatal(err)
			}
			state.Resources = Resources{AccountID: "account", ZoneID: "zone", OTPProviderID: "otp", TunnelID: "tunnel"}
			state.TunnelName = "aura-00000000-0000-4000-8000-000000000001"
			state.Phase = PhaseConnecting
			if _, err = store.Advance(t.Context(), state); err != nil {
				t.Fatal(err)
			}
			cloud := &retryDeletionCloud{denied: true, tunnel: cloudflareapi.Tunnel{ID: "tunnel", Name: state.TunnelName, ConfigSource: "cloudflare"}}
			projection := &testProjection{t: t}
			engine := New(store, cloud, nil, projection, nil)
			if err = engine.Delete(t.Context(), "admin"); err == nil {
				t.Fatal("expected terminal deletion refusal")
			}
			failed, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if failed.Desired.Enabled || failed.Phase != PhaseError || failed.LastError == "" || failed.Resources.TunnelID == "" {
				t.Fatalf("not a retained failed deletion: %+v", failed)
			}
			cloud.denied = false
			cloud.beforeDelete = func() {
				observed, e := store.Load(t.Context())
				if e != nil {
					t.Fatal(e)
				}
				if observed.LastError != failed.LastError {
					t.Error("diagnostic cleared before retry attempted deletion")
				}
			}
			if mode == "disable" {
				if err = engine.Disable(t.Context(), "admin"); err != nil {
					t.Fatal(err)
				}
			} else if mode == "retry" {
				_, err = engine.SaveDesired(t.Context(), failed.Generation, failed.Desired, "admin")
			} else {
				secrets, e := settings.NewStore(pool, strings.Repeat("ab", 32))
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { _ = secrets.Delete(context.Background(), "CLOUDFLARE_API_TOKEN") })
				replacement := failed.Desired
				replacement.Enabled = mode == "replace-enabled"
				_, err = secrets.UpsertWith(t.Context(), "CLOUDFLARE_API_TOKEN", "repaired-token", "admin", func(tx sqlc.DBTX) error {
					_, e := NewStore(tx).SaveDesired(t.Context(), failed.Generation, replacement, "admin")
					return e
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			resumed, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if mode != "disable" && (resumed.Desired.Enabled || resumed.Phase != PhaseDeleting || resumed.LastError != failed.LastError) {
				t.Fatalf("lost deletion intent/diagnostic: %+v", resumed)
			}
			if _, e := store.SaveDesired(t.Context(), failed.Generation, failed.Desired, "stale"); !errors.Is(e, ErrStaleGeneration) {
				t.Fatalf("stale retry=%v", e)
			}
			engine = New(store, cloud, nil, projection, nil)
			if err = engine.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			final, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "disable" {
				if cloud.deleted || cloud.attempts != 1 || final.Phase != PhaseDisabled || final.LastError != "" || final.Resources != failed.Resources {
					t.Fatal("intentional disable deleted resources or retained error")
				}
			} else if !cloud.deleted || final.Resources != (Resources{AccountID: "account"}) || final.Phase != PhaseDisabled || final.LastError != "" {
				t.Fatalf("deletion not completed: %+v", final)
			}
		})
	}
}

func TestSaveDesiredDeletionIntentTransitions(t *testing.T) {
	store := NewStore(remoteAccessPool(t))
	for _, tc := range []struct {
		name                                         string
		phase                                        Phase
		currentEnabled, requestedEnabled, references bool
		wantPhase                                    Phase
		wantEnabled, keepDiagnostic                  bool
	}{
		{"ordinary-disabled", PhaseDisabled, false, false, true, PhaseDisabled, false, false},
		{"ordinary-reenable", PhaseDisabled, false, true, true, PhaseValidating, true, false},
		{"failed-provision-disable", PhaseError, true, false, true, PhaseDisabled, false, false},
		{"failed-delete-retry", PhaseError, false, false, true, PhaseDeleting, false, true},
		{"failed-delete-reenable", PhaseError, false, true, true, PhaseDeleting, false, true},
		{"ongoing-delete-reenable", PhaseDeleting, false, true, true, PhaseDeleting, false, true},
		{"ongoing-delete-finalize", PhaseDeleting, false, true, false, PhaseDeleting, false, true},
		{"error-with-no-references", PhaseError, false, false, false, PhaseDisabled, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			current.Phase = PhaseDisabled
			current.Resources = Resources{AccountID: current.Desired.AccountID}
			if _, err = store.Advance(t.Context(), current); err != nil {
				t.Fatal(err)
			}
			desired := Desired{Enabled: tc.currentEnabled, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura", WARPLabel: "aura-warp"}
			current, err = store.SaveDesired(t.Context(), current.Generation, desired, "admin")
			if err != nil {
				t.Fatal(err)
			}
			current.Phase = tc.phase
			current.LastError = "sanitized fixture diagnostic"
			if tc.references {
				current.Resources.TunnelID = "owned"
			}
			current, err = store.Advance(t.Context(), current)
			if err != nil {
				t.Fatal(err)
			}
			desired.Enabled = tc.requestedEnabled
			next, err := store.SaveDesired(t.Context(), current.Generation, desired, "admin")
			if err != nil {
				t.Fatal(err)
			}
			wantError := ""
			if tc.keepDiagnostic {
				wantError = current.LastError
			}
			if next.Phase != tc.wantPhase || next.Desired.Enabled != tc.wantEnabled || next.LastError != wantError || next.Generation != current.Generation+1 || next.Resources != current.Resources {
				t.Fatalf("unexpected transition: %+v", next)
			}
		})
	}
}
