package packs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fakeCommit = "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"

// The pinned fetch is three git invocations. They are asserted as argv, so the flags — where a
// mistake would actually live — are checked with no network and no git.
func TestFetchAtCommitArgs(t *testing.T) {
	t.Parallel()
	got, err := GitFetcher{}.fetchAtCommitArgs("owner/repo", "/tmp/d", fakeCommit)
	if err != nil {
		t.Fatalf("fetchAtCommitArgs: %v", err)
	}
	want := [][]string{
		{"init", "/tmp/d"},
		{"-C", "/tmp/d", "fetch", "--depth", "1", "--filter=blob:none", "--no-tags", "--recurse-submodules=no",
			"https://github.com/owner/repo.git", fakeCommit},
		{"-C", "/tmp/d", "-c", "advice.detachedHead=false", "checkout", "--detach", "FETCH_HEAD"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d invocations, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("invocation %d = %q, want %q", i, got[i], want[i])
		}
	}
	if _, err := (GitFetcher{}).fetchAtCommitArgs("owner/repo", "/tmp/d", "v0.1.1"); err == nil {
		t.Error("a tag was accepted as a commit")
	}
}

// The remote head is one ls-remote of the default branch: the cheap check Codex's marketplace
// upgrade runs (git_remote_revision) before it clones anything.
func TestHeadArgs(t *testing.T) {
	t.Parallel()
	got, err := GitFetcher{}.headArgs("owner/repo")
	if err != nil {
		t.Fatalf("headArgs: %v", err)
	}
	if want := []string{"ls-remote", "--", "https://github.com/owner/repo.git", "HEAD"}; !slices.Equal(got, want) {
		t.Errorf("argv = %q, want %q", got, want)
	}
	if _, err := (GitFetcher{}).headArgs("owner/repo/plugin"); err == nil {
		t.Error("a plugin path was accepted as a repository")
	}
}

func TestParseHead(t *testing.T) {
	t.Parallel()
	if got, err := parseHead(fakeCommit + "\tHEAD\n"); err != nil || got != fakeCommit {
		t.Errorf("parseHead = (%q, %v), want %q", got, err, fakeCommit)
	}
	for _, bad := range []string{"", "\n", "v0.1.1\tHEAD\n", fakeCommit[:39] + "\tHEAD"} {
		if _, err := parseHead(bad); err == nil {
			t.Errorf("parseHead(%q) accepted", bad)
		}
	}
}

