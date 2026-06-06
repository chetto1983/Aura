package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// SkillTool is the ONE non-deferred skills verb the model sees (D-01/D-05). A
// single manifest entry — name "skill" — fronts the whole skills grammar via an
// `action` enum dispatched through an ActionRouter, mirroring the `task` tool
// (the pre-rewrite N-tool god-class is the anti-pattern this replaces). It is
// NON-deferred (D-05): the skill manifest the model needs to pick a skill rides in
// this tool's Description, so the spec must always be visible — a deferred skill
// tool would hide the very manifest the model searches.
//
// This tool wires the READ actions list|info|use, the authoring actions
// create|update|delete (gated, pending→approve), and the snippet-lifecycle actions
// save_snippet|restore|archive (18-03): save_snippet stages a reusable snippet UNGATED
// (D-02 — normal result, never a pause), restore un-archives a snippet, archive
// de-materializes one (SAFE tier). Discovery+install of third-party skills is NOT a
// tool concern (amendment #51 / D-40): the always-on find-skills skill teaches the
// model to self-extend via `npx skills find/add` in the sandbox, so the catalog client
// + native installer + their tool actions were deleted.
//
// The live loader is injected at registration via the consumer-declared skillLoader
// seam below (golang-structs-interfaces: the consumer owns the interface), so this
// package never imports internal/skills concretely — the live *skills.Loader is
// adapted at the composition root (cmd/aura). The Description is computed from the
// loader snapshot at build time (turn-stable, busts the prefix cache ONCE on
// add/remove — accepted, D-06).
type SkillTool struct {
	Loader skillLoader
	// Writer is the consumer-declared write seam the create/update/delete actions
	// dispatch against (11-05). Nil on the pool-free manifest paths (`aura tools`)
	// and in unit tests that exercise only the read actions — a write action without
	// a writer returns a clear error, never a panic.
	Writer skillWriter
	// Alerter is the optional headless-alert seam (D-26): non-nil only in a context
	// with no interactive resume (a swarm worker / cron job) so a gated mutation
	// proposed there fires an immediate operator alert. Nil in the interactive REPL.
	Alerter skillAlerter

	router *ActionRouter
}

// SkillMeta is the tool-local projection of a loaded skill the manifest renders
// over. It is deliberately decoupled from internal/skills.Skill so the tools
// package declares its own seam (interface segregation): the live loader adapts
// its skills into this shape at registration.
type SkillMeta struct {
	Name        string
	Description string
}

// skillLoader is the consumer-declared seam the skill tool dispatches against. The
// live internal/skills.Loader satisfies it through a thin adapter wired at
// registration (cmd/aura), keeping internal/agent/tools free of an internal/skills
// import. ManifestDescription renders the turn-stable alphabetical manifest (D-06);
// BM25Corpus + corpus-index→name mapping back the overflow `list` ranker (D-09).
type skillLoader interface {
	// List returns the loaded skills (name+description) in a stable order.
	List() []SkillMeta
	// Body returns the markdown body of the named skill, or ok=false if absent.
	Body(name string) (string, bool)
	// ManifestDescription renders the byte-stable, alphabetical manifest block that
	// becomes this tool's Description (with the BM25-overflow tail past the cap).
	ManifestDescription() string
	// Snippet resolves an ACTIVE snippet skill into its HOST by-path invocation
	// (D-01 host-primary): the docs instructions, the host export-dir path the model
	// runs through shell_exec, and the interpreter. The host path is resolved in the
	// cmd/aura adapter from AURA_SKILL_EXPORT_DIR via skills.SnippetHostPath. ok=false
	// when the named skill is absent or not a snippet (action=use then falls back to
	// the instruction-skill authority-frame path).
	Snippet(name string) (instructions, hostPath, interpreter string, ok bool)
}

// skillArgs is the wire shape of the skill tool arguments. Only `action` is
// required at the schema root (D-10); `name` and `query` are per-action and their
// requirements are documented in the schema field descriptions, never as a root
// oneOf/anyOf/enum.
type skillArgs struct {
	Action string `json:"action"`
	Name   string `json:"name"`
	Query  string `json:"query"`
}

