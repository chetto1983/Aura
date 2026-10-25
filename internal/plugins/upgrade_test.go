package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// pluginFilesV2 is plugin "p" at its next version: s1 as before, s2 rewritten, s3 new, and the
// stdio connector on a new argument.
func pluginFilesV2() map[string]string {
	files := pluginFiles("2.0.0")
	files["p/skills/s2/SKILL.md"] = skillMD("s2") + " v2"
	files["p/skills/s3/SKILL.md"] = skillMD("s3")
	files["p/.mcp.json"] = `{"mcpServers":{"local":{"command":"uvx","args":["srv@2"]},
		"remote":{"type":"http","url":"https://r.example/mcp"}}}`
	return files
}

// publish makes files the head of the default branch at commit.
func (w *world) publish(commit string, files map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.commits[commit] = files
	w.head = commit
}

func upgrade(t *testing.T, b *Backend) UpgradeReport {
	t.Helper()
	rep, err := b.Upgrade(context.Background(), "admin", "p")
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	return rep
}

func readTreeFile(t *testing.T, b *Backend, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(b.d.Store.TreeDir("p"), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

func TestUpgradeAtTheRecordedCommitIsUpToDate(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	rep := upgrade(t, b)
	if !rep.UpToDate || rep.Started() || rep.Commit != commitA || rep.Version != "1.0.0" {
		t.Fatalf("report = %+v, want up to date at %s 1.0.0", rep, commitA)
	}
	if len(w.unmounts) != 0 || len(w.registry.updates) != 1 {
		t.Errorf("an up-to-date upgrade acted: unmounts %v, updates %q", w.unmounts, w.registry.updates)
	}
}

func TestUpgradeSwapsTheTreeAndReinstallsThePlugin(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.publish(commitB, pluginFilesV2())
	installedBefore := len(w.library.installed)

	rep := upgrade(t, b)
	if rep.UpToDate || !rep.Started() || rep.FromVersion != "1.0.0" || rep.FromCommit != commitA ||
		rep.Version != "2.0.0" || rep.Commit != commitB {
		t.Fatalf("report = %+v, want 1.0.0@A → 2.0.0@B", rep)
	}
	rec, _, _ := b.Get("p")
	if rec.Commit != commitB || rec.Version != "2.0.0" || rec.Phase != PhaseInstalled || !slices.Equal(rec.Connectors, []string{"local", "remote"}) {
		t.Fatalf("record = %+v", rec)
	}
	if got := readTreeFile(t, b, "skills/s2/SKILL.md"); !strings.HasSuffix(got, " v2") {
		t.Errorf("the kept tree still holds the old s2: %q", got)
	}
	if _, err := os.Stat(filepath.Join(b.d.Store.Root, "p", backupDir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the backup survived a finished upgrade: %v", err)
	}

	for name, want := range map[string]string{"s1": "unchanged", "s2": "updated", "s3": "new"} {
		if s := stepOf(rep.Skills, name); s.Outcome != OutcomeOK || s.Reason != want {
			t.Errorf("%s step = %+v, want ok %s", name, s, want)
		}
	}
	tree := b.d.Store.TreeDir("p")
	wantInstalled := []string{filepath.Join(tree, "skills", "s2"), filepath.Join(tree, "skills", "s3")}
	if got := w.library.installed[installedBefore:]; !slices.Equal(got, wantInstalled) {
		t.Errorf("reinstalled %v, want %v", got, wantInstalled)
	}
	if len(rec.Skills) != 3 {
		t.Errorf("recorded skills = %+v", rec.Skills)
	}
	for _, s := range rec.Skills {
		if hash, _ := w.library.ActiveHash(s.Name); hash != s.Hash {
			t.Errorf("%s recorded %s, active %s", s.Name, s.Hash, hash)
		}
	}

	local, _ := w.registry.server("local")
	if !slices.Equal(local.Args, []string{"srv@2"}) || local.Trust.Reason != "plugin upgrade o/r/p@"+commitB {
		t.Errorf("local = %+v", local)
	}
	if last := w.registry.updates[len(w.registry.updates)-1]; last != "plugin-upgrade p plugin upgrade o/r/p@"+commitB {
		t.Errorf("last update = %q", last)
	}
	if len(w.registry.updates) != 2 {
		t.Errorf("updates = %q, want the install's and one upgrade", w.registry.updates)
	}
	if got := w.registry.doc.ProfileServerNames(""); !slices.Equal(got, []string{"local", "remote"}) {
		t.Errorf("active profile = %v", got)
	}
	slices.Sort(w.unmounts)
	if !slices.Equal(w.unmounts, []string{"local", "remote"}) {
		t.Errorf("unmounted %v, want every connector before the swap", w.unmounts)
	}
	if got := w.mounted[2:]; !slices.Equal(got, []string{"local", "remote"}) {
		t.Errorf("mounted after the upgrade %v", got)
	}
}

// The tree a server runs from is never replaced under it: every connector is unmounted before
// the swap, and the new one is verified against the new tree.
func TestUpgradeVerifiesAgainstTheNewTree(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.publish(commitB, pluginFilesV2())
	var seen string
	var unmountedFirst bool
	w.onVerify = func() {
		seen = readTreeFile(t, b, "skills/s2/SKILL.md")
		rec, _, _ := b.Get("p")
		w.mu.Lock()
		unmountedFirst = len(w.unmounts) == 2 && rec.Commit == commitB && rec.Phase == PhaseUpgrading
		w.mu.Unlock()
	}
	upgrade(t, b)
	if !strings.HasSuffix(seen, " v2") || !unmountedFirst {
		t.Errorf("verification saw tree %q, unmounted and recorded first: %v", seen, unmountedFirst)
	}
}

func TestUpgradeKeepsAnEditedSkillAndDeletesADroppedOne(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	files := pluginFiles("2.0.0")
	files["p/skills/s1/SKILL.md"] = skillMD("s1") + " v2"
	delete(files, "p/skills/s2/SKILL.md")
	w.publish(commitB, files)
	w.library.active["s1"] = "sha256:edited"
	installedBefore := len(w.library.installed)

	rep := upgrade(t, b)
	if s := stepOf(rep.Skills, "s1"); s.Outcome != OutcomeSkipped || s.Reason != "changed since install" {
		t.Errorf("s1 step = %+v", s)
	}
	if s := stepOf(rep.Skills, "s2"); s.Outcome != OutcomeOK || s.Reason != "removed" {
		t.Errorf("s2 step = %+v", s)
	}
	if len(w.library.installed) != installedBefore || !slices.Equal(w.library.deleted, []string{"s2"}) {
		t.Errorf("installed %v, deleted %v", w.library.installed[installedBefore:], w.library.deleted)
	}
	if w.library.active["s1"] != "sha256:edited" {
		t.Error("the operator's edit was overwritten")
	}
	rec, _, _ := b.Get("p")
	if len(rec.Skills) != 1 || rec.Skills[0].Name != "s1" || rec.Skills[0].Hash == "sha256:edited" {
		t.Errorf("recorded skills = %+v, want s1 with its install hash", rec.Skills)
	}
}

// Past the registry write a skill is on its own, as at install: one that fails keeps its old
// content and its old record entry, and the upgrade still finishes.
func TestUpgradeKeepsTheOldEntryOfASkillThatFailedToUpdate(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	before, _, _ := b.Get("p")
	w.publish(commitB, pluginFilesV2())
	w.library.failInstall["s2"] = true
	rep, err := b.Upgrade(context.Background(), "admin", "p")
	if err != nil || !rep.Started() {
		t.Fatalf("Upgrade = (%+v, %v)", rep, err)
	}
	if s := stepOf(rep.Skills, "s2"); s.Outcome != OutcomeFailed {
		t.Errorf("s2 step = %+v", s)
	}
	rec, _, _ := b.Get("p")
	if i := slices.IndexFunc(rec.Skills, func(s SkillEntry) bool { return s.Name == "s2" }); i < 0 || rec.Skills[i] != before.Skills[1] {
		t.Errorf("recorded skills = %+v, want s2's install entry kept", rec.Skills)
	}
}

func TestUpgradeRollsBackWhenANewConnectorFailsItsHandshake(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	before, _, _ := b.Get("p")
	w.publish(commitB, pluginFilesV2())
	w.failVer["local"] = true
	assertRolledBack(t, w, b, before, "handshake failed")
}

func TestUpgradeRollsBackAFailedRegistryWrite(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	before, _, _ := b.Get("p")
	w.publish(commitB, pluginFilesV2())
	w.registry.fail = errors.New("database is unreachable")
	assertRolledBack(t, w, b, before, "database is unreachable")
}

func assertRolledBack(t *testing.T, w *world, b *Backend, before Record, cause string) {
	t.Helper()
	installedBefore, updatesBefore := len(w.library.installed), len(w.registry.updates)
	rep, err := b.Upgrade(context.Background(), "admin", "p")
	if !errors.Is(err, ErrUpgradeRolledBack) || !strings.Contains(err.Error(), cause) || !strings.Contains(err.Error(), "1.0.0") {
		t.Fatalf("Upgrade = %v, want a rollback naming %q and the version kept", err, cause)
	}
	if rep.Started() {
		t.Errorf("a rolled-back upgrade reports it started: %+v", rep)
	}
	if rec, _, _ := b.Get("p"); rec.Commit != before.Commit || rec.Phase != PhaseInstalled || !slices.Equal(rec.Skills, before.Skills) {
		t.Errorf("record = %+v, want %+v", rec, before)
	}
	if got := readTreeFile(t, b, "skills/s2/SKILL.md"); strings.HasSuffix(got, " v2") {
		t.Error("the new tree stayed in place")
	}
	if _, err := os.Stat(filepath.Join(b.d.Store.Root, "p", backupDir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the backup survived the rollback: %v", err)
	}
	if len(w.library.installed) != installedBefore || len(w.library.deleted) != 0 || len(w.registry.updates) != updatesBefore {
		t.Errorf("a rolled-back upgrade wrote: skills %v, deleted %v, updates %q",
			w.library.installed[installedBefore:], w.library.deleted, w.registry.updates)
	}
	if local, _ := w.registry.server("local"); !slices.Equal(local.Args, []string{"srv"}) {
		t.Errorf("local = %+v, want the old connector", local)
	}
	if got := w.mounted[2:]; !slices.Equal(got, []string{"local", "remote"}) {
		t.Errorf("remounted %v, want the old connectors back", got)
	}
}

// Codex reinstalls a plugin whose installed copy is missing even at the same version
// (loader.rs: no active_plugin_version). On the lab VM on 2026-10-25 the broker's connector was
// removed from the MCP board while its record stayed: the upgrade at the same commit puts it back.
func TestUpgradeReinstallsAConnectorRemovedFromTheRegistry(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.registry.mu.Lock()
	w.registry.doc.RemoveServer("local")
	w.registry.mu.Unlock()

	rep := upgrade(t, b)
	if rep.UpToDate || !rep.Started() || rep.FromCommit != commitA || rep.Commit != commitA {
		t.Fatalf("report = %+v, want a reinstall at %s", rep, commitA)
	}
	if local, ok := w.registry.server("local"); !ok || local.Source != "pack:o/r/p" {
		t.Errorf("local = %+v (present %v), want it back from the plugin", local, ok)
	}
	if got := w.mounted[2:]; !slices.Equal(got, []string{"local", "remote"}) {
		t.Errorf("mounted after the reinstall %v", got)
	}
	if s := stepOf(rep.Skills, "s1"); s.Reason != "unchanged" {
		t.Errorf("s1 step = %+v, want unchanged", s)
	}
	if rep2 := upgrade(t, b); !rep2.UpToDate {
		t.Errorf("after the reinstall the plugin is not up to date: %+v", rep2)
	}
}

// A connector name another source took since the install is not the plugin's to take back: it
// does not make the plugin look out of date, or every upgrade would reinstall it.
func TestUpgradeIsUpToDateWhenAConnectorNameIsHeldByAnotherSource(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.put("local", "custom")
	if rep := upgrade(t, b); !rep.UpToDate {
		t.Fatalf("report = %+v, want up to date", rep)
	}
}

// A crash between the swap and the final record leaves the new commit recorded in the
// upgrading phase. The next upgrade finishes the job rather than calling it up to date.
func TestUpgradeFinishesAnUpgradeThatStopped(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.publish(commitB, pluginFilesV2())
	rec, _, _ := b.Get("p")
	rec.Commit, rec.Phase = commitB, PhaseUpgrading
	if err := b.d.Store.WriteRecord(rec); err != nil {
		t.Fatal(err)
	}
	rep := upgrade(t, b)
	if rep.UpToDate || !rep.Started() {
		t.Fatalf("report = %+v, want the upgrade finished", rep)
	}
	if rec, _, _ := b.Get("p"); rec.Phase != PhaseInstalled || rec.Version != "2.0.0" {
		t.Errorf("record = %+v", rec)
	}
}

func TestUpgradeRefusesAPluginThatChangedItsName(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	files := pluginFilesV2()
	files["p/.claude-plugin/plugin.json"] = `{"name":"q","version":"2.0.0"}`
	w.publish(commitB, files)
	if _, err := b.Upgrade(context.Background(), "admin", "p"); !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), `"q"`) {
		t.Fatalf("Upgrade = %v, want a refusal naming the new plugin", err)
	}
	if rec, _, _ := b.Get("p"); rec.Commit != commitA || len(w.unmounts) != 0 {
		t.Errorf("a refused upgrade acted: record %+v, unmounts %v", rec, w.unmounts)
	}
}

func TestUpgradeOfAPluginBeingRemovedIsRefused(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.publish(commitB, pluginFilesV2())
	rec, _, _ := b.Get("p")
	rec.Phase = PhaseRemoving
	if err := b.d.Store.WriteRecord(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Upgrade(context.Background(), "admin", "p"); err == nil || !strings.Contains(err.Error(), "remov") {
		t.Fatalf("Upgrade = %v, want a refusal saying the removal must finish", err)
	}
}

func TestUpgradeOfAnUnknownNameIsNotInstalled(t *testing.T) {
	t.Parallel()
	_, b := newWorld(t)
	if _, err := b.Upgrade(context.Background(), "admin", "ghost"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Upgrade = %v, want ErrNotInstalled", err)
	}
}

func TestUpgradeAllUpgradesEachInstalledPlugin(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.publish(commitB, pluginFilesV2())
	reports, err := b.UpgradeAll(context.Background(), "admin")
	if err != nil || len(reports) != 1 || reports[0].Name != "p" || reports[0].Version != "2.0.0" {
		t.Fatalf("UpgradeAll = (%+v, %v)", reports, err)
	}
	reports, err = b.UpgradeAll(context.Background(), "admin")
	if err != nil || len(reports) != 1 || !reports[0].UpToDate {
		t.Fatalf("second UpgradeAll = (%+v, %v), want up to date", reports, err)
	}
}

func TestUpgradeAllReportsAFailureAndGoesOn(t *testing.T) {
	t.Parallel()
	w, b := newWorld(t)
	reviewAndInstall(t, b)
	w.headErr = errors.New("github is unreachable")
	reports, err := b.UpgradeAll(context.Background(), "admin")
	if err == nil || !strings.Contains(err.Error(), "p: ") || !strings.Contains(err.Error(), "unreachable") || len(reports) != 1 {
		t.Fatalf("UpgradeAll = (%+v, %v), want the failure named after its plugin", reports, err)
	}
}