func TestValidCommit(t *testing.T) {
	t.Parallel()
	if err := ValidCommit(fakeCommit); err != nil {
		t.Errorf("a full commit was refused: %v", err)
	}
	for _, bad := range []string{fakeCommit[:39], strings.ToUpper(fakeCommit), "v0.1.1", "", fakeCommit + "0"} {
		if err := ValidCommit(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func singlePluginFetcher(resolved string, seen *string) Fetcher {
	return func(_ context.Context, _, dir, commit string) (string, error) {
		if seen != nil {
			*seen = commit
		}
		if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
			return "", err
		}
		return resolved, os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{"name":"p"}`), 0o600)
	}
}

func TestCheckoutKeepsTheTreeUntilClose(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	var asked string
	r := &Resolver{Fetch: singlePluginFetcher(fakeCommit, &asked), WorkDir: work}
	c, err := r.Checkout(t.Context(), Ref{Source: "o/r"}, fakeCommit)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if asked != fakeCommit || c.Commit != fakeCommit {
		t.Errorf("asked %q, checkout reports %q", asked, c.Commit)
	}
	if !strings.HasPrefix(c.Root, work) {
		t.Errorf("checkout root %q is not under the work dir %q", c.Root, work)
	}
	if len(c.Packs) != 1 || c.Packs[0].Name != "p" {
		t.Errorf("packs = %+v", c.Packs)
	}
	if _, err := os.Stat(c.Root); err != nil {
		t.Fatalf("the tree is gone before Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(c.Root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Close left the tree: %v", err)
	}
}

func TestCheckoutRefusesAFetcherThatLandsAnotherCommit(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	other := strings.Repeat("a", 40)
	r := &Resolver{Fetch: singlePluginFetcher(other, nil), WorkDir: work}
	if _, err := r.Checkout(t.Context(), Ref{Source: "o/r"}, fakeCommit); err == nil || !strings.Contains(err.Error(), other) {
		t.Fatalf("err = %v, want a refusal naming the commit that landed", err)
	}
	if entries, _ := os.ReadDir(work); len(entries) != 0 {
		t.Errorf("a refused checkout left %d entries in the work dir", len(entries))
	}
}

func TestCheckoutOfARepositoryRootPlugin(t *testing.T) {
	t.Parallel()
	r := &Resolver{Fetch: singlePluginFetcher(fakeCommit, nil), WorkDir: t.TempDir()}
	c, err := r.Checkout(t.Context(), Ref{Source: "o/r"}, "")
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	defer func() { _ = c.Close() }()
	if len(c.Packs) != 1 || c.Packs[0].Directory != "" {
		t.Errorf("packs = %+v, want the root plugin", c.Packs)
	}
}

// The pinned fetch runs against real git before it ever meets the VM: partial clone of a
// commit by id is where git versions and server settings differ, and the aura image ships
// git 2.39.5. The repository is a local bare one served over file://, so no network is used.
func TestGitFetcherAgainstALocalBareRepository(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("git is required under CI: %v", err)
		}
		t.Skip("git is not installed here")
	}
	base := t.TempDir()
	bare := filepath.Join(base, "owner", "repo.git")
	work := filepath.Join(base, "work")
	git(t, "", "init", "--bare", "--initial-branch=main", bare)
	git(t, "", "-C", bare, "config", "uploadpack.allowFilter", "true")
	git(t, "", "-C", bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	git(t, "", "init", "--initial-branch=main", work)
	write(t, filepath.Join(work, ".claude-plugin", "plugin.json"), `{"name":"p","version":"1"}`)
	git(t, work, "add", ".")
	git(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "one")
	first := strings.TrimSpace(git(t, work, "rev-parse", "HEAD"))
	write(t, filepath.Join(work, ".claude-plugin", "plugin.json"), `{"name":"p","version":"2"}`)
	git(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qam", "two")
	second := strings.TrimSpace(git(t, work, "rev-parse", "HEAD"))
	git(t, work, "push", "-q", bare, "main")

	root := filepath.ToSlash(base)
	if !strings.HasPrefix(root, "/") {
		root = "/" + root // a Windows drive path, file:///C:/...
	}
	fetcher := GitFetcher{BaseURL: "file://" + root + "/"}
	if got, err := fetcher.Head(t.Context(), "owner/repo"); err != nil || got != second {
		t.Errorf("Head = (%q, %v), want %s", got, err, second)
	}
	r := &Resolver{Fetch: fetcher.Fetch, WorkDir: t.TempDir()}
	head, err := r.Checkout(t.Context(), Ref{Source: "owner/repo"}, "")
	if err != nil {
		t.Fatalf("default branch: %v", err)
	}
	defer func() { _ = head.Close() }()
	if head.Commit != second || head.Packs[0].Version != "2" {
		t.Errorf("default branch = %s v%s, want %s v2", head.Commit, head.Packs[0].Version, second)
	}
	pinned, err := r.Checkout(t.Context(), Ref{Source: "owner/repo"}, first)
	if err != nil {
		t.Fatalf("pinned: %v", err)
	}
	defer func() { _ = pinned.Close() }()
	if pinned.Commit != first || pinned.Packs[0].Version != "1" {
		t.Errorf("pinned = %s v%s, want %s v1", pinned.Commit, pinned.Packs[0].Version, first)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
