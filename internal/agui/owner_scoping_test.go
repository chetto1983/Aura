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

func (o *ownerConvStore) UpdateReasoningEffortForIdentity(_ context.Context, id, identity, _ string) (int64, error) {
	return o.ownedRows(id, identity), nil
}

// TestConversationsAPI_ForeignReadIs404 proves a GET of another identity's conversation is
// 404 (the read hides its existence — D-06).
func TestConversationsAPI_ForeignReadIs404(t *testing.T) {
	foreignOwner := uuid.Must(uuid.NewV7()).String() // NOT local → the caller does not own it
	srv := convAPIServer(t, newOwnerConvStore(goodID, foreignOwner))

	for _, path := range []string{
		"/api/conversations/" + goodID,
		"/api/conversations/" + goodID + "/rot-events",
		"/api/conversations/" + goodID + "/branches", // D-09 branch enumeration is owner-gated too
	} {
		if code, body := req(t, srv, http.MethodGet, path, ""); code != http.StatusNotFound {
			t.Errorf("GET %s of a foreign conversation = %d, want 404: %s", path, code, body)
		}
	}
}

// TestConversationsBranchAPI_ForeignMutateIsHidden proves the D-09 branch mutating routes
// (edit = fork+re-run, select = re-run a leaf) are owner-gated: a foreign identity gets 404
// (existence hidden) and can neither fork nor drive another identity's thread. Regression
// guard for the Phase-36 branch-route isolation hole (the routes previously ran unscoped).
func TestConversationsBranchAPI_ForeignMutateIsHidden(t *testing.T) {
	foreignOwner := uuid.Must(uuid.NewV7()).String()
	srv := convAPIServer(t, newOwnerConvStore(goodID, foreignOwner))

	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/conversations/" + goodID + "/edit", `{"diverge_seq":1,"content":"x"}`},
		{http.MethodPost, "/api/conversations/" + goodID + "/branches/1/select", ""},
	}
	for _, c := range cases {
		if code, body := req(t, srv, c.method, c.path, c.body); code != http.StatusNotFound {
			t.Errorf("%s %s of a foreign conversation = %d, want 404 (existence hidden): %s", c.method, c.path, code, body)
		}
	}
}

// TestConversationsAPI_ForeignMutateIs404 proves a delete/archive/rename of a KNOWN-foreign
// conversation is 404 — the SAME answer an absent id gets, so the caller cannot tell the two
// apart.
//
// It asserted 403 until migration 0089. D-06 asked for that split, and the handler produced
// it by probing existence on the unscoped base Get — which only ever worked because the
// 0032 policy was permissive when app.current_identity was unset. With aura.conversations
// fail-closed there is no read left that can observe a foreign row, so the distinction is
// gone at the source, not merely stopped being reported. Rewritten rather than deleted
// because the case it covers — a mutate of someone else's id — still needs a pinned status,
// and the safer one is now the only reachable one. It matches what share_api.go already
// does for share links (SC4 row 6: any error is 404, never 403).
func TestConversationsAPI_ForeignMutateIs404(t *testing.T) {
	foreignOwner := uuid.Must(uuid.NewV7()).String()
	srv := convAPIServer(t, newOwnerConvStore(goodID, foreignOwner))

	cases := []struct{ method, path, body string }{
		{http.MethodDelete, "/api/conversations/" + goodID, ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/archive", ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/unarchive", ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/rename", `{"title":"x"}`},
	}
	for _, c := range cases {
		if code, body := req(t, srv, c.method, c.path, c.body); code != http.StatusNotFound {
			t.Errorf("%s %s of a foreign conversation = %d, want 404 (existence hidden): %s", c.method, c.path, code, body)
		}
	}
}

// TestConversationsAPI_AbsentMutateIs404 proves a mutate of an id that does not exist at all
// is 404 — indistinguishable from the foreign case above, which is the point.
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

// TestApprovalsAPI_ResolveForeignIs404 proves resolving another identity's approval is 404
// and never reaches the Runner. It asserted 403 until migration 0089 made
// aura.paused_states fail closed: the unscoped existence probe that produced the 403 can no
// longer see a foreign pause, so foreign and unknown collapse to the same answer. What the
// test still guards is the part that matters — the resolve does not reach SubmitAnswers.
func TestApprovalsAPI_ResolveForeignIs404(t *testing.T) {
	token := uuid.Must(uuid.NewV7()).String()
	foreignOwner := uuid.Must(uuid.NewV7()).String()
	run := &scriptedRunner{}
	srv := newResolveTestServer(t, run, &ownerApprovalStore{token: token, owner: foreignOwner})

	if code, body := postResolve(t, srv, token, `{"action":"accept"}`); code != http.StatusNotFound {
		t.Fatalf("resolve of a foreign approval = %d, want 404: %s", code, body)
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

	// 200 (was 204): resolve now answers with the ResolveDirective projection (Phase A) — the
	// owner gate this test gates on is unchanged, only the success status carries a body.
	if code, body := postResolve(t, srv, token, `{"action":"accept","content":"ok"}`); code != http.StatusOK {
		t.Fatalf("owner resolve = %d, want 200: %s", code, body)
	}
	if run.gotAnswers == nil || run.gotAnswers[token].Action != askuser.ActionAccept {
		t.Errorf("owner resolve did not reach SubmitAnswer with accept; got %v", run.gotAnswers)
	}
}
