---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 11
type: execute
wave: 5
depends_on: ["37F-08"]
files_modified:
  - internal/share/expirer.go
  - internal/cron/handlers/share_expiry.go
  - internal/cron/handlers/share_expiry_test.go
  - internal/runner/runner_delete.go
  - internal/runner/runner_delete_share_test.go
  - internal/share/expirer_integration_test.go
autonomous: true
requirements: [WEBSHARE-02, WEBSHARE-03]

must_haves:
  truths:
    - "Deleting a conversation revokes its shares and drops their Garage bytes BEFORE the persistence delete"
    - "The expiry sweep expires due links and drops their bytes, and is idempotent across re-runs"
    - "A nil expirer yields a disabled no-op sweep, not a panic"
    - "An expired link never resurrects — once resolve 404s for expiry, it 404s forever"
    - "The sweep drops blobs first, then stamps the row — a crash between the two re-runs the idempotent delete rather than orphaning bytes"
    - "internal/cron/handlers does not import internal/share; internal/runner does not import internal/share"
  artifacts:
    - path: "internal/share/expirer.go"
      provides: "ExpireDue — the batch expiry the cron handler drives"
      min_lines: 40
    - path: "internal/cron/handlers/share_expiry.go"
      provides: "KindShareExpirySweep + ShareExpirer seam + NewShareExpiryHandler"
      min_lines: 25
    - path: "internal/runner/runner_delete.go"
      provides: "step 4.5 — revoke shares + drop blobs before the persistence delete"
      contains: "share"
  key_links:
    - from: "internal/cron/handlers/share_expiry.go"
      to: "newCountingSweep"
      via: "the single shared counting-sweep implementation"
      pattern: "newCountingSweep"
    - from: "internal/runner/runner_delete.go"
      to: "a consumer-declared share revoker seam"
      via: "step 4.5, before step 5's DeleteForIdentity"
      pattern: "step 4.5|4\\.5"
  prohibitions:
    - "MUST NOT write a bespoke shareExpiryHandler struct — sweep.go exists precisely so parallel handlers are not duplicated; a bespoke type violates that file's stated reason for existing"
    - "MUST NOT import internal/share from internal/cron/handlers or internal/runner — declare consumer-side seams (the IdentityPurger pattern)"
    - "MUST NOT stamp the row before dropping the blobs — that orphans bytes permanently on a crash"
    - "MUST NOT make the sweep the security gate — lazy enforcement in ResolveByToken is the gate; the sweep is byte reclamation only"
    - "MUST NOT block a conversation delete on a transient share-revoke failure — the FK cascades the row; WARN and continue"
    - "MUST NOT let okFmt carry other than exactly one %d — sweep.go:30 states this as a contract"
    - "MUST NOT set ReschedulesOnRecovery true — the sweep is idempotent and the next tick re-evaluates the same due set"
    - "MUST NOT put more than one build tag on any test file"
---

<objective>
Close the two lifecycle paths: the expiry sweep (byte reclamation) and revoke-on-conversation-delete
(D-15).

The division of labour is the point, and it is easy to get backwards:
- **Lazy enforcement in `ResolveByToken` is the security gate** (plan 37F-07). An expired link 404s even
  if the scheduler is down.
- **The sweep is the garbage collector.** Without it, an expired link is *unreachable* but its Garage
  bytes live forever — unbounded storage growth, and redacted-but-real user content persisting in object
  storage indefinitely past its stated expiry. That is a data-retention problem, not a cost one, and it
  is why D-09's "revoke/expiry drops the bundled copy" is unmet without a sweep.

R-10 is the trap on the delete path: `shared_links.conversation_id ON DELETE CASCADE` removes the **row**
but not the **Garage objects**. A share whose row is cascaded away leaves blobs with nothing pointing at
them — unreclaimable even by the row-scanning sweep. So the lifecycle hook is mandatory: FK = belt,
hook = suspenders.

Purpose: bytes actually go away.
Output: `expirer.go`, `share_expiry.go`, and step 4.5 of `runner_delete.go`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@internal/share/service.go
@CLAUDE.md
</context>

## Artifacts this plan produces

`share.ExpireDue`, `handlers.KindShareExpirySweep` (`"share_expiry_sweep"`), `handlers.ShareExpirer`,
`handlers.NewShareExpiryHandler`, and `runner_delete.go` step 4.5.

<tasks>

