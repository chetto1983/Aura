package agui

import (
	"context"
	"net/http"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/google/uuid"
)

// owner_scoping_test.go covers the Phase-36 D-06 cross-identity semantics DB-free: a read
// of a foreign resource is 404 (existence hidden), a mutate of a KNOWN-foreign resource is
// 403, and an absent resource is 404. The httptest requests carry no auth middleware, so
// scopedIdentityID(ctx) resolves to the seeded `local` id — the "caller". An owner set to a
// DIFFERENT uuid models "A owns it, local (B) is the caller"; owner == local models the
// caller's own resource. The route→store mapping over a live DB is the db_integration suite.

// ownerConvStore models one owned conversation over the fakeConvStore no-op base. Get (the
// unscoped 403-vs-404 existence probe) always finds convID; the *ForIdentity methods succeed
// only for the owner.
type ownerConvStore struct {
	*fakeConvStore
	convID string
	owner  string
}

func newOwnerConvStore(convID, owner string) *ownerConvStore {
	return &ownerConvStore{fakeConvStore: &fakeConvStore{}, convID: convID, owner: owner}
}

func (o *ownerConvStore) Get(_ context.Context, id string) (conversations.Conversation, error) {
	if id == o.convID {
		return conversations.Conversation{ID: id, IdentityID: o.owner}, nil
	}
	return conversations.Conversation{}, conversations.ErrConversationNotFound
}

func (o *ownerConvStore) GetForIdentity(_ context.Context, id, identity string) (conversations.Conversation, error) {
	if id == o.convID && identity == o.owner {
		return conversations.Conversation{ID: id, IdentityID: o.owner}, nil
	}
	return conversations.Conversation{}, conversations.ErrConversationNotFound
}

func (o *ownerConvStore) ownedRows(id, identity string) int64 {
	if id == o.convID && identity == o.owner {
		return 1
	}
	return 0
}

func (o *ownerConvStore) DeleteForIdentity(_ context.Context, id, identity string) (int64, error) {
	return o.ownedRows(id, identity), nil
}

func (o *ownerConvStore) UpdateStatusForIdentity(_ context.Context, id, identity, _ string) (int64, error) {
	return o.ownedRows(id, identity), nil
}

func (o *ownerConvStore) RenameForIdentity(_ context.Context, id, identity, _ string) (int64, error) {
	return o.ownedRows(id, identity), nil
}

// TestConversationsAPI_ForeignReadIs404 proves a GET of another identity's conversation is
// 404 (the read hides its existence — D-06).
func TestConversationsAPI_ForeignReadIs404(t *testing.T) {
	foreignOwner := uuid.Must(uuid.NewV7()).String() // NOT local → the caller does not own it
	srv := convAPIServer(t, newOwnerConvStore(goodID, foreignOwner))

	for _, path := range []string{"/api/conversations/" + goodID, "/api/conversations/" + goodID + "/rot-events"} {
		if code, body := req(t, srv, http.MethodGet, path, ""); code != http.StatusNotFound {
			t.Errorf("GET %s of a foreign conversation = %d, want 404: %s", path, code, body)
		}
	}
}

