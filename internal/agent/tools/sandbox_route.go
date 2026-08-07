package tools

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// This file holds the shared seam ALL box routing rides on (SBX-01/GATE-01, plan 37-07): the
// fail-CLOSED deny result every tool returns when its per-identity box cannot be reached, plus the
// POSIX helpers the box paths use. Every profile reaches this — there is no host arm behind it.

// boxWorkspaceRoot is the box's writable workspace mount root; send_file only delivers artifacts
// under it. boxSkillsRoot is the materialized read-only skills mount (MaterializeIn lands skills
// there); fs_write refuses to write into it so the gated skill-authoring flow is not bypassed even
// inside the box (the box /skills is a materialized COPY — the host skills library is structurally
// untouchable from the box, D-10 — but the fence still guards the in-box copy).
const (
	boxWorkspaceRoot = "/workspace"
	boxSkillsRoot    = "/skills"
)

// sandboxUnavailableResult is the fail-CLOSED deny result a tool returns when its per-identity box
// could not be resolved or reached (D-09/GATE-01). It mirrors shellApprovalRequiredResult's inline
// {error,message} shape so the model self-corrects on it. The invariant it encodes: a box failure
// DENIES with guidance — there is no host os/exec or os.ReadFile path left to fall back to.
func sandboxUnavailableResult(tool string, cause error) ToolResult {
	payload := map[string]string{
		"error": "sandbox_unavailable",
		"tool":  tool,
		"message": "Your workspace container is where every command and file operation runs, and it " +
			"could not be reached, so the request was DENIED — it was NOT run on the host. Retry shortly; " +
			"if it persists the sandbox runtime is down and an operator must restore it.",
	}
	if cause != nil {
		payload["detail"] = cause.Error()
	}
	raw, err := json.Marshal(payload)
	if err != nil { // map[string]string never fails to marshal; defensive only.
		raw = []byte(`{"error":"sandbox_unavailable"}`)
	}
	return ToolResult{Preview: string(raw), Bytes: len(raw)}
}

