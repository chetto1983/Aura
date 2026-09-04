package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// SkillTool is the deferred skills verb the model discovers on demand (D-01/D-05).
// A manifest entry — name "skill" — fronts the READ grammar via an `action` enum
// dispatched through an ActionRouter, mirroring the `task` tool (the pre-rewrite
// N-tool god-class is the anti-pattern this replaces). The full skill manifest rides
// in this tool's Description after tool_search exposes it; the default manifest
// carries only the deferred summary.
//
// Its own router wires list|info|use. The authoring actions (create|update|delete),
// install and the snippet lifecycle (save_snippet|restore|archive) are IMPLEMENTED as
// methods here but dispatched by SkillManageTool, which is where they appear to the
// model and where the governance.write check lives — see skill_manage.go for why the
// grammar was split. save_snippet stores a reusable snippet UNGATED (D-02 — normal
// result, never a pause), restore un-archives one, archive de-materializes one.
//
// install came back (amendment #51 / D-40 had removed it in favour of teaching the
// model `npx skills find/add`): the CLI installs into the directory it is standing in,
// which is not a loader root, so the taught procedure ended in "Installation complete"
// followed by a skill that does not exist as far as Aura is concerned. Discovery still
// belongs to the CLI — `npx skills find` only prints — but the install has to come back
// through the Installer that validates, writes and audits.
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

	routerOnce sync.Once
	router     *ActionRouter
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
	// Snippet resolves an ACTIVE snippet skill into its by-path invocation: the IN-BOX sandboxPath
	// (skills.SnippetSandboxPath — /skills/<name>/<name>.<ext>, the SAME root MaterializeIn lands
	// the snippet at, D-10), plus the docs instructions and the interpreter. There is one path
	// because shell_exec has one place to run. ok=false when the named skill is absent or not a
	// snippet (action=use then falls back to the instruction authority-frame path).
	Snippet(name string) (instructions, sandboxPath, interpreter string, ok bool)
}

// skillLoaderInvalidator is optional so deterministic test/read-only loaders do
// not need cache semantics. The live adapter implements it to make a successful
// write visible to info/use/list in the same turn.
type skillLoaderInvalidator interface {
	Invalidate()
}