// TestConversationsAPI_ForeignMutateIs403 proves a delete/archive/rename of a KNOWN-foreign
// conversation is 403 (a mutate on a resource the caller may not touch — D-06), distinct
// from the 404 an absent id gets.
func TestConversationsAPI_ForeignMutateIs403(t *testing.T) {
	foreignOwner := uuid.Must(uuid.NewV7()).String()
	srv := convAPIServer(t, newOwnerConvStore(goodID, foreignOwner))

	cases := []struct{ method, path, body string }{
		{http.MethodDelete, "/api/conversations/" + goodID, ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/archive", ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/unarchive", ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/rename", `{"title":"x"}`},
	}
	for _, c := range cases {
		if code, body := req(t, srv, c.method, c.path, c.body); code != http.StatusForbidden {
			t.Errorf("%s %s of a foreign conversation = %d, want 403: %s", c.method, c.path, code, body)
		}
	}
}

// TestConversationsAPI_AbsentMutateIs404 proves a mutate of an id that does not exist at all
// is 404 (the existence probe misses), never 403.
func TestConversationsAPI_AbsentMutateIs404(t *testing.T) {
	absent := "22222222-2222-2222-2222-222222222222"
	srv := convAPIServer(t, newOwnerConvStore(goodID, localIdentityID)) // goodID is owned; `absent` is not modeled

	if code, body := req(t, srv, http.MethodDelete, "/api/conversations/"+absent, ""); code != http.StatusNotFound {
		t.Errorf("DELETE of an absent conversation = %d, want 404: %s", code, body)
	}
}

// TestConversationsAPI_OwnerReadAndMutateSucceed proves the owner (here local, the httptest
// caller) reads (200) and mutates (204) their own conversation — no self-lockout.
func TestConversationsAPI_OwnerReadAndMutateSucceed(t *testing.T) {
	srv := convAPIServer(t, newOwnerConvStore(goodID, localIdentityID)) // owner == the caller

	if code, body := req(t, srv, http.MethodGet, "/api/conversations/"+goodID, ""); code != http.StatusOK {
		t.Errorf("owner GET = %d, want 200: %s", code, body)
	}
	if code, body := req(t, srv, http.MethodDelete, "/api/conversations/"+goodID, ""); code != http.StatusNoContent {
		t.Errorf("owner DELETE = %d, want 204: %s", code, body)
	}
	if code, body := req(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/archive", ""); code != http.StatusNoContent {
		t.Errorf("owner archive = %d, want 204: %s", code, body)
	}
}

// ownerApprovalStore models one owned pause for the resolve ownership-gate tests.
type ownerApprovalStore struct {
	token string
	owner string
}

func (o *ownerApprovalStore) ListPendingAll(context.Context, int) ([]askuser.Pending, error) {
	return nil, nil
}

func (o *ownerApprovalStore) ListPendingAllForIdentity(context.Context, string, int) ([]askuser.Pending, error) {
	return nil, nil
}

func (o *ownerApprovalStore) GetByTokenForIdentity(_ context.Context, token, identity string) (askuser.Pending, error) {
	if token == o.token && identity == o.owner {
		return askuser.Pending{Token: token}, nil
	}
	return askuser.Pending{}, askuser.ErrPauseNotFound
}

func (o *ownerApprovalStore) GetByToken(_ context.Context, token string) (askuser.Pending, error) {
	if token == o.token {
		return askuser.Pending{Token: token}, nil
	}
	return askuser.Pending{}, askuser.ErrPauseNotFound
}

// TestApprovalsAPI_ResolveForeignIs403 proves resolving another identity's approval is 403
// (a known-foreign mutate) and never reaches the Runner.
func TestApprovalsAPI_ResolveForeignIs403(t *testing.T) {
	token := uuid.Must(uuid.NewV7()).String()
	foreignOwner := uuid.Must(uuid.NewV7()).String()
	run := &scriptedRunner{}
	srv := newResolveTestServer(t, run, &ownerApprovalStore{token: token, owner: foreignOwner})

	if code, body := postResolve(t, srv, token, `{"action":"accept"}`); code != http.StatusForbidden {
		t.Fatalf("resolve of a foreign approval = %d, want 403: %s", code, body)
	}
	if run.gotAnswers != nil {
		t.Errorf("a foreign resolve must not reach SubmitAnswers; got %v", run.gotAnswers)
	}
}

// TestApprovalsAPI_ResolveUnknownTokenIs404 proves resolving a token that exists for no one
// is 404 (not 403) — the existence probe misses.
func TestApprovalsAPI_ResolveUnknownTokenIs404(t *testing.T) {
	known := uuid.Must(uuid.NewV7()).String()
	unknown := uuid.Must(uuid.NewV7()).String()
	run := &scriptedRunner{}
	srv := newResolveTestServer(t, run, &ownerApprovalStore{token: known, owner: localIdentityID})

	if code, body := postResolve(t, srv, unknown, `{"action":"accept"}`); code != http.StatusNotFound {
		t.Fatalf("resolve of an unknown approval = %d, want 404: %s", code, body)
	}
	if run.gotAnswers != nil {
		t.Errorf("an unknown resolve must not reach SubmitAnswers; got %v", run.gotAnswers)
	}
}

// TestApprovalsAPI_ResolveOwnedReachesRunner proves the owner's resolve passes the gate and
// drives the Runner (the atomic resume) — 204.
func TestApprovalsAPI_ResolveOwnedReachesRunner(t *testing.T) {
	token := uuid.Must(uuid.NewV7()).String()
	run := &scriptedRunner{}
	srv := newResolveTestServer(t, run, &ownerApprovalStore{token: token, owner: localIdentityID})

	if code, body := postResolve(t, srv, token, `{"action":"accept","content":"ok"}`); code != http.StatusNoContent {
		t.Fatalf("owner resolve = %d, want 204: %s", code, body)
	}
	if run.gotAnswers == nil || run.gotAnswers[token].Action != askuser.ActionAccept {
		t.Errorf("owner resolve did not reach SubmitAnswers with accept; got %v", run.gotAnswers)
	}
}
