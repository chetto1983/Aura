# MCP Survey — Knowledge & Notes (2026-05-20)

Domain: tools that let an LLM read/write/search local notes, vaults, and knowledge graphs in a **fully self-hosted** way. Survey covers Obsidian, Logseq, Trilium, Joplin, Anytype, BookStack, plain-markdown directories, and Anthropic's official knowledge-graph memory server. SaaS-only options (Notion / Evernote / Roam) excluded per Aura constraint.

Scoring is per the rubric in the brief: Maturity, Self-hosted purity (0/1), Configurability (0–3), Footprint (green/yellow/red), Write capability, Overlap with Aura native wiki tools. Already-surveyed `MarimerLLC/calendar-mcp` and `huhabla/calculator-mcp` are intentionally excluded.

---

## cyanheads/obsidian-mcp-server
- **URL**: https://github.com/cyanheads/obsidian-mcp-server
- **Language / runtime**: TypeScript (Node 24+ or Bun)
- **License**: Apache-2.0
- **Last commit**: N/A in fetch (active — v latest references current MCP spec)
- **Stars**: ~539
- **Transport**: stdio + Streamable HTTP
- **Tool surface**: 14 tools (`obsidian_get_note`, `obsidian_search_notes`, `obsidian_write_note`, `obsidian_manage_frontmatter`, `obsidian_execute_command`, …)
- **Self-hosted purity**: 1 — no SaaS; but requires Obsidian app running locally with Local REST API plugin v4+
- **Configurability**: 3 — pure env-vars with `.env.example` (`OBSIDIAN_API_KEY`, `OBSIDIAN_BASE_URL`, read/write path allowlists, transport, port). Maps 1:1 to dashboard form fields.
- **Footprint**: yellow — Node sidecar; binary path + Obsidian.app coupling on the host
- **Write capability**: full (create/append/patch/delete + UI commands) — needs capability gate
- **Overlap with Aura native**: HIGH-VALUE complement. Aura's wiki is its own markdown vault; this would expose a *different* user-owned Obsidian vault. Read-bridge use case (pull Obsidian notes into Aura context) is the win.
- **Verdict**: **CONDITIONAL** — adopt only if a user runs Obsidian Desktop with the REST API plugin; otherwise prefer the filesystem-only `lstpsche` variant below.

## lstpsche/obsidian-mcp
- **URL**: https://github.com/lstpsche/obsidian-mcp
- **Language / runtime**: Rust (single static binary)
- **License**: MIT
- **Last commit**: 2026-05-15 (v2.0.0)
- **Stars**: ~9 (very new)
- **Transport**: stdio + Streamable HTTP
- **Tool surface**: 18 tools — CRUD, semantic/text/regex search, wikilinks, backlinks, frontmatter, periodic notes
- **Self-hosted purity**: 1 — operates directly on vault files, **no Obsidian app, no plugin, no REST API** required
- **Configurability**: 3 — env-vars (`OBSIDIAN_VAULT_PATH`, transport, port) + CLI args; trivial to expose as dashboard form
- **Footprint**: green — single Rust binary (~few MB), optional ONNX embedder adds ~60 MB. CPU-bound, fits the mini-PC budget.
- **Write capability**: full (create/write/insert/patch/delete/move) — needs gate
- **Overlap with Aura native**: minimal — Aura wiki and an Obsidian vault are *different* knowledge stores. Backlinks/wikilinks tools are similar in shape to Aura's `[[wiki-links]]` semantics, so this is a natural extension surface.
- **Verdict**: **ADOPT** (when stable) — best architectural fit: stdio, static binary, no plugin coupling, filesystem ground-truth, fully configurable. Star count low → watch maturity but ship it as opt-in.

## perfectra1n/triliumnext-mcp
- **URL**: https://github.com/perfectra1n/triliumnext-mcp
- **Language / runtime**: TypeScript (Node)
- **License**: MIT
- **Last commit**: 2026-05-12
- **Stars**: ~36
- **Transport**: stdio (default) + HTTP/SSE + Streamable HTTP
- **Tool surface**: 19 consolidated tools — `search_notes`, `create_note`, `write_note`, `get_note`, plus attachments and revisions
- **Self-hosted purity**: 1 — talks to your TriliumNext instance via ETAPI, no SaaS
- **Configurability**: 3 — three-tier precedence (CLI args / env / JSON config at `~/.trilium-mcp.json`); maps cleanly to a dashboard card
- **Footprint**: yellow — Node sidecar + assumes TriliumNext server running (the user already pays that cost if they use Trilium)
- **Write capability**: full + attachments/revisions — needs gate
- **Overlap with Aura native**: medium — Trilium is a tree-of-notes model, *different* shape from Aura's flat markdown wiki. Complements rather than duplicates.
- **Verdict**: **CONDITIONAL** — best Trilium MCP today (current commit, broad transport support, JSON config). Ship as opt-in when a user runs TriliumNext.

