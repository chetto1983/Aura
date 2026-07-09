package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/skills"
)

// TestComposerSkills_Active proves GET /api/composer/skills returns 200 with the global
// active-skills rows (name/description/type) projected from the SAME loader snapshot the
// governance board reads (activeSkillRows reused verbatim — D-04 one source of truth).
func TestComposerSkills_Active(t *testing.T) {
	board := &scriptedSkillsBoard{active: []skills.Skill{
		{Name: "active-one", Description: "a", Type: "instruction"},
	}}
	s := govServer(GovernanceProviders{Skills: board})

	rec := doGov(t, s, http.MethodGet, "/api/composer/skills")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"skills"`, `"active-one"`, `"name"`, `"description"`, `"type"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("composer skills body missing %q:\n%s", want, body)
		}
	}
}

// TestComposerSkills_RequireAuthNotCapability is the D-03 anti-403 differential: an
// authenticated identity WITHOUT the governance.read grant still gets its NON-empty list
// from the composer route behind the bare whole-mux RequireAuth, where the governance
// board's RequireCapability(governance.read) wrap would 403 the SAME identity. testDeps
// binds testLocalID with agent.run only (no governance.read).
func TestComposerSkills_RequireAuthNotCapability(t *testing.T) {
	deps := testDeps("operator-secret")
	board := &scriptedSkillsBoard{active: []skills.Skill{
		{Name: "active-one", Description: "a", Type: "instruction"},
	}}
	s := govServer(GovernanceProviders{Skills: board})

	// Composer route behind the bare RequireAuth the parent mux applies: 200 + non-empty
	// for a non-admin identity (the whole reason the route is mounted bare, not gated).
	rec := httptest.NewRecorder()
	RequireAuth(s.Mux(), deps).ServeHTTP(rec, validCookieReq(deps, "/api/composer/skills", false))
	if rec.Code != http.StatusOK {
		t.Fatalf("composer status = %d, want 200 for a non-admin identity: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "active-one") {
		t.Fatalf("composer list empty for a non-admin identity: %s", rec.Body.String())
	}

	// The SAME non-admin identity through the governance.read capability gate → 403 (the
	// D-03 trap the composer route deliberately avoids by mounting bare aguiHandler).
	rec2 := httptest.NewRecorder()
	req2 := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/composer/skills", nil), testLocalID)
	RequireCapability(s.Mux(), deps, "governance.read").ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("governance.read gate status = %d, want 403 for a non-admin identity", rec2.Code)
	}
}

// TestComposerSkills_Unauth proves the composer route inherits the whole-mux RequireAuth:
// an API request (no Accept: text/html) with no session cookie → 401, never the list.
func TestComposerSkills_Unauth(t *testing.T) {
	deps := testDeps("operator-secret")
	board := &scriptedSkillsBoard{active: []skills.Skill{
		{Name: "active-one", Description: "a", Type: "instruction"},
	}}
	s := govServer(GovernanceProviders{Skills: board})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/composer/skills", nil)
	RequireAuth(s.Mux(), deps).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without a session cookie", rec.Code)
	}
}

// TestComposerSkills_NilProvider proves a nil skills provider answers 503 so the client
// degrades to an empty picker (D-09), never a 500.
func TestComposerSkills_NilProvider(t *testing.T) {
	s := govServer(GovernanceProviders{Skills: nil})
	rec := doGov(t, s, http.MethodGet, "/api/composer/skills")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for a nil skills provider", rec.Code)
	}
}
