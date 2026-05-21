# cli-printing-press — Zone Map / Tool Ownership Patterns

**Date:** 2026-05-21
**Source:** `D:/tmp/cli-printing-press` (v4)
**Companion to:** `cli-printing-press-output-discipline-2026-05-21.md`

## TL;DR

Printing-press has **no system prompt and no separate agent class** — the "agent" is Claude Code, and the zone map lives entirely in `SKILL.md` as three named bash vars (`PRESS_RUNSTATE` / `PRESS_LIBRARY` / `PRESS_MANUSCRIPTS`) plus a phase-state machine that pins each phase to one directory + one `printing-press <subcommand>` owner. `mcpdesc.Compose` does NOT inject zone/owner markers — purely action/params/returns. Real pattern: **3 durable locations, exhaustively enumerated up-front, hard rule "every artifact goes to exactly one of them"**, workflow phase implicitly selects which is active.

## Zone & Tool Counts

- **Durable zones:** exactly **3** (`PRESS_RUNSTATE`, `PRESS_LIBRARY`, `PRESS_MANUSCRIPTS`) — `SKILL.md:573-578`
- **Ephemeral zone:** `/tmp/printing-press/` with mandatory deletion — `SKILL.md:90, 580-581`
- **Sub-zones inside RUNSTATE:** 5 (`research/`, `proofs/`, `pipeline/`, `discovery/`, `working/<api>-pp-cli/`) — `SKILL.md:524-529`
- **CLI subcommands ("tools"):** ~12 owner-verbs (`browser-sniff`, `generate`, `dogfood`, `verify`, `scorecard`, `polish`, `publish`, `lock`, `mcp-sync`...); each owns one phase + one sub-zone
- **Skills:** 9 sibling skills, each with its own zone scope

## How "Where What Is" Is Communicated

### 1. Three-zone enumeration at the top of the skill (NOT in a system prompt)

`SKILL.md:573-581`:

> "There are exactly **three** durable writable locations. Every generated artifact this skill preserves goes to **one** of them."
>
> - **`$PRESS_RUNSTATE/`** — mutable working state for the current run
> - **`$PRESS_LIBRARY/`** — published CLIs (`<api-slug>/` subdirectories)
> - **`$PRESS_MANUSCRIPTS/`** — archived run evidence

The word "exactly" carries the weight. Entire zone declaration — no taxonomy file, no JSON.

### 2. Bash variable export = "the path is the contract"

`SKILL.md:225-232` exports `PRESS_HOME`, `PRESS_RUNSTATE`, `PRESS_LIBRARY`, `PRESS_MANUSCRIPTS`, `PRESS_CURRENT` and `mkdir`'s them at preflight. Agent is told to ALWAYS use vars (never literal paths). Examples at `:583-589`.

### 3. Run-scoped sub-zone variables (`SKILL.md:522-554`)

After `RUN_ID` is computed, **five** sub-zone vars are exported and `mkdir -p`'d in one block:

```bash
API_RUN_DIR="$PRESS_RUNSTATE/runs/$RUN_ID"
RESEARCH_DIR="$API_RUN_DIR/research"
PROOFS_DIR="$API_RUN_DIR/proofs"
PIPELINE_DIR="$API_RUN_DIR/pipeline"
DISCOVERY_DIR="$API_RUN_DIR/discovery"
CLI_WORK_DIR="$API_RUN_DIR/working/<api>-pp-cli"
```

