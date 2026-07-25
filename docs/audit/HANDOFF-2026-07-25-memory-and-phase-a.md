# Handoff — 2026-07-25: memory sidecar defects + Phase A close-out

**Branch:** `master` (master-direct). Local is **ahead of `origin/master` and unpushed** — the push is blocked, see §Blockers.
**Governing index:** [consolidated-fix-plan-2026-07-20.md](consolidated-fix-plan-2026-07-20.md) — updated today with Phase A, the memory findings (M-1…M-6) and the onboarding audit. Read that first; this file only carries what it does not.

---

## Blockers, in the order they bite

1. **`internal/adaptive` lint is red — every `.go` commit is blocked.** 46 findings (45 revive missing doc-comments + 1 unused func) from a parallel Codex workstream. `--no-verify` is forbidden. Docs-only and Python-only commits DO land (the lint hook is globbed on `*.go`; `go vet` still runs and passes). Everything committed today was therefore Python or docs.
2. **CI on `origin/master` is red** for the same reason (run 30132905351, job "Build + vet + lint + deadcode").
3. **The appliance's live DB is a moving target.** Codex applies migrations to the live `aura` DB from an *uncommitted* working tree; `serve` only CHECKS the migration head (strict equality, `internal/db/migration_head.go:45`) and never migrates, so the deployed container crash-loops until an image embedding exactly that migration set is built. It bit again at 13:40: the running binary embedded through `0071` while the DB had advanced to `73`, so **restarting `aura` at all would have crash-looped it**.

   **Check before every restart** (30 seconds, and it is the difference between a restart and an outage):
   ```
   docker exec aura-postgres psql -U aura -d aura -tAc "select version, dirty from schema_migrations"
   docker exec aura sh -c 'grep -ao "00[0-9][0-9]_[a-z0-9_]*\.up\.sql" $(command -v aura) | sort -u | tail -1'
   ```
   They must be equal. When they are not, rebuild from a **clean worktree at HEAD** — which is now enough on its own, since `0072`/`0073` are committed and only `0074` is loose:
   ```
   git worktree add /d/tmp/aura-head-build --detach HEAD
   cd /d/tmp/aura-head-build && docker build -t aura:local -f docker/aura/Dockerfile .
   cd /d/Aura && docker compose up -d --force-recreate aura
   ```
   Done at 13:41 (image `48a1784f`, embedded head `73` = DB head `73`). **It will break again the moment `0074` is applied to the live DB** — the same 8-minute rebuild, for a migration that is still uncommitted.

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

## ✅ The read path is fixed — and the controprova ran (round 2)

`df2e055a0` closed the fifth site (`memory_get_entity`) plus two more nobody had listed: the
entities-catalog resource and the relationship echo. The live trace now reads
`{"name": "Davide", "canonical_name": "David"}` on the node that used to answer "David".

The guard was the real lesson. It had been checking a **literal** copied from the site just
fixed, so each new site — written differently — walked past it; that is how a fifth path
shipped after three fixes and a fourth. It now bans the mistake itself: no caller-facing module
may read `display_name` at all, checked over the **AST** (so the comments explaining the rule
don't trip it) on a file set **derived from the package directory** (so a read path in a NEW
module is covered the day it is written). Two counter-guards keep it honest — one pins the
files it must cover, one fails if the projection helper falls out of use, since "project no
name at all" also satisfies a ban. Verified by mutation: reinstating the defect fails three
tests, including one that drives the registered MCP tool. 53 pass.

**Then the daemon was restarted and the controprova re-run.** See §Controprova round 2 — it
found the next defect down, and it is not in the sidecar.

---

## Resume here — next session

```
Aura, memory sidecar + Phase A. Read docs/audit/HANDOFF-2026-07-25-memory-and-phase-a.md
and the Phase A / memory / onboarding sections of
docs/audit/consolidated-fix-plan-2026-07-20.md first.

Steps 1-3 of the previous handoff are DONE: the read path is fixed and committed
(df2e055a0), the daemon re-mounted with 13 memory tools incl. memory_update, and
the controprova ran again. It failed one layer up — see "Controprova round 2".

Do these in order:

1. COMMIT THE TWO GO FIXES (M-7 + M-8) the moment the internal/adaptive lint
   goes green. They are RUNNING LIVE IN THE APPLIANCE BUT NOT IN GIT (operator's
   call, 2026-07-25). Until they land, any rebuild of aura:local from a clean
   tree silently reverts memory_update to refusing the operator's own data AND
   re-bricks the MCP session after a sidecar restart.
     git apply /d/tmp/memory-bridge-and-session-fixes.patch
   Both are build/vet/race/lint-clean at df2e055a0, each with a test that fails
   on the exact live symptom when mutated. The live image was built from
   /d/tmp/aura-head-build (worktree, still on disk, patch applied).

2. Wire the sidecar's 67 tests into CI — nothing runs them today (see below).
   The two that execute Cypher need a Neo4j; the other 65 need nothing.

3. Phase A T8 + T10 still open (see the plan's Phase A table).

Blocked until the internal/adaptive lint goes green (52 findings from a parallel
workstream, up from 46 — it is still growing): every .go commit, the push, and
CI. The pre-commit lint step lints ALL owned packages whenever any *.go file is
staged, so this blocks Go commits that touch nothing near adaptive. Python and
docs commit fine. --no-verify is forbidden. Do not re-dispatch long-running
subagents without reading the failure note — 3 of 4 were killed by a 600s stall
watchdog.
```

