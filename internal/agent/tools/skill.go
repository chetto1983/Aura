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
// This plan (11-02) wires the READ actions list|info|use; the write/install
// actions (create|update|delete|install|catalog|restore|archive) are reserved
// router keys returning a "not yet wired" error so the schema enum is complete
// from the start and downstream plans (11-03/04/05) just fill handlers.
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
	// Catalog is the consumer-declared catalog seam the action=catalog handler
	// dispatches against (11-06, default-ON D-12). Nil on the pool-free manifest path;
	// action=catalog without a catalog returns a clear error, never a panic.
	Catalog skillCatalog
	// Installer is the consumer-declared install seam the action=install handler
	// dispatches against (11-06). Nil on the pool-free manifest path; action=install
	// without an installer returns a clear error, never a panic.
	Installer skillInstaller

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
	// Snippet resolves an ACTIVE snippet skill into its by-path invocation (D-04):
	// the docs instructions, the in-sandbox /skills path, and the interpreter the
	// model passes to sandbox_exec. ok=false when the named skill is absent or not a
	// snippet (action=use falls back to the instruction-skill authority-frame path).
	Snippet(name string) (instructions, sandboxPath, interpreter string, ok bool)
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
// (a property-level enum on a string is wire-safe). The write/install actions are
// in the enum from the start (reserved, D-01) so the schema is downstream-stable.
const skillParamsSchema = `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["list", "info", "use", "create", "update", "delete", "install", "catalog", "restore", "archive"], "description": "The skill operation: list (show available skills; pass an optional query to rank by relevance); info (read a skill's body for inspection by name); use (apply a skill's instructions to the current task by name); create (author a new skill); update (revise an existing skill); delete (remove a skill); catalog (browse skills.sh for installable skills by query); install (clone + stage a third-party skill from a repo URL). create/update/delete/install are STAGED as pending and require explicit human approval before they take effect — you cannot approve your own changes. (restore and archive manage the skill library and are not yet available.)"},
    "name": {"type": "string", "description": "Required when action=info, use, create, update, or delete. The exact skill name (lowercase, [a-z0-9-], 1-64 chars) to inspect, apply, author, revise, or remove."},
    "description": {"type": "string", "description": "Required when action=create or update. A one-line summary of what the skill does (shown in the skill manifest)."},
    "body": {"type": "string", "description": "Required when action=create or update. The markdown instructions that make up the skill."},
    "always": {"type": "boolean", "description": "Optional when action=create or update. When true the skill's instructions are always applied (an always-on skill); always-on skills are gated like any other change and reviewed loudly."},
    "repo": {"type": "string", "description": "Required when action=install. The git repo URL of the skill to install (https/ssh/git). The skill is cloned, staged as pending, and requires human approval; bundled scripts never run automatically."},
    "query": {"type": "string", "description": "Required when action=catalog (the keyword phrase to search skills.sh). Optional when action=list (ranks the skill list by relevance when the full manifest is too large to show at once)."}
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
	// The lead names the FULL grammar (amendment #49): the read verbs AND the
	// catalog/install discovery loop — leaving catalog/install only in the schema
	// enum description left live models unaware the catalog existed (the 2026-06-05
	// E2E brute-forced ad-hoc code instead). Still a fixed const: turn-stable, D-06.
	const lead = "Skills are packaged capabilities (instructions and runnable snippets) that extend you for specific tasks. " +
		"Call action=use with a skill name to apply its instructions to the current task; action=info reads a skill without applying it; action=list shows what is available. " +
		"If NO installed skill covers a reusable task family (spreadsheets, documents, file formats, integrations, recurring workflows), " +
		"action=catalog searches a public catalog of installable skills and action=install stages one — installation always requires operator approval via ask_user; you can request, never grant.\n\n" +
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

// notYetWired is the reserved-action handler: the schema enum lists create/update/
// delete/install/catalog/restore/archive from the start (D-01) so it is stable, but
// their handlers land in downstream plans. Until then they return a clear error.
func notYetWired(action string) ActionFunc {
	return func(context.Context, json.RawMessage) (ToolResult, error) {
		return ToolResult{}, fmt.Errorf("skill: action %q is not yet available", action)
	}
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
			// Discovery→install loop (11-06): catalog browses skills.sh (default-ON,
			// D-12); install clones + stages into pending + ask_user gate (D-13). Install
			// NEVER self-activates (D-03).
			"install": t.actionInstall,
			"catalog": t.actionCatalog,
			// Reserved (D-01) — downstream plans fill these handlers.
			"restore": notYetWired("restore"),
			"archive": notYetWired("archive"),
		})
	}
	return t.router
}
