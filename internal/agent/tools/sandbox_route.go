package tools

import (
	"context"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// This file holds the shared seam the strict-profile box routing rides on (SBX-01/GATE-01, plan
// 37-07): the fail-CLOSED deny result every routed tool returns when its per-identity box cannot
// be reached, plus the small POSIX helpers the box branches use. Under dev/local_trusted the tools
// never reach any of this — a nil router's Route returns routed=false and the host path runs
// byte-for-byte (SC-4).

// boxWorkspaceRoot is the box's writable workspace mount root; send_file only delivers artifacts
// under it. boxSkillsRoot is the materialized read-only skills mount (MaterializeIn lands skills
// there); fs_write refuses to write into it so the gated skill-authoring flow is not bypassed even
// inside the box (the box /skills is a materialized COPY — the host skills library is structurally
// untouchable from the box, D-10 — but the fence still guards the in-box copy).
const (
	boxWorkspaceRoot = "/workspace"
	boxSkillsRoot    = "/skills"
)

// sandboxUnavailableResult is the fail-CLOSED deny result a ROUTED tool returns when its
// per-identity box could not be resolved or reached (D-09/GATE-01). It mirrors
// shellApprovalRequiredResult's inline {error,message} shape so the model self-corrects on it. The
// invariant it encodes: when routed=true the tool NEVER falls back to the host os/exec / os.ReadFile
// path — a box failure DENIES with guidance instead of silently running on the host.
func sandboxUnavailableResult(tool string, cause error) ToolResult {
	payload := map[string]string{
		"error": "sandbox_unavailable",
		"tool":  tool,
		"message": "The per-identity sandbox is required for this operation under the active security profile " +
			"but could not be reached, so the request was DENIED — it was NOT run on the host. Retry shortly; " +
			"if it persists the sandbox runtime is likely down and an operator must restore it.",
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
// command run inside the box. Unlike shellQuotePath (skill_read.go) it does NOT ToSlash — a box
// path is already POSIX; an embedded single quote is escaped the POSIX way ('\”).
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

// withinBoxWorkspace reports whether a box path resolves under /workspace (the pre-copy fence
// send_file's routed branch applies BEFORE CopyArtifactsOut — a box path is not a host path, so it
// cannot be host-stat'd; this literal-root prefix check is the box-side half, re-verified by the
// host checkWorkspace fence over the STAGED copy afterward).
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

// boxRootOrDefault resolves a caller-supplied search root to a POSIX box path, defaulting to the
// box workspace mount. Unlike rootOrDefault it never consults the HOST workspace root: inside the
// box the workspace is always mounted at boxWorkspaceRoot, and a host path means nothing there.
func boxRootOrDefault(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return boxWorkspaceRoot
	}
	if strings.HasPrefix(p, "/") {
		return pathpkg.Clean(p)
	}
	return pathpkg.Join(boxWorkspaceRoot, p)
}

// boxSkippedPath reports whether any DIRECTORY segment of a root-relative path is one the host
// walk prunes. The final element is the file name and is deliberately never tested — skipWalkDir
// is a directory rule, and the host walk applies it only to directory entries.
func boxSkippedPath(rel string) bool {
	segments := strings.Split(rel, "/")
	for _, segment := range segments[:len(segments)-1] {
		if skipWalkDir(segment) {
			return true
		}
	}
	return false
}

// boxListFiles enumerates regular files under root INSIDE the box and returns forward-slash paths
// RELATIVE to root, honouring the same skip rules and node cap as the host walk. truncated reports
// the cap was reached, so callers reuse withWalkTruncation exactly as the host branch does.
//
// Only the ENUMERATION moves into the box. Pattern compilation and matching stay in Go for both
// paths, so a routed search cannot answer differently from a host search over the same tree — the
// alternative (handing the pattern to the box's grep/find) would silently swap RE2 for POSIX
// semantics.
//
// find's exit code is ignored on purpose: it reports non-zero for any unreadable subtree while
// still printing everything it could reach, which is precisely the host walk's behaviour (its
// WalkDir callback swallows per-entry errors and continues). stderr is dropped for the same reason.
func boxListFiles(
	ctx context.Context,
	router *usersandbox.SandboxRouter,
	handle usersandbox.BoxHandle,
	root string,
) (paths []string, truncated bool, err error) {
	nodeCap := fsWalkNodeCap()
	cmd := fmt.Sprintf(
		"find %s -type f -print 2>/dev/null | head -n %d",
		shellQuoteArg(root), nodeCap+1,
	)
	res, execErr := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: cmd})
	if execErr != nil {
		return nil, false, execErr
	}
	listed := strings.Split(strings.Trim(string(res.Stdout), "\n"), "\n")
	if len(listed) > nodeCap {
		truncated = true
		listed = listed[:nodeCap]
	}
	prefix := strings.TrimSuffix(root, "/") + "/"
	for _, p := range listed {
		if p == "" {
			continue
		}
		rel, found := strings.CutPrefix(p, prefix)
		if !found || rel == "" {
			continue
		}
		if boxSkippedPath(rel) {
			continue
		}
		paths = append(paths, rel)
	}
	return paths, truncated, nil
}

