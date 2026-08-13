# BUG: runtime turn appends leave `parent_seq` NULL → branch continuation loses history

**Status:** CONFIRMED from source (not reproduced on a live stack — this environment has
no Postgres/sqlc/migrate/docker). Fix + regression test are ready to execute on the stack.
**Severity:** correctness / context-loss on the edit-a-message / regenerate feature. No
data is destroyed (turns remain in the table); the model's *view* of a continued branch is
truncated. Linear chat is unaffected.

---

## Summary

Every conversation turn appended at runtime **after migration 0017 shipped** has
`parent_seq = NULL`. The branch-path loader (`LoadManagedHistoryForBranch`) reconstructs a
selected branch by walking `parent_seq` leaf→root; a NULL breaks the chain. So when an
operator edits or regenerates a message (which forks a sibling branch and re-runs over the
new leaf), the reconstructed history collapses to just `[system head, forked turn]` — the
entire prior conversation is missing from the model's context for that turn.

## Mechanism (file:line evidence)

1. **`parent_seq` is nullable with no default; the backfill only touched pre-existing rows.**
   `internal/db/migrations/0017_conversation_turn_branches.up.sql:30-38`:
   ```sql
   ALTER TABLE aura.conversation_turns ADD COLUMN parent_seq integer;   -- nullable, no default
   UPDATE aura.conversation_turns SET parent_seq = seq - 1 WHERE seq > 1; -- only rows present at migration time
   ```
   `branch_id` got a constant default (`'000…0'` canonical), so it is always correct;
   `parent_seq` did not.

2. **The runtime append path never sets `parent_seq`.**
   `InsertConversationTurn` writes 12 columns and omits `parent_seq`/`branch_id`
   (`internal/db/queries/conversation_turns.sql:1-6`), and `appendTurnWrites` builds
   `sqlc.InsertConversationTurnParams` with no `parent_seq` field
   (`internal/conversations/store_append.go:241-256`). Result: every appended turn gets
   `parent_seq = NULL` (and `branch_id = canonical` by default).

3. **Fork inherits the NULL.**
   `ForkBranch` sets the new turn's `parent_seq = diverge.ParentSeq`
   (`internal/conversations/store_branch.go:299`, "same parent as the diverging turn → a
   sibling"). For a post-0017 diverging turn that value is NULL, so the forked turn becomes
   a **branch root**.

4. **The branch walk terminates at the NULL.**
   `ListRecentTurnsByBranchPath` / `ListTurnsByBranchPath`
   (`internal/db/queries/conversation_turns.sql`) recurse via
   `JOIN path p ON t.seq = p.parent_seq`. A NULL `parent_seq` on the leaf yields no join
   row, so the walk returns only the leaf; the CTE's separate `head` adds the system turn.
   Net loaded history for the continued branch = `[system, forked turn]`.

5. **Reachability.** `loadTurnHistory` uses `LoadManagedHistoryForBranch` only when
   `branchLeaf > 0` (`internal/runner/runner_context.go`, the branch-selected path);
   `ForkBranch`'s own doc comment states the caller "re-runs over
   `LoadManagedHistoryForBranch(newLeaf)`". Linear chat uses `LoadManagedHistory`
   (`ListRecentTurnsBySeq`, `ORDER BY seq`, no `parent_seq`) and is unaffected — which is
   why the bug is invisible in ordinary use.

## Why CI is green — the test masks it

The branch integration fixtures append via `AppendTurn` and then **manually** chain the
pointers the production path never sets. `seedLinear`
(`internal/conversations/store_branch_fork_test.go:25-40`) calls
`chainCanonical(t, s, convID, 1, 2, 3)`, and `chainCanonical`
(`internal/conversations/store_branch_test.go:28-36`) does:
```go
parent := seq - 1
s.SetBranchPointers(ownerCtx(), convID, seq, CanonicalBranchID, parent)
```
So every branch test establishes the `parent_seq = seq-1` chain by hand. Production never
calls `chainCanonical` (or any equivalent), so the tests validate a state the real append
path does not produce.

## Fix (root cause)

The append path must maintain the canonical `parent_seq = seq-1` chain (root `seq=1` keeps
NULL), exactly as the 0017 backfill did.

**Recommended — set it in the INSERT (one write, cleanest):**
- Add `parent_seq` to `InsertConversationTurn` (`conversation_turns.sql`): a new bound
  param, value `NULLIF(seq - 1, 0)` (NULL for `seq = 1`, `seq-1` otherwise) — or compute it
  in Go.
- Add the field to `sqlc.InsertConversationTurnParams` via **`sqlc generate`** (offline,
  but sqlc must be installed — `make tools`), and set it in `appendTurnWrites`.
- `branch_id` stays the canonical default (already correct).

**Alternatives (if sqlc regen is undesirable):**
- **Extra UPDATE in the append tx:** after `insertTurnAndAggregates`, call the existing
  `SetTurnBranchPointers` query with `(seq, CanonicalBranchID, seq-1)` for `seq > 1`. Uses
  already-generated code (no regen) but adds a write per turn.
- **DB trigger (migration-only):** a `BEFORE INSERT` trigger setting
  `NEW.parent_seq = NEW.seq - 1` when NULL and `NEW.seq > 1`. No Go/sqlc change, but
  triggers are not an established pattern in this codebase — confirm the convention first.

Prefer the INSERT fix; it is the least surprising and matches the existing column-driven
write path.

## Regression test (removes the mask)

Add a fork test whose fixture seeds **only** via `AppendTurn` (NO `chainCanonical`) and
asserts the continued branch keeps the full prior history:
```
seed: AppendTurn seq 1 (system), 2 (user), 3 (assistant)   // no chainCanonical
fork: ForkBranch(divergeSeq=2, "edited question")          // new leaf, fresh branch
load: LoadBranchHistory(newLeaf)
assert: len == 3  → [system, edited user, ...]  and the pre-edit context is present
        (BEFORE the fix this returns len 2 = [system, edited turn] — the bug)
```
Keep the existing `chainCanonical`-based tests (they still pass post-fix) but this new test
must FAIL before the fix and PASS after. Also consider deleting `chainCanonical` from
`seedLinear` once the append path chains automatically, so the suite exercises the real
production state.

## Deeper issue to flag separately (NOT this fix)

Post-fork **continuation** also looks broken independently: turns appended after a fork go
through `AppendTurn`, which writes `branch_id = canonical` (default) and (today) NULL
`parent_seq`. So new turns land on the **canonical** branch, not the forked sibling — a
continued fork does not actually extend its own branch. Fixing the `parent_seq` chain does
not address this; branch-aware appends (carry the active `branch_id` + `parent_seq =
previous leaf`) are a separate design change. Verify against product intent before treating
as a bug.

## Verification checklist (on the stack)

```
make tools                    # sqlc, migrate present
sqlc generate                 # if taking the INSERT fix; go build ./...
go test -tags db_integration -race ./internal/conversations -run 'TestBranchFork|TestBranchPath' -count=1
# the new no-chainCanonical regression test must pass; existing branch tests stay green
bash scripts/coverage_docker.sh   # owned-surface floor >=85%
```
Then update `docs/aura-quality-snapshot.md` for the conversations rows and record the fix.