// shellQuoteArg single-quotes a POSIX box path/argument so it is safe to paste into a `/bin/sh -c`
// command run inside the box. It does NOT ToSlash — a box path is already POSIX; an embedded
// single quote is escaped the POSIX way ('\”).
func shellQuoteArg(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// boxEnv builds the environment for a box exec from ONLY the call's extra env (never the host
// os.Environ) plus the UTF-8 defaults (T-37-07-SECRETENV: no host var crosses into an untrusted
// box; the backend re-scrubs any secret-like caller override anyway).
func boxEnv(extra map[string]string) []string {
	out := make([]string, 0, len(extra)+2)
	out = append(out, "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

// withinBoxWorkspace reports whether a box path resolves under /workspace — the pre-copy fence
// send_file applies BEFORE CopyArtifactsOut. A box path is not a host path and cannot be host-stat'd,
// so this literal-root prefix check is the box-side half; fenceWithinRoot re-verifies the STAGED
// copy afterward.
func withinBoxWorkspace(p string) bool {
	c := pathpkg.Clean("/" + strings.TrimPrefix(strings.TrimSpace(p), "/"))
	return c == boxWorkspaceRoot || strings.HasPrefix(c, boxWorkspaceRoot+"/")
}

// deniedBoxSkillsWrite reports whether a box write target is inside the materialized /skills mount
// — the box-relative equivalent of deniedSkillsWrite's host skills-library fence.
func deniedBoxSkillsWrite(p string) bool {
	c := pathpkg.Clean("/" + strings.TrimPrefix(strings.TrimSpace(p), "/"))
	return c == boxSkillsRoot || strings.HasPrefix(c, boxSkillsRoot+"/")
}

// boxPathArg resolves a caller-supplied path to a POSIX box path — the ONE resolution rule all
// five fs_* tools share. Empty means the box workspace mount, an absolute path is taken as-is, a
// relative one is joined onto boxWorkspaceRoot. It never consults the HOST workspace root: inside
// the box the workspace is always mounted at boxWorkspaceRoot, and a host path means nothing
// there. Resolving explicitly here (rather than leaning on the Docker backend's WorkingDir) keeps
// the rule owed by every Backend, not just the one that happens to set a default cwd.
//
// A leading "~" is REFUSED rather than resolved. The deleted host arm expanded it against the aura
// container's home; the box's home is a different filesystem, and neither write path can expand it
// anyway (fs_write/fs_edit copy in through tar, where no shell ever runs). Accepting it literally
// would create /workspace/~/notes.txt — a real file at a path nobody looks at, which is exactly the
// failure mode this collapse exists to remove. "~user" is left literal, as the host arm left it.
func boxPathArg(tool, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		return "", fmt.Errorf("%s: %q is a shell home shortcut with no meaning in your workspace container; "+
			"pass an absolute path (e.g. /workspace/notes.txt) or one relative to /workspace", tool, p)
	}
	if p == "" {
		return boxWorkspaceRoot, nil
	}
	if strings.HasPrefix(p, "/") {
		return pathpkg.Clean(p), nil
	}
	return pathpkg.Join(boxWorkspaceRoot, p), nil
}

// boxRelPath renders an absolute box path as the sweep-root-relative path the tools report. A file
// handed in AS the root reports under its basename (the deleted host walk reported it as ".").
func boxRelPath(root, abs string) (string, bool) {
	if abs == root {
		return pathpkg.Base(root), true
	}
	rel, found := strings.CutPrefix(abs, strings.TrimSuffix(root, "/")+"/")
	if !found || rel == "" {
		return "", false
	}
	return rel, true
}

// boxReadFileCapped is the ONE bounded read read_file and patch share: a `head -c <cap+1>` exec, so
// the size cap lands on the BYTES that come back rather than on a host stat, and an over-cap file
// is refused without ever materializing the whole thing.
//
// Three outcomes, kept distinct on purpose. A non-nil deny means the box could not be reached and
// the caller must fail CLOSED. A non-nil err is the model's own problem (missing file, over cap,
// binary) and must read as one so it can self-correct. Otherwise the content is complete.
func boxReadFileCapped(
	ctx context.Context,
	router *usersandbox.SandboxRouter,
	h usersandbox.BoxHandle,
	tool, boxPath string,
) (content []byte, deny *ToolResult, err error) {
	b, deny, err := boxReadFileRaw(ctx, router, h, tool, boxPath)
	if deny != nil || err != nil {
		return nil, deny, err
	}
	if looksBinary(b) {
		return nil, nil, fmt.Errorf("%s: binary file contains NUL bytes; use a binary-aware tool instead", tool)
	}
	return b, nil, nil
}

// boxReadFileRaw is boxReadFileCapped WITHOUT the binary refusal. read_file's document-extraction
// path (docx/xlsx are zip archives — binary by construction) needs the raw bytes to decode before
// any text-vs-binary judgment applies; every other caller wants boxReadFileCapped's refusal and
// should use that instead.
func boxReadFileRaw(
	ctx context.Context,
	router *usersandbox.SandboxRouter,
	h usersandbox.BoxHandle,
	tool, boxPath string,
) (content []byte, deny *ToolResult, err error) {
	readCap := fsMaxReadBytes()
	cmd := fmt.Sprintf("head -c %d -- %s", readCap+1, shellQuoteArg(boxPath))
	res, execErr := router.Exec(ctx, h, usersandbox.ExecRequest{Command: cmd})
	if execErr != nil {
		d := sandboxUnavailableResult(tool, execErr)
		return nil, &d, nil
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = fmt.Sprintf("cannot read %q inside the sandbox (exit %d)", boxPath, res.ExitCode)
		}
		return nil, nil, fmt.Errorf("%s: %s", tool, msg)
	}
	if int64(len(res.Stdout)) > readCap {
		return nil, nil, fmt.Errorf("%s: %s is over the %d-byte cap (%s); read a window with read_file offset+limit instead of the whole file",
			tool, boxPath, readCap, envFSMaxReadBytes)
	}
	return res.Stdout, nil, nil
}

// boxSweepDeadline bounds an fs_glob/fs_grep box sweep by AURA_FS_WALK_TIMEOUT_MS, or by the
// caller's own earlier deadline. Without it a `path:/` sweep inside the box would be bounded only
// by the turn, which is the wedge the walk budget exists to prevent; the deleted host walk
// enforced the same knob per visited entry.
func boxSweepDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, fsWalkDeadline())
}

