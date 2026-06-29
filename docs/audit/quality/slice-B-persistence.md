# Quality Audit — Slice B: Persistence, knowledge, memory/learning, config, scheduler, identity

**Audit date:** 2026-06-29  
**Auditor:** Claude Code (read-only, Sonnet 4.6)  
**Scope:** `internal/db/`, `internal/conversations/`, `internal/askuser/`, `internal/toolinvocations/`, `internal/config/`, `internal/secret/`, `internal/knowledge/`, `internal/documents/` (read-only, uncommitted changes present), `internal/objectstore/`, `internal/cachemetrics/`, `internal/semindex/`, `internal/activelearn/`, `internal/reasoningstore/`, `internal/reasoninglearn/`, `internal/toolselectstore/`, `internal/toolselectlearn/`, `internal/identity/`, `internal/identityctx/`, `internal/cron/` (+ `handlers/`)  
**Lens:** maintainability / architecture (NOT security — that is covered by F-001..F-052 in docs/audit/)

---

## A. Slice Summary

| Category | Count |
|---|---|
| Total non-test, non-generated Go files surveyed | 62 |
| Files over 600 LOC | 0 |
| Largest file | `internal/config/config.go` — 556 LOC |
| Largest function | `loadBase()` in config.go — 159 LOC |
| TODO / FIXME / HACK comments | 0 |
| Findings total | 17 |
| Critical | 0 |
| High | 3 |
| Medium | 5 |
| Low | 8 |
| Info | 1 |

The slice-B surface is industrially well-formed: no file exceeds the 600-LOC cap, no orphan mutable globals after init, no TODO debt, and the Store pattern is consistently applied across identity/conversations/askuser/cachemetrics/cron. The dominant issue is **cross-package helper duplication** — functions that were verbatim-copied between store packages rather than extracted to a shared location. The learn/store package proliferation is intentional and correct by design (see Section E), but three helpers (`hashText`/`hashQuery`, `asString`, `asFloats`) are self-acknowledged copies that warrant extraction.

---

## B. Findings Table

### QA-B-01 — `asFloats` + `asString` copied verbatim between `reasoningstore` and `toolselectstore`

| Field | Value |
|---|---|
| Category | Code duplication |
| Severity | **High** |
| Confidence | High |
| Evidence | `internal/reasoningstore/store.go:85-121`, `internal/toolselectstore/store.go:120-156` |
| Symbols | `reasoningstore.asFloats`, `toolselectstore.asFloats`, `reasoningstore.asString`, `toolselectstore.asString` |

**Why:** `toolselectstore/store.go` carries the comment "Copied verbatim from reasoningstore" — self-documented duplication. Both functions implement the APOC-JSON-string / raw-`[]any` coercion for Neo4j embedding columns. If the coercion logic needs a fix (e.g., adding an `int32` branch), both files must be updated in sync. The CLAUDE.md rule "Never duplicate; extract a helper" applies directly.

**Action:** Extract `asFloats(v any) []float64` and `asString(v any) string` to a shared `internal/neostore` (or `internal/neoutil`) package. Both store packages import it. The two `GraphClient` interface declarations (finding QA-B-02) can move there too.

**Effort:** S  
**Safe cleanup strategy:** Extract to new package, update imports. Both functions are unexported today — the new package exports them. All callers are within Slice B. No migration, no behavior change.  
**Regression risk:** Low — pure data-conversion helpers, well-tested via E2E tests in both packages.

---

### QA-B-02 — `GraphClient` interface duplicated between `reasoningstore` and `toolselectstore`

| Field | Value |
|---|---|
| Category | Code duplication / architecture |
| Severity | **High** |
| Confidence | High |
| Evidence | `internal/reasoningstore/store.go:19-24`, `internal/toolselectstore/store.go:22-27` |
| Symbols | `reasoningstore.GraphClient`, `toolselectstore.GraphClient` |

**Why:** Identical interface — same method set (`Read`, `Write`), same signature, both satisfied by `*knowledge.Client`. The comment in `toolselectstore` says "Copied verbatim from reasoningstore (the proven seam)." Two definitions create two independent maintenance points: if `*knowledge.Client.Read` gains an additional context deadline variant, both must be updated independently.

