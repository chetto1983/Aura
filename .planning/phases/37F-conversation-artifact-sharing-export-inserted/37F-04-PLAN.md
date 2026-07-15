---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 04
type: tdd
wave: 2
depends_on: ["37F-01"]
files_modified:
  - internal/share/token.go
  - internal/share/token_test.go
  - internal/share/expiry.go
  - internal/share/expiry_test.go
  - internal/objectstore/types.go
  - internal/objectstore/share_key_test.go
autonomous: true
requirements: [WEBSHARE-02]

must_haves:
  truths:
    - "Mint() returns a 256-bit opaque URL-safe token and its SHA-256 hash; the plaintext is returned to the caller exactly once and never stored"
    - "A crypto/rand failure returns an error and never falls back to a weaker source"
    - "Hash(token) is stable: the same token always yields the same 32 raw bytes"
    - "Two mints never collide, and no plaintext is a prefix or substring of another"
    - "A share key can never address an identity object, and an identity key can never address a share object — the prefixes are lexically disjoint"
    - "ShareSnapshotKey/ShareArtifactKey take uuid.UUID, so a traversal string like ../identity/<victim>/asset/x is unrepresentable in the type"
    - "The default expiry is 7 days; a custom expiry above AURA_SHARE_MAX_EXPIRY_DAYS clamps to the cap"
  artifacts:
    - path: "internal/share/token.go"
      provides: "Mint() (plaintext, hash, error) + Hash(plaintext) [32]byte"
      exports: ["Mint", "Hash"]
    - path: "internal/share/expiry.go"
      provides: "symbolic expiry (1d/7d/30d/custom) → time, cap clamp"
    - path: "internal/objectstore/types.go"
      provides: "ShareSnapshotKey / ShareArtifactKey / ShareKeyPrefix under a disjoint share/ prefix"
      exports: ["ShareSnapshotKey", "ShareArtifactKey", "ShareKeyPrefix"]
  key_links:
    - from: "internal/objectstore/types.go"
      to: "the share/ namespace"
      via: "string prefix disjoint from identity/"
      pattern: "\"share/\""
  prohibitions:
    - "MUST NOT invent a token scheme — copy the shipped crypto/rand[32] + base64.RawURLEncoding pattern from password_reset.go:407-415"
    - "MUST NOT use bcrypt/argon2/scrypt to hash the token — a 256-bit random token has no brute-force surface; D-13 says SHA-256"
    - "MUST NOT fall back to a weaker random source on a rand.Read error — return the error"
    - "MUST NOT derive a blob key from token_hash — the internal tier has no token, and D-10 requires internal shares to resolve artifacts via the same snapshot"
    - "MUST NOT use path.Join to build a key — it normalizes .. and would silently permit traversal; plain concatenation only, matching AssetKey"
    - "MUST NOT log, %w-wrap, or return a plaintext token in any error"
    - "MUST NOT add a build tag to any test file in this plan"
    - "MUST NOT add the ExpireDue/ShareExpirer seam here — that is plan 37F-11; this file holds pure expiry math only"
---

<objective>
Build the three pure primitives the share lifecycle needs: the opaque token (mint + hash), the
token-scoped object-store key derivations, and the expiry math.

All three are pure functions with no I/O — which makes them fast, deterministic, and fully covered
without a DB or Garage. Two of them are security-load-bearing: the token is the only capability that
opens the public tier, and the key derivation is what keeps a recipient's token from ever addressing
another identity's bytes.

Purpose: the primitives, correct and provable, before anything composes them.
Output: `internal/share/token.go`, `internal/share/expiry.go`, `internal/objectstore/types.go` (+3 funcs).
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@CLAUDE.md
</context>

## Artifacts this plan produces

`share.Mint`, `share.Hash`, `share.ExpiryOption` + the expiry math, `objectstore.ShareSnapshotKey`,
`objectstore.ShareArtifactKey`, `objectstore.ShareKeyPrefix`.

