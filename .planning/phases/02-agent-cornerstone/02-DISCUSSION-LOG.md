# Phase 2: Agent Cornerstone - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-29
**Phase:** 02-agent-cornerstone
**Areas discussed:** Public API (interface + structs), Budget/error signalling, Flag/env precedence, Fixtures + canonical-JSON, Ctx/Budget derivation, Author, Branch, Property-testing, Dedup composition, OTel/Event shape
**Method:** 4 interactive discussion rounds + study of `google/adk-go` source + study of curated D:/tmp sources (picobot/nanobot/openhuman) + online research + a 5-agent parallel scout pass requiring ≥95% confidence on the cornerstone.

---

## Round 1 — SPEC-contradiction resolutions + footguns

The user redirected the initial gray-area selection to **"dump google/adk-go and study together"** — so decisions were grounded in upstream source, not guessed.

### Interface (open vs sealed)
| Option | Description | Selected |
|--------|-------------|----------|
| Open interface (diverge from adk) | No unexported sealing method; later phases implement directly | ✓ |
| Sealed like adk | Replicate `internal()` seal; phases use constructors | |

**User's choice:** Open interface. **Notes:** adk-go's `internal() *agent` seal (agent.go:51) contradicts Aura's premise that every later phase implements the interface; adk's own doc signals they're moving toward open.

### Struct visibility + constructor return
| Option | Description | Selected |
|--------|-------------|----------|
| Struct EXPORTED + New→Agent | Exported structs, constructors return interface, compile-assert as-written | ✓ |
| Struct unexported + internal assert | adk-faithful unexported struct, assert in internal test | |

**User's choice:** Exported. **Notes:** Resolves SPEC Req#4 (unexported) vs Acceptance L129 (exported) contradiction in favor of the exported side → PRD-amendment A1.

### Escalate / budget signalling / error slot
| Option | Description | Selected |
|--------|-------------|----------|
| CancelFunc + Event-only + drain nil + ErrBudgetExhausted | Escalate via captured CancelFunc; budget=Event; siblings drain (nil,nil); sentinel for callers | ✓ |
| Sentinel errEscalate internal | escalate as internal error to trigger errgroup cancel | |

**User's choice:** CancelFunc + Event-only. **Notes:** escalate is control-flow, not failure.

### Flag/env precedence
| Option | Description | Selected |
|--------|-------------|----------|
| Sentinel -1 + fail-fast env | flag default -1 → fallthrough to NewBudgetFromEnv; malformed env = fail-fast | ✓ |
| flag.Changed + silent default | cmd.Flags().Changed; malformed env warns + falls back | |

**User's choice:** Sentinel -1 + fail-fast.

### Fixtures + canonical-JSON placement
| Option | Description | Selected |
|--------|-------------|----------|
| Shared `internal/agent/agenttest` | reusable mocks for Phase 3/9 | ✓ |
| Inline in workflow_test.go | no reuse | |

| Option | Description | Selected |
|--------|-------------|----------|
| Shared `internal/canonicaljson` | reusable Phase 4/11 | ✓ |
| Exported in budget.go | improper coupling | |

**User's choice:** Both shared packages → PRD-amendment A3 (canonicaljson placement).

---

## Round 2 — runtime-shape decisions (post adk + D:/tmp study)

The user redirected again to **"loop D:/tmp/openhuman, picobot, nanobot and study best industrial patterns"** before answering.

### WithSubAgent vs Budget.Child + dedup ring
| Option | Description | Selected |
|--------|-------------|----------|
| Two derivations (WithSubAgent shared-ring / Child distinct-ring) | (initially deferred — user requested more online research) | ✓ (locked after scout pass) |
| One derivation with flag | opaque boolean footgun | |

**User's choice:** initially **"cerca ancora online pattern industriali — la fase è la più critica, dobbiamo essere sicuri al 99%"** → escalated to the scout pass; locked as two-derivations after validation.

### Author auto-fill vs explicit
| Option | Description | Selected |
|--------|-------------|----------|
| Explicit per agent + helper | each agent sets Author; optional SetAuthorIfEmpty | ✓ |
| Base Run-wrapper | adk-style auto-fill | |

**User's choice:** Explicit + helper. **Notes:** open interface has no base-struct hook.

