package remotetunnel

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

type deletionDiagnosticCloud struct {
	*fakeCloud
	diagnostic string
}

func (c deletionDiagnosticCloud) DeleteDNS(ctx context.Context, zone, id, owner string) error {
	if c.store.state.LastError != c.diagnostic {
		c.t.Error("failed-deletion diagnostic cleared before remote delete attempt")
	}
	return c.fakeCloud.DeleteDNS(ctx, zone, id, owner)
}

func TestExplicitDeleteRetryRetainsDiagnosticUntilRemoteAttempt(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	h.cloud.failure = &cloudflareapi.APIError{Status: 403}
	if err := h.r.Delete(t.Context(), "admin"); err == nil {
		t.Fatal("expected refusal")
	}
	h.cloud.failure = nil
	h.r.cloud = deletionDiagnosticCloud{fakeCloud: h.cloud, diagnostic: h.store.state.LastError}
	if err := h.r.Delete(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	if h.store.state.LastError != "" || h.cloud.deletes != 8 {
		t.Fatal("successful retry did not finish cleanup")
	}
}