## alondmnt/joplin-mcp
- **URL**: https://github.com/alondmnt/joplin-mcp
- **Language / runtime**: Python (joppy + FastMCP)
- **License**: MIT
- **Last commit**: 2026-05-16 (v0.8.0)
- **Stars**: ~119
- **Transport**: stdio (default) + HTTP / SSE / Streamable HTTP
- **Tool surface**: 26 tools — `find_notes`, `create_note`, `update_note`, `delete_note`, plus notebooks/tags
- **Self-hosted purity**: 1 — talks to local Joplin via Web Clipper API (localhost:41184)
- **Configurability**: 3 — JSON config (`joplin-mcp.json`) + env-vars (`JOPLIN_TOKEN/HOST/PORT`) + per-tool gates (`JOPLIN_TOOL_<NAME>=true|false`). Excellent dashboard fit.
- **Footprint**: yellow — Python sidecar + requires Joplin Desktop with Web Clipper enabled (heavy if user doesn't already run it)
- **Write capability**: full — needs gate
- **Overlap with Aura native**: low — Joplin is a separate notebook universe; pure complement
- **Verdict**: **CONDITIONAL** — best Joplin MCP; per-tool env gate is a configurability win Aura should mimic. Adopt for Joplin users only.

## anyproto/anytype-mcp
- **URL**: https://github.com/anyproto/anytype-mcp
- **Language / runtime**: TypeScript (Node)
- **License**: MIT
- **Last commit**: 2026-05-17 (v1.2.7)
- **Stars**: ~421
- **Transport**: HTTP only (talks to local Anytype API at 127.0.0.1:31009)
- **Tool surface**: ~5 tool families (search, spaces, objects, properties, types) auto-generated from Anytype's OpenAPI spec — likely 20+ MCP tools in practice
- **Self-hosted purity**: 1 — pairs with self-hosted Anytype (official `anyproto/` org)
- **Configurability**: 2 — env-vars (`OPENAPI_MCP_HEADERS` containing Bearer + version, `ANYTYPE_API_BASE_URL`); JSON-headers field is ugly to expose as a dashboard form
- **Footprint**: red — requires running Anytype (Electron desktop or self-hosted any-sync stack — non-trivial server kit)
- **Write capability**: full — needs gate
- **Overlap with Aura native**: low (Anytype is encrypted-objects model, very different)
- **Verdict**: **SKIP** for default Aura — too much infra to demand of a typical Aura user; revisit if Anytype self-host becomes a stated user requirement.

## modelcontextprotocol/server-memory (official KG memory)
- **URL**: https://github.com/modelcontextprotocol/servers/tree/main/src/memory
- **Language / runtime**: TypeScript (Node)
- **License**: MIT
- **Last commit**: N/A (Anthropic monorepo, actively maintained)
- **Stars**: ~80k+ (parent repo)
- **Transport**: stdio
- **Tool surface**: 9 tools — `create_entities`, `create_relations`, `add_observations`, `search_nodes`, `read_graph`, `open_nodes`, plus deletes
- **Self-hosted purity**: 1 — pure local JSONL file (`memory.jsonl`)
- **Configurability**: 2 — single env-var `MEMORY_FILE_PATH`. Trivial card, but no scoping/multi-graph support out of the box.
- **Footprint**: green — Node, but no external service dependencies
- **Write capability**: full (the whole point) — needs gate
- **Overlap with Aura native**: **HIGH** — Aura's wiki + `[[wiki-links]]` + memory tools already cover entity/relation/observation semantics with markdown ground truth. Bolting on a JSONL knowledge graph would create a *second* memory source the LLM has to choose between — exactly the "graph memory IS the project core" anti-pattern in memory.
- **Verdict**: **SKIP** — duplicates Aura's existing graph-memory primitive with a weaker substrate (JSONL vs git-tracked wiki). Reference implementation only.

---

## Top 3 picks for Aura

1. **lstpsche/obsidian-mcp** (Rust, stdio, no Obsidian-app dependency) — cleanest architectural fit; static binary, filesystem ground-truth, env-config maps 1:1 to a dashboard card. Wait one release cycle for star/maturity ramp, then ship as opt-in.
2. **perfectra1n/triliumnext-mcp** — most modern Trilium MCP (May 2026 commit, JSON-config + env + CLI tiers, 19 consolidated tools across stdio + HTTP). Conditional on a user running TriliumNext.
3. **alondmnt/joplin-mcp** — 26 tools with **per-tool env gates** (`JOPLIN_TOOL_<NAME>=true|false`) — a configurability pattern Aura's MCP-UI framework should adopt regardless of whether the server itself ships.

## What's missing

- **HedgeDoc / Outline MCP**: no credible self-hosted MCP found for either. Both have REST APIs — green-field for an Aura contribution if a user demand emerges.
- **Local SQLite-backed notes** (e.g. apps that already store in SQLite without a REST server): no purpose-built MCP exists — most servers assume a sidecar REST API. Aura's own wiki tools already cover this shape.
- **Daily-notes / journal-shaped MCP**: only Obsidian variants have it baked in. Aura could add a tiny "daily notes" tool on top of its own wiki instead of adopting an MCP.
- **Multi-vault / vault-discovery MCP**: every server above is single-vault. If Aura ever supports per-user vaults, this is a gap.
- **A "configurability framework" reference**: only `alondmnt/joplin-mcp` exposes per-tool toggles — Phase-MCP-UI should standardize this pattern across all adopted MCPs, not just inherit whatever each server provides.
