package arcadedb

import (
	"strings"
	"testing"
)

// Recall excludes the conversation the agent is currently in, so these helpers decide what
// the model is allowed to be reminded of mid-turn. The live tier reported the whole file
// uncovered on 2026-09-02: every refusal below had never run.

func TestCanonicalRecallExclusionsRefusesUnusableInput(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		values []string
		reason string
	}{
		"more conversations than the cap": {
			make([]string, recallExclusionMaxIDs+1),
			"exceed",
		},
		"empty id":        {[]string{""}, "invalid"},
		"untrimmed id":    {[]string{" conversation-a"}, "invalid"},
		"record id in id": {[]string{"#12:34"}, "invalid"},
		"duplicated id":   {[]string{"conversation-a", "conversation-a"}, "duplicated"},
		"duplicate after sort": {
			[]string{"conversation-b", "conversation-a", "conversation-b"},
			"duplicated",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := canonicalRecallExclusions(testCase.values)
			if err == nil {
				t.Fatalf("canonicalRecallExclusions accepted %s: %v", name, got)
			}
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Fatalf("error does not name the refusal: %v", err)
			}
		})
	}
}

func TestCanonicalRecallExclusionsSortsWithoutTouchingTheCaller(t *testing.T) {
	t.Parallel()
	values := []string{"conversation-c", "conversation-a", "conversation-b"}
	got, err := canonicalRecallExclusions(values)
	if err != nil {
		t.Fatalf("canonicalRecallExclusions: %v", err)
	}
	want := []string{"conversation-a", "conversation-b", "conversation-c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("exclusions = %v, want %v", got, want)
	}
	if values[0] != "conversation-c" {
		t.Fatalf("the caller's slice was sorted in place: %v", values)
	}
	if empty, err := canonicalRecallExclusions(nil); err != nil || len(empty) != 0 {
		t.Fatalf("canonicalRecallExclusions(nil) = %v, %v; want an empty set", empty, err)
	}
}

func TestApplyRecallExclusionsBindsOneParameterPerConversation(t *testing.T) {
	t.Parallel()
	params := map[string]any{}
	statement := applyRecallExclusions(
		"SELECT FROM Fact WHERE identity_id = :identity_id"+recallExclusionMarker,
		params,
		[]string{"conversation-a", "conversation-b"},
	)
	if strings.Contains(statement, recallExclusionMarker) {
		t.Fatalf("the marker survived into the statement: %s", statement)
	}
	// The ids are bound, never interpolated: a conversation id is model-supplied.
	for name, want := range map[string]any{
		"excluded_conversation_id_0": "conversation-a",
		"excluded_conversation_id_1": "conversation-b",
	} {
		if params[name] != want {
			t.Fatalf("param %s = %v, want %v", name, params[name], want)
		}
		if !strings.Contains(statement, "conversation_id <> :"+name) {
			t.Fatalf("statement does not bind %s: %s", name, statement)
		}
	}
	if strings.Contains(statement, "conversation-a") {
		t.Fatalf("a conversation id was interpolated into the statement: %s", statement)
	}

	empty := map[string]any{}
	if got := applyRecallExclusions("SELECT 1"+recallExclusionMarker, empty, nil); got != "SELECT 1" {
		t.Fatalf("an empty exclusion set left a clause behind: %q", got)
	}
	if len(empty) != 0 {
		t.Fatalf("an empty exclusion set bound parameters: %v", empty)
	}
}

func TestRecallExclusionMembershipAndAbstention(t *testing.T) {
	t.Parallel()
	ids := []string{"conversation-a", "conversation-b"}
	set := recallExcludedConversationSet(ids)
	if len(set) != 2 {
		t.Fatalf("exclusion set = %v, want both conversations", set)
	}
	if _, ok := set["conversation-a"]; !ok {
		t.Fatal("exclusion set lost a conversation")
	}
	if !recallConversationExcluded(ids, "conversation-b") {
		t.Fatal("an excluded conversation was not reported as excluded")
	}
	if recallConversationExcluded(ids, "conversation-z") {
		t.Fatal("an unrelated conversation was reported as excluded")
	}

	result := activeConversationExcludedRecallResult()
	if !result.Abstained || result.Reason != "active_conversation_excluded" {
		t.Fatalf("excluded recall did not abstain explicitly: %+v", result)
	}
	if result.Evidence == nil || len(result.Evidence) != 0 {
		t.Fatalf("abstention carried evidence: %+v", result.Evidence)
	}
	if result.Retrieval.Path != retrievalPathGraph {
		t.Fatalf("abstention did not name its retrieval path: %q", result.Retrieval.Path)
	}
}
