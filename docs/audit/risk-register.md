# Risk Register

| ID | Title | Severity | Probability | Impact | Affected area | Mitigation | Status |
|---|---|---:|---:|---:|---|---|---|
| R-001 | Full-host shell/filesystem authority | P1 | High | Critical | Tool execution, security | Add ToolGateway, sandboxing, workspace fences, production deny-by-default | Open |
| R-002 | Sample env disables destructive shell approval | P1 | High | High | Configuration, shell safety | Remove empty override or change empty parsing to defaults | Open |
| R-003 | Terminal response can run after mutating siblings | P1 | Medium | High | Agent loop correctness | Make `text_response` exclusive with mutating tools | Open |
| R-004 | Resume claim and answer append are not atomic | P1 | Medium | High | HITL persistence | Use one transaction or idempotent resume ledger for single and batch answers | Open |
| R-005 | DB-stored sidecar path can read arbitrary local file | P1 | Low-Medium | High | Conversation persistence, security | Reconstruct and fence sidecar paths | Open |
| R-006 | Command hooks fail open by default | P1 | Medium | High | Shell/MCP security | Default fail-closed or require explicit policy | Open |
| R-007 | Static object-store credentials | P1 | Medium | High | Secrets, artifacts | Reject defaults outside dev; generate local secrets | Open |
| R-008 | HTTP listener failure hidden by healthcheck | P1 | Medium | High | Availability | Wire listener state into readiness; healthcheck `/readyz` | Open |
| R-009 | Remote MCP empty trust becomes runnable | P2 | Medium | Medium-High | MCP governance | Require explicit trust for all remote transports | Open |
| R-010 | Mutating tool ledger best-effort | P2 | Medium | Medium-High | Auditability | Durable pre-execution ledger reservation | Open |
| R-011 | Background shell jobs lack TTL | P2 | Medium | Medium | Runtime resources | Add TTL, owner/session ID, job metrics | Open |
| R-012 | `fs_write` non-atomic writes | P2 | Medium | Medium | Filesystem correctness | Use atomic write helper | Open |
| R-013 | Outside-workspace send-file approval not wired | P2 | Medium | Medium | HITL/tool UX | Implement resume hook or remove advertised flow | Open |
| R-014 | Legacy MCP env bypasses managed trust metadata | P2 | Medium | Medium | MCP governance | Deprecate or production-gate legacy path | Open |
| R-015 | CI raw `./...` package discovery drift | P2 | Medium | Medium | CI reliability | Reuse `scripts/go_packages.sh` everywhere | Open |
| R-016 | Silent env parse fallback | P2 | Medium | Medium | Configuration | Add diagnostics and production fail-fast | Open |
| R-017 | Single-replica Garage topology | P2 | Medium | Medium | Object storage durability | Production validation and topology docs | Open |
| R-018 | Missing load/chaos/security gates | P2 | High | Medium | Release quality | Add capability evaluation and chaos suite | Open |
| R-019 | Full reasoning trace retention risk | P3 | Low-Medium | Medium | Privacy/observability | Retention/encryption policy and warnings | Open |
| R-020 | Permissive CORS production misconfig | P3 | Low-Medium | Medium | Web security | Profile-gated CORS allowlists | Open |
| R-021 | Mixed URL+command MCP transport trust bypass | P1 | Medium | High | MCP governance | Reject ambiguous transport definitions; use one canonical transport classifier | Open |
| R-022 | Multi-user Web/API data not identity-scoped | P1 | Medium | High | AG-UI/Web/API auth | Owner-aware stores and API filtering by authenticated principal | Open |
| R-023 | Single resume claim and answer append not atomic | P2 | Medium | Medium-High | HITL persistence | Transactional claim+append or idempotent resume ledger | Open |
| R-024 | Pause flush failure hidden after consumer stops | P2 | Medium | Medium-High | HITL persistence | Persist pause tool-call turn and pause row atomically before exposing pause | Open |
| R-025 | Mutating panic path loses mutating flag | P2 | Low-Medium | Medium-High | Agent loop/tool dispatch | Preserve tool mutating classification in panic recovery | Open |
| R-026 | Background shell IDs predictable and unscoped | P2 | Medium | Medium-High | Shell/background jobs | Random IDs plus session/actor binding for poll/kill | Open |
| R-027 | MCP mount lacks per-server timeout | P2 | Medium | Medium | MCP lifecycle | Bounded mount context and process reap on timeout | Open |
| R-028 | Stdio MCP frames uncapped | P2 | Medium | Medium | MCP lifecycle | Max frame/body size before parse | Open |
| R-029 | MCP shutdown can hang or leak children | P2 | Low-Medium | Medium | MCP lifecycle | Bounded HTTP close and process-tree termination | Open |
| R-030 | Docker MCP network allowlist advisory only | P2 | Medium | Medium-High | MCP sandbox/network | Enforce egress with proxy/firewall or keep network disabled | Open |
| R-031 | CLI MCP mutations bypass audited writer | P2 | Medium | Medium | MCP governance | Route CLI writes through audit+atomic writer | Open |
| R-032 | MCP trust endpoint defaults empty body to trusted_local | P2 | Medium | Medium-High | MCP governance API | Require explicit trust class and reason | Open |
| R-033 | Conversation delete bypasses session eviction | P2 | Medium | Medium | Persistence/tool state | Route deletes through runner lifecycle eviction | Open |
| R-034 | Relative run dir makes sidecars cwd-dependent | P2 | Medium | Medium | Persistence/config | Normalize or reject non-absolute `AURA_RUN_DIR` | Open |
| R-035 | Scheduler SIGTERM cancels in-flight jobs immediately | P2 | Medium | Medium | Scheduler operations | Separate stop-admission from job-work contexts | Open |
| R-036 | Backup runtime exceeds systemd stop budget | P2 | Low-Medium | Medium | Backup/deployment | Align stop timeout with longest handler or atomically promote backups | Open |
