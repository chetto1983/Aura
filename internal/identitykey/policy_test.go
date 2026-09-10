package identitykey

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestDecide_NoKeyOnOpenRouterRefuses(t *testing.T) {
	t.Parallel()
	decision, err := Decide(DecisionInput{IdentityID: "identity-a", HasKey: false, BackendBills: true})
	if decision != DecisionRefuseNoKey {
		t.Fatalf("decision = %v, want DecisionRefuseNoKey", decision)
	}
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v, want ErrNoKey", err)
	}
}

// capUSD stays a real helper rather than an inlined new(expr) (Go 1.26): most callers
// below pass an integer literal (capUSD(0), capUSD(5)), which new(x) types as *int, not
// *float64 — a compile error against the fields it targets.
//
//nolint:modernize // inlining only compiles for callers passing an explicit float literal.
func capUSD(v float64) *float64 { return &v }

func TestDecideAllowsAKeyWithNoLimit(t *testing.T) {
	decision, err := Decide(DecisionInput{IdentityID: "identity-a", HasKey: true, LimitUSD: nil, BackendBills: true})
	if err != nil || decision != DecisionAllow {
		t.Fatalf("no limit: decision = %v, err = %v, want DecisionAllow and nil", decision, err)
	}
}

func TestDecide_ZeroCapRefuses(t *testing.T) {
	t.Parallel()
	decision, err := Decide(DecisionInput{IdentityID: "identity-a", HasKey: true, LimitUSD: capUSD(0), BackendBills: true})
	if decision != DecisionRefuseNoCredit {
		t.Fatalf("decision = %v, want DecisionRefuseNoCredit", decision)
	}
	if !errors.Is(err, ErrNoCredit) {
		t.Fatalf("err = %v, want ErrNoCredit", err)
	}
}

func TestDecide_PositiveCapAllows(t *testing.T) {
	t.Parallel()
	decision, err := Decide(DecisionInput{IdentityID: "identity-a", HasKey: true, LimitUSD: capUSD(5), BackendBills: true})
	if decision != DecisionAllow {
		t.Fatalf("decision = %v, want DecisionAllow", decision)
	}
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestDecide_LocalBackendIsExempt(t *testing.T) {
	t.Parallel()
	// "whether or not a record exists" — asserted with HasKey both false and true.
	for _, hasKey := range []bool{false, true} {
		decision, err := Decide(DecisionInput{IdentityID: "identity-a", HasKey: hasKey, LimitUSD: capUSD(0), BackendBills: false})
		if decision != DecisionExemptLocal {
			t.Fatalf("HasKey=%v: decision = %v, want DecisionExemptLocal", hasKey, decision)
		}
		if !errors.Is(err, llm.ErrSpendNotApplicable) {
			t.Fatalf("HasKey=%v: err = %v, want llm.ErrSpendNotApplicable", hasKey, err)
		}
	}
}

// TestDecide_LocalBackendWithNoKeyIsExemptNotRefused pins the ordering: the
// exemption must be reached BEFORE the no-key refusal, or a local backend
// with no key would be wrongly refused instead of exempted.
func TestDecide_LocalBackendWithNoKeyIsExemptNotRefused(t *testing.T) {
	t.Parallel()
	decision, err := Decide(DecisionInput{IdentityID: "identity-a", HasKey: false, BackendBills: false})
	if decision != DecisionExemptLocal {
		t.Fatalf("decision = %v, want DecisionExemptLocal (not DecisionRefuseNoKey)", decision)
	}
	if errors.Is(err, ErrNoKey) {
		t.Fatal("err wraps ErrNoKey — the no-key refusal path fired instead of the exemption")
	}
}

func TestDecide_CapBoundaryExactlyZero(t *testing.T) {
	t.Parallel()
	zero, err := Decide(DecisionInput{IdentityID: "identity-a", HasKey: true, LimitUSD: capUSD(0), BackendBills: true})
	if zero != DecisionRefuseNoCredit {
		t.Fatalf("LimitUSD=0: decision = %v, want DecisionRefuseNoCredit", zero)
	}
	if !errors.Is(err, ErrNoCredit) {
		t.Fatalf("LimitUSD=0: err = %v, want ErrNoCredit", err)
	}
	oneCent, _ := Decide(DecisionInput{IdentityID: "identity-a", HasKey: true, LimitUSD: new(0.01), BackendBills: true})
	if oneCent != DecisionAllow {
		t.Fatalf("LimitUSD=0.01: decision = %v, want DecisionAllow", oneCent)
	}
}

// TestDecide_CapPrecisionDoesNotRoundToZero is the CRED-02 precision edge: a
// cap below one cent but above zero must never become zero through the
// decision — a nonzero balance must never round into a refusal.
func TestDecide_CapPrecisionDoesNotRoundToZero(t *testing.T) {
	t.Parallel()
	decision, err := Decide(DecisionInput{IdentityID: "identity-a", HasKey: true, LimitUSD: new(0.004), BackendBills: true})
	if decision != DecisionAllow {
		t.Fatalf("LimitUSD=0.004: decision = %v, want DecisionAllow", decision)
	}
	if err != nil {
		t.Fatalf("LimitUSD=0.004: err = %v, want nil", err)
	}
}

func TestDecide_EmptyIdentityIDRefuses(t *testing.T) {
	t.Parallel()
	decision, err := Decide(DecisionInput{IdentityID: "", HasKey: true, LimitUSD: capUSD(5), BackendBills: true})
	if decision == DecisionAllow {
		t.Fatal("empty identity id decided DecisionAllow — deny by default violated")
	}
	if !errors.Is(err, ErrEmptyIdentityID) {
		t.Fatalf("err = %v, want ErrEmptyIdentityID", err)
	}
	// Also deny-by-default on a whitespace-only id.
	decision, err = Decide(DecisionInput{IdentityID: "   ", HasKey: true, LimitUSD: capUSD(5), BackendBills: true})
	if decision == DecisionAllow {
		t.Fatal("whitespace identity id decided DecisionAllow — deny by default violated")
	}
	if !errors.Is(err, ErrEmptyIdentityID) {
		t.Fatalf("err = %v, want ErrEmptyIdentityID", err)
	}
}
