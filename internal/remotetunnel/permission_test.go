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

func TestWritePermissionRefusalIsTerminalUntilCredentialReplacement(t *testing.T) {
	h := newHarness(t)
	cloud := &deniedTunnelCloud{fakeCloud: h.cloud}
	h.r.cloud = cloud
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("write permission refusal accepted")
	}
	if h.store.state.Phase != PhaseError || h.store.state.LastError == "" || h.store.state.ObservedHealthy || cloud.denied != 1 {
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
