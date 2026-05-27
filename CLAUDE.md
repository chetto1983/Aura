# CLAUDE.md

Project guidance for Claude Code (claude.ai/code) on this codebase.

## Project state

Tabula-rasa rewrite, 2026-05-27. Prior implementation at git tag `pre-rewrite-2026-05-27`.

## Four-component scope (nothing else)

1. **Agent loop** (`internal/agent`) — streaming LLM, tool dispatch, MaxSteps.
2. **KV cache** (`internal/llm` + prompt builder) — provider-aware caching, stable-prefix prompts, zero `messages[0]` mutation.
3. **Sandbox** (`internal/sandbox` + sidecar) — Python + shell exec, container-isolated, seccomp + ulimit + net-deny.
4. **Swarm** (`internal/swarm`) — parallel agents, tier model (chat/reasoning/worker), MAX_SPAWN_DEPTH=3, peer-to-peer talk via shared bus + DM-by-ID.

Persistence: Neo4j via `mcp-neo4j-cypher` MCP server (stdio). No native Go adapter.

## Behavioral rules (apply to every change)

- **NEVER SUPPOSE.** Read code before editing. If uncertain about API contract, stop and ask.
- **READ BEFORE EDIT.** Re-read a file you haven't touched in the last 5 messages.
- **3-STRIKE RULE.** Same failing approach max 3 times. On strike 3, stop and ask.
- **NEVER MODIFY TESTS TO MAKE THEM PASS** unless the test itself is broken.
- **SCOPE CONTROL.** Do exactly what was asked. No unrequested features, refactors, or improvements.
- **FOLLOW EXISTING PATTERNS.** Never invent new approaches when codebase patterns exist.
- **NO GOD CLASS.** Never create a file >600 LOC. Refactor on touch.
- **REUSABLE CODE.** Never duplicate; extract a helper.
- **DEEP REFACTOR ON TOUCH.** Every file you edit gets dead-code removal + dupl-folding + LOC ≤600 + comments-updated in the SAME commit.
- **GIT PUSH DISCIPLINE.** Never `git push` (or any remote-mutating command) unless explicitly requested in the current turn. A previous approval does not carry over.
- **NO COMMENTS UNLESS WHY IS NON-OBVIOUS.** Identifier names already explain what. Comments only for hidden constraints, workarounds, or surprising behavior.

## Tool design — deferred-tool pattern (mandatory)

Big tools (long descriptions, complex JSON schema, examples) live in **dedicated files** with a `Deferred = true` flag on the `ToolSpec`. They do NOT appear in the LLM-visible default manifest — only their name + 1-line summary. The model uses the built-in `tool_search` (a hook tool) to fetch the full spec on demand. This protects the cache (no manifest bloat per turn) and scales to N tools without context cost.

Convention:
- Tool implementation: `internal/agent/tools/<name>.go`
- Tool spec metadata constant in the file
- Big tools: `Deferred: true`
- Small tools (e.g. `text_response`, `ask_user`): `Deferred: false`

## Post-edit validation

After every Go file edit:
- `go vet ./...`
- `go build ./...`
- `go test ./internal/<package>/` if tests exist
Fix issues before moving on.

## Commit discipline

- One slice = one commit.
- Atomic. Commit message: imperative subject + body explaining *why*.
- Co-Authored-By trailer per project convention.

## Persistence

Neo4j on the spike compose (`compose.yaml`): port `7687` bolt, `7474` browser. APOC + GDS plugins enabled.

`mcp-neo4j-cypher` is the LLM's interface to the graph. Configure it in `mcp.json` (created on first `aura init`).
