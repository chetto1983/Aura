package agui

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/skills"
)

// governance_api_skills_house_test.go pins the board's answer for an OPERATOR, the caller
// #214 left with no surface at all: it scoped every cockpit write to the caller's own root
// and named `aura skills` with no --identity as the place house policy is edited instead,
// but that CLI has no archive verb. Measured 2026-09-07 on the operator's own deployment,
// where every skill they had installed sits in the house root and lost its buttons.
//
// The row list is the same three provenances TestBoardMarksOnlyTheCallersOwnSkillsAsActionable
// uses. What changes is who is asking: a caller holding governance.write reaches the house
// root too, and NOTHING else — another identity's share stays out of reach, because the
// capability is about the deployment's own library, not about other people's.

// skillRows drives GET /api/governance/skills and returns name -> owned.
func skillRows(t *testing.T, board *scriptedSkillsBoard) map[string]bool {
	t.Helper()
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
	out := make(map[string]bool, len(payload.Skills))
	for _, row := range payload.Skills {
		out[row.Name] = row.Owned
	}
	return out
}

// threeProvenances is the caller's own root, the house root, and another identity's export
// reached through a share.
func threeProvenances(writableHouse string) *scriptedSkillsBoard {
	mine := filepath.Join("/srv", "aura", "identities", "alice")
	house := filepath.Join("/srv", "aura", "skills")
	return &scriptedSkillsBoard{
		writableRoot:      mine,
		writableHouseRoot: writableHouse,
		active: []skills.Skill{
			{Name: "own", Type: "instruction", Dir: filepath.Join(mine, "own")},
			{Name: "house", Type: "instruction", Dir: filepath.Join(house, "house")},
			{Name: "shared", Type: "instruction", Dir: filepath.Join("/srv", "aura", "identities", "bob", ".export", "shared")},
		},
	}
}

// TestBoardOffersTheHouseRowToAnOperator is the regression: with the house root writable, the
// house row becomes actionable and the share does not.
func TestBoardOffersTheHouseRowToAnOperator(t *testing.T) {
	owned := skillRows(t, threeProvenances(filepath.Join("/srv", "aura", "skills")))

	want := map[string]bool{"own": true, "house": true, "shared": false}
	for name, w := range want {
		if owned[name] != w {
			t.Fatalf("row %q: owned=%v, want %v (all: %v)", name, owned[name], w, owned)
		}
	}
}

// TestBoardNeverOffersTheVerbsOnABuiltin is the one row an operator must NOT be able to act
// on. A builtin is product code, not house policy: MaterializeBuiltins rewrites it at the next
// boot, so Archive and Delete are changes that undo themselves one restart later. Offering the
// verb would be a button that lies.
func TestBoardNeverOffersTheVerbsOnABuiltin(t *testing.T) {
	house := filepath.Join("/srv", "aura", "skills")
	board := &scriptedSkillsBoard{
		writableRoot:      filepath.Join("/srv", "aura", "identities", "alice"),
		writableHouseRoot: house,
		active: []skills.Skill{
			{Name: "house", Type: "instruction", Dir: filepath.Join(house, "house")},
			{Name: "skill-creator", Type: "instruction", Dir: filepath.Join(house, "skill-creator")},
			{Name: "find-skills-aura", Type: "instruction", Dir: filepath.Join(house, "find-skills-aura")},
			{Name: "memory-aura", Type: "instruction", Dir: filepath.Join(house, "memory-aura")},
		},
	}

	owned := skillRows(t, board)
	if !owned["house"] {
		t.Fatalf("ordinary house policy must stay actionable for an operator: %v", owned)
	}
	for _, name := range skills.BuiltinNames() {
		if owned[name] {
			t.Fatalf("builtin %q is actionable: the boot rewrites it, so the verb is a no-op with an animation", name)
		}
	}
}

// TestBoardKeepsTheHouseRowFromATenant is the same board asked by somebody without the
// capability: the answer must be exactly what #214 gives today.
func TestBoardKeepsTheHouseRowFromATenant(t *testing.T) {
	owned := skillRows(t, threeProvenances(""))

	want := map[string]bool{"own": true, "house": false, "shared": false}
	for name, w := range want {
		if owned[name] != w {
			t.Fatalf("row %q: owned=%v, want %v (all: %v)", name, owned[name], w, owned)
		}
	}
}
