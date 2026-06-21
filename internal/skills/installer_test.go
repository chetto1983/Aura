package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installer_test.go exercises the npx-skills transport with a FAKE command runner (no
// real network): the runner materializes a `.claude/skills/<name>/SKILL.md` tree under
// the install work dir, exactly as `npx skills add` would, so the strip+stage+parse+
// hash+validate path runs end to end DB-free. The happy-path hand-off to
// WriteInstallPending (the audit tx) is the db_integration backstop (TestInstaller*).

// fakeAdd returns a CommandRunner that, for `npx skills add ...`, writes a SKILL.md with
// the given name/body into <dir>/.claude/skills/<name>/ (the spike-004a layout). It
// records the argv it saw so a test can assert NO --ignore-scripts was passed.
func fakeAdd(t *testing.T, name, body string, sawArgv *[]string) CommandRunner {
	t.Helper()
	return func(_ context.Context, dir, _ string, args ...string) (string, error) {
		if sawArgv != nil {
			*sawArgv = append([]string{}, args...)
		}
		skillDir := filepath.Join(dir, ".claude", "skills", name)
		if err := os.MkdirAll(skillDir, 0o750); err != nil {
			return "", err
		}
		md := "---\nname: " + name + "\ndescription: a fake skill\ntype: instruction\n---\n" + body
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o600); err != nil {
			return "", err
		}
		return "\x1b[32mInstalled 1 skill\x1b[0m → .claude/skills/" + name + "\n", nil
	}
}

func newTestInstaller(t *testing.T, run CommandRunner) *Installer {
	t.Helper()
	w, _ := newTestWriter(t)
	return NewInstaller(InstallerConfig{
		Writer:       w,
		Run:          run,
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32,
	})
}

// TestInstallerEmptySourceRejected proves a blank source is a safe error BEFORE any
// subprocess runs (the empty edge, SKW-01).
func TestInstallerEmptySourceRejected(t *testing.T) {
	t.Parallel()
	ran := false
	inst := newTestInstaller(t, func(context.Context, string, string, ...string) (string, error) {
		ran = true
		return "", nil
	})
	for _, src := range []string{"", "   ", "\t"} {
		if _, err := inst.Install(t.Context(), src, AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidStructure) {
			t.Errorf("Install(%q) = %v, want ErrInvalidStructure", src, err)
		}
	}
	if ran {
		t.Fatal("Install must reject an empty source before running npx")
	}
}

// installPastValidation runs inst.Install over a nil-pool writer and returns the
// validation outcome BEFORE the audit tx: nil when Install got past every write-boundary
// check (the nil-pool db.WithTx then panics on Pool.Acquire, which we recover and treat
// as "validation passed"), or the validation error when a check rejected it pre-tx. This
// lets the DB-free unit tests assert the boundary edges without a live pool.
func installPastValidation(t *testing.T, inst *Installer, source string) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			// The nil-pool WriteInstallPending panicked at db.WithTx → validation passed.
			err = nil
		}
	}()
	_, err = inst.Install(t.Context(), source, AuditActor{ActorID: "cli"})
	return err
}

// TestInstallerNoIgnoreScripts proves the transport runs `npx skills add <src> -y` WITH
// scripts permitted — NO --ignore-scripts flag (D-06/D-07, post-D-09). The body is at
// the cap, so it clears validation; the argv assertion happens regardless of the (nil-
// pool) tx outcome because the runner captured the argv first.
func TestInstallerNoIgnoreScripts(t *testing.T) {
	t.Parallel()
	var argv []string
	inst := newTestInstaller(t, fakeAdd(t, "good-skill", strings.Repeat("b", 32), &argv))
	_ = installPastValidation(t, inst, "owner/repo")
	if len(argv) == 0 {
		t.Fatal("npx was never invoked")
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "skills add owner/repo -y") {
		t.Errorf("npx argv = %q, want `skills add owner/repo -y`", joined)
	}
	for _, a := range argv {
		if a == "--ignore-scripts" {
			t.Fatal("install must NOT pass --ignore-scripts (D-06/D-07; container = isolation boundary)")
		}
	}
}

// TestInstallerBodyCapBoundary proves the body-cap edge (SKW-01): one byte over the cap
// fails with ErrInvalidStructure BEFORE the hand-off; at-cap clears validation (the only
// remaining failure is the nil-pool audit tx, which installPastValidation absorbs).
func TestInstallerBodyCapBoundary(t *testing.T) {
	t.Parallel()
	overInst := newTestInstaller(t, fakeAdd(t, "over-skill", strings.Repeat("b", 33), nil))
	if _, err := overInst.Install(t.Context(), "owner/over", AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidStructure) {
		t.Errorf("over-cap install = %v, want ErrInvalidStructure", err)
	}
	atInst := newTestInstaller(t, fakeAdd(t, "at-skill", strings.Repeat("b", 32), nil))
	if err := installPastValidation(t, atInst, "owner/at"); err != nil {
		t.Errorf("at-cap install rejected by validation: %v (must clear the cap)", err)
	}
}

// TestInstallerBlocklistHit proves an NFKC-fullwidth injection literal in the staged body
// is caught (ErrBlocklisted) BEFORE the hand-off (encoding edge, SKW-01). The fullwidth
// form collapses to the ASCII blocklist literal under NFKC.
func TestInstallerBlocklistHit(t *testing.T) {
	t.Parallel()
	// A generous cap so the body-cap check (which precedes the blocklist) does not fire
	// first — the assertion is specifically the NFKC injection-literal catch.
	w, _ := newTestWriter(t)
	inst := NewInstaller(InstallerConfig{
		Writer:       w,
		Run:          fakeAdd(t, "inj-skill", "x "+toFullwidth("<|im_start|>")+" y", nil),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})
	if _, err := inst.Install(t.Context(), "owner/inj", AuditActor{ActorID: "cli"}); !errors.Is(err, ErrBlocklisted) {
		t.Fatalf("NFKC injection-literal install = %v, want ErrBlocklisted", err)
	}
}