**Action:** Move to the shared `internal/neostore` package (from QA-B-01) or to `internal/knowledge` itself (where `*Client` lives). Consumers import one interface.

**Effort:** S  
**Safe cleanup strategy:** Define once in `internal/knowledge` (it already owns `*Client`), or in `internal/neostore`. Update both store packages to import the shared interface. `*knowledge.Client` already satisfies it.  
**Regression risk:** Low — interface identity change only; Go structural typing makes this transparent to callers.

---

### QA-B-03 — `hashText`/`hashQuery` — three copies of `sha256.Sum256 + hex.EncodeToString`

| Field | Value |
|---|---|
| Category | Code duplication |
| Severity | **High** |
| Confidence | High |
| Evidence | `internal/activelearn/learner.go:113-116`, `internal/reasoningstore/store.go:80-83`, `internal/toolselectstore/store.go:115-118` |
| Symbols | `activelearn.hashText`, `reasoningstore.hashText`, `toolselectstore.hashQuery` |

**Why:** Three copies of a 4-line function. The function is semantically identical (SHA-256 of a string → hex). The names differ only because the argument semantics differ (`text` vs `query`), but the implementation is byte-identical. The `activelearn` copy is used for in-memory dedup (seen-set); the store copies are used as Neo4j MERGE keys. They represent the same content-addressing scheme.

**Action:** Export a single `ContentHash(s string) string` from `internal/activelearn` (the shared mechanism owner) and import it in both store packages. The `activelearn` package is already imported by `reasoninglearn` and `toolselectlearn`, which are the parents of the store packages — no new import cycle is introduced.

**Effort:** S  
**Safe cleanup strategy:** Export `ContentHash` from `activelearn`, replace all three unexported copies. Zero behavior change — SHA-256 is deterministic.  
**Regression risk:** Low.

---

### QA-B-04 — `numericFromFloat` + `floatFromNumeric` duplicated between `conversations` and `cachemetrics`

| Field | Value |
|---|---|
| Category | Code duplication |
| Severity | **Medium** |
| Confidence | High |
| Evidence | `internal/conversations/store_helpers.go:148-175`, `internal/cachemetrics/store_helpers.go:67-97` |
| Symbols | `conversations.numericFromFloat`, `conversations.floatFromNumeric`, `cachemetrics.numericFromFloat`, `cachemetrics.floatFromNumeric` |

**Why:** Both `conversations` and `cachemetrics` implement `numericFromFloat` (float64 → `pgtype.Numeric` at numeric(10,4) scale with range guard) and `floatFromNumeric` (inverse). The `cachemetrics` comment says "mirrors conversations.numericFromFloat". The two implementations are functionally identical; the error message wording differs slightly. If the `numeric(10,4)` scale changes, both must be updated. The `numericMaxCost` constant is also defined independently in both packages (`999999.9999` each).

**Action:** Extract to `internal/db` as `NumericFromFloat(f float64) (pgtype.Numeric, error)` and `FloatFromNumeric(n pgtype.Numeric) float64`, with the shared `numericMaxCost` constant. Both packages import `internal/db` already.

**Effort:** S  
**Safe cleanup strategy:** Add to `internal/db/numeric.go`, export, update both packages. No behavior change.  
**Regression risk:** Low — well-tested in both packages; existing tests validate round-trip.

---

### QA-B-05 — `parseUUID` defined in 5 separate packages

| Field | Value |
|---|---|
| Category | Code duplication |
| Severity | **Medium** |
| Confidence | High |
| Evidence | `internal/identity/store.go:221`, `internal/conversations/store_helpers.go:200`, `internal/askuser/store.go:407`, `internal/cron/store.go:335`, `internal/cachemetrics/store_helpers.go:54` (as `uuidFrom`), `internal/toolinvocations/store.go:156` (as `uuidParam`) |
| Symbols | Multiple |

**Why:** Six variants of "parse a UUID string into `pgtype.UUID`". The identity comment says "mirrors internal/identity.parseUUID"; conversations says "mirrors internal/identity.parseUUID + internal/askuser". Every new store package recreates this 5-line function. While the copies are trivially correct (they all call `uuid.Parse`), the proliferation violates CLAUDE.md "Never duplicate; extract a helper". The difference between `parseUUID(s string)` (identity, cron) and `parseUUID(field, s string)` (conversations, askuser) is just the error message prefix — this is a minor variance, not a real divergence.

