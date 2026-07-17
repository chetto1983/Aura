---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 04
subsystem: security
tags: [token, crypto-rand, sha256, object-store, uuid, expiry, share, go, mutation-adjacent]

# Dependency graph
requires:
  - phase: 37F-01
    provides: "PRD-amendment WEBSHARE-01..04 + ADR 0039 authorizing this schema; D-04/D-12/D-13 locked decisions"
provides:
  - "internal/share.Mint() (string, [32]byte, error) + internal/share.Hash(string) [32]byte — the D-13 opaque 256-bit share token, mint+hash, no weak-fallback path"
  - "internal/share.ExpiryOption + internal/share.ResolveExpiry(opt, customDays, now, capDays) (time.Time, error) — D-04 pure expiry math with cap clamp, no wall-clock/env read"
  - "internal/objectstore.ShareSnapshotKey/ShareArtifactKey/ShareKeyPrefix(uuid.UUID...) string — D-12 token-scoped object-store key namespace under share/, lexically disjoint from AssetKey's identity/"
affects: [37F-06, 37F-10, 37F-11, share-service, public-share-page, share-revoke]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "RED-phase compiling stub for TDD tasks under a non-bypassable lefthook pre-commit go vet gate: ship the FINAL doc comments with a stub body that returns constant zero-value output, so the RED commit still compiles (satisfying vet) while every dependent test genuinely fails (satisfying TDD RED) — same technique 37F-02/37F-03 established"
    - "Grep-gated acceptance criteria are literal substring bans across the WHOLE file, including prose comments — a doc comment that explains 'why not bcrypt/argon2/scrypt/math/rand' by naming the banned alternative violates its own grep gate; describe the rejected alternative's properties instead of naming it"
    - "uuid.UUID-typed key-derivation functions as a structural traversal defense: ShareSnapshotKey/ShareArtifactKey/ShareKeyPrefix take uuid.UUID (not string, a deliberate deviation from the AssetKey sibling), making a hostile path-traversal string unrepresentable in the type rather than merely filtered"

key-files:
  created:
    - internal/share/token.go
    - internal/share/token_test.go
    - internal/share/expiry.go
    - internal/share/expiry_test.go
    - internal/objectstore/share_key_test.go
  modified:
    - internal/objectstore/types.go

key-decisions:
  - "Token entropy/format: 256-bit crypto/rand, base64.RawURLEncoding (unpadded, URL-safe), mirroring internal/agui/password_reset.go's newRecoveryCode verbatim — the exact shape has shipped twice already in this repo."
  - "Token hash: raw sha256.Sum256 output as [32]byte (not a base64/hex string) for the token_hash bytea column (migration 0040) — deliberately NOT bcrypt/argon2/scrypt, since a 256-bit crypto/rand token has no brute-force surface and a slow KDF only adds latency to every public share-page open."
  - "Token comparison is out of this plan's scope (no lookup/verify function here — Mint/Hash only); constant-time comparison via crypto/subtle or a hash-indexed equality lookup is the responsibility of the plan that reads token_hash back (37F-10)."
  - "Object-store keys derive from share_id+snapshot_id, never token_hash — token_hash is NULL for the internal tier (migration 0040's CHECK), and D-10 requires internal shares to resolve artifacts via the same snapshot as public ones; deriving a key from an authenticator would also couple key rotation to data movement."
  - "ShareSnapshotKey/ShareArtifactKey/ShareKeyPrefix take uuid.UUID, not string (a deliberate deviation from AssetKey) — a hostile \"../identity/<victim>/asset/x\" is unrepresentable in the type, not merely unlikely; plain string concatenation only, never path.Join (which normalizes \"..\" and would silently permit traversal)."
  - "Expiry: now and capDays are function parameters, never read from time.Now()/env internally, so ResolveExpiry stays deterministically testable and the caller (the share service) owns both the transaction clock and the config surface. A custom value above the cap clamps to the cap (fail-closed, friendlier than a hard reject); a non-positive custom value is rejected outright (it would mint an already-dead link, not an operator intent)."
  - "Did NOT add the downstream expiry-sweep/ShareExpirer seam — out of this plan's scope per its own prohibitions; that seam needs a store dependency this file deliberately lacks."
  - "Grep-gated acceptance criteria: two doc-comment rewordings were needed after the fact (see Deviations) because the plan's own verification greps ban certain substrings ANYWHERE in the file, including explanatory prose in comments, not just in code."

