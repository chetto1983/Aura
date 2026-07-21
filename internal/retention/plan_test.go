package retention

import (
	"slices"
	"testing"
)

func TestBuildPlanDeterministicTokenAndCap(t *testing.T) {
	candidates := []Candidate{
		{IdentityID: "owner-b", ConversationID: "conversation-b", ArtifactID: "artifact-b", Version: 2, Action: ActionDeleteArtifact, Class: ClassTemporary, Bytes: 12},
		{IdentityID: "owner-a", ConversationID: "conversation-a", ArtifactID: "artifact-a", Version: 1, Action: ActionDeleteArtifact, Class: ClassCrashArtifact, Bytes: 8},
	}
	planA, err := BuildPlan("policy-v1", candidates, 2)
	if err != nil {
		t.Fatal(err)
	}
	reversed := slices.Clone(candidates)
	slices.Reverse(reversed)
	planB, err := BuildPlan("policy-v1", reversed, 2)
	if err != nil {
		t.Fatal(err)
	}
	if planA.Token != planB.Token || !slices.Equal(planA.Candidates, planB.Candidates) {
		t.Fatalf("order changed immutable plan: %#v / %#v", planA, planB)
	}
	if len(planA.Candidates) != 2 || planA.TotalBytes != 20 {
		t.Fatalf("plan bounds = %+v", planA)
	}
	if err := planA.RequireToken(planA.Token); err != nil {
		t.Fatalf("matching token: %v", err)
	}
	if err := planA.RequireToken(""); err == nil {
		t.Fatal("blank apply token accepted")
	}

	capped, err := BuildPlan("policy-v1", append(candidates, Candidate{
		IdentityID: "owner-c", ArtifactID: "artifact-c", Version: 3,
		Action: ActionDeleteArtifact, Class: ClassTemporary,
	}), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(capped.Candidates) != 2 {
		t.Fatalf("cap+1 candidates = %d, want 2", len(capped.Candidates))
	}
}

func TestBuildPlanTokenDriftAndEmpty(t *testing.T) {
	base := Candidate{IdentityID: "owner", ConversationID: "conversation", ArtifactID: "artifact", Version: 1, Action: ActionDeleteArtifact, Class: ClassTemporary}
	empty, err := BuildPlan("policy-v1", nil, 1)
	if err != nil || len(empty.Candidates) != 0 || empty.Token == "" {
		t.Fatalf("empty plan = %+v, %v", empty, err)
	}
	original, _ := BuildPlan("policy-v1", []Candidate{base}, 1)
	for _, changed := range []struct {
		policy    string
		candidate Candidate
	}{
		{"policy-v2", base},
		{"policy-v1", func() Candidate { c := base; c.Version++; return c }()},
		{"policy-v1", func() Candidate { c := base; c.Action = ActionExpireReplay; return c }()},
		{"policy-v1", func() Candidate { c := base; c.ArtifactID = "other"; return c }()},
	} {
		plan, buildErr := BuildPlan(changed.policy, []Candidate{changed.candidate}, 1)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		if plan.Token == original.Token {
			t.Fatalf("changed snapshot retained token %s", plan.Token)
		}
	}
}

func TestCandidateRejectsPaths(t *testing.T) {
	for _, id := range []string{"../escape", "dir/file", `dir\\file`, "line\nbreak"} {
		_, err := BuildPlan("policy-v1", []Candidate{{IdentityID: "owner", ArtifactID: id, Action: ActionDeleteArtifact}}, 1)
		if err == nil {
			t.Errorf("artifact id %q accepted", id)
		}
	}
}
