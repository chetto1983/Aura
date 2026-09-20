package remotetunnel

import (
	"errors"
	"testing"
)

func TestDisabledMemberSyncCannotCreateDeletionErrorState(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	if err := h.r.Disable(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	before := h.store.state
	calls := h.cloud.calls
	h.r.members = nil
	h.cloud.failure = errors.New("must not reach cloud")
	if err := h.r.SyncMembers(t.Context()); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("disabled sync=%v", err)
	}
	if h.cloud.calls != calls || h.store.state != before {
		t.Fatal("disabled membership sync changed state or reached cloud")
	}
}
