# Aura Plugin/Extension Architecture — Research & Recommendation

Date: 2026-06-14
Status: Research complete; recommendation pending product decision (see Open Question)
Evaluates: `docs/superpowers/specs/2026-06-02-openclaw-plugin-compatibility-design.md`

## Question

What is the **minimal industrial shape** for Aura's plugin/extension system — given Aura
is a Go-based, MCP-centric, local-first agent runtime on a shared 16-core mini-PC whose Go
host must keep the agent loop, governance, audit, HITL approval, and budget? Specifically:
is the approved-but-unbuilt **OpenClaw Node-sidecar compatibility host** the right shape, or
over-engineering?

Method: 5 codebase surveys of curated `d:/tmp` reference runtimes (nanobot, picobot, the
everything-as-MCP cluster, adk-go/codex/elysia, openhuman, a codex deep-dive) + an
adversarially-verified online research pass (19 sources, 91 claims, 25 verified).

## Headline verdict

**Do not build the OpenClaw Node-sidecar compatibility host.** Every independent evidence
stream — five production agent runtimes and the verified online research — converges on the
same shape: tools/providers/channels/hooks belong as **in-process Go extension seams**
(most of which Aura already has), with **MCP as the out-of-process adapter for tool-shaped
capabilities** (which Aura also already has). The Node ESM sidecar is justified **only if
running existing OpenClaw plugin *binaries* unmodified is itself a hard product requirement** —
not merely wanting the *surfaces* OpenClaw plugins expose. Those surfaces are all reachable
without it.

## The five extension surfaces, mapped to evidence