### Branch format + iter index
| Option | Description | Selected |
|--------|-------------|----------|
| dot-join + Loop adds iter-N | matches SPEC `root.iter-2.worker-3` | ✓ |
| adk pure (no iter index) | shorter branch | |

**User's choice:** dot-join + iter-N.

### Property-based testing
| Option | Description | Selected |
|--------|-------------|----------|
| rapid now on budget.go | pgregory.net/rapid for 2 invariants | ✓ |
| Defer to post-MVP | table-tests only | |

**User's choice:** rapid now.

---

## Round 3 — dedup nuances (post online research)

Online research confirmed SHA-256(name+args)+window=3 is the documented standard.

### Dedup fingerprint composition
| Option | Description | Selected (R3) | Final (R4) |
|--------|-------------|----------|----------|
| (name, args) as SPEC | fire pre-execution | | ✓ (primary) |
| (name, args, result) as OpenClaw | result in hash | ✓ | reversed |

### Ping-pong scope
| Option | Description | Selected |
|--------|-------------|----------|
| consecutive-identical only, ping-pong deferred | A-A-A only | |
| Add ping-pong now | A-B-A-B period-2 | ✓ |

**User's choice (R3):** (name,args,result) + ping-pong. **Notes:** user chose maximum robustness. The result-inclusion was **later reversed in R4** after scout-2 proved it fail-open.

---

## Round 4 — scout-pass remediations (≥95% gate)

The user instructed **"manda sotto agenti in avanscoperta online e in D:/tmp fino a quando lo score >95%"** → 5 parallel scouts. Aggregate 86 pre-fix; three findings reversed/corrected prior choices:

### OTel SpanID fix
| Option | Description | Selected |
|--------|-------------|----------|
| SpanID 8-byte + MessageID/ToolCallID/ThreadID | OTel-correct + AG-UI forward-compat | ✓ |
| Keep UUIDv7 everywhere | claim stays false | |

**User's choice:** Adopt fix → A4. **Notes:** scout-5 found a hard defect — OTel SpanID is 8 bytes, UUIDv7 is 16.

### Dedup fingerprint (reversal of R3)
| Option | Description | Selected |
|--------|-------------|----------|
| Two-tier: name+args primary + result veto + exempt allowlist | blocks pre-execution, no fail-open | ✓ |
| Keep (name,args,result) + volatile-strip | requires per-tool strip list | |

**User's choice:** Two-tier → A2. **Notes:** reverses R3; result-in-hash is fail-open on volatile results (timestamp/page-token → loop never detected).

### Sibling starvation
| Option | Description | Selected |
|--------|-------------|----------|
| Per-branch soft cap at Child() | fair-share throttle, hard bound preserved | ✓ |
| Flat counter (as Strands prod) | starvation possible | |

**User's choice:** Soft cap → A5.

---

## Claude's Discretion
- Result-preview byte cap default (1–4 KB; reuse `Config.ToolPreviewCap`).
- `AURA_LOOP_BRANCH_SOFT_FRACTION` exact default vs `ceil(remaining/fanout)`.
- Go version stays 1.26.3.

## Deferred Ideas
- Period-k / general cycle dedup detection (future).
- Per-tool volatile-field strip list (only if result-veto insufficient).
- Proportional/weighted sub-budget allocation (DOVA-style, Phase 9 if needed).
- Real OTel `otel/trace` import (future observability slice).
- AG-UI SSE transport (Phase 12 adapter).
- `tools.Registry`↔Agent wiring + real `LlmAgent` (Phase 3).
- Swarm semantics — DM-by-ID, tier-mapped models, MAX_SPAWN_DEPTH=2 (Phase 9).

## Scout scoreboard (audit)
| Cluster | Pre-fix | Post-fix target | Key fix |
|---|---|---|---|
| Budget | 88 | ~95 | soft cap + atomic decrement-then-check + ctx.WithDeadline |
| Dedup | 86 | ~95+ | two-tier fingerprint + exempt + canonicaljson rescope |
| Go idioms | 91 | ~97 | goleak drain test + ctx invariant + no-bare-yield |
| D:/tmp sources | 89 | ~95 | (validation) |
| OTel/AG-UI | 78 | ~95 | 8-byte SpanID + Event IDs |
