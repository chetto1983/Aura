package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/skills"
)

// governance_api_skills_test.go covers the GOV-02 skills read surface: the two lifecycle
// stages, the always-block flag on the row, and the detail-pane body route. Split out of
// governance_api_test.go on the 600-LOC ceiling; the shared server/request helpers
// (govServer / doGov) stay there.

// scriptedSkillsBoard is a configurable SkillsBoardProvider: canned active/stage skills,
// the audit rows, and optional per-call errors.
//
// It is IDENTITY-AWARE because the real provider is (amendment #214): perIdentity, when set,
// answers each caller with their own library and records the identity the handler resolved,
// so a route that forgot to scope its context fails these tests instead of quietly serving
// one person's skills to another.
type scriptedSkillsBoard struct {
	active           []skills.Skill
	perIdentity      map[string][]skills.Skill
	perIdentityStage map[string][]skills.StageSkill
	sawIdentity      string
	staged           map[string][]skills.StageSkill
	stageErr         error
	audit            []skills.AuditRow
	auditErr         error
	auditLimit       int
	auditSince       time.Time
	// writableRoot / perIdentityWritableRoot answer the board's ownership question — which
	// listed rows this caller may archive or delete. Empty means "nothing is owned", which
	// is the honest answer for a fake that never placed a skill under a root.
	writableRoot            string
	perIdentityWritableRoot map[string]string
}

// activeFor is the fake's one scoping rule, applied by every read exactly as the real
// adapter applies its loader lookup.
func (b *scriptedSkillsBoard) activeFor(ctx context.Context) []skills.Skill {
	b.sawIdentity = identityctx.IdentityID(ctx)
	if b.perIdentity == nil {
		return b.active
	}
	return b.perIdentity[b.sawIdentity]
}

func (b *scriptedSkillsBoard) ActiveSkills(ctx context.Context) []skills.Skill {
	return b.activeFor(ctx)
}

// SkillBody resolves a body from the SAME active snapshot ActiveSkills lists, so the fake
// preserves the list ⊆ resolvable invariant the real skillsBoardAdapter guarantees (both
// delegate to one loader snapshot — Pitfall 2). An unknown name → ("", false).
func (b *scriptedSkillsBoard) SkillBody(ctx context.Context, name string) (string, bool) {
	for _, sk := range b.activeFor(ctx) {
		if sk.Name == name {
			return sk.Body, true
		}
	}
	return "", false
}

// WritableRoot is the root the board uses to decide which listed rows this caller may act
// on. The fake answers with whatever the test set, so a test can place a skill's Dir inside
// or outside it and assert the row's `owned` flag either way.
func (b *scriptedSkillsBoard) WritableRoot(ctx context.Context) string {
	b.sawIdentity = identityctx.IdentityID(ctx)
	if b.perIdentityWritableRoot != nil {
		return b.perIdentityWritableRoot[b.sawIdentity]
	}
	return b.writableRoot
}

func (b *scriptedSkillsBoard) ArchivedSkills(ctx context.Context) ([]skills.StageSkill, error) {
	b.sawIdentity = identityctx.IdentityID(ctx)
	if b.stageErr != nil {
		return nil, b.stageErr
	}
	if b.perIdentityStage != nil {
		return b.perIdentityStage[b.sawIdentity], nil
	}
	return b.staged[skills.StageArchived], nil
}

func (b *scriptedSkillsBoard) AuditLog(_ context.Context, f skills.AuditFilter) ([]skills.AuditRow, error) {
	b.auditLimit = f.Limit
	b.auditSince = f.Since
	if b.auditErr != nil {
		return nil, b.auditErr
	}
	return b.audit, nil
}

