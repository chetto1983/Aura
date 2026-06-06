package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// useAuthorityFrame is the load-bearing literal that wraps a skill body when the
// model applies it (D-08): it frames the body as instructions to follow for the
// current task, not as inert reference text. Changing this string changes how the
// model treats an applied skill — it is part of the contract.
const useAuthorityFrame = "Follow these skill instructions for the current task:\n\n"

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

	skills := t.Loader.List()
	ranked := rankSkills(skills, query)
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
// per skill, reusing the in-process bm25Index (bm25.go). The skills are projected
// into synthetic Specs (Name+Description) so the shared ranker scores them; the
// returned slice is index-aligned back to the input skills via the ranked doc id.
func rankSkills(skills []SkillMeta, query string) []SkillMeta {
	if len(skills) == 0 {
		return nil
	}
	specs := make([]Spec, len(skills))
	for i, s := range skills {
		specs[i] = Spec{Name: s.Name, Description: s.Description}
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

// actionUse applies a skill (D-08). For a SNIPPET (type:snippet) it returns the docs
// instructions + the stable in-sandbox /skills path + the interpreter so the model
// runs it BY PATH via sandbox_exec (D-04 — NO bespoke run, NEVER the exec bit). For
// an instruction skill it returns the body wrapped in the authority frame. The body
// is delivered via NewResult so a large skill pages through the sidecar (>preview cap).
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
	if instructions, sandboxPath, interpreter, ok := t.Loader.Snippet(name); ok {
		return NewResult(ctx, renderSnippetUse(instructions, sandboxPath, interpreter))
	}
	body, ok := t.Loader.Body(name)
	if !ok {
		return ToolResult{}, fmt.Errorf("skill use: unknown skill %q", name)
	}
	return NewResult(ctx, useAuthorityFrame+body)
}

// renderSnippetUse frames a snippet's by-path invocation for the model (D-04): the
// instructions, then the exact sandbox_exec call to run the snippet by path (never
// the exec bit). The literal command shape mirrors the sandbox_exec arg schema so the
// model assembles a correct call.
func renderSnippetUse(instructions, sandboxPath, interpreter string) string {
	var b strings.Builder
	b.WriteString(useAuthorityFrame)
	if instructions != "" {
		b.WriteString(instructions)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Run this snippet by path: call sandbox_exec with command=%q and args=[%q] (add further args as needed). Do NOT execute it as %s directly — always invoke the interpreter with the path.\n",
		interpreter, sandboxPath, sandboxPath)
	return b.String()
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