| Surface | Can MCP express it? | Industrial answer | Aura status |
|---|---|---|---|
| **Tools** | ✅ Yes (the one thing MCP was built for) | In-process Go `Tool` interface + MCP as one Toolset adapter feeding the *same* seam | **Has both** (native tools + `aura mcp`) |
| **Providers** (LLM the host routes to) | ❌ No (sampling is the *inverse* — server borrows host's model) | In-process provider interface + ONE OpenAI-compatible adapter + a config table | **Has** `llm.Client`; add config-table for the long tail |
| **Channels** (inbound msg drives a turn) | ⚠️ Outbound only (=a tool); inbound genuinely missing | In-process `Channel` interface whose `listen()` drives the agent via an in-process bus | **Has** `internal/channels/` (Telegram) |
| **Hooks** (before/after model/tool, lifecycle, rewrite/veto) | ❌ No (host-initiated request/response only) | In-process Go callbacks for *mutating* lifecycle + optional out-of-process command-hooks for observe/approve-deny | **Missing** — the one real new surface |
| **Background services** | ⚠️ Runs, but MCP doesn't supervise it | compose/process supervision at the deployment layer | **Has** `aura mcp` + compose |

### Why MCP stops at tools (verified)

- **Online (3-0 confirmed):** "MCP extensions are optional, opt-in, and **not required for
  protocol conformance**." Standard MCP reliably covers only tools/resources/prompts; betting
  providers/channels/hooks on MCP *extensions* carries conformance/adoption risk.
  (`modelcontextprotocol.io/extensions/overview`)
- **Local (everything-as-MCP survey, 6 repos):** all six use MCP as a **tools-only** protocol;
  resource/prompt/sampling/notification primitives are advertised at most as **dead stubs**
  (`mcp-unix-shell/main.go:87` disables resources; `mail-mcp/src/index.ts:1404-1416` returns
  empty arrays). The decisive case is **channels**: `whatsapp-mcp`'s Go bridge genuinely
  *receives* inbound messages (`whatsapp-bridge/main.go:838-854`) but only **writes them to
  SQLite** — nothing wakes the agent; the agent "receives" a message solely by *polling*
  `list_messages`. Inbound-that-drives-a-turn is outside MCP's request/response grain.

### Why channels & hooks must be in-process (verified, strong)

- **OpenHuman** (Rust, ~200k LOC, 18 channels, an enthusiastic MCP client *and* server) still
  built a dedicated in-process `Channel` trait whose `listen()` drives the agent over an
  internal `agent.run_turn` bus (`channels/traits.rs:59`, `channels/runtime/dispatch.rs:878`) —
  it did **not** model channels as MCP. Its hooks are in-process and both **veto** (StopHook)
  and **rewrite** (TokenJuice compacts tool output before the model sees it).
- **ADK-Go**: hooks are an in-process `PluginManager` with "first non-nil result wins,
  early-exit" semantics (`internal/plugininternal/plugin_manager.go:170-270`); `BeforeTool` can
  rewrite args or veto, `BeforeModel` can substitute a response. HITL is an in-process
  `ConfirmationProvider` (`tool/tool.go:134-149`).
- **Codex**: keeps *mutating* lifecycle contributors in-process (`ext/extension-api`,
  `core/src/tools/lifecycle.rs`) while exposing **out-of-process command-hooks** (10
  Claude-Code-style events, JSON on stdin) that can deny **and** rewrite (`PreToolUse`
  `updatedInput`) — but the host applies the mutation in-process (`core/src/hook_runtime.rs`).

This is the precise split Aura should adopt for hooks: **in-process Go callbacks for the
mutating hot path; optional out-of-process command-programs for observe/approve-deny policy**
(latency + KV-cache-stability reasons make per-call sidecar round-trips for mutating hooks a
hazard).

## The "MCP is just another tool" pattern (the dominant shape)

Both Go/Rust runtimes unify native and MCP tools behind **one registry**, after which the
model is kind-blind:

- **ADK-Go** wraps an MCP server as just another `tool.Toolset` behind the identical
  `tool.Tool` seam (`tool/mcptoolset/set.go:49-129`).
- **Codex** registers MCP tools into the same `ToolRegistry`/`ToolExecutor` dispatch as native
  handlers (`core/src/tools/spec_plan.rs:789-813`); filtering is on `exposure.is_direct()`, not
  native-vs-MCP. Note Codex's `tool_search` is **lexical BM25** — **Aura's embedding `semindex`
  is ahead here**; keep embeddings.

Aura already implements this pattern. No new substrate is needed for tools.

## Distribution / "marketplace" UX (if ever wanted)

Codex's `core-plugins` crate is the reference for a *declarative* plugin-bundle UX with
**zero dynamic native-code ABI**: a `.codex-plugin/plugin.json` manifest bundles MCP-server
configs + skill markdown + connector IDs + hook commands, loaded by reading files from disk
(no `dlopen`/wasm/dylib anywhere in the tree). If Aura later wants a "plugin install" surface,
copy this shape — it's a manifest that *composes existing primitives* (MCP servers + skills +
hooks), not a code-loading host. Aura already has the primitives (`aura mcp`, `aura skills`).

## Where go-plugin and WASM actually fit

- **HashiCorp go-plugin** (verified 3-0 / 2-0): out-of-process subprocess over net/rpc **or
  gRPC**, crash isolation, **bidirectional callbacks** (host passes an interface, plugin calls
  back — so hooks *are* expressible), and **gRPC polyglot** (any language, incl. Node/JS,
  **without** a bespoke Node sidecar). Caveats: **one OS process per plugin** (resource cost on
  the mini-PC), it does **not auto-restart** crashed plugins, and a refuted claim (0-3)
  corrected the record — gRPC plugins are **out-of-process, not "in-process-equivalent"**.
  → Reach for go-plugin **only** if a third party must ship a *crash-isolated, callback-driven,
  polyglot* extension. For Aura's own surfaces, compiled-in Go seams are leaner.
- **go-plugin is NOT a sandbox** (verified 3-0): process-boundary privilege limitation, not
  OS confinement; it assumes vetted plugins (cf. CVE-2025-6000).
- **WASM (Extism/wazero/WASI)** is the genuinely stronger isolation tier for **untrusted**
  third-party code (zero ambient authority, manifest-gated FS/net). **But every WASM, VS Code,
  and OpenClaw-manifest claim in the online pass was UNVERIFIED (0-0, rate-limited)** — treat
  as design leads, not evidence. Known limitation to confirm: wazero leaves WASI sockets
  unimplemented → no plugin network I/O (would block network-bound service/channel plugins).