// TestGovernanceSkillsStages: the two remaining stages list the correct sets and an
// archived row carries NO action field (T-28-02-04 — non-runnable by construction).
func TestGovernanceSkillsStages(t *testing.T) {
	board := &scriptedSkillsBoard{
		active: []skills.Skill{{Name: "active-one", Description: "a", Type: "instruction"}},
		staged: map[string][]skills.StageSkill{
			skills.StageArchived: {{Name: "archived-one", Description: "ar", Type: "snippet", Language: "python", ContentHash: "abc"}},
		},
	}
	s := govServer(GovernanceProviders{Skills: board})

	// Default (no ?stage) → active.
	def := doGov(t, s, http.MethodGet, "/api/governance/skills")
	if !strings.Contains(def.Body.String(), "active-one") {
		t.Fatalf("default stage must be active, got %s", def.Body.String())
	}

	archived := doGov(t, s, http.MethodGet, "/api/governance/skills?stage=archived")
	if archived.Code != http.StatusOK {
		t.Fatalf("archived status = %d, want 200", archived.Code)
	}
	if !strings.Contains(archived.Body.String(), "archived-one") {
		t.Fatalf("archived stage missing its skill: %s", archived.Body.String())
	}
	// An archived row must carry NO action/run/activate control.
	for _, banned := range []string{`"action"`, `"run"`, `"activate"`, `"runnable"`} {
		if strings.Contains(archived.Body.String(), banned) {
			t.Fatalf("archived row exposed a %s control (must be non-runnable): %s", banned, archived.Body.String())
		}
	}
	// An archived body must never be serialized.
	if strings.Contains(strings.ToLower(archived.Body.String()), `"body"`) {
		t.Fatalf("archived row leaked a body field: %s", archived.Body.String())
	}
}

// TestGovernanceSkillBody: the detail-pane body route answers for an ACTIVE skill and
// refuses everything else. The archived case is the one that matters: an archived skill is
// listed by the board, so "it appears in the UI" must NOT imply "its body is readable" —
// the route resolves through the loader snapshot, where an archived name simply is not.
func TestGovernanceSkillBody(t *testing.T) {
	board := &scriptedSkillsBoard{
		active: []skills.Skill{{Name: "active-one", Description: "a", Type: "instruction", Body: "# do the thing"}},
		staged: map[string][]skills.StageSkill{
			skills.StageArchived: {{Name: "archived-one", Description: "ar", Type: "instruction"}},
		},
	}
	s := govServer(GovernanceProviders{Skills: board})

	ok := doGov(t, s, http.MethodGet, "/api/governance/skills/active-one/body")
	if ok.Code != http.StatusOK {
		t.Fatalf("active body status = %d, want 200", ok.Code)
	}
	var got struct{ Name, Body string }
	if err := json.Unmarshal(ok.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body response: %v", err)
	}
	if got.Name != "active-one" || got.Body != "# do the thing" {
		t.Fatalf("body response = %+v, want the active skill's own body", got)
	}

	for _, name := range []string{"archived-one", "never-existed"} {
		rec := doGov(t, s, http.MethodGet, "/api/governance/skills/"+name+"/body")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s body status = %d, want 404 (only ACTIVE bodies are readable)", name, rec.Code)
		}
	}
}

// TestGovernanceSkillBodyUnwired: no provider → 503, the same shape every other
// governance route uses for an unwired board (never a 200 with an empty body).
func TestGovernanceSkillBodyUnwired(t *testing.T) {
	rec := doGov(t, govServer(GovernanceProviders{}), http.MethodGet, "/api/governance/skills/x/body")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired body status = %d, want 503", rec.Code)
	}
}

