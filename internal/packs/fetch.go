package packs

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// gitHubHTTPS is where a `owner/repo` source resolves. It is the default and not a setting on
// purpose: `source` arrives from the skills catalogue or an operator's request, and letting a
// pack name its own host would turn a catalogue entry into an arbitrary fetch target.
// GitFetcher.BaseURL exists only so a test can point it at a local bare repository.
const gitHubHTTPS = "https://github.com/"

// GitFetcher materializes a repository with git, either at its default branch or at a pinned
// commit.
//
// git is the right tool rather than the GitHub contents API because a pack is a TREE — a
// manifest, a skills directory, a connector file — and walking that over the API is one request
// per node plus paging, against a rate limit, to rebuild what one clone already hands over. The
// aura image ships git 2.39.5 (verified in the running container, 2026-08-23).
//
// --depth 1 and --filter=blob:none keep it to the one commit and only the blobs actually read.
// No submodules: a submodule would be a second repository arriving under the trust the
// operator granted the first one.
type GitFetcher struct {
	BaseURL string
}

var commitShape = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ValidCommit accepts a full 40-character lowercase commit id and nothing else. A tag or a
// branch can move between the review and the install; a commit cannot.
func ValidCommit(s string) error {
	if !commitShape.MatchString(s) {
		return fmt.Errorf("commit %q: want the full 40-character id", s)
	}
	return nil
}

// Fetch materializes source into dir, which exists and is empty, and returns the commit it
// landed on. An empty commit means the default branch.
func (g GitFetcher) Fetch(ctx context.Context, source, dir, commit string) (string, error) {
	var runs [][]string
	if commit == "" {
		args, err := g.cloneArgs(source, dir)
		if err != nil {
			return "", err
		}
		runs = [][]string{args}
	} else {
		var err error
		if runs, err = g.fetchAtCommitArgs(source, dir, commit); err != nil {
			return "", err
		}
	}
	for _, args := range runs {
		if _, err := runGit(ctx, args); err != nil {
			return "", fmt.Errorf("fetch %s: %w", source, err)
		}
	}
	head, err := runGit(ctx, []string{"-C", dir, "rev-parse", "HEAD"})
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", source, err)
	}
	return strings.TrimSpace(head), nil
}

func runGit(ctx context.Context, args []string) (string, error) {
	// #nosec G204 -- fixed argv built by cloneArgs/fetchAtCommitArgs; the only interpolated
	// values are a URL from a validated owner/repo pair, a caller-owned temporary directory and
	// a commit id that passed ValidCommit.
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// cloneArgs builds the default-branch argv. It is separate from the exec so the flags — which
// are the whole substance of this fetcher and where a mistake would actually live — are
// asserted by a test that needs no network and no git.
func (g GitFetcher) cloneArgs(source, dir string) ([]string, error) {
	url, err := g.url(source)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("clone destination is empty")
	}
	return []string{
		"clone",
		"--depth", "1",
		"--filter=blob:none",
		"--no-tags",
		"--recurse-submodules=no",
		"--config", "advice.detachedHead=false",
		url, dir,
	}, nil
}

// fetchAtCommitArgs builds the pinned-commit argv: an empty repository, a shallow partial fetch
// of exactly that commit, and a detached checkout of it. `git clone` cannot take a commit id.
func (g GitFetcher) fetchAtCommitArgs(source, dir, commit string) ([][]string, error) {
	if err := ValidCommit(commit); err != nil {
		return nil, err
	}
	url, err := g.url(source)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("clone destination is empty")
	}
	return [][]string{
		{"init", dir},
		{"-C", dir, "fetch", "--depth", "1", "--filter=blob:none", "--no-tags", "--recurse-submodules=no", url, commit},
		{"-C", dir, "-c", "advice.detachedHead=false", "checkout", "--detach", "FETCH_HEAD"},
	}, nil
}

// url turns `owner/repo` into a repository URL, refusing anything that is not exactly that
// shape. A source carrying its own scheme, host, userinfo or query would otherwise reach git
// verbatim.
func (g GitFetcher) url(source string) (string, error) {
	ref, err := ParseRef(source)
	if err != nil {
		return "", err
	}
	if ref.Directory != "" {
		return "", fmt.Errorf("clone source %q: want owner/repo, not a plugin path", source)
	}
	base := g.BaseURL
	if base == "" {
		base = gitHubHTTPS
	}
	return base + ref.Source + ".git", nil
}
