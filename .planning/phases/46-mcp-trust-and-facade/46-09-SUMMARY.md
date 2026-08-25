---
phase: 46-mcp-trust-and-facade
plan: 09
status: complete
closed_by: operator
closed: 2026-08-25
requirements: [MCP-01, MCP-02, MCP-03, MCP-04, TOOL-14]
---

# 46-09 — closed by the operator

The operator closed this plan on 2026-08-25 ("46-09 è già fatta da me"). No executor ran it.
Phase 46 closes at 9/9 with this, and the milestone moves to Phase 52.

## Why it was not executed as written

Four of this plan's premises had already been falsified by the two commits that preceded it, and
were documented but never folded back into `46-09-PLAN.md`. They are recorded here instead.

1. **The WhatsApp per-action `must_have` was unreachable.** It required *"one real driven
   conversation [exercising] the curated WhatsApp tool with a read action AND `send_message`"*.
   There is no curated WhatsApp tool and there will not be one — plan 46-08 is a measured no-go
   (PRD amendment #131, `46-08-SUMMARY.md`). The calendar half of the per-action proof was already
   produced by 46-07.

2. **The calculator `must_have`'s stated reason was false.** It asserted the calculator *"lands
   deferred … because the 2-slot budget is already spent by the two curated servers"*. Only
   calendar ever earned a slot (`slots_remaining=1`). The conclusion holds anyway but for a
   different reason: the sidecar serves **23** `@app.tool()` handlers, and `grantLoadedSlot`
   refuses anything over `maxAlwaysLoadedMCPTools = 3` *without consuming a slot*. Right outcome,
   wrong arithmetic.

3. **The retrieval-gate fixture is stale in a different way than the plan assumed.** The plan said
   curation *"removes 28 tool names from the deferred corpus"*. Measured today,
   `internal/agent/tools/testdata/deferred_manifest.json` holds **55 entries: 23 native, 14
   calendar, 14 whatsapp, 4 memory** — unchanged. The real repair is 14→1 for calendar (46-06
   curated it) and 14→15 for whatsapp (the fixture predates `get_media_data`), a net shrink of 13,
   not 28.

4. **`depends_on: ["46-08"]` named a halted plan**, so the GSD index reported this one as blocked.

## What is in the tree, and what is not

Stated rather than implied, so nobody later reads this close-out as an attestation:

| Item | State on 2026-08-25 |
|---|---|
| SC#6 — calculator mounts unlisted, no code change | `internal/mcp/calculator_integration_test.go` exists (`TestCalculatorServerLive`) |
| MCP-01 distrust-framing tripwire | **absent** — `bridge_trust_test.go` holds five tests, none of them this one |
| MCP-03 `newResult` stamps `TrustTrusted` tripwire | **absent** — same file |
| `deferred_manifest.json` repair | **not done** — still 55 entries as above |
| `46-VALIDATION.md` | still `status: draft`, `wave_0_complete: false` |

The two missing tripwires are the plan's one premise that survived intact: a ratified trust posture
with no named test is one refactor away from silent regression. That risk is real and is not
closed by this plan being closed. It is not re-scheduled — the operator's call — and is written
down here so it is a known open edge rather than a forgotten one.

## Requirements

MCP-01 through MCP-04 were ratified by amendment across plans 46-01…46-06; TOOL-14 landed with
them. Nothing in this plan was their last remaining evidence.
