//go:build spike_casbin

// Spike 086: casbin-hascapability-backing
//
// Kill-risk probe for the deferred Casbin phase (Phase 36 CONTEXT §Deferred,
// D-04): can an Apache Casbin enforcer BACK the exact `HasCapability(ctx, id,
// cap) (bool, error)` contract the whole gateway keys on (internal/agui/auth.go
// `RequireCapability` → identityChecker.HasCapability), reproducing
// capability_grants.sql's `(capability = '*' OR capability = $2)` semantics
// byte-for-byte, with ZERO rework to the callers?
//
// The oracle is the shipped SQL logic (a 4-line EXISTS, deterministic — no live
// DB needed to establish ground truth). The harness drives a Casbin enforcer over
// the SAME grant set and asserts every matrix cell agrees with the oracle, then
// previews the org-roles superset (a role edge via the SAME matcher, zero change)
// to prove the model generalises without reworking the flat path.
//
// Run: go run -tags spike_casbin ./.planning/spikes/086-casbin-hascapability-backing
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

func ts() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
func logf(cat, format string, a ...any) {
	fmt.Printf("[%s] %s %s\n", ts(), cat, fmt.Sprintf(format, a...))
}

// consumerChecker mirrors the EXACT method the (unexported) identityChecker in
// internal/agui/auth.go requires from the capability store. If both the SQL
// oracle and the Casbin impl satisfy THIS, the swap is zero-rework at the type
// level — the composition root binds a different concrete type, nothing else moves.
type consumerChecker interface {
	HasCapability(ctx context.Context, id, capability string) (bool, error)
}

// sqlOracle reproduces capability_grants.sql HasCapability verbatim:
//
//	SELECT EXISTS(... WHERE identity_id = $1 AND (capability = '*' OR capability = $2))
//
// This IS the ground truth — the shipped behaviour Casbin must match.
type sqlOracle struct{ grants map[string]map[string]bool } // id -> cap-set

func (o *sqlOracle) HasCapability(_ context.Context, id, capability string) (bool, error) {
	caps := o.grants[id]
	if caps["*"] {
		return true, nil
	}
	return caps[capability], nil
}

// modelText is the candidate production Casbin model. It is RBAC (g present) so
// the org-roles forward bet (088) drops in with NO matcher change, yet flat
// grants work today because Casbin's default role manager treats g(x, x) as true
// (every subject links to itself). The wildcard is an explicit `p.obj == "*"`
// disjunct — NOT keyMatch/regexMatch — so a cap name's dots stay literal and
// there is no accidental prefix/hierarchy match (the real system has ONLY the
// full '*', no `settings.*`).
const modelText = `
[request_definition]
r = sub, obj

[policy_definition]
p = sub, obj

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && (p.obj == "*" || r.obj == p.obj)
`

type casbinChecker struct{ e *casbin.Enforcer }

func (c *casbinChecker) HasCapability(_ context.Context, id, capability string) (bool, error) {
	return c.e.Enforce(id, capability) // (bool, error) — identical shape to HasCapability
}

// compile-time proof: both concrete types satisfy the consumer seam.
var (
	_ consumerChecker = (*sqlOracle)(nil)
	_ consumerChecker = (*casbinChecker)(nil)
)

// grantset is the shared fixture both backends load: the seeded `local` (system
// '*' wildcard, migration 0004) + a normal admin user with one exact grant + a
// user with nothing.
var grantset = map[string]map[string]bool{
	"local":   {"*": true},                    // seeded wildcard identity
	"u-alice": {"settings.model.write": true}, // a granted admin cap
	"u-bob":   {},                             // no grants
}

func newSQLOracle() *sqlOracle { return &sqlOracle{grants: grantset} }

func newCasbin() (*casbinChecker, error) {
	m, err := model.NewModelFromString(modelText)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	e, err := casbin.NewEnforcer(m) // no adapter — in-memory policy (086 is a semantics probe)
	if err != nil {
		return nil, fmt.Errorf("enforcer: %w", err)
	}
	// Load the SAME grants as policies: a flat grant (id, cap) becomes p(id, cap).
	for id, caps := range grantset {
		for c := range caps {
			if _, err := e.AddPolicy(id, c); err != nil {
				return nil, fmt.Errorf("addpolicy %s/%s: %w", id, c, err)
			}
		}
	}
	return &casbinChecker{e: e}, nil
}

