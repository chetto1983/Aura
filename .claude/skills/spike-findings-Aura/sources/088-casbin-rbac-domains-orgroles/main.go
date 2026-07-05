//go:build spike_casbin

// Spike 088: casbin-rbac-domains-orgroles
//
// The VALUE spike for the deferred Casbin phase: does Casbin's RBAC-with-domains
// deliver the SMB org-roles the DGX-Spark bundle wants — manager/employee/viewer
// per department, with role hierarchy that stays isolated per department (a
// manager in dept-A is NOT a manager in dept-B), all on ONE model that is also a
// superset of 086's flat capability wildcard?
//
// In-memory enforcer — this is a pure enforce-semantics question; persistence
// across the same model is already proven live in 087a/087b.
//
// Run: go run -tags spike_casbin ./.planning/spikes/088-casbin-rbac-domains-orgroles
package main

import (
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

// orgModel is the candidate PRODUCTION Casbin model — the strict superset that
// serves BOTH Phase 36's flat capabilities AND the future org-roles:
//
//   - RBAC-with-domains: r/p = sub, dom, obj, act ; g = _, _, _ (user→role PER domain).
//   - Each of dom/obj/act supports an explicit "*" wildcard OR exact match — so a
//     flat global capability (086's `local` wildcard) is expressible as
//     p(id, "*", "*", "*") in the SAME model, and org-role policies use real
//     (dept, resource, verb) triples.
//   - `g(r.sub, p.sub, r.dom)` is DOMAIN-SCOPED: role membership and role hierarchy
//     are per department, which is what isolates a manager@A from dept-B.
const orgModel = `
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && (p.dom == "*" || r.dom == p.dom) && (p.obj == "*" || r.obj == p.obj) && (p.act == "*" || r.act == p.act)
`

func build() (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(orgModel)
	if err != nil {
		return nil, err
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, err
	}

	// --- Role → permission policies, per department (dept-a and dept-b identical shape) ---
	for _, dom := range []string{"dept-a", "dept-b"} {
		add := func(role, obj, act string) { _, _ = e.AddPolicy(role, dom, obj, act) }
		add("role-manager", "budget", "write")  // managers own the budget
		add("role-employee", "ticket", "write") // employees work tickets
		add("role-viewer", "budget", "read")    // viewers read the budget
		// Role hierarchy WITHIN the domain: manager ⊃ employee ⊃ viewer.
		_, _ = e.AddGroupingPolicy("role-manager", "role-employee", dom)
		_, _ = e.AddGroupingPolicy("role-employee", "role-viewer", dom)
	}

	// --- User → role grants, PER domain (the isolation lever) ---
	_, _ = e.AddGroupingPolicy("u-alice", "role-manager", "dept-a") // alice manages A
	_, _ = e.AddGroupingPolicy("u-alice", "role-viewer", "dept-b")  // alice only views B
	_, _ = e.AddGroupingPolicy("u-bob", "role-employee", "dept-a")  // bob is an employee in A only

	// --- Flat global capability (086 parity): the operator/local wildcard, expressed
	// in the SAME model as a direct p(id, "*","*","*"). Self-link g(local,local,dom)
	// holds in every domain → allowed everywhere, reproducing capability_grants '*'. ---
	_, _ = e.AddPolicy("local", "*", "*", "*")

	return e, nil
}

type tc struct {
	sub, dom, obj, act string
	want               bool
	note               string
}

func main() {
	logf("SETUP", "spike 088 — casbin/v2 RBAC-with-domains org-roles (in-memory)")
	e, err := build()
	if err != nil {
		logf("ERROR", "build: %v", err)
		logf("SUMMARY", "INVALIDATED — model/enforcer would not build")
		os.Exit(1)
	}

	cases := []tc{
		// alice = manager in dept-a → full hierarchy in A
		{"u-alice", "dept-a", "budget", "write", true, "manager@A writes budget (direct)"},
		{"u-alice", "dept-a", "ticket", "write", true, "manager@A writes ticket (⊃ employee, hierarchy)"},
		{"u-alice", "dept-a", "budget", "read", true, "manager@A reads budget (⊃ viewer, hierarchy)"},
		// alice = viewer only in dept-b → CROSS-DOMAIN ISOLATION (manager role does NOT leak from A)
		{"u-alice", "dept-b", "budget", "read", true, "viewer@B reads budget (direct)"},
		{"u-alice", "dept-b", "budget", "write", false, "ISOLATION: alice is NOT manager in B → cannot write budget"},
		{"u-alice", "dept-b", "ticket", "write", false, "ISOLATION: alice is NOT employee in B → cannot write ticket"},
		// bob = employee in dept-a → employee+viewer perms, but NOT manager
		{"u-bob", "dept-a", "ticket", "write", true, "employee@A writes ticket (direct)"},
		{"u-bob", "dept-a", "budget", "read", true, "employee@A reads budget (⊃ viewer)"},
		{"u-bob", "dept-a", "budget", "write", false, "HIERARCHY ONE-WAY: employee is NOT manager → cannot write budget"},
		// bob has NO role in dept-b → nothing
		{"u-bob", "dept-b", "budget", "read", false, "ISOLATION: bob has no role in B → denied"},
		{"u-bob", "dept-b", "ticket", "write", false, "ISOLATION: bob has no role in B → denied"},
		// unknown principal
		{"u-ghost", "dept-a", "budget", "read", false, "unknown user → denied"},
		// flat global wildcard (086 parity) inside the same model
		{"local", "dept-a", "budget", "write", true, "086 parity: local '*' → allowed in any domain"},
		{"local", "dept-b", "ticket", "write", true, "086 parity: local '*' → allowed cross-domain"},
		{"local", "dept-a", "anything.random", "purge", true, "086 parity: local '*' → any obj/act"},
	}

	fails := 0
	for _, c := range cases {
		got, err := e.Enforce(c.sub, c.dom, c.obj, c.act)
		if err != nil {
			logf("ERROR", "enforce(%s,%s,%s,%s): %v", c.sub, c.dom, c.obj, c.act, err)
			fails++
			continue
		}
		status := "PASS"
		if got != c.want {
			status = "FAIL"
			fails++
		}
		logf("CASE", "%s  (%s @%s %s:%s) want=%-5v got=%-5v — %s", status, c.sub, c.dom, c.obj, c.act, c.want, got, c.note)
	}

	// Deep probe: role hierarchy is DOMAIN-SCOPED. GetImplicitRolesForUser returns
	// alice's effective roles in dept-a (manager→employee→viewer) vs dept-b (viewer only).
	rolesA, _ := e.GetImplicitRolesForUser("u-alice", "dept-a")
	rolesB, _ := e.GetImplicitRolesForUser("u-alice", "dept-b")
	logf("PROBE", "alice implicit roles: dept-a=%v dept-b=%v", rolesA, rolesB)
	if len(rolesA) != 3 || len(rolesB) != 1 {
		logf("CASE", "FAIL  domain-scoped hierarchy: want dept-a=3 roles, dept-b=1; got %d/%d", len(rolesA), len(rolesB))
		fails++
	} else {
		logf("CASE", "PASS  domain-scoped hierarchy: alice has 3 effective roles in A, 1 in B (no leak)")
	}

	logf("RESULT", "fails=%d over %d enforce cases + 1 hierarchy-scope probe", fails, len(cases))
	if fails == 0 {
		logf("SUMMARY", "VALIDATED — RBAC-with-domains delivers per-department manager/employee/viewer with domain-scoped hierarchy AND cross-domain isolation; one model is a superset of 086's flat wildcard")
		os.Exit(0)
	}
	logf("SUMMARY", "INVALIDATED — %d assertion(s) failed", fails)
	os.Exit(1)
}