// TestGovernanceSkillsAlwaysFlag: the always-block flag reaches the row (it costs context
// on every turn, so the board shows it), and a non-always skill omits the key entirely
// rather than shipping a misleading false.
func TestGovernanceSkillsAlwaysFlag(t *testing.T) {
	board := &scriptedSkillsBoard{active: []skills.Skill{
		{Name: "pinned", Description: "a", Type: "instruction", Always: true},
		{Name: "ordinary", Description: "b", Type: "instruction"},
	}}
	rec := doGov(t, govServer(GovernanceProviders{Skills: board}), http.MethodGet, "/api/governance/skills")

	var payload struct {
		Skills []struct {
			Name   string `json:"name"`
			Always bool   `json:"always"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if len(payload.Skills) != 2 {
		t.Fatalf("want 2 rows, got %d", len(payload.Skills))
	}
	if !payload.Skills[0].Always {
		t.Fatal("an always-block skill must be marked always on the row")
	}
	if payload.Skills[1].Always {
		t.Fatal("an ordinary skill must not be marked always")
	}
	if strings.Count(rec.Body.String(), `"always"`) != 1 {
		t.Fatalf("always must be omitted when false, got %s", rec.Body.String())
	}
}

// TestGovernanceSkillsUnknownStage: an unknown stage is a clean 400 — including
// "pending", which amendment #97 removed. The wire must reject the retired name rather
// than quietly answer with something else.
func TestGovernanceSkillsUnknownStage(t *testing.T) {
	s := govServer(GovernanceProviders{Skills: &scriptedSkillsBoard{}})
	for _, stage := range []string{"bogus", "pending"} {
		rec := doGov(t, s, http.MethodGet, "/api/governance/skills?stage="+stage)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("stage=%s status = %d, want 400", stage, rec.Code)
		}
	}
}

// TestGovernanceSkillsAuditNewestFirst: the audit endpoint returns the store rows in the
// order the store yields them (newest-first by contract) and applies the default limit.
func TestGovernanceSkillsAuditNewestFirst(t *testing.T) {
	now := time.Now().UTC()
	board := &scriptedSkillsBoard{
		audit: []skills.AuditRow{
			{
				ID:               "newest",
				IdentityID:       "secret-identity",
				SkillName:        "s",
				Action:           skills.AuditAction("create"),
				PausedStateToken: "11111111-1111-1111-1111-111111111111",
				CreatedAt:        now,
			},
			{ID: "older", SkillName: "s", Action: skills.AuditAction("update"), CreatedAt: now.Add(-time.Hour)},
		},
	}
	s := govServer(GovernanceProviders{Skills: board})
	rec := doGov(t, s, http.MethodGet, "/api/governance/skills/audit")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp struct {
		Rows []struct {
			ID string `json:"ID"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode audit body: %v", err)
	}
	if len(resp.Rows) != 2 || resp.Rows[0].ID != "newest" {
		t.Fatalf("audit rows not newest-first as the store yielded: %+v", resp.Rows)
	}
	for _, banned := range []string{"PausedStateToken", "11111111-1111-1111-1111-111111111111", "IdentityID", "secret-identity"} {
		if strings.Contains(rec.Body.String(), banned) {
			t.Fatalf("audit DTO leaked %q: %s", banned, rec.Body.String())
		}
	}
}

// TestSkillsAuditNarrowsTheLedgerAndNeverAnswersNull walks the three legs of the audit route
// that had no assertion at all, only a fake recording them.
//
// The ?limit / ?since narrowing is the page the cockpit's audit drawer asks for, so a filter
// silently dropped on the floor is a drawer that pages through nothing; and rows==nil rendered
// as JSON null instead of [] is a client crash on a ledger that is merely empty, which is the
// state every fresh deployment is in.
func TestSkillsAuditNarrowsTheLedgerAndNeverAnswersNull(t *testing.T) {
	board := &scriptedSkillsBoard{}
	s := govServer(GovernanceProviders{Skills: board})

	rec := doGov(t, s, http.MethodGet, "/api/governance/skills/audit?limit=7&since=2026-01-02T03:04:05Z")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if board.auditLimit != 7 {
		t.Fatalf("filter limit = %d, want the requested 7", board.auditLimit)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !board.auditSince.Equal(want) {
		t.Fatalf("filter since = %v, want %v", board.auditSince, want)
	}
	// An empty ledger is an empty ARRAY on the wire, never null.
	if got := strings.TrimSpace(rec.Body.String()); !strings.Contains(got, `"rows":[]`) {
		t.Fatalf("empty ledger rendered as %s, want an empty rows array", got)
	}

	// An unparseable pair is absorbed rather than rejected: the limit falls back to the default
	// page and the since bound is simply not applied, so the drawer degrades to the newest-first
	// page instead of 400-ing on a query string it built itself.
	board.auditLimit, board.auditSince = -1, time.Now()
	if rec := doGov(t, s, http.MethodGet, "/api/governance/skills/audit?limit=abc&since=yesterday"); rec.Code != http.StatusOK {
		t.Fatalf("unparseable filter = %d, want 200", rec.Code)
	}
	if board.auditLimit != defaultSearchLimit {
		t.Fatalf("unparseable limit = %d, want the default page %d", board.auditLimit, defaultSearchLimit)
	}
	if !board.auditSince.IsZero() {
		t.Fatalf("unparseable since leaked into the query as %v, want no lower bound", board.auditSince)
	}
}

// TestParseOffsetAndProbeDeadlineDefaults pins the two fall-backs the governance routes lean
// on. Both are one line and both are the kind that only ever run on a malformed request or an
// unconfigured server — which is exactly why neither had ever been executed by a test.
func TestParseOffsetAndProbeDeadlineDefaults(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "abc", "-1"} {
		if got := parseOffset(raw); got != 0 {
			t.Errorf("parseOffset(%q) = %d, want 0", raw, got)
		}
	}
	if got := parseOffset("12"); got != 12 {
		t.Errorf("parseOffset(%q) = %d, want 12", "12", got)
	}
	// A server with no test override probes on the production deadline, not on zero.
	if got := (&Server{}).probeDeadline(); got != defaultProbeTimeout {
		t.Errorf("probeDeadline with no override = %v, want %v", got, defaultProbeTimeout)
	}
}

// TestBoardMarksOnlyTheCallersOwnSkillsAsActionable is the wire half of the decision that
// a person mounts the house's skills but may only modify their own. Before it, every active
// row carried an Archive and a Delete affordance with nothing to distinguish a house row
// from the caller's, so the verb reached a name Writer.For(identity) does not hold and the
// operator was shown a backend failure for something they clicked.
//
// The three rows are the three provenances a board actually merges: the caller's own root,
// the house root, and another identity's export reached through a share. Only the first is
// owned, and `owned` is present on every row — false is the value the client must act on,
// so an omitted key would render as an enabled button.
func TestBoardMarksOnlyTheCallersOwnSkillsAsActionable(t *testing.T) {
	mine := filepath.Join("/srv", "aura", "identities", "alice")
	board := &scriptedSkillsBoard{
		writableRoot: mine,
		active: []skills.Skill{
			{Name: "own", Type: "instruction", Dir: filepath.Join(mine, "own")},
			{Name: "house", Type: "instruction", Dir: filepath.Join("/srv", "aura", "skills", "house")},
			{Name: "shared", Type: "instruction", Dir: filepath.Join("/srv", "aura", "identities", "bob", ".export", "shared")},
		},
	}
	rec := doGov(t, govServer(GovernanceProviders{Skills: board}), http.MethodGet, "/api/governance/skills")

	var payload struct {
		Skills []struct {
			Name  string `json:"name"`
			Owned bool   `json:"owned"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if len(payload.Skills) != 3 {
		t.Fatalf("want 3 rows, got %d", len(payload.Skills))
	}
	want := map[string]bool{"own": true, "house": false, "shared": false}
	for _, row := range payload.Skills {
		w, ok := want[row.Name]
		if !ok {
			t.Fatalf("unexpected row %q", row.Name)
		}
		if row.Owned != w {
			t.Fatalf("row %q: owned=%v, want %v", row.Name, row.Owned, w)
		}
	}
	// Absent-means-enabled is the failure this guards: the key must be on every row,
	// including the two that are false.
	if got := strings.Count(rec.Body.String(), `"owned"`); got != 3 {
		t.Fatalf(`want "owned" on all 3 rows, found %d in %s`, got, rec.Body.String())
	}
}

// TestArchivedRowsAreAlwaysTheCallersOwn: the archive listing is read from the caller's own
// stage root, so there is no unowned row to render there. Asserting it keeps the two halves
// of the Restore button in the same directory if the archive read is ever re-scoped.
func TestArchivedRowsAreAlwaysTheCallersOwn(t *testing.T) {
	board := &scriptedSkillsBoard{staged: map[string][]skills.StageSkill{
		skills.StageArchived: {{Name: "retired", Type: "instruction"}},
	}}
	rec := doGov(t, govServer(GovernanceProviders{Skills: board}), http.MethodGet, "/api/governance/skills?stage="+skills.StageArchived)

	var payload struct {
		Skills []struct {
			Name  string `json:"name"`
			Owned bool   `json:"owned"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if len(payload.Skills) != 1 || !payload.Skills[0].Owned {
		t.Fatalf("an archived row must be actionable by the caller who owns the stage: %+v", payload.Skills)
	}
}
