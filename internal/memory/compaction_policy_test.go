//go:build db_integration

package memory

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestPromoteDeniesNonexistentCandidate proves a candidateID with no matching row
// (never created, or already revoked/superseded/expired) surfaces as ErrPolicyDenied
// rather than leaking a raw "no rows" driver error across the authorization boundary.
func TestPromoteDeniesNonexistentCandidate(t *testing.T) {
	s := testStore(t)
	policy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: true}}}
	principal := Principal{TenantID: uuid.NewString(), OwnerID: uuid.NewString(), Purpose: "assistant_personalization", Region: "eu", Capabilities: []string{"memory.read"}, MaxSensitivity: "normal"}
	if _, err := s.Promote(t.Context(), policy, principal, uuid.NewString()); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("nonexistent candidate promote error=%v", err)
	}
}

// TestPromoteDeniesClassNotExplicitlyAllowlisted proves the per-class allowlist gate
// independent of ReviewApproved: an approved review with either a missing class entry
// or an explicit AllowPromotion=false must both fail closed with ErrPromotionDisabled.
func TestPromoteDeniesClassNotExplicitlyAllowlisted(t *testing.T) {
	s := testStore(t)
	principal := func(in CandidateInput) Principal {
		return Principal{TenantID: in.TenantID, OwnerID: in.OwnerID, Purpose: in.Purpose, Region: in.Region, Capabilities: []string{"memory.read"}, MaxSensitivity: "normal"}
	}
	t.Run("class absent from allowlist", func(t *testing.T) {
		in := candidateFixtureForStore(t, s)
		c, err := s.CreateCandidate(t.Context(), in)
		if err != nil {
			t.Fatal(err)
		}
		policy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{}}
		if _, err := s.Promote(t.Context(), policy, principal(in), c.ID); !errors.Is(err, ErrPromotionDisabled) {
			t.Fatalf("absent class promote error=%v", err)
		}
	})
	t.Run("class present but promotion disabled", func(t *testing.T) {
		in := candidateFixtureForStore(t, s)
		c, err := s.CreateCandidate(t.Context(), in)
		if err != nil {
			t.Fatal(err)
		}
		policy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: false}}}
		if _, err := s.Promote(t.Context(), policy, principal(in), c.ID); !errors.Is(err, ErrPromotionDisabled) {
			t.Fatalf("disabled class promote error=%v", err)
		}
	})
}

// TestPromoteDeniesMismatchedPrincipal proves Promote applies the same hard
// principalAllows boundary as Retrieve: an allowlisted, review-approved class still
// cannot be promoted into a Memory readable by a principal outside the candidate's
// tenant/owner/purpose/region/sensitivity envelope.
func TestPromoteDeniesMismatchedPrincipal(t *testing.T) {
	s := testStore(t)
	in := candidateFixtureForStore(t, s)
	c, err := s.CreateCandidate(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: true}}}
	mismatched := Principal{TenantID: in.TenantID, OwnerID: uuid.NewString(), Purpose: in.Purpose, Region: in.Region, Capabilities: []string{"memory.read"}, MaxSensitivity: "normal"}
	if _, err := s.Promote(t.Context(), policy, mismatched, c.ID); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("mismatched principal promote error=%v", err)
	}
}

// TestPromoteWrapsDriverErrorOnMalformedCandidateID proves a syntactically invalid
// (non-UUID) candidateID surfaces the generic wrapped driver error branch, not
// ErrPolicyDenied — distinguishing "candidate absent" from "candidate ID malformed".
func TestPromoteWrapsDriverErrorOnMalformedCandidateID(t *testing.T) {
	s := testStore(t)
	policy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: true}}}
	principal := Principal{TenantID: uuid.NewString(), OwnerID: uuid.NewString(), Purpose: "assistant_personalization", Region: "eu", Capabilities: []string{"memory.read"}, MaxSensitivity: "normal"}
	_, err := s.Promote(t.Context(), policy, principal, "not-a-uuid")
	if err == nil || errors.Is(err, ErrPolicyDenied) || !strings.Contains(err.Error(), "load candidate for promotion") {
		t.Fatalf("malformed candidate ID promote error=%v", err)
	}
}

// TestRetrieveFiltersClassNotAllowedForRetrieval proves the per-row second gate inside
// Retrieve: a memory whose class lacks (or explicitly denies) AllowRetrieval is dropped
// from results even though it passed every hard SQL-level boundary filter.
func TestRetrieveFiltersClassNotAllowedForRetrieval(t *testing.T) {
	s := testStore(t)
	in := candidateFixtureForStore(t, s)
	c, err := s.CreateCandidate(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	promotionPolicy := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: true, AllowRetrieval: true}}}
	p := Principal{TenantID: in.TenantID, OwnerID: in.OwnerID, Purpose: in.Purpose, Region: in.Region, Capabilities: []string{"memory.read"}, MaxSensitivity: "normal"}
	if _, err := s.Promote(t.Context(), promotionPolicy, p, c.ID); err != nil {
		t.Fatal(err)
	}
	retrievalDenied := Policy{ReviewApproved: true, Classes: map[string]ClassPolicy{"preference": {AllowPromotion: true, AllowRetrieval: false}}}
	got, err := s.Retrieve(t.Context(), retrievalDenied, p, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("retrieval-denied class leaked results=%+v", got)
	}
}

// TestSupersedeWrapsDriverErrorOnMalformedCandidateID proves a malformed (non-UUID)
// old-candidate identifier surfaces the wrapped "supersede candidate" driver error
// instead of a silent no-op.
func TestSupersedeWrapsDriverErrorOnMalformedCandidateID(t *testing.T) {
	s := testStore(t)
	err := s.Supersede(t.Context(), "not-a-uuid", "also-not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "supersede candidate") {
		t.Fatalf("malformed supersede error=%v", err)
	}
}

// TestRevokeWrapsDriverErrorOnMalformedIdentifier proves WithdrawConsent (revoke's
// public entry point) surfaces the wrapped "revoke candidates" driver error when given
// a syntactically invalid tenant identifier, rather than silently updating zero rows.
func TestRevokeWrapsDriverErrorOnMalformedIdentifier(t *testing.T) {
	s := testStore(t)
	err := s.WithdrawConsent(t.Context(), "not-a-uuid", uuid.NewString(), "assistant_personalization")
	if err == nil || !strings.Contains(err.Error(), "revoke candidates") {
		t.Fatalf("malformed revoke error=%v", err)
	}
}