// skillParamsSchema is the OpenAI-wire-safe JSON schema (D-10), mirroring
// taskParamsSchema's discipline VERBATIM: the root object's only required field is
// `action`; per-action requirements are spelled out in the field descriptions.
// There is intentionally NO root-level oneOf/anyOf/enum — a root enum 400s
// OpenAI-compat providers (DeepSeek). The `action` property does carry an enum
// (a property-level enum on a string is wire-safe), as does the `language` property
// for save_snippet. The snippet-lifecycle actions (save_snippet/restore/archive) are
// wired (18-03). Discovery+install is NOT a tool action (amendment #51 / D-40 — the
// find-skills always-on skill teaches `npx skills find/add` in the sandbox).
const skillParamsSchema = `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["list", "info", "use", "create", "update", "delete", "save_snippet", "restore", "archive"], "description": "The skill operation: list (show available skills; pass an optional query to rank by relevance); info (read a skill's body for inspection by name); use (apply a skill's instructions, or run a stored snippet by path, by name); create (author a new skill); update (revise an existing skill); delete (remove a skill); save_snippet (store a reusable executable code snippet so you can re-run it by path on a later turn instead of re-authoring it); restore (un-archive a previously archived snippet back to active); archive (de-activate a snippet you no longer need — recoverable with restore). create/update/delete are STAGED as pending and require explicit human approval before they take effect — you cannot approve your own changes. save_snippet stages the snippet as pending too (it activates after operator approval); restore/archive take effect immediately."},
    "name": {"type": "string", "description": "Required when action=info, use, create, update, delete, save_snippet, restore, or archive. The exact skill/snippet name (lowercase, [a-z0-9-], 1-64 chars) to inspect, apply, author, revise, remove, save, restore, or archive."},
    "description": {"type": "string", "description": "Required when action=create or update (and optional for save_snippet). A one-line summary of what the skill/snippet does (shown in the skill manifest)."},
    "body": {"type": "string", "description": "Required when action=create or update. The markdown instructions that make up the skill."},
    "language": {"type": "string", "enum": ["python", "shell", "js"], "description": "Required when action=save_snippet. The language of the snippet code (python, shell, or js)."},
    "code": {"type": "string", "description": "Required when action=save_snippet. The executable snippet body (the code that will be saved and later run by path)."},
    "always": {"type": "boolean", "description": "Optional when action=create or update. When true the skill's instructions are always applied (an always-on skill); always-on skills are gated like any other change and reviewed loudly."},
    "query": {"type": "string", "description": "Optional when action=list (ranks the skill list by relevance when the full manifest is too large to show at once)."}
  },
  "required": ["action"]
}`

// Spec returns the non-deferred manifest entry (D-05). The Description IS the
// turn-stable, alphabetical skill manifest (D-06) computed from the loader
// snapshot — the model reads it to choose a skill, then calls action=use.
func (t *SkillTool) Spec() Spec {
	return Spec{
		Name:        "skill",
		Summary:     "List, inspect, and apply skills that extend your capabilities.",
		Description: t.manifestDescription(),
		Parameters:  json.RawMessage(skillParamsSchema),
		Deferred:    false,
	}
}

// manifestDescription renders the loader's turn-stable manifest, or a fixed
// notice when no loader is wired (the pool/loader-free manifest path, e.g.
// `aura tools`, still lists the tool's Spec without a half-wired loader).
func (t *SkillTool) manifestDescription() string {
	// The lead names ONLY the read/author verbs (amendment #51 / D-40): discovery+
	// install of third-party skills is NOT a tool action — the always-on find-skills
	// skill teaches the model to self-extend via the skills CLI in the sandbox. Fixed
	// const: turn-stable, D-06.
	const lead = "Skills are packaged capabilities (instructions and runnable snippets) that extend you for specific tasks. " +
		"Call action=use with a skill name to apply its instructions to the current task; action=info reads a skill without applying it; action=list shows what is available. " +
		"When NO installed skill covers a reusable task family (spreadsheets, documents, file formats, integrations, recurring workflows), an always-on skill teaches how to discover and install skills from the open ecosystem.\n\n" +
		"Available skills:\n"
	if t.Loader == nil {
		return lead + "(none loaded)\n"
	}
	return lead + t.Loader.ManifestDescription()
}

// Execute parses the `action` discriminator and dispatches through the
// ActionRouter (lazily built once, bound to this tool's loader). It never panics
// on a bad action — the router returns a structured error.
func (t *SkillTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var head struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return ToolResult{}, fmt.Errorf("skill args: %w", err)
	}
	if head.Action == "" {
		return ToolResult{}, fmt.Errorf("skill: action is required")
	}
	return t.actionRouter().Dispatch(ctx, head.Action, raw)
}

func (t *SkillTool) actionRouter() *ActionRouter {
	if t.router == nil {
		t.router = NewActionRouter(map[string]ActionFunc{
			"list": t.actionList,
			"info": t.actionInfo,
			"use":  t.actionUse,
			// Write actions (11-05): validate->gate->pending->ask_user pause (D-02). There
			// is deliberately NO model-facing approve action — activation is human-only
			// (D-03): only an ask_user resume or the `aura skills approve` CLI activate.
			"create": t.actionCreate,
			"update": t.actionUpdate,
			"delete": t.actionDelete,
			// Snippet lifecycle (18-03): save_snippet is the UNGATED in-loop save (D-02 —
			// normal result, NEVER a pause); restore is Archive's inverse; archive is the
			// manual SAFE-tier de-materialize. (Discovery+install is NOT a tool action —
			// amendment #51 / D-40: the find-skills always-on skill teaches self-extension
			// via the skills CLI in the sandbox.)
			"save_snippet": t.actionSaveSnippet,
			"restore":      t.actionRestore,
			"archive":      t.actionArchive,
		})
	}
	return t.router
}
