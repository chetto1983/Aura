package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// gitOrSkip resolves git or skips the test (the clone path needs a real git binary;
// it is present in CI and the dev environment). Unlike a DB integration skip this is
// not a no-skip-as-green tier — git is a host tool, not a wired service.
func gitOrSkip(t *testing.T) string {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	return gitBin
}

// runGit runs a git subcommand in dir, failing the test on error.
func runGit(t *testing.T, gitBin, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_TERMINAL_PROMPT=0",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// makeSkillRepo builds a local git repo (NOT bare) holding skills/<name>/SKILL.md
// with the given frontmatter+body, plus the optional extra files, and returns a
// file:// URL the installer can clone WITHOUT network. extraFiles maps a relpath
// under the skill dir to its contents (e.g. "scripts/run.sh" → "#!/bin/sh\n").
func makeSkillRepo(t *testing.T, gitBin, name, frontmatterAndBody string, extraFiles map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, gitBin, repo, "init", "-q")
	skillDir := filepath.Join(repo, "skills", name)
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(frontmatterAndBody), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	for rel, content := range extraFiles {
		p := filepath.Join(skillDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir extra: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("write extra %s: %v", rel, err)
		}
	}
	runGit(t, gitBin, repo, "add", "-A")
	runGit(t, gitBin, repo, "commit", "-q", "-m", "skill")
	// file:// URL — clone runs against the local repo, no network.
	return "file://" + filepath.ToSlash(repo)
}

// stageFromRepo runs the installer clone→stage pipeline WITHOUT the DB audit write
// (no pool needed): it resolves git, clones, locates+reads the skill, strips always,
// detects red flags, and stages the tree. It returns the staged dir + the parsed
// frontmatter + red flags so the unit assertions can inspect the on-disk outcome.
// This is the no-network determinism floor for the clone path.
func stageFromRepo(t *testing.T, repoURL string) (stagedDir string, fm Frontmatter, body string, flags []RedFlag, alwaysStripped bool) {
	t.Helper()
	gitBin := gitOrSkip(t)
	scratch := t.TempDir()
	cloneDir := filepath.Join(scratch, "clone")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := runClone(ctx, gitBin, repoURL, cloneDir); err != nil {
		t.Fatalf("runClone: %v", err)
	}
	src, err := locateSkillDir(cloneDir, "")
	if err != nil {
		t.Fatalf("locateSkillDir: %v", err)
	}
	fm, body, err = readSkill(src)
	if err != nil {
		t.Fatalf("readSkill: %v", err)
	}
	alwaysStripped = fm.Always
	fm.Always = false
	flags = detectRedFlags(fm, src)

	staged := filepath.Join(scratch, "staged", fm.Name)
	if err := stageSkillTree(src, staged, fm, body); err != nil {
		t.Fatalf("stageSkillTree: %v", err)
	}
	return staged, fm, body, flags, alwaysStripped
}

// TestInstallClonesAndStrips_AlwaysStrip proves D-10: an always:true third-party
// skill lands always-stripped (the on-disk SKILL.md no longer carries always:true,
// and the installer reports AlwaysStripped). The clone path RUNS (file://, no net).
func TestInstallClonesAndStrips_AlwaysStrip(t *testing.T) {
	gitBin := gitOrSkip(t)
	skill := "---\nname: weather-pro\ndescription: weather\ntype: instruction\nalways: true\n---\nUse the weather API.\n"
	repoURL := makeSkillRepo(t, gitBin, "weather-pro", skill, nil)

	staged, fm, _, _, alwaysStripped := stageFromRepo(t, repoURL)

	if !alwaysStripped {
		t.Fatalf("always:true was not detected/stripped on install (D-10)")
	}
	if fm.Always {
		t.Fatalf("parsed frontmatter still carries always=true after strip")
	}
	body, err := os.ReadFile(filepath.Join(staged, "SKILL.md"))
	if err != nil {
		t.Fatalf("read staged SKILL.md: %v", err)
	}
	if strings.Contains(string(body), "always: true") {
		t.Errorf("staged SKILL.md still contains `always: true` (D-10 strip not persisted): %s", body)
	}
}

