# Aura Plugins — Unified Extension Model Design

Date: 2026-06-14
Status: Approved shape + decisions; pre-spec (brainstorming output)
Supersedes for Aura: `docs/superpowers/specs/2026-06-02-openclaw-plugin-compatibility-design.md`
(the Node-sidecar compatibility host is shelved — see `docs/research/2026-06-14-aura-plugin-architecture-research.md`)

## Goal

Make extending Aura **easy** for three audiences from **one** coherent model:

- **(A) Aura self-extending at runtime** — the agent installs an MCP server, writes a skill,
  or registers a hook mid-conversation, no approval ceremony (Claude-Code parity).
- **(B) CLI installer** — `aura plugins add <git|path|recipe>` wires a whole bundle in one
  command.
- **(C) Native Go seams** — a dev adds a tool / hook with minimal boilerplate that auto-wires.

The unit all three share is a declarative manifest that **composes primitives Aura already
has** (MCP servers + skills + hooks), fanned out by one installer to existing machinery.

## Non-goals

- No code-loading plugin host, no Node ESM sidecar, no dynamic native-code ABI (`dlopen`/
  WASM/`.so`). The research found every needed surface is reachable via in-process Go seams +
  MCP + skills.
- No OpenClaw binary/manifest compatibility (Aura wants the *capabilities*, not to run
  OpenClaw's JS plugins unmodified).
- No bypass of Aura's existing governance: budget, dedup, `ask_user` pause, audit ledgers.
- **Providers and channels are out of scope** for this work (see Deferred Surfaces).

## Decisions (locked 2026-06-14)

| Decision | Choice |
|---|---|
| Command + manifest name | **`aura plugins`** + **`aura.plugin.json`** |
| Hook authoring | **Both** — in-process Go (mutating hot path) + out-of-process command programs (observe/approve-deny) |
| Provider extension | **Deferred** — stays `AURA_LLM_MODEL` config; adapter-table is a future slice |

## Architecture

One manifest, one installer, three audiences. The installer is a hand-rolled `switch`
dispatcher mirroring `runMCP`/`runSkills` (`cmd/aura/main.go:44`), fanning out to the
**existing** `mcpInstall` (`cmd/aura/mcp.go:94`) and skills `Writer` (`internal/skills/writer.go:22`)
plus one new hook-register step. No new runtime.

```jsonc
// aura.plugin.json — the shared unit
{
  "name": "notion",
  "version": "0.1.0",
  "mcp":    [{ "name": "notion", "command": "npx", "args": ["-y","@notion/mcp"], "trust": "remote_http" }],
  "skills": [{ "file": "skills/notion-recipes/SKILL.md" }],
  "hooks":  [{ "event": "pre_tool", "match": "send_*", "run": "hooks/redact.sh", "mode": "approve" }]
}
```

What it composes maps 1:1 to existing seams:

| Manifest key | Fans out to | Existing seam |
|---|---|---|
| `mcp[]` | `mcpInstall` → `~/.aura/mcp/servers.json` | `internal/mcp/manager` catalog + `mcptools.bridge` (MCP tools join the **same** `tools.Registry`, kind-blind to the model) |
| `skills[]` | skills `Writer.WriteMutation` (gated, transactional) | `internal/skills` + the one `skill` tool + `messages[1]` always-block |
| `hooks[]` | new hook-register | **new** `HookManager` (Slice 1) |

## Delivery — three sequenced slices

### Slice 1 — Hooks (the one real gap)

The agent loop (`internal/agent/llm_agent.go`, `LlmAgent.Run`) has **no registrable hook
layer**, but already runs five hardcoded interceptors at exactly the points a hook layer
attaches (completion-critic veto `:348`, dedup veto `:419`, result-spillover rewrite `:559`,
reasoning router `:247`, provenance stamp). The hook layer **generalizes** these.

- Add a `HookManager` field on `LlmAgent`, injected via `LlmAgentConfig` (parallel to the
  existing `Classifier`/`Breaker`). Fired at five points:

  | Hook | Insertion (file:line) | Power |
  |---|---|---|
  | `OnTurnStart` | `llm_agent.go:187` | observe |
  | `BeforeModel(ctx, *llm.Request)` | `llm_agent.go:266` | substitute response / early-exit |
  | `BeforeTool(ctx, *llm.ToolCall)` | `llm_agent.go:419`→`:444` (composes with dedup) | **rewrite args / veto** with synthetic result |
  | `AfterTool(ctx, call, *tools.ToolResult)` | `llm_agent.go:450` (composes with spillover) | **rewrite** result before history |
  | `OnTurnEnd` | `llm_agent.go:192` | observe |

- Semantics: ADK-Go "**first non-nil result wins / early-exit**" — the loop already does this
  for the critic and dedup gates.
- **Authoring (both modes):**
  - *In-process Go* — for first-party, mutating, hot-path hooks. Compiled in; fastest; fully
    governed. A `Hook` interface (`Event()/Match()/Run(...)`) registered at the composition
    root like tools.
  - *Out-of-process command program* — Codex shape: an external program per event, JSON on
    stdin, decision via stdout-JSON/exit-code; can `allow`/`deny`/`rewrite` (host applies the
    mutation in-process). For user/agent-authored hooks with no recompile. Behind a
    **trust-hash gate** (don't auto-run unvetted commands). Restricted to observe/approve-deny
    + bounded rewrite; never the sub-millisecond mutating path.
- **Governance composition (mandatory):** hooks run *alongside* the existing gates, never
  bypassing them — budget (`:215`) and dedup (`:419`) still fire; a `BeforeTool` rewrite **must
  re-emit the ToolInvocation Event** so the `tool_invocations` audit ledger records the rewritten
  args, not the originals (Risk #2).

### Slice 2 — Bundle manifest + installer

- `aura.plugin.json` schema + loader/validator (parse-only, no code execution at install).
- `aura plugins {add <source>|list|inspect <name>|enable|disable|remove}` — hand-rolled
  `switch` (`cmd/aura/main.go` adds `case "plugins"`; new `runPlugins` dispatcher).
- `add` resolves source (local path, pinned git ref, or built-in recipe), copies/links into
  `~/.aura/plugins/<name>/`, then **fans out**: `mcp[]`→`mcpInstall`, `skills[]`→skills
  `Writer`, `hooks[]`→hook-register. Idempotent, file-based.
- Every install/enable/disable/remove writes one row to a **new append-only `plugins_audit`
  table (migration 0016)**, mirroring `skill_audit` (0010): actor, plugin, action,
  content_hash, approval_source.
- `inspect` works without activating anything (manifest is parse-only).

### Slice 3 — Self-install loop (no ceremony)

- Aura emits + installs an `aura.plugin.json` mid-conversation through the same installer,
  governed by **`capability_grants`** — currently *built but completely unwired*
  (`internal/identity/store.go:128`, `'*'` wildcard seeded on `local`). This gives it its job:
  replace per-call approval with the "no ceremony for granted capabilities" doctrine.
- Runtime tool additions **rebuild the registry per turn** — the Runner already constructs a
  fresh `LlmAgent` each turn, so this respects the immutable-for-a-run `tools.Registry` (which
  *panics* on duplicate `Register`, `spec.go:113`) without hot-mutating a live registry
  (Risk #1).

## Deferred surfaces (explicitly out of scope)

- **Providers** — `llm.Client` is clean but the provider is a hardcoded string over one
  `openai_compat` adapter (`internal/llm/config.go`). Manifest-declared providers need an
  adapter-table-over-one-adapter (OpenHuman pattern) — the largest lift, least-frequent need.
  Future slice; not blocking "easy extend."
- **Channels** — `channels.Channel` is a clean in-process seam, but inbound channels are native
  Go (the agent is invoked by a direct `turnDriver` call, not a bus). The manifest *may* later
  declare channel adapters, but new channels remain a Go-code surface, not a bundle install.

## Persistence

- New migration **0016 `plugins_audit`** — append-only ledger (UPDATE/DELETE rejected by
  trigger; `aura_app` SELECT+INSERT only), columns mirroring `skill_audit`.
- Plugin config stays **file-based**: `~/.aura/plugins/<name>/` (installed tree) + the existing
  `~/.aura/mcp/servers.json` (MCP) + skills dir + a hooks dir. No MCP/plugin config tables
  beyond the audit ledger.

## Governance composition (the load-bearing invariant)

| Existing gate | Stays where | Hook interaction |
|---|---|---|
| budget step (`:215`) | before model | unchanged; hooks can't skip it |
| dedup (`:419`) | before tool batch | `BeforeTool` composes (both can veto) |
| `ask_user` pause (`:362`) | before dispatch | command-hook `deny` can route through it |
| `tool_invocations` audit | after result (Runner) | rewriting hooks **must re-emit** the Event |
| `capability_grants` | unwired today | **wired by Slice 3** as the self-install gate |

## Top risks + mitigations

1. **Registry immutability vs runtime add** → rebuild registry per turn (Runner already does).
2. **Audit desync on hook rewrite** → re-emit the ToolInvocation Event on any `BeforeTool`
   arg-rewrite; test asserts ledger == rewritten args.
3. **Command-hook = arbitrary code execution** → trust-hash gate + `--ignore-scripts`-style
   default-deny; command hooks observe/approve-deny only, never the mutating hot path.

## Testing strategy

- Unit: manifest parse/validate, hook first-non-nil-wins ordering, registry rebuild idempotence.
- Integration: install a fixture bundle → assert MCP server in `servers.json`, skill in loader,
  hook fired at the right loop point; audit row written.
- Governance: `BeforeTool` rewrite → assert `tool_invocations` records rewritten args; budget
  + dedup still fire; command-hook trust-hash deny path.
- E2E: a fixture `aura.plugin.json` end-to-end through `aura plugins add` then exercised in a
  live turn; self-install loop gated by `capability_grants`.

## PRD impact / next steps

Larger than Slice 7 (Skills). Per project convention (PRD-first), this needs a **PRD amendment**
defining the Plugins capability, then **three GSD phases** (one per slice, hooks first). The
brainstorming flow's next step is to turn Slice 1 (Hooks) into a plan via the project's
`/gsd-spec-phase` → `/gsd-plan-phase` path, not generic writing-plans.

## Self-review

- No placeholders; every seam is cited to a real file:line from the 2026-06-14 seam map.
- Internally consistent: the manifest keys, the installer fan-out, the hook insertion points,
  and the governance table all reference the same existing machinery.
- Scope is bounded by the three locked decisions (name, both-hook-modes, providers deferred);
  providers/channels explicitly carved out.
- Ambiguity resolved: "unified" = the three *audiences*, delivered as three *sequenced slices*,
  not all five *surfaces* at once.