// TestInstallerNonASCIIName proves a non-ASCII / non-grammar skill name (the staged dir
// name) is rejected by SanitizeName via ValidateNameAgainstDir (encoding edge, SKW-01).
func TestInstallerNonASCIIName(t *testing.T) {
	t.Parallel()
	inst := newTestInstaller(t, fakeAdd(t, "Bad_Name", "ok", nil))
	if _, err := inst.Install(t.Context(), "owner/bad", AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("non-grammar staged-name install = %v, want ErrInvalidName", err)
	}
}

// TestInstallerSearchDisabledWhenOptedOut proves Search returns a disabled, empty result
// (toggle state explicit) when AURA_SKILLS_EXTERNAL_DISCOVERY is explicitly opted out
// ("false"), and NEVER runs npx (no network fetch). Discovery is ON by default now, so the
// opt-out is the only way to forbid the external fetch.
func TestInstallerSearchDisabledWhenOptedOut(t *testing.T) {
	t.Setenv(externalDiscoveryEnv, "false")
	ran := false
	inst := newTestInstaller(t, func(context.Context, string, string, ...string) (string, error) {
		ran = true
		return "", nil
	})
	res, err := inst.Search(t.Context(), "xlsx")
	if err != nil {
		t.Fatalf("Search(disabled) = %v", err)
	}
	if res.Enabled {
		t.Error("Search must report Enabled=false when explicitly opted out")
	}
	if len(res.Hits) != 0 {
		t.Errorf("disabled Search must return no hits, got %v", res.Hits)
	}
	if ran {
		t.Fatal("opted-out Search must NOT run npx (no network fetch)")
	}
}

// TestInstallerSearchEnabledByDefault proves Search runs `npx skills find <q>` with no env
// set — external discovery is ON by default (Claude-Code parity, operator directive
// 2026-06-21): a normal user searches the catalog with nothing to configure.
func TestInstallerSearchEnabledByDefault(t *testing.T) {
	t.Setenv(externalDiscoveryEnv, "")
	ran := false
	inst := newTestInstaller(t, func(context.Context, string, string, ...string) (string, error) {
		ran = true
		return "\x1b[1mFound 1 skill\x1b[0m\nanthropics/skills@xlsx 12K installs\n", nil
	})
	res, err := inst.Search(t.Context(), "xlsx")
	if err != nil {
		t.Fatalf("Search(default) = %v", err)
	}
	if !res.Enabled {
		t.Error("Search must be Enabled by default (no env set)")
	}
	if !ran {
		t.Fatal("default Search must run npx (discovery on by default)")
	}
}

// TestInstallerSearchRunsWhenFlagOn proves Search runs `npx skills find <q>` only when
// the flag is on, ANSI-strips, and parses the fixed-shape result lines (D-08).
func TestInstallerSearchRunsWhenFlagOn(t *testing.T) {
	t.Setenv(externalDiscoveryEnv, "true")
	var sawArgs []string
	inst := newTestInstaller(t, func(_ context.Context, _, _ string, args ...string) (string, error) {
		sawArgs = args
		// ANSI-wrapped two-result output + a banner line that must be skipped.
		return "\x1b[1mFound 2 skills\x1b[0m\n" +
			"anthropics/skills@xlsx 12K installs\n" +
			"someone@docx 900 installs\n", nil
	})
	res, err := inst.Search(t.Context(), "xlsx")
	if err != nil {
		t.Fatalf("Search(enabled) = %v", err)
	}
	if !res.Enabled {
		t.Fatal("Search must report Enabled=true when the flag is on")
	}
	if strings.Join(sawArgs, " ") != "skills find xlsx" {
		t.Errorf("npx argv = %q, want `skills find xlsx`", strings.Join(sawArgs, " "))
	}
	if len(res.Hits) != 2 {
		t.Fatalf("want 2 parsed hits, got %d: %+v", len(res.Hits), res.Hits)
	}
	if res.Hits[0].Source != "anthropics/skills" || res.Hits[0].Skill != "xlsx" || res.Hits[0].Installs != "12K" {
		t.Errorf("hit[0] = %+v, want anthropics/skills@xlsx 12K", res.Hits[0])
	}
}

// TestInstallerSearchEmptyState proves an enabled search whose output has no result lines
// renders an empty (non-nil) Hits slice (the empty-state edge, SKW-01).
func TestInstallerSearchEmptyState(t *testing.T) {
	t.Setenv(externalDiscoveryEnv, "1")
	inst := newTestInstaller(t, func(context.Context, string, string, ...string) (string, error) {
		return "\x1b[1mFound 0 skills\x1b[0m\nNo results.\n", nil
	})
	res, err := inst.Search(t.Context(), "nonexistent-xyz")
	if err != nil {
		t.Fatalf("Search(empty) = %v", err)
	}
	if !res.Enabled || res.Hits == nil || len(res.Hits) != 0 {
		t.Fatalf("empty search = %+v, want Enabled=true + empty non-nil Hits", res)
	}
}

// TestInstallChecklistFiveItems proves the surfaced checklist has exactly FIVE items and
// none names --ignore-scripts (post-D-09; the install is RISKY + container-isolated).
func TestInstallChecklistFiveItems(t *testing.T) {
	t.Parallel()
	items := installChecklist()
	if len(items) != 5 {
		t.Fatalf("checklist must have exactly 5 items, got %d", len(items))
	}
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Label), "ignore-scripts") {
			t.Fatalf("checklist must NOT mention --ignore-scripts, found %q", it.Label)
		}
	}
}