- **Untrusted-code isolation** otherwise: reserve the existing out-of-process + OS-sandbox
  boundary (Codex `SandboxPolicy` / OpenHuman `cwd_jail`: landlock/bwrap/seatbelt) — Aura
  already has `sandbox-agent` for this. Note both Codex and OpenHuman run **MCP subprocesses
  unsandboxed**, governed by approval mode only (treated as trusted host config).

## Recommended minimal shape for Aura

1. **Tools** — keep as-is: native Go `Tool` + MCP-as-Toolset (`aura mcp`). Done.
2. **Providers** — keep `llm.Client`; add a single OpenAI-compatible adapter + a
   `[[providers]]` config table for the long tail (OpenHuman pattern). Not a plugin surface.
3. **Channels** — keep the in-process `internal/channels/` framework; new channels =
   in-process Go adapters whose inbound drives a turn. (WhatsApp's MCP bridge is fine as a
   *tool* surface for send/search; a true inbound WhatsApp *channel* is an in-process adapter.)
4. **Hooks** *(the only real gap)* — add in-process Go lifecycle callbacks
   (before/after model, before/after tool, on-event) with "first non-nil wins / early-exit"
   governance semantics (ADK pattern); optionally add Claude-Code-style **out-of-process
   command-hooks** for observe/approve-deny policy behind a trust-hash gate (Codex pattern).
5. **Background services** — supervise via compose/process layer (already have it).
6. **Distribution** *(only if wanted)* — a declarative bundle manifest composing
   MCP+skills+hooks (Codex `core-plugins` shape). No code-loading host.
7. **Untrusted code** *(only if a real requirement)* — out-of-process + OS-sandbox
   (`sandbox-agent`), or a WASM/Extism tier (verify the unverified claims first).

This delivers the OpenClaw spec's *capabilities* using Aura's existing seams + one new
in-process hook layer — **no Node sidecar, no `internal/plugins/{manifest,policy,host,registry}`,
no `plugin-host/`**.

## Open question (product decision — yours to make)

**Does Aura need OpenClaw plugin *binary/manifest compatibility* (run existing OpenClaw
plugins unmodified), or only the *capabilities* those plugins provide?**

- If **only the capabilities** → the recommendation above; the OpenClaw spec is shelved as
  over-engineering ("atomic bomb").
- If **binary compatibility with the OpenClaw ecosystem is a hard product goal** → the Node
  ESM sidecar becomes the *one* justification, because that's the only way to import OpenClaw's
  actual JS plugin modules. Even then, scope it to the narrowest viable slice and keep all
  governance in the Go host.

## Evidence strength / caveats

- **High confidence:** the in-process-seams + MCP-for-tools conclusion (corroborated by 5
  independent production codebases) and the go-plugin/MCP-extension-optionality facts
  (verified primary sources, unanimous votes).
- **Low confidence (unverified, rate-limited in the online pass):** all WASM/Extism/wazero, VS
  Code extension-host, and OpenClaw-manifest specifics — directional design leads only.
- The central "is the Node sidecar worth it" question is answered by *inference* (the surfaces
  are coverable without it), not by adversarial analysis of OpenClaw's own internals.

## Sources

Online (verified primary): `modelcontextprotocol.io/extensions/overview`,
`github.com/hashicorp/go-plugin` (+ README, pkg.go.dev). Online (unverified leads):
`pkg.go.dev/github.com/extism/go-sdk`, `github.com/knqyf263/go-plugin`,
`code.visualstudio.com/api/advanced-topics/extension-host`, `docs.openclaw.ai/plugins/*`.
Local: `d:/tmp/{nanobot,picobot,csmcp,agent-infra-sandbox,mcp-unix-shell,calculator-mcp-server,
mail-mcp,whatsapp-mcp,adk-go-study,codex,elysia,openhuman}` (file:line evidence above).
