package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// UseAuthorityFrame is the load-bearing literal that wraps a skill body when the
// model applies it (D-08): it frames the body as instructions to follow for the
// current task, not as inert reference text. Changing this string changes how the
// model treats an applied skill — it is part of the contract. Exported so the agui
// composer pinned-skill path (37D Mechanism A) prepends the EXACT same frame the
// `use` action emits, rather than re-declaring the literal (DRY — the string IS the
// contract).
const UseAuthorityFrame = "Follow these skill instructions for the current task:\n\n"

// listSkillTail rides on every list result: a list miss is the capability-gap
// decision point. Discovery+install is NOT a tool action (amendment #51 / D-40) —
// it points the model at the always-on find-skills skill, which teaches the
// `npx skills find/add` self-extension loop in the sandbox. Fixed per-turn result
// literal — no cache invariant rides on it.
const listSkillTail = "\n\nNOTE: this listed INSTALLED skills only. If none cover the task family at hand, the always-on find-skills skill teaches how to discover and install skills from the open ecosystem — follow it before hand-coding the deliverable."

// actionList renders the manifest, or — when a query is supplied and the skill set
// is large — ranks the skills by BM25 over their name+description and returns the
// top matches (D-09 overflow path). With no query it returns the full manifest the
// Description already shows, so the model can re-read it on demand.
func (t *SkillTool) actionList(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if t.Loader == nil {
		return ToolResult{}, fmt.Errorf("skill list: no skill loader configured")
	}
	var a skillArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("skill list args: %w", err)
	}
	query := strings.TrimSpace(a.Query)
	if query == "" {
		return NewResult(ctx, t.Loader.ManifestDescription()+listSkillTail)
	}

	ranked, err := t.orderSkillList(ctx, query)
	if err != nil {
		return ToolResult{}, err
	}
	if len(ranked) == 0 {
		// A queried list miss is the capability-gap decision point. Discovery+install
		// is NOT a tool action (amendment #51 / D-40): point the model at the always-on
		// find-skills skill, which teaches the npx self-extension loop in the sandbox.
		return NewResult(ctx, fmt.Sprintf(
			"No INSTALLED skills match %q. This searched only the installed list.\n"+
				"NEXT STEP (for a reusable artifact-family task before any hand-built fallback): the always-on find-skills skill teaches how to discover and install a skill from the open ecosystem — follow it.", query))
	}
	var b strings.Builder
	for _, s := range ranked {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
	}
	return NewResult(ctx, b.String()+listSkillTail)
}

// rankSkills ranks the skills by BM25 over a "name description" search document
// per skill, reusing the in-process bm25Index (bm25.go). A skill's description is
// its one-line capability blurb, so it goes in the Spec field the ranker indexes as
// the retrieval document; the returned slice is index-aligned back to the input
// skills via the ranked doc id.
func rankSkills(skills []SkillMeta, query string) []SkillMeta {
	if len(skills) == 0 {
		return nil
	}
	specs := make([]Spec, len(skills))
	for i, s := range skills {
		specs[i] = Spec{Name: s.Name, Summary: s.Description}
	}
	idx := newBM25Index(specs)
	out := make([]SkillMeta, 0, len(skills))
	for _, r := range idx.rank(query) {
		out = append(out, skills[r.doc])
	}
	return out
}

// actionInfo returns a skill's plain body for inspection/diffing (D-08) — no
// authority frame, so the model reads what a skill says without being instructed
// to act on it.
func (t *SkillTool) actionInfo(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	body, err := t.requireBody(raw, "info")
	if err != nil {
		return ToolResult{}, err
	}
	return NewResult(ctx, body)
}

// actionUse applies a skill (D-08). For a SNIPPET (type:snippet) it renders the docs instructions +
// a by-path invocation + the interpreter so the model runs it via shell_exec. Under a STRICT
// profile it renders the IN-BOX SandboxPath (the routed shell_exec then runs the snippet the box
// materialized at /skills/<name>/..., D-10); otherwise it renders the HOST export-dir path (D-01
// host-primary). action=use EXECUTES NOTHING itself (no backend.Exec) — it only chooses which path
// the subsequent shell_exec runs. For an instruction skill it returns the body wrapped in the
// authority frame. The body is delivered via NewResult so a large skill pages through the sidecar.
func (t *SkillTool) actionUse(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if t.Loader == nil {
		return ToolResult{}, fmt.Errorf("skill use: no skill loader configured")
	}
	var a skillArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("skill use args: %w", err)
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return ToolResult{}, fmt.Errorf("skill use: name is required")
	}
	if instructions, hostPath, sandboxPath, interpreter, ok := t.Loader.Snippet(name); ok {
		path := hostPath
		if t.SandboxRouter.Strict() {
			path = sandboxPath
		}
		return NewResult(ctx, renderSnippetUse(instructions, path, interpreter))
	}
	body, ok := t.Loader.Body(name)
	if !ok {
		return ToolResult{}, fmt.Errorf("skill use: unknown skill %q", name)
	}
	return NewResult(ctx, UseAuthorityFrame+body)
}

// renderSnippetUse frames a snippet's by-path invocation for the model: the
// instructions, then a shell_exec call running the snippet at runPath. It names
// shell_exec and no other tool — actionUse has already chosen runPath (host export
// dir, or the in-box path under a strict profile), and which side of that boundary
// the call lands on is the deployment's decision, not the model's (amendment #96).
// The literal command shape ("<interpreter> '<runPath>'") mirrors the shell_exec arg
// schema so the model assembles a correct call.
func renderSnippetUse(instructions, runPath, interpreter string) string {
	var b strings.Builder
	b.WriteString(UseAuthorityFrame)
	if instructions != "" {
		b.WriteString(instructions)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Run this stored snippet by path with shell_exec: command=%q (append further args as needed).\n",
		interpreter+" "+shellQuotePath(runPath))
	return b.String()
}

// shellQuotePath makes a snippet path safe to paste into the shell_exec command
// the model runs (WR-04). shell_exec ALWAYS runs through bash -c (Git Bash on
// Windows), where a Windows path with backslashes (C:\Users\...) mangles on \U/\n
// escapes and a path under a spaced dir (C:\Program Files\...) word-splits. Normalize
// to forward slashes (filepath.ToSlash for the host OS, plus an explicit backslash
// fold so a path produced on a Windows host stays forward-slashed even when this runs
// on a POSIX host — Git Bash accepts forward-slash drive paths, mirroring runner.go's
// announced-workspace ToSlash treatment) and single-quote so spaces are safe; an
// embedded single quote is escaped the POSIX way ('\”).
func shellQuotePath(p string) string {
	slashed := strings.ReplaceAll(filepath.ToSlash(p), `\`, "/")
	return "'" + strings.ReplaceAll(slashed, "'", `'\''`) + "'"
}

// requireBody resolves the `name` arg and fetches the skill body, returning a
// structured error when name is missing or the skill is unknown.
func (t *SkillTool) requireBody(raw json.RawMessage, action string) (string, error) {
	if t.Loader == nil {
		return "", fmt.Errorf("skill %s: no skill loader configured", action)
	}
	var a skillArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("skill %s args: %w", action, err)
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return "", fmt.Errorf("skill %s: name is required", action)
	}
	body, ok := t.Loader.Body(name)
	if !ok {
		return "", fmt.Errorf("skill %s: unknown skill %q", action, name)
	}
	return body, nil
}