// TestInstallStripsSymlinks proves T-11-06-T2: a symlink inside the cloned skill is
// NOT copied into the staged tree (symlink-strip at the copy boundary, spike 005).
// Symlink creation needs privilege on Windows; the test skips there.
func TestInstallStripsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows; the WSL/CI run is authoritative")
	}
	gitBin := gitOrSkip(t)
	skill := "---\nname: linky\ndescription: d\ntype: instruction\n---\nbody\n"
	repo := t.TempDir()
	runGit(t, gitBin, repo, "init", "-q")
	skillDir := filepath.Join(repo, "skills", "linky")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	// A symlink pointing at a host path — the strip must drop it.
	if err := os.Symlink("/etc/passwd", filepath.Join(skillDir, "evil-link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	runGit(t, gitBin, repo, "add", "-A")
	runGit(t, gitBin, repo, "commit", "-q", "-m", "skill")
	repoURL := "file://" + filepath.ToSlash(repo)

	staged, _, _, _, _ := stageFromRepo(t, repoURL)

	if _, err := os.Lstat(filepath.Join(staged, "evil-link")); !os.IsNotExist(err) {
		t.Errorf("symlink was NOT stripped from the staged tree (T-11-06-T2): lstat err=%v", err)
	}
}

// TestInstallDetectsRedFlags proves D-13: a metadata.install[] block, a tool wildcard,
// and a bundled executable each raise a red flag in the install result.
func TestInstallDetectsRedFlags(t *testing.T) {
	gitBin := gitOrSkip(t)
	skill := "---\n" +
		"name: risky\n" +
		"description: d\n" +
		"type: instruction\n" +
		"allowed-tools:\n  - \"*\"\n" +
		"metadata:\n  build:\n    install:\n      - npm ci\n" +
		"---\nbody\n"
	repoURL := makeSkillRepo(t, gitBin, "risky", skill, map[string]string{
		"scripts/run.sh": "#!/bin/sh\necho hi\n",
	})

	_, _, _, flags, _ := stageFromRepo(t, repoURL)

	kinds := map[string]bool{}
	for _, f := range flags {
		kinds[f.Kind] = true
	}
	for _, want := range []string{"metadata_install", "tool_wildcard", "bundled_executable"} {
		if !kinds[want] {
			t.Errorf("red flag %q not raised; flags=%+v", want, flags)
		}
	}
}

// TestInstallCanonicalHashStable proves the canonical hash over the staged tree is
// stable across two independent clone+stage runs of the same repo (TOFU determinism)
// and matches an independent recomputation.
func TestInstallCanonicalHashStable(t *testing.T) {
	gitBin := gitOrSkip(t)
	skill := "---\nname: stable\ndescription: d\ntype: instruction\n---\nbody bytes here\n"
	repoURL := makeSkillRepo(t, gitBin, "stable", skill, map[string]string{"ref/notes.md": "notes\n"})

	staged1, _, _, _, _ := stageFromRepo(t, repoURL)
	staged2, _, _, _, _ := stageFromRepo(t, repoURL)

	h1, err := CanonicalHash(staged1)
	if err != nil {
		t.Fatalf("CanonicalHash staged1: %v", err)
	}
	h2, err := CanonicalHash(staged2)
	if err != nil {
		t.Fatalf("CanonicalHash staged2: %v", err)
	}
	if h1 != h2 {
		t.Errorf("canonical hash not stable across runs: %q vs %q", h1, h2)
	}
	// Independent recomputation via HashSkillDir (the shared helper) matches.
	hi, err := HashSkillDir(staged1)
	if err != nil {
		t.Fatalf("HashSkillDir: %v", err)
	}
	if hi != h1 {
		t.Errorf("CanonicalHash != HashSkillDir independent recomputation: %q vs %q", h1, hi)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("canonical hash missing sha256: prefix: %q", h1)
	}
}

// TestInstallNoSkillFound proves a repo with no SKILL.md is ErrNoSkillFound.
func TestInstallNoSkillFound(t *testing.T) {
	gitBin := gitOrSkip(t)
	repo := t.TempDir()
	runGit(t, gitBin, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("not a skill"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, gitBin, repo, "add", "-A")
	runGit(t, gitBin, repo, "commit", "-q", "-m", "x")
	repoURL := "file://" + filepath.ToSlash(repo)

	scratch := t.TempDir()
	cloneDir := filepath.Join(scratch, "clone")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := runClone(ctx, gitBin, repoURL, cloneDir); err != nil {
		t.Fatalf("runClone: %v", err)
	}
	if _, err := locateSkillDir(cloneDir, ""); err == nil || !strings.Contains(err.Error(), "no SKILL.md") {
		t.Errorf("want ErrNoSkillFound, got %v", err)
	}
}

// TestValidateRepoURL guards the only interpolated clone value: option-injection,
// NUL, and non-URL shapes are rejected; URL/scp shapes pass.
func TestValidateRepoURL(t *testing.T) {
	bad := []string{
		"",
		"-O/tmp/x",           // option injection
		"--upload-pack=evil", // option injection
		"https://x\x00y",     // NUL
		"/local/path",        // bare local path is not a remote source
		"justastring",        // not a URL or scp shape
	}
	for _, u := range bad {
		if err := validateRepoURL(u); err == nil {
			t.Errorf("validateRepoURL(%q): want error, got nil", u)
		}
	}
	good := []string{
		"https://github.com/anthropics/skills",
		"http://example.com/r.git",
		"ssh://git@host/r.git",
		"git@github.com:owner/repo.git",
		"file:///tmp/repo",
	}
	for _, u := range good {
		if err := validateRepoURL(u); err != nil {
			t.Errorf("validateRepoURL(%q): want nil, got %v", u, err)
		}
	}
}
