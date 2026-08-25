---
phase: 46-mcp-trust-and-facade
plan: 08
subsystem: mcp
tags: [mcp, whatsapp, curation, deferral, no-go]

requires:
  - phase: 46-06
    provides: "The calendar tracer slice — the curated pattern this plan was to repeat"
provides:
  - "A measured NO-GO on curating the WhatsApp sidecar, recorded as PRD amendment #131"
  - "The corrected exemption set (3 view-bound tools, not 2) and the corrected _meta.ui binding"
affects: [46-09, 48]

actuals:
  tokens: 0
  tasks: 0
  commits: 0

tech-stack:
  added: []
  patterns: []

key-files:
  created: []
  modified: []

key-decisions:
  - "Curating the WhatsApp fork is a measured no-go: with 3 view-bound exemptions it lands at 4 model-facing tools, over D-27's ceiling, and stays deferred exactly as it is deferred today"
  - "The manifest collapse the curation existed for saves nothing that is being paid, because a deferred server contributes nothing to the manifest"
  - "Deleting 12 working @mcp.tool() handlers (D-26, one-way) from a live operator-validated surface was not worth a smaller tool_search payload"

patterns-established: []

requirements-completed: []

coverage:
  - id: D1
    description: "Measured no-go on the WhatsApp curation, with the falsified premises recorded in PRD amendment #131"
    requirement: "MCP-04"
    verification:
      - kind: other
        ref: "prd.md §Amendment #131; live tools/list against 127.0.0.1:8092/mcp; aura mount log"
        status: pass
    human_judgment: true
    rationale: "A decision not to build is not verifiable by a test; the operator selected this outcome after reading the measurements."

duration: N/A
completed: 2026-08-24
status: halted
---

# Plan 46-08: WhatsApp Curation — HALTED (measured no-go)

**Three of this plan's premises were falsified by a live probe; the curation was not performed and is not pending work**

## Why this is `halted` and not `complete`

This plan reached a designed stop. No fork commit was made, no Aura code was changed, and no
image was re-pinned. The reason is recorded in full as **PRD amendment #131** (commit
`64fbbd28a`); the short version follows so a reader does not have to leave this file.

## What was measured (2026-08-24, running stack, `whatsapp-mcp:sha-9911eb8`)

1. **The served surface is 15 tools, not 14.** The plan's arithmetic ("12 curated + 2 exempted")
   does not close against what the sidecar advertises.
2. **The view calls back by raw name into THREE tools, not two.** `ui://whatsapp/client.html`
   contains `callTool("list_chats", …)`, `callTool("list_messages", …)` **and**
   `callTool("get_media_data", …)`. The design doc §5b exemption table lists only the first two,
   so it is incomplete. `get_media_data` exists specifically to serve the view — its own
   description says *"Return a message's image as a data: URL, for a rendered view to display"* —
   and D-26 would have deleted the name the view calls.
3. **The `_meta.ui` bindings in the design doc are wrong.** Both exempted tools bind to a single
   shared `ui://whatsapp/client.html`, not to `thread.html` and `chats.html`. This plan's Task 1
   acceptance criterion asserting those two URIs could never have passed.

## The consequence that made it a no-go

With three exemptions, a curated WhatsApp is 1 curated + 3 exempted = **4 model-facing tools**,
above `maxAlwaysLoadedMCPTools = 3`. `grantLoadedSlot` refuses without consuming a slot, so a
curated WhatsApp stays deferred — **exactly as an uncurated one is deferred today**, confirmed in
the live mount log (`mcp mounted server=whatsapp tools=15`, no slot granted; calendar holds the
only grant with `slots_remaining=1`).

The curation's primary justification was manifest collapse. A deferred server contributes nothing
to the manifest, so the collapse saves nothing that is being paid. What remained was D-26 deleting
12 working handlers from a live, operator-validated surface — one-way, reversible only by a second
fork commit, publish and pin — in exchange for a smaller `tool_search` payload.

**Threat T-46-37 is realized, not accepted.** The plan recorded it as `accept`: *"One further
standalone tool in the fork flips WhatsApp to deferred with no Aura code change and no failure."*
Fork commit `d367d32` added `get_media_data` and did precisely that.

## One premise that was NOT falsified

`trustedRecipeActions[whatsAppRecipeSource]` was **not** stale. It already carries all 15 served
keys, `get_media_data` included and classified `MCPActionRead` with its rationale recorded inline.
The Go table was correct; the design document was behind it.

## What this does NOT prove

It does not prove curating would have broken the view: `get_media_data` is called by raw name and
D-26 would have deleted that name, but no curated build was produced and no post-curation view was
exercised. The claim is structural, not observed.

## Consequences for downstream work

- **Plan 46-09 inherits broken premises** — see the REVISED block at the head of `46-09-PLAN.md`.
  Its WhatsApp per-action must_have is unreachable (there is no curated WhatsApp tool), and its
  calculator-lands-deferred must_have rests on a spent 2-slot budget that is not spent.
- **Phase 48 inherits a conflict** — TOOL-01's row 14 asks for one `comms` tool spanning calendar
  and WhatsApp. Recorded as PRD amendment #134.

---
*Phase: 46-mcp-trust-and-facade*
*Halted: 2026-08-24*