requirements-completed: [WEBSHARE-02]

coverage:
  - id: D1
    description: "Mint() returns a 256-bit opaque URL-safe token + its raw SHA-256 hash; a crypto/rand failure returns an error with no weaker fallback; 1000 mints prove no duplicate plaintexts, no duplicate hashes, no plaintext a prefix of another"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "internal/share/token_test.go#TestMintTokenEntropy (32-byte decode), #TestMintTokenURLSafe, #TestHashStability (== sha256.Sum256), #TestMintUniqueness (1000-iteration property), #TestMintRandFailureNoWeakFallback"
        status: pass
      - kind: unit
        ref: "go test -race ./internal/share/ -run 'TestMint|TestHash' -count=1"
        status: pass
      - kind: structural
        ref: "grep -nE \"bcrypt|argon2|scrypt|math/rand\" internal/share/token.go -> no matches; grep -q crypto/rand + base64.RawURLEncoding -> both present; grep -qE 'func Hash\\([^)]*\\) \\[32\\]byte' -> match"
        status: pass
    human_judgment: false
  - id: D2
    description: "ShareSnapshotKey/ShareArtifactKey/ShareKeyPrefix derive object-store keys under a share/ prefix lexically disjoint from AssetKey's identity/ prefix in both directions; every artifact key sits under its share's revoke prefix; keys take uuid.UUID so a traversal string is unrepresentable"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "internal/objectstore/share_key_test.go#TestShareSnapshotKeyShape, #TestShareArtifactKeyShape, #TestShareKeyNamespaceDisjoint (200-iteration property, both directions), #TestShareArtifactKeyUnderPrefix (200-iteration property)"
        status: pass
      - kind: unit
        ref: "go test -race ./internal/objectstore/ -count=1 (includes pre-existing TestAssetKeyContainsNoFilename, unregressed)"
        status: pass
      - kind: structural
        ref: "grep -nE 'path\\.Join|filepath\\.Join' internal/objectstore/types.go -> no matches; grep -cE 'func Share(SnapshotKey|ArtifactKey|KeyPrefix)\\([^)]*uuid\\.UUID' -> 3"
        status: pass
    human_judgment: false
  - id: D3
    description: "ResolveExpiry resolves default/7d to +7 days, 1d/30d to their fixed durations, a custom value within cap exactly, a custom value above cap clamped to the cap, and rejects a non-positive custom value; now/capDays are injected, never read internally"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "internal/share/expiry_test.go#TestResolveExpiry (10 table cases covering every <behavior> row) + #TestResolveExpiryUnrecognizedOption"
        status: pass
      - kind: unit
        ref: "go test -race ./internal/share/ -count=1 (whole package, includes plan 37F-03's snapshot/redact tests, unregressed)"
        status: pass
      - kind: structural
        ref: "grep -n 'time.Now()|os.Getenv|envutil\\.|ExpireDue|ShareExpirer' internal/share/expiry.go -> no matches"
        status: pass
    human_judgment: false

duration: 55min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 04: Share Token, Object-Key Namespace, and Expiry Primitives Summary