**Action:** Export `ParseUUID(field, s string) (pgtype.UUID, error)` from `internal/db` (the home of all Postgres helpers). Packages that want the simpler 1-argument form call `db.ParseUUID("", s)`.

**Effort:** M (six callers to update)  
**Safe cleanup strategy:** Add to `internal/db`, migrate packages one at a time with tests passing at each step. No behavior change.  
**Regression risk:** Low — trivially correct function, well-tested indirectly.

---

### QA-B-06 — `postgresTextSafe` duplicated between `conversations` and `toolinvocations`

| Field | Value |
|---|---|
| Category | Code duplication |
| Severity | **Medium** |
| Confidence | High |
| Evidence | `internal/conversations/store_helpers.go` (NUL-stripping helper), `internal/toolinvocations/redact.go:93` |
| Symbols | `conversations.postgresTextSafe`, `toolinvocations.postgresTextSafe` (or local equivalent) |

**Why:** Both packages strip `\x00` bytes from strings before persisting to Postgres text columns. The function is identical in both; the string replacement constant (`"[NUL]"`) happens to match today but is maintained independently. This is the precise CLAUDE.md DRY violation: "Never duplicate; extract a helper."

**Action:** Move to `internal/db` as `PostgresTextSafe(s string) string`. Both packages already import `internal/db`.

**Effort:** S  
**Safe cleanup strategy:** Add to `internal/db`, replace both sites. Zero behavior change.  
**Regression risk:** Low.

---

### QA-B-07 — `MarkResumedBatch` bypasses `db.WithTx` pattern (comment mismatch)

| Field | Value |
|---|---|
| Category | Antipattern / leaky abstraction |
| Severity | **Medium** |
| Confidence | High |
| Evidence | `internal/askuser/store.go:287-326` |
| Symbols | `askuser.Store.MarkResumedBatch` |

