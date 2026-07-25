# Handoff — 2026-07-25: memory sidecar defects + Phase A close-out

**Branch:** `master` (master-direct). Local is **ahead of `origin/master` and unpushed** — the push is blocked, see §Blockers.
**Governing index:** [consolidated-fix-plan-2026-07-20.md](consolidated-fix-plan-2026-07-20.md) — updated today with Phase A, the memory findings (M-1…M-6) and the onboarding audit. Read that first; this file only carries what it does not.

---

## Blockers, in the order they bite

1. **`internal/adaptive` lint is red — every `.go` commit is blocked.** 46 findings (45 revive missing doc-comments + 1 unused func) from a parallel Codex workstream. `--no-verify` is forbidden. Docs-only and Python-only commits DO land (the lint hook is globbed on `*.go`; `go vet` still runs and passes). Everything committed today was therefore Python or docs.
2. **CI on `origin/master` is red** for the same reason (run 30132905351, job "Build + vet + lint + deadcode").
3. **The appliance's live DB is a moving target.** Codex applies migrations to the live `aura` DB from an *uncommitted* working tree; `serve` only CHECKS the migration head (strict equality) and never migrates, so the deployed container crash-loops until an image embedding exactly that migration set is built. The container was rescued twice today by building from a clean worktree at the merge commit **plus copying in the uncommitted `0071` files**. It will break again on `0072`.

---

## In flight at handoff — three subagents, results NOT yet merged

All were told **not to commit**, so their work sits **uncommitted inside their worktrees** under `.claude/worktrees/agent-<id>/`. Their branches still point at the old base and their diffs are empty — do not judge by `git log`.

**Landing procedure that worked** (their base is `ca587adb8`, behind master, so a copy is wrong):
```
cd .claude/worktrees/agent-<id> && git diff > /d/tmp/x.patch   # + copy any untracked test files
cd /d/Aura && git apply --3way /d/tmp/x.patch                  # main tree must be clean first
```

| Agent | Scope | Files it owns |
|---|---|---|
| ownership + last read path | unify the two definitions of "the user owns this"; find every remaining projection of `canonical_name` as `name`; strengthen the guard to catch the CLASS not the literal | `docker/agent-memory/`: `queries.py`, `integration.py`, `_tools.py`, tests |
| task→memory cascade | a cancelled task must stop being recalled as present intent (BUG-3b / 2.8) | `internal/cron`, `cmd/aura/memory*.go`, possibly a migration |
| onboarding latency | one MCP connection instead of ~20 handshakes; stop the per-page-load status probe; fix the write-ordering bug | `cmd/aura/memory_onboarding.go`, `cmd/aura/memory.go`, `web/src/AppShell.tsx` |

### ALL THREE FAILED — read before re-dispatching

All died the same way: `Agent stalled: no progress for 600s (stream watchdog did not recover)`. Not a code problem; they were killed mid-work. **This is not Go-specific** — the ownership agent was pure Python and stalled too, so the earlier "long Go builds starve the stream" guess is wrong. Four long-running agents were dispatched today and only the first (embedding) finished. Consider smaller, shorter-lived agents, or doing the work inline.

- **onboarding latency** — left **partial, unverified** work in `.claude/worktrees/agent-a218f7c0a1287596f`: 251 lines across `cmd/aura/memory_onboarding.go` + `memory_onboarding_test.go`. `web/src/AppShell.tsx` is untouched, so fix 2 (the per-page-load status probe) was never done, and it never reported a build or test result. **Treat the Go side as a draft to review, not as working code.** Do not prune that worktree before someone reads it.
- **task→memory cascade** — died at "Now I have the full picture. Let me implement. First, the migration." Its worktree is **empty**: nothing to salvage, re-dispatch from scratch. Note it was about to create a migration — whoever redoes it must re-derive the number with `ls internal/db/migrations/ | tail -1`, since the floor moved today (`0071` is on disk but still uncommitted by the parallel workstream).
- **ownership + last read path** — stalled too. Its first half was **done inline afterwards** and is committed (see below); its second half is the one thing still missing before the controprova can pass.

---

## Landed after this file was first written

| Commit | What |
|---|---|
| `28fbe5efb` | the recalled-context block names the entity as stored (a 4th read path the earlier fix missed) |
| `fb0000caa` | extraction embeds on write; composite uniqueness constraint on the MERGE key; backfill wired + CLI |
| (ownership) | extracted entities get their message-owner's `HAS_ENTITY` edge — the actual reason `memory_forget` refused |

The ownership one deserves a note, because the obvious fix was the wrong one. The instinct was to widen `DELETE_ENTITY_SCOPED`'s guard to match `ENTITY_IN_USER_SCOPE`. That would have loosened permission on the destructive verb to paper over a gap in the write path. The real defect: only `add_entity` ever wrote `(:User)-[:HAS_ENTITY]->(:Entity)`, so extraction-born entities had no ownership at all. Fixed where the entity is created, not where it is deleted.

---

## THE one thing blocking the controprova

`memory_get_entity` still projects `canonical_name` as `name`. Same node, same moment, in the live trace: `memory_search` → `"name": "Davide"`, `memory_get_entity` → `"name": "David"`.