**`share.Mint`/`share.Hash` (256-bit crypto/rand token + raw SHA-256 hash), `objectstore.ShareSnapshotKey`/`ShareArtifactKey`/`ShareKeyPrefix` (uuid.UUID-typed, `share/`-prefixed, lexically disjoint from `identity/`), and `share.ResolveExpiry` (pure D-04 expiry math with cap clamp) — three I/O-free, fully-covered primitives with zero new dependencies**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-07-17T11:30Z (session start, after 37F-03)
- **Completed:** 2026-07-17T12:25Z
- **Tasks:** 3 planned, all completed as specified (TDD RED->GREEN each)
- **Files modified:** 6 (5 new, 1 modified)

## Accomplishments

- `internal/share.Mint() (string, [32]byte, error)` — mints a 256-bit opaque URL-safe share token via the exact `crypto/rand[32]` + `base64.RawURLEncoding` shape already shipped twice in this repo (`password_reset.go`'s `newRecoveryCode`, `onboarding_session.go`'s `newSessionToken`); a `crypto/rand` failure returns the error verbatim with no fallback to a weaker source
- `internal/share.Hash(string) [32]byte` — the raw SHA-256 digest for the `token_hash bytea` column (migration 0040), deliberately not bcrypt/argon2/scrypt (a 256-bit random token has no brute-force surface) and not `password_reset.go`'s IP-specific `hashRequestValue` base64-string encoding
- Proven over 1000 mints: all plaintexts distinct, all hashes distinct, no plaintext a prefix of another (`TestMintUniqueness`) — the RESEARCH.md token-opacity/uniqueness property stated as a loop rather than a `gopter`/`rapid` property (the statement is simple enough that a 1000-iteration loop states it exactly)
- `internal/objectstore.ShareSnapshotKey`/`ShareArtifactKey`/`ShareKeyPrefix` — three `AssetKey` siblings taking `uuid.UUID` (not `string`, a deliberate deviation), under a `share/` prefix proven lexically disjoint from `identity/` in BOTH directions (`TestShareKeyNamespaceDisjoint`, a 200-iteration property) and proven to keep every artifact key under its share's revoke prefix (`TestShareArtifactKeyUnderPrefix`) — the exact invariant a future revoke's `List(prefix)`+`Delete` depends on to reclaim every byte
- `internal/share.ResolveExpiry(opt, customDays, now, capDays) (time.Time, error)` — D-04's pure expiry math: default/`"7d"` -> now+7d, `"1d"`/`"30d"` -> their fixed durations, a custom value within cap resolves exactly, a custom value above cap clamps to the cap (fail-closed, friendlier than a hard reject), and a non-positive custom value is rejected via `ErrNonPositiveCustomExpiry`; `now` and `capDays` are parameters, never read internally, keeping the function deterministically testable and the caller in control of the clock and the config surface
- All three primitives are pure/I/O-free, tested with no build tag, no DB, no Garage: `internal/share` measures 97.4% coverage; the 3 new `internal/objectstore` functions measure 100.0% individually (`go tool cover -func`)
- Zero new dependencies: stdlib (`crypto/rand`, `crypto/sha256`, `encoding/base64`, `time`, `errors`, `fmt`) + the already-vendored `github.com/google/uuid`

## Task Commits

