# Risk Register

| ID | Title | Severity | Probability | Impact | Affected Area | Mitigation | Status |
|---|---|---:|---:|---|---|---|---|
| R-001 | Full container/runtime shell/FS access | P1 | High | Container compromise, mounted-volume/secret exposure, destructive writes | Tools/security | Add capability profiles and container-aware sandbox | Open |
| R-002 | Model-authored skill auto-activation | P1 | Medium | Persistent prompt/tool behavior compromise | Skills/security | Gate model-authored skills by default | Open |
| R-003 | Non-atomic `FSWrite` | P1 | Medium | File truncation or corruption | Filesystem tools | Use atomic write helper | Open |
| R-004 | Detached background shells | P1 | Medium | Resource leak, unauthorized polling/killing, lingering side effects | Shell tools | Add owner, TTL, max runtime | Open |
| R-005 | Deadline enforcement depends on caller | P1 | Medium | Hung tool or unbounded execution by direct consumers | Agent loop | Validate context and derive deadlines in `Run` | Open |
| R-006 | Mixed terminal and mutating calls | P1 | Medium | Side effects hidden in final turn | Dispatcher | Enforce terminal exclusivity | Open |
| R-007 | Prompt-directed memory | P2 | Medium | Missed or stale user memory | Memory/context | Deterministic memory middleware | Open |
| R-008 | MCP mutability trust | P2 | Medium | Unsafe retries and missing completion gates | MCP tools | Local policy override | Open |
| R-009 | Best-effort tool ledger | P2 | Medium | Missing forensic records | Observability | Durable ledger outbox | Open |
| R-010 | Full reasoning trace leaks sensitive data | P2 | Medium | PII/secret leakage to disk | Observability/security | Production guard, encryption, retention | Open |
| R-011 | Plaintext direct sidecar writes | P2 | Medium | Secret persistence and crash inconsistency | Persistence | Atomic encrypted sidecars | Open |
| R-012 | Crash after side effect before result | P2 | Medium | Unknown external state, replay risk | Reliability | Tool transaction/idempotency model | Open |
| R-013 | Registry mutable without synchronization | P2 | Low | Race/panic under dynamic tools | Tool registry | Immutable snapshots or locks | Open |
| R-014 | Shell approval patterns incomplete | P2 | High | Policy bypass by command indirection | Shell/security | Parsed command policy engine | Open |
| R-015 | Incomplete dependency health | P2 | Medium | Silent degraded production | Infrastructure | Readiness and dependency matrix | Open |
| R-016 | Silent max parallel env fallback | P3 | Medium | Hidden misconfiguration | Config | Fail-fast env parsing | Open |
| R-017 | Entropy failure panic for nonce | P3 | Low | Avoidable turn failure | Prompt injection mitigation | Non-panic fallback | Open |
| R-018 | Ambiguous FS working directory | P3 | Medium | Writes/reads from unexpected directory | FS tools | Explicit workspace root | Open |
| R-019 | Missing production test lanes | P3 | High | Regressions escape CI | Quality | Add integration/live/chaos/security lanes | Open |
| R-020 | Skill comments contradict behavior | P3 | Medium | Misreviewed security boundary | Maintainability | Update ADR/comments | Open |