Plus `SESSION_DIR` (live cookies — explicitly OUTSIDE `API_RUN_DIR` so the Phase 5.6 archive `cp` can't capture secrets, `:533-543`). **Containment by location, not by manual rm-before-archive** is the design principle (literal comment line 534).

### 4. Phase → zone → owner-tool mapping (state machine)

Phase headers (`SKILL.md:603, 771, 1609, 2565, 2793, 3014`) implicitly bind:

- **Phase 0 Resolve+Reuse** → reads `$PRESS_MANUSCRIPTS/*/research/*` (line 642)
- **Phase 1 Research Brief** → writes `$RESEARCH_DIR`
- **Phase 1.7 Browser-Sniff** → owner `printing-press browser-sniff`, marker at `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json` (line 952)
- **Phase 2 Generate** → owner `printing-press generate`, writes `$CLI_WORK_DIR`
- **Phase 4 Shipcheck** → owners `dogfood`/`verify`/`scorecard`, writes `$PROOFS_DIR`
- **Phase 5.6 Archive** → owner `cp -r $RESEARCH_DIR $PRESS_MANUSCRIPTS/...` (lines 3054-3072)

State persisted in `$STATE_FILE` (`$API_RUN_DIR/state.json`, lines 554-567) so sibling skills rediscover the active zone.

### 5. `mcpdesc.Compose` does NOT carry zone info

`internal/mcpdesc/compose.go:69-96` produces only `action + Required + Optional + Returns + method-marker`. No "Zone:" field. **Zone ownership is conveyed at the workflow layer, never the tool-description layer.**

## Patterns Aura Should Lift

### P1 — Enumerate zones at the top of `AGENT.md` with the "exactly N" framing

State explicitly: **"Aura has exactly 6 zones. Every read/write goes to exactly one of them."** List owner-tools per zone (one read-owner + one write-owner). Bold "exactly" framing matters; vague lists invite the LLM to invent new categories. (Source: `SKILL.md:573-578`.)

### P2 — Path-prefix = ownership signal (no separate registry needed)

Printing-press conveys "anything under `$PRESS_RUNSTATE` is owned by `printing-press <subcommand>`" via path-prefix convention. Aura mirror: `/wiki/*` → `wiki_read/wiki_write`; `/wiki/raw/src_*` → `read_source/store_source`; `/workspace/*` → `workspace_read/workspace_write`; SQLite tables → only via the named tool. Path IS the routing key. (Source: `SKILL.md:583-589`.)

### P3 — Phase-pinned tool availability (soft state machine in prose)

No hard FSM — bolded phase headers, each phase references ONE owner-subcommand. LLM reads top-down and picks right. Aura mirror in `AGENT.md`: "Archived sources → `read_source` (NOT `read_memory`). Wiki content → `wiki_read` (NOT `search_memory`)." Group by USER intent. (Source: `SKILL.md:603-3083`.)

### P4 — Containment by location, not by manual cleanup

`SESSION_DIR` placement OUTSIDE `$API_RUN_DIR` (line 534 comment) is canonical: if a zone must NOT be archived/indexed/exposed, put it physically outside the parent zone so the wrong tool literally can't reach it. Aura mirror: secrets/session state must live outside any directory `search_memory` or `wiki_rebuild` could walk. Verify `data/.env` is never indexed. (Source: `SKILL.md:533-543`.)

### P5 — `state.json` checkpoint for cross-skill / cross-tool rediscovery

Lightweight `state.json` per run lets a separately-invoked sibling skill find the active zone without re-prompting. Aura mirror: single `~/.aura/active_context.json` containing `{conversation_id, last_zone_touched, last_source_id}` — every subsequent tool reads it as "where were we?". (Source: `SKILL.md:554-567`.)

## Cross-ref vs Previous Output-Discipline Study

| Concern                          | Output-Discipline | Zone-Map (this) | Overlap?                  |
| -------------------------------- | ----------------- | --------------- | ------------------------- |
| 4 KiB cap on tool output         | P1                | —               | NO — orthogonal           |
| Forked `context: fork` review    | P4                | —               | NO — orthogonal           |
| Structured `---RESULT---` block  | P5                | —               | NO — orthogonal           |
| `mcpdesc.Compose` description    | P6                | confirmed empty | **Complementary**         |
| 3 durable zone vars              | —                 | P1, P2          | **NEW** — zone exclusive  |
| Phase → owner-tool pinning       | —                 | P3              | **NEW** — zone exclusive  |
| Containment-by-location          | —                 | P4              | **NEW** — zone exclusive  |
| `state.json` checkpoint          | —                 | P5              | **NEW** — zone exclusive  |

**Verdict:** Zero redundancy. Output-discipline = "**what** can leave the substrate" (truncation, forking, schema gating). Zone-map = "**where** data lives + **who** owns it". Aura needs both — output-discipline at the tool-`Execute` boundary, zone-map at the prompt-overlay layer.

## Concrete Next Step

Draft `AGENT.md` section **"Aura has exactly 6 zones"**:

1. `wiki/` → R `read_memory` · W `write_memory`
2. `wiki/raw/src_*/` → R `read_source` · W `store_source`+`ingest_source`
3. `/workspace/*` → R `workspace_read` · W `workspace_write`
4. SQLite `scheduled_tasks` → R `list_tasks` · W `schedule_task`/`cancel_task`
5. Web → R `web_search`+`web_fetch` · W N/A
6. Conversation → R implicit · W N/A

Then in each prose section, name ONE owner-tool per zone touched.