// boxSkippedPath reports whether any DIRECTORY segment of a root-relative path is one the sweep
// prunes. The final element is the file name and is deliberately never tested — skipWalkDir is a
// directory rule and applies only to directory entries.
func boxSkippedPath(rel string) bool {
	segments := strings.Split(rel, "/")
	for _, segment := range segments[:len(segments)-1] {
		if skipWalkDir(segment) {
			return true
		}
	}
	return false
}

// boxFindPrune renders skipWalkDir's rule as a find(1) predicate. It prunes DIRECTORIES only, so a
// file merely named "vendor" is still searched, and it is always paired with `-mindepth 1` so an
// explicitly-targeted hidden root is searched rather than pruned away (the deleted host walk
// guarded the same case with `p != root`).
//
// Pruning at the producer is the point. The host walk returned filepath.SkipDir BEFORE descending,
// so a 66k-file /root/.cache never consumed a single unit of the node budget; a sweep that enumerated
// first and filtered afterwards would exhaust the cap inside the cache and report "[no matches]" for
// files that plainly exist — the appliance incident recorded at skipWalkDir.
func boxFindPrune() string {
	tests := make([]string, 0, len(walkPruneDirs)+1)
	for _, d := range append([]string{".*"}, walkPruneDirs...) {
		tests = append(tests, "-name "+shellQuoteArg(d))
	}
	return "'(' " + strings.Join(tests, " -o ") + " ')' -type d -prune"
}

// boxFile is one enumerated box file and its (possibly capped) content.
type boxFile struct {
	Rel     string
	Content []byte
}

// boxFrameSeparator returns the per-call delimiter the sweep frames its output with: a NUL, a
// fresh 130-bit random token, another NUL. The token is returned as the literal text to embed in
// the box's printf FORMAT string; sep is the byte sequence that comes back on stdout.
//
// A bare NUL is NOT a usable delimiter here, and the old comment claiming otherwise ("safe because
// a NUL byte is exactly what marks a file binary, and binary files are dropped by the caller
// anyway") had the ordering backwards: the caller's looksBinary check runs AFTER decoding, so a
// single .png in the tree injected extra frames and, on an odd count, shifted every following
// pair — yielding real text matches reported under another file's path. That is the same defect
// class as the two-filesystem document_open bug this whole collapse exists to remove. A random
// per-call token makes the desync unrepresentable instead of merely detectable, and it costs one
// exec-line's worth of bytes; a FIXED token could not be used, since a sweep of this repository
// would then find the token in this very file.
func boxFrameSeparator() (token, sep string) {
	token = rand.Text()
	return token, "\x00" + token + "\x00"
}

// boxFrameEmitter renders the per-file emitter the sweep runs inside the box: one printf of the
// framed path, then that file's bytes capped at budget.
//
// Every NUL in the format is written \000, the fully-specified three-digit octal escape, and that
// is the whole reason this is a named function rather than an inline Sprintf. POSIX printf reads a
// backslash escape as up to THREE octal digits, greedily, so the shorter "\0" sitting immediately
// before the token absorbs the token's first character whenever that character is a digit — and the
// token comes from crypto/rand.Text, whose RFC 4648 base32 alphabet ("A-Z2-7") makes 6 of its 32
// symbols digits. "\02ABC…" then reaches the box as the single byte 0x02 followed by "ABC…", so the
// separator the box EMITS is not the one parseBoxFileFrames splits on, and about one sweep in five
// decodes ZERO files: "[no matches]" for a tree full of them, the confidently-wrong answer this
// whole collapse exists to remove. Three zeros terminate the escape before the token begins, which
// makes the format correct for ANY token instead of only for the ones that happen to start with a
// letter — the constraint then lives here, not in the token generator a later edit might swap.
// Byte-verified identical on dash (the box's /bin/sh), busybox ash, bash and GNU coreutils printf.
func boxFrameEmitter(token string, budget int64) string {
	return fmt.Sprintf(
		`for f do printf "\000%s\000%%s\000%s\000" "$f"; head -c %d -- "$f" 2>/dev/null; done`,
		token, token, budget,
	)
}