---

## ✅ Controprova PASSES (2026-07-25 16:11 UTC)

Same driver, same prompt, same four criteria. Four tool calls, twenty seconds:
`get_entity("David")` + `get_entity("Davide")` → `forget(37003044)` → `update(2a368f39)`.

| Criterion | Verdict |
|---|---|
| the operator's node corrected, never recreated | ✅ `2a368f39` kept its id through every round and was edited in place |
| `2a368f39`'s canonical becomes "Davide" | ✅ |
| no `:Entity` named "David" remains | ✅ both duplicates gone — `20d7511c` and `37003044` deleted outright (`removed_node: true`), not merely unlinked |
| no add→forget loop | ✅ no `add_*` call at all |

The graph is now one entity: `Davide`, PERSON, canonical `Davide`, owned by the operator.

What each fix contributed, in the order they stopped mattering: the read path stopped lying
about names, so the agent stopped deleting what it had just written (`df2e055a0`); the bridge
started sending the caller's identity, so `memory_update` stopped refusing the operator's own
data (M-7); the ownership backfill gave the extraction-born duplicates an owner, so
`memory_forget` stopped refusing them (`47f07053e`); forget started cutting the caller's
`MENTIONS` too, so a "deleted" entity actually left the graph instead of lingering unowned
(`4338b0152`); and `get_entity` stopped hiding the second duplicate behind `limit=1`, so the
agent saw both instead of correcting one and stopping (`4338b0152`).

**Left behind, deliberately not cleaned:** the duplicate fact round 2 manufactured —
`325b20fd` (`David → expertise → Go, AI agents`) beside `ca17ee4e` (`Davide → …`). It is the
evidence of what a refused correction verb costs, and it is the operator's data to remove:
`docker exec aura aura neo4j cypher write "MATCH (f:Fact {id:'325b20fd-…'}) DETACH DELETE f"`.

---

## Controprova rounds 3 and 5 — how it got there

Round 3 (15:34 UTC) with M-7 deployed live: `memory_update` **succeeded** on the operator's node
and `2a368f39`'s canonical became **"Davide"**. Ten tool calls, no `add_entity` at all, no loop.
Only `memory_forget` still refused the two unowned duplicates.

Then the ownership backfill (`47f07053e`, 2 entities repaired) and round 5 (15:41 UTC):

| Criterion | Round 1 | Round 2 | Round 5 |
|---|---|---|---|
| three ids survive, corrected not recreated | ❌ recreated | ⚠️ survive, uncorrected | ✅ |
| `2a368f39` canonical → "Davide" | ❌ | ❌ | ✅ |
| no `:Entity` named "David" remains | ❌ | ❌ | ❌ |
| no add→forget loop | ❌ | ✅ | ✅ |

Round 5 is twelve tool calls: three `get_entity`, one `search`, one `forget` (**succeeded** —
`{"deleted": "3700…", "removed_node": false}`), one `update` that corrected the preference text
still reading "User David". No `add_*` at all. Every write verb that was refused two rounds ago
now works.

**Why the third criterion still fails, and it is two different things now:**

1. The agent handled `37003044` and stopped, leaving `20d7511c` untouched — its third
   `get_entity` for "David" came back `{"found": false}` while another call had just returned
   `37003044` under that name. `memory_get_entity` searches with `limit=1`, so a name with two
   matches yields whichever ranks first and the second is invisible to that verb. This is a
   completeness gap in the tooling/agent, not a refusal.
2. **Forget does not remove the entity from the caller's own reads** (M-9). Measured
   immediately after the successful forget: `owned=0, mentioned=1, scope_count=1` — ownership
   is gone, but `ENTITY_IN_USER_SCOPE` also accepts `MENTIONS`, so search, `get_entity`,
   `get_context` and the recalled-context block can all still surface it. The user is told
   "deleted" about something their next turn can still read back.

