# Memory write rules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The memory write rules sit in the write tools' descriptions and at the top of the memory-aura skill. `memory_entities` also lists the predicates in use. Both are measured on the lab VM with Sonnet, before and after.

**Architecture:**
- One Go constant in `cmd/arcadedb-mcp` holds the rules. `memory_upsert_fact` and `memory_batch` put it in their descriptions, and their field tags carry the per-field rule.
- `arcadedb.Client.ListEntities` gains one grouped query over the currently valid FACT edges. `memory_entities` returns its result as `predicates`.
- The skill opens with the same rules. A test in `cmd/arcadedb-mcp` reads the skill file and fails if a rule's marker is missing from that section.

**Tech Stack:** Go, ArcadeDB SQL, MCP go-sdk.

**Spec:** `docs/superpowers/specs/2026-11-28-memory-write-rules-design.md`

## Global Constraints

- Rule markers, checked in both places: `memory_entities`, `snake_case`, `one or two sentences`, `source: {}`, `in a month`.
- The skill section heading, exactly: `## Before any write`. It goes right after the opening paragraphs, before `## Memory tools`.
- `predicates` entries: `{"name": string, "facts": int}`. Only currently valid facts count, most used first. The list is capped by the same bound as the entity listing. `predicates_total` gives the number of distinct predicates.
- Read the ArcadeDB SQL `GROUP BY` page (asciidoc source) before writing the query, and cite it in the code comment.
- `docs/arcadedb-mcp-live-tools.json` is regenerated with `UPDATE_GOLDEN=1`, never edited by hand.
- No file over 600 LOC. `tool_memory.go` is at 560, so the rules constant goes in a new file.
- VM runs are paid: they go through the cockpit on the operator's Sonnet route. Approval cards are the operator's to click.

## Review Focus

- **A memory with no facts.** `predicates` must be an empty list with total 0, not null and not an error. Task 1, unit test.
- **Closed facts.** A predicate used only by superseded facts must not be listed. Task 1, live integration test.
- **A server row without the count column.** Fall back to the page length, as entities already do. Task 1, unit test.
- **Description length.** The rules paragraph must stay short. Cap the constant at 700 bytes in a test (Task 2), because both descriptions ride in every request.
- **The skill and the constant drift apart.** Task 3's marker test covers it.

## Decisions taken while writing this plan

- **The skill marker test lives in `cmd/arcadedb-mcp`,** which owns the constant. It reads `../../internal/skills/embed/memory-aura/SKILL.md` by path, the way `tool_manifest_test.go` reads `docs/`.
- **The baseline comes first (Task 0),** before any code, per CLAUDE.md's measure-first rule.

---

### Task 0: Baseline on the lab VM

- [ ] **Step 1:** In a new cockpit conversation on the Sonnet route, send these four messages one turn at a time. Do not mention memory:
  1. «Mio fratello si chiama Paolo e vive a Torino.»
  2. «Per lavoro uso soprattutto Go e TypeScript, il Python solo per gli script.»
  3. «Da gennaio voglio che i promemoria di lavoro arrivino via email, non su Telegram.»
  4. «Oggi ho passato la mattina a provare il browser della sandbox.»