// boxReadFiles enumerates AND reads the files under root inside the box in ONE exec, returning
// root-relative paths with their content. root may be a directory or a single file. truncated
// reports that the node cap, the per-file cap or the total budget cut the sweep short.
//
// One exec, not one per file: a docker exec round-trip costs tens of milliseconds, so a per-file
// loop over a few hundred files would take tens of seconds.
//
// The point of returning CONTENT rather than letting the box match is semantic: the caller keeps
// scanning with Go's RE2, so a box grep answers identically to the host grep it replaced. Handing
// the pattern to the box's GNU grep would quietly swap the regexp dialect underneath the tool.
func boxReadFiles(
	ctx context.Context,
	router *usersandbox.SandboxRouter,
	handle usersandbox.BoxHandle,
	root string,
) (files []boxFile, truncated bool, err error) {
	nodeCap := fsWalkNodeCap()
	budget := fsMaxReadBytes()
	token, sep := boxFrameSeparator()
	// The box shell is dash, NOT bash — `while IFS= read -r -d '' f` fails there with
	// "read: Illegal option -d", which is a silent empty sweep rather than an error, so the tool
	// reported "[no matches]" for a file it could see. `sh -c 'for f do …' sh …` is POSIX and
	// frames every file in one pass; dash's printf does emit NUL, subject to the octal-escape rule
	// boxFrameEmitter documents.
	//
	// The emitter is held in $e and applied twice: once directly, when root is itself a FILE (find
	// with -mindepth 1 would print nothing for it, and fs_grep's spec accepts a file `path`), and
	// once as find's -exec for a directory root. 2>/dev/null drops unreadable-entry noise, matching
	// the host walk, whose per-entry errors were swallowed. The overall `head -c` bounds the stream;
	// the file COUNT is bounded in parseBoxFileFrames, which stops at nodeCap.
	emit := boxFrameEmitter(token, budget)
	cmd := fmt.Sprintf(
		"e=%s; { [ -f %s ] && sh -c \"$e\" sh %s; "+
			"find %s -mindepth 1 %s -o -type f -exec sh -c \"$e\" sh {} + ; } 2>/dev/null | head -c %d",
		shellQuoteArg(emit),
		shellQuoteArg(root), shellQuoteArg(root),
		shellQuoteArg(root), boxFindPrune(),
		budget,
	)
	ctx, cancel := boxSweepDeadline(ctx)
	defer cancel()
	res, execErr := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: cmd})
	if execErr != nil {
		return nil, false, execErr
	}
	files, framesTruncated := parseBoxFileFrames(res.Stdout, sep, root, nodeCap)
	return files, framesTruncated || int64(len(res.Stdout)) >= budget, nil
}

// parseBoxFileFrames decodes the sweep output into files, dropping skipped paths and stopping at
// nodeCap. Split out from boxReadFiles so the framing — the one place a malformed or
// budget-truncated stream could silently mis-attribute content to the wrong file — is testable
// without a container daemon (docker_integration code contributes zero coverage).
//
// Layout after splitting on sep: ["", name, content, name, content, …]. Names are absolute box
// paths and are re-based on root. A trailing element with no partner means the total budget cut the
// last file short; it is dropped rather than reported half-read, and truncated says so.
func parseBoxFileFrames(stdout []byte, sep, root string, nodeCap int) (files []boxFile, truncated bool) {
	frames := strings.Split(string(stdout), sep)
	for i := 1; i+1 < len(frames); i += 2 {
		rel, ok := boxRelPath(root, frames[i])
		if !ok || boxSkippedPath(rel) {
			continue
		}
		files = append(files, boxFile{Rel: rel, Content: []byte(frames[i+1])})
		if len(files) >= nodeCap {
			return files, true
		}
	}
	// An even frame count means the stream ended mid-name or mid-content.
	return files, len(frames)%2 == 0
}