**And a restart hazard the runs exposed** (M-8): round 4 had to be discarded because every
memory call returned `http 400` after the sidecar was rebuilt — the daemon's streamable-HTTP
session dies with the sidecar and "reconnect-on-use" does not renegotiate it. `aura` must be
restarted after ANY sidecar restart. The boot log still reports `tools=13`, so nothing warns you.

---

## Controprova round 2 (2026-07-25 14:30 UTC) — FAILED, one layer up

Run as the operator (`e3c8eb3b…`, the identity that owns the entities) through the cockpit's own
API, using the Authula login `web/e2e/auth.ts` and `scripts/agui_smoke.sh` already implement —
the memory tools are identity-scoped, so a run as anyone else cannot see the data under test.
Prompt, in plain Italian: *"Nella tua memoria il mio nome risulta sbagliato e ci sono dei
doppioni. Sistema la tua memoria."* Thread `019f99af-22ea-7e28-985e-98878f577cb1`, 26 tool calls.

| Criterion | Verdict |
|---|---|
| the three ids survive, corrected not recreated | ⚠️ survive, **not corrected** — nothing was destroyed and `add_entity` merged into `2a368f39` rather than minting a node, but no correction landed |
| `2a368f39`'s canonical becomes "Davide" | ❌ still "David" |
| no `:Entity` named "David" remains | ❌ both duplicates remain |
| no add→forget loop | ✅ one forget, one add, terminated cleanly (baseline: repeated cycles) |

**Every write verb was refused**, three times with `{"updated": null, "reason": "not found or
not owned by this user"}` — see M-7 in the plan. `memory_update` was missing from the bridge's
identity-injection list (`internal/agent/mcptools/bridge.go:144`), so the sidecar received
`user_identifier=null` and refused the operator's own node. The fix is written, race-tested and
lint-clean in an isolated worktree; it **cannot be committed until the `internal/adaptive` lint
goes green**. Patch: `D:/tmp/bridge-user-identifier.patch`.

Two things this run earned that the first one could not:

- **The destructive behaviour did not recur.** The agent no longer deletes what it just wrote,
  because `memory_get_entity` finally tells it the truth about what is stored. That was the
  actual hypothesis under test, and it held.
- **A refused correction verb still costs data quality.** Denied `memory_update` on the fact
  `David → expertise → Go, AI agents`, the agent fell back to `add_fact` and created
  `Davide → expertise → Go, AI agents` beside it (`ca17ee4e` next to `325b20fd`). The refusal
  did not merely block a fix, it manufactured a duplicate — the same shape as the original
  add/forget loop, one layer up.

**And it reported success it had not achieved.** The reply opens with *"Fatto. Ecco cosa ho
sistemato"* and claims the canonical is now aligned; the graph is byte-for-byte unchanged. It
had narrated each refusal honestly mid-turn and then summarised past them. This is exactly why
the pass criteria are `tool_invocations` + the graph and never the reply.

**Still true after the run:** the two duplicates have **no `HAS_ENTITY` edge**, so
`memory_forget` refuses them even with a correct identity — `a79a8df43` fixed the write path for
entities created from now on and **never backfilled the rows it was diagnosed from**. They are
reachable via `MENTIONS`, so once M-7 lands `memory_update` can rename them; deletion needs the
ownership backfill. They also have no embedding, so `memory_search` cannot see them at all (M-5).

---

## The controprova — round 1: designed, run, FAILED for a reason worth keeping

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

## The suite the sidecar tests are not in

`ci.yml` builds `docker/agent-memory` into an image and runs Aura's `memory_integration` tier
against it — but **nothing ever runs the fork's own 53 pytest tests**. They pass only when
someone runs them by hand, and there is no runner image: the published image carries no `tests/`
and no pytest. The invocation that works, for whoever wires it up:

```
docker run --rm -v /d/Aura/docker/agent-memory:/work -w /work -e PYTHONPATH=/work/src \
  aura-agent-memory-mcp:local sh -c 'pip install -q pytest pytest-asyncio pytest-timeout;
  python -m pytest tests -q'
```

`PYTHONPATH=/work/src` matters: the image installs the package editable from its own baked
`/app/src`, so without it the run silently tests the image's copy instead of the working tree.

---

## Method note worth carrying forward

Every defect that mattered today was found by **using** the system, not by reading it: mounting the memory MCP as a client and creating one test entity exposed that reads lied about names, that canonical resolution crossed user scopes, and that every `*_SCOPED` query failed to scope. The 38-test suite was green throughout and structurally could not see any of it — it uses in-memory fake clients that never execute Cypher. Two of today's diagnoses (mine on the loop's cause, the audit's on `AURA_CONTEXT_MEMORY_RECALL` being off) were **wrong and corrected by evidence**; both are recorded as such in the plan.
