package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

type draftRecovererFake struct {
	owners    []string
	ctxOwners []string
	got       messagedrafts.Recovered
	errFor    string
}

func (f *draftRecovererFake) Recover(ctx context.Context, ownerID string) (messagedrafts.Recovered, error) {
	f.owners = append(f.owners, ownerID)
	f.ctxOwners = append(f.ctxOwners, identityctx.IdentityID(ctx))
	if ownerID == f.errFor {
		return messagedrafts.Recovered{}, errors.New("recover failed")
	}
	return f.got, nil
}

func TestRecoverMessageDraftsSumsEveryActiveOwnerAndKeepsGoingPastAFailure(t *testing.T) {
	drafts := &draftRecovererFake{got: messagedrafts.Recovered{Expired: 2, Interrupted: 1}, errFor: "owner-b"}
	got, err := recoverMessageDraftsByIdentity(t.Context(), expiryIdentityListerFake{identities: []identity.Identity{
		{ID: "owner-a"},
		{ID: "owner-disabled", Deactivated: true},
		{ID: ""},
		{ID: "owner-b"},
		{ID: "owner-c"},
	}}, drafts)
	if got != (messagedrafts.Recovered{Expired: 4, Interrupted: 2}) {
		t.Fatalf("recovered = %+v, want the two successful owners summed", got)
	}
	if err == nil || err.Error() != "identity owner-b: recover failed" {
		t.Fatalf("err = %v, want owner-b's failure alone", err)
	}
	want := []string{"owner-a", "owner-b", "owner-c"}
	for i, owner := range want {
		if i >= len(drafts.owners) || drafts.owners[i] != owner || drafts.ctxOwners[i] != owner {
			t.Fatalf("recovered owners = %v (ctx %v), want %v on both", drafts.owners, drafts.ctxOwners, want)
		}
	}
	if len(drafts.owners) != len(want) {
		t.Fatalf("recovered owners = %v, want %v", drafts.owners, want)
	}
}

func TestRecoverMessageDraftsReportsAFailedListing(t *testing.T) {
	listErr := errors.New("list failed")
	drafts := &draftRecovererFake{}
	if _, err := recoverMessageDraftsByIdentity(t.Context(), expiryIdentityListerFake{err: listErr}, drafts); !errors.Is(err, listErr) {
		t.Fatalf("err = %v, want the listing error", err)
	}
	if len(drafts.owners) != 0 {
		t.Fatalf("recovered %v without a roster", drafts.owners)
	}
}

func TestMessageDraftRecoverySweeperRunsTheBootSweep(t *testing.T) {
	drafts := &draftRecovererFake{}
	sweeper := newMessageDraftRecoverySweeper(drafts, expiryIdentityListerFake{identities: []identity.Identity{{ID: "owner-a"}}})
	sweeper.SweepNow(t.Context())
	if len(drafts.owners) != 1 || drafts.owners[0] != "owner-a" {
		t.Fatalf("boot sweep recovered %v, want owner-a", drafts.owners)
	}
	newMessageDraftRecoverySweeper(drafts, expiryIdentityListerFake{err: errors.New("list failed")}).SweepNow(t.Context())
}