<feature>
  <name>Share token, key namespace, and expiry primitives</name>
  <files>internal/share/token.go, internal/share/expiry.go, internal/objectstore/types.go, internal/share/token_test.go, internal/share/expiry_test.go, internal/objectstore/share_key_test.go</files>

  <behavior>
    **Token (D-13)**
    - `Mint()` returns `(plaintext string, hash [32]byte, err error)`
    - `plaintext` decodes (base64 RawURL) to exactly 32 bytes — 256 bits
    - `hash == sha256.Sum256([]byte(plaintext))`
    - A `crypto/rand` failure ⇒ `err != nil`, empty plaintext, and NO weaker fallback
    - `Hash(p)` is deterministic and stable across calls
    - Over 1000 mints: all plaintexts distinct; all hashes distinct; no plaintext is a prefix of another
    - `plaintext` is URL-safe: no `+`, `/`, or `=` (RawURLEncoding, unpadded)

    **Object keys (D-12 / OQ1)**
    - `ShareSnapshotKey(shareID, snapshotID)` = `share/<shareID>/snapshot/<snapshotID>/canonical.json`
    - `ShareArtifactKey(shareID, snapshotID, assetID)` = `share/<shareID>/snapshot/<snapshotID>/asset/<assetID>`
    - `ShareKeyPrefix(shareID)` = `share/<shareID>/`
    - All three take `uuid.UUID`, not `string`
    - No share key ever starts with `identity/`; `AssetKey(...)` never starts with `share/`
    - `ShareKeyPrefix(a) != ShareKeyPrefix(b)` for `a != b`
    - Every `ShareArtifactKey(s, n, *)` has `ShareKeyPrefix(s)` as a prefix — this is what makes
      revoke's `List(prefix)` + `Delete` complete
    - No key contains `..` or `\`

    **Expiry (D-04)**
    - Default (absent/empty option) ⇒ now + 7 days
    - `1d`/`7d`/`30d` ⇒ now + that duration
    - custom N days ⇒ now + N days, when 0 < N ≤ cap
    - custom N > cap ⇒ clamped to now + cap days (never rejected — clamping is friendlier and still
      fail-closed)
    - custom N ≤ 0 ⇒ rejected with an error (a zero/negative expiry is not an operator intent; it would
      mint an already-dead link)
    - The function takes `now time.Time` and `capDays int` as parameters — never reads the wall clock or
      the env itself
  </behavior>

  <implementation>
    RED → GREEN → REFACTOR, one atomic commit per phase. All tests are plain unit tests: **no build tag,
    no DB, no Garage**.

    `token.go` copies `internal/agui/password_reset.go:407-415` (`newRecoveryCode`) verbatim for the
    mint — `var b [32]byte` + `rand.Read(b[:])` + `base64.RawURLEncoding.EncodeToString(b[:])`. That is
    exactly D-13's 256-bit URL-safe opaque token, already shipped twice. **Deviate on one axis:** OQ5
    stores `token_hash bytea` (raw 32 bytes), so `Hash` returns `[32]byte` from `sha256.Sum256` — NOT the
    base64 string `hashRequestValue` returns. Drop `hashRequestValue`'s trim/`SplitHostPort` preamble
    entirely; it is IP-specific and meaningless here. Take `password_reset.go`'s base64url over
    `onboarding_session.go`'s hex: this token rides in a URL. Keep `onboarding_session.go:92-95`'s
    rand-failure doc discipline — the error returns, never a fallback.

    `objectstore/types.go` gains the three funcs as siblings of `AssetKey` (`:60-62`), three lines below
    it. Copy `AssetKey`'s shape: package-level func, plain concatenation, no `path.Join`, no error return.
    **Deviate on one axis:** take `uuid.UUID` rather than `string`, so a hostile
    `"../identity/<victim>/asset/x"` is unrepresentable in the type. Note the deviation in the doc — it
    reads as an accidental inconsistency with `AssetKey` otherwise.

    `expiry.go` is pure math with injected `now` and `capDays`.
  </implementation>
</feature>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: RED+GREEN — token mint/hash</name>
  <read_first>
    - `internal/agui/password_reset.go:407-425` — `newRecoveryCode` (copy verbatim) + `hashRequestValue` (copy the sha256 call, drop the trim/SplitHostPort preamble; change the return from base64 string to `[32]byte`)
    - `internal/agui/onboarding_session.go:88-98` — `newSessionToken`: the rand-failure doc discipline to keep ("A rand failure is surfaced so the caller fails the start request rather than minting a weak token")
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"Don't Hand-Roll" (the token + hashing rows, and why a slow KDF buys nothing here) + §"Property-based testing" (the token opacity/uniqueness property)
  </read_first>
  <action>
    RED: create `internal/share/token_test.go`, package `share`, **no build tag**. Cover every token row
    of `<behavior>`, including:
    - `TestMintTokenEntropy` — decodes to exactly 32 bytes
    - `TestMintTokenURLSafe` — no `+`, `/`, `=` in the plaintext
    - `TestHashStability` — `Hash(p)` equal across calls, and `== sha256.Sum256([]byte(p))`
    - `TestMintUniqueness` — the property from RESEARCH: over 1000 mints, all plaintexts distinct, all
      hashes distinct, no plaintext a prefix of another (a table/loop property test; `gopter`/`rapid` is
      not required for a statement this simple — a 1000-iteration loop states it exactly)
    Run: tests fail. Commit: `test(37F-04): add failing tests for the share token mint/hash`

    GREEN: create `internal/share/token.go` with `Mint() (string, [32]byte, error)` and
    `Hash(string) [32]byte`.

    Doc the two non-obvious WHYs:
    - **Why SHA-256 and not bcrypt/argon2:** a 256-bit `crypto/rand` token has no brute-force surface, so
      a slow KDF buys nothing and costs per-request latency on every public-page open. This is session/
      API-key discipline, not password discipline.
    - **Why the plaintext is returned, never stored:** it is shown to the owner once at creation and
      thereafter lives only in the URL (D-13). Add the standing rule that a plaintext token is never
      logged and never `%w`-wrapped into an error.

    Run tests green + `-race`. Commit: `feat(37F-04): implement the share token mint and hash`
  </action>
  <verify>
    <automated>go test ./internal/share/ -run 'TestMint|TestHash' -count=1 && go test -race ./internal/share/ -run 'TestMint|TestHash' -count=1 && go vet ./internal/share/</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/share/ -run 'TestMint|TestHash' -count=1` passes; `TestMintUniqueness` runs ≥1000 iterations.
    - `grep -nE "bcrypt|argon2|scrypt|math/rand" internal/share/token.go` returns NOTHING.
    - `grep -q "crypto/rand" internal/share/token.go` and `grep -q "base64.RawURLEncoding" internal/share/token.go` both succeed.
    - `Hash` returns `[32]byte` (raw), not a string: `grep -qE "func Hash\([^)]*\) \[32\]byte" internal/share/token.go`.
    - The rand-failure path returns an error: `grep -A3 "rand.Read" internal/share/token.go | grep -q "return"`.
    - `internal/share/token_test.go` carries no `//go:build` line.
    - `golangci-lint run ./internal/share/` → 0 issues.
  </acceptance_criteria>
  <done>`token.go` mints a 256-bit URL-safe opaque token via the shipped `crypto/rand`+base64url pattern, returns the raw 32-byte SHA-256 hash, errors (never degrades) on a rand failure, and is proven unique over 1000 mints.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: RED+GREEN — token-scoped object-store key namespace (D-12)</name>
  <read_first>
    - `internal/objectstore/types.go:60-62` — `AssetKey(identityID, assetID string) string` returning `"identity/"+identityID+"/asset/"+assetID+"/original"`. The exact shape to mirror, three lines above the insertion point. Note: no `path.Join`, no error return.
    - `internal/objectstore/objectstore_test.go:12-22` — `TestAssetKeyContainsNoFilename`: the house negative-substring invariant (exact-equality assert THEN a forbidden-substring loop). Mirror this shape exactly.
    - `internal/objectstore/identity_store.go:81,151-153` — `Resolve(ctx)` reads the identity from ctx and `isShared("")` returns true for an empty principal. Read this to understand WHY the share store is built from the SHARED credentials at the composition root rather than via `Resolve` — the fallback would work by accident; plan 37F-10 makes it intentional.
    - `internal/objectstore/fake.go:17-21` — `FakeStore`/`NewFake()`: keyed on `ObjectRef`, so `share/` keys round-trip today with no change. This is what keeps 37F inside the two-tag coverage gate — confirm it needs no edit.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ1 — the derivation, the four reasons to key on `share_id` rather than `token_hash`, and the "public-readable ≠ S3-public ACL" clarification
  </read_first>
  <action>
    RED: create `internal/objectstore/share_key_test.go`, package `objectstore`, **no build tag**.
    Mirror `TestAssetKeyContainsNoFilename`'s shape. Cover:
    - `TestShareSnapshotKeyShape` / `TestShareArtifactKeyShape` — exact-equality against the expected
      string for fixed UUIDs, THEN a forbidden-substring loop over `{"identity/", "..", "\\"}`
    - `TestShareKeyNamespaceDisjoint` — the D-12 property: for many generated uuid triples,
      `!strings.HasPrefix(ShareSnapshotKey(a,b), "identity/")`, `!strings.HasPrefix(AssetKey(x,y), "share/")`,
      and `ShareKeyPrefix(a) != ShareKeyPrefix(b)` for `a != b`
    - `TestShareArtifactKeyUnderPrefix` — `strings.HasPrefix(ShareArtifactKey(s,n,x), ShareKeyPrefix(s))`
      for all inputs. This is the invariant revoke depends on: `List(prefix)` + `Delete` only reclaims
      every byte if every artifact key sits under the share's prefix.
    Run: fail. Commit: `test(37F-04): add failing namespace-disjointness tests for the share object keys`

    GREEN: add the three funcs to `internal/objectstore/types.go` as siblings of `AssetKey`.
    Signatures take `uuid.UUID`. Bodies are plain concatenation with `.String()`.

    Doc the three non-obvious WHYs:
    - **Why `uuid.UUID` and not `string` (a deliberate deviation from `AssetKey`):** it makes a hostile
      `"../identity/<victim>/asset/x"` unrepresentable in the type rather than merely unlikely.
    - **Why the key derives from `share_id`+`snapshot_id` and NOT `token_hash`:** `token_hash` is
      public-tier-only (NULL for internal), so a hash-derived key would leave internal-tier shares
      unaddressable — but D-10 requires internal shares to resolve artifacts via the same snapshot. Also:
      D-06's "Update" needs a NEW snapshot blob without destroying the old one mid-write, which
      `snapshot_id` gives (immutable blobs + an atomic pointer swap in the row); and the token is an
      *authenticator* while the key is a *locator* — deriving one from the other couples rotation to data
      movement.
    - **Why the `share/` prefix is lexically disjoint from `identity/`:** a share key can never address an
      identity object and vice versa, and it makes a future dedicated-bucket split a one-line change.

    Run tests green. Commit: `feat(37F-04): add token-scoped share object key derivations`
  </action>
  <verify>
    <automated>go test ./internal/objectstore/ -run 'TestShare|TestAssetKey' -count=1 && go test -race ./internal/objectstore/ -count=1 && go vet ./internal/objectstore/ && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/objectstore/ -run 'TestShare|TestAssetKey' -count=1` passes; the pre-existing `TestAssetKeyContainsNoFilename` still passes unchanged.
    - `grep -nE "path\.Join|filepath\.Join" internal/objectstore/types.go` returns NOTHING.
    - The three funcs take `uuid.UUID`: `grep -cE "func Share(SnapshotKey|ArtifactKey|KeyPrefix)\([^)]*uuid\.UUID" internal/objectstore/types.go` returns `3`.
    - `grep -q '"share/"' internal/objectstore/types.go` succeeds.
    - `TestShareKeyNamespaceDisjoint` exists and asserts BOTH directions (share key never `identity/`-prefixed AND asset key never `share/`-prefixed).
    - `TestShareArtifactKeyUnderPrefix` exists — the revoke-completeness invariant.
    - `internal/objectstore/fake.go` is UNCHANGED: `git diff --name-only` does not list it.
    - `internal/objectstore/share_key_test.go` carries no `//go:build` line.
    - `internal/objectstore/types.go` ≤ 600 LOC (was 62); `bash scripts/check-file-size.sh` exits 0.
    - `go test ./internal/objectstore/ -cover -count=1` reports ≥ 85%.
  </acceptance_criteria>
  <done>`ShareSnapshotKey`/`ShareArtifactKey`/`ShareKeyPrefix` exist as `AssetKey` siblings taking `uuid.UUID`, under a `share/` prefix proven lexically disjoint from `identity/` in both directions, with every artifact key proven to sit under its share's prefix.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: RED+GREEN — expiry math with cap clamp (D-04)</name>
  <read_first>
    - `internal/config/config_share.go` — `ShareConfig.MaxExpiryDays` (default 90), created in plan 37F-02. This file must NOT read it directly; the cap is a parameter.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-CONTEXT.md` D-04 — default 7 days; owner-selectable 1d/7d/30d/custom up to a max cap; revoke independent of expiry
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ3 — why lazy enforcement is the security gate (this file supplies the value the gate compares against) and the "expiry monotonicity" property
  </read_first>
  <action>
    RED: create `internal/share/expiry_test.go`, package `share`, **no build tag**. Table-driven over
    every expiry row of `<behavior>`: default ⇒ +7d; `1d`/`7d`/`30d`; custom within cap; custom above cap
    ⇒ clamped to cap; custom ≤ 0 ⇒ error. Use a fixed `now` — never `time.Now()` — so the assertions are
    exact rather than approximate.
    Run: fail. Commit: `test(37F-04): add failing expiry-math tests`

    GREEN: create `internal/share/expiry.go`. Define the symbolic option type and
    `ResolveExpiry(opt ExpiryOption, customDays int, now time.Time, capDays int) (time.Time, error)`.

    Take `now` and `capDays` as parameters. Doc why: a function that reads the wall clock or the env is
    not deterministically testable, and the caller (the service) already owns the transaction clock. Also
    note that the **DB clock** (`now()`), not this value, is what the lazy resolve predicate compares
    against — a skewed app clock must not be able to resurrect a link; this function only computes the
    value written at mint time.

    Clamp rather than reject an over-cap custom value (friendlier, still fail-closed); reject ≤0 (it
    would mint an already-dead link — not an operator intent).

    **Do NOT add the `ExpireDue`/`ShareExpirer` seam here** — that is plan 37F-11, and it needs the store.
    This file is pure math only.

    Run tests green + `-race`. Commit: `feat(37F-04): implement share expiry math with cap clamp`
  </action>
  <verify>
    <automated>go test ./internal/share/ -count=1 && go test -race ./internal/share/ -count=1 && go vet ./internal/share/ && golangci-lint run ./internal/share/ ./internal/objectstore/</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/share/ -count=1` passes — the whole package, including plan 37F-03's snapshot tests (this plan must not regress them).
    - Every `<behavior>` expiry row has a table case, including custom-above-cap ⇒ clamped and custom ≤0 ⇒ error.
    - `grep -n "time.Now()" internal/share/expiry.go` returns NOTHING — `now` is injected.
    - `grep -n "os.Getenv\|envutil\." internal/share/expiry.go` returns NOTHING — the cap is injected.
    - `grep -n "ExpireDue\|ShareExpirer" internal/share/expiry.go` returns NOTHING — that seam belongs to plan 37F-11.
    - `go test ./internal/share/ -cover -count=1` reports ≥ 85%.
    - `golangci-lint run ./internal/share/ ./internal/objectstore/` → 0 issues.
    - `go build ./...` succeeds.
  </acceptance_criteria>
  <done>`ResolveExpiry` computes the mint-time expiry from a symbolic option with an injected clock and cap, defaulting to 7 days, clamping above the cap, and rejecting a non-positive custom value.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| token plaintext → persistence | The plaintext crosses to the owner exactly once, in the create response body. It must never reach a column, a log line, an error string, or an audit row. |
