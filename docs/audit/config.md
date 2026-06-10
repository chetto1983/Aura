# Audit: internal/config

**Verdict:** needs-work — one not-wired config knob; all other fields verified consumed.
**Counts:** critical 0 / high 0 / medium 1 / low 0

## Findings

### [MEDIUM][NOT-WIRED] HistoryHardCapTurns is parsed but never consumed

**Location:** `internal/config/config.go:53,224`
**Confidence:** high

**Detail:**
`Config.HistoryHardCapTurns` is declared (line 53), given a default of 50 via
`envIntDefault("AURA_HISTORY_HARD_CAP_TURNS", 50)` (line 224), and has a comment
describing it as "L2.5 picobot hard rolling buffer cap". A Grep across the entire
repo (`type:go`) finds no reference to `HistoryHardCapTurns` outside this file —
it is written once in `loadBase()` and never read by any consumer.

The L2.5 context ladder in `internal/conversations/context.go` computes its hard cap
from `ContextConfig` (a formula over `ContextWindow` and `MaxOutputTokens`), not from
a turn count. `runner.Deps` does not accept a `HistoryHardCapTurns` field. The `chat.go`
composition root wires `ContextToolEvictAfterTurns` (line 181) but omits
`HistoryHardCapTurns`. There is no wiring path from the env var to any runtime behavior.

**Risk:** An operator setting `AURA_HISTORY_HARD_CAP_TURNS` will see no effect. The
intent of limiting rolling history by turn count is silently unimplemented.

**Suggested fix:**
Either (a) wire the field into the conversation context config (add a `HardCapTurns`
field to `conversations.ContextConfig` and use it as the L2.5 override), (b) remove
the field and env var entirely if the token-count formula subsumes it, or (c) leave
the field as a pre-wiring placeholder and add a `//nolint:unused` annotation with a
tracking comment.

---

## Clean

The following areas were audited and found clean:

- **All other Config fields** (`MCPServersErr`, `MCPServers`, `MCPPolicies`,
  `RunDir`, `ToolPreviewCap`, `OtelExporter`, `OtelEndpoint`,
  `ConversationTurnCapBytes`, `ContextToolEvictAfterTurns`,
  `RunDirWarnThresholdBytes`, all web/swarm/skills/AGUI/setup/multimodal knobs):
  every field is consumed by at least one production code path; verified by Grep.

- **`composeDSN`**: the `RawPath` field is set intentionally to
  `"/" + url.PathEscape(dbname)` so that a slash inside `dbname` is encoded as
  `%2F` rather than treated as a path separator. Verified with Go runtime: a
  dbname like `"aura/db"` produces `postgres://.../aura%2Fdb` with `RawPath` set
  and `postgres://.../aura/db` (ambiguous) without it. The behavior is correct.

- **`loadMCPServers` deferred-error pattern**: `MCPServersErr` is stored in
  `Config` and consumed by `cmd/aura/main.go:145`, `cmd/aura/mcp_tools.go:58,67`,
  and `internal/eval/harness_swarm_e2e_test.go:164`. The deferred-error design
  (load succeeds, error surfaces at first use) is intentional per the codebase
  convention.

- **`parseMCPServersJSON`**: the two-pass unmarshal (wrapped `mcpServers` key then
  direct map) is correct; both formats are tested by
  `TestLoad_MCPServersJSON` and `TestLoad_MCPManagedConfigAndEnvOverride`.

- **`envIntDefault` / `envBoolDefault` / `envSliceDefault`**: all three helpers
  follow the "silent fallback on malformed input" contract documented in comments
  and enforced by tests. No parsing error is silently swallowed in a way that
  matters operationally.

- **No goroutines, channels, or shared mutable state**: the package is purely
  functional at load time; no race conditions possible.

- **No resource leaks**: no files, network connections, or OS handles are opened
  here. `godotenv.Load()` opens `.env` internally and closes it; error is
  explicitly discarded per documented best-effort contract.
