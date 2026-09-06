package agui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/skills"
	"go.uber.org/goleak"
)

// skills_scope_test.go pins the identity scoping of every skills READ route (amendment #214,
// superseding the stopgap of #216): the governance board, its archive tab, its body pane, the
// composer picker and the run's pinned skill all answer for the AUTHENTICATED PRINCIPAL.
//
// Scoping some of them and not the rest is the failure mode worth a test of its own: a picker
// that lists a name the run pin cannot resolve, or a board that shows a skill whose body the
// same person is refused, is the Pitfall-2 divergence spread across two routes.

const (
	scopeAlice = "11111111-1111-4111-8111-111111111111"
	scopeBob   = "22222222-2222-4222-8222-222222222222"
)

// scopedBoard is a board that answers alice and bob with a skill each, so a route that forgot
// to scope its context serves the wrong one (or none) and fails loudly.
func scopedBoard() *scriptedSkillsBoard {
	return &scriptedSkillsBoard{
		perIdentity: map[string][]skills.Skill{
			scopeAlice: {{Name: "alice-skill", Description: "hers", Type: "instruction", Body: "ALICE BODY"}},
			scopeBob:   {{Name: "bob-skill", Description: "his", Type: "instruction", Body: "BOB BODY"}},
		},
		perIdentityStage: map[string][]skills.StageSkill{
			scopeAlice: {{Name: "alice-archived"}},
			scopeBob:   {{Name: "bob-archived"}},
		},
	}
}

// doScoped issues a GET carrying identity as the authenticated principal.
func doScoped(s *Server, identity, target string) *httptest.ResponseRecorder {
	req := withPrincipal(httptest.NewRequest(http.MethodGet, target, nil), identity)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

// TestSkillsBoardRoutesAnswerForThePrincipal walks the three governance read routes: the
// active list, the archive tab and the body pane each show the caller their own and only
// their own.
func TestSkillsBoardRoutesAnswerForThePrincipal(t *testing.T) {
	board := scopedBoard()
	s := govServer(GovernanceProviders{Skills: board})

	list := doScoped(s, scopeAlice, "/api/governance/skills")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", list.Code, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), "alice-skill") || strings.Contains(list.Body.String(), "bob-skill") {
		t.Fatalf("alice's board = %s, want her skill and not bob's", list.Body.String())
	}
	if board.sawIdentity != scopeAlice {
		t.Fatalf("board resolved identity %q, want the authenticated principal %q", board.sawIdentity, scopeAlice)
	}

	// The ARCHIVE is scoped with the board. A global archive beside a per-identity active
	// list is the half-and-half configuration amendment #216 measured as worse than either
	// whole choice, so it gets its own assertion rather than riding on the list's.
	arch := doScoped(s, scopeBob, "/api/governance/skills?stage=archived")
	if arch.Code != http.StatusOK {
		t.Fatalf("archive status = %d, want 200: %s", arch.Code, arch.Body.String())
	}
	if !strings.Contains(arch.Body.String(), "bob-archived") || strings.Contains(arch.Body.String(), "alice-archived") {
		t.Fatalf("bob's archive = %s, want his archived skill and not alice's", arch.Body.String())
	}

	// The body pane resolves through the same scoped snapshot, so another person's skill is
	// a 404 and not a readable body.
	own := doScoped(s, scopeAlice, "/api/governance/skills/alice-skill/body")
	if own.Code != http.StatusOK || !strings.Contains(own.Body.String(), "ALICE BODY") {
		t.Fatalf("alice's own body = %d %s, want 200 with her body", own.Code, own.Body.String())
	}
	foreign := doScoped(s, scopeAlice, "/api/governance/skills/bob-skill/body")
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("alice reading bob's body = %d, want 404", foreign.Code)
	}
}

// TestComposerPickerResolvesAPersonalSkill closes the gap amendment #216 recorded as open:
// the picker could not resolve a personal skill at all, so a board that named one would have
// offered a person something they could not then pick.
func TestComposerPickerResolvesAPersonalSkill(t *testing.T) {
	board := scopedBoard()
	s := govServer(GovernanceProviders{Skills: board})

	rec := doScoped(s, scopeAlice, "/api/composer/skills")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Skills []struct {
			Name string `json:"name"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode picker: %v (%s)", err, rec.Body.String())
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "alice-skill" {
		t.Fatalf("alice's picker = %+v, want exactly her own skill", got.Skills)
	}
	// list ⊆ resolvable, ACROSS routes and for the same person: every name the picker offers
	// must come back from the body pane for that same principal.
	for _, sk := range got.Skills {
		if body := doScoped(s, scopeAlice, "/api/governance/skills/"+sk.Name+"/body"); body.Code != http.StatusOK {
			t.Fatalf("picker offered %q but the body pane answered %d", sk.Name, body.Code)
		}
	}
}

// TestRunPinResolvesAPersonalSkill is the other half of the same gap: the pin now resolves
// through the caller's own snapshot, so a skill only this person owns can lead their turn —
// and one only somebody else owns is the clean no-op an unknown name always was.
func TestRunPinResolvesAPersonalSkill(t *testing.T) {
	defer goleak.VerifyNone(t)
	run := &scriptedRunner{events: textTurn("ok")}
	s := NewServer(run, &fakeConvStore{known: map[string]bool{skillRunThreadID: true}}, ServerConfig{})
	s.SetGovernanceProviders(GovernanceProviders{Skills: scopedBoard()})

	body := `{"threadId":"` + skillRunThreadID + `","messages":[{"id":"m1","role":"user","content":"do the thing"}],"aura":{"skill":"alice-skill"}}`
	if rec := serveRunAs(t, s, scopeAlice, body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if run.gotModelUserMsg == nil {
		t.Fatal("alice's own pinned skill did not resolve — the pin is still unscoped")
	}
	got := *run.gotModelUserMsg
	if !strings.HasPrefix(got, tools.UseAuthorityFrame) || !strings.Contains(got, "ALICE BODY") {
		t.Fatalf("model msg must lead with the authority frame and carry her body:\n%s", got)
	}

	// Bob pinning alice's name resolves nothing: a name he cannot see is a name he cannot
	// lead his turn with.
	run2 := &scriptedRunner{events: textTurn("ok")}
	s2 := NewServer(run2, &fakeConvStore{known: map[string]bool{skillRunThreadID: true}}, ServerConfig{})
	s2.SetGovernanceProviders(GovernanceProviders{Skills: scopedBoard()})
	if rec := serveRunAs(t, s2, scopeBob, body); rec.Code != http.StatusOK {
		t.Fatalf("bob status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if run2.gotModelUserMsg != nil {
		t.Fatalf("bob pinned another person's skill and got it applied: %q", *run2.gotModelUserMsg)
	}
}