Three projection sites in `integration.py` were fixed with `_entity_name_fields()` (stored name + `canonical_name` as its own field when it differs) and the context builder in `long_term.py` was fixed after that. `get_entity` builds its dict somewhere else and was missed; the source guard in `tests/test_entity_name_projection.py` did not catch it because that site is written differently from the literal the guard checks.

Do two things: fix the site, and make the guard catch the CLASS (any read path exposing `display_name` as a caller-facing name, across modules) rather than one string in one file.

---

## Resume here — next session

```
Aura, memory sidecar + Phase A. Read docs/audit/HANDOFF-2026-07-25-memory-and-phase-a.md
and the Phase A / memory / onboarding sections of
docs/audit/consolidated-fix-plan-2026-07-20.md first.

Do these in order:

1. Fix the last read path: memory_get_entity still reports canonical_name as
   "name". Then strengthen tests/test_entity_name_projection.py to catch the
   class of mistake, not the literal string. Python only, so it commits fine.

2. Rebuild + restart the memory sidecar, then restart the aura container too —
   the daemon mounted 12 memory tools at boot and never re-mounted after the
   sidecar restart, so it may not have memory_update at all. That ambiguity has
   to be gone before the test means anything.

3. Run the controprova. Ask Aura in plain language to fix its own memory (wrong
   name + duplicates). Watch aura.tool_invocations and the Neo4j graph, not its
   reply. Pass = the three entity ids survive (corrected, not recreated),
   2a368f39's canonical becomes "Davide", no :Entity named "David" remains, no
   add->forget loop. The baseline is in the handoff.

4. Then, with the operator present, run the embedding backfill so the two
   embedding-less duplicates become searchable:
   docker exec aura-agent-memory-mcp neo4j-agent-memory \
     maintenance backfill-embeddings --embedding-dimensions 768

Blocked until the internal/adaptive lint goes green (46 findings from a parallel
workstream): every .go commit, the push, and CI. Python and docs commit fine.
--no-verify is forbidden. Do not re-dispatch long-running subagents without
reading the failure note — 3 of 4 were killed by a 600s stall watchdog today.
```

The last two are Go → **they cannot be committed until blocker 1 clears.**

---

## The controprova — designed, run, FAILED for a reason worth keeping

Test: ask Aura in plain language to fix its own memory (wrong name + duplicates) and see whether it corrects instead of destroying. Baseline was pinned at 12:18:19 UTC.

**It failed, and not because of the model.** The trace shows it searched, found the entities, called `memory_get_entity`, then `memory_forget` — and was refused **three times** with `"not found or not owned by this user"` on the operator's own data. It then fell back to `add_entity`, which merged into the old node again.

Two causes, both now with an agent:
- `DELETE_ENTITY_SCOPED` (`queries.py:1182`) requires a direct `HAS_ENTITY` edge; `ENTITY_IN_USER_SCOPE` (used by update and by the scoped searches) also accepts `MENTIONS`/`APPLIES_TO`/`TOUCHED`. The duplicates have only the latter, so they are **visible, updatable and undeletable**.
- `memory_get_entity` still reports `canonical_name` as `name` — same node, `search` says "Davide", `get_entity` says "David". A fourth read path the earlier fix missed and the source guard did not catch because it is written differently.

**Re-run it once those land.** Graph baseline to compare against:
```
37003044…  David   OBJECT  emb=FALSE   ← onboarding duplicate
20d7511c…  David   PERSON  emb=FALSE   ← onboarding duplicate
2a368f39…  Davide  PERSON  emb=TRUE    canonical_name="David"  ← the operator's real node
```
Pass = the three ids survive (corrected, not recreated), `2a368f39`'s canonical becomes "Davide", no `:Entity` named "David" remains, and there is no add→forget loop.

**Also unresolved:** the running daemon logged `mcp mounted server=memory tools=12` at 10:59 and never re-mounted after the sidecar restarted at 11:49, so it may not have `memory_update` in its manifest at all; the operator reports seeing 13. **Restart the `aura` container before re-running** to remove the variable.

---

## Not yet run (deliberate — touches the operator's data)

```
docker exec aura-agent-memory-mcp neo4j-agent-memory \
  maintenance backfill-embeddings --embedding-dimensions 768
```
Repairs the embedding-less entities so `memory_search` can see them. Run it with the operator present. Note the new composite uniqueness constraint on `Entity(name, type, deduplication_scope)` will **refuse to create** while duplicates sharing that key exist — that is intended (it surfaces them), but it means constraint creation and duplicate cleanup are ordered.

---

## Phase A — still open to close

`T8` rebuild the committed `internal/webui/dist` (must be built on Linux node-24 via the docker webbuild stage — a Windows Vite build re-hashes every chunk) and `T10` (coverage gate on the stricter `db_integration`-only Skills number, full live E2E, push, CI green). Everything else is merged: see the plan's Phase A table for per-task commits.

---

## Method note worth carrying forward

Every defect that mattered today was found by **using** the system, not by reading it: mounting the memory MCP as a client and creating one test entity exposed that reads lied about names, that canonical resolution crossed user scopes, and that every `*_SCOPED` query failed to scope. The 38-test suite was green throughout and structurally could not see any of it — it uses in-memory fake clients that never execute Cypher. Two of today's diagnoses (mine on the loop's cause, the audit's on `AURA_CONTEXT_MEMORY_RECALL` being off) were **wrong and corrected by evidence**; both are recorded as such in the plan.
