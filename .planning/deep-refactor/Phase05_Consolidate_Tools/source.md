# Phase05 Source Audit

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` §6 Phase 5 | PRD-canonical scope | All 9 gate bullets | Reinterpreting "visibility" as a runtime concept (it's a discovery-only concept; execution authorization is identity.Capability) | read |
| `D:/Aura/internal/agent/tools/registry/definition.go` | Current ToolDefinition contract | Extend with 3 new fields | Replacing RequiredCapability (Phase01B identity work) | read |
| `D:/Aura/internal/agent/tools/registry/registry.go:144,166` | Existing deterministic-order machinery | Reuse sort.Strings pattern | Re-implementing ordering | read |
| `D:/Aura/internal/agent/toolsprovider.go` | AlwaysOnCore canonical seed | Tools with VisibilityTier=always_on must intersect AlwaysOnCore | Diverging two sources of truth | read |
| `D:/Aura/internal/agent/tools/registry/error.go` | Structured error classes | Reuse classifyToolError vocabulary for RiskClass labels where possible | New error taxonomy | read |
| `D:/tmp/nanobot/nanobot/agent/tools/` | Reference: how nanobot tags tools | Steal "destructive" label semantics | Coupling to nanobot's specific schema | pending targeted reread |
| `D:/tmp/cli-printing-press` `--rate-limit` + sync_warning pattern | Reference: risk-class observability | Surface RiskClass in logs (not values, just the class) | Adding a new dashboard surface in Phase 5 | read |
| `D:/Aura/AGENTS.md` Tool privacy rules | Secret-safe logging | Memory-locked invariant — keys only, never values | Logging tool arguments | read |
| `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01B_Identity_Capability_Grants/` | Capability grants schema | RequiredCapability stays the execution gate | Replacing authorization with a visibility check | read |

## Missing Source Questions

- Which tools should default to VisibilityTier=always_on (top of AlwaysOnCore)? Likely: tool_search, request_dashboard_token, ask_user.
- For RiskClass, do we treat web_fetch as read_only (it's GET) or mutation (it can have side effects on remote servers)? Recommend read_only by default; tools that intentionally call mutating endpoints (e.g. webhook senders) flag mutation explicitly.
- For IdempotencyClass, source ingestion (store_source/ingest_source) is idempotent by SHA-256 dedup — confirm via test before tagging.
