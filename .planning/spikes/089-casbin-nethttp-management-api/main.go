//go:build spike_casbin

// Spike 089: casbin-nethttp-management-api
//
// The integration spike: wire the Casbin enforcer through the ACTUAL gateway shape
// Aura already uses (internal/agui/auth.go `RequireCapability` — read the principal
// off the request context, ask the authz backend, 403-or-pass), driven with
// httptest; prove a live admin management API (grant/revoke ≡ Casbin grouping /
// policy mutation) takes effect at runtime in-process (D-26: CLI + Settings both
// mutate the same store); and CLOSE the 086 landmine — the `*`-is-system-managed +
// capability-name grammar guards are NOT in the Casbin model, so the management API
// must reuse Aura's real `identity.ValidateCapabilityName` before writing policy.
//
// Run: go run -tags spike_casbin ./.planning/spikes/089-casbin-nethttp-management-api
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"

	"github.com/chetto1983/aura/internal/identity"
)

func ts() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
func logf(cat, format string, a ...any) {
	fmt.Printf("[%s] %s %s\n", ts(), cat, fmt.Sprintf(format, a...))
}

// orgModel — the same superset model validated in 088 (RBAC-with-domains + wildcard).
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

// principalKey mirrors internal/agui/auth.go: the authenticated identity is stashed
// on the context by the auth boundary; the capability gate reads it (never a client
// header). A private type prevents cross-package key collision.
type principalKey struct{}

func withPrincipal(r *http.Request, id string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, id))
}
func principalFrom(ctx context.Context) string {
	id, _ := ctx.Value(principalKey{}).(string)
	return id
}

// requireCasbin is the Casbin-backed twin of internal/agui/auth.go RequireCapability:
// it reads the principal the auth boundary stashed, resolves the request's domain
// (here from X-Dept, in prod from the routed tenant), asks the enforcer for
// (principal, dom, obj, act), and 403s on a missing principal, an enforcer error, or
// a deny — byte-identical control flow to the shipped `if err != nil || !ok { 403 }`.
func requireCasbin(next http.Handler, e *casbin.Enforcer, obj, act string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := principalFrom(r.Context())
		if id == "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		dom := r.Header.Get("X-Dept")
		if dom == "" {
			dom = "global"
		}
		ok, err := e.Enforce(id, dom, obj, act)
		if err != nil || !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var errWildcardOrBadName = errors.New("capability rejected by ValidateCapabilityName")

// grantPermission is the management-API write path (D-26: admin grants via CLI AND
// Settings). It CLOSES the 086 landmine: Casbin will happily persist p(role,*,"*","*")
// = a super-user, and does no name validation — so the guard MUST live here. We reuse
// Aura's REAL identity.ValidateCapabilityName (the `^[a-z][a-z0-9._-]{0,63}$` grammar
// + `*` rejection) on the obj BEFORE AddPolicy.
func grantPermission(e *casbin.Enforcer, role, dom, obj, act string) error {
	if err := identity.ValidateCapabilityName(obj); err != nil {
		return fmt.Errorf("%w: %v", errWildcardOrBadName, err)
	}
	_, err := e.AddPolicy(role, dom, obj, act)
	return err
}

func main() {
	logf("SETUP", "spike 089 — Casbin through Aura's RequireCapability shape + live management API (httptest)")

	m, err := model.NewModelFromString(orgModel)
	if err != nil {
		logf("ERROR", "model: %v", err)
		os.Exit(1)
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		logf("ERROR", "enforcer: %v", err)
		os.Exit(1)
	}
	// Seed: dept-a has a manager role that can write budget.
	_, _ = e.AddPolicy("role-manager", "dept-a", "budget", "write")

	// The protected route: POST /budget requires (budget, write) in the request's dept.
	protected := requireCasbin(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		e, "budget", "write",
	)

	// call drives one request through the real middleware and returns the status.
	call := func(principal, dept string) int {
		req := httptest.NewRequest(http.MethodPost, "/budget", nil)
		if dept != "" {
			req.Header.Set("X-Dept", dept)
		}
		if principal != "" {
			req = withPrincipal(req, principal)
		}
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		return rec.Code
	}

	fails := 0
	expect := func(cond bool, pass, failmsg string) {
		if cond {
			logf("CASE", "PASS  %s", pass)
		} else {
			logf("CASE", "FAIL  %s", failmsg)
			fails++
		}
	}

	// (1) no principal → 403 (mirrors auth.go: empty principal is unauthorized)
	expect(call("", "dept-a") == http.StatusForbidden, "no principal → 403", "no-principal should 403")

	// (2) bob has no role → enforced deny through the middleware → 403
	expect(call("u-bob", "dept-a") == http.StatusForbidden, "u-bob (no role) → 403 via middleware", "ungranted should 403")

	// (3) admin grants bob role-manager@dept-a at RUNTIME (management API) ...
	if _, err := e.AddGroupingPolicy("u-bob", "role-manager", "dept-a"); err != nil {
		logf("ERROR", "AddGroupingPolicy: %v", err)
		fails++
	}
	// ... and the SAME in-process enforcer immediately honours it (no restart, D-26)
	expect(call("u-bob", "dept-a") == http.StatusOK, "runtime grant took effect in-process → 200 (403→200, no restart)", "runtime grant should flip to 200")

	// (4) cross-domain isolation holds through the gateway: bob manager@A, not @B → 403
	expect(call("u-bob", "dept-b") == http.StatusForbidden, "cross-domain isolation via gateway: bob manager@A denied in dept-b → 403", "cross-domain should 403")

	// (5) admin revokes at runtime → back to 403
	if _, err := e.RemoveGroupingPolicy("u-bob", "role-manager", "dept-a"); err != nil {
		logf("ERROR", "RemoveGroupingPolicy: %v", err)
		fails++
	}
	expect(call("u-bob", "dept-a") == http.StatusForbidden, "runtime revoke took effect → 403 (200→403)", "runtime revoke should flip to 403")

	// (6) 086 LANDMINE CLOSED: the management API rejects a '*' capability grant and a
	// grammar-invalid name via Aura's REAL identity.ValidateCapabilityName, BEFORE the
	// enforcer would blindly persist a super-user policy.
	starErr := grantPermission(e, "role-manager", "dept-a", "*", "write")
	badErr := grantPermission(e, "role-manager", "dept-a", "Bad.Name!", "write")
	okErr := grantPermission(e, "role-manager", "dept-a", "reports.export", "read")
	expect(errors.Is(starErr, errWildcardOrBadName), "management API rejects '*' capability grant (086 wildcard guard)", "'*' grant should be rejected")
	expect(errors.Is(badErr, errWildcardOrBadName), "management API rejects grammar-invalid capability name", "bad name should be rejected")
	expect(okErr == nil, "management API accepts a valid capability grant (reports.export)", fmt.Sprintf("valid grant errored: %v", okErr))

	logf("PROBE", "single-binary Aura = ONE enforcer → in-process mutation is immediately live; a Casbin Watcher (pg LISTEN/NOTIFY) is only needed for MULTI-instance")

	logf("RESULT", "fails=%d over 8 gateway + management-API assertions", fails)
	if fails == 0 {
		logf("SUMMARY", "VALIDATED — Casbin drops into Aura's RequireCapability shape unchanged, runtime grant/revoke is immediately live in-process (D-26), cross-domain isolation holds end-to-end, and the 086 wildcard/grammar guard is closed by reusing identity.ValidateCapabilityName in the management layer")
		os.Exit(0)
	}
	logf("SUMMARY", "INVALIDATED — %d assertion(s) failed", fails)
	os.Exit(1)
}