Each task was committed atomically (TDD RED -> GREEN, plus one fix commit discovered via the plan's own verification):

1. **Task 1 RED: failing tests for the share token mint/hash** - `681c7a128` (test)
2. **Task 1 GREEN: implement the share token mint and hash** - `6fd54a47a` (feat)
3. **Task 1 fix: reword token.go doc comments to clear the grep-gated ban** - `4e8ed2f47` (fix)
4. **Task 2 RED: failing namespace-disjointness tests for the share object keys** - `1463785d8` (test)
5. **Task 2 GREEN: add token-scoped share object key derivations** - `b68475451` (feat)
6. **Task 3 RED: failing expiry-math tests** - `6ca7f8f6e` (test)
7. **Task 3 GREEN: implement share expiry math with cap clamp** - `50d7db761` (feat)

**Plan metadata:** (this commit, docs: complete plan)

_Note: every RED commit ships a compiling stub (final doc comments, zero-value body) because the
lefthook pre-commit `go vet ./...` gate rejects a genuinely non-compiling commit — the same
accommodation 37F-02/37F-03 established. Each RED state was verified to fail its own package's
test run (`go test -run '<pattern>'`) before the stub was added._

## Files Created/Modified

- `internal/share/token.go` - `Mint()`/`Hash()` — the D-13 opaque token mint + raw SHA-256 hash
- `internal/share/token_test.go` - entropy/URL-safety/hash-stability/1000-mint-uniqueness tests
- `internal/share/expiry.go` - `ExpiryOption` + `ResolveExpiry` — D-04 pure expiry math with cap clamp
- `internal/share/expiry_test.go` - table-driven tests over every `<behavior>` expiry row
- `internal/objectstore/types.go` - added `ShareSnapshotKey`/`ShareArtifactKey`/`ShareKeyPrefix` as `AssetKey` siblings (+ `uuid` import)
- `internal/objectstore/share_key_test.go` - shape + namespace-disjointness + under-prefix tests

## Decisions Made

See frontmatter `key-decisions` for the full list. Summary: token = 256-bit `crypto/rand` +
`base64.RawURLEncoding`, hashed with a plain `sha256.Sum256` (no KDF — a random token has no
brute-force surface to slow down); object-store keys derive from `share_id`+`snapshot_id` (never
`token_hash`, which is authentication, not location) and are `uuid.UUID`-typed to make traversal
unrepresentable; expiry is pure math over injected `now`/`capDays`, clamping over-cap values and
rejecting non-positive ones, with the downstream sweep seam deliberately left for a later plan.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug, self-caught via the plan's own verification] `token.go` doc comments violated their own grep-gated ban**
- **Found during:** Task 1, immediately after the GREEN commit, while running the plan's own acceptance-criteria greps as a self-check
- **Issue:** Task 1's acceptance criteria require `grep -nE "bcrypt|argon2|scrypt|math/rand" internal/share/token.go` to return NOTHING. The GREEN commit's doc comments explained "why not bcrypt/argon2/scrypt" and "no fallback to math/rand" by naming those exact banned alternatives — the grep gate is a literal whole-file substring ban, not a code-only ban, so the explanatory prose tripped its own gate.
- **Fix:** Reworded the two doc-comment passages to describe the same rationale (no slow key-derivation function; no non-cryptographic pseudo-random fallback) without using the literal banned tokens. No behavior change — `Mint`/`Hash` function bodies were untouched.
- **Files modified:** `internal/share/token.go`
- **Verification:** `grep -nE "bcrypt|argon2|scrypt|math/rand" internal/share/token.go` returns nothing; `go test ./internal/share/ -run 'TestMint|TestHash'` and `golangci-lint run ./internal/share/...` both still green after the reword.
- **Committed in:** `4e8ed2f47`

**2. [Rule 1 - Bug, same class] `expiry.go`'s header comment violated its own grep-gated ban (caught before committing)**
- **Found during:** Task 3, before the RED commit, having learned from Deviation 1 above
- **Issue:** The draft header comment explained the file's design by naming `time.Now()` literally and by naming the deferred `ExpireDue`/`ShareExpirer` seam by name — Task 3's own acceptance criteria ban all three substrings anywhere in `expiry.go`.
- **Fix:** Reworded to "the wall clock" / "a downstream expiry-sweep/reaper seam" without the literal banned tokens, before the RED commit was made (no extra commit needed — caught pre-commit this time).
- **Files modified:** `internal/share/expiry.go` (pre-commit edit, folded into the Task 3 RED commit `6ca7f8f6e`)
- **Verification:** `grep -n "time.Now()\|os.Getenv\|envutil\.\|ExpireDue\|ShareExpirer" internal/share/expiry.go` returns nothing at every subsequent commit.
- **Committed in:** `6ca7f8f6e` (folded in, no separate commit)

---

**Total deviations:** 2 auto-fixed, both Rule 1 (self-caught via the plan's own grep-gated acceptance criteria, not externally reported bugs)
**Impact on plan:** Both are wording-only fixes with no behavior change; no scope creep, no architectural change.

## Issues Encountered

- **`internal/objectstore` package-wide coverage (63.9%) is below the plan's literal 85% acceptance threshold** — investigated during Task 2's own verification and found to be a **pre-existing condition unrelated to this plan**, not a regression: measured the baseline at commit `34a87f589` (pre-37F-04) by temporarily swapping in the old `types.go` and removing `share_key_test.go` via `git show`/`git checkout HEAD --` (both restored cleanly afterward, confirmed via `git status`/`git diff --stat` showing no residual diff) — baseline was 63.6%, this plan's change moved it to 63.9% (a net improvement). `go tool cover -func` confirms the 3 new functions (`ShareSnapshotKey`/`ShareArtifactKey`/`ShareKeyPrefix`) are each 100.0% covered, matching `AssetKey`. The package's remaining gap is entirely pre-existing S3/Garage (`s3.go`: `Put`/`Head`/`Get`/`List`/`Delete`/`ConfigureBrowserUploadCORS`, all 0.0%) and Postgres-backed (`identity_store.go`: `Put`/`Delete` 0.0%, `Resolve` 40.0%) code that needs live infrastructure and is out of this plan's scope (pure primitives, explicitly no DB/Garage). Logged in `.planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md` per the SCOPE BOUNDARY rule rather than fixed here. `internal/share` (this plan's other package) measures 97.4%, comfortably clearing the floor.
- **`TestMintTokenURLSafe` and `TestShareArtifactKeyUnderPrefix` trivially pass against their RED-phase stubs** — an accepted, documented artifact of the compiling-stub RED-phase pattern (an empty-string stub output happens to satisfy a "contains none of these forbidden characters" or a `HasPrefix("","")` check). The overall package test run still reports `FAIL` at each RED commit (verified and quoted in each RED commit message), which is what the plan's own `<verify>` blocks require; the other 3-4 sub-tests per file genuinely fail against the stub's constant zero-value output.

## User Setup Required

None - no external service configuration required. Both `internal/share` and the touched
`internal/objectstore` surface have zero external dependencies beyond stdlib + the
already-vendored `github.com/google/uuid`; no DB, no Garage, no network.

## Next Phase Readiness

- `share.Mint`/`share.Hash`/`share.ResolveExpiry` and `objectstore.ShareSnapshotKey`/
  `ShareArtifactKey`/`ShareKeyPrefix` are ready for the share-service plan (37F-10) that composes
  them: mint a token at share-create time, hash it for the `token_hash` column, derive the
  object-store key for the snapshot/artifacts from `share_id`+`snapshot_id`, and compute
  `expires_at` via `ResolveExpiry` fed from `ShareConfig.MaxExpiryDays` (37F-02).
- The expiry-sweep/reaper seam (comparable to `ExpireDue`/a `ShareExpirer` interface) is
  deliberately NOT built here — it belongs to a later plan that owns the store dependency.
- No blockers. `internal/share` still imports neither `internal/conversations` nor `internal/agui`.
  `internal/objectstore`'s package-wide coverage gap is pre-existing and logged in
  `deferred-items.md`, not a blocker for this plan's own deliverable.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 6 created/modified files (`internal/share/token.go`, `token_test.go`, `expiry.go`,
`expiry_test.go`, `internal/objectstore/share_key_test.go`, `types.go`) verified present on
disk; all 7 task commit hashes (`681c7a128`, `6fd54a47a`, `4e8ed2f47`, `1463785d8`, `b68475451`,
`6ca7f8f6e`, `50d7db761`) verified present in `git log --oneline --all`.
