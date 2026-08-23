package packs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// gitHubHTTPS is where a `owner/repo` source resolves. It is a constant and not
// a setting on purpose: `source` arrives from the skills catalogue, which only
// ever indexes GitHub, and letting an installed pack name its own host would
// turn a catalogue entry into an arbitrary fetch target.
const gitHubHTTPS = "https://github.com/"

// GitFetcher shallow-clones source into dir.
//
// git is the right tool rather than the GitHub contents API because a pack is a
// TREE — a manifest, a skills directory, a connector file and a commands
// directory — and walking that over the API is one request per node plus paging,
// against a rate limit, to rebuild what one clone already hands over. The aura
// image ships git 2.39.5 (verified in the running container, 2026-08-23); the
// same binary already backs `npx skills add`, which clones a source too.
//
// --depth 1 and --filter=blob:none keep it to the one commit and only the blobs
// actually read. No submodules: a pack is markdown and JSON, and a submodule
// would be a second repository arriving under the trust the operator granted the
// first one.
func GitFetcher(ctx context.Context, source, dir string) error {
	args, err := cloneArgs(source, dir)
	if err != nil {
		return err
	}
	// #nosec G204 -- fixed argv built by cloneArgs; the only interpolated values
	// are a github.com URL from a validated owner/repo pair and a caller-owned
	// temporary directory.
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone %s: %w: %s", source, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cloneArgs builds the argv. It is separate from the exec so the flags — which
// are the whole substance of this fetcher and where a mistake would actually
// live — are asserted by a test that needs no network and no git.
func cloneArgs(source, dir string) ([]string, error) {
	url, err := cloneURL(source)
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

// cloneURL turns `owner/repo` into a github.com URL, refusing anything that is
// not exactly that shape. A source carrying its own scheme, host, userinfo or
// query would otherwise reach `git clone` verbatim.
func cloneURL(source string) (string, error) {
	ref, err := ParseRef(source)
	if err != nil {
		return "", err
	}
	if ref.Directory != "" {
		return "", fmt.Errorf("clone source %q: want owner/repo, not a plugin path", source)
	}
	return gitHubHTTPS + ref.Source + ".git", nil
}
