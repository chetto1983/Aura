# Legacy Surfaces Audit: Aura — 2026-05-17

Read-only audit by subagent. Identifies dual-path code (old + new coexist) and
documentation that contradicts the actual codebase shape.

## Verified findings

### 1. CLAUDE.md package paths are stale

CLAUDE.md references packages that don't exist:
| Claim in CLAUDE.md | Reality |
|---|---|
| `internal/setup` | Does not exist. Setup wizard lives in `internal/api/setup_server.go` (loopback HTTP server) + `cfg.IsBootstrapped()` gate. |
| `internal/settings` | Does not exist. Settings overlay logic is in `internal/config/applier.go`; the catalog is in `internal/api/settings.go`. |
| `internal/agentloop` | Does not exist. Hub-side loop wrapper is `internal/chat/agentloop.go`. |
| `internal/agentruntime` | Does not exist. Agent loop is `internal/agent/loop.go`; task runner is `internal/agent/runtask.go`. |
| `internal/health` | Does not exist. HTTP server is built inline in `cmd/aura/app.go`. |
| `internal/telegram/streaming.go` | Does not exist. Live in `internal/channels/telegram/outbound.go`. |

**Status:** Fixed locally in `CLAUDE.md` 2026-05-17. CLAUDE.md is gitignored so
the correction is not committed; future Claude Code sessions on this machine
see the corrected file.

### 2. Audit's "fts is dead" claim is wrong

The audit recommended killing `cfg.ToolSearchBackend == "fts"` as a dead path.
This is incorrect: `ToolVectorIndex.NewToolVectorIndex` explicitly comments
"For fts backend, qclient stays nil — all Qdrant methods guard on
`cfg.Backend == "fts"`" — meaning fts IS the no-Qdrant fallback. Lex search
still works when Qdrant is unavailable. Do not remove.

### 3. Genuine dual-paths still alive (kept for now)

| Dual-path | Old | New | Status |
|---|---|---|---|
| Telegram routing | `internal/telegram/` (transport + session) | `internal/channels/telegram/` (chat-Hub adapter) | Both load-bearing — new wraps old, not replaces |
| Agent dispatch | `agent.RunTask` (background) | `chat.Hub → agent.Run` (interactive) | Isolated — RunTask lives behind 3 adapters in `cmd/aura/adapters.go` |
| Web chat | (replaced) | `newHubBackedWebChatService` | Old direct path already excised; no cleanup needed |
| Conversation archive | `CONV_ARCHIVE_ENABLED=false` | always-on archive | Feature flag, reachable but degraded |

None of these are urgent. Telegram dual-layer is an intentional transport/adapter
split; the RunTask adapters were just cleanly extracted to `cmd/aura/adapters.go`
(commit `1534e88e`) so the boundary is now explicit.

### 4. Subagent cross-hub gap (deferred from memory)

`subagentBridgeAdapter` (in `cmd/aura/adapters.go`) wires `swarm.HubBridge`,
which dispatches to the swarm hub (`cronHub`). It's registered in the main
chat hub's tool registry — so tools in one hub spawn work in another. Parent
run ID tracking works via `metadata["parent_run_id"]`, but the two hubs are
not symmetrically scoped (different tool allowlists, lifecycles).

Recommendation: Phase 8 should either merge `cronHub` into main hub with a
channel selector, OR document the boundary contract (which capabilities a
swarm child sees vs. its parent). Until then, the gap is benign because
swarm children fall back to direct tools when the bridge denies them.

## What changed today as a result of this audit

- `CLAUDE.md` package paths corrected locally (gitignored, not committed)
- `cmd/aura/app_adapters` extraction shipped (`1534e88e`) — makes the
  RunTask-vs-Hub boundary explicit in the file structure

## What's deferred to a future cycle

- Phase 8 cross-hub unification (subagent dispatch architectural gap)
- `internal/telegram/` ↔ `internal/channels/telegram/` boundary doc

---

Audit produced 2026-05-17. Verified against codebase by main programmer; the
"fts is dead" recommendation was rejected as a false-positive.