<task type="auto">
  <name>Task 1: share.ExpireDue + the cron sweep handler (copy-and-rename identity_purge.go)</name>
  <read_first>
    - `internal/cron/handlers/identity_purge.go` — **the whole file (41 lines). This is the closest analog in the phase: copy it and rename.** Preserve four non-obvious bits: the seam is declared HERE consumer-side; the `var seam sweepFn; if x != nil { seam = x.Method }` nil-guard idiom; `okFmt` carrying exactly one `%d`; and the `ReschedulesOnRecovery: false` justification.
    - `internal/cron/handlers/sweep.go:28-60` — `newCountingSweep`. **Its header (`:34-45`) is the "extract a helper, never duplicate" precedent to cite**: it says outright that ONE `countingSweepHandler` parameterized by Kind/seam/messages exists rather than two parallel handler types. A bespoke `shareExpiryHandler` would violate this file's stated reason for existing. `:30` states the one-`%d` contract; `:48-49` already contains the ReschedulesOnRecovery wording to reuse; `:57-59` turns a nil seam into a disabled no-op.
    - `internal/share/store.go` — `DueForExpiry` + `RevokeForIdentity` (plan 37F-07); `internal/share/bundle.go` — `dropBlobs` (plan 37F-08). Read the real signatures.
    - `internal/db/migrations/0040_shared_links.up.sql` — confirm `share_expiry_sweep` is in the kind CHECK (plan 37F-02).
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ3 — the ordering rule (drop blobs, THEN stamp) and why the reverse orphans bytes permanently
  </read_first>
  <action>
    Create `internal/share/expirer.go`, package `share`:
    `ExpireDue(ctx context.Context, now time.Time) (int, error)` on the `Service`.

    Order, and this is the whole correctness argument — document it as numbered steps matching the body
    (the `runner_delete.go` discipline): **1. drop the blobs → 2. stamp the row → 3. audit `expire`.**
    Never stamp-then-delete. A crash between 1 and 2 re-runs the (idempotent) delete on the next tick; a
    crash after a stamp-then-delete orphans the bytes permanently, because the sweep scans rows and a
    stamped row is no longer due.

    Make it **idempotent + resumable**: re-running over an already-swept link is a no-op, not an error.
    Emit one `share_audit` `expire` row per link (D-14 names `expire` as an audited action).

    Create `internal/cron/handlers/share_expiry.go` by **copying `identity_purge.go` and substituting**
    `Share`/`share_expiry_sweep`/`ExpireDue`. Declare `ShareExpirer` (with an `ExpireDue` method)
    **in this package, consumer-side** — `handlers` must never import `internal/share` (the reverse-import
    rule; the live `*share.Service` satisfies the seam at the composition root). Use
    `newCountingSweep(KindShareExpirySweep, shareExpiryMaxDuration, seam, "share expiry: disabled (no
    expirer)", "share expiry", "share expiry ok: expired %d link(s)")` — note `okFmt` carries **exactly
    one** `%d`. `shareExpiryMaxDuration` is 5 minutes, mirroring `identityPurgeMaxDuration`; document the
    budget the same way (a small indexed query plus idempotent per-link teardown).

    Do **not** write a bespoke handler struct.

    Write `internal/cron/handlers/share_expiry_test.go`, **no build tag** (pure unit):
    `TestShareExpiryDisabled` — a nil expirer yields a disabled no-op, not a panic (VALIDATION.md's name).
    Also assert `ReschedulesOnRecovery` is false and that `okFmt` formats with one count.
  </action>
  <verify>
    <automated>go build ./... && go vet ./internal/share/ ./internal/cron/handlers/ && go test ./internal/cron/handlers/ -run 'TestShareExpiry' -count=1 && go list -deps ./internal/cron/handlers/ | grep -qE "aura/internal/share$" && { echo "FAIL: handlers must not import internal/share"; exit 1; } || echo "SEAM-OK"</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/cron/handlers/ -run 'TestShareExpiry' -count=1` passes; `TestShareExpiryDisabled` proves a nil expirer is a no-op, not a panic.
    - **No reverse import:** `go list -deps ./internal/cron/handlers/ | grep -E "aura/internal/share$"` returns NOTHING.
    - **No duplicated handler:** `grep -c "newCountingSweep" internal/cron/handlers/share_expiry.go` returns `1`, and `grep -cE "^type shareExpiry.*struct" internal/cron/handlers/share_expiry.go` returns `0`.
    - `grep -c "%d" internal/cron/handlers/share_expiry.go` — the `okFmt` string carries exactly one.
    - `KindShareExpirySweep` equals `"share_expiry_sweep"` and matches the migration's CHECK member exactly: `grep -q "share_expiry_sweep" internal/db/migrations/0040_shared_links.up.sql`.
    - **Ordering:** in `ExpireDue`, the `dropBlobs` call is at a lower line number than the revoke/stamp call.
    - `internal/cron/handlers/share_expiry_test.go` carries no build tag.
    - `golangci-lint run ./internal/share/ ./internal/cron/handlers/` reports 0 issues.
  </acceptance_criteria>
  <done>`ExpireDue` drops blobs before stamping and is idempotent; the cron handler is a ~20-LOC copy-and-rename over `newCountingSweep` with a consumer-declared seam, a nil-safe disabled path, and no reverse import.</done>
</task>

<task type="auto">
  <name>Task 2: runner_delete.go step 4.5 — revoke shares + drop blobs before the persistence delete</name>
  <read_first>
    - `internal/runner/runner_delete.go` — **the whole file (100 LOC). The analog is the file itself.** The insertion point is between `:70` (step 4) and `:73` (step 5). Two things must be edited **in the same commit** because the doc list is load-bearing here, not decorative: the numbered doc list at `:31-37` and the numbered body steps at `:51-73`.
    - `internal/runner/runner_delete.go:56-61` — **step 2 is the pattern to follow, NOT step 5.** Share revoke is the same class as pause-expiry: the FK `ON DELETE CASCADE` backstops the row, so a transient failure is a `slog.Warn`, never a blocked delete. Read its exact comment wording.
    - `internal/runner/runner_delete.go:44-50` — the owner gate that runs FIRST; step 4.5 sits after it and inherits it.
    - `internal/runner/runner_delete.go:91-99` — `tools.SessionJobTerminator` via a type assertion: the file's own model for a consumer-declared seam. Copy it — `runner` must NOT import `internal/share`.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §R-10 — the FK removes the row but NOT the Garage bytes; a skipped hook orphans blobs unreclaimable even by the sweep
  </read_first>
  <action>
    Extend `Runner.DeleteConversationLifecycle` with **step 4.5**, inserted between step 4 (terminate bg
    jobs) and step 5 (delete persistence). The owner gate at `:44-50` already ran, so this step inherits it.

    Step 4.5: revoke the conversation's shares and drop their Garage bytes. Follow **step 2's pattern, not
    step 5's** — best-effort: on a transient failure, `slog.Warn` and continue; never block the delete.

    But R-10 makes the comment different from step 2's, and it must say so. Step 2's comment reads
    *"Best-effort: the paused_states rows FK-cascade with the conversation on delete, so a transient
    failure here is a WARN, never a blocked delete."* 37F's version must state the **inversion**: the
    `shared_links` row FK-cascades, **but the Garage bytes do NOT** — so a WARN here orphans blobs, and the
    sweep's prefix reconcile is the backstop. Write that; it is the non-obvious part, and a reader who
    assumes the FK covers everything will delete this step as redundant.

    Declare the revoker as a **consumer-side seam** in `internal/runner` (a narrow interface with the one
    method), satisfied by the live `*share.Service` at the composition root. Follow the
    `tools.SessionJobTerminator` type-assertion model already in the file at `:91-99`. **`runner` must not
    import `internal/share`.** A nil seam ⇒ the step is skipped silently (share is optional in a
    deployment that never mounted it), not a panic.

    Update the doc list at `:31-37` in the SAME commit to include step 4.5 (CLAUDE.md: comments-updated in
    the same commit). Refactor-on-touch: dead code, dupl, ≤600 LOC.
  </action>
  <verify>
    <automated>go build ./... && go vet ./internal/runner/ && golangci-lint run ./internal/runner/ && go list -deps ./internal/runner/ | grep -qE "aura/internal/share$" && { echo "FAIL: runner must not import internal/share"; exit 1; } || echo "SEAM-OK"</automated>
  </verify>
  <acceptance_criteria>
    - `go build ./... && go vet ./internal/runner/` clean; `golangci-lint run ./internal/runner/` reports 0 issues.
    - **No reverse import:** `go list -deps ./internal/runner/ | grep -E "aura/internal/share$"` returns NOTHING.
    - **Ordering:** the share-revoke step appears at a lower line number than the `DeleteForIdentity` (step 5) call.
    - The doc list at the top of the function names step 4.5: `grep -c "4.5" internal/runner/runner_delete.go` returns ≥ 2 (the doc list AND the body step).
    - The step-4.5 comment states the R-10 inversion (the row cascades, the bytes do not): `grep -ciE "byte|blob|garage" internal/runner/runner_delete.go` returns ≥ 1.
    - A transient failure is a WARN, not a return: `grep -A4 "4.5" internal/runner/runner_delete.go | grep -q "slog.Warn"`.
    - A nil seam is handled: no panic path when the revoker is absent.
    - `internal/runner/runner_delete.go` ≤ 600 LOC.
  </acceptance_criteria>
  <done>Step 4.5 revokes shares and drops their blobs before the persistence delete, best-effort with a WARN, behind a consumer-declared seam with no reverse import, and the numbered doc list is updated in the same commit with the R-10 inversion stated.</done>
</task>

<task type="auto">
  <name>Task 3: lifecycle integration tests — sweep, cascade, monotonicity</name>
  <read_first>
    - `internal/share/service_integration_test.go` (plan 37F-08) and `internal/share/store_integration_test.go` (37F-07) — reuse their seeding helpers; do not duplicate.
    - `internal/agui/server_integration_test.go` — the shared `envOrSkip`
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — the exact names: `TestShareExpirySweep`, `TestDeleteLifecycleRevokesShares`, `TestSharedLinksCascade`; and §"Property-based testing" — the **expiry monotonicity** property (once resolve 404s for expiry, it 404s for all later t — guards clock-skew bugs)
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §R-10 — the orphan path this plan's tests must pin
  </read_first>
  <action>
    Create `internal/share/expirer_integration_test.go` and `internal/runner/runner_delete_share_test.go`,
    each with the **single** build tag `db_integration`, using `objectstore.NewFake()` and provisioned
    **non-wildcard** identities (R-13).

    In `internal/cron/handlers/` (or `internal/share/`, wherever the seam is drivable without a reverse
    import — state the choice in the SUMMARY):
    - `TestShareExpirySweep` — seed two due links and one not-due; run the sweep; assert only the due ones
      are stamped, their blobs are gone (`FakeStore.List(prefix)` empty), the not-due link is untouched and
      still resolves, and an `expire` audit row exists per expired link.
    - `TestShareExpirySweepIdempotent` — running the sweep twice yields the same state and no error; the
      second run expires 0.

    In `internal/share/`:
    - `TestPropertyExpiryMonotonicity` — the VALIDATION property: once `ResolveByToken` 404s for expiry at
      time t, it 404s for every t' > t. Drive it by inserting links with a range of past `expires_at`
      values and asserting the resolve never succeeds. This guards the clock-skew class.

    In `internal/runner/`:
    - `TestDeleteLifecycleRevokesShares` — **the D-15 core**: a conversation with an active public share;
      call `DeleteConversationLifecycle`; assert (a) the share's blobs are gone from the fake store, (b) the
      token no longer resolves, and (c) — the ordering claim — the blob drop happened **before** the
      persistence delete. Prove (c) rather than assuming it: a fake store that records the call order, or a
      revoker seam that records when it fired relative to the conversation row disappearing. A test that
      only checks the end state would pass a stamp-then-delete implementation that orphans bytes.
    - `TestDeleteLifecycleShareRevokerNil` — a nil revoker ⇒ the delete still succeeds (share is optional).
    - `TestDeleteLifecycleShareRevokeFailureDoesNotBlock` — a revoker returning an error ⇒ the conversation
      is still deleted (best-effort, WARN), because the FK backstops the row.

    **Exactly one build tag per file.**
  </action>
  <verify>
    <automated>go test -tags db_integration -race -p 1 -count=1 ./internal/share/ ./internal/runner/ ./internal/cron/handlers/ && go test ./internal/share/ ./internal/runner/ ./internal/cron/handlers/ -count=1</automated>
  </verify>
  <acceptance_criteria>
    - `go test -tags db_integration -race -p 1 -count=1 ./internal/share/ ./internal/runner/ ./internal/cron/handlers/` passes with a **non-sub-second** runtime.
    - The untagged suites still pass.
    - Every new test file carries **exactly one** build tag: `head -1` on each is `//go:build db_integration`; `grep -rn "garage_integration\|authula_integration\|musr_e2e\|docker_integration" internal/share/ internal/runner/ internal/cron/handlers/` returns NOTHING.
    - **`TestDeleteLifecycleRevokesShares` asserts the ORDER, not just the end state** — the blob drop is observed to precede the persistence delete. This is the assertion that distinguishes a correct implementation from an orphaning one.
    - `TestShareExpirySweepIdempotent` asserts the second run expires 0 and errors none.
    - `TestPropertyExpiryMonotonicity` exercises a range of past expiry values, not a single one.
    - `TestDeleteLifecycleShareRevokeFailureDoesNotBlock` asserts the conversation IS deleted despite the error.
    - `internal/share`, `internal/runner`, `internal/cron/handlers` each ≥ 85% coverage under the gate tags.
  </acceptance_criteria>
  <done>The sweep expires only due links, drops their bytes, audits each, and is idempotent; expiry is proven monotonic; and the delete lifecycle is proven to drop blobs *before* the persistence delete, to survive a nil revoker, and to not block on a revoke failure.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| a stated expiry → actual byte deletion | Lazy enforcement makes an expired link unreachable; only the sweep makes its content *gone*. The gap between "unreachable" and "deleted" is a data-retention exposure, not a cost one. |
| conversation delete → share bytes | The FK reaches the row. Nothing but the lifecycle hook reaches the bytes. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-07 | Information Disclosure | orphaned Garage blobs surviving revoke/delete (R-10) | mitigate | `runner_delete.go` step 4.5 drops bytes before the persistence delete (order asserted by test); the FK is the row backstop only. The step's comment states the inversion so it is not deleted as redundant. |
| T-37F-53 | Information Disclosure | redacted-but-real content persisting past its stated expiry | mitigate | `share_expiry_sweep` reclaims bytes on a 5-minute-bounded tick; `TestShareExpirySweep` asserts the blobs are gone, not just the row stamped. |
| T-37F-54 | Tampering | bytes permanently orphaned by a crash mid-sweep | mitigate | Drop-then-stamp ordering: a crash re-runs the idempotent delete next tick. Stamp-then-delete would leave a non-due row pointing at nothing. Order is grep- and test-asserted. |
| T-37F-36 | Elevation of Privilege | treating the sweep as the expiry gate | mitigate | The gate is lazy (`ResolveByToken`, plan 37F-07). `TestShareExpiredLazy404` runs with no sweep; this plan's tests cover reclamation only. Documented in both files. |
| T-37F-55 | Denial of Service | a share-revoke failure blocking conversation deletion | mitigate | Best-effort WARN + continue (step 2's pattern); `TestDeleteLifecycleShareRevokeFailureDoesNotBlock`. |
| T-37F-56 | Denial of Service | a nil seam panicking the sweep or the delete | mitigate | `newCountingSweep` turns a nil seam into a disabled no-op (`sweep.go:57-59`); the runner skips step 4.5 when the revoker is absent. Both tested. |
| T-37F-37 | Tampering | clock skew resurrecting an expired link | mitigate | `TestPropertyExpiryMonotonicity`; the resolve predicate uses the DB clock (plan 37F-07). |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | Existing deps only. |
</threat_model>

<verification>
- `go build ./... && go vet ./internal/share/ ./internal/runner/ ./internal/cron/handlers/`
- `go test ./internal/share/ ./internal/runner/ ./internal/cron/handlers/ -count=1` (untagged)
- `go test -tags db_integration -race -p 1 -count=1 ./internal/share/ ./internal/runner/ ./internal/cron/handlers/`
- `go list -deps ./internal/cron/handlers/ ./internal/runner/ | grep aura/internal/share$` → **no match**
- `golangci-lint run ./internal/share/ ./internal/runner/ ./internal/cron/handlers/` → 0 issues
- `bash scripts/check-file-size.sh` → 0
</verification>

<success_criteria>
Bytes actually go away: the sweep reclaims expired links' blobs idempotently on a bounded tick, and
deleting a conversation drops its shares' bytes *before* the row disappears — proven by an order
assertion, not an end-state check. Neither `handlers` nor `runner` imports `internal/share`. The sweep is
the GC and never the gate.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-11-SUMMARY.md` when done.
Record where the sweep test landed and the three packages' coverage numbers.
</output>