- [ ] **Step 2:** Read back from the VM (`aura.tool_invocations` for the conversation and the memory's new facts by `run_id`):
  - `memory_entities` before the first write (y/n);
  - each predicate, and whether it is English snake_case and an existing one;
  - each new entity, and whether it is a name or a description;
  - each statement's length in characters;
  - whether message 4 was written.

  Record the results in the ledger.
- [ ] **Step 3:** Ask Aura in that conversation to forget the facts written by these runs (`memory_forget` by `run_id`). The operator clicks the approval.

### Task 1: `memory_entities` lists the predicates in use

**Files:**
- Modify: `internal/arcadedb/browse.go`, `cmd/arcadedb-mcp/tool_browse.go`, `docs/arcadedb-mcp-live-tools.json` (golden)
- Test: `internal/arcadedb/browse_test.go`, `internal/arcadedb/merge_live_integration_test.go` (or a new `browse_live_integration_test.go`, same tag), `cmd/arcadedb-mcp/tool_manifest_test.go` (golden)

**Interfaces:**
- Produces:
  - `arcadedb.PredicateSummary{Name string; Facts int}`;
  - `EntityListing.Predicates []PredicateSummary` and `EntityListing.PredicatesTotal int`;
  - `MemoryEntitiesOutput.Predicates`, `MemoryEntitiesOutput.PredicatesTotal`, with json `predicates`, `predicates_total`.

- [ ] **Step 1: Failing unit tests** in `browse_test.go` (fake server rows):
  - the predicate query runs with the current-validity condition (`asOfCondition`), groups by `predicate`, orders by count descending and carries the same LIMIT as the entities;
  - rows map to `PredicateSummary`;
  - an empty result gives `Predicates == []` (non-nil) and total 0;
  - a missing count column falls back to the page length.

  The existing tests that index `rec.statements` by position step by the new count of statements per call. That is an intended change; say so in the commit.
- [ ] **Step 2:** Run `go test ./internal/arcadedb/ -run ListEntities`. Expected: FAIL.
- [ ] **Step 3: Implement** after reading the ArcadeDB `GROUP BY` page: one grouped listing statement and one distinct-count statement. Update the tool description: "`predicates` lists the relations in use: reuse one before coining a new predicate."
- [ ] **Step 4: Live test** (`arcadedb_integration` tag): two facts with `works_on`, one with `lives_in`, and a third predicate whose only fact is superseded. Expected: `works_on:2`, then `lives_in:1`; the superseded-only predicate is absent.
- [ ] **Step 5: Verify.** Run `go vet`, `go test -race ./internal/arcadedb/ ./cmd/arcadedb-mcp/`, and `UPDATE_GOLDEN=1 go test ./cmd/arcadedb-mcp -run Manifest`. Then run the live test against the disposable ArcadeDB, using the same provisioning the `arcadedb_integration` profile uses.
- [ ] **Step 6: Commit** by pathspec: `feat(memory): list the predicates in use beside the entities`.

### Task 2: The write rules in the write tools

**Files:**
- Create: `cmd/arcadedb-mcp/memory_write_rules.go`
- Modify: `cmd/arcadedb-mcp/tool_memory.go` (description, field tags on `subject`, `predicate`, `object`, `statement`), `cmd/arcadedb-mcp/tool_memory_batch.go` (description, the same `MemoryBatchFactInput` tags), golden
- Test: `cmd/arcadedb-mcp/memory_write_rules_test.go`

**Interfaces:**
- Produces: `const memoryWriteRules string` (unexported). Task 3's test reads it.

- [ ] **Step 1: Failing test.**
  - `TestWriteToolsCarryTheRules`: lists the tools through an in-process server, the way `tool_manifest_test.go` does. Both descriptions contain every marker from Global Constraints. `predicate`'s schema description contains `snake_case`, and `statement`'s contains `one or two sentences`.
  - `TestWriteRulesStayShort`: `len(memoryWriteRules) <= 700`.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3: Implement.** The constant states the six rules of spec §Design 1 in at most 700 bytes, and both descriptions append it. Field tags:
  - `subject`/`object`: "a name from memory_entities when one fits; a new one is a noun, never a description";
  - `predicate`: "English snake_case verb phrase, one per relation; reuse one listed by memory_entities";
  - `statement`: "one or two sentences; what gets embedded and searched".
- [ ] **Step 4: Verify.** Run the tests, `UPDATE_GOLDEN=1` for the manifest, then `go test -race ./cmd/arcadedb-mcp/`.
- [ ] **Step 5: Commit:** `feat(memory): put the write rules in the write tools`.

### Task 3: The skill opens with the rules

**Files:**
- Modify: `internal/skills/embed/memory-aura/SKILL.md`
- Test: `cmd/arcadedb-mcp/memory_write_rules_test.go` (adds `TestSkillOpensWithTheWriteRules`)

- [ ] **Step 1: Failing test.** Read the skill file and take the text between `## Before any write` and the next `## `. It must contain every marker. It must sit before `## Memory tools`.
- [ ] **Step 2:** Run the test. Expected: FAIL (no such heading).
- [ ] **Step 3: Write the section.** About 15 lines, the six rules in prose. Under `## Writing`, add a `### Predicates` subsection (snake_case, reuse from the listing, one per relation) and a `### Statements` subsection (one or two sentences, why: correction is per fact).
- [ ] **Step 4: Verify.** Run `go test ./cmd/arcadedb-mcp/ ./internal/skills/...`. The embed size checks must stay green.
- [ ] **Step 5: Commit:** `docs(skill): open the memory skill with the write rules`.

### Task 4: After, on the lab VM

- [ ] **Step 1: Ship.** Push (the WSL recipe, once the peer session's tree allows it) and wait for CI green on every job of that SHA, plus the Aura and ArcadeDB MCP images. Then wait until the VM runs it: check the container labels of `aura` and the memory MCP.
- [ ] **Step 2:** Repeat Task 0 Steps 1–3 with the same four messages in a new conversation.
- [ ] **Step 3: Write** `docs/superpowers/verification/2026-11-28-memory-write-rules.md`: the before/after table, and what one run per side does not prove. Commit by pathspec.