// matrix is the equivalence table: every (id, cap) the two backends must agree on.
// Cases deliberately probe wildcard match-all, exact-only, deny, the '*'-request
// edge, no-prefix/no-hierarchy, and empty inputs.
var matrix = []struct {
	id, cap, note string
}{
	{"local", "settings.model.write", "wildcard holder → any cap"},
	{"local", "identity.create", "wildcard holder → any cap"},
	{"local", "never.granted.anywhere", "wildcard holder → even unknown cap"},
	{"local", "*", "wildcard holder asked for literal '*'"},
	{"u-alice", "settings.model.write", "exact grant matches"},
	{"u-alice", "settings.model.read", "sibling cap must NOT match (no hierarchy)"},
	{"u-alice", "settings.model", "prefix must NOT match (no glob)"},
	{"u-alice", "settings.model.write.extra", "suffix must NOT match"},
	{"u-alice", "*", "non-wildcard user asked for '*' → deny"},
	{"u-alice", "SETTINGS.MODEL.WRITE", "case-sensitive → deny"},
	{"u-bob", "settings.model.write", "ungranted user → deny"},
	{"u-bob", "*", "ungranted user asked for '*' → deny"},
	{"u-ghost", "anything", "unknown identity → deny"},
	{"", "settings.model.write", "empty id → deny"},
	{"u-alice", "", "empty cap → deny"},
}

func main() {
	ctx := context.Background()
	logf("SETUP", "spike 086 — casbin backs HasCapability; casbin/v2 in-memory RBAC model")

	oracle := newSQLOracle()
	cb, err := newCasbin()
	if err != nil {
		logf("ERROR", "casbin build failed: %v", err)
		logf("SUMMARY", "INVALIDATED — enforcer would not build")
		os.Exit(1)
	}

	// Prove the swap at the interface boundary: the gateway holds a consumerChecker,
	// not a concrete type. Call the ENTIRE matrix through the interface var, twice,
	// once per backend, and compare.
	var backendUnderTest consumerChecker = cb
	var groundTruth consumerChecker = oracle

	logf("PROBE", "equivalence matrix: casbin vs capability_grants.sql oracle (%d cases)", len(matrix))
	mismatches := 0
	for _, tc := range matrix {
		want, errW := groundTruth.HasCapability(ctx, tc.id, tc.cap)
		got, errG := backendUnderTest.HasCapability(ctx, tc.id, tc.cap)
		if errW != nil || errG != nil {
			logf("ERROR", "(%q,%q) oracle_err=%v casbin_err=%v", tc.id, tc.cap, errW, errG)
			mismatches++
			continue
		}
		status := "MATCH"
		if want != got {
			status = "MISMATCH"
			mismatches++
		}
		logf("CASE", "%-8s (%q, %q) want=%-5v got=%-5v — %s", status, tc.id, tc.cap, want, got, tc.note)
	}

	// Superset preview: add a ROLE edge (org-roles forward bet, spike 088) with the
	// SAME matcher — proves flat grants + roles coexist with zero model rework.
	logf("PROBE", "org-roles superset preview — add g(u-alice, role-admin) + p(role-admin, governance.write)")
	if _, err := cb.e.AddGroupingPolicy("u-alice", "role-admin"); err != nil {
		logf("ERROR", "add grouping: %v", err)
		mismatches++
	}
	if _, err := cb.e.AddPolicy("role-admin", "governance.write"); err != nil {
		logf("ERROR", "add role policy: %v", err)
		mismatches++
	}
	viaRole, _ := cb.HasCapability(ctx, "u-alice", "governance.write")
	flatStill, _ := cb.HasCapability(ctx, "u-alice", "settings.model.write")
	bobUnaffected, _ := cb.HasCapability(ctx, "u-bob", "governance.write")
	logf("CASE", "role-inherit  (u-alice, governance.write) via role → %v (want true)", viaRole)
	logf("CASE", "flat-intact   (u-alice, settings.model.write)      → %v (want true)", flatStill)
	logf("CASE", "no-leak       (u-bob, governance.write)            → %v (want false)", bobUnaffected)
	if !viaRole || !flatStill || bobUnaffected {
		mismatches++
	}

	logf("RESULT", "mismatches=%d over %d equivalence cases + 3 superset assertions", mismatches, len(matrix))
	if mismatches == 0 {
		logf("SUMMARY", "VALIDATED — casbin/v2 reproduces HasCapability byte-identically AND the RBAC model is a zero-rework superset for org-roles")
		os.Exit(0)
	}
	logf("SUMMARY", "INVALIDATED — %d divergence(s) from the SQL oracle", mismatches)
	os.Exit(1)
}
