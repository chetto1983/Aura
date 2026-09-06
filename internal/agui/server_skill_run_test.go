package agui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/skills"
	"go.uber.org/goleak"
)

const skillRunThreadID = "66666666-6666-6666-6666-666666666666"

// skillRunServer builds a run server wired with a scripted skills board carrying the given
// active skills, so handleRun resolves a pinned skill body via the SAME provider the composer
// picker lists from (one source of truth — D-04).
func skillRunServer(active []skills.Skill) (*Server, *scriptedRunner) {
	run := &scriptedRunner{events: textTurn("ok")}
	s := NewServer(run, &fakeConvStore{known: map[string]bool{skillRunThreadID: true}}, ServerConfig{})
	s.SetGovernanceProviders(GovernanceProviders{Skills: &scriptedSkillsBoard{active: active}})
	return s, run
}

// TestRun_PinnedSkill_Applied proves a known pinned skill is applied via Mechanism A: the
// EXACT tools.UseAuthorityFrame + resolved body leads the MODEL message while the visible/
// persisted turn stays the raw user text (the *userMsg != *modelUserMsg split fires →
// TurnWithModelUserMessage records both).
func TestRun_PinnedSkill_Applied(t *testing.T) {
	defer goleak.VerifyNone(t)
	const skillBody = "Scaffold a new skill directory with SKILL.md."
	s, run := skillRunServer([]skills.Skill{
		{Name: "skill-creator", Description: "make skills", Type: "instruction", Body: skillBody},
	})

	body := `{"threadId":"` + skillRunThreadID + `","messages":[{"id":"m1","role":"user","content":"do the thing"}],"aura":{"skill":"skill-creator"}}`
	rec := serveRunWithPrincipal(t, s, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if run.gotVisibleUserMsg == nil || *run.gotVisibleUserMsg != "do the thing" {
		t.Fatalf("visible userMsg = %v, want the raw \"do the thing\" persisted", run.gotVisibleUserMsg)
	}
	if run.gotModelUserMsg == nil {
		t.Fatal("model userMsg is nil, want authority frame + body + user text")
	}
	got := *run.gotModelUserMsg
	if !strings.HasPrefix(got, tools.UseAuthorityFrame) {
		t.Fatalf("model msg must LEAD with the reused authority frame:\n%s", got)
	}
	for _, want := range []string{tools.UseAuthorityFrame, skillBody, "do the thing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("model msg missing %q:\n%s", want, got)
		}
	}
}

// TestRun_PinnedSkill_UnknownName_NoOp proves an unknown pinned name is a clean no-op:
// SkillBody returns ok=false, so no frame is prepended, the model message stays the raw
// buildTurnUserMessage output (no client text injected as a skill body), and the request is a
// normal 200 — never a 5xx, never a passthrough of client text into the model turn.
func TestRun_PinnedSkill_UnknownName_NoOp(t *testing.T) {
	defer goleak.VerifyNone(t)
	s, run := skillRunServer([]skills.Skill{{Name: "skill-creator", Body: "irrelevant"}})

	body := `{"threadId":"` + skillRunThreadID + `","messages":[{"id":"m1","role":"user","content":"do the thing"}],"aura":{"skill":"does-not-exist"}}`
	rec := serveRunWithPrincipal(t, s, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (unknown name is a no-op, not a 5xx): %s", rec.Code, rec.Body.String())
	}
	// No skill applied => no visible/model split => the runner saw the raw text via Turn, with
	// NO authority frame anywhere (never a passthrough of the client-supplied name as a body).
	if run.gotModelUserMsg != nil && strings.Contains(*run.gotModelUserMsg, tools.UseAuthorityFrame) {
		t.Fatalf("unknown pinned name must NOT prepend the authority frame:\n%s", *run.gotModelUserMsg)
	}
	if run.gotTurnUserMsg == nil || *run.gotTurnUserMsg != "do the thing" {
		t.Fatalf("model msg = %v, want the raw \"do the thing\" buildTurnUserMessage produced", run.gotTurnUserMsg)
	}
}

// TestRun_NoSkill_Unchanged proves that with no aura.skill the run path is byte-identical to
// today: no frame, no split forced by a skill (Turn sees the raw text, no model split).
func TestRun_NoSkill_Unchanged(t *testing.T) {
	defer goleak.VerifyNone(t)
	s, run := skillRunServer([]skills.Skill{{Name: "skill-creator", Body: "irrelevant"}})

	body := `{"threadId":"` + skillRunThreadID + `","messages":[{"id":"m1","role":"user","content":"do the thing"}]}`
	rec := serveRunWithPrincipal(t, s, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if run.gotTurnUserMsg == nil || *run.gotTurnUserMsg != "do the thing" {
		t.Fatalf("model msg = %v, want the raw \"do the thing\" (no skill applied)", run.gotTurnUserMsg)
	}
	if run.gotModelUserMsg != nil {
		t.Fatalf("no skill/attachment => no visible/model split, got split model msg %q", *run.gotModelUserMsg)
	}
}

// TestComposerListSubsetOfResolvable is the WEBSKILL-02 divergence guard (Pitfall 2): every
// name the composer picker lists (ActiveSkills) MUST resolve via SkillBody — the listed set
// is a subset of the invocable set. The real skillsBoardAdapter guarantees this structurally
// (ActiveSkills=loader.List, SkillBody=loader.Get read ONE snapshot); the fake mirrors that
// contract so a regression diverging the two readers fails here.
func TestComposerListSubsetOfResolvable(t *testing.T) {
	board := &scriptedSkillsBoard{active: []skills.Skill{
		{Name: "alpha", Body: "abody"},
		{Name: "bravo", Body: "bbody"},
	}}
	ctx := t.Context()
	for _, sk := range board.ActiveSkills(ctx) {
		if _, ok := board.SkillBody(ctx, sk.Name); !ok {
			t.Fatalf("listed skill %q not resolvable via SkillBody (Pitfall 2 divergence)", sk.Name)
		}
	}
}
