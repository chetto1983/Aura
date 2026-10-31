package remotetunnel

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

type deniedTunnelCloud struct {
	*fakeCloud
	denied int
}

func (c *deniedTunnelCloud) CreateTunnel(context.Context, string, string) (cloudflareapi.Tunnel, error) {
	c.denied++
	return cloudflareapi.Tunnel{}, &cloudflareapi.APIError{Status: 403, Code: 10000}
}

type deniedOTPCloud struct{ *fakeCloud }

func (deniedOTPCloud) EnsureOTPProvider(context.Context, string) (cloudflareapi.IdentityProvider, error) {
	return cloudflareapi.IdentityProvider{}, &cloudflareapi.APIError{Status: 403, Code: 9999}
}

type deniedApplicationCloud struct{ *fakeCloud }

func (deniedApplicationCloud) PutApplication(context.Context, string, string, cloudflareapi.AccessApplication) (cloudflareapi.AccessApplication, error) {
	return cloudflareapi.AccessApplication{}, &cloudflareapi.APIError{Status: 403, Code: 10000}
}

// The pass verified the token moments earlier, so a refusal at these steps is about what the step
// needs. Lab VM, 2026-10-31: both arrived as token_rejected and sent the operator to paste a token
// that was fine, the second time on an account with no Zero Trust at all.
func TestARefusalNamesWhatItsStepNeeds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cloud func(*fakeCloud) Cloudflare
		want  Reason
	}{
		{"tunnel", func(c *fakeCloud) Cloudflare { return &deniedTunnelCloud{fakeCloud: c} }, ReasonTunnelPermission},
		{"one-time pin provider", func(c *fakeCloud) Cloudflare { return deniedOTPCloud{c} }, ReasonAccessUnavailable},
		{"access application", func(c *fakeCloud) Cloudflare { return deniedApplicationCloud{c} }, ReasonAccessUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.r.cloud = tc.cloud(h.cloud)
			if err := h.r.Reconcile(t.Context()); err == nil {
				t.Fatal("refusal accepted")
			}
			if h.store.state.Phase != PhaseError || h.store.state.Reason != tc.want {
				t.Fatalf("phase=%s reason=%s, want error/%s", h.store.state.Phase, h.store.state.Reason, tc.want)
			}
		})
	}
}

func TestRefusedOnlyNamesARefusal(t *testing.T) {
	if got := ReasonOf(Refused(&cloudflareapi.APIError{Status: 403, Code: 9999}, ErrAccessUnavailable)); got != ReasonAccessUnavailable {
		t.Fatalf("403: reason=%s", got)
	}
	if got := ReasonOf(Refused(&cloudflareapi.APIError{Status: 401, Code: 10000}, ErrTunnelPermission)); got != ReasonTunnelPermission {
		t.Fatalf("401: reason=%s", got)
	}
	unreachable := &cloudflareapi.APIError{Status: 503, Retryable: true}
	if got := Refused(unreachable, ErrTunnelPermission); !errors.Is(got, unreachable) || errors.Is(got, ErrTunnelPermission) || !transient(got) {
		t.Fatalf("503 renamed: %v", got)
	}
	if got := Refused(nil, ErrTunnelPermission); got != nil {
		t.Fatalf("nil renamed: %v", got)
	}
}

func TestWritePermissionRefusalIsTerminalUntilCredentialReplacement(t *testing.T) {
	h := newHarness(t)
	cloud := &deniedTunnelCloud{fakeCloud: h.cloud}
	h.r.cloud = cloud
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("write permission refusal accepted")
	}
	if h.store.state.Phase != PhaseError || h.store.state.Reason == "" || h.store.state.ObservedHealthy || cloud.denied != 1 {
		t.Fatalf("phase=%s denied=%d", h.store.state.Phase, cloud.denied)
	}
	calls := h.cloud.calls
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrTerminal) || h.cloud.calls != calls || cloud.denied != 1 {
		t.Fatalf("automatic retry after permission denial: %v", err)
	}
	if _, err := h.r.SaveDesired(t.Context(), h.store.state.Generation, h.store.state.Desired, "admin"); err != nil {
		t.Fatal(err)
	}
	h.r.cloud = h.cloud
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.store.state.Phase != PhaseConnecting {
		t.Fatal("credential replacement did not resume provisioning")
	}
}
