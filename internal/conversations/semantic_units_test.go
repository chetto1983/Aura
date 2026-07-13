package conversations

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestSemanticUnitParallelCallsAndResults(t *testing.T) {
	turns := []SemanticTurn{{Seq: 1, Role: llm.RoleSystem, Tokens: 10, Protected: true}, {Seq: 2, Role: llm.RoleUser, Tokens: 10, ParentTurnID: "p"}, {Seq: 3, Role: llm.RoleAssistant, Tokens: 10, ParentTurnID: "p", ToolCallIDs: []string{"a", "b"}}, {Seq: 4, Role: llm.RoleTool, Tokens: 10, ParentTurnID: "p", ResultFor: "b"}, {Seq: 5, Role: llm.RoleTool, Tokens: 10, ParentTurnID: "p", ResultFor: "a"}, {Seq: 6, Role: llm.RoleAssistant, Tokens: 10, ParentTurnID: "p"}}
	units := BuildSemanticUnits(turns)
	if len(units) != 2 || !units[1].Eligible || len(units[1].Turns) != 5 {
		t.Fatalf("units=%+v", units)
	}
}

func TestSemanticUnitRejectsUnsafeCycles(t *testing.T) {
	for name, turns := range map[string][]SemanticTurn{
		"missing":          {{Seq: 1, Role: llm.RoleAssistant, ParentTurnID: "p", ToolCallIDs: []string{"a"}}},
		"duplicate":        {{Seq: 1, Role: llm.RoleAssistant, ParentTurnID: "p", ToolCallIDs: []string{"a"}}, {Seq: 2, Role: llm.RoleTool, ParentTurnID: "p", ResultFor: "a"}, {Seq: 3, Role: llm.RoleTool, ParentTurnID: "p", ResultFor: "a"}},
		"partial_stream":   {{Seq: 1, Role: llm.RoleAssistant, ResponseID: "r", State: SemanticStateOpen}},
		"cancelled":        {{Seq: 1, Role: llm.RoleAssistant, ParentTurnID: "p", State: SemanticStateCancelled}},
		"retry":            {{Seq: 1, Role: llm.RoleAssistant, ParentTurnID: "p", State: SemanticStateRetried}},
		"malformed_legacy": {{Seq: 1, Role: "???"}},
	} {
		t.Run(name, func(t *testing.T) {
			u := BuildSemanticUnits(turns)
			if len(u) != 1 || u[0].Eligible {
				t.Fatalf("units=%+v", u)
			}
		})
	}
}

func TestSemanticUnitExplicitLegacyNormalization(t *testing.T) {
	u := BuildSemanticUnits([]SemanticTurn{{Seq: 1, Role: llm.RoleUser, LegacyClosure: "paired_v1"}, {Seq: 2, Role: llm.RoleAssistant, LegacyClosure: "paired_v1"}})
	if len(u) != 1 || !u[0].Eligible {
		t.Fatalf("units=%+v", u)
	}
}

func TestRecentTailManifestSelection(t *testing.T) {
	units := BuildSemanticUnits([]SemanticTurn{{Seq: 1, Role: llm.RoleSystem, Tokens: 10, Protected: true}, {Seq: 2, Role: llm.RoleUser, Tokens: 20, ParentTurnID: "a"}, {Seq: 3, Role: llm.RoleAssistant, Tokens: 20, ParentTurnID: "a"}, {Seq: 4, Role: llm.RoleUser, Tokens: 20, ParentTurnID: "b"}, {Seq: 5, Role: llm.RoleAssistant, Tokens: 20, ParentTurnID: "b"}, {Seq: 6, Role: llm.RoleUser, Tokens: 10, ParentTurnID: "c"}, {Seq: 7, Role: llm.RoleAssistant, Tokens: 10, ParentTurnID: "c"}})
	m := SelectCompactionManifests(units, ManifestSelectionPolicy{InputCapacity: 300, RecentTailTokens: 15, MinimumSavingTokens: 30})
	if m.NoOpReason != "" || !sameInts(m.Tail, []int{6, 7}) || !sameInts(m.Summarized, []int{2, 3, 4, 5}) || !sameInts(m.Protected, []int{1}) {
		t.Fatalf("manifest=%+v", m)
	}
	assertManifestPartition(t, m, []int{1, 2, 3, 4, 5, 6, 7})
}

func TestRecentTailCapAndReasonedNoOps(t *testing.T) {
	tooLarge := []SemanticUnit{{Key: "x", Eligible: true, Tokens: 21, Turns: []SemanticTurn{{Seq: 1}}}}
	if got := SelectCompactionManifests(tooLarge, ManifestSelectionPolicy{InputCapacity: 100, RecentTailTokens: 1}); got.NoOpReason != NoOpTailAtomicityCap {
		t.Fatalf("got=%+v", got)
	}
	open := []SemanticUnit{{Key: "x", Eligible: false, Reason: "open", Turns: []SemanticTurn{{Seq: 1}}}}
	if got := SelectCompactionManifests(open, ManifestSelectionPolicy{InputCapacity: 100, RecentTailTokens: 1}); got.NoOpReason != NoOpNoEligiblePrefix {
		t.Fatalf("got=%+v", got)
	}
}

func TestL1EditRequiresAuthorizedReference(t *testing.T) {
	u := SemanticUnit{Eligible: true, Turns: []SemanticTurn{{Seq: 1, Role: llm.RoleTool, Content: "large", Reconstructable: true, AuthorizedReference: "asset:abc"}, {Seq: 2, Role: llm.RoleTool, Content: "lost", Reconstructable: true}}}
	edits := L1EditsForUnits([]SemanticUnit{u})
	if len(edits) != 1 || edits[0].TurnSeq != 1 || edits[0].Reference != "asset:abc" {
		t.Fatalf("edits=%+v", edits)
	}
}

func assertManifestPartition(t *testing.T, m CompactionManifest, want []int) {
	t.Helper()
	seen := map[int]int{}
	for _, xs := range [][]int{m.Summarized, m.Tail, m.Protected, m.Excluded} {
		for _, v := range xs {
			seen[v]++
		}
	}
	for _, v := range want {
		if seen[v] != 1 {
			t.Fatalf("seq %d count=%d manifest=%+v", v, seen[v], m)
		}
	}
}
func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
