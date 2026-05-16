package learning_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/learning"
	"github.com/aura/aura/internal/storage/memoryindex"
)

// fakeAuthz is a test-only Authorizer that returns a fixed decision.
type fakeAuthz struct {
	dec identity.Decision
	err error
}

func (f fakeAuthz) Authorize(_ context.Context, _ identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	return identity.AuthorizationDecision{Decision: f.dec, Reason: "fake"}, f.err
}

func TestWriteApprovedUserFact_Approve(t *testing.T) {
	store := openTestMemStore(t)
	ctx := context.Background()

	handle := "user_memory:aabbccddeeff0011"
	p := learning.Proposal{
		Kind:          "user_memory",
		Fact:          "User prefers dark mode in all editors.",
		SignatureHash: handle,
		Category:      "preference",
	}
	if err := learning.WriteApprovedUserFact(ctx, store, p); err != nil {
		t.Fatalf("WriteApprovedUserFact: %v", err)
	}

	docs, err := store.Search(ctx, handle, memoryindex.Filter{
		Kinds: []string{memoryindex.KindUserMemory},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var found bool
	for _, d := range docs {
		if d.Handle == handle {
			found = true
			if d.Kind != memoryindex.KindUserMemory {
				t.Errorf("kind: got %q, want %q", d.Kind, memoryindex.KindUserMemory)
			}
		}
	}
	if !found {
		t.Errorf("document with Handle=%q not found in search results", handle)
	}
}

func TestWriteApprovedUserFact_DoubleApprove(t *testing.T) {
	store := openTestMemStore(t)
	ctx := context.Background()

	handle := "user_memory:1122334455667788"
	p := learning.Proposal{
		Kind:          "user_memory",
		Fact:          "User lives in Milan.",
		SignatureHash: handle,
		Category:      "person",
	}
	for i := 0; i < 2; i++ {
		if err := learning.WriteApprovedUserFact(ctx, store, p); err != nil {
			t.Fatalf("WriteApprovedUserFact run %d: %v", i+1, err)
		}
	}

	docs, err := store.Search(ctx, handle, memoryindex.Filter{
		Kinds: []string{memoryindex.KindUserMemory},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var count int
	for _, d := range docs {
		if d.Handle == handle {
			count++
		}
	}
	if count != 1 {
		t.Errorf("double-approve: expected exactly 1 row, got %d", count)
	}
}

func TestWriteApprovedUserFact_PatchUpdatesBody(t *testing.T) {
	store := openTestMemStore(t)
	ctx := context.Background()

	handle := "user_memory:deadbeef00112233"
	p1 := learning.Proposal{
		Kind:          "user_memory",
		Fact:          "User likes coffee.",
		SignatureHash: handle,
		Category:      "preference",
	}
	if err := learning.WriteApprovedUserFact(ctx, store, p1); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	p2 := learning.Proposal{
		Kind:          "user_memory",
		Fact:          "User likes tea (updated preference).",
		SignatureHash: handle,
		Category:      "preference",
	}
	if err := learning.WriteApprovedUserFact(ctx, store, p2); err != nil {
		t.Fatalf("patch write: %v", err)
	}

	docs, err := store.Search(ctx, handle, memoryindex.Filter{
		Kinds: []string{memoryindex.KindUserMemory},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var found *memoryindex.Document
	for i, d := range docs {
		if d.Handle == handle {
			found = &docs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("document not found after patch")
	}
	if found.Body != p2.Fact {
		t.Errorf("body after patch: got %q, want %q", found.Body, p2.Fact)
	}
}

func TestWriteApprovedUserFact_WrongKind(t *testing.T) {
	store := openTestMemStore(t)
	ctx := context.Background()

	handle := "user_memory:noop9988776655aa"
	p := learning.Proposal{
		Kind:          "wiki",
		Fact:          "Some wiki knowledge.",
		SignatureHash: handle,
	}
	if err := learning.WriteApprovedUserFact(ctx, store, p); err != nil {
		t.Fatalf("WriteApprovedUserFact wrong-kind: %v", err)
	}

	docs, err := store.Search(ctx, handle, memoryindex.Filter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, d := range docs {
		if d.Handle == handle {
			t.Error("wrong-kind: unexpected user_memory row created for non-user_memory proposal")
		}
	}
}

// TestWriteApprovedUserFactAs_OwnerWithGrant: actor holding CapabilityMemoryUserWrite → write succeeds.
func TestWriteApprovedUserFactAs_OwnerWithGrant(t *testing.T) {
	store := openTestMemStore(t)
	authz := fakeAuthz{dec: identity.DecisionAllow}
	actor := identity.Actor{ID: "actor:owner:test01"}
	p := learning.Proposal{
		Kind:          "user_memory",
		Fact:          "Owner prefers light mode.",
		SignatureHash: "user_memory:owner0001",
		Category:      "preference",
	}
	if err := learning.WriteApprovedUserFactAs(context.Background(), store, authz, actor, p); err != nil {
		t.Fatalf("WriteApprovedUserFactAs with grant: %v", err)
	}
	docs, err := store.Search(context.Background(), p.SignatureHash, memoryindex.Filter{Kinds: []string{memoryindex.KindUserMemory}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var found bool
	for _, d := range docs {
		if d.Handle == p.SignatureHash {
			found = true
		}
	}
	if !found {
		t.Error("owner-with-grant: document not written to store")
	}
}

// TestWriteApprovedUserFactAs_DashboardWithoutGrant: actor without grant → ErrPermissionDenied + no write.
func TestWriteApprovedUserFactAs_DashboardWithoutGrant(t *testing.T) {
	store := openTestMemStore(t)
	authz := fakeAuthz{dec: identity.DecisionDeny}
	actor := identity.Actor{ID: "actor:dashboard:test02"}
	p := learning.Proposal{
		Kind:          "user_memory",
		Fact:          "Dashboard user secret.",
		SignatureHash: "user_memory:deny0002",
		Category:      "preference",
	}
	err := learning.WriteApprovedUserFactAs(context.Background(), store, authz, actor, p)
	if !errors.Is(err, identity.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
	docs, searchErr := store.Search(context.Background(), p.SignatureHash, memoryindex.Filter{})
	if searchErr != nil {
		t.Fatalf("Search: %v", searchErr)
	}
	for _, d := range docs {
		if d.Handle == p.SignatureHash {
			t.Error("dashboard-without-grant: document must NOT be written on denial")
		}
	}
}

// TestWriteApprovedUserFactAs_NilActorBackCompat: nil authz + zero actor → system-actor path, write allowed.
func TestWriteApprovedUserFactAs_NilActorBackCompat(t *testing.T) {
	store := openTestMemStore(t)
	p := learning.Proposal{
		Kind:          "user_memory",
		Fact:          "System write back-compat.",
		SignatureHash: "user_memory:system0003",
		Category:      "fact",
	}
	if err := learning.WriteApprovedUserFactAs(context.Background(), store, nil, identity.Actor{}, p); err != nil {
		t.Fatalf("WriteApprovedUserFactAs system-actor: %v", err)
	}
	docs, err := store.Search(context.Background(), p.SignatureHash, memoryindex.Filter{Kinds: []string{memoryindex.KindUserMemory}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var found bool
	for _, d := range docs {
		if d.Handle == p.SignatureHash {
			found = true
		}
	}
	if !found {
		t.Error("system-actor back-compat: document not found in store")
	}
}

// TestWriteApprovedUserFactAs_FakeAuthzDeny: fake authorizer explicitly returning DecisionDeny → ErrPermissionDenied + no write.
func TestWriteApprovedUserFactAs_FakeAuthzDeny(t *testing.T) {
	store := openTestMemStore(t)
	authz := fakeAuthz{dec: identity.DecisionDeny}
	actor := identity.Actor{ID: "actor:fake:deny0004"}
	p := learning.Proposal{
		Kind:          "user_memory",
		Fact:          "Should be denied.",
		SignatureHash: "user_memory:fakedeny0004",
		Category:      "preference",
	}
	err := learning.WriteApprovedUserFactAs(context.Background(), store, authz, actor, p)
	if !errors.Is(err, identity.ErrPermissionDenied) {
		t.Fatalf("fake-deny: expected ErrPermissionDenied, got %v", err)
	}
	docs, searchErr := store.Search(context.Background(), p.SignatureHash, memoryindex.Filter{})
	if searchErr != nil {
		t.Fatalf("Search: %v", searchErr)
	}
	for _, d := range docs {
		if d.Handle == p.SignatureHash {
			t.Error("fake-deny: document must NOT be written on denial")
		}
	}
}
