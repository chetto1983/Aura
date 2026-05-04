package summarizer

import "testing"

func TestActionClassifiers(t *testing.T) {
	for _, action := range []string{"new", "patch", "skip"} {
		if !IsWikiAction(action) {
			t.Fatalf("IsWikiAction(%q) = false", action)
		}
		if IsSkillAction(action) {
			t.Fatalf("IsSkillAction(%q) = true", action)
		}
	}
	for _, action := range []string{"skill_create", "skill_update", "skill_delete"} {
		if !IsSkillAction(action) {
			t.Fatalf("IsSkillAction(%q) = false", action)
		}
		if IsWikiAction(action) {
			t.Fatalf("IsWikiAction(%q) = true", action)
		}
	}
}