**Why:** The `askuser` package doc states "multi-row atomic writes wrap db.WithTx" but `MarkResumedBatch` rolls its own `pool.Begin` / deferred `tx.Rollback` / `tx.Commit` path. The reason is legitimate: `db.WithTx` passes `*sqlc.Queries` to the callback but `MarkResumedBatch` needs raw `tx.Exec` to access `CommandTag.RowsAffected()` — a capability the sqlc wrapper discards. However, the package documentation is misleading (says "wrap db.WithTx" — it doesn't), and the manual transaction pattern is subtly different from `db.WithTx` in its panic-handling: `db.WithTx` re-panics after rollback, while `MarkResumedBatch`'s deferred closure does not recover panics at all (the outer defer fires on panic, calls `tx.Rollback`, but the panic propagates naturally — actually this is equivalent). The documentation mismatch is the real issue.

**Action:** Either (a) update the package doc to reflect that `MarkResumedBatch` uses a manual transaction (justified by RowsAffected need), or (b) add a `WithTxExec(ctx, pool, fn func(pgx.Tx) error) error` variant to `internal/db` that exposes the raw `pgx.Tx` for RowsAffected access, unifying the pattern. Option (a) is the minimal fix.

**Effort:** S  
**Safe cleanup strategy:** Doc-only change (option a) or small `db.WithTxExec` addition (option b). Option (b) enables `MarkResumedBatch` to be refactored to the canonical pattern.  
**Regression risk:** None for option (a). Low for option (b).

---

### QA-B-08 — `ListRecent` missing `int32` overflow guard (inconsistency with `ListPendingAll`)

| Field | Value |
|---|---|
| Category | Antipattern / potential integer overflow |
| Severity | **Medium** |
| Confidence | High |
| Evidence | `internal/askuser/store.go:227-232` vs `internal/askuser/store.go:193-200` |
| Symbols | `askuser.Store.ListRecent`, `askuser.Store.ListPendingAll` |

**Why:** `ListPendingAll` (line 196-198) guards `int32` narrowing: `if limit > 0 && limit <= math.MaxInt32 { lim = int32(limit) }` with an explicit note "CodeQL go/incorrect-integer-conversion". `ListRecent` (line 231) does `int32(limit)` directly after only a `limit <= 0 → 50` guard. No upper-bound check. A caller passing `limit = math.MaxInt` would produce a negative int32, causing a Postgres error (or at worst fetching all rows on some driver versions). The fix is trivially established by the `ListPendingAll` pattern in the same file.

**Action:** Apply the same `math.MaxInt32` guard to `ListRecent` at line 231.

**Effort:** S  
**Safe cleanup strategy:** Add `if limit > math.MaxInt32 { limit = 50 }` before the int32 cast, matching `ListPendingAll`.  
**Regression risk:** None — purely defensive; production callers never pass `math.MaxInt`.

---

### QA-B-09 — `loadBase` function at 159 LOC exceeds function size guidance

| Field | Value |
|---|---|
| Category | Antipattern / maintainability |
| Severity | **Low** |
| Confidence | High |
| Evidence | `internal/config/config.go:320-478` |
| Symbols | `config.loadBase` |

**Why:** `loadBase` is a 159-line struct literal that has grown with each phase (phase 7 web, phase 9 swarm, phase 11 skills, phase 12 AG-UI, phase 13 channels, phase 14 profile, etc.). The CLAUDE.md rule targets files >600 LOC (not functions), but a 159-line function is the primary maintenance hazard: every new slice adds more fields here, and the function will tip over 200 lines before the project completes. Readability deteriorates and it becomes hard to reason about default-value locality. Notably there is no branching complexity — just sequential env reads.

**Action:** Extract per-phase/per-domain blocks into unexported helpers called from `loadBase`, e.g.:
- `loadWebKnobs() webKnobs` (web-fetch, DNS pin, cache)
- `loadSchedulerKnobs() schedulerKnobs`
- `loadSkillsKnobs() skillsKnobs`
- `loadMultimodalKnobs() multimodalKnobs`

Each helper can be in its own `config_*.go` file, following the existing `config_env.go` / `config_mcp.go` split.

**Effort:** M  
**Safe cleanup strategy:** Refactor-only; no behavior change; tests remain identical.  
**Regression risk:** Low if done carefully (zero logic change, only structural).

---

### QA-B-10 — `RunDirWarnThresholdBytes` default defined twice (1 GiB literal vs named constant)

| Field | Value |
|---|---|
| Category | Code duplication / magic value |
| Severity | **Low** |
| Confidence | High |
| Evidence | `internal/config/config.go:378` (literal `1073741824`) and `internal/conversations/orphan_scan.go:23` (`defaultRunDirWarnThreshold = int64(1) << 30`) |
| Symbols | `config.loadBase`, `conversations.defaultRunDirWarnThreshold` |

**Why:** The config default and the `orphan_scan` constant represent the same semantic value (1 GiB warn threshold). If the value is changed in one place, the other silently diverges. The `orphan_scan.go` constant is correct Go idiom; the `config.go` literal should be a named constant referencing the same value.

**Action:** Export `DefaultRunDirWarnThresholdBytes = int64(1) << 30` from `internal/conversations` (or move to `internal/config`) and reference it from both sites.

**Effort:** S  
**Safe cleanup strategy:** Add the named constant, update both sites.  
**Regression risk:** None.

---

### QA-B-11 — Dev-default credentials in `config.go` will trip secret scanners

| Field | Value |
|---|---|
| Category | Antipattern / secret hygiene |
| Severity | **Low** |
| Confidence | High |
| Evidence | `internal/config/config.go:34-37` (`defaultObjectStoreAccessKey`, `defaultObjectStoreSecretKey`), line 473 (`"changeme-aura-pim-local"`) |
| Symbols | `config.defaultObjectStoreAccessKey`, `config.defaultObjectStoreSecretKey` |

**Why:** These are intentional dev-ergonomics defaults for Garage local dev, not real credentials. However, static secret scanners (GitHub, GitLeaks, govulncheck's secret patterns, the `semgrep-rule-creator` skill installed on this project) will flag them as hardcoded credentials in Go source. This is a false positive from a security standpoint but creates scanner noise that may mask real findings. The pattern used for SearXNG (`empty default → fail-soft at call time`) is cleaner.

**Action:** Change the Garage access/secret key defaults to empty strings. The objectstore client should detect empty keys and log a warning at boot (matching the SearXNG / CalendarMCPURL pattern). This does not break the Docker Compose stack — `compose.yaml` already passes `AURA_OBJECTSTORE_ACCESS_KEY` as a required variable.

**Effort:** S  
**Safe cleanup strategy:** Requires a config-loading + objectstore-construction change, but no migration. The compose hard-errors on the keys already (`:?` mandatory).  
**Regression risk:** Low — dev only needs to set the vars in `.env` (compose already provides them).

---

### QA-B-12 — `reasoningstore` E2E test uses bare `t.Skipf` without CI guard

| Field | Value |
|---|---|
| Category | Test gap / skip-as-green risk |
| Severity | **Low** |
| Confidence | High |
| Evidence | `internal/reasoningstore/store_e2e_test.go:36,52` |
| Symbols | `TestReasoningStoreE2E` |

**Why:** The `reasoningstore` E2E test (build tag `reasoning_live`) skips via `t.Skipf` when the granite sidecar or Neo4j is unreachable — without checking `$CI`. In contrast, `toolselectstore`'s equivalent test has a proper `envOrSkip` that `t.Fatalf`s under `$CI`. The `reasoningstore` skip will silently pass even in a CI environment where the sidecar should be up, masking a real failure.

**Action:** Mirror the `toolselectstore.envOrSkip` CI-guard pattern in `reasoningstore/store_e2e_test.go`: check `os.Getenv("CI") != ""` and call `t.Fatalf` (not `t.Skipf`) when the sidecar is expected to be running.

**Effort:** S  
**Safe cleanup strategy:** Test-only change.  
**Regression risk:** None — makes existing skip stricter in CI without changing local behavior.

---

### QA-B-13 — `secret.secretEnvMarkers` and `alwaysSecretEnvMarkers` share 17 identical entries

| Field | Value |
|---|---|
| Category | Code duplication |
| Severity | **Low** |
| Confidence | High |
| Evidence | `internal/secret/envkey.go:23-59` |
| Symbols | `secret.secretEnvMarkers`, `secret.alwaysSecretEnvMarkers` |

**Why:** `secretEnvMarkers` (19 entries) is a strict superset of `alwaysSecretEnvMarkers` (17 entries). The 17 shared entries must be kept in sync between two slice literals. A simpler structure: `alwaysSecretMarkers` + `credentialURLKeyMarkers` (2 entries), then `secretEnvMarkers = append(alwaysSecretMarkers, credentialURLKeyMarkers...)`.

**Action:** Refactor to define `alwaysSecretMarkers` once, then `secretEnvMarkers = append(alwaysSecretMarkers, credentialURLKeyMarkers...)`. This removes the duplication and makes the two-tier logic explicit in the variable declarations.

**Effort:** S  
**Safe cleanup strategy:** Behavior-preserving refactor; existing tests assert both `IsSecretEnvKey` and `IsSecretEnvVar`.  
**Regression risk:** None.

---

### QA-B-14 — `AURA_TOOLSELECT_ORACLE` not exported to compose.yaml (potential surprise in production)

| Field | Value |
|---|---|
| Category | Not-wired / config gap |
| Severity | **Low** |
| Confidence | Medium |
| Evidence | `internal/toolselectlearn/oracle.go:15` (defines `oracleEnv = "AURA_TOOLSELECT_ORACLE"`); not present in `compose.yaml` |
| Symbols | `toolselectlearn.oracleEnv` |

**Why:** `AURA_TOOLSELECT_ORACLE` defaults to ON. The kill-switch is documented in code and PRD but not wired into `compose.yaml` as a configurable knob. Operators who want to disable paid escalation in production have no obvious compose-level override. `AURA_LLM_REASONING_LEARNING` (a similar self-improvement gate) is already in compose.yaml (line 98). Consistency would suggest `AURA_TOOLSELECT_ORACLE` appears there too.

**Action:** Add `AURA_TOOLSELECT_ORACLE: ${AURA_TOOLSELECT_ORACLE:-on}` to the `aura` service env block in `compose.yaml`, next to `AURA_LLM_REASONING_LEARNING`.

**Effort:** S  
**Safe cleanup strategy:** Compose-only change; default ON preserves current behavior.  
**Regression risk:** None.

---

### QA-B-15 — Magic literals in `conversations/title.go` (unnamed size/count caps)

| Field | Value |
|---|---|
| Category | Antipattern / magic values |
| Severity | **Low** |
| Confidence | High |
| Evidence | `internal/conversations/title.go:74` (literal `500`), line `~90` (literal `6`) |
| Symbols | `conversations.renderHistoryForTitle` |

**Why:** Two unnamed magic literals in `renderHistoryForTitle`: a per-turn content cap of `500` characters and a max-turn count of `6`. Neither is a named constant. If these are ever tuned, the intent is not obvious from the literal alone. The surrounding code already uses well-named constants (`l2HeadroomTokens`, `l2MinOutputReservation`).

**Action:** Define `const titlePerTurnCap = 500` and `const titleMaxTurns = 6` at package level.

**Effort:** XS  
**Safe cleanup strategy:** Trivial rename.  
**Regression risk:** None.

---

### QA-B-16 — `CanonicalBranchID` is an exported mutable `var` (sentinel should be immutable)

| Field | Value |
|---|---|
| Category | Antipattern / mutable global |
| Severity | **Low** |
| Confidence | Medium |
| Evidence | `internal/conversations/store_branch.go:38` |
| Symbols | `conversations.CanonicalBranchID` |

**Why:** `var CanonicalBranchID = uuid.UUID{}` is exported and technically mutable (assignment produces a copy in Go, so this is not a data-race hazard in practice — `uuid.UUID` is `[16]byte`). However, signaling a constant-sentinel as an exported `var` creates a misleading contract: callers seeing `var` may assume it could be set to a different value. A function `CanonicalBranchSentinel() uuid.UUID { return uuid.UUID{} }` or a comment `// Do not assign — read-only sentinel` would make the immutability intent explicit.

**Action:** Add `// Do not assign — read-only sentinel representing the canonical branch.` comment to the var declaration.

**Effort:** XS  
**Safe cleanup strategy:** Comment-only change.  
**Regression risk:** None.

---

### QA-B-17 — `conversations` store default `TurnCapBytes` fallback (65536) duplicates config default silently

| Field | Value |
|---|---|
| Category | Code duplication / magic value |
| Severity | **Low** |
| Confidence | High |
| Evidence | `internal/conversations/store.go:89` (literal `65536`); `internal/config/config.go:375` (`envIntDefault("AURA_CONVERSATION_TURN_CAP_BYTES", 65536)`) |
| Symbols | `conversations.New`, `config.loadBase` |

**Why:** If the config default for `AURA_CONVERSATION_TURN_CAP_BYTES` is changed (e.g., to 131072), the fallback in `conversations.New` silently remains at 65536. The fallback is only reached when `TurnCapBytes <= 0`, which only happens in tests that construct `Store` without going through `config.Load`. Still a divergence risk.

**Action:** Define `const DefaultTurnCapBytes = 65536` in the `conversations` package and reference it from both `New` and document it as the config default. Config can import this constant to eliminate the duplicate literal.

**Effort:** XS  
**Safe cleanup strategy:** Named constant, zero behavior change.  
**Regression risk:** None.

---

## C. Quick Wins

These are safe, bounded changes that collectively reduce technical debt in under a sprint:

| ID | Action | Effort |
|---|---|---|
| QA-B-03 | Export `ContentHash(s string)` from `activelearn`, replace 3 copies | XS |
| QA-B-08 | Add `math.MaxInt32` guard to `ListRecent` (copy from `ListPendingAll`) | XS |
| QA-B-12 | Add CI guard to `reasoningstore` E2E `t.Skipf` | XS |
| QA-B-13 | Compose `secretEnvMarkers` from `alwaysSecretMarkers + credentialURLKeyMarkers` | XS |
| QA-B-14 | Add `AURA_TOOLSELECT_ORACLE` to `compose.yaml` | XS |
| QA-B-15 | Name the `500` and `6` literals in `title.go` | XS |
| QA-B-16 | Add immutability comment to `CanonicalBranchID` | XS |
| QA-B-07 | Update `askuser` package doc re: `MarkResumedBatch` transaction pattern | XS |
| QA-B-17 | Define `DefaultTurnCapBytes` constant in `conversations` | XS |
| QA-B-10 | Share the 1 GiB default via a named constant | XS |

Batching QA-B-01 + QA-B-02 + QA-B-03 into one "create `internal/neostore`" PR is the highest ROI single action (removes 3 High findings).

---

## D. Risky / Uncertain (missing evidence)

| ID | Finding | Uncertainty | Evidence needed |
|---|---|---|---|
| QA-B-04 | `numericFromFloat` / `floatFromNumeric` duplication | Confidence High; uncertainty is whether there are OTHER copies elsewhere not surveyed (e.g., `internal/cron` or `internal/runner`) | `grep -r numericFromFloat ./internal` to confirm only 2 sites |
| QA-B-05 | `parseUUID` proliferation — 5 packages | The `cron` variant (`parseUUID(s string)`) omits the field name parameter; unifying signatures requires updating all call sites | Confirm call site count before refactoring |
| QA-B-11 | Dev-default credentials | `compose.yaml` does pass these via `:?` mandatory vars (confirmed). The hardcoded Go defaults are only reached if `compose.yaml` is bypassed. Risk is CI scan noise, not a real credential exposure. | Run `govulncheck` or `semgrep` to confirm scanner noise level |
| QA-B-14 | `AURA_TOOLSELECT_ORACLE` not in compose | Low confidence it's a real operational gap — `toolselectlearn` was recently added and the compose file may be intentionally minimal for this knob | Confirm with PRD §Env vars whether the knob is intended for operator configuration |

---

## E. Verdict on Learn/Store Package Duplication

**The `activelearn` / `reasoninglearn` / `toolselectlearn` / `reasoningstore` / `toolselectstore` / `semindex` proliferation is intentional and architecturally correct. It is NOT dead code, NOT accidental duplication, and NOT an unrealized common substrate.**

The design is a correctly-layered hierarchy:

```
semindex           → pure math (cosine/centroid/margin/Classifier/Ranker)
   ↑
activelearn        → label-agnostic async queue/dedup/worker (shared mechanism)
   ↑                    ↑
reasoninglearn    toolselectlearn   → domain-specific oracle adapters
   ↑                    ↑
reasoningstore    toolselectstore   → domain-specific Neo4j persistence
```

The stated goal "unified semindex substrate" from the project memory is **already realized**: `semindex` is the unified core for both `reasoninglearn` (Classifier) and `toolselectlearn` (Ranker). `activelearn` is the unified async mechanism. The comment in `activelearn/activelearn.go` ("This honors REUSABLE-CODE for the mechanism while the divergent stores/oracles stay specialized — D-05") correctly describes the design intent.

The remaining duplication is **three private helper functions** (`hashText`/`hashQuery`, `asString`, `asFloats`) and **one interface** (`GraphClient`) that were copy-pasted between the two store packages instead of being extracted to a shared helper. This is actionable (QA-B-01, QA-B-02, QA-B-03) and amounts to approximately 50 LOC of verbatim copy. The packages themselves are not redundant.

**There is no case for collapsing `reasoningstore` and `toolselectstore` into a single generic store** — the node labels, field sets, LabeledVec shapes, and hash semantics (per-text vs per-query) differ in ways that would require type parameterization or an interface{}-based approach that is worse than the current typed duplication.

---

## F. Cross-Slice Flags

| Flag | Target Slice | Finding |
|---|---|---|
| `parseUUID` proliferation | All persistence slices (A, B, C) | The duplication extends beyond Slice B into `channels/telegram/store.go` — this is a cross-slice refactor target |
| `postgresTextSafe` | conversations + toolinvocations | Both are in Slice B; extraction to `internal/db` is within scope |
| `numericFromFloat`/`floatFromNumeric` | conversations + cachemetrics | Both Slice B; extraction to `internal/db` is within scope |
| Config `loadBase` growth | Slice B config + every future slice | Each new slice adds fields; the function will exceed 200 LOC by the end of the project — structural decomposition is time-sensitive |
| `reasoningstore` E2E CI guard (QA-B-12) | Test discipline | Compounds the CLAUDE.md "NO-SKIP-AS-GREEN in CI" rule; the inconsistency between `reasoningstore` and `toolselectstore` E2E tests should be fixed before the next CI run |
