package skills

import "testing"

// stage_name_repro_test.go reproduces the operator-reported bug: a skill installed
// from skills.sh (the cockpit governance Install path) showing a UUID instead of its
// human name in the governance Skills board's pending/archived tabs.
//
// It drives the EXACT skills.sh stage path end to end, DB-free: the Installer stages a
// fetched `npx skills add` tree (a `.claude/skills/<name>/SKILL.md`) through
// WriteInstallPending into pending/<name>/ (the FS copy commits BEFORE the nil-pool
// audit tx panics, which installPastValidation recovers), then the board's per-stage
// reader (ListStage — the SAME reader serve_governance.go's skillsBoardAdapter calls)
// projects the pending metadata. The assertion is the board-visible Name.

// TestStageNameReproSkillsShInstallShowsHumanName is the reproduction the bug report
// demands: install "demo-skill" via the skills.sh transport, then assert the pending
// board row's Name is the human name "demo-skill", NOT a UUID/identifier. If this ever
// shows a UUID, the staged SKILL.md frontmatter `name` (the field the board reads) was
// not the source skill's human name.
func TestStageNameReproSkillsShInstallShowsHumanName(t *testing.T) {
	t.Parallel()

	const humanName = "demo-skill"
	inst := newTestInstaller(t, fakeAdd(t, humanName, "a demo body", nil))

	// Stage the skills.sh install into pending/ (FS copy commits; the nil-pool audit
	// tx panics and is recovered — validation passed, the staged tree is on disk).
	if err := installPastValidation(t, inst, "owner/"+humanName); err != nil {
		t.Fatalf("Install(%q) did not pass validation: %v", humanName, err)
	}

	// Read the pending stage the SAME way the governance board does (ListStage).
	pendingDir := inst.writer.pendingDir
	archiveDir := inst.writer.archiveDir
	staged, err := ListStage(pendingDir, archiveDir, StagePending)
	if err != nil {
		t.Fatalf("ListStage(pending): %v", err)
	}
	if len(staged) != 1 {
		t.Fatalf("ListStage(pending): want exactly 1 staged skill, got %d (%+v)", len(staged), staged)
	}

	got := staged[0].Name
	if got != humanName {
		t.Fatalf("staged skill Name = %q, want the human name %q (a UUID/identifier here is the reported bug)", got, humanName)
	}
}