| recipient token → object namespace | The key derivation is what stops a token from ever addressing `identity/<owner>/asset/…`. The disjoint prefix is structural, not a check. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-02 | Spoofing | token guessing / enumeration | mitigate | 256-bit `crypto/rand` opaque token (`Mint`), base64url, no sequential or derivable component. Proven distinct + non-prefixing over 1000 mints. |
| T-37F-24 | Information Disclosure | weak token on a rand failure | mitigate | `rand.Read` error returns; no fallback to `math/rand` or a time seed. Grep-gated (`math/rand` must not appear). |
| T-37F-11 | Information Disclosure | plaintext token at rest / in logs | mitigate | `Hash` returns raw 32 bytes for the `bytea` column; the plaintext is returned to the caller only. Standing rule documented: never logged, never `%w`-wrapped. |
| T-37F-05 | Information Disclosure | cross-identity read via a token-addressed blob | mitigate | `share/` prefix lexically disjoint from `identity/`, proven in both directions by `TestShareKeyNamespaceDisjoint`. |
| T-37F-25 | Elevation of Privilege | path traversal in a blob key | mitigate | Keys take `uuid.UUID`, so `"../identity/<victim>/asset/x"` is unrepresentable; no `path.Join` (which would normalize `..` and silently permit it); forbidden-substring loop asserts no `..` or `\`. |
| T-37F-07 | Information Disclosure | bytes surviving revoke | mitigate | `TestShareArtifactKeyUnderPrefix` proves every artifact key sits under `ShareKeyPrefix(shareID)`, so revoke's `List(prefix)`+`Delete` reclaims all of them. |
| T-37F-26 | Denial of Service | slow KDF on every public-page open | mitigate | SHA-256, not bcrypt/argon2 — a 256-bit random token has no brute-force surface, so a KDF buys nothing and costs latency on an unauthenticated route. Grep-gated. |
| T-37F-27 | Tampering | an already-dead or effectively-permanent link | mitigate | `ResolveExpiry` rejects a non-positive custom value and clamps above `AURA_SHARE_MAX_EXPIRY_DAYS`. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | Stdlib (`crypto/rand`, `crypto/sha256`, `encoding/base64`, `time`) + the already-vendored `uuid`. No new dependency. |
</threat_model>

<verification>
- `go test ./internal/share/ ./internal/objectstore/ -count=1`
- `go test -race ./internal/share/ ./internal/objectstore/ -count=1`
- `go vet ./internal/share/ ./internal/objectstore/ && go build ./...`
- `golangci-lint run ./internal/share/ ./internal/objectstore/` → 0 issues
- `go test ./internal/share/ ./internal/objectstore/ -cover -count=1` → both ≥ 85%
- `bash scripts/check-file-size.sh` → 0
</verification>

<success_criteria>
The token is a 256-bit opaque URL-safe value hashed to raw 32 bytes with no weak-fallback path; the
share key namespace is provably disjoint from the identity namespace in both directions and every
artifact key sits under its share's revoke prefix; expiry defaults to 7 days and clamps to the operator
cap. All three are pure, deterministic, and covered with no build tag, no DB, and no Garage.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-04-SUMMARY.md` when done.
</output>
