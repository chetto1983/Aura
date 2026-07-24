package knowledge

import (
	"strings"
	"testing"
)

func TestLegacyLearningMigrationDeletesOnlyRetiredLabels(t *testing.T) {
	bodyBytes, err := cypherFS.ReadFile("migrations/0004_decommission_legacy_learning.cypher")
	if err != nil {
		t.Fatal(err)
	}
	body := string(bodyBytes)
	for _, label := range []string{"ReasoningExample", "ReasoningSeed", "ToolSelectionExample", "ToolSelectionSeed"} {
		if !strings.Contains(body, "n:"+label) {
			t.Errorf("migration missing exact legacy label %s", label)
		}
	}
	if !strings.Contains(body, "DETACH DELETE n") {
		t.Fatal("migration must detach-delete matched legacy nodes")
	}
	for _, unrelated := range []string{"User", "AdaptiveEpisode", "AdaptiveEvent", "Document", "Chunk"} {
		if strings.Contains(body, "n:"+unrelated) {
			t.Errorf("migration must not target unrelated label %s", unrelated)
		}
	}
}