// boxFile is one enumerated box file and its (possibly capped) content.
type boxFile struct {
	Rel     string
	Content []byte
}

// boxReadFiles enumerates AND reads the files under root inside the box in ONE exec, returning
// root-relative paths with their content. truncated reports that the node cap, the per-file cap or
// the total budget cut the sweep short.
//
// One exec, not one per file: a docker exec round-trip costs tens of milliseconds, so a per-file
// loop over a few hundred files would take tens of seconds. The output is NUL-framed
// (\0name\0content\0name\0content…) — safe because a NUL byte is exactly what marks a file binary,
// and binary files are dropped by the caller anyway.
//
// The point of returning CONTENT rather than letting the box match is semantic: the caller keeps
// scanning with Go's RE2, so a routed grep answers identically to a host grep. Handing the pattern
// to the box's GNU grep would quietly swap the regexp dialect underneath the same tool.
func boxReadFiles(
	ctx context.Context,
	router *usersandbox.SandboxRouter,
	handle usersandbox.BoxHandle,
	root string,
) (files []boxFile, truncated bool, err error) {
	nodeCap := fsWalkNodeCap()
	budget := fsMaxReadBytes()
	// The box shell is dash, NOT bash — `while IFS= read -r -d '' f` fails there with
	// "read: Illegal option -d", which is a silent empty sweep rather than an error, so the tool
	// reported "[no matches]" for a file it could see. `find -exec sh -c 'for f do …' sh {} +`
	// is POSIX and does the same framing in one pass. dash's printf does emit \0.
	//
	// `cd || exit 0`: a missing root yields no output rather than an error, matching the host
	// walk, whose per-entry errors are swallowed. 2>/dev/null drops unreadable-entry noise for
	// the same reason. The overall `head -c` bounds the stream; the file COUNT is bounded in
	// parseBoxFileFrames, which stops at nodeCap.
	cmd := fmt.Sprintf(
		"cd %s 2>/dev/null || exit 0; "+
			"find . -type f -exec sh -c 'for f do printf \"\\0%%s\\0\" \"$f\"; head -c %d -- \"$f\" 2>/dev/null; done' sh {} + "+
			"2>/dev/null | head -c %d",
		shellQuoteArg(root), budget, budget,
	)
	res, execErr := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: cmd})
	if execErr != nil {
		return nil, false, execErr
	}
	files, framesTruncated := parseBoxFileFrames(res.Stdout, nodeCap)
	return files, framesTruncated || int64(len(res.Stdout)) >= budget, nil
}

// parseBoxFileFrames decodes the NUL-framed sweep output into files, dropping skipped paths and
// stopping at nodeCap. Split out from boxReadFiles so the framing — the one place a malformed or
// budget-truncated stream could silently mis-attribute content to the wrong file — is testable
// without a container daemon (docker_integration code contributes zero coverage).
//
// Layout: ["", name, content, name, content, …]. A trailing element with no partner means the
// total budget cut the last file short; it is dropped rather than reported half-read, and
// truncated says so.
func parseBoxFileFrames(stdout []byte, nodeCap int) (files []boxFile, truncated bool) {
	frames := strings.Split(string(stdout), "\x00")
	for i := 1; i+1 < len(frames); i += 2 {
		rel := strings.TrimPrefix(frames[i], "./")
		if rel == "" || boxSkippedPath(rel) {
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