func (t *SkillTool) invalidateLoader() {
	if loader, ok := t.Loader.(skillLoaderInvalidator); ok {
		loader.Invalidate()
	}
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

// skillParamsSchemaHonest is the ONE OpenAI-wire-safe JSON schema the skill tool
// exposes (D-10), mirroring taskParamsSchema's discipline VERBATIM: the root
// object's only required field is `action`; per-action requirements are spelled out
// in the field descriptions. There is intentionally NO root-level oneOf/anyOf/enum —
// a root enum 400s OpenAI-compat providers (DeepSeek). The `action` property does
// carry an enum (a property-level enum on a string is wire-safe). This is the READ
// half's schema: list|info|use and their two arguments. Authoring, install and the
// snippet lifecycle carry their own schema in skillManageParamsSchema below.
//
// HONESTY (AG-011 / AG-044 / amendment #97): the descriptions state the ACTUAL
// single-operator trust boundary, not an approval ceremony nobody performs. Aura runs
// for one trusted operator on their own host (amendment #50 / D-15c), so EVERY write
// action takes effect when it is written — there is no staging stage and no approval
// step left to describe. This schema is read by the model on every turn; a sentence
// here that promises an approval is a sentence that teaches the model to wait for
// something that will never happen, which is exactly how the save-then-cannot-use
// failure survived.
const skillParamsSchemaHonest = `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["list", "info", "use"], "description": "use applies a skill's instructions to the current task, or runs a stored snippet, by name; info reads a skill's body without applying it; list shows what is available. Authoring, installing and snippet lifecycle live in skill_manage."},
    "name": {"type": "string", "description": "Required for info and use. The exact skill/snippet name (lowercase, [a-z0-9-], 1-64 chars)."},
    "query": {"type": "string", "description": "Optional for list: ranks the manifest by relevance when it is too large to show at once."}
  },
  "required": ["action"]
}`

// skillManageParamsSchema is the write half (D-10 discipline unchanged): the root's
// only required field is `action`, per-action requirements live in the field
// descriptions, and there is NO root-level oneOf/anyOf/enum — a root enum 400s
// OpenAI-compat providers. Property-level enums are wire-safe.
//
// HONESTY (AG-011 / AG-044 / amendment #97) is preserved, compressed: an authorized
// administrator's write takes effect when written. The schema must not promise an
// approval step nobody performs — that is what taught the
// model to wait, and it is why the save-then-cannot-use failure survived. The prior
// wording spent ~50 tokens restating it per-action; one clause carries the same contract.
const skillManageParamsSchema = `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["create", "update", "delete", "install", "save_snippet", "restore", "archive"], "description": "create authors a new skill and update revises one; delete removes it; install fetches a skill from the open ecosystem into the deployment library; save_snippet stores runnable code you can re-run by path on a later turn instead of re-authoring it; archive de-activates a snippet and restore un-archives it. Every write takes effect immediately: what you create, update, install or save is usable on this same turn. Nothing is staged and nothing waits for approval. Never install by running the skills CLI in a terminal - it writes outside the library and the skill will not load."},
    "name": {"type": "string", "description": "Required for every action except install. The exact skill/snippet name (lowercase, [a-z0-9-], 1-64 chars)."},
    "description": {"type": "string", "description": "Required for create, update and save_snippet. A one-line summary, shown in the skill manifest - it is how you find it again. Refused without one."},
    "body": {"type": "string", "description": "Required for create and update. The markdown instructions that make up the skill."},
    "source": {"type": "string", "description": "Required for install: owner/repo (optionally owner/repo@skill-name), a URL, or a local path."},
    "language": {"type": "string", "enum": ["python", "shell", "js"], "description": "Required for save_snippet. The language of the snippet code."},
    "code": {"type": "string", "description": "Required for save_snippet. The executable body, run later by path."},
    "always": {"type": "boolean", "description": "Optional for create and update. true injects the body into every future turn's always-on block instead of only when applied by name - standing instructions only, since it costs context every turn."}
  },
  "required": ["action"]
}`

// Spec returns the deferred manifest entry (D-05). The Description is the
// turn-stable, alphabetical skill manifest (D-06) exposed on demand from the
// loader snapshot.
func (t *SkillTool) Spec() Spec {
	return Spec{
		Name:        "skill",
		Summary:     "List, inspect, and apply skills that extend your capabilities.",
		Description: skillReadDescription,
		Parameters:  json.RawMessage(skillParamsSchemaHonest),
		// NOT deferred, and cheap enough to say so: the catalogue of installed skills no
		// longer rides this Description — it is rendered into the messages[1] always-block
		// beside the always-on skill bodies, which is where hermes-agent and Claude Code
		// both keep it. Measured on the live registry (11 skills installed): the tool was
		// 7091 bytes (~1773 tokens) and the single heaviest entry in the manifest; the
		// constant text below is ~400. Deferring the READ verb was buying ~400 tokens at
		// the price of a tool_search round trip before the model could see that skills
		// exist at all — the same "capability it cannot see is a capability it does not
		// use" failure the prompt-manifest guard exists to stop.
		Deferred: false,
		// READ-ONLY (the write half is skill_manage). list/info/use were ALREADY pinned to
		// scoring.Safe by the gateway's skillFixedTiers, and classify gives a non-Mutating
		// tool exactly Safe, so splitting them out changes no tier: it only stops the reads
		// from dragging the authoring grammar into the manifest with them.
		Mutating:    false,
		Multiplexed: false,
	}
}

// skillReadDescription is the CONSTANT read-verb description (D-06 byte-stability).
//
// It deliberately carries no catalogue. The manifest of installed skills is per-turn
// live state, and embedding it here put it inside the `tools` array, so every skill
// add/remove rewrote the tools payload and invalidated the provider's prefix cache —
// while also making this the most expensive tool in the manifest. The catalogue now
// renders into the messages[1] always-block (skills.RenderAlwaysBlock's caller), which
// is already the seam for "rebuilt per turn from live skill state, without touching
// messages[0]". What stays here is what the model needs at the moment it decides
// whether to reach for a skill at all.
const skillReadDescription = "Skills are packaged capabilities (instructions and runnable snippets) that extend you for specific tasks. " +
	"Call action=use with a skill name to apply its instructions to the current task; action=info reads a skill without applying it; action=list shows what is available (with an optional query to rank it). " +
	"The skills installed right now are listed for you in the always-on block near the start of this conversation — read that list before assuming a capability is missing.\n\n" +
	// The order used to live in the system prompt, thousands of tokens from the
	// decision. It belongs with the schema, where it is read at the moment the
	// question "skill or hand-code?" is actually being answered.
	"Order for a reusable task family: look there FIRST, install second, hand-write last. " +
	"Having the libraries (openpyxl, pandas, a node package) is not a reason to skip it — the skill is the tested playbook for the family, the library is not. " +
	"Installing is skill_manage action=install source=<owner/repo>: one call and the skill is usable this same turn. NEVER install by running the skills CLI yourself — it writes into whatever directory it is standing in, which is not the library, and the skill will not load however encouraging the CLI output looks. " +
	"Skill work is bounded: apply the obvious skill once, then execute. A skill that is instructions-only, or that points at a script which is not there, is guidance — implement with the tools you have instead of hunting for the missing file."

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
	t.routerOnce.Do(func() {
		t.router = NewActionRouter(map[string]ActionFunc{
			"list": t.actionList,
			"info": t.actionInfo,
			"use":  t.actionUse,
		})
	})
	return t.router
}
