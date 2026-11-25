package remotetunnel

import (
	"errors"
	"reflect"
	"testing"
)

func TestDisabledReconcileWithoutMembersCannotCreateDeletionErrorState(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	if err := h.r.Disable(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	before := h.store.state
	calls := h.cloud.calls
	h.r.members = nil
	h.cloud.failure = errors.New("must not reach cloud")
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatalf("disabled reconcile=%v", err)
	}
	if h.cloud.calls != calls || !reflect.DeepEqual(h.store.state, before) {
		t.Fatal("disabled reconcile changed state or reached cloud")
	}
}
