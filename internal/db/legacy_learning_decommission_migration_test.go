package db

import (
	"strings"
	"testing"
)

func TestLegacyLearningDecommissionMigrationTargetsOnlyRetiredState(t *testing.T) {
	upBytes, err := migrationsFS.ReadFile("migrations/0059_decommission_legacy_learning.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"DELETE FROM aura.agent_job_runs",
		"kind = 'learning_compaction'",
		"DELETE FROM aura.scheduler_tasks",
		"DELETE FROM aura.settings",
		"key = 'AURA_LLM_REASONING_LEARNING'",
		"key LIKE 'AURA\\_LEARNING\\_%' ESCAPE '\\'",
		"scheduler_tasks_kind_check",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("up migration missing %q", want)
		}
	}
	if !strings.Contains(up, "'retention_sweep'))") {
		t.Fatal("replacement scheduler kind check must end at retention_sweep")
	}

	downBytes, err := migrationsFS.ReadFile("migrations/0059_decommission_legacy_learning.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	if !strings.Contains(down, "'learning_compaction'") {
		t.Fatal("down migration must restore only the retired kind admissibility")
	}
	for _, forbidden := range []string{"INSERT INTO aura.scheduler_tasks", "INSERT INTO aura.agent_job_runs", "INSERT INTO aura.settings"} {
		if strings.Contains(down, forbidden) {
			t.Errorf("down migration fabricates deleted state via %q", forbidden)
		}
	}
}
