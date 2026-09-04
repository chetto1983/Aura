package tools

import (
	"context"
	"fmt"
	"strings"
)

// shell_exec's skills-install interception. A `npx skills add …` typed into the box SUCCEEDS and
// is useless: the CLI writes relative to its cwd, so the tree lands in /workspace/.agents/skills/
// (plus ~50 sibling agent layouts and a skills-lock.json) where no loader reads it, the model is
// told "Installation complete", and the litter stays in a persistent volume nobody sweeps.
// Measured twice by driving the real agent — 2026-08-31 on xlsx, 2026-09-04 on tushare-finance.
//
// Routing it to the host is not a workaround for the CLI: Installer.Install runs THE SAME
// `npx skills add <source> --copy -y`, in a work dir that is on the right side of D-10, and then
// applies the gates the box structurally cannot reach — ambiguity refusal, name/dir match, content
// hash, the NFKC injection blocklist (a skill body is injected into the model's own context), and
// the audit row that commits before the write. The box still never touches the host library.

// maybeInstallResult answers a skills-install command without running it in the box. The bool
// reports whether it took the command at all: false means it is something else and must reach the
// box untouched.
func (s *ShellExec) maybeInstallResult(ctx context.Context, command string) (ToolResult, bool, error) {
	source, ok := skillsInstallSource(command)
	if !ok {
		return ToolResult{}, false, nil
	}
	if s.InstallHook == nil {
		res, err := NewResult(ctx, fmt.Sprintf(
			"Refused: `%s` would install into this container's working directory, which no skill "+
				"loader reads — the library lives outside the sandbox. Use "+
				"`skill_manage action=install source=%s` instead; it runs the same CLI where the "+
				"skill actually lands and is usable on this turn.", strings.TrimSpace(command), source))
		return res, true, err
	}
	out, err := s.InstallHook(ctx, source)
	if err != nil {
		// The failure is the model's to correct (a bad source, a blocklist hit), so it comes back
		// as the command's output rather than a tool error that hides the reason.
		out = fmt.Sprintf("Install of %q failed: %v", source, err)
	}
	res, resErr := NewResult(ctx, out)
	return res, true, resErr
}

// skillsInstallSource reports the install source a `npx skills add` / `skills add` command names,
// folding a `--skill <name>` selector into the `owner/repo@skill` form the installer takes. It
// matches on whole tokens, so `npx prettier`, `npm install` and a path like /skills/x/SKILL.md are
// left alone.
func skillsInstallSource(command string) (string, bool) {
	tokens := strings.Fields(command)
	i := indexOfSkillsAdd(tokens)
	if i < 0 {
		return "", false
	}

	var source, selector string
	for _, tok := range tokens[i:] {
		switch {
		case strings.HasPrefix(tok, "--skill="):
			selector = strings.TrimPrefix(tok, "--skill=")
		case selector == "" && (tok == "--skill" || tok == "-s"):
			selector = "\x00" // marker: the next non-flag token is the selector
		case selector == "\x00":
			selector = tok
		case strings.HasPrefix(tok, "-"):
			// any other flag (--copy, -y, --agent …) is the CLI's business, not ours
		case source == "":
			source = tok
		}
	}
	if source == "" || selector == "\x00" {
		return "", false
	}
	source = normalizeRepoSource(source)
	if selector != "" && !strings.Contains(source, "@") {
		source += "@" + selector
	}
	return source, true
}

// indexOfSkillsAdd returns the index just past a `skills add` pair — reached either bare or through
// `npx` (whose own flags, `-y`/`--yes`, sit between). It returns -1 when the tokens are some other
// command.
func indexOfSkillsAdd(tokens []string) int {
	for i, tok := range tokens {
		if tok != "skills" {
			continue
		}
		if i > 0 && !isNpxToken(tokens[i-1]) && !strings.HasPrefix(tokens[i-1], "-") {
			continue
		}
		if i+1 < len(tokens) && tokens[i+1] == "add" {
			return i + 2
		}
	}
	return -1
}

// isNpxToken reports whether tok invokes npx, in any of the spellings a model writes.
func isNpxToken(tok string) bool {
	return tok == "npx" || tok == "npx.cmd" || strings.HasSuffix(tok, "/npx")
}

// normalizeRepoSource reduces a GitHub URL to the `owner/repo` shorthand, so a `--skill` selector
// can be appended as `@skill`. Anything else — a shorthand already, a local path, another host — is
// returned untouched for the CLI to resolve.
func normalizeRepoSource(source string) string {
	trimmed := strings.TrimSuffix(strings.TrimSuffix(source, "/"), ".git")
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "git@github.com:"} {
		if rest, found := strings.CutPrefix(trimmed, prefix); found {
			return rest
		}
	}
	return source
}
