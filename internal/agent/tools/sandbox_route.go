package tools

import (
	"encoding/json"
	pathpkg "path"
	"strings"
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
